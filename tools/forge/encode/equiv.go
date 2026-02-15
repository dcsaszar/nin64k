package encode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type EquivResult struct {
	SongNum      int                 `json:"song"`
	Equiv        map[string][]string `json:"equiv"`
	ExcludedOrig []string            `json:"excluded_orig,omitempty"`
}

var globalEquivCache []EquivResult
var equivCacheLoaded bool

func LoadEquivCache(projectRoot string) []EquivResult {
	if equivCacheLoaded {
		return globalEquivCache
	}
	equivCacheLoaded = true

	cachePath := filepath.Join(projectRoot, "tools/odin_convert/equiv_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil
	}
	var results []EquivResult
	if json.Unmarshal(data, &results) != nil {
		return nil
	}
	globalEquivCache = results
	return results
}

// GetEquivSources returns all equiv source rows for a song (excluding already-excluded ones)
func GetEquivSources(projectRoot string, songNum int) []string {
	cache := LoadEquivCache(projectRoot)
	if cache == nil || songNum < 1 || songNum > len(cache) {
		return nil
	}
	songEquiv := cache[songNum-1]
	excluded := make(map[string]bool)
	for _, e := range songEquiv.ExcludedOrig {
		excluded[e] = true
	}
	var sources []string
	for src := range songEquiv.Equiv {
		if !excluded[src] {
			sources = append(sources, src)
		}
	}
	return sources
}

func translateRowHex(origHex string, effectRemap [16]byte, fSubRemap map[int]byte, instRemap []int) string {
	if len(origHex) != 6 {
		return origHex
	}
	var b0, b1, b2 byte
	fmt.Sscanf(origHex, "%02x%02x%02x", &b0, &b1, &b2)
	newB0, newB1, newParam := remapRowBytesForEquiv(b0, b1, b2, effectRemap, fSubRemap, instRemap)
	return fmt.Sprintf("%02x%02x%02x", newB0, newB1, newParam)
}

func remapRowBytesForEquiv(b0, b1, b2 byte, remap [16]byte, fSubRemap map[int]byte, instRemap []int) (byte, byte, byte) {
	oldEffect := (b1 >> 5) | ((b0 >> 4) & 8)
	var newEffect byte
	var newParam byte = b2

	switch oldEffect {
	case 0:
		newEffect = 0
		newParam = 0
	case 1:
		newEffect = remap[1]
		if b2&0x80 != 0 {
			newParam = 0
		} else {
			newParam = 1
		}
	case 2:
		newEffect = remap[2]
		if b2 == 0x80 {
			newParam = 1
		} else {
			newParam = 0
		}
	case 3:
		newEffect = remap[3]
		newParam = ((b2 & 0x0F) << 4) | ((b2 & 0xF0) >> 4)
	case 4:
		newEffect = 0
		newParam = 1
	case 7:
		newEffect = remap[7]
		newParam = b2
	case 8:
		newEffect = remap[8]
		newParam = b2
	case 9:
		newEffect = remap[9]
		newParam = b2
	case 0xA:
		newEffect = remap[0xA]
		newParam = b2
	case 0xB:
		newEffect = remap[0xB]
		newParam = b2
	case 0xD:
		newEffect = 0
		newParam = 2
	case 0xE:
		newEffect = remap[0xE]
		newParam = b2
	case 0xF:
		if b2 < 0x80 {
			newEffect = fSubRemap[0x10]
			newParam = b2
		} else {
			hiNib := b2 & 0xF0
			loNib := b2 & 0x0F
			switch hiNib {
			case 0xB0:
				newEffect = 0
				newParam = 3
			case 0xF0:
				newEffect = fSubRemap[0x11]
				newParam = loNib
			case 0xE0:
				newEffect = fSubRemap[0x12]
				instIdx := int(loNib)
				if instRemap != nil && instIdx > 0 && instIdx < len(instRemap) && instRemap[instIdx] > 0 {
					remapped := instRemap[instIdx]
					if remapped <= 15 {
						instIdx = remapped
					}
				}
				newParam = byte(instIdx << 4)
			case 0x80:
				newEffect = fSubRemap[0x13]
				newParam = loNib
			case 0x90:
				newEffect = fSubRemap[0x14]
				newParam = loNib << 4
			default:
				newEffect = 0
				newParam = 0
			}
		}
	default:
		newEffect = remap[oldEffect]
		newParam = b2
	}

	newB0 := (b0 & 0x7F) | ((newEffect & 8) << 4)

	inst := int(b1 & 0x1F)
	if instRemap != nil && inst > 0 && inst < len(instRemap) && instRemap[inst] > 0 {
		inst = instRemap[inst]
	}
	newB1 := byte(inst&0x1F) | ((newEffect & 7) << 5)

	return newB0, newB1, newParam
}

// TestExclusions is set during equiv bisection to test specific exclusions
var TestExclusions []string

func BuildEquivMap(
	projectRoot string,
	songNum int,
	dict []byte,
	patterns [][]byte,
	truncateLimits []int,
	effectRemap [16]byte,
	fSubRemap map[int]byte,
	instRemap []int,
) map[int]int {
	if songNum < 1 || songNum > 9 {
		return nil
	}

	equivCache := LoadEquivCache(projectRoot)
	if equivCache == nil {
		return nil
	}
	songEquiv := equivCache[songNum-1].Equiv
	if len(songEquiv) == 0 {
		return nil
	}

	excludedOrig := make(map[string]bool)
	for _, origHex := range equivCache[songNum-1].ExcludedOrig {
		excludedOrig[origHex] = true
	}
	// Add test exclusions (used during bisection)
	for _, origHex := range TestExclusions {
		excludedOrig[origHex] = true
	}
	if len(TestExclusions) > 0 {
		fmt.Printf("      [equiv] TestExclusions=%d, total excluded=%d\n", len(TestExclusions), len(excludedOrig))
	}

	translatedEquiv := make(map[string][]string)
	for origSrc, origDsts := range songEquiv {
		if excludedOrig[origSrc] {
			continue
		}
		convSrc := translateRowHex(origSrc, effectRemap, fSubRemap, instRemap)
		var convDsts []string
		for _, origDst := range origDsts {
			convDsts = append(convDsts, translateRowHex(origDst, effectRemap, fSubRemap, instRemap))
		}
		translatedEquiv[convSrc] = convDsts
	}

	numEntries := len(dict) / 3
	rowToIdx := make(map[string]int)
	rowToIdx["000000"] = 0
	for idx := 1; idx < numEntries; idx++ {
		rowHex := fmt.Sprintf("%02x%02x%02x", dict[idx*3], dict[idx*3+1], dict[idx*3+2])
		rowToIdx[rowHex] = idx
	}

	usedInPatterns := make(map[int]bool)
	usedInPatterns[0] = true
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
			if curRow != prevRow {
				rowHex := fmt.Sprintf("%02x%02x%02x", curRow[0], curRow[1], curRow[2])
				if idx, ok := rowToIdx[rowHex]; ok {
					usedInPatterns[idx] = true
				}
			}
			prevRow = curRow
		}
	}

	type equivRow struct {
		idx     int
		options []int
		hasZero bool
	}

	var rows []equivRow
	for rowHex, optionHexList := range translatedEquiv {
		idx, ok := rowToIdx[rowHex]
		if !ok {
			continue
		}
		if !usedInPatterns[idx] || idx == 0 {
			continue
		}
		var options []int
		hasZero := false
		for _, optHex := range optionHexList {
			if optIdx, ok := rowToIdx[optHex]; ok && optIdx != idx {
				options = append(options, optIdx)
				if optIdx == 0 {
					hasZero = true
				}
			}
		}
		if len(options) > 0 {
			rows = append(rows, equivRow{idx: idx, options: options, hasZero: hasZero})
		}
	}

	// Sort: standard odin_convert sorting
	// rows with idx 0 option first, then fewest options, then by idx for determinism
	for i := 0; i < len(rows)-1; i++ {
		for j := i + 1; j < len(rows); j++ {
			swap := false
			if rows[j].hasZero && !rows[i].hasZero {
				swap = true
			} else if rows[j].hasZero == rows[i].hasZero {
				if len(rows[j].options) < len(rows[i].options) {
					swap = true
				} else if len(rows[j].options) == len(rows[i].options) && rows[j].idx < rows[i].idx {
					swap = true
				}
			}
			if swap {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}

	finalUsed := make(map[int]bool)
	for idx := range usedInPatterns {
		finalUsed[idx] = true
	}

	equivMap := make(map[int]int)

	for _, r := range rows {
		if r.hasZero {
			equivMap[r.idx] = 0
			delete(finalUsed, r.idx)
		}
	}

	for _, r := range rows {
		if _, mapped := equivMap[r.idx]; mapped {
			continue
		}

		bestTarget := -1
		for _, opt := range r.options {
			if finalUsed[opt] && (bestTarget < 0 || opt < bestTarget) {
				bestTarget = opt
			}
		}

		if bestTarget >= 0 {
			equivMap[r.idx] = bestTarget
			delete(finalUsed, r.idx)
		}
	}

	changed := true
	for changed {
		changed = false
		for _, r := range rows {
			if _, mapped := equivMap[r.idx]; mapped {
				continue
			}

			bestTarget := -1
			for _, opt := range r.options {
				if finalUsed[opt] && (bestTarget < 0 || opt < bestTarget) {
					bestTarget = opt
				}
			}

			if bestTarget >= 0 {
				equivMap[r.idx] = bestTarget
				delete(finalUsed, r.idx)
				changed = true
			}
		}
	}

	if len(TestExclusions) > 0 {
		fmt.Printf("      [equiv] Final equivMap size: %d\n", len(equivMap))
	}
	return equivMap
}
