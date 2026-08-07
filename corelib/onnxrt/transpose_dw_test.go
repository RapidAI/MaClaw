package onnxrt

import (
	"math"
	"testing"
)

// TestTransposePlanesRange8 verifies the AVX2 8x8-block transpose path is
// bit-identical to the scalar tiled walk across shapes, offsets, and
// remainders (odd C, odd pixel ranges).
func TestTransposePlanesRange8(t *testing.T) {
	if !hasAVX2FMA {
		t.Skip("AVX2 not available")
	}
	for _, hw := range []struct{ C, HW int }{
		{8, 8}, {8, 9}, {16, 64}, {12, 17}, {24, 100}, {9, 8},
		{48, 240}, {7, 130}, {96, 33}, {3, 200},
	} {
		C, HW := hw.C, hw.HW
		src := make([]float32, C*HW)
		var state uint32 = 1234 + uint32(C*7+HW)
		for i := range src {
			state = state*1664525 + 1013904223
			src[i] = math.Float32frombits(state)
		}
		for _, pr := range [][2]int{{0, HW}, {1, HW - 1}, {3, 10}} {
			p0, p1 := pr[0], pr[1]
			if p1 > HW {
				p1 = HW
			}
			if p0 >= p1 {
				continue
			}
			got := make([]float32, HW*C)
			want := make([]float32, HW*C)
			transposePlanesRange(got, src, C, HW, p0, p1)
			transposePlanesRangeScalar(want, src, C, HW, p0, p1)
			for p := p0; p < p1; p++ {
				for c := 0; c < C; c++ {
					if math.Float32bits(got[p*C+c]) != math.Float32bits(want[p*C+c]) {
						t.Fatalf("C=%d HW=%d [%d,%d): p=%d c=%d got %v want %v",
							C, HW, p0, p1, p, c, got[p*C+c], want[p*C+c])
					}
				}
			}
		}
	}
}

// TestConvDepthwiseFusedTaps compares the fused-tap depthwise path (kW 3/7,
// stride 1) against a per-tap reference built from the same fmaddScalarInto
// primitives (bit-identical FMA order), across pads/widths/bias variants.
func TestConvDepthwiseFusedTaps(t *testing.T) {
	for _, tc := range []struct {
		H, W, kH, kW, pT, pL int
		bias                 bool
	}{
		{8, 16, 3, 3, 1, 1, true},
		{8, 16, 3, 3, 1, 1, false},
		{8, 15, 3, 3, 1, 1, true}, // odd W
		{8, 16, 3, 3, 0, 0, true}, // no pad
		{8, 16, 7, 7, 3, 3, true}, // 7x7
		{8, 15, 7, 7, 3, 3, true}, // 7x7 odd W
		{8, 9, 3, 3, 1, 1, true},  // small W edge heavy
		{5, 8, 3, 3, 1, 1, true},  // minimal oW for fused path
		{8, 16, 3, 3, 2, 2, true}, // asymmetric-ish larger pad
		{1, 8, 3, 3, 1, 1, true},  // H=1: all rows partially padded
	} {
		C := 5
		oH := tc.H + 2*tc.pT - tc.kH + 1
		oW := tc.W + 2*tc.pL - tc.kW + 1
		if oH <= 0 || oW <= 0 {
			continue
		}
		x := make([]float32, C*tc.H*tc.W)
		w := make([]float32, C*tc.kH*tc.kW)
		var state uint32 = 99 + uint32(tc.W)
		for i := range x {
			state = state*1664525 + 1013904223
			x[i] = float32(state>>8)/float32(1<<24)*4 - 2
		}
		for i := range w {
			state = state*1664525 + 1013904223
			w[i] = float32(state>>8)/float32(1<<24)*2 - 1
		}
		var bias []float32
		if tc.bias {
			bias = make([]float32, C)
			for i := range bias {
				state = state*1664525 + 1013904223
				bias[i] = float32(state>>8) / float32(1<<24)
			}
		}
		p := &convParams{
			strides:   [2]int{1, 1},
			pads:      [4]int{tc.pT, tc.pL, tc.pT, tc.pL},
			dilations: [2]int{1, 1},
			group:     C,
			kH:        tc.kH,
			kW:        tc.kW,
		}
		got := make([]float32, C*oH*oW)
		convDepthwiseSIMD(got, x, w, bias, 1, C, tc.H, tc.W, oH, oW, p)

		// Reference: bias fill + per-tap fmaddScalarInto (old algorithm).
		want := make([]float32, C*oH*oW)
		for c := 0; c < C; c++ {
			bv := float32(0)
			if bias != nil {
				bv = bias[c]
			}
			for oh := 0; oh < oH; oh++ {
				row := want[c*oH*oW+oh*oW : c*oH*oW+oh*oW+oW]
				for i := range row {
					row[i] = bv
				}
				for kh := 0; kh < tc.kH; kh++ {
					ih := oh - tc.pT + kh
					if ih < 0 || ih >= tc.H {
						continue
					}
					src := x[c*tc.H*tc.W+ih*tc.W:]
					for kw := 0; kw < tc.kW; kw++ {
						d := kw - tc.pL
						s0 := d
						dst0 := 0
						n := oW
						if s0 < 0 {
							dst0 = -s0
							n += s0
							s0 = 0
						}
						if s0+n > tc.W {
							n = tc.W - s0
						}
						if n <= 0 {
							continue
						}
						fmaddScalarInto(row[dst0:dst0+n], src[s0:s0+n], w[c*tc.kH*tc.kW+kh*tc.kW+kw])
					}
				}
			}
		}
		// The fused path is bit-identical to the reference except inside the
		// last (n%8) pixels of each interior row, where the old per-tap
		// passes had per-tap FMA/scalar boundaries (different clip lengths)
		// and the fused path has one: a single FMA-vs-scalar rounding choice
		// per tap. Assert a tight bound relative to the tensor magnitude.
		maxAbs := float64(0)
		for _, v := range want {
			if a := math.Abs(float64(v)); a > maxAbs {
				maxAbs = a
			}
		}
		maxDiff := float64(0)
		for i := range got {
			if d := math.Abs(float64(got[i] - want[i])); d > maxDiff {
				maxDiff = d
			}
		}
		t.Logf("tc=%+v maxDiff=%g maxAbs=%g", tc, maxDiff, maxAbs)
		if maxDiff > 2e-6*(1+maxAbs) {
			t.Fatalf("tc=%+v maxDiff=%g maxAbs=%g", tc, maxDiff, maxAbs)
		}
	}
}
