package tool

// toolhead.go — production loader/inference for the first-party tool head.
//
// The tool head is a one-vs-rest logistic layer over the frozen query vector
// that intent L2/L3 already computed for the turn (Qwen3-Embedding,
// RoleClassification instruct), so scoring the 49 first-party tools costs one
// 49×D mat-vec and no extra embedding. It is meant for candidate injection
// ("intent scope ∪ tool-head top-k (p ≥ θ) ∪ Track A top-N"), not top-1
// selection. Trained offline by maclaw_reranker/scripts/qwen3_toolhead.py.
//
// JSON format (format = "maclaw-tool-head-ovr-v1"):
//
//	{"format": "maclaw-tool-head-ovr-v1",
//	 "embedder_model_id": "<embedding.ModelID() of the training embedder>",
//	 "embed_role": "classification", "dims": D, "l2norm": true,
//	 "label_table_version": "core49@<sha256(tools joined by \n)[:12]>",
//	 "tools": [...C], "trained": [...C bool], "W": [[...D]...C], "b": [...C],
//	 "theta": θ?, "top_k": k?, "note": "..."}
//
// A head is only usable with the embedder it was trained on: Bind refuses a
// model-id or dimension mismatch (fail-closed — a vector from another space
// would produce confident garbage).

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
	"strings"
)

// ToolHeadFormat is the only head JSON format this loader accepts.
const ToolHeadFormat = "maclaw-tool-head-ovr-v1"

// ErrToolHeadMismatch is returned when a head does not match the embedder.
var ErrToolHeadMismatch = errors.New("tool head: embedder mismatch")

// ToolHead is a loaded, validated OvR tool head. Immutable after load; safe
// for concurrent use.
type ToolHead struct {
	ID                string // file base name + "@" + first 12 hex of sha256
	EmbedderModelID   string
	EmbedRole         string
	LabelTableVersion string
	Tools             []string
	Theta             float64 // recommended injection threshold (0 = unset)
	TopKDefault       int     // recommended k (0 = unset)
	Note              string

	trained []bool
	dim     int
	l2norm  bool
	w       []float32 // [C*dim] row-major
	b       []float32
}

type toolHeadFile struct {
	Format            string      `json:"format"`
	EmbedderModelID   string      `json:"embedder_model_id"`
	EmbedRole         string      `json:"embed_role"`
	Dims              int         `json:"dims"`
	L2Norm            bool        `json:"l2norm"`
	LabelTableVersion string      `json:"label_table_version"`
	Tools             []string    `json:"tools"`
	Trained           []bool      `json:"trained"`
	W                 [][]float64 `json:"W"`
	B                 []float64   `json:"b"`
	Theta             float64     `json:"theta"`
	TopK              int         `json:"top_k"`
	Note              string      `json:"note"`
}

// ToolLabelTableVersion is the canonical label-table version for a tool list:
// "core<N>@" + first 12 hex of sha256(tools joined by "\n").
func ToolLabelTableVersion(tools []string) string {
	sum := sha256.Sum256([]byte(strings.Join(tools, "\n")))
	return fmt.Sprintf("core%d@%s", len(tools), hex.EncodeToString(sum[:])[:12])
}

// LoadToolHead reads and validates a tool head JSON file.
func LoadToolHead(path string) (*ToolHead, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	h, err := ParseToolHead(b)
	if err != nil {
		return nil, fmt.Errorf("tool head %s: %w", path, err)
	}
	sum := sha256.Sum256(b)
	h.ID = filepath.Base(path) + "@" + hex.EncodeToString(sum[:])[:12]
	return h, nil
}

// ParseToolHead validates a tool head from its JSON bytes (ID left empty).
func ParseToolHead(data []byte) (*ToolHead, error) {
	var f toolHeadFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Format != ToolHeadFormat {
		return nil, fmt.Errorf("unsupported format %q (want %q)", f.Format, ToolHeadFormat)
	}
	if strings.TrimSpace(f.EmbedderModelID) == "" {
		return nil, errors.New("missing embedder_model_id")
	}
	c := len(f.Tools)
	if c == 0 {
		return nil, errors.New("empty tool table")
	}
	if f.Dims <= 0 {
		return nil, fmt.Errorf("invalid dims %d", f.Dims)
	}
	if len(f.W) != c || len(f.B) != c {
		return nil, fmt.Errorf("W/b rows %d/%d != tools %d", len(f.W), len(f.B), c)
	}
	if f.Trained != nil && len(f.Trained) != c {
		return nil, fmt.Errorf("trained has %d entries, want %d", len(f.Trained), c)
	}
	seen := make(map[string]bool, c)
	for _, t := range f.Tools {
		if t == "" || seen[t] {
			return nil, fmt.Errorf("empty or duplicate tool name %q", t)
		}
		seen[t] = true
	}
	if want := ToolLabelTableVersion(f.Tools); f.LabelTableVersion != want {
		return nil, fmt.Errorf("label_table_version %q does not match tool table (%q)", f.LabelTableVersion, want)
	}
	h := &ToolHead{
		EmbedderModelID:   f.EmbedderModelID,
		EmbedRole:         f.EmbedRole,
		LabelTableVersion: f.LabelTableVersion,
		Tools:             append([]string(nil), f.Tools...),
		Theta:             f.Theta,
		TopKDefault:       f.TopK,
		Note:              f.Note,
		dim:               f.Dims,
		l2norm:            f.L2Norm,
		w:                 make([]float32, 0, c*f.Dims),
		b:                 make([]float32, c),
		trained:           make([]bool, c),
	}
	for k, row := range f.W {
		if len(row) != f.Dims {
			return nil, fmt.Errorf("W row %d has %d cols, want dims %d", k, len(row), f.Dims)
		}
		for _, v := range row {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("non-finite weight in W row %d", k)
			}
			h.w = append(h.w, float32(v))
		}
		if math.IsNaN(f.B[k]) || math.IsInf(f.B[k], 0) {
			return nil, fmt.Errorf("non-finite bias %d", k)
		}
		h.b[k] = float32(f.B[k])
		h.trained[k] = f.Trained == nil || f.Trained[k]
	}
	return h, nil
}

// Dim is the query vector dimension the head expects.
func (h *ToolHead) Dim() int { return h.dim }

// Trained reports whether tool index k had positives in training (untrained
// tools always score 0 and are never returned by TopK).
func (h *ToolHead) Trained(k int) bool { return k >= 0 && k < len(h.trained) && h.trained[k] }

// CheckEmbedder refuses a head trained in another vector space.
func (h *ToolHead) CheckEmbedder(modelID string, dim int) error {
	if dim != h.dim {
		return fmt.Errorf("%w: embedder dim %d != head dims %d", ErrToolHeadMismatch, dim, h.dim)
	}
	if modelID != h.EmbedderModelID {
		return fmt.Errorf("%w: embedder model %q != head embedder_model_id %q", ErrToolHeadMismatch, modelID, h.EmbedderModelID)
	}
	return nil
}

// toolHeadModelIDer is implemented by embedders that expose their vector-space
// identity (embedding.GemmaEmbedder, embedding.Qwen3Embedder).
type toolHeadModelIDer interface {
	ModelID() string
	Dim() int
}

// CheckEmbedderOf checks the head against a live embedder. Embedders that do
// not expose ModelID are refused (fail-closed).
func (h *ToolHead) CheckEmbedderOf(emb interface{}) error {
	m, ok := emb.(toolHeadModelIDer)
	if !ok || m == nil {
		return fmt.Errorf("%w: embedder %T does not expose ModelID/Dim", ErrToolHeadMismatch, emb)
	}
	return h.CheckEmbedder(m.ModelID(), m.Dim())
}

// Predict returns per-tool probabilities sigmoid(W·x + b) for one query
// vector (index-aligned with Tools; untrained tools get 0).
func (h *ToolHead) Predict(v []float32) ([]float64, error) {
	if len(v) != h.dim {
		return nil, fmt.Errorf("%w: vector dim %d != head dims %d", ErrToolHeadMismatch, len(v), h.dim)
	}
	inv := float32(1)
	if h.l2norm {
		var n float64
		for _, f := range v {
			n += float64(f) * float64(f)
		}
		if n > 0 {
			inv = float32(1 / math.Sqrt(n))
		}
	}
	p := make([]float64, len(h.Tools))
	for k := range h.Tools {
		if !h.trained[k] {
			continue
		}
		row := h.w[k*h.dim : (k+1)*h.dim]
		var s float32
		for i, w := range row {
			s += w * v[i]
		}
		z := float64(s*inv) + float64(h.b[k])
		p[k] = 1 / (1 + math.Exp(-z))
	}
	return p, nil
}

// ToolHeadScore is one tool with its head probability.
type ToolHeadScore struct {
	Tool string  `json:"tool"`
	Prob float64 `json:"p"`
}

// TopK returns up to k trained tools with p ≥ theta, highest first (ties by
// name). k ≤ 0 means no cap.
func (h *ToolHead) TopK(probs []float64, k int, theta float64) []ToolHeadScore {
	out := make([]ToolHeadScore, 0, 8)
	for i, p := range probs {
		if i < len(h.trained) && h.trained[i] && p >= theta {
			out = append(out, ToolHeadScore{Tool: h.Tools[i], Prob: p})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Prob != out[b].Prob {
			return out[a].Prob > out[b].Prob
		}
		return out[a].Tool < out[b].Tool
	})
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	return out
}
