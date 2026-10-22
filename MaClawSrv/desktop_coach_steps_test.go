package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

func TestRichFillAppRunStepsParse(t *testing.T) {
	for _, raw := range []any{
		[]any{map[string]any{"action": "click", "x": 10.0, "y": 20.0}},
		[]any{map[string]any{"action": "key", "key": "ctrl+a"}},
		[]any{map[string]any{"action": "key", "key": "ctrl+v"}},
	} {
		if _, err := computeruse.ParseSteps(raw); err != nil {
			t.Fatalf("%v: %v", raw, err)
		}
	}
}
