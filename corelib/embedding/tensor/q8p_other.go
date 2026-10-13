//go:build !amd64 && !arm64

package tensor

// Q8PAvailable reports whether the assembly Q8P kernel is usable.
func Q8PAvailable() bool { return false }

// Q8PISAName is "off" when no Q8P asm is built.
func Q8PISAName() string { return "off" }

func q8pTileAsm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) bool { return false }

func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool { return false }

func attnWSum128Asm(out, w, v []float32, rows, stride int, scale float32) bool { return false }

func attnDots128Asm(out, q, k []float32, n, stride int, scale float32) bool { return false }

func quantizeQ8ActX2Asm(dst *Q8Act, a []float32, M, K int) bool { return false }
