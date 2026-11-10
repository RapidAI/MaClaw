package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/recovery"
)

// recCheckGoal is the run's one check goal as the ledger on disk has it,
// read by a fresh engine (what a restarted process sees).
func recCheckGoal(t *testing.T, tenant, user string) *recovery.Goal {
	t.Helper()
	desktopRecoveryMu.Lock()
	desktopRecoveryEng = nil // a restart: nothing survives but the ledger
	desktopRecoveryMu.Unlock()
	gs := desktopRecovery().Goals(recovery.ScopeKey(tenant, user), "check|")
	if len(gs) != 1 {
		t.Fatalf("%d check goals", len(gs))
	}
	return gs[0]
}

// File-goal steps are charged in the durable ledger before they run, settle
// at their real end (also after the tool call gave up on them), and a
// restart between a step and the next goal_check neither loses nor resets
// the time, nor counts the attempt twice.
func TestRecoveryFileGoalStepTimeIsDurable(t *testing.T) {
	recoveryTestEnv(t)
	t.Setenv("MACLAW_DESKTOP_STEP_TIMEOUT_LONG", "0.4")
	exists := atomic.Bool{}
	desktopRemoteFile = func(ctx context.Context, tenant, user, action, p, content, oldS, newS string) (string, error) {
		if action == "read" && exists.Load() {
			return "ok\n", nil
		}
		return "", errors.New("no such file")
	}
	var commands []string
	var sleep atomic.Int64
	released := make(chan struct{}, 4)
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		commands = append(commands, desktopArg(args, "command"))
		d := time.Duration(sleep.Load())
		if strings.Contains(desktopArg(args, "command"), "honour") {
			select { // an operation that honours cancellation
			case <-time.After(d):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		} else {
			time.Sleep(d) // an external operation that ignores it
		}
		released <- struct{}{}
		return "done", nil
	}
	req := recoveryTurn("tenant-steps", "dora", recRunID("run-steps"), "生成 report.txt")
	check := map[string]any{"action": "goal_check", "goal": "report.txt", "criteria": []any{map[string]any{"kind": "file_exists", "path": "/home/desktop/report.txt"}}}
	if out, err := invoke(t, req, check); err != nil || !strings.Contains(out, "baseline") {
		t.Fatalf("baseline: %s %v", out, err)
	}

	// A step that finishes: its real time is charged, the attempt is counted once.
	sleep.Store(int64(100 * time.Millisecond))
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "make report"}); err != nil {
		t.Fatal(err)
	}
	<-released
	if !strings.Contains(commands[0], "timeout -k 5") || !strings.Contains(commands[0], "make report") {
		t.Fatalf("the container command is not bounded by the step deadline: %q", commands[0])
	}
	g := recCheckGoal(t, "tenant-steps", "dora")
	if g.Count != 1 || !g.Open || len(g.Steps) != 0 || g.SpentMS < 100 || g.SpentMS > 400 {
		t.Fatalf("after one step and a restart: count=%d open=%v steps=%d spent=%d", g.Count, g.Open, len(g.Steps), g.SpentMS)
	}
	spent := g.SpentMS

	// A step that outlives its deadline: the call returns at the deadline,
	// the reservation stays charged while it runs, and its real time is
	// charged when it really ends.
	sleep.Store(int64(900 * time.Millisecond))
	start := time.Now()
	if _, err := invoke(t, req, map[string]any{"action": "bash", "command": "slow report"}); err == nil || !strings.Contains(err.Error(), "attempt time its goal had left") {
		t.Fatalf("slow step: %v", err)
	}
	if took := time.Since(start); took > 800*time.Millisecond {
		t.Fatalf("the step ran past its deadline: %s", took)
	}
	g = recCheckGoal(t, "tenant-steps", "dora")
	if len(g.Steps) != 1 || g.SpentMS < spent+400 {
		t.Fatalf("while running past its deadline: steps=%d spent=%d", len(g.Steps), g.SpentMS)
	}
	<-released
	deadline := time.Now().Add(3 * time.Second)
	for {
		g = recCheckGoal(t, "tenant-steps", "dora")
		if len(g.Steps) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(g.Steps) != 0 || g.SpentMS < spent+900 || g.Count != 1 {
		t.Fatalf("late result: steps=%d spent=%d count=%d", len(g.Steps), g.SpentMS, g.Count)
	}
	spent = g.SpentMS

	// A cancelled step is charged what it ran.
	sleep.Store(int64(5 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	_, err := desktopRuntimeModule{}.InvokeTool(ctx, req, desktopToolName, map[string]any{"action": "bash", "command": "honour cancel"})
	cancel()
	if err == nil {
		t.Fatal("cancelled step succeeded")
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		g = recCheckGoal(t, "tenant-steps", "dora")
		if len(g.Steps) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(g.Steps) != 0 || g.SpentMS < spent+100 || g.SpentMS > spent+400 {
		t.Fatalf("cancelled: steps=%d spent=%d (before %d)", len(g.Steps), g.SpentMS, spent)
	}
	spent = g.SpentMS

	// After the restart the measurement closes the one attempt: not counted
	// twice, time not reset.
	exists.Store(true)
	if out, err := invoke(t, req, map[string]any{"action": "goal_check", "goal": "report.txt"}); err != nil || !strings.Contains(out, "all criteria met") {
		t.Fatalf("check: %s %v", out, err)
	}
	g = recCheckGoal(t, "tenant-steps", "dora")
	if g.Count != 1 || g.Open || g.SpentMS != spent || g.State != recovery.StateDone {
		t.Fatalf("after the check: count=%d open=%v spent=%d (want %d) state=%s", g.Count, g.Open, g.SpentMS, spent, g.State)
	}
	// Another person's ledger is untouched.
	if gs := desktopRecovery().Goals(recovery.ScopeKey("tenant-steps", "erin"), "check|"); len(gs) != 0 {
		t.Fatalf("another tenant user's ledger changed: %v", gs)
	}
}

// A crash between the reservation and the step's end keeps the reservation
// charged: the next process sees less time, never more.
func TestRecoveryCrashedStepStaysCharged(t *testing.T) {
	recoveryTestEnv(t)
	desktopRemoteFile = func(ctx context.Context, tenant, user, action, p, content, oldS, newS string) (string, error) {
		return "", errors.New("no such file")
	}
	req := recoveryTurn("tenant-steps", "finn", recRunID("run-crash"), "生成 out.txt")
	if _, err := invoke(t, req, map[string]any{"action": "goal_check", "goal": "out.txt", "criteria": []any{map[string]any{"kind": "file_exists", "path": "/home/desktop/out.txt"}}}); err != nil {
		t.Fatal(err)
	}
	// The process takes the reservation and dies before the step returns.
	att, err := desktopRecoveryBefore(req, req.Scope, "bash", map[string]any{"action": "bash", "command": "x"})
	if err != nil || att == nil || len(att.steps) != 1 {
		t.Fatalf("reservation: %v %+v", err, att)
	}
	g := recCheckGoal(t, "tenant-steps", "finn")
	if g.Count != 1 || len(g.Steps) != 1 || g.SpentMS != att.steps[0].reserved.Milliseconds() || g.SpentMS <= 0 {
		t.Fatalf("after the crash: count=%d steps=%d spent=%d", g.Count, len(g.Steps), g.SpentMS)
	}
	left := desktopRecovery().WallLeft(g)
	if left >= desktopRecovery().Cfg.MaxWall {
		t.Fatalf("the restart reset the attempt time: %s", left)
	}
}
