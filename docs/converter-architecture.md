# Converter Architecture

## Overview

The converter transforms tracker song data into an optimized format for the 6502 player. This document defines a modular pipeline architecture that enables clean extensibility and maintainability.

## Naming

- **`forge`** (`tools/forge/`) - the pipeline-based converter (complete, all tests passing)
- **`odin_convert`** (`tools/odin_convert/`) - legacy converter, kept as reference

All 9 songs pass both VP (VirtualPlayer) and ASM (6502 emulator) validation.

## Design Principles

1. **Explicit data flow**: Each stage has clearly defined input and output types
2. **Immutability**: Stages produce new data structures rather than mutating inputs
3. **Composability**: Optimization passes can be added, removed, or reordered
4. **Testability**: Each stage can be tested in isolation
5. **Cross-pattern awareness**: Analysis passes have access to full song context

## Architecture Benefits

The pipeline architecture addresses issues from the legacy converter:

1. **Explicit data flow**: Each stage has clearly defined input/output types
2. **Immutability**: Stages produce new data structures rather than mutating inputs
3. **Composability**: Stages can be tested and debugged in isolation
4. **Verification**: Per-stage verifiers catch issues early
5. **Dual validation**: Both Go emulators (VP) and 6502 ASM validate correctness

## Pipeline Stages

```
┌─────────────────────────────────────────────────────────────────────┐
│                           PARSING                                    │
├─────────────────────────────────────────────────────────────────────┤
│  RawSongData → ParsedSong                                           │
│  - Extract instruments, patterns, orders from binary format          │
│  - Extract table addresses from embedded player code                 │
│  - No transformations, just structured access                        │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                          ANALYSIS                                    │
├─────────────────────────────────────────────────────────────────────┤
│  ParsedSong → SongAnalysis                                          │
│  - Effect usage statistics (including F sub-effects)                 │
│  - Pattern reachability via order list walk                          │
│  - Instrument usage per pattern                                      │
│  - Filter trigger instrument detection (FEx effect)                  │
│  - Cross-pattern data flow (arp sequences)                           │
│  - Vibrato depth frequency analysis                                  │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                      TRANSFORMATION                                  │
├─────────────────────────────────────────────────────────────────────┤
│  (ParsedSong, SongAnalysis) → TransformedSong                       │
│  - Effect remapping (frequency-sorted)                               │
│  - F sub-effect splitting and remapping                              │
│  - Instrument remapping (MFU packing, filter trigger to slots 1-15) │
│  - Pattern transpose equivalence detection                           │
│  - Order reachability pruning                                        │
│  - Position jump remapping (Dxx effect)                              │
│  - Pattern index optimization (Cuthill-McKee + swaps)               │
│  - Row equivalence mapping                                           │
│  - Arp note $FF→$E7 remapping                                        │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                         ENCODING                                     │
├─────────────────────────────────────────────────────────────────────┤
│  TransformedSong → EncodedSong                                      │
│  - Build row dictionary (3-byte entries)                            │
│  - Pack patterns using dictionary indices (primary/extended)         │
│  - Cross-channel truncation (earliest break row)                     │
│  - Gap encoding for pattern storage                                  │
│  - Overlap optimization for packed patterns                          │
│  - Row equivalence canonicalization                                  │
│  - Pattern deduplication and reordering                              │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                          SOLVING                                     │
├─────────────────────────────────────────────────────────────────────┤
│  Cross-song optimization                                             │
│  - Delta table solving (optimal ordering for trackptr deltas)        │
│  - Transpose table solving (optimal ordering for transpose deltas)   │
│  - Global wave table building (cross-song deduplication)             │
│  - Order bitstream encoding (transpose + trackptr deltas)            │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                         SERIALIZATION                                │
├─────────────────────────────────────────────────────────────────────┤
│  EncodedSong → []byte                                               │
│  - Layout data sections at fixed offsets                             │
│  - Place patterns in gaps (inst gap, filter gap, arp gap, dict gaps) │
│  - Write instrument data (16 params × N instruments)                 │
│  - Generate final binary with all pointers resolved                  │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                        VERIFICATION                                  │
├─────────────────────────────────────────────────────────────────────┤
│  Stage-by-stage integrity checks                                     │
│  - Parse verification (valid addresses, ranges)                      │
│  - Analysis verification (consistency checks)                        │
│  - Transform verification (remap validity)                           │
│  - Encode verification (dictionary bounds, pattern validity)         │
│  - Serialize verification (gap bounds, offset validity)              │
│  - Semantic verification (cross-stage consistency)                   │
└─────────────────────────────────────────────────────────────────────┘
                                   ↓
┌─────────────────────────────────────────────────────────────────────┐
│                        VALIDATION                                    │
├─────────────────────────────────────────────────────────────────────┤
│  Runtime correctness validation                                      │
│  - VP validation: Go-based player emulator comparison                │
│  - ASM validation: 6502 emulator running actual player code          │
│  - SID write capture and comparison over N frames                    │
└─────────────────────────────────────────────────────────────────────┘
```

## Current Features (Complete List)

The following features exist in `odin_convert` and must be implemented in `forge`. Line references point to `tools/odin_convert/main.go` for lookup:

### Parsing Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| Base address detection | `convertToNewFormat:1501` | Detect $xx00 base from entry JMP |
| Table address extraction | `convertToNewFormat:1504-1525` | Extract transpose, tracklo/hi, inst, wave, arp, filter addresses |
| Instrument count | `convertToNewFormat:1516-1518` | Derive from instAD/instSR delta |
| Order count | `convertToNewFormat:1536-1539` | Derive from transpose0-trackLo0 gap |

### Analysis Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| Effect usage counting | `countEffectUsage` | Count each effect type across patterns |
| Effect param distribution | `countEffectParams` | Map effect → unique params used |
| F sub-effect splitting | `main:5099-5121` | Speed, globalvol, filtmode, fineslide, filttrig, hrdrest |
| Instrument usage | `main:4917-4939` | Count per-instrument usage in patterns |
| Filter trigger detection | `main:4930-4937` | Track instruments used by FEx effect |
| Reachable orders | `findReachableOrders` | Walk order list following jumps |
| Pattern break info | `getPatternBreakInfo` | Find break row and jump target per pattern |
| Vibrato depth frequency | `main:5217-5259` | Analyze which vib depths are used |
| Table duplicate analysis | `analyzeTableDupes` | Find duplicate wave/arp/filter entries |
| Cross-song pattern sharing | `main:5184-5210` | Identify patterns shared between songs |

### Transformation Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| Effect frequency remap | `buildEffectRemap` | Sort effects by usage, assign new numbers |
| F sub-effect remap | `main:5083-5181` | Split F into: vib→0,1; break→0,2; fineslide→0,3; others→1-E |
| Instrument MFU packing | `main:4965-5078` | Most-frequently-used at low indices |
| Filter trigger constraint | `main:4997-5024` | Filter trigger instruments must be slots 1-15 |
| Transpose equivalence | `convertToNewFormat:1580-1665` | Detect patterns differing only by note transpose |
| Transpose rewrite | `convertToNewFormat:1649-1665` | Adjust transpose table for equivalent patterns |
| Pointer rewrite | `convertToNewFormat:1669-1685` | Point to canonical pattern for transpose-equiv |
| Position jump remap | `remapPatternPositionJumps` | Update Dxx params for pruned order list |
| Row effect remap | `remapRowBytes` | Apply effect/inst remapping to row data |
| Pattern effect remap | `remapPatternEffects` | Apply remapping to entire pattern |
| Pattern index optimization | `convertToNewFormat:2159-2428` | Cuthill-McKee + swap optimization for delta minimization |
| Equiv pattern dedup | `convertToNewFormat:2086-2157` | Patterns identical after row equiv mapping |
| Row equivalence | `optimizeEquivMapMinDict` | Find rows that can share dict entries |
| Arp note remap | `convertToNewFormat:1877-1882` | $FF (note 127) → $E7 (note 103) to shrink freq table |

### Encoding Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| Row dictionary building | `buildPatternDict` | Build unique 3-byte row entries |
| Primary/extended indices | `packPatternsWithEquiv` | <179 primary (1 byte), ≥179 extended (escape + byte) |
| Pattern gap encoding | `calculatePatternGap`, `packPatternsWithEquiv` | Skip trailing empty rows |
| Cross-channel truncation | `convertToNewFormat:2009-2051` | Truncate at earliest break across all channels at same order |
| Packed overlap optimization | `optimizePackedOverlap` | Overlap pattern data where possible |
| Order bitstream | `packOrderBitstream` | 4 bytes per order: transpose deltas + trackptr deltas |
| Delta table solving | `solveDeltaTable` | Find optimal delta ordering to minimize table size |
| Start constant optimization | `main:5454-5527` | Find best constant for initial trackptr deltas |
| Transpose table solving | `solveTransposeTable` | Similar to delta table |
| Wave table dedup | `convertToNewFormat:1744-1819` | Group by (content, loopOffset), find overlaps |
| Arp table dedup | `convertToNewFormat:1821-1891` | Same approach as wave |
| Filter table dedup | `convertToNewFormat:1893-1966` | Same approach, with sentinel at position 0 |
| Global wave table | `buildGlobalWaveTable` | Cross-song wave snippet deduplication |

### Serialization Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| Fixed layout offsets | `convertToNewFormat:1988-1994` | inst@0, bitstream@$1F0, filter@$5EC, arp@$6CF, dict@$78D |
| Gap utilization | `convertToNewFormat:2437-2490` | Use gaps between sections for pattern data |
| Instrument data packing | `convertToNewFormat:2565-2660` | 16 params × maxUsedSlot instruments |
| Wave remap adjustment | `convertToNewFormat` | Adjust start/end/loop indices for new table |
| Filter remap adjustment | `convertToNewFormat` | Same for filter indices |
| Global wave lookup | `convertToNewFormat` | Map song snippets to global table offsets |

### Validation Features

| Feature | Current Location | Description |
|---------|------------------|-------------|
| 6502 VM emulator | `validate/vm.go` | Full 6502 instruction set emulation |
| SID write capture | `RunFrames` | Capture all writes to $D400-$D41C |
| ASM comparison test | `validate/compare.go` | Compare SID writes via 6502 emulator |
| VP original player | `validate/vplayer_orig.go` | Go emulator for original GT format |
| VP converted player | `validate/vplayer.go` | Go emulator for converted format |
| VP comparison test | `validate/compare.go` | Compare SID writes via Go emulators |
| Stage verifiers | `verify/*.go` | Per-stage data integrity checks |
| Semantic verifier | `verify/semantic.go` | Cross-stage consistency validation |

## Data Structures

### ParsedSong

```go
type ParsedSong struct {
    BaseAddr    uint16
    Instruments []Instrument
    Patterns    []Pattern      // All patterns, indexed by address
    Orders      [3][]OrderEntry // Per-channel order lists
    WaveTable   []byte
    ArpTable    []byte
    FilterTable []byte
    Metadata    SongMetadata
}

type Instrument struct {
    AD, SR          byte
    WaveStart, WaveEnd, WaveLoop byte
    ArpStart, ArpEnd, ArpLoop    byte
    FilterStart, FilterEnd, FilterLoop byte
    VibDepthSpeed, VibDelay byte
    PulseWidth    uint16
    PulseSpeed    byte
}

type Pattern struct {
    Address uint16
    Rows    [64]Row
}

type Row struct {
    Note   byte // 0-127, 0 = no note
    Inst   byte // 0-31
    Effect byte // 0-15 (original effect number)
    Param  byte
}

type OrderEntry struct {
    PatternAddr uint16
    Transpose   int8
}
```

### SongAnalysis

```go
type SongAnalysis struct {
    // Effect statistics
    EffectUsage     map[byte]int          // effect -> use count
    EffectParams    map[byte][]byte       // effect -> unique params used
    FSubUsage       map[string]int        // F sub-effect -> count

    // Pattern analysis
    ReachableOrders   []int                 // order indices in play order
    PatternAddrs      map[uint16]bool       // unique pattern addresses
    PatternBreaks     map[uint16]int        // pattern addr -> break row
    PatternJumps      map[uint16]int        // pattern addr -> jump target

    // Instrument analysis
    UsedInstruments   []int
    InstrumentFreq    map[int]int           // inst -> use count
    FilterTriggerInst map[int]bool          // instruments used by FEx

    // Cross-pattern flow analysis
    ArpSequences    []ArpSequence         // arp params in playback order
}
```

### TransformedSong

```go
type TransformedSong struct {
    Instruments []Instrument          // Remapped indices
    Patterns    []TransformedPattern  // Canonical patterns only
    Orders      [3][]TransformedOrder // Pruned, remapped

    // Tables with deduplication applied
    WaveTable   []byte
    ArpTable    []byte
    FilterTable []byte

    // Mappings for debugging/validation
    EffectRemap    [16]byte              // old effect -> new effect
    FSubRemap      map[int]byte          // F sub-code -> new effect
    InstRemap      []int                 // old inst -> new inst
    WaveRemap      []int                 // old inst -> wave offset delta
    ArpRemap       []int                 // old inst -> arp offset delta
    FilterRemap    []int                 // old inst -> filter offset delta
    PatternRemap   map[uint16]uint16     // orig addr -> canonical addr
    TransposeDelta map[uint16]int        // addr -> transpose adjustment
    OrderMap       map[int]int           // old order -> new order
}

type TransformedPattern struct {
    OriginalAddr uint16
    CanonicalIdx int
    Rows         [64]TransformedRow
    TruncateAt   int  // Cross-channel truncation point
}

type TransformedRow struct {
    Note   byte
    Inst   byte  // Remapped
    Effect byte  // Remapped
    Param  byte  // Remapped (including position jumps)
}

type TransformedOrder struct {
    PatternIdx int   // Index into canonical pattern list
    Transpose  int8  // Adjusted for transpose equivalence
}
```

### EncodedSong

```go
type EncodedSong struct {
    // Dictionary
    RowDict     []byte        // 3 bytes per entry
    RowToIdx    map[string]int
    EquivMap    map[int]int   // dict idx -> canonical idx

    // Encoded patterns
    PatternData      [][]byte   // Individual packed patterns
    PatternOffsets   []uint16   // Offset of each pattern in packed data
    PatternGapCodes  []byte     // Gap encoding per pattern
    PackedPatterns   []byte     // All patterns with overlap optimization

    PrimaryCount     int        // Entries using 1-byte index
    ExtendedCount    int        // Entries using 2-byte index

    // Encoded orders
    OrderBitstream []byte       // 4 bytes per order

    // Delta tables
    DeltaTable     []byte       // Optimal delta ordering
    DeltaBases     []byte       // Per-song starting indices
    StartConst     int          // Constant for initial trackptr
    TransposeTable []byte
    TransposeBases []byte

    // Packed tables
    InstrumentData []byte       // maxUsedSlot * 16 bytes
    FilterData     []byte
    ArpData        []byte
    // Wave is global, not per-song
}
```

## File Organization

```
tools/forge/
├── main.go              # Entry point, orchestration, CLI
├── types.go             # All data structure definitions
├── parse/
│   ├── parser.go        # RawSongData → ParsedSong
│   └── addresses.go     # Table address extraction from embedded player
├── analysis/
│   ├── analysis.go      # Analysis pass: effects, instruments, filter triggers
│   └── patterns.go      # Pattern reachability, break info
├── transform/
│   ├── transform.go     # Transform pass infrastructure, order pruning
│   ├── effect_remap.go  # Frequency-sorted effect remapping (global)
│   ├── inst_remap.go    # MFU packing with filter trigger constraint
│   ├── transpose_equiv.go # Transpose-equivalent pattern detection
│   ├── row_remap.go     # Row-level effect/inst/param remapping
│   └── dedup.go         # Arp/filter table deduplication
├── encode/
│   ├── encoder.go       # Dictionary building, pattern packing, overlap
│   ├── equiv.go         # Row equivalence canonicalization
│   ├── patterns.go      # Pattern gap encoding
│   ├── orders.go        # Order bitstream encoding
│   └── reorder.go       # Pattern deduplication and reordering
├── solve/
│   ├── delta.go         # Delta table solving (optimal ordering)
│   ├── transpose.go     # Transpose table solving
│   └── wavetable.go     # Global wave table building (cross-song)
├── serialize/
│   ├── layout.go        # Fixed offset constants, gap calculations
│   └── serializer.go    # Final binary assembly
├── validate/
│   ├── vm.go            # 6502 emulator
│   ├── compare.go       # VP and ASM comparison tests
│   ├── vplayer.go       # Go emulator for converted format
│   └── vplayer_orig.go  # Go emulator for original GT format
└── verify/
    ├── verify.go        # Verification infrastructure
    ├── parse.go         # Parse stage verification
    ├── analysis.go      # Analysis stage verification
    ├── transform.go     # Transform stage verification
    ├── encode.go        # Encode stage verification
    ├── solve.go         # Solve stage verification
    ├── serialize.go     # Serialize stage verification
    └── semantic.go      # Cross-stage semantic verification
```

## Migration Status: Complete

### Phase 1: Full Pipeline Implementation ✓

All stages implemented:
1. Types and parsing
2. Analysis
3. Transformation
4. Encoding
5. Solving (delta/transpose tables, global wave table)
6. Serialization
7. Verification (per-stage integrity checks)

### Phase 2: Validation ✓

Dual validation approach:
- **VP validation**: Go-based player emulators compare original vs converted format
- **ASM validation**: 6502 emulator runs actual player code

All 9 songs pass both VP and ASM validation (100% SID write match).

## Example: Adding Arp Repeat

With this architecture, adding arp repeat:

1. **Add to Analysis** (`analysis/arp_flow.go`):
   - Walk order lists in playback order
   - Track arp events per channel
   - Build `ArpSequences`

2. **Add Transform Pass** (`transform/arp_repeat.go`):
   - Iterate `ArpSequences`
   - Mark rows where param matches previous (within pattern or cross-pattern)
   - Modify effect/param to arp repeat encoding (effect 0, param 4)

3. **Update player** (separate from converter):
   - Add `last_arp_param` storage per channel
   - Add effect 0, param 4 handler that uses stored param

The encoding and serialization stages work unchanged on the transformed data.
