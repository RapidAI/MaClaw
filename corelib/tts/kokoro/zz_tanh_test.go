package kokoro

import (
	"math"
	"testing"
)

func TestTanhInplace32Dbg(t *testing.T) {
	x := make([]float32, 1000)
	for i := range x {
		x[i] = float32(i%97)*0.1 - 4.8
	}
	want := make([]float32, 1000)
	for i := range x {
		want[i] = float32(math.Tanh(float64(x[i])))
	}
	tanhInplace32(x)
	maxd := float32(0)
	for i := range x {
		d := x[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
	}
	t.Logf("maxdiff=%v x[500]=%v want[500]=%v", maxd, x[500], want[500])
	if maxd > 1e-4 {
		t.Fatal("vector tanh mismatch")
	}
}
