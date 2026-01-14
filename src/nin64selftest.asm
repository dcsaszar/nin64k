; ============================================================================
; SounDemoN "Ninjas" - Clean Disassembly
; ============================================================================
;
; Original file: nin-soundemo
; Load address:  $0801
; Size:          2047 bytes
;
; Memory layout:
;   $0801-$080C  BASIC stub
;   $080D-$0A74  Main code
;   $0A75-$0BEC  Menu text and data
;   $0BED-$0BFF  Part timing data
;   $0C00-$0D1C  Disk loader code
;   $0D1D-$0E4F  1541 drive code
;   $0E50-$0E5F  Free space (for patches)
;   $0E60-$0F7F  Decompression routine
;   $0F80-$0FFF  Info screen and init
;
; Key variables:
;   $78 - Selected part from menu
;   $79 - Load next part flag (non-zero = load)
;   $7B - Current part number (1-9)
;
; Tune buffers:
;   $1000 - Buffer 1 (odd parts: 1,3,5,7,9)
;   $7000 - Buffer 2 (even parts: 2,4,6,8)
;
; ============================================================================

.setcpu "6502"

; Zero page
zp_selected     = $78
zp_load_flag    = $79
zp_part_num     = $7B
; Selftest zero page ($D9-$E6 to avoid player's $FB-$FE)
zp_csum_lo      = $D9
zp_csum_hi      = $DA
zp_size_lo      = $DB
zp_size_hi      = $DC
zp_song_idx     = $DD
zp_screen_lo    = $DE
zp_screen_hi    = $DF
zp_copy_rem     = $E0
zp_ptr_lo       = $E1
zp_ptr_hi       = $E2
zp_copy_src_lo  = $E3
zp_copy_src_hi  = $E4
zp_copy_dst_lo  = $E5
zp_copy_dst_hi  = $E6

; Decompressor zero page (external interface)
zp_src_lo       = $02
zp_src_hi       = $03
zp_bitbuf       = $04
zp_out_lo       = $05
zp_out_hi       = $06

; Stream destinations for in-place decompression (see README.md)
; stream_main goes to high memory (ends at $FFFD, leaving $FFFE-$FFFF for IRQ vector)
STREAM_MAIN_DEST = $10000 - (stream_tail - stream_main) - 2
STREAM_TAIL_DEST = $663B

; Hardware
VIC_D011        = $D011
VIC_D012        = $D012
VIC_D018        = $D018
VIC_D019        = $D019
VIC_D01A        = $D01A
VIC_D020        = $D020
VIC_D021        = $D021
CIA1_DC0D       = $DC0D
CIA2_DD00       = $DD00

; KERNAL
SCNKEY          = $FF9F
GETIN           = $FFE4
CHROUT          = $FFD2
IRQ_RETURN      = $EA31

; Tune entry points (jump vectors copied here)
TUNE1_BASE      = $1000
TUNE1_INIT      = $1000
TUNE1_PLAY      = $1003
TUNE2_BASE      = $7000
TUNE2_INIT      = $7000
TUNE2_PLAY      = $7003

; Player code starts at $1009/$7009, data at $198C/$798C
TUNE1_PLAYER    = $1009
TUNE1_DATA      = $198C
TUNE2_PLAYER    = $7009
TUNE2_DATA      = $798C

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
        .byte   "2066 NIN!"         ; SYS address + decoration
        .byte   $00                 ; End of line
        .word   $0000               ; End of BASIC program

; ----------------------------------------------------------------------------
start:
        jmp     selftest            ; Run selftest instead of normal startup

start_normal:
        jsr     init_game
        sta     $7B
;       jsr     copy_players        ; Disabled: PRG overlaps $1009
        jsr     setup_irq
        lda     #<msg_loading
        sta     $8C
        lda     #>msg_loading
        sta     $8D
        jsr     print_msg
        jsr     load_d0
        jsr     load_and_init
        inc     $7B
        lda     #<msg_title
        sta     $8C
        lda     #>msg_title
        sta     $8D
        jsr     print_msg
        lda     #$80
        sta     $028A

; ----------------------------------------------------------------------------
main_loop:
        lda     $79
        bne     do_load_next
        jsr     SCNKEY
        jsr     GETIN
        cmp     #$20                ; Space bar
        bne     main_loop
        ; Advance to next song
        lda     zp_part_num
        cmp     #$07                ; Stop at song 7
        beq     main_loop
        lda     #$FF
        sta     zp_load_flag
        inc     zp_part_num
        jmp     main_loop

; ----------------------------------------------------------------------------
do_load_next:
        lda     #$CC
        sta     $0427
        jsr     load_and_init
        lda     #$00
        sta     $79
        lda     #$20
        sta     $0427
        jmp     main_loop

; ----------------------------------------------------------------------------
setup_irq:
        sei
        lda     #$36
        sta     $01
        lda     #$7F
        sta     $DC0D
        lda     #$01
        sta     $D01A
        lda     $D011
        and     #$7F
        sta     $D011
        lda     #$33
        sta     $D012
        lda     $DC0D
        lda     #<irq_handler
        sta     $0314
        lda     #>irq_handler
        sta     $0315
        cli
        rts

; ----------------------------------------------------------------------------
irq_handler:
        pha
        lda     $01
        pha
        txa
        pha
        tya
        pha
        lda     #$35                ; Ensure I/O visible
        sta     $01
        lda     $D019
        sta     $D019
        lda     #$16
        sta     $D018
        lda     $7B
        cmp     $78
        beq     L8B8
        lda     #$07
        sta     $D020
        jsr     play_tick
        lda     #$00
        sta     $D020

; ----------------------------------------------------------------------------
L8B8:
        pla
        tay
        pla
        tax
        pla
        sta     $01
        pla
        rti

; ----------------------------------------------------------------------------
play_tick:
        lda     $7B
        and     #$01
        beq     L8CC
        jsr     $1003
        jmp     check_countdown

; ----------------------------------------------------------------------------
L8CC:
        jsr     $7003

; ----------------------------------------------------------------------------
check_countdown:
        lda     $7B
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
        bne     L8E8
        dec     part_times+1,x

; ----------------------------------------------------------------------------
L8E8:
        lda     part_times,x
        bne     play_done
        lda     part_times+1,x
        bne     play_done
        lda     $7B
        cmp     #$07                ; Stop at song 7 (limited disk space)
        beq     play_done
        lda     #$FF
        sta     $79
        inc     $7B

; ----------------------------------------------------------------------------
play_done:
        rts

; ----------------------------------------------------------------------
; Copy compressed streams to destinations for in-place decompression
; stream_main -> high memory (under ROMs)
; stream_tail -> $663B-$6FFF (buffer A tail)
; NOTE: This routine must be located BEFORE $663B to avoid self-overwrite
; ----------------------------------------------------------------------
STREAM_MAIN_SIZE = stream_tail - stream_main
STREAM_TAIL_SIZE = stream_end - stream_tail

copy_streams:
        sei                         ; Must disable IRQs when KERNAL banked out
        lda     #$30                ; All RAM (including $D000-$DFFF, no I/O)
        sta     $01

        ; Copy stream_main to high memory
        lda     #<stream_main
        sta     zp_copy_src_lo
        lda     #>stream_main
        sta     zp_copy_src_hi
        lda     #<STREAM_MAIN_DEST
        sta     zp_copy_dst_lo
        lda     #>STREAM_MAIN_DEST
        sta     zp_copy_dst_hi
        ldx     #>STREAM_MAIN_SIZE
        lda     #<STREAM_MAIN_SIZE
        sta     zp_copy_rem
        jsr     copy_bytes_bwd

        ; Copy stream_tail to buffer A tail (forward copy - dst < src overlap)
        lda     #<stream_tail
        sta     zp_copy_src_lo
        lda     #>stream_tail
        sta     zp_copy_src_hi
        lda     #<STREAM_TAIL_DEST
        sta     zp_copy_dst_lo
        lda     #>STREAM_TAIL_DEST
        sta     zp_copy_dst_hi
        lda     #<STREAM_TAIL_SIZE
        sta     zp_size_lo
        lda     #>STREAM_TAIL_SIZE
        sta     zp_size_hi
        jsr     copy_bytes_fwd

        lda     #$37                ; Restore ROMs
        sta     $01
        cli                         ; Re-enable IRQs
        rts

; Copy X full pages + remainder bytes, backwards (safe for overlapping regions)
; Inputs: zp_copy_src, zp_copy_dst point to START, X = full pages, zp_copy_rem = remainder
copy_bytes_bwd:
        txa
        clc
        adc     zp_copy_src_hi
        sta     zp_copy_src_hi
        txa
        clc
        adc     zp_copy_dst_hi
        sta     zp_copy_dst_hi
        ; Now pointers are at last page, copy remainder first
        ldy     zp_copy_rem
        beq     @pages
        dey
@rem_loop:
        lda     (zp_copy_src_lo),y
        sta     (zp_copy_dst_lo),y
        dey
        cpy     #$FF
        bne     @rem_loop
@pages:
        cpx     #0
        beq     @done
@page_loop:
        dec     zp_copy_src_hi
        dec     zp_copy_dst_hi
        ldy     #$FF
@byte_loop:
        lda     (zp_copy_src_lo),y
        sta     (zp_copy_dst_lo),y
        dey
        cpy     #$FF
        bne     @byte_loop
        dex
        bne     @page_loop
@done:  rts

; Copy bytes forward (safe for overlapping regions when dst < src)
; Inputs: zp_copy_src, zp_copy_dst point to START, zp_size = byte count
copy_bytes_fwd:
        ldy     #$00
@fwd_loop:
        lda     zp_size_lo
        ora     zp_size_hi
        beq     @fwd_done
        lda     (zp_copy_src_lo),y
        sta     (zp_copy_dst_lo),y
        ; Increment source
        inc     zp_copy_src_lo
        bne     @no_src_carry
        inc     zp_copy_src_hi
@no_src_carry:
        ; Increment dest
        inc     zp_copy_dst_lo
        bne     @no_dst_carry
        inc     zp_copy_dst_hi
@no_dst_carry:
        ; Decrement size
        lda     zp_size_lo
        bne     @dec_size_lo
        dec     zp_size_hi
@dec_size_lo:
        dec     zp_size_lo
        jmp     @fwd_loop
@fwd_done:
        rts

; ----------------------------------------------------------------------
; RAM-based IRQ handler for all-RAM mode ($30)
; When KERNAL is banked out, we need our own handler in RAM.
; This just acknowledges the CIA interrupt and returns.
; ----------------------------------------------------------------------
ram_irq:
        pha
        lda     $01
        pha
        lda     #$35            ; Enable I/O only (KERNAL stays banked out)
        sta     $01
        lda     $DC0D           ; Acknowledge CIA1 interrupt
        lda     $D019
        sta     $D019               ; Acknowledge VIC interrupt
        inc     $d020
        pla
        sta     $01
        pla
        rti

; Set up RAM IRQ vector (call in $30 mode with SEI)
setup_ram_irq:
        lda     #<ram_irq
        sta     $FFFE
        lda     #>ram_irq
        sta     $FFFF
        rts

; ----------------------------------------------------------------------
; Print 16-bit hex word from zp_csum at screen position Y
; (Placed here to stay below $1000 - called during selftest)
; ----------------------------------------------------------------------
print_hex_word:
        lda     zp_csum_hi
        jsr     @print_hex_byte
        lda     zp_csum_lo
@print_hex_byte:
        pha
        lsr     a
        lsr     a
        lsr     a
        lsr     a
        jsr     @print_nibble
        pla
        and     #$0F
@print_nibble:
        cmp     #$0A
        bcc     @digit
        adc     #$06
@digit:
        adc     #$30
        sta     (zp_screen_lo),y
        iny
        rts

; ----------------------------------------------------------------------
; Calculate checksum of decompressed output
; ----------------------------------------------------------------------
selftest_output_checksum:
        ldx     zp_song_idx
        txa
        asl     a
        tax
        lda     selftest_sizes,x
        sta     zp_size_lo
        lda     selftest_sizes+1,x
        sta     zp_size_hi
        lda     zp_song_idx
        and     #$01
        bne     @out_odd
        lda     #$10
        bne     @out_set
@out_odd:
        lda     #$70
@out_set:
        sta     zp_ptr_hi
        lda     #$00
        sta     zp_ptr_lo
        sei
        lda     #$35
        sta     $01
        jsr     calc_checksum
        lda     #$37
        sta     $01
        cli
        rts

; ----------------------------------------------------------------------
; Calculate 16-bit additive checksum
; Input: zp_ptr = start address, zp_size = byte count
; Output: zp_csum = checksum
; ----------------------------------------------------------------------
calc_checksum:
        lda     #$00
        sta     zp_csum_lo
        sta     zp_csum_hi
        ldy     #$00
@csum_loop:
        lda     zp_size_lo
        ora     zp_size_hi
        beq     @csum_done
        lda     (zp_ptr_lo),y
        clc
        adc     zp_csum_lo
        sta     zp_csum_lo
        bcc     @no_carry
        inc     zp_csum_hi
@no_carry:
        inc     zp_ptr_lo
        bne     @no_ptr_carry
        inc     zp_ptr_hi
@no_ptr_carry:
        lda     zp_size_lo
        bne     @dec_lo
        dec     zp_size_hi
@dec_lo:
        dec     zp_size_lo
        jmp     @csum_loop
@csum_done:
        rts

; ----------------------------------------------------------------------
; Verify stream_main checksum (placed here to stay below $1000)
; ----------------------------------------------------------------------
selftest_verify_main:
        ldy     #0
        lda     #char_m
        sta     (zp_screen_lo),y
        iny
        lda     #char_a
        sta     (zp_screen_lo),y
        iny
        lda     #char_i
        sta     (zp_screen_lo),y
        iny
        lda     #char_n
        sta     (zp_screen_lo),y
        iny
        lda     #char_colon
        sta     (zp_screen_lo),y
        iny
        sty     zp_copy_rem
        lda     #<STREAM_MAIN_DEST
        sta     zp_ptr_lo
        lda     #>STREAM_MAIN_DEST
        sta     zp_ptr_hi
        lda     #<STREAM_MAIN_SIZE
        sta     zp_size_lo
        lda     #>STREAM_MAIN_SIZE
        sta     zp_size_hi
        sei
        lda     #$30
        sta     $01
        jsr     calc_checksum
        lda     #$37
        sta     $01
        cli
        lda     zp_csum_lo
        cmp     selftest_stream_main_csum
        bne     @fail_main
        lda     zp_csum_hi
        cmp     selftest_stream_main_csum+1
        bne     @fail_main
        ldy     zp_copy_rem
        lda     #char_o
        sta     (zp_screen_lo),y
        iny
        lda     #char_k
        sta     (zp_screen_lo),y
        jmp     @done_main
@fail_main:
        ldy     zp_copy_rem
        jsr     print_hex_word
@done_main:
        lda     zp_screen_lo
        clc
        adc     #40
        sta     zp_screen_lo
        bcc     @nc_m
        inc     zp_screen_hi
@nc_m:  rts

; ----------------------------------------------------------------------
; Verify stream_tail checksum
; ----------------------------------------------------------------------
selftest_verify_tail:
        ldy     #0
        lda     #char_t
        sta     (zp_screen_lo),y
        iny
        lda     #char_a
        sta     (zp_screen_lo),y
        iny
        lda     #char_i
        sta     (zp_screen_lo),y
        iny
        lda     #char_l
        sta     (zp_screen_lo),y
        iny
        lda     #char_colon
        sta     (zp_screen_lo),y
        iny
        sty     zp_copy_rem
        lda     #<STREAM_TAIL_DEST
        sta     zp_ptr_lo
        lda     #>STREAM_TAIL_DEST
        sta     zp_ptr_hi
        lda     #<STREAM_TAIL_SIZE
        sta     zp_size_lo
        lda     #>STREAM_TAIL_SIZE
        sta     zp_size_hi
        sei
        jsr     calc_checksum
        cli
        lda     zp_csum_lo
        cmp     selftest_stream_tail_csum
        bne     @fail_tail
        lda     zp_csum_hi
        cmp     selftest_stream_tail_csum+1
        bne     @fail_tail
        ldy     zp_copy_rem
        lda     #char_o
        sta     (zp_screen_lo),y
        iny
        lda     #char_k
        sta     (zp_screen_lo),y
        jmp     @done_tail
@fail_tail:
        ldy     zp_copy_rem
        jsr     print_hex_word
@done_tail:
        lda     zp_screen_lo
        clc
        adc     #40
        sta     zp_screen_lo
        bcc     @nc_t
        inc     zp_screen_hi
@nc_t:  rts

; ----------------------------------------------------------------------------
clear_screen:
        lda     #$00
        sta     $8C
        sta     $8E
        lda     #$04
        sta     $8D
        lda     #$D8
        sta     $8F
        ldy     #$00

; ----------------------------------------------------------------------------
L90F:
        lda     #$20
        sta     ($8C),y
        lda     #$0E
        sta     ($8E),y
        lda     #$00
        sec
        adc     $8C
        sta     $8C
        lda     #$00
        adc     $8D
        sta     $8D
        lda     #$00
        sec
        adc     $8E
        sta     $8E
        lda     #$00
        adc     $8F
        sta     $8F
        lda     #$E8
        cmp     $8C
        bne     L90F
        lda     #$07
        cmp     $8D
        bne     L90F
        rts

; ----------------------------------------------------------------------------
print_msg:
        lda     $8C
        pha
        lda     $8D
        pha
        jsr     clear_screen
        pla
        sta     $8D
        pla
        sta     $8C

; ----------------------------------------------------------------------------
print_string:
        lda     #$00
        sta     $8E
        lda     #$04
        sta     $8F
        lda     #$28
        sta     $7A
        ldy     #$00
print_loop:
        lda     ($8C),y
        beq     print_done
        cmp     #$0D
        bne     L977
        lda     $7A
        clc
        adc     $8E
        sta     $8E
        lda     #$00
        adc     $8F
        sta     $8F
        lda     #$28
        sta     $7A
        jmp     advance_src

; ----------------------------------------------------------------------------
L977:
        cmp     #$41
        bcc     L982
        cmp     #$5B
        bcs     L982
        sec
        sbc     #$40

; ----------------------------------------------------------------------------
L982:
        cmp     #$C1
        bcc     L98D
        cmp     #$DB
        bcs     L98D
        sec
        sbc     #$80

; ----------------------------------------------------------------------------
L98D:
        sta     ($8E),y
        dec     $7A
        bne     L997
        lda     #$28
        sta     $7A

; ----------------------------------------------------------------------------
L997:
        lda     #$00
        sec
        adc     $8E
        sta     $8E
        lda     #$00
        adc     $8F
        sta     $8F
advance_src:
        lda     #$00
        sec
        adc     $8C
        sta     $8C
        lda     #$00
        adc     $8D
        sta     $8D
        jmp     print_loop

; ----------------------------------------------------------------------------
print_done:
        rts

; ----------------------------------------------------------------------------
load_and_init:
        lda     $7B
        cmp     #$07                ; Stop at song 7 (limited disk space)
        bne     L9BC
        rts

; ----------------------------------------------------------------------------
L9BC:
        ldx     #$44
        lda     $7B
        clc
        adc     #$31
        tay
        jsr     load_tune
        bcs     load_error
        lda     #$A1
        sta     $0427
        lda     $7B
        and     #$01
        bne     init_buf2

; ----------------------------------------------------------------------------
; init_buf1: Called for EVEN $7B (0,2,4,6,8) → loads ODD files to $1000
init_buf1:
        lda     #$60
        sta     $105C               ; Patch stop routine
        lda     #$00
        jsr     TUNE1_INIT
load_done:
        rts

; ----------------------------------------------------------------------------
; init_buf2: Called for ODD $7B (1,3,5,7) → loads EVEN files to $7000
init_buf2:
        lda     #$60
        sta     $705C               ; Patch stop routine
        lda     #$00
        jsr     TUNE2_INIT
        jmp     load_done

; ----------------------------------------------------------------------------
load_error:
        lda     #<msg_error
        sta     $8C
        lda     #>msg_error
        sta     $8D
        jsr     print_msg
        lda     $7B
        asl     a
        tax
        dex
        dex
        lda     #$FF
        sta     part_times,x
        jmp     load_done

; ----------------------------------------------------------------------
; Message text
; ----------------------------------------------------------------------
msg_loading:
        .byte   $CC
        .byte   "OADING..."
        .byte   $00                     ; End of string
msg_error:
        .byte   $CC
        .byte   "OAD ERROR"
        .byte   $00                     ; End of string
msg_title:
        .byte   $CE
        .byte   "INE "
        .byte   $C9
        .byte   "NCH "
        .byte   $CE
        .byte   "INJAS"
        .byte   $0D                     ; CR
        .byte   " BY "
        .byte   $D3
        .byte   $4F, $55, $4E, $C4, $45, $4D, $4F, $CE, $20, $20, $20, $20, $20, $20, $20, $20
        .byte   "     "
        .byte   $22, $0D, $20, $20, $C3, $4F, $50, $59, $52, $49, $47, $48, $54, $28, $43, $29
        .byte   " 2000 "
        .byte   $CF
        .byte   "TTO "
        .byte   $CA
        .byte   "ARVINEN"
        .byte   $0D                     ; CR
        .byte   $0D                     ; CR
        .byte   $C7
        .byte   "REETS TO:"
        .byte   $0D                     ; CR
        .byte   $0D                     ; CR
        .byte   "                 "
        .byte   $22, $20, $20, $20, $22, $0D, $C1, $47, $45, $4D, $49, $58, $45, $52, $20, $20
        .byte   $CD
        .byte   "ARKO "
        .byte   $CD
        .byte   "AKELA"
        .byte   $0D                     ; CR
        .byte   $C1
        .byte   $CD
        .byte   $CA
        .byte   "       "
        .byte   $CD
        .byte   $43, $46, $0D, $C7, $45, $45, $4C, $20, $20, $20, $20, $20, $20, $D2, $4F, $4E
        .byte   $45, $53, $0D, $C7, $52, $55, $45, $20, $20, $20, $20, $20, $20, $D4, $C2, $C2
        .byte   $0D                     ; CR
        .byte   $CA
        .byte   "EFF      "
        .byte   $DA
        .byte   $45, $44, $0D, $CA, $5A, $55, $20, $20, $20, $20, $20, $20, $20, $DA, $49, $4C
        .byte   $4F, $47, $0D, $CC, $4F, $4C, $CF, $CC, $CF, $4C, $4F, $00

; ----------------------------------------------------------------------
; Part timing data (9 parts x 2 bytes)
; ----------------------------------------------------------------------
part_times:
        .word   $BB44
        .word   $7234
        .word   $57C0
        .word   $0000
        .word   $B90A
        .word   $79F6
        .word   $491A
        .word   $7BF0
        .word   $0100
        brk

; ----------------------------------------------------------------------------
load_d0:
        jmp     load_d0_impl

; ----------------------------------------------------------------------------
load_tune:
        jmp     load_tune_impl

; ----------------------------------------------------------------------------
; Initialize stream pointer for in-memory decompression
; ----------------------------------------------------------------------------
load_d0_impl:
        lda     #<STREAM_MAIN_DEST
        sta     zp_src_lo
        lda     #>STREAM_MAIN_DEST
        sta     zp_src_hi
        lda     #$80
        sta     zp_bitbuf
        rts

; ----------------------------------------------------------------------------
; Load tune from memory using in-place decompression
; X, Y = ignored (were filename chars for disk load)
; Returns: C=0 success, C=1 error (not used for memory load)
; ----------------------------------------------------------------------------
load_tune_impl:
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
        ; Call decompressor (reads from zp_src, writes to zp_out)
        ; Decompressor preserves zp_src and zp_bitbuf state for next call
        jsr     decompress
        clc                         ; Success
        rts

; ============================================================================
; V23 Decompressor - generated by ./compress
; Setup: zp_src, zp_bitbuf=$80, zp_out ($1000 or $7000)
; ============================================================================
.include "../generated/decompress.asm"

; ----------------------------------------------------------------------------
; ----------------------------------------------------------------------------
init_game:
        jsr     copy_streams        ; Copy compressed data to safe location
        lda     #$00
        sta     $78                 ; Auto-select part 1 (skip menu)
        sta     $D020
        sta     $D021
        ldx     #$00

; ----------------------------------------------------------------------------
LFCD:
        lda     init_timing_data,x
        sta     part_times,x
        inx
        cpx     #$12
        bne     LFCD
        lda     #$FF
        sta     $79
        lda     $78
        rts

; ----------------------------------------------------------------------
; Initial part timing data
; ----------------------------------------------------------------------
init_timing_data:
        .byte   $44, $BB, $34, $72, $C0, $57, $D0, $88, $A4, $C0, $F6, $79, $1A, $49, $F0, $7B
        .byte   $00, $01, $72, $C0, $57, $D0, $88, $A4, $C0, $F6, $79, $1A, $49, $F0, $7B, $00
        .byte   $01

; ============================================================================
; SELFTEST - Decompress all songs and verify checksums
; ============================================================================

; Expected checksums for decompressed songs 1-9 (16-bit additive)
selftest_checksums:
        .word   $4541               ; Song 1
        .word   $A9C7               ; Song 2
        .word   $656A               ; Song 3
        .word   $01F5               ; Song 4
        .word   $D543               ; Song 5
        .word   $8757               ; Song 6
        .word   $8831               ; Song 7
        .word   $D3DB               ; Song 8
        .word   $4724               ; Song 9

; Song sizes in bytes (songs 1-9)
selftest_sizes:
        .word   21085               ; Song 1
        .word   21375               ; Song 2
        .word   19464               ; Song 3
        .word   22889               ; Song 4
        .word   22075               ; Song 5
        .word   20300               ; Song 6
        .word   14423               ; Song 7
        .word   20707               ; Song 8
        .word   21620               ; Song 9

; Expected stream checksums
selftest_stream_main_csum:  .word $FFE8
selftest_stream_tail_csum:  .word $4D99

; Screen codes for display
char_0          = $30
char_s          = $13               ; S in screen code
char_colon      = $3A
char_space      = $20
char_p          = $10               ; P
char_a          = $01               ; A
char_f          = $06               ; F
char_i          = $09               ; I
char_l          = $0C               ; L
char_o          = $0F               ; O
char_k          = $0B               ; K
char_m          = $0D               ; M
char_t          = $14               ; T
char_r          = $12               ; R
char_e          = $05               ; E
char_n          = $0E               ; N

; ----------------------------------------------------------------------
; Selftest entry point
; ----------------------------------------------------------------------
selftest:
        lda     #$00
        sta     VIC_D020
        sta     VIC_D021
        lda     #$16                ; Set screen at $0400, char ROM uppercase
        sta     VIC_D018
        jsr     clear_screen

        ; Display header "SELFTEST"
        lda     #<$0400
        sta     zp_screen_lo
        lda     #>$0400
        sta     zp_screen_hi
        ldy     #0
        lda     #char_s
        sta     (zp_screen_lo),y
        iny
        lda     #char_e
        sta     (zp_screen_lo),y
        iny
        lda     #char_l
        sta     (zp_screen_lo),y
        iny
        lda     #char_f
        sta     (zp_screen_lo),y
        iny
        lda     #char_t
        sta     (zp_screen_lo),y
        iny
        lda     #char_e
        sta     (zp_screen_lo),y
        iny
        lda     #char_s
        sta     (zp_screen_lo),y
        iny
        lda     #char_t
        sta     (zp_screen_lo),y

        ; Copy streams to destination
        jsr     copy_streams

        ; Set screen position for stream results (row 2)
        lda     #<($0400 + 80)
        sta     zp_screen_lo
        lda     #>($0400 + 80)
        sta     zp_screen_hi

        ; Verify stream_main checksum
        jsr     selftest_verify_main
        ; Verify stream_tail checksum
        jsr     selftest_verify_tail

        ; Initialize decompressor
        jsr     load_d0

        ; Set screen position for song results (row 4)
        lda     #<($0400 + 160)
        sta     zp_screen_lo
        lda     #>($0400 + 160)
        sta     zp_screen_hi

        ; Test all 9 songs
        lda     #0
        sta     zp_song_idx

@song_loop:
        ; Display "Sx:" where x is song number
        ldy     #0
        lda     #char_s
        sta     (zp_screen_lo),y
        iny
        lda     zp_song_idx
        clc
        adc     #$31                ; '1' + song index
        sta     (zp_screen_lo),y
        iny
        lda     #char_colon
        sta     (zp_screen_lo),y
        iny

        ; Save Y for result position
        sty     zp_copy_rem

        ; Set output destination based on song index
        lda     #$00
        sta     zp_out_lo
        lda     zp_song_idx
        and     #$01
        bne     @odd_song
        lda     #$10                ; Even index (0,2,4,6,8) -> $1000
        bne     @set_dest
@odd_song:
        lda     #$70                ; Odd index (1,3,5,7) -> $7000
@set_dest:
        sta     zp_out_hi

        ; Decompress (stream spans $D000, need all-RAM mode)
        sei
        lda     #$30                ; All RAM (including $D000-$DFFF)
        sta     $01
        jsr     setup_ram_irq       ; Set up RAM-based IRQ handler
        cli                         ; Allow interrupts during decompress
        jsr     decompress
        ; S9 is split: stream_main has first part, stream_tail has rest
        lda     zp_song_idx
        cmp     #8                  ; Song 9 = index 8
        bne     @not_s9
        ; Switch to stream_tail and continue decompressing
        lda     #<STREAM_TAIL_DEST
        sta     zp_src_lo
        lda     #>STREAM_TAIL_DEST
        sta     zp_src_hi
        lda     #$80
        sta     zp_bitbuf
        jsr     decompress
@not_s9:
        sei
        lda     #$37                ; Restore I/O for screen writes
        sta     $01
        cli

        ; Calculate checksum of output
        jsr     selftest_output_checksum

        ; Compare with expected
        ldx     zp_song_idx
        txa
        asl     a
        tax
        lda     zp_csum_lo
        cmp     selftest_checksums,x
        bne     @fail
        lda     zp_csum_hi
        cmp     selftest_checksums+1,x
        bne     @fail

        ; Init the player (checksum passed, code is valid)
        ; Must have I/O banked in for SID access at $D400
        sei
        lda     #$35                ; I/O visible, no ROMs
        sta     $01
        lda     zp_song_idx
        and     #$01
        bne     @init_odd
        lda     #$00
        jsr     TUNE1_INIT
        jmp     @init_done
@init_odd:
        lda     #$00
        jsr     TUNE2_INIT
@init_done:
        lda     #$37                ; Restore normal banking after player init
        sta     $01
        cli

        ; PASS - display OK and store $00
        ldy     zp_copy_rem
        lda     #char_o
        sta     (zp_screen_lo),y
        iny
        lda     #char_k
        sta     (zp_screen_lo),y
        ldx     zp_song_idx
        lda     #$00
        sta     $0801,x
        jmp     @next_song

@fail:
        ; FAIL - display checksum and store $FF
        ldy     zp_copy_rem
        jsr     print_hex_word
        ldx     zp_song_idx
        lda     #$FF
        sta     $0801,x

@next_song:
        ; Advance screen position (40 chars per row)
        lda     zp_screen_lo
        clc
        adc     #40
        sta     zp_screen_lo
        bcc     @no_carry
        inc     zp_screen_hi
@no_carry:
        inc     zp_song_idx
        lda     zp_song_idx
        cmp     #9
        beq     @done
        jmp     @song_loop

        ; Done - loop forever
@done:
        jmp     @done

; ----------------------------------------------------------------------
; Compressed stream data
; ----------------------------------------------------------------------
stream_main:
        .incbin "../generated/stream_main.bin"
stream_tail:
        .incbin "../generated/stream_tail.bin"
stream_end:
