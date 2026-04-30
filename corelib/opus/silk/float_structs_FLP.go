package silk

type ShapeStateFLP struct {
	LastGainIndex      int8
	HarmShapeGain_smth float32
	Tilt_smth          float32
}
type EncoderStateFLP struct {
	SCmn    EncoderState
	SShape  ShapeStateFLP
	X_buf   [720]float32
	LTPCorr float32
}
type EncoderControlFLP struct {
	Gains             [4]float32
	PredCoef          [2][16]float32
	LTPCoef           [20]float32
	LTP_scale         float32
	PitchL            [4]int
	AR                [96]float32
	LF_MA_shp         [4]float32
	LF_AR_shp         [4]float32
	Tilt              [4]float32
	HarmShapeGain     [4]float32
	Lambda            float32
	Input_quality     float32
	Coding_quality    float32
	PredGain          float32
	LTPredCodGain     float32
	ResNrg            [4]float32
	GainsUnq_Q16      [4]int32
	LastGainIndexPrev int8
}
type Encoder struct {
	State_Fxx                 [2]EncoderStateFLP
	SStereo                   StereoEncState
	NBitsUsedLBRR             int32
	NBitsExceeded             int32
	NChannelsAPI              int
	NChannelsInternal         int
	NPrevChannelsInternal     int
	TimeSinceSwitchAllowed_ms int
	AllowBandwidthSwitch      int
	Prev_decode_only_middle   int
}
