package solve

import (
	"sort"
	"sync"
	"sync/atomic"
)

type DeltaTableResult struct {
	Table      []int8
	Bases      [9]int
	SongSets   [9][]int
	StartConst int
}

const DeltaEmpty int8 = -128

type deltaSolverState struct {
	sorted [9][]int
	needs  [9][256]bool
	window int
}

func newDeltaSolverWithWindow(songSets [9][]int, window int) *deltaSolverState {
	s := &deltaSolverState{window: window}
	for i, set := range songSets {
		if len(set) == 0 {
			continue
		}
		s.sorted[i] = make([]int, len(set))
		copy(s.sorted[i], set)
		sort.Ints(s.sorted[i])
		for _, e := range set {
			s.needs[i][e+128] = true
		}
	}
	return s
}

func (s *deltaSolverState) buildLen(order []int, maxLen int) int {
	w := s.window
	var arr [512]int8
	for i := range arr {
		arr[i] = DeltaEmpty
	}
	elems := s.sorted[order[0]]
	for i, e := range elems {
		arr[i] = int8(e)
	}
	arrLen := w
	for orderIdx := 1; orderIdx < 9; orderIdx++ {
		song := order[orderIdx]
		elems := s.sorted[song]
		if len(elems) == 0 {
			continue
		}
		needs := &s.needs[song]
		songSize := len(elems)
		bestBase, bestCost := arrLen, w
		var covCount [256]int8
		emptySlots := 0
		for i := 0; i < w && i < arrLen; i++ {
			if arr[i] == DeltaEmpty {
				emptySlots++
			} else {
				covCount[int(arr[i])+128]++
			}
		}
		if w > arrLen {
			emptySlots += w - arrLen
		}
		missing := songSize
		for i := range covCount {
			if needs[i] && covCount[i] > 0 {
				missing--
			}
		}
		if missing <= emptySlots {
			cost := w - arrLen
			if cost < 0 {
				cost = 0
			}
			if cost < bestCost {
				bestCost, bestBase = cost, 0
			}
		}
		for base := 1; base <= arrLen; base++ {
			oldPos, newPos := base-1, base+w-1
			if arr[oldPos] == DeltaEmpty {
				emptySlots--
			} else {
				idx := int(arr[oldPos]) + 128
				covCount[idx]--
				if needs[idx] && covCount[idx] == 0 {
					missing++
				}
			}
			if newPos < arrLen {
				if arr[newPos] == DeltaEmpty {
					emptySlots++
				} else {
					idx := int(arr[newPos]) + 128
					if needs[idx] && covCount[idx] == 0 {
						missing--
					}
					covCount[idx]++
				}
			} else {
				emptySlots++
			}
			if missing <= emptySlots {
				cost := base + w - arrLen
				if cost < 0 {
					cost = 0
				}
				if cost < bestCost {
					bestCost, bestBase = cost, base
				}
			}
		}
		newLen := bestBase + w
		if newLen > arrLen {
			arrLen = newLen
		}
		if arrLen >= maxLen {
			return -1
		}
		var covered [256]bool
		for i := bestBase; i < bestBase+w; i++ {
			if arr[i] != DeltaEmpty {
				covered[int(arr[i])+128] = true
			}
		}
		slot := bestBase
		for _, e := range elems {
			if !covered[e+128] {
				for arr[slot] != DeltaEmpty {
					slot++
				}
				arr[slot] = int8(e)
				slot++
			}
		}
	}
	return arrLen
}

func (s *deltaSolverState) build(order []int) DeltaTableResult {
	w := s.window
	var arr [512]int8
	for i := range arr {
		arr[i] = DeltaEmpty
	}
	var bases [9]int
	elems := s.sorted[order[0]]
	for i, e := range elems {
		arr[i] = int8(e)
	}
	arrLen := w
	bases[order[0]] = 0
	for orderIdx := 1; orderIdx < 9; orderIdx++ {
		song := order[orderIdx]
		elems := s.sorted[song]
		if len(elems) == 0 {
			continue
		}
		needs := &s.needs[song]
		songSize := len(elems)
		bestBase, bestCost := arrLen, w
		var covCount [256]int8
		emptySlots := 0
		for i := 0; i < w && i < arrLen; i++ {
			if arr[i] == DeltaEmpty {
				emptySlots++
			} else {
				covCount[int(arr[i])+128]++
			}
		}
		if w > arrLen {
			emptySlots += w - arrLen
		}
		missing := songSize
		for i := range covCount {
			if needs[i] && covCount[i] > 0 {
				missing--
			}
		}
		if missing <= emptySlots {
			cost := w - arrLen
			if cost < 0 {
				cost = 0
			}
			if cost < bestCost {
				bestCost, bestBase = cost, 0
			}
		}
		for base := 1; base <= arrLen; base++ {
			oldPos, newPos := base-1, base+w-1
			if arr[oldPos] == DeltaEmpty {
				emptySlots--
			} else {
				idx := int(arr[oldPos]) + 128
				covCount[idx]--
				if needs[idx] && covCount[idx] == 0 {
					missing++
				}
			}
			if newPos < arrLen {
				if arr[newPos] == DeltaEmpty {
					emptySlots++
				} else {
					idx := int(arr[newPos]) + 128
					if needs[idx] && covCount[idx] == 0 {
						missing--
					}
					covCount[idx]++
				}
			} else {
				emptySlots++
			}
			if missing <= emptySlots {
				cost := base + w - arrLen
				if cost < 0 {
					cost = 0
				}
				if cost < bestCost {
					bestCost, bestBase = cost, base
				}
			}
		}
		if bestBase+w > arrLen {
			arrLen = bestBase + w
		}
		bases[song] = bestBase
		var covered [256]bool
		for i := bestBase; i < bestBase+w && i < len(arr); i++ {
			if arr[i] != DeltaEmpty {
				covered[int(arr[i])+128] = true
			}
		}
		for _, e := range elems {
			if !covered[e+128] {
				placed := false
				for slot := bestBase; slot < bestBase+w; slot++ {
					if arr[slot] == DeltaEmpty {
						arr[slot] = int8(e)
						placed = true
						break
					}
				}
				if !placed {
					for slot := bestBase; slot < bestBase+w; slot++ {
						if arr[slot] == int8(e) {
							placed = true
							break
						}
					}
				}
			}
		}
	}
	return DeltaTableResult{Table: append([]int8{}, arr[:arrLen]...), Bases: bases, SongSets: s.sorted}
}

func (s *deltaSolverState) searchWithFirst(first int, globalBest *int32) DeltaTableResult {
	var bestResult DeltaTableResult
	perm := [9]int{}
	j := 0
	for i := 0; i < 9; i++ {
		if i == first {
			continue
		}
		perm[j+1] = i
		j++
	}
	perm[0] = first
	var c [8]int
	maxLen := int(atomic.LoadInt32(globalBest))
	l := s.buildLen(perm[:], maxLen)
	if l > 0 && l < maxLen {
		bestResult = s.build(perm[:])
		for {
			old := atomic.LoadInt32(globalBest)
			if int32(l) >= old || atomic.CompareAndSwapInt32(globalBest, old, int32(l)) {
				break
			}
		}
	}
	i := 0
	for i < 8 {
		if c[i] < i {
			if i&1 == 0 {
				perm[1], perm[i+1] = perm[i+1], perm[1]
			} else {
				perm[c[i]+1], perm[i+1] = perm[i+1], perm[c[i]+1]
			}
			maxLen := int(atomic.LoadInt32(globalBest))
			l := s.buildLen(perm[:], maxLen)
			if l > 0 && l < maxLen {
				bestResult = s.build(perm[:])
				for {
					old := atomic.LoadInt32(globalBest)
					if int32(l) >= old || atomic.CompareAndSwapInt32(globalBest, old, int32(l)) {
						break
					}
				}
			}
			c[i]++
			i = 0
		} else {
			c[i] = 0
			i++
		}
	}
	return bestResult
}

func SolveDeltaTableWithWindow(songSets [9][]int, window int) DeltaTableResult {
	s := newDeltaSolverWithWindow(songSets, window)
	results := make(chan DeltaTableResult, 9)
	var globalBest int32 = 9999
	var wg sync.WaitGroup
	for first := 0; first < 9; first++ {
		if len(songSets[first]) == 0 {
			continue
		}
		wg.Add(1)
		go func(f int) {
			defer wg.Done()
			results <- s.searchWithFirst(f, &globalBest)
		}(first)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	bestLen := 9999
	var bestResult DeltaTableResult
	for r := range results {
		if len(r.Table) > 0 && len(r.Table) < bestLen {
			r.SongSets = songSets
			if verifyDeltaTableInternal(r, window) {
				bestLen = len(r.Table)
				bestResult = r
			}
		}
	}
	if len(bestResult.Table) == 0 {
		bestResult = solveGreedy(songSets, window)
	}
	bestResult.SongSets = songSets
	return bestResult
}

func verifyDeltaTableInternal(result DeltaTableResult, window int) bool {
	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(result.SongSets[songIdx]) == 0 {
			continue
		}
		base := result.Bases[songIdx]
		found := make(map[int]bool)
		for i := base; i < base+window && i < len(result.Table); i++ {
			if result.Table[i] != DeltaEmpty {
				found[int(result.Table[i])] = true
			}
		}
		for _, e := range result.SongSets[songIdx] {
			if !found[e] {
				return false
			}
		}
	}
	return true
}

func solveGreedy(songSets [9][]int, window int) DeltaTableResult {
	var table []int8
	var bases [9]int

	type songInfo struct {
		idx  int
		size int
	}
	var songs []songInfo
	for i, set := range songSets {
		if len(set) > 0 {
			songs = append(songs, songInfo{i, len(set)})
		}
	}
	sort.Slice(songs, func(i, j int) bool {
		return songs[i].size > songs[j].size
	})

	for _, s := range songs {
		set := songSets[s.idx]
		bestBase := len(table)
		bestOverlap := 0

		for base := 0; base <= len(table); base++ {
			overlap := 0
			missing := 0
			for _, val := range set {
				foundInWindow := false
				for i := base; i < base+window && i < len(table); i++ {
					if table[i] == int8(val) {
						foundInWindow = true
						overlap++
						break
					}
				}
				if !foundInWindow {
					missing++
				}
			}

			emptyInWindow := 0
			for i := base; i < base+window; i++ {
				if i >= len(table) || table[i] == DeltaEmpty {
					emptyInWindow++
				}
			}

			if missing <= emptyInWindow && overlap > bestOverlap {
				bestOverlap = overlap
				bestBase = base
			}
		}

		for len(table) < bestBase+window {
			table = append(table, DeltaEmpty)
		}

		bases[s.idx] = bestBase
		for _, val := range set {
			foundInWindow := false
			for i := bestBase; i < bestBase+window; i++ {
				if table[i] == int8(val) {
					foundInWindow = true
					break
				}
			}
			if !foundInWindow {
				for i := bestBase; i < bestBase+window; i++ {
					if table[i] == DeltaEmpty {
						table[i] = int8(val)
						break
					}
				}
			}
		}
	}

	return DeltaTableResult{Table: table, Bases: bases}
}

func SolveDeltaTable(songSets [9][]int) DeltaTableResult {
	return SolveDeltaTableWithWindow(songSets, 32)
}

func VerifyDeltaTable(result DeltaTableResult, window int) bool {
	allOk := true
	for songIdx := 0; songIdx < 9; songIdx++ {
		if len(result.SongSets[songIdx]) == 0 {
			continue
		}
		base := result.Bases[songIdx]
		found := make(map[int]bool)
		for i := base; i < base+window && i < len(result.Table); i++ {
			if result.Table[i] != DeltaEmpty {
				found[int(result.Table[i])] = true
			}
		}
		for _, e := range result.SongSets[songIdx] {
			if !found[e] {
				allOk = false
			}
		}
	}
	return allOk
}
