package skill

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

// MaintenanceScheduler runs BuildSkillMaintenancePlan periodically and
// persists results to long-term memory for proactive recall.
type MaintenanceScheduler struct {
	Interval    time.Duration
	MemorySaver func(content string, tags []string) error
	SkillLoader func() []corelib.NLSkillEntry
	PlanOptions SkillMaintenancePlanOptions

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

	log.Printf("[maintenance-scheduler] plan generated: %d actions", len(plan.Actions))

	// Persist to memory if saver is available.
	if s.MemorySaver != nil {
		content := formatMaintenancePlanForMemory(plan)
		tags := []string{"skill_maintenance", "auto_scheduled", "task_artifact"}
		if err := s.MemorySaver(content, tags); err != nil {
			log.Printf("[maintenance-scheduler] failed to save plan to memory: %v", err)
		} else {
			log.Printf("[maintenance-scheduler] plan saved to memory (%d bytes)", len(content))
		}
	}
}

func formatMaintenancePlanForMemory(plan SkillMaintenancePlan) string {
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
