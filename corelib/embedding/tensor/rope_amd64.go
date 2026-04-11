//go:build amd64

package tensor

// ropePrecomputedASM applies RoPE using pre-computed cos/sin tables with AVX2.
// x is [nHeads * headDim], cosTable and sinTable are [halfDim].
// Processes 4 pairs (8 floats) per AVX2 iteration.
//
//go:noescape
func ropePrecomputedASM(x []float32, nHeads, headDim int, cosTable, sinTable []float32)
