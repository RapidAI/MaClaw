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
	if !strings.Contains(got, "2026-08-20") || strings.Contains(got, "日期：") || !strings.Contains(got, `"content"`) {
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
	if PDFLooksLikeTitleOnly("Bring an umbrella.") {
		t.Error("an English sentence was treated as a title")
	}
	if !PDFLooksLikeTitleOnly("report.pdf") {
		t.Error("a dotted token was treated as report content")
	}
	if PDFLooksLikeTitleOnly("先看天气。再出门") {
		t.Error("a sentence break without a space was treated as a title")
	}
}

func TestPDFReportDateString(t *testing.T) {
	for _, input := range []string{"2026-08-20", "2026/08/20", "2026.08.20"} {
		if got := PDFReportDateString(input); got != input {
			t.Errorf("PDFReportDateString(%q) = %q", input, got)
		}
	}
	for _, input := range []string{"南京天气", "2026年8月20日", "", strings.Repeat("2", 33)} {
		if got := PDFReportDateString(input); got != "" {
			t.Errorf("invalid date %q returned %q", input, got)
		}
	}
}

func TestOmitHostPDFToolStatus(t *testing.T) {
	input := "天气数据如下。\n当前轮次未授权 `generate_pdf` 工具，无法生成PDF。天气数据已获取：南京小雨31℃。"
	got := OmitHostPDFToolStatus(input)
	if strings.Contains(got, "generate_pdf") || strings.Contains(got, "未授权") {
		t.Fatalf("tool-status sentence survived: %q", got)
	}
	if !strings.Contains(got, "天气数据如下") || !strings.Contains(got, "南京小雨31℃") {
		t.Fatalf("report sentences were dropped: %q", got)
	}
	if got := OmitHostPDFToolStatus("I will generate a PDF, please wait.\n正文保留"); !strings.Contains(got, "please wait") || !strings.Contains(got, "正文保留") {
		t.Fatalf("a promise that does not cite the tool id must stay: %q", got)
	}
	if got := OmitHostPDFToolStatus("午后大风无法直接生成有效对流。"); got != "午后大风无法直接生成有效对流。" {
		t.Fatalf("report sentence without the tool id was dropped: %q", got)
	}
	if got := OmitHostPDFToolStatus("模型返回了无法解析的工具调用，已拦截原始工具 XML。请重试，或切换更兼容 OpenAI tool_calls 的模型。"); got != "" {
		t.Fatalf("host malformed-tool notice survived: %q", got)
	}
}

func TestProjectHostPublishedPDFChat(t *testing.T) {
	transcript := "今天多云，26℃。\n接下来我将生成 PDF，请稍候。"
	got := ProjectHostPublishedPDFChat(transcript)
	if !strings.Contains(got, "26℃") || !strings.Contains(got, "请稍候") {
		t.Fatalf("the answer was rewritten: %q", got)
	}
	summary := "崇州天气 PDF 已生成并发送给你了。\n\n今日（10/03 周六）：多云转小雨，25/16°C，微风，湿度约79%，外出带伞。\n\n未来趋势：4日小雨 23/16°C，5日阴转多云 21/14°C。"
	got = ProjectHostPublishedPDFChat(summary)
	if got != summary {
		t.Fatalf("a finished forecast was rewritten: %q", got)
	}
	cited := "彭州今日多云，22到29度。\n当前没有 generate_pdf 授权。"
	got = ProjectHostPublishedPDFChat(cited)
	if strings.Contains(got, "generate_pdf") || !strings.Contains(got, "彭州") {
		t.Fatalf("tool-status sentence leaked into the report reply: %q", got)
	}
	if got := ProjectHostPublishedPDFChat("当前没有 generate_pdf 授权。"); got != "" {
		t.Fatalf("a tool-status-only reply must fall through to the receipt: %q", got)
	}
}
