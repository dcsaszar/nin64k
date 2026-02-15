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

func packPatterns(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int) ([][]byte, []byte, int, int) {
	return packPatternsWithEquiv(patterns, dict, rowToIdx, truncateLimits, nil)
}

func packPatternsWithEquiv(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int, equivMap map[int]int) ([][]byte, []byte, int, int) {
	const primaryMax = 224
	const rleMax = 16
	const rleBase = 0xEF
	const extMarker = 0xFF
	const dictZeroRleMax = 15
	const dictOffsetBase = 0x10

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

				idx := rowToIdx[string(curRow[:])]
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

				if idx < primaryMax {
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
			}
			prevRow = curRow
		}

		emitRLE()
		patternPacked[i] = patPacked
	}

	return patternPacked, gapCodes, primaryCount, extendedCount
}

func optimizeOverlap(patterns [][]byte) ([]byte, []uint16) {
	n := len(patterns)
	if n == 0 {
		return nil, nil
	}

	canonical := make([]int, n)
	for i := range canonical {
		canonical[i] = i
	}
	for i := 0; i < n; i++ {
		if canonical[i] != i {
			continue
		}
		for j := i + 1; j < n; j++ {
			if canonical[j] != j {
				continue
			}
			if string(patterns[i]) == string(patterns[j]) {
				canonical[j] = i
			}
		}
	}

	var uniquePatterns [][]byte
	origToUnique := make([]int, n)
	for i := 0; i < n; i++ {
		if canonical[i] == i {
			origToUnique[i] = len(uniquePatterns)
			uniquePatterns = append(uniquePatterns, patterns[i])
		} else {
			origToUnique[i] = -1
		}
	}
	for i := 0; i < n; i++ {
		if canonical[i] != i {
			origToUnique[i] = origToUnique[canonical[i]]
		}
	}

	numUnique := len(uniquePatterns)
	if numUnique == 0 {
		return nil, make([]uint16, n)
	}

	strings := make([][]byte, numUnique)
	for i := range strings {
		strings[i] = make([]byte, len(uniquePatterns[i]))
		copy(strings[i], uniquePatterns[i])
	}

	patternOffset := make([]int, numUnique)
	root := make([]int, numUnique)
	for i := range root {
		root[i] = i
	}

	for {
		bestOverlap := 0
		bestI, bestJ := -1, -1

		for i := 0; i < numUnique; i++ {
			if strings[i] == nil {
				continue
			}
			for j := 0; j < numUnique; j++ {
				if i == j || strings[j] == nil {
					continue
				}
				si, sj := strings[i], strings[j]
				maxLen := len(si)
				if len(sj) < maxLen {
					maxLen = len(sj)
				}
				for l := maxLen; l >= 1; l-- {
					if string(si[len(si)-l:]) == string(sj[:l]) {
						if l > bestOverlap {
							bestOverlap = l
							bestI, bestJ = i, j
						}
						break
					}
				}
			}
		}

		if bestOverlap == 0 {
			break
		}

		si := strings[bestI]
		sj := strings[bestJ]
		merged := make([]byte, len(si)+len(sj)-bestOverlap)
		copy(merged, si)
		copy(merged[len(si):], sj[bestOverlap:])
		strings[bestI] = merged

		offsetShift := len(si) - bestOverlap
		for p := 0; p < numUnique; p++ {
			if root[p] == bestJ {
				root[p] = bestI
				patternOffset[p] += offsetShift
			}
		}

		strings[bestJ] = nil
	}

	var packed []byte
	uniqueOffset := make([]int, numUnique)
	for i := 0; i < numUnique; i++ {
		if strings[i] != nil {
			baseOffset := len(packed)
			packed = append(packed, strings[i]...)
			for p := 0; p < numUnique; p++ {
				if root[p] == i {
					uniqueOffset[p] = baseOffset + patternOffset[p]
				}
			}
		}
	}

	offsets := make([]uint16, n)
	for i := 0; i < n; i++ {
		offsets[i] = uint16(uniqueOffset[origToUnique[i]])
	}

	return packed, offsets
}
