package solve

import (
	"bytes"
	"fmt"
	"sort"
)

type WaveSnippet struct {
	Content    []byte
	LoopOffset int
}

type GlobalWaveTable struct {
	Data     []byte
	Snippets map[string]int
	Remap    map[int]map[int][3]int
}

type WaveInstrumentInfo struct {
	Start int
	End   int
	Loop  int
}

func CollectWaveSnippets(songWaveTables [][]byte, songInstruments [][]WaveInstrumentInfo) map[string]WaveSnippet {
	snippets := make(map[string]WaveSnippet)

	for songNum, waveTable := range songWaveTables {
		if waveTable == nil || songNum >= len(songInstruments) {
			continue
		}

		for _, info := range songInstruments[songNum] {
			if info.Start >= 255 || info.End >= 255 || info.End < info.Start {
				continue
			}

			minIdx, maxIdx := info.Start, info.End
			if info.Loop < minIdx {
				minIdx = info.Loop
			}
			if info.Loop > maxIdx {
				maxIdx = info.Loop
			}

			if minIdx >= 0 && maxIdx < len(waveTable) {
				content := waveTable[minIdx : maxIdx+1]
				key := string(content)
				if _, exists := snippets[key]; !exists {
					snippets[key] = WaveSnippet{
						Content:    append([]byte{}, content...),
						LoopOffset: info.Loop - minIdx,
					}
				}
			}
		}
	}
	return snippets
}

func BuildGlobalWaveTable(songWaveTables [][]byte, songInstruments [][]WaveInstrumentInfo) *GlobalWaveTable {
	snippets := CollectWaveSnippets(songWaveTables, songInstruments)

	var contents [][]byte
	for _, snip := range snippets {
		contents = append(contents, snip.Content)
	}

	sort.Slice(contents, func(i, j int) bool {
		if len(contents[i]) != len(contents[j]) {
			return len(contents[i]) > len(contents[j])
		}
		return string(contents[i]) < string(contents[j])
	})

	var filtered [][]byte
	for i, s := range contents {
		isSubstring := false
		for j, t := range contents {
			if i != j && len(t) >= len(s) && bytes.Contains(t, s) {
				isSubstring = true
				break
			}
		}
		if !isSubstring {
			filtered = append(filtered, s)
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		if len(filtered[i]) != len(filtered[j]) {
			return len(filtered[i]) > len(filtered[j])
		}
		return string(filtered[i]) < string(filtered[j])
	})

	current := filtered
	for len(current) > 1 {
		bestI, bestJ := 0, 1
		bestOverlap := 0
		var bestMerged []byte

		for i := 0; i < len(current); i++ {
			for j := 0; j < len(current); j++ {
				if i == j {
					continue
				}
				maxOv := len(current[i])
				if len(current[j]) < maxOv {
					maxOv = len(current[j])
				}
				for ov := maxOv; ov > 0; ov-- {
					if bytes.Equal(current[i][len(current[i])-ov:], current[j][:ov]) {
						if ov > bestOverlap {
							bestOverlap = ov
							bestI, bestJ = i, j
							bestMerged = append(append([]byte{}, current[i]...), current[j][ov:]...)
						}
						break
					}
				}
			}
		}

		if bestMerged == nil {
			bestMerged = append(append([]byte{}, current[0]...), current[1]...)
			bestI, bestJ = 0, 1
		}

		var next [][]byte
		for k := range current {
			if k != bestI && k != bestJ {
				next = append(next, current[k])
			}
		}
		next = append(next, bestMerged)
		current = next
	}

	var combined []byte
	if len(current) > 0 {
		combined = current[0]
	}

	snippetOffsets := make(map[string]int)
	for content := range snippets {
		idx := bytes.Index(combined, []byte(content))
		snippetOffsets[content] = idx
	}

	remap := make(map[int]map[int][3]int)
	for songNum, waveTable := range songWaveTables {
		if waveTable == nil || songNum >= len(songInstruments) {
			continue
		}
		remap[songNum] = make(map[int][3]int)

		for instNum, info := range songInstruments[songNum] {
			if info.Start >= 255 || info.End >= 255 || info.End < info.Start {
				remap[songNum][instNum] = [3]int{255, 255, 255}
				continue
			}

			minIdx, maxIdx := info.Start, info.End
			if info.Loop < minIdx {
				minIdx = info.Loop
			}
			if info.Loop > maxIdx {
				maxIdx = info.Loop
			}

			if minIdx >= 0 && maxIdx < len(waveTable) {
				content := string(waveTable[minIdx : maxIdx+1])
				globalOffset := snippetOffsets[content]

				newStart := globalOffset + (info.Start - minIdx)
				newEnd := globalOffset + (info.End - minIdx)
				newLoop := globalOffset + (info.Loop - minIdx)
				remap[songNum][instNum] = [3]int{newStart, newEnd, newLoop}
			} else {
				remap[songNum][instNum] = [3]int{255, 255, 255}
			}
		}
	}

	result := &GlobalWaveTable{
		Data:     combined,
		Snippets: snippetOffsets,
		Remap:    remap,
	}

	if err := ValidateWaveRemap(result, songWaveTables, songInstruments); err != nil {
		panic(err)
	}

	return result
}

func ValidateWaveRemap(gwt *GlobalWaveTable, songWaveTables [][]byte, songInstruments [][]WaveInstrumentInfo) error {
	for songNum, waveTable := range songWaveTables {
		if waveTable == nil || songNum >= len(songInstruments) {
			continue
		}

		songRemap, ok := gwt.Remap[songNum]
		if !ok {
			continue
		}

		for instNum, info := range songInstruments[songNum] {
			if info.Start >= 255 || info.End >= 255 || info.End < info.Start {
				continue
			}

			remap, ok := songRemap[instNum]
			if !ok {
				return fmt.Errorf("song %d inst %d: missing remap", songNum, instNum)
			}

			if remap[0] == 255 {
				continue
			}

			for i := info.Start; i <= info.End; i++ {
				origByte := waveTable[i]
				remapIdx := remap[0] + (i - info.Start)
				if remapIdx < 0 || remapIdx >= len(gwt.Data) {
					return fmt.Errorf("song %d inst %d: remap index %d out of bounds (global len %d)",
						songNum, instNum, remapIdx, len(gwt.Data))
				}
				globalByte := gwt.Data[remapIdx]
				if origByte != globalByte {
					return fmt.Errorf("song %d inst %d: wave mismatch at offset %d: orig $%02X != global $%02X (remap start=%d)",
						songNum, instNum, i, origByte, globalByte, remap[0])
				}
			}
		}
	}

	return nil
}
