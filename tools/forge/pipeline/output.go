package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"forge/serialize"
	"forge/solve"
	"forge/verify"
)

func SerializeAndWrite(cfg *Config, songs [9]*ProcessedSong, tables TablesResult) [][]byte {
	fmt.Println("\n=== Serialize with global tables ===")
	outputs := make([][]byte, 9)

	type serializeResult struct {
		output     []byte
		outputPath string
		gapsUsed   int
		gapsAvail  int
		err        error
	}
	results := make([]serializeResult, 9)
	var wg sync.WaitGroup

	// Serialize and verify in parallel
	for i, ps := range songs {
		if ps == nil {
			continue
		}
		wg.Add(1)
		go func(idx int, ps *ProcessedSong) {
			defer wg.Done()

			ps.Encoded.DeltaTable = make([]byte, len(tables.DeltaResult.Table))
			for j, v := range tables.DeltaResult.Table {
				if v == solve.DeltaEmpty {
					ps.Encoded.DeltaTable[j] = 0
				} else {
					ps.Encoded.DeltaTable[j] = byte(v)
				}
			}
			ps.Encoded.DeltaBases = tables.DeltaResult.Bases[:]
			ps.Encoded.TransposeTable = make([]byte, len(tables.TransposeResult.Table))
			for j, v := range tables.TransposeResult.Table {
				ps.Encoded.TransposeTable[j] = byte(v)
			}
			ps.Encoded.TransposeBases = tables.TransposeResult.Bases[:]

			output := serialize.SerializeWithWaveRemap(
				ps.Transformed,
				ps.Encoded,
				tables.DeltaToIdx[idx],
				tables.TransposeToIdx[idx],
				tables.DeltaResult.Bases[idx],
				tables.TransposeResult.Bases[idx],
				tables.GlobalWave.Remap[idx],
				tables.DeltaResult.StartConst,
				ps.Anal.DuplicateOrder,
				ps.Anal.DuplicateSource,
			)

			if err := verify.SerializeWithWaveRemap(ps.Transformed, ps.Encoded, output, tables.GlobalWave.Remap[idx]); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("serialize verification failed: %w", err)}
				return
			}

			if err := verify.SerializedDictionary(ps.Encoded, output); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("dictionary serialization failed: %w", err)}
				return
			}

			if err := verify.PackedPatterns(ps.Transformed, ps.Encoded, output); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("packed patterns verification failed: %w", err)}
				return
			}

			if err := verify.BitstreamRoundtrip(
				ps.Encoded.TempTrackptr,
				ps.Encoded.TempTranspose,
				tables.DeltaToIdx[idx],
				tables.TransposeToIdx[idx],
				tables.DeltaResult.Table,
				tables.TransposeResult.Table,
				tables.DeltaResult.Bases[idx],
				tables.TransposeResult.Bases[idx],
				tables.DeltaResult.StartConst,
			); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("bitstream roundtrip failed: %w", err)}
				return
			}

			if err := verify.PlaybackStream(
				ps.Transformed,
				ps.Encoded,
				output,
				tables.DeltaResult.Table,
				tables.TransposeResult.Table,
				tables.DeltaResult.Bases[idx],
				tables.TransposeResult.Bases[idx],
				tables.DeltaResult.StartConst,
			); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("playback stream verification failed: %w", err)}
				return
			}

			outputPath := filepath.Join(cfg.OutputDir, fmt.Sprintf("part%d.bin", idx+1))
			if err := os.WriteFile(outputPath, output, 0644); err != nil {
				results[idx] = serializeResult{err: fmt.Errorf("error writing: %w", err)}
				return
			}

			results[idx] = serializeResult{
				output:     output,
				outputPath: outputPath,
				gapsUsed:   serialize.GapStats.Used,
				gapsAvail:  serialize.GapStats.Available,
			}
		}(i, ps)
	}
	wg.Wait()

	// Print results in song order and check for errors
	for i := range songs {
		if songs[i] == nil {
			continue
		}
		r := results[i]
		if r.err != nil {
			fmt.Printf("FATAL: %s %v\n", songs[i].Name, r.err)
			os.Exit(1)
		}
		outputs[i] = r.output
		fmt.Printf("  %s: %d bytes, gaps %d/%d -> %s\n", songs[i].Name, len(r.output), r.gapsUsed, r.gapsAvail, r.outputPath)
	}

	tablesPath := cfg.ProjectPath("generated/tables.inc")
	if err := serialize.WriteTablesInc(tables.DeltaResult, tables.TransposeResult, tablesPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("\nWrote tables: %s\n", tablesPath)
	}

	wavetablePath := cfg.ProjectPath("generated/wavetable.inc")
	if err := serialize.WriteWavetableInc(tables.GlobalWave, wavetablePath); err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Wrote wavetable: %s\n", wavetablePath)
	}

	return outputs
}
