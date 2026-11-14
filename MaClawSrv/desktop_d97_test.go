package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Errors seen on papers 2026-11-11 08:32–08:35 (Bot 2 run_69cbb334, Bot 3
// run_ef8bef3b).
const (
	d97NotOpen = "docker service rejected the desktop request (HTTP 400): invalid desktop request: this bot's desktop is not open yet"
	d97Refused = `desktop service is unreachable: Post "http://127.0.0.1:9399/api/v1/desktop-services/tool": dial tcp 127.0.0.1:9399: connect: connection refused`
)

func d97Env(t *testing.T) agentruntime.TurnRequest {
	t.Helper()
	// Trajectory writes are async and read test globals the cleanup restores.
	t.Setenv("MACLAW_DESKTOP_TRAJECTORY", "off")
	recoveryTestEnv(t)
	oldCtl, oldLedger := desktopRemoteCtl, desktopLedgerDir
	oldBack, oldRe, oldStep, oldRun := desktopOutageBackoff, desktopOutageReconnect, desktopOutageStepWindow, desktopOutageRunWindow
	dir := t.TempDir()
	desktopLedgerDir = func() string { return dir }
	desktopRemoteCtl = func(ctx context.Context, tenantID, userID, display string, request map[string]any) (map[string]any, error) {
		return map[string]any{"ok": true}, nil
	}
	desktopOutageBackoff = func(int) time.Duration { return time.Millisecond }
	desktopOutageStepWindow = func() time.Duration { return 2 * time.Second }
	desktopOutageRunWindow = func() time.Duration { return 10 * time.Second }
	t.Cleanup(func() {
		desktopRemoteCtl, desktopLedgerDir = oldCtl, oldLedger
		desktopOutageBackoff, desktopOutageReconnect, desktopOutageStepWindow, desktopOutageRunWindow = oldBack, oldRe, oldStep, oldRun
		openBotScreen = defaultOpenBotScreen
		resetBotScreens()
	})
	resetBotScreens()
	run := fmt.Sprintf("run-d97-%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	return recoveryTurn("tenant-d97", desktop.BotDesktopUser("daniel", "bot_e0e4"), run,
		"目标：登录远程服务器，训练一个文本二分类模型并给出评测结果。")
}

func TestDesktopD97OutageErrorsRecognised(t *testing.T) {
	for _, msg := range []string{d97Refused,
		`desktop wait: desktop service is unreachable: Post "http://127.0.0.1:9399/api/v1/desktop-services/ctl": dial tcp 127.0.0.1:9399: connect: connection refused`,
		`desktop service is unreachable: Post "http://h/x": dial tcp 10.0.0.1:9399: i/o timeout`,
	} {
		if !desktopOutageFailure(errors.New(msg)) {
			t.Errorf("not an outage: %s", msg)
		}
	}
	for _, msg := range []string{
		d97NotOpen, // the screen is closed: open it, do not wait 90s
		"docker service rejected the desktop request (HTTP 400): invalid desktop request: bad command",
		"desktop service is unreachable", // no client configured: not transient
		`desktop service is unreachable: Post "http://h/x": read tcp 1.2.3.4:5->6.7.8.9:443: connection reset by peer`, // may have run
		"bash: exit status 1",
		"cdp timeout: Page.navigate",
	} {
		if desktopOutageFailure(errors.New(msg)) {
			t.Errorf("must not be waited out (could have run or is permanent): %s", msg)
		}
	}
}

// A GUI step that hits "desktop is not open yet" opens the screen and runs
// once more. A plain shell does not. Neither spends the outage window.
func TestDesktopD97ClickOpensTheScreenAndRetriesOnce(t *testing.T) {
	req := d97Env(t)
	reconnects := 0
	desktopOutageReconnect = func(context.Context, agentruntime.Scope) string {
		reconnects++
		return "asked for the desktop session again"
	}
	opens := 0
	openBotScreen = func(context.Context, agentruntime.Scope) error {
		opens++
		return nil
	}
	calls := 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if desktopArg(args, "action") == "click" {
			calls++
			if calls == 1 {
				return "", errors.New(d97NotOpen)
			}
			return "clicked", nil
		}
		return "ok", nil
	}
	out, err := invoke(t, req, map[string]any{"action": "click", "x": 1, "y": 2})
	if err != nil {
		t.Fatalf("click after the screen opened: %v", err)
	}
	if calls != 2 || opens != 2 || reconnects != 0 || !strings.Contains(out, "clicked") || strings.Contains(out, "unavailable") {
		t.Fatalf("calls=%d opens=%d reconnects=%d out=%q", calls, opens, reconnects, out)
	}
	if desktopOutageWasSeen(req.Scope.RunID) {
		t.Fatal("a closed screen was waited out as an outage")
	}
	out, err = invoke(t, req, map[string]any{"action": "bash", "command": "nvidia-smi"})
	if err != nil || strings.Contains(out, "blocked") {
		t.Fatalf("second bash: %q %v", out, err)
	}
}

// Bot 2 / Bot 3 at 08:34:10: Hub restarted for 19 s. Steps wait it out.
func TestDesktopD97HubRestartIsWaitedOut(t *testing.T) {
	req := d97Env(t)
	desktopOutageReconnect = func(context.Context, agentruntime.Scope) string { return "" }
	down := 3
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if down > 0 {
			down--
			return "", errors.New(d97Refused)
		}
		return "done: " + desktopArg(args, "action"), nil
	}
	for _, a := range []map[string]any{{"action": "bash", "command": "python train.py"}, {"action": "screenshot"}, {"action": "app_list"}} {
		if out, err := invoke(t, req, a); err != nil || !strings.HasPrefix(strings.TrimSpace(out[strings.Index(out, "\n")+1:]), "done") && !strings.Contains(out, "done") {
			t.Fatalf("%v: %q %v", a, out, err)
		}
	}
}

// A service that stays down: bounded wait, honest error, then counted as before.
func TestDesktopD97OutageBoundedAndHonest(t *testing.T) {
	req := d97Env(t)
	desktopOutageStepWindow = func() time.Duration { return 30 * time.Millisecond }
	desktopOutageRunWindow = func() time.Duration { return 80 * time.Millisecond }
	desktopOutageReconnect = func(context.Context, agentruntime.Scope) string { return "" }
	calls := 0
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		calls++
		return "", errors.New(d97Refused)
	}
	_, err := invoke(t, req, map[string]any{"action": "bash", "command": "ls"})
	if err == nil || !strings.Contains(err.Error(), "still unreachable") || calls < 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	for i := 0; i < 6; i++ {
		_, _ = invoke(t, req, map[string]any{"action": "bash", "command": fmt.Sprintf("ls %d", i)})
	}
	before := calls
	start := time.Now()
	_, _ = invoke(t, req, map[string]any{"action": "bash", "command": "ls last"})
	if time.Since(start) > time.Second {
		t.Fatal("run window spent: must not keep waiting")
	}
	if calls-before > 1 {
		t.Fatalf("after the run window, no retries: %d", calls-before)
	}
	// The run did nothing and stopped on the outage: blocked, never a quiet success.
	outcome, why := desktopRunOutcome(req.Scope.RunID, &agentservice.Message{Content: "训练没有执行。"})
	if outcome != agentservice.TaskOutcomeBlocked || !strings.Contains(why, "连不上") {
		t.Fatalf("outcome=%q why=%q", outcome, why)
	}
}

// A publish already submitted is never re-run on an outage.
func TestDesktopD97OutageNeverRerunsSubmittedPublish(t *testing.T) {
	req := d97Env(t)
	if !desktopOutageAllowed(req.Scope, "publish", errors.New(d97Refused)) {
		t.Fatal("unsent publish may wait")
	}
	if desktopOutageAllowed(req.Scope, "bash", errors.New("exit status 2")) {
		t.Fatal("a real failure is not an outage")
	}
}

// A run that did real work and whose reply is fine keeps "done".
func TestDesktopD97WorkedRunNotBlocked(t *testing.T) {
	req := d97Env(t)
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		if desktopArg(args, "command") == "fail" {
			return "", errors.New("exit status 1")
		}
		return "ok", nil
	}
	_, _ = invoke(t, req, map[string]any{"action": "bash", "command": "python train.py"})
	_, _ = invoke(t, req, map[string]any{"action": "bash", "command": "fail"})
	if why, never := desktopRunNeverWorked(req.Scope.RunID); never {
		t.Fatalf("worked run reported as never worked: %s", why)
	}
}

// Cold start ("not open yet") never counts toward a block; infra blocks expire.
func TestDesktopD97ColdStartNotCountedAndInfraBlockExpires(t *testing.T) {
	req := d97Env(t)
	t.Setenv("MACLAW_DESKTOP_OUTAGE_WAIT", "off")
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		return "", errors.New(d97NotOpen)
	}
	for i := 0; i < 4; i++ {
		_, _ = invoke(t, req, map[string]any{"action": "bash", "command": fmt.Sprintf("echo %d", i)})
	}
	if err := desktopFailClassCheck(desktopOwner(req.Scope), "bash", map[string]any{"command": "x"}); err != nil {
		t.Fatalf("cold start blocked bash: %v", err)
	}
	operateDesktopFunc = func(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
		return "", errors.New(d97Refused)
	}
	for i := 0; i < 2; i++ {
		_, _ = invoke(t, req, map[string]any{"action": "bash", "command": fmt.Sprintf("ls %d", i)})
	}
	if err := desktopFailClassCheck(desktopOwner(req.Scope), "bash", map[string]any{"command": "x"}); err == nil {
		t.Fatal("two refused dials (no wait) must block for now")
	}
	old := desktopInfraBlockTTL
	desktopInfraBlockTTL = 0
	defer func() { desktopInfraBlockTTL = old }()
	if err := desktopFailClassCheck(desktopOwner(req.Scope), "bash", map[string]any{"command": "x"}); err != nil {
		t.Fatalf("infra block must expire: %v", err)
	}
}

// Every run gets its own scratch folder under its bot's (11-12: parallel
// runs of one bot overwrote each other's files in /tmp/maclaw-<bot>).
func TestDesktopD97PerBotScratch(t *testing.T) {
	req := d97Env(t)
	args := map[string]any{"command": "ls /tmp"}
	note := desktopBotScratch(req.Scope, args)
	dir := desktopBotScratchDir(req.Scope)
	if !strings.HasPrefix(dir, "/tmp/maclaw-bot_e0e4/run-") || !strings.Contains(note, dir) || !strings.Contains(args["command"].(string), "export TMPDIR="+dir+"; ls /tmp") {
		t.Fatalf("dir=%q note=%q cmd=%q", dir, note, args["command"])
	}
	if again := desktopBotScratch(req.Scope, map[string]any{"command": "pwd"}); again != "" {
		t.Fatal("folder named once per run")
	}
	noRun := req.Scope
	noRun.RunID = ""
	if got := desktopBotScratchDir(noRun); got != "/tmp/maclaw-bot_e0e4" {
		t.Fatalf("no run id keeps the per-bot folder: %q", got)
	}
	owner := req.Scope
	owner.UserID = "daniel"
	if got := desktopBotScratchDir(owner); !strings.HasPrefix(got, "/tmp/maclaw-main/run-") {
		t.Fatalf("owner desktop run folder: %q", got)
	}
	owner.RunID = ""
	if desktopBotScratchDir(owner) != "" {
		t.Fatal("owner desktop without a run keeps /tmp")
	}
}

// The final notice names the cause and what was left behind.
func TestDesktopD97SideEffectsInNotice(t *testing.T) {
	run := fmt.Sprintf("run-d97-side-%d", time.Now().UnixNano())
	desktopSideNoteCause(run, desktopOutageCause(errors.New(d97Refused)))
	desktopSideNote(run, "zhuanlan.zhihu.com：编辑器里留下了《测试2》，点过一次发布，没有发出去")
	n := desktopSideNotice(run)
	if !strings.Contains(n, "Hub") || !strings.Contains(n, "测试2") || !strings.Contains(n, "点过一次发布") {
		t.Fatalf("notice: %s", n)
	}
	if desktopSideNotice(run) != "" {
		t.Fatal("taken once")
	}
}

// check_posted is read-only: without a post list it refuses, never fills.
func TestDesktopD97CheckPostedIsReadOnly(t *testing.T) {
	req := d97Env(t)
	out, done, err := desktopPublishLedgerGuard(req.Scope, nil, browser.PublishSpec{Title: "LingBot-Touch 速读", EditorURL: "https://zhuanlan.zhihu.com/write"}, map[string]any{"check_posted": true})
	if !done || err == nil || !strings.Contains(err.Error(), "read-only") || out != "" {
		t.Fatalf("done=%v out=%q err=%v", done, out, err)
	}
}
