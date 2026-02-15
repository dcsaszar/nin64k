package verify

import (
	"fmt"
	"forge/encode"
	"forge/transform"
)

func Encode(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	numDictEntries := len(encoded.RowDict) / 3
	if numDictEntries == 0 {
		details = append(details, "empty row dictionary")
	}

	dictContains := make(map[string]bool)
	dictContains[string([]byte{0, 0, 0})] = true
	for i := 1; i < numDictEntries; i++ {
		row := encoded.RowDict[i*3 : i*3+3]
		key := string(row)
		dictContains[key] = true
		if idx, ok := encoded.RowToIdx[key]; !ok || idx != i {
			details = append(details, fmt.Sprintf("dictionary entry %d not in RowToIdx map correctly", i))
		}
	}

	seen := make(map[string]int)
	for i := 1; i < numDictEntries; i++ {
		row := string(encoded.RowDict[i*3 : i*3+3])
		if prev, exists := seen[row]; exists {
			details = append(details, fmt.Sprintf("duplicate dictionary entry at %d and %d", prev, i))
		}
		seen[row] = i
	}

	numPatterns := len(transformed.Patterns)
	if len(encoded.PatternData) != numPatterns {
		details = append(details, fmt.Sprintf("pattern count mismatch: transformed has %d, encoded has %d",
			numPatterns, len(encoded.PatternData)))
	}

	if len(encoded.PatternOffsets) != numPatterns {
		details = append(details, fmt.Sprintf("pattern offset count mismatch: expected %d, got %d",
			numPatterns, len(encoded.PatternOffsets)))
	}

	for patIdx, pat := range transformed.Patterns {
		truncateAt := pat.TruncateAt
		if truncateAt <= 0 || truncateAt > 64 {
			truncateAt = 64
		}

		var prevRow [3]byte
		for row := 0; row < truncateAt; row++ {
			r := pat.Rows[row]
			b0 := (r.Note & 0x7F) | ((r.Effect & 8) << 4)
			b1 := (r.Inst & 0x1F) | ((r.Effect & 7) << 5)
			b2 := r.Param
			curRow := [3]byte{b0, b1, b2}

			if curRow == prevRow {
				continue
			}

			key := string(curRow[:])
			inDict := dictContains[key]
			inMap := false
			if _, ok := encoded.RowToIdx[key]; ok {
				inMap = true
			}
			if !inDict && !inMap {
				details = append(details, fmt.Sprintf("pattern %d row %d (trunc=%d): row %02X %02X %02X (note=%d inst=%d eff=%d param=$%02X) not in dict or map",
					patIdx, row, pat.TruncateAt, b0, b1, b2, r.Note, r.Inst, r.Effect, r.Param))
			} else if inDict && !inMap {
				details = append(details, fmt.Sprintf("pattern %d row %d (trunc=%d): row %02X %02X %02X in dict but not in RowToIdx map",
					patIdx, row, pat.TruncateAt, b0, b1, b2))
			}

			prevRow = curRow
		}
	}

	for ch := 0; ch < 3; ch++ {
		if len(encoded.TempTrackptr[ch]) != len(transformed.Orders[ch]) {
			details = append(details, fmt.Sprintf("channel %d trackptr count mismatch: expected %d, got %d",
				ch, len(transformed.Orders[ch]), len(encoded.TempTrackptr[ch])))
		}

		if len(encoded.TempTranspose[ch]) != len(transformed.Orders[ch]) {
			details = append(details, fmt.Sprintf("channel %d transpose count mismatch: expected %d, got %d",
				ch, len(transformed.Orders[ch]), len(encoded.TempTranspose[ch])))
		}
	}

	for ch := 0; ch < 3; ch++ {
		for i := range transformed.Orders[ch] {
			if i < len(encoded.TempTrackptr[ch]) {
				trackptr := int(encoded.TempTrackptr[ch][i])
				if trackptr < 0 || trackptr >= numPatterns {
					details = append(details, fmt.Sprintf("channel %d order %d: trackptr %d out of bounds (0-%d)",
						ch, i, trackptr, numPatterns-1))
				}
			}

			if i < len(encoded.TempTranspose[ch]) {
				transpose := int8(encoded.TempTranspose[ch][i])
				expectedTranspose := transformed.Orders[ch][i].Transpose
				if transpose != expectedTranspose {
					details = append(details, fmt.Sprintf("channel %d order %d: transpose %d != expected %d",
						ch, i, transpose, expectedTranspose))
				}
			}
		}
	}

	vibDepthRemap := [16]byte{0, 4, 2, 3, 1, 7, 5, 0, 8, 0, 6, 0, 0, 0, 0, 9}

	for slot := 1; slot <= transformed.MaxUsedSlot && slot < len(transformed.Instruments); slot++ {
		inst := transformed.Instruments[slot]
		base := (slot - 1) * 16

		if base+15 >= len(encoded.InstrumentData) {
			details = append(details, fmt.Sprintf("instrument slot %d: data truncated (need %d bytes, have %d)",
				slot, base+16, len(encoded.InstrumentData)))
			continue
		}

		data := encoded.InstrumentData[base : base+16]

		if data[0] != inst.AD {
			details = append(details, fmt.Sprintf("inst slot %d: AD mismatch: got $%02X, want $%02X", slot, data[0], inst.AD))
		}
		if data[1] != inst.SR {
			details = append(details, fmt.Sprintf("inst slot %d: SR mismatch: got $%02X, want $%02X", slot, data[1], inst.SR))
		}

		expectedWaveEnd := inst.WaveEnd
		if expectedWaveEnd < 255 {
			expectedWaveEnd++
		}
		if data[3] != expectedWaveEnd {
			details = append(details, fmt.Sprintf("inst slot %d: WaveEnd mismatch: got $%02X, want $%02X (+1 transform)",
				slot, data[3], expectedWaveEnd))
		}

		expectedArpEnd := inst.ArpEnd
		if expectedArpEnd < 255 {
			expectedArpEnd++
		}
		if data[6] != expectedArpEnd {
			details = append(details, fmt.Sprintf("inst slot %d: ArpEnd mismatch: got $%02X, want $%02X (+1 transform)",
				slot, data[6], expectedArpEnd))
		}

		if data[8] != inst.VibDelay {
			details = append(details, fmt.Sprintf("inst slot %d: VibDelay mismatch: got $%02X, want $%02X", slot, data[8], inst.VibDelay))
		}

		oldDepth := inst.VibDepthSpeed >> 4
		speed := inst.VibDepthSpeed & 0x0F
		expectedVibDepthSpeed := (vibDepthRemap[oldDepth] << 4) | speed
		if data[9] != expectedVibDepthSpeed {
			details = append(details, fmt.Sprintf("inst slot %d: VibDepthSpeed mismatch: got $%02X, want $%02X (remap from $%02X)",
				slot, data[9], expectedVibDepthSpeed, inst.VibDepthSpeed))
		}

		expectedPulseWidth := (inst.PulseWidth << 4) | (inst.PulseWidth >> 4)
		if data[10] != expectedPulseWidth {
			details = append(details, fmt.Sprintf("inst slot %d: PulseWidth mismatch: got $%02X, want $%02X (nibble swap from $%02X)",
				slot, data[10], expectedPulseWidth, inst.PulseWidth))
		}

		expectedFilterEnd := inst.FilterEnd
		if expectedFilterEnd < 255 {
			expectedFilterEnd++
		}
		if data[14] != expectedFilterEnd {
			details = append(details, fmt.Sprintf("inst slot %d: FilterEnd mismatch: got $%02X, want $%02X (+1 transform)",
				slot, data[14], expectedFilterEnd))
		}
	}

	if len(details) > 0 {
		return NewError("encode", "encoding produced invalid results", details...)
	}

	return nil
}

func FilterTableRemap(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	filterTable := transformed.FilterTable
	filterSize := len(filterTable)

	for slot := 1; slot <= transformed.MaxUsedSlot && slot < len(transformed.Instruments); slot++ {
		inst := transformed.Instruments[slot]
		base := (slot - 1) * 16

		if base+15 >= len(encoded.InstrumentData) {
			continue
		}

		data := encoded.InstrumentData[base : base+16]

		filterStart := data[13]
		filterEnd := data[14]
		filterLoop := data[15]

		hasValidRange := inst.FilterStart < 255 && inst.FilterEnd < 255 && inst.FilterEnd >= inst.FilterStart

		if !hasValidRange {
			if filterStart != 255 || (filterEnd != 255 && filterEnd != 0) {
				details = append(details, fmt.Sprintf("inst slot %d: invalid filter range but pointers not disabled (start=$%02X end=$%02X)",
					slot, filterStart, filterEnd))
			}
			continue
		}

		actualEnd := filterEnd
		if actualEnd > 0 && actualEnd < 255 {
			actualEnd--
		}

		if int(filterStart) >= filterSize {
			details = append(details, fmt.Sprintf("inst slot %d: FilterStart $%02X >= filterSize %d",
				slot, filterStart, filterSize))
		}

		if int(actualEnd) >= filterSize && actualEnd < 255 {
			details = append(details, fmt.Sprintf("inst slot %d: FilterEnd $%02X (actual $%02X) >= filterSize %d",
				slot, filterEnd, actualEnd, filterSize))
		}

		if filterLoop < 255 && int(filterLoop) >= filterSize {
			details = append(details, fmt.Sprintf("inst slot %d: FilterLoop $%02X >= filterSize %d",
				slot, filterLoop, filterSize))
		}

		expectedStart := inst.FilterStart
		expectedEnd := inst.FilterEnd
		if expectedEnd < 255 {
			expectedEnd++
		}
		expectedLoop := inst.FilterLoop

		if filterStart != expectedStart {
			details = append(details, fmt.Sprintf("inst slot %d: FilterStart encoded $%02X != transformed $%02X",
				slot, filterStart, expectedStart))
		}
		if filterEnd != expectedEnd {
			details = append(details, fmt.Sprintf("inst slot %d: FilterEnd encoded $%02X != transformed $%02X (+1)",
				slot, filterEnd, expectedEnd))
		}
		if filterLoop != expectedLoop {
			details = append(details, fmt.Sprintf("inst slot %d: FilterLoop encoded $%02X != transformed $%02X",
				slot, filterLoop, expectedLoop))
		}
	}

	if len(details) > 0 {
		return NewError("encode/filter", "filter table remap errors", details...)
	}

	return nil
}

func ArpTableRemap(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	arpTable := transformed.ArpTable
	arpSize := len(arpTable)

	for slot := 1; slot <= transformed.MaxUsedSlot && slot < len(transformed.Instruments); slot++ {
		inst := transformed.Instruments[slot]
		base := (slot - 1) * 16

		if base+15 >= len(encoded.InstrumentData) {
			continue
		}

		data := encoded.InstrumentData[base : base+16]

		arpStart := data[5]
		arpEnd := data[6]
		arpLoop := data[7]

		hasValidRange := inst.ArpStart < 255 && inst.ArpEnd < 255 && inst.ArpEnd >= inst.ArpStart

		if !hasValidRange {
			if arpStart != 255 || (arpEnd != 255 && arpEnd != 0) {
				details = append(details, fmt.Sprintf("inst slot %d: invalid arp range but pointers not disabled (start=$%02X end=$%02X)",
					slot, arpStart, arpEnd))
			}
			continue
		}

		actualEnd := arpEnd
		if actualEnd > 0 && actualEnd < 255 {
			actualEnd--
		}

		if int(arpStart) >= arpSize {
			details = append(details, fmt.Sprintf("inst slot %d: ArpStart $%02X >= arpSize %d",
				slot, arpStart, arpSize))
		}

		if int(actualEnd) >= arpSize && actualEnd < 255 {
			details = append(details, fmt.Sprintf("inst slot %d: ArpEnd $%02X (actual $%02X) >= arpSize %d",
				slot, arpEnd, actualEnd, arpSize))
		}

		if arpLoop < 255 && int(arpLoop) >= arpSize {
			details = append(details, fmt.Sprintf("inst slot %d: ArpLoop $%02X >= arpSize %d",
				slot, arpLoop, arpSize))
		}

		expectedStart := inst.ArpStart
		expectedEnd := inst.ArpEnd
		if expectedEnd < 255 {
			expectedEnd++
		}
		expectedLoop := inst.ArpLoop

		if arpStart != expectedStart {
			details = append(details, fmt.Sprintf("inst slot %d: ArpStart encoded $%02X != transformed $%02X",
				slot, arpStart, expectedStart))
		}
		if arpEnd != expectedEnd {
			details = append(details, fmt.Sprintf("inst slot %d: ArpEnd encoded $%02X != transformed $%02X (+1)",
				slot, arpEnd, expectedEnd))
		}
		if arpLoop != expectedLoop {
			details = append(details, fmt.Sprintf("inst slot %d: ArpLoop encoded $%02X != transformed $%02X",
				slot, arpLoop, expectedLoop))
		}
	}

	if len(details) > 0 {
		return NewError("encode/arp", "arp table remap errors", details...)
	}

	return nil
}
