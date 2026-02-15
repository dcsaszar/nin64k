package encode

import (
	"sort"

	"forge/transform"
)

func OptimizePatternOrder(song transform.TransformedSong) []int {
	numPatterns := len(song.Patterns)
	numOrders := len(song.Orders[0])
	if numPatterns == 0 || numOrders == 0 {
		return nil
	}

	trackSeqs := [3][]int{}
	for ch := 0; ch < 3; ch++ {
		trackSeqs[ch] = make([]int, numOrders)
		for i, order := range song.Orders[ch] {
			trackSeqs[ch][i] = order.PatternIdx
		}
	}

	adjSet := make(map[[2]int]bool)
	for ch := 0; ch < 3; ch++ {
		for i := 1; i < len(trackSeqs[ch]); i++ {
			a, b := trackSeqs[ch][i-1], trackSeqs[ch][i]
			if a > b {
				a, b = b, a
			}
			if a != b {
				adjSet[[2]int{a, b}] = true
			}
		}
	}

	degree := make([]int, numPatterns)
	adj := make([][]int, numPatterns)
	for i := range adj {
		adj[i] = []int{}
	}
	pairs := make([][2]int, 0, len(adjSet))
	for pair := range adjSet {
		pairs = append(pairs, pair)
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i][0] != pairs[j][0] {
			return pairs[i][0] < pairs[j][0]
		}
		return pairs[i][1] < pairs[j][1]
	})
	for _, pair := range pairs {
		a, b := pair[0], pair[1]
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
		degree[a]++
		degree[b]++
	}
	for i := range adj {
		sort.Ints(adj[i])
	}

	countDeltas := func(mapping []int) int {
		var seen [256]bool
		count := 0
		for ch := 0; ch < 3; ch++ {
			seq := trackSeqs[ch]
			for i := 1; i < len(seq); i++ {
				d := mapping[seq[i]] - mapping[seq[i-1]]
				if d > 127 {
					d -= 256
				} else if d < -128 {
					d += 256
				}
				if !seen[d+128] {
					seen[d+128] = true
					count++
				}
			}
		}
		return count
	}

	optimizeFromStart := func(startNode int) ([]int, int) {
		visited := make([]bool, numPatterns)
		cmOrder := make([]int, 0, numPatterns)
		queue := []int{startNode}
		visited[startNode] = true
		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]
			cmOrder = append(cmOrder, curr)
			neighbors := make([]int, len(adj[curr]))
			copy(neighbors, adj[curr])
			sort.Slice(neighbors, func(i, j int) bool {
				if degree[neighbors[i]] != degree[neighbors[j]] {
					return degree[neighbors[i]] < degree[neighbors[j]]
				}
				return neighbors[i] < neighbors[j]
			})
			for _, n := range neighbors {
				if !visited[n] {
					visited[n] = true
					queue = append(queue, n)
				}
			}
		}
		for i := 0; i < numPatterns; i++ {
			if !visited[i] {
				cmOrder = append(cmOrder, i)
			}
		}
		mapping := make([]int, numPatterns)
		for newIdx, oldIdx := range cmOrder {
			mapping[oldIdx] = newIdx
		}
		posToPattern := make([]int, numPatterns)
		for pat, pos := range mapping {
			posToPattern[pos] = pat
		}
		bestScore := countDeltas(mapping)
		for bestScore > 32 {
			improved := false
			for i := 0; i < numPatterns; i++ {
				for j := i + 1; j < numPatterns; j++ {
					patI, patJ := posToPattern[i], posToPattern[j]
					mapping[patI], mapping[patJ] = j, i
					posToPattern[i], posToPattern[j] = patJ, patI
					if score := countDeltas(mapping); score < bestScore {
						bestScore = score
						improved = true
					} else {
						mapping[patI], mapping[patJ] = i, j
						posToPattern[i], posToPattern[j] = patI, patJ
					}
				}
			}
			if !improved {
				break
			}
		}
		return mapping, bestScore
	}

	minDeg := numPatterns + 1
	for i := 0; i < numPatterns; i++ {
		if degree[i] > 0 && degree[i] < minDeg {
			minDeg = degree[i]
		}
	}
	startCandidates := []int{}
	for deg := minDeg; deg <= minDeg+2 && len(startCandidates) < 24; deg++ {
		for i := 0; i < numPatterns && len(startCandidates) < 24; i++ {
			if degree[i] == deg {
				startCandidates = append(startCandidates, i)
			}
		}
	}
	if len(startCandidates) == 0 {
		startCandidates = []int{0}
	}

	type result struct {
		mapping []int
		score   int
		start   int
	}
	results := make(chan result, len(startCandidates))
	for _, startNode := range startCandidates {
		go func(s int) {
			m, sc := optimizeFromStart(s)
			results <- result{m, sc, s}
		}(startNode)
	}
	allResults := make([]result, 0, len(startCandidates))
	for range startCandidates {
		allResults = append(allResults, <-results)
	}
	sort.Slice(allResults, func(i, j int) bool {
		if allResults[i].score != allResults[j].score {
			return allResults[i].score < allResults[j].score
		}
		return allResults[i].start < allResults[j].start
	})

	if len(allResults) == 0 {
		identity := make([]int, numPatterns)
		for i := range identity {
			identity[i] = i
		}
		return identity
	}

	return allResults[0].mapping
}
