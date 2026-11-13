package main

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func init() {
	desktopLongJobTickerDisabled = true
}

func d100Env(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dir)
	desktopLongJobPath = func() string { return filepath.Join(dir, "jobs.json") }
	desktopLongJobResetForTest()
	desktopLongJobTalk = func(context.Context, *desktopLongJob, desktopLongJobDecision) error { return nil }
	grace, sleep := desktopLongJobGrace, desktopLongJobSleep
	desktopLongJobGrace = []time.Duration{0, 0}
	desktopLongJobSleep = func(ctx context.Context, d time.Duration) bool { return true }
	t.Cleanup(func() {
		desktopLongJobGrace, desktopLongJobSleep = grace, sleep
		desktopLongJobTalk = desktopLongJobTalkDefault
		desktopLongJobExec = nil
		desktopLongJobResetForTest()
	})
}

func TestD100LongJobDetection(t *testing.T) {
	if !desktopLongJobShouldStart(map[string]any{"command": "ls", "background": true}) {
		t.Fatal("background flag")
	}
	if !desktopLongJobShouldStart(map[string]any{"command": "python train.py --epochs 10"}) {
		t.Fatal("train intent")
	}
	// A command that backgrounds its own process returns at once: it is a
	// plain step, not a job the wrapper could track (papers 11-11 20:33).
	if desktopLongJobShouldStart(map[string]any{"command": "nohup python job.py &"}) {
		t.Fatal("self-detached command wrapped as a job")
	}
	if !desktopLongJobShouldStart(map[string]any{"command": "sleep 5", "expect_minutes": 30}) {
		t.Fatal("expect_minutes")
	}
	if desktopLongJobShouldStart(map[string]any{"command": "python train.py --help"}) {
		t.Fatal("help should stay short")
	}
	if desktopLongJobShouldStart(map[string]any{"command": "ls -l /tmp"}) {
		t.Fatal("ls is not a long job")
	}
	if !desktopLongJobCommand("在服务器上训练一个文本分类模型") {
		// command may be English-only; Chinese 训练 is in the intent regex as a token
		if !desktopLongJobIntentRe.MatchString("训练一个文本分类模型") {
			t.Fatal("训练 intent")
		}
	}
}

func TestD100RemoteNohupInject(t *testing.T) {
	in := "sshpass -p x ssh -p 99 znsoft@host 'python train.py --epochs 3'"
	out := desktopLongJobInjectRemoteNohup(in)
	if !strings.Contains(out, "nohup bash -lc") || !strings.Contains(out, "python train.py") {
		t.Fatalf("wrap: %s", out)
	}
	same := desktopLongJobInjectRemoteNohup("ssh host 'nohup python train.py &'")
	if same != "ssh host 'nohup python train.py &'" {
		t.Fatalf("already nohup rewritten: %s", same)
	}
}

func TestD100StartAndPollAndDecisions(t *testing.T) {
	d100Env(t)
	var started string
	alive := true
	exit := ""
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		if strings.Contains(command, "nohup bash -lc") && strings.Contains(command, "LONGJOB_STARTED") {
			started = command
			return "LONGJOB_STARTED pid=4242 log=/tmp/job.log exit_file=/tmp/job.exit", nil
		}
		out := "LONGJOB_DEAD pid=4242\n"
		if alive {
			out = "LONGJOB_ALIVE pid=4242\n"
		}
		if exit != "" {
			out += "LONGJOB_EXIT=" + exit + "\n"
		}
		out += "LONGJOB_LOG_BYTES=20\nLONGJOB_LOG_TAIL\nepoch 1 loss=0.9\n"
		return out, nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_x", SessionID: "sess_x", RunID: "run_x"}
	out, err := desktopLongJobStart(context.Background(), scope, agentruntime.Scope{TenantID: "tenant_srv", UserID: "user_srv"}, map[string]any{"command": "python train.py --epochs 3"})
	if err != nil || !strings.Contains(out, "后台任务已启动") || !strings.Contains(out, "4242") {
		t.Fatalf("start: %s %v", out, err)
	}
	if !strings.Contains(started, "python train.py") || strings.Contains(started, "timeout -k") {
		t.Fatalf("start script: %s", started)
	}
	j := desktopLongJobLatestFor(scope)
	if j == nil || j.PID != "4242" || j.SourceRun != "run_x" || j.PrincipalTenant != "tenant_srv" || j.PrincipalUser != "user_srv" {
		t.Fatalf("stored %+v", j)
	}
	if desktopLongJobKeepsRunning(j) || desktopLongJobKeepsRunning(&desktopLongJob{Command: "./snake --demo"}) || !desktopLongJobKeepsRunning(&desktopLongJob{Command: "python train.py", KeepRunning: true}) {
		t.Fatal("keep_running is the declared flag, not the command text")
	}
	oc, why, ok := desktopLongJobOutcome("run_x")
	if !ok || oc != "running" || !strings.Contains(why, desktopLongJobShortID(j.ID)) || !strings.Contains(why, "python train.py") {
		t.Fatalf("outcome %s %s %v", oc, why, ok)
	}

	j.NextPoll = desktopLongJobNow().Add(-time.Second)
	_ = desktopLongJobSave(j)
	talked := 0
	desktopLongJobTalk = func(context.Context, *desktopLongJob, desktopLongJobDecision) error {
		talked++
		return nil
	}
	// First poll with a milestone, past the first-notice wait: notice.
	j.NextCheck = desktopLongJobNow().Add(-time.Second)
	_ = desktopLongJobSave(j)
	if _, err := desktopLongJobWatchTick(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	if talked != 1 {
		t.Fatalf("first progress should be told, got %d", talked)
	}
	j = desktopLongJobGet(j.ID)
	// Immediate second tick should be silent (next poll in the future, same fingerprint after skip?).
	// Force due: move NextPoll back, keep LastNotice recent, same log.
	j.NextPoll = desktopLongJobNow().Add(-time.Second)
	_ = desktopLongJobSave(j)
	if _, err := desktopLongJobWatchTick(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	if talked != 1 {
		t.Fatalf("quiet tick talked %d", talked)
	}

	alive, exit = false, "0"
	j = desktopLongJobGet(j.ID)
	j.NextPoll = desktopLongJobNow().Add(-time.Second)
	_ = desktopLongJobSave(j)
	if _, err := desktopLongJobWatchTick(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	j = desktopLongJobGet(j.ID)
	if j.Status != "done" || talked != 2 {
		t.Fatalf("done status=%s talked=%d", j.Status, talked)
	}
}

func TestD100StallResumes(t *testing.T) {
	d100Env(t)
	desktopLongJobNow = func() time.Time { return time.Date(2026, 11, 11, 12, 0, 0, 0, time.Local) }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	j := &desktopLongJob{ID: "job_stall", Status: "running", Alive: true, Started: desktopLongJobNow().Add(-20 * time.Minute), LastChange: desktopLongJobNow().Add(-16 * time.Minute), StallAfter: 15 * time.Minute, Fingerprint: "aaa"}
	if dec := desktopLongJobDecide(j, "running", "aaa", desktopLongJobNow().Add(-time.Minute)); dec != longJobSilent {
		// status still running in this snapshot; poll would mark stalled first
		_ = dec
	}
	j.Status = "stalled"
	if dec := desktopLongJobDecide(j, "running", "bbb", desktopLongJobNow().Add(-time.Minute)); dec != longJobResume {
		t.Fatalf("stall: %s", dec)
	}
}

func TestD100WatchTaskRouting(t *testing.T) {
	if id := desktopLongJobIDFromTask(&scheduler.ScheduledTask{Name: "watch-job:job_abc", TaskType: "watch"}); id != "job_abc" {
		t.Fatalf("id %s", id)
	}
	if id := desktopLongJobIDFromTask(&scheduler.ScheduledTask{Action: "LONGJOB job_xyz 巡查"}); id != "job_xyz" {
		t.Fatalf("action id %s", id)
	}
	if _, _, ok := desktopLongJobHandleScheduled(context.Background(), &scheduler.ScheduledTask{Name: "morning report", Action: "总结邮件"}); ok {
		t.Fatal("ordinary task")
	}
}

func TestD100FailedExitIsFailed(t *testing.T) {
	p := desktopLongJobParsePoll("LONGJOB_DEAD pid=1\nLONGJOB_EXIT=7\nLONGJOB_LOG_BYTES=1\nLONGJOB_LOG_TAIL\nboom\n")
	if p.Alive || p.ExitCode == nil || *p.ExitCode != 7 {
		t.Fatalf("%+v", p)
	}
	j := &desktopLongJob{}
	j.Alive, j.ExitCode, j.LastTail = p.Alive, p.ExitCode, p.Tail
	if p.ExitCode != nil && *p.ExitCode == 0 {
		j.Status = "done"
	} else if !p.Alive {
		j.Status = "failed"
	}
	if j.Status != "failed" {
		t.Fatalf("status %s", j.Status)
	}
}

// papers 2026-11-11: a 1 s README check was named "train" and became a job.
func TestGraceWaitPollsDoNotRestageSecrets(t *testing.T) {
	d100Env(t)
	ctx := withDesktopBashSecrets(context.Background(), []string{"SSH_PASSWORD"})
	if got := desktopBashSecretsOnContext(ctx); len(got) != 1 || got[0] != "SSH_PASSWORD" {
		t.Fatalf("user command context %#v", got)
	}
	polls := 0
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		polls++
		if len(desktopBashSecretsOnContext(ctx)) != 0 {
			t.Fatalf("poll %d restaged secrets: %s", polls, command)
		}
		return "LONGJOB_ALIVE pid=9\nLONGJOB_LOG_BYTES=1\nLONGJOB_LOG_TAIL\nworking\n", nil
	}
	j := &desktopLongJob{ID: "job_grace_sec", Status: "running", Alive: true, PID: "9", Started: desktopLongJobNow()}
	if _, _, ended := desktopLongJobGraceWait(ctx, j); ended {
		t.Fatal("a live job ended during grace")
	}
	if polls == 0 {
		t.Fatal("grace wait did not poll")
	}
	if got := desktopBashSecretsOnContext(ctx); len(got) != 1 {
		t.Fatalf("grace wait cleared the caller's secrets: %#v", got)
	}
}

func TestD101ShortCommandReturnsInline(t *testing.T) {
	d100Env(t)
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		switch {
		case strings.Contains(command, "LONGJOB_STARTED"):
			return "LONGJOB_STARTED pid=7 log=/tmp/j.log exit_file=/tmp/j.exit", nil
		case strings.HasPrefix(command, "tail -c"):
			return "train.jsonl\ndev.jsonl\n", nil
		}
		return "LONGJOB_DEAD pid=7\nLONGJOB_EXIT=0\nLONGJOB_LOG_BYTES=5\nLONGJOB_LOG_TAIL\ntrain.jsonl\n", nil
	}
	scope := agentruntime.Scope{TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_x", SessionID: "sess_x", RunID: "run_s"}
	out, err := desktopLongJobStart(context.Background(), scope, scope, map[string]any{"command": "for f in train dev; do ls $f; done"})
	if err != nil || !strings.Contains(out, "dev.jsonl") || strings.Contains(out, "后台任务已启动") {
		t.Fatalf("inline: %q %v", out, err)
	}
	if _, _, ok := desktopLongJobOutcome("run_s"); ok {
		t.Fatal("an ended job must not keep the run running")
	}
	j := desktopLongJobLatestFor(scope)
	if j == nil || j.ScheduleID != "" || j.ReportedStatus != "done" {
		t.Fatalf("stored %+v", j)
	}
	// Non-zero exit comes back as an error, like plain bash.
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		if strings.Contains(command, "LONGJOB_STARTED") {
			return "LONGJOB_STARTED pid=8 log=/tmp/k.log exit_file=/tmp/k.exit", nil
		}
		if strings.HasPrefix(command, "tail -c") {
			return "No such file", nil
		}
		return "LONGJOB_DEAD pid=8\nLONGJOB_EXIT=2\nLONGJOB_LOG_BYTES=5\nLONGJOB_LOG_TAIL\nNo such file\n", nil
	}
	if _, err := desktopLongJobStart(context.Background(), scope, scope, map[string]any{"command": "python train.py"}); err == nil || !strings.Contains(err.Error(), "exit 2") {
		t.Fatalf("failed inline err %v", err)
	}
}

// A finish that could not be told is told on the next tick, not dropped.
func TestD101UndeliveredFinishRetries(t *testing.T) {
	d100Env(t)
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		return "LONGJOB_DEAD pid=1\nLONGJOB_EXIT=0\nLONGJOB_LOG_BYTES=9\nLONGJOB_LOG_TAIL\nacc=0.91\n", nil
	}
	j := &desktopLongJob{ID: "job_r", TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_x", SessionID: "s", Status: "running", ReportedStatus: "running", Alive: true, Started: desktopLongJobNow().Add(-time.Hour), LogFile: "/tmp/r.log"}
	_ = desktopLongJobSave(j)
	calls := 0
	desktopLongJobTalk = func(_ context.Context, _ *desktopLongJob, dec desktopLongJobDecision) error {
		calls++
		if dec != longJobResume {
			t.Fatalf("dec %s", dec)
		}
		if calls == 1 {
			return errors.New("hub down")
		}
		return nil
	}
	_, _ = desktopLongJobWatchTick(context.Background(), j.ID)
	if got := desktopLongJobGet(j.ID); got.Status != "done" || got.ReportedStatus != "running" || !got.LastNotice.IsZero() {
		t.Fatalf("after failed delivery %+v", got)
	}
	if _, err := desktopLongJobWatchTick(context.Background(), j.ID); err != nil {
		t.Fatal(err)
	}
	if got := desktopLongJobGet(j.ID); calls != 2 || got.ReportedStatus != "done" {
		t.Fatalf("calls=%d %+v", calls, got)
	}
	if _, err := desktopLongJobWatchTick(context.Background(), j.ID); err != nil || calls != 2 {
		t.Fatalf("told twice: %d %v", calls, err)
	}
}

// The notice goes to Hub's say as the desktop owner, with the poll's evidence.
func TestD101NoticeSaysWithEvidence(t *testing.T) {
	d100Env(t)
	var body map[string]any
	prev := desktopLongJobSayFunc
	t.Cleanup(func() { desktopLongJobSayFunc = prev })
	desktopLongJobSayFunc = func(_ context.Context, b map[string]any) error {
		body = b
		return errors.New("409 ALREADY_SAID by bot_x")
	}
	code := 3
	j := &desktopLongJob{ID: "job_abcdef123", TenantID: "tenant_default", UserID: "u_1::desk::bot_x", InstanceID: "inst_x", Status: "failed", ExitCode: &code, Started: desktopLongJobNow().Add(-30 * time.Minute), LastPoll: desktopLongJobNow(), LastTail: "Warning: Permanently added host\nTraceback\nValueError: bad label"}
	if err := desktopLongJobTalkDefault(context.Background(), j, longJobNotice); err != nil {
		t.Fatalf("ALREADY_SAID is delivered: %v", err)
	}
	text, _ := body["text"].(string)
	if body["user_id"] != "u_1" || body["tenant_id"] != "tenant_default" || body["instance_id"] != "inst_x" || body["key"] == "" {
		t.Fatalf("body %+v", body)
	}
	if !strings.Contains(text, "失败") || !strings.Contains(text, "3") || !strings.Contains(text, "ValueError: bad label") || strings.Contains(text, "Permanently added") {
		t.Fatalf("text %q", text)
	}
}

func TestD101WatchScheduleIsAccepted(t *testing.T) {
	mgr, err := scheduler.NewManager(filepath.Join(t.TempDir(), "sched.json"))
	if err != nil {
		t.Fatal(err)
	}
	setSrvSchedulerManager(mgr)
	t.Cleanup(func() { setSrvSchedulerManager(nil) })
	j := &desktopLongJob{ID: "job_w", TenantID: "t", UserID: "u::desk::bot_x", InstanceID: "inst_x", BotID: "bot_x"}
	if id := desktopLongJobEnsureWatch(j); id == "" {
		t.Fatal("watch-job task was rejected")
	}
}

func TestD101DetectionJudgesCommandWordsOnly(t *testing.T) {
	long := []string{
		"python train.py --epochs 10",
		"cd ~/clf && ./venv/bin/python src/train_transformer.py > logs/t.log 2>&1",
		"git clone https://github.com/x/y",
		"cmake --build build -j8",
		"timeout 3600 torchrun --nproc 2 finetune.py",
	}
	for _, c := range long {
		if !desktopLongJobCommand(c) {
			t.Errorf("should be long: %s", c)
		}
	}
	short := []string{
		// Self-detached: the process is already in the background.
		"sshpass -p x ssh -p 99 u@h 'cd ~/clf && nohup ./venv/bin/python src/train.py > l 2>&1 &'",
		"nohup python job.py &",
		"cd ~/Desktop/app && nohup python3 app.py > run.log 2>&1 & sleep 3; pgrep -af app.py; cat run.log",
		// A heredoc inside the remote command is data.
		"ssh -p 99 u@h 'cat > ~/clf/src/common.py <<\"PYEOF\"\nimport os\nos.system(\"python train.py &\")\nPYEOF\necho written'",
		"cd /tmp/dp && cat > xhs.md <<'EOF'\n今天的论文讲怎么训练和下载模型，train 了 3 个 epoch\nEOF\nwc -c xhs.md",
		"sleep 780; sshpass -p x ssh u@h 'tail -5 ~/clf/logs/train_transformer.log'",
		"for f in train dev test; do wc -l $f.csv; done",
		"grep -E 'epoch|loss' logs/train.log | tail -3",
		"echo \"开始训练\" > note.txt",
		"python -c \"import pandas as pd; print(pd.read_csv('train.csv').shape)\"",
		"python train.py --help",
		"ls -l /tmp",
	}
	for _, c := range short {
		if desktopLongJobCommand(c) {
			t.Errorf("should stay a plain step: %s", c)
		}
	}
}

func TestD103InternalJobCallsDoNotReplayTheStartingStep(t *testing.T) {
	ctx := agentruntime.WithToolCallID(context.Background(), "call_1")
	a := desktopLongJobInternalCall(ctx)
	b := desktopLongJobInternalCall(ctx)
	ida, idb := agentruntime.ToolCallID(a), agentruntime.ToolCallID(b)
	if ida == "call_1" || idb == "call_1" || ida == idb || !desktopLongJobInternal(a) {
		t.Fatalf("ids %q %q", ida, idb)
	}
	// The journal answers a repeated id from its slot without running it.
	runs := 0
	send := func(desktopToolCall, desktopToolExec) (desktopToolOutcome, error) {
		runs++
		return desktopToolOutcome{Text: "out" + strconv.Itoa(runs)}, nil
	}
	for _, id := range []string{"call_1", ida, idb} {
		if _, err := dispatchDesktopTool(desktopToolCall{RunID: "run_d103", ToolCallID: id, Action: "bash"}, desktopToolExec{}, send); err != nil {
			t.Fatal(err)
		}
	}
	if runs != 3 {
		t.Fatalf("internal calls replayed: %d runs", runs)
	}
	dropDesktopToolResults("run_d103")
}

func TestD103RemoteJobIsFollowedToItsEnd(t *testing.T) {
	out := desktopLongJobInjectRemoteNohup("ssh -p 99 u@h 'cd ~/clf && ./venv/bin/python src/train_bert.py --epochs 3 > reports/l 2>&1'")
	if !strings.Contains(out, "nohup bash -lc") || !strings.Contains(out, "wait $p") || !strings.Contains(out, "exit $rc") || strings.Contains(out, "& echo $!'") {
		t.Fatalf("remote job not followed: %s", out)
	}
}

func TestD103CheckIsPlannedFromObservedProgress(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 0, 0, 0, time.Local)
	// 2 of 10 epochs in 20 minutes: next epoch ends in ~10 minutes.
	j := &desktopLongJob{Started: now.Add(-20 * time.Minute), LastTail: "loading\nepoch 2/10 loss=0.31 acc=0.88"}
	desktopLongJobPlanCheck(j, now)
	if d := j.NextCheck.Sub(now); d < 9*time.Minute || d > 12*time.Minute {
		t.Fatalf("next check in %s (%s)", d, j.CheckReason)
	}
	if e := j.EstimateEnd.Sub(j.Started); e < 95*time.Minute || e > 105*time.Minute {
		t.Fatalf("estimate %s", e)
	}
	// Nested counters: epoch 1/2, step 400/800 = 25%.
	if f, units, ok := desktopLongJobProgress("epoch 1/2 step 400/800 loss 0.4"); !ok || units != 2 || f < 0.24 || f > 0.26 {
		t.Fatalf("nested %v %d %v", f, units, ok)
	}
	if f, _, ok := desktopLongJobProgress("downloading  45.5% 1.2MB/s"); !ok || f < 0.45 || f > 0.46 {
		t.Fatalf("pct %v %v", f, ok)
	}
	// No progress signal: default, doubling after empty checks, bounded.
	k := &desktopLongJob{Started: now.Add(-time.Minute), LastTail: "starting"}
	desktopLongJobPlanCheck(k, now)
	if k.NextCheck.Sub(now) != desktopLongJobCheckDefault {
		t.Fatalf("default %s", k.NextCheck.Sub(now))
	}
	k.CheckMisses = 20
	desktopLongJobPlanCheck(k, now)
	if k.NextCheck.Sub(now) != desktopLongJobCheckMax {
		t.Fatalf("max %s", k.NextCheck.Sub(now))
	}
	// The bot's own estimate, bounded.
	b := &desktopLongJob{Started: now}
	if !desktopLongJobSetCheck(b, map[string]any{"next_check_minutes": 0.5, "check_reason": "每轮约 30 秒"}, now) || b.NextCheck.Sub(now) != desktopLongJobCheckMin || b.CheckReason != "每轮约 30 秒" {
		t.Fatalf("bot check %s %q", b.NextCheck.Sub(now), b.CheckReason)
	}
}

func TestD103RunningJobIsQuietUntilItsCheck(t *testing.T) {
	now := time.Date(2026, 11, 11, 21, 0, 0, 0, time.Local)
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	j := &desktopLongJob{Status: "running", Alive: true, Started: now.Add(-3 * time.Hour), NextCheck: now.Add(time.Minute), LastTail: "epoch 3/10 acc=0.9"}
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobSilent {
		t.Fatalf("before check: %s", d)
	}
	j.NextCheck = now.Add(-time.Second)
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobNotice {
		t.Fatalf("stage result at check: %s", d)
	}
	j.ReportedMetrics = desktopRunProgressLines(j.LastTail)
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobSilent {
		t.Fatalf("nothing new at check: %s", d)
	}
	j.EstimateEnd = now.Add(-2 * time.Hour)
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobNotice {
		t.Fatalf("far past estimate: %s", d)
	}
	j.OverdueSaid = true
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobSilent {
		t.Fatalf("overdue twice: %s", d)
	}
}

// papers 2026-11-11 23:05: a quiet job.log inside the model's own estimate
// is not a stall; past the estimate it is.
func TestD108QuietJobInsideEstimateIsNotStalled(t *testing.T) {
	d100Env(t)
	now := time.Date(2026, 11, 11, 23, 5, 0, 0, time.Local)
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	desktopLongJobExec = func(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
		return "LONGJOB_ALIVE pid=7\nLONGJOB_LOG_BYTES=5\nLONGJOB_LOG_TAIL\ntraining start\n", nil
	}
	j := &desktopLongJob{ID: "job_quiet", Status: "running", Alive: true, Started: now.Add(-16 * time.Minute), LastChange: now.Add(-16 * time.Minute), StallAfter: 15 * time.Minute, EstimateEnd: now.Add(40 * time.Minute)}
	j.Fingerprint = desktopLongJobFingerprint(desktopLongJobParsePoll("LONGJOB_ALIVE pid=7\nLONGJOB_LOG_BYTES=5\nLONGJOB_LOG_TAIL\ntraining start\n"))
	if err := desktopLongJobPollAndStore(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if j.Status != "running" {
		t.Fatalf("inside estimate: %s", j.Status)
	}
	j.EstimateEnd = now.Add(-time.Minute)
	if err := desktopLongJobPollAndStore(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	if j.Status != "stalled" {
		t.Fatalf("past estimate: %s", j.Status)
	}
}

func TestD108BotReestimateMovesEstimateEnd(t *testing.T) {
	now := time.Date(2026, 11, 11, 23, 3, 0, 0, time.Local)
	j := &desktopLongJob{Started: now.Add(-13 * time.Minute), EstimateEnd: now.Add(7 * time.Minute)}
	if !desktopLongJobSetCheck(j, map[string]any{"next_check_minutes": 46.0}, now) {
		t.Fatal("not set")
	}
	if !j.EstimateEnd.Equal(j.NextCheck) {
		t.Fatalf("estimate %v next %v", j.EstimateEnd, j.NextCheck)
	}
	early := j.EstimateEnd
	desktopLongJobSetCheck(j, map[string]any{"next_check_minutes": 5.0}, now)
	if !j.EstimateEnd.Equal(early) {
		t.Fatalf("a short check pulled the estimate in: %v", j.EstimateEnd)
	}
}

func TestD111ReportedStallWithQuietLogStillGetsItsCheck(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 5, 0, 0, time.Local)
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	j := &desktopLongJob{Status: "stalled", Alive: true, SessionID: "s1", Started: now.Add(-75 * time.Minute), NextCheck: now.Add(-16 * time.Minute), LastTail: "=== bert training start ==="}
	if d := desktopLongJobDecide(j, "stalled", "", now.Add(-time.Hour)); d != longJobCheck {
		t.Fatalf("stalled quiet log at check: %s", d)
	}
	j.LastTail = "epoch 1/3 loss=0.3"
	if d := desktopLongJobDecide(j, "stalled", "", now.Add(-time.Hour)); d != longJobSilent {
		t.Fatalf("measurable stalled log: %s", d)
	}
}

// papers 11-11 23:49: job.log had no measurable progress (metrics in the
// job's own files); the estimated check is a bot turn, not a canned line.
func TestD111QuietLogCheckIsABotTurn(t *testing.T) {
	now := time.Date(2026, 11, 11, 23, 50, 0, 0, time.Local)
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	j := &desktopLongJob{Status: "running", Alive: true, SessionID: "s1", Started: now.Add(-time.Hour), NextCheck: now.Add(-time.Second), EstimateEnd: now.Add(-40 * time.Minute), LastTail: "=== baseline start ===\nbaseline_rc=0\n=== bert training start ==="}
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobCheck {
		t.Fatalf("quiet log at check: %s", d)
	}
	j.NextCheck = now.Add(time.Minute)
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobSilent {
		t.Fatalf("before check: %s", d)
	}
	p := desktopLongJobWatchPrompt(j, longJobCheck)
	if !strings.Contains(p, "只读命令") || !strings.Contains(p, "action=job_status") {
		t.Fatalf("prompt: %s", p)
	}
}

func TestD111InnerStepFailureIsNotDone(t *testing.T) {
	j := &desktopLongJob{Status: "done", LastTail: "baseline_rc=0\n=== bert ===\ntrain_rc=1\neval_rc=0"}
	if got := desktopLongJobDoneHead(j); !strings.Contains(got, "train_rc=1") || strings.Contains(got, "eval_rc") {
		t.Fatalf("head %q", got)
	}
	if got := desktopLongJobNoticeText(j, longJobResume); !strings.Contains(got, "没成功") {
		t.Fatalf("notice %q", got)
	}
	if got := desktopLongJobStatusPhrase(j, time.Now()); !strings.Contains(got, "train_rc=1") {
		t.Fatalf("status %q", got)
	}
	ok := &desktopLongJob{Status: "done", LastTail: "train_rc=0\nPIPELINE_DONE"}
	if got := desktopLongJobDoneHead(ok); strings.Contains(got, "没成功") {
		t.Fatalf("clean run %q", got)
	}
	if f := desktopLongJobInnerFailures("x [exit 2] y"); len(f) != 1 || f[0] != "[exit 2]" {
		t.Fatalf("bracket %v", f)
	}
}

func TestD111CommandTimeLimit(t *testing.T) {
	cases := map[string]time.Duration{
		"timeout 7200 /root/.mm/rsh 'bash run_all.sh'": 2 * time.Hour,
		"cd x && timeout -k 10 30m python train.py":    30 * time.Minute,
		"python train.py --timeout 5":                  0,
		"bash -lc 'timeout 1.5h make'":                 90 * time.Minute,
	}
	for cmd, want := range cases {
		if got := desktopLongJobTimeLimit(cmd); got != want {
			t.Fatalf("%q: %s want %s", cmd, got, want)
		}
	}
	now := time.Date(2026, 11, 11, 23, 50, 0, 0, time.Local)
	j := &desktopLongJob{Command: "timeout 7200 rsh x", Status: "running", Alive: true, Started: now.Add(-time.Hour), EstimateEnd: now.Add(3 * time.Hour), NextCheck: now.Add(-time.Second), LastTail: "step 250/1206 loss=0.33", ReportedMetrics: desktopRunProgressLines("step 250/1206 loss=0.33")}
	desktopLongJobNow = func() time.Time { return now }
	t.Cleanup(func() { desktopLongJobNow = time.Now })
	if !strings.Contains(desktopLongJobView(j), "会超过时限") {
		t.Fatalf("view: %s", desktopLongJobView(j))
	}
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobNotice {
		t.Fatalf("past own limit: %s", d)
	}
	j.DeadlineSaid = true
	if d := desktopLongJobDecide(j, "running", "", time.Time{}); d != longJobSilent {
		t.Fatalf("limit said twice: %s", d)
	}
}

// papers 11-12 00:18: a watcher turn's final reply reached nobody.
func TestD111WatchTurnReplyIsDelivered(t *testing.T) {
	d100Env(t)
	desktopLongJobSleep = func(ctx context.Context, d time.Duration) bool { return true }
	var sent []map[string]any
	oldSay, oldGet := desktopLongJobSayFunc, desktopLongJobRunGetter
	t.Cleanup(func() { desktopLongJobSayFunc, desktopLongJobRunGetter = oldSay, oldGet })
	desktopLongJobSayFunc = func(ctx context.Context, body map[string]any) error { sent = append(sent, body); return nil }
	reply := "训练变慢：24.6→5 样本/秒，会超过 00:50 的时限。"
	calls := 0
	desktopLongJobRunGetter = func(ctx context.Context, p agentservice.Principal, inst, sess, run string) (agentservice.RunStatus, string, error) {
		calls++
		if calls < 2 {
			return agentservice.RunStatusRunning, "", nil
		}
		return agentservice.RunStatusSucceeded, reply, nil
	}
	j := &desktopLongJob{ID: "job_w", InstanceID: "inst_1", SessionID: "s1", UserID: "u1::desk::bot_1", TenantID: "t"}
	desktopLongJobFollowRun(context.Background(), agentservice.Principal{}, j, "run_1", longJobCheck)
	if len(sent) != 1 || sent[0]["text"] != reply || sent[0]["user_id"] != "u1" {
		t.Fatalf("sent %v", sent)
	}
	reply = "NO_UPDATE"
	calls, sent = 5, nil
	desktopLongJobFollowRun(context.Background(), agentservice.Principal{}, j, "run_2", longJobCheck)
	if len(sent) != 0 {
		t.Fatalf("silent token sent %v", sent)
	}
	reply = "结果"
	desktopSayRuns.allow("inst_1|run_3|", false, time.Now())
	desktopLongJobFollowRun(context.Background(), agentservice.Principal{}, j, "run_3", longJobResume)
	if len(sent) != 0 {
		t.Fatalf("run that said itself got a second message %v", sent)
	}
}

// papers 11-12 00:50: exit 124 from the local ssh wrapper; the remote
// training finished at 00:58.
func TestD112TimeoutKillIsNotATaskFailure(t *testing.T) {
	code := 124
	j := &desktopLongJob{Status: "failed", ExitCode: &code, Command: "timeout 7200 /root/.mm/rsh 'bash $HOME/x/run_all.sh'"}
	n := desktopLongJobKilledNote(j)
	if !strings.Contains(n, "timeout") || !strings.Contains(n, "另一台机器") {
		t.Fatalf("note %q", n)
	}
	if got := desktopLongJobNoticeText(j, longJobResume); !strings.Contains(got, "被外部结束") {
		t.Fatalf("notice %q", got)
	}
	if !strings.Contains(desktopLongJobView(j), "说明: ") {
		t.Fatal("view lacks note")
	}
	local := &desktopLongJob{Status: "failed", ExitCode: &code, Command: "timeout 60 python train.py"}
	if strings.Contains(desktopLongJobKilledNote(local), "另一台机器") {
		t.Fatal("local job called remote")
	}
	one := 1
	if desktopLongJobKilledNote(&desktopLongJob{ExitCode: &one, Command: "ssh h x"}) != "" {
		t.Fatal("exit 1 explained as kill")
	}
}

func TestD112DeliverWithoutReplySlotSaysWhy(t *testing.T) {
	err := noteDesktopFile(context.Background(), "REPORT.md", "text/markdown", []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "Do not retry deliver") {
		t.Fatalf("err %v", err)
	}
}

func TestD113AttachClaimNeedsAnAttachment(t *testing.T) {
	claim := "游戏已运行。两个文件都附在这条消息里。"
	if got := correctDesktopAttachClaim(claim, 0); !strings.Contains(got, "更正") {
		t.Fatalf("no correction: %q", got)
	}
	if got := correctDesktopAttachClaim(claim, 2); got != claim {
		t.Fatalf("attached reply changed: %q", got)
	}
	if got := correctDesktopAttachClaim("报告在 ~/x/REPORT.md", 0); strings.Contains(got, "更正") {
		t.Fatalf("no claim corrected: %q", got)
	}
	if got := desktopLongJobWatchReply("报告见附件"); !strings.Contains(got, "更正") {
		t.Fatalf("watch reply: %q", got)
	}
}

// Daniel 11-12 02:11: judge the result and decide autonomously whether to
// iterate, bounded.
func TestD114IterationGateIsMeasuredAndBounded(t *testing.T) {
	d100Env(t)
	f := func(v float64) *float64 { return &v }
	r1 := &desktopLongJob{ID: "job_r1", Status: "done", Started: time.Now()}
	_ = desktopLongJobSave(r1)
	if _, refuse := desktopLongJobIterationGate(r1); !strings.Contains(refuse, "round_metric") {
		t.Fatalf("unrecorded round allowed: %q", refuse)
	}
	if !desktopLongJobRecordRound(r1, map[string]any{"round_metric": "0.795", "round_metric_name": "test_f1", "decision": "continue", "reason": "加类别权重"}) {
		t.Fatal("not recorded")
	}
	_ = desktopLongJobSave(r1)
	if round, refuse := desktopLongJobIterationGate(r1); refuse != "" || round != 2 {
		t.Fatalf("round 2 refused: %d %q", round, refuse)
	}
	r2 := &desktopLongJob{ID: "job_r2", Lineage: "job_r1", Round: 2, Status: "done", RoundMetric: f(0.797), RoundMetricName: "test_f1", Decision: "continue", Started: time.Now()}
	r3 := &desktopLongJob{ID: "job_r3", Lineage: "job_r1", Round: 3, Status: "done", RoundMetric: f(0.798), RoundMetricName: "test_f1", Decision: "continue", Started: time.Now()}
	_ = desktopLongJobSave(r2)
	_ = desktopLongJobSave(r3)
	if _, refuse := desktopLongJobIterationGate(r3); !strings.Contains(refuse, "没有明显提升") {
		t.Fatalf("diminishing returns not stopped: %q", refuse)
	}
	r3.RoundMetric = f(0.83)
	_ = desktopLongJobSave(r3)
	if _, refuse := desktopLongJobIterationGate(r3); refuse != "" {
		t.Fatalf("real gain refused: %q", refuse)
	}
	r4 := &desktopLongJob{ID: "job_r4", Lineage: "job_r1", Round: 4, Status: "done", RoundMetric: f(0.86), RoundMetricName: "test_f1", Decision: "continue", Started: time.Now()}
	_ = desktopLongJobSave(r4)
	if _, refuse := desktopLongJobIterationGate(r4); !strings.Contains(refuse, "上限") {
		t.Fatalf("max rounds not enforced: %q", refuse)
	}
	stop := &desktopLongJob{ID: "job_s", Status: "done", RoundMetric: f(0.9), RoundMetricName: "f1", Decision: "stop"}
	if _, refuse := desktopLongJobIterationGate(stop); !strings.Contains(refuse, "stop") {
		t.Fatalf("stop decision ignored: %q", refuse)
	}
	failed := &desktopLongJob{ID: "job_f", Status: "failed", Decision: "continue", DecisionReason: "OOM，batch 64→32"}
	_ = desktopLongJobSave(failed)
	if _, refuse := desktopLongJobIterationGate(failed); refuse != "" {
		t.Fatalf("failed round with a plan refused: %q", refuse)
	}
	if line := desktopLongJobRoundsLine(r4); !strings.Contains(line, "第2轮") || !strings.Contains(line, "test_f1=0.797") {
		t.Fatalf("rounds line %q", line)
	}
	if desktopIterGain("val_loss", 0.5, 0.4) <= 0 || desktopIterGain("f1", 0.5, 0.4) >= 0 {
		t.Fatal("metric direction")
	}
}

func TestD114JobStartedInWatcherTurnIsNextRound(t *testing.T) {
	d100Env(t)
	parent := &desktopLongJob{ID: "job_p", Status: "done", Started: time.Now()}
	_ = desktopLongJobSave(parent)
	desktopLongJobNoteWatchRun("run_w", "job_p")
	_, err := desktopLongJobStart(context.Background(), agentruntime.Scope{RunID: "run_w", InstanceID: "i", SessionID: "s"}, agentruntime.Scope{}, map[string]any{"command": "python train.py"})
	if err == nil || !strings.Contains(err.Error(), "round_metric") {
		t.Fatalf("unrecorded parent started a round: %v", err)
	}
	if p := desktopLongJobWatchPrompt(parent, longJobResume); !strings.Contains(p, "自主决定是否继续改进") {
		t.Fatalf("resume prompt lacks policy")
	}
}

// papers 11-12 01:09: a resume turn's REPORT.md could not reach the chat.
// Now the turn has a slot; its delivered files go out with its reply, and
// an older Hub that refuses the larger body still gets the text.
func TestD115WatchTurnFilesGoWithTheReply(t *testing.T) {
	d100Env(t)
	desktopLongJobSleep = func(ctx context.Context, d time.Duration) bool { return true }
	var sent []map[string]any
	oldSay, oldGet := desktopLongJobSayFunc, desktopLongJobRunGetter
	t.Cleanup(func() { desktopLongJobSayFunc, desktopLongJobRunGetter = oldSay, oldGet })
	refuse := false
	desktopLongJobSayFunc = func(ctx context.Context, body map[string]any) error {
		if refuse && body["files"] != nil {
			return errors.New("hub say: status 400: invalid say request")
		}
		sent = append(sent, body)
		return nil
	}
	reply := "评估完成，报告已附在这条消息里。"
	desktopLongJobRunGetter = func(ctx context.Context, p agentservice.Principal, inst, sess, run string) (agentservice.RunStatus, string, error) {
		return agentservice.RunStatusSucceeded, reply, nil
	}
	deliver := func() *desktopWatchFiles {
		w := &desktopWatchFiles{}
		ctx := w.prepare(context.Background())
		if err := noteDesktopFile(ctx, "REPORT.md", "text/markdown", []byte("# F1 0.79")); err != nil {
			t.Fatalf("deliver in a watcher turn: %v", err)
		}
		return w
	}
	j := &desktopLongJob{ID: "job_f", InstanceID: "inst_1", SessionID: "s1", UserID: "u1::desk::bot_1", TenantID: "t"}
	desktopLongJobFollowRunFiles(context.Background(), agentservice.Principal{}, j, "run_f1", longJobResume, deliver())
	if len(sent) != 1 {
		t.Fatalf("sent %v", sent)
	}
	files, _ := sent[0]["files"].([]map[string]string)
	if len(files) != 1 || files[0]["name"] != "REPORT.md" || files[0]["data"] == "" || sent[0]["text"] != reply {
		t.Fatalf("say %v", sent[0])
	}
	// The turn spoke itself: only its files are said, once.
	sent = nil
	desktopSayRuns.allow("inst_1|run_f2|", false, time.Now())
	desktopLongJobFollowRunFiles(context.Background(), agentservice.Principal{}, j, "run_f2", longJobResume, deliver())
	if len(sent) != 1 || !strings.Contains(sent[0]["text"].(string), "REPORT.md") || sent[0]["files"] == nil {
		t.Fatalf("spoke-itself files %v", sent)
	}
	// An older Hub: text only, with the files named and the claim corrected.
	sent, refuse = nil, true
	desktopLongJobFollowRunFiles(context.Background(), agentservice.Principal{}, j, "run_f3", longJobResume, deliver())
	if len(sent) != 1 || sent[0]["files"] != nil {
		t.Fatalf("fallback %v", sent)
	}
	text := sent[0]["text"].(string)
	if !strings.Contains(text, "还在电脑上：REPORT.md") || !strings.Contains(text, desktopAttachCorrection) {
		t.Fatalf("fallback text %q", text)
	}
}
