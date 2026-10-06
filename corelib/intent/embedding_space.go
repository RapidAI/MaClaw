package intent

// embedding_space.go — the vector space Layer 2 classifies in.
//
// Layer 2 embeds both its anchors and the user message with the
// EmbeddingGemma classification task prompt ("task: classification | query: ",
// embedding.RoleClassification) instead of the raw text. Measured on an
// independently written 839-sentence test set (two model families, blind
// third-family judge), the prompt lifts anchor max-cos top-1 from 0.462 to
// 0.558, and from 0.752 to 0.850 on the in-house held-out set.
//
// The prompt shifts the cosine distribution (scores rise and spread), and
// every Layer 2 threshold in this package (EmbeddingConfidentMinScore/Gap,
// lookup and composite floors) was calibrated on raw-text cosines. Instead of
// re-deriving each constant, prompted scores are mapped back onto the raw
// scale with a monotone affine calibration before any threshold sees them,
// so all constants keep their reviewed meaning. The map was fitted without
// labels by matching score quantiles of the in-house synthetic training
// sentences in both spaces; with it the unchanged grant rule (0.78/0.10,
// lookup 0.70/0.05) keeps selective accuracy >= 0.97 on the in-house set and
// on both independent generators separately, while confident coverage rises.
//
// Embedders that do not implement embedding.RoleEmbedder (test doubles,
// remote services) and processes with MACLAW_EMBED_PROMPTS=0 keep the raw
// space and the identity calibration, so their behaviour is unchanged.

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// intentEmbeddingRole is the task prompt Layer 2 embeds anchors and queries
// with. Anchors and queries must always use the same role.
const intentEmbeddingRole = embedding.RoleClassification

// intentScoreCalibration maps a cosine produced in the prompted space onto
// the raw-text scale the Layer 2 thresholds were calibrated on.
type intentScoreCalibration struct {
	Scale float64
	Shift float64
}

// classificationPromptScoreCalibration: raw ≈ 0.889·prompted + 0.098
// (quantile match of top-5 per-label max-cos on the synthetic training set).
// Re-fit whenever the model, the prompt template, the anchor set, or
// DefaultEmbeddingDim changes.
var classificationPromptScoreCalibration = intentScoreCalibration{Scale: 0.889, Shift: 0.098}

// qwen3ClassificationScoreCalibration is the same label-free quantile map for
// Qwen3-Embedding (pure-Go runner, Q8_0, "Instruct: Classify the intent of
// this assistant user request\nQuery: " on both anchors and queries), fitted
// against the raw EmbeddingGemma top-5 max-cos distribution on the synthetic
// training sentences, so the unchanged Gemma-raw constants keep their meaning.
// raw ≈ 0.6758·qwen3 + 0.2834 ⇒ prompt-space grant rule ≈ 0.735/0.148 (lookup
// 0.617/0.074); measured selective accuracy 0.978 / 0.989 / 1.000 / 1.000 on
// synthetic / in-house test / Gemini / Kimi (coverage 0.35 / 0.27 / 0.11 / 0.11).
var qwen3ClassificationScoreCalibration = intentScoreCalibration{Scale: 0.6758, Shift: 0.2834}

var identityScoreCalibration = intentScoreCalibration{Scale: 1, Shift: 0}

// intentEmbeddingPrompted reports whether emb will actually apply the
// classification prompt for Layer 2.
func intentEmbeddingPrompted(emb embedding.Embedder) bool {
	if emb == nil || embedding.IsNoop(emb) || !embedding.NeedsPrompt(intentEmbeddingRole) {
		return false
	}
	_, ok := emb.(embedding.RoleEmbedder)
	return ok
}

// intentScoreCalibrationFor returns the score map matching the space emb
// embeds Layer 2 text in.
func intentScoreCalibrationFor(emb embedding.Embedder) intentScoreCalibration {
	if _, ok := emb.(*embedding.Qwen3Embedder); ok {
		// Qwen3 cosines live on a different scale than Gemma's in both the
		// prompted and the raw regime; only the prompted map is calibrated.
		if intentEmbeddingPrompted(emb) {
			return qwen3ClassificationScoreCalibration
		}
		return identityScoreCalibration
	}
	if intentEmbeddingPrompted(emb) {
		return classificationPromptScoreCalibration
	}
	return identityScoreCalibration
}

// apply maps a max-cosine score; non-positive scores stay 0 ("no evidence").
func (c intentScoreCalibration) apply(score float64) float64 {
	if score <= 0 {
		return 0
	}
	v := c.Scale*score + c.Shift
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// embedIntentQuery embeds a user message in the Layer 2 space.
func embedIntentQuery(emb embedding.Embedder, text string) ([]float32, error) {
	return embedding.EmbedAs(emb, text, intentEmbeddingRole)
}

// embedIntentAnchors embeds anchor sentences in the Layer 2 space.
func embedIntentAnchors(emb embedding.Embedder, texts []string) ([][]float32, error) {
	return embedding.EmbedBatchAs(emb, texts, intentEmbeddingRole)
}

// maxAnchorScore returns the calibrated max cosine between queryVec and one
// anchor set, or ok=false when the anchor set is not warmed.
func maxAnchorScore(queryVec []float32, anchor intentAnchor, cal intentScoreCalibration) (float64, bool) {
	if len(anchor.Vecs) == 0 {
		return 0, false
	}
	maxSim := 0.0
	for _, vec := range anchor.Vecs {
		if sim := cosineSimilarity(queryVec, vec); sim > maxSim {
			maxSim = sim
		}
	}
	return cal.apply(maxSim), true
}

// ---------------------------------------------------------------------------
// Anchor vector disk cache
// ---------------------------------------------------------------------------
//
// Warming ~530 anchors one by one costs ~25 s on a desktop CPU at every cold
// start. Vectors are cached on disk under a key that covers the embedder's
// ModelID (model identity, dimension, prompt regime), the Layer 2 role prompt
// and every anchor label/text, so a model, prompt or anchor change can never
// reuse stale vectors. Only embedders that expose a non-empty ModelID are
// cached. MACLAW_INTENT_ANCHOR_CACHE=0 disables the cache;
// MACLAW_INTENT_ANCHOR_CACHE_DIR overrides its location.

const intentAnchorCacheVersion = "intent-anchor-cache-v1"

type intentAnchorCacheFile struct {
	Key    string
	Dim    int
	Labels []string
	Counts []int
	Vecs   [][]float32
}

func intentAnchorCacheDir() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_INTENT_ANCHOR_CACHE"))) {
	case "0", "off", "false", "no":
		return ""
	}
	if dir := strings.TrimSpace(os.Getenv("MACLAW_INTENT_ANCHOR_CACHE_DIR")); dir != "" {
		return dir
	}
	if models := embedding.DefaultModelsDir(); models != "" {
		return filepath.Join(filepath.Dir(models), "cache", "intent-anchors")
	}
	if dir, err := os.UserCacheDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "maclaw", "intent-anchors")
	}
	return ""
}

// intentAnchorCacheKey returns "" when emb cannot be identified.
func intentAnchorCacheKey(emb embedding.Embedder, anchors []intentAnchor) string {
	identified, ok := emb.(interface{ ModelID() string })
	if !ok {
		return ""
	}
	modelID := strings.TrimSpace(identified.ModelID())
	if modelID == "" {
		return ""
	}
	h := sha256.New()
	prompt := ""
	if intentEmbeddingPrompted(emb) {
		prompt = embedding.PromptPrefix(intentEmbeddingRole)
	}
	fmt.Fprintf(h, "%s\x00%s\x00%d\x00%q\x00", intentAnchorCacheVersion, modelID, emb.Dim(), prompt)
	for _, anchor := range anchors {
		fmt.Fprintf(h, "L\x00%s\x00%d\x00", anchor.Label, len(anchor.Texts))
		for _, text := range anchor.Texts {
			fmt.Fprintf(h, "%s\x00", text)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func loadIntentAnchorCache(dir, key string, emb embedding.Embedder, anchors []intentAnchor) ([]intentAnchor, bool) {
	data, err := os.ReadFile(filepath.Join(dir, key+".gob"))
	if err != nil {
		return nil, false
	}
	var file intentAnchorCacheFile
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&file); err != nil || file.Key != key || len(file.Labels) != len(anchors) || len(file.Counts) != len(anchors) {
		return nil, false
	}
	dim := emb.Dim()
	warmed := make([]intentAnchor, len(anchors))
	next := 0
	for i, anchor := range anchors {
		if file.Labels[i] != string(anchor.Label) || file.Counts[i] != len(anchor.Texts) || next+file.Counts[i] > len(file.Vecs) {
			return nil, false
		}
		warmed[i] = intentAnchor{Label: anchor.Label, Texts: append([]string(nil), anchor.Texts...)}
		if len(anchor.Texts) == 0 {
			continue
		}
		vecs := file.Vecs[next : next+len(anchor.Texts)]
		for _, vec := range vecs {
			if len(vec) == 0 || (dim > 0 && len(vec) != dim) || (file.Dim > 0 && len(vec) != file.Dim) {
				return nil, false
			}
		}
		warmed[i].Vecs = vecs
		next += len(anchor.Texts)
	}
	if next != len(file.Vecs) {
		return nil, false
	}
	return warmed, true
}

func saveIntentAnchorCache(dir, key string, emb embedding.Embedder, warmed []intentAnchor) {
	file := intentAnchorCacheFile{Key: key, Dim: emb.Dim()}
	for _, anchor := range warmed {
		file.Labels = append(file.Labels, string(anchor.Label))
		file.Counts = append(file.Counts, len(anchor.Vecs))
		file.Vecs = append(file.Vecs, anchor.Vecs...)
	}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(&file); err != nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("[UnifiedIntentClassifier] anchor cache: %v", err)
		return
	}
	tmp, err := os.CreateTemp(dir, key+".*.tmp")
	if err != nil {
		log.Printf("[UnifiedIntentClassifier] anchor cache: %v", err)
		return
	}
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, key+".gob")); err != nil {
		_ = os.Remove(tmp.Name())
	}
}
