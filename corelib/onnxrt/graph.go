package onnxrt

import (
	"fmt"
	"math"
	"os"
	"sort"
)

// debugNaN enables per-node non-finite output tracing (env ONNXRT_DEBUG_NAN=1).
var debugNaN = os.Getenv("ONNXRT_DEBUG_NAN") == "1"

// noArena disables output-tensor arena allocation (env ONNXRT_NO_ARENA=1):
// every op output comes from the GC heap. Debug/triage escape hatch for the
// arena (round 4); production runs leave it unset.
var noArena = os.Getenv("ONNXRT_NO_ARENA") == "1"

// Graph is a prepared, executable ONNX graph. It wraps a parsed GraphProto
// with converted initializers and a topologically sorted node list.
type Graph struct {
	proto        *GraphProto
	nodes        []*Node // topologically sorted
	initializers map[string]*Tensor
	inputNames   []string // graph inputs that are not initializers
	inputShapes  map[string][]Dim
	outputNames  []string
	opset        int64
	epilogues    map[*Node]*epilogue  // fused conv activations
	matMulBT     map[string][]float32 // pre-transposed constant MatMul B operands, by value name
	plan         *arenaPlan           // liveness/alias analysis for output arena allocation
	ctc          *ctcTail             // detected fusable CTC head (nil when absent)
}

// LoadGraph reads an ONNX model file, parses it, and prepares an executable
// graph.
func LoadGraph(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := ParseModel(data)
	if err != nil {
		return nil, err
	}
	return NewGraph(m)
}

// NewGraph prepares an executable graph from a parsed model.
func NewGraph(m *Model) (*Graph, error) {
	gp := m.Graph
	if gp == nil {
		return nil, fmt.Errorf("onnxrt: model has no graph")
	}
	g := &Graph{
		proto:        gp,
		initializers: map[string]*Tensor{},
		inputShapes:  map[string][]Dim{},
		opset:        m.Opset,
	}
	for name, tp := range gp.Initializers {
		t, err := tensorFromProto(tp)
		if err != nil {
			return nil, fmt.Errorf("onnxrt: initializer %q: %w", name, err)
		}
		g.initializers[name] = t
		// Free the raw wire bytes: the decoded tensor is authoritative from
		// here on, and keeping both doubles the model's memory footprint.
		tp.RawData = nil
	}
	// Load-time rewrites (identity elim, constant folding, BN/norm folding,
	// epilogue fusion, DCE). Mutates gp.Nodes and g.initializers.
	if err := g.optimize(); err != nil {
		return nil, err
	}
	for _, vi := range gp.Inputs {
		if _, isInit := g.initializers[vi.Name]; isInit {
			continue
		}
		g.inputNames = append(g.inputNames, vi.Name)
		g.inputShapes[vi.Name] = vi.Shape
	}
	for _, vi := range gp.Outputs {
		g.outputNames = append(g.outputNames, vi.Name)
	}
	nodes, err := topoSort(gp, g.initializers)
	if err != nil {
		return nil, err
	}
	g.nodes = nodes
	// Pre-transpose constant MatMul B operands: the runtime kernel wants B
	// as [N, K] rows, and re-transposing a large constant weight (e.g. the
	// CTC head) on every forward is pure waste. Frozen at load, so Run
	// stays concurrency-safe.
	for _, nd := range g.nodes {
		if nd.OpType != "MatMul" || len(nd.Inputs) != 2 {
			continue
		}
		bt, ok := g.initializers[nd.Inputs[1]]
		if !ok || bt.DType != DFloat32 || bt.Rank() != 2 {
			continue
		}
		K, N := bt.Shape[0], bt.Shape[1]
		if g.matMulBT == nil {
			g.matMulBT = map[string][]float32{}
		}
		g.matMulBT[nd.Inputs[1]] = transpose2D(bt.F32, K, N)
	}
	// Detect a fusable CTC head (MatMul -> [Add] -> Softmax graph output).
	// Runs after matMulBT so the fused kernel can share the pre-transposed
	// weight; records only, never rewrites.
	g.detectCTCTail()
	// Value liveness + alias analysis for the output arena. Frozen like the
	// rest of the graph state, so Run stays concurrency-safe.
	g.computeArenaPlan()
	return g, nil
}

// InputNames returns the names of the runtime (non-initializer) inputs.
func (g *Graph) InputNames() []string { return g.inputNames }

// OutputNames returns the graph output names.
func (g *Graph) OutputNames() []string { return g.outputNames }

// Opset returns the model opset version.
func (g *Graph) Opset() int64 { return g.opset }

// tensorFromProto converts a parsed TensorProto initializer to a Tensor.
func tensorFromProto(tp *TensorProto) (*Tensor, error) {
	if tp.DataLocation == 1 {
		return nil, fmt.Errorf("external data not supported (location=%v)", tp.ExternalData)
	}
	shape, nElem, err := checkedShape(tp.Dims)
	if err != nil {
		return nil, fmt.Errorf("onnxrt: initializer %q: %w", tp.Name, err)
	}
	switch tp.DataType {
	case TypeFloat, TypeFloat16, TypeDouble:
		var data []float32
		var err error
		switch {
		case len(tp.RawData) > 0:
			data, err = decodeRawFloats(tp.RawData, tp.DataType)
		case tp.DataType == TypeFloat:
			data = tp.FloatData
		case tp.DataType == TypeDouble:
			data = make([]float32, len(tp.DoubleData))
			for i, v := range tp.DoubleData {
				data[i] = float32(v)
			}
		default:
			err = fmt.Errorf("float16 tensor without raw_data not supported")
		}
		if err != nil {
			return nil, err
		}
		t := &Tensor{Shape: shape, DType: DFloat32, F32: data}
		if nElem > 0 && nElem != len(data) {
			return nil, fmt.Errorf("shape %v holds %d elements but data has %d", shape, nElem, len(data))
		}
		return t, nil
	case TypeInt64, TypeInt32:
		var data []int64
		var err error
		switch {
		case len(tp.RawData) > 0:
			data, err = decodeRawInts(tp.RawData, tp.DataType)
		case tp.DataType == TypeInt64:
			data = tp.Int64Data
		default: // INT32
			data = make([]int64, len(tp.Int32Data))
			for i, v := range tp.Int32Data {
				data[i] = int64(v)
			}
		}
		if err != nil {
			return nil, err
		}
		if nElem > 0 && nElem != len(data) {
			return nil, fmt.Errorf("shape %v holds %d elements but data has %d", shape, nElem, len(data))
		}
		return &Tensor{Shape: shape, DType: DInt64, I64: data}, nil
	}
	return nil, fmt.Errorf("unsupported initializer dtype %s", tp.DataType)
}

// topoSort orders nodes so every producer precedes its consumers, preserving
// the original order wherever possible (Kahn's algorithm, smallest index
// first). inits provides the value names available without any producer
// (converted initializers, including constant-folded ones).
func topoSort(gp *GraphProto, inits map[string]*Tensor) ([]*Node, error) {
	n := len(gp.Nodes)
	// Map value name -> producing node index.
	producer := map[string]int{}
	for i, nd := range gp.Nodes {
		for _, out := range nd.Outputs {
			if out != "" {
				producer[out] = i
			}
		}
	}
	// Values available without any node: initializers and graph inputs.
	available := map[string]bool{}
	for name := range inits {
		available[name] = true
	}
	for _, vi := range gp.Inputs {
		available[vi.Name] = true
	}

	indeg := make([]int, n)
	deps := make([][]int, n) // consumer -> producers it waits on
	for i, nd := range gp.Nodes {
		seen := map[int]bool{}
		for _, in := range nd.Inputs {
			if in == "" || available[in] {
				continue
			}
			p, ok := producer[in]
			if !ok {
				return nil, fmt.Errorf("onnxrt: node %q (%s) input %q has no producer", nd.Name, nd.OpType, in)
			}
			if !seen[p] {
				seen[p] = true
				indeg[i]++
				deps[p] = append(deps[p], i)
			}
		}
	}
	// Kahn with smallest-original-index selection.
	ready := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if indeg[i] == 0 {
			ready = append(ready, i)
		}
	}
	order := make([]*Node, 0, n)
	for len(order) < n {
		if len(ready) == 0 {
			return nil, fmt.Errorf("onnxrt: graph has a cycle")
		}
		i := ready[0]
		ready = ready[1:]
		order = append(order, gp.Nodes[i])
		for _, c := range deps[i] {
			indeg[c]--
			if indeg[c] == 0 {
				// insert keeping ready sorted
				pos := sort.SearchInts(ready, c)
				ready = append(ready, 0)
				copy(ready[pos+1:], ready[pos:])
				ready[pos] = c
			}
		}
	}
	return order, nil
}

// Run executes the graph. inputs must provide every runtime input; shapes are
// validated against the declared signature where static dims are known.
// Returns all graph outputs keyed by name.
//
// Run is safe for concurrent use on the same Graph: all graph state is frozen
// after NewGraph and every run works on its own value map, scratch buffers
// and output arena. The caller must not mutate input tensors while a run is
// in flight, and must treat returned tensors as read-only — outputs may alias
// input or initializer storage (Identity/Reshape/Squeeze views and constant
// graph outputs), so mutating them can corrupt shared state. Returned outputs
// never alias arena memory: graph outputs (and every value aliasing them) are
// excluded from arena allocation.
func (g *Graph) Run(inputs map[string]*Tensor) (map[string]*Tensor, error) {
	outs, _, err := g.exec(inputs, nil)
	return outs, err
}

// exec is the shared Run/RunCTC driver. With a non-nil tail the CTC head
// nodes are skipped and the fused decode runs when the tail MatMul is
// reached (before its arena frees); the full output map is still assembled
// for whatever values remain, but callers using a tail only read dec.
func (g *Graph) exec(inputs map[string]*Tensor, tail *ctcTail) (outs map[string]*Tensor, dec *ctcDecode, err error) {
	rc := &runCtx{Graph: g}
	if g.plan != nil && len(g.plan.eligible) > 0 && !noArena {
		rc.arena = acquireArena()
		defer releaseArena(rc.arena)
	}
	values := make(map[string]*Tensor, len(g.initializers)+len(inputs)+len(g.nodes))
	for name, t := range g.initializers {
		values[name] = t
	}
	for _, name := range g.inputNames {
		t, ok := inputs[name]
		if !ok {
			return nil, nil, fmt.Errorf("onnxrt: missing input %q", name)
		}
		if err := g.validateInput(name, t); err != nil {
			return nil, nil, err
		}
		values[name] = t
	}
	for nodeIdx, nd := range g.nodes {
		if tail != nil && tail.skip[nd] {
			if nd == tail.matmul {
				d, derr := tail.decode(values[nd.Inputs[0]])
				if derr != nil {
					return nil, nil, fmt.Errorf("onnxrt: node %q (MatMul): fused CTC head: %w", nd.Name, derr)
				}
				dec = d
			}
			rc.freeDead(nodeIdx, values)
			continue
		}
		args := make([]*Tensor, len(nd.Inputs))
		for i, in := range nd.Inputs {
			if in == "" {
				continue // omitted optional input
			}
			t, ok := values[in]
			if !ok {
				return nil, nil, fmt.Errorf("onnxrt: node %q (%s): input %q not available", nd.Name, nd.OpType, in)
			}
			args[i] = t
		}
		op, ok := opRegistry[nd.OpType]
		if !ok {
			return nil, nil, fmt.Errorf("onnxrt: node %q: unsupported op %q", nd.Name, nd.OpType)
		}
		outs, err := op(rc, nd, args)
		if err != nil {
			return nil, nil, fmt.Errorf("onnxrt: node %q (%s): %w", nd.Name, nd.OpType, err)
		}
		if debugNaN {
			for i, t := range outs {
				if t == nil || t.DType != DFloat32 {
					continue
				}
				for j, v := range t.F32 {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						fmt.Fprintf(os.Stderr, "onnxrt debug: node %q (%s) output %d has non-finite at index %d (shape %v)\n",
							nd.Name, nd.OpType, i, j, t.Shape)
						for k, a := range args {
							if a != nil && a.DType == DFloat32 && len(a.F32) > 0 {
								mn, mx := a.F32[0], a.F32[0]
								for _, u := range a.F32 {
									if u < mn {
										mn = u
									}
									if u > mx {
										mx = u
									}
								}
								fmt.Fprintf(os.Stderr, "  input %d %q range [%g, %g]\n", k, nd.Inputs[k], mn, mx)
							}
						}
						return nil, nil, fmt.Errorf("onnxrt: non-finite at node %q", nd.Name)
					}
				}
			}
		}
		for i, outName := range nd.Outputs {
			if outName == "" || i >= len(outs) {
				continue
			}
			values[outName] = outs[i]
		}
		// Recycle arena buffers whose holding value died with this node.
		rc.freeDead(nodeIdx, values)
	}
	result := make(map[string]*Tensor, len(g.outputNames))
	for _, name := range g.outputNames {
		t, ok := values[name]
		if !ok {
			if tail != nil {
				continue // head outputs are intentionally not produced
			}
			return nil, nil, fmt.Errorf("onnxrt: output %q not produced", name)
		}
		result[name] = t
	}
	return result, dec, nil
}

// validateInput checks dtype and shape against the declared input signature.
// Dynamic (symbolic or unknown) dims accept any size.
func (g *Graph) validateInput(name string, t *Tensor) error {
	decl, ok := g.inputShapes[name]
	if !ok || decl == nil {
		return nil // no declared shape info
	}
	if t.DType != DFloat32 {
		return fmt.Errorf("onnxrt: input %q: only float32 inputs supported, got %v", name, t.DType)
	}
	if len(t.Shape) != len(decl) {
		return fmt.Errorf("onnxrt: input %q: rank %d does not match declared rank %d", name, len(t.Shape), len(decl))
	}
	for i, d := range decl {
		if d.Param == "" && d.Value >= 0 && int(d.Value) != t.Shape[i] {
			return fmt.Errorf("onnxrt: input %q: dim %d is %d, declared %d", name, i, t.Shape[i], d.Value)
		}
	}
	return nil
}
