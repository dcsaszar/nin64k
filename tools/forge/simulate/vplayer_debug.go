package simulate

import "fmt"

// CompareVirtual compares MinimalPlayer output against original GT player output.
func CompareVirtual(
	origWrites []SIDWrite,
	songData []byte,
	deltaTable, transposeTable, waveTable []byte,
	numPatterns int,
	frames int,
	startConst int,
) (bool, int, string) {
	return CompareVirtualWithDebug(origWrites, songData, deltaTable, transposeTable, waveTable, numPatterns, frames, startConst, false)
}

func CompareVirtualWithDebug(
	origWrites []SIDWrite,
	songData []byte,
	deltaTable, transposeTable, waveTable []byte,
	numPatterns int,
	frames int,
	startConst int,
	debugMode bool,
) (bool, int, string) {
	mp := NewMinimalPlayerWithDebug(songData, numPatterns, deltaTable, transposeTable, waveTable, startConst, debugMode)

	writeIdx := 0
	for i := 0; i < frames; i++ {
		mpFrame := mp.Tick()

		for _, w := range mpFrame {
			if writeIdx >= len(origWrites) {
				return false, writeIdx, fmt.Sprintf("frame %d: extra write $%04X=%02X", i, w.Addr, w.Value)
			}
			if origWrites[writeIdx] != w {
				return false, writeIdx, fmt.Sprintf("frame %d: orig=$%04X=%02X, got=$%04X=%02X",
					i, origWrites[writeIdx].Addr, origWrites[writeIdx].Value, w.Addr, w.Value)
			}
			writeIdx++
		}
	}

	return true, writeIdx, ""
}
