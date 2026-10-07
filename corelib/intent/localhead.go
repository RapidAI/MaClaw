package intent

// localhead.go — production inference for the calibrated local intent head.
//
// The head is a small classifier (optional standardisation, at most one ReLU
// hidden layer, softmax with a calibrated temperature) trained offline on
// frozen Layer 2 query vectors. It is the fully local candidate replacement
// for the remote Layer 3 tree classifier. This file only loads and runs it;
// it is not wired into routing. Shadow mode (shadow.go) runs it next to the
// remote L3 and logs both verdicts.
//
// JSON format (written by maclaw_reranker/scripts/train_cls_heads.py):
//
//	{"labels": [...C], "w1": [[...D]...H], "b1": [...H], "w2": [[...H]...C],
//	 "b2": [...C], "mean": [...D]?, "scale": [...D]?, "l2norm": bool,
//	 "temperature": T, "tau": τ, "tau_s": τ_s?, "sensitive": [...],
//	 "embed_role": "classification", "note": "..."}
//
// w1/b1 empty means a linear head. Pure Go, no allocation beyond two
// scratch slices per call, safe for concurrent use after load.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
)

// Local head decisions under τ / τ_s.
const (
	LocalHeadAccept    = "accept"    // top-1 is a capability label at or above its threshold
	LocalHeadAmbiguous = "ambiguous" // top-1 below its threshold: the head abstains
	LocalHeadUnknown   = "unknown"   // top-1 is "unknown" (chit-chat) at or above τ
)

// LocalHead is a loaded, validated classification head. Immutable after
// LoadLocalHead; safe for concurrent Predict calls.
type LocalHead struct {
	ID        string   // file base name + "@" + first 12 hex of the file's sha256
	Labels    []string // class labels, index-aligned with the probabilities
	Tau       float64  // global acceptance threshold
	TauS      float64  // threshold for sensitive labels (≥ Tau; == Tau when unset)
	EmbedRole string   // "classification" = trained on Layer 2 prompted vectors
	Note      string
	// EmbedModel is the embedder family the head was trained on ("qwen3" /
	// "gemma"), from the optional "embed_model" JSON field; "" = infer.
	EmbedModel string
	// CalibrationSource is "calibration.source" from the JSON ("" for heads
	// fitted offline; "human_labels" for heads re-fitted from shadow logs by
	// scripts/intent_shadow_recalibrate.py).
	CalibrationSource string
	// CalibrationCompute is "calibration.compute": the embedder compute mode
	// (embedding.EmbedderComputeMode, e.g. q8p:avx2:1pass) the head's T/τ/τ_s
	// were fitted under; "" = unknown (offline heads, fitted on float/2-pass).
	CalibrationCompute string

	// AnchorFeatureLabels, when non-empty, means the head's input is the query
	// vector followed by the raw Layer 2 max anchor cosine of each of these
	// labels (optional "anchor_features" JSON field; Qwen3 r4 heads only).
	AnchorFeatureLabels []string

	sensitive map[string]bool
	dim       int
	hidden    int
	l2norm    bool
	temp      float64
	mean      []float64
	invScale  []float64
	w1        []float64 // [hidden*dim] row-major
	b1        []float64
	w2        []float64 // [C*in] row-major, in = hidden or dim
	b2        []float64
}

type localHeadFile struct {
	Labels      []string    `json:"labels"`
	Mean        []float64   `json:"mean"`
	Scale       []float64   `json:"scale"`
	W1          [][]float64 `json:"w1"`
	B1          []float64   `json:"b1"`
	W2          [][]float64 `json:"w2"`
	B2          []float64   `json:"b2"`
	Temperature float64     `json:"temperature"`
	Tau         float64     `json:"tau"`
	TauS        float64     `json:"tau_s"`
	L2Norm      bool        `json:"l2norm"`
	Sensitive   []string    `json:"sensitive"`
	EmbedRole   string      `json:"embed_role"`
	Note        string      `json:"note"`
	EmbedModel  string      `json:"embed_model"`
	// EmbedderModelID is the trainer's full embedder ID (e.g. "Qwen3
	// Embedding 0.6b:1024:prompt-v1:qwen3-instruct-v1"); used as the family
	// hint when embed_model is absent.
	EmbedderModelID string `json:"embedder_model_id"`
	// Calibration is either {"source":..,"compute":..} or a free-text
	// description string (cross-family trainer heads, 2026-10-07): a string
	// used to fail the whole load with an unmarshal error.
	Calibration localHeadCalibration `json:"calibration"`
	// AnchorFeatures (optional): labels whose raw L2 max anchor cosines are
	// appended to the query vector, in this order. Standardisation is folded
	// into w1/b1 at export time.
	AnchorFeatures *struct {
		Labels []string `json:"labels"`
	} `json:"anchor_features,omitempty"`
}

type localHeadCalibration struct {
	Source  string `json:"source"`
	Compute string `json:"compute"`
}

func (c *localHeadCalibration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		c.Source = s
		return nil
	}
	type plain localHeadCalibration
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*c = localHeadCalibration(p)
	return nil
}

// LoadLocalHead reads and validates a head JSON file.
func LoadLocalHead(path string) (*LocalHead, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	h, err := ParseLocalHead(b)
	if err != nil {
		return nil, fmt.Errorf("local head %s: %w", path, err)
	}
	h.ID = filepath.Base(path) + "@" + hex.EncodeToString(sum[:])[:12]
	return h, nil
}

// ParseLocalHead validates a head from its JSON bytes (ID is left empty).
func ParseLocalHead(b []byte) (*LocalHead, error) {
	var f localHeadFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	c := len(f.Labels)
	if c < 2 {
		return nil, errors.New("need at least 2 labels")
	}
	if len(f.W2) != c || len(f.B2) != c {
		return nil, fmt.Errorf("w2/b2 rows %d/%d != labels %d", len(f.W2), len(f.B2), c)
	}
	in := len(f.W2[0])
	if in == 0 {
		return nil, errors.New("empty w2")
	}
	h := &LocalHead{
		Labels:             append([]string(nil), f.Labels...),
		Tau:                f.Tau,
		TauS:               f.TauS,
		EmbedRole:          f.EmbedRole,
		Note:               f.Note,
		EmbedModel:         firstNonEmpty(f.EmbedModel, f.EmbedderModelID),
		CalibrationSource:  f.Calibration.Source,
		CalibrationCompute: f.Calibration.Compute,
		sensitive:          map[string]bool{},
		l2norm:             f.L2Norm,
		temp:               f.Temperature,
		b2:                 append([]float64(nil), f.B2...),
	}
	if h.temp <= 0 || math.IsNaN(h.temp) {
		h.temp = 1
	}
	if h.TauS < h.Tau {
		h.TauS = h.Tau
	}
	for _, s := range f.Sensitive {
		h.sensitive[s] = true
	}
	if len(f.W1) > 0 {
		h.hidden = len(f.W1)
		h.dim = len(f.W1[0])
		if len(f.B1) != h.hidden || in != h.hidden {
			return nil, fmt.Errorf("hidden layer shape mismatch (w1 %d, b1 %d, w2 in %d)", h.hidden, len(f.B1), in)
		}
		h.w1 = make([]float64, 0, h.hidden*h.dim)
		for j, row := range f.W1 {
			if len(row) != h.dim {
				return nil, fmt.Errorf("w1 row %d has %d cols, want %d", j, len(row), h.dim)
			}
			h.w1 = append(h.w1, row...)
		}
		h.b1 = append([]float64(nil), f.B1...)
	} else {
		h.dim = in
	}
	h.w2 = make([]float64, 0, c*in)
	for k, row := range f.W2 {
		if len(row) != in {
			return nil, fmt.Errorf("w2 row %d has %d cols, want %d", k, len(row), in)
		}
		h.w2 = append(h.w2, row...)
	}
	if len(f.Mean) > 0 || len(f.Scale) > 0 {
		if len(f.Mean) != h.dim || len(f.Scale) != h.dim {
			return nil, fmt.Errorf("mean/scale length %d/%d != dim %d", len(f.Mean), len(f.Scale), h.dim)
		}
		h.mean = append([]float64(nil), f.Mean...)
		h.invScale = make([]float64, h.dim)
		for i, s := range f.Scale {
			if s == 0 {
				s = 1
			}
			h.invScale[i] = 1 / s
		}
	}
	for _, v := range h.w2 {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("non-finite weight in w2")
		}
	}
	if f.AnchorFeatures != nil && len(f.AnchorFeatures.Labels) > 0 {
		if h.mean != nil {
			return nil, errors.New("anchor_features with mean/scale is not supported")
		}
		if len(f.AnchorFeatures.Labels) >= h.dim {
			return nil, fmt.Errorf("anchor_features %d >= input dim %d", len(f.AnchorFeatures.Labels), h.dim)
		}
		h.AnchorFeatureLabels = append([]string(nil), f.AnchorFeatures.Labels...)
	}
	return h, nil
}

// Dim is the query-vector (embedding) dimension the head expects; for heads
// with anchor features the network input is Dim()+len(AnchorFeatureLabels).
func (h *LocalHead) Dim() int { return h.dim - len(h.AnchorFeatureLabels) }

// NeedsAnchorScores reports whether Predict needs Layer 2 anchor cosines
// (use PredictWithAnchors).
func (h *LocalHead) NeedsAnchorScores() bool { return len(h.AnchorFeatureLabels) > 0 }

// PredictWithAnchors is Predict for heads with anchor features: v is the
// query vector, scores the raw per-label max anchor cosines of the Layer 2
// pass. Heads without anchor features ignore scores.
func (h *LocalHead) PredictWithAnchors(v []float32, scores map[string]float64) ([]float64, error) {
	if !h.NeedsAnchorScores() {
		return h.Predict(v)
	}
	ed := h.Dim()
	if len(v) != ed {
		return nil, fmt.Errorf("local head: vector dim %d != head dim %d", len(v), ed)
	}
	x := make([]float64, h.dim)
	var n float64
	for i, f := range v {
		x[i] = float64(f)
		n += x[i] * x[i]
	}
	if h.l2norm && n > 0 {
		inv := 1 / math.Sqrt(n)
		for i := 0; i < ed; i++ {
			x[i] *= inv
		}
	}
	for j, l := range h.AnchorFeatureLabels {
		s, ok := scores[l]
		if !ok {
			return nil, fmt.Errorf("local head: missing L2 anchor score for %q", l)
		}
		x[ed+j] = s
	}
	return h.forward(x), nil
}

// IsSensitive reports whether label uses τ_s.
func (h *LocalHead) IsSensitive(label string) bool { return h.sensitive[label] }

// Threshold returns the acceptance threshold for label (τ_s for sensitive labels).
func (h *LocalHead) Threshold(label string) float64 {
	if h.sensitive[label] {
		return h.TauS
	}
	return h.Tau
}

// Predict returns calibrated class probabilities for one query vector
// (same space and dimension the head was trained on).
func (h *LocalHead) Predict(v []float32) ([]float64, error) {
	if h.NeedsAnchorScores() {
		return nil, errors.New("local head: needs L2 anchor scores (PredictWithAnchors)")
	}
	if len(v) != h.dim {
		return nil, fmt.Errorf("local head: vector dim %d != head dim %d", len(v), h.dim)
	}
	x := make([]float64, h.dim)
	var n float64
	for i, f := range v {
		x[i] = float64(f)
		n += x[i] * x[i]
	}
	if h.l2norm && n > 0 {
		inv := 1 / math.Sqrt(n)
		for i := range x {
			x[i] *= inv
		}
	}
	if h.mean != nil {
		for i := range x {
			x[i] = (x[i] - h.mean[i]) * h.invScale[i]
		}
	}
	return h.forward(x), nil
}

// forward runs the network on the prepared input x (len h.dim).
func (h *LocalHead) forward(x []float64) []float64 {
	if h.hidden > 0 {
		hid := make([]float64, h.hidden)
		for j := 0; j < h.hidden; j++ {
			row := h.w1[j*h.dim : (j+1)*h.dim]
			s := h.b1[j]
			for i, w := range row {
				s += w * x[i]
			}
			if s > 0 {
				hid[j] = s
			}
		}
		x = hid
	}
	in := len(x)
	c := len(h.Labels)
	p := make([]float64, c)
	mx := math.Inf(-1)
	for k := 0; k < c; k++ {
		row := h.w2[k*in : (k+1)*in]
		s := h.b2[k]
		for i, w := range row {
			s += w * x[i]
		}
		s /= h.temp
		p[k] = s
		if s > mx {
			mx = s
		}
	}
	var z float64
	for k := range p {
		p[k] = math.Exp(p[k] - mx)
		z += p[k]
	}
	for k := range p {
		p[k] /= z
	}
	return p
}

// LocalHeadScore is one label with its calibrated probability.
type LocalHeadScore struct {
	Label string  `json:"label"`
	Prob  float64 `json:"p"`
}

// TopK returns the k most probable labels (descending).
func (h *LocalHead) TopK(probs []float64, k int) []LocalHeadScore {
	idx := make([]int, len(probs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return probs[idx[a]] > probs[idx[b]] })
	if k > len(idx) {
		k = len(idx)
	}
	out := make([]LocalHeadScore, k)
	for i := 0; i < k; i++ {
		out[i] = LocalHeadScore{Label: h.Labels[idx[i]], Prob: probs[idx[i]]}
	}
	return out
}

// Decide applies τ / τ_s to the top-1 label: accept, unknown (confident
// chit-chat) or ambiguous (abstain). threshold is the one that applied.
func (h *LocalHead) Decide(probs []float64) (label string, prob float64, decision string, threshold float64) {
	best := 0
	for i, p := range probs {
		if p > probs[best] {
			best = i
		}
	}
	label, prob = h.Labels[best], probs[best]
	threshold = h.Threshold(label)
	switch {
	case prob < threshold:
		decision = LocalHeadAmbiguous
	case label == string(LabelUnknown):
		decision = LocalHeadUnknown
	default:
		decision = LocalHeadAccept
	}
	return label, prob, decision, threshold
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
