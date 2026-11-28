package kokoro

import (
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/npu/nnapi"
)

func TestAlbertNPUMatchesCPU(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	kokoroFastMath = false

	spec := albertGraphSpec{emb: 8, hidden: 16, heads: 4, intermediate: 20, layers: 2, vocab: 11, maxPos: 6}
	var seed uint32 = 3
	w := albertNPUWeights{
		word:    npuFill(spec.vocab*spec.emb, &seed, 0.2),
		pos:     npuFill(spec.maxPos*spec.emb, &seed, 0.2),
		typeEmb: npuFill(spec.emb, &seed, 0.1),
		lnW:     npuGamma(spec.emb, &seed),
		lnB:     npuFill(spec.emb, &seed, 0.05),
		mapW:    npuFill(spec.hidden*spec.emb, &seed, 0.15),
		mapB:    npuFill(spec.hidden, &seed, 0.05),
		qW:      npuFill(spec.hidden*spec.hidden, &seed, 0.12),
		qB:      npuFill(spec.hidden, &seed, 0.05),
		kW:      npuFill(spec.hidden*spec.hidden, &seed, 0.12),
		kB:      npuFill(spec.hidden, &seed, 0.05),
		vW:      npuFill(spec.hidden*spec.hidden, &seed, 0.12),
		vB:      npuFill(spec.hidden, &seed, 0.05),
		denseW:  npuFill(spec.hidden*spec.hidden, &seed, 0.12),
		denseB:  npuFill(spec.hidden, &seed, 0.05),
		attnLnW: npuGamma(spec.hidden, &seed),
		attnLnB: npuFill(spec.hidden, &seed, 0.05),
		ffW:     npuFill(spec.intermediate*spec.hidden, &seed, 0.12),
		ffB:     npuFill(spec.intermediate, &seed, 0.05),
		ffOutW:  npuFill(spec.hidden*spec.intermediate, &seed, 0.12),
		ffOutB:  npuFill(spec.hidden, &seed, 0.05),
		fullLnW: npuGamma(spec.hidden, &seed),
		fullLnB: npuFill(spec.hidden, &seed, 0.05),
	}
	b, err := buildAlbertGraph(spec, w)
	if err != nil {
		t.Fatal(err)
	}
	ids := []int{1, 4, 2}
	ref, err := albertNPURef(w, spec, ids)
	if err != nil {
		t.Fatal(err)
	}
	tok := []int32{1, 4, 2}
	pos := []int32{0, 1, 2}
	outs, err := nnapi.Eval(b, []nnapi.ExecIn{
		{I32: tok, Shape: []int{len(ids)}},
		{I32: pos, Shape: []int{len(ids)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 1 || len(outs[0].F32) != len(ref) {
		t.Fatalf("len %d want %d", len(outs[0].F32), len(ref))
	}
	maxAbs, meanAbs := albertGap(ref, outs[0].F32)
	t.Logf("maxAbs %.4g meanAbs %.4g ops %d", maxAbs, meanAbs, b.NumOps())
	if maxAbs > 1e-3 || meanAbs > 1e-4 {
		t.Fatalf("maxAbs %.4g meanAbs %.4g", maxAbs, meanAbs)
	}
}

func TestAlbertNPUTokenDigestsConstants(t *testing.T) {
	spec := albertGraphSpec{emb: 4, hidden: 4, heads: 2, intermediate: 4, layers: 1, vocab: 3, maxPos: 3}
	w := albertNPUWeights{qW: []float32{1, 0, 0, 0, 0, 0, 0, 2}}
	base := albertNPUToken(spec, w)
	if albertNPUToken(spec, w) != base {
		t.Fatal("token changed without a weight change")
	}
	w.qW = []float32{1, 0, 0, 9, 0, 0, 0, 2}
	if albertNPUToken(spec, w) == base {
		t.Fatal("middle weight did not change the token")
	}
	w.qW = []float32{1, 0, 0, 0, 0, 0, 0, 2}
	w.qB = []float32{0.5}
	if albertNPUToken(spec, w) == base {
		t.Fatal("bias did not change the token")
	}
	w.qB = nil
	w.typeEmb = []float32{0.25}
	if albertNPUToken(spec, w) == base {
		t.Fatal("type embedding did not change the token")
	}
	w.typeEmb = nil
	if albertNPUToken(spec, w) != base {
		t.Fatal("restored weights changed the token")
	}
}

func albertNPURef(w albertNPUWeights, spec albertGraphSpec, ids []int) ([]float32, error) {
	seq := len(ids)
	emb := spec.emb
	x := make([]float32, seq*emb)
	for t, id := range ids {
		for d := 0; d < emb; d++ {
			x[t*emb+d] = w.word[id*emb+d] + w.pos[t*emb+d] + w.typeEmb[d]
		}
	}
	if err := LayerNormLastDim(x, x, w.lnW, w.lnB, seq, emb, 1e-12); err != nil {
		return nil, err
	}
	h := make([]float32, seq*spec.hidden)
	if err := albertLinearRows(h, x, w.mapW, w.mapB, seq, emb, spec.hidden); err != nil {
		return nil, err
	}
	for layer := 0; layer < spec.layers; layer++ {
		next, err := albertRefLayer(h, w, spec, seq)
		if err != nil {
			return nil, err
		}
		h = next
	}
	return h, nil
}

func albertRefLayer(x []float32, w albertNPUWeights, spec albertGraphSpec, seq int) ([]float32, error) {
	h := spec.hidden
	q := make([]float32, seq*h)
	k := make([]float32, seq*h)
	v := make([]float32, seq*h)
	ctx := make([]float32, seq*h)
	if err := albertLinearRows(q, x, w.qW, w.qB, seq, h, h); err != nil {
		return nil, err
	}
	if err := albertLinearRows(k, x, w.kW, w.kB, seq, h, h); err != nil {
		return nil, err
	}
	if err := albertLinearRows(v, x, w.vW, w.vB, seq, h, h); err != nil {
		return nil, err
	}
	headDim := h / spec.heads
	for head := 0; head < spec.heads; head++ {
		albertHeadDot(q, k, v, ctx, seq, h, head, headDim)
	}
	proj := make([]float32, seq*h)
	if err := albertLinearRows(proj, ctx, w.denseW, w.denseB, seq, h, h); err != nil {
		return nil, err
	}
	addInplace32(proj, x)
	if err := LayerNormLastDim(proj, proj, w.attnLnW, w.attnLnB, seq, h, 1e-12); err != nil {
		return nil, err
	}
	ff := make([]float32, seq*spec.intermediate)
	if err := albertLinearRows(ff, proj, w.ffW, w.ffB, seq, h, spec.intermediate); err != nil {
		return nil, err
	}
	geluInplace(ff)
	ffOut := make([]float32, seq*h)
	if err := albertLinearRows(ffOut, ff, w.ffOutW, w.ffOutB, seq, spec.intermediate, h); err != nil {
		return nil, err
	}
	addInplace32(ffOut, proj)
	if err := LayerNormLastDim(ffOut, ffOut, w.fullLnW, w.fullLnB, seq, h, 1e-12); err != nil {
		return nil, err
	}
	return ffOut, nil
}

func albertLinearRows(out, x, weight, bias []float32, steps, in, outDim int) error {
	for t := 0; t < steps; t++ {
		if err := Linear(out[t*outDim:(t+1)*outDim], x[t*in:(t+1)*in], weight, bias, in, outDim); err != nil {
			return err
		}
	}
	return nil
}

func npuFill(n int, seed *uint32, scale float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		*seed = *seed*1664525 + 1013904223
		out[i] = (float32(*seed>>8)/float32(1<<24) - 0.5) * scale
	}
	return out
}

func npuGamma(n int, seed *uint32) []float32 {
	g := npuFill(n, seed, 0.1)
	for i := range g {
		g[i] += 1
	}
	return g
}

func albertGap(a, b []float32) (maxAbs, meanAbs float64) {
	var sum float64
	for i := range a {
		d := math.Abs(float64(a[i] - b[i]))
		if d > maxAbs {
			maxAbs = d
		}
		sum += d
	}
	if len(a) == 0 {
		return math.Inf(1), math.Inf(1)
	}
	return maxAbs, sum / float64(len(a))
}
