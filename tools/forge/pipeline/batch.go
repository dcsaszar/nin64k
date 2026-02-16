package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"forge/analysis"
	"forge/build"
	"forge/encode"
	"forge/parse"
	"forge/serialize"
	"forge/simulate"
	"forge/solve"
	"forge/transform"
	"forge/verify"
)

func RunBatch(cfg *Config) {
	songNames := []string{
		"d1p", "d2p", "d3p", "d4p", "d5p", "d6p", "d7p", "d8p", "d9p",
	}

	var songs [9]*ProcessedSong
	var rawData [9][]byte
	var parsedSongs [9]parse.ParsedSong
	var analyses [9]analysis.SongAnalysis

	fmt.Println("=== Parse and analyze all songs ===")
	var allAnalyses []analysis.SongAnalysis
	for i, name := range songNames {
		inputPath := cfg.ProjectPath(fmt.Sprintf("uncompressed/%s.raw", name))
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			fmt.Printf("  %s: skipped (not found)\n", name)
			continue
		}

		fmt.Printf("  %s: %d bytes\n", name, len(raw))

		song := parse.Parse(raw)
		if err := verify.Parse(song); err != nil {
			fmt.Printf("FATAL: %s parse verification failed:\n%v\n", name, err)
			os.Exit(1)
		}

		anal := analysis.Analyze(song, raw)
		if err := verify.Analysis(song, anal); err != nil {
			fmt.Printf("FATAL: %s analysis verification failed:\n%v\n", name, err)
			os.Exit(1)
		}

		rawData[i] = raw
		parsedSongs[i] = song
		analyses[i] = anal
		allAnalyses = append(allAnalyses, anal)
	}

	fmt.Println("\n=== Build global effect remap ===")
	globalEffectRemap, globalFSubRemap, permArpEffect, portaUpEffect, _, tonePortaEffect := transform.BuildGlobalEffectRemap(allAnalyses)
	transformOpts := transform.TransformOptions{
		PermanentArp:     false,
		PermArpEffect:    permArpEffect,
		MaxPermArpRows:   0,
		PersistPorta:     false,
		PortaUpEffect:    portaUpEffect,
		PortaDownEffect:  0,
		PersistTonePorta: false,
		TonePortaEffect:  tonePortaEffect,
		OptimizeInst:     false,
	}
	fmt.Println("  Effect remap (orig -> new):")
	for orig := 0; orig < 16; orig++ {
		if globalEffectRemap[orig] != 0 || orig == 0 {
			fmt.Printf("    %X -> %d\n", orig, globalEffectRemap[orig])
		}
	}
	fmt.Println("  F sub-effect remap:")
	for code, newEff := range globalFSubRemap {
		fmt.Printf("    0x%X -> %d\n", code, newEff)
	}

	fmt.Println("\n=== Build equiv maps ===")
	equivMaps := make([]map[string]string, len(songNames))
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		patterns, truncateLimits := transform.ExtractRawPatternsAsBytes(parsedSongs[i], analyses[i], rawData[i])
		equivMaps[i] = encode.BuildEquivHexMap(cfg.ProjectRoot, i+1, patterns, truncateLimits)
		if len(equivMaps[i]) > 0 {
			fmt.Printf("  %s: %d mappings\n", name, len(equivMaps[i]))
		}
	}

	fmt.Println("\n=== Apply equiv (pre-transform fixup) ===")
	hasEquiv := false
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		if len(equivMaps[i]) > 0 {
			fmt.Printf("  %s: %d row substitutions\n", name, len(equivMaps[i]))
			hasEquiv = true
		}
	}
	if !hasEquiv {
		fmt.Println("  (none)")
	}

	fmt.Println("\n=== Transform (effect + inst remap) ===")
	transformedSongs := make([]transform.TransformedSong, len(songNames))
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		songOpts := transformOpts
		songOpts.EquivMap = equivMaps[i]
		transformedSongs[i] = transform.TransformWithGlobalEffects(
			parsedSongs[i], analyses[i], rawData[i],
			globalEffectRemap, globalFSubRemap, songOpts,
		)
		usedOrig := len(analyses[i].UsedInstruments)
		fmt.Printf("  %s: %d patterns, %d instruments (was %d)\n",
			name, len(transformedSongs[i].Patterns), transformedSongs[i].MaxUsedSlot, usedOrig)
	}

	fmt.Println("\n=== Selective persistent FX optimization ===")
	for _, eff := range transform.PersistentPlayerEffects() {
		converted := 0
		for i := range songNames {
			if rawData[i] == nil {
				continue
			}
			var c int
			transformedSongs[i].Patterns, c = transform.OptimizePersistentFXSelective(
				transformedSongs[i].Patterns, transformedSongs[i].Orders, eff)
			converted += c
		}
		if converted > 0 {
			fmt.Printf("  %s: %d rows -> NOP\n", transform.PlayerEffectName(eff), converted)
		}
	}

	fmt.Println("\n=== Verify remapped patterns ===")
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		if err := verify.Transform(parsedSongs[i], analyses[i], transformedSongs[i], rawData[i]); err != nil {
			fmt.Printf("FATAL: %s verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
	}
	fmt.Println("  All verified")

	fmt.Println("\n=== Reorder patterns ===")
	encodeStates := make([]EncodeState, len(songNames))
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		patterns, truncateLimits, reorderMap := encode.ConvertPatternsToBytes(transformedSongs[i], true)
		encodeStates[i] = EncodeState{
			Patterns:       patterns,
			TruncateLimits: truncateLimits,
			ReorderMap:     reorderMap,
		}
		fmt.Printf("  %s: %d patterns\n", name, len(patterns))
	}

	fmt.Println("\n=== Build row dictionary ===")
	maxDictSize := 0
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		es := &encodeStates[i]
		origDict := encode.BuildDictionary(es.Patterns, es.TruncateLimits)
		es.RowToIdx = encode.BuildRowToIdx(origDict)
		es.NoteOnlyRows = encode.FindNoteOnlyRows(es.Patterns, es.TruncateLimits)
		compactDict, oldToNew := encode.CompactDictionaryWithNoteOnly(origDict, es.RowToIdx, es.Patterns, es.TruncateLimits, nil, es.NoteOnlyRows)
		es.Dict = compactDict

		es.RowToIdx = make(map[string]int)
		es.RowToIdx[string([]byte{0, 0, 0})] = 0
		numCompact := len(compactDict) / 3
		for idx := 1; idx < numCompact; idx++ {
			row := string(compactDict[idx*3 : idx*3+3])
			es.RowToIdx[row] = idx
		}

		numOrig := len(origDict) / 3
		for oldIdx := 1; oldIdx < numOrig; oldIdx++ {
			row := string(origDict[oldIdx*3 : oldIdx*3+3])
			if _, exists := es.RowToIdx[row]; !exists {
				if newIdx, hasMapping := oldToNew[oldIdx]; hasMapping {
					es.RowToIdx[row] = newIdx
				}
			}
		}

		dictSize := len(es.Dict) / 3
		if dictSize > maxDictSize {
			maxDictSize = dictSize
		}
		fmt.Printf("  %s: dict=%d, note-only=%d\n", name, dictSize, len(es.NoteOnlyRows))
	}
	serialize.DictArraySize = maxDictSize - 1
	fmt.Printf("  ROW_DICT_SIZE = %d\n", serialize.DictArraySize)

	fmt.Println("\n=== Pack patterns ===")
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		es := &encodeStates[i]
		canonPatterns, canonTruncate, patternToCanon := encode.DeduplicatePatternsWithEquiv(
			es.Patterns, es.Dict, es.RowToIdx, es.TruncateLimits, nil)
		packedData, gapCodes, primaryCount, extendedCount := encode.PackPatternsWithNoteOnly(
			canonPatterns, es.Dict, es.RowToIdx, canonTruncate, nil, es.NoteOnlyRows)
		es.CanonPatterns = packedData
		es.CanonGapCodes = gapCodes
		es.PatternToCanon = patternToCanon
		es.PrimaryCount = primaryCount
		es.ExtendedCount = extendedCount
		fmt.Printf("  %s: %d canonical patterns\n", name, len(canonPatterns))
	}

	fmt.Println("\n=== Optimize overlap ===")
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		es := &encodeStates[i]
		es.PackedPatterns, es.PatternOffsets = encode.OptimizeOverlap(es.CanonPatterns)
		fmt.Printf("  %s: %d bytes saved\n", name, encode.OverlapStats.BytesSaved)
	}

	fmt.Println("\n=== Encode orders ===")
	encodedSongs := make([]encode.EncodedSong, len(songNames))
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		es := &encodeStates[i]
		transpose, trackptr, trackStarts := encode.EncodeOrdersWithRemap(transformedSongs[i], es.ReorderMap)
		instData := encode.EncodeInstruments(transformedSongs[i].Instruments, transformedSongs[i].MaxUsedSlot)

		encodedSongs[i] = encode.EncodedSong{
			RowDict:        es.Dict,
			RowToIdx:       es.RowToIdx,
			NoteOnlyRows:   es.NoteOnlyRows,
			RawPatterns:    es.Patterns,
			TruncateLimits: es.TruncateLimits,
			PackedPatterns: es.PackedPatterns,
			CanonPatterns:  es.CanonPatterns,
			CanonGapCodes:  es.CanonGapCodes,
			PatternCanon:   es.PatternToCanon,
			TempTranspose:  transpose,
			TempTrackptr:   trackptr,
			TrackStarts:    trackStarts,
			InstrumentData: instData,
			PrimaryCount:   es.PrimaryCount,
			ExtendedCount:  es.ExtendedCount,
		}

		numPat := len(es.Patterns)
		encodedSongs[i].PatternData = make([][]byte, numPat)
		encodedSongs[i].PatternGapCodes = make([]byte, numPat)
		encodedSongs[i].PatternOffsets = make([]uint16, numPat)
		for p := 0; p < numPat; p++ {
			canonIdx := es.PatternToCanon[p]
			encodedSongs[i].PatternData[p] = es.CanonPatterns[canonIdx]
			encodedSongs[i].PatternGapCodes[p] = es.CanonGapCodes[canonIdx]
			if canonIdx < len(es.PatternOffsets) {
				encodedSongs[i].PatternOffsets[p] = es.PatternOffsets[canonIdx]
			}
		}
		encodedSongs[i].RawPatternsEquiv = es.Patterns

		fmt.Printf("  %s: %d instruments\n", name, transformedSongs[i].MaxUsedSlot)
	}

	fmt.Println("\n=== Verify packed patterns ===")
	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		if err := verify.Encode(transformedSongs[i], encodedSongs[i]); err != nil {
			fmt.Printf("FATAL: %s encode verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
		if err := verify.PatternSemantics(transformedSongs[i], encodedSongs[i]); err != nil {
			fmt.Printf("FATAL: %s semantic verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
		if err := verify.DictionaryInstruments(transformedSongs[i], encodedSongs[i]); err != nil {
			fmt.Printf("FATAL: %s dictionary verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
		if err := verify.FilterTableRemap(transformedSongs[i], encodedSongs[i]); err != nil {
			fmt.Printf("FATAL: %s filter verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
		if err := verify.ArpTableRemap(transformedSongs[i], encodedSongs[i]); err != nil {
			fmt.Printf("FATAL: %s arp verification failed:\n%v\n", name, err)
			os.Exit(1)
		}
	}
	fmt.Println("  All verified")

	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		songs[i] = &ProcessedSong{
			Name:        name,
			Raw:         rawData[i],
			Song:        parsedSongs[i],
			Anal:        analyses[i],
			Transformed: transformedSongs[i],
			Encoded:     encodedSongs[i],
		}
	}

	fmt.Println("\n=== Collect delta and transpose sets ===")
	var baseDeltaSets [9][]int
	var trackStarts [9][3]byte
	var transposeSets [9][]int8

	for i, ps := range songs {
		if ps == nil {
			continue
		}
		numOrders := len(ps.Anal.ReachableOrders)
		baseDeltaSets[i] = encode.ComputeDeltaSet(ps.Encoded.TempTrackptr, numOrders)
		trackStarts[i] = ps.Encoded.TrackStarts
		transposeSets[i] = encode.ComputeTransposeSet(ps.Encoded.TempTranspose, numOrders)

		fmt.Printf("  %s: base_deltas=%d, transposes=%d, starts=[%d,%d,%d]\n",
			ps.Name, len(baseDeltaSets[i]), len(transposeSets[i]),
			trackStarts[i][0], trackStarts[i][1], trackStarts[i][2])
	}

	fmt.Println("\n=== Find optimal start constant ===")
	bestConst, deltaSets := solve.FindOptimalStartConstant(baseDeltaSets, trackStarts)
	fmt.Printf("  Best const: %d\n", bestConst)

	fmt.Println("\n=== Solve global tables ===")
	deltaResult := solve.SolveDeltaTable(deltaSets)
	deltaResult.StartConst = bestConst
	fmt.Printf("  Delta table: %d bytes\n", len(deltaResult.Table))

	if err := verify.DeltaTable(deltaResult, deltaSets, 32); err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}

	transposeResult := solve.SolveTransposeTable(transposeSets)
	fmt.Printf("  Transpose table: %d bytes\n", len(transposeResult.Table))

	if err := verify.TransposeTable(transposeResult, transposeSets, 16); err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  Delta bases: %v\n", deltaResult.Bases)
	fmt.Printf("  Transpose bases: %v\n", transposeResult.Bases)

	fmt.Println("\n=== Build lookup maps ===")
	deltaToIdx := solve.BuildDeltaLookupMaps(deltaResult)
	transposeToIdx := solve.BuildTransposeLookupMaps(transposeResult)

	if err := verify.DeltaLookupMaps(deltaToIdx, deltaSets); err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}

	if err := verify.TransposeLookupMaps(transposeToIdx, transposeSets); err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}

	if err := verify.DeltaTableConsistency(deltaResult.Table, deltaToIdx, deltaResult.Bases); err != nil {
		fmt.Printf("FATAL: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== Build global wave table ===")
	var waveTables [][]byte
	var waveInstruments [][]solve.WaveInstrumentInfo

	for _, ps := range songs {
		if ps == nil {
			waveTables = append(waveTables, nil)
			waveInstruments = append(waveInstruments, nil)
			continue
		}
		waveTables = append(waveTables, ps.Song.WaveTable)

		var instInfo []solve.WaveInstrumentInfo
		for _, inst := range ps.Transformed.Instruments {
			instInfo = append(instInfo, solve.WaveInstrumentInfo{
				Start: int(inst.WaveStart),
				End:   int(inst.WaveEnd),
				Loop:  int(inst.WaveLoop),
			})
		}
		waveInstruments = append(waveInstruments, instInfo)
	}

	globalWave := solve.BuildGlobalWaveTable(waveTables, waveInstruments)
	fmt.Printf("  Global wave table: %d bytes (%d unique snippets)\n",
		len(globalWave.Data), len(globalWave.Snippets))

	fmt.Println("\n=== Serialize with global tables ===")
	outputs := make([][]byte, 9)
	for i, ps := range songs {
		if ps == nil {
			continue
		}

		ps.Encoded.DeltaTable = make([]byte, len(deltaResult.Table))
		for j, v := range deltaResult.Table {
			if v == solve.DeltaEmpty {
				ps.Encoded.DeltaTable[j] = 0
			} else {
				ps.Encoded.DeltaTable[j] = byte(v)
			}
		}
		ps.Encoded.DeltaBases = deltaResult.Bases[:]
		ps.Encoded.TransposeTable = make([]byte, len(transposeResult.Table))
		for j, v := range transposeResult.Table {
			ps.Encoded.TransposeTable[j] = byte(v)
		}
		ps.Encoded.TransposeBases = transposeResult.Bases[:]

		output := serialize.SerializeWithWaveRemap(
			ps.Transformed,
			ps.Encoded,
			deltaToIdx[i],
			transposeToIdx[i],
			deltaResult.Bases[i],
			transposeResult.Bases[i],
			globalWave.Remap[i],
			deltaResult.StartConst,
		)

		if err := verify.SerializeWithWaveRemap(ps.Transformed, ps.Encoded, output, globalWave.Remap[i]); err != nil {
			fmt.Printf("FATAL: %s serialize verification failed:\n%v\n", ps.Name, err)
			os.Exit(1)
		}

		if err := verify.SerializedDictionary(ps.Encoded, output); err != nil {
			fmt.Printf("FATAL: %s dictionary serialization failed:\n%v\n", ps.Name, err)
			os.Exit(1)
		}

		if err := verify.PackedPatterns(ps.Transformed, ps.Encoded, output); err != nil {
			fmt.Printf("FATAL: %s packed patterns verification failed:\n%v\n", ps.Name, err)
			os.Exit(1)
		}

		if err := verify.BitstreamRoundtrip(
			ps.Encoded.TempTrackptr,
			ps.Encoded.TempTranspose,
			deltaToIdx[i],
			transposeToIdx[i],
			deltaResult.Table,
			transposeResult.Table,
			deltaResult.Bases[i],
			transposeResult.Bases[i],
			deltaResult.StartConst,
		); err != nil {
			fmt.Printf("FATAL: %s bitstream roundtrip failed:\n%v\n", ps.Name, err)
			os.Exit(1)
		}

		if err := verify.PlaybackStream(
			ps.Transformed,
			ps.Encoded,
			output,
			deltaResult.Table,
			transposeResult.Table,
			deltaResult.Bases[i],
			transposeResult.Bases[i],
			deltaResult.StartConst,
		); err != nil {
			fmt.Printf("FATAL: %s playback stream verification failed:\n%v\n", ps.Name, err)
			os.Exit(1)
		}

		outputs[i] = output

		outputPath := filepath.Join(cfg.OutputDir, fmt.Sprintf("part%d.bin", i+1))
		if err := os.WriteFile(outputPath, output, 0644); err != nil {
			fmt.Printf("  %s: error writing: %v\n", ps.Name, err)
			continue
		}
		fmt.Printf("  %s: %d bytes, gaps %d/%d -> %s\n", ps.Name, len(output), serialize.GapStats.Used, serialize.GapStats.Available, outputPath)
	}

	tablesPath := cfg.ProjectPath("generated/tables.inc")
	if err := serialize.WriteTablesInc(deltaResult, transposeResult, tablesPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("\nWrote tables: %s\n", tablesPath)
	}

	wavetablePath := cfg.ProjectPath("generated/wavetable.inc")
	if err := serialize.WriteWavetableInc(globalWave, wavetablePath); err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Wrote wavetable: %s\n", wavetablePath)
	}

	fmt.Println("\n=== Validate with VM ===")
	if err := build.RebuildPlayer(cfg.ProjectPath("tools/odin_convert"), cfg.ProjectPath("build")); err != nil {
		fmt.Printf("  Warning: could not rebuild player: %v\n", err)
	}

	playerData, err := os.ReadFile(cfg.ProjectPath("build/player.bin"))
	if err != nil {
		fmt.Printf("  Skipping validation: %v\n", err)
		return
	}

	if err := build.VerifyTablesInPlayer(deltaResult.Table, transposeResult.Table, globalWave.Data, playerData); err != nil {
		fmt.Printf("FATAL: tables not correctly embedded in player binary:\n  %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Tables verified in player binary")

	fmt.Println("\n=== Transformed VP Validation ===")
	vpPassed, vpFailed := 0, 0
	for i, ps := range songs {
		if ps == nil || outputs[i] == nil {
			continue
		}

		testFrames := cfg.PartTimes[i]
		origWrites := simulate.GetOriginalWrites(ps.Raw, i+1, testFrames)

		deltaBytes := make([]byte, len(deltaResult.Table))
		for j, v := range deltaResult.Table {
			if v == solve.DeltaEmpty {
				deltaBytes[j] = 0
			} else {
				deltaBytes[j] = byte(v)
			}
		}
		transposeBytes := make([]byte, len(transposeResult.Table))
		for j, v := range transposeResult.Table {
			transposeBytes[j] = byte(v)
		}

		simulate.SetVPDebugSong("")
		simulate.SetVPDebugFrame(0)

		ok, writes, msg := simulate.CompareVirtual(
			ps.Name,
			origWrites,
			outputs[i],
			deltaBytes,
			transposeBytes,
			globalWave.Data,
			ps.Transformed,
			ps.Encoded,
			testFrames,
		)
		if ok {
			fmt.Printf("  %s: VPASS (%d writes)\n", ps.Name, writes)
			vpPassed++
		} else {
			fmt.Printf("  %s: VFAIL - %s at write %d\n", ps.Name, msg, writes)
			vpFailed++
			badEntries := bisectEquivEntries(cfg,
				i+1, ps, origWrites,
				deltaBytes, transposeBytes, globalWave.Data,
				deltaToIdx[i], transposeToIdx[i],
				deltaResult.Bases[i], transposeResult.Bases[i],
				globalWave.Remap[i], deltaResult.StartConst,
				testFrames,
			)
			if len(badEntries) > 0 {
				fmt.Printf("    Found %d bad equiv entries:\n", len(badEntries))
				for _, entry := range badEntries {
					fmt.Printf("      %s\n", entry)
				}
			}
		}
	}
	fmt.Printf("Virtual validation: %d passed, %d failed\n", vpPassed, vpFailed)

	fmt.Println("\n=== Verify Excluded Equiv Entries ===")
	hasOptionalExclusions := checkExcludedEntries(cfg, songs, outputs, deltaResult, transposeResult, globalWave, deltaToIdx, transposeToIdx, globalEffectRemap, globalFSubRemap, transformOpts, playerData)
	if hasOptionalExclusions {
		fmt.Println("\nFATAL: Optional exclusions found - these should be removed from equiv_cache.json")
		os.Exit(1)
	}

	fmt.Println("\n=== ASM Player Validation ===")

	type asmResult struct {
		ok     bool
		writes int
		stats  *simulate.ASMStats
	}
	asmResults := make([]asmResult, len(songs))
	var wg sync.WaitGroup

	for i, ps := range songs {
		if ps == nil || outputs[i] == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, ps *ProcessedSong) {
			defer wg.Done()
			ok, writes, stats := TestSong(cfg, idx+1, ps.Raw, outputs[idx], playerData, ps.Transformed, ps.Encoded, true)
			asmResults[idx] = asmResult{ok, writes, stats}
		}(i, ps)
	}
	wg.Wait()

	passed, failed := 0, 0
	allStats := make([]*simulate.ASMStats, len(songs))
	for i := range songs {
		if songs[i] == nil || outputs[i] == nil {
			continue
		}
		r := asmResults[i]
		allStats[i] = r.stats

		cyclesRatio := float64(r.stats.TotalCycles) / float64(r.stats.OrigCycles)
		maxRatio := float64(r.stats.MaxFrameCycles) / float64(r.stats.OrigMaxCycles)
		sizeRatio := float64(r.stats.NewSize) / float64(r.stats.OrigSize)

		status := "PASS"
		if !r.ok {
			status = "FAIL"
			failed++
		} else {
			passed++
		}
		fmt.Printf("  Song %d: %s cycles: %.2fx, max: %.2fx, size: %.2fx, dict: %d, len: $%X (%d writes)\n",
			i+1, status, cyclesRatio, maxRatio, sizeRatio, r.stats.DictSize, r.stats.NewSize, r.writes)
	}
	fmt.Printf("\nASM validation: %d passed, %d failed\n", passed, failed)

	simulate.ReportASMStats(allStats, playerData)
}
