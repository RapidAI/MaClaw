package libopus

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_Get_Decoder_Size(decSizeBytes *int) int {
	*decSizeBytes = silk.GetDecoderSize()
	return SILK_NO_ERROR
}
func silk_InitDecoder(decState unsafe.Pointer) int {
	return ((*silk.Decoder)(decState)).Init()
}
func silk_Decode(decState unsafe.Pointer, decControl *silk_DecControlStruct, lostFlag int, newPacketFlag int, psRangeDec *ec_dec, samplesOut []int16, nSamplesOut *int32, arch int) int {
	return ((*silk.Decoder)(decState)).Decode(decControl, lostFlag, newPacketFlag, psRangeDec, samplesOut, nSamplesOut, arch)
}
