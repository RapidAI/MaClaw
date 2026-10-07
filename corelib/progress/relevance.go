package progress

// relevance.go — embedder-specific interrupt relevance (task ↔ new message).
//
// The scheduler's bands (high ≥ 0.60, low < 0.30) were set on EmbeddingGemma's
// raw cosine scale. A RelevanceModel computes a model-specific raw score —
// cosine of instruction-prompted vectors, or a tiny logistic scorer over
// [u, v, |u−v|, u⊙v, cos] — and maps it piecewise-linearly onto the
// scheduler scale so that raw Low → 0.30 and raw High → 0.60. Schedule()
// itself is unchanged; a nil model keeps the legacy plain-cosine relevance.
//
// File format "maclaw-interrupt-relevance-v1" (JSON):
//
//	{"format":"maclaw-interrupt-relevance-v1","kind":"cosine"|"logistic",
//	 "embedder_model_id":"Qwen3 Embedding 0.6b:1024:...","dims":1024,
//	 "task_instruction":"...","message_instruction":"...",
//	 "w":[4*dims+1 floats, logistic only],"b":0.0,"high":0.71,"low":0.42}
//
// Instructions use the Qwen3 instruct format "Instruct: {task}\nQuery: {text}"
// (empty → plain text). Pure Go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
)

// RelevanceModelFormat is the only accepted file format.
const RelevanceModelFormat = "maclaw-interrupt-relevance-v1"

// Scheduler-scale anchors the raw thresholds map onto.
const (
	relevanceScaleLow  = lowRelevanceThreshold  // 0.30
	relevanceScaleHigh = highRelevanceThreshold // 0.60
)

// RelevanceModel scores task ↔ message relevance for one embedder family.
type RelevanceModel struct {
	Format             string    `json:"format"`
	Kind               string    `json:"kind"`
	EmbedderModelID    string    `json:"embedder_model_id"`
	Dims               int       `json:"dims"`
	TaskInstruction    string    `json:"task_instruction"`
	MessageInstruction string    `json:"message_instruction"`
	W                  []float64 `json:"w,omitempty"`
	B                  float64   `json:"b,omitempty"`
	High               float64   `json:"high"`
	Low                float64   `json:"low"`
	Note               string    `json:"note,omitempty"`
}

// ErrRelevanceDim reports vectors that do not match the model width.
var ErrRelevanceDim = errors.New("relevance model: vector width mismatch")

// LoadRelevanceModel reads and validates a model file.
func LoadRelevanceModel(path string) (*RelevanceModel, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := ParseRelevanceModel(b)
	if err != nil {
		return nil, fmt.Errorf("relevance model %s: %w", path, err)
	}
	return m, nil
}

// ParseRelevanceModel validates a model document.
func ParseRelevanceModel(b []byte) (*RelevanceModel, error) {
	var m RelevanceModel
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

// Validate checks format, kind, thresholds and weight shape.
func (m *RelevanceModel) Validate() error {
	if m == nil {
		return errors.New("nil relevance model")
	}
	if m.Format != RelevanceModelFormat {
		return fmt.Errorf("format %q, want %q", m.Format, RelevanceModelFormat)
	}
	if !(m.High > m.Low) || math.IsNaN(m.High) || math.IsNaN(m.Low) {
		return fmt.Errorf("need high > low (high=%v low=%v)", m.High, m.Low)
	}
	switch m.Kind {
	case "cosine":
	case "logistic":
		if m.Dims <= 0 || len(m.W) != 4*m.Dims+1 {
			return fmt.Errorf("logistic: len(w)=%d, want 4*dims+1=%d", len(m.W), 4*m.Dims+1)
		}
		for _, w := range m.W {
			if math.IsNaN(w) || math.IsInf(w, 0) {
				return errors.New("logistic: non-finite weight")
			}
		}
	default:
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	return nil
}

func applyInstruction(inst, text string) string {
	if inst == "" {
		return text
	}
	return "Instruct: " + inst + "\nQuery: " + text
}

// FormatTask returns the text to embed for the running task description.
func (m *RelevanceModel) FormatTask(text string) string {
	return applyInstruction(m.TaskInstruction, text)
}

// FormatMessage returns the text to embed for the new user message.
func (m *RelevanceModel) FormatMessage(text string) string {
	return applyInstruction(m.MessageInstruction, text)
}

// RawScore is the model's uncalibrated score (cosine, or logistic
// probability) for task vector u and message vector v.
func (m *RelevanceModel) RawScore(u, v []float32) (float64, error) {
	if len(u) == 0 || len(u) != len(v) || (m.Dims > 0 && len(u) != m.Dims) {
		return 0, ErrRelevanceDim
	}
	cos := CosineSimilarity(u, v)
	if m.Kind == "cosine" {
		return cos, nil
	}
	// Logistic on L2-normalised u, v: [u, v, |u−v|, u⊙v, cos].
	var nu, nv float64
	for i := range u {
		nu += float64(u[i]) * float64(u[i])
		nv += float64(v[i]) * float64(v[i])
	}
	if nu == 0 || nv == 0 {
		return 0, nil
	}
	iu, iv := 1/math.Sqrt(nu), 1/math.Sqrt(nv)
	d := len(u)
	z := m.B + m.W[4*d]*cos
	for i := 0; i < d; i++ {
		a, b := float64(u[i])*iu, float64(v[i])*iv
		z += m.W[i]*a + m.W[d+i]*b + m.W[2*d+i]*math.Abs(a-b) + m.W[3*d+i]*a*b
	}
	return 1 / (1 + math.Exp(-z)), nil
}

// Calibrate maps a raw score onto the scheduler scale (Low → 0.30,
// High → 0.60, linear, clamped to [0, 1]).
func (m *RelevanceModel) Calibrate(raw float64) float64 {
	r := relevanceScaleLow + (raw-m.Low)*(relevanceScaleHigh-relevanceScaleLow)/(m.High-m.Low)
	return math.Max(0, math.Min(1, r))
}

// Relevance returns the scheduler-scale relevance, or -1 (unavailable) on a
// width mismatch so Schedule degrades to its two-signal mode.
func (m *RelevanceModel) Relevance(u, v []float32) float64 {
	raw, err := m.RawScore(u, v)
	if err != nil {
		return -1
	}
	return m.Calibrate(raw)
}
