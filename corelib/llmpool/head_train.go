package llmpool

import (
	"fmt"
	"math"
	"time"
)

// LabeledEmbedding is one training row. Label must be a frozen class.
type LabeledEmbedding struct {
	Embedding []float32
	Label     string
}

func EmptyHead(version int, tau float64) ClassificationHead {
	weights := make([][]float64, len(FrozenWorkloadClasses))
	for i := range weights {
		weights[i] = make([]float64, HeadDim)
	}
	if tau <= 0 || tau >= 1 {
		tau = DefaultHeadTau
	}
	return ClassificationHead{
		Version:   version,
		Weights:   weights,
		Bias:      make([]float64, len(FrozenWorkloadClasses)),
		Tau:       tau,
		TrainedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func classIndex(label string) int {
	label = NormalizeWorkloadClass(label)
	for i, class := range FrozenWorkloadClasses {
		if class == label {
			return i
		}
	}
	return -1
}

// TrainClassificationHead fits softmax W,b with a few SGD epochs.
// It does not load Gemma; the caller embeds first.
func TrainClassificationHead(samples []LabeledEmbedding, version int, tau float64) (ClassificationHead, error) {
	usable := make([]LabeledEmbedding, 0, len(samples))
	for _, sample := range samples {
		if classIndex(sample.Label) < 0 || len(sample.Embedding) < HeadDim {
			continue
		}
		usable = append(usable, sample)
	}
	if len(usable) == 0 {
		return ClassificationHead{}, fmt.Errorf("no labeled embeddings to train")
	}
	head := EmptyHead(version, tau)
	lr := 0.2
	epochs := 60
	nClass := len(FrozenWorkloadClasses)
	for epoch := 0; epoch < epochs; epoch++ {
		for _, sample := range usable {
			yi := classIndex(sample.Label)
			logits := make([]float64, nClass)
			maxLogit := math.Inf(-1)
			for i := 0; i < nClass; i++ {
				sum := head.Bias[i]
				row := head.Weights[i]
				for j := 0; j < HeadDim; j++ {
					sum += row[j] * float64(sample.Embedding[j])
				}
				logits[i] = sum
				if sum > maxLogit {
					maxLogit = sum
				}
			}
			var denom float64
			probs := make([]float64, nClass)
			for i := 0; i < nClass; i++ {
				probs[i] = math.Exp(logits[i] - maxLogit)
				denom += probs[i]
			}
			if denom == 0 {
				continue
			}
			for i := 0; i < nClass; i++ {
				probs[i] /= denom
				grad := probs[i]
				if i == yi {
					grad -= 1
				}
				head.Bias[i] -= lr * grad
				row := head.Weights[i]
				for j := 0; j < HeadDim; j++ {
					row[j] -= lr * grad * float64(sample.Embedding[j])
				}
			}
		}
	}
	head.TrainedAt = time.Now().UTC().Format(time.RFC3339)
	return head, nil
}
