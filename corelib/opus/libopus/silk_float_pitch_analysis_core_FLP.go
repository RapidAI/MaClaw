package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/silk"
)

func silk_pitch_analysis_core_FLP(frame []float32, pitch_out []int, lagIndex *int16, contourIndex *int8, LTPCorr *float32, prevLag int, search_thres1 float32, search_thres2 float32, Fs_kHz int, complexity int, nb_subfr int, arch int) int {
	return silk.PitchAnalysisCore_FLP(frame, pitch_out, lagIndex, contourIndex, LTPCorr, prevLag, search_thres1, search_thres2, Fs_kHz, complexity, nb_subfr, arch)
}
