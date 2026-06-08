package workflow

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/bm25"
)

// WorkflowRegistry holds registered workflow templates and provides lookup by
// WorkflowType. It is safe for concurrent use.
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*WorkflowTemplate
	bm25Index *bm25.Index // lazy-built BM25 index over template descriptions
	bm25Dirty bool        // true when templates changed since last index build
}

// NewWorkflowRegistry creates a WorkflowRegistry pre-populated with all built-in
// workflow templates.
func NewWorkflowRegistry() *WorkflowRegistry {
	r := &WorkflowRegistry{
		templates: make(map[WorkflowType]*WorkflowTemplate),
	}
	RegisterBuiltinTemplates(r)
	return r
}

// Register adds or overwrites a template in the registry after validating the
// phase contract. Invalid templates are rejected before they can affect phase
// progression, tool exposure, or orchestrator activation.
func (r *WorkflowRegistry) Register(tmpl *WorkflowTemplate) error {
	if tmpl == nil {
		return fmt.Errorf("workflow template is nil")
	}
	if errs := ValidateWorkflowTemplateContract(tmpl); len(errs) != 0 {
		return fmt.Errorf("workflow template %s contract invalid: %w", tmpl.Type, errors.Join(errs...))
	}
	r.mu.Lock()
	r.templates[tmpl.Type] = tmpl
	r.bm25Dirty = true
	r.mu.Unlock()
	return nil
}

// MustRegister registers a template and panics on contract errors. Built-in
// template registration uses this so startup/test failures surface immediately
// instead of silently dropping a workflow.
func (r *WorkflowRegistry) MustRegister(tmpl *WorkflowTemplate) {
	if err := r.Register(tmpl); err != nil {
		panic(err)
	}
}

// Match returns the template registered for the given WorkflowType, or nil if
// no template is registered for that type.
func (r *WorkflowRegistry) Match(wt WorkflowType) *WorkflowTemplate {
	r.mu.RLock()
	tmpl := r.templates[wt]
	r.mu.RUnlock()
	return tmpl
}

// All returns one pointer per registered template in a deterministic order
// (sorted by Type) so downstream generation (code generator, contract tests) is
// byte-stable across runs. It takes a read-locked snapshot and is never used on
// the hot path.
func (r *WorkflowRegistry) All() []*WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()

	templates := make([]*WorkflowTemplate, 0, len(r.templates))
	for _, tmpl := range r.templates {
		templates = append(templates, tmpl)
	}
	sort.Slice(templates, func(i, j int) bool {
		return templates[i].Type < templates[j].Type
	})
	return templates
}

// AllDescriptions returns a formatted summary of every registered template,
// suitable for inclusion in an intent-classifier system prompt.
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

// BestTemplateScore returns the highest BM25 score of the text against all
// registered template documents (Name + Description + Keywords concatenated).
// This score is advisory only; workflow routing decisions must come from
// structured intent classification.
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

// BestTemplateType returns the WorkflowType of the template with the highest
// BM25 score for the given text. It is intended for diagnostics/ranking, not as
// a replacement for structured intent classification.
func (r *WorkflowRegistry) BestTemplateType(text string) WorkflowType {
	r.mu.Lock()
	if r.bm25Index == nil || r.bm25Dirty {
		r.rebuildBM25Locked()
	}
	idx := r.bm25Index
	r.mu.Unlock()

	if idx == nil {
		return ""
	}

	scores := idx.Score(text)
	bestScore := 0.0
	bestID := ""
	for id, s := range scores {
		if s > bestScore {
			bestScore = s
			bestID = id
		}
	}
	if bestID == "" || bestScore < 2.0 {
		return ""
	}
	return WorkflowType(bestID)
}

// rebuildBM25Locked rebuilds the BM25 index from all registered templates.
// Each template becomes one document with ID=Type and Text=Name+Description+Keywords.
// Must be called with r.mu held for writing.
func (r *WorkflowRegistry) rebuildBM25Locked() {
	docs := make([]bm25.Doc, 0, len(r.templates))
	for _, tmpl := range r.templates {
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
