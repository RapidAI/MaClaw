package progress

import (
	"strings"
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

func TestFormatDurationUsesUILanguage(t *testing.T) {
	if got := formatDuration(90*time.Second, "zh"); got != "1 分 30 秒" {
		t.Fatalf("zh = %q", got)
	}
	if got := formatDuration(2*time.Minute, "zh"); got != "2 分钟" {
		t.Fatalf("zh minutes = %q", got)
	}
	if got := formatDuration(45*time.Second, "en"); got != "45 sec" {
		t.Fatalf("en seconds = %q", got)
	}
	if got := formatDuration(90*time.Second, "mn"); got != "1 мин 30 сек" {
		t.Fatalf("mn = %q", got)
	}
	if got := formatDuration(2*time.Minute, "mn-MN"); strings.Contains(got, "分") || strings.Contains(got, "秒") || got != "2 мин" {
		t.Fatalf("mn minutes = %q", got)
	}
	if got := formatDuration(2*time.Hour+30*time.Minute+10*time.Second, "zh"); got != "2 小时 30 分 10 秒" {
		t.Fatalf("zh hours = %q", got)
	}
	if got := formatDuration(2*time.Hour, "mn"); got != "2 ц" || strings.Contains(got, "分") {
		t.Fatalf("mn hours = %q", got)
	}
	if got := formatDuration(2*time.Hour+10*time.Second, "zh"); got != "2 小时 10 秒" {
		t.Fatalf("zh hours without minutes = %q", got)
	}
	if got := formatDuration(2*time.Hour+10*time.Second, "mn"); got != "2 ц 10 сек" || strings.Contains(got, "0 мин") {
		t.Fatalf("mn hours without minutes = %q", got)
	}
	if got := formatDuration(59*time.Minute+59*time.Second, "zh"); got != "59 分 59 秒" {
		t.Fatalf("just under an hour = %q", got)
	}
	if got := formatDuration(time.Hour, "en"); got != "1 hr" {
		t.Fatalf("exact hour = %q", got)
	}
	if got := formatDuration(time.Hour+time.Second, "mn"); got != "1 ц 1 сек" {
		t.Fatalf("hour plus one second = %q", got)
	}
	if got := formatDuration(time.Hour+time.Minute+time.Second, "zh"); got != "1 小时 1 分 1 秒" {
		t.Fatalf("hour minute second = %q", got)
	}
}

func TestCompletedOutputSummaryLang(t *testing.T) {
	buf := NewMilestoneBuffer(10)
	buf.Reset("task", "coding", nil)
	for _, summary := range []string{"a", "b", "c", "d"} {
		buf.Record(Milestone{Time: time.Now(), Tool: "bash", Summary: summary, Completed: true})
	}
	if got := buf.CompletedOutputSummary(); got != "a, b, c 等 4 个步骤" {
		t.Fatalf("zh = %q", got)
	}
	got := buf.CompletedOutputSummaryLang("mn")
	if got != "a, b, c, 4 алхам" || strings.Contains(got, "步骤") {
		t.Fatalf("mn = %q", got)
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
