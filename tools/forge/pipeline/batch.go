package pipeline

var SongNames = []string{
	"d1p", "d2p", "d3p", "d4p", "d5p", "d6p", "d7p", "d8p", "d9p",
}

func RunBatch(cfg *Config) {
	// Load and analyze
	rawData, parsedSongs, analyses := LoadAndAnalyze(cfg, SongNames)

	// Build remap and transform
	remap := BuildRemapAndTransform(cfg, SongNames, rawData, parsedSongs, analyses)

	// Pack and encode
	songs, _ := PackAndEncode(SongNames, rawData, parsedSongs, analyses, remap.Transformed)

	// Solve global tables
	tables := SolveTables(songs)

	// Serialize and write output
	outputs := SerializeAndWrite(cfg, songs, tables)

	// Run validation
	RunValidation(cfg, songs, outputs, tables, remap.EffectRemap, remap.FSubRemap, remap.TransformOpts)
}
