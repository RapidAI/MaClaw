package silk

const FLAG_DECODE_NORMAL = 0
const FLAG_PACKET_LOST = 1
const FLAG_DECODE_LBRR = 2

type EncControlStruct struct {
	NChannelsAPI              int32
	NChannelsInternal         int32
	API_sampleRate            int32
	MaxInternalSampleRate     int32
	MinInternalSampleRate     int32
	DesiredInternalSampleRate int32
	PayloadSize_ms            int
	BitRate                   int32
	PacketLossPercentage      int
	Complexity                int
	UseInBandFEC              int
	LBRR_coded                int
	UseDTX                    int
	UseCBR                    int
	MaxBits                   int
	ToMono                    int
	OpusCanSwitch             int
	ReducedDependency         int
	InternalSampleRate        int32
	AllowBandwidthSwitch      int
	InWBmodeWithoutVariableLP int
	StereoWidth_Q14           int
	SwitchReady               int
	SignalType                int
	Offset                    int
}
type DecControlStruct struct {
	NChannelsAPI       int32
	NChannelsInternal  int32
	API_sampleRate     int32
	InternalSampleRate int32
	PayloadSize_ms     int
	PrevPitchLag       int
}
