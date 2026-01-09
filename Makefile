ASM = ca65
LD = ld65
C1541 = ~/Desktop/vice-arm64-sdl2-3.10/bin/c1541
VICE = ~/Desktop/vice-arm64-sdl2-3.10/bin/x64sc

SRC = src/soundemon_loop.asm
CFG = src/c64.cfg
OBJ = build/soundemon_loop.o
PRG = build/soundemon_loop.prg
D64 = build/ninjas.d64

PATCH_DATA = build/patch_data.inc build/player_odd_base.bin build/player_even_base.bin

.PHONY: all clean run generate-patches

all: $(D64)

# Generate patch data and base players from original songs
$(PATCH_DATA): $(wildcard uncompressed/d*p.raw)
	@mkdir -p build
	node generate_patches.js

generate-patches: $(PATCH_DATA)

$(OBJ): $(SRC) $(PATCH_DATA)
	@mkdir -p build
	$(ASM) -o $@ $<

$(PRG): $(OBJ) $(CFG)
	$(LD) -C $(CFG) -o $@ $<

$(D64): $(PRG) $(PATCH_DATA)
	$(C1541) -format "ninjas,00" d64 $@ \
		-write $(PRG) "nin-soundemon" \
		-write uncompressed/d1p.raw "d1" \
		-write uncompressed/d2p.raw "d2" \
		-write uncompressed/d3p.raw "d3" \
		-write uncompressed/d4p.raw "d4" \
		-write uncompressed/d5p.raw "d5" \
		-write uncompressed/d6p.raw "d6" \
		-write uncompressed/d7p.raw "d7"

run: $(D64)
	$(VICE) -warp $(D64) &

clean:
	rm -rf build/*.o build/*.prg build/*.d64 build/*.d71 build/*.bin build/*.inc
