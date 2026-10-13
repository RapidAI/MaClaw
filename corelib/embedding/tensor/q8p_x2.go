package tensor

// q8p_x2.go — "X2" fused two-term activations + I8MM (SMMLA) weight layout.
//
// Single int8 activations cost ~3e-4 in embedding cosine on Qwen3 (measured
// on Apple M4: min cos 0.9992 vs the float path), so the product needs the
// two-pass ("int8+int8") activations. Instead of quantizing the residual
// separately and running the GEMM twice, X2 quantizes each 32-block once
// into two int8 terms that share the block scale:
//
//	d = amax/127, p = x·127/amax, q1 = rne(p), q2 = sat8(rne((p−q1)·256))
//	x ≈ (d/256)·(256·q1 + q2)
//
// so one block's int32 dot is (Σq1·w)<<8 + Σq2·w and needs one float
// conversion and one scale (S = d/256). |Σq1·w|·256 ≤ 32·127·128·256 < 2^31.
//
// Layouts (rp = row pair, sub = row within the pair, c = 8-wide K chunk):
//
//	act Q/Q2[((rp*NB+b)*4+c)*16 + sub*8 + e] = q(row 2rp+sub, k 32b+8c+e)
//	act S[row*NB+b] = d/256;  S4[(rp*NB+b)*4 + {0,1,2,3}] = S of rows (2rp,2rp,2rp+1,2rp+1)
//	w QB[(p*NB+b)*1024 + h*512 + c*128 + j*16 + s*8 + e] = W(32p+16h+2j+s, 32b+8c+e)
//
// which is what SMMLA consumes (2×8 by 8×2 → 2×2 int32). SB is unchanged.

import "math"

// q8pX2 is set by the arm64 build when the I8MM kernel is in use: weights
// are packed in the SMMLA layout and QuantizeQ8Act produces X2 activations.
var q8pX2 = false

// Q8PFusedTwoTerm reports whether Q8P activations already carry two int8
// terms (callers must not add a separate residual pass).
func Q8PFusedTwoTerm() bool { return q8pX2 }

func packI8MMBlock(qb []byte, base int, nn int, blk []byte) {
	h, j, s := nn/16, (nn%16)/2, nn%2
	for c := 0; c < 4; c++ {
		o := base + h*512 + c*128 + j*16 + s*8
		copy(qb[o:o+8], blk[2+c*8:2+c*8+8])
	}
}

func quantizeQ8ActX2(dst *Q8Act, a []float32, M, K int) {
	nb := K / 32
	mp := dst.Mpad
	if cap(dst.Q2) < mp*K {
		dst.Q2 = make([]int8, mp*K)
	}
	dst.Q2 = dst.Q2[:mp*K]
	if cap(dst.S4) < mp*nb*2 {
		dst.S4 = make([]float32, mp*nb*2)
	}
	dst.S4 = dst.S4[:mp*nb*2]
	dst.X2 = true
	if !quantizeQ8ActX2Asm(dst, a, M, K) {
		quantizeQ8ActX2Go(dst, a, M, K)
	}
	if mp > M {
		for r := M; r < mp; r++ {
			rp, sub := r/2, r%2
			for b := 0; b < nb; b++ {
				for c := 0; c < 4; c++ {
					o := ((rp*nb+b)*4+c)*16 + sub*8
					clear(dst.Q[o : o+8])
					clear(dst.Q2[o : o+8])
				}
			}
		}
		clear(dst.S[M*nb:])
	}
	clear(dst.C)
	for rp := 0; rp < mp/2; rp++ {
		for b := 0; b < nb; b++ {
			s0, s1 := dst.S[(2*rp)*nb+b], dst.S[(2*rp+1)*nb+b]
			o := (rp*nb + b) * 4
			dst.S4[o], dst.S4[o+1], dst.S4[o+2], dst.S4[o+3] = s0, s0, s1, s1
		}
	}
}

func quantizeQ8ActX2Go(dst *Q8Act, a []float32, M, K int) {
	nb := K / 32
	for r := 0; r < M; r++ {
		rp, sub := r/2, r%2
		for b := 0; b < nb; b++ {
			x := a[r*K+b*32 : r*K+b*32+32]
			var amax float32
			for _, v := range x {
				if v < 0 {
					v = -v
				}
				if v > amax {
					amax = v
				}
			}
			id := 127 / max(amax, float32(1e-30))
			dst.S[r*nb+b] = amax / 127 / 256
			for k, v := range x {
				p := float32(v * id) // explicit rounding: no FMA fusion into p−q1 (arm64)
				q1 := float32(math.RoundToEven(float64(p)))
				q2 := math.RoundToEven(float64((p - q1) * 256))
				q2 = max(-128, min(127, q2))
				o := ((rp*nb+b)*4+k/8)*16 + sub*8 + k%8
				dst.Q[o] = int8(q1)
				dst.Q2[o] = int8(q2)
			}
		}
	}
}

// q8pTileGoX2 is the portable reference for the X2/I8MM 4×32 tile.
func q8pTileGoX2(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) {
	nb := w.NB
	for i := range out {
		out[i] = 0
	}
	for b := 0; b < nb; b++ {
		base := (p*nb + b) * 1024
		for m := 0; m < 4; m++ {
			row := m0 + m
			rp, sub := row/2, row%2
			sa := act.S[row*nb+b]
			for n := 0; n < 32; n++ {
				h, j, s := n/16, (n%16)/2, n%2
				var d1, d2 int32
				for c := 0; c < 4; c++ {
					ao := ((rp*nb+b)*4+c)*16 + sub*8
					wo := base + h*512 + c*128 + j*16 + s*8
					for e := 0; e < 8; e++ {
						wv := int32(int8(w.QB[wo+e]))
						d1 += int32(act.Q[ao+e]) * wv
						d2 += int32(act.Q2[ao+e]) * wv
					}
				}
				out[m*32+n] += float32(d1<<8+d2) * (sa * w.SB[(p*nb+b)*32+n])
			}
		}
	}
}
