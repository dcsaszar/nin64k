package pipeline

import (
	"fmt"

	"forge/analysis"
	"forge/parse"
	"forge/transform"
)

type TransposeResult struct {
	Equiv [9]transform.TransposeEquivResult
}

func FindTransposeEquiv(
	songNames []string,
	rawData [9][]byte,
	parsedSongs [9]parse.ParsedSong,
	analyses [9]analysis.SongAnalysis,
) TransposeResult {
	fmt.Println("\n=== Find transpose-equivalent patterns ===")
	var result TransposeResult

	for i, name := range songNames {
		if rawData[i] == nil {
			continue
		}
		result.Equiv[i] = transform.FindTransposeEquivalents(parsedSongs[i], analyses[i], rawData[i])

		numPatterns := len(analyses[i].PatternAddrs)
		numCanonical := countCanonical(result.Equiv[i].PatternRemap)
		merged := numPatterns - numCanonical

		if merged > 0 {
			fmt.Printf("  %s: %d patterns -> %d canonical (%d transpose-equiv)\n",
				name, numPatterns, numCanonical, merged)
		} else {
			fmt.Printf("  %s: %d patterns (no transpose-equiv)\n", name, numPatterns)
		}
	}

	return result
}

func countCanonical(remap map[uint16]uint16) int {
	unique := make(map[uint16]bool)
	for _, canonical := range remap {
		unique[canonical] = true
	}
	return len(unique)
}
