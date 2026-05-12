package main

import (
	"strings"
	"testing"
)

func TestGateIntentClassify_NoLocalKeywordFallback(t *testing.T) {
	gic := NewGateIntentClassifier(nil)

	tests := []string{
		"build a web application",
		"fix the crash on startup",
		"translate this document",
		"go ahead",
		"continue",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			result := gic.Classify(input, "test-user")
			if result.Intent != GateIntentUnknown {
				t.Fatalf("expected unknown without UIC, embeddings, or LLM; got %s (%s)", result.Intent, result.Reason)
			}
			if result.Layer != 1 {
				t.Fatalf("expected conservative layer-1 unknown, got layer %d", result.Layer)
			}
			if strings.Contains(strings.ToLower(result.Reason), "keyword") {
				t.Fatalf("reason should not mention keyword fallback: %q", result.Reason)
			}
		})
	}
}

func TestShouldAcceptGateResult(t *testing.T) {
	tests := []struct {
		name string
		in   GateIntentResult
		want bool
	}{
		{
			name: "degraded new_project high confidence accepted",
			in:   GateIntentResult{Intent: GateIntentNewProject, Confidence: 0.95, Degraded: true},
			want: true,
		},
		{
			name: "degraded new_project low confidence rejected",
			in:   GateIntentResult{Intent: GateIntentNewProject, Confidence: 0.60, Degraded: true},
			want: false,
		},
		{
			name: "degraded non_coding moderate confidence accepted",
			in:   GateIntentResult{Intent: GateIntentNonCoding, Confidence: 0.55, Degraded: true},
			want: true,
		},
		{
			name: "degraded bug_fix moderate confidence accepted",
			in:   GateIntentResult{Intent: GateIntentBugFix, Confidence: 0.55, Degraded: true},
			want: true,
		},
		{
			name: "degraded unknown accepted (not clearly coding)",
			in:   GateIntentResult{Intent: GateIntentUnknown, Confidence: 0.80, Degraded: true},
			want: true,
		},
		{
			name: "confident semantic result accepted",
			in:   GateIntentResult{Intent: GateIntentBugFix, Confidence: 0.70},
			want: true,
		},
		{
			name: "low confidence semantic result rejected",
			in:   GateIntentResult{Intent: GateIntentMaintenance, Confidence: 0.40},
			want: false,
		},
		{
			name: "low confidence unknown rejected",
			in:   GateIntentResult{Intent: GateIntentUnknown, Confidence: 0.30},
			want: false,
		},
		{
			name: "high confidence unknown rejected",
			in:   GateIntentResult{Intent: GateIntentUnknown, Confidence: 0.90},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldAcceptGateResult(tt.in); got != tt.want {
				t.Fatalf("shouldAcceptGateResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseGateLLMResponse_ValidJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantIntent GateIntent
		wantConf   float64
		wantReason string
		wantLayer  int
	}{
		{
			name:       "new_project with high confidence",
			body:       `{"gate_intent":"new_project","confidence":0.91,"reason":"create app"}`,
			wantIntent: GateIntentNewProject,
			wantConf:   0.91,
			wantReason: "create app",
			wantLayer:  3,
		},
		{
			name:       "json fenced",
			body:       "```json\n{\"gate_intent\":\"continuation\",\"confidence\":0.72,\"reason\":\"continue prior task\"}\n```",
			wantIntent: GateIntentContinuation,
			wantConf:   0.72,
			wantReason: "continue prior task",
			wantLayer:  3,
		},
		{
			name:       "confidence clamped high",
			body:       `{"gate_intent":"bug_fix","confidence":1.7,"reason":"repair failure"}`,
			wantIntent: GateIntentBugFix,
			wantConf:   1.0,
			wantReason: "repair failure",
			wantLayer:  3,
		},
		{
			name:       "confidence clamped low",
			body:       `{"gate_intent":"non_coding","confidence":-1,"reason":"not code"}`,
			wantIntent: GateIntentNonCoding,
			wantConf:   0.0,
			wantReason: "not code",
			wantLayer:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseGateLLMResponse(tt.body)
			if err != nil {
				t.Fatalf("parseGateLLMResponse: %v", err)
			}
			if got.Intent != tt.wantIntent {
				t.Fatalf("intent = %s, want %s", got.Intent, tt.wantIntent)
			}
			if got.Confidence != tt.wantConf {
				t.Fatalf("confidence = %v, want %v", got.Confidence, tt.wantConf)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
			if got.Layer != tt.wantLayer {
				t.Fatalf("layer = %d, want %d", got.Layer, tt.wantLayer)
			}
		})
	}
}

func TestParseGateLLMResponse_InvalidJSON(t *testing.T) {
	tests := []string{
		"",
		`not json`,
		`{"gate_intent":"not_allowed","confidence":0.8,"reason":"bad enum"}`,
		`{"gate_intent":"unknown","confidence":"high","reason":"bad confidence"}`,
	}

	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			if _, err := parseGateLLMResponse(body); err == nil {
				t.Fatalf("expected error for %q", body)
			}
		})
	}
}

func TestGateClassifierSystemPrompt(t *testing.T) {
	for _, required := range []string{
		"new_project",
		"bug_fix",
		"maintenance",
		"non_coding",
		"continuation",
		"unknown",
		"Return only one JSON object",
	} {
		if !strings.Contains(gateClassifierSystemPrompt, required) {
			t.Fatalf("system prompt missing %q", required)
		}
	}
}

func TestGateClassifierJSONSchema(t *testing.T) {
	props, ok := gateClassifierJSONSchema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("schema properties missing")
	}
	intentProp, ok := props["gate_intent"].(map[string]interface{})
	if !ok {
		t.Fatal("gate_intent schema missing")
	}
	enums, ok := intentProp["enum"].([]string)
	if !ok {
		t.Fatal("gate_intent enum missing")
	}
	for _, want := range []string{"new_project", "bug_fix", "maintenance", "non_coding", "continuation", "unknown"} {
		found := false
		for _, got := range enums {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("schema enum missing %q", want)
		}
	}
}
