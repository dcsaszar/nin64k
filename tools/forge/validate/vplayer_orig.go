package validate

import (
	"fmt"

	"forge/parse"
)

// OriginalVP plays ParsedSong data using original GT effect numbering.
// This validates that our core playback logic is correct before any transformation.
type OriginalVP struct {
	song *parse.ParsedSong

	// Channel state
	chn [3]origChannelState

	// Global state
	speed           int
	speedCounter    int
	mod3counter     int
	order           int
	nextOrder       int
	row             int
	forceNewPattern bool

	// Filter state
	filterCutoff    byte
	filterResonance byte
	filterMode      byte
	globalVolume    byte
	filterIdx       byte
	filterEnd       byte
	filterLoop      byte

	// Output
	writes       []SIDWrite
	currentFrame int
}

type origChannelState struct {
	// Playback state
	note        byte
	playingNote byte
	inst        byte
	effect      byte
	param       byte
	transpose   int8

	// Instrument state
	instIdx    int
	instActive bool
	waveIdx    byte
	arpIdx     byte

	// Pulse modulation
	pulseSpeed     byte
	pulseLimitUp   byte
	pulseLimitDown byte
	pulseDir       byte

	// Vibrato
	vibDelay byte
	vibDepth byte
	vibSpeed byte
	vibPos   byte

	// Slide
	slideEnable  byte
	slideDeltaLo byte
	slideDeltaHi byte

	// Output registers
	freqLo     byte
	freqHi     byte
	noteFreqLo byte
	noteFreqHi byte
	finFreqLo  byte
	finFreqHi  byte
	pulseLo    byte
	pulseHi    byte
	waveform   byte
	ad         byte
	sr         byte
	gateon     byte
	hardRestart byte
}

var ovpDebug = false

// NewOriginalVP creates a virtual player for original GT format data.
func NewOriginalVP(song *parse.ParsedSong) *OriginalVP {
	vp := &OriginalVP{
		song:         song,
		speed:        6,
		globalVolume: 0x0F,
	}

	for ch := 0; ch < 3; ch++ {
		vp.chn[ch].gateon = 0xFE
		vp.chn[ch].hardRestart = 2
	}

	return vp
}

// RunFrames runs the player for the specified number of frames.
func (vp *OriginalVP) RunFrames(frames int) []SIDWrite {
	vp.writes = nil
	vp.nextOrder = 1
	vp.initOrder(0)

	for frame := 0; frame < frames; frame++ {
		vp.currentFrame = frame
		vp.playFrame()
	}

	return vp.writes
}

func (vp *OriginalVP) initOrder(orderNum int) {
	vp.order = orderNum

	if orderNum >= len(vp.song.Orders[0]) {
		return
	}

	for ch := 0; ch < 3; ch++ {
		if orderNum < len(vp.song.Orders[ch]) {
			vp.chn[ch].transpose = vp.song.Orders[ch][orderNum].Transpose
		}
	}
}

func (vp *OriginalVP) playFrame() {
	vp.mod3counter--
	if vp.mod3counter < 0 {
		vp.mod3counter = 2
	}

	if vp.speedCounter == 0 {
		vp.processRow()
	}

	for ch := 0; ch < 3; ch++ {
		vp.processInstrument(ch)
	}

	vp.processFilter()

	for ch := 0; ch < 3; ch++ {
		shouldCheck := vp.speedCounter+int(vp.chn[ch].hardRestart) >= vp.speed
		if shouldCheck {
			vp.checkHardRestart(ch)
		}
	}

	vp.dumpRegisters()

	vp.speedCounter++
	if vp.speedCounter >= vp.speed {
		vp.speedCounter = 0
		if vp.forceNewPattern {
			vp.row = 0
			vp.order = vp.nextOrder
			vp.nextOrder++
			vp.forceNewPattern = false
			vp.initOrder(vp.order)
		} else {
			vp.row++
			if vp.row >= 64 {
				vp.row = 0
				vp.order = vp.nextOrder
				vp.nextOrder++
				vp.initOrder(vp.order)
			}
		}
	}
}

func (vp *OriginalVP) processRow() {
	for ch := 0; ch < 3; ch++ {
		row := vp.getRow(ch)
		if row == nil {
			continue
		}

		c := &vp.chn[ch]
		c.note = row.Note
		c.inst = row.Inst
		c.effect = row.Effect
		c.param = row.Param

		if ovpDebug && vp.currentFrame < 8 {
			fmt.Printf("  [f%d] ch%d row%d: note=%02X inst=%d eff=%X param=%02X\n",
				vp.currentFrame, ch, vp.row, c.note, c.inst, c.effect, c.param)
		}

		// Trigger instrument if specified
		if c.inst > 0 {
			vp.triggerInstrument(ch, int(c.inst))
		}

		// Handle note
		// Original GT format: 0=none, 1-96=notes, $7F=key off
		if c.note > 0 && c.note < 0x7F {
			c.playingNote = c.note

			// Effect 3 = tone portamento in original GT
			if c.effect == 3 {
				if c.inst > 0 {
					c.gateon = 0xFF
				}
				targetNote := int(c.note) - 1 + int(c.transpose)
				if targetNote >= 0 && targetNote < len(freqTable) {
					freq := freqTable[targetNote]
					c.noteFreqLo = byte(freq)
					c.noteFreqHi = byte(freq >> 8)
				}
			} else {
				c.gateon = 0xFF
			}

			// Reset wave/arp/slide on any note
			if c.instIdx > 0 && c.instIdx <= len(vp.song.Instruments) {
				inst := vp.song.Instruments[c.instIdx-1]
				c.waveIdx = inst.WaveStart
				c.arpIdx = inst.ArpStart
			}
			c.slideDeltaLo = 0
			c.slideDeltaHi = 0
			c.slideEnable = 0
		} else if c.note == 0x7F {
			c.gateon = 0xFE
		}

		vp.processEffect(ch, c.effect, c.param)
	}
}

func (vp *OriginalVP) getRow(ch int) *parse.Row {
	if vp.order >= len(vp.song.Orders[ch]) {
		return nil
	}
	patAddr := vp.song.Orders[ch][vp.order].PatternAddr
	pat, ok := vp.song.Patterns[patAddr]
	if !ok {
		return nil
	}
	if vp.row >= 64 {
		return nil
	}
	return &pat.Rows[vp.row]
}

func (vp *OriginalVP) triggerInstrument(ch, inst int) {
	if inst <= 0 || inst >= len(vp.song.Instruments) {
		return
	}

	c := &vp.chn[ch]
	i := vp.song.Instruments[inst]
	c.instIdx = inst
	c.instActive = true
	c.ad = i.AD
	c.sr = i.SR
	c.waveIdx = i.WaveStart
	c.arpIdx = i.ArpStart
	c.vibDelay = i.VibDelay
	c.vibPos = 0

	// Pulse: original format has $XY, split into lo/hi
	c.pulseLo = i.PulseWidth & 0xF0
	c.pulseHi = i.PulseWidth & 0x0F
	c.pulseSpeed = i.PulseSpeed
	c.pulseLimitDown = i.PulseLimits >> 4
	c.pulseLimitUp = i.PulseLimits & 0x0F
	c.pulseDir = 0

	if ovpDebug && vp.currentFrame < 5 {
		fmt.Printf("      inst%d: AD=%02X SR=%02X waveIdx=%d arpIdx=%d\n",
			inst, c.ad, c.sr, c.waveIdx, c.arpIdx)
	}
}

func (vp *OriginalVP) processInstrument(ch int) {
	c := &vp.chn[ch]

	if !c.instActive || c.instIdx <= 0 || c.instIdx >= len(vp.song.Instruments) {
		c.finFreqLo = c.freqLo
		c.finFreqHi = c.freqHi
		return
	}

	inst := vp.song.Instruments[c.instIdx]

	// Wave table processing
	if c.waveIdx != 255 && int(c.waveIdx) < len(vp.song.WaveTable) {
		c.waveform = vp.song.WaveTable[c.waveIdx]
		c.waveIdx++
		if c.waveIdx > inst.WaveEnd {
			c.waveIdx = inst.WaveLoop
		}
	}

	// Skip arp/freq for portamento (effect 3 in original)
	if c.effect == 3 {
		goto skipArpFreq
	}

	// Pattern arpeggio (effect 1 in original - but actually this is slide)
	// Original GT effect 1 is portamento up, not pattern arpeggio
	// Need to handle this correctly...

	// Calculate frequency from note + transpose + instrument arp
	{
		note := int(c.playingNote)
		if note > 0 && note < 0x7F {
			var finalNote int
			if c.arpIdx < 255 && int(c.arpIdx) < len(vp.song.ArpTable) {
				arpVal := vp.song.ArpTable[c.arpIdx]
				if ovpDebug && vp.currentFrame >= 38845 && vp.currentFrame <= 38847 && ch == 1 {
					fmt.Printf("  [f%d ch%d] arp: idx=%d val=%02X note=%d instIdx=%d\n", vp.currentFrame, ch, c.arpIdx, arpVal, note, c.instIdx)
				}
				if arpVal&0x80 != 0 {
					finalNote = int(arpVal & 0x7F)
				} else {
					finalNote = (note - 1) + int(c.transpose) + int(int8(arpVal))
				}
				c.arpIdx++
				if c.arpIdx > inst.ArpEnd {
					c.arpIdx = inst.ArpLoop
				}
			} else {
				finalNote = (note - 1) + int(c.transpose)
			}

			if finalNote < 0 {
				finalNote = 0
			}
			if finalNote >= len(freqTable) {
				finalNote = len(freqTable) - 1
			}
			freq := freqTable[finalNote]
			if ovpDebug && vp.currentFrame >= 38845 && vp.currentFrame <= 38847 && ch == 1 {
				fmt.Printf("  [f%d ch%d] finalNote=%d freq=%04X transpose=%d\n", vp.currentFrame, ch, finalNote, freq, c.transpose)
			}
			c.freqLo = byte(freq)
			c.freqHi = byte(freq >> 8)
			c.noteFreqLo = byte(freq)
			c.noteFreqHi = byte(freq >> 8)
		}
	}

skipArpFreq:

	// Vibrato
	c.vibDepth = 0
	if c.vibDelay > 0 {
		c.vibDelay--
	} else {
		c.vibDepth = inst.VibDepthSpeed & 0xF0
		if c.vibDepth != 0 {
			c.vibSpeed = inst.VibDepthSpeed & 0x0F
		}
	}

	// Pulse modulation
	if c.pulseSpeed != 0 {
		if c.pulseDir == 0 {
			newLo := int(c.pulseLo) + int(c.pulseSpeed)
			carry := byte(0)
			if newLo > 255 {
				carry = 1
			}
			c.pulseLo = byte(newLo)
			newHi := int(c.pulseHi) + int(carry)
			if newHi >= int(c.pulseLimitUp) && newHi > int(c.pulseLimitUp) {
				c.pulseDir = 0x80
				c.pulseLo = 0xFF
				c.pulseHi = c.pulseLimitUp
			} else {
				c.pulseHi = byte(newHi)
			}
		} else {
			newLo := int(c.pulseLo) - int(c.pulseSpeed)
			borrow := byte(0)
			if newLo < 0 {
				borrow = 1
				newLo += 256
			}
			c.pulseLo = byte(newLo)
			newHi := int(c.pulseHi) - int(borrow)
			if newHi < int(c.pulseLimitDown) {
				c.pulseDir = 0
				c.pulseLo = 0
				c.pulseHi = c.pulseLimitDown
			} else {
				c.pulseHi = byte(newHi)
			}
		}
	}

	// Portamento (effect 3)
	if c.effect == 3 {
		speedLo := c.param & 0xF0
		speedHi := c.param & 0x0F
		currFreq := int(c.freqLo) | (int(c.freqHi) << 8)
		targetFreq := int(c.noteFreqLo) | (int(c.noteFreqHi) << 8)
		speed := int(speedLo) | (int(speedHi) << 8)

		if currFreq < targetFreq {
			newFreq := currFreq + speed
			if newFreq >= targetFreq {
				c.freqLo = c.noteFreqLo
				c.freqHi = c.noteFreqHi
			} else {
				c.freqLo = byte(newFreq)
				c.freqHi = byte(newFreq >> 8)
			}
		} else if currFreq > targetFreq {
			newFreq := currFreq - speed
			if newFreq <= targetFreq {
				c.freqLo = c.noteFreqLo
				c.freqHi = c.noteFreqHi
			} else {
				c.freqLo = byte(newFreq)
				c.freqHi = byte(newFreq >> 8)
			}
		}
	}

	// Slide effect (effect B in original GT - but verify)
	if c.effect == 0xB {
		c.slideEnable = 0x80
		if c.param == 0 {
			newLo := int(c.slideDeltaLo) + 0x20
			if newLo > 255 {
				c.slideDeltaHi++
			}
			c.slideDeltaLo = byte(newLo)
		} else {
			newLo := int(c.slideDeltaLo) - 0x20
			if newLo < 0 {
				c.slideDeltaHi--
			}
			c.slideDeltaLo = byte(newLo)
		}
	}

	// Apply slide
	if c.slideEnable != 0 {
		newLo := int(c.freqLo) + int(c.slideDeltaLo)
		carry := 0
		if newLo > 255 {
			carry = 1
		}
		c.freqLo = byte(newLo)
		newHi := int(c.freqHi) + int(c.slideDeltaHi) + carry
		c.freqHi = byte(newHi)
	}

	// Apply vibrato
	if c.vibDepth == 0 {
		c.finFreqLo = c.freqLo
		c.finFreqHi = c.freqHi
	} else {
		pos := c.vibPos & 0x1F
		if pos >= 0x10 {
			pos = pos ^ 0x1F
		}
		depthRow := int(c.vibDepth>>4) - 1
		if depthRow < 0 || depthRow >= len(vibratoTable) {
			depthRow = 0
		}
		vibOffset := int(vibratoTable[depthRow][pos]) * 2

		freq := int(c.freqLo) | (int(c.freqHi) << 8)
		if c.vibPos&0x20 != 0 {
			freq += vibOffset
		} else {
			freq -= vibOffset
		}
		if freq < 0 {
			freq = 0
		}
		if freq > 0xFFFF {
			freq = 0xFFFF
		}
		c.finFreqLo = byte(freq)
		c.finFreqHi = byte(freq >> 8)
		c.vibPos += c.vibSpeed
	}
}

func (vp *OriginalVP) processFilter() {
	if vp.filterIdx == 0 {
		return
	}
	if int(vp.filterIdx) < len(vp.song.FilterTable) {
		vp.filterCutoff = vp.song.FilterTable[vp.filterIdx]
	}
	vp.filterIdx++
	if vp.filterIdx > vp.filterEnd {
		vp.filterIdx = vp.filterLoop
	}
}

func (vp *OriginalVP) processEffect(ch int, effect, param byte) {
	c := &vp.chn[ch]

	// Original GT effects:
	// 0 = none
	// 1 = portamento up (slide up)
	// 2 = portamento down (slide down)
	// 3 = tone portamento (glide to note)
	// 4 = vibrato
	// 7 = set waveform (but in these songs, set AD)
	// 8 = set pulse (but in these songs, set SR)
	// 9 = set AD (but in these songs, set waveform)
	// A = set SR
	// B = slide/filter
	// D = pattern break
	// E = set HR timing
	// F = various sub-effects

	switch effect {
	case 0:
		// No effect
	case 1:
		// Portamento up / slide up - every frame
		// In these GT songs, this seems to be pattern arpeggio
		// Check row_remap.go for clues
	case 2:
		// Portamento down / slide down
	case 3:
		// Tone portamento - handled in processInstrument
	case 4:
		// Vibrato - set vibrato params
		c.vibDepth = param & 0xF0
		c.vibSpeed = param & 0x0F
	case 7:
		// In standard GT: set waveform
		// In these songs: set AD (based on VP comments)
		c.ad = param
	case 8:
		// In standard GT: set pulse width
		// In these songs: set SR (based on VP comments)
		c.sr = param
	case 9:
		// In standard GT: set AD
		// In these songs: set waveform (based on VP comments)
		c.waveIdx = 255
		c.waveform = param
	case 0xA:
		// Set SR
		c.sr = param
	case 0xB:
		// Slide - handled in processInstrument
		c.slideEnable = 0x80
	case 0xD:
		// Pattern break
		vp.forceNewPattern = true
	case 0xE:
		// Set HR timing
		if vp.speedCounter == 0 {
			c.hardRestart = param
		}
	case 0xF:
		// Various sub-effects based on param
		if param < 0x80 {
			// Fxx (00-7F) = speed
			if param > 0 && vp.speedCounter == 0 {
				vp.speed = int(param)
			}
		} else {
			hiNib := param & 0xF0
			loNib := param & 0x0F
			switch hiNib {
			case 0xB0:
				// FBx = fineslide
				if vp.speedCounter == 0 {
					newLo := int(c.slideDeltaLo) + 0x04
					if newLo > 255 {
						c.slideDeltaHi++
					}
					c.slideDeltaLo = byte(newLo)
					c.slideEnable = 0x80
				}
			case 0xF0:
				// FFx = hard restart timing
				if vp.speedCounter == 0 {
					c.hardRestart = loNib
				}
			case 0xE0:
				// FEx = filter trigger
				if vp.speedCounter == 0 && loNib > 0 {
					instIdx := int(loNib)
					if instIdx < len(vp.song.Instruments) {
						inst := vp.song.Instruments[instIdx]
						vp.filterIdx = inst.FilterStart
						vp.filterEnd = inst.FilterEnd
						vp.filterLoop = inst.FilterLoop
					}
				}
			case 0x80:
				// F8x = global volume
				if vp.speedCounter == 0 {
					vp.globalVolume = loNib
				}
			case 0x90:
				// F9x = filter mode
				if vp.speedCounter == 0 {
					vp.filterMode = loNib << 4
				}
			}
		}
	}
}

func (vp *OriginalVP) checkHardRestart(ch int) {
	var hrRow int
	var hrOrder int
	if vp.forceNewPattern {
		hrRow = 0
		hrOrder = vp.nextOrder
	} else {
		hrRow = vp.row + 1
		hrOrder = vp.order
		if hrRow >= 64 {
			hrRow = 0
			hrOrder = vp.nextOrder
		}
	}

	row := vp.getRowForOrder(ch, hrOrder, hrRow)
	if row == nil {
		return
	}

	note := row.Note
	if note == 0 || note == 0x7F {
		return
	}

	// Effect 3 = tone portamento in original, skip HR
	if row.Effect == 3 {
		return
	}

	vp.doHardRestart(ch)
}

func (vp *OriginalVP) getRowForOrder(ch, orderNum, rowNum int) *parse.Row {
	if orderNum >= len(vp.song.Orders[ch]) {
		return nil
	}
	patAddr := vp.song.Orders[ch][orderNum].PatternAddr
	pat, ok := vp.song.Patterns[patAddr]
	if !ok {
		return nil
	}
	if rowNum >= 64 {
		return nil
	}
	return &pat.Rows[rowNum]
}

func (vp *OriginalVP) doHardRestart(ch int) {
	vp.chn[ch].waveform = 0
	vp.chn[ch].ad = 0
	vp.chn[ch].sr = 0
}

func (vp *OriginalVP) dumpRegisters() {
	sidBase := uint16(0xD400)

	for ch := 0; ch < 3; ch++ {
		c := &vp.chn[ch]
		chBase := sidBase + uint16(ch*7)

		vp.writeSID(chBase+2, c.pulseLo)
		vp.writeSID(chBase+3, c.pulseHi)
		vp.writeSID(chBase+0, c.finFreqLo)
		vp.writeSID(chBase+1, c.finFreqHi)
		vp.writeSID(chBase+4, c.waveform&c.gateon)
		vp.writeSID(chBase+5, c.ad)
		vp.writeSID(chBase+6, c.sr)
	}

	vp.writeSID(0xD416, vp.filterCutoff)
	vp.writeSID(0xD417, vp.filterResonance)
	vp.writeSID(0xD418, vp.globalVolume|vp.filterMode)
}

func (vp *OriginalVP) writeSID(addr uint16, val byte) {
	vp.writes = append(vp.writes, SIDWrite{
		Addr:  addr,
		Value: val,
		Frame: vp.currentFrame,
	})
}

// CompareOriginal compares original VP output against GT player output.
func CompareOriginal(origWrites []SIDWrite, song *parse.ParsedSong, frames int, debug bool) (bool, int, string) {
	ovpDebug = debug
	vp := NewOriginalVP(song)
	vpWrites := vp.RunFrames(frames)

	if len(vpWrites) != len(origWrites) {
		return false, 0, fmt.Sprintf("write count: vp=%d orig=%d", len(vpWrites), len(origWrites))
	}

	for i := 0; i < len(origWrites); i++ {
		if vpWrites[i] != origWrites[i] {
			if debug {
				// Show first 24 writes comparison
				fmt.Printf("\n=== First 24 writes comparison ===\n")
				for j := 0; j < 24 && j < len(origWrites); j++ {
					marker := " "
					if vpWrites[j] != origWrites[j] {
						marker = "X"
					}
					fmt.Printf("  %s %2d: vp=$%04X=%02X orig=$%04X=%02X\n",
						marker, j, vpWrites[j].Addr, vpWrites[j].Value, origWrites[j].Addr, origWrites[j].Value)
				}
			}
			return false, i, fmt.Sprintf("vp=$%04X=%02X orig=$%04X=%02X f=%d",
				vpWrites[i].Addr, vpWrites[i].Value, origWrites[i].Addr, origWrites[i].Value, origWrites[i].Frame)
		}
	}

	return true, len(origWrites), ""
}
