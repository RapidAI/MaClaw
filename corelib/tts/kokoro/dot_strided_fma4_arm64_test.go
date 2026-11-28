//go:build arm64

package kokoro

import "testing"

func nearBlocked(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	m := a
	if m < 0 {
		m = -m
	}
	ab := b
	if ab < 0 {
		ab = -ab
	}
	if ab > m {
		m = ab
	}
	return d <= 2e-4+2e-3*m
}

func TestBlockedFMA4NEONMatchesScalar(t *testing.T) {
	for _, n := range []int{1, 3, 4, 5, 8, 15, 16, 17, 32, 128, 256} {
		for _, taps := range []int{1, 2, 3, 7, 11} {
			for _, dil := range []int{1, 2, 3} {
				xStride := dil * n
				x := make([]float32, taps*xStride+2*n+8)
				w := make([]float32, 4*taps*n+8)
				for i := range x {
					x[i] = float32((i%17)-8) * 0.011
				}
				for i := range w {
					w[i] = float32((i%13)-6) * 0.017
				}
				wOc := taps * n
				sr0, sr1, sr2, sr3, sr4, sr5, sr6, sr7 := dotStridedFMA4x2Scalar(x, xStride, w, n, wOc, taps, n)
				nr0, nr1, nr2, nr3, nr4, nr5, nr6, nr7 := dotStridedFMA4x2NEON(x, xStride, w, n, wOc, taps, n)
				if !nearBlocked(sr0, nr0) || !nearBlocked(sr1, nr1) || !nearBlocked(sr2, nr2) || !nearBlocked(sr3, nr3) ||
					!nearBlocked(sr4, nr4) || !nearBlocked(sr5, nr5) || !nearBlocked(sr6, nr6) || !nearBlocked(sr7, nr7) {
					t.Fatalf("4x2 n=%d taps=%d dil=%d scalar=(%v %v %v %v %v %v %v %v) neon=(%v %v %v %v %v %v %v %v)",
						n, taps, dil, sr0, sr1, sr2, sr3, sr4, sr5, sr6, sr7, nr0, nr1, nr2, nr3, nr4, nr5, nr6, nr7)
				}
				qr0, qr1, qr2, qr3 := dotStridedFMA4Scalar(x, xStride, w, n, wOc, taps, n)
				pr0, pr1, pr2, pr3 := dotStridedFMA4NEON(x, xStride, w, n, wOc, taps, n)
				if !nearBlocked(qr0, pr0) || !nearBlocked(qr1, pr1) || !nearBlocked(qr2, pr2) || !nearBlocked(qr3, pr3) {
					t.Fatalf("4 n=%d taps=%d dil=%d scalar=(%v %v %v %v) neon=(%v %v %v %v)",
						n, taps, dil, qr0, qr1, qr2, qr3, pr0, pr1, pr2, pr3)
				}
			}
		}
	}
	const n = 32
	const taps = 5
	const gap = 96
	w := make([]float32, (taps-1)*gap+4*taps*n)
	x := make([]float32, taps*n*2+n)
	for i := range w {
		w[i] = float32((i%9)-4) * 0.013
	}
	for i := range x {
		x[i] = float32((i%11)-5) * 0.019
	}
	wBase := (taps - 1) * gap
	sr0, sr1, sr2, sr3, sr4, sr5, sr6, sr7 := dotStridedFMA4x2Scalar(x, n, w[wBase:], -gap, n, taps, n)
	nr0, nr1, nr2, nr3, nr4, nr5, nr6, nr7 := dotStridedFMA4x2NEON(x, n, w[wBase:], -gap, n, taps, n)
	if !nearBlocked(sr0, nr0) || !nearBlocked(sr1, nr1) || !nearBlocked(sr2, nr2) || !nearBlocked(sr3, nr3) ||
		!nearBlocked(sr4, nr4) || !nearBlocked(sr5, nr5) || !nearBlocked(sr6, nr6) || !nearBlocked(sr7, nr7) {
		t.Fatalf("neg 4x2 scalar=(%v %v %v %v %v %v %v %v) neon=(%v %v %v %v %v %v %v %v)",
			sr0, sr1, sr2, sr3, sr4, sr5, sr6, sr7, nr0, nr1, nr2, nr3, nr4, nr5, nr6, nr7)
	}
	qr0, qr1, qr2, qr3 := dotStridedFMA4Scalar(x, n, w[wBase:], -gap, n, taps, n)
	pr0, pr1, pr2, pr3 := dotStridedFMA4NEON(x, n, w[wBase:], -gap, n, taps, n)
	if !nearBlocked(qr0, pr0) || !nearBlocked(qr1, pr1) || !nearBlocked(qr2, pr2) || !nearBlocked(qr3, pr3) {
		t.Fatalf("neg 4 scalar=(%v %v %v %v) neon=(%v %v %v %v)", qr0, qr1, qr2, qr3, pr0, pr1, pr2, pr3)
	}
	z0, z1, z2, z3, z4, z5, z6, z7 := dotStridedFMA4x2NEON(x, n, w, n, n, 0, n)
	if z0 != 0 || z1 != 0 || z2 != 0 || z3 != 0 || z4 != 0 || z5 != 0 || z6 != 0 || z7 != 0 {
		t.Fatal("zero taps")
	}
}
