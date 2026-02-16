package transform

import (
	"fmt"
	"sort"
)

var DebugPersistFX = false

// OptimizePersistentFXSelective converts consecutive same-effect rows to NOP
// only when it reduces total dictionary entries.
// Tracks runs across pattern boundaries using orders.
func OptimizePersistentFXSelective(patterns []TransformedPattern, orders [3][]TransformedOrder, targetEffect byte) ([]TransformedPattern, int) {
	// First pass: count all unique rows
	rowCounts := make(map[string]int)
	for _, pat := range patterns {
		truncateAt := pat.TruncateAt
		if truncateAt <= 0 || truncateAt > 64 {
			truncateAt = 64
		}
		var prevRow TransformedRow
		for row := 0; row < truncateAt; row++ {
			r := pat.Rows[row]
			if r == prevRow {
				prevRow = r
				continue
			}
			key := rowKey(r.Note, r.Inst, r.Effect, r.Param)
			rowCounts[key]++
			prevRow = r
		}
	}

	// Second pass: find candidates by walking through orders for each channel
	// Track how many times each (patIdx, row) is a valid candidate vs total uses
	type rowLocation struct {
		patIdx int
		rowIdx int
	}
	type candidateInfo struct {
		effRow    string
		nopRow    string
		validUses int // times this row follows same effect+param
		totalUses int // total times this row is encountered with targetEffect
	}
	candidateMap := make(map[rowLocation]*candidateInfo)

	for ch := 0; ch < 3; ch++ {
		var lastWasTarget bool
		var lastParam byte
		var prevRow TransformedRow

		for _, order := range orders[ch] {
			patIdx := order.PatternIdx
			if patIdx < 0 || patIdx >= len(patterns) {
				continue
			}
			pat := patterns[patIdx]

			truncateAt := pat.TruncateAt
			if truncateAt <= 0 || truncateAt > 64 {
				truncateAt = 64
			}

			for row := 0; row < truncateAt; row++ {
				r := pat.Rows[row]

				// Skip identical consecutive rows (they're RLE'd)
				if r == prevRow {
					prevRow = r
					continue
				}

				if r.Effect == targetEffect {
					loc := rowLocation{patIdx, row}
					if candidateMap[loc] == nil {
						candidateMap[loc] = &candidateInfo{
							effRow: rowKey(r.Note, r.Inst, r.Effect, r.Param),
							nopRow: rowKey(r.Note, r.Inst, 0, 0),
						}
					}
					candidateMap[loc].totalUses++
					if lastWasTarget && r.Param == lastParam {
						candidateMap[loc].validUses++
					}
					lastParam = r.Param
					lastWasTarget = true
				} else if r.Effect == 0 && r.Param == 0 {
					// NOP continues run
				} else {
					lastWasTarget = false
				}
				prevRow = r
			}
		}
	}

	// Only include candidates where ALL uses are valid (always follows same effect+param)
	type candidate struct {
		patIdx int
		rowIdx int
		effRow string
		nopRow string
	}
	var candidates []candidate
	for loc, info := range candidateMap {
		if info.validUses == info.totalUses && info.totalUses > 0 {
			candidates = append(candidates, candidate{
				patIdx: loc.patIdx,
				rowIdx: loc.rowIdx,
				effRow: info.effRow,
				nopRow: info.nopRow,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].patIdx != candidates[j].patIdx {
			return candidates[i].patIdx < candidates[j].patIdx
		}
		return candidates[i].rowIdx < candidates[j].rowIdx
	})

	// Evaluate each candidate: only convert if it helps
	result := make([]TransformedPattern, len(patterns))
	for i, pat := range patterns {
		result[i] = TransformedPattern{
			OriginalAddr: pat.OriginalAddr,
			CanonicalIdx: pat.CanonicalIdx,
			TruncateAt:   pat.TruncateAt,
			Rows:         make([]TransformedRow, len(pat.Rows)),
		}
		copy(result[i].Rows, pat.Rows)
	}

	converted := 0
	for _, c := range candidates {
		effCount := rowCounts[c.effRow]
		nopCount := rowCounts[c.nopRow]

		// Only convert if:
		// - This is the last use of effRow (removes 1 dict entry), OR
		// - nopRow already exists (no new dict entry needed)
		willRemoveEntry := effCount == 1
		nopAlreadyExists := nopCount > 0

		if willRemoveEntry || nopAlreadyExists {
			// Calculate net change
			netChange := 0
			if willRemoveEntry {
				netChange-- // removes effRow entry
			}
			if !nopAlreadyExists {
				netChange++ // adds nopRow entry
			}

			if netChange < 0 || (netChange == 0 && nopAlreadyExists) {
				// Apply conversion
				result[c.patIdx].Rows[c.rowIdx].Effect = 0
				result[c.patIdx].Rows[c.rowIdx].Param = 0

				// Update counts
				rowCounts[c.effRow]--
				rowCounts[c.nopRow]++
				converted++

				if DebugPersistFX {
					fmt.Printf("  pat %d row %d: %s -> NOP (eff=%d, nop=%d, net=%d)\n",
						c.patIdx, c.rowIdx, PlayerEffectName(targetEffect), effCount, nopCount, netChange)
				}
			}
		}
	}

	return result, converted
}
