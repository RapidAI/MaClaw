package agentruntime

import (
	"strings"
	"testing"
)

func TestNormalizePDFInvocationArgs(t *testing.T) {
	got := NormalizePDFInvocationArgs(`{"content":"# 南京\n18C","date":"2026-08-20","query":"南京天气","path":"out.pdf"}`)
	if strings.Contains(got, `"query"`) || strings.Contains(got, `"path"`) {
		t.Fatalf("decorative fields leaked: %s", got)
	}
	if !strings.Contains(got, "日期：2026-08-20") || !strings.Contains(got, `"content"`) {
		t.Fatalf("date/content normalization missing: %s", got)
	}
	unchanged := `{"content":"南京天气报告","date":"2026-08-20"}`
	if got := NormalizePDFInvocationArgs(unchanged); got != unchanged {
		t.Fatalf("title-only payload should remain unchanged: %s", got)
	}
	malformed := `{"content":true,"title":"南京天气"}`
	if got := NormalizePDFInvocationArgs(malformed); got != malformed {
		t.Fatalf("malformed content should remain unchanged: %s", got)
	}
}

func TestPDFArgsTooThin(t *testing.T) {
	for _, input := range []string{
		`{"content":"南京天气报告"}`,
		`{"content":"# Weekly Status\n","title":"Weekly Status"}`,
		`{"content":"Weekly Status","title":"Weekly Status"}`,
	} {
		if !PDFArgsTooThin(input) {
			t.Errorf("PDFArgsTooThin(%s) = false", input)
		}
	}
	if PDFArgsTooThin(`{"content":"# 南京天气\n\n小雨31℃"}`) {
		t.Error("substantive report was classified as title-only")
	}
}

func TestPDFReportDateString(t *testing.T) {
	for _, input := range []string{"2026-08-20", "2026年8月20日", "2026/08/20"} {
		if got := PDFReportDateString(input); got != input {
			t.Errorf("PDFReportDateString(%q) = %q", input, got)
		}
	}
	for _, input := range []string{"南京天气", "", strings.Repeat("2", 33)} {
		if got := PDFReportDateString(input); got != "" {
			t.Errorf("invalid date %q returned %q", input, got)
		}
	}
}

func TestStripDeferredPDFPromise(t *testing.T) {
	input := "天气数据如下。\n请稍候，我将生成 PDF 报告。\nPDF 生成失败，工具列表中没有 PDF 生成工具。"
	if got := StripDeferredPDFPromise(input); got != "天气数据如下。" {
		t.Fatalf("deferred/failure text was not removed: %q", got)
	}
	if got := StripDeferredPDFPromise("I will generate a PDF, please wait.\n正文保留"); got != "正文保留" {
		t.Fatalf("English deferred line was not removed: %q", got)
	}
	if got := StripDeferredPDFPromise("请稍候\n有用的正文"); got != "有用的正文" {
		t.Fatalf("wait-only line was not removed: %q", got)
	}
}

func TestFailedPDFAuthorizationExcuseLine(t *testing.T) {
	if !FailedPDFAuthorizationExcuseLine("无法直接生成 PDF，请重新授权工具") {
		t.Fatal("expected authorization excuse to be classified")
	}
	if FailedPDFAuthorizationExcuseLine("PDF 报告包含 25℃ 的天气数据。") {
		t.Fatal("substantive PDF content was misclassified as an excuse")
	}
}
