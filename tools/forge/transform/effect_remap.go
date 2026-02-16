package transform

import (
	"forge/analysis"
	"sort"
)

// BuildGlobalEffectRemap builds effect remapping based on usage frequency across all songs.
// Effects are sorted by frequency and assigned new effect numbers starting from 1.
// Returns effectRemap, fSubRemap, permArpEffect, portaUpEffect, portaDownEffect, tonePortaEffect.
func BuildGlobalEffectRemap(analyses []analysis.SongAnalysis) ([16]byte, map[int]byte, byte, byte, byte, byte) {
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

	// Collect used effects (excluding special cases handled separately)
	// GTEffectVibOff, GTEffectPosJump, GTEffectBreak, GTEffectSub are handled specially
	type effectFreq struct {
		name  string
		code  int
		count int
	}
	var usedEffects []effectFreq

	for effect := byte(1); effect < 16; effect++ {
		if effect == GTEffectVibOff || effect == GTEffectPosJump || effect == GTEffectBreak || effect == GTEffectSub {
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
		{"speed", GTSubCodeSpeed},
		{"hrdrest", GTSubCodeHrdRest},
		{"filttrig", GTSubCodeFiltTrig},
		{"globalvol", GTSubCodeGlobalVol},
		{"filtmode", GTSubCodeFiltMode},
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

	// Special cases that map to player effect 0 with specific params
	effectRemap[GTEffectVibOff] = PlayerEffectSpecial  // -> effect 0, param 1 (vib off)
	effectRemap[GTEffectPosJump] = PlayerEffectSpecial // -> effect 0, param 2 (break)
	effectRemap[GTEffectBreak] = PlayerEffectSpecial   // -> effect 0, param 2 (break)
	effectRemap[GTEffectSub] = PlayerEffectSpecial     // F handled via fSubRemap; fineslide -> effect 0, param 3

	// Permanent ARP uses reserved player effect 14
	var permArpEffect byte
	if effectRemap[GTEffectArp] != 0 {
		permArpEffect = PlayerEffectPermArp
	}

	// Find porta effects (GT effects 1, 2, 3)
	portaUpEffect := effectRemap[GTEffectPortaUp]
	portaDownEffect := effectRemap[GTEffectPortaDown]
	tonePortaEffect := effectRemap[GTEffectTonePorta]

	return effectRemap, fSubRemap, permArpEffect, portaUpEffect, portaDownEffect, tonePortaEffect
}
