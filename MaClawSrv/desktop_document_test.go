package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDeliverAttachesADocxWithoutTouchingTheDesktop(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-doc", UserID: "alice", InstanceID: "doc-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	text, err := (desktopRuntimeModule{}).InvokeTool(ctx, turn, "desktop", map[string]any{
		"action":  "deliver",
		"name":    "自我描述.docx",
		"content": "我是数字伙伴。\n下一行 <ok> & done",
	})
	if err != nil || !strings.Contains(text, "自我描述.docx") || !strings.Contains(text, "attached") {
		t.Fatalf("text=%q err=%v", text, err)
	}
	msg := &agentservice.Message{}
	if n := attachDesktopFile(msg, slot); n != 1 || len(msg.Attachments) != 1 {
		t.Fatalf("attached=%d %#v", n, msg.Attachments)
	}
	file := msg.Attachments[0]
	if file.Type != "file" || file.FileName != "自我描述.docx" || file.MimeType != desktopDocxMIME {
		t.Fatalf("file=%#v", file)
	}
	raw, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var document string
	for _, item := range reader.File {
		if item.Name != "word/document.xml" {
			continue
		}
		rc, openErr := item.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(rc)
		_ = rc.Close()
		document = buf.String()
	}
	if !strings.Contains(document, "我是数字伙伴。") || !strings.Contains(document, "下一行 &lt;ok&gt; &amp; done") {
		t.Fatalf("document=%s", document)
	}
	if n := attachDesktopFile(msg, slot); n != 0 || len(msg.Attachments) != 1 {
		t.Fatalf("second attach=%d %#v", n, msg.Attachments)
	}
}

func TestDeliverRejectsAPathAndAnEmptyBody(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	turn := agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	for _, args := range []map[string]any{
		{"action": "deliver", "name": "../secret.docx", "content": "hi"},
		{"action": "deliver", "name": ".secret.txt", "content": "hi"},
		{"action": "deliver", "name": "note.docx", "content": "  "},
	} {
		_, err := (desktopRuntimeModule{}).InvokeTool(ctx, turn, "desktop", args)
		if err == nil {
			t.Fatalf("accepted %#v", args)
		}
	}
	if n := attachDesktopFile(&agentservice.Message{}, slot); n != 0 {
		t.Fatalf("rejected deliver still attached %d", n)
	}
}

func TestDeliverSendsTextUnderTheGivenName(t *testing.T) {
	ctx, slot := withDesktopShotSlot(context.Background())
	turn := agentruntime.TurnRequest{
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	_, err := (desktopRuntimeModule{}).InvokeTool(ctx, turn, "desktop", map[string]any{
		"action":  "deliver",
		"name":    "note.pdf",
		"content": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := &agentservice.Message{}
	if n := attachDesktopFile(msg, slot); n != 1 {
		t.Fatalf("attached=%d", n)
	}
	file := msg.Attachments[0]
	raw, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName != "note.pdf" || file.MimeType != "application/pdf" || string(raw) != "hi" {
		t.Fatalf("file=%#v body=%q", file, raw)
	}
	if _, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw))); err == nil {
		t.Fatal("text was wrapped as a docx")
	}
}

func TestDeliverReadsTheDesktopFile(t *testing.T) {
	raw := append([]byte("%PDF-1.4\n"), 0xff, 0xfe)
	encoded := base64.StdEncoding.EncodeToString(raw)
	previous := desktopRemoteFile
	var gotAction, gotPath string
	var calls int
	desktopRemoteFile = func(_ context.Context, _, _, action, filePath, _, _, _ string) (string, error) {
		calls++
		gotAction, gotPath = action, filePath
		return encoded, nil
	}
	t.Cleanup(func() { desktopRemoteFile = previous })
	ctx, slot := withDesktopShotSlot(context.Background())
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "tenant-doc", UserID: "alice", InstanceID: "doc-bot"},
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	mod := desktopRuntimeModule{}
	text, err := mod.InvokeTool(ctx, turn, "desktop", map[string]any{
		"action": "deliver",
		"path":   "~/Desktop/北京天气.pdf",
	})
	if err != nil || !strings.Contains(text, "北京天气.pdf") || gotAction != "bytes" || gotPath != "/home/desktop/Desktop/北京天气.pdf" || calls != 1 {
		t.Fatalf("text=%q action=%s path=%q calls=%d err=%v", text, gotAction, gotPath, calls, err)
	}
	msg := &agentservice.Message{}
	if n := attachDesktopFile(msg, slot); n != 1 {
		t.Fatalf("attached=%d", n)
	}
	file := msg.Attachments[0]
	got, err := base64.StdEncoding.DecodeString(file.Data)
	if err != nil {
		t.Fatal(err)
	}
	if file.FileName != "北京天气.pdf" || file.MimeType != "application/pdf" || string(got) != string(raw) {
		t.Fatalf("file=%#v body=%q", file, got)
	}

	ctx, slot = withDesktopShotSlot(context.Background())
	calls = 0
	_, err = mod.InvokeTool(ctx, turn, "desktop", map[string]any{
		"action": "deliver",
		"path":   `C:\Windows\a.c`,
		"name":   "note.pdf",
	})
	if err == nil || calls != 0 {
		t.Fatalf("host path err=%v calls=%d", err, calls)
	}
	if n := attachDesktopFile(&agentservice.Message{}, slot); n != 0 {
		t.Fatalf("host path attached %d", n)
	}

	calls = 0
	_, err = mod.InvokeTool(ctx, turn, "desktop", map[string]any{
		"action": "deliver",
		"path":   "~/Desktop/北京天气.pdf",
		"name":   "../secret.pdf",
	})
	if err == nil || calls != 0 {
		t.Fatalf("bad name err=%v calls=%d", err, calls)
	}

	desktopRemoteFile = func(_ context.Context, _, _, _, _, _, _, _ string) (string, error) {
		return base64.StdEncoding.EncodeToString(make([]byte, desktop.FileBytesMax+1)), nil
	}
	ctx, slot = withDesktopShotSlot(context.Background())
	_, err = mod.InvokeTool(ctx, turn, "desktop", map[string]any{
		"action": "deliver",
		"path":   "~/Desktop/北京天气.pdf",
	})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("large err=%v", err)
	}
	if n := attachDesktopFile(&agentservice.Message{}, slot); n != 0 {
		t.Fatalf("large file attached %d", n)
	}
}
