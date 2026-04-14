package workflow

import (
	"fmt"
	"strings"
	"sync"
)

// WorkflowRegistry holds registered workflow templates and provides
// lookup by WorkflowType. It is safe for concurrent use.
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*WorkflowTemplate
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
// Each entry contains the template Name and Description.
func (r *WorkflowRegistry) AllDescriptions() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.templates) == 0 {
		return ""
	}

	var b strings.Builder
	for _, tmpl := range r.templates {
		fmt.Fprintf(&b, "- %s: %s\n", tmpl.Name, tmpl.Description)
	}
	return b.String()
}
