package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_residual_energy_FLP(nrgs [4]float32, x []float32, a [2][16]float32, gains []float32, subfr_length int, nb_subfr int, LPC_order int) {
	silk.Residual_energy_FLP(nrgs, x, a, gains, subfr_length, nb_subfr, LPC_order)
}
