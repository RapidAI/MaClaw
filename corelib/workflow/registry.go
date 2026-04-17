package workflow

import (
	"fmt"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// WorkflowRegistry holds registered workflow templates and provides
// lookup by WorkflowType. It is safe for concurrent use.
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*WorkflowTemplate
	bm25Index *bm25.Index // lazy-built BM25 index over template descriptions
	bm25Dirty bool        // true when templates changed since last index build
}

// NewWorkflowRegistry creates a WorkflowRegistry pre-populated with
// all built-in workflow templates (coding, product_design, innovation,
// business_plan, testing).
func NewWorkflowRegistry() *WorkflowRegistry {
	r := &WorkflowRegistry{
		templates: make(map[WorkflowType]*WorkflowTemplate),
	}
	RegisterBuiltinTemplates(r)
	return r
}

// Register adds or overwrites a template in the registry.
// If a template with the same Type already exists, it is replaced.
func (r *WorkflowRegistry) Register(tmpl *WorkflowTemplate) {
	if tmpl == nil {
		return
	}
	r.mu.Lock()
	r.templates[tmpl.Type] = tmpl
	r.bm25Dirty = true
	r.mu.Unlock()
}

// Match returns the template registered for the given WorkflowType,
// or nil if no template is registered for that type.
func (r *WorkflowRegistry) Match(wt WorkflowType) *WorkflowTemplate {
	r.mu.RLock()
	tmpl := r.templates[wt]
	r.mu.RUnlock()
	return tmpl
}

// AllDescriptions returns a formatted summary of every registered
// template, suitable for inclusion in an LLM system prompt.
// Each entry contains the template Type (category value), Name and Description.
func (r *WorkflowRegistry) AllDescriptions() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.templates) == 0 {
		return ""
	}

	var b strings.Builder
	for _, tmpl := range r.templates {
		fmt.Fprintf(&b, "- **%s** (%s): %s\n", string(tmpl.Type), tmpl.Name, tmpl.Description)
	}
	return b.String()
}

// MatchesAnyTemplate checks whether the text matches any registered template's
// Keywords using a two-tier scoring system:
//
//   - Strong keywords (≥3 Chinese chars, or uppercase abbreviations like PRD/PPT/SWOT):
//     a single hit is sufficient to match.
//   - Weak keywords (short common words like "产品", "设计", "需求"):
//     require at least 2 hits from the same template to avoid false positives
//     in non-workflow contexts (e.g., "翻译这个产品说明").
//
// This is the primary extensibility mechanism: adding a new template with
// Keywords automatically makes it detectable by QuickFilter without any
// code changes to the classification logic.
func (r *WorkflowRegistry) MatchesAnyTemplate(text string) bool {
	_, matched := r.MatchTemplateByKeywords(text)
	return matched
}

// MatchTemplateByKeywords returns the WorkflowType of the best-matching
// template whose keywords match the given text, using the same two-tier
// scoring as MatchesAnyTemplate. Returns ("", false) if no template matches.
// When multiple templates match, the one with the highest score wins
// (strong keyword = 10 points, weak keyword = 1 point each).
func (r *WorkflowRegistry) MatchTemplateByKeywords(text string) (WorkflowType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(text)
	var bestType WorkflowType
	bestScore := 0

	for _, tmpl := range r.templates {
		score := 0
		for _, kw := range tmpl.Keywords {
			kwLower := strings.ToLower(kw)
			if !strings.Contains(lower, kwLower) {
				continue
			}
			if isUpperAbbrev(kw) || chineseRuneCount(kw) >= 3 {
				score += 10 // strong keyword
			} else {
				score++ // weak keyword
			}
		}
		// Require at least one strong hit (≥10) or two weak hits (≥2).
		if score >= 2 && score > bestScore {
			bestScore = score
			bestType = tmpl.Type
		}
	}
	if bestScore > 0 {
		return bestType, true
	}
	return "", false
}

// MatchTemplateByStrongKeyword returns the WorkflowType of a template that
// has a strong keyword match (uppercase abbreviation or ≥3 Chinese char phrase).
// This is stricter than MatchTemplateByKeywords and is used as a fallback
// when the LLM intent understanding call FAILS (timeout, network error).
// It should NOT be used to override an explicit LLM rejection.
func (r *WorkflowRegistry) MatchTemplateByStrongKeyword(text string) (WorkflowType, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	lower := strings.ToLower(text)
	for _, tmpl := range r.templates {
		for _, kw := range tmpl.Keywords {
			kwLower := strings.ToLower(kw)
			if !strings.Contains(lower, kwLower) {
				continue
			}
			if isUpperAbbrev(kw) || chineseRuneCount(kw) >= 3 {
				return tmpl.Type, true
			}
		}
	}
	return "", false
}

// isUpperAbbrev returns true if s is 2+ uppercase ASCII letters (PRD, PPT, BP, etc.).
func isUpperAbbrev(s string) bool {
	if len(s) < 2 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 'A' || s[i] > 'Z' {
			return false
		}
	}
	return true
}

// chineseRuneCount counts the number of CJK Unified Ideograph runes in s.
func chineseRuneCount(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// BM25 semantic matching
// ---------------------------------------------------------------------------

// BestTemplateScore returns the highest BM25 score of the text against all
// registered template documents (Name + Description + Keywords concatenated).
// Uses a lazily-built BM25 index that auto-rebuilds when templates change.
// Returns 0.0 if no templates are registered or the index is empty.
//
// Typical score ranges:
//   - Strong match (e.g., "生成网络安全产品的PRD" vs product_design): 3.0–6.0
//   - Weak match (e.g., "翻译这段话" vs any template): 0.0–0.5
//   - Threshold recommendation: 2.0 (conservative, avoids false positives)
func (r *WorkflowRegistry) BestTemplateScore(text string) float64 {
	r.mu.Lock()
	if r.bm25Index == nil || r.bm25Dirty {
		r.rebuildBM25Locked()
	}
	idx := r.bm25Index
	r.mu.Unlock()

	if idx == nil {
		return 0.0
	}

	scores := idx.Score(text)
	best := 0.0
	for _, s := range scores {
		if s > best {
			best = s
		}
	}
	return best
}

// rebuildBM25Locked rebuilds the BM25 index from all registered templates.
// Each template becomes one document with ID=Type and Text=Name+Description+Keywords.
// Must be called with r.mu held for writing.
func (r *WorkflowRegistry) rebuildBM25Locked() {
	docs := make([]bm25.Doc, 0, len(r.templates))
	for _, tmpl := range r.templates {
		// Concatenate Name + Description + Keywords into a single searchable text.
		var b strings.Builder
		b.WriteString(tmpl.Name)
		b.WriteString(" ")
		b.WriteString(tmpl.Description)
		for _, kw := range tmpl.Keywords {
			b.WriteString(" ")
			b.WriteString(kw)
		}
		docs = append(docs, bm25.Doc{
			ID:   string(tmpl.Type),
			Text: b.String(),
		})
	}
	if r.bm25Index == nil {
		r.bm25Index = bm25.New()
	}
	r.bm25Index.Rebuild(docs)
	r.bm25Dirty = false
}
