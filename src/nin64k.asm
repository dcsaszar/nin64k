; ============================================================================
; Nine Inch Ninjas - Standalone PRG player
; ============================================================================

.setcpu "6502"

; Zero page
zp_load_flag     = $79
zp_part_num     = $7B

; Decompressor zero page (external interface)
zp_src_lo       = $02
zp_src_hi       = $03
zp_bitbuf       = $04
zp_out_lo       = $05
zp_out_hi       = $06

; State
zp_last_line    = $0d

; Tune entry points
TUNE1_INIT      = $1000
TUNE1_PLAY      = $1003
TUNE2_INIT      = $7000
TUNE2_PLAY      = $7003

.segment "LOADADDR"
        .word   $0801

.segment "CODE"


; ----------------------------------------------------------------------
; BASIC stub: SYS 2066
; ----------------------------------------------------------------------
basic_stub:
        .word   $0810               ; Pointer to next BASIC line
        .word   8580                ; Line number
        .byte   $9E                 ; SYS token
        .byte   "2061"              ; SYS address + decoration
        .byte   $00                 ; End of line
        .word   $0000               ; End of BASIC program

; ----------------------------------------------------------------------------
start:
        jsr     init_game
        lda     #1
        sta     zp_part_num
        jsr     setup_irq
        jsr     init_stream
        lda     #0
        jsr     TUNE1_INIT

; ----------------------------------------------------------------------------
main_loop:
        jsr     checkpoint
        lda     #$35
        sta     $01
        lda     zp_load_flag
        bne     do_load_next
        ; Direct keyboard read (CIA1 at $DC00/$DC01, I/O already banked in)
        lda     #$7F                ; Select row 7
        sta     $DC00
        lda     $DC01
        and     #$10                ; Check bit 4 (space bar)
        bne     main_loop           ; Not pressed
@debounce:
        jsr     checkpoint
        lda     #$35
        sta     $01
        lda     $DC01               ; Wait for release
        and     #$10
        beq     @debounce
        ; Reset SID
        ldx     #$18
        lda     #$00
@sid:   sta     $D400,x
        dex
        bpl     @sid
        ; Advance to next song
        lda     zp_part_num
        cmp     #$09                ; Stop at song 9
        beq     main_loop
        lda     #$FF
        sta     zp_load_flag
        inc     zp_part_num
        jmp     main_loop

checkpoint:
        lda     #$35
        sta     $01
        rol     $D020
        lda     $D012
        bmi     @no_vblank
        cmp     zp_last_line
        bpl     @no_vblank
        jsr     play_tick_if_ready
        lda     #0
@no_vblank:
        sta     zp_last_line
        lda     #$30
        sta     $01
        rts

; ----------------------------------------------------------------------------
do_load_next:
        lda     #$CC
        sta     $0427
        jsr     load_and_init
        lda     #$00
        sta     zp_load_flag
        lda     #$20
        sta     $0427
        jmp     main_loop

; ----------------------------------------------------------------------------
setup_irq:
        sei
        lda     #$35                ; I/O on, KERNAL off
        sta     $01
        lda     #$7F
        sta     $DC0D               ; Disable CIA interrupts
        lda     $DC0D               ; Acknowledge pending CIA
        cli
        rts

; ----------------------------------------------------------------------------

play_tick_if_ready:
        lda     #$07
        sta     $D020
        txa
        pha
        tya
        pha
        jsr     play_tick
        pla
        tay
        pla
        tax
        lda     #$00
        sta     $D020
        rts

play_tick:
        lda     zp_part_num
        and     #$01
        beq     @play_even
        jsr     $1003
        jmp     check_countdown

@play_even:
        jsr     $7003

check_countdown:
        lda     zp_part_num
        asl     a
        tax
        dex
        dex
        lda     #$FF
        clc
        adc     part_times,x
        sta     part_times,x
        lda     #$FF
        cmp     part_times,x
        bne     @check_zero
        dec     part_times+1,x
@check_zero:
        lda     part_times,x
        bne     play_done
        lda     part_times+1,x
        bne     play_done
        lda     zp_part_num
        cmp     #$09                ; Stop at song 9
        beq     play_done
        lda     #$FF
        sta     zp_load_flag
        inc     zp_part_num

; ----------------------------------------------------------------------------
play_done:
        rts

; ----------------------------------------------------------------------
; Copy streams wrapper with banking
; ----------------------------------------------------------------------
copy_streams_banked:
        sei                         ; Must disable IRQs when KERNAL banked out
        lda     #$30                ; All RAM (including $D000-$DFFF, no I/O)
        sta     $01
        jsr     copy_streams
        lda     #$37                ; Restore ROMs
        sta     $01
        cli                         ; Re-enable IRQs
        rts

; ----------------------------------------------------------------------------
load_and_init:
        lda     zp_part_num
        cmp     #$09
        bcs     @done
        jsr     decompress_next
        lda     #$A1
        sta     $0427
        lda     zp_part_num
        and     #$01
        bne     @init_buf2
        lda     #$00
        jmp     TUNE1_INIT
@init_buf2:
        lda     #$00
        jmp     TUNE2_INIT
@done:
        rts

; ----------------------------------------------------------------------
; Part timing data (decremented in place during playback)
; ----------------------------------------------------------------------
part_times:
.include "part_times.inc"

; ----------------------------------------------------------------------------
; Initialize stream pointer to song 2 (song 1 is pre-decompressed at $1000)
; ----------------------------------------------------------------------------
init_stream:
        lda     #<(STREAM_MAIN_DEST + 1)
        sta     zp_src_lo
        lda     #>(STREAM_MAIN_DEST + 1)
        sta     zp_src_hi
        lda     STREAM_MAIN_DEST
        asl     a
        asl     a
        asl     a
        asl     a
        asl     a
        ora     #$10
        sta     zp_bitbuf
        rts

; ----------------------------------------------------------------------------
; Decompress next song from stream
; ----------------------------------------------------------------------------
decompress_next:
        ; Set output destination based on part number
        lda     #$00
        sta     zp_out_lo
        lda     zp_part_num
        and     #$01
        beq     @even_part
        lda     #$70                ; Odd part num -> $7000
        bne     @do_decompress
@even_part:
        lda     #$10                ; Even part num -> $1000
@do_decompress:
        sta     zp_out_hi
        ; Call decompressor in all-RAM mode (stream spans I/O region)
        lda     #$30                ; All RAM
        sta     $01
        jsr     decompress
        ; Song 9 is split: stream_main has first part, stream_tail has rest
        lda     zp_part_num
        cmp     #$08                ; Song 9 = part 8
        bne     @load_done
        lda     #<STREAM_TAIL_DEST
        sta     zp_src_lo
        lda     #>STREAM_TAIL_DEST
        sta     zp_src_hi
        lda     #$80
        sta     zp_bitbuf
        jsr     decompress
@load_done:
        lda     #$35                ; Back to I/O mode
        sta     $01
        clc                         ; Success
        rts

; ============================================================================
; V23 Decompressor - generated by ./compress
; Setup: zp_src, zp_bitbuf=$80, zp_out ($1000 or $7000)
; ============================================================================
.include "../generated/decompress.asm"

; ----------------------------------------------------------------------------
init_game:
        jsr     copy_streams_banked
        lda     #$FF
        sta     zp_load_flag
        rts

.segment "PART1"
.incbin "../generated/part1.bin"

.segment "DATA"
STREAM_OFFSET = 4997
.include "stream.inc"
