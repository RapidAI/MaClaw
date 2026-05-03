// Package amrnb provides local AMR-NB encoding for native IM voice messages.
//
// It wraps the OpenCORE AMR-NB encoder sources that are built into this Go
// package via cgo. No external encoder process or system codec is required.
package amrnb

/*
#cgo CXXFLAGS: -std=c++98 -DDISABLE_AMRNB_DECODER -I${SRCDIR}/internal/opencore-amr-0.1.3/amrnb -I${SRCDIR}/internal/opencore-amr-0.1.3/oscl -I${SRCDIR}/internal/opencore-amr-0.1.3/opencore/codecs_v2/audio/gsm_amr/amr_nb/common/include -I${SRCDIR}/internal/opencore-amr-0.1.3/opencore/codecs_v2/audio/gsm_amr/amr_nb/dec/include -I${SRCDIR}/internal/opencore-amr-0.1.3/opencore/codecs_v2/audio/gsm_amr/common/dec/include -I${SRCDIR}/internal/opencore-amr-0.1.3/opencore/codecs_v2/audio/gsm_amr/amr_nb/enc/src -I${SRCDIR}/internal/opencore-amr-0.1.3/opencore/codecs_v2/audio/gsm_amr/amr_nb/dec/src
#include "internal/opencore-amr-0.1.3/amrnb/interf_enc.h"
*/
import "C"

import (
	"bytes"
	"fmt"
	"unsafe"
)

const (
	SampleRate  = 8000
	FrameSize   = 160 // 20 ms at 8 kHz
	maxFrameLen = 32
)

// EncodeS16 encodes 8 kHz mono signed 16-bit PCM to an AMR-NB file payload.
// The returned bytes include the AMR magic header (#!AMR\n).
func EncodeS16(pcm []int16) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("amrnb: empty PCM")
	}
	state := C.Encoder_Interface_init(0)
	if state == nil {
		return nil, fmt.Errorf("amrnb: encoder init failed")
	}
	defer C.Encoder_Interface_exit(state)

	padded := make([]int16, len(pcm))
	copy(padded, pcm)
	if rem := len(padded) % FrameSize; rem != 0 {
		padded = append(padded, make([]int16, FrameSize-rem)...)
	}

	var out bytes.Buffer
	out.WriteString("#!AMR\n")
	frame := make([]byte, maxFrameLen)
	for off := 0; off < len(padded); off += FrameSize {
		n := C.Encoder_Interface_Encode(
			state,
			C.enum_Mode(C.MR122),
			(*C.short)(unsafe.Pointer(&padded[off])),
			(*C.uchar)(unsafe.Pointer(&frame[0])),
			1,
		)
		if n <= 0 || int(n) > len(frame) {
			return nil, fmt.Errorf("amrnb: encode failed at sample %d", off)
		}
		out.Write(frame[:int(n)])
	}
	return out.Bytes(), nil
}
