package onnxrt

import (
	"math"
	"testing"
)

func gridSampleRef(x []float32, N, C, H, W int, grid []float32, oH, oW int, mode, padding string, align bool) []float32 {
	out := make([]float32, N*C*oH*oW)
	plane := H * W
	for b := 0; b < N; b++ {
		for oy := 0; oy < oH; oy++ {
			for ox := 0; ox < oW; ox++ {
				g := ((b*oH+oy)*oW + ox) * 2
				ix := gridCoord(float64(grid[g]), W, align)
				iy := gridCoord(float64(grid[g+1]), H, align)
				for c := 0; c < C; c++ {
					base := (b*C + c) * plane
					var v float32
					if mode == "nearest" {
						v = gridSampleAt(x[base:base+plane], W, H, int(math.Round(iy)), int(math.Round(ix)), padding)
					} else {
						v = gridBilinear(x[base:base+plane], W, H, ix, iy, padding)
					}
					out[((b*C+c)*oH+oy)*oW+ox] = v
				}
			}
		}
	}
	return out
}

func fillPlane(n, c, h, w int) []float32 {
	v := make([]float32, n*c*h*w)
	for i := range v {
		v[i] = float32(i%17) - 8
	}
	return v
}

func TestGridSampleMatchesScalar(t *testing.T) {
	n := &Node{Attrs: map[string]Attr{
		"align_corners": {I: 0},
	}}
	cases := []struct {
		name               string
		N, C, H, W, oH, oW int
		mode, pad          string
		align              bool
		grid               func(oH, oW int) []float32
	}{
		{"bilinear-sparse", 1, 8, 5, 6, 3, 2, "bilinear", "zeros", false, func(oH, oW int) []float32 {
			return []float32{0, 0, -1, -1, 0.5, -0.25, 1, 1, -0.2, 0.3, 0.9, -0.9}
		}},
		{"bilinear-dense", 1, 8, 4, 4, 4, 4, "bilinear", "zeros", false, func(oH, oW int) []float32 {
			g := make([]float32, oH*oW*2)
			for y := 0; y < oH; y++ {
				for x := 0; x < oW; x++ {
					i := (y*oW + x) * 2
					g[i] = float32(x)/float32(oW-1)*2 - 1
					g[i+1] = float32(y)/float32(oH-1)*2 - 1
				}
			}
			return g
		}},
		{"nearest-border", 1, 5, 3, 3, 2, 2, "nearest", "border", false, func(oH, oW int) []float32 {
			return []float32{-1.2, 0.2, 1.4, -0.7, 0, 0, 0.3, 1.1}
		}},
		{"bilinear-reflect", 1, 4, 3, 5, 2, 3, "bilinear", "reflection", true, func(oH, oW int) []float32 {
			return []float32{-1.5, 0.25, 0, 1.2, 0.7, -0.4, 1, -1, 0.1, 0.1, -0.8, 0.9}
		}},
		{"batch", 2, 3, 2, 2, 1, 2, "bilinear", "zeros", false, func(oH, oW int) []float32 {
			return []float32{0, 0, -1, -1, 0.5, 0.5, 1, -1}
		}},
		// PP-DocLayoutV3's largest decoder level is N=8,C=32,100×100,300×4.
		// This is the same direct (not packed) parallel shape, scaled down
		// so the scalar reference stays cheap: C*samples >= 8192 and
		// samples*4 < H*W.
		{"bilinear-direct-parallel", 2, 32, 40, 40, 64, 4, "bilinear", "zeros", false, func(oH, oW int) []float32 {
			g := make([]float32, 2*oH*oW*2)
			for n := 0; n < 2; n++ {
				for y := 0; y < oH; y++ {
					for x := 0; x < oW; x++ {
						i := ((n*oH+y)*oW + x) * 2
						g[i] = float32(x)/float32(oW)*1.6 - 0.8
						g[i+1] = float32(y)/float32(oH)*2.2 - 1.1
					}
				}
			}
			return g
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := fillPlane(tc.N, tc.C, tc.H, tc.W)
			grid := tc.grid(tc.oH, tc.oW)
			nd := &Node{Attrs: map[string]Attr{
				"mode":          {S: []byte(tc.mode)},
				"padding_mode":  {S: []byte(tc.pad)},
				"align_corners": {I: 0},
			}}
			if tc.align {
				nd.Attrs["align_corners"] = Attr{I: 1}
			}
			got, err := opGridSample(nil, nd, []*Tensor{
				FloatFrom(x, tc.N, tc.C, tc.H, tc.W),
				FloatFrom(grid, tc.N, tc.oH, tc.oW, 2),
			})
			if err != nil {
				t.Fatal(err)
			}
			want := gridSampleRef(x, tc.N, tc.C, tc.H, tc.W, grid, tc.oH, tc.oW, tc.mode, tc.pad, tc.align)
			if len(got[0].F32) != len(want) {
				t.Fatalf("len %d want %d", len(got[0].F32), len(want))
			}
			for i := range want {
				if got[0].F32[i] != want[i] {
					t.Fatalf("out[%d]=%v want %v", i, got[0].F32[i], want[i])
				}
			}
		})
	}
	_ = n
}

func TestGridSamplePackedMatchesScalar(t *testing.T) {
	// Sparse relative to the map, so gridSampleRun stays on the direct path.
	// The packed kernel must still match, including zeros outside the map.
	const N, C, H, W, oH, oW = 1, 16, 8, 8, 3, 4
	x := fillPlane(N, C, H, W)
	g := layoutGrid(N, oH, oW)
	out := make([]float32, N*C*oH*oW)
	gridSamplePacked(out, x, g, C, H, W, oH, oW, false)
	want := gridSampleRef(x, N, C, H, W, g, oH, oW, "bilinear", "zeros", false)
	for i := range want {
		if out[i] != want[i] {
			t.Fatalf("packed [%d]=%v want %v", i, out[i], want[i])
		}
	}
}

func TestBilinear4Tail(t *testing.T) {
	n := 17
	a := make([]float32, n)
	b := make([]float32, n)
	c := make([]float32, n)
	d := make([]float32, n)
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		a[i] = float32(i) - 3
		b[i] = float32(i) * 0.5
		c[i] = 1
		d[i] = float32(-i)
	}
	wx, wy := float32(0.3), float32(0.75)
	bilinear4Into(out, a, b, c, d, wx, wy)
	omx, omy := float32(1)-wx, float32(1)-wy
	for i := 0; i < n; i++ {
		want := a[i]*omx*omy + b[i]*wx*omy + c[i]*omx*wy + d[i]*wx*wy
		if out[i] != want {
			t.Fatalf("i=%d got %v want %v", i, out[i], want)
		}
	}
}

func TestAffineIntoExact(t *testing.T) {
	src := make([]float32, 31)
	for i := range src {
		src[i] = float32(i) - 10
	}
	dst := make([]float32, 31)
	affineInto(dst, src, 1.5, -0.25, false)
	for i, x := range src {
		want := x*1.5 + -0.25
		if dst[i] != want {
			t.Fatalf("i=%d got %v want %v", i, dst[i], want)
		}
	}
	affineInto(dst, src, 0.5, -4, true)
	for i, x := range src {
		want := x*0.5 + -4
		if want < 0 {
			want = 0
		}
		if dst[i] != want {
			t.Fatalf("relu i=%d got %v want %v", i, dst[i], want)
		}
	}
}

// layoutGrid spreads samples across the map and a little past the border,
// the way a deformable decoder's 300×4 grid does.
func layoutGrid(n, oH, oW int) []float32 {
	g := make([]float32, n*oH*oW*2)
	for i := 0; i < n*oH*oW; i++ {
		g[i*2] = float32((i*17)%1000)/500 - 1.1
		g[i*2+1] = float32((i*13)%1000)/500 - 0.9
	}
	return g
}

func BenchmarkGridSampleLevel0(b *testing.B) {
	const N, C, H, W, oH, oW = 8, 32, 100, 100, 300, 4
	x := fillPlane(N, C, H, W)
	g := layoutGrid(N, oH, oW)
	out := make([]float32, N*C*oH*oW)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gridSampleRun(out, x, g, N, C, H, W, oH, oW, "bilinear", "zeros", false)
	}
}

func BenchmarkGridSampleLevel0Ref(b *testing.B) {
	const N, C, H, W, oH, oW = 8, 32, 100, 100, 300, 4
	x := fillPlane(N, C, H, W)
	g := layoutGrid(N, oH, oW)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gridSampleRef(x, N, C, H, W, g, oH, oW, "bilinear", "zeros", false)
	}
}
