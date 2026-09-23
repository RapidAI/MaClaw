//go:build amd64

package yolo

import "golang.org/x/sys/cpu"

// hasAVX2FMA reports whether AVX2+FMA is available (Haswell+ / Zen+).
var hasAVX2FMA = cpu.X86.HasAVX2 && cpu.X86.HasFMA

// gemm6x16AVX2 computes a C[6][16] tile of the conv GEMM with bias
// initialization and an optional fused SiLU epilogue. See gemm_amd64.s.
//
// a: [6,k] A rows with row stride lda, b: [k,ldb] B rows starting at column
// n0, c: [6,ldc] C rows starting at column n0, bias: [6]. lda is separate
// from k so k-chunked callers can keep the original A row layout.
func gemm6x16AVX2(a, b, c, bias *float32, k, lda, ldb, ldc int, silu bool)

// gemm6x16AccAVX2 is the accumulating variant: C[m][n] += sum_k A[m][k] *
// B[k][n0+n], with accumulators loaded from and stored back to the C tile
// (no bias, no activation). Used for k-chunked accumulation into scratch.
func gemm6x16AccAVX2(a, b, c *float32, k, lda, ldb, ldc int)

// axpyAVX2 computes out[i] += w*x[i] for n elements (n multiple of 8).
func axpyAVX2(out, x *float32, w float32, n int)

// siluAVX2 computes x[i] = x[i]*sigmoid(x[i]) for n elements
// (n multiple of 8), using the same vectorized exp as the GEMM epilogue.
func siluAVX2(x *float32, n int)
