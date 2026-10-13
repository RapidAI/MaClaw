//go:build arm64

package tensor

import (
	"os"

	"golang.org/x/sys/cpu"
)

func init() {
	q8pPackSigned = true // SDOT needs s8×s8; no u8 offset / C correction
	q8pX2 = enableQ8P && enableI8MM
}

// x/sys/cpu reports no features on darwin/arm64 (no HWCAP), so Apple
// Silicon would silently fall back to the float path; armHasDotProd adds an
// OS-specific probe (sysctl hw.optional.arm.FEAT_DotProd on darwin).
var enableQ8P = (cpu.ARM64.HasASIMDDP || armHasDotProd()) && os.Getenv("MACLAW_Q8P") != "0"

// Q8PAvailable reports whether the arm64 SDOT Q8P kernel is usable.
func Q8PAvailable() bool { return enableQ8P }

// enableI8MM selects the SMMLA kernel with fused two-term (X2) activations
// (q8p_x2.go): ≈ float accuracy at about single-pass SDOT cost. Needs
// FEAT_I8MM (Apple M2+, Neoverse V1/N2+). MACLAW_Q8P_ISA=sdot forces SDOT.
var enableI8MM = (cpu.ARM64.HasI8MM || armHasI8MM()) && os.Getenv("MACLAW_Q8P_ISA") != "sdot"

// Q8PISAName returns "i8mm" (SMMLA, X2 activations), "sdot", or "off".
func Q8PISAName() string {
	if !enableQ8P {
		return "off"
	}
	if q8pX2 {
		return "i8mm"
	}
	return "sdot"
}

//go:noescape
func q8pTileX2I8MM(out *float32, aq1, aq2 *int8, rpStride int, as4 *float32, as4Stride int, qb *byte, sb *float32, nb int)

//go:noescape
func quantX2RowNEON(q1, q2 *int8, s *float32, a *float32, nb int, c127, tiny, c256 float32)

func q8pTileX2Asm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) {
	nb := w.NB
	rp := m0 / 2
	_ = act.Q[(rp+2)*nb*64-1]
	_ = act.Q2[(rp+2)*nb*64-1]
	_ = act.S4[(rp+2)*nb*4-1]
	_ = w.QB[(p+1)*nb*1024-1]
	_ = w.SB[(p+1)*nb*32-1]
	var buf [128]float32
	qb, sb := p*nb*1024, p*nb*32
	ao, so := rp*nb*64, rp*nb*4
	q8pTileX2I8MM(&buf[0], &act.Q[ao], &act.Q2[ao], nb*64, &act.S4[so], nb*16, &w.QB[qb], &w.SB[sb], nb)
	q8pTileX2I8MM(&buf[64], &act.Q[ao], &act.Q2[ao], nb*64, &act.S4[so], nb*16, &w.QB[qb+512], &w.SB[sb+16], nb)
	// buf[h*64 + rp*32 + j*4 + l] → out[(2rp + l/2)*32 + 16h + 2j + l%2]
	for h := 0; h < 2; h++ {
		for r := 0; r < 2; r++ {
			src := buf[h*64+r*32 : h*64+r*32+32]
			o0 := out[(2*r)*32+16*h : (2*r)*32+16*h+16]
			o1 := out[(2*r+1)*32+16*h : (2*r+1)*32+16*h+16]
			for j := 0; j < 8; j++ {
				o0[2*j], o0[2*j+1] = src[j*4], src[j*4+1]
				o1[2*j], o1[2*j+1] = src[j*4+2], src[j*4+3]
			}
		}
	}
}

func quantizeQ8ActX2Asm(dst *Q8Act, a []float32, M, K int) bool {
	if !quantNEON {
		return false
	}
	nb := K / 32
	_ = a[M*K-1]
	run := func(r0, r1 int) {
		for r := r0; r < r1; r++ {
			o := (r/2)*nb*64 + (r%2)*8
			_ = dst.Q[o+(nb-1)*64+48+7]
			_ = dst.Q2[o+(nb-1)*64+48+7]
			quantX2RowNEON(&dst.Q[o], &dst.Q2[o], &dst.S[r*nb], &a[r*K], nb, 127, 1e-30, 256)
		}
	}
	if M >= 64 && q8pDynamic {
		parallelRanges(M, run)
	} else {
		run(0, M)
	}
	return true
}

// q8pSDOTv1 selects the original 4x4-group kernel (MACLAW_Q8P_SDOT=v1) for
// A/B comparison; default is the register-blocked 4x16 SDOT-by-element tile.
var q8pSDOTv1 = os.Getenv("MACLAW_Q8P_SDOT") == "v1"

//go:noescape
func q8pTile4x16SDOT(out *float32, aq *int8, kStride int, as *float32, nbStride int, qb *byte, sb *float32, nb int)

//go:noescape
func q8pTile4x32SDOT(out *float32, aq *int8, kStride int, as *float32, nbStride int, qb *byte, sb *float32, nb int)

func q8pTileAsm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) bool {
	if !enableQ8P {
		return false
	}
	if w.I8MM {
		if !q8pX2 {
			return false
		}
		q8pTileX2Asm(out, act, w, m0, p)
		return true
	}
	nb := w.NB
	K := w.K
	_ = act.Q[(m0+3)*K+K-1]
	_ = act.S[(m0+3)*nb+nb-1]
	_ = w.QB[(p+1)*nb*1024-1]
	_ = w.SB[(p+1)*nb*32-1]
	if q8pSDOTv1 {
		q8pTile4x32SDOT(&out[0], &act.Q[m0*K], K, &act.S[m0*nb], nb*4, &w.QB[p*nb*1024], &w.SB[p*nb*32], nb)
		return true
	}
	*out = [128]float32{}
	qb, sb := p*nb*1024, p*nb*32
	q8pTile4x16SDOT(&out[0], &act.Q[m0*K], K, &act.S[m0*nb], nb*4, &w.QB[qb], &w.SB[sb], nb)
	q8pTile4x16SDOT(&out[16], &act.Q[m0*K], K, &act.S[m0*nb], nb*4, &w.QB[qb+64], &w.SB[sb+16], nb)
	return true
}

//go:noescape
func quantQ8ActNEON(q *int8, s *float32, a *float32, nblocks int, c127, tiny float32)

var quantNEON = os.Getenv("MACLAW_Q8P_QUANT_NEON") != "0"

// quantizeQ8ActAsm: NEON quantizer, bit-identical q/s to quantizeQ8ActGo
// (FCVTNS = round-half-even of the same float32 product). The C term (u8
// offset correction) is unused by the signed SDOT layout and left zero.
func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool {
	if !quantNEON || !q8pPackSigned || M*K == 0 {
		return false
	}
	nb := M * K / 32
	_ = a[nb*32-1]
	_ = dst.Q[nb*32-1]
	_ = dst.S[nb-1]
	quantQ8ActNEON(&dst.Q[0], &dst.S[0], &a[0], nb, 127, 1e-30)
	clear(dst.C[:nb])
	return true
}

//go:noescape
func attnWSum128NEON(out, w, v *float32, rows, strideBytes int, scale float32)

//go:noescape
func attnDots128NEON(out, q, k *float32, n, strideBytes int, scale float32)

var attnNEON = os.Getenv("MACLAW_ATTN_NEON") != "0"

func attnWSum128Asm(out, w, v []float32, rows, stride int, scale float32) bool {
	if !attnNEON || rows <= 0 {
		return false
	}
	_ = out[127]
	_ = w[rows-1]
	_ = v[(rows-1)*stride+127]
	attnWSum128NEON(&out[0], &w[0], &v[0], rows, stride*4, scale)
	return true
}

func attnDots128Asm(out, q, k []float32, n, stride int, scale float32) bool {
	if !attnNEON || n <= 0 {
		return false
	}
	_ = out[n-1]
	_ = q[127]
	_ = k[(n-1)*stride+127]
	attnDots128NEON(&out[0], &q[0], &k[0], n, stride*4, scale)
	return true
}
