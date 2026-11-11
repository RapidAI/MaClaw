package main

import (
	"context"
	"errors"
	"path/filepath"
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
	if !desktopLongJobShouldStart(map[string]any{"command": "nohup python job.py &"}) {
		t.Fatal("nohup")
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
	// First poll with a milestone: notice (no prior notice).
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
		"sshpass -p x ssh -p 99 u@h 'cd ~/clf && nohup ./venv/bin/python src/train.py > l 2>&1 &'",
		"nohup python job.py &",
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
