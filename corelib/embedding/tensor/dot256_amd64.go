//go:build amd64

package tensor

//go:noescape
func dot256AVX2(a, b *float32) float32

//go:noescape
func dot256AVX512(a, b *float32) float32

func dot256(a, b []float32) float32 {
	if hasAVX512 {
		return dot256AVX512(&a[0], &b[0])
	}
	if hasAVX2andFMA {
		return dot256AVX2(&a[0], &b[0])
	}
	return dot256Scalar(a, b)
}
