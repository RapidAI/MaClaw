package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func workdirScope(run string) agentruntime.Scope {
	return agentruntime.Scope{TenantID: "t", UserID: "u_1::desk::bot_abc", RunID: run}
}

func TestDesktopWorkdirBashStartsInRunFolder(t *testing.T) {
	scope := workdirScope("run_wd-bash-0001-xyz")
	dir := desktopRunWorkDir(scope)
	if !strings.HasPrefix(dir, "/home/desktop/work/") || !strings.Contains(dir, "wd-bash-0001") {
		t.Fatalf("dir = %q", dir)
	}
	args := map[string]any{"command": "python3 x.py > ~/Desktop/log.txt"}
	note := desktopApplyWorkdir(scope, "bash", args)
	if got := args["command"].(string); !strings.HasPrefix(got, "mkdir -p "+dir+" && cd "+dir+" && python3") {
		t.Fatalf("command = %q", got)
	}
	if !strings.Contains(note, "project folder") || !strings.Contains(note, "uses the Desktop") {
		t.Fatalf("note = %q", note)
	}
	args = map[string]any{"command": "ls"}
	if note := desktopApplyWorkdir(scope, "bash", args); note != "" {
		t.Fatalf("second note = %q", note)
	}
}

func TestDesktopWorkdirMovesDesktopWritesUnlessAsked(t *testing.T) {
	scope := workdirScope("run_wd-file-0002")
	dir := desktopRunWorkDir(scope)
	args := map[string]any{"path": "~/Desktop/snake.html", "content": "x"}
	note := desktopApplyWorkdir(scope, "file_write", args)
	if args["path"] != dir+"/snake.html" || !strings.Contains(note, "instead of /home/desktop/Desktop/snake.html") {
		t.Fatalf("path = %v note = %q", args["path"], note)
	}
	read := map[string]any{"path": "/home/desktop/Desktop/snake.html"}
	desktopApplyWorkdir(scope, "deliver", read)
	if read["path"] != dir+"/snake.html" {
		t.Fatalf("deliver path = %v", read["path"])
	}
	home := map[string]any{"path": "/home/desktop/notes.md"}
	desktopApplyWorkdir(scope, "file_write", home)
	if home["path"] != dir+"/notes.md" {
		t.Fatalf("home path = %v", home["path"])
	}
	asked := map[string]any{"path": "/home/desktop/Desktop/r.md", "user_location": true}
	desktopApplyWorkdir(scope, "file_write", asked)
	if asked["path"] != "/home/desktop/Desktop/r.md" {
		t.Fatalf("asked path = %v", asked["path"])
	}
	rel := map[string]any{"path": "src/a.c"}
	desktopApplyWorkdir(scope, "file_read", rel)
	if rel["path"] != dir+"/src/a.c" {
		t.Fatalf("relative = %v", rel["path"])
	}
	other := map[string]any{"path": "/home/desktop/.config/x"}
	desktopApplyWorkdir(scope, "file_write", other)
	if other["path"] != "/home/desktop/.config/x" {
		t.Fatalf("dot path = %v", other["path"])
	}
}

func TestDesktopWorkdirOff(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_WORKDIR", "off")
	args := map[string]any{"command": "ls"}
	if note := desktopApplyWorkdir(workdirScope("run_off"), "bash", args); note != "" || args["command"] != "ls" {
		t.Fatalf("off changed %v %q", args, note)
	}
}
