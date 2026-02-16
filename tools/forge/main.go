package main

import (
	"fmt"
	"os"
	"path/filepath"

	"forge/pipeline"
)

func main() {
	cfg := &pipeline.Config{
		ProjectRoot: pipeline.FindProjectRoot(),
		OutputDir:   filepath.Join(pipeline.FindProjectRoot(), "generated", "parts"),
		PartTimes:   pipeline.DefaultPartTimes,
	}
	if len(os.Args) > 1 {
		cfg.OutputDir = os.Args[1]
	}
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}
	pipeline.RunBatch(cfg)
}
