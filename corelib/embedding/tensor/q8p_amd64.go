//go:build amd64

package tensor

import (
	"os"
	"strings"
)

// Q8P ISA selection (MACLAW_Q8P_ISA=avx512|avxvnni|avx2|off). Empty = best
// available. MACLAW_Q8P=0 forces off (same as ISA=off).
const (
	q8pISAOff = iota
	q8pISAAVX2
	q8pISAAVXVNNI
	q8pISAAVX512
)

var (
	q8pISA    = resolveQ8PISA()
	enableQ8P = q8pISA != q8pISAOff
)

func resolveQ8PISA() int {
	if os.Getenv("MACLAW_Q8P") == "0" {
		return q8pISAOff
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_Q8P_ISA"))) {
	case "off", "0", "none":
		return q8pISAOff
	case "avx512":
		if hasAVX512VNNI {
			return q8pISAAVX512
		}
		return q8pISAOff
	case "avxvnni":
		if hasAVXVNNI {
			return q8pISAAVXVNNI
		}
		return q8pISAOff
	case "avx2":
		if hasAVX2andFMA {
			return q8pISAAVX2
		}
		return q8pISAOff
	}
	// Auto: AVX-512 VNNI > AVX-VNNI > AVX2.
	if hasAVX512VNNI {
		return q8pISAAVX512
	}
	if hasAVXVNNI {
		return q8pISAAVXVNNI
	}
	if hasAVX2andFMA {
		return q8pISAAVX2
	}
	return q8pISAOff
}

// Q8PAvailable reports whether an assembly Q8P kernel is usable.
func Q8PAvailable() bool { return enableQ8P }

// Q8PISAName returns the active ISA ("avx512", "avxvnni", "avx2", or "off").
func Q8PISAName() string {
	switch q8pISA {
	case q8pISAAVX512:
		return "avx512"
	case q8pISAAVXVNNI:
		return "avxvnni"
	case q8pISAAVX2:
		return "avx2"
	default:
		return "off"
	}
}

// forceQ8PISAForTest overrides ISA selection after init (tests only).
func forceQ8PISAForTest(name string) {
	os.Setenv("MACLAW_Q8P_ISA", name)
	if name == "off" || name == "0" || name == "none" {
		os.Setenv("MACLAW_Q8P", "0")
	} else {
		os.Unsetenv("MACLAW_Q8P")
	}
	q8pISA = resolveQ8PISA()
	enableQ8P = q8pISA != q8pISAOff
}

//go:noescape
func q8pTile4x32VNNI(out *float32, aq *int8, kStride int, ac *int32, as *float32, nbStride int, qb *byte, sb *float32, nb int)

//go:noescape
func q8pTile4x32AVXVNNI(out *float32, aq *int8, kStride int, ac *int32, as *float32, nbStride int, qb *byte, sb *float32, nb int)

//go:noescape
func q8pTile4x32AVX2(out *float32, aq *int8, kStride int, ac *int32, as *float32, nbStride int, qb *byte, sb *float32, nb int)

func q8pTileAsm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) bool {
	if !enableQ8P {
		return false
	}
	nb := w.NB
	K := w.K
	_ = act.Q[(m0+3)*K+K-1]
	_ = act.S[(m0+3)*nb+nb-1]
	_ = act.C[(m0+3)*nb+nb-1]
	_ = w.QB[(p+1)*nb*1024-1]
	_ = w.SB[(p+1)*nb*32-1]
	aq, ac, as := &act.Q[m0*K], &act.C[m0*nb], &act.S[m0*nb]
	qb, sb := &w.QB[p*nb*1024], &w.SB[p*nb*32]
	nbStride := nb * 4
	switch q8pISA {
	case q8pISAAVX512:
		q8pTile4x32VNNI(&out[0], aq, K, ac, as, nbStride, qb, sb, nb)
	case q8pISAAVXVNNI:
		q8pTile4x32AVXVNNI(&out[0], aq, K, ac, as, nbStride, qb, sb, nb)
	case q8pISAAVX2:
		q8pTile4x32AVX2(&out[0], aq, K, ac, as, nbStride, qb, sb, nb)
	default:
		return false
	}
	return true
}

//go:noescape
func q8pQuantizeAVX512(q *int8, s *float32, c *int32, a *float32, nBlocks int)

//go:noescape
func q8pQuantizeAVX2(q *int8, s *float32, c *int32, a *float32, nBlocks int)

func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool {
	nb := M * (K / 32)
	if !enableQ8P || nb == 0 {
		return false
	}
	_ = a[nb*32-1]
	_ = dst.Q[nb*32-1]
	_ = dst.S[nb-1]
	_ = dst.C[nb-1]
	switch q8pISA {
	case q8pISAAVX512:
		q8pQuantizeAVX512(&dst.Q[0], &dst.S[0], &dst.C[0], &a[0], nb)
	case q8pISAAVXVNNI, q8pISAAVX2:
		q8pQuantizeAVX2(&dst.Q[0], &dst.S[0], &dst.C[0], &a[0], nb)
	default:
		return false
	}
	return true
}

//go:noescape
func attnWSum128AVX512(out *float32, w *float32, v *float32, rows, strideBytes int, scale float32)

func attnWSum128Asm(out, w, v []float32, rows, stride int, scale float32) bool {
	if !hasAVX512 || rows <= 0 {
		return false
	}
	_ = out[127]
	_ = w[rows-1]
	_ = v[(rows-1)*stride+127]
	attnWSum128AVX512(&out[0], &w[0], &v[0], rows, stride*4, scale)
	return true
}

func attnDots128Asm(out, q, k []float32, n, stride int, scale float32) bool { return false }

func quantizeQ8ActX2Asm(dst *Q8Act, a []float32, M, K int) bool { return false }
