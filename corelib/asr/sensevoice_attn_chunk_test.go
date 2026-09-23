package asr

import (
	"math"
	"testing"
)

// TestSVAttnChunkBitIdentical verifies that evaluating query rows in tile-aligned
// chunks via svAttnScoresPackedQ128NSRange is bit-identical to the full-range
// kernel, for the frame counts the encoder actually produces.
func TestSVAttnChunkBitIdentical(t *testing.T) {
	for _, frames := range []int{16, 17, 23, 32, 33, 47, 97, 98, 120} {
		const heads, headDim, hidden = 4, 128, 512
		q := make([]float32, heads*frames*headDim)
		k := make([]float32, heads*frames*headDim)
		v := make([]float32, heads*frames*headDim)
		for i := range q {
			q[i] = float32((i%17)-8) * 0.01
			k[i] = float32((i%13)-6) * 0.02
			v[i] = float32((i%11)-5) * 0.03
		}
		scores := make([]float32, 8*frames)
		for C := 1; C <= 6; C++ {
			chunk := ((frames/C + 7) / 8) * 8
			for h := 0; h < heads; h++ {
				base := h * frames * headDim
				qPack := q[base : base+frames*headDim]
				kPack := k[base : base+frames*headDim]
				vPack := v[base : base+frames*headDim]
				outFull := make([]float32, frames*hidden)
				svAttnScoresPackedQ128NS(outFull, qPack, kPack, vPack, scores, frames, hidden, h*headDim)
				outChunk := make([]float32, frames*hidden)
				for c := 0; c < C; c++ {
					qf0 := c * chunk
					qf1 := qf0 + chunk
					if c == C-1 || qf1 > frames {
						qf1 = frames
					}
					if qf0 >= qf1 {
						continue
					}
					svAttnScoresPackedQ128NSRange(outChunk, qPack, kPack, vPack, scores, qf0, qf1, frames, hidden, h*headDim)
				}
				for i := range outFull {
					if math.Float32bits(outFull[i]) != math.Float32bits(outChunk[i]) {
						t.Fatalf("frames=%d C=%d h=%d idx=%d: full=%v chunk=%v", frames, C, h, i, outFull[i], outChunk[i])
					}
				}
			}
		}
	}
}
