package onnxrt

import (
	"testing"
)

// Adversarial tests for the output-tensor arena (arena.go). Each test builds
// a graph that hits one failure mode the arena must prevent and would FAIL
// under the naive variant of the offending mechanism:
//
//   - TestArenaViewOutlivesSourceLastUse: naive last-use freeing (free the
//     root buffer at the root's own last use) hands the view's backing buffer
//     to a later node, clobbering the view before its final read. Only the
//     union-find alias group (buffer dies at the last GROUP member's death)
//     keeps the values intact.
//   - TestArenaGraphOutputNeverRecycled: graph outputs (and values aliasing
//     them) must never come from the arena, or a later Run recycles the
//     buffer under a live result tensor.
//   - TestArenaNeedsZeroDirtyReuse: ConvTranspose (col2im scatter-add) and
//     ReduceMean (generic path) accumulate into their output, so an arena
//     checkout holding garbage must be cleared first (arenaPlan.needsZero).
//
// The graphs are crafted so the dirty buffer reuse is DETERMINISTIC: a
// same-sized eligible value dies immediately before the accumulating op
// executes, and the arena best-fit free list then hands its (non-zero) buffer
// to the accumulator. White-box plan assertions pin the exact free sites.

// arenaTestNode is a small builder for readable test graphs.
func arenaTestNode(name, op string, inputs, outputs []string, attrs map[string]Attr) *Node {
	if attrs == nil {
		attrs = map[string]Attr{}
	}
	return &Node{Name: name, OpType: op, Inputs: inputs, Outputs: outputs, Attrs: attrs}
}

// topoIndexOf returns the index of the named node in the sorted graph.
func topoIndexOf(t *testing.T, g *Graph, name string) int {
	t.Helper()
	for i, nd := range g.nodes {
		if nd.Name == name {
			return i
		}
	}
	t.Fatalf("node %q not in topo order", name)
	return -1
}

// planFreedAfter asserts the value's group root is freed exactly after the
// topo node with index idx — and at no earlier index.
func planFreedAfter(t *testing.T, g *Graph, root string, idx int) {
	t.Helper()
	for i, frees := range g.plan.freeAfter {
		for _, name := range frees {
			if name != root {
				continue
			}
			if i != idx {
				t.Fatalf("plan frees %q after topo index %d, want %d", root, i, idx)
			}
			return
		}
	}
	t.Fatalf("plan never frees %q (not eligible?)", root)
}

func wantF32Exact(t *testing.T, got *Tensor, want []float32) {
	t.Helper()
	if got == nil {
		t.Fatal("missing output tensor")
	}
	if len(got.F32) != len(want) {
		t.Fatalf("len %d, want %d (shape %v)", len(got.F32), len(want), got.Shape)
	}
	for i, v := range want {
		if got.F32[i] != v {
			t.Fatalf("out[%d] = %v, want %v (full %v)", i, got.F32[i], v, got.F32)
		}
	}
}

// TestArenaViewOutlivesSourceLastUse crafts graphs where a view op's output
// is read AFTER its source's last non-view use, with a same-sized buffer
// allocated in between. Under naive last-use freeing the in-between node
// would check out the source's freed buffer and overwrite the data the view
// still aliases; the final Add would then read junk+junk instead of
// src+junk. The alias-group plan keeps the buffer alive until the view's
// last read, which the white-box freeAfter assertions pin down.
func TestArenaViewOutlivesSourceLastUse(t *testing.T) {
	// reshape: add0 -> reshape(view) -> mul(junk, would reuse src's buffer
	// under naive freeing) -> reshape(junkV) -> add4(view, junkV).
	t.Run("Reshape", func(t *testing.T) {
		m := &Model{Opset: 13, Graph: &GraphProto{
			Nodes: []*Node{
				arenaTestNode("add0", "Add", []string{"x", "w"}, []string{"src"}, nil),
				arenaTestNode("view", "Reshape", []string{"src", "shp"}, []string{"vw"}, nil),
				arenaTestNode("mul2", "Mul", []string{"x", "w2"}, []string{"junk"}, nil),
				arenaTestNode("view2", "Reshape", []string{"junk", "shp"}, []string{"junkV"}, nil),
				arenaTestNode("add4", "Add", []string{"vw", "junkV"}, []string{"out"}, nil),
			},
			Initializers: map[string]*TensorProto{
				"w":   f32Init("w", []int64{2, 2}, []float32{10, 10, 10, 10}),
				"w2":  f32Init("w2", []int64{2, 2}, []float32{2, 2, 2, 2}),
				"shp": i64Init("shp", []int64{1}, []int64{4}),
			},
			Inputs:  []ValueInfo{{Name: "x"}},
			Outputs: []ValueInfo{{Name: "out"}},
		}}
		g, err := NewGraph(m)
		if err != nil {
			t.Fatal(err)
		}
		// White-box: src and junk are arena-eligible, and src's buffer dies
		// only with the final Add (the view's last read) — NOT after the
		// Reshape (src's own last use), where naive freeing would put it.
		if !g.plan.eligible["src"] || !g.plan.eligible["junk"] {
			t.Fatalf("expected src and junk arena-eligible, plan=%v", g.plan.eligible)
		}
		planFreedAfter(t, g, "src", topoIndexOf(t, g, "add4"))
		planFreedAfter(t, g, "junk", topoIndexOf(t, g, "add4"))

		x := FloatFrom([]float32{1, 2, 3, 4}, 2, 2)
		outs, err := g.Run(map[string]*Tensor{"x": x})
		if err != nil {
			t.Fatal(err)
		}
		// src = x+10 = [11,12,13,14]; junk = 2x = [2,4,6,8]; out = src+junk.
		wantF32Exact(t, outs["out"], []float32{13, 16, 19, 22})
		// Second run reuses the pooled arena (dirty buffers); same result.
		outs, err = g.Run(map[string]*Tensor{"x": x})
		if err != nil {
			t.Fatal(err)
		}
		wantF32Exact(t, outs["out"], []float32{13, 16, 19, 22})
	})

	// squeeze chain: add0 -> squeeze(view); src's LAST NON-VIEW use is the
	// Relu AFTER the view was created; the Mul then allocates a same-sized
	// buffer (src's, under naive freeing) before the view is finally read.
	t.Run("SqueezeChain", func(t *testing.T) {
		axes0 := map[string]Attr{"axes": {Name: "axes", Type: 7, IntVals: []int64{0}}}
		m := &Model{Opset: 13, Graph: &GraphProto{
			Nodes: []*Node{
				arenaTestNode("add0", "Add", []string{"x", "w"}, []string{"src"}, nil),
				arenaTestNode("sq", "Squeeze", []string{"src"}, []string{"vw"}, axes0),
				arenaTestNode("relu", "Relu", []string{"src"}, []string{"mid"}, nil),
				arenaTestNode("mul", "Mul", []string{"mid", "w2"}, []string{"junk"}, nil),
				arenaTestNode("sq2", "Squeeze", []string{"junk"}, []string{"junkV"}, axes0),
				arenaTestNode("add5", "Add", []string{"vw", "junkV"}, []string{"out"}, nil),
			},
			Initializers: map[string]*TensorProto{
				"w":  f32Init("w", []int64{1, 2, 2}, []float32{10, 10, 10, 10}),
				"w2": f32Init("w2", []int64{1, 2, 2}, []float32{2, 2, 2, 2}),
			},
			Inputs:  []ValueInfo{{Name: "x"}},
			Outputs: []ValueInfo{{Name: "out"}},
		}}
		g, err := NewGraph(m)
		if err != nil {
			t.Fatal(err)
		}
		if !g.plan.eligible["src"] || !g.plan.eligible["junk"] {
			t.Fatalf("expected src and junk arena-eligible, plan=%v", g.plan.eligible)
		}
		// src's own last use is the Relu; its alias group must live until the
		// final Add reads the Squeeze view.
		planFreedAfter(t, g, "src", topoIndexOf(t, g, "add5"))

		x := FloatFrom([]float32{1, 2, 3, 4}, 1, 2, 2)
		outs, err := g.Run(map[string]*Tensor{"x": x})
		if err != nil {
			t.Fatal(err)
		}
		// src = [11,12,13,14]; junk = 2*relu(src) = [22,24,26,28].
		wantF32Exact(t, outs["out"], []float32{33, 36, 39, 42})
	})
}

// TestArenaGraphOutputNeverRecycled keeps Run-1's output tensors live across
// Run-2 (and later runs) with different inputs. Graph outputs — including an
// Identity that aliases an intermediate — are excluded from the arena, so no
// later Run may recycle their backing memory. Under a plan that excluded only
// the output NAME but not its alias group, the Identity's source would be
// arena-allocated and clobbered (within Run-1 already, by the junk chain).
func TestArenaGraphOutputNeverRecycled(t *testing.T) {
	m := &Model{Opset: 13, Graph: &GraphProto{
		Nodes: []*Node{
			arenaTestNode("add", "Add", []string{"x", "w"}, []string{"inter"}, nil),
			// Graph-output Identity survives load-time Identity elimination
			// (the output name is part of the Run contract) and aliases inter.
			arenaTestNode("id", "Identity", []string{"inter"}, []string{"out"}, nil),
			// Junk chain: same-sized eligible buffers churned after inter dies.
			arenaTestNode("mul1", "Mul", []string{"x", "w2"}, []string{"j1"}, nil),
			arenaTestNode("add2", "Add", []string{"j1", "w3"}, []string{"j2"}, nil),
			arenaTestNode("mul3", "Mul", []string{"j2", "w4"}, []string{"j3"}, nil),
			arenaTestNode("add4", "Add", []string{"j3", "w5"}, []string{"out2"}, nil),
		},
		Initializers: map[string]*TensorProto{
			"w":  f32Init("w", []int64{4}, []float32{10, 10, 10, 10}),
			"w2": f32Init("w2", []int64{4}, []float32{2, 2, 2, 2}),
			"w3": f32Init("w3", []int64{4}, []float32{3, 3, 3, 3}),
			"w4": f32Init("w4", []int64{4}, []float32{4, 4, 4, 4}),
			"w5": f32Init("w5", []int64{4}, []float32{5, 5, 5, 5}),
		},
		Inputs:  []ValueInfo{{Name: "x"}},
		Outputs: []ValueInfo{{Name: "out"}, {Name: "out2"}},
	}}
	g, err := NewGraph(m)
	if err != nil {
		t.Fatal(err)
	}
	// The Identity output is immortal, so the whole alias group — inter
	// included — must be kept out of the arena; the junk chain stays eligible.
	if g.plan.eligible["inter"] {
		t.Fatal("inter (aliased by graph output) must not be arena-eligible")
	}
	if !g.plan.eligible["j1"] || !g.plan.eligible["j2"] || !g.plan.eligible["j3"] {
		t.Fatalf("junk chain should be arena-eligible, plan=%v", g.plan.eligible)
	}

	outs1, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{1, 1, 1, 1}, 4)})
	if err != nil {
		t.Fatal(err)
	}
	o1, o2 := outs1["out"], outs1["out2"]
	// inter = 1+10 = 11; out2 = ((1*2)+3)*4+5 = 25.
	wantF32Exact(t, o1, []float32{11, 11, 11, 11})
	wantF32Exact(t, o2, []float32{25, 25, 25, 25})
	if o1.abuf != nil || o2.abuf != nil {
		t.Fatal("graph outputs must not be arena-backed")
	}

	// Run-2 with different inputs must not clobber Run-1's retained outputs.
	outs2, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{100, 100, 100, 100}, 4)})
	if err != nil {
		t.Fatal(err)
	}
	wantF32Exact(t, outs2["out"], []float32{110, 110, 110, 110})
	wantF32Exact(t, outs2["out2"], []float32{817, 817, 817, 817}) // ((100*2)+3)*4+5
	wantF32Exact(t, o1, []float32{11, 11, 11, 11})
	wantF32Exact(t, o2, []float32{25, 25, 25, 25})

	// Hammer a few more runs (arena pool reuse) and re-check Run-1 outputs.
	for i := 0; i < 8; i++ {
		v := float32(i * 7)
		if _, err := g.Run(map[string]*Tensor{"x": FloatFrom([]float32{v, v, v, v}, 4)}); err != nil {
			t.Fatal(err)
		}
	}
	wantF32Exact(t, o1, []float32{11, 11, 11, 11})
	wantF32Exact(t, o2, []float32{25, 25, 25, 25})
}

// TestArenaNeedsZeroDirtyReuse feeds the accumulating kernels a dirty arena
// buffer: a same-sized eligible value holding large non-zero garbage dies
// immediately before the accumulator runs, so the arena best-fit checkout is
// guaranteed to hand over the dirty buffer. Without needsZero clearing, the
// results would be offset by the garbage.
func TestArenaNeedsZeroDirtyReuse(t *testing.T) {
	// ReduceMean generic path (non-suffix axes) accumulates out.F32[off] += .
	t.Run("ReduceMean", func(t *testing.T) {
		m := &Model{Opset: 13, Graph: &GraphProto{
			Nodes: []*Node{
				arenaTestNode("mul0", "Mul", []string{"xa", "w"}, []string{"a"}, nil),
				arenaTestNode("relu1", "Relu", []string{"xb"}, []string{"z"}, nil),
				arenaTestNode("add2", "Add", []string{"a", "wb"}, []string{"b"}, nil),
				arenaTestNode("mean3", "ReduceMean", []string{"z"}, []string{"m"},
					map[string]Attr{"axes": {Name: "axes", Type: 7, IntVals: []int64{1}}}),
				arenaTestNode("add4", "Add", []string{"b", "m"}, []string{"out"}, nil),
			},
			Initializers: map[string]*TensorProto{
				"w":  f32Init("w", []int64{1, 1, 3}, []float32{7, 8, 9}),
				"wb": f32Init("wb", []int64{1, 1, 3}, []float32{1, 1, 1}),
			},
			Inputs:  []ValueInfo{{Name: "xa"}, {Name: "xb"}},
			Outputs: []ValueInfo{{Name: "out"}},
		}}
		g, err := NewGraph(m)
		if err != nil {
			t.Fatal(err)
		}
		meanIdx := topoIndexOf(t, g, "mean3")
		if !g.plan.needsZero[g.nodes[meanIdx]] {
			t.Fatal("ReduceMean node missing needsZero")
		}
		if !g.plan.eligible["a"] || !g.plan.eligible["m"] {
			t.Fatalf("expected a and m arena-eligible, plan=%v", g.plan.eligible)
		}
		// a dies right before the ReduceMean runs, so m's checkout reuses a's
		// dirty [7,8,9] buffer.
		planFreedAfter(t, g, "a", topoIndexOf(t, g, "add2"))

		run := func() map[string]*Tensor {
			outs, err := g.Run(map[string]*Tensor{
				"xa": FloatFrom([]float32{1, 1, 1}, 1, 1, 3),          // a = [7,8,9] garbage
				"xb": FloatFrom([]float32{1, 2, 3, 4, 5, 6}, 1, 2, 3), // z = relu = same
			})
			if err != nil {
				t.Fatal(err)
			}
			return outs
		}
		// m = mean over axis 1 = [2.5, 3.5, 4.5]; b = a+1 = [8,9,10].
		want := []float32{10.5, 12.5, 14.5}
		wantF32Exact(t, run()["out"], want)
		// Second run: pooled arena hands back run-1's dirty buffers.
		wantF32Exact(t, run()["out"], want)
	})

	// ConvTranspose col2im scatter-add accumulates out.F32[...] += .
	t.Run("ConvTranspose", func(t *testing.T) {
		m := &Model{Opset: 13, Graph: &GraphProto{
			Nodes: []*Node{
				arenaTestNode("mul0", "Mul", []string{"xa", "w9"}, []string{"a"}, nil),
				arenaTestNode("relu1", "Relu", []string{"a"}, []string{"b"}, nil),
				arenaTestNode("ct2", "ConvTranspose", []string{"xc", "wct"}, []string{"ct"}, nil),
				arenaTestNode("add3", "Add", []string{"b", "ct"}, []string{"out"}, nil),
			},
			Initializers: map[string]*TensorProto{
				"w9":  f32Init("w9", []int64{1, 1, 3, 3}, []float32{5, 5, 5, 5, 5, 5, 5, 5, 5}),
				"wct": f32Init("wct", []int64{1, 1, 2, 2}, []float32{1, 1, 1, 1}),
			},
			Inputs:  []ValueInfo{{Name: "xa"}, {Name: "xc"}},
			Outputs: []ValueInfo{{Name: "out"}},
		}}
		g, err := NewGraph(m)
		if err != nil {
			t.Fatal(err)
		}
		ctIdx := topoIndexOf(t, g, "ct2")
		if !g.plan.needsZero[g.nodes[ctIdx]] {
			t.Fatal("ConvTranspose node missing needsZero")
		}
		if !g.plan.eligible["a"] || !g.plan.eligible["ct"] {
			t.Fatalf("expected a and ct arena-eligible, plan=%v", g.plan.eligible)
		}
		// a (9 floats of garbage) dies right before the ConvTranspose checks
		// out its same-sized output buffer.
		planFreedAfter(t, g, "a", topoIndexOf(t, g, "relu1"))

		run := func() map[string]*Tensor {
			outs, err := g.Run(map[string]*Tensor{
				"xa": FloatFrom([]float32{1, 1, 1, 1, 1, 1, 1, 1, 1}, 1, 1, 3, 3), // a = all 5s
				"xc": FloatFrom([]float32{1, 2, 3, 4}, 1, 1, 2, 2),
			})
			if err != nil {
				t.Fatal(err)
			}
			return outs
		}
		// convtranspose(xc, ones 2x2, stride 1) = [1,3,2, 4,10,6, 3,7,4];
		// b = relu(a) = all 5s; out = b + ct.
		want := []float32{6, 8, 7, 9, 15, 11, 8, 12, 9}
		wantF32Exact(t, run()["out"], want)
		wantF32Exact(t, run()["out"], want)
	})
}
