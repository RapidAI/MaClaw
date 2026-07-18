package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffTextSimple(t *testing.T) {
	d := unifiedDiffText("a\nb\n", "a\nc\n", "f.txt")
	if !strings.Contains(d, "-b") || !strings.Contains(d, "+c") {
		t.Fatalf("diff=%q", d)
	}
}

func TestBuildACPWriteDiffSummaryNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	rid := "acp-diff-test-1"
	// capture missing file as empty before
	globalACPWriteSnaps.capture(rid, "write_file", `{"path":"hello.txt","content":"hi"}`, dir)
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	card := buildACPWriteDiffSummary(rid, "write_file", `{"path":"hello.txt","content":"hi"}`, dir)
	if card == "" || !strings.Contains(card, "hello.txt") || !strings.Contains(card, "```diff") {
		t.Fatalf("card=%q", card)
	}
	if !strings.Contains(card, "new file") && !strings.Contains(card, "+hi") {
		t.Fatalf("expected new-file markers, card=%q", card)
	}
}

func TestAllowAlwaysMemory(t *testing.T) {
	s := newACPHostSession(nil, "tok", nil)
	if s.isAllowAlways("sess1", "bash") {
		t.Fatal("expected false")
	}
	s.rememberAllowAlways("sess1", "bash")
	if !s.isAllowAlways("sess1", "bash") {
		t.Fatal("expected true after remember")
	}
	if s.isAllowAlways("sess2", "bash") {
		t.Fatal("other session should not inherit")
	}
}

func TestWriteSnapshotKeyCaseInsensitive(t *testing.T) {
	if filepath.Separator != '\\' {
		t.Skip("Windows path case only")
	}
	dir := t.TempDir()
	// Force mixed-case abs paths via different casing of the same location.
	pathLower := strings.ToLower(filepath.Join(dir, "x.txt"))
	pathMixed := pathLower
	if len(pathMixed) > 0 {
		// Flip drive letter case when present (D: vs d:).
		r := []rune(pathMixed)
		if len(r) >= 2 && r[1] == ':' {
			if r[0] >= 'a' && r[0] <= 'z' {
				r[0] = r[0] - 'a' + 'A'
			} else if r[0] >= 'A' && r[0] <= 'Z' {
				r[0] = r[0] - 'A' + 'a'
			}
			pathMixed = string(r)
		}
	}
	rid := "acp-case-key"
	var store acpWriteSnapshotStore
	store.before = map[string]string{}
	store.before[store.key(rid, pathLower)] = "old"
	got, ok := store.take(rid, pathMixed)
	if !ok || got != "old" {
		t.Fatalf("case-normalized key miss: ok=%v got=%q lower=%q mixed=%q", ok, got, pathLower, pathMixed)
	}
}
