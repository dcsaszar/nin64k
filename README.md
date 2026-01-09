# SounDemoN "Ninjas" - Single PRG

Disassembly and rebuild of SounDemoN's "Nine Inch Ninjas" (2000) C64 music demo. This is archival data - the original will never change.

## Goal

Fit all nine music parts into a single PRG file, eliminating disk loading between parts.

## Architecture

Dual-buffer system with patching:
- **$1000** - Odd songs (1, 3, 5, 7, 9)
- **$7000** - Even songs (2, 4, 6, 8)

Songs alternate between buffers. Each song consists of:
- **Player code** ($0000-$098B): 2,444 bytes - 98% identical across songs
- **Song data** ($098C+): 12-20KB unique per song

## Compression Strategy

1. Songs 1 and 2 stored as complete base files
2. Songs 3-9 stored as delta patches referencing current buffer contents

Buffer state during playback:
```
S1 -> $1000 (base)
S2 -> $7000 (base)
S3 -> $1000: patches from S1
S4 -> $7000: patches from S2
S5 -> $1000: patches from S3
...
```

## Building

```bash
go run compress.go   # Generate delta files (~13s parallel)
make                 # Build PRG and D64
make run             # Run in VICE
make clean           # Remove build artifacts
```

## Files

- `src/soundemon_loop.asm` - Main loader/player
- `src/c64.cfg` - Linker configuration
- `generate_patches.js` - Generates patch tables from song files
- `disasm_clean.js` - Original disassembler (for reference)
- `uncompressed/d*p.raw` - Extracted song files with player
- `build/patch_data.inc` - Generated patch tables
- `build/player_*_base.bin` - Base player binaries
- `compress.go` - Delta compressor (V23 Exp-Golomb, DP optimal parsing, parallel)
- `build/d*_delta.bin` - Compressed delta files

## Delta Compression (V23)

### Encoding Scheme

```
0     + expgol(d,3)   + expgol(len-2,1):  Self-ref, dist = 3*(d+1)
10    + 8bits:                            Literal byte
110   + expgol(d,3)   + expgol(len-2,1):  Self-ref, dist = 3*(d+1) - 2
1110  + expgol(o,1)   + expgol(len-2,1):  Dict-self match
11110 + expgol(d,3)   + expgol(len-2,1):  Self-ref, dist = 3*(d+1) - 1
11111 + expgol(o,1)   + expgol(len-2,1):  Dict-other match
```

Exp-Golomb: `expgol(n,k) = gamma(n>>k) + k low bits of n`

### Key Optimizations

- **DP optimal parsing**: Dynamic programming finds globally optimal encoding (vs greedy)
- **Cross-song self-reference**: Self-ref can reach into previous song's buffer end
- **3x distance encoding**: `dist = 3*(d+1) + rem` saves ~1,125 bytes
- **Tuned Exp-Golomb k values**: k=1 (length), k=3 (distance), k=1 (offset)

### Results

| Data | Size |
|------|------|
| S1+S2 base | 42,460 bytes |
| S3-S9 delta | 17,800 bytes |
| **Total** | **60,260 bytes** |

### Trade-offs (DP optimal parsing)

| Config | Size | Delta |
|--------|------|-------|
| Full (3x + dict-other) | 17,800 | baseline |
| No dict-other | ~17,890 | +90 |

Dict-other saves 91 bytes for one additional decoder branch.

K_OFFSET=1 (vs k=4) saves 29 bytes by better matching the small offset distribution.

## Next Steps

1. **6502 decoder**: Bit reader, Exp-Golomb decoder, cross-song negative index handling
2. **Further optimization potential**:
   - ~~Huffman literals~~ (analyzed: 255 unique symbols, 7.66 bits entropy — table overhead exceeds savings)
   - ~~Per-song k tuning~~ (analyzed: ~8 bytes net savings, not worth decoder complexity)
   - ~~3x encoding for dict offsets~~ (analyzed: only 8 bytes over K=1 fix, not worth decoder complexity)
   - ~~Data reordering~~ (analyzed: deinterleaving and block sorting both hurt compression)
3. **Memory placement**: Decoder must avoid $1000-$6xxx and $7000-$Cxxx during decompression
4. **Verification**: 6502 output must match JS byte-for-byte (checksums in load table)
