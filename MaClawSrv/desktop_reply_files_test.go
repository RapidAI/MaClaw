package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestReplyDeliversADeclaredOutput(t *testing.T) {
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
	noteDesktopOutput(ctx, work)
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

func TestReplyTextDoesNotDeliverAPath(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", RunID: "run_7c0236e7"}
	prev := desktopRemoteFile
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		t.Fatal("a path in the reply was fetched")
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteFile = prev })
	ctx, _ := withDesktopShotSlot(context.Background())
	msg := &agentservice.Message{Content: "看 ~/Desktop/shot.png 和 /tmp/secret"}
	if n := autoDeliverReplyFiles(ctx, scope, msg); n != 0 || len(msg.Attachments) != 0 {
		t.Fatalf("n=%d attachments=%v", n, msg.Attachments)
	}
	noteDesktopOutput(ctx, "/tmp/secret")
	noteDesktopOutput(ctx, "/home/desktop/work/bot_a/.hidden/key")
	if n := autoDeliverReplyFiles(ctx, scope, msg); n != 0 {
		t.Fatalf("refused paths were delivered n=%d", n)
	}
}

func TestReplyOutputDeliveryStopsAtTheCap(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", RunID: "run_cap"}
	prev := desktopRemoteFile
	desktopRemoteFile = func(_ context.Context, _, _, _, _, _, _, _ string) (string, error) {
		return base64.StdEncoding.EncodeToString([]byte("x")), nil
	}
	t.Cleanup(func() { desktopRemoteFile = prev })
	ctx, _ := withDesktopShotSlot(context.Background())
	for i := 0; i < desktopReplyOutputMaxFiles+2; i++ {
		noteDesktopOutput(ctx, "/home/desktop/work/bot_a/f"+string(rune('a'+i))+".txt")
	}
	msg := &agentservice.Message{}
	n := autoDeliverReplyFiles(ctx, scope, msg)
	if n != desktopReplyOutputMaxFiles || len(msg.Attachments) != desktopReplyOutputMaxFiles {
		t.Fatalf("n=%d attachments=%d", n, len(msg.Attachments))
	}
}
