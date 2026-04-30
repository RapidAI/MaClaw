package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_del_dec_quant(indices []int8, x_Q10 []int16, w_Q5 []int16, pred_coef_Q8 []uint8, ec_ix []int16, ec_rates_Q5 []uint8, quant_step_size_Q16 int, inv_quant_step_size_Q6 int16, mu_Q20 int32, order int16) int32 {
	return silk.NLSF_del_dec_quant(indices, x_Q10, w_Q5, pred_coef_Q8, ec_ix, ec_rates_Q5, quant_step_size_Q16, inv_quant_step_size_Q6, mu_Q20, order)
}
