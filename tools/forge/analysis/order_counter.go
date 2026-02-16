package analysis

import (
	"forge/parse"
)

// GT effect numbers for order counting
const (
	gtEffectBreak = 0xD // Pattern break
	gtEffectSub   = 0xF // Sub-effects (speed when param < 0x80)
)

// GT effect for position jump
const gtEffectPosJump = 0xB

// CountMaxOrderGT computes the highest order accessed during playback
// using GT effect numbers (before remapping).
// Returns maxOrder and the list of visited orders in playback sequence.
func CountMaxOrderGT(song parse.ParsedSong, frames int) (int, []int) {
	numOrders := song.NumOrders
	if numOrders == 0 {
		return 0, nil
	}

	// Build pattern info: for each pattern, find speed changes, break row, and jump target
	type patternInfo struct {
		breakRow     int
		jumpTarget   int // -1 if no jump, else target order
		speedChanges map[int]int
	}

	patternInfos := make(map[uint16]patternInfo)
	for addr, pat := range song.Patterns {
		info := patternInfo{
			breakRow:     64,
			jumpTarget:   -1,
			speedChanges: make(map[int]int),
		}
		for row := 0; row < 64; row++ {
			r := pat.Rows[row]
			// GT effect 0xB = position jump (triggers after this row)
			if r.Effect == gtEffectPosJump {
				breakAt := row + 1
				if breakAt <= info.breakRow {
					info.breakRow = breakAt
					info.jumpTarget = int(r.Param)
				}
			}
			// GT effect 0xD = pattern break (triggers after this row)
			if r.Effect == gtEffectBreak {
				breakAt := row + 1
				if breakAt < info.breakRow {
					info.breakRow = breakAt
					info.jumpTarget = -1
				}
			}
			// GT effect 0xF with param < 0x80 = speed
			if r.Effect == gtEffectSub && r.Param < 0x80 && r.Param > 0 {
				info.speedChanges[row] = int(r.Param)
			}
		}
		patternInfos[addr] = info
	}

	// For each order, find the earliest break row and jump target across all 3 channels
	type orderInfo struct {
		breakRow     int
		jumpTarget   int
		speedChanges map[int]int
	}
	orderInfos := make([]orderInfo, numOrders)
	for ord := 0; ord < numOrders; ord++ {
		info := orderInfo{
			breakRow:     64,
			jumpTarget:   -1,
			speedChanges: make(map[int]int),
		}
		for ch := 0; ch < 3; ch++ {
			if ord >= len(song.Orders[ch]) {
				continue
			}
			addr := song.Orders[ch][ord].PatternAddr
			pinfo, ok := patternInfos[addr]
			if !ok {
				continue
			}
			// Use earliest break row (or same row with jump)
			if pinfo.breakRow < info.breakRow {
				info.breakRow = pinfo.breakRow
				info.jumpTarget = pinfo.jumpTarget
			} else if pinfo.breakRow == info.breakRow {
				// Same break row - prefer jump over no-jump
				if pinfo.jumpTarget >= 0 {
					info.jumpTarget = pinfo.jumpTarget
				}
			}
			for row, spd := range pinfo.speedChanges {
				if row < info.breakRow {
					info.speedChanges[row] = spd
				}
			}
		}
		orderInfos[ord] = info
	}

	// Simulate order progression
	order := song.StartOrder
	row := 0
	speed := 6
	speedCounter := 5
	frameCount := 0
	maxOrder := song.StartOrder

	visited := make(map[int]bool)
	var visitedOrders []int
	visited[order] = true
	visitedOrders = append(visitedOrders, order)

	for frameCount < frames {
		frameCount++
		speedCounter++

		if speedCounter >= speed {
			speedCounter = 0

			if order >= numOrders {
				if order > maxOrder {
					maxOrder = order
				}
				order++
				row = 0
				continue
			}

			info := orderInfos[order]

			if newSpeed, ok := info.speedChanges[row]; ok {
				speed = newSpeed
			}

			row++

			if row >= info.breakRow {
				if order > maxOrder {
					maxOrder = order
				}
				// GT player behavior: advance to next order first, then apply jump
				nextOrder := order + 1
				if nextOrder < numOrders && !visited[nextOrder] {
					visited[nextOrder] = true
					visitedOrders = append(visitedOrders, nextOrder)
					if nextOrder > maxOrder {
						maxOrder = nextOrder
					}
				}

				// Now apply jump (or continue to next+1 if no jump)
				if info.jumpTarget >= 0 {
					order = info.jumpTarget
				} else {
					order = nextOrder
				}
				row = 0

				// Track the jump target order
				if order < numOrders && !visited[order] {
					visited[order] = true
					visitedOrders = append(visitedOrders, order)
					if order > maxOrder {
						maxOrder = order
					}
				}
			}
		}
	}

	return maxOrder, visitedOrders
}
