package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
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
	n, err := autoDeliverReplyFiles(ctx, scope, msg)
	if err != nil || n != 1 || len(msg.Attachments) != 1 || msg.Attachments[0].FileName != "snake.cpp" {
		t.Fatalf("n=%d err=%v attachments=%v asked=%v", n, err, msg.Attachments, asked)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "bytes ") {
		t.Fatalf("asked %v", asked)
	}
	again, againErr := autoDeliverReplyFiles(ctx, scope, msg)
	if againErr != nil || again != 0 || len(msg.Attachments) != 1 {
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
	if n, err := autoDeliverReplyFiles(ctx, scope, msg); err != nil || n != 0 || len(msg.Attachments) != 0 {
		t.Fatalf("n=%d err=%v attachments=%v", n, err, msg.Attachments)
	}
	noteDesktopOutput(ctx, "/tmp/secret")
	noteDesktopOutput(ctx, "/home/desktop/work/bot_a/.hidden/key")
	if n, err := autoDeliverReplyFiles(ctx, scope, msg); err != nil || n != 0 {
		t.Fatalf("refused paths were delivered n=%d err=%v", n, err)
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
	n, err := autoDeliverReplyFiles(ctx, scope, msg)
	if n != desktopReplyOutputMaxFiles || len(msg.Attachments) != desktopReplyOutputMaxFiles {
		t.Fatalf("n=%d attachments=%d err=%v", n, len(msg.Attachments), err)
	}
	var over *desktopReplyDeliveryError
	if !errors.As(err, &over) || over.Code != desktopOutputOverCount {
		t.Fatalf("extra outputs were dropped without a typed failure: %v", err)
	}
	if !strings.Contains(err.Error(), "f"+string(rune('a'+desktopReplyOutputMaxFiles))+".txt") || !strings.Contains(err.Error(), desktopOutputOverCount) {
		t.Fatalf("the first undelivered output is missing from %v", err)
	}
}

func TestReplyOutputPastTheInlineBudgetIsSentOnItsOwn(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "t", UserID: "alice::desk::bot_a", InstanceID: "inst", RunID: "run_big"}
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "over-200kb", raw: bytes.Repeat([]byte("A"), desktopReplyOutputMaxBytes+1)},
		{name: "base64-over-limit", raw: bytes.Repeat([]byte("B"), desktopHubReplyFileMax/4*3+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.raw) > desktopReplyOutputMaxBytes && tc.name == "base64-over-limit" {
				t.Fatal("base64 case must stay under the raw inline total so only the encoded length routes it out")
			}
			if base64.StdEncoding.EncodedLen(len(tc.raw)) <= desktopHubReplyFileMax && tc.name == "base64-over-limit" {
				t.Fatal("base64 case is still inside the reply limit")
			}
			prevFile, prevUp, prevSay := desktopRemoteFile, desktopSayUploadBytes, desktopDeliverLargeSay
			t.Cleanup(func() {
				desktopRemoteFile, desktopSayUploadBytes, desktopDeliverLargeSay = prevFile, prevUp, prevSay
			})
			desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
				return base64.StdEncoding.EncodeToString(tc.raw), nil
			}
			var uploaded int
			desktopSayUploadBytes = func(_ context.Context, _ agentruntime.Scope, name, _ string, raw []byte) (string, error) {
				if name != "out.bin" || len(raw) != len(tc.raw) {
					t.Fatalf("upload name=%s bytes=%d", name, len(raw))
				}
				uploaded = len(raw)
				return "bf_0123456789abcdef0123456", nil
			}
			var said bool
			desktopDeliverLargeSay = func(_ context.Context, body map[string]any) error {
				files, _ := body["files"].([]map[string]string)
				if len(files) != 1 || files[0]["ref"] != "bf_0123456789abcdef0123456" {
					t.Fatalf("say body %#v", body["files"])
				}
				said = true
				return nil
			}
			ctx, _ := withDesktopShotSlot(context.Background())
			noteDesktopOutput(ctx, "/home/desktop/work/bot_a/out.bin")
			msg := &agentservice.Message{Content: "文件已附在这条消息里。"}
			n, err := autoDeliverReplyFiles(ctx, scope, msg)
			if err != nil || n != 1 || len(msg.Attachments) != 0 || uploaded != len(tc.raw) || !said {
				t.Fatalf("n=%d err=%v attachments=%d uploaded=%d said=%v", n, err, len(msg.Attachments), uploaded, said)
			}
		})
	}
}

func TestReplyOutputUnreadableIsATypedFailure(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", InstanceID: "inst", RunID: "run_bad"}
	prev := desktopRemoteFile
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "", errors.New("permission denied")
	}
	t.Cleanup(func() { desktopRemoteFile = prev })
	ctx, slot := withDesktopShotSlot(context.Background())
	path := "/home/desktop/work/bot_a/missing.bin"
	noteDesktopOutput(ctx, path)
	msg := &agentservice.Message{Content: "结果在文件里。"}
	n, err := autoDeliverReplyFiles(ctx, scope, msg)
	var got *desktopReplyDeliveryError
	if n != 0 || len(msg.Attachments) != 0 || !errors.As(err, &got) || got.Code != desktopOutputUnreadable || got.Path != path {
		t.Fatalf("n=%d err=%v attachments=%d", n, err, len(msg.Attachments))
	}
	publishDesktopReplyIn(ctx, scope, msg, slot)
	if msg.Metadata["desktop_delivery_failures"] == "" || !strings.Contains(msg.Content, desktopOutputUnreadable) || !strings.Contains(msg.Content, path) {
		t.Fatalf("failure was not on the reply: content=%q meta=%v", msg.Content, msg.Metadata)
	}
}

func TestReplyOutputRefusedByTheDesktopUsesLargeDelivery(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", InstanceID: "inst", RunID: "run_store"}
	prevFile, prevBash, prevStore := desktopRemoteFile, desktopDeliverLargeBash, desktopDeliverViaStore
	t.Cleanup(func() {
		desktopRemoteFile, desktopDeliverLargeBash, desktopDeliverViaStore = prevFile, prevBash, prevStore
	})
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "", errors.New("file is too large")
	}
	desktopDeliverLargeBash = func(context.Context, agentruntime.Scope, string) (string, error) {
		return "MCLDL_STORE 300000\n", nil
	}
	desktopDeliverViaStore = func(context.Context, agentruntime.Scope, string, string, string) (string, error) {
		return "stored out.bin", nil
	}
	ctx, _ := withDesktopShotSlot(context.Background())
	noteDesktopOutput(ctx, "/home/desktop/work/bot_a/out.bin")
	msg := &agentservice.Message{}
	n, err := autoDeliverReplyFiles(ctx, scope, msg)
	if err != nil || n != 1 || len(msg.Attachments) != 0 {
		t.Fatalf("n=%d err=%v attachments=%d", n, err, len(msg.Attachments))
	}
}

func TestReplyOutputPastTheLargeCapIsATypedFailure(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice::desk::bot_a", InstanceID: "inst", RunID: "run_huge"}
	prevFile, prevBash := desktopRemoteFile, desktopDeliverLargeBash
	t.Cleanup(func() {
		desktopRemoteFile, desktopDeliverLargeBash = prevFile, prevBash
	})
	desktopRemoteFile = func(context.Context, string, string, string, string, string, string, string) (string, error) {
		return "", errors.New("file is too large")
	}
	desktopDeliverLargeBash = func(context.Context, agentruntime.Scope, string) (string, error) {
		return "MCLDL_BIG 9999999\n", nil
	}
	ctx, _ := withDesktopShotSlot(context.Background())
	path := "/home/desktop/work/bot_a/huge.bin"
	noteDesktopOutput(ctx, path)
	msg := &agentservice.Message{}
	n, err := autoDeliverReplyFiles(ctx, scope, msg)
	var got *desktopReplyDeliveryError
	if n != 0 || len(msg.Attachments) != 0 || !errors.As(err, &got) || got.Code != desktopOutputTooLarge || got.Path != path {
		t.Fatalf("n=%d err=%v attachments=%d", n, err, len(msg.Attachments))
	}
}
