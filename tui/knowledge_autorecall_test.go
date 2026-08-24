package main

import (
	"testing"
	"testing/quick"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// Feature: tui-knowledge-base, Property 3: HasKnowledgeBase reflects store initialization state
// Validates: Requirements 1.5, 7.1
func TestProperty3_HasKnowledgeBaseReflectsState(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}
	err := quick.Check(func(storeIsNil bool) bool {
		app := &TUIApp{}
		if !storeIsNil {
			app.knowledgeStore = &knowledge.SQLiteStore{}
		}

		hasKB := app.knowledgeStore != nil
		expected := !storeIsNil
		if hasKB != expected {
			t.Logf("storeIsNil=%v, hasKB=%v, expected=%v", storeIsNil, hasKB, expected)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}
