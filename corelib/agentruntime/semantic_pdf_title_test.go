package agentruntime

import (
	"strings"
	"testing"
)

func TestHostOwnedPDFReportTitle(t *testing.T) {
	cases := map[string]string{
		"查询南京天气，并生成pdf报告":                         "查询南京天气",
		"杭州天气，请帮我生成PDF报告":                         "杭州天气",
		"生成pdf报告":                                 "报告",
		"Hangzhou weather, generate a PDF report": "Hangzhou weather",
	}
	for input, want := range cases {
		if got := HostOwnedPDFReportTitle(input); got != want {
			t.Errorf("HostOwnedPDFReportTitle(%q)=%q, want %q", input, got, want)
		}
	}
}

func TestSubstantialPDFReportText(t *testing.T) {
	if SubstantialPDFReportText("short") {
		t.Fatal("short acknowledgement must not be considered report content")
	}
	if !SubstantialPDFReportText(strings.Repeat("这是用于验证正文阈值的扩展文本。", 10)) {
		t.Fatal("substantial report text should pass the minimum threshold")
	}
}
