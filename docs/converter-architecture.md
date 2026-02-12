# Converter Architecture

## Overview

The converter transforms tracker song data into an optimized format for the 6502 player. This document defines a modular pipeline architecture that enables clean extensibility and maintainability.

## Naming

- **`forge`** (`tools/forge/`) - the new pipeline-based converter (target implementation)
- **`odin_convert`** (`tools/odin_convert/`) - existing converter, kept as reference/oracle for VM comparison tests

During development, `forge` output is validated against `odin_convert`. Once all VM tests pass, `odin_convert` can be retired.

## Design Principles

1. **Explicit data flow**: Each stage has clearly defined input and output types
2. **Immutability**: Stages produce new data structures rather than mutating inputs
3. **Composability**: Optimization passes can be added, removed, or reordered
4. **Testability**: Each stage can be tested in isolation
5. **Cross-pattern awareness**: Analysis passes have access to full song context

## Recommendation: Full Rewrite

The current converter (7000+ lines in a single file) has grown organically with features interleaved in complex ways. Key issues:

1. **Coupled data flows**: Pattern data is processed in multiple loops (patternData, allPatternData) that must stay in sync
2. **Mutation**: Raw song data is mutated in place (transpose rewrites, pointer rewrites)
3. **Order-dependent operations**: Effect remapping, transpose adjustment, and dictionary building must happen in a specific order that isn't explicit
4. **Global state**: Many mappings and remaps are built incrementally and passed around

A **full rewrite** (`forge`) with the pipeline architecture is recommended. The existing `odin_convert` serves as reference implementation and test oracle.

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
│  - Encode order lists as bitstream (transpose + trackptr deltas)     │
│  - Delta table solving (optimal ordering)                            │
│  - Transpose table building                                          │
│  - Pack wave/arp/filter tables with deduplication                    │
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
| 6502 VM emulator | `CPU6502` type + methods | Full 6502 instruction set emulation |
| SID write capture | `RunFrames` | Capture all writes to $D400-$D41C |
| VM comparison test | `testSong` | Compare SID writes: original song vs converted over N frames |
| Delta table verification | `verifyDeltaTable` | Ensure all song deltas can be encoded |
| Equiv validation | `runEquivValidate` | Validate cached row equivalences |

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
│   ├── analysis.go      # Analysis pass infrastructure
│   ├── effects.go       # Effect usage, F sub-effect splitting
│   ├── patterns.go      # Reachability, break info
│   ├── instruments.go   # Usage counts, filter trigger detection
│   ├── arp_flow.go      # Cross-pattern arp tracking
│   └── equivalence.go   # Row/pattern equivalence detection
├── transform/
│   ├── transform.go     # Transform pass infrastructure
│   ├── effect_remap.go  # Frequency-sorted effect remapping
│   ├── inst_remap.go    # MFU packing with filter trigger constraint
│   ├── transpose_equiv.go # Transpose-equivalent pattern handling
│   ├── order_prune.go   # Reachable order pruning, jump remapping
│   ├── pattern_index.go # Cuthill-McKee optimization
│   ├── table_dedup.go   # Wave/arp/filter deduplication
│   └── arp_repeat.go    # Arp repeat optimization (future)
├── encode/
│   ├── encoder.go       # Encoding infrastructure
│   ├── dictionary.go    # Row dictionary building
│   ├── patterns.go      # Pattern packing with gaps
│   ├── overlap.go       # Packed pattern overlap optimization
│   ├── orders.go        # Order bitstream encoding
│   └── delta.go         # Delta/transpose table solving
├── serialize/
│   ├── layout.go        # Fixed offset constants
│   ├── gaps.go          # Gap calculation and utilization
│   └── serializer.go    # Final binary assembly
├── validate/
│   ├── vm.go            # 6502 emulator
│   ├── compare.go       # VM comparison test
│   └── verify.go        # Delta table verification
└── global/
    └── wavetable.go     # Cross-song global wave table
```

## Migration Strategy

### Phase 1: Full Pipeline Implementation

Implement all stages together:
1. Types and parsing
2. Analysis
3. Transformation
4. Encoding
5. Serialization

Reference `odin_convert` source for exact behavior. No intermediate tests - the clean pipeline architecture makes debugging tractable if issues arise.

### Phase 2: VM Comparison Test

Run VM comparison: `forge` output vs original songs. If SID writes match across all songs, the implementation is correct. Migration complete.

If mismatches occur, add targeted tests for the failing behavior, fix, repeat.

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
