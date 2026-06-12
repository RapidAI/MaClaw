package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cagent "github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

func TestClassifyTaskIntent_WithoutUIC_ReturnsUnknown(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	tests := []struct {
		name string
		text string
	}{
		{"coding task", "fix the Go project bug and edit code"},
		{"ssh task", "ssh to 10.0.0.8 and inspect nginx logs"},
		{"non-coding task", "translate this paper"},
		{"ambiguous task", "handle the production issue"},
		{"knowledge base", "save this AI coding benchmark report into the knowledge base"},
		{"promo ppt", "generate a promotional PPT"},
		{"product intro ppt", "make a product introduction PPT"},
		{"presentation doc", "organize this content into a presentation draft"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntent(tt.text)
			if result.Intent != intentUnknown {
				t.Fatalf("without UIC, expected unknown for %q, got %q", tt.text, result.Intent)
			}
			if result.Source != "semantic-unavailable" {
				t.Fatalf("expected semantic-unavailable source, got %q", result.Source)
			}
		})
	}
}

func TestSetUnifiedClassifierForIMWiresCoreAgentClassifier(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	uic := intent.New(intent.Config{Embedder: embedding.NoopEmbedder{}, LLMTimeout: time.Second})

	setUnifiedClassifierForIM(uic)

	result := cagent.ClassifyTaskIntent("anything")
	if result.Source != "uic" {
		t.Fatalf("expected core agent classifier to use shared UIC, got %#v", result)
	}
}

func TestClassifyTaskIntent_Empty(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	result := classifyTaskIntent("")
	if result.Intent != intentUnknown {
		t.Fatalf("expected unknown for empty text, got %q", result.Intent)
	}
	if result.Reason != "empty task text; no execution route classified" {
		t.Fatalf("expected empty task fallback reason, got %q", result.Reason)
	}
	if result.Confidence != 0.3 {
		t.Fatalf("expected empty task confidence 0.3, got %v", result.Confidence)
	}
}

func TestClassifyTaskIntentWithoutSemantic_DoesNotGuessWithoutSemantic(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "coding-looking", text: "fix the crash and edit code"},
		{name: "ssh-looking", text: "ssh to the server and inspect nginx logs"},
		{name: "non-coding-looking", text: "translate this paper"},
		{name: "knowledge base", text: "save this report into the knowledge base"},
		{name: "chinese coding-looking", text: "\u4fee\u6539\u4ee3\u7801\u5e76\u4fee\u590d bug"},
		{name: "chinese knowledge base", text: "\u628a\u8fd9\u4efd\u62a5\u544a\u653e\u5165\u77e5\u8bc6\u5e93"},
		{name: "empty", text: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntentWithoutSemantic(tt.text)
			if result.Intent != intentUnknown {
				t.Fatalf("intent = %q, want %q (%#v)", result.Intent, intentUnknown, result)
			}
			if result.Source != "semantic-unavailable" {
				t.Fatalf("expected semantic-unavailable source, got %q", result.Source)
			}
			if len(result.Evidence) != 0 || result.Matched != "" {
				t.Fatalf("expected no local evidence, got matched=%q evidence=%#v", result.Matched, result.Evidence)
			}
		})
	}
}

func TestClassifyTaskIntentForSessionGuard_UsesHandlerUICWhenGlobalUnavailable(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	h := &IMMessageHandler{
		unifiedClassifier: intent.New(intent.Config{
			Embedder:   embedding.NoopEmbedder{},
			LLMTimeout: time.Second,
		}),
	}

	result := h.classifyTaskIntentForSessionGuard("anything")
	if result.Source != "uic" {
		t.Fatalf("expected handler UIC source, got %#v", result)
	}
}

func TestClassifyTaskIntentWithUIC_UsesAppUICWhenHandlerFieldEmpty(t *testing.T) {
	h := &IMMessageHandler{
		app: &App{
			unifiedClassifier: intent.New(intent.Config{
				Embedder:   embedding.NoopEmbedder{},
				LLMTimeout: time.Second,
			}),
		},
	}

	result, ok := h.classifyTaskIntentWithUIC("anything")
	if !ok {
		t.Fatal("expected app UIC to be used")
	}
	if result.Source != "uic" {
		t.Fatalf("expected uic source, got %#v", result)
	}
}

func TestClassifyTaskIntentWithUIC_NilHandlerReturnsFalse(t *testing.T) {
	var h *IMMessageHandler
	result, ok := h.classifyTaskIntentWithUIC("anything")
	if ok {
		t.Fatalf("expected nil handler to have no UIC, got %#v", result)
	}
}

func TestShouldConsiderExecutionConfirmation(t *testing.T) {
	if !shouldConsiderExecutionConfirmation(true, IMUserMessage{Text: "  fix bug  "}, "fix bug") {
		t.Fatal("expected fresh foreground task with text to be considered")
	}
	if shouldConsiderExecutionConfirmation(false, IMUserMessage{Text: "fix bug"}, "fix bug") {
		t.Fatal("non-fresh task should not be considered")
	}
	if shouldConsiderExecutionConfirmation(true, IMUserMessage{Text: "fix bug", IsBackground: true}, "fix bug") {
		t.Fatal("background task should not be considered")
	}
	if shouldConsiderExecutionConfirmation(true, IMUserMessage{Text: "   "}, "") {
		t.Fatal("empty text should not be considered")
	}
}

func TestShouldRequireExecutionConfirmationForIntent(t *testing.T) {
	msg := IMUserMessage{Text: "fix bug"}
	if !shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentCoding}) {
		t.Fatal("coding task should require confirmation")
	}
	if !shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentSSH}) {
		t.Fatal("ssh task should require confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentAmbiguous}) {
		t.Fatal("ambiguous task should use ordinary agent path, not pre-execution confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentUnknown}) {
		t.Fatal("unknown task should use ordinary agent path, not pre-execution confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentNonCoding}) {
		t.Fatal("non-coding task should not require coding execution confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(msg, nil, taskIntentResult{Intent: intentCoding, Matched: "continuation"}) {
		t.Fatal("continuation should not require a new execution confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(IMUserMessage{Text: "fix bug", IsBackground: true}, nil, taskIntentResult{Intent: intentCoding}) {
		t.Fatal("background task should not require confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(IMUserMessage{Text: "   "}, nil, taskIntentResult{Intent: intentCoding}) {
		t.Fatal("empty task text should not require confirmation")
	}
	if shouldRequireExecutionConfirmationForIntent(msg, &pendingConfirmation{}, taskIntentResult{Intent: intentCoding}) {
		t.Fatal("existing pending confirmation should block a new one")
	}
}

func TestNormalizeIntentClassification(t *testing.T) {
	result, err := normalizeIntentClassification(llmIntentClassification{
		Intent:     "non_coding",
		Confidence: 0.92,
		Reason:     "user is organizing source material and generating a promotional PPT",
		Evidence:   []string{"organizing material", "promotional PPT"},
	})
	if err != nil {
		t.Fatalf("normalizeIntentClassification: %v", err)
	}
	if result.Intent != intentNonCoding {
		t.Fatalf("expected non_coding intent, got %q", result.Intent)
	}
	if result.Source != "llm" {
		t.Fatalf("expected llm source, got %q", result.Source)
	}
	if result.Confidence != 0.92 {
		t.Fatalf("unexpected confidence: %v", result.Confidence)
	}
	if result.Reason == "" {
		t.Fatal("expected reason to be preserved")
	}
}

func TestDecodeIntentClassificationContent_StripsCodeFence(t *testing.T) {
	parsed, err := decodeIntentClassificationContent("```json\n{\"intent\":\"coding\",\"confidence\":0.8,\"reason\":\"explicitly asks to edit code\",\"evidence\":[\"edit code\"]}\n```")
	if err != nil {
		t.Fatalf("decodeIntentClassificationContent: %v", err)
	}
	if parsed.Intent != "coding" {
		t.Fatalf("expected coding intent, got %q", parsed.Intent)
	}
}

func TestRequestIntentClassificationUsesResponsesWireAPI(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("Decode: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"{\"intent\":\"coding\",\"confidence\":0.9,\"reason\":\"edit code\",\"evidence\":[\"fix\"]}"}]}]}`)),
			Request:    r,
		}, nil
	})}
	h := &IMMessageHandler{}

	got, err := h.requestIntentClassification(corelib.MaclawLLMConfig{
		URL:     "https://open.bigmodel.cn/api/paas/v4",
		Model:   "glm-5.1",
		WireAPI: "responses",
	}, "user-1", []interface{}{map[string]interface{}{"role": "user", "content": "fix bug"}}, client)
	if err != nil {
		t.Fatalf("requestIntentClassification: %v", err)
	}
	if got.Intent != intentCoding || got.Confidence != 0.9 {
		t.Fatalf("classification = %#v, want coding 0.9", got)
	}
	if gotPath != "/api/paas/v4/responses" {
		t.Fatalf("path = %q, want /api/paas/v4/responses", gotPath)
	}
	if _, ok := gotBody["input"]; !ok {
		t.Fatalf("request body missing input: %#v", gotBody)
	}
	if _, ok := gotBody["messages"]; ok {
		t.Fatalf("request body leaked messages: %#v", gotBody)
	}
	if text, ok := gotBody["text"].(map[string]interface{}); !ok || text["format"] == nil {
		t.Fatalf("responses text.format missing: %#v", gotBody)
	}
}

func TestSummarizeAttachmentTypesAndNames(t *testing.T) {
	attachments := []MessageAttachment{
		{Type: "image", FileName: "shot.png"},
		{MimeType: "application/pdf", FileName: "brief.pdf"},
		{Type: "image", FileName: "shot2.png"},
	}
	types := summarizeAttachmentTypes(attachments)
	if len(types) != 2 || types[0] != "image" || types[1] != "application" {
		t.Fatalf("unexpected attachment types: %#v", types)
	}
	names := summarizeAttachmentNames(attachments)
	if len(names) != 3 || names[0] != "shot.png" {
		t.Fatalf("unexpected attachment names: %#v", names)
	}
}

func TestConfirmationGate_PresentationTask_WithoutUIC(t *testing.T) {
	setUnifiedClassifierForIM(nil)
	t.Cleanup(func() { setUnifiedClassifierForIM(nil) })
	result := classifyTaskIntent("generate a promotional PPT")
	if result.Intent != intentUnknown {
		t.Fatalf("expected unknown for PPT task without UIC, got %q", result.Intent)
	}
}

func TestFormatIntentEvidence(t *testing.T) {
	r := taskIntentResult{Evidence: []string{"semantic evidence"}}
	formatted := formatIntentEvidence(r)
	if !strings.Contains(formatted, "semantic evidence") {
		t.Fatalf("expected evidence to be formatted, got %q", formatted)
	}
}
