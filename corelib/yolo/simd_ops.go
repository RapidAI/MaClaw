//go:build amd64

package yolo

// axpySlice computes out[i] += w * x[i] over the overlap of out and x.
// Uses the AVX2 FMA kernel when available.
func axpySlice(out, x []float32, w float32) {
	n := len(out)
	if len(x) < n {
		n = len(x)
	}
	if n <= 0 {
		return
	}
	if hasAVX2FMA {
		body := n &^ 7
		if body >= 8 {
			axpyAVX2(&out[0], &x[0], w, body)
		}
		for i := body; i < n; i++ {
			out[i] += w * x[i]
		}
		return
	}
	for i := 0; i < n; i++ {
		out[i] += w * x[i]
	}
}

// siluSlice applies SiLU in place: x[i] = x[i] * sigmoid(x[i]).
// Uses the 512-bit kernel when AVX512 is available (16-wide), else the AVX2
// kernel (8-wide); the scalar tail keeps the exact math.Exp-based sigmoid
// for the last (<8) elements.
func siluSlice(x []float32) {
	n := len(x)
	if n == 0 {
		return
	}
	if hasAVX512 {
		body := n &^ 15
		if body >= 16 {
			siluZmmAVX512(&x[0], body)
		}
		for i := body; i < n; i++ {
			x[i] = x[i] * sigmoid(x[i])
		}
		return
	}
	if hasAVX2FMA {
		body := n &^ 7
		if body >= 8 {
			siluAVX2(&x[0], body)
		}
		for i := body; i < n; i++ {
			x[i] = x[i] * sigmoid(x[i])
		}
		return
	}
	for i, v := range x {
		x[i] = v * sigmoid(v)
	}
}
