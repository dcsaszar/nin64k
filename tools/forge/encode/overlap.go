package encode

// OverlapStats tracks pattern overlap compression statistics
var OverlapStats struct {
	UnpackedSize int
	PackedSize   int
	BytesSaved   int
}

// OptimizeOverlap uses greedy superstring algorithm to pack patterns with overlap
func OptimizeOverlap(patterns [][]byte) ([]byte, []uint16) {
	n := len(patterns)
	if n == 0 {
		return nil, nil
	}

	// Track unpacked size
	unpackedSize := 0
	for _, p := range patterns {
		unpackedSize += len(p)
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

		si, sj := strings[bestI], strings[bestJ]
		merged := make([]byte, len(si)+len(sj)-bestOverlap)
		copy(merged, si)
		copy(merged[len(si):], sj[bestOverlap:])

		offsetShift := len(si) - bestOverlap
		for k := 0; k < numUnique; k++ {
			if root[k] == bestJ {
				patternOffset[k] += offsetShift
				root[k] = bestI
			}
		}

		strings[bestI] = merged
		strings[bestJ] = nil
	}

	var packed []byte
	uniqueOffset := make([]int, numUnique)
	for i := 0; i < numUnique; i++ {
		if strings[i] != nil {
			baseOffset := len(packed)
			packed = append(packed, strings[i]...)
			for k := 0; k < numUnique; k++ {
				if root[k] == i {
					uniqueOffset[k] = baseOffset + patternOffset[k]
				}
			}
		}
	}

	offsets := make([]uint16, n)
	for i := 0; i < n; i++ {
		offsets[i] = uint16(uniqueOffset[origToUnique[i]])
	}

	// Update stats
	OverlapStats.UnpackedSize = unpackedSize
	OverlapStats.PackedSize = len(packed)
	OverlapStats.BytesSaved = unpackedSize - len(packed)

	return packed, offsets
}
