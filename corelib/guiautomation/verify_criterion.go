package guiautomation

import "github.com/RapidAI/CodeClaw/corelib/taskengine"

// VerifyCriterion is the JSON-friendly input format for gui_verify tool.
// It maps directly to taskengine.CriterionSpec but uses simpler field names
// for LLM consumption.
type VerifyCriterion struct {
	Type     string `json:"type"`               // text_contains, element_exists, element_value, window_exists
	Pattern  string `json:"pattern"`            // match pattern
	Selector string `json:"selector,omitempty"` // "role::name" for element_exists/element_value
	Window   string `json:"window,omitempty"`   // window title for scoping
}

// ToSpec converts to the unified taskengine.CriterionSpec.
func (c VerifyCriterion) ToSpec() taskengine.CriterionSpec {
	return taskengine.CriterionSpec{
		Type:     c.Type,
		Pattern:  c.Pattern,
		Selector: c.Selector,
		Window:   c.Window,
	}
}
