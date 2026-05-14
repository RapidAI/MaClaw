package tool

import (
	"fmt"
	"strings"
	"testing"
)

func TestClassifyContent_HTML(t *testing.T) {
	cases := []struct {
		input string
		want  ContentType
	}{
		{`<html><body>hello</body></html>`, ContentHTML},
		{`<!DOCTYPE html><html>`, ContentHTML},
		{`<head><title>Test</title></head>`, ContentHTML},
		{`<div><p>text</p><p>more</p><a href="#">link</a><span>x</span><br/><hr/>`, ContentHTML},
	}
	for _, tc := range cases {
		got := ClassifyContent(tc.input)
		if got != tc.want {
			t.Errorf("ClassifyContent(%q) = %d, want %d", tc.input[:30], got, tc.want)
		}
	}
}

func TestClassifyContent_JSON(t *testing.T) {
	cases := []string{
		`{"key": "value", "num": 42}`,
		`[1, 2, 3, 4, 5]`,
		`{"nested": {"deep": [1,2,3]}}`,
	}
	for _, input := range cases {
		got := ClassifyContent(input)
		if got != ContentJSON {
			label := input
			if len(label) > 20 {
				label = label[:20]
			}
			t.Errorf("ClassifyContent(%q) = %d, want ContentJSON", label, got)
		}
	}
}

func TestClassifyContent_Terminal(t *testing.T) {
	input := "$ go build ./...\nerror: undefined reference\nwarning: unused variable\n$ echo done\n# comment\n> prompt"
	got := ClassifyContent(input)
	if got != ContentTerminal {
		t.Errorf("ClassifyContent(terminal) = %d, want ContentTerminal", got)
	}
}

func TestClassifyContent_Plain(t *testing.T) {
	input := "This is a plain text response with some markdown **bold** and a list:\n- item 1\n- item 2"
	got := ClassifyContent(input)
	if got != ContentPlain {
		t.Errorf("ClassifyContent(plain) = %d, want ContentPlain", got)
	}
}

func TestCompressHTML_RemovesScriptAndStyle(t *testing.T) {
	input := `<html><head><style>body{color:red}</style><script>alert('x')</script></head><body><p>Hello World</p></body></html>`
	got := compressHTML(input)
	if strings.Contains(got, "alert") {
		t.Error("script content not removed")
	}
	if strings.Contains(got, "color:red") {
		t.Error("style content not removed")
	}
	if !strings.Contains(got, "Hello World") {
		t.Error("body content lost")
	}
}

func TestCompressHTML_RemovesNavFooter(t *testing.T) {
	input := `<html><body><nav>menu items here</nav><main><p>Main content</p></main><footer>copyright 2024</footer></body></html>`
	got := compressHTML(input)
	if strings.Contains(got, "menu items") {
		t.Error("nav content not removed")
	}
	if strings.Contains(got, "copyright") {
		t.Error("footer content not removed")
	}
	if !strings.Contains(got, "Main content") {
		t.Error("main content lost")
	}
}

func TestCompressJSON_TruncatesLargeArrays(t *testing.T) {
	items := make([]interface{}, 20)
	for i := range items {
		items[i] = map[string]interface{}{"id": i, "name": fmt.Sprintf("item_%d", i)}
	}
	input := mustMarshal(items)
	got := compressJSON(input)
	if !strings.Contains(got, "省略") {
		t.Error("large array not truncated")
	}
	if !strings.Contains(got, "item_0") {
		t.Error("first items lost")
	}
	if !strings.Contains(got, "item_19") {
		t.Error("last items lost")
	}
}

func TestCompressJSON_ShortArrayUnchanged(t *testing.T) {
	input := `[1, 2, 3]`
	got := compressJSON(input)
	if strings.Contains(got, "省略") {
		t.Error("short array should not be truncated")
	}
}

func TestCompressJSON_Base64Replaced(t *testing.T) {
	b64 := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", 5) + "=="
	input := fmt.Sprintf(`{"image": "%s"}`, b64)
	got := compressJSON(input)
	if !strings.Contains(got, "base64 data") {
		t.Error("base64 not detected and replaced")
	}
}

func TestCompressTerminal_StripANSI(t *testing.T) {
	input := "\x1b[32mPASS\x1b[0m test_foo\n\x1b[32mPASS\x1b[0m test_bar\n\x1b[31mFAIL\x1b[0m test_baz"
	got := compressTerminal(input)
	if strings.Contains(got, "\x1b") {
		t.Error("ANSI codes not stripped")
	}
	if !strings.Contains(got, "PASS") {
		t.Error("content lost")
	}
}

func TestCompressTerminal_CollapseRepeatedLines(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "npm warn deprecated package@1.0.0"
	}
	input := strings.Join(lines, "\n")
	got := compressTerminal(input)
	if !strings.Contains(got, "重复") {
		t.Error("repeated lines not collapsed")
	}
	if strings.Count(got, "npm warn") > 2 {
		t.Error("too many repeated lines kept")
	}
}

func TestCompressTerminal_CollapseProgressBars(t *testing.T) {
	lines := []string{
		"Downloading [████░░░░░░] 40%",
		"Downloading [██████░░░░] 60%",
		"Downloading [████████░░] 80%",
		"Downloading [██████████] 100%",
		"Done!",
	}
	input := strings.Join(lines, "\n")
	got := compressTerminal(input)
	// Should keep only the last progress line + Done
	if strings.Count(got, "Downloading") > 1 {
		t.Errorf("progress lines not collapsed, got:\n%s", got)
	}
	if !strings.Contains(got, "100%") {
		t.Error("final progress state lost")
	}
	if !strings.Contains(got, "Done!") {
		t.Error("post-progress content lost")
	}
}

func TestCompressPlain_ShortenURLs(t *testing.T) {
	longURL := "https://www.example.com/very/long/path/to/some/deeply/nested/resource/page.html?query=value&another=param"
	input := "Check this link: " + longURL + " for details."
	got := compressPlain(input)
	if len(got) >= len(input) {
		t.Error("URL not shortened")
	}
	if !strings.Contains(got, "example.com") {
		t.Error("domain lost in URL shortening")
	}
}

func TestCompressPlain_ReplaceBase64(t *testing.T) {
	b64 := strings.Repeat("ABCDEFGHIJKLMNOP", 50)
	input := "Data: " + b64 + "\nEnd"
	got := compressPlain(input)
	if !strings.Contains(got, "base64 data") {
		t.Error("base64 block not replaced")
	}
}

func TestCompressToolResult_ShortInputUnchanged(t *testing.T) {
	input := "OK: file written successfully"
	got := CompressToolResult("write_file", input)
	if got != input {
		t.Errorf("short input changed: %q → %q", input, got)
	}
}

func TestCompressToolResult_EmptyInput(t *testing.T) {
	got := CompressToolResult("bash", "")
	if got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
}

func TestGetResultCap_KnownTool(t *testing.T) {
	cap := GetResultCap("web_fetch")
	if cap != 4000 {
		t.Errorf("web_fetch cap = %d, want 4000", cap)
	}
}

func TestGetResultCap_UnknownTool(t *testing.T) {
	cap := GetResultCap("some_unknown_mcp_tool")
	if cap != DefaultResultCap {
		t.Errorf("unknown tool cap = %d, want %d", cap, DefaultResultCap)
	}
}

func TestShortenURL_Short(t *testing.T) {
	short := "https://example.com/page"
	got := shortenURL(short)
	if got != short {
		t.Errorf("short URL changed: %q → %q", short, got)
	}
}

func TestShortenURL_Long(t *testing.T) {
	long := "https://www.example.com/api/v2/users/12345/posts/67890/comments/11111/replies/22222/attachments"
	got := shortenURL(long)
	if len(got) >= len(long) {
		t.Errorf("long URL not shortened: len=%d", len(got))
	}
	if !strings.Contains(got, "example.com") {
		t.Error("domain lost")
	}
}

func mustMarshal(v interface{}) string {
	out := "["
	arr, ok := v.([]interface{})
	if !ok {
		return "[]"
	}
	for i, item := range arr {
		if i > 0 {
			out += ","
		}
		switch val := item.(type) {
		case map[string]interface{}:
			out += fmt.Sprintf(`{"id":%v,"name":"%v"}`, val["id"], val["name"])
		default:
			out += fmt.Sprintf("%v", val)
		}
	}
	out += "]"
	return out
}
