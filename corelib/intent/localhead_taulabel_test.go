package intent

import (
	"encoding/json"
	"testing"
)

// TestLocalHeadTauByLabel checks the optional per-label threshold map:
// listed labels use their own τ_c, others τ/τ_s, sensitive never below τ_s.
func TestLocalHeadTauByLabel(t *testing.T) {
	head := map[string]any{
		"labels": []string{"a", "b", "s", "x"}, "sensitive": []string{"s", "x"},
		"w2": [][]float64{{1, 0}, {0, 1}, {1, 1}, {0, 0}}, "b2": []float64{0, 0, 0, 0},
		"tau": 0.8, "tau_s": 0.95,
		"tau_by_label": map[string]float64{"a": 0.6, "s": 0.99, "x": 0.5},
	}
	b, _ := json.Marshal(head)
	h, err := ParseLocalHead(b)
	if err != nil {
		t.Fatal(err)
	}
	for l, want := range map[string]float64{"a": 0.6, "b": 0.8, "s": 0.99, "x": 0.95} {
		if got := h.Threshold(l); got != want {
			t.Errorf("Threshold(%s)=%v want %v", l, got, want)
		}
	}
	if _, _, d, th := h.Decide([]float64{0.7, 0.1, 0.1, 0.1}); d != LocalHeadAccept || th != 0.6 {
		t.Errorf("decide a@0.7: %s th %v", d, th)
	}
	head["tau_by_label"] = map[string]float64{"zz": 0.5}
	b, _ = json.Marshal(head)
	if _, err := ParseLocalHead(b); err == nil {
		t.Error("expected error for unknown label")
	}
	delete(head, "tau_by_label")
	b, _ = json.Marshal(head)
	h, _ = ParseLocalHead(b)
	if h.Threshold("a") != 0.8 || h.Threshold("s") != 0.95 {
		t.Error("default thresholds changed without tau_by_label")
	}
}
