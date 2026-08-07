//go:build amd64

package onnxrt

import (
	"math"
	"math/rand"
	"testing"
)

// Cross-validation of the AVX2 kernels against the portable Go fallbacks:
// the exact same inputs go through the SIMD path and the scalar path, with
// bit-exact or bounded-ulp equality required. The fallback is forced by
// flipping the hasAVX2FMA dispatch var (the minimal seam — no build tags).
//
// Rounding model: the kernels use FMA (single rounding) while the portable
// loops do mul-then-add (double rounding), so fmadd-style kernels are NOT
// bit-identical to the fallback in general; they agree to <=2 ulp except
// under heavy cancellation, where the honest bound is the ulp of the
// largest intermediate magnitude. geluErfAVX2 is bit-exact with the
// vek32-op pipeline by construction; transpose8x8F32 is pure data movement
// and must be bit-exact always.

// f32Ord maps a non-NaN float32 to an order-preserving int64 (+0 == -0).
func f32Ord(f float32) int64 {
	i := int64(int32(math.Float32bits(f)))
	if i < 0 {
		i = math.MinInt32 - i
	}
	return i
}

// ulpDiff returns the ulp distance between a and b. Both NaN counts as 0;
// a lone NaN or a mismatched infinity counts as "infinite" distance.
func ulpDiff(a, b float32) uint64 {
	na, nb := math.IsNaN(float64(a)), math.IsNaN(float64(b))
	if na || nb {
		if na && nb {
			return 0
		}
		return math.MaxUint64
	}
	if math.IsInf(float64(a), 0) || math.IsInf(float64(b), 0) {
		if a == b {
			return 0
		}
		return math.MaxUint64
	}
	d := f32Ord(a) - f32Ord(b)
	if d < 0 {
		return uint64(-d)
	}
	return uint64(d)
}

// ulpOf returns the ulp of a finite float32 magnitude as float64.
func ulpOf(scale float64) float64 {
	s := float32(scale)
	if s == 0 {
		return float64(math.Nextafter32(0, 1))
	}
	return float64(math.Nextafter32(s, float32(math.Inf(1))) - s)
}

// hostileF32 are values that stress rounding, overflow and NaN/Inf paths.
var hostileF32 = []float32{
	float32(math.Inf(1)), float32(math.Inf(-1)), float32(math.NaN()),
	0, float32(math.Copysign(0, -1)),
	1e-38, -1e-38, 1e38, -1e38, 6e-39, -6e-39, // subnormal-ish
	88.0, -88.0, 9.0, -9.0, 3.14159, -2.71828, 1e-20,
}

// fillRandomF32 fills buf with uniform [-10, 10] values plus hostile ones.
func fillRandomF32(rng *rand.Rand, buf []float32) {
	for i := range buf {
		if rng.Intn(7) == 0 {
			buf[i] = hostileF32[rng.Intn(len(hostileF32))]
		} else {
			buf[i] = rng.Float32()*20 - 10
		}
	}
}

// crossvalSizes returns every size in [0, 130] plus random sizes up to 500.
func crossvalSizes(rng *rand.Rand) []int {
	sizes := make([]int, 0, 200)
	for n := 0; n <= 130; n++ {
		sizes = append(sizes, n)
	}
	for k := 0; k < 60; k++ {
		sizes = append(sizes, 131+rng.Intn(370))
	}
	return sizes
}

// requireAVX2 skips when the CPU lacks AVX2+FMA and restores the dispatch
// var at test end.
func requireAVX2(t *testing.T) {
	t.Helper()
	if !hasAVX2FMA {
		t.Skip("AVX2+FMA not available on this CPU")
	}
	t.Cleanup(func() { hasAVX2FMA = true })
}

// bitsEqF32 compares two float32s treating NaN == NaN (payloads may differ
// between hardware NaN propagation and Go float64->float32 conversion).
func bitsEqF32(a, b float32) bool {
	if math.IsNaN(float64(a)) || math.IsNaN(float64(b)) {
		return math.IsNaN(float64(a)) && math.IsNaN(float64(b))
	}
	return math.Float32bits(a) == math.Float32bits(b)
}

// checkFMADispatch verifies one fmadd-style element. The kernels use FMA
// (single rounding); the portable fallback does mul-then-add (double
// rounding). fmaRef/scalarRef replicate each rounding sequence exactly in
// float64 (float64(w)*float64(x) is exact), so:
//
//   - the scalar path must match scalarRef bit-for-bit (gc on amd64 never
//     auto-fuses, so the fallback IS mul-then-add);
//   - the SIMD path must match fmaRef to <=maxUlp ulp (the float64
//     simulation rounds at 2^-53 before the final float32 rounding, which
//     can flip a float32 boundary in rare cases);
//   - cross-path, the two must agree to <=2 ulp, or — under cancellation —
//     both stay within a few ulp-of-intermediate-scale of the exact value.
//
// overflow reports that some exact intermediate product exceeded float32
// range: then the scalar path first rounds that product to ±Inf, which can
// legitimately yield Inf/NaN where the FMA path yields a finite (or
// opposite-class) result. That divergence is inherent to single vs double
// rounding, matches the documented w==0 fast-path deviation, and is
// unreachable for finite model data — the cross-path check is skipped.
func checkFMADispatch(t *testing.T, ctx string, i int, simd, scalar, fmaRef, scalarRef float32, scale float64, overflow bool, maxUlp uint64) {
	t.Helper()
	if !bitsEqF32(scalar, scalarRef) {
		t.Fatalf("%s i=%d: scalar path %v (0x%08x) != simulated scalar ref %v (0x%08x)",
			ctx, i, scalar, math.Float32bits(scalar), scalarRef, math.Float32bits(scalarRef))
	}
	switch {
	case math.IsNaN(float64(fmaRef)):
		if !math.IsNaN(float64(simd)) {
			t.Fatalf("%s i=%d: fma ref NaN, simd %v (0x%08x)", ctx, i, simd, math.Float32bits(simd))
		}
	case math.IsInf(float64(fmaRef), 0):
		if simd != fmaRef {
			t.Fatalf("%s i=%d: fma ref %v, simd %v (0x%08x)", ctx, i, fmaRef, simd, math.Float32bits(simd))
		}
	default:
		if ulpDiff(simd, fmaRef) > maxUlp {
			t.Fatalf("%s i=%d: simd %v (0x%08x) vs fma ref %v (0x%08x): >%d ulp",
				ctx, i, simd, math.Float32bits(simd), fmaRef, math.Float32bits(fmaRef), maxUlp)
		}
	}
	if overflow {
		return
	}
	if ulpDiff(simd, scalar) <= 2 {
		return
	}
	// Cancellation: bound the absolute error by the ulp of the largest
	// intermediate magnitude (capped so ulpOf stays finite).
	tol := 3 * ulpOf(math.Min(scale, 3e38))
	if math.Abs(float64(simd)-float64(fmaRef)) > tol || math.Abs(float64(scalar)-float64(fmaRef)) > tol {
		t.Fatalf("%s i=%d: simd %v (0x%08x), scalar %v (0x%08x), fma ref %v, scale %v",
			ctx, i, simd, math.Float32bits(simd), scalar, math.Float32bits(scalar), fmaRef, scale)
	}
}

// prodOverflowsF32 reports whether the exact product's magnitude exceeds
// float32 range (the scalar path rounds it to ±Inf; the FMA path keeps full
// precision), or is non-finite in a way the cross-path check still covers.
func prodOverflowsF32(prod float64) bool {
	return !math.IsInf(prod, 0) && !math.IsNaN(prod) && math.Abs(prod) > float64(math.MaxFloat32)
}

func TestFmaddScalarCrossValidation(t *testing.T) {
	requireAVX2(t)
	rng := rand.New(rand.NewSource(20260807))
	for _, n := range crossvalSizes(rng) {
		off := rng.Intn(4) // unaligned base pointer
		xbuf := make([]float32, off+n+4)
		obuf := make([]float32, off+n+4)
		fillRandomF32(rng, xbuf)
		fillRandomF32(rng, obuf)
		x := xbuf[off : off+n]
		out := obuf[off : off+n]
		// Occasionally shorten x to exercise the len(x) clamp.
		if n > 2 && rng.Intn(5) == 0 {
			x = x[:rng.Intn(n)]
		}
		ws := []float32{rng.Float32()*4 - 2, 1, -1, 1e38, -1e-38, 0}
		w := ws[rng.Intn(len(ws))]

		got := append([]float32(nil), out...)
		want := append([]float32(nil), out...)
		fmaddScalarInto(got, x, w) // SIMD dispatch
		hasAVX2FMA = false
		fmaddScalarInto(want, x, w) // forced portable fallback
		hasAVX2FMA = true

		nEff := len(out)
		if len(x) < nEff {
			nEff = len(x)
		}
		// The wrapper runs its own scalar loop past the 8-aligned body, even
		// in SIMD mode — tail elements must match the double-rounding ref.
		body := 0
		if nEff >= 8 {
			body = nEff &^ 7
		}
		for i := 0; i < nEff; i++ {
			if w == 0 {
				// amd64 fast path: out untouched regardless of x (compare
				// bits — the pre-fill may itself be NaN).
				if math.Float32bits(got[i]) != math.Float32bits(out[i]) || math.Float32bits(want[i]) != math.Float32bits(out[i]) {
					t.Fatalf("w==0 n=%d i=%d: got %v want %v orig %v", n, i, got[i], want[i], out[i])
				}
				continue
			}
			prod := float64(w) * float64(x[i]) // exact
			fmaRef := float32(float64(out[i]) + prod)
			scalarRef := float32(float64(out[i]) + float64(float32(prod)))
			scale := math.Max(math.Max(math.Abs(prod), math.Abs(float64(out[i]))), math.Abs(float64(fmaRef)))
			if i >= body {
				if !bitsEqF32(got[i], scalarRef) || !bitsEqF32(want[i], scalarRef) {
					t.Fatalf("fmaddScalar tail n=%d i=%d: got %v (0x%08x), want %v (0x%08x), scalar ref %v (0x%08x)",
						n, i, got[i], math.Float32bits(got[i]), want[i], math.Float32bits(want[i]), scalarRef, math.Float32bits(scalarRef))
				}
				continue
			}
			checkFMADispatch(t, "fmaddScalar", i, got[i], want[i], fmaRef, scalarRef, scale, prodOverflowsF32(prod), 1)
		}
	}
}

func TestFmadd3CrossValidation(t *testing.T) {
	requireAVX2(t)
	rng := rand.New(rand.NewSource(20260808))
	for _, n := range crossvalSizes(rng) {
		off := rng.Intn(4)
		xbuf := make([]float32, off+n+6)
		obuf := make([]float32, off+n+4)
		fillRandomF32(rng, xbuf)
		fillRandomF32(rng, obuf)
		x := xbuf[off : off+n+2]
		out := obuf[off : off+n]
		// Occasionally shorten x below n+2 to exercise the len(x)-2 clamp.
		if n > 4 && rng.Intn(5) == 0 {
			x = x[:2+rng.Intn(n)]
		}
		w0 := rng.Float32()*4 - 2
		w1 := rng.Float32()*4 - 2
		w2 := rng.Float32()*4 - 2

		got := append([]float32(nil), out...)
		want := append([]float32(nil), out...)
		fmadd3Into(got, x, w0, w1, w2)
		hasAVX2FMA = false
		fmadd3Into(want, x, w0, w1, w2)
		hasAVX2FMA = true

		nEff := len(out)
		if len(x)-2 < nEff {
			nEff = len(x) - 2
		}
		if nEff < 0 {
			nEff = 0
		}
		body := 0
		if nEff >= 8 {
			body = nEff &^ 7
		}
		for i := 0; i < nEff; i++ {
			// Replicate each path's rounding sequence per tap in float64.
			p0 := float64(w0) * float64(x[i])
			p1 := float64(w1) * float64(x[i+1])
			p2 := float64(w2) * float64(x[i+2])
			fmaRef := float32(float64(out[i]) + p0)
			fmaRef = float32(float64(fmaRef) + p1)
			fmaRef = float32(float64(fmaRef) + p2)
			scalarRef := float32(float64(out[i]) + float64(float32(p0)))
			scalarRef = float32(float64(scalarRef) + float64(float32(p1)))
			scalarRef = float32(float64(scalarRef) + float64(float32(p2)))
			if i >= body {
				// Wrapper scalar tail: both paths run the same loop.
				if !bitsEqF32(got[i], scalarRef) || !bitsEqF32(want[i], scalarRef) {
					t.Fatalf("fmadd3 tail n=%d i=%d: got %v (0x%08x), want %v (0x%08x), scalar ref %v (0x%08x)",
						n, i, got[i], math.Float32bits(got[i]), want[i], math.Float32bits(want[i]), scalarRef, math.Float32bits(scalarRef))
				}
				continue
			}
			s1 := float64(out[i]) + p0
			s2 := s1 + p1
			scale := math.Abs(float64(out[i]))
			for _, v := range []float64{p0, s1, p1, s2, p2, s2 + p2} {
				if math.Abs(v) > scale {
					scale = math.Abs(v)
				}
			}
			overflow := prodOverflowsF32(p0) || prodOverflowsF32(p1) || prodOverflowsF32(p2)
			checkFMADispatch(t, "fmadd3", i, got[i], want[i], fmaRef, scalarRef, scale, overflow, 4)
		}
	}
}

func TestGeluErfCrossValidation(t *testing.T) {
	requireAVX2(t)
	rng := rand.New(rand.NewSource(20260809))
	for _, n := range crossvalSizes(rng) {
		off := rng.Intn(4)
		sbuf := make([]float32, off+n+4)
		fillRandomF32(rng, sbuf)
		src := sbuf[off : off+n]

		got := make([]float32, n)
		want := make([]float32, n)
		geluErfInto(got, src)     // fused kernel + portable tail
		geluErfIntoVek(want, src) // portable vek32-op pipeline
		for i := 0; i < n; i++ {
			g, w := got[i], want[i]
			if math.IsNaN(float64(g)) && math.IsNaN(float64(w)) {
				continue
			}
			if math.Float32bits(g) != math.Float32bits(w) {
				t.Fatalf("n=%d i=%d src=%v: got %v (0x%08x), want %v (0x%08x)",
					n, i, src[i], g, math.Float32bits(g), w, math.Float32bits(w))
			}
		}
		// In-place must match too.
		inplace := append([]float32(nil), src...)
		geluErfInto(inplace, inplace)
		for i := 0; i < n; i++ {
			if math.IsNaN(float64(inplace[i])) && math.IsNaN(float64(want[i])) {
				continue
			}
			if math.Float32bits(inplace[i]) != math.Float32bits(want[i]) {
				t.Fatalf("in-place n=%d i=%d: got %v, want %v", n, i, inplace[i], want[i])
			}
		}
	}
}

func TestTransposePlanesCrossValidation(t *testing.T) {
	requireAVX2(t)
	rng := rand.New(rand.NewSource(20260810))
	for trial := 0; trial < 400; trial++ {
		C := 1 + rng.Intn(40)
		HW := 1 + rng.Intn(80)
		p0 := rng.Intn(HW)
		p1 := p0 + rng.Intn(HW-p0+1)
		src := make([]float32, C*HW)
		fillRandomF32(rng, src)
		dstSIMD := make([]float32, HW*C)
		dstScal := make([]float32, HW*C)
		fillRandomF32(rng, dstSIMD) // pre-fill: untouched cells must stay
		copy(dstScal, dstSIMD)

		transposePlanesRange(dstSIMD, src, C, HW, p0, p1)
		hasAVX2FMA = false
		transposePlanesRange(dstScal, src, C, HW, p0, p1)
		hasAVX2FMA = true

		for i := range dstSIMD {
			// Pure data movement: bit-exact, NaN payloads included.
			if math.Float32bits(dstSIMD[i]) != math.Float32bits(dstScal[i]) {
				t.Fatalf("C=%d HW=%d [%d,%d) i=%d: simd 0x%08x, scalar 0x%08x",
					C, HW, p0, p1, i, math.Float32bits(dstSIMD[i]), math.Float32bits(dstScal[i]))
			}
		}
	}
}

// TestSIMDKernelsDirect exercises the kernels at their exact contract
// boundaries: n == 0 (no-op, no OOB access), minimal 8-wide blocks, and
// unaligned bases. Canary elements guard against out-of-bounds writes.
func TestSIMDKernelsDirect(t *testing.T) {
	requireAVX2(t)
	rng := rand.New(rand.NewSource(20260811))

	// n == 0 must be a no-op for every kernel (do-while hardening).
	{
		buf := []float32{1, 2, 3, 4, 5, 6, 7, 8}
		orig := append([]float32(nil), buf...)
		fmaddScalarAVX2(&buf[0], &buf[0], 3.5, 0)
		fmadd3AVX2(&buf[0], &buf[0], 1, 2, 3, 0)
		geluErfAVX2(&buf[0], &buf[0], 0)
		for i := range buf {
			if buf[i] != orig[i] {
				t.Fatalf("n=0 kernel modified buf[%d]: %v -> %v", i, orig[i], buf[i])
			}
		}
	}

	// fmaddScalarAVX2 direct: 8-wide blocks incl. non-32-multiples.
	for _, n := range []int{8, 16, 24, 40, 72} {
		off := rng.Intn(4)
		xbuf := make([]float32, off+n+8)
		obuf := make([]float32, off+n+8)
		fillRandomF32(rng, xbuf)
		fillRandomF32(rng, obuf)
		x := xbuf[off : off+n]
		out := obuf[off : off+n]
		w := rng.Float32()*4 - 2
		got := append([]float32(nil), out...)
		want := append([]float32(nil), out...)
		fmaddScalarAVX2(&got[0], &x[0], w, n)
		fmaddScalarScalar(want, x, w, n)
		for i := 0; i < n; i++ {
			prod := float64(w) * float64(x[i])
			fmaRef := float32(float64(out[i]) + prod)
			scalarRef := float32(float64(out[i]) + float64(float32(prod)))
			scale := math.Max(math.Max(math.Abs(prod), math.Abs(float64(out[i]))), math.Abs(float64(fmaRef)))
			checkFMADispatch(t, "fmaddScalarAVX2 direct", i, got[i], want[i], fmaRef, scalarRef, scale, prodOverflowsF32(prod), 1)
		}
	}

	// fmadd3AVX2 direct: minimal 8-wide block, canary after x.
	for _, n := range []int{8, 16, 48} {
		off := rng.Intn(4)
		xbuf := make([]float32, off+n+2+1)
		obuf := make([]float32, off+n+1)
		fillRandomF32(rng, xbuf)
		fillRandomF32(rng, obuf)
		x := xbuf[off : off+n+2]
		out := obuf[off : off+n]
		canaryX, canaryO := xbuf[off+n+2], obuf[off+n]
		w0, w1, w2 := rng.Float32()*4-2, rng.Float32()*4-2, rng.Float32()*4-2
		got := append([]float32(nil), out...)
		want := append([]float32(nil), out...)
		fmadd3AVX2(&got[0], &x[0], w0, w1, w2, n)
		fmadd3Scalar(want, x, w0, w1, w2, n)
		if xbuf[off+n+2] != canaryX || obuf[off+n] != canaryO {
			t.Fatalf("fmadd3AVX2 n=%d clobbered canary", n)
		}
		for i := 0; i < n; i++ {
			p0 := float64(w0) * float64(x[i])
			p1 := float64(w1) * float64(x[i+1])
			p2 := float64(w2) * float64(x[i+2])
			fmaRef := float32(float64(out[i]) + p0)
			fmaRef = float32(float64(fmaRef) + p1)
			fmaRef = float32(float64(fmaRef) + p2)
			scalarRef := float32(float64(out[i]) + float64(float32(p0)))
			scalarRef = float32(float64(scalarRef) + float64(float32(p1)))
			scalarRef = float32(float64(scalarRef) + float64(float32(p2)))
			s1 := float64(out[i]) + p0
			s2 := s1 + p1
			scale := math.Abs(float64(out[i]))
			for _, v := range []float64{p0, s1, p1, s2, p2, s2 + p2} {
				if math.Abs(v) > scale {
					scale = math.Abs(v)
				}
			}
			overflow := prodOverflowsF32(p0) || prodOverflowsF32(p1) || prodOverflowsF32(p2)
			checkFMADispatch(t, "fmadd3AVX2 direct", i, got[i], want[i], fmaRef, scalarRef, scale, overflow, 4)
		}
	}

	// geluErfAVX2 direct: 32-aligned sizes are bit-exact vs the vek pipeline;
	// in-place safe.
	for _, n := range []int{32, 64, 96} {
		off := rng.Intn(4)
		sbuf := make([]float32, off+n+8)
		fillRandomF32(rng, sbuf)
		src := sbuf[off : off+n]
		got := make([]float32, n+1)
		canary := got[n]
		want := make([]float32, n)
		geluErfAVX2(&got[0], &src[0], n)
		geluErfIntoVek(want, src)
		if got[n] != canary {
			t.Fatalf("geluErfAVX2 n=%d clobbered canary", n)
		}
		inplace := append([]float32(nil), src...)
		geluErfAVX2(&inplace[0], &inplace[0], n)
		for i := 0; i < n; i++ {
			if math.IsNaN(float64(got[i])) && math.IsNaN(float64(want[i])) {
				continue
			}
			if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
				t.Fatalf("geluErfAVX2 n=%d i=%d: got 0x%08x want 0x%08x",
					n, i, math.Float32bits(got[i]), math.Float32bits(want[i]))
			}
			if math.Float32bits(inplace[i]) != math.Float32bits(got[i]) {
				t.Fatalf("geluErfAVX2 in-place n=%d i=%d mismatch", n, i)
			}
		}
	}

	// transpose8x8F32 direct: varying leading dimensions, bit-exact vs the
	// scalar reference, NaN payloads preserved.
	for _, ld := range []int{8, 9, 13, 32} {
		src := make([]float32, 8*ld)
		dst := make([]float32, 8*ld)
		fillRandomF32(rng, src)
		fillRandomF32(rng, dst)
		want := append([]float32(nil), dst...)
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				want[j*ld+i] = src[i*ld+j]
			}
		}
		transpose8x8F32(&dst[0], ld, &src[0], ld)
		for i := range dst {
			if math.Float32bits(dst[i]) != math.Float32bits(want[i]) {
				t.Fatalf("transpose8x8F32 ld=%d i=%d: got 0x%08x want 0x%08x",
					ld, i, math.Float32bits(dst[i]), math.Float32bits(want[i]))
			}
		}
	}
}

// TestSIMDWrappersNilAndEmpty: nil/empty slices must not panic anywhere.
func TestSIMDWrappersNilAndEmpty(t *testing.T) {
	fmaddScalarInto(nil, nil, 1.5)
	fmaddScalarInto([]float32{}, []float32{}, 1.5)
	fmadd3Into(nil, nil, 1, 2, 3)
	fmadd3Into([]float32{1}, []float32{1}, 1, 2, 3) // len(x)-2 < 0 clamps
	geluErfInto(nil, nil)
	geluErfInto([]float32{}, []float32{})
	transposePlanesRange(nil, nil, 0, 0, 0, 0)
}
