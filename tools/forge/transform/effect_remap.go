package transform

import (
	"forge/analysis"
	"sort"
)

// BuildGlobalEffectRemap builds effect remapping based on usage frequency across all songs.
// Effects are sorted by frequency and assigned new effect numbers starting from 1.
// This matches odin_convert's dynamic effect remapping behavior.
func BuildGlobalEffectRemap(analyses []analysis.SongAnalysis) ([16]byte, map[int]byte) {
	// Aggregate effect usage across all songs
	allEffectCounts := make(map[byte]int)
	fSubCounts := make(map[string]int)

	for _, anal := range analyses {
		for effect, count := range anal.EffectUsage {
			allEffectCounts[effect] += count
		}
		for subName, count := range anal.FSubUsage {
			fSubCounts[subName] += count
		}
	}

	// Collect used effects (excluding 0, 4, D, F which are handled specially)
	type effectFreq struct {
		name  string
		code  int
		count int
	}
	var usedEffects []effectFreq

	for effect := byte(1); effect < 16; effect++ {
		if effect == 4 || effect == 0xD || effect == 0xF {
			continue
		}
		if count, ok := allEffectCounts[effect]; ok && count > 0 {
			usedEffects = append(usedEffects, effectFreq{
				code:  int(effect),
				count: count,
			})
		}
	}

	// Add F sub-effects
	fSubNames := []struct {
		name string
		code int
	}{
		{"speed", 0x10},
		{"hrdrest", 0x11},
		{"filttrig", 0x12},
		{"globalvol", 0x13},
		{"filtmode", 0x14},
	}
	for _, fs := range fSubNames {
		if c := fSubCounts[fs.name]; c > 0 {
			usedEffects = append(usedEffects, effectFreq{
				name:  fs.name,
				code:  fs.code,
				count: c,
			})
		}
	}

	// Sort by frequency (descending)
	sort.Slice(usedEffects, func(i, j int) bool {
		return usedEffects[i].count > usedEffects[j].count
	})

	// Build remapping: new effect number = position + 1
	effectRemap := [16]byte{}
	fSubRemap := make(map[int]byte)

	for newIdx, ef := range usedEffects {
		newEffect := byte(newIdx + 1)
		if ef.code < 0x10 {
			effectRemap[ef.code] = newEffect
		} else {
			fSubRemap[ef.code] = newEffect
		}
	}

	// Special cases that map to effect 0 with specific params
	effectRemap[4] = 0   // GT vibrato -> effect 0, param 1
	effectRemap[0xD] = 0 // GT break -> effect 0, param 2
	effectRemap[0xF] = 0 // F handled via fSubRemap; fineslide -> effect 0, param 3

	return effectRemap, fSubRemap
}
