package memory

import (
	"strings"
	"testing"
)

// TestToolDescriptionContainsPaginationGuidance verifies the tool description
// includes usage guidance for cursor, exhaustive, and session parameters.
// Validates: Requirements 10.1, 10.2
func TestToolDescriptionContainsPaginationGuidance(t *testing.T) {
	desc := MemoryToolDescriptionBase

	checks := []struct {
		name    string
		keyword string
	}{
		{"cursor guidance", "has_more=true"},
		{"cursor usage", "cursor"},
		{"exhaustive guidance", "mode=exhaustive"},
		{"exhaustive trigger", "list all"},
		{"session guidance", "session=true"},
		{"session use case", "multi-step task"},
	}

	for _, c := range checks {
		if !strings.Contains(desc, c.keyword) {
			t.Errorf("MemoryToolDescriptionBase missing %s keyword %q", c.name, c.keyword)
		}
	}
}

// TestToolSchemaContainsNewParameters verifies the schema includes cursor,
// mode=exhaustive, and session=true parameter descriptions.
// Validates: Requirement 10.1
func TestToolSchemaContainsNewParameters(t *testing.T) {
	def := ToolDefinitionSchema()

	// cursor parameter
	cursorProp := def.Properties["cursor"]
	if cursorProp == nil {
		t.Fatal("schema missing 'cursor' property")
	}
	cursorDesc := extractDesc(cursorProp)
	if !strings.Contains(cursorDesc, "pagination") {
		t.Errorf("cursor description missing 'pagination': %q", cursorDesc)
	}
	if !strings.Contains(cursorDesc, "Mutually exclusive") {
		t.Errorf("cursor description missing mutual exclusion note: %q", cursorDesc)
	}

	// session parameter
	sessionProp := def.Properties["session"]
	if sessionProp == nil {
		t.Fatal("schema missing 'session' property")
	}
	sessionDesc := extractDesc(sessionProp)
	if !strings.Contains(sessionDesc, "scroll-through") {
		t.Errorf("session description missing 'scroll-through': %q", sessionDesc)
	}
	if !strings.Contains(sessionDesc, "multi-step") {
		t.Errorf("session description missing 'multi-step': %q", sessionDesc)
	}

	// mode parameter includes exhaustive
	modeProp := def.Properties["mode"]
	if modeProp == nil {
		t.Fatal("schema missing 'mode' property")
	}
	modeDesc := extractDesc(modeProp)
	if !strings.Contains(modeDesc, "exhaustive") {
		t.Errorf("mode description missing 'exhaustive': %q", modeDesc)
	}
	if !strings.Contains(modeDesc, "list all") {
		t.Errorf("mode description missing usage trigger 'list all': %q", modeDesc)
	}
}

// TestToolSchemaDescriptionUsesConstant verifies that the schema uses the
// shared MemoryToolDescriptionBase constant.
// Validates: Requirement 10.4
func TestToolSchemaDescriptionUsesConstant(t *testing.T) {
	def := ToolDefinitionSchema()
	if def.Description != MemoryToolDescriptionBase {
		t.Errorf("schema description does not match MemoryToolDescriptionBase constant")
	}
}

// TestBuildHasMoreHint verifies the has_more hint format.
// Validates: Requirement 10.3
func TestBuildHasMoreHint(t *testing.T) {
	cursor := "abc123token"
	hint := BuildHasMoreHint(cursor)

	if !strings.Contains(hint, cursor) {
		t.Errorf("hint missing cursor value: %q", hint)
	}
	if !strings.Contains(hint, "cursor='") {
		t.Errorf("hint missing cursor= format: %q", hint)
	}
	if !strings.Contains(hint, "more results") {
		t.Errorf("hint missing 'more results': %q", hint)
	}
}

// TestBuildTruncatedHint verifies the truncated hint format.
// Validates: Requirement 10.3
func TestBuildTruncatedHint(t *testing.T) {
	hint := BuildTruncatedHint(250)

	if !strings.Contains(hint, "250") {
		t.Errorf("hint missing total_matching count: %q", hint)
	}
	if !strings.Contains(hint, "mode=exhaustive") {
		t.Errorf("hint missing mode=exhaustive guidance: %q", hint)
	}
	if !strings.Contains(hint, "category filter") {
		t.Errorf("hint missing category filter suggestion: %q", hint)
	}
}

// TestParamDescConstants verifies parameter description constants are non-empty
// and contain expected keywords.
// Validates: Requirement 10.4
func TestParamDescConstants(t *testing.T) {
	if ParamDescCursor == "" {
		t.Error("ParamDescCursor is empty")
	}
	if ParamDescMode == "" {
		t.Error("ParamDescMode is empty")
	}
	if ParamDescSession == "" {
		t.Error("ParamDescSession is empty")
	}
	if HintHasMorePrefix == "" {
		t.Error("HintHasMorePrefix is empty")
	}
	if HintHasMoreSuffix == "" {
		t.Error("HintHasMoreSuffix is empty")
	}
	if HintTruncatedTemplate == "" {
		t.Error("HintTruncatedTemplate is empty")
	}
	if HintSessionExhausted == "" {
		t.Error("HintSessionExhausted is empty")
	}
}

func extractDesc(prop interface{}) string {
	switch v := prop.(type) {
	case map[string]string:
		return v["description"]
	case map[string]interface{}:
		if d, ok := v["description"]; ok {
			if s, ok := d.(string); ok {
				return s
			}
		}
	}
	return ""
}
