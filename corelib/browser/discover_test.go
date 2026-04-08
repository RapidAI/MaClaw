package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsProfileDevToolsCandidatesIncludesProfiles(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "Default"), 0o755); err != nil {
		t.Fatalf("mkdir Default: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "Profile 2"), 0o755); err != nil {
		t.Fatalf("mkdir Profile 2: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "System Profile"), 0o755); err != nil {
		t.Fatalf("mkdir System Profile: %v", err)
	}

	candidates := windowsProfileDevToolsCandidates(base)
	joined := strings.Join(candidates, "\n")
	for _, want := range []string{
		filepath.Join(base, "DevToolsActivePort"),
		filepath.Join(base, "Default", "DevToolsActivePort"),
		filepath.Join(base, "Profile 2", "DevToolsActivePort"),
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("candidates missing %q: %v", want, candidates)
		}
	}
	if strings.Contains(joined, filepath.Join(base, "System Profile", "DevToolsActivePort")) {
		t.Fatalf("unexpected non-profile candidate: %v", candidates)
	}
}

func TestSummarizeStderrCompactsWhitespace(t *testing.T) {
	got := summarizeStderr("\n  first line\r\n second\tline  ")
	if got != "first line second line" {
		t.Fatalf("summarizeStderr = %q", got)
	}
}
