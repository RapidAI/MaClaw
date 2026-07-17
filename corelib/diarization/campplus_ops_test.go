package diarization

import (
	"math"
	"math/rand"
	"testing"
)

func TestConv1x1SIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	for _, tc := range []struct{ in, out, frames int }{{1, 32, 11}, {128, 32, 67}, {512, 256, 3}} {
		x := make([]float32, tc.in*tc.frames)
		w := make([]float32, tc.out*tc.in)
		for i := range x {
			x[i] = rng.Float32()*2 - 1
		}
		for i := range w {
			w[i] = rng.Float32()*2 - 1
		}
		got := conv1x1SIMD(x, w, tc.in, tc.out, tc.frames)
		for oc := 0; oc < tc.out; oc++ {
			for frame := 0; frame < tc.frames; frame++ {
				var want float32
				for ic := 0; ic < tc.in; ic++ {
					want += w[oc*tc.in+ic] * x[ic*tc.frames+frame]
				}
				if d := math.Abs(float64(got[oc*tc.frames+frame] - want)); d > 2e-4 {
					t.Fatalf("in=%d out=%d T=%d [%d,%d]: got=%g want=%g diff=%g", tc.in, tc.out, tc.frames, oc, frame, got[oc*tc.frames+frame], want, d)
				}
			}
		}
	}
}

func TestConv3x3SIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, tc := range []struct{ in, out, h, w, stride, pad int }{{8, 8, 7, 13, 1, 1}, {32, 32, 20, 67, 2, 1}} {
		x := make([]float32, tc.in*tc.h*tc.w)
		w := make([]float32, tc.out*tc.in*9)
		for i := range x {
			x[i] = rng.Float32()*2 - 1
		}
		for i := range w {
			w[i] = rng.Float32()*2 - 1
		}
		oh, ow := (tc.h+2*tc.pad-3)/tc.stride+1, tc.w+2*tc.pad-3+1
		got := conv3x3SIMD(x, w, tc.in, tc.h, tc.w, tc.out, oh, ow, tc.stride, tc.pad)
		for oc := 0; oc < tc.out; oc++ {
			for yy := 0; yy < oh; yy++ {
				for xx := 0; xx < ow; xx++ {
					var want float32
					for ic := 0; ic < tc.in; ic++ {
						for ky := 0; ky < 3; ky++ {
							for kx := 0; kx < 3; kx++ {
								iy, ix := yy*tc.stride-tc.pad+ky, xx-tc.pad+kx
								if iy >= 0 && iy < tc.h && ix >= 0 && ix < tc.w {
									want += w[((oc*tc.in+ic)*3+ky)*3+kx] * x[(ic*tc.h+iy)*tc.w+ix]
								}
							}
						}
					}
					if d := math.Abs(float64(got[(oc*oh+yy)*ow+xx] - want)); d > 4e-4 {
						t.Fatalf("%+v [%d,%d,%d] diff=%g", tc, oc, yy, xx, d)
					}
				}
			}
		}
	}
}

func TestConv1d3SIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	const in, out, frames = 128, 32, 41
	x, w := make([]float32, in*frames), make([]float32, out*in*3)
	for i := range x {
		x[i] = rng.Float32()*2 - 1
	}
	for i := range w {
		w[i] = rng.Float32()*2 - 1
	}
	m := &CAMPlus{w: map[string]tensor{"p.weight": {shape: []int{out, in, 3}, data: w}}}
	got := m.conv1d3SIMD(x, in, frames, "p")
	for oc := 0; oc < out; oc++ {
		for frame := 0; frame < frames; frame++ {
			var want float32
			for ic := 0; ic < in; ic++ {
				for z := 0; z < 3; z++ {
					at := frame - 1 + z
					if at >= 0 && at < frames {
						want += w[(oc*in+ic)*3+z] * x[ic*frames+at]
					}
				}
			}
			if d := math.Abs(float64(got[oc*frames+frame] - want)); d > 3e-4 {
				t.Fatalf("[%d,%d] diff=%g", oc, frame, d)
			}
		}
	}
}

func TestConv1dPointMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(123))
	const in, out = 64, 35
	x, w, bias := make([]float32, in), make([]float32, out*in), make([]float32, out)
	for i := range x {
		x[i] = rng.Float32()*2 - 1
	}
	for i := range w {
		w[i] = rng.Float32()*2 - 1
	}
	for i := range bias {
		bias[i] = rng.Float32()*2 - 1
	}
	m := &CAMPlus{w: map[string]tensor{"p.weight": {shape: []int{out, in, 1}, data: w}, "p.bias": {shape: []int{out}, data: bias}}}
	got := m.conv1dPoint(x, in, "p", true)
	for oc := 0; oc < out; oc++ {
		var want float32
		for i := 0; i < in; i++ {
			want += x[i] * w[oc*in+i]
		}
		want += bias[oc]
		if want < 0 {
			want = 0
		}
		if d := math.Abs(float64(got[oc] - want)); d > 1e-5 {
			t.Fatalf("out=%d diff=%g", oc, d)
		}
	}
}
