package tool

import (
	"strings"
	"testing"
	"time"
)

func TestSkillMemory_BuildCapabilitySummary_WithPatterns(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record 10 successful ssh calls in "server" context.
	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"服务器", "资源", "GPU"},
			Success:     true,
			Timestamp:   time.Now().Add(-time.Duration(i) * time.Hour),
		})
		tracker.mu.Unlock()
	}

	sm := NewSkillMemory(tracker)
	summary := sm.BuildCapabilitySummary([]string{"服务器", "GPU"})

	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if !strings.Contains(summary, "ssh") {
		t.Errorf("summary should mention ssh: %s", summary)
	}
	if !strings.Contains(summary, "能力记忆") {
		t.Errorf("summary should have header: %s", summary)
	}
}

func TestSkillMemory_BuildCapabilitySummary_Empty(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	sm := NewSkillMemory(tracker)

	summary := sm.BuildCapabilitySummary([]string{"test"})
	if summary != "" {
		t.Errorf("expected empty summary for empty tracker, got %q", summary)
	}
}

func TestSkillMemory_BuildCapabilitySummary_Nil(t *testing.T) {
	var sm *SkillMemory
	summary := sm.BuildCapabilitySummary([]string{"test"})
	if summary != "" {
		t.Errorf("expected empty summary for nil SkillMemory, got %q", summary)
	}
}

func TestSkillMemory_BuildFailureWarnings(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// Record 5 failed write_file calls in "large file" context.
	for i := 0; i < 5; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "write_file",
			QueryTokens: []string{"大文件", "写入", "JSON"},
			Success:     false,
			FollowUp:    "retry",
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	sm := NewSkillMemory(tracker)
	warnings := sm.BuildFailureWarnings([]string{"大文件", "JSON"})

	if warnings == "" {
		t.Error("expected non-empty warnings")
	}
	if !strings.Contains(warnings, "write_file") {
		t.Errorf("warnings should mention write_file: %s", warnings)
	}
}

func TestSkillMemory_BuildFailureWarnings_NoFailures(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	for i := 0; i < 10; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"run"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	sm := NewSkillMemory(tracker)
	warnings := sm.BuildFailureWarnings([]string{"run"})
	if warnings != "" {
		t.Errorf("expected empty warnings for all-success tool, got %q", warnings)
	}
}

func TestSkillMemory_SuggestAlternatives(t *testing.T) {
	tracker, _ := NewUsageTracker("")

	// ssh failed in "deploy" context.
	for i := 0; i < 5; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "ssh",
			QueryTokens: []string{"部署", "应用"},
			Success:     false,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	// bash succeeded in "deploy" context.
	for i := 0; i < 5; i++ {
		tracker.mu.Lock()
		tracker.records = append(tracker.records, UsageRecord{
			ToolName:    "bash",
			QueryTokens: []string{"部署", "应用"},
			Success:     true,
			Timestamp:   time.Now(),
		})
		tracker.mu.Unlock()
	}

	sm := NewSkillMemory(tracker)
	alts := sm.SuggestAlternatives("ssh", []string{"部署", "应用"})

	if len(alts) == 0 {
		t.Error("expected at least one alternative")
	}
	found := false
	for _, a := range alts {
		if a == "bash" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bash as alternative, got %v", alts)
	}
}

func TestSkillMemory_SuggestAlternatives_NoData(t *testing.T) {
	tracker, _ := NewUsageTracker("")
	sm := NewSkillMemory(tracker)

	alts := sm.SuggestAlternatives("ssh", []string{"test"})
	if len(alts) != 0 {
		t.Errorf("expected no alternatives for empty tracker, got %v", alts)
	}
}
