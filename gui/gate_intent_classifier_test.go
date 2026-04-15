package main

import (
	"testing"
)

// TestGateIntentClassify_Layer1_KeywordClassification verifies that Layer 1
// keyword-based classification correctly maps messages to gate intents.
func TestGateIntentClassify_Layer1_KeywordClassification(t *testing.T) {
	// Create classifier with nil embedder (Layer 1 only).
	gic := NewGateIntentClassifier(nil)

	tests := []struct {
		name       string
		input      string
		wantIntent GateIntent
		wantLayer  int
		minConf    float64
	}{
		// --- new_project (creation keywords) ---
		{
			name:       "Chinese creation: develop a game",
			input:      "开发一个贪吃蛇游戏",
			wantIntent: GateIntentNewProject,
			wantLayer:  1,
			minConf:    0.90,
		},
		{
			name:       "English creation: build a web app",
			input:      "build a web application",
			wantIntent: GateIntentMaintenance, // "build" is in codingKeywords but not creationCodingKeywords
			wantLayer:  1,
			minConf:    0.80,
		},
		{
			name:       "Chinese creation: write a script",
			input:      "写一个脚本来处理数据",
			wantIntent: GateIntentNewProject,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- bug_fix (bugfix keywords, no creation) ---
		{
			name:       "Chinese bugfix: has bug loading",
			input:      "有bug，一直显示加载中",
			wantIntent: GateIntentBugFix,
			wantLayer:  1,
			minConf:    0.90,
		},
		{
			name:       "English bugfix: fix crash",
			input:      "fix the crash on startup",
			wantIntent: GateIntentBugFix,
			wantLayer:  1,
			minConf:    0.90,
		},
		{
			name:       "Chinese bugfix: debug issue",
			input:      "调试一下这个问题",
			wantIntent: GateIntentBugFix,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- non_coding (non-coding keywords, no coding) ---
		{
			name:       "Chinese non-coding: translate document",
			input:      "翻译文档",
			wantIntent: GateIntentNonCoding,
			wantLayer:  1,
			minConf:    0.90,
		},
		{
			name:       "Chinese non-coding: search papers",
			input:      "搜索论文",
			wantIntent: GateIntentNonCoding,
			wantLayer:  1,
			minConf:    0.90,
		},
		{
			name:       "Chinese non-coding: summarize article",
			input:      "总结文章",
			wantIntent: GateIntentNonCoding,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- maintenance (coding keywords that are refactor/optimize) ---
		{
			name:       "Chinese maintenance: refactor function",
			input:      "重构这个函数",
			wantIntent: GateIntentMaintenance,
			wantLayer:  1,
			minConf:    0.80,
		},
		{
			name:       "English maintenance: optimize queries",
			input:      "optimize the database queries",
			wantIntent: GateIntentMaintenance,
			wantLayer:  1,
			minConf:    0.80,
		},

		// --- Mixed intent: creation dominates bug-fix (Req 10.1) ---
		{
			name:       "Mixed: develop a bug tracking system",
			input:      "开发一个bug追踪系统",
			wantIntent: GateIntentNewProject,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- Mixed intent: bug-fix dominates maintenance (Req 10.2) ---
		{
			name:       "Mixed: fix bug then refactor",
			input:      "修复这个bug然后重构代码",
			wantIntent: GateIntentBugFix,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- Mixed intent: non-coding dominates coding (Req 10.3) ---
		{
			name:       "Mixed: translate code comments",
			input:      "翻译这段代码的注释",
			wantIntent: GateIntentNonCoding,
			wantLayer:  1,
			minConf:    0.90,
		},

		// --- Low confidence: no strong match ---
		{
			name:       "Ambiguous: hello",
			input:      "你好",
			wantIntent: GateIntentUnknown,
			wantLayer:  1,
			minConf:    0.0,
		},
		{
			name:       "Empty string",
			input:      "",
			wantIntent: GateIntentUnknown,
			wantLayer:  1,
			minConf:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gic.Classify(tt.input, "test-user")

			if result.Intent != tt.wantIntent {
				t.Errorf("Classify(%q): intent = %s, want %s (reason: %s)",
					tt.input, result.Intent, tt.wantIntent, result.Reason)
			}
			if result.Layer != tt.wantLayer {
				t.Errorf("Classify(%q): layer = %d, want %d",
					tt.input, result.Layer, tt.wantLayer)
			}
			if result.Confidence < tt.minConf {
				t.Errorf("Classify(%q): confidence = %.2f, want >= %.2f",
					tt.input, result.Confidence, tt.minConf)
			}
		})
	}
}

// TestGateIntentClassify_Layer1_MixedIntentDominance specifically tests the
// mixed-intent dominance rules from Requirement 10.
func TestGateIntentClassify_Layer1_MixedIntentDominance(t *testing.T) {
	gic := NewGateIntentClassifier(nil)

	tests := []struct {
		name       string
		input      string
		wantIntent GateIntent
		reason     string
	}{
		// Req 10.1: creation dominates bug-fix
		{
			name:       "creation + bug → new_project",
			input:      "开发一个bug追踪系统",
			wantIntent: GateIntentNewProject,
			reason:     "creation keyword '开发' dominates bug keyword 'bug'",
		},
		{
			name:       "creation + fix → new_project (Chinese)",
			input:      "开发一个bug追踪工具",
			wantIntent: GateIntentNewProject,
			reason:     "creation keyword '开发' dominates bug keyword 'bug'",
		},

		// Req 10.2: bug-fix dominates maintenance
		{
			name:       "bugfix + refactor → bug_fix",
			input:      "修复这个bug然后重构代码",
			wantIntent: GateIntentBugFix,
			reason:     "bugfix keyword '修复'+'bug' dominates maintenance '重构'",
		},
		{
			name:       "fix + optimize → bug_fix",
			input:      "fix the error and optimize performance",
			wantIntent: GateIntentBugFix,
			reason:     "bugfix keyword 'fix'+'error' dominates maintenance 'optimize'",
		},

		// Req 10.3: non-coding dominates coding when primary action is non-coding
		{
			name:       "translate + code → non_coding",
			input:      "翻译这段代码的注释",
			wantIntent: GateIntentNonCoding,
			reason:     "non-coding '翻译' is primary action, '代码' is object",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gic.Classify(tt.input, "test-user")
			if result.Intent != tt.wantIntent {
				t.Errorf("Classify(%q): intent = %s, want %s\n  reason: %s\n  got reason: %s",
					tt.input, result.Intent, tt.wantIntent, tt.reason, result.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mockContextProvider implements ConversationContextProvider for testing.
// ---------------------------------------------------------------------------

type mockContextProvider struct {
	messages []string
}

func (m *mockContextProvider) RecentMessages(userID string, n int) []string {
	if n > len(m.messages) {
		return m.messages
	}
	return m.messages[len(m.messages)-n:]
}

// TestGateIntentClassify_ContinuationWithCodingContext verifies that short
// continuation phrases return GateIntentContinuation with confidence ≥ 0.60
// when conversation context contains coding signals.
// Validates: Requirements 1.5, 6.1, 6.2, 6.3
func TestGateIntentClassify_ContinuationWithCodingContext(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	gic.SetContextProvider(&mockContextProvider{
		messages: []string{
			"我想开发一个贪吃蛇游戏",
			"好的，让我先帮你写需求文档",
			"需求文档已确认",
			"技术设计也确认了",
		},
	})

	tests := []struct {
		name  string
		input string
	}{
		{"Chinese: 继续", "继续"},
		{"Chinese: 开工", "开工"},
		{"Chinese: 开干", "开干"},
		{"Chinese: 动手", "动手"},
		{"Chinese: 搞起来", "搞起来"},
		{"Chinese: 好的", "好的"},
		{"English: go ahead", "go ahead"},
		{"English: continue", "continue"},
		{"English: start", "start"},
		{"English: ok", "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gic.Classify(tt.input, "test-user")

			if result.Intent != GateIntentContinuation {
				t.Errorf("Classify(%q): intent = %s, want %s (reason: %s)",
					tt.input, result.Intent, GateIntentContinuation, result.Reason)
			}
			if result.Confidence < 0.60 {
				t.Errorf("Classify(%q): confidence = %.2f, want >= 0.60",
					tt.input, result.Confidence)
			}
			if result.Layer != 1 {
				t.Errorf("Classify(%q): layer = %d, want 1",
					tt.input, result.Layer)
			}
		})
	}
}

// TestGateIntentClassify_ContinuationWithoutCodingContext verifies that short
// continuation phrases return low confidence (< 0.50) when conversation
// context does NOT contain coding signals.
// Validates: Requirements 1.6, 6.4
func TestGateIntentClassify_ContinuationWithoutCodingContext(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	gic.SetContextProvider(&mockContextProvider{
		messages: []string{
			"帮我翻译一下这篇文章",
			"好的，翻译完成了",
			"谢谢",
		},
	})

	tests := []struct {
		name  string
		input string
	}{
		{"Chinese: 继续", "继续"},
		{"Chinese: 开工", "开工"},
		{"English: go ahead", "go ahead"},
		{"English: ok", "ok"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gic.Classify(tt.input, "test-user")

			if result.Confidence >= 0.50 {
				t.Errorf("Classify(%q): confidence = %.2f, want < 0.50 (no coding context)",
					tt.input, result.Confidence)
			}
		})
	}
}

// TestGateIntentClassify_ContinuationNilProvider verifies that when no
// ConversationContextProvider is set, continuation phrases return low
// confidence (< 0.50) — same as no coding context.
func TestGateIntentClassify_ContinuationNilProvider(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	// No SetContextProvider call — ctxProvider is nil.

	result := gic.Classify("开工", "test-user")

	if result.Confidence >= 0.50 {
		t.Errorf("Classify(\"开工\"): confidence = %.2f, want < 0.50 (nil provider)",
			result.Confidence)
	}
}

// TestGateIntentClassify_ShortNonContinuationFallsThrough verifies that short
// messages that do NOT match continuation phrases fall through to keyword
// classification (or unknown).
func TestGateIntentClassify_ShortNonContinuationFallsThrough(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	gic.SetContextProvider(&mockContextProvider{
		messages: []string{
			"我想开发一个贪吃蛇游戏",
		},
	})

	tests := []struct {
		name       string
		input      string
		wantIntent GateIntent
	}{
		{
			name:       "Short greeting: 你好",
			input:      "你好",
			wantIntent: GateIntentUnknown,
		},
		{
			name:       "Short question: 什么",
			input:      "什么",
			wantIntent: GateIntentUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gic.Classify(tt.input, "test-user")
			if result.Intent != tt.wantIntent {
				t.Errorf("Classify(%q): intent = %s, want %s (reason: %s)",
					tt.input, result.Intent, tt.wantIntent, result.Reason)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Layer 3: LLM refinement tests
// ---------------------------------------------------------------------------

// TestParseGateLLMResponse_ValidJSON verifies that parseGateLLMResponse
// correctly parses valid JSON responses from the LLM.
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
			body:       `{"gate_intent": "new_project", "confidence": 0.95, "reason": "用户要开发新应用"}`,
			wantIntent: GateIntentNewProject,
			wantConf:   0.95,
			wantReason: "用户要开发新应用",
			wantLayer:  3,
		},
		{
			name:       "bug_fix",
			body:       `{"gate_intent": "bug_fix", "confidence": 0.88, "reason": "fixing a crash"}`,
			wantIntent: GateIntentBugFix,
			wantConf:   0.88,
			wantReason: "fixing a crash",
			wantLayer:  3,
		},
		{
			name:       "maintenance",
			body:       `{"gate_intent": "maintenance", "confidence": 0.75, "reason": "refactoring code"}`,
			wantIntent: GateIntentMaintenance,
			wantConf:   0.75,
			wantReason: "refactoring code",
			wantLayer:  3,
		},
		{
			name:       "non_coding",
			body:       `{"gate_intent": "non_coding", "confidence": 0.82, "reason": "翻译任务"}`,
			wantIntent: GateIntentNonCoding,
			wantConf:   0.82,
			wantReason: "翻译任务",
			wantLayer:  3,
		},
		{
			name:       "continuation",
			body:       `{"gate_intent": "continuation", "confidence": 0.70, "reason": "user wants to continue"}`,
			wantIntent: GateIntentContinuation,
			wantConf:   0.70,
			wantReason: "user wants to continue",
			wantLayer:  3,
		},
		{
			name:       "unknown intent maps to GateIntentUnknown",
			body:       `{"gate_intent": "unknown", "confidence": 0.30, "reason": "unclear"}`,
			wantIntent: GateIntentUnknown,
			wantConf:   0.30,
			wantReason: "unclear",
			wantLayer:  3,
		},
		{
			name:       "invalid intent string maps to unknown",
			body:       `{"gate_intent": "something_else", "confidence": 0.50, "reason": "invalid"}`,
			wantIntent: GateIntentUnknown,
			wantConf:   0.50,
			wantReason: "invalid",
			wantLayer:  3,
		},
		{
			name:       "confidence clamped to 0 when negative",
			body:       `{"gate_intent": "new_project", "confidence": -0.5, "reason": "negative"}`,
			wantIntent: GateIntentNewProject,
			wantConf:   0.0,
			wantReason: "negative",
			wantLayer:  3,
		},
		{
			name:       "confidence clamped to 1 when > 1",
			body:       `{"gate_intent": "bug_fix", "confidence": 1.5, "reason": "over"}`,
			wantIntent: GateIntentBugFix,
			wantConf:   1.0,
			wantReason: "over",
			wantLayer:  3,
		},
		{
			name:       "JSON wrapped in markdown code fence",
			body:       "```json\n{\"gate_intent\": \"new_project\", \"confidence\": 0.90, \"reason\": \"fenced\"}\n```",
			wantIntent: GateIntentNewProject,
			wantConf:   0.90,
			wantReason: "fenced",
			wantLayer:  3,
		},
		{
			name:       "JSON with extra text before",
			body:       "Here is the result: {\"gate_intent\": \"bug_fix\", \"confidence\": 0.80, \"reason\": \"extra text\"}",
			wantIntent: GateIntentBugFix,
			wantConf:   0.80,
			wantReason: "extra text",
			wantLayer:  3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseGateLLMResponse(tt.body)
			if err != nil {
				t.Fatalf("parseGateLLMResponse(%q): unexpected error: %v", tt.body, err)
			}
			if result.Intent != tt.wantIntent {
				t.Errorf("intent = %s, want %s", result.Intent, tt.wantIntent)
			}
			if result.Confidence != tt.wantConf {
				t.Errorf("confidence = %.2f, want %.2f", result.Confidence, tt.wantConf)
			}
			if result.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", result.Reason, tt.wantReason)
			}
			if result.Layer != tt.wantLayer {
				t.Errorf("layer = %d, want %d", result.Layer, tt.wantLayer)
			}
		})
	}
}

// TestParseGateLLMResponse_InvalidJSON verifies that parseGateLLMResponse
// returns an error for invalid JSON input.
func TestParseGateLLMResponse_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"empty string", ""},
		{"plain text", "this is not json"},
		{"incomplete JSON", `{"gate_intent": "new_project"`},
		{"array instead of object", `["new_project", 0.9, "reason"]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseGateLLMResponse(tt.body)
			if err == nil {
				t.Errorf("parseGateLLMResponse(%q): expected error, got nil", tt.body)
			}
		})
	}
}

// TestGateIntentClassify_Layer3_FallbackOnNoLLM verifies that when LLM is
// not configured, the classifier falls back to Layer 1/2 results without
// attempting an LLM call.
func TestGateIntentClassify_Layer3_FallbackOnNoLLM(t *testing.T) {
	gic := NewGateIntentClassifier(nil)
	// No SetLLMConfig call — llmConfig and httpClient are nil.

	// An ambiguous message that doesn't match keywords should return unknown
	// without attempting LLM.
	result := gic.Classify("你好世界", "test-user")
	if result.Intent != GateIntentUnknown {
		t.Errorf("Classify(\"你好世界\"): intent = %s, want %s", result.Intent, GateIntentUnknown)
	}
	if result.Layer != 1 {
		t.Errorf("Classify(\"你好世界\"): layer = %d, want 1 (no LLM available)", result.Layer)
	}
}

// TestGateClassifierSystemPrompt verifies the system prompt constant is
// non-empty and contains expected classification categories.
func TestGateClassifierSystemPrompt(t *testing.T) {
	if gateClassifierSystemPrompt == "" {
		t.Fatal("gateClassifierSystemPrompt is empty")
	}

	expectedCategories := []string{"new_project", "bug_fix", "maintenance", "non_coding", "continuation"}
	for _, cat := range expectedCategories {
		if !gateTestContains(gateClassifierSystemPrompt, cat) {
			t.Errorf("gateClassifierSystemPrompt missing category %q", cat)
		}
	}
}

// TestGateClassifierJSONSchema verifies the JSON schema has the expected
// structure and required fields.
func TestGateClassifierJSONSchema(t *testing.T) {
	if gateClassifierJSONSchema == nil {
		t.Fatal("gateClassifierJSONSchema is nil")
	}

	schemaType, ok := gateClassifierJSONSchema["type"].(string)
	if !ok || schemaType != "object" {
		t.Errorf("schema type = %v, want \"object\"", gateClassifierJSONSchema["type"])
	}

	required, ok := gateClassifierJSONSchema["required"].([]string)
	if !ok {
		t.Fatal("schema missing 'required' field")
	}
	wantRequired := map[string]bool{"gate_intent": true, "confidence": true, "reason": true}
	for _, r := range required {
		if !wantRequired[r] {
			t.Errorf("unexpected required field: %q", r)
		}
		delete(wantRequired, r)
	}
	for r := range wantRequired {
		t.Errorf("missing required field: %q", r)
	}
}

func gateTestContains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && len(s) >= len(substr) && gateTestContainsSubstring(s, substr)
}

func gateTestContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
