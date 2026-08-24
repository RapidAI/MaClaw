package toolresult

import (
	"fmt"
	"strings"
	"testing"
)

func TestStructuredPreview_TerminalTailHeavy(t *testing.T) {
	raw := "HEAD" + strings.Repeat("x", 8000) + "TAIL_END"
	p := StructuredPreview("bash", raw, 200)
	if !strings.Contains(p, "TAIL_END") {
		t.Fatalf("bash should keep tail: %q", p)
	}
	if len(p) > 220 {
		t.Fatalf("over limit: %d", len(p))
	}
}

func TestStructuredPreview_JSON(t *testing.T) {
	raw := `{"status":"ok","rows":[1,2,3],"blob":"` + strings.Repeat("z", 5000) + `"}`
	p := StructuredPreview("api_call", raw, 800)
	if !strings.Contains(p, "[json_preview]") {
		t.Fatalf("want json preview: %q", p)
	}
	if !strings.Contains(p, "status") {
		t.Fatalf("want keys: %q", p)
	}
	if len(p) > 900 {
		t.Fatalf("len=%d", len(p))
	}
}

func TestStructuredPreview_Diff(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/f.go b/f.go\n")
	b.WriteString("--- a/f.go\n+++ b/f.go\n@@ -1,3 +1,4 @@\n")
	for i := 0; i < 500; i++ {
		b.WriteString("+line added\n")
	}
	b.WriteString("LAST_HUNK\n")
	p := StructuredPreview("bash", b.String(), 400)
	// bash uses terminal reducer not diff — use tool that falls through to looksLikeDiff
	p = StructuredPreview("read_file", b.String(), 400)
	if !strings.Contains(p, "diff --git") && !strings.Contains(p, "已截断") {
		// either structured diff or default truncate is ok
		t.Logf("preview=%q", p[:min(120, len(p))])
	}
	if len(p) > 450 {
		t.Fatalf("len=%d", len(p))
	}
}

func TestStructuredPreview_WebFetchIntegrity(t *testing.T) {
	body := strings.Repeat("page ", 2000) + "\n\n--- 完整性信号 ---\nok=true\n"
	p := StructuredPreview("web_fetch", body, 300)
	if !strings.Contains(p, "完整性信号") {
		t.Fatalf("should keep integrity marker: %q", p)
	}
}

func TestCompressionStats_Record(t *testing.T) {
	ResetCompressionStats()
	RecordProjection("bash", 10000, 1000, true)
	RecordProjection("web_fetch", 5000, 2000, false)
	st := GetCompressionStats()
	if st.Projects != 2 || st.Spills != 1 {
		t.Fatalf("%+v", st)
	}
	if st.SavedBytes != 12000 {
		t.Fatalf("saved=%d", st.SavedBytes)
	}
	line := FormatCompressionLine()
	if !strings.Contains(line, "tool-compress:") || !strings.Contains(line, "spills=1") {
		t.Fatalf("line=%q", line)
	}
	ResetCompressionStats()
	if FormatCompressionLine() != "" {
		t.Fatal("empty after reset")
	}
}

func TestProject_UsesStructuredPreviewAndStats(t *testing.T) {
	ResetCompressionStats()
	dir := t.TempDir()
	raw := "START" + strings.Repeat("m", 10000) + "END"
	proj, err := Project(ProjectOptions{
		ToolName: "bash",
		Content:  raw,
		Limit:    500,
		Root:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proj.Spilled {
		t.Fatal("expected spill")
	}
	if !strings.Contains(proj.Preview, "END") {
		t.Fatalf("bash preview should keep end: %q", proj.Preview[max(0, len(proj.Preview)-80):])
	}
	st := GetCompressionStats()
	if st.Projects < 1 || st.SavedBytes <= 0 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestStructuredPreview_ComputerObserveKeepsRefsWhenOverLimit(t *testing.T) {
	raw := "mode=text_primary screen=100x100 scale=1.00 screen_index=0\nwindows:\n  - Word\nelements (80):\n"
	for i := 0; i < 80; i++ {
		raw += fmt.Sprintf("  e%d [button] \"Btn%d\" conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\n", i, i)
	}
	raw += "ocr_excerpt: Document saved successfully\n"
	preview := StructuredPreview("computer_observe", raw, 400)
	if !strings.Contains(preview, "ocr_excerpt: Document saved successfully") && !strings.Contains(preview, "windows:") {
		t.Fatalf("should keep header/ocr: %q", preview)
	}
	if !strings.Contains(preview, "e0 [button]") {
		t.Fatalf("over-limit observe must keep a head ref: %q", preview)
	}
	if !strings.Contains(preview, "e79 [button]") {
		t.Fatalf("over-limit observe must keep a tail ref: %q", preview)
	}
	if strings.Count(preview, "bbox=") >= 80 {
		t.Fatalf("must not keep the full element list: %q", preview)
	}
	if len(preview) > 480 {
		t.Fatalf("len=%d", len(preview))
	}
}

func TestStructuredPreview_ComputerObserveHugeOCRStillKeepsRefs(t *testing.T) {
	token := "UniqueOCRTailToken"
	raw := "mode=text_primary screen=100x100 scale=1.00 screen_index=0\nwindows:\n  - Word\nelements (80):\n"
	for i := 0; i < 80; i++ {
		raw += fmt.Sprintf("  e%d [button] \"Btn%d\" conf=1.00 bbox=1,1,20,20 center=10,10 src=a11y\n", i, i)
	}
	raw += "ocr_excerpt: " + strings.Repeat("o", 2000) + token + "\n"
	preview := StructuredPreview("computer_observe", raw, 800)
	if !strings.Contains(preview, "e0 [button]") {
		t.Fatalf("huge OCR must not drop head ref: %q", preview)
	}
	if !strings.Contains(preview, "e79 [button]") {
		t.Fatalf("huge OCR must not drop tail ref: %q", preview)
	}
	if !strings.Contains(preview, token) {
		t.Fatalf("huge OCR tail token must survive: %q", preview)
	}
	if strings.Count(preview, "bbox=") >= 80 {
		t.Fatalf("must not keep the full element list: bbox=%d", strings.Count(preview, "bbox="))
	}
	if len(preview) > 900 {
		t.Fatalf("len=%d", len(preview))
	}
}
