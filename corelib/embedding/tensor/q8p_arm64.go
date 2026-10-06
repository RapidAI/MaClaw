//go:build arm64

package tensor

import (
	"os"

	"golang.org/x/sys/cpu"
)

func init() {
	q8pPackSigned = true // SDOT needs s8×s8; no u8 offset / C correction
}

var enableQ8P = cpu.ARM64.HasASIMDDP && os.Getenv("MACLAW_Q8P") != "0"

// Q8PAvailable reports whether the arm64 SDOT Q8P kernel is usable.
func Q8PAvailable() bool { return enableQ8P }

// Q8PISAName returns "sdot" when the kernel is on, else "off".
func Q8PISAName() string {
	if enableQ8P {
		return "sdot"
	}
	return "off"
}

//go:noescape
func q8pTile4x32SDOT(out *float32, aq *int8, kStride int, as *float32, nbStride int, qb *byte, sb *float32, nb int)

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
	q8pTile4x32SDOT(&out[0], &act.Q[m0*K], K, &act.S[m0*nb], nb*4, &w.QB[p*nb*1024], &w.SB[p*nb*32], nb)
	return true
}

func quantizeQ8ActAsm(dst *Q8Act, a []float32, M, K int) bool {
	// Go quantizer is fine on arm64 (not the GEMM hotspot).
	return false
}

func attnWSum128Asm(out, w, v []float32, rows, stride int, scale float32) bool {
	return false
}
