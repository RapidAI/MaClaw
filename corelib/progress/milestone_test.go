package progress

import (
	"testing"
	"time"
)

func TestMilestoneBuffer_RecordAndRetrieve(t *testing.T) {
	buf := NewMilestoneBuffer(4)
	buf.Reset("test task", "coding", nil)

	buf.Record(Milestone{Time: time.Now(), Tool: "bash", Summary: "执行命令: ls", Completed: true})
	buf.Record(Milestone{Time: time.Now(), Tool: "write_file", Summary: "生成: main.go", Completed: true})

	if buf.Len() != 2 {
		t.Fatalf("expected 2 milestones, got %d", buf.Len())
	}
	if buf.CompletedCount() != 2 {
		t.Fatalf("expected 2 completed, got %d", buf.CompletedCount())
	}
}

func TestMilestoneBuffer_RingEviction(t *testing.T) {
	buf := NewMilestoneBuffer(3)
	buf.Reset("test", "coding", nil)

	for i := 0; i < 5; i++ {
		buf.Record(Milestone{
			Time:      time.Now(),
			Tool:      "bash",
			Summary:   "step",
			Completed: true,
		})
	}

	if buf.Len() != 3 {
		t.Fatalf("expected 3 milestones after eviction, got %d", buf.Len())
	}
}

func TestMilestoneBuffer_Since(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("test", "coding", nil)

	t1 := time.Now()
	time.Sleep(time.Millisecond)
	buf.Record(Milestone{Time: time.Now(), Tool: "a", Summary: "first", Completed: true})
	time.Sleep(time.Millisecond)
	t2 := time.Now()
	time.Sleep(time.Millisecond)
	buf.Record(Milestone{Time: time.Now(), Tool: "b", Summary: "second", Completed: true})

	since1 := buf.Since(t1)
	if len(since1) != 2 {
		t.Fatalf("expected 2 milestones since t1, got %d", len(since1))
	}

	since2 := buf.Since(t2)
	if len(since2) != 1 {
		t.Fatalf("expected 1 milestone since t2, got %d", len(since2))
	}
}

func TestMilestoneBuffer_Latest(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("test", "coding", nil)

	if buf.Latest() != nil {
		t.Fatal("expected nil latest on empty buffer")
	}

	buf.Record(Milestone{Time: time.Now(), Tool: "a", Summary: "first", Completed: true})
	buf.Record(Milestone{Time: time.Now(), Tool: "b", Summary: "second", Completed: true})

	latest := buf.Latest()
	if latest == nil || latest.Summary != "second" {
		t.Fatalf("expected latest to be 'second', got %v", latest)
	}
}

func TestMilestoneBuffer_ProgressSummary(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("test", "coding", nil)

	// Empty buffer.
	summary := buf.ProgressSummary()
	if summary == "" {
		t.Fatal("expected non-empty summary even with no milestones")
	}

	buf.Record(Milestone{Time: time.Now(), Tool: "bash", Summary: "执行命令: ls", Completed: true})
	buf.Record(Milestone{Time: time.Now(), Tool: "write_file", Summary: "生成: main.go", Completed: false})

	summary = buf.ProgressSummary()
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestMilestoneBuffer_CompletedOutputSummary(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("test", "coding", nil)

	// Empty.
	if s := buf.CompletedOutputSummary(); s != "" {
		t.Fatalf("expected empty summary, got %q", s)
	}

	buf.Record(Milestone{Time: time.Now(), Tool: "a", Summary: "step1", Completed: true})
	buf.Record(Milestone{Time: time.Now(), Tool: "b", Summary: "step2", Completed: true})

	s := buf.CompletedOutputSummary()
	if s != "step1, step2" {
		t.Fatalf("expected 'step1, step2', got %q", s)
	}

	// More than 3 completed → truncated.
	buf.Record(Milestone{Time: time.Now(), Tool: "c", Summary: "step3", Completed: true})
	buf.Record(Milestone{Time: time.Now(), Tool: "d", Summary: "step4", Completed: true})

	s = buf.CompletedOutputSummary()
	if s != "step1, step2, step3 等 4 个步骤" {
		t.Fatalf("expected truncated summary, got %q", s)
	}
}

func TestMilestoneBuffer_Reset(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("task1", "coding", []float32{1, 2, 3})
	buf.Record(Milestone{Time: time.Now(), Tool: "a", Summary: "s", Completed: true})

	buf.Reset("task2", "ssh", nil)

	if buf.Len() != 0 {
		t.Fatal("expected empty buffer after reset")
	}
	if buf.TaskDesc() != "task2" {
		t.Fatalf("expected task2, got %q", buf.TaskDesc())
	}
	if buf.TaskIntent() != "ssh" {
		t.Fatalf("expected ssh, got %q", buf.TaskIntent())
	}
	if buf.TaskEmbed() != nil {
		t.Fatal("expected nil embed after reset with nil")
	}
}
