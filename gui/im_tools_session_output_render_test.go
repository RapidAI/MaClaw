package main

import (
	"strings"
	"testing"
)

func TestSnapshotSessionOutputCopiesRawLines(t *testing.T) {
	session := &RemoteSession{
		Status:         SessionBusy,
		Summary:        SessionSummary{CurrentTask: "task"},
		RawOutputLines: []string{"one", "two"},
	}

	snapshot := snapshotSessionOutput(session)
	session.RawOutputLines[0] = "changed"

	if snapshot.Status != string(SessionBusy) {
		t.Fatalf("Status = %q, want %q", snapshot.Status, SessionBusy)
	}
	if snapshot.Summary.CurrentTask != "task" {
		t.Fatalf("CurrentTask = %q, want task", snapshot.Summary.CurrentTask)
	}
	if got := snapshot.RawLines[0]; got != "one" {
		t.Fatalf("RawLines[0] = %q, want copied value one", got)
	}
}

func TestRenderSessionOutputIncludesSummaryAndLimitsRawLines(t *testing.T) {
	out := renderSessionOutput("s1", 2, sessionOutputSnapshot{
		Status: string(SessionWaitingInput),
		Summary: SessionSummary{
			CurrentTask:     "build",
			ProgressSummary: "halfway",
			LastResult:      "ok",
			LastCommand:     "go test",
			WaitingForUser:  true,
			SuggestedAction: "continue",
		},
		RawLines: []string{"line1", "line2", "line3"},
	}, sessionOutputHintFacts{Status: SessionWaitingInput, CompletionLevel: CompletionCompleted})

	for _, want := range []string{
		"会话 s1 状态: waiting_input",
		"当前任务: build",
		"进度: halfway",
		"最近结果: ok",
		"最近命令: go test",
		"建议操作: continue",
		"--- 最近 2 行输出 ---",
		"line2",
		"line3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "line1") {
		t.Fatalf("rendered output included trimmed raw line:\n%s", out)
	}
}

func TestRenderSessionOutputNoOutputUsesHintFacts(t *testing.T) {
	out := renderSessionOutput("s1", 2, sessionOutputSnapshot{
		Status: string(SessionRunning),
	}, sessionOutputHintFacts{Status: SessionRunning})

	if !strings.Contains(out, "(暂无输出)") || !strings.Contains(out, "send_and_observe") {
		t.Fatalf("rendered no-output hint missing expected guidance:\n%s", out)
	}
}
