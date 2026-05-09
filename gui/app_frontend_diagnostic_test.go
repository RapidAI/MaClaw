package main

import (
	"strings"
	"testing"
)

func TestSanitizeFrontendDiagnosticPayloadDropsContentFields(t *testing.T) {
	payload := map[string]interface{}{
		"tag":             "ai-role-prefix",
		"messageId":       "msg-1",
		"beforeSnippet":   "Browser: sensitive generated text",
		"afterContent":    "sensitive generated text",
		"rawPrompt":       "make a confidential deck",
		"beforeText":      "Browser: sensitive generated text",
		"textLen":         32,
		"contextID":       "ctx-1",
		"beforeLen":       32,
		"responseSource":  "file_delivery",
		"longStringField": strings.Repeat("x", 300),
	}

	got := sanitizeFrontendDiagnosticPayload(payload)
	if _, ok := got["beforeSnippet"]; ok {
		t.Fatal("beforeSnippet should be dropped")
	}
	if _, ok := got["afterContent"]; ok {
		t.Fatal("afterContent should be dropped")
	}
	if _, ok := got["rawPrompt"]; ok {
		t.Fatal("rawPrompt should be dropped")
	}
	if _, ok := got["beforeText"]; ok {
		t.Fatal("beforeText should be dropped")
	}
	if got["textLen"] != 32 {
		t.Fatalf("textLen = %#v, want 32", got["textLen"])
	}
	if got["contextID"] != "ctx-1" {
		t.Fatalf("contextID = %#v, want ctx-1", got["contextID"])
	}
	if got["messageId"] != "msg-1" {
		t.Fatalf("messageId = %#v, want msg-1", got["messageId"])
	}
	if got["beforeLen"] != 32 {
		t.Fatalf("beforeLen = %#v, want 32", got["beforeLen"])
	}
	if value, _ := got["longStringField"].(string); len([]rune(value)) <= 240 || !strings.HasSuffix(value, "...[truncated]") {
		t.Fatalf("long string was not truncated safely: len=%d value=%q", len([]rune(value)), value)
	}
}
