package onnxrt

import xt "github.com/RapidAI/CodeClaw/corelib/embedding/tensor"

// adjacentSwap reports the left axis of the only swapped adjacent pair in
// perm. PP-DocLayoutV3's transposes are all of this form: [1,0], [0,2,1],
// [0,2,1,3], and [0,2,1,3,4].
func adjacentSwap(perm []int) (int, bool) {
	axis := -1
	for i := 0; i < len(perm); i++ {
		if perm[i] == i {
			continue
		}
		if axis >= 0 || i+1 >= len(perm) || perm[i] != i+1 || perm[i+1] != i {
			return 0, false
		}
		axis = i
		i++
	}
	if axis < 0 {
		return 0, false
	}
	return axis, true
}

// transposeAdjacent swaps source axes axis and axis+1.
// src layout [outer, A, B, inner] becomes [outer, B, A, inner].
// inner == 1 uses an 8×8 AVX2 or 4×4 NEON tile; a trailing inner span
// (attention head dim) is a blocked copy of that span.
func transposeAdjacent(dst, src []float32, shape []int, axis int) {
	if axis < 0 || axis+1 >= len(shape) {
		return
	}
	outer := numElements(shape[:axis])
	A := shape[axis]
	B := shape[axis+1]
	inner := numElements(shape[axis+2:])
	if outer == 0 || A == 0 || B == 0 || inner == 0 {
		return
	}
	if inner == 1 && hasAVX2FMA && A >= 8 && B >= 8 {
		transposeMat8(dst, src, outer, A, B)
		return
	}
	if inner == 1 && transposeMat4(dst, src, outer, A, B) {
		return
	}
	transposeBlocked(dst, src, outer, A, B, inner)
}

func transposeMat8(dst, src []float32, outer, A, B int) {
	// One pool level only. transposeMat8Outer also calls ParallelRanges;
	// a pool worker that waits on that same fixed pool deadlocks once
	// every worker is inside the outer split (outer >= worker count).
	if outer >= 2 && outer*A*B >= 8192 {
		xt.ParallelRanges(outer, func(start, end int) {
			for o := start; o < end; o++ {
				transposeMat8Band(dst[o*A*B:], src[o*A*B:], A, B, 0, A)
			}
		})
		return
	}
	for o := 0; o < outer; o++ {
		transposeMat8Outer(dst[o*A*B:], src[o*A*B:], A, B)
	}
}

func transposeMat8Outer(dst, src []float32, A, B int) {
	tiles := A / 8
	if tiles >= 4 && A*B >= 8192 {
		xt.ParallelRanges(tiles, func(start, end int) {
			transposeMat8Band(dst, src, A, B, start*8, end*8)
		})
		if tail := tiles * 8; tail < A {
			transposeMat8Band(dst, src, A, B, tail, A)
		}
		return
	}
	transposeMat8Band(dst, src, A, B, 0, A)
}

func transposeMat8Band(dst, src []float32, A, B, a0, a1 int) {
	a := a0
	for ; a+8 <= a1; a += 8 {
		b := 0
		for ; b+8 <= B; b += 8 {
			transpose8x8F32(&dst[b*A+a], A, &src[a*B+b], B)
		}
		for ; b < B; b++ {
			for i := 0; i < 8; i++ {
				dst[b*A+a+i] = src[(a+i)*B+b]
			}
		}
	}
	for ; a < a1; a++ {
		for b := 0; b < B; b++ {
			dst[b*A+a] = src[a*B+b]
		}
	}
}

func transposeBlocked(dst, src []float32, outer, A, B, inner int) {
	n := outer * A * B * inner
	if outer >= 2 && n >= 8192 {
		xt.ParallelRanges(outer, func(start, end int) {
			step := A * B * inner
			for o := start; o < end; o++ {
				transposeBlockedOuter(dst[o*step:], src[o*step:], A, B, inner)
			}
		})
		return
	}
	step := A * B * inner
	for o := 0; o < outer; o++ {
		transposeBlockedOuter(dst[o*step:], src[o*step:], A, B, inner)
	}
}

func transposeBlockedOuter(dst, src []float32, A, B, inner int) {
	const tile = 32
	for a0 := 0; a0 < A; a0 += tile {
		a1 := a0 + tile
		if a1 > A {
			a1 = A
		}
		for b0 := 0; b0 < B; b0 += tile {
			b1 := b0 + tile
			if b1 > B {
				b1 = B
			}
			if inner == 1 {
				for a := a0; a < a1; a++ {
					row := src[a*B+b0 : a*B+b1]
					for b, v := range row {
						dst[(b0+b)*A+a] = v
					}
				}
				continue
			}
			for a := a0; a < a1; a++ {
				for b := b0; b < b1; b++ {
					s := (a*B + b) * inner
					d := (b*A + a) * inner
					copy(dst[d:d+inner], src[s:s+inner])
				}
			}
		}
	}
}
