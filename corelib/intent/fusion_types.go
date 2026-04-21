package intent

// ---------------------------------------------------------------------------
// Dual-channel fusion types — inspired by intent-fusion's architecture.
// See docs/intent-fusion-upgrade-design.md for design rationale.
// ---------------------------------------------------------------------------

// FusionVerdict represents the three-state outcome of dual-channel fusion.
// Borrowed from intent-fusion's production-validated three-state design:
//   - CLEAR: top candidate is clearly dominant → route directly
//   - AMBIGUOUS: top-2 are close → execute top, suggest runner-up
//   - LOW: no confident match → fall back to free chat / keyword rules
type FusionVerdict string

const (
	VerdictClear     FusionVerdict = "clear"
	VerdictAmbiguous FusionVerdict = "ambiguous"
	VerdictLow       FusionVerdict = "low"
)

// FusedCandidate is a single candidate after merging both channels.
type FusedCandidate struct {
	Label      IntentLabel
	FinalScore float64
	EmbScore   float64
	TreeScore  float64
	InEmb      bool // appeared in embedding channel results
	InTree     bool // appeared in tree channel results
}

// FusionResult is the output of dual-channel fusion.
type FusionResult struct {
	Verdict        FusionVerdict
	Top            FusedCandidate
	RunnerUp       *FusedCandidate // non-nil when Verdict == VerdictAmbiguous
	Candidates     []FusedCandidate
	EmbMs          float64 // embedding channel latency in ms
	TreeMs         float64 // tree channel latency in ms
	TotalMs        float64 // total fusion latency in ms
	Degraded       bool    // true when one channel failed
	ActiveChannels []string
}

// IntentDefinition is the single source of truth for one intent category.
// Inspired by intent-fusion's IntentEntry dual-text design: a single
// description cannot simultaneously optimize for lexical matching and
// semantic reasoning.
//
// All layers (keywords, embedding, LLM tree, tool affinity) are derived
// from this definition. Adding a new intent requires only adding one
// IntentDefinition — all layers auto-update.
type IntentDefinition struct {
	Label      IntentLabel
	Domain     string         // grouping for tree reasoning (e.g., "Coding", "Remote")
	Keywords   []KeywordEntry // Layer 1: keyword rules
	EmbedTexts []string       // Layer 2: anchor sentences for embedding cosine
	TreeText   string         // Layer 3: descriptive text for LLM tree reasoning
	ToolNames  []string       // tool affinity: tools to activate when this intent wins
}

// Fusion parameters — defaults calibrated for MacLaw's intent space.
// Override via FusionConfig or offline calibration.
const (
	DefaultAlpha          = 0.15  // embedding weight (tree-dominant, like intent-fusion's 0.10)
	DefaultDelta          = 0.10  // gap threshold for CLEAR vs AMBIGUOUS
	DefaultLowThreshold   = 0.15  // minimum score for any match
)

// FusionConfig holds tunable parameters for the fusion algorithm.
type FusionConfig struct {
	Alpha        float64 // embedding channel weight [0, 1]. Default 0.15.
	Delta        float64 // score gap threshold for CLEAR vs AMBIGUOUS. Default 0.10.
	LowThreshold float64 // minimum top score for any match. Default 0.15.
}

// DefaultFusionConfig returns the default fusion parameters.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		Alpha:        DefaultAlpha,
		Delta:        DefaultDelta,
		LowThreshold: DefaultLowThreshold,
	}
}
