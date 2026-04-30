package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_LPC_analysis_filter_FLP(r_LPC []float32, PredCoef []float32, s []float32, length int, Order int) {
	silk.LPC_analysis_filter_FLP(r_LPC, PredCoef, s, length, Order)
}
