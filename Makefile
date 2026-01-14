ASM = ca65
LD = ld65

SRC = src/nin64k.asm
CFG = src/c64.cfg
OBJ = build/nin64k.o
PRG = build/nin64k.prg

SELFTEST_SRC = src/nin64selftest.asm
SELFTEST_OBJ = build/nin64selftest.o
SELFTEST_PRG = build/nin64selftest.prg

.PHONY: all clean run selftest run-selftest

all: $(PRG)

$(OBJ): $(SRC) generated/decompress.asm generated/stream_main.bin generated/stream_tail.bin
	@mkdir -p build
	$(ASM) -o $@ $<

$(PRG): $(OBJ) $(CFG)
	$(LD) -C $(CFG) -o $@ $<

run: $(PRG)
ifdef VICE_BIN
	$(VICE_BIN)/x64sc $(PRG) &
else
	@echo "Set VICE_BIN to run in emulator, e.g.: export VICE_BIN=~/path/to/vice/bin"
endif

selftest: $(SELFTEST_PRG)

$(SELFTEST_OBJ): $(SELFTEST_SRC) generated/decompress.asm generated/stream_main.bin generated/stream_tail.bin
	@mkdir -p build
	$(ASM) -o $@ $<

$(SELFTEST_PRG): $(SELFTEST_OBJ) $(CFG)
	$(LD) -C $(CFG) -o $@ $<

run-selftest: $(SELFTEST_PRG)
ifdef VICE_BIN
	$(VICE_BIN)/x64sc -warp $(SELFTEST_PRG) &
else
	@echo "Set VICE_BIN to run in emulator, e.g.: export VICE_BIN=~/path/to/vice/bin"
endif

clean:
	rm -rf build/*.o build/*.prg build/*.bin build/*.inc
