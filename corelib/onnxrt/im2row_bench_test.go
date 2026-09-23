package onnxrt

import "testing"

// BenchmarkIm2row3x3 measures the 3x3 sW=1 interior path (det-like shapes).
func BenchmarkIm2row3x3(b *testing.B) {
	cases := []struct{ name string; H, W, Cg int }{
		{"Cg16_64x64", 64, 64, 16},
		{"Cg4_64x64", 64, 64, 4},
		{"Cg16_128x128", 128, 128, 16},
	}
	for _, tc := range cases {
		H, W, Cg := tc.H, tc.W, tc.Cg
		p := &convParams{
			kH: 3, kW: 3,
			strides:   [2]int{1, 1},
			pads:      [4]int{1, 1},
			dilations: [2]int{1, 1},
		}
		oH, oW := H-2, W-2
		K := Cg * 9
		x := make([]float32, Cg*H*W)
		B := make([]float32, oH*oW*K)
		b.Run(tc.name, func(b *testing.B) {
			b.SetBytes(int64(oH * oW * K * 4))
			for i := 0; i < b.N; i++ {
				im2rowFast(B, x, 0, 0, oH*oW, oW, H, W, Cg, p)
			}
		})
	}
}
