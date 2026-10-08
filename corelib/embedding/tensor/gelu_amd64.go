//go:build amd64

package tensor

//go:noescape
func geluMulAVX2(gate, up []float32)

// geluMulBody writes the leading multiple of 8 lanes with the AVX2
// gelu_pytorch_tanh kernel. It returns 0 when that kernel is not used.
func geluMulBody(gate, up []float32) int {
	n := len(gate) &^ 7
	if n == 0 || len(up) < n || !hasAVX2andFMA {
		return 0
	}
	geluMulAVX2(gate[:n], up[:n])
	return n
}
