package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// The supervisor's auto hand back shows in the run feed once.
func TestDesktopNoteAutoHandback(t *testing.T) {
	dir := t.TempDir()
	old := desktopEventsRoot
	desktopEventsRoot = dir
	defer func() { desktopEventsRoot = old }()
	scope := agentruntime.Scope{TenantID: "t1", UserID: "u-ah", InstanceID: "i", RunID: "run-ah-1"}
	desktopNoteAutoHandback(scope, map[string]any{"auto_handback": false})
	desktopNoteAutoHandback(scope, map[string]any{"auto_handback": true, "auto_handback_idle": float64(300)})
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files=%v", files)
	}
	raw, _ := os.ReadFile(files[0])
	if n := strings.Count(string(raw), "takeover_auto_handback"); n != 1 || !strings.Contains(string(raw), "300") {
		t.Fatalf("feed=%s", raw)
	}
}
