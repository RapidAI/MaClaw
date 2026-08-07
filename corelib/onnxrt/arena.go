package onnxrt

import "sync"

// Output-tensor arena. Op outputs dominate the runtime's allocation volume
// (~79% of e2e bytes in the OCR pipeline), so instead of letting every
// kernel NewFloat its result from the GC heap, eligible intermediate tensors
// are carved out of a per-Run arena whose buffers are recycled: freed back
// the moment the holding value dies (liveness computed at NewGraph time) and
// retained across Runs via a small pool of arenas.
//
// Safety invariants:
//   - A value dies after its last consumer node executes. Graph inputs,
//     initializers and graph outputs never die: their groups are excluded
//     from the arena, so Run results never alias recyclable memory.
//   - View ops (Identity/Squeeze/Unsqueeze/Reshape) alias their input's
//     backing buffer. Aliased values form one liveness group; the buffer
//     lives until the last group member dies and is freed exactly once, by
//     the group's root (the value produced by the non-view op).
//   - The arena is owned exclusively by one in-flight Run; the pool only
//     hands out whole arenas. No state is shared between concurrent Runs.
//   - Ops that accumulate into their output (ConvTranspose col2im
//     scatter-add, ReduceMean) get a zeroed buffer on checkout
//     (arenaPlan.needsZero). Every other kernel fully overwrites its output.

// arenaMaxElems caps how many floats one arena retains (512 MB). Buffers
// returned past the cap are dropped for the GC.
const arenaMaxElems = 128 << 20

// maxPooledArenas bounds how many arenas are retained for reuse across Runs.
// One arena is checked out per in-flight Run, so this also bounds the number
// of concurrent Runs whose working sets stay cached.
const maxPooledArenas = 8

// tensorArena is a best-fit free list of float32 buffers. Not safe for
// concurrent use by itself; each Run owns its arena exclusively.
type tensorArena struct {
	free  [][]float32
	elems int // total cap of buffers in free
}

// get returns a buffer of length n, reusing the smallest free buffer that
// fits or allocating a fresh one. The contents are unspecified.
func (a *tensorArena) get(n int) []float32 {
	best := -1
	for i, b := range a.free {
		if cap(b) >= n && (best < 0 || cap(b) < cap(a.free[best])) {
			best = i
		}
	}
	if best < 0 {
		return make([]float32, n)
	}
	b := a.free[best]
	a.free = append(a.free[:best], a.free[best+1:]...)
	a.elems -= cap(b)
	return b[:n]
}

// put returns a buffer to the free list.
func (a *tensorArena) put(b []float32) {
	if cap(b) == 0 || a.elems+cap(b) > arenaMaxElems {
		return // drop for the GC
	}
	a.free = append(a.free, b)
	a.elems += cap(b)
}

// arenaPool retains finished Runs' arenas so the next Run reuses their
// buffers instead of re-allocating the whole working set. The mutex only
// guards checkout/checkin of whole arenas.
var arenaPool = struct {
	sync.Mutex
	list []*tensorArena
}{}

func acquireArena() *tensorArena {
	arenaPool.Lock()
	defer arenaPool.Unlock()
	n := len(arenaPool.list)
	if n == 0 {
		return &tensorArena{}
	}
	a := arenaPool.list[n-1]
	arenaPool.list = arenaPool.list[:n-1]
	return a
}

func releaseArena(a *tensorArena) {
	arenaPool.Lock()
	defer arenaPool.Unlock()
	if len(arenaPool.list) < maxPooledArenas {
		arenaPool.list = append(arenaPool.list, a)
	}
}

// arenaPlan is the frozen result of the load-time liveness/alias analysis.
type arenaPlan struct {
	eligible  map[string]bool // value name -> its float32 buffer may come from the arena
	needsZero map[*Node]bool  // nodes whose output kernel accumulates into zeroed memory
	freeAfter [][]string      // per topo node index: eligible roots whose buffer dies after that node
}

// viewOps alias their first input's backing buffer instead of allocating.
var viewOps = map[string]bool{
	"Identity": true, "Squeeze": true, "Unsqueeze": true, "Reshape": true,
}

// computeArenaPlan derives value liveness over the topologically sorted node
// list and decides which node outputs may be arena-allocated.
func (g *Graph) computeArenaPlan() {
	nodes := g.nodes
	plan := &arenaPlan{
		eligible:  map[string]bool{},
		needsZero: map[*Node]bool{},
		freeAfter: make([][]string, len(nodes)),
	}
	if len(nodes) == 0 {
		g.plan = plan
		return
	}

	producer := map[string]int{} // value -> topo index of producing node
	lastUse := map[string]int{}  // value -> topo index of last consumer
	producedByView := map[string]bool{}
	for i, nd := range nodes {
		for _, out := range nd.Outputs {
			if out == "" {
				continue
			}
			producer[out] = i
			if viewOps[nd.OpType] {
				producedByView[out] = true
			}
		}
		for _, in := range nd.Inputs {
			if in != "" {
				lastUse[in] = i
			}
		}
	}

	// Immortal values can never be freed; any alias group touching one is
	// excluded from the arena.
	immortal := map[string]bool{}
	for name := range g.initializers {
		immortal[name] = true
	}
	for _, name := range g.inputNames {
		immortal[name] = true
	}
	for _, name := range g.outputNames {
		immortal[name] = true
	}

	// Union-find over view ops: an aliased chain shares one buffer.
	parent := map[string]string{}
	var find func(string) string
	find = func(x string) string {
		p, ok := parent[x]
		if !ok || p == x {
			parent[x] = x
			return x
		}
		r := find(p)
		parent[x] = r
		return r
	}
	union := func(a, b string) { parent[find(a)] = find(b) }
	for _, nd := range nodes {
		if !viewOps[nd.OpType] || len(nd.Inputs) == 0 || nd.Inputs[0] == "" {
			continue
		}
		for _, out := range nd.Outputs {
			if out != "" {
				union(out, nd.Inputs[0])
			}
		}
	}

	// Gather group members.
	groups := map[string][]string{}
	for name := range producer {
		r := find(name)
		groups[r] = append(groups[r], name)
	}
	// View ops can alias a graph input/initializer that has no producer;
	// those groups are ineligible anyway (immortal), so skipping non-produced
	// members only loses nothing.

	for _, members := range groups {
		// The root is the unique member produced by a non-view op.
		root := ""
		roots := 0
		death := -1
		bad := false
		for _, name := range members {
			if immortal[name] {
				bad = true
				break
			}
			if !producedByView[name] {
				root = name
				roots++
			}
			d := producer[name] // dead code: dies right after production
			if u, ok := lastUse[name]; ok {
				d = u
			}
			if d > death {
				death = d
			}
		}
		if bad || roots != 1 {
			continue
		}
		plan.eligible[root] = true
		plan.freeAfter[death] = append(plan.freeAfter[death], root)
	}

	for _, nd := range nodes {
		switch nd.OpType {
		case "ConvTranspose", "ReduceMean":
			plan.needsZero[nd] = true
		}
	}
	g.plan = plan
}

// runCtx carries the per-Run state into kernels. It embeds the frozen graph
// so kernel code keeps working unchanged; the arena is exclusively owned by
// the calling Run. A nil runCtx (direct kernel calls in tests) falls back to
// plain heap allocation everywhere.
type runCtx struct {
	*Graph
	arena *tensorArena
}

// graf returns the underlying graph, tolerating a nil runCtx.
func (rc *runCtx) graf() *Graph {
	if rc == nil {
		return nil
	}
	return rc.Graph
}

// newFloat allocates a kernel's output tensor, sourcing the backing buffer
// from the Run's arena when the value's liveness group is eligible. Buffers
// for accumulating kernels (needsZero) are cleared on checkout; all other
// kernels fully overwrite their output.
func (rc *runCtx) newFloat(n *Node, ord int, shape ...int) *Tensor {
	g := rc.graf()
	if rc == nil || rc.arena == nil || g == nil || g.plan == nil || n == nil || ord >= len(n.Outputs) {
		return NewFloat(shape...)
	}
	name := n.Outputs[ord]
	if name == "" || !g.plan.eligible[name] {
		return NewFloat(shape...)
	}
	nElem := numElements(shape)
	if nElem == 0 {
		return NewFloat(shape...)
	}
	buf := rc.arena.get(nElem)
	if g.plan.needsZero[n] {
		clear(buf)
	}
	return &Tensor{Shape: cloneInts(shape), DType: DFloat32, F32: buf, abuf: buf}
}

// freeDead returns arena buffers whose holding value died after the node at
// topo index idx executed. Each eligible buffer is freed exactly once, via
// its group's root value.
func (rc *runCtx) freeDead(idx int, values map[string]*Tensor) {
	if rc.arena == nil || rc.plan == nil {
		return
	}
	for _, name := range rc.plan.freeAfter[idx] {
		t := values[name]
		if t != nil && t.abuf != nil {
			rc.arena.put(t.abuf)
			t.abuf = nil
		}
	}
}
