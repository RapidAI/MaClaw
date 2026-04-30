package libopus

import "unsafe"

const MODE_SILK_ONLY = 1000
const MODE_HYBRID = 1001
const MODE_CELT_ONLY = 1002
const OPUS_SET_VOICE_RATIO_REQUEST = 11018
const OPUS_GET_VOICE_RATIO_REQUEST = 11019
const OPUS_SET_FORCE_MODE_REQUEST = 11002

type OpusRepacketizer struct {
	Toc       uint8
	Nb_frames int
	Frames    [48]*uint8
	Len       [48]int16
	Framesize int
}
type ChannelLayout struct {
	Nb_channels        int
	Nb_streams         int
	Nb_coupled_streams int
	Mapping            [256]uint8
}
type MappingType int

const (
	MAPPING_TYPE_NONE = MappingType(iota)
	MAPPING_TYPE_SURROUND
	MAPPING_TYPE_AMBISONICS
)

type OpusMSEncoder struct {
	Layout            ChannelLayout
	Arch              int
	Lfe_stream        int
	Application       int
	Variable_duration int
	Mapping_type      MappingType
	Bitrate_bps       int32
}
type OpusMSDecoder struct {
	Layout ChannelLayout
}
type opus_copy_channel_in_func func(dst *opus_val16, dst_stride int, src unsafe.Pointer, src_stride int, src_channel int, frame_size int, user_data unsafe.Pointer)
type opus_copy_channel_out_func func(dst unsafe.Pointer, dst_stride int, dst_channel int, src *opus_val16, src_stride int, frame_size int, user_data unsafe.Pointer)
type downmix_func func(unsafe.Pointer, *opus_val32, int, int, int, int, int)

func align(i int) int {
	type foo struct {
		C int8
		U struct {
			// union
			P unsafe.Pointer
			I int32
			V opus_val32
		}
	}
	var alignment uint = uint(unsafe.Offsetof(foo{}.U))
	return ((i + int(alignment) - 1) / int(alignment)) * int(alignment)
}
