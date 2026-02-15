package verify

import (
	"fmt"
	"forge/encode"
	"forge/serialize"
	"forge/transform"
)

func SerializeWithWaveRemap(
	transformed transform.TransformedSong,
	encoded encode.EncodedSong,
	output []byte,
	waveRemap map[int][3]int,
) error {
	if err := Serialize(transformed, encoded, output); err != nil {
		return err
	}

	if waveRemap == nil {
		return nil
	}

	var details []string

	for slot := 1; slot <= transformed.MaxUsedSlot && slot < len(transformed.Instruments); slot++ {
		inst := transformed.Instruments[slot]
		base := serialize.InstOffset + (slot-1)*16

		if base+15 >= len(output) {
			continue
		}

		remap, hasRemap := waveRemap[slot]

		expectedWaveStart := inst.WaveStart
		expectedWaveEnd := inst.WaveEnd
		expectedWaveLoop := inst.WaveLoop

		if hasRemap {
			expectedWaveStart = byte(remap[0])
			expectedWaveEnd = byte(remap[1])
			expectedWaveLoop = byte(remap[2])
		}

		if output[base+2] != expectedWaveStart {
			details = append(details, fmt.Sprintf("inst slot %d: WaveStart mismatch: got $%02X, want $%02X",
				slot, output[base+2], expectedWaveStart))
		}

		expectedWaveEndPlusOne := expectedWaveEnd
		if expectedWaveEndPlusOne < 255 {
			expectedWaveEndPlusOne++
		}
		if output[base+3] != expectedWaveEndPlusOne {
			details = append(details, fmt.Sprintf("inst slot %d: WaveEnd mismatch: got $%02X, want $%02X (+1 transform)",
				slot, output[base+3], expectedWaveEndPlusOne))
		}

		if output[base+4] != expectedWaveLoop {
			details = append(details, fmt.Sprintf("inst slot %d: WaveLoop mismatch: got $%02X, want $%02X",
				slot, output[base+4], expectedWaveLoop))
		}
	}

	if len(details) > 0 {
		return NewError("serialize/wave", "wave remap verification failed", details...)
	}

	return nil
}

func Serialize(
	transformed transform.TransformedSong,
	encoded encode.EncodedSong,
	output []byte,
) error {
	var details []string

	if len(output) == 0 {
		details = append(details, "output is empty")
	}

	if len(output) > serialize.MaxOutputSize {
		details = append(details, fmt.Sprintf("output size %d exceeds limit %d",
			len(output), serialize.MaxOutputSize))
	}

	numPatterns := len(encoded.PatternOffsets)
	patternDataStart := serialize.PackedPtrsOffset + numPatterns*2

	for i := 0; i < numPatterns; i++ {
		if serialize.PackedPtrsOffset+i*2+1 >= len(output) {
			details = append(details, fmt.Sprintf("pattern pointer %d out of bounds", i))
			continue
		}

		lo := output[serialize.PackedPtrsOffset+i*2]
		hi := output[serialize.PackedPtrsOffset+i*2+1] & 0x1F
		ptr := int(lo) | (int(hi) << 8)

		if ptr >= len(output) {
			details = append(details, fmt.Sprintf("pattern %d pointer $%04X out of bounds (output len %d)",
				i, ptr, len(output)))
		}

		if ptr < patternDataStart && ptr >= serialize.PackedPtrsOffset {
			if ptr >= serialize.RowDictOffset && ptr < serialize.PackedPtrsOffset {
				continue
			}
			if ptr >= serialize.InstOffset && ptr < serialize.BitstreamOffset {
				continue
			}
			if ptr >= serialize.BitstreamOffset && ptr < serialize.FilterOffset {
				continue
			}
			if ptr >= serialize.FilterOffset && ptr < serialize.ArpOffset {
				continue
			}
			if ptr >= serialize.ArpOffset && ptr < serialize.TransBaseOffset {
				continue
			}
		}
	}

	instEnd := serialize.InstOffset + transformed.MaxUsedSlot*16
	if instEnd > serialize.BitstreamOffset {
		details = append(details, fmt.Sprintf("instrument data (%d bytes) overflows into bitstream area",
			transformed.MaxUsedSlot*16))
	}

	filterSize := len(transformed.FilterTable)
	if filterSize > serialize.MaxFilterSize {
		filterSize = serialize.MaxFilterSize
	}
	filterEnd := serialize.FilterOffset + filterSize
	if filterEnd > serialize.ArpOffset {
		details = append(details, fmt.Sprintf("filter table (%d bytes) overflows into arp area",
			filterSize))
	}

	arpSize := len(transformed.ArpTable)
	if arpSize > serialize.MaxArpSize {
		arpSize = serialize.MaxArpSize
	}
	arpEnd := serialize.ArpOffset + arpSize
	if arpEnd > serialize.TransBaseOffset {
		details = append(details, fmt.Sprintf("arp table (%d bytes) overflows into trans/delta base area",
			arpSize))
	}

	numDictEntries := len(encoded.RowDict) / 3
	// Check dictionary fits in allocated space (entry 0 is implicit, so max is DictArraySize+1 total)
	if numDictEntries > serialize.DictArraySize+1 {
		details = append(details, fmt.Sprintf("dictionary has %d entries but max is %d",
			numDictEntries, serialize.DictArraySize+1))
	}

	if len(details) > 0 {
		return NewError("serialize", "serialization produced invalid output", details...)
	}

	return nil
}

func SerializeBounds(
	instSize int,
	bitstreamSize int,
	filterSize int,
	arpSize int,
	numDictEntries int,
	numPatterns int,
) error {
	var details []string

	if instSize > serialize.BitstreamOffset-serialize.InstOffset {
		details = append(details, fmt.Sprintf("instrument data %d bytes exceeds limit %d",
			instSize, serialize.BitstreamOffset-serialize.InstOffset))
	}

	if bitstreamSize > serialize.FilterOffset-serialize.BitstreamOffset {
		details = append(details, fmt.Sprintf("bitstream %d bytes exceeds limit %d",
			bitstreamSize, serialize.FilterOffset-serialize.BitstreamOffset))
	}

	if filterSize > serialize.MaxFilterSize {
		details = append(details, fmt.Sprintf("filter table %d bytes exceeds limit %d",
			filterSize, serialize.MaxFilterSize))
	}

	if arpSize > serialize.MaxArpSize {
		details = append(details, fmt.Sprintf("arp table %d bytes exceeds limit %d",
			arpSize, serialize.MaxArpSize))
	}

	if numDictEntries > serialize.DictArraySize+1 {
		details = append(details, fmt.Sprintf("dictionary %d entries exceeds limit %d",
			numDictEntries-1, serialize.DictArraySize))
	}

	ptrTableSize := numPatterns * 2
	ptrTableEnd := serialize.PackedPtrsOffset + ptrTableSize
	if ptrTableEnd > serialize.MaxOutputSize {
		details = append(details, fmt.Sprintf("pattern pointer table extends to %d, exceeds max output size %d",
			ptrTableEnd, serialize.MaxOutputSize))
	}

	if len(details) > 0 {
		return NewError("serialize/bounds", "data exceeds serialization bounds", details...)
	}

	return nil
}
