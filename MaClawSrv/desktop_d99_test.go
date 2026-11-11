package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// Snake, Nov 11 10:57: goal_check met 6/6, then bash steps (screenshots,
// OCR) reopened the goal and the run ended "partial". The run's end now
// measures the reopened goal again: still met → done; broken → not done.
func TestD99ReopenedGoalIsMeasuredAgainAtRunEnd(t *testing.T) {
	for _, still := range []bool{true, false} {
		recoveryTestEnv(t)
		exists := atomic.Bool{}
		desktopRemoteFile = func(ctx context.Context, tenant, user, action, p, content, oldS, newS string) (string, error) {
			if action == "read" && exists.Load() {
				return "ok\n", nil
			}
			return "", errors.New("no such file")
		}
		operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
			return "done", nil
		}
		run := recRunID("run-d99-" + map[bool]string{true: "met", false: "broken"}[still])
		req := recoveryTurn("tenant-d99", "sam", run, "做贪吃蛇")
		check := map[string]any{"action": "goal_check", "goal": "snake", "criteria": []any{map[string]any{"kind": "file_exists", "path": "/home/desktop/snake.py"}}}
		if _, err := invoke(t, req, check); err != nil {
			t.Fatal(err)
		}
		if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "write snake"}); err != nil {
			t.Fatal(err)
		}
		exists.Store(true)
		if out, err := invoke(t, req, map[string]any{"action": "goal_check", "goal": "snake"}); err != nil || !strings.Contains(out, "all criteria met") {
			t.Fatalf("check: %s %v", out, err)
		}
		if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "import -window root shot.png"}); err != nil {
			t.Fatal(err)
		}
		g := desktopRecovery().Goals(recovery.ScopeKey("tenant-d99", "sam"), "check|")[0]
		if g.State == recovery.StateDone || g.Reason != desktopRecReopenWhy {
			t.Fatalf("bash did not reopen: state=%s reason=%q", g.State, g.Reason)
		}
		exists.Store(still)
		desktopRecRecheckAtEnd(context.Background(), run)
		outcome, why := desktopRecoveryOutcome(run)
		if still && outcome != "done" {
			t.Fatalf("still met but outcome=%s (%s)", outcome, why)
		}
		if !still && outcome == "done" {
			t.Fatalf("broken but outcome=done")
		}
	}
}

// macmini training, Nov 11 11:09: stopped by its time limit, recorded done.
func TestD99WatchdogStopIsNeverDone(t *testing.T) {
	run := "run_d99_wall"
	watchdogMu.Lock()
	watchdogRuns[run] = &watchdogRun{started: time.Now().Add(-61 * time.Minute), lastStep: time.Now(), warned: map[string]bool{}, stop: "wall", steps: 67, ok: 60, wrote: []string{"/home/desktop/train.py"}}
	watchdogMu.Unlock()
	t.Cleanup(func() { desktopWatchdogEnd(run) })
	desktopRecStateMu.Lock()
	desktopRecRunSteps[run] = true
	desktopRecStateMu.Unlock()
	t.Cleanup(func() { desktopRecoveryEndRun(run) })
	msg := &agentservice.Message{Content: "训练跑到第 3 轮，验证集 F1 0.91，模型存在 ~/model。"}
	outcome, why := desktopRunOutcome(run, msg)
	if outcome != agentservice.TaskOutcomePartial || !strings.Contains(why, "时间上限") {
		t.Fatalf("outcome=%s why=%q", outcome, why)
	}
	// The model gets one turn to say what it reached; then the stop reply.
	if _, ok := desktopWatchdogVerdict(run); ok {
		t.Fatal("no grace turn")
	}
	reply, ok := desktopWatchdogVerdict(run)
	if !ok || !strings.Contains(reply, "还没做完") || !strings.Contains(reply, "67 步") || !strings.Contains(reply, "/home/desktop/train.py") || !strings.Contains(reply, "剩下的部分") {
		t.Fatalf("reply %q", reply)
	}
}

// Bot 1 step 44, Nov 11: the whole shell command arrived as action= and was
// refused as "not part of this phase".
func TestD99ShellCommandAsActionIsNamed(t *testing.T) {
	cmd := "cd /tmp/maclaw-bot_x && python3 - <<'eof' import subprocess"
	err := desktopPublishPlanCheck(phaseResearch, cmd, map[string]any{"action": cmd})
	if err == nil || !strings.Contains(err.Error(), "action=bash with command=") || strings.Contains(err.Error(), "publish_plan") {
		t.Fatalf("err = %v", err)
	}
	if err := desktopPublishPlanCheck(phaseResearch, "bash", map[string]any{"command": cmd}); err != nil {
		t.Fatalf("bash refused in research: %v", err)
	}
	if err := desktopPublishPlanCheck(phaseCompose, "screenshot", nil); err == nil || !strings.Contains(err.Error(), "publish_plan(compose)") {
		t.Fatalf("phase refusal lost: %v", err)
	}
}
