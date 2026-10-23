package guiapp

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

func TestLLMRetryProgressActivity(t *testing.T) {
	cases := []struct {
		text string
		want llmRetryActivity
	}{
		{text: "[LiveStatus:llm-retry] 正在重新请求模型（第 2 次）", want: llmRetryActivityArm},
		{text: "[LiveStatus:llm-retry] 官方额度约 12 分钟后恢复，到时继续当前任务。", want: llmRetryActivityArm},
		{text: "__heartbeat__", want: llmRetryActivityNone},
		{text: "[Status] 正在准备执行路径", want: llmRetryActivityNone},
		{text: "〔进度〕收到，正在处理", want: llmRetryActivityNone},
		{text: "收到，正在处理", want: llmRetryActivityNone},
		{text: "命令仍在执行中：compile", want: llmRetryActivityNone},
		{text: "〔进度〕仍在执行中，整理结果", want: llmRetryActivityDisarm},
		{text: "工具 · 执行命令 (bash)\nls", want: llmRetryActivityDisarm},
		{text: "正在读取文件", want: llmRetryActivityDisarm},
	}
	for _, tc := range cases {
		if got := llmRetryProgressActivity(tc.text); got != tc.want {
			t.Fatalf("activity(%q)=%v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestRetryActivityNotesUntilReleased(t *testing.T) {
	var notes atomic.Int32
	stop := startRetryActivityNotes(func() { notes.Add(1) }, 5*time.Millisecond)
	deadline := time.Now().Add(200 * time.Millisecond)
	for notes.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	stop()
	if notes.Load() < 2 {
		t.Fatalf("notes=%d, want the immediate note and at least one repeat", notes.Load())
	}
	stoppedAt := notes.Load()
	time.Sleep(40 * time.Millisecond)
	if got := notes.Load(); got > stoppedAt+1 {
		t.Fatalf("notes kept firing after release: %d -> %d", stoppedAt, got)
	}
}

func TestObserveLLMRetryProgressForwardsUntilTheTurnEnds(t *testing.T) {
	prev := llmRetryActivityNoteInterval
	llmRetryActivityNoteInterval = 5 * time.Millisecond
	t.Cleanup(func() { llmRetryActivityNoteInterval = prev })

	h := &IMMessageHandler{interruptHandler: newIMInterruptHandler(nil)}
	tracker := progress.NewAgentProgressTracker(func(string) {}, "convert the scanned pdf", "", nil)
	t.Cleanup(tracker.Stop)
	h.interruptHandler.SetTracker("user", tracker)

	var got []string
	onProgress, release := observeLLMRetryProgress(h, "user", func(text string) {
		got = append(got, text)
	})
	onProgress(agent.LLMRetryInFlightStatus(2))
	onProgress("__heartbeat__")
	onProgress("[Status] 正在准备执行路径")
	onProgress("〔进度〕收到，正在处理")
	onProgress("正在读取文件")
	release()
	release()
	if len(got) != 5 {
		t.Fatalf("forwarded %d lines, want every progress line including the hidden ones", len(got))
	}
}
