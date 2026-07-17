package main

import (
	"strings"
	"testing"
)

func TestDesktopWorkflowDocOverrideScopesToWorkflowDocs(t *testing.T) {
	s := desktopWorkflowDocOverride()
	for _, needle := range []string{
		"仅工作流阶段文档",
		"需求文档",
		"不要使用 send_file 发送上述工作流阶段文档",
		"会议/长时录音后处理",
		"write_file",
		"generate_pdf",
		"send_file",
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("desktop override missing %q:\n%s", needle, s)
		}
	}
	// Old blanket ban on all document send_file should not remain as the sole rule.
	if strings.Contains(s, "不要使用 send_file 发送文档——") {
		t.Fatalf("legacy blanket send_file ban still present:\n%s", s)
	}
}
