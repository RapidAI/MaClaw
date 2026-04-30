package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_VQ(err_Q24 []int32, in_Q15 []int16, pCB_Q8 []uint8, pWght_Q9 []int16, K int, LPC_order int) {
	silk.NLSF_VQ(err_Q24, in_Q15, pCB_Q8, pWght_Q9, K, LPC_order)
}
