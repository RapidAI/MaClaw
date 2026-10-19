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
		Text: "要执行 git push 这一次吗？15 秒没有回答就不执行。",
	})
	cb := &coreAgentCallbacks{session: Session{ID: "conv-danger"}}
	meta := map[string]string{}
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != "要执行 git push 这一次吗？15 秒没有回答就不执行。" || meta["send_to_user_kind"] != "ask" {
		t.Fatalf("metadata=%#v", meta)
	}
	meta["send_to_user"] = "已经有一句"
	cb.mergeConnectorAsk(meta)
	if meta["send_to_user"] != "已经有一句" {
		t.Fatal("an existing sentence was overwritten")
	}
}
