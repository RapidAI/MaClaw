package silk

import "github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

func silk_LTP_scale_ctrl_FLP(psEnc *EncoderStateFLP, psEncCtrl *EncoderControlFLP, condCoding int) {
	var round_loss int
	if condCoding == CODE_INDEPENDENTLY {
		round_loss = psEnc.SCmn.PacketLoss_perc * psEnc.SCmn.NFramesPerPacket
		if int(psEnc.SCmn.LBRR_flag) != 0 {
			round_loss = (int(int32(int16(round_loss)))*int(int32(int16(round_loss))))/100 + 2
		}
		psEnc.SCmn.Indices.LTP_scaleIndex = int8(libc.BoolToInt((int(int32(int16(psEncCtrl.LTPredCodGain))) * int(int32(int16(round_loss)))) > int(silk_log2lin(int32(2900-psEnc.SCmn.SNR_dB_Q7)))))
		psEnc.SCmn.Indices.LTP_scaleIndex += int8(libc.BoolToInt((int(int32(int16(psEncCtrl.LTPredCodGain))) * int(int32(int16(round_loss)))) > int(silk_log2lin(int32(3900-psEnc.SCmn.SNR_dB_Q7)))))
	} else {
		psEnc.SCmn.Indices.LTP_scaleIndex = 0
	}
	psEncCtrl.LTP_scale = float32(silk_LTPScales_table_Q14[psEnc.SCmn.Indices.LTP_scaleIndex]) / 16384.0
}
