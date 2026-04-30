package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"unsafe"
)

func silk_LPC_inverse_pred_gain_FLP(A *float32, order int32) float32 {
	var (
		k        int
		n        int
		invGain  float64
		rc       float64
		rc_mult1 float64
		rc_mult2 float64
		tmp1     float64
		tmp2     float64
		Atmp     [24]float32
	)
	libc.MemCpy(unsafe.Pointer(&Atmp[0]), unsafe.Pointer(A), int(uintptr(order)*unsafe.Sizeof(float32(0))))
	invGain = 1.0
	for k = int(order) - 1; k > 0; k-- {
		rc = float64(-Atmp[k])
		rc_mult1 = 1.0 - rc*rc
		invGain *= rc_mult1
		if invGain*MAX_PREDICTION_POWER_GAIN < 1.0 {
			return 0.0
		}
		rc_mult2 = 1.0 / rc_mult1
		for n = 0; n < (k+1)>>1; n++ {
			tmp1 = float64(Atmp[n])
			tmp2 = float64(Atmp[k-n-1])
			Atmp[n] = float32((tmp1 - tmp2*rc) * rc_mult2)
			Atmp[k-n-1] = float32((tmp2 - tmp1*rc) * rc_mult2)
		}
	}
	rc = float64(-Atmp[0])
	rc_mult1 = 1.0 - rc*rc
	invGain *= rc_mult1
	if invGain*MAX_PREDICTION_POWER_GAIN < 1.0 {
		return 0.0
	}
	return float32(invGain)
}
