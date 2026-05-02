package workflow

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIntentUnderstanding_StartAndHandleInput(t *testing.T) {
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"coding","summary":"做一个CRM","goals":["客户管理"],"constraints":["多租户"],"confidence":0.7,"ready":false},"reply":"我理解你想做一个CRM系统","ready":false}`,
	}
	registry := NewWorkflowRegistry()
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, registry)

	// Start a session
	startResult, err := mgr.Start("u1", "帮我做一个CRM系统")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if startResult.Rejected {
		t.Error("Start should not reject a workflow task")
	}
	if startResult.Reply == "" {
		t.Error("Start returned empty reply")
	}
	if !mgr.HasActiveSession("u1") {
		t.Error("expected active session after Start")
	}

	// Handle follow-up input
	llm.Response = `{"intent":{"category":"coding","summary":"CRM with sales","goals":["客户管理","销售漏斗"],"constraints":["多租户"],"confidence":0.9,"ready":false},"reply":"好的，我更新了理解","ready":false}`
	reply2, ready, cancelled, _, err := mgr.HandleInput("u1", "还要有销售漏斗功能")
	if err != nil {
		t.Fatalf("HandleInput failed: %v", err)
	}
	if reply2 == "" {
		t.Error("HandleInput returned empty reply")
	}
	if ready {
		t.Error("should not be ready yet")
	}
	if cancelled {
		t.Error("should not be cancelled")
	}

	// Confirm ready
	llm.Response = `{"intent":{"category":"coding","summary":"CRM","ready":true},"reply":"好的，开始工作","ready":true}`
	_, ready3, _, _, err := mgr.HandleInput("u1", "开工")
	if err != nil {
		t.Fatalf("HandleInput (ready) failed: %v", err)
	}
	if !ready3 {
		t.Error("expected ready=true after confirmation")
	}
	// Session should be cleaned up after ready
	if mgr.HasActiveSession("u1") {
		t.Error("session should be removed after ready")
	}
}

func TestIntentUnderstanding_CancelWordDetection(t *testing.T) {
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"coding","summary":"test"},"reply":"ok","ready":false}`,
	}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	cancelWords := []string{"算了", "取消", "不做了"}
	for _, word := range cancelWords {
		// Start a session first
		_, err := mgr.Start("cancel_user", "做个系统")
		if err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		if !mgr.HasActiveSession("cancel_user") {
			t.Fatal("expected active session")
		}

		// Send cancel word
		_, _, cancelled, _, err := mgr.HandleInput("cancel_user", word)
		if err != nil {
			t.Fatalf("HandleInput(%q) failed: %v", word, err)
		}
		if !cancelled {
			t.Errorf("expected cancelled=true for %q", word)
		}
		if mgr.HasActiveSession("cancel_user") {
			t.Errorf("session should be removed after cancel word %q", word)
		}
	}
}

func TestIntentUnderstanding_SessionExpiry(t *testing.T) {
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"coding","summary":"test"},"reply":"ok","ready":false}`,
	}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	// Manually create an expired session
	mgr.mu.Lock()
	mgr.sessions["expired_user"] = &UnderstandingSession{
		ID:        "iu-expired",
		UserID:    "expired_user",
		State:     UnderstandingActive,
		UpdatedAt: time.Now().Add(-45 * time.Minute),
		CreatedAt: time.Now().Add(-45 * time.Minute),
	}
	mgr.mu.Unlock()

	if !mgr.HasActiveSession("expired_user") {
		t.Fatal("session should exist before cleanup")
	}

	mgr.CleanupExpired()

	if mgr.HasActiveSession("expired_user") {
		t.Error("expired session should be removed after CleanupExpired")
	}
}

func TestIntentUnderstanding_LLMResponseFormatError(t *testing.T) {
	// LLM returns non-JSON response — parseLLMIntentResponse falls back to
	// raw text with empty category, which means Rejected=true (category="")
	llm := &MockLLMCaller{
		Response: "这不是JSON格式的回复，我来帮你分析一下需求",
	}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	result, err := mgr.Start("u1", "做个系统")
	if err != nil {
		t.Fatalf("Start should not fail on bad JSON: %v", err)
	}
	// Non-JSON response → empty category → Rejected
	if !result.Rejected {
		t.Error("non-JSON LLM response should result in Rejected=true (empty category)")
	}
}

func TestIntentUnderstanding_LLMError(t *testing.T) {
	llm := &MockLLMCaller{
		Err: errors.New("connection timeout"),
	}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	_, err := mgr.Start("u1", "做个系统")
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}

func TestIntentUnderstanding_HandleInputNoSession(t *testing.T) {
	llm := &MockLLMCaller{Response: `{"reply":"ok","ready":false}`}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	_, _, _, _, err := mgr.HandleInput("nonexistent", "hello")
	if err == nil {
		t.Error("expected error for non-existent session")
	}
}

func TestParseLLMIntentResponse_TrailingComma(t *testing.T) {
	// LLMs frequently produce trailing commas in JSON. Go's json.Unmarshal
	// rejects them. parseLLMIntentResponse must strip them before parsing.
	raw := `{
		"intent": {
			"category": "presentation_design",
			"summary": "生成PPT",
			"goals": ["设计PPT"],
			"constraints": ["庆祝主题"],
			"confidence": 0.85,
			"ready": true
		},
		"reply": "收到！已经为你启动工作流。",
	}`

	reply, intent, ready, parseOK := parseLLMIntentResponse(raw)
	if !parseOK {
		t.Fatal("expected parseOK=true, trailing comma should be stripped")
	}
	if reply != "收到！已经为你启动工作流。" {
		t.Errorf("reply = %q, want extracted reply text", reply)
	}
	if intent.Category != "presentation_design" {
		t.Errorf("category = %q, want presentation_design", intent.Category)
	}
	if !ready {
		t.Error("expected ready=true (from intent.Ready)")
	}
}

func TestParseLLMIntentResponse_ReadyInsideIntent(t *testing.T) {
	// Some LLMs place "ready" inside the "intent" object instead of at
	// the top level. parseLLMIntentResponse must accept both locations.
	raw := `{"intent":{"category":"coding","summary":"test","ready":true},"reply":"开始吧"}`

	_, _, ready, parseOK := parseLLMIntentResponse(raw)
	if !parseOK {
		t.Fatal("expected parseOK=true")
	}
	if !ready {
		t.Error("expected ready=true from intent.Ready (top-level ready is missing)")
	}
}

func TestParseLLMIntentResponse_ReadyAtTopLevel(t *testing.T) {
	// Standard case: ready at top level.
	raw := `{"intent":{"category":"coding","summary":"test"},"reply":"ok","ready":true}`

	_, _, ready, parseOK := parseLLMIntentResponse(raw)
	if !parseOK {
		t.Fatal("expected parseOK=true")
	}
	if !ready {
		t.Error("expected ready=true from top-level ready")
	}
}

func TestStripTrailingJSONCommas(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`{"a": 1,}`, `{"a": 1}`},
		{`{"a": [1, 2,]}`, `{"a": [1, 2]}`},
		{`{"a": 1, "b": 2,}`, `{"a": 1, "b": 2}`},
		{`{"a": 1}`, `{"a": 1}`},               // no trailing comma — unchanged
		{`{"a": "hello,"}`, `{"a": "hello,"}`}, // comma inside string — unchanged (regex doesn't match)
	}
	for _, tc := range cases {
		got := stripTrailingJSONCommas(tc.in)
		if got != tc.want {
			t.Errorf("stripTrailingJSONCommas(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseLLMIntentResponse_Failure_ReturnsEmptyReply(t *testing.T) {
	// When JSON parsing fails, reply must be empty (not the raw LLM output).
	// This is the mechanism-level fix: callers check parseOK and provide
	// a user-friendly fallback instead of displaying raw JSON.
	raw := `{"intent": {"category": "coding", INVALID JSON HERE}`

	reply, _, _, parseOK := parseLLMIntentResponse(raw)
	if parseOK {
		t.Fatal("expected parseOK=false for invalid JSON")
	}
	if reply != "" {
		t.Errorf("on parse failure, reply should be empty, got %q", reply)
	}
}

func TestHandleInput_ParseFailure_PreservesSession(t *testing.T) {
	// When the LLM returns malformed JSON during HandleInput, the session must
	// stay active. The user's follow-up is still bound to the workflow
	// clarification task, and falling through to the normal agent loop can make
	// task-context classify it against unrelated prior history.
	llm := &MockLLMCaller{
		Response: `{"intent":{"category":"coding"},"reply":"ok","ready":false}`,
	}
	mgr := NewIntentUnderstandingManager(NullStore{}, llm, nil)

	// Start a session (this succeeds).
	_, err := mgr.Start("u1", "做个系统")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !mgr.HasActiveSession("u1") {
		t.Fatal("expected active session")
	}

	// Now make the LLM return malformed JSON for HandleInput.
	llm.Response = `{"intent": BROKEN, "reply": "should not see this"}`

	reply, ready, cancelled, _, err := mgr.HandleInput("u1", "开工")
	if err != nil {
		t.Fatalf("HandleInput should not error on parse failure: %v", err)
	}
	if reply == "" {
		t.Fatal("expected safe fallback reply on parse failure")
	}
	if ready {
		t.Fatal("parse failure must not mark workflow ready")
	}
	if cancelled {
		t.Fatal("parse failure must not cancel the session")
	}
	if !mgr.HasActiveSession("u1") {
		t.Error("session should remain active after parse failure")
	}
}

func TestBuildIntentParseFailureReply_HidesStructuredGarbage(t *testing.T) {
	cases := []string{
		`{"intent": BROKEN, "reply": "should not see this"}`,
		"```json\n{\"intent\": BROKEN}\n```",
		`前缀 {"intent": BROKEN}`,
	}
	for _, raw := range cases {
		reply := buildIntentParseFailureReply(raw)
		if strings.Contains(reply, "BROKEN") || strings.Contains(reply, "intent") {
			t.Fatalf("fallback leaked structured garbage: %q", reply)
		}
	}
}

func TestBuildIntentParseFailureReply_AllowsNaturalLanguageClarification(t *testing.T) {
	raw := "我已理解你想做一份激昂风格的抗战胜利纪念PPT，请确认受众和页数。"
	reply := buildIntentParseFailureReply(raw)
	if !strings.Contains(reply, "抗战胜利纪念PPT") {
		t.Fatalf("expected natural-language clarification to be preserved, got %q", reply)
	}
}
