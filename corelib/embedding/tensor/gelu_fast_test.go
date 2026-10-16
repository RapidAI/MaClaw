package tensor

import (
	"math"
	"testing"
)

func tanhF32Poly(x float32) float32 {
	z := x
	if z < 0 {
		z = -z
	}
	var y float32
	switch {
	case z >= 9:
		y = 1
	case z >= 0.625:
		e := expF32(2 * z)
		y = float32(1 - 2/(float64(e)+1))
	default:
		s := z * z
		p := (float32(-0.96439916)*s+float32(-99.28772))*s + float32(-1614.6877)
		q := ((s+float32(112.81168))*s+float32(2235.4883))*s + float32(4844.063)
		y = z + z*s*p/q
	}
	if x < 0 {
		return -y
	}
	return y
}

func TestTanhF32PolyMatchesMath(t *testing.T) {
	maxT, maxG := 0.0, 0.0
	for i := -20000; i <= 20000; i++ {
		x := float32(i) * 0.002
		if d := math.Abs(float64(tanhF32Poly(x) - float32(math.Tanh(float64(x))))); d > maxT {
			maxT = d
		}
		inner := float32(geluPyTorchC) * (x + 0.044715*x*x*x)
		got := 0.5 * x * (1 + tanhF32Poly(inner))
		ref := geluPyTorchTanh(float64(x))
		if d := math.Abs(float64(got - ref)); d > maxG {
			maxG = d
		}
	}
	t.Logf("poly tanh %g gelu %g", maxT, maxG)
	if maxT > 2e-6 || maxG > 2e-6 {
		t.Fatalf("poly tanh %g gelu %g", maxT, maxG)
	}
}

func TestGELUMulMatchesMathTanh(t *testing.T) {
	check := func(gate, up []float32) float64 {
		t.Helper()
		src := append([]float32(nil), gate...)
		got := append([]float32(nil), gate...)
		GELUMul(got, up)
		var maxG float64
		body := len(gate) &^ 7
		differ := 0
		for i := range src {
			if math.IsNaN(float64(got[i])) || math.IsInf(float64(got[i]), 0) {
				t.Fatalf("n=%d non-finite at %d v=%g up=%g got=%g", len(gate), i, src[i], up[i], got[i])
			}
			ref := geluPyTorchTanh(float64(src[i])) * up[i]
			if d := math.Abs(float64(got[i] - ref)); d > maxG {
				maxG = d
			}
			if i < body {
				scalar := geluPyTorchTanhFast(src[i]) * up[i]
				if math.Float32bits(got[i]) != math.Float32bits(scalar) {
					differ++
				}
			} else {
				want := geluPyTorchTanhFast(src[i]) * up[i]
				if got[i] != want && !(got[i] == 0 && want == 0) {
					t.Fatalf("tail[%d] got %g want scalar %g", i, got[i], want)
				}
			}
		}
		if len(gate) >= 1000 && differ == 0 {
			t.Fatalf("n=%d avx body matched scalar bits on every lane", len(gate))
		}
		if len(gate) >= 1000 {
			t.Logf("n=%d body lanes differing from scalar %d/%d", len(gate), differ, body)
		}
		if maxG > 2e-6 {
			t.Fatalf("n=%d gelu max abs %g", len(gate), maxG)
		}
		return maxG
	}
	gate := make([]float32, 40001)
	up := make([]float32, len(gate))
	for i := range gate {
		gate[i] = float32(i-20000) * 0.002
		up[i] = 1
	}
	t.Logf("sweep up=1 max abs %g", check(gate, up))
	for i := range up {
		up[i] = -1.25
	}
	t.Logf("sweep up=-1.25 max abs %g", check(gate, up))

	for _, n := range []int{0, 1, 7, 8, 9, 17, 1152} {
		g := make([]float32, n)
		u := make([]float32, n)
		for i := range g {
			g[i] = float32((i%41)-20) * 0.37
			u[i] = float32((i%9)-4) * 0.5
		}
		check(g, u)
	}
	// One vector in each tanh region, plus a split across 0.625 and 9.
	check([]float32{0.05, -0.02, 0.1, -0.08, 0.01, 0.2, -0.15, 0.03}, []float32{1, 1, 1, 1, 1, 1, 1, 1})
	check([]float32{1.2, -1.4, 1.8, -2.2, 2.6, -3, 3.4, -3.8}, []float32{0.5, -1, 2, 0.25, -0.5, 1, -2, 0.75})
	check([]float32{12, -15, 20, -40, 8, -9, 30, -11}, []float32{1, -1, 0.5, 2, -3, 0.25, 1, -0.5})
	check([]float32{0.05, 1.5, 12, -0.1, -2, 40, 0.2, -8}, []float32{1, 1, 1, 1, 1.5, -1.5, 0.5, -0.5})
}

func BenchmarkGELUMulLayer(b *testing.B) {
	const n = 48 * 1152
	gate := make([]float32, n)
	up := make([]float32, n)
	for i := range gate {
		gate[i] = float32(i%23) - 11
		up[i] = float32((i%7)-3) * 0.5
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GELUMul(gate, up)
	}
}

func TestTanhF32MatchesMath(t *testing.T) {
	maxAbs := 0.0
	maxGELU := 0.0
	for i := -20000; i <= 20000; i++ {
		x := float32(i) * 0.002 // [-40, 40]
		refT := float32(math.Tanh(float64(x)))
		gotT := tanhF32(x)
		if d := math.Abs(float64(gotT - refT)); d > maxAbs {
			maxAbs = d
		}
		refG := geluPyTorchTanh(float64(x))
		gotG := geluPyTorchTanhFast(x)
		if d := math.Abs(float64(gotG - refG)); d > maxGELU {
			maxGELU = d
		}
	}
	t.Logf("tanh max abs %g gelu max abs %g", maxAbs, maxGELU)
	if maxAbs > 2e-7 || maxGELU > 1e-6 {
		t.Fatalf("tanh %g gelu %g", maxAbs, maxGELU)
	}
}
