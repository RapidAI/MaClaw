//go:build amd64

package kokoro

// fmaMask8[k] holds eight float32 lanes: the first 8-k are +0.0 and the last
// k are +1.0. The assembly kernels multiply an overlapped final 8-wide chunk
// by masks[n&7] so partial tails can be accumulated without scalar code.
// Referenced from dot_strided_amd64.s.
var fmaMask8 = [8][8]float32{
	{0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 1},
	{0, 0, 0, 0, 0, 0, 1, 1},
	{0, 0, 0, 0, 0, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1},
	{0, 0, 0, 1, 1, 1, 1, 1},
	{0, 0, 1, 1, 1, 1, 1, 1},
	{0, 1, 1, 1, 1, 1, 1, 1},
}

//go:noescape
func dotStridedFMA(x []float32, xStride int, w []float32, wStride int, taps, n int) float32

//go:noescape
func dotStridedFMA2(x []float32, xStride int, w []float32, wStride int, taps, n int) (r0, r1 float32)

// dotStridedFMA4x2 computes four output channels at two adjacent output
// positions; x rows are shared across channels, weight rows across positions.
// Requires n >= 8.
//
//go:noescape
func dotStridedFMA4x2(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3, r4, r5, r6, r7 float32)

// dotStridedFMA4 computes one output position for four output channels,
// sharing each x row across all four weight rows. Requires n >= 8.
//
//go:noescape
func dotStridedFMA4(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3 float32)

// transpose8x8F32AVX2 transposes one 8x8 float32 block between strided
// buffers. Strides are in float32 elements.
//
//go:noescape
func transpose8x8F32AVX2(src []float32, srcStride int, dst []float32, dstStride int)

//go:noescape
func leakyReLUInplace32AVX2(x []float32, slope float32)

//go:noescape
func normScaleInto32AVX2(dst, x []float32, mean, scale, beta float32)

// normScaleScaledInto32AVX2 writes dst[i] = (x[i]-mean)*scale + beta and
// scaled[i] = a*dst[i] in a single pass. Requires AVX2+FMA.
//
//go:noescape
func normScaleScaledInto32AVX2(dst, scaled, x []float32, mean, scale, beta, a float32)

// squareInplace32AVX2 squares each element of x in place. Requires AVX2.
//
//go:noescape
func squareInplace32AVX2(x []float32)

//go:noescape
func axpyInplace32AVX2(dst, x []float32, a float32)

func normScaleScaledInto(dst, scaled, x []float32, mean, scale, beta, a float32) {
	if useKokoroStridedFMA() {
		normScaleScaledInto32AVX2(dst, scaled, x, mean, scale, beta, a)
		return
	}
	for i, v := range x {
		d := (v-mean)*scale + beta
		dst[i] = d
		scaled[i] = a * d
	}
}

func squareInplace(x []float32) {
	if useKokoroStridedFMA() {
		squareInplace32AVX2(x)
		return
	}
	for i, v := range x {
		x[i] = v * v
	}
}

// useKokoroStridedFMA reports whether the fused multi-tap FMA kernels can run
// on this CPU. It shares the fused-conv FMA kill switch so deployments can
// fall back to the per-tap vek path without a separate knob.
func useKokoroStridedFMA() bool { return useKokoroFusedConvFMA() }

func leakyReLUInplace(x []float32, slope float32) {
	if useKokoroSIMD() {
		leakyReLUInplace32AVX2(x, slope)
		return
	}
	for i, v := range x {
		x[i] = leakyReLU(v, slope)
	}
}

func normScaleInto(dst, x []float32, mean, scale, beta float32) {
	if useKokoroStridedFMA() {
		normScaleInto32AVX2(dst, x, mean, scale, beta)
		return
	}
	for i, v := range x {
		dst[i] = (v-mean)*scale + beta
	}
}

func axpyInplace(dst, x []float32, a float32) {
	if useKokoroStridedFMA() {
		axpyInplace32AVX2(dst, x, a)
		return
	}
	for i, v := range x {
		dst[i] += a * v
	}
}

//go:noescape
func dotStridedConv4x2G(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int, xPosStride int, out []float32, outOcStride int, outPosStride int, bias []float32, res []float32)

var zeroBias4 = [4]float32{}

// conv4x2G evaluates two adjacent output positions for four output channels
// and stores bias(+residual)+dot results directly into out. bias may be nil
// (treated as zeros); res may be nil or empty (skipped).
func conv4x2G(x []float32, xStride int, w []float32, wStride, wOcStride int, taps, n, xPosStride int, out []float32, outOcStride, outPosStride int, bias []float32, res []float32) {
	if bias == nil {
		bias = zeroBias4[:]
	}
	dotStridedConv4x2G(x, xStride, w, wStride, wOcStride, taps, n, xPosStride, out, outOcStride, outPosStride, bias, res)
}
