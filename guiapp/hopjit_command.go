package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
	"github.com/RapidAI/CodeClaw/corelib/hopspec"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func (h *IMMessageHandler) handleHopJITCommand(msg IMUserMessage, trimmed string, onProgress tool.ProgressCallback) (*IMAgentResponse, bool) {
	action, payload, ok := parseHopJITCommand(trimmed)
	if !ok {
		return nil, false
	}
	if action == "usage" {
		return &IMAgentResponse{Text: hopJITUsage()}, true
	}
	if h == nil || h.app == nil {
		return &IMAgentResponse{Error: "HopJIT app is unavailable"}, true
	}
	identity := hopJITExecutionIdentity{
		PrincipalID: msg.UserID, PolicyOwnerID: h.workflowPolicyUserID(msg.UserID),
		SessionID: msg.UserID, Platform: msg.Platform,
	}
	switch action {
	case "usage":
		return &IMAgentResponse{Text: hopJITUsage()}, true
	case "compile":
		if strings.TrimSpace(payload) == "" {
			return &IMAgentResponse{Text: "任务不能为空\n" + hopJITUsage()}, true
		}
		spec, err := h.app.compileHopSpecWithIdentity(identity, payload)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return &IMAgentResponse{Text: spec, ResponseSource: "hopjit"}, true
	case "define":
		id, markdown, err := parseHopJITDefine(payload)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		provider, err := h.app.hopJITSpecProvider(identity)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		if err := provider.SaveSpec(id, markdown); err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return &IMAgentResponse{Text: "HopSpec 已注册\nspecId: " + id, ResponseSource: "hopjit"}, true
	case "specs":
		provider, err := h.app.hopJITSpecProvider(identity)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		ids, err := provider.ListSpecs()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		text := "暂无已注册 HopSpec"
		if len(ids) > 0 {
			text = "已注册 HopSpec:\n" + strings.Join(ids, "\n")
		}
		return &IMAgentResponse{Text: text, ResponseSource: "hopjit"}, true
	case "delete":
		id := strings.TrimSpace(payload)
		if err := validateHopSpecID(id); err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		provider, err := h.app.hopJITSpecProvider(identity)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		if err := provider.DeleteSpec(id); err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return &IMAgentResponse{Text: "HopSpec 已删除\nspecId: " + id, ResponseSource: "hopjit"}, true
	case "run":
		runPayload, inputs, err := parseHopJITRunPayload(payload)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		var result hopjit.ReuseResult
		if directHopSpec(runPayload) {
			result, err = h.app.runHopSpecWithIdentity(identity, runPayload, inputs)
		} else {
			result, err = h.app.runHopTaskWithIdentity(identity, runPayload)
		}
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return hopJITReuseResponse(result), true
	case "resume":
		runID, rawAnswers, err := parseHopJITResume(payload)
		if err != nil {
			return &IMAgentResponse{Text: err.Error() + "\n" + hopJITUsage()}, true
		}
		var answers map[string]any
		if err := json.Unmarshal([]byte(rawAnswers), &answers); err != nil {
			return &IMAgentResponse{Text: "回答必须是 JSON 对象，例如 {\"answer\":\"继续\"}"}, true
		}
		result, err := h.app.resumeHopSpecWithIdentity(identity, runID, answers)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return hopJITReuseResponse(result), true
	case "abort":
		runID, reason := hopJITRunIDAndReason(payload)
		if runID == "" {
			return &IMAgentResponse{Text: hopJITUsage()}, true
		}
		if err := h.app.abortHopSpecWithIdentity(identity, runID, reason); err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return &IMAgentResponse{Text: "HopJIT 已中止。\nrunId: " + runID}, true
	case "list":
		store, err := h.app.openHopJITPersistence()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		snapshots, err := store.ListSnapshots(context.Background())
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return hopJITRunListResponse(snapshots, identity), true
	case "spec", "status", "events":
		runID := strings.TrimSpace(payload)
		if runID == "" {
			return &IMAgentResponse{Text: hopJITUsage()}, true
		}
		snapshot, err := h.app.readHopJITSnapshot(identity, runID)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		switch action {
		case "spec":
			return &IMAgentResponse{Text: hopspec.Format(snapshot.Spec), ResponseSource: "hopjit"}, true
		case "events":
			return hopJITEventsResponse(snapshot), true
		default:
			return hopJITSnapshotResponse(snapshot), true
		}
	default:
		return &IMAgentResponse{Text: hopJITUsage()}, true
	}
}

func directHopSpec(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "# Spec:") || strings.HasPrefix(trimmed, "# Spec：")
}

func parseHopJITCommand(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 4 || !strings.EqualFold(trimmed[:4], "/hop") {
		return "", "", false
	}
	if len(trimmed) > 4 {
		next := trimmed[4]
		if next != ' ' && next != '\t' && next != '\n' && next != '\r' {
			return "", "", false
		}
	}
	rest := strings.TrimSpace(trimmed[4:])
	if rest == "" {
		return "usage", "", true
	}
	action, payload := rest, ""
	if index := strings.IndexAny(rest, " \t\r\n"); index >= 0 {
		action, payload = strings.ToLower(rest[:index]), strings.TrimSpace(rest[index+1:])
	} else {
		action = strings.ToLower(action)
	}
	switch action {
	case "help", "usage", "--help", "-h":
		return "usage", "", true
	case "run", "compile", "resume", "status", "abort", "list", "spec", "define", "specs", "delete", "events":
		return action, payload, true
	default:
		return "run", rest, true
	}
}

func parseHopJITDefine(payload string) (string, string, error) {
	payload = strings.TrimSpace(payload)
	index := strings.IndexAny(payload, " \t\r\n")
	if index <= 0 {
		return "", "", errors.New("用法: /hop define <specId> <HopSpec Markdown>")
	}
	id := strings.TrimSpace(payload[:index])
	markdown := strings.TrimSpace(payload[index+1:])
	if err := validateHopSpecID(id); err != nil {
		return "", "", err
	}
	if markdown == "" {
		return "", "", errors.New("HopSpec Markdown 不能为空")
	}
	return id, markdown, nil
}

func parseHopJITRunPayload(payload string) (string, map[string]any, error) {
	payload = strings.TrimSpace(payload)
	const flag = "--inputs"
	if !strings.HasPrefix(payload, flag) {
		return payload, nil, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(payload, flag))
	if rest == "" {
		return "", nil, errors.New("--inputs 后需要 JSON 对象")
	}
	decoder := json.NewDecoder(strings.NewReader(rest))
	var inputs map[string]any
	if err := decoder.Decode(&inputs); err != nil {
		return "", nil, errors.New("HopJIT Inputs 必须是完整 JSON 对象: " + err.Error())
	}
	if inputs == nil {
		return "", nil, errors.New("HopJIT Inputs 必须是 JSON 对象")
	}
	remainder := strings.TrimSpace(rest[decoder.InputOffset():])
	if !strings.HasPrefix(remainder, "--") {
		return "", nil, errors.New("HopJIT Inputs 后需要 -- 分隔符")
	}
	spec := strings.TrimSpace(remainder[2:])
	if !directHopSpec(spec) {
		return "", nil, errors.New("--inputs 仅用于直接运行 # Spec HopSpec")
	}
	return spec, inputs, nil
}

func parseHopJITResume(payload string) (string, string, error) {
	payload = strings.TrimSpace(payload)
	index := strings.IndexAny(payload, " \t\r\n")
	if index <= 0 {
		return "", "", errors.New("用法: /hop resume <runId> <JSON 回答>")
	}
	runID := strings.TrimSpace(payload[:index])
	answers := strings.TrimSpace(payload[index+1:])
	if runID == "" || answers == "" {
		return "", "", errors.New("runId 和 JSON 回答不能为空")
	}
	return runID, answers, nil
}

func hopJITRunIDAndReason(payload string) (string, string) {
	payload = strings.TrimSpace(payload)
	if index := strings.IndexAny(payload, " \t\r\n"); index >= 0 {
		return strings.TrimSpace(payload[:index]), strings.TrimSpace(payload[index+1:])
	}
	return payload, "用户中止"
}

func hopJITEventsResponse(snapshot hopjit.EngineSnapshot) *IMAgentResponse {
	events := snapshot.Events
	total := len(events)
	if total > 50 {
		events = events[total-50:]
	}
	response := &IMAgentResponse{ResponseSource: "hopjit"}
	if total == 0 {
		response.Text = "暂无 HopJIT 执行事件"
		return response
	}
	var text strings.Builder
	fmt.Fprintf(&text, "HopJIT 执行事件: %d", total)
	if total > len(events) {
		fmt.Fprintf(&text, "（显示最近 %d 条）", len(events))
	}
	for _, event := range events {
		fmt.Fprintf(&text, "\n#%d", event.Sequence)
		if event.StepID != "" {
			fmt.Fprintf(&text, " [%s]", event.StepID)
		}
		fmt.Fprintf(&text, " %s", event.Event)
		if event.Detail != "" {
			fmt.Fprintf(&text, " — %s", event.Detail)
		}
	}
	response.Text = text.String()
	response.Fields = []IMResponseField{{Label: "事件数", Value: fmt.Sprint(total)}}
	return response
}

func hopJITReuseResponse(result hopjit.ReuseResult) *IMAgentResponse {
	response := hopJITSnapshotResponse(result.Snapshot)
	if result.Status == hopjit.NextFailed && response.Error == "" {
		if result.Failure != nil {
			response.Error = result.Failure.Reason
		} else {
			response.Error = "HopJIT 执行失败"
		}
	}
	return response
}

func hopJITSnapshotResponse(snapshot hopjit.EngineSnapshot) *IMAgentResponse {
	run := ProjectHopRun(snapshot)
	response := &IMAgentResponse{
		Text: "HopJIT 状态: " + run.Status + "\nrunId: " + snapshot.RunID,
		Fields: []IMResponseField{
			{Label: "runId", Value: snapshot.RunID},
			{Label: "状态", Value: run.Status},
			{Label: "剩余步骤", Value: fmt.Sprint(run.Remaining)},
		},
		ResponseSource: "hopjit",
	}
	if snapshot.TerminalState == hopjit.TerminalFailed && snapshot.TerminalFailure != nil {
		response.Error = snapshot.TerminalFailure.Reason
	}
	if snapshot.Pause != nil {
		response.Text += "\n\n" + snapshot.Pause.Question
		response.Fields = append(response.Fields, IMResponseField{Label: "等待输入", Value: snapshot.Pause.Question})
		response.Actions = []IMResponseAction{{
			Label:   "填写回答",
			Command: "/hop resume " + snapshot.RunID + " <JSON 回答>",
		}}
		if snapshot.Pause.Kind == "confirm" {
			response.Confirmation = &IMResponseConfirmation{ID: snapshot.RunID, Summary: snapshot.Pause.Question, TaskType: "hopjit_confirm", Status: "pending"}
			response.Actions = []IMResponseAction{
				{Label: "批准", Command: "/hop resume " + snapshot.RunID + " {\"approved\":true}", Style: "primary"},
				{Label: "拒绝", Command: "/hop resume " + snapshot.RunID + " {\"approved\":false}", Style: "danger"},
			}
		}
	}
	if snapshot.TerminalState == hopjit.TerminalCompleted {
		if answer, ok := hopCompletedAnswer(snapshot); ok {
			response.Text += "\n\n" + answer
		} else if encoded, ok := hopCompletedOutputJSON(snapshot); ok {
			response.Text += "\n\n" + encoded
		}
	}
	return response
}

// hopCompletedAnswer is the user-facing text of a finished hop. A string
// output is that answer. A bool or number stays in the JSON object.
func hopCompletedAnswer(snapshot hopjit.EngineSnapshot) (string, bool) {
	var texts []string
	for _, decl := range snapshot.Spec.Outputs {
		value, ok := snapshot.Variables[decl.Name]
		if !ok || !value.Assigned || value.Value == nil {
			continue
		}
		text, ok := value.Value.(string)
		if !ok {
			return "", false
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return "", false
		}
		texts = append(texts, text)
	}
	if len(texts) == 0 {
		return "", false
	}
	return strings.Join(texts, "\n\n"), true
}

func hopCompletedOutputJSON(snapshot hopjit.EngineSnapshot) (string, bool) {
	outputs := map[string]any{}
	for _, decl := range snapshot.Spec.Outputs {
		if value, ok := snapshot.Variables[decl.Name]; ok && value.Assigned {
			outputs[decl.Name] = value.Value
		}
	}
	if len(outputs) == 0 {
		return "", false
	}
	encoded, err := json.Marshal(outputs)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func hopJITRunListResponse(snapshots []hopjit.EngineSnapshot, identity hopJITExecutionIdentity) *IMAgentResponse {
	visible := make([]hopjit.EngineSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if authorizeHopJITSnapshot(snapshot, identity) == nil {
			visible = append(visible, snapshot)
		}
	}
	total := len(visible)
	if total > 20 {
		visible = visible[:20]
	}
	response := &IMAgentResponse{ResponseSource: "hopjit"}
	if total == 0 {
		response.Text = "暂无 HopJIT 运行记录"
		return response
	}
	var text strings.Builder
	fmt.Fprintf(&text, "HopJIT 运行记录: %d", total)
	if total > len(visible) {
		fmt.Fprintf(&text, "（显示最近 %d 条）", len(visible))
	}
	for _, snapshot := range visible {
		goal := strings.TrimSpace(snapshot.Spec.Goal)
		if goal == "" {
			goal = "未命名任务"
		}
		fmt.Fprintf(&text, "\n%s [%s] %s", snapshot.RunID, ProjectHopRun(snapshot).Status, goal)
	}
	response.Text = text.String()
	response.Fields = []IMResponseField{{Label: "记录数", Value: fmt.Sprint(total)}}
	return response
}

func hopJITUsage() string {
	return "HopJIT 用法:\n" +
		"/hop help\n" +
		"/hop run <任务或 HopSpec Markdown>\n" +
		"/hop run --inputs <JSON对象> -- <HopSpec Markdown>\n" +
		"/hop compile <任务>（只生成 HopSpec，不执行）\n" +
		"/hop define <specId> <HopSpec Markdown>\n" +
		"/hop specs（列出已注册子规约）\n" +
		"/hop delete <specId>\n" +
		"/hop list\n" +
		"/hop spec <runId>（导出 HopSpec）\n" +
		"/hop status <runId>\n" +
		"/hop events <runId>（查看执行事件）\n" +
		"/hop resume <runId> <JSON 回答>\n" +
		"/hop abort <runId> [原因]"
}
