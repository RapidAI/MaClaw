package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestResumeHooksGiveTheTurnAFileSlot(t *testing.T) {
	prep, finish := desktopResumeExecutionHooks(
		agentservice.Principal{TenantID: "tenant-a", UserID: "svc"},
		agentservice.Instance{ID: "inst_resume", Metadata: map[string]string{
			"hub_bot": "1", "hub_user_id": "alice", "hub_tenant_id": "tenant-a", "hub_desktop_bot": "bot_1",
		}},
		"运行后截屏发给我",
	)
	ctx := prep(context.Background())
	png := desktopTestPNG(t, 2, 2, false)
	if err := noteDesktopFile(ctx, "snake.png", "image/png", png); err != nil {
		t.Fatal(err)
	}
	msg := &agentservice.Message{Content: "游戏在跑。"}
	images, files := 0, 0
	finish(ctx, &agentservice.Run{ID: "run_resume", Status: agentservice.RunStatusSucceeded}, msg, nil)
	if !strings.Contains(msg.Content, "游戏在跑") {
		t.Fatalf("content=%q", msg.Content)
	}
	for _, a := range msg.Attachments {
		if a.FileName == "snake.png" {
			files++
		}
		if strings.HasPrefix(a.MimeType, "image/") && a.Type == "image" {
			images++
		}
	}
	if files != 1 || images != 0 {
		t.Fatalf("files=%d images=%d attachments=%d", files, images, len(msg.Attachments))
	}
	user, tenant := rememberDesktopOwner("inst_resume", nil, "svc", "tenant-srv")
	if user != desktop.BotDesktopUser("alice", "bot_1") || tenant != "tenant-a" {
		t.Fatalf("desktop user=%q tenant=%q", user, tenant)
	}
}
