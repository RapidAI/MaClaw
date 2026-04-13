package main

import (
	"strings"
	"testing"
)

func TestClassifyTaskIntent_Coding(t *testing.T) {
	result := classifyTaskIntent("帮我修复这个 Go 项目的 bug 并修改代码")
	if result.Intent != intentCoding {
		t.Fatalf("expected coding intent, got %q with evidence %#v", result.Intent, result.Evidence)
	}
	if result.Source != "rules" {
		t.Fatalf("expected rules source, got %q", result.Source)
	}
}

func TestClassifyTaskIntent_SSH(t *testing.T) {
	result := classifyTaskIntent("ssh 到 10.0.0.8 看 nginx 日志")
	if result.Intent != intentSSH {
		t.Fatalf("expected ssh intent, got %q with evidence %#v", result.Intent, result.Evidence)
	}
}

func TestClassifyTaskIntent_NonCoding(t *testing.T) {
	result := classifyTaskIntent("帮我翻译这篇论文")
	if result.Intent != intentNonCoding {
		t.Fatalf("expected non-coding intent, got %q with evidence %#v", result.Intent, result.Evidence)
	}
}

func TestClassifyTaskIntent_Ambiguous(t *testing.T) {
	result := classifyTaskIntent("帮我处理一下线上问题")
	if result.Intent != intentAmbiguous {
		t.Fatalf("expected ambiguous intent, got %q with evidence %#v", result.Intent, result.Evidence)
	}
}

func TestClassifyTaskIntent_NonCodingKnowledgeBaseReport(t *testing.T) {
	result := classifyTaskIntent("现在帮我把桌面上的 AI 编程评测报告放入知识库")
	if result.Intent != intentNonCoding {
		t.Fatalf("expected non-coding intent, got %q with evidence %#v", result.Intent, result.Evidence)
	}
}

func TestClassifyTaskIntent_PresentationTasks(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		evidence string
	}{
		{name: "promo ppt", text: "生成宣传PPT", evidence: "ppt"},
		{name: "product intro ppt", text: "帮我做一个产品介绍PPT", evidence: "ppt"},
		{name: "presentation doc", text: "把这份内容整理成演示文稿", evidence: "演示文稿"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntent(tt.text)
			if result.Intent != intentNonCoding {
				t.Fatalf("expected non-coding intent, got %q with evidence %#v", result.Intent, result.Evidence)
			}
			if !strings.Contains(formatIntentEvidence(result), tt.evidence) {
				t.Fatalf("expected evidence to mention %q, got %q", tt.evidence, formatIntentEvidence(result))
			}
		})
	}
}

func TestClassifyTaskIntent_PresentationCodingTasksDoNotBecomeNonCoding(t *testing.T) {
	tests := []string{
		"写代码生成 PPTX 文件",
		"修复 PPT 导出 bug",
	}
	for _, text := range tests {
		result := classifyTaskIntent(text)
		if result.Intent == intentNonCoding {
			t.Fatalf("expected coding-related task %q to avoid non-coding classification, got %q with evidence %#v", text, result.Intent, result.Evidence)
		}
		if !strings.Contains(formatIntentEvidence(result), "ppt") {
			t.Fatalf("expected evidence to mention ppt for %q, got %q", text, formatIntentEvidence(result))
		}
	}
}

func TestClassifyTaskIntent_WeakCodingEvidenceAloneIsNonCoding(t *testing.T) {
	// File names containing "测试" should not trigger coding classification
	// when there is no other coding evidence.
	tests := []struct {
		name string
		text string
	}{
		{name: "xmind test case file", text: "参考: 微补丁丁终端功能测试用例_完整版.xmind，这个格式是正常的"},
		{name: "test case document", text: "微补丁丁终端功能测试用例_v4.xmind 文件打开失败"},
		{name: "bare test keyword", text: "这个测试文件打不开"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntent(tt.text)
			if result.Intent == intentCoding {
				t.Fatalf("weak coding evidence %q should not classify as coding, got %q with evidence %#v", tt.text, result.Intent, result.Evidence)
			}
		})
	}
}

func TestClassifyTaskIntent_StrongCodingEvidenceStillWorks(t *testing.T) {
	// "测试" combined with strong coding keywords should still be coding.
	tests := []struct {
		name string
		text string
	}{
		{name: "write unit test", text: "帮我写单元测试"},
		{name: "fix test bug", text: "修复测试中的 bug"},
		{name: "run test", text: "帮我跑一下这个项目的测试并修改代码"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyTaskIntent(tt.text)
			if result.Intent == intentNonCoding {
				t.Fatalf("strong coding task %q should not be non-coding, got %q with evidence %#v", tt.text, result.Intent, result.Evidence)
			}
		})
	}
}

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
