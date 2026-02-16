package serialize

const (
	InstOffset      = 0x000
	BitstreamOffset = 0x1F0
	FilterOffset    = 0x5EC
	ArpOffset       = 0x6CF
	TransBaseOffset = 0x78B
	DeltaBaseOffset = 0x78C
	RowDictOffset   = 0x78D

	MaxFilterSize = 227
	MaxArpSize    = 188

	MaxOutputSize = 0x2000
)

var DictArraySize = 365

func PackedPtrsOffset() int {
	return RowDictOffset + DictArraySize*3
}
