package experience

import "github.com/RapidAI/CodeClaw/corelib"

const (
	defaultMinPatternQualityScore   = 5
	defaultMinEvidenceScore         = 2
	defaultSimilarTriggerThreshold  = 0.5
	defaultMaxPatternsPerExtraction = 5
)

// Options controls how conservative the experience learning pipeline is.
// Zero values are replaced with the production defaults.
type Options struct {
	MinPatternQualityScore   int
	MinEvidenceScore         int
	SimilarTriggerThreshold  float64
	MaxPatternsPerExtraction int
	// ExperienceDomain is the pool the analysed sessions belong to, stamped
	// onto every extracted skill. A caller that only ever analyses one kind of
	// work should set it, otherwise the skill becomes universal and a recipe
	// distilled from one kind of work is advertised in every other. Consolidation
	// needs it before deciding a match, so it cannot be applied afterwards.
	ExperienceDomain string
}

func DefaultOptions() Options {
	return Options{
		MinPatternQualityScore:   defaultMinPatternQualityScore,
		MinEvidenceScore:         defaultMinEvidenceScore,
		SimilarTriggerThreshold:  defaultSimilarTriggerThreshold,
		MaxPatternsPerExtraction: defaultMaxPatternsPerExtraction,
	}
}

func normalizeOptions(opts Options) Options {
	defaults := DefaultOptions()
	if opts.MinPatternQualityScore <= 0 {
		opts.MinPatternQualityScore = defaults.MinPatternQualityScore
	}
	if opts.MinEvidenceScore <= 0 {
		opts.MinEvidenceScore = defaults.MinEvidenceScore
	}
	if opts.SimilarTriggerThreshold <= 0 || opts.SimilarTriggerThreshold > 1 {
		opts.SimilarTriggerThreshold = defaults.SimilarTriggerThreshold
	}
	if opts.MaxPatternsPerExtraction <= 0 {
		opts.MaxPatternsPerExtraction = defaults.MaxPatternsPerExtraction
	}
	// An unrecognized pool would hide the skill from every agent.
	opts.ExperienceDomain = corelib.NormalizeSkillExperienceDomain(opts.ExperienceDomain)
	return opts
}
