//go:build !amd64

package yolo

// expSlice applies exp element-wise (portable).
func expSlice(dst, src []float32) {
	n := len(dst)
	if len(src) < n {
		n = len(src)
	}
	for i := 0; i < n; i++ {
		dst[i] = expF32(src[i])
	}
}

// scaleSlice multiplies every element by s in place (portable).
func scaleSlice(dst []float32, s float32) {
	for i := range dst {
		dst[i] *= s
	}
}
