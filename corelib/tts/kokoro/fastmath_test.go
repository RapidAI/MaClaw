package kokoro

import (
	"math"
	"testing"
)

func TestFastSinMatchesLibm(t *testing.T) {
	maxd := float32(0)
	var at float32
	check := func(x float32) {
		d := fastSin32(x) - float32(math.Sin(float64(x)))
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
			at = x
		}
	}
	for i := -6400; i <= 6400; i++ {
		check(float32(i) * 0.01)
	}
	for _, x := range []float32{0, 0.5, -0.5, 1, -1, 10, -10, 100, -100, 1e5, -1e5, float32(math.Pi), float32(-math.Pi / 2)} {
		check(x)
	}
	if maxd > 2e-6 {
		t.Fatalf("sin maxAbs=%g at %g", maxd, at)
	}
}

func TestFastSinCosMatchesLibm(t *testing.T) {
	maxS, maxC := float32(0), float32(0)
	var atS, atC float32
	same := func(a, b float32) bool {
		return a == b || (a != a && b != b)
	}
	check := func(x float32) {
		s, c := fastSinCos32(x)
		if !same(s, fastSin32(x)) {
			t.Fatalf("sin mismatch at %g fastSin %g sincos %g", x, fastSin32(x), s)
		}
		ds := s - float32(math.Sin(float64(x)))
		dc := c - float32(math.Cos(float64(x)))
		if ds < 0 {
			ds = -ds
		}
		if dc < 0 {
			dc = -dc
		}
		if ds > maxS {
			maxS = ds
			atS = x
		}
		if dc > maxC {
			maxC = dc
			atC = x
		}
	}
	for i := -6400; i <= 6400; i++ {
		check(float32(i) * 0.01)
	}
	for _, x := range []float32{0, 0.5, -0.5, 1, -1, 10, -10, 100, -100, 1e5, -1e5, float32(math.Pi), float32(-math.Pi / 2), float32(math.NaN())} {
		check(x)
	}
	if maxS > 2e-6 || maxC > 2e-6 {
		t.Fatalf("sincos sin maxAbs=%g at %g cos maxAbs=%g at %g", maxS, atS, maxC, atC)
	}
}

func TestFastAtanHypotMatchesLibm(t *testing.T) {
	maxA, maxH := float32(0), float32(0)
	var atA, atH float32
	checkAtan := func(x float32) {
		if x != x {
			if fastAtan32(x) == fastAtan32(x) {
				t.Fatalf("atan nan became %g", fastAtan32(x))
			}
			return
		}
		d := fastAtan32(x) - float32(math.Atan(float64(x)))
		if d < 0 {
			d = -d
		}
		if d > maxA {
			maxA = d
			atA = x
		}
	}
	for i := -4000; i <= 4000; i++ {
		checkAtan(float32(i) * 0.01)
	}
	for _, x := range []float32{0, 0.4375, 0.6875, 1, 1.1875, 2.4375, -0.5, 10, -10, 1e6, -1e6, float32(math.Inf(1)), float32(math.Inf(-1))} {
		checkAtan(x)
	}
	checkHyp := func(x, y float32) {
		d := fastHypot32(x, y) - float32(math.Hypot(float64(x), float64(y)))
		if d < 0 {
			d = -d
		}
		den := float32(math.Hypot(float64(x), float64(y)))
		if den > 1 {
			d /= den
		}
		if d > maxH {
			maxH = d
			atH = x + y
		}
	}
	for i := -200; i <= 200; i++ {
		for j := -200; j <= 200; j++ {
			checkHyp(float32(i)*0.05, float32(j)*0.07)
		}
	}
	checkHyp(0, 0)
	checkHyp(3, 4)
	checkHyp(-3, 4)
	if maxA > 2e-6 || maxH > 2e-6 {
		t.Fatalf("atan maxAbs=%g at %g hypot max=%g near %g", maxA, atA, maxH, atH)
	}
	max2 := float32(0)
	var at2 float32
	check2 := func(y, x float32) {
		got := fastAtan232(y, x)
		want := float32(math.Atan2(float64(y), float64(x)))
		d := got - want
		if d > math.Pi {
			d -= 2 * math.Pi
		} else if d < -math.Pi {
			d += 2 * math.Pi
		}
		if d < 0 {
			d = -d
		}
		if d > max2 {
			max2 = d
			at2 = y
			if x > at2 {
				at2 = x
			}
		}
	}
	for i := -80; i <= 80; i++ {
		for j := -80; j <= 80; j++ {
			check2(float32(i)*0.1, float32(j)*0.1)
		}
	}
	for _, y := range []float32{-2, -0.1, 0, 0.1, 2} {
		for _, x := range []float32{-3, -0.2, 0, 0.2, 4} {
			check2(y, x)
		}
	}
	if max2 > 2e-6 {
		t.Fatalf("atan2 maxAbs=%g near %g", max2, at2)
	}
}

func TestFastExpMatchesLibm(t *testing.T) {
	maxRel := 0.0
	var at float32
	for i := -8000; i <= 8000; i++ {
		x := float32(i) * 0.01
		got := float64(fastExp32(x))
		want := math.Exp(float64(x))
		den := math.Abs(want)
		if den < 1e-30 {
			den = 1
		}
		rel := math.Abs(got-want) / den
		if rel > maxRel {
			maxRel = rel
			at = x
		}
	}
	if maxRel > 2e-6 {
		t.Fatalf("exp maxRel=%g at %g", maxRel, at)
	}
}

func TestFastTanhSigmoidMatchesLibm(t *testing.T) {
	maxT, maxS := float32(0), float32(0)
	for i := -2000; i <= 2000; i++ {
		x := float32(i) * 0.01
		td := fastTanh32(x) - float32(math.Tanh(float64(x)))
		if td < 0 {
			td = -td
		}
		if td > maxT {
			maxT = td
		}
		sd := fastSigmoid32(x) - float32(1/(1+math.Exp(float64(-x))))
		if sd < 0 {
			sd = -sd
		}
		if sd > maxS {
			maxS = sd
		}
	}
	if maxT > 2e-6 || maxS > 2e-6 {
		t.Fatalf("tanh maxAbs=%g sigmoid maxAbs=%g", maxT, maxS)
	}
}

func TestFastGeluMatchesLibm(t *testing.T) {
	maxd := float32(0)
	var at float32
	for i := -1200; i <= 1200; i++ {
		x := float32(i) * 0.01
		got := fastGelu32(x)
		want := 0.5 * x * (1 + float32(math.Tanh(float64(0.7978845608028654*(x+0.044715*x*x*x)))))
		d := got - want
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
			at = x
		}
	}
	// 0.5*|x| times the tanh error. The grid crosses the |inner|=9 clamp.
	if maxd > 2e-5 {
		t.Fatalf("gelu maxAbs=%g at %g", maxd, at)
	}
}

func TestFastSoftmaxMatchesLibm(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	logits := []float32{-4, -1, 0, 0.5, 2, 8, -20, 3.5}
	libm := append([]float32(nil), logits...)
	fast := append([]float32(nil), logits...)
	kokoroFastMath = false
	softmaxInplace(libm)
	kokoroFastMath = true
	softmaxInplace(fast)
	for i := range libm {
		d := fast[i] - libm[i]
		if d < 0 {
			d = -d
		}
		if d > 2e-5 {
			t.Fatalf("softmax[%d]=%g libm=%g", i, fast[i], libm[i])
		}
	}
}

func TestLSTMFastMathMatchesLibm(t *testing.T) {
	saved := kokoroFastMath
	t.Cleanup(func() { kokoroFastMath = saved })
	const steps, inDim, hidden = 6, 48, 32
	rows := 4 * hidden
	w := LSTMWeights{
		WeightIH: make([]float32, rows*inDim),
		WeightHH: make([]float32, rows*hidden),
		BiasIH:   make([]float32, rows),
		BiasHH:   make([]float32, rows),
		InputDim: inDim,
		Hidden:   hidden,
	}
	x := make([]float32, steps*inDim)
	for i := range w.WeightIH {
		w.WeightIH[i] = float32((i%11)-5) * 0.02
	}
	for i := range w.WeightHH {
		w.WeightHH[i] = float32((i%9)-4) * 0.02
	}
	for i := range w.BiasIH {
		w.BiasIH[i] = float32((i%7)-3) * 0.01
	}
	for i := range x {
		x[i] = float32((i%13)-6) * 0.03
	}
	run := func() []float32 {
		out := make([]float32, steps*hidden)
		if err := LSTMLayer(out, x, steps, w, false); err != nil {
			t.Fatal(err)
		}
		return out
	}
	kokoroFastMath = false
	want := run()
	kokoroFastMath = true
	got := run()
	lstmOutputsClose(t, got, want)
}
