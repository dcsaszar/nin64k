package serialize

import (
	"fmt"
	"os"

	"forge/solve"
)

func WriteTablesInc(deltaResult solve.DeltaTableResult, transposeResult solve.TransposeTableResult, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create tables.inc: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "; Auto-generated lookup tables - DO NOT EDIT\n\n")

	fmt.Fprintf(f, "; Delta table: %d bytes\n", len(deltaResult.Table))
	fmt.Fprintf(f, "delta_table:\n")
	for i := 0; i < len(deltaResult.Table); i += 16 {
		fmt.Fprintf(f, "\t.byte\t")
		end := i + 16
		if end > len(deltaResult.Table) {
			end = len(deltaResult.Table)
		}
		for j := i; j < end; j++ {
			v := deltaResult.Table[j]
			if v == solve.DeltaEmpty {
				v = 0
			}
			fmt.Fprintf(f, "$%02X", byte(v))
			if j < end-1 {
				fmt.Fprintf(f, ", ")
			}
		}
		fmt.Fprintf(f, "\t; %d\n", i)
	}
	fmt.Fprintf(f, "\nTRACKPTR_START = %d\n", deltaResult.StartConst)
	fmt.Fprintf(f, "ROW_DICT_SIZE = %d\n", DictArraySize)

	fmt.Fprintf(f, "\n; Transpose table: %d bytes\n", len(transposeResult.Table))
	fmt.Fprintf(f, "transpose_table:\n")
	for i := 0; i < len(transposeResult.Table); i += 16 {
		fmt.Fprintf(f, "\t.byte\t")
		end := i + 16
		if end > len(transposeResult.Table) {
			end = len(transposeResult.Table)
		}
		for j := i; j < end; j++ {
			fmt.Fprintf(f, "$%02X", byte(transposeResult.Table[j]))
			if j < end-1 {
				fmt.Fprintf(f, ", ")
			}
		}
		fmt.Fprintf(f, "\t; %d\n", i)
	}

	return nil
}

func WriteWavetableInc(globalWave *solve.GlobalWaveTable, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create wavetable.inc: %w", err)
	}
	defer f.Close()

	fmt.Fprintf(f, "; Auto-generated wavetable - DO NOT EDIT\n\n")
	fmt.Fprintf(f, "; Global wave table: %d bytes\n", len(globalWave.Data))
	fmt.Fprintf(f, "global_wavetable:\n")
	for i := 0; i < len(globalWave.Data); i += 16 {
		fmt.Fprintf(f, "\t.byte\t")
		end := i + 16
		if end > len(globalWave.Data) {
			end = len(globalWave.Data)
		}
		for j := i; j < end; j++ {
			fmt.Fprintf(f, "$%02X", globalWave.Data[j])
			if j < end-1 {
				fmt.Fprintf(f, ", ")
			}
		}
		fmt.Fprintf(f, "\t; %d\n", i)
	}

	return nil
}
