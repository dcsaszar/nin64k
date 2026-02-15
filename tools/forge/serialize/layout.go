package serialize

const (
	InstOffset       = 0x000
	BitstreamOffset  = 0x1F0
	FilterOffset     = 0x5EC
	ArpOffset        = 0x6CF
	TransBaseOffset  = 0x78B
	DeltaBaseOffset  = 0x78C
	RowDictOffset    = 0x78D
	PackedPtrsOffset = 0xBD4 // 0x78D + 365*3

	MaxFilterSize = 227
	MaxArpSize    = 188
	DictArraySize = 365 // Max needed with equiv mapping (song 5 = 366 entries, 000000 implicit)

	MaxOutputSize = 0x2000
)
