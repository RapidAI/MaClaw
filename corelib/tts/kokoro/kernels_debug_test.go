//go:build arm64

package kokoro

import "testing"

func TestDotNEONDebug(t *testing.T) {
	if !useKokoroSIMD() {
		t.Skip("SIMD disabled")
	}
	for n := 1; n <= 17; n++ {
		a := make([]float32, n)
		b := make([]float32, n)
		for i := range a {
			a[i] = float32(i + 1)
			b[i] = 1
		}
		got := dotNEON(a, b)
		want := float32(n*(n+1)) / 2
		t.Logf("n=%d got=%v want=%v", n, got, want)
		if got != want {
			t.Errorf("n=%d got=%v want=%v", n, got, want)
		}
	}
}
