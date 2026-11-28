package kokoro

import (
	"math"
	"testing"
)

func TestAlbertGEMMMatchesDots(t *testing.T) {
	savedGEMM := kokoroAlbertGEMM
	savedPool := kokoroBufferPoolEnabled
	t.Cleanup(func() {
		kokoroAlbertGEMM = savedGEMM
		kokoroBufferPoolEnabled = savedPool
	})
	shapes := []struct{ seq, hidden, heads int }{
		{1, 16, 2},
		{7, 32, 4},
		{24, 32, 4},
		{128, 128, 2},
	}
	for _, sh := range shapes {
		q := make([]float32, sh.seq*sh.hidden)
		k := make([]float32, sh.seq*sh.hidden)
		v := make([]float32, sh.seq*sh.hidden)
		fillUnit(q, 1)
		fillUnit(k, 2)
		fillUnit(v, 3)
		kokoroAlbertGEMM = false
		kokoroBufferPoolEnabled = false
		dot := make([]float32, sh.seq*sh.hidden)
		albertAttend(q, k, v, dot, sh.seq, sh.hidden, sh.heads)

		kokoroAlbertGEMM = true
		kokoroBufferPoolEnabled = true
		poison := getFloat32Buffer(sh.seq * sh.seq)
		for i := range poison {
			poison[i] = 42
		}
		putFloat32Buffer(poison)
		gemm := make([]float32, sh.seq*sh.hidden)
		albertAttend(q, k, v, gemm, sh.seq, sh.hidden, sh.heads)
		again := make([]float32, sh.seq*sh.hidden)
		albertAttend(q, k, v, again, sh.seq, sh.hidden, sh.heads)
		for i := range gemm {
			if again[i] != gemm[i] {
				t.Fatalf("seq=%d dirty pool drifted at %d", sh.seq, i)
			}
		}
		if sh.seq < 2 {
			for i := range dot {
				if gemm[i] != dot[i] {
					t.Fatalf("seq=1 GEMM flag changed dot path at %d: %g vs %g", i, gemm[i], dot[i])
				}
			}
			continue
		}
		if !attnClose(dot, gemm) {
			t.Fatalf("seq=%d hidden=%d heads=%d GEMM left the dot path", sh.seq, sh.hidden, sh.heads)
		}
	}
}

func TestAdaINSnakePoolMatchesAlloc(t *testing.T) {
	saved := kokoroBufferPoolEnabled
	t.Cleanup(func() { kokoroBufferPoolEnabled = saved })
	const channels, frames, styleN = 64, 80, 16
	x := make([]float32, channels*frames)
	style := make([]float32, styleN)
	fcW := make([]float32, styleN*channels*2)
	fcB := make([]float32, channels*2)
	alpha := make([]float32, channels)
	fillUnit(x, 4)
	fillUnit(style, 5)
	fillUnit(fcW, 6)
	fillUnit(fcB, 7)
	for i := range alpha {
		alpha[i] = 0.5 + float32(i%5)*0.1
	}
	kokoroBufferPoolEnabled = false
	alloc := make([]float32, len(x))
	if err := adaINSnake1D(alloc, x, channels, frames, style, fcW, fcB, alpha); err != nil {
		t.Fatal(err)
	}
	kokoroBufferPoolEnabled = true
	poison := getFloat32Buffer(channels * frames)
	for i := range poison {
		poison[i] = -7
	}
	putFloat32Buffer(poison)
	pooled := make([]float32, len(x))
	if err := adaINSnake1D(pooled, x, channels, frames, style, fcW, fcB, alpha); err != nil {
		t.Fatal(err)
	}
	again := make([]float32, len(x))
	if err := adaINSnake1D(again, x, channels, frames, style, fcW, fcB, alpha); err != nil {
		t.Fatal(err)
	}
	for i := range alloc {
		if pooled[i] != alloc[i] || again[i] != alloc[i] {
			t.Fatalf("pool drift at %d: alloc=%g pooled=%g again=%g", i, alloc[i], pooled[i], again[i])
		}
	}
}

func fillUnit(dst []float32, seed uint32) {
	for i := range dst {
		seed = seed*1664525 + 1013904223
		dst[i] = float32(int32(seed>>8)%2001-1000) / 500
	}
}

func attnClose(want, got []float32) bool {
	if len(want) != len(got) || len(want) == 0 {
		return false
	}
	maxd := float32(0)
	acc := float32(0)
	for i := range want {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > maxd {
			maxd = d
		}
		acc += want[i] * want[i]
	}
	rms := float32(math.Sqrt(float64(acc / float32(len(want)))))
	return !(maxd > 5e-3 && maxd > 0.05*rms)
}
