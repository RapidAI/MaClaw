package im

import (
	"fmt"
	"strings"
	"sync"
)

// WorkflowRegistry is a thread-safe registry of workflow templates.
type WorkflowRegistry struct {
	mu        sync.RWMutex
	templates map[WorkflowType]*WorkflowTemplate
}

// NewWorkflowRegistry creates a new registry with the 4 built-in templates
// pre-registered.
func NewWorkflowRegistry() *WorkflowRegistry {
	r := &WorkflowRegistry{
		templates: make(map[WorkflowType]*WorkflowTemplate),
	}
	r.templates[WorkflowCoding] = builtinCodingTemplate()
	r.templates[WorkflowProductDesign] = builtinProductDesignTemplate()
	r.templates[WorkflowInnovation] = builtinInnovationTemplate()
	r.templates[WorkflowBusinessPlan] = builtinBusinessPlanTemplate()
	return r
}

// Register adds or replaces a workflow template in the registry.
func (r *WorkflowRegistry) Register(t *WorkflowTemplate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.templates[t.Type] = t
}

// Match returns the template for the given workflow type, or nil if not found.
func (r *WorkflowRegistry) Match(wt WorkflowType) *WorkflowTemplate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.templates[wt]
}

// AllDescriptions returns a formatted string describing all registered
// templates (type, name, description, keywords). This is intended for
// inclusion in LLM prompts so the model can classify user intent.
// Output is sorted by type for deterministic ordering.
func (r *WorkflowRegistry) AllDescriptions() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Fixed order for deterministic output (important for LLM prompt caching).
	order := []WorkflowType{WorkflowCoding, WorkflowProductDesign, WorkflowInnovation, WorkflowBusinessPlan}

	var b strings.Builder
	for _, wt := range order {
		t, ok := r.templates[wt]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "- 类型: %s | 名称: %s | 描述: %s | 关键词: %s\n",
			t.Type, t.Name, t.Description, strings.Join(t.Keywords, ", "))
	}
	// Append any custom-registered types not in the fixed order.
	for wt, t := range r.templates {
		found := false
		for _, o := range order {
			if wt == o {
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(&b, "- 类型: %s | 名称: %s | 描述: %s | 关键词: %s\n",
				t.Type, t.Name, t.Description, strings.Join(t.Keywords, ", "))
		}
	}
	return b.String()
}
