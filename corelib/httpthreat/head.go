package httpthreat

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"
)

type Head struct {
	Version   int         `json:"version"`
	Weights   [][]float64 `json:"weights"`
	Bias      []float64   `json:"bias"`
	Tau       float64     `json:"tau"`
	TrainedAt string      `json:"trained_at,omitempty"`
	Sig       string      `json:"sig,omitempty"`
	hash      atomic.Value
}

type Prediction struct {
	Class string             `json:"class"`
	MaxP  float64            `json:"max_p"`
	Probs map[string]float64 `json:"probs,omitempty"`
}

func EmptyHead(version int, tau float64) Head {
	w := make([][]float64, len(TrainableClasses))
	for i := range w {
		w[i] = make([]float64, HeadDim)
	}
	if tau <= 0 || tau >= 1 {
		tau = DefaultTau
	}
	return Head{Version: version, Weights: w, Bias: make([]float64, len(TrainableClasses)), Tau: tau}
}

func (h *Head) Ready() bool {
	if h == nil || len(h.Weights) != len(TrainableClasses) || len(h.Bias) != len(TrainableClasses) {
		return false
	}
	for i := range h.Weights {
		if len(h.Weights[i]) != HeadDim {
			return false
		}
	}
	return true
}

func (h *Head) EffectiveTau() float64 {
	if h == nil || h.Tau <= 0 || h.Tau >= 1 {
		return DefaultTau
	}
	return h.Tau
}

func (h *Head) Hash() string {
	if h == nil {
		return ""
	}
	if v, ok := h.hash.Load().(string); ok && v != "" {
		return v
	}
	wire := struct {
		Version   int         `json:"version"`
		Weights   [][]float64 `json:"weights"`
		Bias      []float64   `json:"bias"`
		Tau       float64     `json:"tau"`
		TrainedAt string      `json:"trained_at,omitempty"`
	}{h.Version, h.Weights, h.Bias, h.Tau, h.TrainedAt}
	b, _ := json.Marshal(wire)
	sum := sha256.Sum256(b)
	out := hex.EncodeToString(sum[:])
	h.hash.Store(out)
	return out
}

func SignHead(h *Head, key []byte) {
	if h == nil || len(key) == 0 {
		return
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(h.Hash()))
	h.Sig = hex.EncodeToString(mac.Sum(nil))
}

func VerifyHead(h *Head, key []byte) bool {
	if h == nil || !h.Ready() {
		return false
	}
	if len(key) == 0 {
		return true
	}
	if strings.TrimSpace(h.Sig) == "" {
		return false
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(h.Hash()))
	want, err := hex.DecodeString(h.Sig)
	if err != nil {
		return false
	}
	return hmac.Equal(mac.Sum(nil), want)
}

func (h *Head) Clone() *Head {
	if h == nil {
		return nil
	}
	out := *h
	out.Weights = make([][]float64, len(h.Weights))
	for i, row := range h.Weights {
		cp := make([]float64, len(row))
		copy(cp, row)
		out.Weights[i] = cp
	}
	out.Bias = append([]float64(nil), h.Bias...)
	out.Sig = h.Sig
	return &out
}

func classIndex(label string) int {
	for i, c := range TrainableClasses {
		if c == label {
			return i
		}
	}
	return -1
}

func (h *Head) Predict(emb []float32) Prediction {
	out := Prediction{Class: ClassUnknown, Probs: map[string]float64{}}
	if !h.Ready() || len(emb) < HeadDim {
		return out
	}
	logits := make([]float64, len(TrainableClasses))
	maxLogit := math.Inf(-1)
	for i := range TrainableClasses {
		sum := h.Bias[i]
		row := h.Weights[i]
		for j := 0; j < HeadDim; j++ {
			sum += row[j] * float64(emb[j])
		}
		logits[i] = sum
		if sum > maxLogit {
			maxLogit = sum
		}
	}
	var denom float64
	exps := make([]float64, len(logits))
	for i, z := range logits {
		exps[i] = math.Exp(z - maxLogit)
		denom += exps[i]
	}
	if denom == 0 {
		return out
	}
	best := 0
	for i, class := range TrainableClasses {
		p := exps[i] / denom
		out.Probs[class] = p
		if p > out.Probs[TrainableClasses[best]] {
			best = i
		}
	}
	out.Class = TrainableClasses[best]
	out.MaxP = out.Probs[out.Class]
	if out.MaxP < h.EffectiveTau() {
		out.Class = ClassUnknown
	}
	return out
}

type labeledEmb struct {
	Emb    []float32
	Label  string
	Weight float64
}

func TrainHead(samples []labeledEmb, version int, tau float64, trainedAt string) (Head, error) {
	usable := make([]labeledEmb, 0, len(samples))
	counts := map[string]int{}
	for _, s := range samples {
		if classIndex(s.Label) < 0 || len(s.Emb) < HeadDim {
			continue
		}
		usable = append(usable, s)
		counts[s.Label]++
	}
	if len(usable) == 0 {
		return Head{}, fmt.Errorf("no labeled embeddings to train")
	}
	maxN := 0
	for _, n := range counts {
		if n > maxN {
			maxN = n
		}
	}
	head := EmptyHead(version, tau)
	lr := 0.2
	epochs := 60
	nClass := len(TrainableClasses)
	for epoch := 0; epoch < epochs; epoch++ {
		for _, sample := range usable {
			yi := classIndex(sample.Label)
			weight := sample.Weight
			if weight <= 0 {
				weight = 1.0
			}
			if c := counts[sample.Label]; c > 0 && maxN > 0 {
				cr := float64(maxN) / float64(c)
				if cr > 8 {
					cr = 8
				}
				weight *= cr
			}
			logits := make([]float64, nClass)
			maxLogit := math.Inf(-1)
			for i := 0; i < nClass; i++ {
				sum := head.Bias[i]
				row := head.Weights[i]
				for j := 0; j < HeadDim; j++ {
					sum += row[j] * float64(sample.Emb[j])
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
				grad := (probs[i] - bool01(i == yi)) * weight
				head.Bias[i] -= lr * grad
				row := head.Weights[i]
				for j := 0; j < HeadDim; j++ {
					row[j] -= lr * grad * float64(sample.Emb[j])
				}
			}
		}
	}
	if strings.TrimSpace(trainedAt) == "" {
		trainedAt = time.Now().UTC().Format(time.RFC3339)
	}
	head.TrainedAt = trainedAt
	return head, nil
}

func bool01(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
