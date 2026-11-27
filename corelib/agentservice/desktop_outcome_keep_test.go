package agentservice

import (
	"encoding/json"
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

func TestDesktopOutcomeSeesSpilledDeliveryAndSendToUser(t *testing.T) {
	spilled := []agent.ConversationEntry{
		{Role: "tool", ToolName: "desktop", Content: "/home/desktop/tasks/s1/out/1.txt\nDelivered big.docx (90000 bytes) on this chat reply."},
		{Role: "tool", ToolName: "desktop", Content: "window list"},
	}
	if !desktopOutcomeDelivered(spilled) {
		t.Fatal("a delivery moved to a file was not seen")
	}
	sent := []agent.ConversationEntry{
		{Role: "tool", ToolName: "send_to_user", Content: "报告在 /home/desktop/Desktop/r.docx"},
		{Role: "tool", ToolName: "desktop", Content: "window list"},
	}
	if !desktopOutcomeDelivered(sent) {
		t.Fatal("a send_to_user record was not seen")
	}
	if desktopOutcomeDelivered([]agent.ConversationEntry{{Role: "tool", ToolName: "desktop", Content: "/tmp/x\nDelivered nothing"}}) {
		t.Fatal("a non-spill path line counted as a delivery")
	}
	cb := judgeCallbacks(t, `{"ok":false,"gap":"没有截图"}`)
	cb.desktopOutcomeMissContinues = desktopOutcomeContinueMax
	cb.connectorAsk.Text = "要发这封邮件吗？"
	if d := cb.ReviewDesktopOutcome("发邮件", "草稿", spilled[1:], nil); d.Replace != "" {
		t.Fatalf("a run whose send_to_user is set was replaced: %+v", d)
	}
}

func TestDesktopOutcomeEvidenceKeepsBotPDFAndWebEvidence(t *testing.T) {
	history := []agent.ConversationEntry{
		{Role: "tool", ToolName: "web_fetch", Content: "北京当前晴，未来七天预报"},
		{Role: "tool", ToolName: "desktop", Content: "Error: docker service rejected: no docker service is assigned to this user key"},
		{Role: "tool", ToolName: "bot_generate_pdf", Content: "PDF generation failed: PDF font is unavailable on this server"},
		{Role: "tool", ToolName: "desktop", Content: "Delivered 北京天气报告.pdf (1700 bytes) on this chat reply."},
	}
	used, ask, remoteOnly, sawSSH, text := desktopOutcomeEvidence(history)
	if !used || ask || remoteOnly || sawSSH {
		t.Fatalf("used=%v ask=%v remoteOnly=%v sawSSH=%v", used, ask, remoteOnly, sawSSH)
	}
	for _, want := range []string{"web_fetch: 北京当前晴", "bot_generate_pdf: PDF generation failed", "Delivered 北京天气报告.pdf"} {
		if !strings.Contains(text, want) {
			t.Fatalf("evidence missing %q: %s", want, text)
		}
	}
	if !desktopOutcomeDelivered(history) {
		t.Fatal("desktop delivery was not recognized with support evidence present")
	}
}

func TestDesktopOutcomeDeliveredRecognizesBotPDF(t *testing.T) {
	history := []agent.ConversationEntry{{Role: "tool", ToolName: "bot_generate_pdf", Content: "Delivered 北京天气报告.pdf (1700 bytes) on this chat reply."}}
	if !desktopOutcomeDelivered(history) {
		t.Fatal("bot_generate_pdf delivery was not recognized")
	}
}

func TestDesktopOutcomeJudgeSeesWebAndBotPDFEvidence(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(req.Messages)
		body = string(raw)
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true,\"gap\":\"\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{
		{Role: "tool", ToolName: "web_fetch", Content: "北京天气事实"},
		{Role: "tool", ToolName: "bot_generate_pdf", Content: "Delivered 北京天气报告.pdf (1700 bytes) on this chat reply."},
	}
	if decision := cb.ReviewDesktopOutcome("北京天气，生成pdf", "PDF已发送", history, nil); decision != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("decision=%+v", decision)
	}
	if !strings.Contains(body, "北京天气事实") || !strings.Contains(body, "bot_generate_pdf") || !strings.Contains(body, "北京天气报告.pdf") {
		t.Fatalf("judge did not receive support evidence: %s", body)
	}
}
