package encode

import (
	"fmt"

	"forge/transform"
)

// ConvertPatternsToBytes converts TransformedPatterns to raw byte format
// Returns patterns [][]byte, truncateLimits []int, reorderMap []int
func ConvertPatternsToBytes(song transform.TransformedSong, doReorder bool) ([][]byte, []int, []int) {
	numPatterns := len(song.Patterns)
	if numPatterns == 0 {
		return nil, nil, nil
	}

	var reorderMap []int
	if doReorder {
		reorderMap = OptimizePatternOrder(song)
	}

	patterns := make([][]byte, numPatterns)
	truncateLimits := make([]int, numPatterns)

	usedSlots := make([]bool, numPatterns)
	for oldIdx, pat := range song.Patterns {
		newIdx := oldIdx
		if reorderMap != nil {
			newIdx = reorderMap[oldIdx]
			if newIdx < 0 || newIdx >= numPatterns {
				panic(fmt.Sprintf("ConvertPatternsToBytes: reorderMap[%d] = %d out of bounds", oldIdx, newIdx))
			}
		}
		if usedSlots[newIdx] {
			panic(fmt.Sprintf("ConvertPatternsToBytes: slot %d used twice", newIdx))
		}
		usedSlots[newIdx] = true

		patData := make([]byte, 192)
		for row := 0; row < 64; row++ {
			r := pat.Rows[row]
			b0 := (r.Note & 0x7F) | ((r.Effect & 8) << 4)
			b1 := (r.Inst & 0x1F) | ((r.Effect & 7) << 5)
			patData[row*3] = b0
			patData[row*3+1] = b1
			patData[row*3+2] = r.Param
		}
		patterns[newIdx] = patData
		truncateLimits[newIdx] = pat.TruncateAt
	}

	for i, used := range usedSlots {
		if !used {
			panic(fmt.Sprintf("ConvertPatternsToBytes: slot %d not filled", i))
		}
	}

	return patterns, truncateLimits, reorderMap
}

// BuildRowToIdx builds the row-to-index mapping from a dictionary
func BuildRowToIdx(dict []byte) map[string]int {
	rowToIdx := make(map[string]int)
	rowToIdx[string([]byte{0, 0, 0})] = 0
	numEntries := len(dict) / 3
	for idx := 1; idx < numEntries; idx++ {
		row := string(dict[idx*3 : idx*3+3])
		rowToIdx[row] = idx
	}
	return rowToIdx
}
