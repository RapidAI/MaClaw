package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestLansengerMediaTypeForFileName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":  "file",
		"photo.PNG":   "image",
		"clip.mp4":    "video",
		"voice.ogg":   "file", // no native voice upload for bots
		"track.mp3":   "file", // audio goes through the generic file type
		"noextension": "file",
	}
	for name, want := range cases {
		if got := lansengerMediaTypeForFileName(name); got != want {
			t.Fatalf("%q -> %q, want %q", name, got, want)
		}
	}
}

func TestDeliverIMFileValidation(t *testing.T) {
	groupTarget := []scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindGroup, GroupID: "g1"}}

	if _, _, err := (&App{}).DeliverIMFile(context.Background(), "lansenger", nil, "x", "", ""); err == nil {
		t.Fatal("want error for empty targets")
	}
	if _, _, err := (&App{}).DeliverIMFile(context.Background(), "lansenger", groupTarget, "  ", "", ""); err == nil {
		t.Fatal("want error for empty path")
	}

	a := &App{}
	missing := filepath.Join(t.TempDir(), "missing.bin")
	if _, _, err := a.DeliverIMFile(context.Background(), "lansenger", groupTarget, missing, "", ""); err == nil ||
		!strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing file err = %v", err)
	}
	if _, _, err := a.DeliverIMFile(context.Background(), "lansenger", groupTarget, t.TempDir(), "", ""); err == nil ||
		!strings.Contains(err.Error(), "directory") {
		t.Fatalf("directory err = %v", err)
	}

	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Unsupported channels are rejected after the file validates, before any gateway work.
	if _, _, err := a.DeliverIMFile(context.Background(), "weixin",
		[]scheduler.DeliveryTarget{{Kind: scheduler.DeliveryKindUser, UserID: "self"}}, p, "", ""); err == nil ||
		!strings.Contains(err.Error(), "暂不支持") {
		t.Fatalf("unsupported channel err = %v", err)
	}

	// Empty files are rejected before any caption text can go out.
	empty := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.DeliverIMFile(context.Background(), "lansenger", groupTarget, empty, "", "说明"); err == nil ||
		!strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty file err = %v", err)
	}
}

func TestToolIMMessageSendFileArgValidation(t *testing.T) {
	var h *IMMessageHandler
	if out := h.toolIMMessageSendFile(nil); !strings.Contains(out, "未初始化") {
		t.Fatalf("nil handler: %q", out)
	}
	h = &IMMessageHandler{app: &App{}}
	if out := h.toolIMMessageSendFile(map[string]interface{}{"group_id": "g"}); !strings.Contains(out, "缺少 path") {
		t.Fatalf("missing path: %q", out)
	}
	if out := h.toolIMMessageSendFile(map[string]interface{}{"path": "a.pdf"}); !strings.Contains(out, "缺少投递目标") {
		t.Fatalf("missing target: %q", out)
	}
}
