package onnxrt

// NodesForDebug exposes the executed node list for debugging/tooling.
func (g *Graph) NodesForDebug() []*Node { return g.nodes }

// ArenaStatsForDebug reports the arena plan: eligible value count, scheduled
// frees, and needsZero node count.
func (g *Graph) ArenaStatsForDebug() (eligible, frees, needsZero int) {
	if g.plan == nil {
		return 0, 0, 0
	}
	for _, l := range g.plan.freeAfter {
		frees += len(l)
	}
	return len(g.plan.eligible), frees, len(g.plan.needsZero)
}

// EpilogueCountForDebug returns the number of fused conv epilogues by kind.
func (g *Graph) EpilogueCountForDebug() map[EpilogueKind]int {
	out := map[EpilogueKind]int{}
	for _, e := range g.epilogues {
		out[e.kind]++
	}
	return out
}
