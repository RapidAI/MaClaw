package asr

import (
	"math"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
	"github.com/RapidAI/CodeClaw/corelib/npu/nnapi"
)

func TestSenseVoiceNPUMatchesCPU(t *testing.T) {
	const (
		hidden = 16
		heads  = 4
		ff     = 24
		kernel = 3
		feats  = 12
		vocab  = 8
	)
	m := &SenseVoiceModel{hp: SenseVoiceHParams{
		HiddenSize:  hidden,
		NumHeads:    heads,
		LinearUnits: ff,
		FSMNKernel:  kernel,
		FeatsDim:    feats,
		VocabSize:   vocab,
	}}
	var seed uint32 = 7
	entry := tinySANM(&seed, feats, hidden, ff, kernel, true)
	main := tinySANM(&seed, hidden, hidden, ff, kernel, true)
	m.w.encoder0 = entry
	m.w.encoders = []svLayerWeights{main}
	m.w.afterNormW, m.w.afterNormB = fillGamma(hidden, &seed), fillUnit(hidden, &seed, 0.05)
	m.w.tpNormW, m.w.tpNormB = fillGamma(hidden, &seed), fillUnit(hidden, &seed, 0.05)
	m.w.ctcW = linearWeight{f32: fillUnit(vocab*hidden, &seed, 0.15), rows: vocab}
	m.w.ctcB = fillUnit(vocab, &seed, 0.05)

	b, err := m.buildSenseVoiceNPU()
	if err != nil {
		t.Fatal(err)
	}
	lfrFrames := 3
	lfr := fillUnit(lfrFrames*feats, &seed, 0.4)
	prepared, total := m.prepFeats(lfr, lfrFrames)
	snap := append([]float32(nil), prepared...)
	ref, totalCPU := m.encodeHidden(append([]float32(nil), lfr...), lfrFrames)
	if totalCPU != total {
		t.Fatalf("frames %d vs %d", totalCPU, total)
	}
	refSnap := append([]float32(nil), ref[:total*hidden]...)
	outs, err := nnapi.Eval(b, []nnapi.ExecIn{{F32: snap, Shape: []int{total, feats}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 || len(outs[0].F32) != total*hidden || len(outs[1].I32) != total {
		t.Fatalf("outs hidden %d ids %d", len(outs[0].F32), len(outs[1].I32))
	}
	maxAbs, meanAbs := f32Gap(refSnap, outs[0].F32)
	ids := make([]int, total)
	matMulLinearArgmax(ids, refSnap, m.w.ctcW, m.w.ctcB, total, vocab, hidden)
	agree := 0
	for i, id := range ids {
		if id == int(outs[1].I32[i]) {
			agree++
		}
	}
	t.Logf("maxAbs %.4g meanAbs %.4g argmax %d/%d", maxAbs, meanAbs, agree, total)
	if maxAbs > 5e-2 || meanAbs > 5e-3 {
		t.Fatalf("hidden maxAbs %.4g meanAbs %.4g", maxAbs, meanAbs)
	}
}

func TestSenseVoiceNPUQ8WeightMatchesDequant(t *testing.T) {
	const cols, rows = 32, 2
	data := make([]byte, rows*34)
	for r := 0; r < rows; r++ {
		off := r * 34
		data[off] = 0x00
		data[off+1] = 0x3C
		for i := 0; i < cols; i++ {
			data[off+2+i] = byte(int8(r + 1))
		}
	}
	var m SenseVoiceModel
	b := &nnapi.Builder{}
	x := b.InputF32([]int{1, cols})
	y, err := m.npuFC(b, x, linearWeight{q8: &tensor.Q8Tensor{Data: data, Rows: rows, Cols: cols}}, []float32{0.5, -0.25}, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	b.Output(y)
	if b.ConstBytes() != (rows*cols+rows)*4 {
		t.Fatalf("const bytes %d", b.ConstBytes())
	}
	in := make([]float32, cols)
	for i := range in {
		in[i] = 1
	}
	got, err := nnapi.Eval(b, []nnapi.ExecIn{{F32: in, Shape: []int{1, cols}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{float32(cols) + 0.5, float32(cols)*2 - 0.25}
	if len(got) != 1 || len(got[0].F32) != 2 || got[0].F32[0] != want[0] || got[0].F32[1] != want[1] {
		t.Fatalf("got %v want %v", got[0].F32, want)
	}

	bad := &nnapi.Builder{}
	bx := bad.InputF32([]int{1, 16})
	if _, err := m.npuFC(bad, bx, linearWeight{q8: &tensor.Q8Tensor{Rows: 1, Cols: 16}}, nil, 1, false); err == nil {
		t.Fatal("expected q8 shape error")
	}
}

func TestSenseVoiceNPUQ8ParallelFill(t *testing.T) {
	const cols, rows = 512, 512
	if rows*cols < svNPUDequantParallelMin {
		t.Fatal("matrix is below the parallel threshold")
	}
	blocks := cols / 32
	data := make([]byte, rows*blocks*34)
	for r := 0; r < rows; r++ {
		v := byte(int8(1 + r%7))
		for b := 0; b < blocks; b++ {
			off := (r*blocks + b) * 34
			data[off] = 0x00
			data[off+1] = 0x3C
			for i := 0; i < 32; i++ {
				data[off+2+i] = v
			}
		}
	}
	var m SenseVoiceModel
	b := &nnapi.Builder{}
	x := b.InputF32([]int{1, cols})
	y, err := m.npuFC(b, x, linearWeight{q8: &tensor.Q8Tensor{Data: data, Rows: rows, Cols: cols}}, nil, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	b.Output(y)
	in := make([]float32, cols)
	for i := range in {
		in[i] = 1
	}
	got, err := nnapi.Eval(b, []nnapi.ExecIn{{F32: in, Shape: []int{1, cols}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].F32) != rows {
		t.Fatalf("out %d", len(got[0].F32))
	}
	for r := 0; r < rows; r++ {
		want := float32(cols * (1 + r%7))
		if got[0].F32[r] != want {
			t.Fatalf("row %d got %v want %v", r, got[0].F32[r], want)
		}
	}
}

func tinySANM(seed *uint32, inDim, hidden, ff, kernel int, fused bool) svLayerWeights {
	l := svLayerWeights{
		fusedQKV: fused,
		norm1W:   fillGamma(inDim, seed),
		norm1B:   fillUnit(inDim, seed, 0.05),
		norm2W:   fillGamma(hidden, seed),
		norm2B:   fillUnit(hidden, seed, 0.05),
		outW:     linearWeight{f32: fillUnit(hidden*hidden, seed, 0.15), rows: hidden},
		outB:     linearWeight{f32: fillUnit(hidden, seed, 0.05)},
		ff1W:     linearWeight{f32: fillUnit(ff*hidden, seed, 0.15), rows: ff},
		ff1B:     linearWeight{f32: fillUnit(ff, seed, 0.05)},
		ff2W:     linearWeight{f32: fillUnit(hidden*ff, seed, 0.15), rows: hidden},
		ff2B:     linearWeight{f32: fillUnit(hidden, seed, 0.05)},
		fsmnW:    fillUnit(kernel*hidden, seed, 0.1),
	}
	if fused {
		l.qW = linearWeight{f32: fillUnit(3*hidden*inDim, seed, 0.12), rows: 3 * hidden}
		l.qB = linearWeight{f32: fillUnit(3*hidden, seed, 0.05)}
		return l
	}
	l.qW = linearWeight{f32: fillUnit(hidden*inDim, seed, 0.12), rows: hidden}
	l.qB = linearWeight{f32: fillUnit(hidden, seed, 0.05)}
	l.kW = linearWeight{f32: fillUnit(hidden*inDim, seed, 0.12), rows: hidden}
	l.kB = linearWeight{f32: fillUnit(hidden, seed, 0.05)}
	l.vW = linearWeight{f32: fillUnit(hidden*inDim, seed, 0.12), rows: hidden}
	l.vB = linearWeight{f32: fillUnit(hidden, seed, 0.05)}
	return l
}

func TestSenseVoiceNPUSelfCheckDetachesQuantPanels(t *testing.T) {
	q8p := &tensor.Q8Packed{}
	q4 := &tensor.Q4Tensor{}
	encPacked := &tensor.Q8Packed{}
	ctcPacked := &tensor.Q8Packed{}
	m := &SenseVoiceModel{w: svWeights{
		encoder0:   svLayerWeights{qW: linearWeight{q8p: q8p}, ff2W: linearWeight{q4: q4}},
		encoders:   []svLayerWeights{{outW: linearWeight{q8p: encPacked}}},
		tpEncoders: []svLayerWeights{{ff1W: linearWeight{q8p: &tensor.Q8Packed{}}}},
		ctcW:       linearWeight{q8p: ctcPacked},
	}}
	tpPacked := m.w.tpEncoders[0].ff1W.q8p
	restore := m.detachQuantPanels()
	if m.w.encoder0.qW.q8p != nil || m.w.encoder0.ff2W.q4 != nil ||
		m.w.encoders[0].outW.q8p != nil || m.w.tpEncoders[0].ff1W.q8p != nil ||
		m.w.ctcW.q8p != nil {
		t.Fatal("quant panels still visible to the float reference")
	}
	restore()
	if m.w.encoder0.qW.q8p != q8p || m.w.encoder0.ff2W.q4 != q4 ||
		m.w.encoders[0].outW.q8p != encPacked || m.w.tpEncoders[0].ff1W.q8p != tpPacked ||
		m.w.ctcW.q8p != ctcPacked {
		t.Fatal("quant panels were not restored")
	}
}

func TestSenseVoiceNPUTokenDigestsConstants(t *testing.T) {
	q := make([]byte, 64)
	q[0], q[30], q[63] = 1, 3, 2
	fsmn := []float32{1, 0, 0, 0, 9}
	m := &SenseVoiceModel{
		hp: SenseVoiceHParams{HiddenSize: 16, NumHeads: 4, LinearUnits: 24, FSMNKernel: 3, FeatsDim: 12, VocabSize: 8},
		w: svWeights{encoder0: svLayerWeights{
			qW:    linearWeight{q8: &tensor.Q8Tensor{Data: append([]byte(nil), q...), Rows: 2, Cols: 32}},
			fsmnW: append([]float32(nil), fsmn...),
		}},
	}
	same := m.svNPUCacheToken()
	if m.svNPUCacheToken() != same {
		t.Fatal("token changed without a weight change")
	}

	mid := append([]byte(nil), q...)
	mid[30] = 4
	m.w.encoder0.qW.q8.Data = mid
	if m.svNPUCacheToken() == same {
		t.Fatal("q8 middle byte did not change the token")
	}

	m.w.encoder0.qW.q8.Data = append([]byte(nil), q...)
	m.w.encoder0.qW.q8.Rows = 4
	if m.svNPUCacheToken() == same {
		t.Fatal("q8 shape did not change the token")
	}

	m.w.encoder0.qW.q8.Rows = 2
	m.w.encoder0.fsmnW[2] = 7
	if m.svNPUCacheToken() == same {
		t.Fatal("fsmn middle float did not change the token")
	}

	m.w.encoder0.fsmnW = append([]float32(nil), fsmn...)
	m.w.encoder0.qB = linearWeight{f32: []float32{0.25}, rows: 1}
	if m.svNPUCacheToken() == same {
		t.Fatal("bias did not change the token")
	}

	m.w.encoder0.qB = linearWeight{}
	m.w.encoder0.norm1B = []float32{0.5}
	if m.svNPUCacheToken() == same {
		t.Fatal("norm bias did not change the token")
	}

	m.w.encoder0.norm1B = nil
	ends := make([]byte, 64)
	ends[0], ends[63] = 1, 2
	m.w.encoder0.qW.q8.Data = ends
	if m.svNPUCacheToken() == same {
		t.Fatal("identical ends with a different middle reused the token")
	}
}

func fillUnit(n int, seed *uint32, scale float32) []float32 {
	out := make([]float32, n)
	for i := range out {
		*seed = *seed*1664525 + 1013904223
		out[i] = (float32(*seed>>8)/float32(1<<24) - 0.5) * scale
	}
	return out
}

func fillGamma(n int, seed *uint32) []float32 {
	g := fillUnit(n, seed, 0.1)
	for i := range g {
		g[i] += 1
	}
	return g
}

func f32Gap(a, b []float32) (maxAbs, meanAbs float64) {
	if len(a) != len(b) || len(a) == 0 {
		return math.Inf(1), math.Inf(1)
	}
	var sum float64
	for i := range a {
		d := math.Abs(float64(a[i] - b[i]))
		if d > maxAbs {
			maxAbs = d
		}
		sum += d
	}
	return maxAbs, sum / float64(len(a))
}
