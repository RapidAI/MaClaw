package computeruse

import "testing"

func TestStepWithoutActionFieldIsInferred(t *testing.T) {
	for _, m := range []map[string]any{
		{"op": "click", "x": 10.0, "y": 20.0},
		{"action": "", "name": "click", "x": 10.0, "y": 20.0},
		{"x": 10.0, "y": 20.0},
	} {
		step, err := ParseStep(m)
		if err != nil || step.Kind != "click" {
			t.Fatalf("%v: %+v %v", m, step, err)
		}
	}
	if _, err := ParseStep(map[string]any{"foo": 1}); err == nil {
		t.Fatal("empty step accepted")
	}
}
