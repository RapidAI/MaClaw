package main

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
