; ============================================================================
; Standalone build of odin_player.inc for testing
; Entry points: +$00 init (A=song, X=buffer), +$03 play
; ============================================================================

.setcpu "6502"

.segment "CODE"

.include "../../src/odin_player.inc"

; Checkpoint stub for standalone player (no-op)
; Important: Always assume A and P are modified by JSR checkpoint
checkpoint:
        rts
