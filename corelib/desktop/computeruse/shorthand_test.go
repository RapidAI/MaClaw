package computeruse

import "testing"

func TestParseStepsShorthand(t *testing.T) {
	steps, err := ParseSteps([]any{
		map[string]any{"key": "ctrl+a"},
		map[string]any{"click": map[string]any{"x": 10, "y": 20}},
		map[string]any{"key": "ctrl+v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if steps[0].Kind != "key" || steps[1].Kind != "click" || !steps[1].HasXY || steps[1].X != 10 || steps[2].Kind != "key" {
		t.Fatalf("got %+v", steps)
	}
	if _, err := ParseSteps([]any{map[string]any{"type": "keypress", "keys": []any{"ctrl", "v"}}}); err != nil {
		t.Fatal(err)
	}
}
