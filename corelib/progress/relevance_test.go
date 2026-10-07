package progress

import (
	"encoding/json"
	"math"
	"testing"
)

func TestRelevanceModelCosineCalibration(t *testing.T) {
	m := &RelevanceModel{Format: RelevanceModelFormat, Kind: "cosine", High: 0.70, Low: 0.40,
		TaskInstruction: "T", MessageInstruction: ""}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	for raw, want := range map[float64]float64{0.40: 0.30, 0.70: 0.60, 0.55: 0.45, 0.0: 0, 2.0: 1} {
		if got := m.Calibrate(raw); math.Abs(got-want) > 1e-9 {
			t.Fatalf("Calibrate(%v)=%v want %v", raw, got, want)
		}
	}
	if got := m.FormatTask("fix the bug"); got != "Instruct: T\nQuery: fix the bug" {
		t.Fatalf("FormatTask=%q", got)
	}
	if got := m.FormatMessage("also add tests"); got != "also add tests" {
		t.Fatalf("FormatMessage=%q (empty instruction = plain)", got)
	}
	u, v := []float32{1, 0}, []float32{1, 0}
	if r := m.Relevance(u, v); math.Abs(r-0.9) > 1e-9 { // raw cos 1 → 0.30 + 0.6/0.3*0.30
		t.Fatalf("Relevance=%v", r)
	}
	if r := m.Relevance([]float32{1}, v); r != -1 {
		t.Fatalf("width mismatch must give -1 (unavailable), got %v", r)
	}
	// Band behaviour through the unchanged scheduler.
	hi := Schedule(ScheduleInput{Relevance: m.Calibrate(0.71), Structure: StructureSignal{IsShort: false}})
	lo := Schedule(ScheduleInput{Relevance: m.Calibrate(0.39), Structure: StructureSignal{IsShort: false}})
	if hi.Action != ActionMerge || lo.Action != ActionQueue {
		t.Fatalf("raw 0.71 → %v (want merge), raw 0.39 → %v (want queue)", hi.Action, lo.Action)
	}
}

func TestRelevanceModelLogisticMatchesReference(t *testing.T) {
	d := 3
	w := make([]float64, 4*d+1)
	for i := range w {
		w[i] = 0.1 * float64(i%5-2)
	}
	m := &RelevanceModel{Format: RelevanceModelFormat, Kind: "logistic", Dims: d, W: w, B: 0.3, High: 0.8, Low: 0.4}
	b, _ := json.Marshal(m)
	pm, err := ParseRelevanceModel(b)
	if err != nil {
		t.Fatal(err)
	}
	u, v := []float32{3, 0, 4}, []float32{0, 2, 0} // un-normalised on purpose
	got, err := pm.RawScore(u, v)
	if err != nil {
		t.Fatal(err)
	}
	a := []float64{0.6, 0, 0.8}
	c := []float64{0, 1, 0}
	z := 0.3 + w[4*d]*0 // cos = 0
	for i := 0; i < d; i++ {
		z += w[i]*a[i] + w[d+i]*c[i] + w[2*d+i]*math.Abs(a[i]-c[i]) + w[3*d+i]*a[i]*c[i]
	}
	if want := 1 / (1 + math.Exp(-z)); math.Abs(got-want) > 1e-12 {
		t.Fatalf("RawScore=%v want %v", got, want)
	}
}

func TestParseRelevanceModelRejectsBadFiles(t *testing.T) {
	for name, doc := range map[string]string{
		"format":   `{"format":"x","kind":"cosine","high":0.7,"low":0.4}`,
		"kind":     `{"format":"maclaw-interrupt-relevance-v1","kind":"mlp","high":0.7,"low":0.4}`,
		"bands":    `{"format":"maclaw-interrupt-relevance-v1","kind":"cosine","high":0.4,"low":0.4}`,
		"w shape":  `{"format":"maclaw-interrupt-relevance-v1","kind":"logistic","dims":2,"w":[1,2,3],"high":0.7,"low":0.4}`,
		"not json": `{`,
	} {
		if _, err := ParseRelevanceModel([]byte(doc)); err == nil {
			t.Fatalf("%s: want error", name)
		}
	}
}
