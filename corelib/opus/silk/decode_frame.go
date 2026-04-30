package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func DecodeFrame(psDec *DecoderState, psRangeDec *entcode.Decoder, pOut []int16, pN *int32, lostFlag int, condCoding int, arch int) int {
	L := psDec.Frame_length
	psDecCtrl := new(DecoderControl)
	psDecCtrl.LTP_scale_Q14 = 0
	if lostFlag == FLAG_DECODE_NORMAL || lostFlag == FLAG_DECODE_LBRR && psDec.LBRR_flags[psDec.NFramesDecoded] == 1 {
		pulses := make([]int16, (L+SHELL_CODEC_FRAME_LENGTH-1) & ^(int(SHELL_CODEC_FRAME_LENGTH-1)))
		DecodeIndices(psDec, psRangeDec, psDec.NFramesDecoded, lostFlag != FLAG_DECODE_NORMAL, condCoding)
		DecodePulses(psRangeDec, pulses, int(psDec.Indices.SignalType), int(psDec.Indices.QuantOffsetType), psDec.Frame_length)
		DecodeParameters(psDec, psDecCtrl, condCoding)
		DecodeCore(psDec, psDecCtrl, pOut, [320]int16(pulses), arch)
		PLC(psDec, psDecCtrl, pOut, 0, arch)
		psDec.LossCnt = 0
		psDec.PrevSignalType = int(psDec.Indices.SignalType)
		psDec.First_frame_after_reset = 0
	} else {
		PLC(psDec, psDecCtrl, pOut, 1, arch)
	}
	mv_len := psDec.Ltp_mem_length - psDec.Frame_length
	libc.MemMove(unsafe.Pointer(&psDec.OutBuf[0]), unsafe.Pointer(&psDec.OutBuf[psDec.Frame_length]), mv_len*int(unsafe.Sizeof(int16(0))))
	libc.MemCpy(unsafe.Pointer(&psDec.OutBuf[mv_len]), unsafe.Pointer(&pOut[0]), psDec.Frame_length*int(unsafe.Sizeof(int16(0))))
	CNG(psDec, psDecCtrl, pOut, L)
	PLC_glue_frames(psDec, pOut, L)
	psDec.LagPrev = psDecCtrl.PitchL[psDec.Nb_subfr-1]
	*pN = int32(L)
	return 0
}
