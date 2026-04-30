package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_find_LTP_FLP(XX [100]float32, xX [20]float32, r_ptr []float32, lag [4]int, subfr_length int, nb_subfr int) {
	silk.Find_LTP_FLP(XX, xX, r_ptr, lag, subfr_length, nb_subfr)
}
