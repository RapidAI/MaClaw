package llmservice

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProbeOutputBaselineAndProjection(t *testing.T) {
	n := func(v int64) *int64 { return &v }
	base := ProbeOutputBaseline("openai", nil, "", nil, n(128), nil, false)
	if base["max_tokens"] != 128 {
		t.Fatalf("small baseline=%v", base["max_tokens"])
	}
	out := projectSent(t, `{"max_tokens":128}`, base)
	if out["max_tokens"].Value != 128 || out["max_tokens"].Clipped {
		t.Fatalf("small=%#v", out["max_tokens"])
	}
	base = ProbeOutputBaseline("openai", nil, "", nil, n(100000), n(2000), false)
	out = projectSent(t, `{"max_tokens":16384,"max_completion_tokens":2000}`, base)
	if out["max_tokens"].Value != 16384 || !out["max_tokens"].Clipped || out["max_completion_tokens"].Value != 2000 || out["max_completion_tokens"].Clipped {
		t.Fatalf("both=%#v", out)
	}
	base = ProbeOutputBaseline("openai", nil, "", nil, n(16384), nil, false)
	out = projectSent(t, `{"max_tokens":16384}`, base)
	if out["max_tokens"].Clipped {
		t.Fatal("exact cap counted as clipped")
	}
	base = ProbeOutputBaseline("openai", nil, "", nil, nil, nil, false)
	if base["max_tokens"] != 64 {
		t.Fatalf("omitted baseline=%v", base["max_tokens"])
	}
	out = projectSent(t, `{"max_tokens":64}`, base)
	if out["max_tokens"].Value != 64 || out["max_tokens"].Clipped {
		t.Fatalf("omitted above cap=%#v", out["max_tokens"])
	}
	out = projectSent(t, `{"max_tokens":32}`, base)
	if out["max_tokens"].Value != 32 || !out["max_tokens"].Clipped {
		t.Fatalf("omitted below default=%#v", out["max_tokens"])
	}
	if tools := ProbeOutputBaseline("openai", nil, "", nil, nil, nil, true)["max_tokens"]; tools != 256 {
		t.Fatalf("tools baseline=%v", tools)
	}
	base = ProbeOutputBaseline("openai", nil, "", nil, n(0), nil, false)
	out = projectSent(t, `{"max_tokens":0}`, base)
	if out["max_tokens"].Value != 0 || out["max_tokens"].Clipped {
		t.Fatalf("zero=%#v", out["max_tokens"])
	}

	base = ProbeOutputBaseline("openai", map[string]any{"max_tokens": 10.5}, "", nil, nil, nil, false)
	if base["max_tokens"] != 10.5 {
		t.Fatalf("fraction baseline=%v", base["max_tokens"])
	}
	out = projectSent(t, `{"max_tokens":10.5}`, base)
	if out["max_tokens"].Value != 10 || out["max_tokens"].Clipped {
		t.Fatalf("kept fraction=%#v", out["max_tokens"])
	}
	base = ProbeOutputBaseline("openai", map[string]any{"max_tokens": 16384.9}, "", nil, nil, nil, false)
	if base["max_tokens"] != 16384.9 {
		t.Fatalf("over fraction baseline=%v", base["max_tokens"])
	}
	out = projectSent(t, `{"max_tokens":16384}`, base)
	if out["max_tokens"].Value != 16384 || !out["max_tokens"].Clipped {
		t.Fatalf("over fraction=%#v", out["max_tokens"])
	}
	base = ProbeOutputBaseline("openai", map[string]any{"max_tokens": float64(100000)}, "", nil, n(128), nil, false)
	if base["max_tokens"] != 100000 {
		t.Fatalf("extra raise baseline=%v", base["max_tokens"])
	}
	out = projectSent(t, `{"max_tokens":16384}`, base)
	if !out["max_tokens"].Clipped || out["max_tokens"].Value != 16384 {
		t.Fatalf("extra raise=%#v", out["max_tokens"])
	}

	anthropic := ProbeOutputBaseline("anthropic", nil, "", nil, n(100000), n(2000), false)
	if anthropic["max_tokens"] != 100000 {
		t.Fatalf("anthropic baseline=%v", anthropic)
	}
	if _, ok := anthropic["max_completion_tokens"]; ok {
		t.Fatal("anthropic baseline kept both fields")
	}
	responses := ProbeOutputBaseline("responses", nil, "", nil, n(111), n(222), false)
	if responses["max_output_tokens"] != 222 {
		t.Fatalf("responses baseline=%v", responses)
	}
	google := ProbeOutputBaseline("google", nil, "", nil, n(111), n(222), false)
	if google["maxOutputTokens"] != 222 {
		t.Fatalf("google baseline=%v", google)
	}
	out = projectSent(t, `{"generationConfig":{"maxOutputTokens":32}}`, google)
	if _, ok := out["maxOutputTokens"]; !ok || out["maxOutputTokens"].Value != 32 || !out["maxOutputTokens"].Clipped {
		t.Fatalf("google=%#v", out)
	}
	if ProjectProbeOutput(nil, base) != nil || ProjectProbeOutput([]byte(`{"messages":[]}`), base) != nil {
		t.Fatal("empty projection returned fields")
	}
}

func projectSent(t *testing.T, body string, baseline map[string]float64) map[string]ProbeOutputField {
	t.Helper()
	out := ProjectProbeOutput([]byte(body), baseline)
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "messages") || !strings.Contains(string(raw), `"clipped"`) {
		t.Fatalf("projection=%s", raw)
	}
	return out
}
