package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestScreenIntentFollowsTheFlagNotTheCommandText(t *testing.T) {
	req := d97Env(t)
	dir := t.TempDir()
	prevPath, prevGrace, prevExec := desktopLongJobPath, desktopLongJobGrace, desktopLongJobExec
	desktopLongJobPath = func() string { return filepath.Join(dir, "jobs.json") }
	desktopLongJobGrace = nil
	desktopLongJobExec = nil
	desktopLongJobResetForTest()
	t.Cleanup(func() {
		desktopLongJobPath, desktopLongJobGrace, desktopLongJobExec = prevPath, prevGrace, prevExec
		desktopLongJobResetForTest()
		desktopRoundWakes.Delete(req.Scope.RunID)
	})
	desktopRemoteCtl = func(context.Context, string, string, string, map[string]any) (map[string]any, error) {
		return nil, errors.New("no windows")
	}
	type call struct {
		command string
		needs   bool
	}
	var calls []call
	prevBash := desktopRemoteBash
	desktopRemoteBash = func(ctx context.Context, _, _, command string) (string, error) {
		needs, _ := ctx.Value(desktopNeedsScreenKey{}).(bool)
		calls = append(calls, call{command: command, needs: needs})
		if strings.Contains(command, "setsid") {
			return "LONGJOB_STARTED pid=4242 pgid=4242 start=100 log=/tmp/job.log exit_file=/tmp/job.exit", nil
		}
		return "ran", nil
	}
	t.Cleanup(func() { desktopRemoteBash = prevBash })
	operateDesktopFunc = operateDesktop

	cases := []struct {
		name       string
		args       map[string]any
		needs      bool
		open       bool
		background bool
	}{
		{name: "omitted gui text", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf"}, needs: false, open: false},
		{name: "false gui text", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf", "needs_screen": false}, needs: false, open: false},
		{name: "true gui text", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf", "needs_screen": true}, needs: true, open: true},
		{name: "true plain text", args: map[string]any{"action": "bash", "command": "echo hi", "needs_screen": true}, needs: true, open: true},
		{name: "background omitted", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf", "background": true}, needs: false, open: false, background: true},
		{name: "background false", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf", "background": true, "needs_screen": false}, needs: false, open: false, background: true},
		{name: "background true", args: map[string]any{"action": "bash", "command": "xdg-open /tmp/a.pdf", "background": true, "needs_screen": true}, needs: true, open: true, background: true},
		{name: "keep_running true", args: map[string]any{"action": "bash", "command": "echo hi", "keep_running": true, "needs_screen": true}, needs: true, open: true, background: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls = nil
			resetBotScreens()
			desktopRoundWakes.Delete(req.Scope.RunID)
			if tc.needs {
				desktopRoundWakes.Store(req.Scope.RunID, &desktopRoundWake{
					have: true, woken: true, endpoint: desktopEndpoint{Display: ":21"},
				})
			}
			var opens atomic.Int32
			openBotScreen = func(context.Context, agentruntime.Scope) error {
				opens.Add(1)
				return nil
			}
			out, err := invoke(t, req, tc.args)
			if err != nil {
				t.Fatalf("out=%q err=%v", out, err)
			}
			if opens.Load() > 0 != tc.open {
				t.Fatalf("opens=%d want open=%v", opens.Load(), tc.open)
			}
			if len(calls) == 0 {
				t.Fatal("the shell was not run")
			}
			got := calls[0]
			if got.needs != tc.needs {
				t.Fatalf("needs_screen=%v command=%q", got.needs, got.command)
			}
			if tc.background {
				if !strings.Contains(got.command, "setsid") {
					t.Fatalf("background shell was not the job wrapper: %s", got.command)
				}
				saved := desktopLongJobLatestFor(req.Scope)
				if saved == nil || saved.NeedsScreen != tc.needs || saved.SourceRun != req.Scope.RunID {
					t.Fatalf("stored job %+v", saved)
				}
				calls = nil
				if _, err := desktopLongJobRunScreen(context.Background(), desktopLongJobScope(saved), saved.Command, saved.NeedsScreen); err != nil {
					t.Fatal(err)
				}
				if len(calls) != 1 || calls[0].needs != tc.needs || !strings.Contains(calls[0].command, tc.args["command"].(string)) {
					t.Fatalf("resume calls %+v", calls)
				}
			}
		})
	}
}
