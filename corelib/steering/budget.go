package steering

import "github.com/RapidAI/CodeClaw/corelib"

// Token budget constants for steering injection.
//
// Budget rationale (based on default 110K context window):
//   Total context:           110,000 tokens
//   Output reserve (20%):    -22,000
//   Effective input:          88,000
//   System prompt fixed:      ~3,000 (identity, core principles, device status)
//   Coding workflow rules:    ~2,500 (Pro mode 9-step flow)
//   Tool definitions:         ~3,000-5,000 (15-40 tools)
//   Memory recall:            ~800-1,500 (up to 12 entries)
//   Knowledge skills:         ~2,000 (defaultKnowledgeSkillTokenBudget)
//   Workflow phase prompt:    ~500-1,000 (when active)
//   * Steering budget:        ~3,000 (this package)
//   Conversation history:    ~70,000-75,000 (remainder)
const (
	// MaxSteeringTokenBudget is the total token budget for all steering
	// files injected into the system prompt. ~3% of the default 110K
	// context window's effective input budget.
	MaxSteeringTokenBudget = 3000

	// MaxSingleFileTokens caps any individual steering file to prevent
	// one file from consuming the entire budget.
	MaxSingleFileTokens = 1500

	// MaxAlwaysFiles limits the number of always-inclusion files to
	// prevent unbounded context growth.
	MaxAlwaysFiles = 5

	// MaxTotalFiles limits the total number of steering files loaded
	// across both user-level and project-level directories.
	MaxTotalFiles = 20

	// MaxFileBytes is the hard filesystem-level size limit per file.
	// Files larger than this are skipped during loading.
	MaxFileBytes = 15 * 1024

	// minBudgetTokens is the absolute minimum steering budget, used
	// when the context window is very small.
	minBudgetTokens = 500

	// smallContextThreshold is the effective context token count below
	// which the steering budget is dynamically scaled down.
	smallContextThreshold = 80_000

	// budgetPercent is the percentage of effective context tokens
	// allocated to steering when dynamic scaling is active.
	budgetPercent = 3
)

// effectiveBudget returns the steering token budget for the given context size.
// For large context models (>=80K effective), returns MaxSteeringTokenBudget.
// For smaller models, scales proportionally (3% of effective context), with
// a floor of minBudgetTokens.
func effectiveBudget(effectiveContextTokens int) int {
	if effectiveContextTokens <= 0 || effectiveContextTokens >= smallContextThreshold {
		return MaxSteeringTokenBudget
	}
	budget := effectiveContextTokens * budgetPercent / 100
	if budget < minBudgetTokens {
		return minBudgetTokens
	}
	return budget
}

// estimateTokens delegates to corelib.EstimateTextTokens.
func estimateTokens(s string) int {
	return corelib.EstimateTextTokens(s)
}
