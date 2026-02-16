package pipeline

import (
	"forge/analysis"
	"forge/encode"
	"forge/parse"
	"forge/transform"
)

type ProcessedSong struct {
	Name        string
	Raw         []byte
	Song        parse.ParsedSong
	Anal        analysis.SongAnalysis
	Transformed transform.TransformedSong
	Encoded     encode.EncodedSong
}

type EncodeState struct {
	Patterns       [][]byte
	TruncateLimits []int
	ReorderMap     []int
	Dict           []byte
	RowToIdx       map[string]int
	NoteOnlyRows   map[string]bool
	CanonPatterns  [][]byte
	CanonGapCodes  []byte
	PatternToCanon []int
	PackedPatterns []byte
	PatternOffsets []uint16
	PrimaryCount   int
	ExtendedCount  int
}
