package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestDesktopRoundNoteOncePerRound(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-rounds-" + t.Name()}
	if n := desktopRoundNote(scope, "tail log", "[06:51] epoch 3 step 600/804 loss=0.0433", false); n != "" {
		t.Fatalf("a training step is not a round result: %q", n)
	}
	out := `[07:38:40] TEST(best epoch 3): {"accuracy": 0.8294, "precision": 0.7411, "recall": 0.91}`
	n := desktopRoundNote(scope, "tail log", out, false)
	if !strings.Contains(n, "round 1") || !strings.Contains(n, "accuracy=0.8294") || !strings.Contains(n, "round_metric") || !strings.Contains(n, "final reply") {
		t.Fatalf("test result must ask once for the record and the assessment, got %q", n)
	}
	if n := desktopRoundNote(scope, "tail log", out, false); n != "" {
		t.Fatalf("one nudge per round, got %q", n)
	}
	// The bot launches round 2 without recording: no second nudge for round 1.
	if n := desktopRoundNote(scope, "nohup .venv/bin/python train.py --lr 1e-5 > r2.log 2>&1 &", "", false); n != "" {
		t.Fatalf("round 1 was already nudged, got %q", n)
	}
	n = desktopRoundNote(scope, "tail r2.log", `final eval: f1=0.88`, false)
	if !strings.Contains(n, "round 2") {
		t.Fatalf("round 2 result must be nudged once, got %q", n)
	}
}

func TestDesktopRoundNoteSkipsRecordedRound(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-rounds-rec-" + t.Name()}
	desktopRoundNoteRecorded(scope.RunID)
	if n := desktopRoundNote(scope, "cat out", "test accuracy: 0.93", false); n != "" {
		t.Fatalf("recorded round must not be nudged, got %q", n)
	}
}

func TestDesktopRoundNoteOnLaunchAfterUnrecordedResult(t *testing.T) {
	scope := agentruntime.Scope{RunID: "run-rounds-launch-" + t.Name()}
	desktopRunRoundsMu.Lock()
	desktopRunRoundsLocked(scope.RunID).resultSeen = true
	desktopRunRoundsMu.Unlock()
	n := desktopRoundNote(scope, "python train.py", "后台任务已启动", true)
	if !strings.Contains(n, "new training round is starting") {
		t.Fatalf("launch after an unrecorded result must nudge, got %q", n)
	}
}

func TestDesktopRoundMetricsAndLaunch(t *testing.T) {
	if m := desktopRoundMetrics(`dev={"accuracy": 0.9453, "f1": 0.93} loss=0.04 accuracy=0.1`); m != "accuracy=0.9453, f1=0.93, loss=0.04" {
		t.Fatalf("metrics: %q", m)
	}
	if !desktopRoundLaunches("nohup python train.py &") || desktopRoundLaunches("python train.py") || desktopRoundLaunches("nohup python serve.py &") {
		t.Fatal("launch detection")
	}
	if len(desktopRoundResultLines("测试集 准确率: 0.91")) != 1 {
		t.Fatal("Chinese result line")
	}
}
