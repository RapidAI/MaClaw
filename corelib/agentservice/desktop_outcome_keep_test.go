package agentservice

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func judgeCallbacks(t *testing.T, content string) *coreAgentCallbacks {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, content)
	}))
	t.Cleanup(server.Close)
	return &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
}

func TestDesktopOutcomeUnparseableCheckKeepsTheReply(t *testing.T) {
	cb := judgeCallbacks(t, "I think it is fine")
	cb.desktopOutcomeMissContinues = desktopOutcomeContinueMax
	h := []agent.ConversationEntry{{Role: "tool", ToolName: "desktop", Content: "window list"}}
	if d := cb.ReviewDesktopOutcome("写文档", "已完成", h, nil); d != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("judge error changed the reply: %+v", d)
	}
}

func TestDesktopOutcomeDeliveredRunIsNotReplaced(t *testing.T) {
	cb := judgeCallbacks(t, `{"ok":false,"gap":"没有截图"}`)
	cb.desktopOutcomeMissContinues = desktopOutcomeContinueMax
	h := []agent.ConversationEntry{
		{Role: "tool", ToolName: "desktop", Content: "Delivered a.docx (100 bytes) on this chat reply."},
		{Role: "tool", ToolName: "desktop", Content: "window list"},
	}
	if d := cb.ReviewDesktopOutcome("写文档", "文档已发送", h, nil); d.Replace != "" {
		t.Fatalf("delivered run replaced: %+v", d)
	}
	// Without a delivery the last miss still replaces.
	cb.desktopOutcomeMissContinues = desktopOutcomeContinueMax
	if d := cb.ReviewDesktopOutcome("写文档", "文档已发送", h[1:], nil); d.Replace == "" {
		t.Fatalf("undelivered miss kept the draft: %+v", d)
	}
}

func TestDesktopOutcomeEvidenceKeepsEveryDelivery(t *testing.T) {
	h := []agent.ConversationEntry{
		{Role: "tool", ToolName: "desktop", Content: "Delivered doc.docx (100 bytes)"},
		{Role: "tool", ToolName: "desktop", Content: "x"},
		{Role: "tool", ToolName: "desktop", Content: "Delivered shot.png (5 bytes)"},
	}
	for i := 0; i < 6; i++ {
		h = append(h, agent.ConversationEntry{Role: "tool", ToolName: "desktop", Content: "window list"})
	}
	_, _, _, _, text := desktopOutcomeEvidence(h)
	if !strings.Contains(text, "doc.docx") || !strings.Contains(text, "shot.png") {
		t.Fatalf("delivery dropped: %s", text)
	}
}
