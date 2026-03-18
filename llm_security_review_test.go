package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReview_LLMNotConfigured_ReturnsSafe(t *testing.T) {
	r := NewLLMSecurityReview(MaclawLLMConfig{})
	ctx := RiskContext{ToolName: "Bash", Arguments: map[string]interface{}{"command": "rm -rf /"}}
	assessment := RiskAssessment{Level: RiskCritical, Reason: "dangerous keyword"}

	verdict, explanation, err := r.Review(ctx, assessment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict != VerdictSafe {
		t.Errorf("expected safe, got %s", verdict)
	}
	if explanation == "" {
		t.Error("expected non-empty explanation")
	}
}

func TestReview_LLMNotConfigured_EmptyModel(t *testing.T) {
	r := NewLLMSecurityReview(MaclawLLMConfig{URL: "http://localhost:1234/v1", Key: "sk-test"})
	ctx := RiskContext{ToolName: "Write"}
	assessment := RiskAssessment{Level: RiskMedium}

	verdict, _, err := r.Review(ctx, assessment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict != VerdictSafe {
		t.Errorf("expected safe when model is empty, got %s", verdict)
	}
}

func TestReview_LLMReturnsVerdict(t *testing.T) {
	tests := []struct {
		name            string
		responseVerdict string
		expectedVerdict LLMSecurityVerdict
	}{
		{"safe", "safe", VerdictSafe},
		{"risky", "risky", VerdictRisky},
		{"dangerous", "dangerous", VerdictDangerous},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"message": map[string]string{
							"content": `{"verdict":"` + tt.responseVerdict + `","explanation":"test reason"}`,
						}},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()

			r := NewLLMSecurityReview(MaclawLLMConfig{
				URL:   srv.URL,
				Key:   "test-key",
				Model: "test-model",
			})

			ctx := RiskContext{ToolName: "Bash", SessionID: "s1"}
			assessment := RiskAssessment{Level: RiskHigh, Reason: "test"}

			verdict, explanation, err := r.Review(ctx, assessment)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict != tt.expectedVerdict {
				t.Errorf("expected %s, got %s", tt.expectedVerdict, verdict)
			}
			if explanation == "" {
				t.Error("expected non-empty explanation")
			}
		})
	}
}

func TestReview_LLMTimeout_FallsBackToRules(t *testing.T) {
	// Server that sleeps longer than the 5-second timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer srv.Close()

	review := &LLMSecurityReview{
		llmConfig: MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "m"},
		client:    &http.Client{Timeout: 100 * time.Millisecond}, // short timeout for test speed
	}

	tests := []struct {
		level    RiskLevel
		expected LLMSecurityVerdict
	}{
		{RiskCritical, VerdictDangerous},
		{RiskHigh, VerdictRisky},
		{RiskMedium, VerdictRisky},
		{RiskLow, VerdictSafe},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			ctx := RiskContext{ToolName: "Bash"}
			assessment := RiskAssessment{Level: tt.level}

			verdict, explanation, err := review.Review(ctx, assessment)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict != tt.expected {
				t.Errorf("expected %s for %s risk, got %s", tt.expected, tt.level, verdict)
			}
			if explanation == "" {
				t.Error("expected non-empty explanation on fallback")
			}
		})
	}
}

func TestReview_LLMHTTPError_FallsBackToRules(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	r := NewLLMSecurityReview(MaclawLLMConfig{URL: srv.URL, Key: "k", Model: "m"})
	ctx := RiskContext{ToolName: "Write"}
	assessment := RiskAssessment{Level: RiskHigh}

	verdict, _, err := r.Review(ctx, assessment)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verdict != VerdictRisky {
		t.Errorf("expected risky fallback for high risk, got %s", verdict)
	}
}

func TestParseLLMResponse_PlainText(t *testing.T) {
	tests := []struct {
		content  string
		expected LLMSecurityVerdict
	}{
		{`This operation looks dangerous because it deletes system files`, VerdictDangerous},
		{`The operation is risky but not immediately harmful`, VerdictRisky},
		{`This is a safe read-only operation`, VerdictSafe},
		{`I cannot determine the risk`, VerdictRisky}, // default
	}

	for _, tt := range tests {
		t.Run(tt.content[:20], func(t *testing.T) {
			verdict, _, err := parseSecurityVerdict(tt.content)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if verdict != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, verdict)
			}
		})
	}
}

func TestRuleBasedFallback(t *testing.T) {
	tests := []struct {
		level    RiskLevel
		expected LLMSecurityVerdict
	}{
		{RiskCritical, VerdictDangerous},
		{RiskHigh, VerdictRisky},
		{RiskMedium, VerdictRisky},
		{RiskLow, VerdictSafe},
	}
	for _, tt := range tests {
		verdict, reason := ruleBasedFallback(tt.level)
		if verdict != tt.expected {
			t.Errorf("ruleBasedFallback(%s): expected %s, got %s", tt.level, tt.expected, verdict)
		}
		if reason == "" {
			t.Errorf("ruleBasedFallback(%s): expected non-empty reason", tt.level)
		}
	}
}

func TestBuildSecurityPrompt(t *testing.T) {
	ctx := RiskContext{
		ToolName:       "Bash",
		Arguments:      map[string]interface{}{"command": "ls -la"},
		SessionID:      "session-1",
		ProjectPath:    "/home/user/project",
		PermissionMode: PermissionModeDefault,
		CallCount:      3,
	}
	assessment := RiskAssessment{Level: RiskMedium, Reason: "write tool"}

	prompt := buildSecurityPrompt(ctx, assessment)
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
	// Verify key information is present.
	for _, want := range []string{"Bash", "session-1", "/home/user/project", "medium", "ls -la"} {
		if !containsIgnoreCase(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestNormalizeVerdict(t *testing.T) {
	tests := []struct {
		input    string
		expected LLMSecurityVerdict
	}{
		{"safe", VerdictSafe},
		{"SAFE", VerdictSafe},
		{" Safe ", VerdictSafe},
		{"risky", VerdictRisky},
		{"dangerous", VerdictDangerous},
		{"unknown", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeVerdict(tt.input)
		if got != tt.expected {
			t.Errorf("normalizeVerdict(%q): expected %q, got %q", tt.input, tt.expected, got)
		}
	}
}

// mustMarshalChatResponse creates a mock OpenAI chat completion response body.
func mustMarshalChatResponse(content string) []byte {
	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": content}},
		},
	}
	data, _ := json.Marshal(resp)
	return data
}
