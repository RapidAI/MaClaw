package httpthreat

import (
	"crypto/sha256"
	"math"
	"strconv"
)

const DefaultEncoderID = "hash-v1"

// HashEmbed is a frozen deterministic encoder: sha256 tiles -> 256-d L2 vector.
// Swap for a real local embedder by passing EmbedFunc into NewEngine.
func HashEmbed(preview string) ([]float32, error) {
	out := make([]float32, HeadDim)
	for i := 0; i < HeadDim; i += 32 {
		sum := sha256.Sum256([]byte(preview + "\n" + strconv.Itoa(i)))
		for j := 0; j < 32 && i+j < HeadDim; j++ {
			out[i+j] = (float32(sum[j]) / 127.5) - 1
		}
	}
	var norm float64
	for _, v := range out {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		scale := float32(math.Sqrt(norm))
		for i := range out {
			out[i] /= scale
		}
	}
	return out, nil
}
