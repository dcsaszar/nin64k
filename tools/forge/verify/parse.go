package verify

import (
	"fmt"
	"forge/parse"
)

func Parse(song parse.ParsedSong) error {
	var details []string

	if song.NumOrders <= 0 {
		details = append(details, "NumOrders <= 0")
	}

	if len(song.Patterns) == 0 {
		details = append(details, "no patterns parsed")
	}

	for ch := 0; ch < 3; ch++ {
		if len(song.Orders[ch]) != song.NumOrders {
			details = append(details, fmt.Sprintf("channel %d has %d orders, expected %d",
				ch, len(song.Orders[ch]), song.NumOrders))
		}

		for i, order := range song.Orders[ch] {
			if _, exists := song.Patterns[order.PatternAddr]; !exists {
				details = append(details, fmt.Sprintf("channel %d order %d references missing pattern $%04X",
					ch, i, order.PatternAddr))
			}
		}
	}

	for i, inst := range song.Instruments {
		if i == 0 {
			continue
		}

		if inst.WaveEnd < inst.WaveStart && inst.WaveEnd != 0xFF {
			details = append(details, fmt.Sprintf("instrument %d: wave end %d < start %d",
				i, inst.WaveEnd, inst.WaveStart))
		}

		if inst.ArpEnd < inst.ArpStart && inst.ArpEnd != 0xFF {
			details = append(details, fmt.Sprintf("instrument %d: arp end %d < start %d",
				i, inst.ArpEnd, inst.ArpStart))
		}

		if inst.FilterEnd < inst.FilterStart && inst.FilterEnd != 0xFF {
			details = append(details, fmt.Sprintf("instrument %d: filter end %d < start %d",
				i, inst.FilterEnd, inst.FilterStart))
		}

		if int(inst.WaveStart) >= len(song.WaveTable) && inst.WaveStart < 0xFF {
			details = append(details, fmt.Sprintf("instrument %d: wave start %d out of bounds (table len %d)",
				i, inst.WaveStart, len(song.WaveTable)))
		}
	}

	if len(details) > 0 {
		return NewError("parse", "parsed song has invalid data", details...)
	}

	return nil
}
