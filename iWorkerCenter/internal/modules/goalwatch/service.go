package goalwatch

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

const (
	EventGoalPush       = "goal_push"
	EventGoalPushAck    = "goal_push_ack"
	DefaultStalledAfter = 5 * time.Minute
	DefaultPushCooldown = 10 * time.Minute
	DefaultTickInterval = 1 * time.Minute
)

type Config struct {
	StalledAfter time.Duration
	PushCooldown time.Duration
	TickInterval time.Duration
}

type Service struct {
	collabRepo *collaboration.Repo
	config     Config
}

type Push struct {
	EventID       string    `json:"event_id,omitempty"`
	TaskID        string    `json:"task_id"`
	Title         string    `json:"title"`
	ToColleagueID string    `json:"to_colleague_id"`
	ToRoleCode    string    `json:"to_role_code"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason"`
	AgeSeconds    int64     `json:"age_seconds"`
	CreatedAt     time.Time `json:"created_at"`
}

type CheckResult struct {
	TenantID string `json:"tenant_id"`
	Checked  int    `json:"checked"`
	Pushed   int    `json:"pushed"`
	Pushes   []Push `json:"pushes"`
}

type AckResult struct {
	EventID    string    `json:"event_id"`
	TaskID     string    `json:"task_id"`
	AckEventID string    `json:"ack_event_id"`
	Status     string    `json:"status"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

func NewService(collabRepo *collaboration.Repo, cfg Config) *Service {
	if cfg.StalledAfter <= 0 {
		cfg.StalledAfter = DefaultStalledAfter
	}
	if cfg.PushCooldown <= 0 {
		cfg.PushCooldown = DefaultPushCooldown
	}
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = DefaultTickInterval
	}
	return &Service{collabRepo: collabRepo, config: cfg}
}

func (s *Service) Config() Config { return s.config }

func (s *Service) CheckTenant(tenantID string, now time.Time) (CheckResult, error) {
	if s == nil || s.collabRepo == nil {
		return CheckResult{}, fmt.Errorf("goalwatch collaboration repo is unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tenantID = normalizeTenantID(tenantID)
	result := CheckResult{TenantID: tenantID, Pushes: []Push{}}
	tasks, err := s.collabRepo.ListAll(tenantID)
	if err != nil {
		return result, err
	}
	for _, task := range tasks {
		if task == nil || task.IsTerminal() {
			continue
		}
		result.Checked++
		age := now.Sub(task.UpdatedAt)
		if age < s.config.StalledAfter {
			continue
		}
		if s.hasRecentPush(tenantID, task.ID, now) {
			continue
		}
		push := Push{TaskID: task.ID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: reasonForStatus(task.Status), AgeSeconds: int64(age.Seconds()), CreatedAt: now}
		if err := s.recordPush(tenantID, task, push, now); err != nil {
			return result, err
		}
		result.Pushes = append(result.Pushes, push)
		result.Pushed++
	}
	return result, nil
}

func (s *Service) ListPushesForColleague(tenantID, colleagueID string, limit int) ([]Push, error) {
	if s == nil || s.collabRepo == nil {
		return nil, fmt.Errorf("goalwatch collaboration repo is unavailable")
	}
	tenantID = normalizeTenantID(tenantID)
	colleagueID = strings.TrimSpace(colleagueID)
	if colleagueID == "" {
		return nil, fmt.Errorf("colleague_id is required")
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	tasks, err := s.collabRepo.ListByColleague(tenantID, colleagueID)
	if err != nil {
		return nil, err
	}
	pushes := make([]Push, 0)
	for _, task := range tasks {
		if task == nil || task.IsTerminal() {
			continue
		}
		events, err := s.collabRepo.ListEvents(tenantID, task.ID)
		if err != nil {
			return nil, err
		}
		acked := ackedPushIDs(events)
		for _, event := range events {
			if event.Event != EventGoalPush || acked[event.ID] {
				continue
			}
			pushes = append(pushes, Push{EventID: event.ID, TaskID: task.ID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: parseNoteValue(event.Note, "reason"), AgeSeconds: parseNoteInt64(event.Note, "age_seconds"), CreatedAt: event.CreatedAt})
		}
	}
	sort.SliceStable(pushes, func(i, j int) bool { return pushes[i].CreatedAt.After(pushes[j].CreatedAt) })
	if len(pushes) > limit {
		pushes = pushes[:limit]
	}
	return pushes, nil
}

func (s *Service) AckPush(tenantID, colleagueID, eventID, status, note string, now time.Time) (AckResult, error) {
	if s == nil || s.collabRepo == nil {
		return AckResult{}, fmt.Errorf("goalwatch collaboration repo is unavailable")
	}
	tenantID = normalizeTenantID(tenantID)
	colleagueID = strings.TrimSpace(colleagueID)
	eventID = strings.TrimSpace(eventID)
	if colleagueID == "" {
		return AckResult{}, fmt.Errorf("colleague_id is required")
	}
	if eventID == "" {
		return AckResult{}, fmt.Errorf("event_id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status = normalizeAckStatus(status)
	tasks, err := s.collabRepo.ListByColleague(tenantID, colleagueID)
	if err != nil {
		return AckResult{}, err
	}
	for _, task := range tasks {
		if task == nil || task.IsTerminal() {
			continue
		}
		events, err := s.collabRepo.ListEvents(tenantID, task.ID)
		if err != nil {
			return AckResult{}, err
		}
		acked := ackedPushIDs(events)
		for _, event := range events {
			if event.Event != EventGoalPush || event.ID != eventID {
				continue
			}
			if acked[event.ID] {
				return AckResult{}, fmt.Errorf("push already acknowledged: %s", eventID)
			}
			ackEventID := idgen.New("gack")
			ackNote := fmt.Sprintf("ack_event_id=%s status=%s note=%s", eventID, status, sanitizeNote(note))
			if err := s.collabRepo.InsertEvent(tenantID, &collaboration.TaskEvent{ID: ackEventID, TaskID: task.ID, Event: EventGoalPushAck, ActorID: colleagueID, Note: ackNote, CreatedAt: now}); err != nil {
				return AckResult{}, err
			}
			return AckResult{EventID: eventID, TaskID: task.ID, AckEventID: ackEventID, Status: status, Note: strings.TrimSpace(note), CreatedAt: now}, nil
		}
	}
	return AckResult{}, fmt.Errorf("push not found for colleague: %s", eventID)
}

func (s *Service) hasRecentPush(tenantID, taskID string, now time.Time) bool {
	events, err := s.collabRepo.ListEvents(tenantID, taskID)
	if err != nil {
		return false
	}
	for _, event := range events {
		if event.Event != EventGoalPush {
			continue
		}
		if now.Sub(event.CreatedAt) < s.config.PushCooldown {
			return true
		}
	}
	return false
}

func (s *Service) recordPush(tenantID string, task *collaboration.Task, push Push, now time.Time) error {
	note := fmt.Sprintf("reason=%s age_seconds=%d to_colleague_id=%s role_code=%s", push.Reason, push.AgeSeconds, strings.TrimSpace(push.ToColleagueID), strings.TrimSpace(push.ToRoleCode))
	return s.collabRepo.InsertEvent(tenantID, &collaboration.TaskEvent{ID: idgen.New("gpush"), TaskID: task.ID, Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: note, CreatedAt: now})
}

func reasonForStatus(status string) string {
	switch status {
	case collaboration.StatusPending:
		return "task_not_accepted"
	case collaboration.StatusAccepted:
		return "task_not_started"
	case collaboration.StatusInProgress:
		return "task_in_progress_stalled"
	default:
		return "task_stalled"
	}
}

type Monitor struct {
	svc       *Service
	tenants   *tenant.TenantService
	stop      chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
}

func NewMonitor(svc *Service, tenants *tenant.TenantService) *Monitor {
	return &Monitor{svc: svc, tenants: tenants, stop: make(chan struct{})}
}

func (m *Monitor) Start() {
	if m == nil || m.svc == nil || m.tenants == nil {
		return
	}
	m.startOnce.Do(func() { go m.loop() })
}

func (m *Monitor) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stop) })
}

func (m *Monitor) loop() {
	interval := m.svc.Config().TickInterval
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.checkAll(now.UTC())
		}
	}
}

func (m *Monitor) checkAll(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tenants, err := m.tenants.ListActiveTenants(ctx)
	if err != nil {
		log.Printf("[goalwatch] list tenants failed: %v", err)
		return
	}
	for _, t := range tenants {
		if t == nil || strings.TrimSpace(t.ID) == "" {
			continue
		}
		result, err := m.svc.CheckTenant(t.ID, now)
		if err != nil {
			log.Printf("[goalwatch] check tenant %s failed: %v", t.ID, err)
			continue
		}
		if result.Pushed > 0 {
			log.Printf("[goalwatch] tenant=%s pushed=%d checked=%d", result.TenantID, result.Pushed, result.Checked)
		}
	}
}

func ackedPushIDs(events []*collaboration.TaskEvent) map[string]bool {
	acked := map[string]bool{}
	for _, event := range events {
		if event == nil || event.Event != EventGoalPushAck {
			continue
		}
		id := parseNoteValue(event.Note, "ack_event_id")
		if id != "" {
			acked[id] = true
		}
	}
	return acked
}

func normalizeAckStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "accepted", "resumed", "blocked":
		return strings.TrimSpace(status)
	default:
		return "accepted"
	}
}

func sanitizeNote(note string) string {
	note = strings.TrimSpace(note)
	note = strings.ReplaceAll(note, "\n", " ")
	note = strings.ReplaceAll(note, "\r", " ")
	return strings.ReplaceAll(note, " ", "_")
}

func parseNoteValue(note, key string) string {
	prefix := key + "="
	for _, field := range strings.Fields(note) {
		if strings.HasPrefix(field, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(field, prefix))
		}
	}
	return ""
}

func parseNoteInt64(note, key string) int64 {
	value := parseNoteValue(note, key)
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func normalizeTenantID(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return "default"
	}
	return strings.TrimSpace(tenantID)
}
