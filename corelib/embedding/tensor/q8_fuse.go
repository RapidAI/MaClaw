package tensor

import "sync"

const (
	gemmaDim    = 768
	gemmaFFDim  = 1152
	gemmaKVDim  = 256
	gemmaFFTile = 384 // must divide 1152; multiple of 32
	gemmaMTile  = 16
)

// MatMulQ8PackedQKV computes Q/K/V from the same A panel without packing weights.
// q is [seq,768], k/v are [seq,256], a is [seq,768].
func MatMulQ8PackedQKV(q, k, v, a []float32, wq, wk, wv *Q8Tensor, seq, maxWorkers int) {
	const K, Nq, Nkv, mt = gemmaDim, gemmaDim, gemmaKVDim, gemmaMTile
	if seq <= 0 {
		return
	}
	if maxWorkers != 1 && packedQKVGemmaShort(q, k, v, a, wq, wk, wv, seq) {
		return
	}
	for m0 := 0; m0 < seq; m0 += mt {
		rows := mt
		if m0+rows > seq {
			rows = seq - m0
		}
		aPanel := a[m0*K : (m0+rows)*K]
		MatMulQ8N(q[m0*Nq:], aPanel, wq, rows, Nq, K, maxWorkers)
		MatMulQ8N(k[m0*Nkv:], aPanel, wk, rows, Nkv, K, maxWorkers)
		MatMulQ8N(v[m0*Nkv:], aPanel, wv, rows, Nkv, K, maxWorkers)
	}
}

// MatMulQ8DualOut computes two N=1152 projections from the same A, then SiLUMul.
// Fuses gate/up at the N-loop so each A panel stays in L1 across both weights.
func MatMulQ8DualOut(gate, up, a []float32, wGate, wUp *Q8Tensor, seq, maxWorkers int) {
	const K, N, mt = gemmaDim, gemmaFFDim, gemmaMTile
	if seq <= 0 {
		return
	}
	nBlocks := K / q8BlockSize
	hasSc := wGate != nil && wUp != nil &&
		len(wGate.Scales) >= wGate.Rows*nBlocks &&
		len(wUp.Scales) >= wUp.Rows*nBlocks && nBlocks > 0
	if !hasSc {
		for m0 := 0; m0 < seq; m0 += mt {
			rows := mt
			if m0+rows > seq {
				rows = seq - m0
			}
			aPanel := a[m0*K : (m0+rows)*K]
			MatMulQ8N(gate[m0*N:], aPanel, wGate, rows, N, K, maxWorkers)
			MatMulQ8N(up[m0*N:], aPanel, wUp, rows, N, K, maxWorkers)
		}
		SiLUMul(gate[:seq*N], up[:seq*N])
		return
	}
	if maxWorkers != 1 && packedDualOutGemmaShort(gate, up, a, wGate, wUp, seq) {
		return
	}
	run := func(ns, ne int) {
		for m0 := 0; m0 < seq; m0 += mt {
			rows := mt
			if m0+rows > seq {
				rows = seq - m0
			}
			matMulQ8DualOutRange(gate[m0*N:], up[m0*N:], a[m0*K:(m0+rows)*K],
				wGate, wUp, rows, N, K, ns, ne, nBlocks)
		}
	}
	if maxWorkers == 1 || !shouldParallel(seq, N, K) {
		run(0, N)
	} else {
		parallelRanges(N, run)
	}
	SiLUMul(gate[:seq*N], up[:seq*N])
}

func matMulQ8DualOutRange(gate, up, a []float32, wGate, wUp *Q8Tensor, M, N, K, ns, ne, nBlocks int) {
	var dG0, dG1, dU0, dU1 [8]float32
	var d4g, d4u [4]float32
	m := dualOutGemmDual8(gate, up, a, wGate, wUp, M, N, K, ns, ne, nBlocks)
	for ; m+7 < M; m += 8 {
		aPanel := a[m*K : (m+8)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot8T(&dG0, &dG1, aPanel, wGate, n, n+1, nBlocks, K)
			q8DualMultiDot8T(&dU0, &dU1, aPanel, wUp, n, n+1, nBlocks, K)
			storeDual4Plain(gate, m, n, N, &dG0, 0, 0)
			storeDual4Plain(gate, m+4, n, N, &dG1, 0, 0)
			storeDual4Plain(up, m, n, N, &dU0, 0, 0)
			storeDual4Plain(up, m+4, n, N, &dU1, 0, 0)
		}
		for ; n < ne; n++ {
			q8MultiDot8T(&dG0, aPanel, wGate, n, nBlocks, K)
			q8MultiDot8T(&dU0, aPanel, wUp, n, nBlocks, K)
			storeDot8Plain(gate, m, n, N, &dG0, 0)
			storeDot8Plain(up, m, n, N, &dU0, 0)
		}
	}
	if M-m == 3 {
		aPanel := a[m*K : (m+3)*K]
		if nBlocks == 24 && gemmaDualOutM3N24(gate[m*N:], up[m*N:], aPanel, wGate, wUp, N, ns, ne) {
			return
		}
		n := ns
		for ; n+1 < ne; n += 2 {
			if q8DualMultiDot3T(&dG0, aPanel, wGate, n, n+1, nBlocks, K) &&
				q8DualMultiDot3T(&dU0, aPanel, wUp, n, n+1, nBlocks, K) {
				storeDual3Plain(gate, m, n, N, &dG0)
				storeDual3Plain(up, m, n, N, &dU0)
				continue
			}
			for r := 0; r < 3; r++ {
				aRow := aPanel[r*K : (r+1)*K]
				g0, g1 := DotQ8RowDualScaled(aRow, wGate, n, n+1)
				u0, u1 := DotQ8RowDualScaled(aRow, wUp, n, n+1)
				gate[(m+r)*N+n], gate[(m+r)*N+n+1] = g0, g1
				up[(m+r)*N+n], up[(m+r)*N+n+1] = u0, u1
			}
		}
		for ; n < ne; n++ {
			for r := 0; r < 3; r++ {
				aRow := aPanel[r*K : (r+1)*K]
				gate[(m+r)*N+n] = DotQ8RowScaled(aRow, wGate, n)
				up[(m+r)*N+n] = DotQ8RowScaled(aRow, wUp, n)
			}
		}
		vzeroupperASM()
		return
	}
	for ; m+3 < M; m += 4 {
		aPanel := a[m*K : (m+4)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			if nBlocks == 24 && q8DualOut4T(&dG0, &dU0, aPanel, wGate, wUp, n, n+1, nBlocks, K) {
				storeDual4Plain(gate, m, n, N, &dG0, 0, 0)
				storeDual4Plain(up, m, n, N, &dU0, 0, 0)
				continue
			}
			q8DualMultiDot4T(&dG0, aPanel, wGate, n, n+1, nBlocks, K)
			q8DualMultiDot4T(&dU0, aPanel, wUp, n, n+1, nBlocks, K)
			storeDual4Plain(gate, m, n, N, &dG0, 0, 0)
			storeDual4Plain(up, m, n, N, &dU0, 0, 0)
		}
		for ; n < ne; n++ {
			q8MultiDot4T(&d4g, aPanel, wGate, n, nBlocks, K)
			q8MultiDot4T(&d4u, aPanel, wUp, n, nBlocks, K)
			storeDot4Plain(gate, m, n, N, &d4g, 0)
			storeDot4Plain(up, m, n, N, &d4u, 0)
		}
	}
	if m < M && (m > 0 || M <= 2) {
		rows := M - m
		ap := dualOutPadPool.Get().(*[]float32)
		aPad := *ap
		if cap(aPad) < 4*K {
			aPad = make([]float32, 4*K)
			*ap = aPad
		} else {
			aPad = aPad[:4*K]
		}
		clear(aPad)
		copy(aPad, a[m*K:M*K])
		n := ns
		for ; n+1 < ne; n += 2 {
			q8DualMultiDot4T(&dG0, aPad, wGate, n, n+1, nBlocks, K)
			q8DualMultiDot4T(&dU0, aPad, wUp, n, n+1, nBlocks, K)
			for r := 0; r < rows; r++ {
				gate[(m+r)*N+n] = dG0[r]
				gate[(m+r)*N+n+1] = dG0[r+4]
				up[(m+r)*N+n] = dU0[r]
				up[(m+r)*N+n+1] = dU0[r+4]
			}
		}
		dualOutPadPool.Put(ap)
		return
	}
	for ; m < M; m++ {
		aRow := a[m*K : (m+1)*K]
		n := ns
		for ; n+1 < ne; n += 2 {
			g0, g1 := DotQ8RowDualScaled(aRow, wGate, n, n+1)
			u0, u1 := DotQ8RowDualScaled(aRow, wUp, n, n+1)
			gate[m*N+n], gate[m*N+n+1] = g0, g1
			up[m*N+n], up[m*N+n+1] = u0, u1
		}
		for ; n < ne; n++ {
			gate[m*N+n] = DotQ8RowScaled(aRow, wGate, n)
			up[m*N+n] = DotQ8RowScaled(aRow, wUp, n)
		}
	}
}

var dualOutPadPool = sync.Pool{New: func() any { p := make([]float32, 4*gemmaDim); return &p }}

// MatMulQ8RowRange is MatMulQ8 over B rows [n0,n1). out is [M, n1-n0].
func MatMulQ8RowRange(out, a []float32, b *Q8Tensor, M, K, n0, n1, maxWorkers int) {
	if n1 <= n0 || M <= 0 || K <= 0 || b == nil {
		return
	}
	rowBlocks := b.Cols / q8BlockSize
	rowBytes := rowBlocks * q8BlockBytes
	sub := Q8Tensor{
		Data: b.Data[n0*rowBytes : n1*rowBytes],
		Rows: n1 - n0,
		Cols: b.Cols,
	}
	if len(b.Scales) >= n1*rowBlocks {
		sub.Scales = b.Scales[n0*rowBlocks : n1*rowBlocks]
	}
	MatMulQ8N(out, a, &sub, M, n1-n0, K, maxWorkers)
}

// MatMulQ8NKRange dots A[M,kLen] with B[N, Kfull] columns [k0,k0+kLen).
// k0 and kLen must be multiples of 32. Does not construct a Cols=kLen multi-row view.
// accum=true: out[m,n] += dot.
func MatMulQ8NKRange(out, a []float32, b *Q8Tensor, M, N, k0, kLen, maxWorkers int, accum bool) {
	if M <= 0 || N <= 0 || kLen <= 0 || b == nil {
		return
	}
	if k0%q8BlockSize != 0 || kLen%q8BlockSize != 0 {
		return
	}
	parentBlocks := b.Cols / q8BlockSize
	kBlocks := kLen / q8BlockSize
	kBlock0 := k0 / q8BlockSize
	run := func(ns, ne int) {
		for n := ns; n < ne; n++ {
			rowOff := (n*parentBlocks + kBlock0) * q8BlockBytes
			hasSc := len(b.Scales) >= (n+1)*parentBlocks
			mt := mTileForK(kLen)
			m := 0
			if mt >= 4 && hasSc {
				win := Q8Tensor{
					Data:   b.Data[rowOff:],
					Scales: b.Scales[n*parentBlocks+kBlock0 : n*parentBlocks+kBlock0+kBlocks],
					Rows:   1,
					Cols:   kLen,
				}
				for ; m+3 < M; m += 4 {
					var d4 [4]float32
					q8MultiDot4T(&d4, a[m*kLen:(m+4)*kLen], &win, 0, kBlocks, kLen)
					for t := 0; t < 4; t++ {
						dst := (m+t)*N + n
						if accum {
							out[dst] += d4[t]
						} else {
							out[dst] = d4[t]
						}
					}
				}
			}
			for ; m < M; m++ {
				aRow := a[m*kLen : (m+1)*kLen]
				var s float32
				if hasSc {
					s = dotQ8RowScaled(aRow, b.Data, &b.Scales[n*parentBlocks+kBlock0], rowOff, kBlocks)
				} else {
					s = dotQ8RowASM(aRow, b.Data, rowOff, kBlocks)
				}
				dst := m*N + n
				if accum {
					out[dst] += s
				} else {
					out[dst] = s
				}
			}
		}
	}
	if maxWorkers == 1 || !shouldParallel(M, N, kLen) {
		run(0, N)
		return
	}
	parallelRanges(N, run)
}

// MatMulQ8SwiGLUDown tiles gate/up N and down K at 384, then RMSNorm+residual on acc.
// a is [seq,768]; gTile/uTile are [seq,384]; acc/x are [seq,768].
func MatMulQ8SwiGLUDown(x, a, gTile, uTile, acc []float32, wGate, wUp, wDown *Q8Tensor, wRMS []float32, seq, maxWorkers int, eps float32) {
	if seq <= 0 {
		return
	}
	clear(acc[:seq*gemmaDim])
	for t := 0; t < 3; t++ {
		n0 := t * gemmaFFTile
		MatMulQ8RowRange(gTile, a, wGate, seq, gemmaDim, n0, n0+gemmaFFTile, maxWorkers)
		MatMulQ8RowRange(uTile, a, wUp, seq, gemmaDim, n0, n0+gemmaFFTile, maxWorkers)
		SiLUMul(gTile[:seq*gemmaFFTile], uTile[:seq*gemmaFFTile])
		MatMulQ8NKRange(acc, gTile, wDown, seq, gemmaDim, n0, gemmaFFTile, maxWorkers, true)
	}
	for s := 0; s < seq; s++ {
		row := acc[s*gemmaDim : (s+1)*gemmaDim]
		RMSNorm(row, row, wRMS, eps)
		Add(x[s*gemmaDim:(s+1)*gemmaDim], x[s*gemmaDim:(s+1)*gemmaDim], row)
	}
}

// MatMulQ8RMSResidual: y = RMSNorm(A @ B^T); x += y. yTile is [mt,N] and must
// not alias x or a.
func MatMulQ8RMSResidual(x, a, yTile []float32, b *Q8Tensor, wRMS []float32, seq, N, K, mt, maxWorkers int, eps float32) {
	if seq <= 0 || N <= 0 || K <= 0 {
		return
	}
	if maxWorkers != 1 && rmsResidualGemmaShort(x, a, yTile, b, wRMS, seq, N, K, eps) {
		return
	}
	if mt <= 0 || mt > 8 {
		mt = 8
	}
	for m0 := 0; m0 < seq; m0 += mt {
		rows := mt
		if m0+rows > seq {
			rows = seq - m0
		}
		MatMulQ8N(yTile[:rows*N], a[m0*K:(m0+rows)*K], b, rows, N, K, maxWorkers)
		for r := 0; r < rows; r++ {
			y := yTile[r*N : (r+1)*N]
			RMSNorm(y, y, wRMS, eps)
			Add(x[(m0+r)*N:(m0+r+1)*N], x[(m0+r)*N:(m0+r+1)*N], y)
		}
	}
}

var dualOutYTilePool = sync.Pool{New: func() any { p := make([]float32, 8*gemmaDim); return &p }}

// MatMulQ8DualOutDownRMS fuses DualOut Dual8 with FFN-down Dual8 under one
// M-outer parallelRanges. Each 8-row tile runs serial Dual8 (A stays in L1,
// SwiGLU stays hot for down) so there is a single join, not one per tile.
func MatMulQ8DualOutDownRMS(x, a, gate, up, yTile []float32, wGate, wUp, wDown *Q8Tensor, wRMS []float32, seq, maxWorkers int, eps float32) {
	const K, N, D, mt = gemmaDim, gemmaFFDim, gemmaDim, 8
	if seq <= 0 {
		return
	}
	nTiles := (seq + mt - 1) / mt
	doTile := func(m0, rows int, yt []float32) {
		MatMulQ8DualOut(gate[m0*N:], up[m0*N:], a[m0*K:(m0+rows)*K], wGate, wUp, rows, 1)
		MatMulQ8RMSResidual(x[m0*D:], gate[m0*N:], yt, wDown, wRMS, rows, D, N, mt, 1, eps)
	}
	run := func(ts, te int) {
		yt := yTile
		if ts != 0 || te != nTiles || len(yt) < mt*D {
			yp := dualOutYTilePool.Get().(*[]float32)
			yt = *yp
			if cap(yt) < mt*D {
				yt = make([]float32, mt*D)
				*yp = yt
			} else {
				yt = yt[:mt*D]
			}
			defer dualOutYTilePool.Put(yp)
		}
		for t := ts; t < te; t++ {
			m0 := t * mt
			rows := mt
			if m0+rows > seq {
				rows = seq - m0
			}
			doTile(m0, rows, yt)
		}
	}
	if maxWorkers == 1 || nTiles <= 1 || !shouldParallel(seq, N, K) {
		// One tile (Short Dual3): keep caller's maxWorkers so N-split DualDot2/Dual3 stays.
		if nTiles <= 1 {
			MatMulQ8DualOut(gate, up, a, wGate, wUp, seq, maxWorkers)
			MatMulQ8RMSResidual(x, gate, yTile, wDown, wRMS, seq, D, N, mt, maxWorkers, eps)
			return
		}
		run(0, nTiles)
		return
	}
	parallelRanges(nTiles, run)
}
