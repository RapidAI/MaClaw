package agent

import (
	"runtime"
	"strings"
	"testing"
)

func TestToolBashOutputTruncation(t *testing.T) {
	command := "printf 'x%.0s' {1..8193}; printf 'y%.0s' {1..4097} >&2"
	if runtime.GOOS == "windows" {
		command = "[Console]::Out.Write(('x' * 8193) -join ''); [Console]::Error.Write(('y' * 4097) -join '')"
	}

	got := ToolBash(map[string]interface{}{"command": command}, nil)
	want := strings.Repeat("x", 8192) + "\n... (输出已截断)" +
		"\n[stderr] " + strings.Repeat("y", 4096) + "\n... (错误输出已截断)"
	if got != want {
		t.Fatalf("truncated output mismatch: got length %d, want %d", len(got), len(want))
	}
}
