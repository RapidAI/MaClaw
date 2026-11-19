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

// A desktop handoff ask points at the takeover button instead of a typed
// reply: the person acts on the computer screen, not in this chat.
func TestFormatAskUserTakeoverHint(t *testing.T) {
	shown := FormatAskUserForDisplay(&AskUserRequest{
		Question:  "这一步需要你在当前桌面的浏览器里完成登录或验证。",
		InputType: "text",
		Takeover:  true,
	})
	if !strings.Contains(shown, "请在电脑上完成，或点「接管」继续。") || strings.Contains(shown, "请直接输入您的回复") {
		t.Fatalf("takeover display=%q", shown)
	}
	plain := FormatAskUserForDisplay(&AskUserRequest{Question: "站点密码是多少?", InputType: "text"})
	if !strings.Contains(plain, "请直接输入您的回复") {
		t.Fatalf("plain display=%q", plain)
	}
	// The field rides the stable marker so hosts can tell a handoff ask apart.
	raw := AskUserResultMarker(&AskUserRequest{Question: "q", InputType: "text", Takeover: true})
	parsed, ok := ParseAskUserResult(raw)
	if !ok || !parsed.Takeover {
		t.Fatalf("marker lost takeover: %q", raw)
	}
}
