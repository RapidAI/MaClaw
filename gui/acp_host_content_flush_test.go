package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/acpagent"
)

// Ensures we always encode agent_message_chunk the way VS Code ACP client expects:
// params.update.sessionUpdate + params.update.content.type/text.
func TestACPAgentMessageChunkShape(t *testing.T) {
	params := acpagent.SessionUpdateParams{
		SessionID: "acp_gui_test",
		Update: map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": "你好，我在。"},
		},
	}
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params":  params,
	})
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(line, &env) != nil {
		t.Fatal("envelope")
	}
	var p acpagent.SessionUpdateParams
	if json.Unmarshal(env["params"], &p) != nil {
		t.Fatal("params")
	}
	if p.SessionID != "acp_gui_test" {
		t.Fatalf("sessionId=%q", p.SessionID)
	}
	su, _ := p.Update["sessionUpdate"].(string)
	if su != "agent_message_chunk" {
		t.Fatalf("sessionUpdate=%v", p.Update["sessionUpdate"])
	}
	content, _ := p.Update["content"].(map[string]any)
	if content == nil {
		// after unmarshal through map[string]any nested objects become map
		raw, _ := json.Marshal(p.Update["content"])
		_ = json.Unmarshal(raw, &content)
	}
	// content may be map[string]interface{} from round-trip
	b, _ := json.Marshal(p.Update["content"])
	var c struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(b, &c) != nil || c.Type != "text" || !strings.Contains(c.Text, "你好") {
		t.Fatalf("content=%s", string(b))
	}
}

func TestVisibleACPStreamDeltaRemovesControlCharacters(t *testing.T) {
	if got := visibleACPStreamDelta("visible\x00 text\nnext\u0085"); got != "visible text\nnext" {
		t.Fatalf("visible delta = %q", got)
	}
}
