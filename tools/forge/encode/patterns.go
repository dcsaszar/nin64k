package encode

var gapCodeToValue = []int{0, 1, 3, 7, 15, 31, 63}

func calculatePatternGap(pat []byte, truncateAfter int) int {
	numRows := len(pat) / 3
	if truncateAfter <= 0 || truncateAfter > numRows {
		truncateAfter = numRows
	}

	for code := 6; code >= 1; code-- {
		gap := gapCodeToValue[code]
		spacing := gap + 1
		if 64%spacing != 0 {
			continue
		}
		numSlots := 64 / spacing
		matches := true
		for slot := 0; slot < numSlots && matches; slot++ {
			startRow := slot * spacing
			for zeroIdx := 1; zeroIdx <= gap && matches; zeroIdx++ {
				rowNum := startRow + zeroIdx
				if rowNum >= truncateAfter {
					break
				}
				off := rowNum * 3
				if pat[off] != 0 || pat[off+1] != 0 || pat[off+2] != 0 {
					matches = false
				}
			}
		}
		if matches {
			return code
		}
	}
	return 0
}

func PackPatterns(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int) ([][]byte, []byte, int, int) {
	return PackPatternsWithNoteOnly(patterns, dict, rowToIdx, truncateLimits, nil, nil)
}

// NoteOnlyStats tracks note-only encoding statistics
var NoteOnlyStats struct {
	Used    int
	Skipped int
}

func PackPatternsWithEquiv(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int, equivMap map[int]int) ([][]byte, []byte, int, int) {
	return PackPatternsWithNoteOnly(patterns, dict, rowToIdx, truncateLimits, equivMap, nil)
}

func PackPatternsWithNoteOnly(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int, equivMap map[int]int, noteOnlyRows map[string]bool) ([][]byte, []byte, int, int) {
	const primaryMax = 224
	const rleMax = 15 // Changed from 16 to make room for $FE
	const rleBase = 0xEF
	const noteOnlyMarker = 0xFE // New: $FE = note-only escape
	const extMarker = 0xFF
	const dictZeroRleMax = 15
	const dictOffsetBase = 0x10

	// Reset stats
	NoteOnlyStats.Used = 0
	NoteOnlyStats.Skipped = 0

	numEntries := len(dict) / 3
	patternPacked := make([][]byte, len(patterns))
	gapCodes := make([]byte, len(patterns))
	primaryCount := 0
	extendedCount := 0

	for i, pat := range patterns {
		numRows := len(pat) / 3
		truncateAfter := numRows
		if i < len(truncateLimits) && truncateLimits[i] > 0 && truncateLimits[i] < truncateAfter {
			truncateAfter = truncateLimits[i]
		}

		gapCode := calculatePatternGap(pat, truncateAfter)
		gapCodes[i] = byte(gapCode)
		gap := gapCodeToValue[gapCode]
		spacing := gap + 1

		var patPacked []byte
		var prevRow [3]byte
		repeatCount := 0
		lastWasDictZero := false
		lastDictZeroPos := -1

		emitRLE := func() {
			if repeatCount == 0 {
				return
			}
			if lastWasDictZero && lastDictZeroPos >= 0 && repeatCount <= dictZeroRleMax {
				patPacked[lastDictZeroPos] = byte(repeatCount)
				lastWasDictZero = false
			} else {
				if lastWasDictZero {
					lastWasDictZero = false
				}
				for repeatCount > 0 {
					emit := repeatCount
					if emit > rleMax {
						emit = rleMax
					}
					patPacked = append(patPacked, byte(rleBase+emit-1))
					repeatCount -= emit
				}
			}
			repeatCount = 0
		}

		// Encode all slots (64/spacing) to ensure decoder can access any row
		// For rows beyond truncation, emit zeros
		numSlots := 64 / spacing
		for slot := 0; slot < numSlots; slot++ {
			row := slot * spacing
			var curRow [3]byte
			if row < truncateAfter {
				off := row * 3
				curRow = [3]byte{pat[off], pat[off+1], pat[off+2]}
			}
			// curRow is already [0,0,0] for rows beyond truncation

			if curRow == prevRow {
				repeatCount++
				maxAllowed := rleMax
				if lastWasDictZero && lastDictZeroPos >= 0 {
					maxAllowed = dictZeroRleMax
				}
				if repeatCount >= maxAllowed {
					emitRLE()
				}
			} else {
				emitRLE()

				// Check if this row is in the forced note-only set (excluded from dict)
				rowKey := string(curRow[:])
				forceNoteOnly := noteOnlyRows != nil && noteOnlyRows[rowKey]

				if forceNoteOnly {
					// Row was excluded from dict - must use note-only encoding
					patPacked = append(patPacked, noteOnlyMarker, curRow[0])
					lastWasDictZero = false
					NoteOnlyStats.Used++
				} else {
					// Normal encoding path
					idx := rowToIdx[rowKey]
					if idx == 0 && curRow != [3]byte{0, 0, 0} {
						for j := 1; j < numEntries; j++ {
							if dict[j*3] == curRow[0] && dict[j*3+1] == curRow[1] && dict[j*3+2] == curRow[2] {
								idx = j
								break
							}
						}
					}

					if equivMap != nil {
						if mappedIdx, ok := equivMap[idx]; ok {
							idx = mappedIdx
						}
					}

					// Check for note-only as fallback (when dict lookup would use extended)
					isNoteOnlyCapable := false
					if prevRow != [3]byte{0, 0, 0} && curRow != [3]byte{0, 0, 0} {
						sameInstEffParam := curRow[1] == prevRow[1] && curRow[2] == prevRow[2]
						sameEffBit3 := (curRow[0] & 0x80) == (prevRow[0] & 0x80)
						diffNote := (curRow[0] & 0x7F) != (prevRow[0] & 0x7F)
						isNoteOnlyCapable = sameInstEffParam && sameEffBit3 && diffNote
					}

					// Use note-only when it would otherwise need extended dict (both 2 bytes)
					useNoteOnly := isNoteOnlyCapable && idx >= primaryMax

					if useNoteOnly {
						patPacked = append(patPacked, noteOnlyMarker, curRow[0])
						lastWasDictZero = false
						NoteOnlyStats.Used++
					} else if idx < primaryMax {
						if idx == 0 {
							lastDictZeroPos = len(patPacked)
							patPacked = append(patPacked, 0)
							lastWasDictZero = true
						} else {
							patPacked = append(patPacked, byte(dictOffsetBase+idx-1))
							lastWasDictZero = false
						}
						primaryCount++
					} else {
						patPacked = append(patPacked, extMarker, byte(idx-primaryMax))
						lastWasDictZero = false
						extendedCount++
					}

					if isNoteOnlyCapable && !useNoteOnly {
						NoteOnlyStats.Skipped++
					}
				}
			}
			prevRow = curRow
		}

		emitRLE()
		patternPacked[i] = patPacked
	}

	return patternPacked, gapCodes, primaryCount, extendedCount
}

