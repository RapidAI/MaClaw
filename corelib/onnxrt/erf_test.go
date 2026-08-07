package onnxrt

import (
	"math"
	"testing"
)

func TestErf32Accuracy(t *testing.T) {
	n := 1 << 20
	src := make([]float32, n)
	for i := range src {
		src[i] = float32(i)/float32(n)*24 - 12
	}
	dst := make([]float32, n)
	erf32Into(dst, src)
	bad := 0
	maxDiff := float64(0)
	for i := range src {
		want := float32(math.Erf(float64(src[i])))
		d := math.Abs(float64(dst[i] - want))
		if d > maxDiff {
			maxDiff = d
		}
		if math.IsNaN(float64(dst[i])) || math.IsInf(float64(dst[i]), 0) {
			if bad < 5 {
				t.Logf("bad at %d: src=%v got=%v", i, src[i], dst[i])
			}
			bad++
		}
	}
	t.Logf("bad=%d maxDiff=%g", bad, maxDiff)
	if bad > 0 || maxDiff > 1e-5 {
		t.Fail()
	}
}
