//go:build amd64

package kokoro

import (
	"testing"
)

func refDotTaps(x []float32, xStride int, w []float32, wStride, taps, n int, xBase, wBase int) float32 {
	sum := float32(0)
	for t := 0; t < taps; t++ {
		xs := xBase + t*xStride
		ws := wBase + t*wStride
		for i := 0; i < n; i++ {
			sum += x[xs+i] * w[ws+i]
		}
	}
	return sum
}

func TestDotStridedFMA(t *testing.T) {
	if !useKokoroStridedFMA() {
		t.Skip("strided FMA kernels unavailable on this CPU")
	}
	for _, n := range []int{1, 7, 8, 16, 31, 32, 33, 64, 130} {
		for _, taps := range []int{1, 2, 3, 7, 11} {
			xStride := n * 2 // dilated layout
			wStride := n
			x := make([]float32, taps*xStride+n)
			w := make([]float32, taps*wStride+n)
			for i := range x {
				x[i] = float32((i%13)-6) * 0.021
			}
			for i := range w {
				w[i] = float32((i%7)-3) * 0.017
			}
			want := refDotTaps(x, xStride, w, wStride, taps, n, 0, 0)
			got := dotStridedFMA(x, xStride, w, wStride, taps, n)
			if !closeEnough(got, want) {
				t.Fatalf("n=%d taps=%d: got=%v want=%v", n, taps, got, want)
			}
		}
	}
}

func TestDotStridedFMANegativeStride(t *testing.T) {
	if !useKokoroStridedFMA() {
		t.Skip("strided FMA kernels unavailable on this CPU")
	}
	const n = 40
	const taps = 5
	wStride := -96 // elements, negative like the conv-transpose path
	w := make([]float32, (taps-1)*96+n)
	for i := range w {
		w[i] = float32((i%9)-4) * 0.013
	}
	x := make([]float32, taps*n)
	for i := range x {
		x[i] = float32((i%11)-5) * 0.019
	}
	wBase := (taps - 1) * 96
	want := refDotTaps(x, n, w, wStride, taps, n, 0, wBase)
	got := dotStridedFMA(x, n, w[wBase:], wStride, taps, n)
	if !closeEnough(got, want) {
		t.Fatalf("negative stride: got=%v want=%v", got, want)
	}
}

func TestDotStridedFMA2(t *testing.T) {
	if !useKokoroStridedFMA() {
		t.Skip("strided FMA kernels unavailable on this CPU")
	}
	for _, n := range []int{1, 5, 8, 16, 32, 37, 128} {
		for _, taps := range []int{1, 3, 11} {
			xStride := 3 * n
			x := make([]float32, taps*xStride+2*n)
			w := make([]float32, taps*n)
			for i := range x {
				x[i] = float32((i%17)-8) * 0.011
			}
			for i := range w {
				w[i] = float32((i%5)-2) * 0.023
			}
			want0 := refDotTaps(x, xStride, w, n, taps, n, 0, 0)
			want1 := refDotTaps(x, xStride, w, n, taps, n, n, 0)
			got0, got1 := dotStridedFMA2(x, xStride, w, n, taps, n)
			if !closeEnough(got0, want0) || !closeEnough(got1, want1) {
				t.Fatalf("n=%d taps=%d: got=(%v,%v) want=(%v,%v)", n, taps, got0, got1, want0, want1)
			}
		}
	}
}

func TestLeakyReLUInplace(t *testing.T) {
	x := make([]float32, 67)
	for i := range x {
		x[i] = float32(i-33) * 0.25
	}
	want := make([]float32, len(x))
	for i, v := range x {
		want[i] = leakyReLU(v, 0.1)
	}
	leakyReLUInplace(x, 0.1)
	requireSliceClose(t, x, want)
}

func TestNormScaleInto(t *testing.T) {
	x := make([]float32, 41)
	for i := range x {
		x[i] = float32((i%19)-9) * 0.05
	}
	dst := make([]float32, len(x))
	normScaleInto(dst, x, 0.3, 1.7, -0.2)
	for i, v := range x {
		want := (v-0.3)*1.7 - 0.2
		if !closeEnough(dst[i], want) {
			t.Fatalf("at %d got=%v want=%v", i, dst[i], want)
		}
	}
}

func TestConvTranspose1DStridedFMA(t *testing.T) {
	if !useKokoroStridedFMA() {
		t.Skip("strided FMA kernels unavailable on this CPU")
	}
	const inC = 16
	const inT = 9
	const outC = 4
	const kernel = 6
	const stride = 4
	const padding = 1
	outT := (inT-1)*stride - 2*padding + kernel
	x := make([]float32, inC*inT)
	for i := range x {
		x[i] = float32((i%23)-11) * 0.009
	}
	w := make([]float32, inC*outC*kernel)
	for i := range w {
		w[i] = float32((i%7)-3) * 0.021
	}
	b := []float32{0.1, -0.05, 0.02, 0.3}
	wT := make([]float32, kernel*outC*inC)
	transposeConvTranspose1DWeight(wT, w, inC, outC, kernel)
	got := make([]float32, outC*outT)
	if err := convTranspose1DSIMDTransposedWeight(got, x, wT, b, inC, inT, outC, kernel, stride, padding, outT); err != nil {
		t.Fatal(err)
	}
	want := make([]float32, outC*outT)
	if err := ConvTranspose1D(want, x, w, b, inC, inT, outC, kernel, stride, padding, 0, 1); err != nil {
		t.Fatal(err)
	}
	requireSliceClose(t, got, want)
}

func TestAxpyInplace(t *testing.T) {
	dst := make([]float32, 53)
	x := make([]float32, 53)
	for i := range dst {
		dst[i] = float32(i) * 0.1
		x[i] = float32((i%9)-4) * 0.03
	}
	want := make([]float32, len(dst))
	for i := range dst {
		want[i] = dst[i] + -1.7*x[i]
	}
	axpyInplace(dst, x, -1.7)
	requireSliceClose(t, dst, want)
}

func refConv4x2G(x []float32, xStride int, w []float32, wStride, wOcStride, taps, n, xPosStride int, bias, res []float32, resStride, oc, p int) float32 {
	sum := float32(0)
	if bias != nil {
		sum = bias[oc]
	}
	for k := 0; k < taps; k++ {
		xo := k*xStride + p*xPosStride
		wo := oc*wOcStride + k*wStride
		for i := 0; i < n; i++ {
			sum += x[xo+i] * w[wo+i]
		}
	}
	if res != nil {
		sum += res[oc*resStride+p]
	}
	return sum
}

func TestConv4x2GConv1DLike(t *testing.T) {
	// conv1d-like: 4 oc, 2 adjacent positions, xStride=2n (dilation 2),
	// bias+residual, out position stride 1.
	const n = 40
	const taps = 5
	const pairs = 60
	const outT = 256
	xStride := 2 * n
	rows := pairs*2*2 + taps*2 + 4
	x := make([]float32, rows*n)
	w := make([]float32, 4*taps*n)
	bias := []float32{0.1, -0.2, 0.3, 0.05}
	res := make([]float32, 4*outT)
	out := make([]float32, 4*outT)
	for i := range x {
		x[i] = float32((i%29)-14) * 0.006
	}
	for i := range w {
		w[i] = float32((i%13)-6) * 0.011
	}
	for i := range res {
		res[i] = float32((i%7)-3) * 0.003
	}
	for ot := 0; ot+1 < pairs*2; ot += 2 {
		conv4x2G(x[ot*xStride:], xStride, w, n, taps*n, taps, n, n,
			out[ot:], outT, 1, bias, res[ot:])
	}
	for oc := 0; oc < 4; oc++ {
		for p := 0; p < 2; p++ {
			for ot := 0; ot+1 < pairs*2; ot += 2 {
				got := out[oc*outT+ot+p]
				want := refConv4x2G(x[ot*xStride:], xStride, w, n, taps*n, taps, n, n, bias, res[ot:], outT, oc, p)
				if !closeEnough(got, want) {
					t.Fatalf("oc=%d p=%d ot=%d got=%v want=%v", oc, p, ot, got, want)
				}
			}
		}
	}
}

func TestConv4x2GTransposeLike(t *testing.T) {
	// convtranspose-like: tap weight stride negative, paired positions
	// `stride` apart (same residue class), out position stride = stride.
	const n = 16
	const stride = 4
	const outC = 4
	const kernel = 9
	const inT = 30
	const outT = 100
	H := (kernel + stride) / stride
	inputT := make([]float32, (inT+2*H)*n)
	for i := range inputT {
		inputT[i] = float32((i%17)-8) * 0.007
	}
	weightT := make([]float32, kernel*outC*n)
	for i := range weightT {
		weightT[i] = float32((i%11)-5) * 0.009
	}
	bias := []float32{0.2, -0.1, 0.4, 0.0}
	out := make([]float32, outC*outT)
	ktab := make([]int, stride)
	for k0 := 0; k0 < stride; k0++ {
		ktab[k0] = (kernel - 1 - k0) / stride + 1
	}
	pairStride := 2 * stride
	for ot := 0; ot+stride < outT; ot += pairStride {
		base := ot
		k0 := base % stride
		K := ktab[k0]
		it0 := (base - k0) / stride
		kMax := k0 + (K-1)*stride
		xBase := inputT[(it0-(K-1)+H)*n:]
		wBase := weightT[(kMax*outC+0)*n:]
		conv4x2G(xBase, n, wBase, -stride*outC*n, n, K, n, n,
			out[ot:], outT, stride, bias, nil)
	}
	for ot := 0; ot+stride < outT; ot += pairStride {
		for p := 0; p < 2; p++ {
			for oc := 0; oc < outC; oc++ {
				base := ot + p*stride
				k0 := base % stride
				K := ktab[k0]
				it0 := (base - k0) / stride
				kMax := k0 + (K-1)*stride
				var want float32 = bias[oc]
				for j := 0; j < K; j++ {
					it := it0 - (K - 1) + j
					k := kMax - j*stride
					for i := 0; i < n; i++ {
						want += inputT[(it+H)*n+i] * weightT[(k*outC+oc)*n+i]
					}
				}
				got := out[oc*outT+base]
				if !closeEnough(got, want) {
					t.Fatalf("oc=%d base=%d got=%v want=%v", oc, base, got, want)
				}
			}
		}
	}
}

func TestConv1DMultiStride(t *testing.T) {
	// Covers stride>1 through the fused path, including the
	// generator noise_convs.0 shape (inC=22, k=12, stride=6).
	cases := []struct {
		inC, outC, T, k, stride, pad, dil int
	}{
		{22, 32, 600, 12, 6, 3, 1},
		{22, 32, 600, 12, 6, 3, 2},
		{64, 32, 400, 4, 2, 1, 1},
		{64, 32, 400, 7, 3, 3, 1},
		{128, 16, 300, 3, 2, 2, 2},
		{16, 8, 200, 5, 4, 2, 1},
	}
	for _, c := range cases {
		outT := (c.T+2*c.pad-c.dil*(c.k-1)-1)/c.stride + 1
		if outT < 1 {
			continue
		}
		x := make([]float32, c.inC*c.T)
		w := make([]float32, c.outC*c.inC*c.k)
		bias := make([]float32, c.outC)
		for i := range x {
			x[i] = float32((i%31)-15) * 0.01
		}
		for i := range w {
			w[i] = float32((i%17)-8) * 0.01
		}
		for i := range bias {
			bias[i] = float32((i%5)-2) * 0.05
		}
		weightT := make([]float32, c.outC*c.k*c.inC)
		transposeConv1DWeight(weightT, w, c.inC, c.outC, c.k)
		got := make([]float32, c.outC*outT)
		if err := conv1DSIMDTransposedWeight(got, x, weightT, bias, nil, c.inC, c.T, c.outC, c.k, c.stride, c.pad, c.dil, outT); err != nil {
			t.Fatal(err)
		}
		want := make([]float32, c.outC*outT)
		if err := conv1DParallel(want, x, w, bias, c.inC, c.T, c.outC, c.k, c.stride, c.pad, c.dil, 1, outT); err != nil {
			t.Fatal(err)
		}
		for i := range got {
			diff := got[i] - want[i]
			if diff < 0 {
				diff = -diff
			}
			if diff > 2e-3 {
				t.Fatalf("case %+v at %d got=%v want=%v", c, i, got[i], want[i])
			}
		}
	}
}
