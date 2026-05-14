package agent

// SituationReport generates a concise context summary of the user's current state.
// Inspired by OpenHuman's subconscious/situation_report/ module — provides the
// agent with "global awareness" of what's happening, regardless of what the user
// asks in the current message.
//
// The report is injected into the system prompt (before memory recall) so the
// agent always knows:
// - What task is currently in progress
// - What was recently completed
// - What active sessions/connections exist
// - Current time context
//
// Budget: ≤200 tokens (~400 chars). Concise bullet points.

import (
	"fmt"
	"strings"
	"time"
)

// SituationContext holds the data sources for building a situation report.
// Each field is optional — nil/empty means that data source is unavailable.
type SituationContext struct {
	// InFlightTask is the description of the currently executing task (from in-flight marker).
	InFlightTask string
	// RecentArtifacts are titles/descriptions of recently completed task artifacts.
	RecentArtifacts []string
	// ActiveSSHSessions are descriptions of active SSH connections (e.g. "root@api.rapidai.tech:22").
	ActiveSSHSessions []string
	// ActiveWorkflow describes the current workflow state (e.g. "coding/implementation").
	ActiveWorkflow string
	// UnfinishedTask is the description from an unfinished task slot.
	UnfinishedTask string
	// BackgroundTasks are descriptions of running background tasks.
	BackgroundTasks []string
	// CurrentTime is the current timestamp.
	CurrentTime time.Time
}

// BuildSituationReport generates a concise situation summary from the given context.
// Returns empty string if there's nothing meaningful to report.
func BuildSituationReport(ctx SituationContext) string {
	var sections []string

	// In-flight task (highest priority)
	if ctx.InFlightTask != "" {
		sections = append(sections, "进行中: "+truncateReport(ctx.InFlightTask, 80))
	} else if ctx.UnfinishedTask != "" {
		sections = append(sections, "未完成: "+truncateReport(ctx.UnfinishedTask, 80))
	}

	// Active workflow
	if ctx.ActiveWorkflow != "" {
		sections = append(sections, "工作流: "+ctx.ActiveWorkflow)
	}

	// Active SSH sessions
	if len(ctx.ActiveSSHSessions) > 0 {
		n := min(len(ctx.ActiveSSHSessions), 3)
		sections = append(sections, "SSH会话: "+strings.Join(ctx.ActiveSSHSessions[:n], ", "))
	}

	// Background tasks
	if len(ctx.BackgroundTasks) > 0 {
		n := min(len(ctx.BackgroundTasks), 3)
		sections = append(sections, "后台任务: "+strings.Join(ctx.BackgroundTasks[:n], ", "))
	}

	// Recent completions
	if len(ctx.RecentArtifacts) > 0 {
		n := min(len(ctx.RecentArtifacts), 3)
		sections = append(sections, "最近完成: "+strings.Join(ctx.RecentArtifacts[:n], "; "))
	}

	// Time context
	if !ctx.CurrentTime.IsZero() {
		sections = append(sections, "时间: "+ctx.CurrentTime.Format("2006-01-02 15:04 Mon"))
	}

	if len(sections) == 0 {
		return ""
	}

	return "[当前情境]\n" + strings.Join(sections, "\n")
}

// HasMeaningfulContext returns true if the situation context has any
// non-trivial information worth reporting.
func (ctx SituationContext) HasMeaningfulContext() bool {
	return ctx.InFlightTask != "" ||
		ctx.UnfinishedTask != "" ||
		ctx.ActiveWorkflow != "" ||
		len(ctx.ActiveSSHSessions) > 0 ||
		len(ctx.BackgroundTasks) > 0 ||
		len(ctx.RecentArtifacts) > 0
}

// EstimateTokens returns a rough token estimate for the report.
func EstimateSituationTokens(report string) int {
	if report == "" {
		return 0
	}
	// Chinese text: ~1.5 chars per token
	return len([]rune(report))*2/3 + 10
}

func truncateReport(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// FormatTimeContext returns a human-friendly time description.
func FormatTimeContext(t time.Time) string {
	hour := t.Hour()
	var period string
	switch {
	case hour < 6:
		period = "凌晨"
	case hour < 9:
		period = "早上"
	case hour < 12:
		period = "上午"
	case hour < 14:
		period = "中午"
	case hour < 18:
		period = "下午"
	case hour < 22:
		period = "晚上"
	default:
		period = "深夜"
	}
	return fmt.Sprintf("%s %s %02d:%02d", t.Format("01-02"), period, hour, t.Minute())
}
