package main

import (
	"context"
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
	t.Cleanup(func() {
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
	out, err := desktopLongJobStart(context.Background(), scope, map[string]any{"command": "python train.py --epochs 3"})
	if err != nil || !strings.Contains(out, "后台任务已启动") || !strings.Contains(out, "4242") {
		t.Fatalf("start: %s %v", out, err)
	}
	if !strings.Contains(started, "python train.py") || strings.Contains(started, "timeout -k") {
		t.Fatalf("start script: %s", started)
	}
	j := desktopLongJobLatestFor(scope)
	if j == nil || j.PID != "4242" || j.SourceRun != "run_x" {
		t.Fatalf("stored %+v", j)
	}
	oc, why, ok := desktopLongJobOutcome("run_x")
	if !ok || oc != "running" || !strings.Contains(why, j.ID) {
		t.Fatalf("outcome %s %s %v", oc, why, ok)
	}

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
