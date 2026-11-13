package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestChinesePNGStaysOnTheReply(t *testing.T) {
	raw := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 378*1024/4)
	encoded := base64.StdEncoding.EncodedLen(len(raw))
	if encoded <= 200_000 || encoded > desktop.FileDeliverBase64Max {
		t.Fatalf("fixture encoded %d is not the 378KB case", encoded)
	}
	if desktopHubReplyFileMax != desktop.FileDeliverBase64Max || !desktopDeliverOnReply(raw) {
		t.Fatal("the reply cap is not the shared file cap")
	}
	var said int
	prev := desktopDeliverLargeSay
	desktopDeliverLargeSay = func(context.Context, map[string]any) error {
		said++
		return nil
	}
	t.Cleanup(func() { desktopDeliverLargeSay = prev })

	ctx, slot := withDesktopShotSlot(context.Background())
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot", InstanceID: "inst", RunID: "run_png"}
	out, err := deliverDesktopBytes(ctx, scope, "训练曲线.png", "image/png", raw)
	if err != nil || !strings.Contains(out, "this chat reply") {
		t.Fatalf("mid-run deliver %q %v", out, err)
	}
	if said != 0 {
		t.Fatal("mid-run deliver pushed the png through say")
	}
	msg := &agentservice.Message{Content: "图在这条消息里"}
	_, files := publishDesktopReply(msg, slot)
	if files != 1 || len(msg.Attachments) != 1 {
		t.Fatalf("final reply files=%d attachments=%d", files, len(msg.Attachments))
	}
	att := msg.Attachments[0]
	if att.Type != "image" || att.MimeType != "image/png" || att.FileName != "训练曲线.png" || len(att.Data) != encoded {
		t.Fatalf("attachment %+v len=%d", att.FileName, len(att.Data))
	}
}
