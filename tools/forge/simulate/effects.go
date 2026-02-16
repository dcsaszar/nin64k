package simulate

// Player effect numbers - after remapping (as shown in odin_player.inc)
const (
	PlayerEffectSpecial  = 0  // 0=nop, 1=vib off, 2=break, 3=fineslide
	PlayerEffectArp      = 1  // Pattern arpeggio
	PlayerEffectPorta    = 2  // Tone portamento (slide to note)
	PlayerEffectSpeed    = 3  // Set speed
	PlayerEffectHrdRest  = 4  // Hard restart timing
	PlayerEffectFiltTrig = 5  // Filter trigger
	PlayerEffectSR       = 6  // Set SR envelope
	PlayerEffectWave     = 7  // Set waveform
	PlayerEffectPulse    = 8  // Set pulse width
	PlayerEffectAD       = 9  // Set AD envelope
	PlayerEffectReso     = 10 // Filter resonance
	PlayerEffectSlide    = 11 // Pitch slide (accumulates delta)
	PlayerEffectGlobVol  = 12 // Global volume
	PlayerEffectFiltMode = 13 // Filter mode
	PlayerEffectPermArp  = 14 // Permanent arpeggio (reserved)
	PlayerEffectPermArp2 = 15 // Alias for permanent arpeggio
)

// Player effect 0 param values
const (
	PlayerParam0Nop       = 0 // NOP - do nothing
	PlayerParam0VibOff    = 1 // Disable vibrato
	PlayerParam0Break     = 2 // Pattern break
	PlayerParam0FineSlide = 3 // Fine pitch slide
	PlayerParam0NopHard   = 4 // NOP that clears permarp
)

// GT (GoatTracker) source effect numbers - for original player validation
const (
	GTEffectTonePorta = 0x3 // Tone portamento (slide to note)
	GTEffectSlide     = 0xB // Pitch slide in original GT
)
