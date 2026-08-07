package onnxrt

import (
	"math"

	"github.com/viterin/vek/vek32"
)

// EpilogueKind identifies a fused conv output activation.
type EpilogueKind int

const (
	EpiNone EpilogueKind = iota
	EpiRelu
	EpiSigmoid
	EpiHardSigmoid // y = clamp(alpha*x + beta, 0, 1)
	EpiHSwish      // y = x * clamp(alpha*x + beta, 0, 1)
	EpiGELU        // y = 0.5*x*(1+erf(x/sqrt(2)))
)

// epilogue is a fused activation applied at conv store time.
type epilogue struct {
	kind        EpilogueKind
	alpha, beta float32
}

// optimize runs all load-time graph rewrites. Mutates g.proto's node list,
// g.initializers, and fills g.epilogues.
func (g *Graph) optimize() error {
	g.elimIdentities()
	g.foldConstants()
	g.foldBatchNorms()
	g.foldConvBiasOps()
	// Epilogue detection runs before pre-conv norm folding, whose affine
	// chain walk would otherwise steal the trailing 0.5-mul of GELU blocks.
	g.detectEpilogues()
	g.foldPreConvNorms()
	// Second pass: catches activations exposed by the folds above
	// (e.g. Conv->BN->Relu becomes Conv->Relu only after BN folding).
	g.detectEpilogues()
	g.elimDeadNodes()
	return nil
}

// scalarValue returns the value of a 0-dim or 1-element float initializer.
func (g *Graph) scalarValue(name string) (float32, bool) {
	t, ok := g.initializers[name]
	if !ok {
		return 0, false
	}
	f, err := t.Floats()
	if err != nil || len(f) != 1 {
		return 0, false
	}
	return f[0], true
}

// isChannelVector reports whether t broadcasts as a per-output-channel
// constant over a 4D NCHW conv output, i.e. shape [1, M, 1, 1]. Anything
// else with M elements (e.g. [M] or [1, 1, M]) right-aligns to a spatial
// axis under NumPy broadcasting and must NOT be folded into the bias.
func isChannelVector(t *Tensor, M int) bool {
	return t.DType == DFloat32 && t.Rank() == 4 &&
		t.Shape[0] == 1 && t.Shape[1] == M && t.Shape[2] == 1 && t.Shape[3] == 1
}

// consumersOf builds value -> consuming nodes map.
func consumersOf(nodes []*Node) map[string][]*Node {
	cons := map[string][]*Node{}
	for _, n := range nodes {
		for _, in := range n.Inputs {
			if in != "" {
				cons[in] = append(cons[in], n)
			}
		}
	}
	return cons
}

// producerOf builds value -> producing node map.
func producerOf(nodes []*Node) map[string]*Node {
	prod := map[string]*Node{}
	for _, n := range nodes {
		for _, out := range n.Outputs {
			if out != "" {
				prod[out] = n
			}
		}
	}
	return prod
}

// graphOutputNames returns the set of graph output value names.
func (g *Graph) graphOutputNames() map[string]bool {
	out := map[string]bool{}
	for _, vi := range g.proto.Outputs {
		out[vi.Name] = true
	}
	return out
}

// renameValue substitutes a value name everywhere: node inputs and graph
// outputs. Node outputs keep their original names (the producer side is
// rewritten by the caller where needed).
func renameValue(nodes []*Node, gp *GraphProto, from, to string) {
	for _, n := range nodes {
		for i, in := range n.Inputs {
			if in == from {
				n.Inputs[i] = to
			}
		}
	}
	for i := range gp.Outputs {
		if gp.Outputs[i].Name == from {
			gp.Outputs[i].Name = to
		}
	}
}

// elimIdentities removes Identity nodes, rewiring consumers to the source.
func (g *Graph) elimIdentities() {
	nodes := g.proto.Nodes
	rename := map[string]string{}
	var resolve func(s string) string
	resolve = func(s string) string {
		for {
			r, ok := rename[s]
			if !ok {
				return s
			}
			s = r
		}
	}
	removed := map[*Node]bool{}
	gout := g.graphOutputNames()
	for _, n := range nodes {
		if n.OpType != "Identity" || len(n.Inputs) == 0 || len(n.Outputs) == 0 {
			continue
		}
		// A declared graph output name is part of the Run contract (and
		// onnxruntime returns it verbatim): keep the Identity so the name
		// survives instead of renaming the output to the aliased source.
		if gout[n.Outputs[0]] {
			continue
		}
		src := resolve(n.Inputs[0])
		dst := n.Outputs[0]
		if src != dst {
			rename[dst] = src
		}
		removed[n] = true
	}
	if len(removed) == 0 {
		return
	}
	for _, n := range nodes {
		if removed[n] {
			continue
		}
		for i, in := range n.Inputs {
			if in != "" {
				n.Inputs[i] = resolve(in)
			}
		}
	}
	for i := range g.proto.Outputs {
		g.proto.Outputs[i].Name = resolve(g.proto.Outputs[i].Name)
	}
	g.proto.Nodes = filterRemoved(nodes, removed)
}

func filterRemoved(nodes []*Node, removed map[*Node]bool) []*Node {
	out := nodes[:0]
	for _, n := range nodes {
		if !removed[n] {
			out = append(out, n)
		}
	}
	return out
}

// foldableOps are cheap ops eligible for constant folding.
var foldableOps = map[string]bool{
	"Shape": true, "Slice": true, "Concat": true, "Reshape": true,
	"Squeeze": true, "Unsqueeze": true, "Transpose": true,
	"Add": true, "Sub": true, "Mul": true, "Div": true, "Pow": true,
}

// foldConstants evaluates nodes whose inputs are all initializers (with a
// small result size cap) and replaces them with new initializers.
func (g *Graph) foldConstants() {
	const maxFoldElems = 1 << 16
	for iter := 0; iter < 8; iter++ {
		progress := false
		removed := map[*Node]bool{}
		for _, nd := range g.proto.Nodes {
			if !foldableOps[nd.OpType] || len(nd.Outputs) != 1 {
				continue
			}
			op := opRegistry[nd.OpType]
			if op == nil {
				continue
			}
			args := make([]*Tensor, len(nd.Inputs))
			ok := true
			for i, in := range nd.Inputs {
				if in == "" {
					continue
				}
				t, isInit := g.initializers[in]
				if !isInit {
					ok = false
					break
				}
				args[i] = t
			}
			if !ok {
				continue
			}
			outs, err := op(&runCtx{Graph: g}, nd, args)
			if err != nil || len(outs) != 1 {
				continue
			}
			if outs[0].NumElements() > maxFoldElems {
				continue
			}
			// Clone: view ops (Reshape/Squeeze/Unsqueeze) alias their input's
			// backing array, and later passes (BN/norm folding) mutate conv
			// weight initializers in place — a folded view of those weights
			// would be silently corrupted.
			g.initializers[nd.Outputs[0]] = outs[0].Clone()
			removed[nd] = true
			progress = true
		}
		if progress {
			g.proto.Nodes = filterRemoved(g.proto.Nodes, removed)
		} else {
			break
		}
	}
}

// foldBatchNorms folds inference BatchNormalization into a preceding Conv
// when the conv output feeds only that BN.
func (g *Graph) foldBatchNorms() {
	nodes := g.proto.Nodes
	cons := consumersOf(nodes)
	prod := producerOf(nodes)
	gout := g.graphOutputNames()
	removed := map[*Node]bool{}
	for _, bn := range nodes {
		if bn.OpType != "BatchNormalization" || len(bn.Inputs) < 5 {
			continue
		}
		// Only fold the plain inference form: training mode or extra
		// (statistic) outputs change the semantics we cannot reproduce.
		if attrInt(bn, "training_mode", 0) != 0 {
			continue
		}
		extraOut := false
		for _, o := range bn.Outputs[1:] {
			if o != "" {
				extraOut = true
			}
		}
		if extraOut {
			continue
		}
		conv := prod[bn.Inputs[0]]
		if conv == nil || conv.OpType != "Conv" || len(conv.Inputs) < 2 {
			continue
		}
		if len(cons[conv.Outputs[0]]) != 1 {
			continue
		}
		// A graph output must keep producing the pre-BN value.
		if gout[conv.Outputs[0]] {
			continue
		}
		wT, ok := g.initializers[conv.Inputs[1]]
		if !ok || wT.DType != DFloat32 || wT.Rank() != 4 {
			continue
		}
		// weights must not be shared with other nodes
		if len(cons[conv.Inputs[1]]) != 1 {
			continue
		}
		params := make([][]float32, 4)
		ok = true
		for i := 1; i <= 4; i++ {
			t, isInit := g.initializers[bn.Inputs[i]]
			if !isInit {
				ok = false
				break
			}
			f, err := t.Floats()
			if err != nil {
				ok = false
				break
			}
			params[i-1] = f
		}
		if !ok {
			continue
		}
		scale, bias, mean, varr := params[0], params[1], params[2], params[3]
		eps := float64(attrFloat(bn, "epsilon", 1e-5))
		M := wT.Shape[0]
		if len(scale) != M || len(bias) != M || len(mean) != M || len(varr) != M {
			continue
		}
		f := make([]float32, M)
		for m := 0; m < M; m++ {
			f[m] = scale[m] / float32(math.Sqrt(float64(varr[m])+eps))
		}
		// w'[m] = w[m] * f[m]
		perCh := wT.NumElements() / M
		for m := 0; m < M; m++ {
			row := wT.F32[m*perCh : (m+1)*perCh]
			fm := f[m]
			for i := range row {
				row[i] *= fm
			}
		}
		// bias: b' = (b - mean)*f + beta
		var bb []float32
		if len(conv.Inputs) > 2 && conv.Inputs[2] != "" {
			bT, isInit := g.initializers[conv.Inputs[2]]
			if !isInit || bT.DType != DFloat32 || len(bT.F32) != M {
				continue
			}
			if len(cons[conv.Inputs[2]]) != 1 {
				continue
			}
			bb = bT.F32
		} else {
			bb = make([]float32, M)
			name := conv.Inputs[1] + ".fused_bias"
			g.initializers[name] = &Tensor{Shape: []int{M}, DType: DFloat32, F32: bb}
			for len(conv.Inputs) < 3 {
				conv.Inputs = append(conv.Inputs, "")
			}
			conv.Inputs[2] = name
		}
		for m := 0; m < M; m++ {
			bb[m] = (bb[m]-mean[m])*f[m] + bias[m]
		}
		conv.Outputs[0] = bn.Outputs[0]
		removed[bn] = true
	}
	if len(removed) > 0 {
		g.proto.Nodes = filterRemoved(nodes, removed)
	}
}

// foldPreConvNorms folds affine elementwise chains (x*s + c with scalar
// constants) feeding a Conv into that conv's weights and bias:
// Conv(x*s+c, W, b) = Conv(x, s*W, b + c*rowsum(W)).
//
// Soundness: the bias correction c*rowsum(W) assumes every kernel tap reads
// the offset value c, but zero-padded taps contribute 0, so with c != 0 the
// identity only holds when no output window ever leaves the input (Conv with
// all-zero pads). For ConvTranspose a constant input likewise spreads over a
// position-dependent number of taps (borders, output_padding), so the fold is
// restricted to the pure-scale case (c == 0) there.
func (g *Graph) foldPreConvNorms() {
	nodes := g.proto.Nodes
	cons := consumersOf(nodes)
	prod := producerOf(nodes)
	gout := g.graphOutputNames()
	removed := map[*Node]bool{}
	for _, conv := range nodes {
		if removed[conv] || (conv.OpType != "Conv" && conv.OpType != "ConvTranspose") || len(conv.Inputs) < 2 {
			continue
		}
		wT, ok := g.initializers[conv.Inputs[1]]
		if !ok || wT.DType != DFloat32 || wT.Rank() != 4 || len(cons[conv.Inputs[1]]) != 1 {
			continue
		}
		// walk the elementwise chain backwards from the conv input
		s, c := float32(1), float32(0)
		cur := conv.Inputs[0]
		var chain []*Node
		valid := true
		for {
			nd := prod[cur]
			if nd == nil || len(nd.Outputs) != 1 || len(cons[nd.Outputs[0]]) != 1 {
				break
			}
			// a chain node's output is removed by the fold; it must not be a
			// graph output
			if gout[nd.Outputs[0]] {
				break
			}
			if nd.OpType != "Mul" && nd.OpType != "Div" && nd.OpType != "Add" && nd.OpType != "Sub" {
				break
			}
			if len(nd.Inputs) != 2 {
				break
			}
			// exactly one side must be a scalar initializer
			var k float32
			reversed := false
			if v, isScalar := g.scalarValue(nd.Inputs[1]); isScalar {
				k = v
			} else if v, isScalar := g.scalarValue(nd.Inputs[0]); isScalar {
				k = v
				reversed = true
			} else {
				break
			}
			// chain node's other (non-const) input must itself have a single
			// consumer (this node) to keep the rewrite local
			srcName := nd.Inputs[0]
			if reversed {
				srcName = nd.Inputs[1]
			}
			if len(cons[srcName]) != 1 && prod[srcName] != nil {
				break
			}
			// Compose: the invariant is chainOut = s*cur + c, and cur is
			// replaced by the node's non-constant input src. Multiplicative
			// ops scale s only; additive ops contribute s*k to c.
			switch nd.OpType {
			case "Mul":
				s *= k
			case "Div":
				if reversed {
					valid = false // k / (s*x+c) is not affine
				} else {
					s /= k
				}
			case "Add":
				c += s * k
			case "Sub":
				if reversed {
					c += s * k // k - src: negate s AFTER applying s*k to c
					s = -s
				} else {
					c -= s * k
				}
			}
			if !valid {
				break
			}
			chain = append(chain, nd)
			cur = srcName
		}
		if !valid || len(chain) == 0 {
			continue
		}
		if _, isInit := g.initializers[cur]; isInit {
			continue // chain rooted at a constant; leave it for const folding
		}
		if c != 0 {
			// The bias correction is only exact when every kernel tap reads
			// real input (see the pass comment); otherwise padded/border
			// positions would silently gain the full c*rowsum(W).
			if conv.OpType != "Conv" {
				continue
			}
			padsOK := true
			for _, pv := range attrInts(conv, "pads") {
				if pv != 0 {
					padsOK = false
					break
				}
			}
			switch attrStr(conv, "auto_pad", "NOTSET") {
			case "SAME_UPPER", "SAME_LOWER":
				padsOK = false
			}
			if !padsOK {
				continue
			}
		}
		// apply: W *= s ; bias[m] += c * rowsum(W[m])
		M := wT.Shape[0]
		perCh := wT.NumElements() / M
		var bb []float32
		if len(conv.Inputs) > 2 && conv.Inputs[2] != "" {
			bT, isInit := g.initializers[conv.Inputs[2]]
			if !isInit || bT.DType != DFloat32 || len(bT.F32) != M || len(cons[conv.Inputs[2]]) != 1 {
				continue
			}
			bb = bT.F32
		} else {
			bb = make([]float32, M)
			name := conv.Inputs[1] + ".fused_bias"
			g.initializers[name] = &Tensor{Shape: []int{M}, DType: DFloat32, F32: bb}
			for len(conv.Inputs) < 3 {
				conv.Inputs = append(conv.Inputs, "")
			}
			conv.Inputs[2] = name
		}
		for m := 0; m < M; m++ {
			row := wT.F32[m*perCh : (m+1)*perCh]
			var sum float64
			for i := range row {
				sum += float64(row[i])
				row[i] *= s
			}
			bb[m] += c * float32(sum)
		}
		conv.Inputs[0] = cur
		for _, nd := range chain {
			removed[nd] = true
		}
	}
	if len(removed) > 0 {
		g.proto.Nodes = filterRemoved(g.proto.Nodes, removed)
	}
}

// foldConvBiasOps folds Conv/ConvTranspose followed by Add/Sub with a scalar
// or per-output-channel constant into the conv bias.
func (g *Graph) foldConvBiasOps() {
	nodes := g.proto.Nodes
	cons := consumersOf(nodes)
	gout := g.graphOutputNames()
	removed := map[*Node]bool{}
	for _, cv := range nodes {
		if removed[cv] || (cv.OpType != "Conv" && cv.OpType != "ConvTranspose") || len(cv.Inputs) < 2 {
			continue
		}
		out := cv.Outputs[0]
		cs := cons[out]
		if len(cs) != 1 {
			continue
		}
		// A graph output must keep producing the pre-add value.
		if gout[out] {
			continue
		}
		e := cs[0]
		if e.OpType != "Add" && e.OpType != "Sub" {
			continue
		}
		if len(e.Inputs) != 2 || e.Inputs[0] != out {
			continue // only conv_out ± const (not const - conv_out)
		}
		t, isInit := g.initializers[e.Inputs[1]]
		if !isInit || t.DType != DFloat32 {
			continue
		}
		wT, ok := g.initializers[cv.Inputs[1]]
		if !ok {
			continue
		}
		var M int
		if cv.OpType == "Conv" {
			M = wT.Shape[0]
		} else {
			M = wT.Shape[1] * int(attrInt(cv, "group", 1))
		}
		var perCh []float32
		if v, isScalar := g.scalarValue(e.Inputs[1]); isScalar {
			perCh = make([]float32, M)
			for m := range perCh {
				perCh[m] = v
			}
		} else if isChannelVector(t, M) {
			perCh = t.F32
		} else {
			continue
		}
		var bb []float32
		if len(cv.Inputs) > 2 && cv.Inputs[2] != "" {
			bT, isInit := g.initializers[cv.Inputs[2]]
			if !isInit || bT.DType != DFloat32 || len(bT.F32) != M || len(cons[cv.Inputs[2]]) != 1 {
				continue
			}
			bb = bT.F32
		} else {
			bb = make([]float32, M)
			name := cv.Inputs[1] + ".fused_bias"
			g.initializers[name] = &Tensor{Shape: []int{M}, DType: DFloat32, F32: bb}
			for len(cv.Inputs) < 3 {
				cv.Inputs = append(cv.Inputs, "")
			}
			cv.Inputs[2] = name
		}
		sign := float32(1)
		if e.OpType == "Sub" {
			sign = -1
		}
		for m := 0; m < M; m++ {
			bb[m] += sign * perCh[m]
		}
		cv.Outputs[0] = e.Outputs[0]
		removed[e] = true
	}
	if len(removed) > 0 {
		g.proto.Nodes = filterRemoved(g.proto.Nodes, removed)
	}
}

// detectEpilogues finds Conv(+bias) activation patterns and records fused
// epilogues: Relu, Sigmoid, HardSigmoid, h-swish (x*HardSigmoid(x)), and
// exact GELU (0.5*x*(1+erf(x/√2))).
func (g *Graph) detectEpilogues() {
	nodes := g.proto.Nodes
	cons := consumersOf(nodes)
	gout := g.graphOutputNames()
	removed := map[*Node]bool{}
	if g.epilogues == nil {
		g.epilogues = map[*Node]*epilogue{}
	}

	scalarInit := g.scalarValue
	// singleOut returns the node consuming v if it is the only consumer.
	singleConsumer := func(v string) *Node {
		cs := cons[v]
		if len(cs) == 1 {
			return cs[0]
		}
		return nil
	}

	for _, cv := range nodes {
		if removed[cv] || (cv.OpType != "Conv" && cv.OpType != "ConvTranspose") {
			continue
		}
		out := cv.Outputs[0]
		// The conv's raw output name is replaced by the fused activation's
		// output name; a graph output must keep producing the raw value.
		if gout[out] {
			continue
		}
		cs := cons[out]
		if len(cs) == 1 {
			act := cs[0]
			switch act.OpType {
			case "Relu":
				g.epilogues[cv] = &epilogue{kind: EpiRelu}
			case "Sigmoid":
				g.epilogues[cv] = &epilogue{kind: EpiSigmoid}
			case "HardSigmoid":
				g.epilogues[cv] = &epilogue{kind: EpiHardSigmoid,
					alpha: attrFloat(act, "alpha", 0.2), beta: attrFloat(act, "beta", 0.5)}
			default:
				continue
			}
			cv.Outputs[0] = act.Outputs[0]
			removed[act] = true
			continue
		}
		if len(cs) != 2 {
			continue
		}
		// h-swish: consumers {HardSigmoid, Mul}; Mul(x, HardSigmoid(x))
		var hs, mul *Node
		for _, c := range cs {
			switch c.OpType {
			case "HardSigmoid":
				hs = c
			case "Mul":
				mul = c
			}
		}
		if hs != nil && mul != nil && len(mul.Inputs) == 2 && len(hs.Outputs) == 1 &&
			!gout[hs.Outputs[0]] && // removed by the fold; must not be a graph output
			singleConsumer(hs.Outputs[0]) == mul &&
			((mul.Inputs[0] == out && mul.Inputs[1] == hs.Outputs[0]) ||
				(mul.Inputs[1] == out && mul.Inputs[0] == hs.Outputs[0])) {
			g.epilogues[cv] = &epilogue{kind: EpiHSwish,
				alpha: attrFloat(hs, "alpha", 0.2), beta: attrFloat(hs, "beta", 0.5)}
			cv.Outputs[0] = mul.Outputs[0]
			removed[hs] = true
			removed[mul] = true
			continue
		}
		// exact GELU: consumers {Div, Mul2};
		// Div(x,√2) → Erf → Add(e,1) feeds Mul2(x, add) → Mul4(m2, 0.5)
		var div, mul2 *Node
		for _, c := range cs {
			switch c.OpType {
			case "Div":
				div = c
			case "Mul":
				mul2 = c
			}
		}
		if div == nil || mul2 == nil || len(div.Inputs) != 2 || div.Inputs[0] != out {
			continue
		}
		// The constants must match exactly: the fused epilogue hard-codes
		// 1/√2, 1 and 0.5, so folding an approximate-but-different divisor
		// would silently change numerics. Exporters materialize √2, 1.0 and
		// 0.5 exactly in float32, so exactness costs nothing.
		dk, ok := scalarInit(div.Inputs[1])
		if !ok || dk != float32(math.Sqrt2) {
			continue
		}
		erf := singleConsumer(div.Outputs[0])
		if erf == nil || erf.OpType != "Erf" {
			continue
		}
		add := singleConsumer(erf.Outputs[0])
		if add == nil || add.OpType != "Add" || len(add.Inputs) != 2 {
			continue
		}
		addConstIdx := -1
		if add.Inputs[0] == erf.Outputs[0] {
			addConstIdx = 1
		} else if add.Inputs[1] == erf.Outputs[0] {
			addConstIdx = 0
		}
		if addConstIdx < 0 {
			continue
		}
		ak, ok := scalarInit(add.Inputs[addConstIdx])
		if !ok || ak != 1 {
			continue
		}
		if len(mul2.Inputs) != 2 ||
			!((mul2.Inputs[0] == out && mul2.Inputs[1] == add.Outputs[0]) ||
				(mul2.Inputs[1] == out && mul2.Inputs[0] == add.Outputs[0])) {
			continue
		}
		if singleConsumer(add.Outputs[0]) != mul2 {
			continue
		}
		mul4 := singleConsumer(mul2.Outputs[0])
		if mul4 == nil || mul4.OpType != "Mul" || len(mul4.Inputs) != 2 {
			continue
		}
		m4ConstIdx := -1
		if mul4.Inputs[0] == mul2.Outputs[0] {
			m4ConstIdx = 1
		} else if mul4.Inputs[1] == mul2.Outputs[0] {
			m4ConstIdx = 0
		}
		if m4ConstIdx < 0 {
			continue
		}
		mk, ok := scalarInit(mul4.Inputs[m4ConstIdx])
		if !ok || mk != 0.5 {
			continue
		}
		// The fold removes div/erf/add/mul2; none of their outputs may be a
		// declared graph output (mul4's output name survives as the conv's).
		if gout[div.Outputs[0]] || gout[erf.Outputs[0]] ||
			gout[add.Outputs[0]] || gout[mul2.Outputs[0]] {
			continue
		}
		g.epilogues[cv] = &epilogue{kind: EpiGELU}
		cv.Outputs[0] = mul4.Outputs[0]
		removed[div] = true
		removed[erf] = true
		removed[add] = true
		removed[mul2] = true
		removed[mul4] = true
	}
	if len(removed) > 0 {
		g.proto.Nodes = filterRemoved(g.proto.Nodes, removed)
	}
}

// elimDeadNodes removes nodes whose outputs are all unused, to fixpoint.
func (g *Graph) elimDeadNodes() {
	for {
		used := g.graphOutputNames()
		for _, n := range g.proto.Nodes {
			for _, in := range n.Inputs {
				if in != "" {
					used[in] = true
				}
			}
		}
		removed := map[*Node]bool{}
		for _, n := range g.proto.Nodes {
			anyUsed := false
			for _, out := range n.Outputs {
				if used[out] {
					anyUsed = true
					break
				}
			}
			if !anyUsed {
				removed[n] = true
			}
		}
		if len(removed) == 0 {
			break
		}
		g.proto.Nodes = filterRemoved(g.proto.Nodes, removed)
	}
}

// applyEpilogue applies a fused activation in place (vectorized).
func applyEpilogue(dst []float32, e *epilogue) {
	if e == nil {
		return
	}
	switch e.kind {
	case EpiRelu:
		parallelChunks(len(dst), func(s, en int) { vek32.MaximumNumber_Inplace(dst[s:en], 0) })
	case EpiSigmoid:
		sigmoidInplace(dst)
	case EpiHardSigmoid:
		hardSigmoidInplace(dst, e.alpha, e.beta)
	case EpiHSwish:
		hswishInplace(dst, e.alpha, e.beta)
	case EpiGELU:
		parallelChunks(len(dst), func(s, en int) { geluErfInto(dst[s:en], dst[s:en]) })
	}
}

// sigmoidInplace computes x = 1/(1+exp(-x)) in place.
func sigmoidInplace(x []float32) {
	parallelChunks(len(x), func(s, e int) {
		seg := x[s:e]
		vek32.Neg_Inplace(seg)
		vek32.MaximumNumber_Inplace(seg, -80)
		vek32.MinimumNumber_Inplace(seg, 80)
		vek32.Exp_Inplace(seg)
		vek32.AddNumber_Inplace(seg, 1)
		vek32.Inv_Inplace(seg)
	})
}

// hardSigmoidInplace computes x = clamp(alpha*x + beta, 0, 1) in place.
func hardSigmoidInplace(x []float32, alpha, beta float32) {
	parallelChunks(len(x), func(s, e int) {
		seg := x[s:e]
		vek32.MulNumber_Inplace(seg, alpha)
		vek32.AddNumber_Inplace(seg, beta)
		vek32.MaximumNumber_Inplace(seg, 0)
		vek32.MinimumNumber_Inplace(seg, 1)
	})
}

// hswishInplace computes x = x * clamp(alpha*x + beta, 0, 1) in place.
func hswishInplace(x []float32, alpha, beta float32) {
	parallelChunks(len(x), func(s, e int) {
		seg := x[s:e]
		tmp := getScratch(len(seg))
		copy(tmp, seg)
		vek32.MulNumber_Inplace(tmp, alpha)
		vek32.AddNumber_Inplace(tmp, beta)
		vek32.MaximumNumber_Inplace(tmp, 0)
		vek32.MinimumNumber_Inplace(tmp, 1)
		vek32.Mul_Inplace(seg, tmp)
		putScratch(tmp)
	})
}
