//go:build amd64

package tensor

// dotQ8RowASM computes the fused dequant-dot product of a float32 vector
// with a single Q8_0 row using AVX2 SIMD instructions.
// Parameters:
//   a      — float32 vector, len >= nBlocks*32
//   data   — raw Q8_0 data (entire tensor)
//   rowOff — byte offset to the start of the target row (row * nBlocks * 34)
//   nBlocks — number of Q8 blocks in the row (cols / 32)
// Returns the dot product as float32.
//
//go:noescape
func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32

// dequantRowIntoASM dequantizes a Q8_0 row into a float32 destination buffer
// using AVX2 SIMD instructions.
// Parameters:
//   dst    — float32 destination, len >= nBlocks*32
//   data   — raw Q8_0 data (entire tensor)
//   rowOff — byte offset to the start of the target row
//   nBlocks — number of Q8 blocks in the row
//
//go:noescape
func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int)
