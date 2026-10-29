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
	"github.com/RapidAI/CodeClaw/corelib/demoskill"
)

func TestDemoSkillReviewChecksStepsThenDeliverable(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	stepDone := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		body := string(raw)
		ok, gap := true, ""
		switch {
		case strings.Contains(body, "屏幕上只有浏览器窗口") && !stepDone:
			ok, gap = false, "还有终端窗口"
		case strings.Contains(body, "交付物"):
			ok = stepDone
			gap = "桌面还没整理好"
		}
		verdict, _ := json.Marshal(map[string]any{"ok": ok, "gap": gap})
		content, _ := json.Marshal(string(verdict))
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, content)
	}))
	defer server.Close()
	sk := &demoskill.Skill{Name: "focus", Goal: "只留浏览器", Steps: []demoskill.Step{{Intent: "最小化其他窗口", Check: "屏幕上只有浏览器窗口"}}, Deliverable: "桌面只剩浏览器", Evidence: "截图"}
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{
		{Role: "tool", ToolName: "skill", Content: sk.Guidance(nil)},
		{Role: "tool", ToolName: "desktop", Content: "screenshot ok"},
	}
	first := cb.ReviewDesktopOutcome("整理桌面", "好了", history, nil)
	if !first.Continue || !strings.Contains(first.Message, "第 1 步") || !strings.Contains(first.Message, "还有终端窗口") || !strings.Contains(first.Message, "不要中止") {
		t.Fatalf("first=%+v", first)
	}
	stepDone = true
	second := cb.ReviewDesktopOutcome("整理桌面", "好了", history, nil)
	if second != (agent.DesktopOutcomeDecision{}) || !cb.demoProgress.Complete() {
		t.Fatalf("second=%+v complete=%v", second, cb.demoProgress.Complete())
	}
}

func TestDemoSkillReviewOffLeavesGenericCheck(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "")
	sk := &demoskill.Skill{Name: "x", Goal: "g", Steps: []demoskill.Step{{Intent: "a", Check: "c"}}, Deliverable: "d"}
	cb := &coreAgentCallbacks{}
	if _, handled := cb.reviewDemoSkill(nil, "u", "d", "e", []agent.ConversationEntry{{Role: "tool", Content: sk.Guidance(nil)}}, nil); handled || cb.demoProgress != nil {
		t.Fatal("flag off must not touch the review")
	}
}
