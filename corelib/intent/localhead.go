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
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
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

	// InputBlocks, when non-empty (r13 F1: ["last","mean"]), means the head
	// input is the concatenation of these pooled views of one classification-
	// role forward pass, each L2-normalised on its own (optional
	// "input_blocks" JSON field). Use PredictPooled / HeadInputVector.
	InputBlocks []string

	sensitive map[string]bool
	dim       int
	hidden    int
	l2norm    bool
	temp      float64
	mean      []float64
	invScale  []float64
	// Weights are float32: forward() runs them through tensor.Dot (AVX-512 /
	// AVX2 / NEON), which measured ~8x faster than the float64 scalar loop for
	// the shipped r13 head (5.9M MACs: 4.27ms -> 0.51ms) and halves the
	// resident footprint. Probabilities shift by ~1e-6, far below any
	// calibrated threshold.
	w1       []float32 // [hidden*dim] row-major
	b1       []float32
	w2       []float32 // [C*in] row-major, in = last hidden width or dim
	extra    []denseLayer
	srcDim   int // >0: Matryoshka truncation from srcDim to dim
	tauLabel map[string]float64
	b2       []float32

	// Q8P fast path for the hidden layer. The 2783x2048 mat-vec is memory-bound
	// in float32 (22.8 MB streamed per prediction), so once the matrix no longer
	// fits in L3 the weights are quantised to int8 at load and multiplied with
	// the VNNI GEMM kernel: 4x less traffic, 1.44x end-to-end on the shipped r13
	// head over a 16 MB L3 (380us -> 264us). w1 is dropped once packed; b1 stays
	// float32 and is applied after the GEMM. MACLAW_HEAD_Q8P=0/1/2 forces off /
	// single / two-pass; the default is decided by headQ8PPolicy and then
	// verified against the float32 path at load (finishQ8PSetup), which falls
	// back to float32 when the deviation is too large.
	w1q       *tensor.Q8Packed
	padHidden int
	headQ8P2  bool
	// q8pPending marks an auto-selected int8 path whose numeric agreement with
	// float32 has not been checked yet. LoadLocalHead verifies it after w2/b2
	// are loaded (forward needs the whole network) and falls back to float32
	// when the deviation is too large.
	q8pPending bool

	// scratch reuses the forward-pass temporaries (~52 KB for the shipped r13
	// head) instead of allocating them per prediction. The probability
	// vector is NOT pooled: it is returned to the caller.
	scratch sync.Pool
}

// headScratch holds the reusable forward-pass temporaries.
type headScratch struct {
	xf     []float32 // input vector, float32
	hid    []float64 // hidden activations
	hf     []float32 // hidden activations, float32
	hidRaw []float32 // Q8P GEMM output (padded hidden width)
	resid  []float32 // Q8P two-pass residual
	act    tensor.Q8Act
	act2   tensor.Q8Act
}

func (h *LocalHead) getScratch(n int) *headScratch {
	pad := h.hidden
	if h.w1q != nil {
		pad = h.padHidden
	}
	if v := h.scratch.Get(); v != nil {
		s := v.(*headScratch)
		if cap(s.xf) >= n && cap(s.hid) >= h.hidden && cap(s.hf) >= h.hidden &&
			cap(s.hidRaw) >= pad && cap(s.resid) >= n {
			return s
		}
	}
	return &headScratch{
		xf:     make([]float32, n),
		hid:    make([]float64, h.hidden),
		hf:     make([]float32, h.hidden),
		hidRaw: make([]float32, pad),
		resid:  make([]float32, n),
	}
}

type localHeadFile struct {
	Labels []string    `json:"labels"`
	Mean   []float64   `json:"mean"`
	Scale  []float64   `json:"scale"`
	W1     [][]float64 `json:"w1"`
	B1     []float64   `json:"b1"`
	// Hidden is optional extra ReLU layers applied after w1/b1 (deeper
	// heads); w2 then takes the last layer's width as input.
	Hidden []struct {
		W [][]float64 `json:"w"`
		B []float64   `json:"b"`
	} `json:"hidden,omitempty"`
	W2 [][]float64 `json:"w2"`
	// SourceDim, when set, marks a Matryoshka head: the query vector has
	// SourceDim dims, the network uses its first len(w1[0]) dims, then
	// re-normalises them (l2norm must be true).
	SourceDim int `json:"source_dim,omitempty"`
	// TauByLabel is an optional per-label acceptance threshold map (fitted on
	// out-of-family OOF). Labels not listed use tau / tau_s; sensitive labels
	// never go below the tau_s in force.
	TauByLabel  map[string]float64 `json:"tau_by_label,omitempty"`
	B2          []float64          `json:"b2"`
	Temperature float64            `json:"temperature"`
	Tau         float64            `json:"tau"`
	TauS        float64            `json:"tau_s"`
	L2Norm      bool               `json:"l2norm"`
	Sensitive   []string           `json:"sensitive"`
	EmbedRole   string             `json:"embed_role"`
	Note        string             `json:"note"`
	EmbedModel  string             `json:"embed_model"`
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
	InputBlocks    []string `json:"input_blocks,omitempty"`
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
		CalibrationCompute: normalizeCalibrationCompute(f.Calibration.Compute),
		sensitive:          map[string]bool{},
		l2norm:             f.L2Norm,
		temp:               f.Temperature,
		b2:                 appendF32(nil, f.B2),
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
	if len(f.TauByLabel) > 0 {
		known := map[string]bool{}
		for _, l := range f.Labels {
			known[l] = true
		}
		h.tauLabel = map[string]float64{}
		for l, t := range f.TauByLabel {
			if !known[l] || !(t > 0 && t <= 1) {
				return nil, fmt.Errorf("tau_by_label: bad entry %q=%v", l, t)
			}
			h.tauLabel[l] = t
		}
	}
	if len(f.W1) > 0 {
		h.hidden = len(f.W1)
		h.dim = len(f.W1[0])
		last := h.hidden
		for li, L := range f.Hidden {
			if len(L.W) == 0 || len(L.B) != len(L.W) {
				return nil, fmt.Errorf("hidden[%d]: w rows %d, b %d", li, len(L.W), len(L.B))
			}
			d := denseLayer{out: len(L.W), in: last, b: append([]float64(nil), L.B...)}
			d.w = make([]float64, 0, d.out*d.in)
			for j, row := range L.W {
				if len(row) != last {
					return nil, fmt.Errorf("hidden[%d] row %d has %d cols, want %d", li, j, len(row), last)
				}
				for _, v := range row {
					if math.IsNaN(v) || math.IsInf(v, 0) {
						return nil, fmt.Errorf("non-finite weight in hidden[%d]", li)
					}
				}
				d.w = append(d.w, row...)
			}
			h.extra = append(h.extra, d)
			last = d.out
		}
		if len(f.B1) != h.hidden || in != last {
			return nil, fmt.Errorf("hidden layer shape mismatch (w1 %d, b1 %d, last hidden %d, w2 in %d)", h.hidden, len(f.B1), last, in)
		}
		h.w1 = make([]float32, 0, h.hidden*h.dim)
		for j, row := range f.W1 {
			if len(row) != h.dim {
				return nil, fmt.Errorf("w1 row %d has %d cols, want %d", j, len(row), h.dim)
			}
			for _, v := range row {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					return nil, fmt.Errorf("non-finite weight in w1")
				}
			}
			h.w1 = appendF32(h.w1, row)
		}
		h.b1 = appendF32(nil, f.B1)
		h.setupHeadQ8P()
	} else {
		if len(f.Hidden) > 0 {
			return nil, errors.New("hidden layers need w1/b1")
		}
		h.dim = in
	}
	h.w2 = make([]float32, 0, c*in)
	for k, row := range f.W2 {
		if len(row) != in {
			return nil, fmt.Errorf("w2 row %d has %d cols, want %d", k, len(row), in)
		}
		if err := checkFinite(row); err != nil {
			return nil, fmt.Errorf("w2 row %d: %w", k, err)
		}
		h.w2 = appendF32(h.w2, row)
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
	if f.SourceDim > 0 && f.SourceDim != h.dim {
		if f.SourceDim < h.dim || !h.l2norm || h.mean != nil || (f.AnchorFeatures != nil && len(f.AnchorFeatures.Labels) > 0) {
			return nil, fmt.Errorf("source_dim %d needs source_dim > input dim %d, l2norm, no mean/scale, no anchor_features", f.SourceDim, h.dim)
		}
		h.srcDim = f.SourceDim
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
	if len(f.InputBlocks) > 0 {
		seen := map[string]bool{}
		for _, b := range f.InputBlocks {
			if (b != "last" && b != "mean") || seen[b] {
				return nil, fmt.Errorf("input_blocks: bad or repeated block %q", b)
			}
			seen[b] = true
		}
		if h.dim%len(f.InputBlocks) != 0 || h.mean != nil || h.srcDim > 0 || len(h.AnchorFeatureLabels) > 0 {
			return nil, fmt.Errorf("input_blocks %v: input dim %d must split evenly; no mean/scale, source_dim or anchor_features", f.InputBlocks, h.dim)
		}
		h.InputBlocks = append([]string(nil), f.InputBlocks...)
	}
	h.finishQ8PSetup()
	return h, nil
}

// NeedsPooling reports whether the head input includes the mean-pooled view
// (embedding.PooledEmbedder required).
func (h *LocalHead) NeedsPooling() bool {
	for _, b := range h.InputBlocks {
		if b == "mean" {
			return true
		}
	}
	return false
}

// Dim is the query-vector (embedding) dimension the head expects; for heads
// with anchor features the network input is Dim()+len(AnchorFeatureLabels).
func (h *LocalHead) Dim() int {
	if len(h.InputBlocks) > 0 {
		return h.dim / len(h.InputBlocks)
	}
	if h.srcDim > 0 {
		return h.srcDim
	}
	return h.dim - len(h.AnchorFeatureLabels)
}

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
	t, ok := h.tauLabel[label]
	if h.sensitive[label] {
		if ok && t > h.TauS {
			return t
		}
		return h.TauS
	}
	if ok {
		return t
	}
	return h.Tau
}

// Predict returns calibrated class probabilities for one query vector
// (same space and dimension the head was trained on).
func (h *LocalHead) Predict(v []float32) ([]float64, error) {
	if h.NeedsAnchorScores() {
		return nil, errors.New("local head: needs L2 anchor scores (PredictWithAnchors)")
	}
	if h.srcDim > 0 {
		if len(v) != h.srcDim {
			return nil, fmt.Errorf("local head: vector dim %d != head source dim %d", len(v), h.srcDim)
		}
		v = v[:h.dim] // Matryoshka prefix; re-normalised below (l2norm required)
	}
	if len(v) != h.dim {
		return nil, fmt.Errorf("local head: vector dim %d != head dim %d", len(v), h.dim)
	}
	if len(h.InputBlocks) > 0 {
		return h.forward(blockNormalize(v, len(h.InputBlocks), h.l2norm)), nil
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
// appendF32 appends src as float32 to dst.
func appendF32(dst []float32, src []float64) []float32 {
	for _, v := range src {
		dst = append(dst, float32(v))
	}
	return dst
}

// checkFinite rejects NaN/Inf weights, so a corrupt head fails at load instead
// of poisoning every later prediction.
func checkFinite(row []float64) error {
	for _, v := range row {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("non-finite weight")
		}
	}
	return nil
}

// float32s converts a vector for the SIMD kernels.
func float32s(x []float64) []float32 {
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = float32(v)
	}
	return out
}

// headQ8PPolicy decides whether the hidden layer uses the int8 VNNI path and
// whether to spend the second activation pass.
//
// MACLAW_HEAD_Q8P forces a mode; "auto" (the default) decides from the weight
// footprint against the last-level cache:
//
//	w1 bytes > L3  -> int8, two-pass
//
// The float32 hidden matrix is streamed once per prediction, so its size
// relative to L3 is what decides. While it fits, the float32 AVX2 dot runs at
// cache speed and int8 only buys ~1.1x while costing probability accuracy. Once
// it does not fit, every prediction re-reads the matrix from DRAM and the 4x
// smaller footprint dominates: measured on a 16 MB L3 desktop with the 21.7 MB
// r13 head, single head, block-timed across processes, float32 380us vs int8
// two-pass 264us (1.44x, max probability delta 2.8e-3). An unknown cache size
// keeps float32: never trade accuracy without knowing the hardware.
func headQ8PPolicy(w1Bytes int64) (use, twoPass, forced bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_HEAD_Q8P"))) {
	case "0", "off", "false":
		return false, false, true
	case "1", "onepass", "single":
		return true, false, true
	case "2", "twopass", "two":
		return true, true, true
	}
	l3 := l3CacheBytes()
	if l3 <= 0 {
		return false, false, false
	}
	return w1Bytes > l3, true, false
}

// Accuracy gates for the load-time int8 self-check (max absolute probability
// shift against the float32 path over a fixed probe set). Single-pass must be
// all but indistinguishable to be preferred; two-pass may drift slightly more,
// which is still far below the calibrated τ margins.
const (
	headQ8PProbes          = 12
	headQ8PSingleMaxDelta  = 1e-3
	headQ8PTwoPassMaxDelta = 1e-2
)

// chooseQ8PMode picks the cheapest int8 mode whose probe deviation passes its
// gate. ok=false means neither mode is trustworthy and float32 must be kept.
func chooseQ8PMode(single, twoPass float64) (useTwo, ok bool) {
	switch {
	case single >= 0 && single <= headQ8PSingleMaxDelta:
		return false, true
	case twoPass >= 0 && twoPass <= headQ8PTwoPassMaxDelta:
		return true, true
	default:
		return false, false
	}
}

// q8pProbeInputs builds the fixed probe set: unit-norm pseudo-random vectors
// with the magnitude of a normalised embedding. Deterministic, so every
// process reaches the same verdict.
func (h *LocalHead) q8pProbeInputs() [][]float64 {
	r := rand.New(rand.NewSource(0x5eed))
	probes := make([][]float64, headQ8PProbes)
	for p := range probes {
		x := make([]float64, h.dim)
		var ss float64
		for i := range x {
			v := r.NormFloat64()
			x[i] = v
			ss += v * v
		}
		if ss > 0 {
			inv := 1 / math.Sqrt(ss)
			for i := range x {
				x[i] *= inv
			}
		}
		probes[p] = x
	}
	return probes
}

// q8pProbeDeltas computes the float32 reference once and compares both int8
// modes against it. When skipTwo is set the two-pass delta is +Inf (not run).
func (h *LocalHead) q8pProbeDeltas(probes [][]float64, skipTwo bool) (single, two float64) {
	if h.hidden <= 0 || len(h.w1) < h.hidden*h.dim {
		// No float32 reference left to compare against. +Inf makes the caller
		// keep float32 rather than trust int8 unverified.
		return math.Inf(1), math.Inf(1)
	}
	q, prevTwo := h.w1q, h.headQ8P2
	defer func() { h.w1q, h.headQ8P2 = q, prevTwo }()
	refs := make([][]float64, len(probes))
	for i, x := range probes {
		h.w1q = nil
		refs[i] = h.forward(x)
	}
	measure := func(twoPass bool) float64 {
		worst := 0.0
		for i, x := range probes {
			h.w1q, h.headQ8P2 = q, twoPass
			got := h.forward(x)
			for j := range refs[i] {
				d := math.Abs(refs[i][j] - got[j])
				if math.IsNaN(d) || math.IsInf(d, 0) {
					return math.Inf(1)
				}
				if d > worst {
					worst = d
				}
			}
		}
		return worst
	}
	single = measure(false)
	two = math.Inf(1)
	if !skipTwo {
		two = measure(true)
	}
	return single, two
}

// finishQ8PSetup verifies the auto-selected int8 path and then drops the
// float32 weights. An explicit MACLAW_HEAD_Q8P skips the check: the operator
// already chose, and tests pin exact modes.
func (h *LocalHead) finishQ8PSetup() {
	if h.w1q == nil {
		return
	}
	mode := "single-pass"
	if h.headQ8P2 {
		mode = "two-pass"
	}
	detail := "forced"
	if h.q8pPending {
		h.q8pPending = false
		// One pass over the probe set: the float32 reference is computed once
		// and shared by both int8 modes.
		single, two := h.q8pProbeDeltas(h.q8pProbeInputs(), false)
		useTwo, ok := chooseQ8PMode(single, two)
		if !ok {
			log.Printf("[local-head] int8 hidden layer rejected (probe delta single %.2g / two-pass %.2g, limit %.0e); keeping float32",
				single, two, headQ8PTwoPassMaxDelta)
			h.w1q, h.padHidden, h.headQ8P2 = nil, 0, false
			return
		}
		h.headQ8P2 = useTwo
		if useTwo {
			mode = "two-pass"
			detail = fmt.Sprintf("probe delta %.2g", two)
		} else {
			detail = fmt.Sprintf("probe delta %.2g", single)
		}
	}
	log.Printf("[local-head] int8 hidden layer %s (%.1f MB, L3 %.1f MB, %s)",
		mode, float64(h.padHidden*h.dim*4)/1e6, float64(l3CacheBytes())/1e6, detail)
	h.w1 = nil // packed weights replace the float32 copy
}

// setupHeadQ8P packs w1 for the VNNI GEMM when the policy and shape allow it.
// Weights are padded with zero rows to a multiple of 32 (the kernel's tile);
// the extra outputs are discarded, so results are unchanged.
func (h *LocalHead) setupHeadQ8P() {
	if h.hidden <= 0 || h.dim%32 != 0 || len(h.w1) < h.hidden*h.dim {
		return
	}
	use, two, forced := headQ8PPolicy(int64(h.hidden) * int64(h.dim) * 4)
	if !use {
		return
	}
	if tensor.Q8PFusedTwoTerm() {
		// The activation quantiser already carries two int8 terms there, so a
		// separate residual pass would only multiply zeros.
		two = false
	}
	if !tensor.Q8PAvailable() {
		return
	}
	pad := (h.hidden + 31) / 32 * 32
	wp := h.w1
	if pad != h.hidden {
		wp = make([]float32, pad*h.dim)
		copy(wp, h.w1)
	}
	q8 := tensor.QuantizeToQ8(wp, pad, h.dim)
	if q8 == nil {
		return
	}
	packed := tensor.PackQ8P(q8)
	if packed == nil {
		return
	}
	h.w1q, h.padHidden, h.headQ8P2 = packed, pad, two
	h.q8pPending = !forced
}

func (h *LocalHead) forward(x []float64) []float64 {
	sp := h.getScratch(len(x))
	defer h.scratch.Put(sp)
	xf := sp.xf[:len(x)]
	for i, v := range x {
		xf[i] = float32(v)
	}
	if h.hidden > 0 {
		hid := sp.hid[:h.hidden]
		hidf := sp.hf[:h.hidden]
		var raw []float32
		if h.w1q != nil {
			raw = sp.hidRaw[:h.padHidden]
			tensor.QuantizeQ8Act(&sp.act, xf, 1, h.dim)
			tensor.MatMulQ8P(raw, &sp.act, h.w1q, false, 1)
			if h.headQ8P2 {
				// Second pass over the quantisation residual: brings the
				// activation error down to ~float accuracy for 2x the GEMM.
				if cap(sp.resid) < h.dim {
					sp.resid = make([]float32, h.dim)
				}
				r := sp.resid[:h.dim]
				tensor.Q8ActResidual(r, xf, &sp.act)
				tensor.QuantizeQ8Act(&sp.act2, r, 1, h.dim)
				tensor.MatMulQ8P(raw, &sp.act2, h.w1q, true, 1)
			}
		}
		for j := 0; j < h.hidden; j++ {
			var s float64
			if raw != nil {
				s = float64(raw[j]) + float64(h.b1[j])
			} else {
				// Accumulate the bias in float64 around a float32 SIMD dot.
				s = float64(h.b1[j]) + float64(tensor.Dot(h.w1[j*h.dim:(j+1)*h.dim], xf))
			}
			if s > 0 {
				hid[j], hidf[j] = s, float32(s)
			} else {
				// Pooled buffers keep last call's values; a ReLU'd zero must be
				// explicit here.
				hid[j], hidf[j] = 0, 0
			}
		}
		x, xf = hid, hidf
		for _, d := range h.extra {
			x = d.relu(x)
			xf = float32s(x)
		}
	}
	in := len(x)
	c := len(h.Labels)
	p := make([]float64, c)
	mx := math.Inf(-1)
	for k := 0; k < c; k++ {
		s := float64(h.b2[k]) + float64(tensor.Dot(h.w2[k*in:(k+1)*in], xf))
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

// denseLayer is one extra ReLU layer of a deeper local head.
type denseLayer struct {
	in, out int
	w       []float64 // [out*in] row-major
	b       []float64
}

func (d denseLayer) relu(x []float64) []float64 {
	y := make([]float64, d.out)
	for j := 0; j < d.out; j++ {
		row := d.w[j*d.in : (j+1)*d.in]
		s := d.b[j]
		for i, w := range row {
			s += w * x[i]
		}
		if s > 0 {
			y[j] = s
		}
	}
	return y
}

// normalizeCalibrationCompute maps free-text calibration tags written by
// older export scripts to the embedder compute-mode token they were measured
// under. "box amd64 TestZZVecDump vectors" (r7 and earlier box exports) is
// q8p:avx512:2pass: on old-339 + indep839 + M_confirm (1320 texts) the r7
// head's probabilities under that mode match float (MACLAW_Q8P=0) within
// 0.0004 with identical decisions (2026-10-14).
func normalizeCalibrationCompute(s string) string {
	switch strings.TrimSpace(s) {
	case "box amd64 TestZZVecDump vectors":
		return "q8p:avx512:2pass"
	}
	return strings.TrimSpace(s)
}

// blockNormalize converts v to float64, L2-normalising each of nb equal blocks.
func blockNormalize(v []float32, nb int, l2 bool) []float64 {
	x := make([]float64, len(v))
	bd := len(v) / nb
	for b := 0; b < nb; b++ {
		var n float64
		for i := b * bd; i < (b+1)*bd; i++ {
			x[i] = float64(v[i])
			n += x[i] * x[i]
		}
		if l2 && n > 0 {
			inv := 1 / math.Sqrt(n)
			for i := b * bd; i < (b+1)*bd; i++ {
				x[i] *= inv
			}
		}
	}
	return x
}
