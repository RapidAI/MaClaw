package llm

import (
	"context"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestApplyWorkloadHintHeaders_HubManagedSendsHints(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://hub.example.com/api/llm/v1/chat/completions", nil)
	cfg := corelib.MaclawLLMConfig{
		URL:   "https://hub.example.com/api/llm/v1",
		Model: "auto",
	}.WithHubWorkloadHints("fast", "coding", "implementation")

	ApplyWorkloadHintHeaders(req, cfg)

	if got := req.Header.Get(llmpool.TaskTypeHeader); got != "fast" {
		t.Fatalf("task type = %q", got)
	}
	if got := req.Header.Get(llmpool.WorkflowTypeHeader); got != "coding" {
		t.Fatalf("workflow type = %q", got)
	}
	if got := req.Header.Get(llmpool.PhaseKindHeader); got != "implementation" {
		t.Fatalf("phase kind = %q", got)
	}
	if got := req.Header.Get(llmpool.WorkloadClassHeader); got != "" {
		t.Fatalf("desktop must not invent P0 class, got %q", got)
	}
}

func TestApplyWorkloadHintHeaders_ThirdPartyNeverSends(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	cfg := corelib.MaclawLLMConfig{
		URL:              "https://api.openai.com/v1",
		Model:            "gpt-4o",
		TaskTypeHint:     "fast",
		WorkflowTypeHint: "coding",
		PhaseKindHint:    "implementation",
	}

	ApplyWorkloadHintHeaders(req, cfg)

	if got := req.Header.Get(llmpool.TaskTypeHeader); got != "" {
		t.Fatalf("third-party sent task hint %q", got)
	}
	if got := req.Header.Get(llmpool.WorkflowTypeHeader); got != "" {
		t.Fatalf("third-party sent workflow hint %q", got)
	}
}

func TestNewOpenAIChatRequest_AttachesHubHints(t *testing.T) {
	cfg := corelib.MaclawLLMConfig{
		URL:   "https://hub.example.com/api/llm/v1",
		Model: "auto",
		Key:   "k",
	}.WithHubWorkloadHints("reasoning", "business_plan", "document_planning")

	req, _, _, err := NewOpenAIChatRequest(context.Background(), cfg,
		[]interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		OpenAIChatRequestOptions{Stream: false})
	if err != nil {
		t.Fatalf("NewOpenAIChatRequest: %v", err)
	}
	if got := req.Header.Get(llmpool.WorkflowTypeHeader); got != "business_plan" {
		t.Fatalf("workflow type = %q", got)
	}
	if got := req.Header.Get(llmpool.PhaseKindHeader); got != "document_planning" {
		t.Fatalf("phase kind = %q", got)
	}
	if got := req.Header.Get(llmpool.TaskTypeHeader); got != "reasoning" {
		t.Fatalf("task type = %q", got)
	}
}
