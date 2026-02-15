package verify

import (
	"fmt"
	"forge/analysis"
	"forge/parse"
)

func Analysis(song parse.ParsedSong, anal analysis.SongAnalysis) error {
	var details []string

	if len(anal.ReachableOrders) == 0 {
		details = append(details, "no reachable orders found")
	}

	for _, orderIdx := range anal.ReachableOrders {
		if orderIdx < 0 || orderIdx >= song.NumOrders {
			details = append(details, fmt.Sprintf("reachable order %d out of bounds (0-%d)",
				orderIdx, song.NumOrders-1))
		}
	}

	if len(anal.PatternAddrs) == 0 {
		details = append(details, "no reachable patterns found")
	}

	for addr := range anal.PatternAddrs {
		if _, exists := song.Patterns[addr]; !exists {
			details = append(details, fmt.Sprintf("pattern address $%04X marked reachable but not in song", addr))
		}
	}

	for _, inst := range anal.UsedInstruments {
		if inst <= 0 || inst >= len(song.Instruments) {
			details = append(details, fmt.Sprintf("used instrument %d out of bounds (1-%d)",
				inst, len(song.Instruments)-1))
		}
	}

	if len(details) > 0 {
		return NewError("analysis", "analysis produced invalid results", details...)
	}

	return nil
}
