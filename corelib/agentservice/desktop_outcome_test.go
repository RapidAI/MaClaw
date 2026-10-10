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

func TestDesktopOutcomePlanAndHandoffSkipTheCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("plan and handoff must not call the checker")
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "plan"},
	}
	if decision := cb.ReviewDesktopOutcome("做个贪吃蛇", "完成了", nil, nil); decision != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("plan decision=%+v", decision)
	}
	cb.messageMetadata["bot_phase"] = "execute"
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "desktop", Content: agent.AskUserResultMarker(&agent.AskUserRequest{Question: "请登录"}),
	}}
	if decision := cb.ReviewDesktopOutcome("做个贪吃蛇", "Score: 10", history, nil); decision != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("handoff decision=%+v", decision)
	}
}

func TestDesktopOutcomeWithoutDesktopWorkContinuesTwice(t *testing.T) {
	cb := &coreAgentCallbacks{messageMetadata: map[string]string{"bot_phase": "execute"}}
	first := cb.ReviewDesktopOutcome("做个贪吃蛇", "已经完成", nil, nil)
	if !first.Continue || !strings.Contains(first.Message, "没有桌面工具结果") || strings.Contains(first.Message, "已经完成") {
		t.Fatalf("first=%+v", first)
	}
	second := cb.ReviewDesktopOutcome("做个贪吃蛇", "已经完成", nil, nil)
	if !second.Continue {
		t.Fatalf("second=%+v", second)
	}
	third := cb.ReviewDesktopOutcome("做个贪吃蛇", "请告诉我安装到哪", nil, nil)
	if third != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("third=%+v", third)
	}
}

func TestDesktopOutcomeIdleRemindersLeaveTheRepairRounds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"终端是空的\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	if !cb.ReviewDesktopOutcome("做个贪吃蛇", "完成了", nil, nil).Continue {
		t.Fatal("first idle finish was published")
	}
	if !cb.ReviewDesktopOutcome("做个贪吃蛇", "完成了", nil, nil).Continue {
		t.Fatal("second idle finish was published")
	}
	history := []agent.ConversationEntry{{Role: "tool", ToolName: "desktop", Content: "bash: 未找到命令"}}
	miss := cb.ReviewDesktopOutcome("做个贪吃蛇", "Score: 10", history, nil)
	if !miss.Continue || !strings.Contains(miss.Message, "终端是空的") {
		t.Fatalf("repair round was already spent: %+v", miss)
	}
}

func TestDesktopOutcomeEvidenceKeepsTheCommandTail(t *testing.T) {
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
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "desktop", Content: strings.Repeat("前置说明。", 200) + "bash: 未找到命令",
	}}
	if decision := cb.ReviewDesktopOutcome("做个贪吃蛇", "完成了", history, nil); decision != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("decision=%+v", decision)
	}
	if !strings.Contains(body, "未找到命令") || !strings.Contains(body, "没有当前桌面截图") {
		t.Fatalf("evidence lost the outcome: %s", body)
	}
}

func TestDesktopOutcomeRejectsAReportTheScreenDoesNotSupport(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(req.Messages)
		bodies = append(bodies, string(raw))
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"终端是空的，画面是网页\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k", SupportsVision: true},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "desktop", Content: "bash: 未找到命令",
	}}
	image := &agent.ToolModelImage{MIME: "image/png", Base64: "aaaa"}
	draft := "Score: 10，游戏已运行"
	first := cb.ReviewDesktopOutcome("开发图形贪吃蛇并运行", draft, history, image)
	if !first.Continue || !strings.Contains(first.Message, "终端是空的") || strings.Contains(first.Message, "Score") {
		t.Fatalf("first=%+v", first)
	}
	second := cb.ReviewDesktopOutcome("开发图形贪吃蛇并运行", draft, history, image)
	if !second.Continue {
		t.Fatalf("second=%+v", second)
	}
	third := cb.ReviewDesktopOutcome("开发图形贪吃蛇并运行", draft, history, image)
	if third.Continue || third.Replace != "终端是空的，画面是网页" || strings.Contains(third.Replace, "Score") {
		t.Fatalf("third=%+v", third)
	}
	for _, body := range bodies {
		if strings.Contains(body, "Score: 10") || !strings.Contains(body, "未找到命令") || !strings.Contains(body, "aaaa") || !strings.Contains(body, "贪吃蛇") {
			t.Fatalf("checker evidence=%s", body)
		}
	}
}

func TestDesktopOutcomeAcceptsEvidenceThatMeetsTheRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":true,\"gap\":\"\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "desktop", Content: "Saved /home/desktop/Desktop/北京天气.pdf",
	}}
	decision := cb.ReviewDesktopOutcome("查天气并生成 PDF", "文件已附上", history, nil)
	if decision != (agent.DesktopOutcomeDecision{}) {
		t.Fatalf("accepted work was held back: %+v", decision)
	}
}

func TestDesktopOutcomeReadsAnSSHResult(t *testing.T) {
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
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"远程命令没有跑起来\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "ssh", Content: "training started",
	}}
	first := cb.ReviewDesktopOutcome("在远程机器上启动训练", "已经开始训练", history, nil)
	if !first.Continue || !strings.Contains(first.Message, "远程命令没有跑起来") || !strings.Contains(first.Message, "ssh") || strings.Contains(first.Message, "没有桌面工具结果") || strings.Contains(first.Message, "已经开始训练") {
		t.Fatalf("ssh miss: %+v", first)
	}
	if !strings.Contains(body, "training started") || !strings.Contains(body, "ssh:") {
		t.Fatalf("checker missed the ssh result: %s", body)
	}
}

func TestDesktopOutcomeKeepsSSHAfterLaterDesktopResults(t *testing.T) {
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "ssh", Content: "remote-ok-marker",
	}}
	for i := 1; i <= 5; i++ {
		history = append(history, agent.ConversationEntry{
			Role: "tool", ToolName: "desktop", Content: fmt.Sprintf("desk-%d", i),
		})
	}
	for i := 1; i <= 3; i++ {
		history = append(history, agent.ConversationEntry{
			Role: "tool", ToolName: "ssh", Content: fmt.Sprintf("remote-%d", i),
		})
	}
	used, ask, remoteOnly, sawSSH, text := desktopOutcomeEvidence(history)
	if !used || ask || remoteOnly || !sawSSH {
		t.Fatalf("used=%v ask=%v remoteOnly=%v sawSSH=%v", used, ask, remoteOnly, sawSSH)
	}
	if !strings.Contains(text, "ssh: remote-2") || !strings.Contains(text, "ssh: remote-3") || strings.Contains(text, "remote-ok-marker") || strings.Contains(text, "ssh: remote-1") {
		t.Fatalf("ssh evidence: %s", text)
	}
	if !strings.Contains(text, "desktop: desk-5") || !strings.Contains(text, "desktop: desk-2") || strings.Contains(text, "desktop: desk-1") {
		t.Fatalf("desktop evidence: %s", text)
	}
}

func TestDesktopOutcomeMixedMissKeepsTheRemoteFollow(t *testing.T) {
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
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"两边都没好\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k", SupportsVision: true},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	history := []agent.ConversationEntry{
		{Role: "tool", ToolName: "desktop", Content: "画面是网页"},
		{Role: "tool", ToolName: "ssh", Content: "training started"},
	}
	draft := "已经开始训练"
	first := cb.ReviewDesktopOutcome("在远程机器上启动并在桌面打开页面", draft, history, &agent.ToolModelImage{MIME: "image/png", Base64: "aaaa"})
	if !first.Continue || !strings.Contains(first.Message, "两边都没好") || !strings.Contains(first.Message, "远程机器上的要求继续用 ssh") || !strings.Contains(first.Message, "桌面截图不能用来判断远程命令") || strings.Contains(first.Message, draft) {
		t.Fatalf("mixed miss: %+v", first)
	}
	if strings.Contains(first.Message, "继续用 ssh 工具在那台远程机器上改，直到 ssh 的工具结果能够证明要求已经达成。") {
		t.Fatalf("mixed miss used the remote-only follow: %+v", first)
	}
	if !strings.Contains(body, "training started") || !strings.Contains(body, "ssh:") || !strings.Contains(body, "画面是网页") || !strings.Contains(body, "不能用来否定 ssh 的输出") || !strings.Contains(body, "桌面截图也不能把它判成未达成") {
		t.Fatalf("checker mixed evidence: %s", body)
	}
}

func TestDesktopOutcomeDoesNotTreatACommandLineAsSSH(t *testing.T) {
	marker := "ssh: Connection refused"
	// The clip keeps a 266-rune tail. The marker sits at the start of that tail.
	body := strings.Repeat("头", 200) + marker + strings.Repeat("尾", 243)
	history := []agent.ConversationEntry{{
		Role: "tool", ToolName: "desktop", Content: "echo\n" + body,
	}}
	used, ask, remoteOnly, sawSSH, text := desktopOutcomeEvidence(history)
	if !used || ask || remoteOnly || sawSSH {
		t.Fatalf("used=%v ask=%v remoteOnly=%v sawSSH=%v", used, ask, remoteOnly, sawSSH)
	}
	if !strings.Contains(text, marker) || strings.Contains(text, "\nssh: ") || !strings.HasPrefix(text, "desktop: ") {
		t.Fatalf("command line became an ssh record: %s", text)
	}
	continued, _, remoteOnly, sawSSH, sshText := desktopOutcomeEvidence([]agent.ConversationEntry{{
		Role: "tool", ToolName: "ssh", Content: "started\nssh: same session",
	}})
	if !continued || !remoteOnly || !sawSSH || !strings.HasPrefix(sshText, "ssh: started\n  ssh: same session") || strings.Contains(sshText, "\nssh: ") {
		t.Fatalf("ssh record: remoteOnly=%v sawSSH=%v text=%q", remoteOnly, sawSSH, sshText)
	}

	var judgeBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(req.Messages)
		judgeBody = string(raw)
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"终端是空的\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &coreAgentCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: server.URL, Model: "judge", Key: "k"},
		httpClient:      server.Client(),
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	decision := cb.ReviewDesktopOutcome("运行命令", "已经连上", history, nil)
	if !decision.Continue || !strings.Contains(decision.Message, "证明要求已经达成") || strings.Contains(decision.Message, "远程机器上的要求继续用 ssh") || strings.Contains(decision.Message, "已经连上") {
		t.Fatalf("desktop miss: %+v", decision)
	}
	if strings.Contains(judgeBody, "不能用来否定") || !strings.Contains(judgeBody, marker) {
		t.Fatalf("checker treated the command line as ssh: %s", judgeBody)
	}
}
