package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_NLSF_unpack(ec_ix []int16, pred_Q8 []uint8, psNLSF_CB *silk_NLSF_CB_struct, CB1_index int) {
	silk.NLSF_unpack(ec_ix, pred_Q8, psNLSF_CB, CB1_index)
}
