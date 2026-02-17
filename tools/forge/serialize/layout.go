package serialize

const (
	InstOffset      = 0x000
	BitstreamOffset = 0x1F0
	MaxOrders       = 119
	FilterOffset    = 0x3CC
	ArpOffset       = 0x4AF
	TransBaseOffset = 0x56B
	DeltaBaseOffset = 0x56C
	RowDictOffset   = 0x56D

	MaxFilterSize = 227
	MaxArpSize    = 188

	MaxOutputSize = 0x2000
)

var DictArraySize = 365

func PackedPtrsOffset() int {
	return RowDictOffset + DictArraySize*3
}
