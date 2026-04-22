package progress

import (
	"sync"
	"testing"
	"time"
)

func TestAgentProgressTracker_BasicFlow(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	onProgress := func(text string) {
		mu.Lock()
		messages = append(messages, text)
		mu.Unlock()
	}

	tracker := NewAgentProgressTracker(onProgress, "开发贪吃蛇游戏", "coding", nil)
	defer tracker.Stop()

	// Coding task → ComplexityHeavy → immediate ack on first milestone.
	tracker.RecordToolCall("bash", `{"command": "mkdir game"}`, true)

	mu.Lock()
	count := len(messages)
	mu.Unlock()

	// Should have at least the ack message.
	if count < 1 {
		t.Fatalf("expected at least 1 message (ack), got %d", count)
	}

	mu.Lock()
	first := messages[0]
	mu.Unlock()

	if first != "收到，正在处理 🔄" {
		t.Fatalf("expected ack message, got %q", first)
	}
}

func TestAgentProgressTracker_LightTask_NoAck(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	onProgress := func(text string) {
		mu.Lock()
		messages = append(messages, text)
		mu.Unlock()
	}

	// Short message + non-coding intent → ComplexityLight → no ack.
	tracker := NewAgentProgressTracker(onProgress, "天气", "search", nil)
	defer tracker.Stop()

	tracker.RecordToolCall("web_search", `{"query": "杭州天气"}`, true)

	// Give a moment for any async delivery.
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count := len(messages)
	mu.Unlock()

	// Light task: no ack, no milestone push (merge window not expired).
	if count != 0 {
		mu.Lock()
		t.Fatalf("expected 0 messages for light task, got %d: %v", count, messages)
		mu.Unlock()
	}
}

func TestAgentProgressTracker_SilentToolsIgnored(t *testing.T) {
	var mu sync.Mutex
	var messages []string
	onProgress := func(text string) {
		mu.Lock()
		messages = append(messages, text)
		mu.Unlock()
	}

	tracker := NewAgentProgressTracker(onProgress, "开发游戏", "coding", nil)
	defer tracker.Stop()

	// Silent tools should not produce milestones or trigger ack.
	tracker.RecordToolCall("read_file", `{"path": "main.go"}`, true)
	tracker.RecordToolCall("memory", `{"action": "recall"}`, true)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	count := len(messages)
	mu.Unlock()

	if count != 0 {
		mu.Lock()
		t.Fatalf("expected 0 messages for silent tools, got %d: %v", count, messages)
		mu.Unlock()
	}
}

func TestAgentProgressTracker_BufferAccessible(t *testing.T) {
	tracker := NewAgentProgressTracker(nil, "test task", "coding", []float32{1, 2, 3})
	defer tracker.Stop()

	buf := tracker.Buffer()
	if buf == nil {
		t.Fatal("expected non-nil buffer")
	}
	if buf.TaskDesc() != "test task" {
		t.Fatalf("expected 'test task', got %q", buf.TaskDesc())
	}
	if buf.TaskIntent() != "coding" {
		t.Fatalf("expected 'coding', got %q", buf.TaskIntent())
	}

	embed := buf.TaskEmbed()
	if len(embed) != 3 {
		t.Fatalf("expected embed len 3, got %d", len(embed))
	}
}

func TestAgentProgressTracker_StopIdempotent(t *testing.T) {
	tracker := NewAgentProgressTracker(nil, "test", "coding", nil)

	// Should not panic on multiple stops.
	tracker.Stop()
	tracker.Stop()
	tracker.Stop()
}

func TestClassifyComplexity(t *testing.T) {
	tests := []struct {
		intent   string
		msgLen   int
		expected TaskComplexity
	}{
		{"coding", 50, ComplexityHeavy},
		{"ssh", 10, ComplexityHeavy},
		{"workflow", 100, ComplexityHeavy},
		{"bug_fix", 30, ComplexityHeavy},
		{"search", 100, ComplexityMedium},  // long message
		{"search", 10, ComplexityLight},     // short message
		{"non_coding", 5, ComplexityLight},
		{"unknown", 3, ComplexityLight},
	}

	for _, tt := range tests {
		got := ClassifyComplexity(tt.intent, tt.msgLen)
		if got != tt.expected {
			t.Errorf("ClassifyComplexity(%q, %d) = %d, want %d",
				tt.intent, tt.msgLen, got, tt.expected)
		}
	}
}
