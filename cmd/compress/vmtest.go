package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// MemoryValidator tracks which memory regions are valid for reading
// and detects invalid memory accesses during decompression.
type MemoryValidator struct {
	// Buffer state tracking
	buf1000Valid [bufferDist]bool // Which bytes in $1000-$6FFF are valid
	buf7000Valid [bufferDist]bool // Which bytes in $7000-$CFFF are valid

	// Current decompression state
	currentSong   int
	selfBuffer    uint16 // $1000 or $7000
	outputPos     uint16 // Current output position within buffer

	// Violation tracking
	violations    []string
}

func NewMemoryValidator() *MemoryValidator {
	return &MemoryValidator{}
}

// InitForSong sets up the validator for decompressing a specific song
func (v *MemoryValidator) InitForSong(song int, songs map[int][]byte) {
	v.currentSong = song
	v.violations = nil

	// Determine which buffer is self vs other
	if song%2 == 1 {
		v.selfBuffer = 0x1000
	} else {
		v.selfBuffer = 0x7000
	}
	v.outputPos = 0

	// Set up buffer validity based on what's been decompressed so far
	// Songs are decompressed in order: 1,2,3,4,5,6,7,8,9
	// Buffer $1000: songs 1,3,5,7,9 (odd)
	// Buffer $7000: songs 2,4,6,8 (even)

	// Clear both buffers first
	for i := range v.buf1000Valid {
		v.buf1000Valid[i] = false
	}
	for i := range v.buf7000Valid {
		v.buf7000Valid[i] = false
	}

	// Song 1: no previous data
	if song == 1 {
		return
	}

	// Song 2: self=$7000 (empty), other=$1000 (S1)
	if song == 2 {
		for i := 0; i < len(songs[1]) && i < bufferDist; i++ {
			v.buf1000Valid[i] = true
		}
		// Protect scratch in other buffer (S1 was played, scratch corrupted)
		v.protectScratch(v.buf1000Valid[:])
		// Self buffer ($7000) is empty - no scratch to protect
		return
	}

	// Songs 3-9: both buffers have data from previous songs
	// Track HIGH WATER MARK (max bytes ever written to each buffer)
	var hwm1000, hwm7000 int
	for s := 1; s < song; s++ {
		songLen := len(songs[s])
		if s%2 == 1 {
			if songLen > hwm1000 {
				hwm1000 = songLen
			}
		} else {
			if songLen > hwm7000 {
				hwm7000 = songLen
			}
		}
	}

	// Set validity for $1000 buffer up to high water mark
	for i := 0; i < hwm1000 && i < bufferDist; i++ {
		v.buf1000Valid[i] = true
	}

	// Set validity for $7000 buffer up to high water mark
	for i := 0; i < hwm7000 && i < bufferDist; i++ {
		v.buf7000Valid[i] = true
	}

	// Protect scratch regions in BOTH buffers
	// Self buffer scratch could be read via fwdref before being overwritten
	// Other buffer scratch was corrupted by playroutine
	v.protectScratch(v.buf1000Valid[:])
	v.protectScratch(v.buf7000Valid[:])
}

// protectScratch marks scratch regions as invalid
func (v *MemoryValidator) protectScratch(valid []bool) {
	// Scratch regions (offsets relative to buffer base):
	// $0115-$0116 (2 bytes)
	// $081E-$088C (111 bytes)
	for i := 0x0115; i <= 0x0116 && i < len(valid); i++ {
		valid[i] = false
	}
	for i := 0x081E; i <= 0x088C && i < len(valid); i++ {
		valid[i] = false
	}
}

// MarkWritten marks a byte as written to output
func (v *MemoryValidator) MarkWritten(addr uint16) {
	if addr >= 0x1000 && addr < 0x1000+bufferDist {
		v.buf1000Valid[addr-0x1000] = true
	} else if addr >= 0x7000 && addr < 0x7000+bufferDist {
		v.buf7000Valid[addr-0x7000] = true
	}
	// Track output position
	if addr >= v.selfBuffer && addr < v.selfBuffer+bufferDist {
		offset := addr - v.selfBuffer
		if offset >= v.outputPos {
			v.outputPos = offset + 1
		}
	}
}

// ValidateRead checks if reading from addr is valid during copy operations
func (v *MemoryValidator) ValidateRead(addr uint16) bool {
	// Only validate reads from the decompression buffers
	if addr < 0x1000 || addr >= 0xD000 {
		return true // Not a buffer read
	}

	var valid bool
	var reason string

	if addr >= 0x1000 && addr < 0x1000+bufferDist {
		offset := int(addr - 0x1000)
		if v.selfBuffer == 0x1000 {
			// Reading from self buffer ($1000)
			// Valid if: already written (backref) OR initialized from prev song (fwdref)
			valid = v.buf1000Valid[offset]
			if !valid {
				reason = fmt.Sprintf("self buffer offset $%04X not initialized", offset)
			}
		} else {
			// Reading from other buffer ($1000) - must be initialized and not scratch
			valid = v.buf1000Valid[offset]
			if !valid {
				reason = fmt.Sprintf("other buffer ($1000) offset $%04X invalid/scratch", offset)
			}
		}
	} else if addr >= 0x7000 && addr < 0x7000+bufferDist {
		offset := int(addr - 0x7000)
		if v.selfBuffer == 0x7000 {
			// Reading from self buffer ($7000)
			valid = v.buf7000Valid[offset]
			if !valid {
				reason = fmt.Sprintf("self buffer offset $%04X not initialized", offset)
			}
		} else {
			// Reading from other buffer ($7000)
			valid = v.buf7000Valid[offset]
			if !valid {
				reason = fmt.Sprintf("other buffer ($7000) offset $%04X invalid/scratch", offset)
			}
		}
	}

	if !valid {
		v.violations = append(v.violations,
			fmt.Sprintf("Song %d: invalid read from $%04X (%s)", v.currentSong, addr, reason))
	}

	return valid
}

// HasViolations returns true if any memory access violations occurred
func (v *MemoryValidator) HasViolations() bool {
	return len(v.violations) > 0
}

// Violations returns all recorded violations
func (v *MemoryValidator) Violations() []string {
	return v.violations
}

func testDecompressor() error {
	fmt.Println("6502 Decompressor Test")
	fmt.Println("======================")

	// Load expected song data (new format parts)
	songs := make(map[int][]byte)
	for i := 1; i <= 9; i++ {
		data, err := os.ReadFile(filepath.Join("generated", "parts", fmt.Sprintf("part%d.bin", i)))
		if err != nil {
			return fmt.Errorf("loading part %d: %w", i, err)
		}
		songs[i] = data
	}

	// Load single stream file
	stream, err := os.ReadFile(filepath.Join("generated", "stream.bin"))
	if err != nil {
		return fmt.Errorf("loading stream.bin: %w\n(run compressor first: go run ./cmd/compress)", err)
	}

	// Get decompressor code
	decompCode := GetDecompressorCode()
	fmt.Printf("Decompressor size: %d bytes\n\n", len(decompCode))

	fmt.Println("Single Stream Test")
	fmt.Println("------------------")
	fmt.Printf("Stream: %d bytes\n", len(stream))

	// Memory layout:
	// - Stream in high memory ending at $FFFF
	// - Buffer A (odd songs): $1800-$62FF
	// - Buffer B (even songs): $6300-$ADFF
	streamStart := 0x10000 - len(stream)

	fmt.Printf("Layout: stream=$%04X-$%04X, bufA=$%04X, bufB=$%04X\n\n",
		streamStart, 0xFFFF, addrLow, addrHigh)

	cpu := NewCPU6502()
	cpu.LoadAt(0x0D00, decompCode)
	cpu.LoadAt(uint16(streamStart), stream)

	cpu.Mem[zpSrcLo] = byte(streamStart)
	cpu.Mem[zpSrcHi] = byte(streamStart >> 8)
	cpu.Mem[zpBitBuf] = 0x80
	cpu.Mem[0x0CFF] = 0x00

	allPassed := true
	var totalCycles uint64

	// Decompress all 9 songs from single stream
	for song := 1; song <= 9; song++ {
		target := songs[song]

		var dstAddr uint16
		if song%2 == 1 {
			dstAddr = addrLow
		} else {
			dstAddr = addrHigh
		}
		cpu.Mem[zpOutLo] = byte(dstAddr)
		cpu.Mem[zpOutHi] = byte(dstAddr >> 8)

		cpu.Mem[0x01FF] = 0x0C
		cpu.Mem[0x01FE] = 0xFE
		cpu.SP = 0xFD
		cpu.PC = 0x0D00
		cpu.Halted = false
		cpu.Cycles = 0

		err := cpu.Run(2000000)
		if err != nil {
			fmt.Printf("Song %d: RUNTIME ERROR: %v\n", song, err)
			allPassed = false
			continue
		}
		if !cpu.Halted {
			fmt.Printf("Song %d: TIMEOUT\n", song)
			allPassed = false
			continue
		}

		output := cpu.Mem[dstAddr : dstAddr+uint16(len(target))]
		if bytes.Equal(output, target) {
			srcPos := uint16(cpu.Mem[zpSrcLo]) | uint16(cpu.Mem[zpSrcHi])<<8
			fmt.Printf("Song %d: PASS (%d bytes, %d cycles) [src=$%04X]\n",
				song, len(target), cpu.Cycles, srcPos)
			totalCycles += cpu.Cycles
		} else {
			firstDiff := -1
			for i := range target {
				if output[i] != target[i] {
					firstDiff = i
					break
				}
			}
			fmt.Printf("Song %d: FAIL at offset %d (got $%02X, want $%02X)\n",
				song, firstDiff, output[firstDiff], target[firstDiff])
			allPassed = false
		}
	}

	fmt.Printf("\nTotal cycles: %d\n", totalCycles)

	if allPassed {
		fmt.Println("\nAll tests PASSED!")
	}

	if !allPassed {
		return fmt.Errorf("some tests failed")
	}
	return nil
}

func vmTestMain() {
	if err := testDecompressor(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// CycleStats holds cycle analysis results from the VM
type CycleStats struct {
	LowestMaxGapOffset int    // Offset from $0D00 of instruction with lowest max cycle gap
	MaxCycleGap        uint64 // Max cycles between revisits for that instruction
}

// RunDecompressorForCycleStats runs the decompressor and returns cycle statistics
func RunDecompressorForCycleStats(songs map[int][]byte, stream, _ []byte) (*CycleStats, error) {
	decompCode := GetDecompressorCode()

	streamStart := 0x10000 - len(stream)

	cpu := NewCPU6502()
	cpu.LoadAt(0x0D00, decompCode)
	cpu.LoadAt(uint16(streamStart), stream)

	cpu.Mem[zpSrcLo] = byte(streamStart)
	cpu.Mem[zpSrcHi] = byte(streamStart >> 8)
	cpu.Mem[zpBitBuf] = 0x80
	cpu.Mem[0x0CFF] = 0x00

	// Decompress all 9 songs from single stream
	for song := 1; song <= 9; song++ {
		target := songs[song]

		var dstAddr uint16
		if song%2 == 1 {
			dstAddr = addrLow
		} else {
			dstAddr = addrHigh
		}
		cpu.Mem[zpOutLo] = byte(dstAddr)
		cpu.Mem[zpOutHi] = byte(dstAddr >> 8)

		cpu.Mem[0x01FF] = 0x0C
		cpu.Mem[0x01FE] = 0xFE
		cpu.SP = 0xFD
		cpu.PC = 0x0D00
		cpu.Halted = false
		startCycles := cpu.Cycles

		if err := cpu.Run(startCycles + 2000000); err != nil {
			return nil, fmt.Errorf("song %d: %w", song, err)
		}
		if !cpu.Halted {
			return nil, fmt.Errorf("song %d: timeout", song)
		}

		output := cpu.Mem[dstAddr : dstAddr+uint16(len(target))]
		if !bytes.Equal(output, target) {
			return nil, fmt.Errorf("song %d: output mismatch", song)
		}
	}

	offset, maxGap := cpu.LowestMaxCycleGapPC(0x0D00)
	return &CycleStats{
		LowestMaxGapOffset: offset,
		MaxCycleGap:        maxGap,
	}, nil
}
