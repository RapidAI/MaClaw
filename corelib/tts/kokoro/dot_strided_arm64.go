//go:build arm64

package kokoro

// dotStridedNEON computes sum over t in [0,taps) of
// dot(x[t*xStride : t*xStride+n], w[t*wStride : t*wStride+n]) with fused
// multi-tap accumulation (see dot_strided_amd64.s for the reference
// semantics). Strides are in float32 elements and may be negative.
//
//go:noescape
func dotStridedNEON(x []float32, xStride int, w []float32, wStride int, taps, n int) float32

// dotStridedNEON2 is dotStridedNEON evaluating two outputs at once: output 0
// starts at x[0], output 1 starts at x[n] (the next stride-1 output
// position). Weight chunks are loaded once and shared by both FMA chains.
//
//go:noescape
func dotStridedNEON2(x []float32, xStride int, w []float32, wStride int, taps, n int) (r0, r1 float32)

func useKokoroStridedFMA() bool { return useKokoroFusedConvASM() }

func dotStridedFMA(x []float32, xStride int, w []float32, wStride int, taps, n int) float32 {
	if useKokoroStridedFMA() {
		return dotStridedNEON(x, xStride, w, wStride, taps, n)
	}
	sum := float32(0)
	for t := 0; t < taps; t++ {
		sum += dot32(x[t*xStride:t*xStride+n], w[t*wStride:t*wStride+n])
	}
	return sum
}

func dotStridedFMA2(x []float32, xStride int, w []float32, wStride int, taps, n int) (r0, r1 float32) {
	if useKokoroStridedFMA() {
		return dotStridedNEON2(x, xStride, w, wStride, taps, n)
	}
	for t := 0; t < taps; t++ {
		xo := t * xStride
		wo := t * wStride
		for i := 0; i < n; i++ {
			p := x[xo+i] * w[wo+i]
			r0 += p
			r1 += x[xo+n+i] * w[wo+i]
		}
	}
	return r0, r1
}

func leakyReLUInplace(x []float32, slope float32) {
	for i, v := range x {
		x[i] = leakyReLU(v, slope)
	}
}

func normScaleInto(dst, x []float32, mean, scale, beta float32) {
	for i, v := range x {
		dst[i] = (v-mean)*scale + beta
	}
}

func axpyInplace(dst, x []float32, a float32) {
	for i, v := range x {
		dst[i] += a * v
	}
}
