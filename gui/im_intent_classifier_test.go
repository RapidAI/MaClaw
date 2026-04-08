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
