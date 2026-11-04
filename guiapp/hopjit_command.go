package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/hopjit"
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
	switch action {
	case "usage":
		return &IMAgentResponse{Text: hopJITUsage()}, true
	case "run":
		var result hopjit.ReuseResult
		var err error
		if strings.HasPrefix(strings.TrimSpace(payload), "# Spec:") {
			result, err = h.app.RunHopSpec(payload, nil)
		} else {
			result, err = h.app.RunHopTask(payload)
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
		result, err := h.app.ResumeHopSpec(runID, answers)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return hopJITReuseResponse(result), true
	case "abort":
		runID, reason := hopJITRunIDAndReason(payload)
		if runID == "" {
			return &IMAgentResponse{Text: hopJITUsage()}, true
		}
		if err := h.app.AbortHopSpec(runID, reason); err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return &IMAgentResponse{Text: "HopJIT 已中止。\nrunId: " + runID}, true
	case "status":
		runID := strings.TrimSpace(payload)
		if runID == "" {
			return &IMAgentResponse{Text: hopJITUsage()}, true
		}
		store, err := h.app.openHopJITPersistence()
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		defer store.Close()
		snapshot, err := store.LoadSnapshot(context.Background(), runID)
		if err != nil {
			return &IMAgentResponse{Error: err.Error()}, true
		}
		return hopJITSnapshotResponse(snapshot), true
	default:
		return &IMAgentResponse{Text: hopJITUsage()}, true
	}
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
	case "run", "resume", "status", "abort":
		return action, payload, true
	default:
		return "run", rest, true
	}
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
		outputs := map[string]any{}
		for _, decl := range snapshot.Spec.Outputs {
			if value, ok := snapshot.Variables[decl.Name]; ok && value.Assigned {
				outputs[decl.Name] = value.Value
			}
		}
		if len(outputs) > 0 {
			encoded, _ := json.Marshal(outputs)
			response.Text += "\n\n" + string(encoded)
		}
	}
	return response
}

func hopJITUsage() string {
	return "HopJIT 用法:\n" +
		"/hop run <HopSpec Markdown>\n" +
		"/hop status <runId>\n" +
		"/hop resume <runId> <JSON 回答>\n" +
		"/hop abort <runId> [原因]"
}
