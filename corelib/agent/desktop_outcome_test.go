package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func TestApplyDesktopOutcomeKeepsAnUncheckedDraftOffTheReply(t *testing.T) {
	draft := "Score: 10，游戏已运行"
	kept, message, cont := ApplyDesktopOutcome(draft, DesktopOutcomeDecision{
		Continue: true,
		Message:  "[系统] 画面里没有游戏。",
	}, true)
	if !cont || kept != draft || !strings.Contains(message, "画面里没有游戏") {
		t.Fatalf("continue=%v kept=%q message=%q", cont, kept, message)
	}
	replaced, message, cont := ApplyDesktopOutcome(draft, DesktopOutcomeDecision{Replace: "终端是空的，游戏没有运行。"}, true)
	if cont || message != "" || replaced != "终端是空的，游戏没有运行。" {
		t.Fatalf("replace continue=%v message=%q text=%q", cont, message, replaced)
	}
	closed, _, cont := ApplyDesktopOutcome(draft, DesktopOutcomeDecision{Continue: true, Message: "再改"}, false)
	if cont || closed == draft || !strings.Contains(closed, "还没有对照桌面确认") {
		t.Fatalf("closed room text=%q cont=%v", closed, cont)
	}
	same, _, cont := ApplyDesktopOutcome(draft, DesktopOutcomeDecision{}, true)
	if cont || same != draft {
		t.Fatalf("empty decision changed the draft: %q cont=%v", same, cont)
	}
}

func TestDesktopOutcomeUserContentNamesAMissingAndAnUnseenShot(t *testing.T) {
	missing, ok := desktopOutcomeUserContent(corelib.MaclawLLMConfig{}, "运行贪吃蛇", "bash: 未找到命令", nil).(string)
	if !ok || !strings.Contains(missing, "没有当前桌面截图") || !strings.Contains(missing, "更早的截图不能当作现在的画面") {
		t.Fatalf("missing=%v", missing)
	}
	unseen, ok := desktopOutcomeUserContent(corelib.MaclawLLMConfig{SupportsVision: false}, "运行贪吃蛇", "bash: 未找到命令", &ToolModelImage{MIME: "image/png", Base64: "aaaa"}).(string)
	if !ok || !strings.Contains(unseen, "看不到图片") || !strings.Contains(unseen, "没有写明的画面不能算已经达成") || strings.Contains(unseen, "aaaa") {
		t.Fatalf("unseen=%v", unseen)
	}
	if strings.Contains(missing, "不能用来否定") || strings.Contains(unseen, "不能用来否定") {
		t.Fatal("a desktop-only transcript picked up the ssh note")
	}
}

func TestDesktopOutcomeUserContentSeparatesSSHFromTheDesktopShot(t *testing.T) {
	remote, ok := desktopOutcomeUserContent(corelib.MaclawLLMConfig{}, "在远程机器上启动", "desktop: 画面是网页\nssh: training started", nil).(string)
	if !ok || !strings.Contains(remote, "不能用来否定 ssh 的输出") || !strings.Contains(remote, "training started") || !strings.Contains(remote, "画面是网页") || !strings.Contains(remote, "没有当前桌面截图") {
		t.Fatalf("remote=%v", remote)
	}
	quoted, ok := desktopOutcomeUserContent(corelib.MaclawLLMConfig{}, "运行命令", "desktop: echo\n  ssh: Connection refused", nil).(string)
	if !ok || strings.Contains(quoted, "不能用来否定") || !strings.Contains(quoted, "Connection refused") {
		t.Fatalf("quoted=%v", quoted)
	}
}

func TestParseDesktopOutcomeVerdictIgnoresAFence(t *testing.T) {
	ok, gap, err := parseDesktopOutcomeVerdict("```json\n{\"ok\":false,\"gap\":\"终端是空的\"}\n```")
	if err != nil || ok || gap != "终端是空的" {
		t.Fatalf("ok=%v gap=%q err=%v", ok, gap, err)
	}
	if _, _, err := parseDesktopOutcomeVerdict("已经完成了"); err == nil {
		t.Fatal("prose was accepted as a verdict")
	}
}

func TestJudgeDesktopOutcomeDoesNotSendTheWorkerDraft(t *testing.T) {
	var body atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		raw, _ := json.Marshal(req.Messages)
		body.Store(string(raw))
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"{\"ok\":false,\"gap\":\"终端是空的\"}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	ok, gap, err := JudgeDesktopOutcome(nil, corelib.MaclawLLMConfig{
		URL: server.URL, Model: "judge", Key: "k", SupportsVision: true,
	}, server.Client(), "开发贪吃蛇并运行", "bash: 未找到命令", &ToolModelImage{MIME: "image/png", Base64: "aaaa"})
	if err != nil || ok || gap != "终端是空的" {
		t.Fatalf("ok=%v gap=%q err=%v", ok, gap, err)
	}
	got, _ := body.Load().(string)
	if strings.Contains(got, "Score: 10") || !strings.Contains(got, "开发贪吃蛇") || !strings.Contains(got, "未找到命令") || !strings.Contains(got, "aaaa") {
		t.Fatalf("judge saw the wrong evidence: %s", got)
	}
}

type outcomeReviewCallbacks struct {
	mockCallbacks
	shots   []ToolModelImage
	reviews int
	sawShot bool
}

func (m *outcomeReviewCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	_ = callID
	return ToolExecutionResult{
		Result:      m.ExecuteTool(name, args),
		Outcome:     ToolExecutionOutcomeOK,
		ModelImages: append([]ToolModelImage(nil), m.shots...),
	}
}

func (m *outcomeReviewCallbacks) ReviewDesktopOutcome(_, draft string, _ []ConversationEntry, image *ToolModelImage) DesktopOutcomeDecision {
	m.reviews++
	if image != nil && image.Base64 != "" {
		m.sawShot = true
	}
	if strings.Contains(draft, "Score") {
		return DesktopOutcomeDecision{Continue: true, Message: "[系统] 这一步的结果还没有满足用户要求：画面里没有游戏。"}
	}
	return DesktopOutcomeDecision{}
}

func TestRunLoopDesktopOutcomeContinuesUntilTheCheckPasses(t *testing.T) {
	var sawNudge atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		text := string(raw)
		if strings.Contains(text, "画面里没有游戏") {
			sawNudge.Store(true)
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"终端里已经能看到贪吃蛇。"},"finish_reason":"stop"}]}`)
			return
		}
		if strings.Contains(text, "Desktop screenshot") {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Score: 10，游戏已运行"},"finish_reason":"stop"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc1","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"screenshot\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &outcomeReviewCallbacks{
		mockCallbacks: mockCallbacks{
			config:     corelib.MaclawLLMConfig{URL: server.URL, Model: "vision", Key: "k", SupportsVision: true},
			maxIter:    6,
			sysPrompt:  "sys",
			tools:      []map[string]interface{}{tooldef.BuildToolDef("desktop", "desktop", map[string]interface{}{"type": "object"})},
			toolResult: "Desktop screenshot of display :20.",
		},
		shots: []ToolModelImage{{MIME: "image/png", Base64: "aaaa"}},
	}
	result := RunLoop(cb, "开发图形贪吃蛇并运行", nil, server.Client())
	if result.Error != "" || result.Text != "终端里已经能看到贪吃蛇。" {
		t.Fatalf("result=%+v", result)
	}
	if strings.Contains(result.Text, "Score") || !sawNudge.Load() || cb.reviews < 2 || !cb.sawShot {
		t.Fatalf("nudge=%v reviews=%d sawShot=%v", sawNudge.Load(), cb.reviews, cb.sawShot)
	}
}

func TestRunLoopDesktopOutcomeDropsAScreenshotAfterALaterDesktopAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		text := string(raw)
		if strings.Contains(text, "未找到命令") {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"游戏已运行"},"finish_reason":"stop"}]}`)
			return
		}
		if strings.Contains(text, "Desktop screenshot") {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc2","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"bash\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"tc1","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"screenshot\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &laterDesktopActionCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "vision", Key: "k", SupportsVision: true},
		maxIter:   6,
		sysPrompt: "sys",
		tools:     []map[string]interface{}{tooldef.BuildToolDef("desktop", "desktop", map[string]interface{}{"type": "object"})},
	}}
	result := RunLoop(cb, "运行贪吃蛇", nil, server.Client())
	if result.Error != "" || result.Text != "checked" || !cb.reviewed {
		t.Fatalf("result=%+v reviewed=%v", result, cb.reviewed)
	}
	if cb.image != nil {
		t.Fatalf("later desktop action kept the old screenshot: %#v", cb.image)
	}
}

type laterDesktopActionCallbacks struct {
	mockCallbacks
	n        int
	reviewed bool
	image    *ToolModelImage
}

func (m *laterDesktopActionCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	_ = name
	_ = args
	_ = callID
	m.n++
	if m.n == 1 {
		return ToolExecutionResult{
			Result:      "Desktop screenshot of display :20.",
			Outcome:     ToolExecutionOutcomeOK,
			ModelImages: []ToolModelImage{{MIME: "image/png", Base64: "aaaa"}},
		}
	}
	return ToolExecutionResult{Result: "bash: 未找到命令", Outcome: ToolExecutionOutcomeOK}
}

func (m *laterDesktopActionCallbacks) ReviewDesktopOutcome(_, _ string, _ []ConversationEntry, image *ToolModelImage) DesktopOutcomeDecision {
	m.reviewed = true
	if image != nil {
		shot := *image
		m.image = &shot
	}
	return DesktopOutcomeDecision{Replace: "checked"}
}

func TestRunLoopDesktopOutcomeReplacesANoProgressClaim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Score: 10，游戏已运行","tool_calls":[{"id":"tc1","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"bash\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &stopClaimCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "vision", Key: "k"},
		maxIter:   8,
		sysPrompt: "sys",
		tools:     []map[string]interface{}{tooldef.BuildToolDef("desktop", "desktop", map[string]interface{}{"type": "object"})},
	}}
	result := RunLoop(cb, "运行贪吃蛇", nil, server.Client())
	if result.Error != "" || result.Text != "终端是空的，游戏没有运行。" || strings.Contains(result.Text, "Score") || cb.reviews < 1 {
		t.Fatalf("result=%+v reviews=%d", result, cb.reviews)
	}
}

type stopClaimCallbacks struct {
	mockCallbacks
	reviews int
}

func (m *stopClaimCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	_ = name
	_ = args
	_ = callID
	return ToolExecutionResult{Result: "Error: bash: 未找到命令", Outcome: ToolExecutionOutcomeError}
}

func (m *stopClaimCallbacks) ReviewDesktopOutcome(string, string, []ConversationEntry, *ToolModelImage) DesktopOutcomeDecision {
	m.reviews++
	return DesktopOutcomeDecision{Replace: "终端是空的，游戏没有运行。"}
}

func TestRunLoopDesktopOutcomeContinuesAnEmptyExit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []map[string]interface{} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		raw, _ := json.Marshal(req.Messages)
		text := string(raw)
		if strings.Contains(text, "画面里没有游戏") {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"终端里已经能看到贪吃蛇。"},"finish_reason":"stop"}]}`)
			return
		}
		if strings.Contains(text, "未找到命令") {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Score: 10，游戏已运行","tool_calls":[{"id":"tc1","type":"function","function":{"name":"desktop","arguments":"{\"action\":\"bash\"}"}}]},"finish_reason":"tool_calls"}]}`)
	}))
	defer server.Close()
	cb := &emptyExitCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "vision", Key: "k"},
		maxIter:   12,
		sysPrompt: "sys",
		tools:     []map[string]interface{}{tooldef.BuildToolDef("desktop", "desktop", map[string]interface{}{"type": "object"})},
	}}
	result := RunLoop(cb, "运行贪吃蛇", nil, server.Client())
	if result.Error != "" || result.Text != "终端里已经能看到贪吃蛇。" || strings.Contains(result.Text, "Score") || cb.reviews < 2 {
		t.Fatalf("result=%+v reviews=%d", result, cb.reviews)
	}
}

type emptyExitCallbacks struct {
	mockCallbacks
	reviews int
}

func (m *emptyExitCallbacks) ExecuteToolCall(name, args, callID string) ToolExecutionResult {
	_ = name
	_ = args
	_ = callID
	return ToolExecutionResult{Result: "bash: 未找到命令", Outcome: ToolExecutionOutcomeOK}
}

func (m *emptyExitCallbacks) ReviewDesktopOutcome(_, draft string, _ []ConversationEntry, _ *ToolModelImage) DesktopOutcomeDecision {
	m.reviews++
	if strings.Contains(draft, "Score") && m.reviews == 1 {
		return DesktopOutcomeDecision{Continue: true, Message: "[系统] 画面里没有游戏。"}
	}
	return DesktopOutcomeDecision{}
}

func TestRunLoopDesktopOutcomeReplacesAnUncheckedFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Score: 10，游戏已运行"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	cb := &replaceOutcomeCallbacks{mockCallbacks: mockCallbacks{
		config:    corelib.MaclawLLMConfig{URL: server.URL, Model: "vision", Key: "k"},
		maxIter:   3,
		sysPrompt: "sys",
	}}
	result := RunLoop(cb, "运行贪吃蛇", nil, server.Client())
	if result.Error != "" || result.Text != "终端是空的，游戏没有运行。" || strings.Contains(result.Text, "Score") {
		t.Fatalf("result=%+v", result)
	}
}

type replaceOutcomeCallbacks struct {
	mockCallbacks
}

func (m *replaceOutcomeCallbacks) ReviewDesktopOutcome(string, string, []ConversationEntry, *ToolModelImage) DesktopOutcomeDecision {
	return DesktopOutcomeDecision{Replace: "终端是空的，游戏没有运行。"}
}
