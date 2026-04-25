package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// classifyTaskIntent without UIC — all return ambiguous (degraded mode)
// ---------------------------------------------------------------------------

// When UIC is nil, classifyTaskIntent returns ambiguous for all inputs.
// This is by design: keyword-based classification is unreliable and has been
// disabled in favor of UIC semantic classification.
func TestClassifyTaskIntent_WithoutUIC_ReturnsAmbiguous(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"coding task", "帮我修复这个 Go 项目的 bug 并修改代码"},
		{"ssh task", "ssh 到 10.0.0.8 看 nginx 日志"},
		{"non-coding task", "帮我翻译这篇论文"},
		{"ambiguous task", "帮我处理一下线上问题"},
		{"knowledge base", "现在帮我把桌面上的 AI 编程评测报告放入知识库"},
		{"promo ppt", "生成宣传PPT"},
		{"product intro ppt", "帮我做一个产品介绍PPT"},
		{"presentation doc", "把这份内容整理成演示文稿"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntent(tt.text)
			if result.Intent != intentAmbiguous {
				t.Fatalf("without UIC, expected ambiguous for %q, got %q", tt.text, result.Intent)
			}
			if result.Source != "rules-degraded" {
				t.Fatalf("expected rules-degraded source, got %q", result.Source)
			}
		})
	}
}

// Empty message still returns unknown (not ambiguous).
func TestClassifyTaskIntent_Empty(t *testing.T) {
	result := classifyTaskIntent("")
	if result.Intent != intentAmbiguous {
		t.Fatalf("expected ambiguous for empty text, got %q", result.Intent)
	}
}

// ---------------------------------------------------------------------------
// Tests that don't depend on keyword classification
// ---------------------------------------------------------------------------

func TestNormalizeIntentClassification(t *testing.T) {
	result, err := normalizeIntentClassification(llmIntentClassification{
		Intent:     "non_coding",
		Confidence: 0.92,
		Reason:     "用户是在整理资料并生成宣传 PPT，不涉及代码或服务器",
		Evidence:   []string{"整理资料", "宣传PPT"},
	})
	if err != nil {
		t.Fatalf("normalizeIntentClassification: %v", err)
	}
	if result.Intent != intentNonCoding {
		t.Fatalf("expected non_coding intent, got %q", result.Intent)
	}
	if result.Source != "llm" {
		t.Fatalf("expected llm source, got %q", result.Source)
	}
	if result.Confidence != 0.92 {
		t.Fatalf("unexpected confidence: %v", result.Confidence)
	}
	if result.Reason == "" {
		t.Fatal("expected reason to be preserved")
	}
}

func TestDecodeIntentClassificationContent_StripsCodeFence(t *testing.T) {
	parsed, err := decodeIntentClassificationContent("```json\n{\"intent\":\"coding\",\"confidence\":0.8,\"reason\":\"明确要修改代码\",\"evidence\":[\"修改代码\"]}\n```")
	if err != nil {
		t.Fatalf("decodeIntentClassificationContent: %v", err)
	}
	if parsed.Intent != "coding" {
		t.Fatalf("expected coding intent, got %q", parsed.Intent)
	}
}

func TestSummarizeAttachmentTypesAndNames(t *testing.T) {
	attachments := []MessageAttachment{
		{Type: "image", FileName: "shot.png"},
		{MimeType: "application/pdf", FileName: "brief.pdf"},
		{Type: "image", FileName: "shot2.png"},
	}
	types := summarizeAttachmentTypes(attachments)
	if len(types) != 2 || types[0] != "image" || types[1] != "application" {
		t.Fatalf("unexpected attachment types: %#v", types)
	}
	names := summarizeAttachmentNames(attachments)
	if len(names) != 3 || names[0] != "shot.png" {
		t.Fatalf("unexpected attachment names: %#v", names)
	}
}

// ---------------------------------------------------------------------------
// Confirmation gate test — presentation tasks without UIC
// ---------------------------------------------------------------------------

func TestConfirmationGate_PresentationTask_WithoutUIC(t *testing.T) {
	// Without UIC, classifyTaskIntent returns ambiguous for PPT tasks.
	// This means the confirmation gate WILL trigger (ambiguous → requires confirmation).
	// This is the conservative behavior we want.
	result := classifyTaskIntent("生成宣传PPT")
	if result.Intent != intentAmbiguous {
		t.Fatalf("expected ambiguous for PPT task without UIC, got %q", result.Intent)
	}
}

// ---------------------------------------------------------------------------
// Helper function tests (not dependent on keyword classification)
// ---------------------------------------------------------------------------

func TestHasOnlyWeakCodingEvidence(t *testing.T) {
	if !hasOnlyWeakCodingEvidence([]string{"代码"}) {
		t.Fatal("expected true for weak evidence '代码'")
	}
	if hasOnlyWeakCodingEvidence([]string{"开发"}) {
		t.Fatal("expected false for strong evidence '开发'")
	}
	if hasOnlyWeakCodingEvidence([]string{"代码", "开发"}) {
		t.Fatal("expected false for mixed evidence")
	}
}

func TestFormatIntentEvidence(t *testing.T) {
	r := taskIntentResult{Evidence: []string{"ssh", "服务器"}}
	formatted := formatIntentEvidence(r)
	if !strings.Contains(formatted, "ssh") {
		t.Fatalf("expected evidence to contain 'ssh', got %q", formatted)
	}
}
