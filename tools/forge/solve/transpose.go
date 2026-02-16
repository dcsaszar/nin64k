package solve

type TransposeTableResult struct {
	Table []int8
	Bases [9]int
}

func SolveTransposeTable(songSets [9][]int8) TransposeTableResult {
	var intSets [9][]int
	for i, set := range songSets {
		intSets[i] = make([]int, len(set))
		for j, v := range set {
			intSets[i][j] = int(v)
		}
	}

	deltaResult := SolveDeltaTableWithWindow(intSets, 16)

	table := make([]int8, len(deltaResult.Table))
	for i, v := range deltaResult.Table {
		if v == DeltaEmpty {
			table[i] = 0
		} else {
			table[i] = v
		}
	}

	return TransposeTableResult{Table: table, Bases: deltaResult.Bases}
}

func BuildTransposeLookupMaps(transposeResult TransposeTableResult) [9]map[int8]byte {
	var transposeToIdx [9]map[int8]byte
	for songIdx := 0; songIdx < 9; songIdx++ {
		transposeToIdx[songIdx] = make(map[int8]byte)
		tbase := transposeResult.Bases[songIdx]
		for i := 0; i < 16 && tbase+i < len(transposeResult.Table); i++ {
			v := transposeResult.Table[tbase+i]
			if _, exists := transposeToIdx[songIdx][v]; !exists {
				transposeToIdx[songIdx][v] = byte(i)
			}
		}
	}
	return transposeToIdx
}
