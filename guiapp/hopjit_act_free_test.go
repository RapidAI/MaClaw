package guiapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestHopPythonBodyUsesGovernedGUIProvider(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	caller := &guiHopJITCaller{
		handler:  handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
		invoke: func(context.Context, []interface{}, bool) (string, error) {
			t.Fatal("hop_python body unexpectedly called the model")
			return "", nil
		},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 确定性工具调用
Goal: body 使用受治理工具
Outputs:
- content % text
## Steps
1. [act] 读取
  - Tools: read_file
  + → content % text
  > `+"```hop_python"+`
  > content = read_file(path: "input.md")
  > `+"```"+`
`, nil, caller)
	if err != nil || result.Status != hopjit.NextCompleted || result.Outputs["content"] != "file-content" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestHopPythonBodyRejectsUndeclaredGUITool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	caller := &guiHopJITCaller{
		handler:  handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 越权工具调用
Goal: 未声明工具必须失败
Outputs:
- content % text
## Steps
1. [act] 读取
  - Tools: read_file
  + → content % text
  > `+"```hop_python"+`
  > content = write_file(path: "input.md")
  > `+"```"+`
`, nil, caller)
	if err != nil || result.Status != hopjit.NextFailed || result.Failure == nil {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestExecuteStepPlainActFillsJSON(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		if !jsonResponse {
			t.Fatal("plain act did not ask for JSON")
		}
		return `{"report":"晴"}`, nil
	}}
	got, err := caller.ExecuteStep(context.Background(), hopjit.StepReady{
		StepType: hopspec.StepAct, Summary: "查询",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	})
	if err != nil || got["report"] != "晴" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestExecuteStepEmptySchemaSkipsModel(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		t.Fatal("empty output schema called the model")
		return "", nil
	}}
	for _, stepType := range []hopspec.StepType{hopspec.StepReason, hopspec.StepAct, hopspec.StepCheck, hopspec.StepCommit} {
		got, err := caller.ExecuteStep(context.Background(), hopjit.StepReady{StepType: stepType, Summary: "无输出"})
		if err != nil || len(got) != 0 {
			t.Fatalf("type=%s got=%v err=%v", stepType, got, err)
		}
	}
	called := false
	withOutput := &guiHopJITCaller{invoke: func(_ context.Context, _ []interface{}, jsonResponse bool) (string, error) {
		called = true
		if !jsonResponse {
			t.Fatal("reason with an output did not ask for JSON")
		}
		return `{"report":"笔记"}`, nil
	}}
	got, err := withOutput.ExecuteStep(context.Background(), hopjit.StepReady{
		StepType: hopspec.StepReason, Summary: "写出结果",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	})
	if err != nil || !called || got["report"] != "笔记" {
		t.Fatalf("got=%v called=%v err=%v", got, called, err)
	}
}

func TestExecuteStepActWithToolsStaysOnActFree(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		t.Fatal("whitelisted act used the JSON step")
		return "", nil
	}}
	_, err := caller.ExecuteStep(context.Background(), hopjit.StepReady{StepType: hopspec.StepAct, Tools: []string{"web_search"}})
	if err == nil || !strings.Contains(err.Error(), "act free") {
		t.Fatalf("err=%v", err)
	}
}

func TestExecuteStepActFreeWithoutToolsStaysOnActFree(t *testing.T) {
	caller := &guiHopJITCaller{invoke: func(context.Context, []interface{}, bool) (string, error) {
		t.Fatal("act free without tools used the JSON step")
		return "", nil
	}}
	_, err := caller.ExecuteStep(context.Background(), hopjit.StepReady{StepType: hopspec.StepAct, Free: true})
	if err == nil || !strings.Contains(err.Error(), "act free") {
		t.Fatalf("err=%v", err)
	}
}

func TestHopJITActFreeAnswerUsesJSONCompletion(t *testing.T) {
	var jsonMode bool
	var system, user string
	caller := &guiHopJITCaller{invoke: func(_ context.Context, messages []interface{}, jsonResponse bool) (string, error) {
		jsonMode = jsonResponse
		system, _ = messages[0].(map[string]interface{})["content"].(string)
		user, _ = messages[1].(map[string]interface{})["content"].(string)
		return `{"report":"今天多云，最高14℃"}`, nil
	}}
	callbacks := &hopJITActFreeCallbacks{answerFromEvidence: true, task: "北京天气"}
	result := caller.actFreeRunner(context.Background(), callbacks)("任务: 北京天气\n检索证据:\n北京 多云", nil)
	if result.Error != "" || !jsonMode || !strings.Contains(system, "不要调用工具") || !strings.Contains(user, "北京 多云") || !strings.Contains(result.Text, "今天多云") {
		t.Fatalf("json=%v err=%q system=%q user=%q text=%q", jsonMode, result.Error, system, user, result.Text)
	}
}

func TestHopJITActFreeMissingAppReturnsError(t *testing.T) {
	caller := &guiHopJITCaller{handler: newHopJITActFreeTestHandler(t)}
	_, err := caller.executeActFree(context.Background(), hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}})
	if err == nil {
		t.Fatal("missing app did not return an error")
	}
}

func TestHopJITActFreeToolProviderUsesOnlyDeclaredTool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 1 {
		t.Fatalf("definitions=%d", len(definitions))
	}
	value, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err != nil || value != "file-content" {
		t.Fatalf("value=%v err=%v", value, err)
	}
	if _, err := provider.Execute(context.Background(), "write_file", map[string]any{}); err == nil {
		t.Fatal("undeclared tool was executed")
	}
}

func TestHopJITActFreeRunLoopExecutesToolAndReturnsJSON(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if requests.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-read","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"input.md\"}"}}]},"finish_reason":"tool_calls"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"{\"content\":\"file-content\"}"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()
	callbacks := &hopJITActFreeCallbacks{
		ctx: context.Background(), cfg: corelib.MaclawLLMConfig{URL: server.URL, Model: "test"}, tools: definitions, provider: provider,
	}
	result := agent.RunLoop(callbacks, "读取文件并返回 JSON", nil, server.Client())
	if result.Error != "" || result.Text != `{"content":"file-content"}` {
		t.Fatalf("result=%+v", result)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestHopJITActFreeRejectsJSONWithoutWebSearch(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{}
	var prompts []string
	_, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_search"}, Summary: "查询天气"}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		prompts = append(prompts, prompt)
		return agent.LoopResult{Text: `{"report":"晴"}`, HistoryDelta: []agent.ConversationEntry{{Role: "assistant", Content: `{"report":"晴"}`}}}
	})
	if err == nil || !strings.Contains(err.Error(), "web_search") {
		t.Fatalf("err=%v", err)
	}
	if len(prompts) != 2 || !strings.Contains(prompts[1], "web_search") {
		t.Fatalf("prompts=%v", prompts)
	}
}

func TestHopJITActFreeAcceptsReadFileJSONWithoutCall(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{}
	prompts := 0
	got, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"read_file"}}, func(string, []agent.ConversationEntry) agent.LoopResult {
		prompts++
		return agent.LoopResult{Text: `{"content":"file-content"}`}
	})
	if err != nil || got["content"] != "file-content" || prompts != 1 {
		t.Fatalf("got=%v err=%v prompts=%d", got, err, prompts)
	}
}

func TestHopJITActFreeAcceptsWebSearchOnCorrection(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{}
	prompts := 0
	got, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_search"}}, func(string, []agent.ConversationEntry) agent.LoopResult {
		prompts++
		if prompts == 2 {
			callbacks.called = map[string]int{"web_search": 1}
			return agent.LoopResult{Text: `{"report":"晴 26"}`}
		}
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err != nil || got["report"] != "晴 26" || prompts != 2 {
		t.Fatalf("got=%v err=%v prompts=%d", got, err, prompts)
	}
}

func TestHopJITActFreeSkipsCorrectionAfterWebSearch(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}}
	prompts := 0
	got, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_search"}}, func(string, []agent.ConversationEntry) agent.LoopResult {
		prompts++
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err != nil || got["report"] != "晴" || prompts != 1 {
		t.Fatalf("got=%v err=%v prompts=%d", got, err, prompts)
	}
}

func TestHopJITActFreeCorrectionKeepsStepWithoutHistory(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{}
	var second string
	prompts := 0
	_, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_search"}, Summary: "查询北京天气"}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		prompts++
		if prompts == 2 {
			second = prompt
		}
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(second, "查询北京天气") || !strings.Contains(second, "web_search") {
		t.Fatalf("err=%v second=%q", err, second)
	}
}

func TestHopJITActFreeRequiresWebFetchWhenSearchIsAbsent(t *testing.T) {
	callbacks := &hopJITActFreeCallbacks{}
	_, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_fetch"}}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "web_fetch") {
		t.Fatalf("err=%v", err)
	}
	callbacks.called = map[string]int{"web_fetch": 1}
	got, err := completeActFree(callbacks, hopjit.StepReady{Tools: []string{"web_search", "web_fetch"}}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || got != nil || !strings.Contains(err.Error(), "web_search") {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestHopJITActFreeCountsOnlyASuccessfulCall(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	callbacks := &hopJITActFreeCallbacks{ctx: context.Background(), tools: definitions, provider: provider}
	if got := callbacks.ExecuteTool("read_file", "{"); got == "file-content" || callbacks.calledTool("read_file") {
		t.Fatalf("malformed call counted: %q called=%v", got, callbacks.called)
	}
	if got := callbacks.ExecuteTool("write_file", "{}"); !strings.Contains(got, "Error:") || callbacks.calledTool("write_file") || callbacks.toolFailure("write_file") == "" {
		t.Fatalf("failed call counted: %q called=%v failure=%q", got, callbacks.called, callbacks.toolFailure("write_file"))
	}
	if got := callbacks.ExecuteTool("read_file", `{"path":"input.md"}`); got != "file-content" || !callbacks.calledTool("read_file") || callbacks.retrieval["read_file"] != "file-content" {
		t.Fatalf("successful call missing: %q called=%v retrieval=%v", got, callbacks.called, callbacks.retrieval)
	}
}

func TestHopJITActFreeBindsLookupTextToTheToolResult(t *testing.T) {
	step := hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}, {Name: "detail", Type: "text"}, {Name: "accepted", Type: "bool"}},
	}
	callbacks := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	got, err := completeActFree(callbacks, step, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴","detail":"25度","accepted":true}`}
	})
	if err != nil || got["report"] != "北京 多云" || got["detail"] != "北京 多云" || got["accepted"] != true {
		t.Fatalf("got=%v err=%v", got, err)
	}

	fromTask := &hopJITActFreeCallbacks{task: "北京天气", called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	var answerPrompt string
	got, err = completeActFree(fromTask, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		answerPrompt = prompt
		return agent.LoopResult{Text: `{"report":"晴，最高14℃"}`}
	})
	if err != nil || got["report"] != "晴，最高14℃" || !strings.Contains(answerPrompt, "北京 多云") {
		t.Fatalf("task got=%v err=%v prompt=%q", got, err, answerPrompt)
	}

	missing := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "  "}}
	_, err = completeActFree(missing, step, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴","accepted":true}`}
	})
	if err == nil || !strings.Contains(err.Error(), "web_search result") {
		t.Fatalf("err=%v", err)
	}

	kept := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	got, err = completeActFree(kept, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理笔记",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"摘要"}`}
	})
	if err != nil || got["report"] != "摘要" {
		t.Fatalf("kept=%v err=%v", got, err)
	}
}

func TestHopJITActFreeBindsLookupStringTypesAndKeepsEvidence(t *testing.T) {
	step := hopjit.StepReady{
		Tools:   []string{"web_search"},
		Summary: "查询天气",
		OutputSchema: []hopspec.VariableDecl{
			{Name: "report", Type: "markdown"},
			{Name: "line", Type: "line"},
			{Name: "prompt", Type: "prompt"},
			{Name: "accepted", Type: "bool"},
		},
	}
	callbacks := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	got, err := completeActFree(callbacks, step, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴","line":"晴","prompt":"晴","accepted":true}`}
	})
	if err != nil || got["report"] != "北京 多云" || got["line"] != "北京 多云" || got["prompt"] != "北京 多云" || got["accepted"] != true {
		t.Fatalf("types got=%v err=%v", got, err)
	}

	prose := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	var prosePrompt string
	got, err = completeActFree(prose, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "markdown"}},
	}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		prosePrompt = prompt
		return agent.LoopResult{Text: `{"report":"今天多云"}`}
	})
	if err != nil || got["report"] != "今天多云" || !strings.Contains(prosePrompt, "北京 多云") {
		t.Fatalf("prose got=%v err=%v prompt=%q", got, err, prosePrompt)
	}

	needsJSON := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	_, err = completeActFree(needsJSON, step, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: "今天晴。"}
	})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("bool prose err=%v", err)
	}

	miss := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "未找到相关结果"}}
	_, err = completeActFree(miss, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "web_search result") {
		t.Fatalf("miss err=%v", err)
	}

	noSchema := &hopJITActFreeCallbacks{called: map[string]int{"web_search": 1}, retrieval: map[string]string{"web_search": "北京 多云"}}
	_, err = completeActFree(noSchema, hopjit.StepReady{Tools: []string{"web_search"}, Summary: "查询天气"}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: "今天晴。"}
	})
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("no schema err=%v", err)
	}

	failed := &hopJITActFreeCallbacks{toolErrors: map[string]string{"web_search": "搜索失败: timeout"}}
	var second string
	_, err = completeActFree(failed, hopjit.StepReady{Tools: []string{"web_search"}, Summary: "查询天气"}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		second = prompt
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "搜索失败: timeout") || strings.Contains(err.Error(), "without calling") || !strings.Contains(second, "搜索失败: timeout") {
		t.Fatalf("failed err=%v second=%q", err, second)
	}
}

func TestHopJITActFreeHostSearchesWhenTheModelSkipsTheCall(t *testing.T) {
	var query string
	searches := 0
	callbacks := &hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(q string) (string, error) {
		searches++
		query = q
		return "北京 多云", nil
	}}
	prompts := 0
	got, err := completeActFree(callbacks, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "markdown"}, {Name: "accepted", Type: "bool"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		prompts++
		return agent.LoopResult{Text: `{"report":"晴","accepted":true}`}
	})
	if err != nil || got["report"] != "北京 多云" || got["accepted"] != true || query != "北京天气" || searches != 1 || prompts != 1 {
		t.Fatalf("got=%v err=%v query=%q searches=%d prompts=%d", got, err, query, searches, prompts)
	}

	proseSearches := 0
	prosePrompts := 0
	var prosePrompt string
	prose := &hopJITActFreeCallbacks{lookupSearch: func(string) (string, error) {
		proseSearches++
		return "北京 多云", nil
	}}
	got, err = completeActFree(prose, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		prosePrompts++
		prosePrompt = prompt
		return agent.LoopResult{Text: `{"report":"今天多云，最高14℃"}`}
	})
	if err != nil || got["report"] != "今天多云，最高14℃" || proseSearches != 1 || prosePrompts != 1 || !strings.Contains(prosePrompt, "北京 多云") {
		t.Fatalf("prose got=%v err=%v searches=%d prompts=%d prompt=%q", got, err, proseSearches, prosePrompts, prosePrompt)
	}

	plainPrompts := 0
	var plainPrompt string
	plain := &hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		return "北京 多云", nil
	}}
	got, err = completeActFree(plain, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理",
		OutputSchema: []hopspec.VariableDecl{{Name: "report"}, {Name: "detail", Type: "text。"}},
	}, func(prompt string, _ []agent.ConversationEntry) agent.LoopResult {
		plainPrompts++
		plainPrompt = prompt
		return agent.LoopResult{Text: `{"report":"多云","detail":"最高14℃"}`}
	})
	if err != nil || got["report"] != "多云" || got["detail"] != "最高14℃" || plainPrompts != 1 || !strings.Contains(plainPrompt, "北京 多云") {
		t.Fatalf("plain got=%v err=%v prompts=%d", got, err, plainPrompts)
	}

	copies := 0
	copied, err := completeActFree(&hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		return "北京 多云", nil
	}}, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		copies++
		if copies == 1 {
			return agent.LoopResult{Text: `{"report":"北京 多云"}`}
		}
		return agent.LoopResult{Text: `{"report":"今天多云，最高14℃"}`}
	})
	if err != nil || copied["report"] != "今天多云，最高14℃" || copies != 2 {
		t.Fatalf("copy got=%v err=%v prompts=%d", copied, err, copies)
	}

	quotedPrompts := 0
	quoted, err := completeActFree(&hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		return "北京 多云", nil
	}}, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		quotedPrompts++
		return agent.LoopResult{Text: `{"report":"今天北京 多云，最高14℃"}`}
	})
	if err != nil || quoted["report"] != "今天北京 多云，最高14℃" || quotedPrompts != 1 {
		t.Fatalf("quoted got=%v err=%v prompts=%d", quoted, err, quotedPrompts)
	}

	failures := 0
	_, err = completeActFree(&hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		return "北京 多云", nil
	}}, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		failures++
		return agent.LoopResult{Error: "llm unavailable"}
	})
	if err == nil || !strings.Contains(err.Error(), "llm unavailable") || failures != 1 {
		t.Fatalf("failure err=%v prompts=%d", err, failures)
	}

	degrees := 0
	degreePrompts := 0
	_, err = completeActFree(&hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		degrees++
		return "北京 多云", nil
	}}, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "temp", Type: "int"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		degreePrompts++
		return agent.LoopResult{Text: `{"temp":26}`}
	})
	if err == nil || !strings.Contains(err.Error(), "without calling") || degrees != 0 || degreePrompts != 2 {
		t.Fatalf("int err=%v searches=%d prompts=%d", err, degrees, degreePrompts)
	}

	notes := 0
	kept := &hopJITActFreeCallbacks{task: "整理笔记", lookupSearch: func(string) (string, error) {
		notes++
		return "北京 多云", nil
	}}
	_, err = completeActFree(kept, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理笔记",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"摘要"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "without calling") || notes != 0 {
		t.Fatalf("notes err=%v searches=%d", err, notes)
	}

	fetches := 0
	fetch := &hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		fetches++
		return "北京 多云", nil
	}}
	_, err = completeActFree(fetch, hopjit.StepReady{
		Tools:        []string{"web_fetch"},
		Summary:      "查询天气",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "web_fetch") || fetches != 0 {
		t.Fatalf("fetch err=%v searches=%d", err, fetches)
	}

	misses := 0
	missPrompts := 0
	miss := &hopJITActFreeCallbacks{task: "北京天气", lookupSearch: func(string) (string, error) {
		misses++
		return "未找到相关结果", nil
	}}
	_, err = completeActFree(miss, hopjit.StepReady{
		Tools:        []string{"web_search"},
		Summary:      "整理",
		OutputSchema: []hopspec.VariableDecl{{Name: "report", Type: "text"}},
	}, func(string, []agent.ConversationEntry) agent.LoopResult {
		missPrompts++
		return agent.LoopResult{Text: `{"report":"晴"}`}
	})
	if err == nil || !strings.Contains(err.Error(), "no results") || misses != 1 || missPrompts != 0 {
		t.Fatalf("miss err=%v searches=%d prompts=%d", err, misses, missPrompts)
	}
}

func TestHopJITActFreeLookupQueryPinsTheTask(t *testing.T) {
	if got := hopLookupQuery("北京天气", hopjit.StepReady{Summary: "整理", Goal: "查询北京今天的天气"}); got != "北京天气" {
		t.Fatalf("task query=%q", got)
	}
	if got := hopLookupQuery("整理笔记", hopjit.StepReady{Summary: "查询天气", Goal: "查询北京今天的天气"}); got != "查询北京今天的天气" {
		t.Fatalf("goal query=%q", got)
	}
	if got := hopLookupQuery("计算", hopjit.StepReady{Summary: "计算"}); got != "" {
		t.Fatalf("plain query=%q", got)
	}
	args := map[string]any{"query": "hello", "max_results": 3}
	got := applyLookupSearchQuery("web_search", args, "北京天气")
	if got["query"] != "北京天气" || got["max_results"] != 3 || args["query"] != "hello" {
		t.Fatalf("rewritten=%v original=%v", got, args)
	}
	if applyLookupSearchQuery("read_file", args, "北京天气")["query"] != "hello" {
		t.Fatal("read_file query changed")
	}
	if applyLookupSearchQuery("web_search", nil, "  ") != nil {
		t.Fatal("blank query rewrote arguments")
	}
	var seen map[string]any
	provider := hopLookupSearchProvider{
		inner: hopjit.ToolProviderFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
			if name != "web_search" {
				t.Fatalf("name=%s", name)
			}
			seen = args
			return "Search hit", nil
		}),
		query: "北京天气",
	}
	value, err := provider.Execute(context.Background(), "web_search", map[string]any{"query": "hello"})
	if err != nil || value != "Search hit" || seen["query"] != "北京天气" {
		t.Fatalf("value=%v err=%v seen=%v", value, err, seen)
	}
	for _, miss := range []string{"未找到相关结果", "未找到相关结果。", "No results found."} {
		missProvider := hopLookupSearchProvider{
			inner: hopjit.ToolProviderFunc(func(context.Context, string, map[string]any) (any, error) {
				return miss, nil
			}),
			query: "北京天气",
		}
		if _, err := missProvider.Execute(context.Background(), "web_search", nil); err == nil || !strings.Contains(err.Error(), "no results") {
			t.Fatalf("miss %q err=%v", miss, err)
		}
	}
	page := hopLookupSearchProvider{
		inner: hopjit.ToolProviderFunc(func(_ context.Context, name string, args map[string]any) (any, error) {
			if name != "web_fetch" || args["url"] != "https://example.test" {
				t.Fatalf("fetch name=%s args=%v", name, args)
			}
			return "未找到相关结果", nil
		}),
		query: "北京天气",
	}
	fetched, err := page.Execute(context.Background(), "web_fetch", map[string]any{"url": "https://example.test"})
	if err != nil || fetched != "未找到相关结果" {
		t.Fatalf("fetch value=%v err=%v", fetched, err)
	}
}

func TestHopJITActFreeRejectsMalformedToolArguments(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	provider, definitions, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	callbacks := &hopJITActFreeCallbacks{ctx: context.Background(), tools: definitions, provider: provider}
	if got := callbacks.ExecuteTool("read_file", "{"); got == "file-content" {
		t.Fatal("malformed tool arguments were replaced with an empty object")
	}
}

func TestHopJITActFreeProviderExecutesDeclaredTool(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	for range 2 {
		provider, _, err := handler.hopJITActFreeToolProvider(step, hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHopJITActFreeAppUsesDurableExecutionRecords(t *testing.T) {
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	handler := newHopJITActFreeTestHandler(t)
	handler.hopJITResults = newFileHopJITResultStore(t.TempDir())
	handler.app = &App{
		semanticExecutionCoordinator: coordinator,
		semanticPlanExecutionStore:   coordinator.Executions,
		semanticInvocationStore:      coordinator.Grants,
		semanticInvocationKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	var calls atomic.Int32
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		calls.Add(1)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	step := hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}}
	identity := hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"}
	provider, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
		t.Fatal(err)
	}
	record, err := coordinator.Executions.Execution(provider.Scope, provider.Plan.Selections[0].ID)
	if err != nil || record.State != tool.PlanExecutionSucceeded {
		t.Fatalf("record=%+v err=%v", record, err)
	}
	restarted, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err == nil {
		t.Fatal("completed execution was invoked again")
	}
	step.Recovered = true
	recovered, _, err := handler.hopJITActFreeToolProvider(step, identity)
	if err != nil {
		t.Fatal(err)
	}
	value, err := recovered.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err != nil || value != "file-content" {
		t.Fatalf("recovered value=%v err=%v", value, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
}

func TestHopPythonLoopToolsUseDistinctPersistentIterations(t *testing.T) {
	coordinator, err := tool.NewSQLiteSemanticExecutionCoordinator(filepath.Join(t.TempDir(), "semantic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer coordinator.Close()
	handler := newHopJITActFreeTestHandler(t)
	handler.hopJITResults = newFileHopJITResultStore(t.TempDir())
	handler.app = &App{
		semanticExecutionCoordinator: coordinator,
		semanticPlanExecutionStore:   coordinator.Executions,
		semanticInvocationStore:      coordinator.Grants,
		semanticInvocationKey:        []byte("0123456789abcdef0123456789abcdef"),
	}
	var calls atomic.Int32
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		calls.Add(1)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	caller := &guiHopJITCaller{
		app: handler.app, handler: handler,
		identity: hopJITExecutionIdentity{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
	}
	result, err := runHopSpecWithPersistence(context.Background(), hopjit.NewMemoryPersistence(), `# Spec: 循环工具作用域
Goal: 每轮独立执行
Inputs:
- items % [text]
Outputs:
- reports % [text]
## Steps
1. [loop for-each item in items, collect content into reports]
  + → reports % [text]
  1.1. [act] 读取
    - Tools: read_file
    + → content % text
    > `+"```hop_python"+`
    > content = read_file(path: "input.md")
    > `+"```"+`
`, map[string]any{"items": []string{"A", "B"}}, caller)
	reports, ok := result.Outputs["reports"].([]any)
	if err != nil || result.Status != hopjit.NextCompleted || calls.Load() != 2 || !ok || len(reports) != 2 {
		t.Fatalf("result=%+v calls=%d err=%v", result, calls.Load(), err)
	}
}

func TestHopJITActFreeDoesNotProjectApprovalWaitAsSuccess(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(map[string]interface{}) string {
		return databaseMutationApprovalWaitText
	}
	handler.registry.mu.Unlock()
	provider, _, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{StepID: "1", Tools: []string{"read_file"}}, hopJITExecutionIdentity{
		PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"})
	if err == nil || value != nil {
		t.Fatalf("approval wait became a tool value: value=%v err=%v", value, err)
	}
}

func TestHopJITActFreeBindsToolIdentityPerCaller(t *testing.T) {
	handler := newHopJITActFreeTestHandler(t)
	owners := []string{}
	handler.registry.mu.Lock()
	handler.registry.tools["read_file"].Handler = func(args map[string]interface{}) string {
		owner, _ := args[registeredToolPolicyOwnerIDField].(string)
		owners = append(owners, owner)
		return "file-content"
	}
	handler.registry.mu.Unlock()
	step := hopjit.StepReady{StepID: "1", Summary: "读取文件", Tools: []string{"read_file"}}
	for _, identity := range []hopJITExecutionIdentity{
		{PrincipalID: "user-a", PolicyOwnerID: "owner-a", SessionID: "session-a", Platform: "desktop"},
		{PrincipalID: "user-b", PolicyOwnerID: "owner-b", SessionID: "session-b", Platform: "desktop"},
	} {
		provider, _, err := handler.hopJITActFreeToolProvider(step, identity)
		if err != nil {
			t.Fatal(err)
		}
		if provider.Scope.PrincipalID != identity.PrincipalID || provider.Scope.SessionID != identity.SessionID {
			t.Fatalf("scope=%+v identity=%+v", provider.Scope, identity)
		}
		if _, err := provider.Execute(context.Background(), "read_file", map[string]any{"path": "input.md"}); err != nil {
			t.Fatal(err)
		}
		if len(owners) == 0 || owners[len(owners)-1] != identity.PolicyOwnerID {
			t.Fatalf("tool owners=%v identity=%+v", owners, identity)
		}
	}
}

// The repaired 北京天气 act lists only builtin web_search and a string output.
// Planning that registration must authorize the pinned query. The handler is
// replaced so this test never opens a search connection.
func TestHopJITActFreeBuiltinWebSearchAuthorizesThePinnedQuery(t *testing.T) {
	registry := NewToolRegistry()
	handler := &IMMessageHandler{registry: registry}
	registerBuiltinTools(registry, handler)
	var gotQuery string
	handler.registry.mu.Lock()
	registered := handler.registry.tools["web_search"]
	if registered == nil || registered.Status != RegToolAvailable || len(registered.CapabilityProvisions) == 0 {
		handler.registry.mu.Unlock()
		t.Fatal("builtin web_search is not a governed tool")
	}
	registered.Handler = func(args map[string]interface{}) string {
		gotQuery, _ = args["query"].(string)
		return "北京今天晴"
	}
	handler.registry.mu.Unlock()
	provider, definitions, err := handler.hopJITActFreeToolProvider(hopjit.StepReady{
		StepID: "2", RunID: "run-weather", Summary: "检索北京天气",
		Tools:        []string{"web_search"},
		OutputSchema: []hopspec.VariableDecl{{Name: "beijing_weather", Type: "str"}},
	}, hopJITExecutionIdentity{
		PrincipalID: "desktop-user", PolicyOwnerID: "desktop-user", SessionID: "desktop-user", Platform: "desktop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil || len(definitions) != 1 {
		t.Fatalf("provider=%v definitions=%d", provider != nil, len(definitions))
	}
	value, err := provider.Execute(context.Background(), "web_search", applyLookupSearchQuery("web_search", nil, "北京天气"))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := value.(string)
	if gotQuery != "北京天气" || !strings.Contains(text, "晴") {
		t.Fatalf("query=%q value=%v", gotQuery, value)
	}
}

func newHopJITActFreeTestHandler(t *testing.T) *IMMessageHandler {
	t.Helper()
	registry := NewToolRegistry()
	schema := map[string]interface{}{
		"type": "object", "properties": map[string]interface{}{"path": map[string]interface{}{"type": "string"}},
		"required": []string{"path"}, "additionalProperties": false,
	}
	if err := registry.Register(RegisteredTool{
		Name: "read_file", Status: RegToolAvailable, InputSchema: schema,
		CapabilityProvisions: []tool.CapabilityProvision{{Capability: tool.CapabilityFSReadLocal, Quality: 1}},
		SemanticEffects:      []tool.EffectClass{tool.EffectReadOnly},
		Handler:              func(args map[string]interface{}) string { return "file-content" },
	}); err != nil {
		t.Fatal(err)
	}
	return &IMMessageHandler{registry: registry}
}
