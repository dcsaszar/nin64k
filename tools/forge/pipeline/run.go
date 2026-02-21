package pipeline

import (
	"fmt"
	"os"
	"sync"

	"forge/build"
	"forge/simulate"
	"forge/solve"
	"forge/transform"
)

func RunValidation(
	cfg *Config,
	songs [9]*ProcessedSong,
	outputs [][]byte,
	tables TablesResult,
	globalEffectRemap [16]byte,
	globalFSubRemap map[int]byte,
	transformOpts transform.TransformOptions,
) {
	fmt.Println("\n=== Validate with VM ===")
	if err := build.RebuildPlayer(cfg.ProjectPath("tools/odin_convert"), cfg.ProjectPath("build")); err != nil {
		fmt.Printf("FATAL: could not rebuild player: %v\n", err)
		os.Exit(1)
	}

	playerData, err := os.ReadFile(cfg.ProjectPath("build/player.bin"))
	if err != nil {
		fmt.Printf("  Skipping validation: %v\n", err)
		return
	}

	if err := build.VerifyTablesInPlayer(tables.DeltaResult.Table, tables.TransposeResult.Table, tables.GlobalWave.Data, playerData); err != nil {
		fmt.Printf("FATAL: tables not correctly embedded in player binary:\n  %v\n", err)
		os.Exit(1)
	}
	fmt.Println("  Tables verified in player binary")

	runVPValidation(cfg, songs, outputs, tables, playerData, globalEffectRemap, globalFSubRemap, transformOpts)
	runASMValidation(cfg, songs, outputs, tables, playerData)
}

func runVPValidation(
	cfg *Config,
	songs [9]*ProcessedSong,
	outputs [][]byte,
	tables TablesResult,
	playerData []byte,
	globalEffectRemap [16]byte,
	globalFSubRemap map[int]byte,
	transformOpts transform.TransformOptions,
) {
	fmt.Println("\n=== Transformed VP Validation ===")

	type vpResult struct {
		ok         bool
		writes     int
		msg        string
		badEntries []string
	}
	vpResults := make([]vpResult, len(songs))
	var wg sync.WaitGroup

	// Prepare shared table conversions once
	deltaBytes := make([]byte, len(tables.DeltaResult.Table))
	for j, v := range tables.DeltaResult.Table {
		if v == solve.DeltaEmpty {
			deltaBytes[j] = 0
		} else {
			deltaBytes[j] = byte(v)
		}
	}
	transposeBytes := make([]byte, len(tables.TransposeResult.Table))
	for j, v := range tables.TransposeResult.Table {
		transposeBytes[j] = byte(v)
	}

	// Run validation in parallel
	for i, ps := range songs {
		if ps == nil || outputs[i] == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, ps *ProcessedSong) {
			defer wg.Done()
			testFrames := cfg.PartTimes[idx]
			origWrites := simulate.GetOriginalWrites(ps.Raw, idx+1, testFrames)

			ok, writes, msg := simulate.CompareVirtual(
				origWrites,
				outputs[idx],
				deltaBytes,
				transposeBytes,
				tables.GlobalWave.Data,
				len(ps.Encoded.PatternOffsets),
				testFrames,
				tables.DeltaResult.StartConst,
			)

			var badEntries []string
			if !ok {
				badEntries = bisectEquivEntries(cfg,
					idx+1, ps, origWrites,
					deltaBytes, transposeBytes, tables.GlobalWave.Data,
					tables.DeltaToIdx[idx], tables.TransposeToIdx[idx],
					tables.DeltaResult.Bases[idx], tables.TransposeResult.Bases[idx],
					tables.GlobalWave.Remap[idx], tables.DeltaResult.StartConst,
					testFrames,
				)
			}
			vpResults[idx] = vpResult{ok, writes, msg, badEntries}
		}(i, ps)
	}
	wg.Wait()

	// Print results in song order
	vpPassed, vpFailed := 0, 0
	for i := range songs {
		if songs[i] == nil || outputs[i] == nil {
			continue
		}
		r := vpResults[i]
		if r.ok {
			fmt.Printf("  %s: PASS (%d writes)\n", songs[i].Name, r.writes)
			vpPassed++
		} else {
			fmt.Printf("  %s: VFAIL - %s at write %d\n", songs[i].Name, r.msg, r.writes)
			vpFailed++
			if len(r.badEntries) > 0 {
				fmt.Printf("    Found %d bad equiv entries:\n", len(r.badEntries))
				for _, entry := range r.badEntries {
					fmt.Printf("      %s\n", entry)
				}
			}
		}
	}
	fmt.Printf("Virtual validation: %d passed, %d failed\n", vpPassed, vpFailed)
	if vpFailed > 0 {
		os.Exit(1)
	}

	fmt.Println("\n=== Verify Excluded Equiv Entries ===")
	hasOptionalExclusions := checkExcludedEntries(cfg, songs, outputs, tables.DeltaResult, tables.TransposeResult, tables.GlobalWave, tables.DeltaToIdx, tables.TransposeToIdx, globalEffectRemap, globalFSubRemap, transformOpts, playerData)
	if hasOptionalExclusions {
		fmt.Println("\nFATAL: Optional exclusions found - these should be removed from equiv_cache.json")
		os.Exit(1)
	}
}

func runASMValidation(
	cfg *Config,
	songs [9]*ProcessedSong,
	outputs [][]byte,
	tables TablesResult,
	playerData []byte,
) {
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

	fullCoverage := simulate.ReportASMStats(allStats, playerData)

	if failed > 0 {
		os.Exit(1)
	}

	if !fullCoverage {
		fmt.Println("\nFATAL: Code coverage is not 100% - all instructions must be executed")
		os.Exit(1)
	}
}
