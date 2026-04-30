package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

const MAX_FRAME_SIZE = 384

func silk_burg_modified_FLP(A []float32, x []float32, minInvGain float32, subfr_length int, nb_subfr int, D int) float32 {
	var (
		k                int
		n                int
		s                int
		reached_max_gain int
		C0               float64
		invGain          float64
		num              float64
		nrg_f            float64
		nrg_b            float64
		rc               float64
		Atmp             float64
		tmp1             float64
		tmp2             float64
		C_first_row      [24]float64
		C_last_row       [24]float64
		CAf              [25]float64
		CAb              [25]float64
		Af               [24]float64
	)
	C0 = silk_energy_FLP(x, nb_subfr*subfr_length)
	libc.MemSet(unsafe.Pointer(&C_first_row[0]), 0, int(SILK_MAX_ORDER_LPC*unsafe.Sizeof(float64(0))))
	for s = 0; s < nb_subfr; s++ {
		x_ptr := x[s*subfr_length:]
		for n = 1; n < D+1; n++ {
			C_first_row[n-1] += silk_inner_product_FLP(x_ptr, x_ptr[n:], subfr_length-n)
		}
	}
	libc.MemCpy(unsafe.Pointer(&C_last_row[0]), unsafe.Pointer(&C_first_row[0]), int(SILK_MAX_ORDER_LPC*unsafe.Sizeof(float64(0))))
	CAb[0] = func() float64 {
		p := &CAf[0]
		CAf[0] = C0 + FIND_LPC_COND_FAC*C0 + 1e-09
		return *p
	}()
	invGain = 1.0
	reached_max_gain = 0
	for n = 0; n < D; n++ {
		for s = 0; s < nb_subfr; s++ {
			x_ptr := x[s*subfr_length:]
			tmp1 = float64(x_ptr[n])
			tmp2 = float64(x_ptr[subfr_length-n-1])
			for k = 0; k < n; k++ {
				C_first_row[k] -= float64(x_ptr[n] * x_ptr[n-k-1])
				C_last_row[k] -= float64(x_ptr[subfr_length-n-1] * x_ptr[subfr_length-n+k])
				Atmp = Af[k]
				tmp1 += float64(x_ptr[n-k-1]) * Atmp
				tmp2 += float64(x_ptr[subfr_length-n+k]) * Atmp
			}
			for k = 0; k <= n; k++ {
				CAf[k] -= tmp1 * float64(x_ptr[n-k])
				CAb[k] -= tmp2 * float64(x_ptr[subfr_length-n+k-1])
			}
		}
		tmp1 = C_first_row[n]
		tmp2 = C_last_row[n]
		for k = 0; k < n; k++ {
			Atmp = Af[k]
			tmp1 += C_last_row[n-k-1] * Atmp
			tmp2 += C_first_row[n-k-1] * Atmp
		}
		CAf[n+1] = tmp1
		CAb[n+1] = tmp2
		num = CAb[n+1]
		nrg_b = CAb[0]
		nrg_f = CAf[0]
		for k = 0; k < n; k++ {
			Atmp = Af[k]
			num += CAb[n-k] * Atmp
			nrg_b += CAb[k+1] * Atmp
			nrg_f += CAf[k+1] * Atmp
		}
		rc = num * (-2.0) / (nrg_f + nrg_b)
		tmp1 = invGain * (1.0 - rc*rc)
		if tmp1 <= float64(minInvGain) {
			rc = math.Sqrt(1.0 - float64(minInvGain)/invGain)
			if num > 0 {
				rc = -rc
			}
			invGain = float64(minInvGain)
			reached_max_gain = 1
		} else {
			invGain = tmp1
		}
		for k = 0; k < (n+1)>>1; k++ {
			tmp1 = Af[k]
			tmp2 = Af[n-k-1]
			Af[k] = tmp1 + rc*tmp2
			Af[n-k-1] = tmp2 + rc*tmp1
		}
		Af[n] = rc
		if reached_max_gain != 0 {
			for k = n + 1; k < D; k++ {
				Af[k] = 0.0
			}
			break
		}
		for k = 0; k <= n+1; k++ {
			tmp1 = CAf[k]
			CAf[k] += rc * CAb[n-k+1]
			CAb[n-k+1] += rc * tmp1
		}
	}
	if reached_max_gain != 0 {
		for k = 0; k < D; k++ {
			A[k] = float32(-Af[k])
		}
		for s = 0; s < nb_subfr; s++ {
			C0 -= silk_energy_FLP(x[s*subfr_length:], D)
		}
		nrg_f = C0 * invGain
	} else {
		nrg_f = CAf[0]
		tmp1 = 1.0
		for k = 0; k < D; k++ {
			Atmp = Af[k]
			nrg_f += CAf[k+1] * Atmp
			tmp1 += Atmp * Atmp
			A[k] = float32(-Atmp)
		}
		nrg_f -= FIND_LPC_COND_FAC * C0 * tmp1
	}
	return float32(nrg_f)
}
