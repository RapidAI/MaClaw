package onnxrt

import (
	"errors"
	"fmt"
	"math"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/viterin/vek/vek32"
)

// ctcTail records a detected fusable CTC head: a graph output produced by
// Softmax(last axis) over MatMul(activation, W) plus an optional bias Add.
// The fused run decodes per-frame argmax ids and their softmax probabilities
// directly, never materializing the [T, V] probability tensor.
type ctcTail struct {
	matmul *Node
	skip   map[*Node]bool // matmul, optional bias Add, Identities, Softmax
	bT     []float32      // pre-transposed weight [N, K] (shared with matMulBT)
	bias   []float32      // [N], nil when the head has no bias Add
	axis   int64          // softmax axis attribute (pre-normalization)
	K, N   int
}

// ctcDecode is the fused-head result: per-frame argmax class ids and the
// softmax probability of each argmax. tlen = len(ids), vocab = N.
type ctcDecode struct {
	ids   []int
	probs []float32
	vocab int
}

// HasCTCHead reports whether the graph tail matches the fusable CTC pattern
// (MatMul -> [Add bias] -> Softmax over the last axis producing a graph
// output), enabling RunCTC.
func (g *Graph) HasCTCHead() bool { return g.ctc != nil }

// RunCTC executes the graph like Run but decodes the detected CTC head in one
// fused pass: it returns the per-frame argmax class ids and their softmax
// probabilities (1/sum(exp(x-xmax)) over the vocab), identical in semantics
// to running the graph and taking per-row argmax / max-probability of the
// Softmax output. Returns an error when the graph has no fusable head or the
// runtime activation shape does not match the pattern.
//
// The same concurrency rules as Run apply.
func (g *Graph) RunCTC(inputs map[string]*Tensor) (ids []int, probs []float32, vocab int, err error) {
	if g.ctc == nil {
		return nil, nil, 0, errors.New("onnxrt: graph has no fusable CTC head")
	}
	_, dec, err := g.exec(inputs, g.ctc)
	if err != nil {
		return nil, nil, 0, err
	}
	if dec == nil {
		return nil, nil, 0, errors.New("onnxrt: CTC tail MatMul was not executed")
	}
	return dec.ids, dec.probs, dec.vocab, nil
}

// detectCTCTail matches the CTC head pattern on the post-optimize node list
// and records it in g.ctc. Nothing is rewritten: Run keeps producing the full
// Softmax output; only RunCTC consults the plan. Detection needs the
// pre-transposed MatMul weights, so it runs after matMulBT is built.
func (g *Graph) detectCTCTail() {
	cons := consumersOf(g.nodes)
	prod := producerOf(g.nodes)

	defAxis := int64(1)
	if g.Opset() >= 13 {
		defAxis = -1
	}

	// Declaration order keeps detection deterministic for multi-output graphs.
	for _, outName := range g.outputNames {
		sm := prod[outName]
		if sm == nil || sm.OpType != "Softmax" || len(sm.Inputs) != 1 || len(cons[outName]) != 0 {
			continue
		}
		skip := map[*Node]bool{sm: true}
		// Walk back through Identity nodes to the value feeding the softmax.
		cur := sm.Inputs[0]
		for {
			nd := prod[cur]
			if nd == nil || nd.OpType != "Identity" || len(nd.Inputs) != 1 || len(cons[cur]) != 1 {
				break
			}
			skip[nd] = true
			cur = nd.Inputs[0]
		}
		// Optional bias: Add(matmulOut, b) with a 1D float initializer b, in
		// either operand order.
		var bias []float32
		mm := prod[cur]
		if mm != nil && mm.OpType == "Add" && len(mm.Inputs) == 2 && len(cons[cur]) == 1 {
			var other string
			for i := 0; i < 2; i++ {
				if bt, isInit := g.initializers[mm.Inputs[i]]; isInit && bt.DType == DFloat32 && bt.Rank() == 1 {
					bias = bt.F32
				} else {
					other = mm.Inputs[i]
				}
			}
			if bias == nil {
				continue // Add without a 1D constant side: not our pattern
			}
			skip[mm] = true
			cur = other
			mm = prod[cur]
		}
		if mm == nil || mm.OpType != "MatMul" || len(mm.Inputs) != 2 || len(cons[cur]) != 1 {
			continue
		}
		// B operand must be a 2D float initializer with a pre-transposed copy;
		// A must be a runtime activation.
		bName := mm.Inputs[1]
		bt, isInit := g.initializers[bName]
		if !isInit || bt.DType != DFloat32 || bt.Rank() != 2 {
			continue
		}
		if _, isInit := g.initializers[mm.Inputs[0]]; isInit {
			continue
		}
		bT := g.matMulBT[bName]
		if bT == nil {
			continue
		}
		K, N := bt.Shape[0], bt.Shape[1]
		if bias != nil && len(bias) != N {
			continue
		}
		skip[mm] = true
		g.ctc = &ctcTail{
			matmul: mm,
			skip:   skip,
			bT:     bT,
			bias:   bias,
			axis:   attrInt(sm, "axis", defAxis),
			K:      K,
			N:      N,
		}
		return
	}
}

// decode runs the fused kernel on the MatMul's A activation. It verifies the
// runtime shape against the recorded pattern (batch-free leading dims, last
// dim K, softmax over the last axis).
func (t *ctcTail) decode(a *Tensor) (*ctcDecode, error) {
	if a == nil || a.DType != DFloat32 {
		return nil, fmt.Errorf("activation missing or not float32")
	}
	r := a.Rank()
	if r < 2 {
		return nil, fmt.Errorf("activation rank %d < 2", r)
	}
	if a.Shape[r-1] != t.K {
		return nil, fmt.Errorf("activation last dim %d, want %d", a.Shape[r-1], t.K)
	}

	ax, err := normalizeAxis(t.axis, r)
	if err != nil || ax != r-1 {
		return nil, fmt.Errorf("softmax axis %d is not the last axis of rank %d", t.axis, r)
	}
	T := a.NumElements() / t.K
	ids, probs := ctcHeadKernel(a.F32, t.bT, t.bias, T, t.N, t.K)
	return &ctcDecode{ids: ids, probs: probs, vocab: t.N}, nil
}

const (
	// ctcMaxBuf caps the block buffer at 2M floats (8 MiB).
	ctcMaxBuf = 2 << 20
)

// ctcChunkCols is the GEMM column-block size for the fused head: the
// [T, chunk] block stays cache-resident between the GEMM store and the
// argmax/expsum epilogue. 512 columns keeps the PP-OCRv6 [48,18710,120]
// working tile below 100KB and is measurably faster than the former 2048
// column tile on the single-core inference path. A var lets benchmarks probe
// alternative sizes.
var ctcChunkCols = 512

// ctcHeadKernel computes, per row of A [T, K], the argmax over N of
// A@B^T + bias (B pre-transposed [N, K]) and the softmax probability of that
// argmax, without materializing the [T, N] logits. Numerics mirror
// softmaxRowVec: the max-subtracted exponent is clamped at -80 and the
// denominator is accumulated in float64, so probs[t] == float32(1/sum).
// Ties resolve to the first index, matching the strict-greater scan in the
// consumer's greedy decoder.
func ctcHeadKernel(a, bT, bias []float32, T, N, K int) (ids []int, probs []float32) {
	ids = make([]int, T)
	probs = make([]float32, T)
	if T <= 0 || N <= 0 || K <= 0 {
		return ids, probs
	}
	maxs := make([]float32, T)
	sums := make([]float64, T)
	chunk := ctcChunkCols
	if c := ctcMaxBuf / T; chunk > c {
		chunk = c
	}
	if chunk < 1 {
		chunk = 1
	}
	if chunk > N {
		chunk = N
	}
	buf := getScratch(T * chunk)
	defer putScratch(buf)
	for n0 := 0; n0 < N; n0 += chunk {
		cn := chunk
		if n0+cn > N {
			cn = N - n0
		}
		var bs []float32
		if bias != nil {
			bs = bias[n0 : n0+cn]
		}
		xt.MatMulBias(buf[:T*cn], a, bT[n0*K:(n0+cn)*K], bs, T, cn, K)
		epi := func(t0, t1 int) {
			for t := t0; t < t1; t++ {
				row := buf[t*cn : t*cn+cn]
				cm := vek32.Max(row)
				m := maxs[t]
				if n0 == 0 || cm > m {
					if n0 > 0 && sums[t] != 0 {
						sums[t] *= math.Exp(float64(m - cm))
					}
					maxs[t] = cm
					m = cm
					// First occurrence wins, as in the strict-greater scan.
					for j, v := range row {
						if v == cm {
							ids[t] = n0 + j
							break
						}
					}
				}
				// exp(row - m) clamped at -80, in place: the logits are no
				// longer needed once the block max/argmax is recorded.
				vek32.SubNumber_Inplace(row, m)
				vek32.MaximumNumber_Inplace(row, -80)
				vek32.Exp_Inplace(row)
				var s float64
				for _, v := range row {
					s += float64(v)
				}
				sums[t] += s
			}
		}
		// The epilogue parallelizes over rows at a lower threshold than the
		// elementwise ops: its per-row work (max, clamp, exp, float64 sum)
		// costs several passes over cn elements.
		if T >= 4 && T*cn >= 1<<15 {
			xt.ParallelRanges(T, epi)
		} else {
			epi(0, T)
		}
	}
	for t := 0; t < T; t++ {
		if s := sums[t]; s != 0 {
			probs[t] = float32(1.0 / s)
		}
	}
	return ids, probs
}
