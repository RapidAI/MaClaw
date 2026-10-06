package tensor

// q8p.go — Q8_0 × Q8_0 (int8 activation) GEMM for repacked weights.
//
// Activations are quantized per 32-wide block to int8 (Q8_0 style: d =
// amax/127, q = round(x/d)); weights keep their Q8_0 int8 values but are
// repacked at load time into a VNNI-friendly, column-interleaved layout
// (32 output columns per panel, 4 consecutive K bytes per column, values
// stored as u8 = w+128). One VPDPBUSD then computes 16 columns × 4 K for one
// row; the +128 offset is removed by starting each block's int32 accumulator
// at −128·Σq_a (precomputed per activation block). Each 32-K block's int32
// dot is converted to float and scaled by d_a·d_w, so the arithmetic is the
// same as llama.cpp's Q8_0×Q8_0 dot (only the activation rounding differs
// from the float path).
//
// Layout (pair p = columns 32p..32p+31, block b, step j = K sub-group of 4):
//
//	QB[((p*NB+b)*8+j)*128 + n*4 + e] = uint8(W[32p+n][32b+4j+e] + 128)
//	SB[(p*NB+b)*32 + n]              = d_w(32p+n, b)
//
// The register-blocked tile is 4 rows × 32 columns. amd64 dispatch
// (MACLAW_Q8P_ISA): AVX-512 VNNI > AVX-VNNI (VEX) > AVX2 (VPMADDUBSW sign-
// trick) > off. q8pTileGo is the portable reference used by tests/fallback.
// arm64 uses a signed s8 weight layout + SDOT (see q8p_arm64*).

import (
	"math"
)

// Q8Packed is a Q8_0 weight matrix [N rows (outputs) × K] repacked for
// MatMulQ8P. N and K must be multiples of 32.
type Q8Packed struct {
	N, K, NB int
	QB       []byte
	SB       []float32
}

// Q8PSupported reports whether a [N×K] Q8_0 matrix can be packed.
func Q8PSupported(N, K int) bool { return N > 0 && K > 0 && N%32 == 0 && K%32 == 0 }

// PackQ8P repacks a Q8_0 tensor (Rows=N outputs, Cols=K). Returns nil when
// the shape is unsupported.
func PackQ8P(t *Q8Tensor) *Q8Packed {
	N, K := t.Rows, t.Cols
	if !Q8PSupported(N, K) || len(t.Data) < N*K/32*q8BlockBytes {
		return nil
	}
	return packQ8PTo(t, make([]byte, N*K), make([]float32, N*(K/32)))
}

func packQ8PTo(t *Q8Tensor, qb []byte, sb []float32) *Q8Packed {
	N, K := t.Rows, t.Cols
	if len(t.Data) < N*K/32*q8BlockBytes {
		return nil
	}
	nb := K / 32
	p := &Q8Packed{N: N, K: K, NB: nb, QB: qb[:N*K], SB: sb[:N*nb]}
	for n := 0; n < N; n++ {
		pair, nn := n/32, n%32
		for b := 0; b < nb; b++ {
			blk := t.Data[(n*nb+b)*q8BlockBytes : (n*nb+b+1)*q8BlockBytes]
			p.SB[(pair*nb+b)*32+nn] = float16to32(uint16(blk[0]) | uint16(blk[1])<<8)
			base := (pair*nb + b) * 8 * 128
			for j := 0; j < 8; j++ {
				o := base + j*128 + nn*4
				for e := 0; e < 4; e++ {
					p.QB[o+e] = blk[2+j*4+e] + 128 // int8 → u8 offset (two's complement wrap)
				}
			}
		}
	}
	return p
}

// Q8Act is a block-quantized activation matrix (rows padded to a multiple of
// 4 with zeros so the 4-row tile never reads garbage).
type Q8Act struct {
	M, Mpad, K, NB int
	Q              []int8
	S              []float32 // [Mpad][NB] block scales
	C              []int32   // [Mpad][NB] −128·Σq (VNNI u8 offset correction)
}

// QuantizeQ8Act quantizes a[M×K] into dst (grown as needed) and returns it.
func QuantizeQ8Act(dst *Q8Act, a []float32, M, K int) *Q8Act {
	if dst == nil {
		dst = &Q8Act{}
	}
	nb := K / 32
	mp := (M + 3) &^ 3
	dst.M, dst.Mpad, dst.K, dst.NB = M, mp, K, nb
	if cap(dst.Q) < mp*K {
		dst.Q = make([]int8, mp*K)
	}
	dst.Q = dst.Q[:mp*K]
	if cap(dst.S) < mp*nb {
		dst.S = make([]float32, mp*nb)
		dst.C = make([]int32, mp*nb)
	}
	dst.S = dst.S[:mp*nb]
	dst.C = dst.C[:mp*nb]
	if !quantizeQ8ActAsm(dst, a, M, K) {
		quantizeQ8ActGo(dst.Q, dst.S, dst.C, a, M, K)
	}
	if mp > M {
		clear(dst.Q[M*K:])
		clear(dst.S[M*nb:])
		clear(dst.C[M*nb:])
	}
	return dst
}

func quantizeQ8ActGo(q []int8, s []float32, c []int32, a []float32, M, K int) {
	nb := K / 32
	for i := 0; i < M*nb; i++ {
		x := a[i*32 : i*32+32 : i*32+32]
		var amax float32
		for _, v := range x {
			if v < 0 {
				v = -v
			}
			if v > amax {
				amax = v
			}
		}
		d := amax / 127
		id := 127 / max(amax, float32(1e-30)) // floor keeps 127/amax finite (matches asm)
		qq := q[i*32 : i*32+32 : i*32+32]
		var sum int32
		for k, v := range x {
			r := int32(math.RoundToEven(float64(v * id)))
			qq[k] = int8(r)
			sum += r
		}
		s[i] = d
		c[i] = -128 * sum
	}
}

// q8pTileGo computes the 4×32 tile out[m*32+n] = Σ_b sa[m,b]·sw[n,b]·Σ_k qa·qw
// for rows m0..m0+3 of act and column pair p of w (portable reference).
func q8pTileGo(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) {
	nb := w.NB
	K := w.K
	for i := range out {
		out[i] = 0
	}
	for b := 0; b < nb; b++ {
		qb := w.QB[(p*nb+b)*1024 : (p*nb+b+1)*1024]
		sb := w.SB[(p*nb+b)*32 : (p*nb+b+1)*32]
		for m := 0; m < 4; m++ {
			row := m0 + m
			aq := act.Q[row*K+b*32 : row*K+b*32+32]
			sa := act.S[row*nb+b]
			c := act.C[row*nb+b]
			for n := 0; n < 32; n++ {
				acc := c
				for j := 0; j < 8; j++ {
					o := j*128 + n*4
					for e := 0; e < 4; e++ {
						acc += int32(qb[o+e]) * int32(aq[j*4+e])
					}
				}
				out[m*32+n] += float32(acc) * (sa * sb[n])
			}
		}
	}
}

func q8pTile(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) {
	if q8pTileAsm(out, act, w, m0, p) {
		return
	}
	q8pTileGo(out, act, w, m0, p)
}

// MatMulQ8P computes out[m*N+n] (=, or += when accum) Σ_k act[m,k]·W[n,k]
// for the act.M real rows. maxWorkers==1 runs serially on the caller.
func MatMulQ8P(out []float32, act *Q8Act, w *Q8Packed, accum bool, maxWorkers int) {
	N := w.N
	pairs := N / 32
	M := act.M
	run := func(ps, pe int) {
		var tile [128]float32
		for p := ps; p < pe; p++ {
			for m0 := 0; m0 < M; m0 += 4 {
				q8pTile(&tile, act, w, m0, p)
				rows := M - m0
				if rows > 4 {
					rows = 4
				}
				for m := 0; m < rows; m++ {
					dst := out[(m0+m)*N+p*32 : (m0+m)*N+p*32+32 : (m0+m)*N+p*32+32]
					src := tile[m*32 : m*32+32 : m*32+32]
					if accum {
						for i := range dst {
							dst[i] += src[i]
						}
					} else {
						copy(dst, src)
					}
				}
			}
		}
	}
	if maxWorkers == 1 || !shouldParallel(M, N, w.K) {
		run(0, pairs)
		return
	}
	nw := poolWorkers()
	if maxWorkers > 1 && nw > maxWorkers {
		nw = maxWorkers
	}
	parallelRangesWithWorkers(pairs, nw, run)
}

// Q8ActResidual writes a − dequant(act) into r (len ≥ act.M*act.K): the
// rounding error of the int8 activation, which a second Q8P pass can add
// back (two-term "int8+int8" activations ≈ 15-bit precision at 2× cost).
func Q8ActResidual(r, a []float32, act *Q8Act) {
	K, nb := act.K, act.NB
	for i := 0; i < act.M*nb; i++ {
		d := act.S[i]
		x := a[i*32 : i*32+32 : i*32+32]
		q := act.Q[i*32 : i*32+32 : i*32+32]
		o := r[i*32 : i*32+32 : i*32+32]
		for k := range x {
			o[k] = x[k] - float32(q[k])*d
		}
	}
	_ = K
}

// SoftmaxWeightedSum128 is SoftmaxWeightedSumStrided for dim == 128 with an
// AVX-512 strided FMA accumulation (GQA V rows are strided by nKV·128, which
// the existing dim-128 path only handles with scalar Go). Other dims and
// non-AVX-512 CPUs use SoftmaxWeightedSumStrided unchanged.
func SoftmaxWeightedSum128(out, scores, values []float32, rows, stride, dim int) {
	if dim != 128 || rows <= 0 {
		SoftmaxWeightedSumStrided(out, scores, values, rows, stride, dim)
		return
	}
	scores = scores[:rows]
	inv := softmaxInplaceInv(scores)
	if inv == 0 {
		clear(out[:dim])
		return
	}
	if !attnWSum128Asm(out, scores, values, rows, stride, inv) {
		weightedSumStridedScaled(out, scores, values, rows, stride, dim, inv)
	}
}

// MatMulQ8PMulti runs several MatMulQ8P products that share one activation
// (Q/K/V, gate/up) as a single parallel dispatch over all their column
// pairs: one fork/join instead of len(ws). accum is false for all outputs.
func MatMulQ8PMulti(outs [][]float32, act *Q8Act, ws []*Q8Packed, maxWorkers int) {
	var starts [9]int
	if len(ws) > 8 || len(ws) != len(outs) {
		for i := range ws {
			MatMulQ8P(outs[i], act, ws[i], false, maxWorkers)
		}
		return
	}
	total := 0
	flops := 0
	for i, w := range ws {
		starts[i] = total
		total += w.N / 32
		flops += w.N
	}
	starts[len(ws)] = total
	M := act.M
	run := func(ps, pe int) {
		var tile [128]float32
		for gp := ps; gp < pe; gp++ {
			i := 0
			for gp >= starts[i+1] {
				i++
			}
			w, out, p := ws[i], outs[i], gp-starts[i]
			N := w.N
			for m0 := 0; m0 < M; m0 += 4 {
				q8pTile(&tile, act, w, m0, p)
				rows := min(M-m0, 4)
				for m := 0; m < rows; m++ {
					copy(out[(m0+m)*N+p*32:(m0+m)*N+p*32+32], tile[m*32:m*32+32])
				}
			}
		}
	}
	if maxWorkers == 1 || !shouldParallel(M, flops, ws[0].K) {
		run(0, total)
		return
	}
	nw := poolWorkers()
	if maxWorkers > 1 && nw > maxWorkers {
		nw = maxWorkers
	}
	parallelRangesWithWorkers(total, nw, run)
}
