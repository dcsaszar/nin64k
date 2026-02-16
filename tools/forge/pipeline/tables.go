package pipeline

import (
	"fmt"
	"os"

	"forge/encode"
	"forge/solve"
	"forge/verify"
)

type TablesResult struct {
	DeltaResult     solve.DeltaTableResult
	TransposeResult solve.TransposeTableResult
	DeltaToIdx      [9]map[int]byte
	TransposeToIdx  [9]map[int8]byte
	GlobalWave      *solve.GlobalWaveTable
}

func SolveTables(songs [9]*ProcessedSong) TablesResult {
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

	return TablesResult{
		DeltaResult:     deltaResult,
		TransposeResult: transposeResult,
		DeltaToIdx:      deltaToIdx,
		TransposeToIdx:  transposeToIdx,
		GlobalWave:      globalWave,
	}
}
