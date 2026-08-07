package onnxrt

import (
	"math"
	"testing"
)

// TestIm2rowFastMatchesGeneric fills B via the row-hoisted fast path and the
// original per-pixel walk across shapes that exercise mid-row block splits,
// edge segments, padded rows, and stride-2; both write pure copies, so the
// results must be bit-identical.
func TestIm2rowFastMatchesGeneric(t *testing.T) {
	for _, tc := range []struct {
		H, W, oH, oW, Cg, kH, kW int
		strides                  [2]int
		pads                     [4]int
		pix0, rows               int
	}{
		{8, 16, 8, 16, 3, 3, 3, [2]int{1, 1}, [4]int{1, 1, 1, 1}, 0, 128}, // full rows
		{8, 16, 8, 16, 3, 3, 3, [2]int{1, 1}, [4]int{1, 1, 1, 1}, 7, 33},  // mid-row split
		{8, 16, 8, 16, 5, 3, 3, [2]int{1, 1}, [4]int{1, 1, 1, 1}, 15, 1},  // single pixel
		{8, 16, 4, 8, 3, 3, 3, [2]int{2, 2}, [4]int{1, 1, 1, 1}, 3, 9},    // stride 2
		{8, 16, 8, 18, 2, 2, 2, [2]int{1, 1}, [4]int{1, 1, 1, 1}, 5, 20},  // 2x2 pad 1
		{8, 16, 10, 20, 2, 3, 5, [2]int{1, 1}, [4]int{2, 3, 2, 3}, 9, 40}, // kW=5 wide pad
		{8, 16, 8, 23, 2, 3, 7, [2]int{1, 1}, [4]int{1, 3, 1, 3}, 11, 25}, // kW=7 pad 3 (owA>1)
		{8, 8, 8, 12, 4, 7, 7, [2]int{1, 1}, [4]int{3, 3, 3, 3}, 13, 17},  // 7x7
		{4, 6, 4, 13, 2, 3, 3, [2]int{1, 1}, [4]int{4, 4, 0, 0}, 3, 10},   // pad > kernel reach
		{8, 16, 8, 16, 1, 1, 1, [2]int{1, 1}, [4]int{0, 0, 0, 0}, 4, 50},  // 1x1
	} {
		H, W, Cg := tc.H, tc.W, tc.Cg
		x := make([]float32, Cg*H*W)
		var state uint32 = 7 + uint32(H*W)
		for i := range x {
			state = state*1664525 + 1013904223
			x[i] = math.Float32frombits(state)
		}
		p := &convParams{
			strides:   tc.strides,
			pads:      tc.pads,
			dilations: [2]int{1, 1},
			group:     1,
			kH:        tc.kH,
			kW:        tc.kW,
		}
		K := Cg * tc.kH * tc.kW
		oH, oW := tc.oH, tc.oW
		if tc.pix0+tc.rows > oH*oW {
			t.Fatalf("tc %+v: pixel range out of bounds", tc)
		}
		fast := make([]float32, tc.rows*K)
		gen := make([]float32, tc.rows*K)
		// poison both buffers to catch unwritten elements
		for i := range fast {
			fast[i] = float32(math.NaN())
			gen[i] = float32(math.NaN())
		}
		im2rowFast(fast, x, 0, tc.pix0, tc.rows, oW, H, W, Cg, p)
		im2rowGeneric(gen, x, 0, tc.pix0, tc.rows, oW, H, W, Cg, p)
		for i := range fast {
			if math.Float32bits(fast[i]) != math.Float32bits(gen[i]) {
				pi := i / K
				t.Fatalf("tc=%+v pi=%d i=%d: fast %v (0x%08x) != generic %v (0x%08x)",
					tc, pi, i, fast[i], math.Float32bits(fast[i]), gen[i], math.Float32bits(gen[i]))
			}
		}
	}
}
