package toolresult

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectNoSpillWhenShort(t *testing.T) {
	dir := t.TempDir()
	proj, err := Project(ProjectOptions{
		ToolName: "bash",
		Content:  "hello",
		Preview:  "hello",
		Root:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Spilled || proj.Handle != nil {
		t.Fatalf("should not spill: %+v", proj)
	}
	if proj.Preview != "hello" {
		t.Fatalf("preview=%q", proj.Preview)
	}
}

func TestProjectSpillsWhenTruncated(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("line\n", 5000) // ~25KB
	preview := DefaultPreview(raw, 4096)
	if preview == raw {
		t.Fatal("expected truncation for large content")
	}

	proj, err := Project(ProjectOptions{
		ToolName:   "bash",
		SessionKey: "user-1",
		Content:    raw,
		Preview:    preview,
		Root:       dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proj.Spilled || proj.Handle == nil {
		t.Fatalf("expected spill: %+v", proj)
	}
	if !strings.Contains(proj.Preview, "[tool_result_handle]") {
		t.Fatalf("preview missing handle footer: %s", proj.Preview[:min(200, len(proj.Preview))])
	}
	if !strings.Contains(proj.Preview, proj.Handle.Path) {
		t.Fatalf("preview missing path")
	}
	if _, err := os.Stat(proj.Handle.Path); err != nil {
		t.Fatalf("spilled file: %v", err)
	}
	gotRes, err := Read(ReadOptions{Path: proj.Handle.Path, Root: dir, Limit: len(raw) + 10})
	if err != nil {
		t.Fatal(err)
	}
	if gotRes.Content != raw {
		t.Fatalf("stored content mismatch: got %d bytes want %d", len(gotRes.Content), len(raw))
	}
	// Handle under session dir
	if !strings.Contains(proj.Handle.Path, filepath.Join(dir, "user-1")) {
		t.Fatalf("path=%s", proj.Handle.Path)
	}
}

func TestDefaultPreviewKeepsHeadAndTail(t *testing.T) {
	raw := "HEAD" + strings.Repeat("x", 8000) + "TAIL"
	p := DefaultPreview(raw, 200)
	if !strings.Contains(p, "HEAD") || !strings.Contains(p, "TAIL") {
		t.Fatalf("preview=%q", p)
	}
	if !strings.Contains(p, "已截断") {
		t.Fatalf("missing truncation mark: %q", p)
	}
}

func TestReadByIDAndOffset(t *testing.T) {
	dir := t.TempDir()
	raw := "ABCDEFGHIJKLMNOPQRSTUVWXYZ" + strings.Repeat("!", 1000)
	proj, err := Project(ProjectOptions{
		ToolName:      "bash",
		SessionKey:    "sess-a",
		Content:       raw,
		Preview:       DefaultPreview(raw, 64),
		Root:          dir,
		MinSpillBytes: 1,
		ForceSpill:    true,
	})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	if !strings.Contains(proj.Preview, "read_tool_result") {
		t.Fatalf("footer should mention read_tool_result: %s", proj.Preview[max(0, len(proj.Preview)-200):])
	}

	// Read by id + session
	r1, err := Read(ReadOptions{
		ID:         proj.Handle.ID,
		SessionKey: "sess-a",
		Root:       dir,
		Offset:     0,
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != raw[:10] || !r1.Truncated || r1.NextOffset != 10 {
		t.Fatalf("r1=%+v", r1)
	}

	// Continue paging
	r2, err := Read(ReadOptions{
		Path:   proj.Handle.Path,
		Root:   dir,
		Offset: r1.NextOffset,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Content != raw[10:20] {
		t.Fatalf("r2 content=%q", r2.Content)
	}

	// Security: path outside store rejected
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ReadOptions{Path: outside, Root: dir}); err == nil {
		t.Fatal("expected path outside store to fail")
	}

	text := FormatReadResult(r1)
	if !strings.Contains(text, "[tool_result_read]") || !strings.Contains(text, "next_offset") {
		t.Fatalf("format=%q", text)
	}
}

func TestResolveMissingHandle(t *testing.T) {
	dir := t.TempDir()
	if _, err := Resolve("no_such_handle", "", "sess", dir); err == nil {
		t.Fatal("expected missing handle error")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
