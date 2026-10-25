package onnxrt

import (
	"math"

	xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// gridSampleRun evaluates GridSample. Coordinates and the four bilinear taps
// are computed once per output pixel, then applied to every channel. A dense
// bilinear/zeros sample (the output grid covers the map) is packed NCHW→NHWC
// so the channel blend is a contiguous AVX2/AVX-512/NEON multiply. A sparse
// deformable grid stays in NCHW: packing a large map to read a few points
// touches more memory than the gathers it would save.
func gridSampleRun(out, xf, gf []float32, N, C, H, W, oH, oW int, mode, padding string, align bool) {
	if N == 0 || C == 0 || oH == 0 || oW == 0 {
		return
	}
	plane := H * W
	oPlane := oH * oW
	if H == 0 || W == 0 || plane == 0 {
		for i := range out[:N*C*oPlane] {
			out[i] = 0
		}
		return
	}
	pack := mode == "bilinear" && padding == "zeros" && shouldPackGrid(C, plane, oPlane)
	for b := 0; b < N; b++ {
		src := xf[b*C*plane : (b+1)*C*plane]
		dst := out[b*C*oPlane : (b+1)*C*oPlane]
		grid := gf[b*oPlane*2 : (b+1)*oPlane*2]
		if pack {
			gridSamplePacked(dst, src, grid, C, H, W, oH, oW, align)
			continue
		}
		gridSampleDirect(dst, src, grid, C, H, W, oH, oW, mode, padding, align)
	}
}

// shouldPackGrid reports whether one NHWC pack of the feature map is cheaper
// than strided gathers. Each sample reads 4 taps; packing wins once those
// taps cover the map.
func shouldPackGrid(C, plane, samples int) bool {
	return C >= 8 && samples >= 8 && samples*4 >= plane
}

// gridTap4 is one output pixel's bilinear taps, shared by every channel.
// A negative index is an out-of-bounds zeros tap and contributes 0.
type gridTap4 struct {
	t00, t01, t10, t11 int
	omx, wx, omy, wy   float32
}

func gridSampleDirect(dst, src, grid []float32, C, H, W, oH, oW int, mode, padding string, align bool) {
	plane := H * W
	samples := oH * oW
	taps := make([]gridTap4, samples)
	allIn := true
	nearest := mode == "nearest"
	for s := 0; s < samples; s++ {
		g := s * 2
		ix := gridCoord(float64(grid[g]), W, align)
		iy := gridCoord(float64(grid[g+1]), H, align)
		if nearest {
			t := gridTap(int(math.Round(ix)), int(math.Round(iy)), W, H, padding)
			taps[s].t00 = t
			if t < 0 {
				allIn = false
			}
			continue
		}
		x0 := math.Floor(ix)
		y0 := math.Floor(iy)
		wx := float32(ix - x0)
		wy := float32(iy - y0)
		t := gridTap4{
			t00: gridTap(int(x0), int(y0), W, H, padding),
			t01: gridTap(int(x0)+1, int(y0), W, H, padding),
			t10: gridTap(int(x0), int(y0)+1, W, H, padding),
			t11: gridTap(int(x0)+1, int(y0)+1, W, H, padding),
			omx: float32(1) - wx,
			wx:  wx,
			omy: float32(1) - wy,
			wy:  wy,
		}
		if t.t00 < 0 || t.t01 < 0 || t.t10 < 0 || t.t11 < 0 {
			allIn = false
		}
		taps[s] = t
	}
	// Channel-outer: each plane is HW floats and stays hot while the taps
	// are gathered. The previous channel-inner walk jumped a full plane
	// between the two channels of one pixel, so a 100×100 map never reused
	// a cache line. Output stores are contiguous per channel.
	apply := func(start, end int) {
		for c := start; c < end; c++ {
			sp := src[c*plane : (c+1)*plane]
			dp := dst[c*samples : (c+1)*samples]
			if nearest {
				if allIn {
					for s, t := range taps {
						dp[s] = sp[t.t00]
					}
					continue
				}
				for s, t := range taps {
					if t.t00 < 0 {
						dp[s] = 0
						continue
					}
					dp[s] = sp[t.t00]
				}
				continue
			}
			if allIn {
				for s, t := range taps {
					dp[s] = sp[t.t00]*t.omx*t.omy + sp[t.t01]*t.wx*t.omy + sp[t.t10]*t.omx*t.wy + sp[t.t11]*t.wx*t.wy
				}
				continue
			}
			for s, t := range taps {
				var v00, v01, v10, v11 float32
				if t.t00 >= 0 {
					v00 = sp[t.t00]
				}
				if t.t01 >= 0 {
					v01 = sp[t.t01]
				}
				if t.t10 >= 0 {
					v10 = sp[t.t10]
				}
				if t.t11 >= 0 {
					v11 = sp[t.t11]
				}
				dp[s] = v00*t.omx*t.omy + v01*t.wx*t.omy + v10*t.omx*t.wy + v11*t.wx*t.wy
			}
		}
	}
	if C > 1 && C*samples >= 8192 {
		xt.ParallelRanges(C, apply)
		return
	}
	apply(0, C)
}

func gridSamplePacked(dst, src, grid []float32, C, H, W, oH, oW int, align bool) {
	plane := H * W
	samples := oH * oW
	nhwc := getScratch(C * plane)
	defer putScratch(nhwc)
	zeros := getScratch(C)
	defer putScratch(zeros)
	for i := range zeros {
		zeros[i] = 0
	}
	nhwcOut := getScratch(C * samples)
	defer putScratch(nhwcOut)
	transposePlanes(nhwc, src, C, plane)
	blend := func(start, end int) {
		for s := start; s < end; s++ {
			g := s * 2
			ix := gridCoord(float64(grid[g]), W, align)
			iy := gridCoord(float64(grid[g+1]), H, align)
			x0 := math.Floor(ix)
			y0 := math.Floor(iy)
			wx := float32(ix - x0)
			wy := float32(iy - y0)
			taps := [4]int{
				gridTap(int(x0), int(y0), W, H, "zeros"),
				gridTap(int(x0)+1, int(y0), W, H, "zeros"),
				gridTap(int(x0), int(y0)+1, W, H, "zeros"),
				gridTap(int(x0)+1, int(y0)+1, W, H, "zeros"),
			}
			var rows [4][]float32
			for i, t := range taps {
				if t < 0 {
					rows[i] = zeros
				} else {
					rows[i] = nhwc[t*C : t*C+C]
				}
			}
			bilinear4Into(nhwcOut[s*C:(s+1)*C], rows[0], rows[1], rows[2], rows[3], wx, wy)
		}
	}
	if samples >= 32 && C*samples >= 8192 {
		xt.ParallelRanges(samples, blend)
	} else {
		blend(0, samples)
	}
	// nhwcOut is [samples, C]; the ONNX output is NCHW [C, samples].
	transposePlanes(dst, nhwcOut, samples, C)
}

// gridTap is the integer location gridSampleAt would read, or -1 when zeros
// padding discards the tap. The index is shared by every channel.
func gridTap(x, y, w, h int, padding string) int {
	switch padding {
	case "border":
		if x < 0 {
			x = 0
		}
		if y < 0 {
			y = 0
		}
		if x >= w {
			x = w - 1
		}
		if y >= h {
			y = h - 1
		}
	case "reflection":
		x = reflectIndex(x, w)
		y = reflectIndex(y, h)
	default:
		if x < 0 || y < 0 || x >= w || y >= h {
			return -1
		}
	}
	if w == 0 || h == 0 {
		return -1
	}
	return y*w + x
}

// bilinear4Into writes out[i] = a[i]*(1-wx)*(1-wy) + b[i]*wx*(1-wy) +
// c[i]*(1-wx)*wy + d[i]*wx*wy. The product association matches gridBilinear,
// so an in-bounds sample agrees with that scalar formula bit for bit.
func bilinear4Into(out, a, b, c, d []float32, wx, wy float32) {
	n := len(out)
	if len(a) < n {
		n = len(a)
	}
	if len(b) < n {
		n = len(b)
	}
	if len(c) < n {
		n = len(c)
	}
	if len(d) < n {
		n = len(d)
	}
	if n <= 0 {
		return
	}
	omx := float32(1) - wx
	omy := float32(1) - wy
	body := bilinear4Vec(out[:n], a[:n], b[:n], c[:n], d[:n], omx, wx, omy, wy)
	for i := body; i < n; i++ {
		out[i] = a[i]*omx*omy + b[i]*wx*omy + c[i]*omx*wy + d[i]*wx*wy
	}
}
