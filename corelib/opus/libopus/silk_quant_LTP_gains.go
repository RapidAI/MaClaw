package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_quant_LTP_gains(B_Q14 [20]int16, cbk_index [4]int8, periodicity_index *int8, sum_log_gain_Q7 *int32, pred_gain_dB_Q7 *int, XX_Q17 [100]int32, xX_Q17 [20]int32, subfr_len int, nb_subfr int, arch int) {
	silk.QuantLTPGains(B_Q14, cbk_index, periodicity_index, sum_log_gain_Q7, pred_gain_dB_Q7, XX_Q17, xX_Q17, subfr_len, nb_subfr, arch)
}
