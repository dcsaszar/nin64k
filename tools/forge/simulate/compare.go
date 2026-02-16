package simulate

import "bytes"

func SerializeWrites(writes []SIDWrite) []byte {
	out := make([]byte, len(writes)*3)
	for i, w := range writes {
		out[i*3] = byte(w.Addr)
		out[i*3+1] = byte(w.Addr >> 8)
		out[i*3+2] = w.Value
	}
	return out
}

func CompareRuns(origWrites, newWrites []SIDWrite) bool {
	return bytes.Equal(SerializeWrites(origWrites), SerializeWrites(newWrites))
}
