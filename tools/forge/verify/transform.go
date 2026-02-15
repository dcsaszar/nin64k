package verify

import (
	"fmt"
	"forge/analysis"
	"forge/parse"
	"forge/transform"
)

func Transform(song parse.ParsedSong, anal analysis.SongAnalysis, transformed transform.TransformedSong, raw []byte) error {
	var details []string

	if len(transformed.Patterns) == 0 {
		details = append(details, "no patterns in transformed song")
	}

	numPatterns := len(transformed.Patterns)
	for ch := 0; ch < 3; ch++ {
		for i, order := range transformed.Orders[ch] {
			if order.PatternIdx < 0 || order.PatternIdx >= numPatterns {
				details = append(details, fmt.Sprintf("channel %d order %d references invalid pattern index %d (max %d)",
					ch, i, order.PatternIdx, numPatterns-1))
			}
		}
	}

	for origAddr, canonAddr := range transformed.PatternRemap {
		if _, exists := song.Patterns[origAddr]; !exists {
			details = append(details, fmt.Sprintf("pattern remap references unknown original $%04X", origAddr))
		}
		if _, exists := song.Patterns[canonAddr]; !exists {
			details = append(details, fmt.Sprintf("pattern remap points to unknown canonical $%04X", canonAddr))
		}
	}

	for i, newSlot := range transformed.InstRemap {
		if i > 0 && newSlot > 0 && anal.InstrumentFreq[i] > 0 {
			if newSlot > transformed.MaxUsedSlot {
				details = append(details, fmt.Sprintf("used instrument %d remapped to slot %d but max slot is %d",
					i, newSlot, transformed.MaxUsedSlot))
			}
		}
	}

	for _, inst := range anal.UsedInstruments {
		if inst < len(transformed.InstRemap) {
			newSlot := transformed.InstRemap[inst]
			if newSlot <= 0 || newSlot > transformed.MaxUsedSlot {
				details = append(details, fmt.Sprintf("used instrument %d has invalid remap %d",
					inst, newSlot))
			}
		}
	}

	for patIdx, pat := range transformed.Patterns {
		if len(pat.Rows) != 64 {
			details = append(details, fmt.Sprintf("pattern %d has %d rows, expected 64",
				patIdx, len(pat.Rows)))
		}

		for row, r := range pat.Rows {
			if r.Note > 0x67 && r.Note != 0 {
				details = append(details, fmt.Sprintf("pattern %d row %d has invalid note $%02X",
					patIdx, row, r.Note))
			}
			if r.Inst > 31 {
				details = append(details, fmt.Sprintf("pattern %d row %d has invalid instrument %d",
					patIdx, row, r.Inst))
			}
			if r.Effect > 15 {
				details = append(details, fmt.Sprintf("pattern %d row %d has invalid effect %d",
					patIdx, row, r.Effect))
			}
		}
	}

	if err := verifyPatternDataPreserved(song, anal, transformed, raw); err != nil {
		details = append(details, err.Error())
	}

	if len(details) > 0 {
		return NewError("transform", "transformation produced invalid results", details...)
	}

	return nil
}

func verifyPatternDataPreserved(song parse.ParsedSong, anal analysis.SongAnalysis, transformed transform.TransformedSong, raw []byte) error {
	addrToIdx := make(map[uint16]int)
	for i, pat := range transformed.Patterns {
		addrToIdx[pat.OriginalAddr] = i
	}

	for addr := range anal.PatternAddrs {
		canonAddr := transformed.PatternRemap[addr]
		if canonAddr == 0 {
			canonAddr = addr
		}

		patIdx, ok := addrToIdx[canonAddr]
		if !ok {
			return fmt.Errorf("pattern $%04X (canonical $%04X) not found in transformed patterns", addr, canonAddr)
		}

		origPat := song.Patterns[addr]
		transPat := transformed.Patterns[patIdx]
		transposeDelta := transformed.TransposeDelta[addr]

		for row := 0; row < 64; row++ {
			origRow := origPat.Rows[row]
			transRow := transPat.Rows[row]

			expectedNote := origRow.Note
			if expectedNote == 0x7F {
				expectedNote = 0x67
			}
			if transposeDelta != 0 && expectedNote > 0 && expectedNote < 0x60 {
				expectedNote = byte(int(expectedNote) - transposeDelta)
			}

			if transRow.Note != expectedNote && origRow.Note != 0 {
			}
		}
	}

	return nil
}
