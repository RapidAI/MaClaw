package main

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Each run works in its own project folder (Daniel 11-11 21:55: bots were
// leaving code, logs and scratch files on the shared Desktop). The folder is
// /home/desktop/work/<bot>/<run>, made at the run's first file or bash step.
// bash starts there, a relative file path resolves there, and a file_write
// aimed at the Desktop or the bare home folder lands there instead, unless
// the model says the person asked for that place (user_location=true).
// Deliverables still go to the chat with deliver. MACLAW_DESKTOP_WORKDIR=off
// turns this off.

const desktopHomeDir = "/home/desktop"

var desktopWorkdirSafe = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// desktopWorkdirTold: run id -> true once the folder was named.
var desktopWorkdirTold sync.Map

// desktopWorkdirMoved: run id -> *sync.Map of asked path -> written path, so
// a later read, edit or deliver of the Desktop path finds the moved file.
var desktopWorkdirMoved sync.Map

var desktopBashDesktopPath = regexp.MustCompile(`(~|\$HOME|\$\{HOME\}|/home/desktop)/(Desktop|桌面)(/|\b)`)

func desktopWorkdirEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_DESKTOP_WORKDIR")))
	return v != "off" && v != "0" && v != "false"
}

// desktopRunWorkDir is this run's project folder ("" without a run id).
func desktopRunWorkDir(scope agentruntime.Scope) string {
	run := strings.TrimSpace(scope.RunID)
	if run == "" || !desktopWorkdirEnabled() {
		return ""
	}
	_, bot := desktop.SplitDesktopUser(desktopOwner(scope).UserID)
	if bot == "" {
		bot = "main"
	}
	run = strings.TrimPrefix(run, "run_")
	if len(run) > 12 {
		run = run[:12]
	}
	return path.Join(desktopHomeDir, "work", desktopWorkdirSafe.ReplaceAllString(bot, "_"), desktopWorkdirSafe.ReplaceAllString(run, "_"))
}

func desktopWorkdirAbs(p string) string {
	p = strings.TrimSpace(p)
	switch {
	case p == "~":
		return desktopHomeDir
	case strings.HasPrefix(p, "~/"):
		return path.Join(desktopHomeDir, p[2:])
	}
	return p
}

// desktopPersonPlace reports a path in the person's own places: the Desktop
// folder or directly in the home folder.
func desktopPersonPlace(p string) (rel string, ok bool) {
	p = path.Clean(p)
	for _, desk := range []string{path.Join(desktopHomeDir, "Desktop"), path.Join(desktopHomeDir, "桌面")} {
		if strings.HasPrefix(p, desk+"/") {
			return strings.TrimPrefix(p, desk+"/"), true
		}
	}
	if path.Dir(p) == desktopHomeDir && !strings.HasPrefix(path.Base(p), ".") {
		return path.Base(p), true
	}
	return "", false
}

func desktopBoolArg(args map[string]any, key string) bool {
	switch v := args[key].(type) {
	case bool:
		return v
	case string:
		v = strings.ToLower(strings.TrimSpace(v))
		return v == "true" || v == "1" || v == "yes"
	}
	return false
}

func desktopWorkdirMovedFor(runID string) *sync.Map {
	value, _ := desktopWorkdirMoved.LoadOrStore(strings.TrimSpace(runID), &sync.Map{})
	return value.(*sync.Map)
}

// desktopWorkdirTell names the folder once per run.
func desktopWorkdirTell(scope agentruntime.Scope, dir string) string {
	if _, told := desktopWorkdirTold.LoadOrStore(strings.TrimSpace(scope.RunID), true); told {
		return ""
	}
	return fmt.Sprintf("(this run's project folder is %s: bash starts there and relative paths resolve there. Keep code, logs and temporary files in it, not on the Desktop or directly in %s; those are the person's, so put a file there only when the person asked for that place (file_write user_location=true). Send results to the person with deliver.)\n", dir, desktopHomeDir)
}

// desktopApplyWorkdir rewrites a file or bash step for this run's folder and
// returns a note for the result.
func desktopApplyWorkdir(scope agentruntime.Scope, action string, args map[string]any) string {
	dir := desktopRunWorkDir(scope)
	if dir == "" || args == nil {
		return ""
	}
	note := ""
	switch action {
	case "bash":
		cmd, _ := args["command"].(string)
		if strings.TrimSpace(cmd) == "" {
			return ""
		}
		args["command"] = fmt.Sprintf("mkdir -p %s && cd %s && %s", dir, dir, cmd)
		note = desktopWorkdirTell(scope, dir)
		if desktopBashDesktopPath.MatchString(cmd) {
			note += fmt.Sprintf("(note: this command uses the Desktop. Working files belong in %s; leave a file on the Desktop only when the person asked for it there.)\n", dir)
		}
	case "file_read", "file_write", "file_edit", "file_list", "deliver":
		raw, _ := args["path"].(string)
		if strings.TrimSpace(raw) == "" {
			return ""
		}
		p := desktopWorkdirAbs(raw)
		if !strings.HasPrefix(p, "/") {
			p = path.Join(dir, p)
		}
		moved := desktopWorkdirMovedFor(scope.RunID)
		if action == "file_write" && !desktopBoolArg(args, "user_location") {
			if rel, ok := desktopPersonPlace(p); ok {
				target := path.Join(dir, rel)
				moved.Store(path.Clean(p), target)
				note = fmt.Sprintf("(written to this run's project folder as %s instead of %s: the Desktop and home folder are the person's. Use that path from now on; send it with deliver. Only when the person asked for the file at %s, write it again with user_location=true.)\n", target, p, p)
				p = target
			}
		} else if target, ok := moved.Load(path.Clean(p)); ok {
			p = target.(string)
		}
		args["path"] = p
		if note == "" {
			note = desktopWorkdirTell(scope, dir)
		}
	}
	return note
}
