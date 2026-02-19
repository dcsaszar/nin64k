package simulate

import (
	"fmt"
	"sort"
	"strings"
)

type ASMStats struct {
	Coverage        map[uint16]bool
	DataCoverage    map[uint16]bool
	DataBase        uint16
	DataSize        int
	RedundantCLC    map[uint16]int
	RedundantSEC    map[uint16]int
	TotalCLC        map[uint16]int
	TotalSEC        map[uint16]int
	CheckpointGap   uint64
	CheckpointFrom  uint16
	CheckpointTo    uint16
	TotalCycles     uint64
	MaxFrameCycles  uint64
	OrigCycles      uint64
	OrigMaxCycles   uint64
	OrigSize        int
	NewSize         int
	DictSize        int
	SongLength      int
}

func GetOriginalWrites(rawData []byte, songNum int, testFrames int) []SIDWrite {
	var bufferBase uint16
	if songNum%2 == 1 {
		bufferBase = 0x1000
	} else {
		bufferBase = 0x7000
	}
	playAddr := bufferBase + 3

	cpu := NewCPU()
	copy(cpu.Memory[bufferBase:], rawData)
	cpu.A = 0
	cpu.Call(bufferBase)
	return cpu.RunFrames(playAddr, testFrames)
}

func ReportASMStats(allStats []*ASMStats, playerData []byte) bool {
	playerBase := uint16(0xF000)

	mergedCoverage := make(map[uint16]bool)
	mergedDataCoverage := make(map[int]bool)
	mergedRedundantCLC := make(map[uint16]int)
	mergedRedundantSEC := make(map[uint16]int)
	mergedTotalCLC := make(map[uint16]int)
	mergedTotalSEC := make(map[uint16]int)
	var worstGap uint64
	var worstGapFrom, worstGapTo uint16

	for _, stats := range allStats {
		if stats == nil {
			continue
		}
		for addr := range stats.Coverage {
			mergedCoverage[addr] = true
		}
		for addr := range stats.DataCoverage {
			mergedDataCoverage[int(addr-stats.DataBase)] = true
		}
		for addr, count := range stats.RedundantCLC {
			mergedRedundantCLC[addr] += count
		}
		for addr, count := range stats.RedundantSEC {
			mergedRedundantSEC[addr] += count
		}
		for addr, count := range stats.TotalCLC {
			mergedTotalCLC[addr] += count
		}
		for addr, count := range stats.TotalSEC {
			mergedTotalSEC[addr] += count
		}
		if stats.CheckpointGap > worstGap {
			worstGap = stats.CheckpointGap
			worstGapFrom = stats.CheckpointFrom
			worstGapTo = stats.CheckpointTo
		}
	}

	instrStarts := FindInstructionStarts(playerData, playerBase)
	var uncovered []uint16
	for _, addr := range instrStarts {
		if !mergedCoverage[addr] {
			uncovered = append(uncovered, addr)
		}
	}
	fmt.Printf("\nCode coverage: %d/%d instructions executed\n", len(instrStarts)-len(uncovered), len(instrStarts))
	if len(uncovered) > 0 {
		fmt.Printf("Uncovered instructions (%d):", len(uncovered))
		for i, addr := range uncovered {
			if i%16 == 0 {
				fmt.Printf("\n ")
			}
			fmt.Printf(" $%04X", addr)
		}
		fmt.Println()
	}

	codeEnd := len(instrStarts)
	dataStart := 0
	if codeEnd > 0 {
		lastInstr := instrStarts[codeEnd-1]
		lastLen := 1
		if int(lastInstr-playerBase) < len(playerData) {
			opcode := playerData[lastInstr-playerBase]
			if l, ok := InstrLengths()[opcode]; ok {
				lastLen = l
			}
		}
		dataStart = int(lastInstr-playerBase) + lastLen
	}
	dataEnd := len(playerData)
	dataCovered := 0
	for off := dataStart; off < dataEnd; off++ {
		if mergedDataCoverage[off] {
			dataCovered++
		}
	}
	dataTotal := dataEnd - dataStart
	if dataTotal > 0 {
		fmt.Printf("Data coverage: %d/%d bytes (%.0f%%)\n", dataCovered, dataTotal, 100*float64(dataCovered)/float64(dataTotal))
	}

	var uncoveredData []string
	for off := dataStart; off < dataEnd; off++ {
		if !mergedDataCoverage[off] {
			uncoveredData = append(uncoveredData, fmt.Sprintf("%d:$%02X", off-dataStart, playerData[off]))
		}
	}
	if len(uncoveredData) > 0 && len(uncoveredData) <= 30 {
		fmt.Printf("Uncovered data: %s\n", strings.Join(uncoveredData, ", "))
	}

	type flagOp struct {
		addr      uint16
		redundant int
		total     int
		isCLC     bool
	}
	var redundantOps []flagOp
	for addr, count := range mergedRedundantCLC {
		if count == mergedTotalCLC[addr] && count > 0 {
			redundantOps = append(redundantOps, flagOp{addr, count, mergedTotalCLC[addr], true})
		}
	}
	for addr, count := range mergedRedundantSEC {
		if count == mergedTotalSEC[addr] && count > 0 {
			redundantOps = append(redundantOps, flagOp{addr, count, mergedTotalSEC[addr], false})
		}
	}
	if len(redundantOps) > 0 {
		sort.Slice(redundantOps, func(i, j int) bool { return redundantOps[i].addr < redundantOps[j].addr })
		var clcAddrs, secAddrs []string
		for _, op := range redundantOps {
			if op.isCLC {
				clcAddrs = append(clcAddrs, fmt.Sprintf("$%04X", op.addr))
			} else {
				secAddrs = append(secAddrs, fmt.Sprintf("$%04X", op.addr))
			}
		}
		fmt.Printf("\nRedundant flags: %d CLC %v, %d SEC %v\n", len(clcAddrs), clcAddrs, len(secAddrs), secAddrs)
	}

	if worstGap > 0 {
		fmt.Printf("\nSlowest checkpoint: %d cycles (from $%04X to $%04X)\n", worstGap, worstGapFrom, worstGapTo)
	}

	// Return true only if 100% code coverage achieved
	return len(uncovered) == 0
}
