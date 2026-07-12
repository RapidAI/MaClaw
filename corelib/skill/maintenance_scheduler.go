package skill

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MaintenanceScheduler runs BuildSkillMaintenancePlan periodically and
// persists high-value results to long-term memory / experience learning for
// proactive recall on later turns.
type MaintenanceScheduler struct {
	Interval    time.Duration
	MemorySaver func(content string, tags []string) error
	SkillLoader func() []corelib.NLSkillEntry
	PlanOptions SkillMaintenancePlanOptions
	// UsageIngester optionally records high-value failure/review signals into
	// UsageTracker so routing/skill memory can avoid broken skills next turn.
	UsageIngester func(skills []corelib.NLSkillEntry, opts SkillMaintenancePlanOptions) int

	stopCh chan struct{}
	once   sync.Once
}

// NewMaintenanceScheduler creates a scheduler with sensible defaults.
func NewMaintenanceScheduler(
	skillLoader func() []corelib.NLSkillEntry,
	memorySaver func(content string, tags []string) error,
) *MaintenanceScheduler {
	return &MaintenanceScheduler{
		Interval:    24 * time.Hour,
		MemorySaver: memorySaver,
		SkillLoader: skillLoader,
		stopCh:      make(chan struct{}),
	}
}

// Start begins the background scheduling loop.
func (s *MaintenanceScheduler) Start() {
	if s == nil || s.SkillLoader == nil {
		return
	}
	go s.loop()
}

// Stop terminates the background scheduling loop.
func (s *MaintenanceScheduler) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stopCh)
	})
}

func (s *MaintenanceScheduler) loop() {
	// Initial delay: run first plan 5 minutes after startup.
	initialDelay := 5 * time.Minute
	select {
	case <-s.stopCh:
		return
	case <-time.After(initialDelay):
	}

	// Run immediately on first tick.
	s.runOnce()

	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func (s *MaintenanceScheduler) runOnce() {
	skills := s.SkillLoader()
	if len(skills) == 0 {
		return
	}

	opts := s.PlanOptions
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	plan := BuildSkillMaintenancePlan(skills, opts)
	if len(plan.Actions) == 0 {
		log.Printf("[maintenance-scheduler] plan generated: no actions needed")
		return
	}

	high := FilterHighValueMaintenanceActions(plan.Actions)
	log.Printf("[maintenance-scheduler] plan generated: %d actions (%d high-value)", len(plan.Actions), len(high))

	// Persist high-value recommendations into experience/memory for proactive recall.
	if s.MemorySaver != nil {
		content := BuildHighValueMaintenanceExperienceContent(plan, 10)
		if content == "" {
			content = formatMaintenancePlanForMemory(plan)
		}
		tags := []string{"skill_maintenance", "auto_scheduled", "task_artifact", "experience_learning", "high_value"}
		if err := s.MemorySaver(content, tags); err != nil {
			log.Printf("[maintenance-scheduler] failed to save plan to memory: %v", err)
		} else {
			log.Printf("[maintenance-scheduler] high-value plan saved to memory (%d bytes)", len(content))
		}
	}
	if s.UsageIngester != nil {
		if n := s.UsageIngester(skills, opts); n > 0 {
			log.Printf("[maintenance-scheduler] ingested %d high-value maintenance experiences", n)
		}
	}
}

func formatMaintenancePlanForMemory(plan SkillMaintenancePlan) string {
	if content := BuildHighValueMaintenanceExperienceContent(plan, 10); content != "" {
		return content
	}
	var b strings.Builder
	b.WriteString("# Skill 维护计划（自动生成）\n\n")
	b.WriteString("生成时间: ")
	b.WriteString(plan.GeneratedAt)
	b.WriteString("\n\n")
	b.WriteString(plan.Summary)
	b.WriteString("\n\n## 建议操作\n\n")

	for i, action := range plan.Actions {
		if i >= 10 {
			b.WriteString("\n... 还有更多操作，使用 manage_skill(action=\"maintenance\") 查看完整计划\n")
			break
		}
		b.WriteString("- **")
		b.WriteString(action.Skill)
		b.WriteString("**: ")
		b.WriteString(action.RecommendedAction)
		b.WriteString(" (")
		b.WriteString(action.Reason)
		b.WriteString(")\n")
	}

	return b.String()
}
