package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenCodeFreeOfficialToolsMatchProfile(t *testing.T) {
	var tools []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(openCodeFreeToolsJSON), &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != len(openCodeFreeOfficialTools) {
		t.Fatalf("profile has %d tools, set has %d", len(tools), len(openCodeFreeOfficialTools))
	}
	for _, tool := range tools {
		if _, ok := openCodeFreeOfficialTools[tool.Function.Name]; !ok {
			t.Fatalf("profile tool %q is not in the official set", tool.Function.Name)
		}
	}
}

func TestOpenCodeFreeHostExtrasSkipOfficialAndMapped(t *testing.T) {
	got := OpenCodeFreeHostExtras([]string{"goal", "bash", "shell", "write_file", "generate_pdf", "goal", "read"})
	want := []string{"generate_pdf", "goal"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("extras = %v", got)
	}
	if OpenCodeFreeHostChannelNote(nil) != "" {
		t.Fatal("empty extras still produced a note")
	}
}

func TestOpenCodeFreeParseHostCallJSONAndJS(t *testing.T) {
	jsonCode := `return MACLAW_CALL("goal", {"action":"plan","understanding":"北京","steps":[{"title":"采集","detail":"搜索 {ok}"}],"acceptance_criteria":["pdf"]})`
	jsCode := "return MACLAW_CALL('goal', {action:'plan', understanding:'北京天气', steps:[{title:'采集', detail:'搜索'}], acceptance_criteria:['可打开的 pdf'],})"
	quoted := `const label = "MACLAW_CALL(\"goal\", {})"; return 1`
	for _, code := range []string{jsonCode, jsCode} {
		calls, ok := parseOpenCodeFreeHostCalls(code)
		if !ok || len(calls) != 1 || calls[0].Name != "goal" {
			t.Fatalf("parse %q -> %+v ok=%v", code, calls, ok)
		}
		var args map[string]any
		if json.Unmarshal([]byte(calls[0].Args), &args) != nil || args["action"] != "plan" {
			t.Fatalf("args = %s", calls[0].Args)
		}
		steps, _ := args["steps"].([]any)
		if len(steps) != 1 {
			t.Fatalf("steps = %s", calls[0].Args)
		}
	}
	if _, ok := parseOpenCodeFreeHostCalls(quoted); ok {
		t.Fatal("MACLAW_CALL inside a string was treated as a call")
	}
	if _, ok := parseOpenCodeFreeHostCalls(`return { availableToolKeys: Object.keys(tools || {}) };`); ok {
		t.Fatal("catalog introspection was treated as a host call")
	}
}

func TestOpenCodeFreeAdaptExecuteBecomesGoal(t *testing.T) {
	code := `return MACLAW_CALL("goal", {"action":"plan","understanding":"北京","steps":[{"title":"采集","detail":"搜索"}],"acceptance_criteria":["pdf"]}); return MACLAW_CALL("generate_pdf", {"path":"beijing.pdf"})`
	raw, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	got := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
		ID:       "call-1",
		Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
	}})
	if len(got) != 2 || got[0].Function.Name != "goal" || got[1].Function.Name != "generate_pdf" {
		t.Fatalf("calls = %+v", got)
	}
	if got[0].ID != "call-1" || got[1].ID != "call-1#2" {
		t.Fatalf("ids = %s %s", got[0].ID, got[1].ID)
	}
	if !strings.Contains(got[0].Function.Arguments, `"action":"plan"`) || !strings.Contains(got[0].Function.Arguments, "北京") {
		t.Fatalf("goal args = %s", got[0].Function.Arguments)
	}
	kept := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
		Function: ToolCallFunction{Name: "execute", Arguments: `{"code":"return Object.keys(tools||{})"}`},
	}})
	if len(kept) != 1 || kept[0].Function.Name != "execute" {
		t.Fatalf("introspection was rewritten: %+v", kept)
	}
}

func TestOpenCodeFreeClientBodyKeepsFingerprintAndAddsHostNote(t *testing.T) {
	caller := []byte(`{"model":"hy4-flash-free","tools":[{"type":"function","function":{"name":"goal"}},{"type":"function","function":{"name":"bash"}},{"type":"function","function":{"name":"generate_pdf","parameters":{"type":"object","properties":{"content":{"type":"string"},"title":{"type":"string"},"phase_id":{"type":"string"}},"required":["content"]}}}],"messages":[{"role":"system","content":"host prompt"},{"role":"user","content":"调用 goal(action=\"plan\")"}]}`)
	body, err := openCodeFreeClientBody(caller)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Tools    json.RawMessage `json:"tools"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(parsed.Tools)) != strings.TrimSpace(openCodeFreeToolsJSON) {
		t.Fatal("official tools were rewritten")
	}
	if strings.Contains(string(parsed.Tools), `"name":"goal"`) {
		t.Fatal("goal was added to the fingerprint tools")
	}
	if parsed.Messages[0].Role != "system" || !strings.Contains(parsed.Messages[0].Content, "The catalog is complete") {
		t.Fatal("official system prompt was replaced")
	}
	if strings.Contains(parsed.Messages[0].Content, "MACLAW_CALL") {
		t.Fatal("host note was written into the system prompt")
	}
	last := parsed.Messages[len(parsed.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "MACLAW_CALL(\"goal\"") {
		t.Fatalf("user note = %s", last.Content)
	}
	if strings.Contains(last.Content, "bash") {
		t.Fatal("mapped host tool was listed as an extra")
	}
	sig := "Argument names: generate_pdf(content, phase_id?, title?)."
	idx := strings.Index(last.Content, sig)
	if idx < 0 {
		t.Fatalf("generate_pdf parameters were not listed: %s", last.Content)
	}
	if strings.Contains(last.Content[idx:], "goal") {
		t.Fatal("goal schema was duplicated beside its example")
	}
	if !strings.Contains(last.Content[:idx], `MACLAW_CALL("generate_pdf", {"content":"..."})`) {
		t.Fatalf("generate_pdf example = %s", last.Content)
	}
	if strings.Contains(last.Content, `"argument"`) {
		t.Fatal("placeholder argument was still in the note")
	}
	plain, err := openCodeFreeClientBody([]byte(`{"model":"hy4-flash-free","messages":[{"role":"user","content":"Reply with exactly PING"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), "MACLAW_CALL") {
		t.Fatal("a turn with no host extras still got the note")
	}
}

func TestOpenCodeFreeHostChannelRetryKeepsExecuteOpen(t *testing.T) {
	got := OpenCodeFreeHostChannelRetry([]string{"goal", "read_file", "bash"})
	if !strings.Contains(got, "MACLAW_CALL") || !strings.Contains(got, "goal") {
		t.Fatalf("retry = %s", got)
	}
	if strings.Contains(got, "Do not retry") {
		t.Fatal("retry tells the model to stop using execute")
	}
	if strings.Contains(got, "<system-reminder>") {
		t.Fatal("retry embeds the wire reminder")
	}
	if strings.Contains(got, "read_file") || strings.Contains(got, "bash") {
		t.Fatal("mapped tools were listed as host extras")
	}
	if !strings.Contains(OpenCodeFreeHostChannelRetry(nil), "shell") {
		t.Fatal("empty extras retry lost the direct tools")
	}
}

func TestOpenCodeFreeParseHostCallSkipsNoiseAndBareValues(t *testing.T) {
	bare := "return MACLAW_CALL('goal', {action: plan, index: 1, ok: true, steps: [{title: 采集}], acceptance_criteria: [pdf,],})"
	commentBrace := "return MACLAW_CALL(\"goal\", {action: \"plan\", /* } */ understanding: \"北京\", steps: [{title: \"采集\",},], // MACLAW_CALL(\"name\", {})\n})"
	lineComment := "// return MACLAW_CALL(\"name\", {\"argument\":\"value\"})\nreturn MACLAW_CALL(\"goal\", {\"action\":\"step\",\"index\":1,\"status\":\"done\",\"result\":\"ok\"})"
	template := "const sample = `MACLAW_CALL(\"name\", {})`\nreturn MACLAW_CALL(\"goal\", {\"action\":\"plan\",\"understanding\":\"北京\",\"steps\":[{\"title\":\"采集\"}],\"acceptance_criteria\":[\"pdf\"]})"
	calls, ok := parseOpenCodeFreeHostCalls(bare)
	if !ok || len(calls) != 1 || calls[0].Name != "goal" {
		t.Fatalf("bare = %+v ok=%v", calls, ok)
	}
	var args map[string]any
	if json.Unmarshal([]byte(calls[0].Args), &args) != nil || args["action"] != "plan" || args["ok"] != true || args["index"] != float64(1) {
		t.Fatalf("bare args = %s", calls[0].Args)
	}
	steps, _ := args["steps"].([]any)
	step, _ := steps[0].(map[string]any)
	criteria, _ := args["acceptance_criteria"].([]any)
	if step["title"] != "采集" || len(criteria) != 1 || criteria[0] != "pdf" {
		t.Fatalf("bare fields = %s", calls[0].Args)
	}
	for _, code := range []string{commentBrace, lineComment, template} {
		got, ok := parseOpenCodeFreeHostCalls(code)
		if !ok || len(got) != 1 || got[0].Name != "goal" {
			t.Fatalf("noise %q -> %+v ok=%v", code, got, ok)
		}
	}
	if _, ok := parseOpenCodeFreeHostCalls(`return MACLAW_CALL_EXTRA("goal", {})`); ok {
		t.Fatal("identifier continuation was treated as a call")
	}
	templateValue := "return MACLAW_CALL('goal', {action: 'plan', understanding: `北京天气`, steps: [{title: `采集`}], acceptance_criteria: [`pdf`]})"
	got, ok := parseOpenCodeFreeHostCalls(templateValue)
	if !ok || len(got) != 1 || got[0].Name != "goal" || !strings.Contains(got[0].Args, "北京天气") {
		t.Fatalf("template = %+v ok=%v", got, ok)
	}
	if _, ok := parseOpenCodeFreeHostCalls("return MACLAW_CALL('goal', {understanding: `北京${city}`})"); ok {
		t.Fatal("template interpolation was executed")
	}
	hyphen, ok := parseOpenCodeFreeHostCalls(`return MACLAW_CALL("sequential-thinking", {"thought":"x"})`)
	if !ok || len(hyphen) != 1 || hyphen[0].Name != "sequential-thinking" {
		t.Fatalf("hyphen = %+v ok=%v", hyphen, ok)
	}
}

func TestOpenCodeFreeToolsCallBecomesHostTool(t *testing.T) {
	dot := "return tools.goal({action: 'plan', understanding: `北京天气`, steps: [{title: '采集'}], acceptance_criteria: ['pdf']})"
	bracket := `return tools["generate_pdf"]({content: "今日天气"})`
	for _, code := range []string{dot, bracket} {
		raw, err := json.Marshal(map[string]string{"code": code})
		if err != nil {
			t.Fatal(err)
		}
		got := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
			ID:       "call-1",
			Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
		}})
		if len(got) != 1 {
			t.Fatalf("calls = %+v", got)
		}
		if code == dot {
			if got[0].Function.Name != "goal" || !strings.Contains(got[0].Function.Arguments, "北京天气") {
				t.Fatalf("tools.goal = %+v", got[0].Function)
			}
			continue
		}
		if got[0].Function.Name != "generate_pdf" || !strings.Contains(got[0].Function.Arguments, "今日天气") {
			t.Fatalf("tools bracket = %+v", got[0].Function)
		}
	}
	for _, code := range []string{
		`return tools.shell({command: "dir"})`,
		`return tools.opencode.models({query: "x"})`,
		`return mytools.goal({action: "plan"})`,
		`return tools?.goal({action: "plan", understanding: "北京", steps: [{title: "采集"}]})`,
	} {
		raw, err := json.Marshal(map[string]string{"code": code})
		if err != nil {
			t.Fatal(err)
		}
		got := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
			Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
		}})
		name := ""
		if len(got) == 1 {
			name = got[0].Function.Name
		}
		wantGoal := strings.Contains(code, "tools?.goal")
		if wantGoal {
			if name != "goal" || !strings.Contains(got[0].Function.Arguments, "北京") {
				t.Fatalf("optional %s -> %+v", code, got)
			}
			continue
		}
		if name != "execute" {
			t.Fatalf("%s was rewritten to %s", code, name)
		}
	}
	caller := []byte(`{"model":"hy4-flash-free","tools":[{"type":"function","function":{"name":"count_things","parameters":{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}}}],"messages":[{"role":"user","content":"数一数"}]}`)
	body, err := openCodeFreeClientBody(caller)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	last := parsed.Messages[len(parsed.Messages)-1].Content
	if !strings.Contains(last, `MACLAW_CALL("count_things", {"n":0})`) {
		t.Fatalf("integer example missing: %s", last)
	}
}

func TestOpenCodeFreeMaclawCallUsesNameBridge(t *testing.T) {
	code := `return MACLAW_CALL("shell", {"command":"dir","workdir":"D:\\oc","timeout":120000}); return MACLAW_CALL("read", {"filePath":"a.txt","offset":10,"limit":20})`
	raw, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	got := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
		ID:       "call-1",
		Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
	}})
	if len(got) != 2 {
		t.Fatalf("calls = %+v", got)
	}
	shellArgs := openCodeFreeArgs(t, got[0].Function.Arguments)
	if got[0].ID != "call-1" || got[0].Function.Name != "bash" || shellArgs["working_dir"] == nil || shellArgs["workdir"] != nil || shellArgs["timeout"] != float64(120) {
		t.Fatalf("shell = %s %s", got[0].Function.Name, got[0].Function.Arguments)
	}
	readArgs := openCodeFreeArgs(t, got[1].Function.Arguments)
	if got[1].ID != "call-1#2" || got[1].Function.Name != "read_file" || readArgs["path"] == nil || readArgs["filePath"] != nil || readArgs["start_line"] != float64(10) || readArgs["lines"] != float64(20) {
		t.Fatalf("read = %s %s", got[1].Function.Name, got[1].Function.Arguments)
	}
	kept := AdaptOpenCodeFreeToolCalls([]string{"shell"}, []ToolCall{{
		Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
	}})
	keptShell := openCodeFreeArgs(t, kept[0].Function.Arguments)
	if len(kept) != 2 || kept[0].Function.Name != "shell" || keptShell["workdir"] == nil || keptShell["timeout"] != float64(120000) || kept[1].Function.Name != "read_file" {
		t.Fatalf("declared shell = %+v", kept)
	}
}

func openCodeFreeArgs(t *testing.T, raw string) map[string]any {
	t.Helper()
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	return args
}

func TestOpenCodeFreeHostCallRoundTripAndReplay(t *testing.T) {
	presented, ok := presentOpenCodeFreeHostCall("goal", `{"action":"plan","understanding":"北京","steps":[{"title":"采集"}],"acceptance_criteria":["pdf"]}`)
	if !ok {
		t.Fatal("present")
	}
	got := AdaptOpenCodeFreeToolCalls(nil, []ToolCall{{
		ID:       "c1",
		Function: ToolCallFunction{Name: "execute", Arguments: presented},
	}})
	if len(got) != 1 || got[0].Function.Name != "goal" || !strings.Contains(got[0].Function.Arguments, "北京") {
		t.Fatalf("round trip %+v", got)
	}
	if _, ok := presentOpenCodeFreeHostCall("shell", `{"command":"dir"}`); ok {
		t.Fatal("shell was wrapped")
	}
	if _, ok := presentOpenCodeFreeHostCall("bash", `{"command":"dir"}`); ok {
		t.Fatal("bash was wrapped")
	}

	caller := []byte(`{"model":"hy4-flash-free","tools":[{"type":"function","function":{"name":"goal"}},{"type":"function","function":{"name":"generate_pdf"}}],"messages":[{"role":"user","content":"做计划"},{"role":"assistant","content":"","tool_calls":[{"function":{"name":"goal","arguments":"{\"action\":\"plan\",\"understanding\":\"北京\"}"}}]},{"role":"tool","content":"plan stored"}]}`)
	body, err := openCodeFreeClientBody(caller)
	if err != nil {
		t.Fatal(err)
	}
	if n := userReminderCount(t, body); n != 1 {
		t.Fatalf("reminder count = %d\n%s", n, body)
	}
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Messages) != 4 {
		t.Fatalf("messages = %+v", parsed.Messages)
	}
	userTurn := parsed.Messages[1].Content
	if !strings.HasPrefix(userTurn, "做计划") || strings.Count(userTurn, "<system-reminder>") != 1 || !strings.Contains(userTurn, `MACLAW_CALL("goal"`) {
		t.Fatalf("user = %s", userTurn)
	}
	assistant := parsed.Messages[2].Content
	if !strings.Contains(assistant, "execute") || !strings.Contains(assistant, `return MACLAW_CALL("goal"`) || strings.Contains(assistant, `\"`) || strings.HasPrefix(strings.TrimSpace(assistant), "goal") {
		t.Fatalf("assistant replay = %s", assistant)
	}
	last := parsed.Messages[3].Content
	if last != "plan stored" || strings.Contains(last, "<system-reminder>") {
		t.Fatalf("tool observation = %s", last)
	}

	note := OpenCodeFreeHostChannelNote([]string{"goal"})
	encoded, err := json.Marshal(userTurn)
	if err != nil {
		t.Fatal(err)
	}
	again := []byte(`{"model":"hy4-flash-free","tools":[{"type":"function","function":{"name":"goal"}},{"type":"function","function":{"name":"generate_pdf"}}],"messages":[{"role":"user","content":` + string(encoded) + `}]}`)
	second, err := openCodeFreeClientBody(again)
	if err != nil {
		t.Fatal(err)
	}
	if n := userReminderCount(t, second); n != 1 {
		t.Fatalf("replayed tail stacked the note (%d):\n%s", n, second)
	}
	if note == "" {
		t.Fatal("note")
	}

	// BR-20261121-1B807E2B4CDB: official execute returned a denial, and the
	// next body appended the harness reminder onto that tool observation.
	denied := []byte(`{"model":"hy4-flash-free","tools":[{"type":"function","function":{"name":"chat_task_request_input"}}],"messages":[{"role":"user","content":"扫描论文"},{"role":"assistant","content":"","tool_calls":[{"function":{"name":"execute","arguments":"{\"code\":\"const fs = require('fs');\"}"}}]},{"role":"tool","content":"Error: this execute call did not deliver a host tool. Submit one return MACLAW_CALL statement. Host tools: chat_task_request_input. Do not inspect the catalog."}]}`)
	deniedBody, err := openCodeFreeClientBody(denied)
	if err != nil {
		t.Fatal(err)
	}
	if n := userReminderCount(t, deniedBody); n != 1 {
		t.Fatalf("denied reminder count = %d\n%s", n, deniedBody)
	}
	var deniedParsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(deniedBody, &deniedParsed); err != nil {
		t.Fatal(err)
	}
	var sawTask, sawDenial bool
	for _, msg := range deniedParsed.Messages {
		if strings.Contains(msg.Content, "did not deliver a host tool") {
			sawDenial = true
			if strings.Contains(msg.Content, "<system-reminder>") || strings.Contains(msg.Content, "扫描论文") {
				t.Fatalf("reminder rode on the tool observation: %s", msg.Content)
			}
		}
		if strings.HasPrefix(msg.Content, "扫描论文") {
			sawTask = true
			if strings.Count(msg.Content, "<system-reminder>") != 1 {
				t.Fatalf("task turn = %s", msg.Content)
			}
		}
	}
	if !sawTask || !sawDenial {
		t.Fatalf("denied body = %s", deniedBody)
	}
}

func TestOpenCodeFreeGoalPhaseKeepsOneExample(t *testing.T) {
	planMessages := []map[string]string{
		{"role": "user", "content": "[系统续接][plan] 本轮只做规划"},
		{"role": "assistant", "content": "[系统续接][execute] index=9 引用不是阶段"},
	}
	// The assistant echo is not a phase change, so the phase stays plan and
	// its example carries no step index. The note rides on the latest user
	// turn of the wire body, and this body ends with an assistant message:
	// the note is on the user turn, not on the tail.
	planNote := openCodeFreeGoalNote(t, planMessages)
	if !strings.Contains(planNote, `"action":"plan"`) || strings.Contains(planNote, `"action":"step"`) || strings.Contains(planNote, `"index":9`) {
		t.Fatalf("plan note = %s", planNote)
	}
	if strings.Contains(planMessages[1]["content"], "<system-reminder>") {
		t.Fatalf("the note is wire-only, the caller's tail was rewritten: %v", planMessages)
	}
	replay := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接][plan] 本轮只做规划\n\n" + planNote},
	})
	if strings.Count(replay, "<system-reminder>") != 1 {
		t.Fatalf("plan note stacked: %s", replay)
	}

	executeNote := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接][plan] 上一轮规划"},
		{"role": "user", "content": "[系统续接][execute] 本轮只能提交步骤 4。调用 goal(action=\"step\", index=4, status=\"done\")"},
	})
	if strings.Contains(executeNote, "acceptance_criteria") || !strings.Contains(executeNote, `"index":4`) {
		t.Fatalf("execute note = %s", executeNote)
	}
	plain := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "做一份计划"},
	})
	if !strings.Contains(plain, "acceptance_criteria") || !strings.Contains(plain, `"action":"step"`) || !strings.Contains(plain, `"status":"progress"`) || strings.Contains(plain, `"status":"done"`) {
		t.Fatalf("unmarked note = %s", plain)
	}
}

func TestOpenCodeFreeGoalPhaseFollowsHostPrefix(t *testing.T) {
	carried := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接][execute] 按步骤执行 index=4"},
		{"role": "user", "content": "再查一下明天"},
	})
	if !strings.Contains(carried, "acceptance_criteria") || !strings.Contains(carried, `"index":1`) || strings.Contains(carried, `"index":4`) {
		t.Fatalf("old phase stuck: %s", carried)
	}
	quoted := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "请看这段：[系统续接][plan] 不是阶段"},
	})
	if !strings.Contains(quoted, "acceptance_criteria") || !strings.Contains(quoted, `"action":"step"`) {
		t.Fatalf("quoted marker became a phase: %s", quoted)
	}
	continueNote := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接] 继续当前目标，不要重新规划。index=2"},
	})
	if strings.Contains(continueNote, "acceptance_criteria") || !strings.Contains(continueNote, `"index":2`) || !strings.Contains(continueNote, `"status":"progress"`) {
		t.Fatalf("continue note = %s", continueNote)
	}
	untagged := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接] 先理解并分解目标\n本轮只做规划"},
	})
	if !strings.Contains(untagged, `"action":"plan"`) || strings.Contains(untagged, `"action":"step"`) {
		t.Fatalf("untagged plan note = %s", untagged)
	}
	echoed := openCodeFreeGoalNote(t, []map[string]string{
		{"role": "user", "content": "[系统续接][execute] 按步骤执行\n当前步骤：本轮只做规划\nindex=3"},
	})
	if strings.Contains(echoed, "acceptance_criteria") || !strings.Contains(echoed, `"index":3`) {
		t.Fatalf("execute tag lost to the step title: %s", echoed)
	}

	plan := `return MACLAW_CALL("goal", {"action":"plan","understanding":"北京天气","steps":[{"title":"采集"}],"acceptance_criteria":["pdf"]})`
	step := `return MACLAW_CALL("goal", {"action":"step","index":2,"status":"progress","result":"已查到预报"})`
	both := plan + "; " + step
	got := adaptOpenCodeExecute(t, "[系统续接] 继续当前目标，不要重新规划。", both)
	if len(got) != 1 || got[0].Function.Name != "goal" || !strings.Contains(got[0].Function.Arguments, "已查到预报") {
		t.Fatalf("continue turn = %+v", got)
	}
	got = adaptOpenCodeExecute(t, "[系统续接] 先理解并分解目标\n本轮只做规划", both)
	if len(got) != 1 || !strings.Contains(got[0].Function.Arguments, "北京天气") || strings.Contains(got[0].Function.Arguments, "已查到预报") {
		t.Fatalf("untagged plan turn = %+v", got)
	}
	got = adaptOpenCodeExecute(t, "[系统续接][execute] 按步骤执行\n当前步骤：本轮只做规划\nindex=3", both)
	if len(got) != 1 || !strings.Contains(got[0].Function.Arguments, "已查到预报") || strings.Contains(got[0].Function.Arguments, "北京天气") {
		t.Fatalf("echoed plan sentence = %+v", got)
	}
}

func TestOpenCodeFreeGoalPhaseAndDotsAreNotDelivery(t *testing.T) {
	plan := `return MACLAW_CALL("goal", {"action":"plan","understanding":"北京天气","steps":[{"title":"采集"}],"acceptance_criteria":["pdf"]})`
	step := `return MACLAW_CALL("goal", {"action":"step","index":1,"status":"done","result":"已写入 pdf"})`
	both := plan + "; " + step
	got := adaptOpenCodeExecute(t, "[系统续接][plan] 本轮只做规划", both)
	if len(got) != 1 || got[0].Function.Name != "goal" || strings.Contains(got[0].Function.Arguments, `"step"`) || !strings.Contains(got[0].Function.Arguments, "北京天气") {
		t.Fatalf("plan turn = %+v", got)
	}
	got = adaptOpenCodeExecute(t, "[系统续接][execute] 当前步骤 index=4", both)
	if len(got) != 1 || got[0].Function.Name != "goal" || !strings.Contains(got[0].Function.Arguments, `"step"`) || strings.Contains(got[0].Function.Arguments, "北京天气") {
		t.Fatalf("execute turn = %+v", got)
	}
	got = adaptOpenCodeExecute(t, "", both)
	if len(got) != 2 || got[0].Function.Name != "goal" || got[1].Function.Name != "goal" {
		t.Fatalf("unmarked turn = %+v", got)
	}

	dotted := `return MACLAW_CALL("goal", {"action":"plan","understanding":"...","steps":[{"title":"...","detail":"..."}],"acceptance_criteria":["..."]})`
	got = adaptOpenCodeExecute(t, "[系统续接][plan] 本轮只做规划", dotted)
	if len(got) != 1 || got[0].Function.Name != "execute" {
		t.Fatalf("dotted plan was stored: %+v", got)
	}
	ellipsis := "return MACLAW_CALL('goal', {action: 'plan', understanding: '…', steps: [{title: '采集'}], acceptance_criteria: ['pdf']})"
	got = adaptOpenCodeExecute(t, "[系统续接][plan] 本轮只做规划", ellipsis)
	if len(got) != 1 || got[0].Function.Name != "execute" {
		t.Fatalf("ellipsis plan was stored: %+v", got)
	}
	real := "return MACLAW_CALL('goal', {action: 'plan', understanding: '北京天气，生成 pdf。', steps: [{title: '采集…', detail: '查预报'}], acceptance_criteria: ['可打开的 pdf']})"
	got = adaptOpenCodeExecute(t, "[系统续接][plan] 本轮只做规划", real)
	if len(got) != 1 || got[0].Function.Name != "goal" || !strings.Contains(got[0].Function.Arguments, "采集…") {
		t.Fatalf("real ellipsis = %+v", got)
	}
	mixed := plan + `; return MACLAW_CALL("generate_pdf", {"content":"..."})`
	got = adaptOpenCodeExecute(t, "[系统续接][plan] 本轮只做规划", mixed)
	if len(got) != 1 || got[0].Function.Name != "goal" {
		t.Fatalf("dotted pdf was kept: %+v", got)
	}

	dottedArgs, err := json.Marshal(map[string]string{"code": dotted})
	if err != nil {
		t.Fatal(err)
	}
	retry := OpenCodeFreeHostChannelRetryForTurn([]string{"goal", "bash"}, "[系统续接][plan] 本轮只做规划", string(dottedArgs))
	if !strings.Contains(retry, "example dots") || !strings.Contains(retry, "goal") || strings.Contains(retry, "Do not retry") || strings.Contains(retry, "<system-reminder>") {
		t.Fatalf("dots retry = %s", retry)
	}
	stepArgs, err := json.Marshal(map[string]string{"code": step})
	if err != nil {
		t.Fatal(err)
	}
	retry = OpenCodeFreeHostChannelRetryForTurn([]string{"goal"}, "[系统续接][plan] 本轮只做规划", string(stepArgs))
	if !strings.Contains(retry, "action plan") || strings.Contains(retry, "action step") || strings.Contains(retry, "example dots") {
		t.Fatalf("phase retry = %s", retry)
	}
	planArgs, err := json.Marshal(map[string]string{"code": plan})
	if err != nil {
		t.Fatal(err)
	}
	retry = OpenCodeFreeHostChannelRetryForTurn([]string{"goal"}, "[系统续接][execute] index=4", string(planArgs))
	if !strings.Contains(retry, "action step") || strings.Contains(retry, "action plan") {
		t.Fatalf("execute retry = %s", retry)
	}
	retry = OpenCodeFreeHostChannelRetryForTurn([]string{"goal"}, "[系统续接][plan] 本轮只做规划", `{"code":"return Object.keys(tools||{})"}`)
	if !strings.Contains(retry, "did not deliver") || strings.Contains(retry, "example dots") {
		t.Fatalf("catalog retry = %s", retry)
	}
}

func openCodeFreeGoalNote(t *testing.T, messages []map[string]string) string {
	t.Helper()
	caller, err := json.Marshal(map[string]any{
		"model":    "hy4-flash-free",
		"tools":    []any{map[string]any{"type": "function", "function": map[string]any{"name": "goal"}}},
		"messages": messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := openCodeFreeClientBody(caller)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	// The note rides on the latest user turn of the wire body, and the body
	// does not always end with a user message: an assistant tail, or an
	// echoed step, stays behind it. Read the note back from the message that
	// carries it instead of assuming it is last, otherwise a test with an
	// assistant tail fails on the tail rather than on the note.
	last := ""
	for i := len(parsed.Messages) - 1; i >= 0; i-- {
		if strings.Contains(parsed.Messages[i].Content, "<system-reminder>") {
			last = parsed.Messages[i].Content
			break
		}
	}
	if last == "" {
		last = parsed.Messages[len(parsed.Messages)-1].Content
		t.Fatalf("note missing: %s", last)
	}
	at := strings.LastIndex(last, "<system-reminder>")
	if at < 0 {
		t.Fatalf("note missing: %s", last)
	}
	return last
}

func adaptOpenCodeExecute(t *testing.T, userText, code string) []ToolCall {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	return AdaptOpenCodeFreeToolCallsForTurn(userText, nil, []ToolCall{{
		ID:       "call-1",
		Function: ToolCallFunction{Name: "execute", Arguments: string(raw)},
	}})
}

func userReminderCount(t *testing.T, body []byte) int {
	t.Helper()
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, msg := range parsed.Messages {
		if msg.Role != "system" {
			n += strings.Count(msg.Content, "<system-reminder>")
		}
	}
	return n
}
