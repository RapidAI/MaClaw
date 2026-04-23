package agent

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// SelfReviewInterval is the minimum time between self-review cycles.
const SelfReviewInterval = 24 * time.Hour

// SelfReviewTaskThreshold is the minimum number of completed sessions
// since the last review before a new review is triggered.
const SelfReviewTaskThreshold = 15

// ToolUsageStat tracks usage frequency for a single tool.
type ToolUsageStat struct {
	Name       string `json:"name"`
	CallCount  int    `json:"call_count"`
	ErrorCount int    `json:"error_count"`
}

// SkillHealthStat tracks health metrics for a single skill.
type SkillHealthStat struct {
	Name        string  `json:"name"`
	UsageCount  int     `json:"usage_count"`
	SuccessRate float64 `json:"success_rate"`
	LastError   string  `json:"last_error,omitempty"`
}

// SelfReviewReport is the output of a self-review cycle.
type SelfReviewReport struct {
	ReviewedAt      time.Time         `json:"reviewed_at"`
	SessionCount    int               `json:"session_count"`
	TopTools        []ToolUsageStat   `json:"top_tools"`
	UnhealthySkills []SkillHealthStat `json:"unhealthy_skills"`
	Insights        []string          `json:"insights"`
}

// SessionStatsProvider abstracts access to recent session statistics.
type SessionStatsProvider interface {
	// CompletedSessionsSince returns the number of sessions completed
	// after the given time.
	CompletedSessionsSince(since time.Time) int
}

// ToolStatsProvider abstracts access to tool usage statistics.
type ToolStatsProvider interface {
	// TopToolsByUsage returns the N most frequently used tools.
	TopToolsByUsage(n int) []ToolUsageStat
}

// SkillHealthProvider abstracts access to skill health data.
type SkillHealthProvider interface {
	// UnhealthySkills returns skills with success rate below threshold.
	UnhealthySkills(minUsage int, maxSuccessRate float64) []SkillHealthStat
}

// MemorySaver abstracts the ability to save insights to long-term memory.
type MemorySaver interface {
	SaveInsight(content string, category string, tags []string) error
}

// SelfReviewLoop runs periodic self-assessment cycles.
type SelfReviewLoop struct {
	mu           sync.Mutex
	lastReviewAt time.Time
	lastReport   *SelfReviewReport
	sessionStats SessionStatsProvider
	toolStats    ToolStatsProvider
	skillHealth  SkillHealthProvider
	memorySaver  MemorySaver
	stopCh       chan struct{}
	running      bool
}

// NewSelfReviewLoop creates a new self-review loop.
func NewSelfReviewLoop(
	sessionStats SessionStatsProvider,
	toolStats ToolStatsProvider,
	skillHealth SkillHealthProvider,
	memorySaver MemorySaver,
) *SelfReviewLoop {
	return &SelfReviewLoop{
		sessionStats: sessionStats,
		toolStats:    toolStats,
		skillHealth:  skillHealth,
		memorySaver:  memorySaver,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the background self-review loop.
func (r *SelfReviewLoop) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	go r.loop()
}

// Stop halts the background loop.
func (r *SelfReviewLoop) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	select {
	case <-r.stopCh:
		// already closed
	default:
		close(r.stopCh)
	}
}

// LastReport returns the most recent review report.
func (r *SelfReviewLoop) LastReport() *SelfReviewReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReport
}

func (r *SelfReviewLoop) loop() {
	// Run once at startup after a short delay.
	select {
	case <-time.After(5 * time.Minute):
	case <-r.stopCh:
		return
	}
	r.maybeReview()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.maybeReview()
		case <-r.stopCh:
			return
		}
	}
}

func (r *SelfReviewLoop) maybeReview() {
	r.mu.Lock()
	lastReview := r.lastReviewAt
	r.mu.Unlock()

	// Check time threshold.
	if time.Since(lastReview) < SelfReviewInterval {
		return
	}

	// Check session count threshold.
	if r.sessionStats != nil {
		count := r.sessionStats.CompletedSessionsSince(lastReview)
		if count < SelfReviewTaskThreshold {
			return
		}
	}

	report := r.runReview()

	r.mu.Lock()
	r.lastReviewAt = time.Now()
	r.lastReport = report
	r.mu.Unlock()

	// Save insights to memory.
	if r.memorySaver != nil && len(report.Insights) > 0 {
		content := fmt.Sprintf("自我评估 (%s): %s",
			report.ReviewedAt.Format("2006-01-02"),
			strings.Join(report.Insights, "; "))
		if err := r.memorySaver.SaveInsight(content, "instruction", []string{"self_review", "proactive"}); err != nil {
			log.Printf("[self-review] failed to save insights to memory: %v", err)
		}
	}
}

func (r *SelfReviewLoop) runReview() *SelfReviewReport {
	report := &SelfReviewReport{
		ReviewedAt: time.Now(),
	}

	// Gather tool usage stats.
	if r.toolStats != nil {
		report.TopTools = r.toolStats.TopToolsByUsage(10)
	}

	// Gather unhealthy skills.
	if r.skillHealth != nil {
		report.UnhealthySkills = r.skillHealth.UnhealthySkills(3, 0.5)
	}

	// Generate insights.
	report.Insights = generateInsights(report)

	log.Printf("[self-review] completed: %d top tools, %d unhealthy skills, %d insights",
		len(report.TopTools), len(report.UnhealthySkills), len(report.Insights))

	return report
}

// generateInsights produces human-readable insights from the review data.
func generateInsights(report *SelfReviewReport) []string {
	var insights []string

	// Insight: tools with high error rates.
	for _, t := range report.TopTools {
		if t.CallCount > 5 && t.ErrorCount > 0 {
			errorRate := float64(t.ErrorCount) / float64(t.CallCount) * 100
			if errorRate > 30 {
				insights = append(insights, fmt.Sprintf(
					"工具 %s 错误率 %.0f%% (%d/%d)，需要关注",
					t.Name, errorRate, t.ErrorCount, t.CallCount))
			}
		}
	}

	// Insight: unhealthy skills.
	for _, s := range report.UnhealthySkills {
		insights = append(insights, fmt.Sprintf(
			"Skill %q 成功率 %.0f%% (%d 次使用)，最近错误: %s",
			s.Name, s.SuccessRate*100, s.UsageCount, s.LastError))
	}

	// Insight: most used tools (for awareness).
	if len(report.TopTools) >= 3 {
		names := make([]string, 0, 3)
		for i := 0; i < 3 && i < len(report.TopTools); i++ {
			names = append(names, report.TopTools[i].Name)
		}
		insights = append(insights, fmt.Sprintf("最常用工具: %s", strings.Join(names, ", ")))
	}

	return insights
}

// SortToolsByUsage sorts tool stats by call count descending.
func SortToolsByUsage(stats []ToolUsageStat) {
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].CallCount > stats[j].CallCount
	})
}

// FilterUnhealthySkills filters skills below the success rate threshold.
func FilterUnhealthySkills(skills []SkillHealthStat, minUsage int, maxSuccessRate float64) []SkillHealthStat {
	var result []SkillHealthStat
	for _, s := range skills {
		if s.UsageCount >= minUsage && s.SuccessRate < maxSuccessRate {
			result = append(result, s)
		}
	}
	return result
}

// Ensure SelfReviewLoop satisfies a basic context-aware shutdown.
func (r *SelfReviewLoop) RunOnce(ctx context.Context) *SelfReviewReport {
	select {
	case <-ctx.Done():
		return nil
	default:
	}
	return r.runReview()
}
