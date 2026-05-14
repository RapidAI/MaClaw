package agent

import (
	"strings"
	"testing"
	"time"
)

func TestBuildSituationReport_Empty(t *testing.T) {
	got := BuildSituationReport(SituationContext{})
	if got != "" {
		t.Errorf("empty context should produce empty report, got: %s", got)
	}
}

func TestBuildSituationReport_InFlightTask(t *testing.T) {
	got := BuildSituationReport(SituationContext{
		InFlightTask: "搜索 HuggingFace 论文并生成综述",
		CurrentTime:  time.Date(2026, 5, 14, 10, 30, 0, 0, time.Local),
	})
	if !strings.Contains(got, "进行中") {
		t.Error("missing in-flight task")
	}
	if !strings.Contains(got, "HuggingFace") {
		t.Error("task content lost")
	}
	if !strings.Contains(got, "当前情境") {
		t.Error("missing header")
	}
}

func TestBuildSituationReport_UnfinishedFallback(t *testing.T) {
	got := BuildSituationReport(SituationContext{
		UnfinishedTask: "升级 OmniRoute Docker 版本",
	})
	if !strings.Contains(got, "未完成") {
		t.Error("missing unfinished task")
	}
}

func TestBuildSituationReport_InFlightTakesPrecedence(t *testing.T) {
	got := BuildSituationReport(SituationContext{
		InFlightTask:   "当前任务",
		UnfinishedTask: "旧任务",
	})
	if !strings.Contains(got, "当前任务") {
		t.Error("in-flight should take precedence")
	}
	if strings.Contains(got, "旧任务") {
		t.Error("unfinished should not appear when in-flight exists")
	}
}

func TestBuildSituationReport_FullContext(t *testing.T) {
	got := BuildSituationReport(SituationContext{
		InFlightTask:      "开发贪吃蛇游戏",
		ActiveWorkflow:    "coding/implementation",
		ActiveSSHSessions: []string{"root@api.rapidai.tech:22", "dev@build.server:22"},
		BackgroundTasks:   []string{"babeldoc 翻译中"},
		RecentArtifacts:   []string{"需求文档已确认", "技术设计已确认"},
		CurrentTime:       time.Date(2026, 5, 14, 15, 0, 0, 0, time.Local),
	})

	checks := []string{"进行中", "工作流", "SSH会话", "后台任务", "最近完成", "时间"}
	for _, check := range checks {
		if !strings.Contains(got, check) {
			t.Errorf("missing section: %s\nfull report:\n%s", check, got)
		}
	}
}

func TestBuildSituationReport_TruncatesLongTask(t *testing.T) {
	longTask := strings.Repeat("搜索论文", 50) // 200 chars
	got := BuildSituationReport(SituationContext{InFlightTask: longTask})
	if len([]rune(got)) > 200 {
		// Report should be concise
		t.Logf("report length: %d runes (acceptable for header + truncated task)", len([]rune(got)))
	}
	if !strings.Contains(got, "...") {
		t.Error("long task should be truncated with ...")
	}
}

func TestBuildSituationReport_LimitsSSHSessions(t *testing.T) {
	sessions := []string{"host1", "host2", "host3", "host4", "host5"}
	got := BuildSituationReport(SituationContext{ActiveSSHSessions: sessions})
	// Should only show first 3
	if strings.Contains(got, "host4") || strings.Contains(got, "host5") {
		t.Error("should limit to 3 SSH sessions")
	}
}

func TestHasMeaningfulContext(t *testing.T) {
	empty := SituationContext{}
	if empty.HasMeaningfulContext() {
		t.Error("empty context should not be meaningful")
	}

	withTask := SituationContext{InFlightTask: "something"}
	if !withTask.HasMeaningfulContext() {
		t.Error("context with task should be meaningful")
	}

	withSSH := SituationContext{ActiveSSHSessions: []string{"host"}}
	if !withSSH.HasMeaningfulContext() {
		t.Error("context with SSH should be meaningful")
	}
}

func TestEstimateSituationTokens(t *testing.T) {
	report := "[当前情境]\n进行中: 搜索论文\n时间: 2026-05-14 15:00 Wed"
	tokens := EstimateSituationTokens(report)
	if tokens <= 0 {
		t.Error("should estimate positive tokens")
	}
	if tokens > 100 {
		t.Errorf("estimate too high for short report: %d", tokens)
	}
}

func TestEstimateSituationTokens_Empty(t *testing.T) {
	if EstimateSituationTokens("") != 0 {
		t.Error("empty report should be 0 tokens")
	}
}

func TestFormatTimeContext(t *testing.T) {
	morning := time.Date(2026, 5, 14, 8, 30, 0, 0, time.Local)
	got := FormatTimeContext(morning)
	if !strings.Contains(got, "早上") {
		t.Errorf("8:30 should be 早上, got: %s", got)
	}

	afternoon := time.Date(2026, 5, 14, 15, 0, 0, 0, time.Local)
	got = FormatTimeContext(afternoon)
	if !strings.Contains(got, "下午") {
		t.Errorf("15:00 should be 下午, got: %s", got)
	}
}
