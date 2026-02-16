package pipeline

import (
	"fmt"
	"os"

	"forge/analysis"
	"forge/parse"
	"forge/verify"
)

func LoadAndAnalyze(cfg *Config, songNames []string) (
	[9][]byte,
	[9]parse.ParsedSong,
	[9]analysis.SongAnalysis,
) {
	var rawData [9][]byte
	var parsedSongs [9]parse.ParsedSong
	var analyses [9]analysis.SongAnalysis

	fmt.Println("=== Parse and analyze all songs (frame-based) ===")
	for i, name := range songNames {
		inputPath := cfg.ProjectPath(fmt.Sprintf("uncompressed/%s.raw", name))
		raw, err := os.ReadFile(inputPath)
		if err != nil {
			fmt.Printf("  %s: skipped (not found)\n", name)
			continue
		}

		song := parse.Parse(raw)
		if err := verify.Parse(song); err != nil {
			fmt.Printf("FATAL: %s parse verification failed:\n%v\n", name, err)
			os.Exit(1)
		}

		// Hybrid approach:
		// 1. Get max order from frame-based simulation (determines where to stop)
		// 2. Use static analysis for order sequence (no briefly-played orders)
		// 3. Limit static orders to max reached
		testFrames := cfg.PartTimes[i]
		maxOrder, _ := analysis.CountMaxOrderGT(song, testFrames)

		// Get static analysis (full order sequence following jumps)
		fullAnal := analysis.Analyze(song, raw)

		// Filter to only orders <= maxOrder
		var limitedOrders []int
		for _, ord := range fullAnal.ReachableOrders {
			if ord <= maxOrder {
				limitedOrders = append(limitedOrders, ord)
			}
		}

		// Re-analyze with limited orders
		anal := analysis.AnalyzeWithOrders(song, raw, limitedOrders)
		if err := verify.Analysis(song, anal); err != nil {
			fmt.Printf("FATAL: %s analysis verification failed:\n%v\n", name, err)
			os.Exit(1)
		}

		// Calculate dropped orders and patterns
		droppedOrders := len(fullAnal.ReachableOrders) - len(limitedOrders)
		droppedPatterns := len(fullAnal.PatternAddrs) - len(anal.PatternAddrs)

		if droppedOrders > 0 || droppedPatterns > 0 {
			fmt.Printf("  %s: %d bytes, %d frames -> %d orders, %d patterns (dropped %d orders, %d patterns)\n",
				name, len(raw), testFrames, len(limitedOrders), len(anal.PatternAddrs), droppedOrders, droppedPatterns)
		} else {
			fmt.Printf("  %s: %d bytes, %d frames -> %d orders, %d patterns\n",
				name, len(raw), testFrames, len(limitedOrders), len(anal.PatternAddrs))
		}

		rawData[i] = raw
		parsedSongs[i] = song
		analyses[i] = anal
	}

	return rawData, parsedSongs, analyses
}
