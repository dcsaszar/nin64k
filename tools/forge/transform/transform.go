package transform

import (
	"sort"

	"forge/analysis"
	"forge/parse"
)

type TransformedRow struct {
	Note   byte
	Inst   byte
	Effect byte
	Param  byte
}

type TransformedPattern struct {
	OriginalAddr uint16
	CanonicalIdx int
	Rows         []TransformedRow
	TruncateAt   int
}

type TransformedOrder struct {
	PatternIdx int
	Transpose  int8
}

type TransformedSong struct {
	Instruments    []parse.Instrument
	Patterns       []TransformedPattern
	Orders         [3][]TransformedOrder
	WaveTable      []byte
	ArpTable       []byte
	FilterTable    []byte
	EffectRemap    [16]byte
	FSubRemap      map[int]byte
	InstRemap      []int
	PatternRemap   map[uint16]uint16
	TransposeDelta map[uint16]int
	OrderMap       map[int]int
	MaxUsedSlot    int
	PatternOrder   []uint16
}

func Transform(song parse.ParsedSong, anal analysis.SongAnalysis, raw []byte) TransformedSong {
	effectRemap, fSubRemap := BuildGlobalEffectRemap([]analysis.SongAnalysis{anal})
	return TransformWithGlobalEffects(song, anal, raw, effectRemap, fSubRemap)
}

func TransformWithGlobalEffects(song parse.ParsedSong, anal analysis.SongAnalysis, raw []byte, effectRemap [16]byte, fSubRemap map[int]byte) TransformedSong {
	result := TransformedSong{
		PatternRemap:   make(map[uint16]uint16),
		TransposeDelta: make(map[uint16]int),
		OrderMap:       anal.OrderMap,
	}

	result.EffectRemap = effectRemap
	result.FSubRemap = fSubRemap
	numInst := len(song.Instruments)
	result.InstRemap, result.MaxUsedSlot = buildInstRemap(anal, numInst)

	canonicalPatterns, transposeDelta := findTransposeEquivalents(song, anal, raw)
	result.TransposeDelta = transposeDelta

	for addr, canonical := range canonicalPatterns {
		result.PatternRemap[addr] = canonical
	}

	// Collect unique canonical patterns and sort by address (matching odin_convert)
	uniqueCanonical := make(map[uint16]bool)
	for _, canonical := range canonicalPatterns {
		uniqueCanonical[canonical] = true
	}
	var sortedPatterns []uint16
	for addr := range uniqueCanonical {
		sortedPatterns = append(sortedPatterns, addr)
	}
	sort.Slice(sortedPatterns, func(i, j int) bool {
		return sortedPatterns[i] < sortedPatterns[j]
	})
	result.PatternOrder = sortedPatterns

	addrToIdx := make(map[uint16]int)
	for idx, addr := range sortedPatterns {
		addrToIdx[addr] = idx
	}

	for _, addr := range sortedPatterns {
		pat := song.Patterns[addr]
		truncateAt := 64
		if limit, ok := anal.TruncateLimits[addr]; ok && limit < truncateAt {
			truncateAt = limit
		}

		transformed := TransformedPattern{
			OriginalAddr: addr,
			CanonicalIdx: addrToIdx[addr],
			TruncateAt:   truncateAt,
			Rows:         make([]TransformedRow, 64),
		}

		for row := 0; row < 64; row++ {
			r := pat.Rows[row]
			newNote := r.Note
			if newNote == 0x7F {
				newNote = 0x61 // Map original key-off ($7F) to new format key-off ($61)
			}

			newB0, newB1, newParam := remapRowBytes(
				encodeB0(newNote, r.Effect), // Use remapped note
				encodeB1(r.Inst, r.Effect),
				r.Param,
				result.EffectRemap,
				result.FSubRemap,
				result.InstRemap,
			)

			transformed.Rows[row] = TransformedRow{
				Note:   newB0 & 0x7F,
				Inst:   newB1 & 0x1F,
				Effect: (newB1 >> 5) | ((newB0 >> 4) & 8),
				Param:  newParam,
			}
		}

		result.Patterns = append(result.Patterns, transformed)
	}

	for ch := 0; ch < 3; ch++ {
		for _, oldOrder := range anal.ReachableOrders {
			if oldOrder >= len(song.Orders[ch]) {
				continue
			}
			entry := song.Orders[ch][oldOrder]
			canonical := result.PatternRemap[entry.PatternAddr]
			delta := result.TransposeDelta[entry.PatternAddr]

			result.Orders[ch] = append(result.Orders[ch], TransformedOrder{
				PatternIdx: addrToIdx[canonical],
				Transpose:  int8(int(entry.Transpose) + delta),
			})
		}
	}

	result.WaveTable = song.WaveTable

	newArpTable, arpRemap, arpValid := deduplicateArpTable(song.Instruments, song.ArpTable)
	newFilterTable, filterRemap, filterValid := deduplicateFilterTable(song.Instruments, song.FilterTable)

	remappedInstruments := make([]parse.Instrument, len(song.Instruments))
	copy(remappedInstruments, song.Instruments)
	if arpRemap != nil {
		applyArpRemap(remappedInstruments, arpRemap, arpValid)
	}
	if filterRemap != nil {
		applyFilterRemap(remappedInstruments, filterRemap, filterValid)
	}

	result.Instruments = remapInstruments(remappedInstruments, result.InstRemap, result.MaxUsedSlot)
	result.ArpTable = newArpTable
	result.FilterTable = newFilterTable

	return result
}

func encodeB0(note, effect byte) byte {
	return (note & 0x7F) | ((effect & 8) << 4)
}

func encodeB1(inst, effect byte) byte {
	return (inst & 0x1F) | ((effect & 7) << 5)
}

func remapInstruments(instruments []parse.Instrument, instRemap []int, maxUsedSlot int) []parse.Instrument {
	if maxUsedSlot <= 0 {
		return nil
	}

	result := make([]parse.Instrument, maxUsedSlot+1)

	for oldIdx, newIdx := range instRemap {
		if newIdx > 0 && newIdx <= maxUsedSlot && oldIdx < len(instruments) {
			result[newIdx] = instruments[oldIdx]
		}
	}

	return result
}
