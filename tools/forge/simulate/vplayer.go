package simulate

import (
	"fmt"
	"forge/serialize"
)

// MinimalPlayer is a clean, minimal implementation of the SID player.
// It tracks only essential state needed for playback.
type MinimalPlayer struct {
	// Song data
	fullData   []byte   // Complete song data (for absolute pattern offsets)
	instData   []byte   // Instrument parameters
	bitstream  []byte   // Order bitstream (4 bytes per order)
	dict       [3][]byte // Row dictionary [note, inst, effect]
	patternPtr []uint16 // Pattern offsets (absolute into fullData)
	patternGap []byte   // Gap codes per pattern
	filterTbl  []byte
	debugMode  bool     // Enable debug logging
	arpTbl     []byte
	waveTbl    []byte
	transTbl   []byte
	deltaTbl   []byte
	transBase  int
	deltaBase  int
	startConst int
	numInst    int

	// Global state
	order           int
	nextOrder       int
	row             int
	speed           int
	speedCtr        int
	mod3            int
	forceNewPattern bool

	// Filter
	filterIdx  int
	filterEnd  int
	filterLoop int
	filterCut  byte
	filterRes  byte
	filterMode byte
	volume     byte

	// Channels
	ch [3]minChan

	// Output
	writes []SIDWrite
	frame  int
}

type minChan struct {
	// Pattern stream - always one row ahead
	src_off      int  // offset into pattern data
	prevIdx      int  // previous dict index (-1 = zero row)
	prevNote     byte // note override (for $FE note-only)
	rle_count    int  // RLE repeats remaining
	gap_remaining int  // gap zeros remaining
	nextIdx      int  // next dict index (read ahead for peek)
	nextNote     byte // note override for next row
	rowsDecoded  int  // number of rows decoded from this pattern (for bounds checking)

	// Row data
	note     byte // currently sounding note (0 = not set yet)
	inst     byte // instrument (0 = none loaded yet)
	effect   byte // effect 0-15
	effectpar byte // effect parameter
	permarp  byte // permanent arpeggio (persists across NOP rows)

	// Instrument playback (positions advance, limits derived from inst)
	waveidx  int
	arpidx   int
	vibdelay byte
	vibpos   byte
	plsdir   byte // 0 = up, $80 = down

	// SID output state
	transpose  int8 // transpose
	ad, sr     byte
	waveform   byte
	gateon     byte // 0xFF=on, 0xFE=off
	plswidthlo byte
	plswidthhi byte
	freqlo     byte
	freqhi     byte
	notefreqlo byte // Target for portamento
	notefreqhi byte
	finfreqlo  byte // Final after vibrato
	finfreqhi  byte

	// Slide
	slideenable   byte
	slidedelta_lo byte
	slidedelta_hi byte

	// Trackptr
	trackptr_cur int
	hardrestart  byte
}

// gapValue computes gap size from gap code: 0→0, 1→1, 2→3, 3→7, etc.
// Gap code N produces (2^N - 1) empty rows: 1→1 zero, 2→3 zeros, 3→7 zeros, 4→15 zeros
func gapValue(code byte) int {
	if code == 0 {
		return 0
	}
	return (1 << code) - 1
}

// setFreq sets both current and target frequency from freq value
func (c *minChan) setFreq(freq uint16) {
	c.freqlo = byte(freq)
	c.freqhi = byte(freq >> 8)
	c.notefreqlo = byte(freq)
	c.notefreqhi = byte(freq >> 8)
}

func NewMinimalPlayer(
	songData []byte,
	numPatterns int,
	deltaTbl, transTbl, waveTbl []byte,
	startConst int,
) *MinimalPlayer {
	return NewMinimalPlayerWithDebug(songData, numPatterns, deltaTbl, transTbl, waveTbl, startConst, false)
}

func NewMinimalPlayerWithDebug(
	songData []byte,
	numPatterns int,
	deltaTbl, transTbl, waveTbl []byte,
	startConst int,
	debugMode bool,
) *MinimalPlayer {
	p := &MinimalPlayer{
		speed:      6,
		speedCtr:   5,  // triggers row on frame 0
		mod3:       0,  // matches ASM player (0 -> -1 -> 2 on first frame)
		volume:     0x0F,
		row:        -1, // so first advanceRow() goes to row 0
		transTbl:   transTbl,
		deltaTbl:   deltaTbl,
		waveTbl:    waveTbl,
		startConst: startConst,
		debugMode:  debugMode,
	}

	p.parseSongData(songData, numPatterns)

	// Init channels
	for i := range p.ch {
		p.ch[i].hardrestart = 2
	}

	// Load first order (sets up decoders)
	p.order = 0
	p.nextOrder = 1
	p.loadOrder(0)

	return p
}

func (p *MinimalPlayer) parseSongData(data []byte, numPatterns int) {
	dictSize := serialize.DictArraySize
	ptrsOff := serialize.PackedPtrsOffset()

	// Store full data for absolute pattern offsets
	p.fullData = data

	// Instruments: at offset 0, 16 bytes each, size determined by bitstream start
	p.instData = data[serialize.InstOffset:serialize.BitstreamOffset]
	p.numInst = len(p.instData) / 16

	// Bitstream
	p.bitstream = data[serialize.BitstreamOffset:serialize.FilterOffset]

	// Filter table
	p.filterTbl = data[serialize.FilterOffset:serialize.ArpOffset]

	// Arp table
	p.arpTbl = data[serialize.ArpOffset:serialize.TransBaseOffset]

	// Bases
	p.transBase = int(data[serialize.TransBaseOffset])
	p.deltaBase = int(data[serialize.DeltaBaseOffset])

	// Dictionary (3 separate arrays: note, inst, effect)
	p.dict[0] = data[serialize.RowDictOffset : serialize.RowDictOffset+dictSize]
	p.dict[1] = data[serialize.RowDictOffset+dictSize : serialize.RowDictOffset+dictSize*2]
	p.dict[2] = data[serialize.RowDictOffset+dictSize*2 : serialize.RowDictOffset+dictSize*3]

	// Pattern pointers (absolute offsets into fullData)
	p.patternPtr = make([]uint16, numPatterns)
	p.patternGap = make([]byte, numPatterns)
	for i := 0; i < numPatterns; i++ {
		lo := data[ptrsOff+i*2]
		hi := data[ptrsOff+i*2+1]
		p.patternPtr[i] = uint16(lo) | (uint16(hi&0x1F) << 8)
		p.patternGap[i] = hi >> 5
	}
}

// decodeOrderBitstream extracts transpose and delta indices from 4-byte order entry
func decodeOrderBitstream(bs []byte) (trans, delta [3]byte) {
	trans = [3]byte{bs[0] & 0x0F, bs[0] >> 4, bs[1] & 0x0F}
	delta = [3]byte{
		(bs[1] >> 4) | ((bs[2] & 0x01) << 4),
		(bs[2] >> 1) & 0x1F,
		(bs[2] >> 6) | ((bs[3] & 0x07) << 2),
	}
	return
}

func (p *MinimalPlayer) loadOrder(orderNum int) {
	trans, delta := decodeOrderBitstream(p.bitstream[orderNum*4:])

	// Apply to channels
	for ch := 0; ch < 3; ch++ {
		// Transpose
		tIdx := p.transBase + int(trans[ch])
		p.ch[ch].transpose = int8(p.transTbl[tIdx])

		// Trackptr delta (pattern index)
		// IMPORTANT: Order 0 is absolute (startConst + delta), others are relative (+=delta)
		dIdx := p.deltaBase + int(delta[ch])
		d := int8(p.deltaTbl[dIdx])
		if orderNum == 0 {
			p.ch[ch].trackptr_cur = p.startConst + int(d)
		} else {
			p.ch[ch].trackptr_cur += int(d)
		}

		// Init pattern decoder for this pattern
		p.initDecoder(ch, p.ch[ch].trackptr_cur)
	}
}

func (p *MinimalPlayer) initDecoder(ch, patIdx int) {
	c := &p.ch[ch]
	c.src_off = int(p.patternPtr[patIdx])
	c.prevIdx = -1
	c.prevNote = 0
	c.rle_count = 0
	c.gap_remaining = 0
	c.rowsDecoded = 0
	// IMPORTANT: Pattern decoder is always one row ahead. nextIdx/nextNote contain
	// the NEXT row to be consumed, not the current row. This allows peeking for HR.
	c.nextIdx, c.nextNote = p.advanceStream(c)
}

// rowFromIdx returns the 3-byte row for a dict index (-1 = zero row).
func (p *MinimalPlayer) rowFromIdx(idx int, noteOverride byte) [3]byte {
	if idx < 0 {
		return [3]byte{noteOverride, 0, 0}
	}
	note := p.dict[0][idx]
	if noteOverride != 0 {
		note = noteOverride
	}
	return [3]byte{note, p.dict[1][idx], p.dict[2][idx]}
}

// peekNextRow returns the next row without advancing.
func (p *MinimalPlayer) peekNextRow(ch int) [3]byte {
	c := &p.ch[ch]
	return p.rowFromIdx(c.nextIdx, c.nextNote)
}

// consumeRow returns the current row and advances to the next.
func (p *MinimalPlayer) consumeRow(ch int) [3]byte {
	c := &p.ch[ch]
	row := p.rowFromIdx(c.nextIdx, c.nextNote)
	c.nextIdx, c.nextNote = p.advanceStream(c)
	return row
}

// advanceStream advances through the pattern stream and returns the next dict index.
// Returns (idx, noteOverride) where idx=-1 means zero row (empty with note override).
//
// IMPORTANT: RLE and gaps interact:
// - RLE count means "repeat N MORE times" (not including the first instance)
// - Gaps are inserted AFTER each RLE repeat if pattern has a gap code
// - Example: dict entry with RLE=2 and gap=1 produces: [entry, gap, entry, gap, entry]
func (p *MinimalPlayer) advanceStream(c *minChan) (int, byte) {
	// Increment row counter and check if we've generated all 64 rows
	c.rowsDecoded++
	if c.rowsDecoded > 64 {
		// Trying to decode beyond row 64 - return empty without reading stream
		return -1, 0
	}

	// Gap zeros (inserted after RLE repeats or new entries)
	if c.gap_remaining > 0 {
		c.gap_remaining--
		return -1, 0
	}

	// RLE repeat (return previous entry again)
	if c.rle_count > 0 {
		c.rle_count--
		// IMPORTANT: Gaps are inserted after EVERY RLE repeat
		if gapCode := p.patternGap[c.trackptr_cur]; gapCode > 0 {
			c.gap_remaining = gapValue(gapCode)
		}
		return c.prevIdx, c.prevNote
	}

	// Read encoded byte and decode pattern stream format
	b := p.fullData[c.src_off]
	c.src_off++

	// Pattern stream encoding:
	// 0x00-0x0F: Empty row + RLE (count = 0-15)
	// 0x10-0xEE: Dictionary index 0-222 (idx = byte - 0x10)
	// 0xEF-0xFD: RLE using previous entry (count = 0-14, repeat entry 1-15 more times)
	// 0xFE: Note-only override (next byte = note, keeps prevIdx for inst/effect)
	// 0xFF: Extended dict index 224+ (next byte = offset, idx = 224 + byte - 1)
	switch {
	case b <= 0x0F:
		// Empty row with RLE count (0 = emit once, 1 = emit twice, etc.)
		c.prevIdx = -1
		c.prevNote = 0
		c.rle_count = int(b)
	case b >= 0x10 && b <= 0xEE:
		// Direct dictionary index 0-222
		c.prevIdx = int(b - 0x10)
		c.prevNote = 0
	case b >= 0xEF && b <= 0xFD:
		// RLE repeat previous entry 1-15 more times (0xEF=1 more, 0xFD=15 more)
		c.rle_count = int(b - 0xEF)
	case b == 0xFE:
		// Note-only: override note in previous entry, keep inst/effect
		c.prevNote = p.fullData[c.src_off]
		c.src_off++
	case b == 0xFF:
		// Extended dictionary index 224-479 (next byte is offset 1-255)
		c.prevIdx = 224 + int(p.fullData[c.src_off]) - 1
		c.prevNote = 0
		c.src_off++
	}

	if gapCode := p.patternGap[c.trackptr_cur]; gapCode > 0 {
		c.gap_remaining = gapValue(gapCode)
	}
	return c.prevIdx, c.prevNote
}

func (p *MinimalPlayer) Tick() []SIDWrite {
	p.writes = nil

	if p.debugMode && p.frame == 0 {
		fmt.Printf("[VPlayer pre-f0] speedCtr=%d speed=%d forceNewPattern=%v\n",
			p.speedCtr, p.speed, p.forceNewPattern)
	}

	// Mod3 counter (for pattern arpeggio tick - cycles 0,1,2,0,1,2,...)
	p.mod3--
	if p.mod3 < 0 {
		p.mod3 = 2
	}

	// Speed counter (ticks per row)
	// IMPORTANT: Counter counts 0 to (speed-1), then wraps and advances row
	// Example: speed=6 means counter goes 0,1,2,3,4,5, then wraps to 0 and advances row
	p.speedCtr++
	if p.speedCtr >= p.speed {
		p.speedCtr = 0
		p.advanceRow()
		if p.debugMode && p.frame == 0 {
			fmt.Printf("[VPlayer post-advance] ch0.note=$%02X ch0.trans=%d\n",
				p.ch[0].note, p.ch[0].transpose)
		}
	}

	// Per-frame effects (wave, arp, pulse modulation)
	p.processFrame()

	if p.debugMode && p.frame == 0 {
		fmt.Printf("[VPlayer post-process] ch0.freqlo=$%02X ch0.freqhi=$%02X\n",
			p.ch[0].freqlo, p.ch[0].freqhi)
	}

	// HR lookahead (after processFrame, matching ASM player order)
	p.hrLookahead()

	// Output to SID
	p.outputSID()

	p.frame++
	return p.writes
}

func (p *MinimalPlayer) advanceRow() {
	if p.forceNewPattern {
		// Pattern break - jump to next order, row 0
		p.row = 0
		p.order = p.nextOrder
		p.nextOrder = p.order + 1
		p.forceNewPattern = false
		p.loadOrder(p.order)
	} else {
		p.row++
		if p.row >= 64 {
			p.row = 0
			p.order = p.nextOrder
			p.nextOrder = p.order + 1
			// TODO: handle loop
			p.loadOrder(p.order)
		}
	}

	// Consume pre-decoded row for each channel and apply it
	for ch := 0; ch < 3; ch++ {
		row := p.consumeRow(ch)
		p.applyRow(ch, row)
	}
}

func (p *MinimalPlayer) applyRow(ch int, row [3]byte) {
	c := &p.ch[ch]

	// Decode row format (3 bytes):
	// row[0]: bit 7 = effect bit 3 (for effects 8-15), bits 0-6 = note (0x00-0x7F)
	// row[1]: bits 5-7 = effect bits 0-2, bits 0-4 = instrument (0-31)
	// row[2]: effect parameter (0-255)
	//
	// Note values: 0=empty, 1-96=notes, 0x61=key-off, 0x62+=unused
	// Effect numbers are split: low 3 bits from row[1] high bits, bit 3 from row[0] bit 7
	note := row[0] & 0x7F
	hasEffBit3 := (row[0] & 0x80) != 0
	inst := row[1] & 0x1F
	effect := row[1] >> 5
	if hasEffBit3 {
		effect |= 0x08 // Reconstruct effects 8-15
	}
	param := row[2]

	c.effect = effect
	c.effectpar = param

	// Load instrument first if inst > 0 (regardless of note - matches ASM)
	// IMPORTANT: Instrument numbers are 1-based (inst 1 = first instrument)
	// but instrument data array is 0-based, so subtract 1 for array index
	if inst > 0 {
		c.inst = inst
		p.loadInstrument(ch, int(inst)-1)
	}

	// Note-off (0x61 is the key-off note value)
	if note == 0x61 {
		c.gateon = 0xFE
	}

	// New note (note > 0 and not key-off)
	// IMPORTANT: note value stored as-is (1-based from file format)
	// but frequency table is 0-based, so subtract 1 for lookups
	if note > 0 && note != 0x61 {
		c.note = note

		// Reset slide on new note
		c.slidedelta_lo = 0
		c.slidedelta_hi = 0
		c.slideenable = 0

		// Reset wave and arp indices on new note (even without new inst)
		if c.inst > 0 {
			instBase := (int(c.inst) - 1) * 16
			c.waveidx = int(p.instData[instBase+2])
			c.arpidx = int(p.instData[instBase+5])
		}

		// Portamento (effect 2): set target frequency but don't trigger gate
		// IMPORTANT: Portamento WITHOUT instrument = slide to new pitch, keep playing
		// Portamento WITH instrument = slide to new pitch AND retrigger with new instrument
		if effect == 2 {
			targetNote := int(note) - 1 + int(c.transpose)
			freq := freqTable[targetNote]
			c.notefreqlo = byte(freq)
			c.notefreqhi = byte(freq >> 8)
			// Only gate if there's also an instrument trigger
			if inst > 0 {
				c.gateon = 0xFF
			}
		} else {
			// Normal note: always gate
			c.gateon = 0xFF
		}
	}

	// Apply effect
	p.applyEffect(ch)
}

func (p *MinimalPlayer) loadInstrument(ch, instIdx int) {
	c := &p.ch[ch]
	// Instrument data layout (16 bytes per instrument):
	// +0: AD (attack/decay)
	// +1: SR (sustain/release)
	// +2: wave start pos
	// +3: wave end pos
	// +4: wave loop pos
	// +5: arp start pos
	// +6: arp end pos
	// +7: arp loop pos
	// +8: vibrato delay
	// +9: vibrato depth/speed
	// +10: pulse width (nibble-swapped)
	// +11: pulse speed
	// +12: pulse limits
	// +13: filter start
	// +14: filter end
	// +15: filter loop
	base := instIdx * 16

	c.ad = p.instData[base+0]
	c.sr = p.instData[base+1]
	c.waveidx = int(p.instData[base+2])
	c.arpidx = int(p.instData[base+5])
	c.vibdelay = p.instData[base+8]
	c.vibpos = 0

	// Initialize pulse width (nibble-swapped: original $XY -> stored $YX)
	pw := p.instData[base+10]
	c.plswidthlo = pw & 0xF0
	c.plswidthhi = pw & 0x0F
	c.plsdir = 0
}

func (p *MinimalPlayer) applyEffect(ch int) {
	c := &p.ch[ch]

	// Effect numbers from effects.go:
	// 0=Special, 1=Arp, 2=TonePorta, 3=Speed, 4=HRTiming, 5=FiltTrig
	// 6=SR, 7=Wave, 8=Pulse, 9=AD, 10=Reso, 11=Slide, 12=GlobVol, 13=FiltMode

	// Clear permArp for effects other than Special (0) and Arp (1)
	if c.effect != 0 && c.effect != 1 {
		c.permarp = 0
	}

	switch c.effect {
	case 0: // Special
		if c.effectpar != 0 {
			c.permarp = 0 // Non-NOP clears permarp
		}
		// Pattern break (param 2)
		if c.effectpar == 2 {
			p.forceNewPattern = true
		}
		// Fine slide (param 3) - add $04 to slide delta once per row
		if c.effectpar == 3 && p.speedCtr == 0 {
			c.slideenable = 0x80
			newLo := int(c.slidedelta_lo) + 0x04
			if newLo > 255 {
				c.slidedelta_hi++
			}
			c.slidedelta_lo = byte(newLo)
		}
	case 1: // Pattern arpeggio
		c.permarp = c.effectpar
	case 2: // Tone portamento (uses c.effectpar directly in processChannelFrame)
	case 3: // Speed
		p.speed = int(c.effectpar)
	case 4: // HR timing
		c.hardrestart = c.effectpar
	case 5: // Filter trigger - load filter params from instrument
		// Param is instrument number * 16 (pre-shifted)
		instBase := int(c.effectpar) - 16
		p.filterIdx = int(p.instData[instBase+13])  // INST_FILTSTART
		p.filterEnd = int(p.instData[instBase+14])  // INST_FILTEND
		p.filterLoop = int(p.instData[instBase+15]) // INST_FILTLOOP
	case 6: // SR
		c.sr = c.effectpar
	case 8: // Pulse width - effect applied every frame in processChannelFrame
		// Nothing to do here - handled in processChannelFrame
	case 9: // AD
		c.ad = c.effectpar
	case 10: // Filter resonance
		p.filterRes = c.effectpar
	case 11: // Slide - sets up slide mode (param 0=up, nonzero=down)
		c.slideenable = 0x80
	case 12: // Global volume
		p.volume = c.effectpar & 0x0F
	case 13: // Filter mode
		p.filterMode = c.effectpar
	case 15: // Permanent arpeggio
		c.permarp = c.effectpar
	}
}

func (p *MinimalPlayer) hrLookahead() {
	// Determine which row to peek for HR decision
	var hrRow, hrOrder int
	if p.forceNewPattern {
		hrRow, hrOrder = 0, p.nextOrder
	} else {
		hrRow, hrOrder = p.row+1, p.order
		if hrRow >= 64 {
			hrRow, hrOrder = 0, p.nextOrder
		}
	}

	for ch := 0; ch < 3; ch++ {
		c := &p.ch[ch]
		// HR timing: Check if next row will be applied within hardrestart frames
		// IMPORTANT: hardrestart controls early HR trigger (higher = earlier)
		// - hardrestart=0: HR on frame where speedCtr wraps (speedCtr+0 >= speed)
		// - hardrestart=1: HR one frame early (speedCtr+1 >= speed)
		// The condition "< speed" means SKIP HR check (not time yet)
		if p.speedCtr+int(c.hardrestart) < p.speed {
			continue
		}

		// Get next row and check if HR should be skipped
		// IMPORTANT: If next order, must decode that order's first row
		var row [3]byte
		if hrRow == 0 {
			row = p.decodeNextOrderFirstRow(ch, hrOrder)
		} else {
			row = p.peekNextRow(ch)
		}

		if !p.shouldSkipHR(row) {
			p.doHR(ch)
		}
	}
}

// shouldSkipHR returns true if HR should be skipped for this row
func (p *MinimalPlayer) shouldSkipHR(row [3]byte) bool {
	note := row[0] & 0x7F
	if note == 0 || note == 0x61 {
		return true // No note or key-off -> skip HR
	}
	// Immediate HR bit (row[0] bit 7) forces HR regardless of effect
	if row[0]&0x80 != 0 {
		return false // Immediate HR -> do HR
	}
	// Check for portamento (effect 2)
	// IMPORTANT: Only extracts low 3 bits of effect (0-7) from row[1] bits 5-7
	// This is safe because portamento is effect 2, but would be wrong for effects 8-15
	effect := row[1] >> 5
	return effect == 2 // Portamento -> skip HR, otherwise do HR
}

// decodeNextOrderFirstRow decodes the first row of the next order's pattern
func (p *MinimalPlayer) decodeNextOrderFirstRow(ch, nextOrder int) [3]byte {
	// Bounds check: ensure nextOrder is valid
	if nextOrder*4 >= len(p.bitstream) {
		return [3]byte{0, 0, 0} // Return empty row if beyond bitstream
	}

	_, delta := decodeOrderBitstream(p.bitstream[nextOrder*4:])

	dIdx := p.deltaBase + int(delta[ch])
	tp := p.ch[ch].trackptr_cur + int(int8(p.deltaTbl[dIdx]))

	// Bounds check: ensure pattern index is valid
	if tp < 0 || tp >= len(p.patternPtr) {
		return [3]byte{0, 0, 0}
	}

	patPtr := int(p.patternPtr[tp])
	// Bounds check: ensure pattern pointer is within buffer
	if patPtr >= len(p.fullData) {
		return [3]byte{0, 0, 0}
	}

	// Create temporary channel state for next order's pattern
	var tempChan minChan
	tempChan.src_off = patPtr
	tempChan.trackptr_cur = tp
	tempChan.prevIdx = -1

	// Decode first row
	idx, noteOvr := p.advanceStream(&tempChan)
	return p.rowFromIdx(idx, noteOvr)
}

func (p *MinimalPlayer) doHR(ch int) {
	c := &p.ch[ch]
	c.waveform = 0
	c.ad = 0
	c.sr = 0
}

func (p *MinimalPlayer) processFrame() {
	// Filter table processing (runs every frame if filterIdx != 0)
	// IMPORTANT: filterIdx=0 means "filter inactive" (sentinel value)
	// Active filter indices are 1-based
	if p.filterIdx != 0 {
		p.filterCut = p.filterTbl[p.filterIdx]
		p.filterIdx++
		if p.filterEnd > 0 && p.filterIdx >= p.filterEnd {
			p.filterIdx = p.filterLoop
		}
	}

	// Per-channel
	for ch := 0; ch < 3; ch++ {
		p.processChannelFrame(ch)
	}
}

func (p *MinimalPlayer) processChannelFrame(ch int) {
	c := &p.ch[ch]

	// Early exit if no instrument loaded (occurs only in initial frames before any music)
	if c.inst == 0 {
		c.finfreqlo = c.freqlo
		c.finfreqhi = c.freqhi
		return
	}

	// Compute instrument base offset once (reused throughout frame processing)
	instBase := (int(c.inst) - 1) * 16

	// Wavetable (1 byte per entry)
	c.waveform = p.waveTbl[c.waveidx]
	c.waveidx++
	if c.waveidx >= int(p.instData[instBase+3]) {
		c.waveidx = int(p.instData[instBase+4])
	}

	// Effect 7 (Wave) overrides waveform every frame
	if c.effect == 7 {
		c.waveform = c.effectpar
	}

	// Frequency/arp processing
	// Portamento (effect 2) skips arp/freq calculation - just slides toward target
	if c.effect == 2 {
		// Slide current freq toward noteFreq
		speedLo := c.effectpar & 0xF0
		speedHi := c.effectpar & 0x0F
		currFreq := int(c.freqlo) | (int(c.freqhi) << 8)
		targetFreq := int(c.notefreqlo) | (int(c.notefreqhi) << 8)
		speed := int(speedLo) | (int(speedHi) << 8)

		if currFreq < targetFreq {
			newFreq := currFreq + speed
			if newFreq >= targetFreq {
				c.freqlo = c.notefreqlo
				c.freqhi = c.notefreqhi
			} else {
				c.freqlo = byte(newFreq)
				c.freqhi = byte(newFreq >> 8)
			}
		} else if currFreq > targetFreq {
			newFreq := currFreq - speed
			if newFreq <= targetFreq {
				c.freqlo = c.notefreqlo
				c.freqhi = c.notefreqhi
			} else {
				c.freqlo = byte(newFreq)
				c.freqhi = byte(newFreq >> 8)
			}
		}
	} else {
		// Normal arp/freq calculation
		var arpOffset int
		arpVal := p.arpTbl[c.arpidx]
		if arpVal&0x80 != 0 {
			arpOffset = int(arpVal&0x7F) - (int(c.note) - 1 + int(c.transpose))
		} else {
			arpOffset = int(arpVal)
		}
		c.arpidx++
		if c.arpidx >= int(p.instData[instBase+6]) {
			c.arpidx = int(p.instData[instBase+7])
		}

		freqNote := int(c.note) - 1 + int(c.transpose) + arpOffset
		c.setFreq(freqTable[freqNote])

		// Pattern arpeggio OVERWRITES frequency - effect 1/15 use param, effect 0 NOP uses permArp
		if c.effect == 1 || c.effect == 15 || (c.effect == 0 && c.effectpar == 0 && c.permarp != 0) {
			playNote := int(c.note)
			if playNote > 0 && playNote < 0x61 {
				arpVal := c.effectpar
				if c.effect == 0 {
					arpVal = c.permarp
				}
				var patArpOffset int
				switch p.mod3 {
				case 0:
					patArpOffset = int(arpVal & 0x0F)
				case 1:
					patArpOffset = int(arpVal >> 4)
				default:
					patArpOffset = 0
				}
				finalNote := (playNote - 1) + patArpOffset + int(c.transpose)
				c.setFreq(freqTable[finalNote])
			}
		}
	}

	// Slide effect - accumulate delta every frame if effect is 11
	if c.effect == 11 {
		c.slideenable = 0x80
		if c.effectpar == 0 {
			newLo := int(c.slidedelta_lo) + 0x20
			if newLo > 255 {
				c.slidedelta_hi++
			}
			c.slidedelta_lo = byte(newLo)
		} else {
			newLo := int(c.slidedelta_lo) - 0x20
			if newLo < 0 {
				c.slidedelta_hi--
			}
			c.slidedelta_lo = byte(newLo)
		}
	}

	// Apply slide to frequency if enabled
	if c.slideenable != 0 {
		newLo := int(c.freqlo) + int(c.slidedelta_lo)
		carry := 0
		if newLo > 255 {
			carry = 1
		}
		c.freqlo = byte(newLo)
		c.freqhi = byte(int(c.freqhi) + int(c.slidedelta_hi) + carry)
	}

	// Vibrato processing - early exit if delay active or disabled
	if c.vibdelay > 0 {
		c.vibdelay--
		c.finfreqlo = c.freqlo
		c.finfreqhi = c.freqhi
	} else if c.effect == 0 && c.effectpar == 1 {
		// VibOff - no vibrato
		c.finfreqlo = c.freqlo
		c.finfreqhi = c.freqhi
	} else {
		// Load vibrato params from instrument
		vibDS := p.instData[instBase+9]
		vibDepth := vibDS & 0xF0
		vibSpeed := vibDS & 0x0F

		if vibDepth == 0 {
			c.finfreqlo = c.freqlo
			c.finfreqhi = c.freqhi
		} else {
			// Apply vibrato
			pos := c.vibpos & 0x1F
			if pos >= 0x10 {
				pos = pos ^ 0x1F
			}
			depthRow := int(vibDepth>>4) - 1
			vibOffset := int(vibratoTable[depthRow][pos]) * 2

			freq := uint16(c.freqlo) | (uint16(c.freqhi) << 8)
			if c.vibpos&0x20 != 0 {
				freq += uint16(vibOffset)
			} else {
				freq -= uint16(vibOffset)
			}
			c.finfreqlo = byte(freq)
			c.finfreqhi = byte(freq >> 8)

			c.vibpos += vibSpeed
		}
	}

	// Pulse modulation - runs if pulseSpeed != 0 AND effect != 8 (pulse override)
	if c.effect != 8 {
		pulseSpeed := p.instData[instBase+11]
		if pulseSpeed != 0 {
			limits := p.instData[instBase+12]
			limitUp := limits & 0x0F
			limitDown := limits >> 4
			if c.plsdir == 0 {
				newLo := int(c.plswidthlo) + int(pulseSpeed)
				carry := byte(0)
				if newLo > 255 {
					carry = 1
				}
				c.plswidthlo = byte(newLo)
				newHi := int(c.plswidthhi) + int(carry)
				if newHi > int(limitUp) {
					c.plsdir = 0x80
					c.plswidthlo = 0xFF
					c.plswidthhi = limitUp
				} else {
					c.plswidthhi = byte(newHi)
				}
			} else {
				newLo := int(c.plswidthlo) - int(pulseSpeed)
				borrow := byte(0)
				if newLo < 0 {
					borrow = 1
					newLo += 256
				}
				c.plswidthlo = byte(newLo)
				newHi := int(c.plswidthhi) - int(borrow)
				if newHi < int(limitDown) {
					c.plsdir = 0
					c.plswidthlo = 0
					c.plswidthhi = limitDown
				} else {
					c.plswidthhi = byte(newHi)
				}
			}
		}
	}

	// Effect 8 (Pulse) - overrides pulse width every frame
	if c.effect == 8 {
		if c.effectpar != 0 {
			c.plswidthhi = 0x08
		} else {
			c.plswidthhi = 0x00
		}
		c.plswidthlo = 0x00
	}
}

func (p *MinimalPlayer) outputSID() {
	// Channel registers (order matches ASM: pulse, freq, wave, ad, sr)
	for ch := 0; ch < 3; ch++ {
		c := &p.ch[ch]
		base := uint16(0xD400 + ch*7)

		p.write(base+2, c.plswidthlo)
		p.write(base+3, c.plswidthhi)
		if p.debugMode && p.frame == 0 && ch == 0 {
			fmt.Printf("[VPlayer] write f%d ch%d: $D400=$%02X (finFreqLo)\n", p.frame, ch, c.finfreqlo)
		}
		p.write(base+0, c.finfreqlo)
		p.write(base+1, c.finfreqhi)
		p.write(base+4, c.waveform&c.gateon) // waveform AND gate
		p.write(base+5, c.ad)
		p.write(base+6, c.sr)
	}

	// Filter (matches ASM player: D416=cutoff, D417=resonance, D418=volume|mode)
	p.write(0xD416, p.filterCut)
	p.write(0xD417, p.filterRes)
	p.write(0xD418, p.volume|p.filterMode)
}

func (p *MinimalPlayer) write(addr uint16, val byte) {
	p.writes = append(p.writes, SIDWrite{Addr: addr, Value: val, Frame: p.frame})
}
