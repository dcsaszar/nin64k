package pipeline

import (
	"fmt"
	"os"

	"forge/analysis"
	"forge/encode"
	"forge/parse"
	"forge/serialize"
	"forge/transform"
	"forge/verify"
)

func PackAndEncode(
	songNames []string,
	rawData [9][]byte,
	parsedSongs [9]parse.ParsedSong,
	analyses [9]analysis.SongAnalysis,
	transformedSongs []transform.TransformedSong,
) ([9]*ProcessedSong, []encode.EncodedSong) {
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

	var songs [9]*ProcessedSong
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

	return songs, encodedSongs
}
