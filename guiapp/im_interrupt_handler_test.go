package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/progress"
)

type interruptTestEmbedder struct{}

func (interruptTestEmbedder) Embed(string) ([]float32, error) { return []float32{1, 0}, nil }
func (interruptTestEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{1, 0}
	}
	return result, nil
}
func (interruptTestEmbedder) Dim() int { return 2 }
func (interruptTestEmbedder) Close()   {}

func TestInterruptQueuesIndependentSameDomainRequestWithoutEmbedding(t *testing.T) {
	const userID = "im:user-1"
	handler := &IMMessageHandler{}
	handler.setSessionLoopCtx(userID, NewLoopContext("active-task", 3, nil))

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "测试 GPT-0430-MoE 模型的性能", "unknown", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	result := interrupt.TryInterrupt(userID, "测试 GPT-0430-MoE 这个模型的性能，只测 1 并发，输入 1k 长度")
	if result.Action != progress.ActionQueue || !result.Queued || result.Handled {
		t.Fatalf("independent same-domain request = %+v, want queued without handling", result)
	}
	if _, injected := handler.pendingInjection.Load(userID); injected {
		t.Fatal("independent request must not be injected into the active task")
	}
}

func TestInterruptMergeRequestsReplan(t *testing.T) {
	const userID = "im:user-merge"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	interrupt.SetEmbedder(interruptTestEmbedder{})
	tracker := progress.NewAgentProgressTracker(nil, "实现登录页面", "coding", nil)
	defer tracker.Stop()
	tracker.Buffer().SetTaskEmbed([]float32{1, 0})
	interrupt.SetTracker(userID, tracker)

	result := interrupt.TryInterrupt(userID, "登录页面增加记住我复选框")
	if result.Action != progress.ActionMerge || !result.Handled {
		t.Fatalf("interrupt result = %+v, want handled merge", result)
	}
	if ctx.ReplanRevision() != 1 {
		t.Fatalf("replan revision = %d, want 1", ctx.ReplanRevision())
	}
	if raw, ok := handler.pendingInjection.Load(userID); !ok || raw == "" {
		t.Fatal("merged message was not retained as a pending injection")
	}
}

func TestOldTrackerCleanupDoesNotRemoveReplacementTracker(t *testing.T) {
	const userID = "im:tracker-replacement"
	handler := &IMMessageHandler{}
	interrupt := newIMInterruptHandler(handler)
	oldTracker := progress.NewAgentProgressTracker(nil, "old task", "coding", nil)
	defer oldTracker.Stop()
	newTracker := progress.NewAgentProgressTracker(nil, "new task", "coding", nil)
	defer newTracker.Stop()

	interrupt.SetTracker(userID, oldTracker)
	interrupt.SetTracker(userID, newTracker)
	interrupt.ClearTrackerIfCurrent(userID, oldTracker)
	if got, ok := interrupt.milestoneTrackers.Load(userID); !ok || got != newTracker {
		t.Fatalf("tracker after old cleanup = %v, want replacement tracker", got)
	}
	interrupt.ClearTrackerIfCurrent(userID, newTracker)
	if _, ok := interrupt.milestoneTrackers.Load(userID); ok {
		t.Fatal("current tracker was not removed during its own cleanup")
	}
}

var _ embedding.Embedder = interruptTestEmbedder{}

// TestInterruptSlashCommandWithAttachmentStillExecutes pins the ordering of
// strip-then-classify: a control command sent together with an attachment
// previously fell through to the scheduler (exact-match "/stop" failed
// against the trailing host-injected block) and could be queued behind the
// very task it meant to stop. After stripping, the command must execute.
func TestInterruptSlashCommandWithAttachmentStillExecutes(t *testing.T) {
	const userID = "im:user-slash-with-attachment"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "开发一个图形界面版的贪吃蛇", "coding", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	composed := "/stop\n\n" +
		"[用户选择的本地文件路径]\n" +
		"C:\\Users\\ma139\\.maclaw\\temp\\paste_20261006_075303_62e6.png"

	result := interrupt.TryInterrupt(userID, composed)
	if !result.Handled || result.Action != progress.ActionReplace {
		t.Fatalf("/stop with attachment must execute as cancel, got: %+v", result)
	}
}

// TestInterruptImageAttachmentBoilerplateDoesNotCancelTask reproduces the
// 2026-10-06 incident: a UI-modification request ("改进界面风格，现在太厚重了：")
// sent with an attached screenshot arrived at TryInterrupt with the desktop
// file picker block appended. The block's English boilerplate contains
// "do not", which DetectNegation classified as cancel intent — the running
// coding task was cancelled and the workflow step marked skipped. The
// scheduler must judge signals on the user-authored text only.
func TestInterruptImageAttachmentBoilerplateDoesNotCancelTask(t *testing.T) {
	const userID = "im:user-attach-boilerplate"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "开发一个图形界面版的贪吃蛇", "coding", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	composed := "改进界面风格，现在太厚重了：\n\n" +
		"[用户选择的本地文件路径]\n" +
		"C:\\Users\\ma139\\.maclaw\\temp\\paste_20261006_075303_62e6.png\n" +
		"For image files, the host sends them directly to a vision-capable model when available. " +
		"Analyze attached images first; do not re-capture them or use read_file on image bytes. " +
		"Use OCR only for exact text when needed."

	result := interrupt.TryInterrupt(userID, composed)
	if result.Action == progress.ActionReplace {
		t.Fatalf("attachment boilerplate triggered replace: %+v", result)
	}
	if result.Handled && result.Action == progress.ActionReplace {
		t.Fatalf("task was cancelled by attachment boilerplate: %+v", result)
	}
	if ctx.IsCancelled() {
		t.Fatal("active loop was cancelled by attachment boilerplate")
	}
}

// TestInterruptOCRNoteContentDoesNotCancelTask covers the same failure class
// via a different injection source: the local OCR of a user screenshot is
// arbitrary content appended to the message between image_ocr markers. A
// screenshot containing English "cancel" must not read as cancel intent.
func TestInterruptOCRNoteContentDoesNotCancelTask(t *testing.T) {
	const userID = "im:user-ocr-negation"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "整理会议纪要", "coding", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	composed := "参考这张截图调整布局：\n\n" +
		"[图片 screenshot.png 的文字内容（本地 OCR 识别）]:\n" +
		"--- image_ocr: begin ---\n" +
		"cancel subscription do not renew\n" +
		"--- image_ocr: end ---"

	result := interrupt.TryInterrupt(userID, composed)
	if result.Action == progress.ActionReplace {
		t.Fatalf("OCR note content triggered replace: %+v", result)
	}
	if ctx.IsCancelled() {
		t.Fatal("active loop was cancelled by OCR note content")
	}
}

// TestInterruptHistoricalFileMarkerStrips verifies the history-annotated
// marker variant is also cut, keeping correction/overlap detection on
// user-authored text for restored sessions.
func TestInterruptHistoricalFileMarkerStrips(t *testing.T) {
	const userID = "im:user-hist-marker"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "开发一个图形界面版的贪吃蛇", "coding", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	composed := "改进界面风格，现在太厚重了：\n\n" +
		"[之前选择的本地文件路径（仅供参考，非本次上传）]\n" +
		"C:\\Users\\ma139\\.maclaw\\temp\\paste.png\n" +
		"For image files, the host sends them directly to a vision-capable model when available. " +
		"Analyze attached images first; do not re-capture them or use read_file on image bytes."

	result := interrupt.TryInterrupt(userID, composed)
	if result.Action == progress.ActionReplace {
		t.Fatalf("historical attachment marker triggered replace: %+v", result)
	}
	if ctx.IsCancelled() {
		t.Fatal("active loop was cancelled by historical attachment marker")
	}
}

// TestInterruptAttachmentOnlyMessageDoesNotSchedule verifies that dropping a
// file with no commentary falls through to normal serialization instead of
// being judged on injected boilerplate.
func TestInterruptAttachmentOnlyMessageDoesNotSchedule(t *testing.T) {
	const userID = "im:user-attach-only"
	handler := &IMMessageHandler{}
	ctx := NewLoopContext("active-task", 3, nil)
	handler.setSessionLoopCtx(userID, ctx)

	interrupt := newIMInterruptHandler(handler)
	tracker := progress.NewAgentProgressTracker(nil, "开发一个图形界面版的贪吃蛇", "coding", nil)
	defer tracker.Stop()
	interrupt.SetTracker(userID, tracker)

	composed := "[用户选择的本地文件路径]\n" +
		"C:\\Users\\ma139\\.maclaw\\temp\\paste_20261006_075303_62e6.png\n" +
		"For image files, the host sends them directly to a vision-capable model when available. " +
		"Analyze attached images first; do not re-capture them or use read_file on image bytes."

	result := interrupt.TryInterrupt(userID, composed)
	if result.Handled || result.Queued {
		t.Fatalf("attachment-only message was scheduled: %+v", result)
	}
	if ctx.IsCancelled() {
		t.Fatal("active loop was cancelled by attachment-only message")
	}
}
