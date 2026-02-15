package verify

import (
	"fmt"
	"forge/solve"
)

func DeltaTable(result solve.DeltaTableResult, deltaSets [9][]int, window int) error {
	var details []string

	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(deltaSets[songIdx]) == 0 {
			continue
		}

		base := result.Bases[songIdx]
		if base < 0 {
			details = append(details, fmt.Sprintf("song %d: negative base %d", songIdx+1, base))
			continue
		}

		found := make(map[int]bool)
		for i := 0; i < window && base+i < len(result.Table); i++ {
			v := result.Table[base+i]
			if v != solve.DeltaEmpty {
				found[int(v)] = true
			}
		}

		for _, delta := range deltaSets[songIdx] {
			if !found[delta] {
				details = append(details, fmt.Sprintf("song %d: delta %d not found in window [%d, %d)",
					songIdx+1, delta, base, base+window))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/delta", "delta table does not cover all required deltas", details...)
	}

	return nil
}

func TransposeTable(result solve.TransposeTableResult, transposeSets [9][]int8, window int) error {
	var details []string

	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(transposeSets[songIdx]) == 0 {
			continue
		}

		base := result.Bases[songIdx]
		if base < 0 {
			details = append(details, fmt.Sprintf("song %d: negative base %d", songIdx+1, base))
			continue
		}

		found := make(map[int8]bool)
		for i := 0; i < window && base+i < len(result.Table); i++ {
			found[result.Table[base+i]] = true
		}

		for _, transpose := range transposeSets[songIdx] {
			if !found[transpose] {
				details = append(details, fmt.Sprintf("song %d: transpose %d not found in window [%d, %d)",
					songIdx+1, transpose, base, base+window))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/transpose", "transpose table does not cover all required transposes", details...)
	}

	return nil
}

func DeltaLookupMaps(deltaToIdx [9]map[int]byte, deltaSets [9][]int) error {
	var details []string

	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(deltaSets[songIdx]) == 0 {
			continue
		}

		lookup := deltaToIdx[songIdx]
		if lookup == nil {
			details = append(details, fmt.Sprintf("song %d: nil delta lookup map", songIdx+1))
			continue
		}

		for _, delta := range deltaSets[songIdx] {
			if _, ok := lookup[delta]; !ok {
				details = append(details, fmt.Sprintf("song %d: delta %d not in lookup map", songIdx+1, delta))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/delta_map", "delta lookup maps incomplete", details...)
	}

	return nil
}

func TransposeLookupMaps(transposeToIdx [9]map[int8]byte, transposeSets [9][]int8) error {
	var details []string

	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(transposeSets[songIdx]) == 0 {
			continue
		}

		lookup := transposeToIdx[songIdx]
		if lookup == nil {
			details = append(details, fmt.Sprintf("song %d: nil transpose lookup map", songIdx+1))
			continue
		}

		for _, transpose := range transposeSets[songIdx] {
			if _, ok := lookup[transpose]; !ok {
				details = append(details, fmt.Sprintf("song %d: transpose %d not in lookup map", songIdx+1, transpose))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/transpose_map", "transpose lookup maps incomplete", details...)
	}

	return nil
}

func DeltaTableConsistency(deltaTable []int8, deltaToIdx [9]map[int]byte, bases [9]int) error {
	var details []string

	for songIdx := 0; songIdx < 9; songIdx++ {
		if deltaToIdx[songIdx] == nil {
			continue
		}

		base := bases[songIdx]
		for delta, idx := range deltaToIdx[songIdx] {
			if base+int(idx) >= len(deltaTable) {
				details = append(details, fmt.Sprintf("song %d: delta %d maps to idx %d, but base+idx=%d exceeds table len %d",
					songIdx+1, delta, idx, base+int(idx), len(deltaTable)))
				continue
			}

			actualDelta := int(deltaTable[base+int(idx)])
			if actualDelta != delta {
				details = append(details, fmt.Sprintf("song %d: deltaToIdx[%d]=%d, but delta_table[%d+%d]=%d (expected %d)",
					songIdx+1, delta, idx, base, idx, actualDelta, delta))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/consistency", "delta table/map mismatch", details...)
	}

	return nil
}

func BitstreamRoundtrip(
	trackptr [3][]byte,
	transpose [3][]byte,
	deltaToIdx map[int]byte,
	transposeToIdx map[int8]byte,
	deltaTable []int8,
	transposeTable []int8,
	deltaBase int,
	transposeBase int,
	startConst int,
) error {
	var details []string

	for ch := 0; ch < 3; ch++ {
		if len(trackptr[ch]) == 0 {
			continue
		}

		prevTrackptr := startConst
		for i := 0; i < len(trackptr[ch]); i++ {
			absTrackptr := int(trackptr[ch][i])
			delta := absTrackptr - prevTrackptr
			if delta > 127 {
				delta -= 256
			} else if delta < -128 {
				delta += 256
			}

			idx, ok := deltaToIdx[delta]
			if !ok {
				details = append(details, fmt.Sprintf("ch%d order %d: delta %d not in deltaToIdx",
					ch, i, delta))
				prevTrackptr = absTrackptr
				continue
			}

			tablePos := deltaBase + int(idx)
			if tablePos >= len(deltaTable) {
				details = append(details, fmt.Sprintf("ch%d order %d: tablePos %d exceeds table len %d",
					ch, i, tablePos, len(deltaTable)))
				prevTrackptr = absTrackptr
				continue
			}

			decodedDelta := int(deltaTable[tablePos])
			decodedTrackptr := (prevTrackptr + decodedDelta) & 0xFF

			if decodedTrackptr != absTrackptr {
				details = append(details, fmt.Sprintf("ch%d order %d: trackptr roundtrip fail: orig=%d, delta=%d, idx=%d, decoded_delta=%d, decoded=%d",
					ch, i, absTrackptr, delta, idx, decodedDelta, decodedTrackptr))
			}

			prevTrackptr = absTrackptr
		}

		for i := 0; i < len(transpose[ch]); i++ {
			trans := int8(transpose[ch][i])

			idx, ok := transposeToIdx[trans]
			if !ok {
				details = append(details, fmt.Sprintf("ch%d order %d: transpose %d not in transposeToIdx",
					ch, i, trans))
				continue
			}

			tablePos := transposeBase + int(idx)
			if tablePos >= len(transposeTable) {
				details = append(details, fmt.Sprintf("ch%d order %d: transposeTablePos %d exceeds table len %d",
					ch, i, tablePos, len(transposeTable)))
				continue
			}

			decodedTrans := transposeTable[tablePos]
			if decodedTrans != trans {
				details = append(details, fmt.Sprintf("ch%d order %d: transpose roundtrip fail: orig=%d, idx=%d, decoded=%d",
					ch, i, trans, idx, decodedTrans))
			}
		}
	}

	if len(details) > 0 {
		return NewError("solve/bitstream", "bitstream roundtrip verification failed", details...)
	}

	return nil
}
