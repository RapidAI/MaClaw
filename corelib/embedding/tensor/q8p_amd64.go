//go:build amd64

package tensor

import "os"

// enableQ8P gates the AVX-512 VNNI Q8P kernel (MACLAW_Q8P=0 disables).
var enableQ8P = hasAVX512VNNI && os.Getenv("MACLAW_Q8P") != "0"

// Q8PAvailable reports whether the assembly Q8P kernel is usable.
func Q8PAvailable() bool { return enableQ8P }

//go:noescape
func q8pTile4x32VNNI(out *float32, aq *int8, kStride int, ac *int32, as *float32, nbStride int, qb *byte, sb *float32, nb int)

func q8pTileAsm(out *[128]float32, act *Q8Act, w *Q8Packed, m0, p int) bool {
	if !enableQ8P {
		return false
	}
	nb := w.NB
	K := w.K
	_ = act.Q[(m0+3)*K+K-1]
	_ = act.S[(m0+3)*nb+nb-1]
	_ = w.QB[(p+1)*nb*1024-1]
	_ = w.SB[(p+1)*nb*32-1]
	q8pTile4x32VNNI(&out[0], &act.Q[m0*K], K, &act.C[m0*nb], &act.S[m0*nb], nb*4, &w.QB[p*nb*1024], &w.SB[p*nb*32], nb)
	return true
}

//go:noescape
func q8pQuantizeAVX512(q *int8, s *float32, c *int32, a *float32, nBlocks int)

func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool {
	nb := M * (K / 32)
	if !enableQ8P || nb == 0 {
		return false
	}
	_ = a[nb*32-1]
	_ = dst.Q[nb*32-1]
	_ = dst.S[nb-1]
	_ = dst.C[nb-1]
	q8pQuantizeAVX512(&dst.Q[0], &dst.S[0], &dst.C[0], &a[0], nb)
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
