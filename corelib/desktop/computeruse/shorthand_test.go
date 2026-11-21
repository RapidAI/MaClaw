package computeruse

import (
	"encoding/json"
	"strings"
	"testing"
)

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

func TestParseStepsAcceptsAJSONStringAndNamesTheShape(t *testing.T) {
	steps, err := ParseSteps(`[{"Action":"click","x":12,"y":34}]`)
	if err != nil || len(steps) != 1 || steps[0].Kind != "click" || steps[0].X != 12 {
		t.Fatalf("json string = %+v %v", steps, err)
	}
	encoded, _ := json.Marshal(`[{"op":"key","key":"Return"}]`)
	nested, err := ParseSteps(string(encoded))
	if err != nil || len(nested) != 1 || nested[0].Kind != "key" {
		t.Fatalf("double encoded = %+v %v", nested, err)
	}
	_, err = ParseSteps([]any{map[string]any{"button": "left", "note": "secret-value"}})
	if err == nil || !strings.Contains(err.Error(), "needs \"action\"") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), "{button:string(4),note:string(12)}") {
		t.Fatalf("shape = %v", err)
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("shape included the value: %v", err)
	}
}
