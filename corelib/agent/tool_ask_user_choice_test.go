package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolAskUserAllowMultipleBecomesMultiChoice(t *testing.T) {
	raw := ToolAskUser(map[string]interface{}{
		"question":       "InferX 怎么处理?",
		"options":        []interface{}{"继续用 InferX", "换成本地路径"},
		"allow_multiple": true,
	})
	if !strings.Contains(raw, "__ASK_USER__") {
		t.Fatalf("missing marker: %q", raw)
	}
	payload := strings.TrimPrefix(raw, "__ASK_USER__")
	var req AskUserRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatal(err)
	}
	if req.InputType != "multi_choice" {
		t.Fatalf("input_type=%q want multi_choice", req.InputType)
	}
	if !AskUserAllowsMultiple(&req) {
		t.Fatal("AskUserAllowsMultiple false")
	}
	shown := FormatAskUserForDisplay(&req)
	if !strings.Contains(shown, "A. 继续用 InferX") || !strings.Contains(shown, "B. 换成本地路径") {
		t.Fatalf("display missing letters: %q", shown)
	}
}

func TestToolAskUserOptionsDefaultToChoice(t *testing.T) {
	raw := ToolAskUser(map[string]interface{}{
		"question": "选哪条?",
		"options":  []interface{}{"路径 A", "路径 B"},
	})
	payload := strings.TrimPrefix(raw, "__ASK_USER__")
	var req AskUserRequest
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		t.Fatal(err)
	}
	if req.InputType != "choice" {
		t.Fatalf("input_type=%q want choice", req.InputType)
	}
}

func TestFormatAskUserSelectedReplyJoins(t *testing.T) {
	got := FormatAskUserSelectedReply([]string{"继续用 InferX", " 换成本地路径 ", ""})
	if got != "继续用 InferX；换成本地路径" {
		t.Fatalf("got %q", got)
	}
}
