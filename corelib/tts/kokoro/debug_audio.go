package kokoro

import "math"

func PCMFromConvPost(post []float32, frames int) []float32 {
	bins := 11
	mag := make([]float32, bins*frames)
	phase := make([]float32, bins*frames)
	for b := 0; b < bins; b++ {
		for t := 0; t < frames; t++ {
			mag[b*frames+t] = float32(math.Exp(float64(post[b*frames+t])))
			phase[b*frames+t] = float32(math.Sin(float64(post[(bins+b)*frames+t])))
		}
	}
	return istft(mag, phase, frames, 20, 5)
}
