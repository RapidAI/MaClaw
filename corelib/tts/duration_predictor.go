package tts

import (
	"math"
)

// DurationPredictorForward runs the deterministic duration predictor.
// x: [hidden, T] encoder output
// g: [ginChannels, 1] speaker embedding
// Returns: logw [1, T] log-durations
func DurationPredictorForward(x []float32, hidden, T int,
	g []float32, ginCh int,
	dp *DurationPredictorWeights) []float32 {

	// Speaker conditioning: x = x + cond(g)
	input := make([]float32, hidden*T)
	copy(input, x)
	if g != nil && dp.Cond.Weight != nil {
		gProj := Conv1D(g, ginCh, 1, dp.Cond.Weight, 1, hidden, 1, 0, dp.Cond.Bias)
		for h := 0; h < hidden; h++ {
			gVal := gProj[h]
			for t := 0; t < T; t++ {
				input[h*T+t] += gVal
			}
		}
	}

	// Conv1 → ReLU → LayerNorm
	filter := dp.Conv1.OutCh
	if filter == 0 {
		filter = 256
	}
	kSize := dp.Conv1.KSize
	if kSize == 0 {
		kSize = 3
	}
	y := Conv1D(input, hidden, T, dp.Conv1.Weight, kSize, filter, 1, (kSize-1)/2, dp.Conv1.Bias)
	ReLU(y)
	applyLayerNormCT(y, filter, T, dp.Norm1.Weight, dp.Norm1.Bias)

	// Conv2 → ReLU → LayerNorm
	kSize2 := dp.Conv2.KSize
	if kSize2 == 0 {
		kSize2 = 3
	}
	y = Conv1D(y, filter, T, dp.Conv2.Weight, kSize2, filter, 1, (kSize2-1)/2, dp.Conv2.Bias)
	ReLU(y)
	applyLayerNormCT(y, filter, T, dp.Norm2.Weight, dp.Norm2.Bias)

	// Proj: [filter, T] → [1, T]
	logw := Conv1D(y, filter, T, dp.Proj.Weight, 1, 1, 1, 0, dp.Proj.Bias)
	return logw
}

// ComputeDurations converts log-durations to integer durations.
// logw: [1, T] or [T]
// lengthScale: controls speed (1.0 = normal, >1 = slower, <1 = faster)
// Returns: durations [T] and total mel frames.
func ComputeDurations(logw []float32, lengthScale float32) (durations []int, totalMel int) {
	T := len(logw)
	durations = make([]int, T)
	for t := 0; t < T; t++ {
		w := float32(math.Exp(float64(logw[t]))) * lengthScale
		d := int(math.Ceil(float64(w)))
		if d < 0 {
			d = 0
		}
		durations[t] = d
		totalMel += d
	}
	if totalMel == 0 {
		totalMel = 1
		if T > 0 {
			durations[0] = 1
		}
	}
	return durations, totalMel
}

// ExpandByDurations expands [C, T_text] to [C, T_mel] using the alignment matrix.
// path: [T_mel, T_text] from GeneratePath
// src: [C, T_text]
// Returns: [C, T_mel]
func ExpandByDurations(src []float32, C, tText int, path []float32, tMel int) []float32 {
	// out = src @ path^T  →  [C, T_text] @ [T_text, T_mel] = [C, T_mel]
	// But path is [T_mel, T_text], so we need path^T = [T_text, T_mel]
	out := make([]float32, C*tMel)
	for c := 0; c < C; c++ {
		for tm := 0; tm < tMel; tm++ {
			var sum float32
			for tt := 0; tt < tText; tt++ {
				sum += src[c*tText+tt] * path[tm*tText+tt]
			}
			out[c*tMel+tm] = sum
		}
	}
	return out
}

// applyLayerNormCT is defined in text_encoder.go
