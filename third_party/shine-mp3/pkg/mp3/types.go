package mp3

const (
	PI          = 3.14159265358979
	PI4         = 0.78539816339745
	PI12        = 0.26179938779915
	PI36        = 0.087266462599717
	PI64        = 0.049087385212
	SQRT2       = 1.41421356237
	LN2         = 0.69314718
	LN_TO_LOG10 = 0.2302585093
	BLKSIZE     = 1024
	/* for loop unrolling, require that HAN_SIZE%8==0 */
	HAN_SIZE      = 512
	SCALE_BLOCK   = 12
	SCALE_RANGE   = 64
	SCALE         = 32768
	SUBBAND_LIMIT = 32
	MAX_CHANNELS  = 2
	GRANULE_SIZE  = 576
	MAX_GRANULES  = 2
)

type mode int

const (
	STEREO mode = iota
	JOINT_STEREO
	DUAL_CHANNEL
	MONO
)

type emphasis int

const (
	NONE    emphasis = 0
	MU50_15 emphasis = 1
	CITT    emphasis = 3
)

type Wave struct {
	Channels   int64
	SampleRate int64
}
type MPEG struct {
	Version            mpegVersion
	Layer              int64
	GranulesPerFrame   int64
	Mode               mode
	Bitrate            int64
	Emph               emphasis
	Padding            int64
	BitsPerFrame       int64
	BitsPerSlot        int64
	FracSlotsPerFrame  float64
	Slot_lag           float64
	WholeSlotsPerFrame int64
	BitrateIndex       int64
	SampleRateIndex    int64
	Crc                int64
	Ext                int64
	ModeExt            int64
	Copyright          int64
	Original           int64
}
type L3Loop struct {
	// Magnitudes of the spectral values
	Xr    []int32
	Xrsq  [GRANULE_SIZE]int32
	Xrabs [GRANULE_SIZE]int32
	// Maximum of xrabs array
	Xrmax int32
	// gr
	EnTot  [2]int32
	En     [2][21]int32
	Xm     [2][21]int32
	Xrmaxl [2]int32
	// 2**(-x/4) for x = -127..0
	StepTable [128]float64
	// 2**(-x/4) for x = -127..0
	StepTableI [128]int32
	// x**(3/4) for x = 0..9999
	Int2idx [10000]int64
}
type MDCT struct {
	CosL [18][36]int32
}
type Subband struct {
	Off [2]int64
	Fl  [32][64]int32
	X   [2][512]int32
}
type GranuleInfo struct {
	Part2_3Length         uint64
	BigValues             uint64
	Count1                uint64
	GlobalGain            uint64
	ScaleFactorCompress   uint64
	TableSelect           [3]uint64
	Region0Count          uint64
	Region1Count          uint64
	PreFlag               uint64
	ScaleFactorScale      uint64
	Count1TableSelect     uint64
	Part2Length           uint64
	ScaleFactorBandMaxLen uint64
	Address1              uint64
	Address2              uint64
	Address3              uint64
	QuantizerStepSize     int64
	ScaleFactorLen        [4]uint64
}
type SideInfo struct {
	PrivateBits           uint64
	ReservoirDrain        int64
	ScaleFactorSelectInfo [MAX_CHANNELS][4]uint64
	Granules              [MAX_GRANULES]struct {
		Channels [MAX_CHANNELS]struct {
			Tt GranuleInfo
		}
	}
}
type PsyRatio struct {
	L [MAX_GRANULES][MAX_CHANNELS][21]float64
}
type PsyXMin struct {
	L [MAX_GRANULES][MAX_CHANNELS][21]float64
}
type ScaleFactor struct {
	L [2][2][22]int32
	S [2][2][13][3]int32
}
type Encoder struct {
	Wave             Wave
	Mpeg             MPEG
	bitstream        bitstream
	sideInfo         SideInfo
	sideInfoLen      int64
	meanBits         int64
	ratio            PsyRatio
	scaleFactor      ScaleFactor
	buffer           [2]*int16
	PerceptualEnergy [2][2]float64
	l3Encoding       [2][2][GRANULE_SIZE]int64
	l3SubbandSamples [2][3][18][32]int32
	mdctFrequency    [2][2][GRANULE_SIZE]int32
	reservoirSize    int64
	reservoirMaxSize int64
	l3loop           L3Loop
	mdct             MDCT
	subband          Subband
}
