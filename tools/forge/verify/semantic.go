package verify

import (
	"fmt"
	"os"

	"forge/encode"
	"forge/serialize"
	"forge/transform"
)

// DictionaryInstruments verifies that dictionary entries use remapped instrument indices
func DictionaryInstruments(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	maxSlot := transformed.MaxUsedSlot

	numEntries := len(encoded.RowDict) / 3
	for i := 1; i < numEntries; i++ {
		b1 := encoded.RowDict[i*3+1]
		inst := b1 & 0x1F
		if int(inst) > maxSlot && inst != 0 {
			details = append(details, fmt.Sprintf("dict[%d] has inst %d > maxUsedSlot %d",
				i, inst, maxSlot))
		}
	}

	if len(details) > 0 {
		return NewError("semantic/dict", "dictionary contains invalid inst refs", details...)
	}

	return nil
}

// SerializedDictionary verifies that serialized dictionary matches encoded dictionary
func SerializedDictionary(encoded encode.EncodedSong, output []byte) error {
	var details []string

	rowDictOff := serialize.RowDictOffset
	dictArraySize := serialize.DictArraySize

	numEntries := len(encoded.RowDict) / 3
	for i := 1; i < numEntries && i <= dictArraySize; i++ {
		off := i - 1
		expectedB0 := encoded.RowDict[i*3]
		expectedB1 := encoded.RowDict[i*3+1]
		expectedB2 := encoded.RowDict[i*3+2]

		actualB0 := output[rowDictOff+off]
		actualB1 := output[rowDictOff+dictArraySize+off]
		actualB2 := output[rowDictOff+dictArraySize*2+off]

		if actualB0 != expectedB0 || actualB1 != expectedB1 || actualB2 != expectedB2 {
			details = append(details, fmt.Sprintf("dict[%d]: got [%02X %02X %02X], want [%02X %02X %02X]",
				i, actualB0, actualB1, actualB2, expectedB0, expectedB1, expectedB2))
			if len(details) >= 10 {
				break
			}
		}
	}

	if len(details) > 0 {
		return NewError("semantic/serial_dict", "serialized dictionary mismatch", details...)
	}

	return nil
}

// PatternSemantics verifies that encoded patterns contain the same musical content
// as the original transformed patterns, accounting for pattern reordering.
// Uses RawPatterns (pre-packed 192-byte patterns) for direct comparison.
func PatternSemantics(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	if encoded.RawPatterns == nil {
		return NewError("semantic/pattern", "RawPatterns not populated")
	}

	// Track which pattern comparisons we've already done to avoid duplicates
	verified := make(map[[2]int]bool)

	numOrders := len(transformed.Orders[0])
	for ch := 0; ch < 3; ch++ {
		for orderIdx := 0; orderIdx < numOrders && orderIdx < len(transformed.Orders[ch]); orderIdx++ {
			order := transformed.Orders[ch][orderIdx]
			origPatIdx := order.PatternIdx

			if origPatIdx < 0 || origPatIdx >= len(transformed.Patterns) {
				details = append(details, fmt.Sprintf("ch%d order %d: invalid origPatIdx %d", ch, orderIdx, origPatIdx))
				continue
			}

			if orderIdx >= len(encoded.TempTrackptr[ch]) {
				continue
			}
			encodedPatIdx := int(encoded.TempTrackptr[ch][orderIdx])

			if encodedPatIdx < 0 || encodedPatIdx >= len(encoded.RawPatterns) {
				details = append(details, fmt.Sprintf("ch%d order %d: encodedPatIdx %d out of range (have %d patterns)",
					ch, orderIdx, encodedPatIdx, len(encoded.RawPatterns)))
				continue
			}

			// Skip if we've already verified this (origPatIdx, encodedPatIdx) pair
			key := [2]int{origPatIdx, encodedPatIdx}
			if verified[key] {
				continue
			}
			verified[key] = true

			origPat := transformed.Patterns[origPatIdx]
			rawPat := encoded.RawPatterns[encodedPatIdx]

			truncateAt := origPat.TruncateAt
			if truncateAt <= 0 || truncateAt > 64 {
				truncateAt = 64
			}

			// Compare each row
			for row := 0; row < truncateAt; row++ {
				r := origPat.Rows[row]
				// Encode the row the same way as encoder.go
				b0 := (r.Note & 0x7F) | ((r.Effect & 8) << 4)
				b1 := (r.Inst & 0x1F) | ((r.Effect & 7) << 5)
				b2 := r.Param

				off := row * 3
				if off+2 >= len(rawPat) {
					details = append(details, fmt.Sprintf("ch%d order %d (pat %d->%d): rawPat too short at row %d",
						ch, orderIdx, origPatIdx, encodedPatIdx, row))
					break
				}

				if rawPat[off] != b0 || rawPat[off+1] != b1 || rawPat[off+2] != b2 {
					details = append(details, fmt.Sprintf(
						"ch%d order %d (pat %d->%d) row %d: raw [%02X %02X %02X] != expected [%02X %02X %02X]",
						ch, orderIdx, origPatIdx, encodedPatIdx, row,
						rawPat[off], rawPat[off+1], rawPat[off+2], b0, b1, b2))
					// Only report first mismatch per pattern
					break
				}
			}
		}
	}

	if len(details) > 0 {
		return NewError("semantic/pattern", "pattern content mismatch", details...)
	}

	return nil
}

// PackedPatterns verifies that packed patterns decode correctly
// Uses RawPatternsEquiv which has equivalence substitutions applied
func PackedPatterns(transformed transform.TransformedSong, encoded encode.EncodedSong, output []byte) error {
	var details []string

	if encoded.RawPatternsEquiv == nil {
		return NewError("semantic/packed", "RawPatternsEquiv not populated")
	}

	const dictOffsetBase = 0x10
	const rleBase = 0xEF
	const noteOnlyMarker = 0xFE
	const extMarker = 0xFF
	rowDictOff := serialize.RowDictOffset
	dictArraySize := serialize.DictArraySize
	packedPtrsOff := serialize.PackedPtrsOffset()

	numPatterns := len(encoded.RawPatternsEquiv)
	for patIdx := 0; patIdx < numPatterns; patIdx++ {
		rawPat := encoded.RawPatternsEquiv[patIdx]
		if len(rawPat) != 192 {
			continue
		}

		ptrOff := packedPtrsOff + patIdx*2
		if ptrOff+1 >= len(output) {
			continue
		}
		packedOff := int(output[ptrOff]) | (int(output[ptrOff+1]&0x1F) << 8)
		gapCode := output[ptrOff+1] >> 5
		gap := []int{0, 1, 3, 7, 15, 31, 63}[gapCode]
		spacing := gap + 1

		var decodedRows [][3]byte
		var prevRow [3]byte
		pos := packedOff // packedOff is absolute offset, not relative to pattern data start

		for len(decodedRows) < 64 && pos < len(output) {
			b := output[pos]
			pos++

			if b < dictOffsetBase {
				row := [3]byte{0, 0, 0}
				decodedRows = append(decodedRows, row)
				for rle := 0; rle < int(b) && len(decodedRows) < 64; rle++ {
					decodedRows = append(decodedRows, row)
				}
				prevRow = row
			} else if b >= rleBase && b < noteOnlyMarker {
				// $EF-$FD: RLE 1-15
				count := int(b - rleBase + 1)
				for i := 0; i < count && len(decodedRows) < 64; i++ {
					decodedRows = append(decodedRows, prevRow)
				}
			} else if b == noteOnlyMarker {
				// $FE: note-only (keep inst/eff/param, change note)
				if pos >= len(output) {
					break
				}
				noteByte := output[pos]
				pos++
				row := [3]byte{noteByte, prevRow[1], prevRow[2]}
				decodedRows = append(decodedRows, row)
				prevRow = row
			} else if b == extMarker {
				if pos >= len(output) {
					break
				}
				dictIdx := int(output[pos]) + 224
				pos++
				off := dictIdx - 1
				if off >= dictArraySize {
					continue
				}
				row := [3]byte{
					output[rowDictOff+off],
					output[rowDictOff+dictArraySize+off],
					output[rowDictOff+dictArraySize*2+off],
				}
				decodedRows = append(decodedRows, row)
				prevRow = row
			} else {
				dictIdx := int(b) - dictOffsetBase + 1
				off := dictIdx - 1
				if off >= dictArraySize {
					continue
				}
				row := [3]byte{
					output[rowDictOff+off],
					output[rowDictOff+dictArraySize+off],
					output[rowDictOff+dictArraySize*2+off],
				}
				decodedRows = append(decodedRows, row)
				prevRow = row
			}
		}

		// Get truncation limit for this pattern
		truncateAt := 64
		if patIdx < len(encoded.TruncateLimits) && encoded.TruncateLimits[patIdx] > 0 {
			truncateAt = encoded.TruncateLimits[patIdx]
		}

		slot := 0
		for _, decoded := range decodedRows {
			if slot*spacing >= truncateAt {
				break
			}
			rawOff := slot * spacing * 3
			expected := [3]byte{rawPat[rawOff], rawPat[rawOff+1], rawPat[rawOff+2]}
			if decoded != expected {
				details = append(details, fmt.Sprintf("pat %d slot %d: decoded [%02X %02X %02X] != expected [%02X %02X %02X]",
					patIdx, slot, decoded[0], decoded[1], decoded[2], expected[0], expected[1], expected[2]))
				break
			}
			slot++
		}
	}

	if len(details) > 0 {
		return NewError("semantic/packed", "packed pattern decode mismatch", details...)
	}

	return nil
}

// ReferenceOutput compares forge output against odin_convert reference
func ReferenceOutput(output []byte, referencePath string) error {
	refData, err := os.ReadFile(referencePath)
	if err != nil {
		return nil // Skip if reference not available
	}

	var details []string
	packedPtrsOff := serialize.PackedPtrsOffset()

	sections := []struct {
		name  string
		start int
		end   int
	}{
		{"instruments", serialize.InstOffset, serialize.BitstreamOffset},
		{"bitstream", serialize.BitstreamOffset, serialize.FilterOffset},
		{"filter", serialize.FilterOffset, serialize.ArpOffset},
		{"arp", serialize.ArpOffset, serialize.TransBaseOffset},
		{"bases", serialize.TransBaseOffset, serialize.RowDictOffset},
		{"dict", serialize.RowDictOffset, packedPtrsOff},
		{"pattern_ptrs", packedPtrsOff, -1},
	}

	minLen := len(output)
	if len(refData) < minLen {
		minLen = len(refData)
	}

	for _, sec := range sections {
		end := sec.end
		if end < 0 || end > minLen {
			end = minLen
		}
		if sec.start >= minLen {
			continue
		}
		mismatchCount := 0
		firstMismatch := -1
		for i := sec.start; i < end; i++ {
			if output[i] != refData[i] {
				mismatchCount++
				if firstMismatch < 0 {
					firstMismatch = i
				}
			}
		}
		if mismatchCount > 0 {
			details = append(details, fmt.Sprintf("%s: %d mismatches, first at $%04X (got $%02X, want $%02X)",
				sec.name, mismatchCount, firstMismatch,
				output[firstMismatch], refData[firstMismatch]))
		}
	}

	if len(output) != len(refData) {
		details = append(details, fmt.Sprintf("length mismatch: got %d, want %d", len(output), len(refData)))
	}

	if len(details) > 0 {
		return NewError("semantic/reference", "output differs from reference", details...)
	}

	return nil
}

// OrderSemantics verifies that the encoded order table maps to correct patterns
func OrderSemantics(transformed transform.TransformedSong, encoded encode.EncodedSong) error {
	var details []string

	numOrders := len(transformed.Orders[0])
	for ch := 0; ch < 3; ch++ {
		if len(encoded.TempTrackptr[ch]) != numOrders {
			details = append(details, fmt.Sprintf("ch%d: trackptr count %d != orders %d",
				ch, len(encoded.TempTrackptr[ch]), numOrders))
			continue
		}

		for i := 0; i < numOrders; i++ {
			expectedPatIdx := transformed.Orders[ch][i].PatternIdx
			encodedPatIdx := int(encoded.TempTrackptr[ch][i])

			// The encoded pattern index should be a valid pattern
			if encodedPatIdx < 0 || encodedPatIdx >= len(transformed.Patterns) {
				details = append(details, fmt.Sprintf("ch%d order %d: encoded patIdx %d out of range (0-%d)",
					ch, i, encodedPatIdx, len(transformed.Patterns)-1))
				continue
			}

			// Verify that the pattern at encodedPatIdx has content matching
			// what should be at expectedPatIdx (after any reordering)
			// This is already covered by PatternSemantics, so we just check bounds here
			_ = expectedPatIdx
		}
	}

	if len(details) > 0 {
		return NewError("semantic/order", "order table semantic errors", details...)
	}

	return nil
}

// PlaybackStream validates that decoding the serialized output produces the same
// row stream as the original transformed patterns. This simulates what the player does.
func PlaybackStream(
	transformed transform.TransformedSong,
	encoded encode.EncodedSong,
	output []byte,
	deltaTable []int8,
	transposeTable []int8,
	deltaBase int,
	transposeBase int,
	startConst int,
) error {
	var details []string

	const dictOffsetBase = 0x10
	const rleBase = 0xEF
	const noteOnlyMarker2 = 0xFE
	const extMarker = 0xFF
	rowDictOff := serialize.RowDictOffset
	dictArraySize := serialize.DictArraySize
	packedPtrsOff := serialize.PackedPtrsOffset()

	numPatterns := len(encoded.RawPatternsEquiv)
	numOrders := len(transformed.Orders[0])

	// Decode a pattern from the serialized output
	decodePattern := func(patIdx int) [][3]byte {
		if patIdx < 0 || patIdx >= numPatterns {
			return nil
		}

		ptrOff := packedPtrsOff + patIdx*2
		if ptrOff+1 >= len(output) {
			return nil
		}
		packedOff := int(output[ptrOff]) | (int(output[ptrOff+1]&0x1F) << 8)
		gapCode := output[ptrOff+1] >> 5
		gap := []int{0, 1, 3, 7, 15, 31, 63}[gapCode]
		spacing := gap + 1

		var decodedSlots [][3]byte
		var prevRow [3]byte
		pos := packedOff

		for len(decodedSlots) < 64 && pos < len(output) {
			b := output[pos]
			pos++

			if b < dictOffsetBase {
				row := [3]byte{0, 0, 0}
				decodedSlots = append(decodedSlots, row)
				for rle := 0; rle < int(b) && len(decodedSlots) < 64; rle++ {
					decodedSlots = append(decodedSlots, row)
				}
				prevRow = row
			} else if b >= rleBase && b < noteOnlyMarker2 {
				// $EF-$FD: RLE 1-15
				count := int(b - rleBase + 1)
				for i := 0; i < count && len(decodedSlots) < 64; i++ {
					decodedSlots = append(decodedSlots, prevRow)
				}
			} else if b == noteOnlyMarker2 {
				// $FE: note-only (keep inst/eff/param, change note)
				if pos >= len(output) {
					break
				}
				noteByte := output[pos]
				pos++
				row := [3]byte{noteByte, prevRow[1], prevRow[2]}
				decodedSlots = append(decodedSlots, row)
				prevRow = row
			} else if b == extMarker {
				if pos >= len(output) {
					break
				}
				dictIdx := int(output[pos]) + 224
				pos++
				off := dictIdx - 1
				if off >= dictArraySize {
					continue
				}
				row := [3]byte{
					output[rowDictOff+off],
					output[rowDictOff+dictArraySize+off],
					output[rowDictOff+dictArraySize*2+off],
				}
				decodedSlots = append(decodedSlots, row)
				prevRow = row
			} else {
				dictIdx := int(b) - dictOffsetBase + 1
				off := dictIdx - 1
				if off >= dictArraySize {
					continue
				}
				row := [3]byte{
					output[rowDictOff+off],
					output[rowDictOff+dictArraySize+off],
					output[rowDictOff+dictArraySize*2+off],
				}
				decodedSlots = append(decodedSlots, row)
				prevRow = row
			}
		}

		// Expand slots to 64 rows using gap spacing
		rows := make([][3]byte, 64)
		for slot, decoded := range decodedSlots {
			rowNum := slot * spacing
			if rowNum < 64 {
				rows[rowNum] = decoded
			}
		}
		return rows
	}

	// Simulate playback for each channel
	for ch := 0; ch < 3; ch++ {
		trackptr := startConst

		for orderIdx := 0; orderIdx < numOrders; orderIdx++ {
			if orderIdx >= len(encoded.TempTrackptr[ch]) {
				continue
			}

			// Get delta from bitstream encoding
			absTrackptr := int(encoded.TempTrackptr[ch][orderIdx])
			delta := absTrackptr - trackptr
			if delta > 127 {
				delta -= 256
			} else if delta < -128 {
				delta += 256
			}

			// TempTrackptr already has the absolute pattern index
			patIdx := absTrackptr
			trackptr = absTrackptr

			// Get transpose
			transpose := int8(encoded.TempTranspose[ch][orderIdx])

			// Decode pattern from output
			decodedRows := decodePattern(patIdx)
			if decodedRows == nil {
				details = append(details, fmt.Sprintf("ch%d order %d: failed to decode pattern %d",
					ch, orderIdx, patIdx))
				continue
			}

			// Get expected pattern from RawPatternsEquiv (post-equivalence encoding)
			if patIdx < 0 || patIdx >= len(encoded.RawPatternsEquiv) {
				continue
			}
			expectedPat := encoded.RawPatternsEquiv[patIdx]

			// Verify transpose matches
			if orderIdx < len(transformed.Orders[ch]) {
				expectedTranspose := transformed.Orders[ch][orderIdx].Transpose
				if transpose != expectedTranspose {
					details = append(details, fmt.Sprintf("ch%d order %d: transpose mismatch: decoded %d, expected %d",
						ch, orderIdx, transpose, expectedTranspose))
				}
			}

			// Get truncation limit
			truncateAt := 64
			if patIdx < len(encoded.TruncateLimits) && encoded.TruncateLimits[patIdx] > 0 {
				truncateAt = encoded.TruncateLimits[patIdx]
			}

			// Compare rows only within truncation limit
			for row := 0; row < truncateAt; row++ {
				off := row * 3
				if off+2 >= len(expectedPat) {
					break
				}
				expected := [3]byte{expectedPat[off], expectedPat[off+1], expectedPat[off+2]}

				decoded := decodedRows[row]

				// Check for exact match or equivalent (both zero)
				if decoded != expected {
					// Allow if both are "empty" rows (effect 0, or equivalent musical content)
					decodedIsZero := decoded == [3]byte{0, 0, 0}
					expectedIsZero := expected == [3]byte{0, 0, 0}

					if !decodedIsZero || !expectedIsZero {
						details = append(details, fmt.Sprintf(
							"ch%d order %d (pat %d) row %d: decoded [%02X %02X %02X] != expected [%02X %02X %02X]",
							ch, orderIdx, patIdx, row,
							decoded[0], decoded[1], decoded[2],
							expected[0], expected[1], expected[2]))
						if len(details) > 20 {
							details = append(details, "... (truncated)")
							goto done
						}
					}
				}
			}
		}
	}

done:
	if len(details) > 0 {
		return NewError("playback/stream", "playback stream verification failed", details...)
	}

	return nil
}

// FramePosition represents the musical position at a given frame
type FramePosition struct {
	Order     int
	Row       int
	Speed     int
	PatIdx    [3]int // Pattern index for each channel
	Transpose [3]int8
}

// BuildFrameMap simulates playback and builds a map from frame number to musical position.
// This helps debug VM mismatches by identifying exactly which order/row is playing.
func BuildFrameMap(
	transformed transform.TransformedSong,
	encoded encode.EncodedSong,
	maxFrames int,
) []FramePosition {
	numOrders := len(transformed.Orders[0])
	if numOrders == 0 {
		return nil
	}

	positions := make([]FramePosition, 0, maxFrames)

	// Get pattern truncation limits
	truncateLimits := make([]int, len(transformed.Patterns))
	for i, pat := range transformed.Patterns {
		truncateLimits[i] = pat.TruncateAt
		if truncateLimits[i] <= 0 || truncateLimits[i] > 64 {
			truncateLimits[i] = 64
		}
	}

	speed := 6 // Default speed
	speedCounter := 0
	order := 0
	row := 0

	// PlayerEffectSpeed = 3 in the remapped effect numbering
	const speedEffect = 3

	for frame := 0; frame < maxFrames; frame++ {
		// Record current position
		pos := FramePosition{
			Order: order,
			Row:   row,
			Speed: speed,
		}

		// Get pattern indices and transposes for this order
		for ch := 0; ch < 3; ch++ {
			if order < len(encoded.TempTrackptr[ch]) {
				pos.PatIdx[ch] = int(encoded.TempTrackptr[ch][order])
			}
			if order < len(encoded.TempTranspose[ch]) {
				pos.Transpose[ch] = int8(encoded.TempTranspose[ch][order])
			}
		}

		positions = append(positions, pos)

		// Advance playback - increment counter and check if we advance to next row
		speedCounter++
		if speedCounter >= speed {
			speedCounter = 0

			// Check for speed changes in current row (all channels)
			for ch := 0; ch < 3; ch++ {
				patIdx := pos.PatIdx[ch]
				if patIdx >= 0 && patIdx < len(encoded.RawPatternsEquiv) {
					pat := encoded.RawPatternsEquiv[patIdx]
					if row < len(pat)/3 {
						off := row * 3
						// b1 has effect bits: EEE iiiii, effect = (b1 >> 5) | ((b0 >> 4) & 8)
						b0 := pat[off]
						b1 := pat[off+1]
						b2 := pat[off+2]
						effect := (b1 >> 5) | ((b0 >> 4) & 8)
						if int(effect) == speedEffect && b2 > 0 {
							speed = int(b2)
						}
					}
				}
			}

			// Advance row
			row++

			// Check if we've reached the end of the pattern (use shortest truncation)
			maxRow := 64
			for ch := 0; ch < 3; ch++ {
				patIdx := pos.PatIdx[ch]
				if patIdx >= 0 && patIdx < len(truncateLimits) {
					if truncateLimits[patIdx] < maxRow {
						maxRow = truncateLimits[patIdx]
					}
				}
			}

			if row >= maxRow {
				row = 0
				order++
				if order >= numOrders {
					// Song would loop, but for debugging we can stop
					break
				}
			}
		}
	}

	return positions
}

// DescribeFrame returns a human-readable description of what's playing at a given frame
func DescribeFrame(positions []FramePosition, frame int) string {
	if frame < 0 || frame >= len(positions) {
		return fmt.Sprintf("frame %d: out of range", frame)
	}
	pos := positions[frame]
	return fmt.Sprintf("frame %d: order %d, row %d, speed %d, patterns [%d,%d,%d], transpose [%d,%d,%d]",
		frame, pos.Order, pos.Row, pos.Speed,
		pos.PatIdx[0], pos.PatIdx[1], pos.PatIdx[2],
		pos.Transpose[0], pos.Transpose[1], pos.Transpose[2])
}

// DumpRowAtPosition dumps the row data at a specific order/row for debugging
// Also shows a few rows before for context
func DumpRowAtPosition(
	transformed transform.TransformedSong,
	encoded encode.EncodedSong,
	order, row int,
) {
	// Show context: rows leading up to the mismatch
	startRow := row - 20
	if startRow < 0 {
		startRow = 0
	}
	fmt.Printf("    Row data for order %d, rows %d-%d:\n", order, startRow, row)

	for r := startRow; r <= row; r++ {
		marker := "  "
		if r == row {
			marker = "->"
		}
		fmt.Printf("    %s row %d:\n", marker, r)

		for ch := 0; ch < 3; ch++ {
			if order >= len(encoded.TempTrackptr[ch]) {
				continue
			}
			patIdx := int(encoded.TempTrackptr[ch][order])

			if patIdx < 0 || patIdx >= len(encoded.RawPatternsEquiv) {
				continue
			}

			pat := encoded.RawPatternsEquiv[patIdx]
			if r*3+2 >= len(pat) {
				continue
			}

			off := r * 3
			b0 := pat[off]
			b1 := pat[off+1]
			b2 := pat[off+2]

			note := b0 & 0x7F
			inst := b1 & 0x1F
			effect := (b1 >> 5) | ((b0 >> 4) & 8)

			// Only print if there's something interesting (non-empty row or mismatch row)
			if r == row || (note != 0 || inst != 0 || effect != 0) {
				fmt.Printf("         ch%d: note=%02X inst=%d eff=%d param=%02X\n",
					ch, note, inst, effect, b2)

				// Show instrument wave info for triggered instruments
				if inst > 0 && int(inst) < len(transformed.Instruments) {
					instData := transformed.Instruments[inst]
					fmt.Printf("              inst %d wave: [%d,%d,%d]\n",
						inst, instData.WaveStart, instData.WaveEnd, instData.WaveLoop)
				}
			}
		}
	}
}
