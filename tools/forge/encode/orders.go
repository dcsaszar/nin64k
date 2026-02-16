package encode

import (
	"forge/transform"
)

func EncodeOrders(song transform.TransformedSong) ([3][]byte, [3][]byte, [3]byte) {
	return EncodeOrdersWithRemap(song, nil)
}

func EncodeOrdersWithRemap(song transform.TransformedSong, reorderMap []int) ([3][]byte, [3][]byte, [3]byte) {
	numOrders := len(song.Orders[0])
	if numOrders == 0 {
		return [3][]byte{}, [3][]byte{}, [3]byte{}
	}

	var transpose [3][]byte
	var trackptr [3][]byte
	var trackStarts [3]byte

	for ch := 0; ch < 3; ch++ {
		transpose[ch] = make([]byte, numOrders)
		trackptr[ch] = make([]byte, numOrders)
	}

	for ch := 0; ch < 3; ch++ {
		if len(song.Orders[ch]) > 0 {
			patIdx := song.Orders[ch][0].PatternIdx
			if reorderMap != nil && patIdx < len(reorderMap) {
				patIdx = reorderMap[patIdx]
			}
			trackStarts[ch] = byte(patIdx)
		}

		for i, order := range song.Orders[ch] {
			transpose[ch][i] = byte(order.Transpose)
			patIdx := order.PatternIdx
			if reorderMap != nil && patIdx < len(reorderMap) {
				patIdx = reorderMap[patIdx]
			}
			trackptr[ch][i] = byte(patIdx)
		}
	}

	return transpose, trackptr, trackStarts
}

func ComputeDeltaSet(trackptr [3][]byte, numOrders int) []int {
	seen := make(map[int]bool)

	for ch := 0; ch < 3; ch++ {
		limit := len(trackptr[ch])
		if numOrders > 0 && numOrders < limit {
			limit = numOrders
		}
		for i := 1; i < limit; i++ {
			prev := int(trackptr[ch][i-1])
			curr := int(trackptr[ch][i])
			d := curr - prev
			if d > 127 {
				d -= 256
			} else if d < -128 {
				d += 256
			}
			seen[d] = true
		}
	}

	result := make([]int, 0, len(seen))
	for d := range seen {
		result = append(result, d)
	}
	return result
}

func ComputeTransposeSet(transpose [3][]byte, numOrders int) []int8 {
	seen := make(map[int8]bool)

	for ch := 0; ch < 3; ch++ {
		for i := 0; i < len(transpose[ch]) && i < numOrders; i++ {
			seen[int8(transpose[ch][i])] = true
		}
	}

	result := make([]int8, 0, len(seen))
	for v := range seen {
		result = append(result, v)
	}
	return result
}

func PackOrderBitstream(numOrders int, transpose [3][]byte, trackptr [3][]byte) []byte {
	out := make([]byte, numOrders*4)
	for i := 0; i < numOrders; i++ {
		ch0Tr := transpose[0][i] & 0x0F
		ch1Tr := transpose[1][i] & 0x0F
		ch2Tr := transpose[2][i] & 0x0F
		ch0Tp := trackptr[0][i] & 0x1F
		ch1Tp := trackptr[1][i] & 0x1F
		ch2Tp := trackptr[2][i] & 0x1F

		out[i*4+0] = ch0Tr | (ch1Tr << 4)
		out[i*4+1] = ch2Tr | ((ch0Tp & 0x0F) << 4)
		out[i*4+2] = (ch0Tp >> 4) | (ch1Tp << 1) | ((ch2Tp & 0x03) << 6)
		out[i*4+3] = ch2Tp >> 2
	}
	return out
}
