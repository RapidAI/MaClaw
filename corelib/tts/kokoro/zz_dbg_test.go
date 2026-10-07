package kokoro

import "testing"

func TestDbgPairKernel(t *testing.T) {
	n := 16
	taps := 3
	xStride := 16
	x := make([]float32, 2*n+taps*xStride)
	for i := range x {
		x[i] = float32(i % 10)
	}
	w := make([]float32, 4*taps*n)
	for i := range w {
		w[i] = 1
	}
	r0, r1, r2, r3, r4, r5, r6, r7 := dotStridedFMA4x2(x, xStride, w, n, taps*n, taps, n)
	// want r0 = sum x[0..15] = 0+..+9+0+..+5 = 60+15=75? compute: 0..9=45, 0..5=15 -> 60
	// r1 = sum x[16..31] = 6+7+8+9+0+..+9+0+1 = 30+45+1=76... just print
	t.Logf("r0=%v r1=%v r2=%v r3=%v r4=%v r5=%v r6=%v r7=%v", r0, r1, r2, r3, r4, r5, r6, r7)
	// reference
	ref := func(oc, ot int) float32 {
		s := float32(0)
		for k := 0; k < taps; k++ {
			for c := 0; c < n; c++ {
				s += x[ot*n+k*xStride+c] * w[oc*taps*n+k*n+c]
			}
		}
		return s
	}
	t.Logf("want r0=%v r1=%v r2=%v", ref(0, 0), ref(0, 1), ref(1, 0))
}
