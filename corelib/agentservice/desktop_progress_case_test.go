package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotProgressCases(t *testing.T) {
	previous := CloudProgress
	var calls int
	var gotName, gotTask string
	CloudProgress = func(_ context.Context, _, _, taskID, toolName, _ string) {
		calls++
		gotName = toolName
		gotTask = taskID
	}
	t.Cleanup(func() { CloudProgress = previous })

	cb := &coreAgentCallbacks{
		botID:           "bot-progress",
		session:         Session{ID: "conv-progress"},
		messageMetadata: map[string]string{"bot_phase": "execute"},
	}
	out := cb.finishBotToolResult("bash", `{}`, agent.ToolExecutionResult{
		Result:  "listed the page",
		Outcome: agent.ToolExecutionOutcomeOK,
	})
	if calls != 1 || gotName != "bash" || gotTask != "conv-progress" {
		t.Fatalf("calls=%d name=%s task=%s", calls, gotName, gotTask)
	}
	if out.Result != "listed the page" {
		t.Fatalf("tool result changed: %s", out.Result)
	}
	if cb.connectorAsk.Text != "" {
		t.Fatalf("progress wrote an ask: %+v", cb.connectorAsk)
	}

	host := &coreAgentCallbacks{session: Session{ID: "host"}}
	host.finishBotToolResult("bash", `{}`, agent.ToolExecutionResult{
		Result:  "host tool",
		Outcome: agent.ToolExecutionOutcomeOK,
	})
	if calls != 1 {
		t.Fatalf("a host turn wrote progress, calls=%d", calls)
	}

	desktopbot.ResetProgressCopies()
	t.Cleanup(desktopbot.ResetProgressCopies)
	path := desktopbot.TaskProgressPath("conv-progress")
	desktopbot.RememberProgress("conv-progress", desktopbot.Sections{
		WhereWeAre: "正在把登录后的第一屏存下来。", NextStep: "打开设置里的通知页。", OpenProblems: "无",
	}, path)
	meta := map[string]string{}
	cb.mergeProgressCopy(meta)
	if meta["send_to_user"] != "" || meta["progress_next"] != "打开设置里的通知页。" || meta["progress_path"] != path {
		t.Fatalf("run copy = %#v", meta)
	}
	if summary := desktopbot.ProgressSummary(meta["progress_path"]); summary != path || len(summary) != len(path) {
		t.Fatalf("summary = %q", summary)
	}
	view := desktopbot.Visible(desktopbot.Turn{SendToUser: meta["send_to_user"], Prose: meta["progress_where"]})
	if view.Bubble {
		t.Fatal("the run copy opened a progress bubble")
	}
}
