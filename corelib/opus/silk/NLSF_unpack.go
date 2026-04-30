package silk

func NLSF_unpack(ec_ix []int16, pred_Q8 []uint8, psNLSF_CB *NLSF_CB, CB1_index int) {
	ec_sel_ptr := psNLSF_CB.Ec_sel[CB1_index*int(psNLSF_CB.Order)/2:]
	for i := 0; i < int(psNLSF_CB.Order); i += 2 {
		entry := ec_sel_ptr[0]
		ec_sel_ptr = ec_sel_ptr[1:]
		ec_ix[i] = int16(int(int32(int16((int(entry)>>1)&7))) * int(int32(int16((NLSF_QUANT_MAX_AMPLITUDE*2)+1))))
		pred_Q8[i] = psNLSF_CB.Pred_Q8[i+(int(entry)&1)*(int(psNLSF_CB.Order)-1)]
		ec_ix[i+1] = int16(int(int32(int16((int(entry)>>5)&7))) * int(int32(int16((NLSF_QUANT_MAX_AMPLITUDE*2)+1))))
		pred_Q8[i+1] = psNLSF_CB.Pred_Q8[i+((int(entry)>>4)&1)*(int(psNLSF_CB.Order)-1)+1]
	}
}
