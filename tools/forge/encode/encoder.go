package encode

import (
	"fmt"
	"forge/parse"
	"forge/transform"
	"sort"
	"strings"
)

type EncodedSong struct {
	RowDict           []byte
	RowToIdx          map[string]int
	RawPatterns       [][]byte // Pre-packed patterns (192 bytes each = 64 rows * 3) - before equiv
	RawPatternsEquiv  [][]byte // Post-equivalence patterns - what was actually encoded
	TruncateLimits    []int    // Truncation point for each pattern
	PatternData       [][]byte
	PatternOffsets    []uint16
	PatternGapCodes   []byte
	PackedPatterns    []byte
	CanonPatterns     [][]byte // Deduplicated patterns for gap filling
	CanonGapCodes     []byte   // Gap codes for canonical patterns
	PatternCanon      []int    // Maps original pattern index to canonical index
	PrimaryCount      int
	ExtendedCount     int
	OrderBitstream    []byte
	DeltaTable        []byte
	DeltaBases        []int
	StartConst        int
	TransposeTable    []byte
	TransposeBases    []int
	InstrumentData    []byte
	TrackStarts       [3]byte
	TempTranspose     [3][]byte
	TempTrackptr      [3][]byte
}

func Encode(song transform.TransformedSong) EncodedSong {
	return EncodeWithReorder(song, true)
}

func EncodeWithEquiv(song transform.TransformedSong, songNum int, projectRoot string) EncodedSong {
	return encodeInternal(song, true, songNum, projectRoot)
}

func EncodeWithEquivNoReorder(song transform.TransformedSong, songNum int, projectRoot string) EncodedSong {
	return encodeInternal(song, false, songNum, projectRoot)
}

func EncodeWithReorder(song transform.TransformedSong, doReorder bool) EncodedSong {
	return encodeInternal(song, doReorder, 0, "")
}

func encodeInternal(song transform.TransformedSong, doReorder bool, songNum int, projectRoot string) EncodedSong {
	result := EncodedSong{
		RowToIdx: make(map[string]int),
	}

	numPatterns := len(song.Patterns)

	var reorderMap []int
	if doReorder && numPatterns > 0 {
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
				panic(fmt.Sprintf("encodeInternal: reorderMap[%d] = %d out of bounds (numPatterns=%d)", oldIdx, newIdx, numPatterns))
			}
		}
		if usedSlots[newIdx] {
			panic(fmt.Sprintf("encodeInternal: slot %d used twice (oldIdx=%d)", newIdx, oldIdx))
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
			panic(fmt.Sprintf("encodeInternal: slot %d not filled (patterns array has gap)", i))
		}
	}

	result.RowDict = buildDictionary(patterns, truncateLimits)

	result.RowToIdx[string([]byte{0, 0, 0})] = 0
	numEntries := len(result.RowDict) / 3
	dictSet := make(map[string]bool)
	dictSet[string([]byte{0, 0, 0})] = true
	for idx := 1; idx < numEntries; idx++ {
		row := string(result.RowDict[idx*3 : idx*3+3])
		result.RowToIdx[row] = idx
		dictSet[row] = true
	}

	for patIdx, pat := range song.Patterns {
		truncateAt := pat.TruncateAt
		if truncateAt <= 0 || truncateAt > 64 {
			truncateAt = 64
		}
		var prevRow [3]byte
		for row := 0; row < truncateAt; row++ {
			r := pat.Rows[row]
			b0 := (r.Note & 0x7F) | ((r.Effect & 8) << 4)
			b1 := (r.Inst & 0x1F) | ((r.Effect & 7) << 5)
			curRow := [3]byte{b0, b1, r.Param}
			if curRow == prevRow || curRow == [3]byte{0, 0, 0} {
				continue
			}
			if !dictSet[string(curRow[:])] {
				panic(fmt.Sprintf("encodeInternal: song.Patterns[%d] row %d (%02X %02X %02X) not in dictionary",
					patIdx, row, curRow[0], curRow[1], curRow[2]))
			}
			prevRow = curRow
		}
	}

	var equivMap map[int]int
	if songNum > 0 && projectRoot != "" {
		equivMap = BuildEquivMap(
			projectRoot,
			songNum,
			result.RowDict,
			patterns,
			truncateLimits,
			song.EffectRemap,
			song.FSubRemap,
			song.InstRemap,
		)
	}

	origDict := result.RowDict
	compactDict, oldToNew := compactDictionary(
		result.RowDict, result.RowToIdx, patterns, truncateLimits, equivMap)
	result.RowDict = compactDict

	// Report dictionary size (temporarily removed limit for testing)
	numCompact := len(compactDict) / 3
	fmt.Printf("  [dict] song %d: %d entries\n", songNum, numCompact)

	result.RowToIdx = make(map[string]int)
	result.RowToIdx[string([]byte{0, 0, 0})] = 0
	for idx := 1; idx < numCompact; idx++ {
		row := string(compactDict[idx*3 : idx*3+3])
		result.RowToIdx[row] = idx
	}

	numOrig := len(origDict) / 3
	for oldIdx := 1; oldIdx < numOrig; oldIdx++ {
		row := string(origDict[oldIdx*3 : oldIdx*3+3])
		if _, exists := result.RowToIdx[row]; !exists {
			newIdx, hasMapping := oldToNew[oldIdx]
			if hasMapping {
				result.RowToIdx[row] = newIdx
			} else if equivMap != nil {
				if target, ok := equivMap[oldIdx]; ok {
					if targetNewIdx, ok := oldToNew[target]; ok {
						result.RowToIdx[row] = targetNewIdx
					}
				}
			}
		}
	}

	for patIdx, pat := range song.Patterns {
		truncateAt := pat.TruncateAt
		if truncateAt <= 0 || truncateAt > 64 {
			truncateAt = 64
		}
		var prevRow [3]byte
		for row := 0; row < truncateAt; row++ {
			r := pat.Rows[row]
			b0 := (r.Note & 0x7F) | ((r.Effect & 8) << 4)
			b1 := (r.Inst & 0x1F) | ((r.Effect & 7) << 5)
			curRow := [3]byte{b0, b1, r.Param}
			if curRow == prevRow || curRow == [3]byte{0, 0, 0} {
				continue
			}
			if _, ok := result.RowToIdx[string(curRow[:])]; !ok {
				inOrig := false
				origIdx := -1
				for i := 1; i < numOrig; i++ {
					if origDict[i*3] == curRow[0] && origDict[i*3+1] == curRow[1] && origDict[i*3+2] == curRow[2] {
						inOrig = true
						origIdx = i
						break
					}
				}
				newIdx := -1
				equivTarget := -1
				equivTargetNewIdx := -1
				if origIdx >= 0 {
					newIdx = oldToNew[origIdx]
					if equivMap != nil {
						if t, ok := equivMap[origIdx]; ok {
							equivTarget = t
							equivTargetNewIdx = oldToNew[t]
						}
					}
				}
				reorderedPos := patIdx
				if reorderMap != nil {
					reorderedPos = reorderMap[patIdx]
				}
				inPatterns := false
				patRow := -1
				if reorderedPos < len(patterns) {
					p := patterns[reorderedPos]
					for r := 0; r < len(p)/3; r++ {
						if p[r*3] == curRow[0] && p[r*3+1] == curRow[1] && p[r*3+2] == curRow[2] {
							inPatterns = true
							patRow = r
							break
						}
					}
				}
				panic(fmt.Sprintf("encodeInternal AFTER compaction: song.Patterns[%d] row %d (%02X %02X %02X) not in RowToIdx\n"+
					"  was in origDict: %v (origIdx=%d, oldToNew[origIdx]=%d)\n"+
					"  equivMap[%d]=%d, oldToNew[%d]=%d\n"+
					"  reordered to patterns[%d], found in patterns: %v (row %d)\n"+
					"  truncateLimits[%d]=%d",
					patIdx, row, curRow[0], curRow[1], curRow[2],
					inOrig, origIdx, newIdx,
					origIdx, equivTarget, equivTarget, equivTargetNewIdx,
					reorderedPos, inPatterns, patRow,
					reorderedPos, truncateLimits[reorderedPos]))
			}
			prevRow = curRow
		}
	}

	compactEquiv := make(map[int]int)
	for oldIdx, target := range equivMap {
		newOld := oldToNew[oldIdx]
		newTarget := oldToNew[target]
		if newOld != newTarget {
			compactEquiv[newOld] = newTarget
		}
	}

	// Deduplicate patterns BEFORE packing based on equiv-aware signatures (odin approach)
	canonPatterns, canonTruncate, patternToCanon := deduplicatePatternsWithEquiv(
		patterns, compactDict, result.RowToIdx, truncateLimits, compactEquiv)

	// Pack only canonical patterns
	canonPackedData, canonGapCodes, primaryCount, extendedCount :=
		packPatternsWithEquiv(canonPatterns, compactDict, result.RowToIdx, canonTruncate, compactEquiv)

	result.PrimaryCount = primaryCount
	result.ExtendedCount = extendedCount

	// Build PatternData by mapping original patterns to canonical packed data
	result.PatternData = make([][]byte, len(patterns))
	result.PatternGapCodes = make([]byte, len(patterns))
	for i := range patterns {
		canonIdx := patternToCanon[i]
		result.PatternData[i] = canonPackedData[canonIdx]
		result.PatternGapCodes[i] = canonGapCodes[canonIdx]
	}

	// Run overlap optimization on canonical packed patterns
	var canonOffsets []uint16
	result.PackedPatterns, canonOffsets = optimizeOverlap(canonPackedData)

	// Map offsets back to original pattern indices
	result.PatternOffsets = make([]uint16, len(patterns))
	for i := range patterns {
		canonIdx := patternToCanon[i]
		result.PatternOffsets[i] = canonOffsets[canonIdx]
	}
	_ = canonTruncate

	// Store canonical patterns for gap filling in serializer
	result.CanonPatterns = canonPackedData
	result.CanonGapCodes = canonGapCodes
	result.PatternCanon = patternToCanon

	result.TempTranspose, result.TempTrackptr, result.TrackStarts =
		encodeOrdersWithRemap(song, reorderMap)

	result.InstrumentData = encodeInstruments(song.Instruments, song.MaxUsedSlot)

	// Store raw patterns for semantic verification (pre-equivalence)
	result.RawPatterns = patterns
	// Store post-equivalence patterns for packed pattern verification
	result.RawPatternsEquiv = applyEquivToPatterns(patterns, compactDict, result.RowToIdx, truncateLimits, compactEquiv)
	// Store truncation limits for verification
	result.TruncateLimits = truncateLimits

	return result
}

func applyEquivToPatterns(patterns [][]byte, dict []byte, rowToIdx map[string]int, truncateLimits []int, equivMap map[int]int) [][]byte {
	numEntries := len(dict) / 3
	idxToRow := make(map[int][3]byte, numEntries)
	idxToRow[0] = [3]byte{0, 0, 0}
	for idx := 1; idx < numEntries; idx++ {
		idxToRow[idx] = [3]byte{dict[idx*3], dict[idx*3+1], dict[idx*3+2]}
	}

	result := make([][]byte, len(patterns))
	for i, pat := range patterns {
		newPat := make([]byte, len(pat))

		numRows := len(pat) / 3
		truncateAt := numRows
		if i < len(truncateLimits) && truncateLimits[i] > 0 && truncateLimits[i] < truncateAt {
			truncateAt = truncateLimits[i]
		}

		// Apply equivalence substitution for rows within truncation
		for row := 0; row < truncateAt; row++ {
			off := row * 3
			curRow := [3]byte{pat[off], pat[off+1], pat[off+2]}
			idx := rowToIdx[string(curRow[:])]
			// Apply additional equivalence mapping if present
			if targetIdx, hasMapping := equivMap[idx]; hasMapping {
				idx = targetIdx
			}
			// Always use dictionary bytes (handles both equivalence and compaction)
			targetRow := idxToRow[idx]
			newPat[off] = targetRow[0]
			newPat[off+1] = targetRow[1]
			newPat[off+2] = targetRow[2]
		}
		// Rows beyond truncation are zero (implicit - newPat already initialized to zeros)
		result[i] = newPat
	}
	return result
}

func buildDictionary(patterns [][]byte, truncateLimits []int) []byte {
	// Count frequency for ALL rows (including beyond truncation) to match odin_convert's ordering
	rowUsage := make(map[string]int)
	allRows := make(map[string]bool)

	for _, pat := range patterns {
		numRows := len(pat) / 3
		var prevRow [3]byte
		for row := 0; row < numRows; row++ {
			off := row * 3
			curRow := [3]byte{pat[off], pat[off+1], pat[off+2]}
			if curRow != [3]byte{0, 0, 0} {
				allRows[string(curRow[:])] = true
			}
			if curRow != prevRow && curRow != [3]byte{0, 0, 0} {
				rowUsage[string(curRow[:])]++
			}
			prevRow = curRow
		}
	}

	type dictEntry struct {
		row   [3]byte
		count int
	}
	var entries []dictEntry
	for rowStr, count := range rowUsage {
		var row [3]byte
		copy(row[:], rowStr)
		entries = append(entries, dictEntry{row, count})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		// Tie-breaker: sort by row bytes ascending for determinism
		return string(entries[i].row[:]) < string(entries[j].row[:])
	})

	dict := make([]byte, (len(entries)+1)*3)
	dictSet := make(map[string]bool)
	dictSet[string([]byte{0, 0, 0})] = true
	for i, e := range entries {
		slot := i + 1
		copy(dict[slot*3:], e.row[:])
		dictSet[string(e.row[:])] = true
	}

	for row := range allRows {
		if !dictSet[row] {
			b := []byte(row)
			panic(fmt.Sprintf("buildDictionary: row %02X %02X %02X in patterns but not in dictionary", b[0], b[1], b[2]))
		}
	}

	return dict
}

func encodeInstruments(instruments []parse.Instrument, maxUsedSlot int) []byte {
	return encodeInstrumentsFromSource(instruments, maxUsedSlot, nil, 0, 0)
}

func encodeInstrumentsFromSource(instruments []parse.Instrument, maxUsedSlot int, raw []byte, srcInstOff, numInst int) []byte {
	if maxUsedSlot <= 0 {
		return nil
	}

	vibDepthRemap := [16]byte{0, 4, 2, 3, 1, 7, 5, 0, 8, 0, 6, 0, 0, 0, 0, 9}

	data := make([]byte, maxUsedSlot*16)

	for i := 1; i <= maxUsedSlot && i < len(instruments); i++ {
		base := (i - 1) * 16

		// Read raw bytes from source if available, otherwise use parsed struct
		var params [16]byte
		if raw != nil && numInst > 0 && i < numInst {
			for p := 0; p < 16; p++ {
				idx := srcInstOff + p*numInst + i
				if idx < len(raw) {
					params[p] = raw[idx]
				}
			}
		} else {
			inst := instruments[i]
			params[0] = inst.AD
			params[1] = inst.SR
			params[2] = inst.WaveStart
			params[3] = inst.WaveEnd
			params[4] = inst.WaveLoop
			params[5] = inst.ArpStart
			params[6] = inst.ArpEnd
			params[7] = inst.ArpLoop
			params[8] = inst.VibDelay
			params[9] = inst.VibDepthSpeed
			params[10] = inst.PulseWidth
			params[11] = inst.PulseSpeed
			params[12] = inst.PulseLimits
			params[13] = inst.FilterStart
			params[14] = inst.FilterEnd
			params[15] = inst.FilterLoop
		}

		// Copy with transformations matching odin_convert
		data[base+0] = params[0]  // AD
		data[base+1] = params[1]  // SR
		data[base+2] = params[2]  // WaveStart
		// WaveEnd + 1 (only if < 255)
		if params[3] < 255 {
			data[base+3] = params[3] + 1
		} else {
			data[base+3] = params[3]
		}
		data[base+4] = params[4]  // WaveLoop
		data[base+5] = params[5]  // ArpStart
		// ArpEnd + 1 (only if < 255)
		if params[6] < 255 {
			data[base+6] = params[6] + 1
		} else {
			data[base+6] = params[6]
		}
		data[base+7] = params[7]  // ArpLoop
		data[base+8] = params[8]  // VibDelay (param 8)

		// Remap vibrato depth (param 9)
		oldDepth := params[9] >> 4
		speed := params[9] & 0x0F
		data[base+9] = (vibDepthRemap[oldDepth] << 4) | speed

		// Swap nibbles for pulse width (param 10)
		data[base+10] = (params[10] << 4) | (params[10] >> 4)

		data[base+11] = params[11]  // PulseSpeed (param 11)
		data[base+12] = params[12]  // PulseLimits (param 12)
		data[base+13] = params[13]  // FilterStart
		// FilterEnd + 1 (only if < 255)
		if params[14] < 255 {
			data[base+14] = params[14] + 1
		} else {
			data[base+14] = params[14]
		}
		data[base+15] = params[15]  // FilterLoop
	}

	return data
}

func compactDictionary(
	dict []byte,
	rowToIdx map[string]int,
	patterns [][]byte,
	truncateLimits []int,
	equivMap map[int]int,
) ([]byte, map[int]int) {
	numEntries := len(dict) / 3
	usedIdx := make(map[int]bool)
	idxCount := make(map[int]int)
	usedIdx[0] = true

	for i, pat := range patterns {
		numRows := len(pat) / 3
		truncateAt := numRows
		if i < len(truncateLimits) && truncateLimits[i] > 0 && truncateLimits[i] < truncateAt {
			truncateAt = truncateLimits[i]
		}

		var prevRow [3]byte
		for row := 0; row < truncateAt; row++ {
			off := row * 3
			curRow := [3]byte{pat[off], pat[off+1], pat[off+2]}
			if curRow == prevRow {
				continue
			}
			idx := rowToIdx[string(curRow[:])]
			if equivMap != nil {
				if mappedIdx, ok := equivMap[idx]; ok {
					idx = mappedIdx
				}
			}
			usedIdx[idx] = true
			idxCount[idx]++
			prevRow = curRow
		}
	}

	type entry struct {
		row    [3]byte
		oldIdx int
		count  int
	}
	var entries []entry
	for idx := 1; idx < numEntries; idx++ {
		if usedIdx[idx] {
			var row [3]byte
			copy(row[:], dict[idx*3:idx*3+3])
			entries = append(entries, entry{row, idx, idxCount[idx]})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		// Tie-breaker: sort by row bytes ascending for determinism
		return string(entries[i].row[:]) < string(entries[j].row[:])
	})

	compactDict := make([]byte, (len(entries)+1)*3)
	oldToNew := make(map[int]int)
	oldToNew[0] = 0

	for i, e := range entries {
		slot := i + 1
		copy(compactDict[slot*3:], e.row[:])
		oldToNew[e.oldIdx] = slot
	}

	changed := true
	for changed {
		changed = false
		for oldIdx := 0; oldIdx < numEntries; oldIdx++ {
			if _, exists := oldToNew[oldIdx]; !exists {
				if equivMap != nil {
					if target, ok := equivMap[oldIdx]; ok {
						if newTarget, ok := oldToNew[target]; ok {
							oldToNew[oldIdx] = newTarget
							changed = true
							continue
						}
					}
				}
			}
		}
	}

	for oldIdx := 0; oldIdx < numEntries; oldIdx++ {
		if _, exists := oldToNew[oldIdx]; !exists {
			oldToNew[oldIdx] = 0
		}
	}

	return compactDict, oldToNew
}

// deduplicatePatternsWithEquiv deduplicates patterns BEFORE packing based on equiv-aware signatures
// This matches odin's approach of deduplicating at the semantic level
func deduplicatePatternsWithEquiv(
	patterns [][]byte,
	dict []byte,
	rowToIdx map[string]int,
	truncateLimits []int,
	equivMap map[int]int,
) ([][]byte, []int, []int) {
	n := len(patterns)
	if n == 0 {
		return nil, nil, nil
	}

	// Build equiv-aware signature for each pattern
	// Signature is the sequence of dict indices after applying equiv mapping
	getSignature := func(pat []byte, truncateAt int) string {
		var sig strings.Builder
		numRows := len(pat) / 3
		if truncateAt > 0 && truncateAt < numRows {
			numRows = truncateAt
		}
		for row := 0; row < numRows; row++ {
			off := row * 3
			rowBytes := string(pat[off : off+3])
			idx := rowToIdx[rowBytes]
			// Apply equiv mapping
			if equivMap != nil {
				if mappedIdx, ok := equivMap[idx]; ok {
					idx = mappedIdx
				}
			}
			sig.WriteString(fmt.Sprintf("%d,", idx))
		}
		return sig.String()
	}

	// Find canonical pattern for each pattern
	sigToCanon := make(map[string]int)
	patternToCanon := make([]int, n)

	for i, pat := range patterns {
		truncateAt := 64
		if i < len(truncateLimits) && truncateLimits[i] > 0 {
			truncateAt = truncateLimits[i]
		}
		sig := getSignature(pat, truncateAt)
		if canon, exists := sigToCanon[sig]; exists {
			patternToCanon[i] = canon
		} else {
			sigToCanon[sig] = i
			patternToCanon[i] = i
		}
	}

	// Build canonical pattern list
	var canonPatterns [][]byte
	var canonTruncate []int
	oldToNew := make(map[int]int)

	for i, pat := range patterns {
		if patternToCanon[i] == i {
			oldToNew[i] = len(canonPatterns)
			canonPatterns = append(canonPatterns, pat)
			if i < len(truncateLimits) {
				canonTruncate = append(canonTruncate, truncateLimits[i])
			} else {
				canonTruncate = append(canonTruncate, 64)
			}
		}
	}

	// Update patternToCanon to use new indices
	finalMapping := make([]int, n)
	for i := range patterns {
		canonOldIdx := patternToCanon[i]
		finalMapping[i] = oldToNew[canonOldIdx]
	}

	dedupCount := n - len(canonPatterns)
	if dedupCount > 0 {
		fmt.Printf("  [equiv-dedup] Deduplicated %d patterns before packing (%d → %d)\n",
			dedupCount, n, len(canonPatterns))
	}

	return canonPatterns, canonTruncate, finalMapping
}

func findEquivEquivPatterns(
	packedPatterns [][]byte,
	gapCodes []byte,
	truncateLimits []int,
) ([][]byte, []byte, []int, []int) {
	n := len(packedPatterns)
	if n == 0 {
		return nil, nil, nil, nil
	}

	sigToCanon := make(map[string]int)
	patternCanon := make([]int, n)

	for i, packed := range packedPatterns {
		// Include gap code in signature - patterns with different gaps decode differently
		var gap byte
		if i < len(gapCodes) {
			gap = gapCodes[i]
		}
		sig := string(append([]byte{gap}, packed...))
		if canon, exists := sigToCanon[sig]; exists {
			patternCanon[i] = canon
		} else {
			sigToCanon[sig] = i
			patternCanon[i] = i
		}
	}

	var canonPatterns [][]byte
	var canonGapCodes []byte
	var canonTruncate []int
	oldToNew := make(map[int]int)

	for i, packed := range packedPatterns {
		if patternCanon[i] == i {
			oldToNew[i] = len(canonPatterns)
			canonPatterns = append(canonPatterns, packed)
			if i < len(gapCodes) {
				canonGapCodes = append(canonGapCodes, gapCodes[i])
			} else {
				canonGapCodes = append(canonGapCodes, 0)
			}
			if i < len(truncateLimits) {
				canonTruncate = append(canonTruncate, truncateLimits[i])
			} else {
				canonTruncate = append(canonTruncate, 64)
			}
		}
	}

	finalCanon := make([]int, n)
	for i := range packedPatterns {
		canonIdx := patternCanon[i]
		finalCanon[i] = oldToNew[canonIdx]
	}

	return canonPatterns, canonGapCodes, canonTruncate, finalCanon
}
