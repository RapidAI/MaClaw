package codingagent

import (
	"strings"
	"testing"
)

func TestStripFinalAnswerAuditSectionsDropsInternalAuditAndKeepsSemanticSections(t *testing.T) {
	input := "已完成修复。\n\n" +
		"## 质量审计\n" +
		"PASS files=1\n\n" +
		"## 修复的问题\n" +
		"修复关闭时的空指针。\n\n" +
		"## 验证\n" +
		"`go test ./pkg` 通过"
	got := StripFinalAnswerAuditSections(input)
	if strings.Contains(got, "质量审计") || strings.Contains(got, "PASS files=") {
		t.Fatalf("internal audit leaked: %q", got)
	}
	for _, want := range []string{"## 修复的问题", "空指针", "## 验证", "go test ./pkg"} {
		if !strings.Contains(got, want) {
			t.Fatalf("semantic content %q missing: %q", want, got)
		}
	}
}

func TestStripFinalAnswerAuditSectionsDropsFileLedgerAndNormalizesVerificationAlias(t *testing.T) {
	input := "已完成优化。\n\n## 涉及文件\nfoo.go\nbar.go\n\n## 验证结果\n`go test ./...` 通过"
	got := StripFinalAnswerAuditSections(input)
	if strings.Contains(got, "涉及文件") || strings.Contains(got, "bar.go") {
		t.Fatalf("bare file ledger leaked: %q", got)
	}
	if !strings.Contains(got, "## 验证") || strings.Contains(got, "验证结果") || !strings.Contains(got, "go test ./...") {
		t.Fatalf("verification alias was not normalized: %q", got)
	}
}

func TestStripFinalAnswerAuditSectionsDoesNotDropInlineExecutionStepsMention(t *testing.T) {
	input := "已完成修复。正文提到后续执行步骤：只保留语义说明。\n\n## 验证\n`go test ./pkg` 通过"
	got := StripFinalAnswerAuditSections(input)
	if !strings.Contains(got, "正文提到后续执行步骤") || !strings.Contains(got, "## 验证") {
		t.Fatalf("inline execution-steps mention was treated as plan board chrome: %q", got)
	}
}

func TestStripFinalAnswerAuditSectionsKeepsEnglishBoldSemanticHeading(t *testing.T) {
	input := "Implemented the fix.\n\n" +
		"**Quality audit**\n" +
		"PASS\n\n" +
		"**Fixed issues:**\n" +
		"Fixed the close-time nil dereference.\n\n" +
		"**Verification**:\n" +
		"`go test ./pkg` passed."
	got := StripFinalAnswerAuditSections(input)
	if strings.Contains(got, "Quality audit") || strings.Contains(got, "PASS") {
		t.Fatalf("english internal audit leaked: %q", got)
	}
	if !strings.Contains(got, "## Fixed issues") || !strings.Contains(got, "nil dereference") || !strings.Contains(got, "## Verification") {
		t.Fatalf("english bold semantic section was swallowed: %q", got)
	}
}
