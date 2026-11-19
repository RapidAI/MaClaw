package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
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
			out, err := mod.InvokeTool(ctx, request, botPDFToolName, map[string]any{"title": "北京天气", "content": "# 北京天气\n晴", "item_id": "file-1"})
			if err != nil || !strings.Contains(out, "北京天气.pdf") {
				t.Fatalf("PDF result = %q, %v", out, err)
			}
			msg := &agentservice.Message{}
			if got := attachDesktopFile(msg, slot); got != 1 || len(msg.Attachments) != 1 {
				t.Fatalf("attached = %d, %#v", got, msg.Attachments)
			}
			file := msg.Attachments[0]
			raw, err := base64.StdEncoding.DecodeString(file.Data)
			if err != nil || file.FileName != "北京天气.pdf" || file.MimeType != "application/pdf" || file.ItemID != "file-1" || string(raw) != string(pdf) {
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
	prepare, finish := desktopAsyncHooks(nil, "t", "u", "inst-pdf", "bot_pdf", nil, true)
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

// testFirstPageTextPDF builds a minimal PDF whose first page carries pageText
// ("" leaves it blank) and whose second page always has body text, so the
// first-page check is what decides.
func testFirstPageTextPDF(t *testing.T, pageText string) []byte {
	t.Helper()
	content := func(text string) string {
		if text == "" {
			return ""
		}
		return "BT /F1 12 Tf 72 720 Td (" + text + ") Tj ET"
	}
	first, second := content(pageText), content("body")
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [4 0 R 6 0 R] /Count 2 >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 5 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(first), first),
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents 7 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(second), second),
	}
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects)+1)
	for i, object := range objects {
		offsets[i+1] = out.Len()
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return out.Bytes()
}

func TestDesktopRefuseBlankPDF(t *testing.T) {
	text := testFirstPageTextPDF(t, "hello")
	if err := desktopRefuseBlankPDF("report.pdf", text); err != nil {
		t.Fatalf("text first page refused: %v", err)
	}
	blank := testFirstPageTextPDF(t, "")
	if err := desktopRefuseBlankPDF("cover.pdf", blank); err == nil || !strings.Contains(err.Error(), "blank_first_page") {
		t.Fatalf("blank first page not refused: %v", err)
	}
	// A non-PDF name and an unreadable body are not declared blank.
	if err := desktopRefuseBlankPDF("notes.txt", blank); err != nil {
		t.Fatalf("non-pdf name refused: %v", err)
	}
	if err := desktopRefuseBlankPDF("broken.pdf", []byte("not a pdf")); err != nil {
		t.Fatalf("unparseable pdf refused: %v", err)
	}
}
