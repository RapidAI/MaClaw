package nnapi

import (
	"errors"
	"math"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

func TestOperandTypeLayout(t *testing.T) {
	var o operandType
	if unsafe.Sizeof(o) != 24 {
		t.Fatalf("size %d", unsafe.Sizeof(o))
	}
	if unsafe.Offsetof(o.typ) != 0 || unsafe.Offsetof(o.dimensionCount) != 4 ||
		unsafe.Offsetof(o.dimensions) != 8 || unsafe.Offsetof(o.scale) != 16 ||
		unsafe.Offsetof(o.zeroPoint) != 20 {
		t.Fatalf("offsets typ=%d count=%d dims=%d scale=%d zp=%d",
			unsafe.Offsetof(o.typ), unsafe.Offsetof(o.dimensionCount),
			unsafe.Offsetof(o.dimensions), unsafe.Offsetof(o.scale), unsafe.Offsetof(o.zeroPoint))
	}
}

func TestTransientKeepsDeadlineAndResourceMiss(t *testing.T) {
	if !Transient(&execError{code: nnMissedDeadlineTransient}) || !Transient(&execError{code: nnResourceExhaustedTransient}) {
		t.Fatal("transient codes")
	}
	if Transient(&execError{code: 5}) || Transient(errors.New("nnapi: compute 10")) || Transient(nil) {
		t.Fatal("permanent or untyped")
	}
}

func TestCompileRejectsNonAndroid(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("accelerator compile is implemented on android")
	}
	if Available() {
		t.Fatal("Available")
	}
	if !strings.Contains(UnavailableReason(), "android") {
		t.Fatal(UnavailableReason())
	}
	b := &Builder{}
	x := b.InputF32([]int{0, 2})
	w := b.ConstF32([]int{2, 2}, []float32{1, 0, 0, 1})
	y := b.FullyConnected(x, w, -1, false)
	b.Output(y)
	if _, err := Compile(b, "", [32]byte{}); err == nil || !strings.Contains(err.Error(), "android") {
		t.Fatalf("Compile err %v", err)
	}
}

func TestDisabledEnv(t *testing.T) {
	if Disabled("") || Disabled("MACLAW_SENSEVOICE_NPU") {
		t.Fatal("default should try the accelerator")
	}
	t.Setenv("MACLAW_VOICE_NPU", "off")
	if !Disabled("MACLAW_SENSEVOICE_NPU") || !Disabled("") {
		t.Fatal("MACLAW_VOICE_NPU")
	}
	t.Setenv("MACLAW_VOICE_NPU", "")
	t.Setenv("MACLAW_KOKORO_NPU", "0")
	if Disabled("") || !Disabled("MACLAW_KOKORO_NPU") || Disabled("MACLAW_SENSEVOICE_NPU") {
		t.Fatal("specific switch")
	}
}

func TestFullyConnectedRelu(t *testing.T) {
	b := &Builder{}
	x := b.InputF32([]int{0, 3})
	w := b.ConstF32([]int{4, 3}, []float32{
		1, 0, 0,
		0, 1, 0,
		0, 0, 1,
		1, 1, 1,
	})
	bias := b.ConstF32([]int{4}, []float32{0.5, 0, 0, -100})
	y := b.FullyConnected(x, w, bias, true)
	b.Output(y)
	got, err := Eval(b, []ExecIn{{
		F32:   []float32{1, 2, 3, 4, 5, 6},
		Shape: []int{2, 3},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{1.5, 2, 3, 0, 4.5, 5, 6, 0}
	if !closeF32(got[0].F32, want, 1e-6) {
		t.Fatalf("got %v", got[0].F32)
	}
}

func TestLayerNormLast(t *testing.T) {
	x := []float32{1, 2, 3, 3, 3, 3}
	gamma := []float32{1, 1, 2}
	beta := []float32{0, 0.5, -1}
	const eps = 1e-5
	b := &Builder{}
	in := b.InputF32([]int{0, 3})
	y := b.LayerNormLast(in, b.ConstF32([]int{3}, gamma), b.ConstF32([]int{3}, beta), eps)
	b.Output(y)
	got, err := Eval(b, []ExecIn{{F32: x, Shape: []int{2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	want := layerNormRef(x, gamma, beta, 2, 3, eps)
	if !closeF32(got[0].F32, want, 1e-5) {
		t.Fatalf("got %v want %v", got[0].F32, want)
	}
}

func TestAttention(t *testing.T) {
	// One head, seq 2, head dim 2. Scale is 1/sqrt(2).
	q := []float32{1, 0, 0, 1}
	k := []float32{1, 0, 0, 1}
	v := []float32{1, 2, 3, 4}
	b := &Builder{}
	qin := b.InputF32([]int{0, 2})
	kin := b.InputF32([]int{0, 2})
	vin := b.InputF32([]int{0, 2})
	y := b.Attention(qin, kin, vin, 2, 1)
	b.Output(y)
	got, err := Eval(b, []ExecIn{
		{F32: q, Shape: []int{2, 2}},
		{F32: k, Shape: []int{2, 2}},
		{F32: v, Shape: []int{2, 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := attentionRef(q, k, v, 2, 2, 1)
	if !closeF32(got[0].F32, want, 1e-5) {
		t.Fatalf("got %v want %v", got[0].F32, want)
	}
}

func TestDepthwiseSame(t *testing.T) {
	// Kernel 1,0,1 and bias 0.5. SAME pad drops the taps that fall off the edge.
	b := &Builder{}
	x := b.InputF32([]int{1, 0, 1, 1})
	filter := b.ConstF32([]int{1, 3, 1, 1}, []float32{1, 0, 1})
	bias := b.ConstF32([]int{1}, []float32{0.5})
	y := b.DepthwiseSame(x, filter, bias)
	flat := b.Reshape(y, []int{-1, 1})
	b.Output(flat)
	got, err := Eval(b, []ExecIn{{F32: []float32{1, 2, 3, 4}, Shape: []int{1, 4, 1, 1}}})
	if err != nil {
		t.Fatal(err)
	}
	// t0 drops the left tap (zero pad): 0*1 + 1*0 + 2*1 + 0.5.
	want := []float32{2.5, 4.5, 6.5, 3.5}
	if !closeF32(got[0].F32, want, 1e-6) {
		t.Fatalf("got %v", got[0].F32)
	}
}

func TestGather(t *testing.T) {
	b := &Builder{}
	params := b.ConstF32([]int{3, 2}, []float32{1, 2, 3, 4, 5, 6})
	idx := b.InputI32([]int{0})
	y := b.Gather(params, idx, 0)
	b.Output(y)
	got, err := Eval(b, []ExecIn{{I32: []int32{2, 0, 2}, Shape: []int{3}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{5, 6, 1, 2, 5, 6}
	if !closeF32(got[0].F32, want, 0) {
		t.Fatalf("got %v", got[0].F32)
	}
}

func TestConstFillMatchesStoredMatrix(t *testing.T) {
	b := &Builder{}
	x := b.InputF32([]int{1, 2})
	w := b.ConstF32Fill([]int{2, 2}, func(dst []float32) error {
		copy(dst, []float32{1, 2, 3, 4})
		return nil
	})
	y := b.FullyConnected(x, w, -1, false)
	b.Output(y)
	if b.Err() != nil {
		t.Fatal(b.Err())
	}
	if b.ConstBytes() != 24 {
		t.Fatalf("const bytes %d", b.ConstBytes())
	}
	got, err := Eval(b, []ExecIn{{F32: []float32{1, 1}, Shape: []int{1, 2}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{3, 7}
	if !closeF32(got[0].F32, want, 0) {
		t.Fatalf("got %v", got[0].F32)
	}
}

func TestConstFillError(t *testing.T) {
	b := &Builder{}
	x := b.InputF32([]int{1, 1})
	w := b.ConstF32Fill([]int{1, 1}, func([]float32) error {
		return errors.New("boom")
	})
	y := b.FullyConnected(x, w, -1, false)
	b.Output(y)
	if _, err := Eval(b, []ExecIn{{F32: []float32{1}, Shape: []int{1, 1}}}); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err %v", err)
	}
	empty := &Builder{}
	if empty.ConstF32Fill([]int{0}, func([]float32) error { return nil }) != -1 || empty.Err() == nil {
		t.Fatal("expected dynamic fill rejection")
	}
}

func TestDepthwiseLayoutRejected(t *testing.T) {
	b := &Builder{}
	x := b.InputF32([]int{1, 4, 1, 1})
	filter := b.ConstF32([]int{3}, []float32{1, 0, 1})
	bias := b.ConstF32([]int{1}, []float32{0})
	if b.DepthwiseSame(x, filter, bias) != -1 || b.Err() == nil {
		t.Fatal("expected layout error")
	}
}

func TestBiasShapeRejected(t *testing.T) {
	b := &Builder{}
	x := b.InputF32([]int{1, 2})
	w := b.ConstF32([]int{2, 2}, []float32{1, 0, 0, 1})
	bias := b.ConstF32([]int{1}, []float32{1})
	if b.FullyConnected(x, w, bias, false) != -1 || b.Err() == nil {
		t.Fatal("expected bias rank error")
	}
}

func layerNormRef(x, gamma, beta []float32, rows, dim int, eps float32) []float32 {
	out := make([]float32, len(x))
	for r := 0; r < rows; r++ {
		row := x[r*dim : (r+1)*dim]
		mean := float32(0)
		for _, v := range row {
			mean += v
		}
		mean /= float32(dim)
		var varc float32
		for _, v := range row {
			d := v - mean
			varc += d * d
		}
		varc /= float32(dim)
		inv := float32(1 / math.Sqrt(float64(varc+eps)))
		for c := 0; c < dim; c++ {
			out[r*dim+c] = (row[c]-mean)*inv*gamma[c] + beta[c]
		}
	}
	return out
}

func attentionRef(q, k, v []float32, seq, hidden, heads int) []float32 {
	headDim := hidden / heads
	scale := float32(1 / math.Sqrt(float64(headDim)))
	out := make([]float32, seq*hidden)
	for h := 0; h < heads; h++ {
		base := h * headDim
		for i := 0; i < seq; i++ {
			scores := make([]float32, seq)
			max := float32(math.Inf(-1))
			for j := 0; j < seq; j++ {
				var dot float32
				for d := 0; d < headDim; d++ {
					dot += q[i*hidden+base+d] * k[j*hidden+base+d]
				}
				scores[j] = dot * scale
				if scores[j] > max {
					max = scores[j]
				}
			}
			sum := float32(0)
			for j := range scores {
				scores[j] = float32(math.Exp(float64(scores[j] - max)))
				sum += scores[j]
			}
			for d := 0; d < headDim; d++ {
				var acc float32
				for j := 0; j < seq; j++ {
					acc += scores[j] / sum * v[j*hidden+base+d]
				}
				out[i*hidden+base+d] = acc
			}
		}
	}
	return out
}

func closeF32(got, want []float32, tol float32) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > tol {
			return false
		}
	}
	return true
}
