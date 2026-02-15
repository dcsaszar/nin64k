package transform

import "forge/parse"

type tableRange struct {
	start   int
	end     int
	loop    int
	content []byte
}

type groupKey struct {
	content string
	loop    int
}

func deduplicateArpTable(instruments []parse.Instrument, arpTable []byte) ([]byte, []int, []bool) {
	hasValidRange := make([]bool, len(instruments))

	if len(arpTable) == 0 {
		return arpTable, nil, hasValidRange
	}

	ranges := make([]tableRange, len(instruments))
	for i, inst := range instruments {
		start := int(inst.ArpStart)
		end := int(inst.ArpEnd)
		loop := int(inst.ArpLoop)

		if start >= 255 || end >= 255 || end < start {
			continue
		}

		minIdx, maxIdx := start, end
		if loop < 255 {
			if loop < minIdx {
				minIdx = loop
			}
			if loop > maxIdx {
				maxIdx = loop
			}
		}

		if maxIdx >= len(arpTable) {
			maxIdx = len(arpTable) - 1
		}
		if minIdx > maxIdx {
			continue
		}

		loopOff := loop - minIdx
		if loop >= 255 {
			loopOff = 255
		}

		ranges[i] = tableRange{
			start:   minIdx,
			end:     maxIdx,
			loop:    loopOff,
			content: arpTable[minIdx : maxIdx+1],
		}
		hasValidRange[i] = true
	}

	groups := make(map[groupKey][]int)
	for i, r := range ranges {
		if len(r.content) == 0 {
			continue
		}
		key := groupKey{string(r.content), r.loop}
		groups[key] = append(groups[key], i)
	}

	type sortedGroup struct {
		key   groupKey
		insts []int
	}
	var sorted []sortedGroup
	for key, insts := range groups {
		sorted = append(sorted, sortedGroup{key, insts})
	}
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			li, lj := len(sorted[i].key.content), len(sorted[j].key.content)
			if lj > li || (lj == li && sorted[j].key.content < sorted[i].key.content) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	var newTable []byte
	remap := make([]int, len(instruments))

	for _, sg := range sorted {
		content := []byte(sg.key.content)
		// Remap absolute note 127 ($FF) to note 103 ($E7) to shrink freq table
		for i := range content {
			if content[i] == 0xFF {
				content[i] = 0xE7 // $80 | 103
			}
		}
		pos := findInTable(newTable, content)
		if pos < 0 {
			pos = len(newTable)
			newTable = append(newTable, content...)
		}
		for _, inst := range sg.insts {
			remap[inst] = pos - ranges[inst].start
		}
	}

	return newTable, remap, hasValidRange
}

func deduplicateFilterTable(instruments []parse.Instrument, filterTable []byte) ([]byte, []int, []bool) {
	hasValidRange := make([]bool, len(instruments))

	if len(filterTable) == 0 {
		return filterTable, nil, hasValidRange
	}

	ranges := make([]tableRange, len(instruments))
	for i, inst := range instruments {
		start := int(inst.FilterStart)
		end := int(inst.FilterEnd)
		loop := int(inst.FilterLoop)

		if start >= 255 || end >= 255 || end < start {
			continue
		}

		minIdx, maxIdx := start, end
		if loop < 255 {
			if loop < minIdx {
				minIdx = loop
			}
			if loop > maxIdx {
				maxIdx = loop
			}
		}

		if maxIdx >= len(filterTable) {
			maxIdx = len(filterTable) - 1
		}
		if minIdx > maxIdx {
			continue
		}

		loopOff := loop - minIdx
		if loop >= 255 {
			loopOff = 255
		}

		ranges[i] = tableRange{
			start:   minIdx,
			end:     maxIdx,
			loop:    loopOff,
			content: filterTable[minIdx : maxIdx+1],
		}
		hasValidRange[i] = true
	}

	groups := make(map[groupKey][]int)
	for i, r := range ranges {
		if len(r.content) == 0 {
			continue
		}
		key := groupKey{string(r.content), r.loop}
		groups[key] = append(groups[key], i)
	}

	type sortedGroup struct {
		key   groupKey
		insts []int
	}
	var sorted []sortedGroup
	for key, insts := range groups {
		sorted = append(sorted, sortedGroup{key, insts})
	}
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			li, lj := len(sorted[i].key.content), len(sorted[j].key.content)
			if lj > li || (lj == li && sorted[j].key.content < sorted[i].key.content) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	newTable := []byte{0}
	remap := make([]int, len(instruments))

	for _, sg := range sorted {
		content := []byte(sg.key.content)
		pos := findInTable(newTable, content)
		if pos < 0 {
			pos = len(newTable)
			newTable = append(newTable, content...)
		}
		for _, inst := range sg.insts {
			remap[inst] = pos - ranges[inst].start
		}
	}

	return newTable, remap, hasValidRange
}

func findInTable(table, content []byte) int {
	if len(content) == 0 || len(table) < len(content) {
		return -1
	}
	for i := 0; i <= len(table)-len(content); i++ {
		match := true
		for j := 0; j < len(content); j++ {
			if table[i+j] != content[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func applyArpRemap(instruments []parse.Instrument, remap []int, hasValidRange []bool) {
	for i := range instruments {
		if instruments[i].ArpStart >= 255 {
			continue
		}

		if !hasValidRange[i] {
			instruments[i].ArpStart = 255
			instruments[i].ArpEnd = 255
			instruments[i].ArpLoop = 255
			continue
		}

		newStart := int(instruments[i].ArpStart) + remap[i]
		newEnd := int(instruments[i].ArpEnd) + remap[i]
		newLoop := int(instruments[i].ArpLoop)
		if newLoop < 255 {
			newLoop += remap[i]
		}
		if newStart >= 0 && newStart < 255 {
			instruments[i].ArpStart = byte(newStart)
		}
		if newEnd >= 0 && newEnd < 255 {
			instruments[i].ArpEnd = byte(newEnd)
		}
		if newLoop >= 0 && newLoop < 255 {
			instruments[i].ArpLoop = byte(newLoop)
		}
	}
}

func applyFilterRemap(instruments []parse.Instrument, remap []int, hasValidRange []bool) {
	for i := range instruments {
		if instruments[i].FilterStart >= 255 {
			continue
		}

		if !hasValidRange[i] {
			instruments[i].FilterStart = 255
			instruments[i].FilterEnd = 255
			instruments[i].FilterLoop = 255
			continue
		}

		newStart := int(instruments[i].FilterStart) + remap[i]
		newEnd := int(instruments[i].FilterEnd) + remap[i]
		newLoop := int(instruments[i].FilterLoop)
		if newLoop < 255 {
			newLoop += remap[i]
		}
		if newStart >= 0 && newStart < 255 {
			instruments[i].FilterStart = byte(newStart)
		}
		if newEnd >= 0 && newEnd < 255 {
			instruments[i].FilterEnd = byte(newEnd)
		}
		if newLoop >= 0 && newLoop < 255 {
			instruments[i].FilterLoop = byte(newLoop)
		}
	}
}
