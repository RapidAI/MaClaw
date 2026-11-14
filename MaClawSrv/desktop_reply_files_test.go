package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestReplyAutoDeliversANamedWorkFile(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", RunID: "run_7c0236e7"}
	work := "/home/desktop/work/bot_a/7c0236e7/snake.cpp"
	var asked []string
	prev := desktopRemoteFile
	desktopRemoteFile = func(_ context.Context, _, _, action, filePath, _, _, _ string) (string, error) {
		asked = append(asked, action+" "+filePath)
		if filePath != work {
			return "", context.Canceled
		}
		return base64.StdEncoding.EncodeToString([]byte("int main(){}")), nil
	}
	t.Cleanup(func() { desktopRemoteFile = prev })
	ctx, _ := withDesktopShotSlot(context.Background())
	msg := &agentservice.Message{Content: "蛇的代码在 " + work + " ，说明见 /Users/dan/notes.txt"}
	n := autoDeliverReplyFiles(ctx, scope, msg)
	if n != 1 || len(msg.Attachments) != 1 || msg.Attachments[0].FileName != "snake.cpp" {
		t.Fatalf("n=%d attachments=%v asked=%v", n, msg.Attachments, asked)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "bytes ") {
		t.Fatalf("asked %v", asked)
	}
	again := autoDeliverReplyFiles(ctx, scope, msg)
	if again != 0 || len(msg.Attachments) != 1 {
		t.Fatalf("delivered twice n=%d %+v", again, msg.Attachments)
	}
}

func TestReplyFileCandidatesStayInsideTheRun(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", RunID: "run_7c0236e7"}
	got := desktopReplyFileCandidates("看 ~/Desktop/shot.png 和 /tmp/secret 以及 https://example.com/p/1", scope)
	if len(got) != 1 || got[0] != "/home/desktop/Desktop/shot.png" {
		t.Fatalf("candidates %v", got)
	}
}
