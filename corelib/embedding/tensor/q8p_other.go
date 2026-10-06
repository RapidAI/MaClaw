//go:build !amd64

package tensor

// Q8PAvailable reports whether the assembly Q8P kernel is usable. Off on
// non-amd64: the existing float Q8 kernels (NEON on arm64) are faster than
// the portable int8 reference.
func Q8PAvailable() bool { return false }

func q8pTileAsm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) bool { return false }

func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool { return false }
