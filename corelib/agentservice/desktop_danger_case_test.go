package agentservice

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotDangerCases(t *testing.T) {
	desktopbot.ResetDangerState()
	t.Cleanup(desktopbot.ResetDangerState)
	desktopbot.NoteDangerAsk([]string{"conv-danger"}, desktopbot.ConnectorAsk{
		Kind: "ask",
		Text: "我需要执行「git push」才能继续，可以吗？",
	})
	cb := &coreAgentCallbacks{session: Session{ID: "conv-danger"}}
	meta := map[string]string{}
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != "我需要执行「git push」才能继续，可以吗？" || meta["send_to_user_kind"] != "ask" {
		t.Fatalf("metadata=%#v", meta)
	}
	meta["send_to_user"] = "已经有一句"
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != "已经有一句" {
		t.Fatal("an existing sentence was overwritten")
	}
}
