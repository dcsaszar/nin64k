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
zp_ptr_lo       = $8C
zp_ptr_hi       = $8D

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
        jsr     $FF9F
        jsr     $FFE4
        cmp     #$5F
        bne     main_loop
        inc     $D020
        sei
        jsr     play_tick
        cli
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
        lda     #$95
        sta     $0314
        lda     #$08
        sta     $0315
        cli
        rts

; ----------------------------------------------------------------------------
irq_handler:
        pha
        txa
        pha
        tya
        pha
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
        jmp     $EA31

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
msg_menu:
        .res    125, $00                ; Padding (menu text removed)
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
load_d0_impl:
        lda     #<(drivecode-1)
        sta     upload_lda+1
        lda     #>(drivecode-1)
        sta     upload_lda+2
        lda     #$00
        sta     fastload_params+2
        lda     #$05
        sta     fastload_params+1

; ----------------------------------------------------------------------------
LC1A:
        jsr     setup_drive
        ldx     #$05

; ----------------------------------------------------------------------------
LC1F:
        lda     fastload_params,x
        jsr     $FFA8
        dex
        bpl     LC1F
        ldx     #$00

; ----------------------------------------------------------------------------
upload_lda:
        lda     drivecode,x
        jsr     $FFA8
        inx
        cpx     #$20
        bne     upload_lda
        jsr     $FFAE
        clc
        lda     #$20
        adc     upload_lda+1
        sta     upload_lda+1
        bcc     LC47
        clc
        inc     upload_lda+2

; ----------------------------------------------------------------------------
LC47:
        lda     #$20
        adc     fastload_params+2
        sta     fastload_params+2
        tax
        lda     #$00
        adc     fastload_params+1
        sta     fastload_params+1
        cpx     #$32
        sbc     #$06
        bcc     LC1A
        jsr     setup_drive
        ldx     #$04

; ----------------------------------------------------------------------------
LC63:
        lda     fastload_counter,x
        jsr     $FFA8
        dex
        bpl     LC63
        jmp     $FFAE

; ----------------------------------------------------------------------------
setup_drive:
        lda     $BA
        jsr     $FFB1
        lda     #$6F
        jmp     $FF93

; ----------------------------------------------------------------------
; Fastload parameters
; ----------------------------------------------------------------------
fastload_params:
        .byte   $20, $06, $40, $57, $2D, $4D    ; M-W command template
fastload_counter:
        .byte   $05, $00                        ; 16-bit counter
        .byte   $45, $2D, $4D                   ; M-E command

; ----------------------------------------------------------------------------
load_tune_impl:
        lda     $DD00
        and     #$CF
        sta     getbit_imm2+1
        sta     sendbyte_imm2+1
        eor     #$10
        sta     getbit_imm1+1
        sta     sendbyte_imm1+1
        txa
        jsr     fastload_sendbyte
        tya
        jsr     fastload_sendbyte
        jsr     set_load_dest       ; Set Y=0 and store_byte+2 based on part
        nop                         ; Padding to maintain original size
        nop
        nop
        nop
        nop
        nop
        nop

; ----------------------------------------------------------------------------
LCA9:
        jsr     fastload_byte
store_byte:
        sta     $9F00,y
        iny
        bne     LCA9
        inc     store_byte+2
        jmp     LCA9

; ----------------------------------------------------------------------------
fastload_byte:
        jsr     fastload_getbit
        cmp     #$AC
        bne     LCCA
        jsr     fastload_getbit
        cmp     #$AC
        beq     LCCA
        cmp     #$01
        pla
        pla

; ----------------------------------------------------------------------------
LCCA:
        rts

; ----------------------------------------------------------------------------
fastload_getbit:
        ldx     #$08

; ----------------------------------------------------------------------------
LCCD:
        lda     $DD00
        and     #$C0
        eor     #$C0
        beq     LCCD
        asl     a
getbit_imm1:
        lda     #$D7
        bcs     LCDD
        eor     #$30

; ----------------------------------------------------------------------------
LCDD:
        sta     $DD00
        ror     $8B
        lda     #$C0

; ----------------------------------------------------------------------------
LCE4:
        bit     $DD00
        beq     LCE4
getbit_imm2:
        lda     #$C7
        sta     $DD00
        dex
        bne     LCCD
        lda     $8B
        rts

; ----------------------------------------------------------------------------
fastload_sendbyte:
        sta     $8B
        ldx     #$08

; ----------------------------------------------------------------------------
LCF8:
        lsr     $8B
sendbyte_imm1:
        lda     #$D7
        bcc     LD00
        eor     #$30

; ----------------------------------------------------------------------------
LD00:
        sta     $DD00
        lda     #$C0

; ----------------------------------------------------------------------------
LD05:
        bit     $DD00
        bne     LD05
sendbyte_imm2:
        lda     #$C7
        sta     $DD00

; ----------------------------------------------------------------------------
LD0F:
        lda     $DD00
        and     #$C0
        eor     #$C0
        bne     LD0F
        dex
        bne     LCF8
        rts
        cld

; ----------------------------------------------------------------------
; Drive code (uploaded to 1541)
; ----------------------------------------------------------------------
drivecode:
        .byte   $58, $AD, $00, $1C, $29, $F7, $8D, $00, $1C, $20, $03, $06, $8D, $32, $06, $20
        .byte   $03, $06, $8D, $33, $06, $A9, $08, $0D, $00, $1C, $8D, $00, $1C, $A2, $12, $A0
        .byte   $01, $86, $08, $84, $09, $20, $AF, $05, $B0, $2A, $A0, $02, $B9, $00, $04, $29
        .byte   $83, $C9, $82, $D0, $10, $B9, $03, $04, $CD, $32, $06, $D0, $08, $B9, $04, $04
        .byte   $CD, $33, $06, $F0, $1C, $98, $18, $69, $20, $A8, $90, $E0, $AC, $01, $04, $AE
        .byte   $00, $04, $D0, $CD, $A2, $AC, $20, $DD, $05, $A2, $01, $20, $DD, $05, $4C, $01
        .byte   $05, $B9, $01, $04, $85, $08, $B9, $02, $04, $85, $09, $20, $AF, $05, $B0, $E4
        .byte   $A0, $00, $AD, $01, $04, $85, $09, $AD, $00, $04, $85, $08, $D0, $04, $AC, $01
        .byte   $04, $C8, $8C, $32, $06, $A0, $02, $BE, $00, $04, $E0, $AC, $D0, $05, $20, $DD
        .byte   $05, $A2, $AC, $20, $DD, $05, $C8, $CC, $32, $06, $D0, $EB, $AD, $00, $04, $D0
        .byte   $CA, $A2, $AC, $20, $DD, $05, $A2, $00, $20, $DD, $05, $4C, $01, $05, $A0, $05
        .byte   $58, $A9, $80, $85, $01, $A5, $01, $30, $FC, $C9, $01, $D0, $03, $18, $78, $60
        .byte   $88, $30, $16, $C0, $02, $D0, $04, $A9, $C0, $85, $01, $A5, $16, $85, $12, $A5
        .byte   $17, $85, $13, $A5, $01, $30, $FC, $10, $D8, $38, $78, $60, $86, $14, $A2, $08
        .byte   $46, $14, $A9, $02, $B0, $02, $A9, $08, $8D, $00, $18, $AD, $00, $18, $29, $05
        .byte   $49, $05, $D0, $F7, $8D, $00, $18, $A9, $05, $2C, $00, $18, $D0, $FB, $CA, $D0
        .byte   $DF, $60, $A2, $08, $A9, $85, $2D, $00, $18, $30, $22, $F0, $F7, $4A, $A9, $02
        .byte   $90, $02, $A9, $08, $8D, $00, $18, $66, $14, $AD, $00, $18, $29, $05, $49, $05
        .byte   $F0, $F7, $A9, $00, $8D, $00, $18, $CA, $D0, $DA, $A5, $14, $60, $68, $68, $58
        .byte   $60, $00, $00

; ----------------------------------------------------------------------------
decompress:
        rts

; ----------------------------------------------------------------------------
; ----------------------------------------------------------------------------
init_game:
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

; ----------------------------------------------------------------------------
; Set load destination based on song number (full files only)
set_load_dest:
        ldy     #$00
        sty     store_byte+1
        lda     zp_part_num
        and     #$01
        beq     @odd
        lda     #$70                ; Odd $7B → $7000
        bne     @store
@odd:   lda     #$10                ; Even $7B → $1000
@store: sta     store_byte+2
        rts

