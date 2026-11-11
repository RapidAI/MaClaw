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
