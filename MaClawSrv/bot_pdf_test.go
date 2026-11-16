package main

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func pdfBotTurn(mode, phase string) agentruntime.TurnRequest {
	return agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "inst", RunID: "run"},
		Input: agentservice.ExecuteRequest{
			Instance: agentservice.Instance{Metadata: map[string]string{"hub_bot": "1", "hub_bot_desktop": mode}},
			Message:  agentservice.Message{Metadata: map[string]string{"bot_phase_decided": "1", "bot_phase": phase}},
		},
	}
}

func TestBotPDFWorksWithAndWithoutDesktopAndAttachesToTurn(t *testing.T) {
	pdf := []byte("%PDF-1.4\nrendered document")
	mod := botPDFModule{render: func(content string) ([]byte, error) {
		if content != "# 北京天气\n晴" {
			t.Fatalf("rendered content = %q", content)
		}
		return pdf, nil
	}}
	for _, mode := range []string{"none", "isolated"} {
		t.Run(mode, func(t *testing.T) {
			request := pdfBotTurn(mode, "execute")
			tools, err := mod.Tools(context.Background(), request)
			if err != nil || len(tools) != 1 || tools[0].Name != botPDFToolName {
				t.Fatalf("PDF tools = %v, %v", tools, err)
			}
			if tools[0].ChangesState == nil || !tools[0].ChangesState(nil) {
				t.Fatal("PDF delivery was classified as read-only")
			}
			ctx, slot := withDesktopShotSlot(context.Background())
			out, err := mod.InvokeTool(ctx, request, botPDFToolName, map[string]any{"title": "北京天气", "content": "# 北京天气\n晴"})
			if err != nil || !strings.Contains(out, "北京天气.pdf") {
				t.Fatalf("PDF result = %q, %v", out, err)
			}
			msg := &agentservice.Message{}
			if got := attachDesktopFile(msg, slot); got != 1 || len(msg.Attachments) != 1 {
				t.Fatalf("attached = %d, %#v", got, msg.Attachments)
			}
			file := msg.Attachments[0]
			raw, err := base64.StdEncoding.DecodeString(file.Data)
			if err != nil || file.FileName != "北京天气.pdf" || file.MimeType != "application/pdf" || string(raw) != string(pdf) {
				t.Fatalf("file = %#v, decode = %v", file, err)
			}
		})
	}
}

func TestBotPDFDoesNotAdvertiseToOrdinaryAgentOrRunDuringPlan(t *testing.T) {
	mod := botPDFModule{render: func(string) ([]byte, error) { t.Fatal("rendered a plan"); return nil, nil }}
	if tools, _ := mod.Tools(context.Background(), agentruntime.TurnRequest{}); len(tools) != 0 {
		t.Fatalf("ordinary agent PDF tools = %v", tools)
	}
	ctx, slot := withDesktopShotSlot(context.Background())
	_, err := mod.InvokeTool(ctx, pdfBotTurn("none", "plan"), botPDFToolName, map[string]any{"content": "draft"})
	if err == nil || !strings.Contains(err.Error(), "execute") {
		t.Fatalf("plan PDF error = %v", err)
	}
	if got := attachDesktopFile(&agentservice.Message{}, slot); got != 0 {
		t.Fatalf("plan attached %d files", got)
	}
	_, err = mod.InvokeTool(ctx, pdfBotTurn("none", "execute"), botPDFToolName, map[string]any{"title": "../private", "content": "draft"})
	if err == nil {
		t.Fatal("accepted unsafe filename")
	}
}

func TestNoDesktopAsyncFinishKeepsGeneratedPDF(t *testing.T) {
	mod := botPDFModule{render: func(string) ([]byte, error) {
		return []byte("%PDF-1.4\nasync"), nil
	}}
	prepare, finish := desktopAsyncHooks("t", "u", "inst-pdf", "bot_pdf", nil, true)
	ctx := prepare(context.Background())
	request := pdfBotTurn("none", "execute")
	request.Scope.InstanceID = "inst-pdf"
	request.Scope.RunID = "run-pdf-async"
	if _, err := mod.InvokeTool(ctx, request, botPDFToolName, map[string]any{"content": "report"}); err != nil {
		t.Fatal(err)
	}
	run := &agentservice.Run{ID: request.Scope.RunID, Status: agentservice.RunStatusSucceeded}
	msg := &agentservice.Message{Content: "PDF 已附上", Metadata: map[string]string{
		"task_outcome": "done", "send_to_user": "PDF 已附上",
	}}
	finish(ctx, run, msg, nil)
	t.Cleanup(func() { desktopTurnResults.Delete(run.ID) })
	result, ok := loadDesktopTurnResult(run.ID)
	if !ok || result.Message == nil || len(result.Message.Attachments) != 1 ||
		result.Message.Attachments[0].FileName != "文档.pdf" {
		t.Fatalf("PDF lost at async finish: %#v, found=%v", result, ok)
	}
}
