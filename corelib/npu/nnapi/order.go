package nnapi

import "sort"

// orderAccelRank lists device indexes, highest feature level first.
// Equal levels keep probe order.
func orderAccelRank(levels []int64) []int {
	idx := make([]int, len(levels))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return levels[idx[a]] > levels[idx[b]]
	})
	return idx
}
