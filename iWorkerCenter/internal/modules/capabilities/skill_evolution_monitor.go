package capabilities

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/audit"
	tenantmodule "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

const DefaultSkillEvolutionInterval = 15 * time.Minute

type activeTenantLister interface {
	ListActiveTenants(ctx context.Context) ([]*tenantmodule.Tenant, error)
}

type SkillEvolutionMonitorConfig struct {
	Interval time.Duration
	Limit    int
}

type SkillEvolutionMonitorConfigStatus struct {
	TickIntervalSeconds int64 `json:"tick_interval_seconds"`
	DefaultLimit        int   `json:"default_limit"`
}

type SkillEvolutionTenantStatus struct {
	TenantID          string    `json:"tenant_id"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at"`
	AutomationEnabled bool      `json:"automation_enabled"`
	IntervalSeconds   int64     `json:"interval_seconds"`
	Limit             int       `json:"limit"`
	Scanned           int       `json:"scanned"`
	Attempted         int       `json:"attempted"`
	Published         int       `json:"published"`
	Skipped           int       `json:"skipped"`
	Failed            int       `json:"failed"`
	SkipReason        string    `json:"skip_reason,omitempty"`
	Error             string    `json:"error,omitempty"`
}

type SkillEvolutionMonitorStatus struct {
	Running bool                              `json:"running"`
	Config  SkillEvolutionMonitorConfigStatus `json:"config"`
	Tenants []SkillEvolutionTenantStatus      `json:"tenants"`
}

type SkillEvolutionMonitor struct {
	handler   *Handler
	tenants   activeTenantLister
	config    SkillEvolutionMonitorConfig
	stop      chan struct{}
	stopOnce  sync.Once
	startOnce sync.Once
	mu        sync.RWMutex
	running   bool
	statuses  map[string]SkillEvolutionTenantStatus
}

func NewSkillEvolutionMonitor(handler *Handler, tenants activeTenantLister, cfg SkillEvolutionMonitorConfig) *SkillEvolutionMonitor {
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultSkillEvolutionInterval
	}
	if cfg.Limit <= 0 || cfg.Limit > 100 {
		cfg.Limit = 20
	}
	return &SkillEvolutionMonitor{handler: handler, tenants: tenants, config: cfg, stop: make(chan struct{}), statuses: map[string]SkillEvolutionTenantStatus{}}
}

func (m *SkillEvolutionMonitor) Start() {
	if m == nil || m.handler == nil || m.tenants == nil {
		return
	}
	m.startOnce.Do(func() { go m.loop() })
}

func (m *SkillEvolutionMonitor) Stop() {
	if m == nil {
		return
	}
	m.stopOnce.Do(func() { close(m.stop) })
}

func (m *SkillEvolutionMonitor) Status() SkillEvolutionMonitorStatus {
	if m == nil {
		return SkillEvolutionMonitorStatus{}
	}
	status := SkillEvolutionMonitorStatus{Config: SkillEvolutionMonitorConfigStatus{TickIntervalSeconds: int64(m.config.Interval.Seconds()), DefaultLimit: m.config.Limit}}
	m.mu.RLock()
	defer m.mu.RUnlock()
	status.Running = m.running
	status.Tenants = make([]SkillEvolutionTenantStatus, 0, len(m.statuses))
	for _, tenantStatus := range m.statuses {
		status.Tenants = append(status.Tenants, tenantStatus)
	}
	sort.SliceStable(status.Tenants, func(i, j int) bool { return status.Tenants[i].TenantID < status.Tenants[j].TenantID })
	return status
}

func (m *SkillEvolutionMonitor) loop() {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-ticker.C:
			m.runAll(now.UTC())
		}
	}
}

func (m *SkillEvolutionMonitor) runAll(now time.Time) {
	if m == nil || m.handler == nil || m.tenants == nil {
		return
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !m.markRunning(true) {
		return
	}
	defer m.markRunning(false)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tenants, err := m.tenants.ListActiveTenants(ctx)
	if err != nil {
		log.Printf("[skill-evolution] list tenants failed: %v", err)
		return
	}
	for _, t := range tenants {
		if t == nil || strings.TrimSpace(t.ID) == "" {
			continue
		}
		m.runTenant(ctx, strings.TrimSpace(t.ID), now)
	}
}

func (m *SkillEvolutionMonitor) markRunning(running bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if running && m.running {
		return false
	}
	m.running = running
	return true
}

func (m *SkillEvolutionMonitor) runTenant(ctx context.Context, tenantID string, now time.Time) {
	startedAt := time.Now().UTC()
	status := SkillEvolutionTenantStatus{TenantID: tenantID, StartedAt: startedAt}
	rule, err := m.handler.GetSkillEvolutionAutomationRule(ctx, tenantID)
	if err != nil {
		status.Error = err.Error()
		status.FinishedAt = time.Now().UTC()
		m.recordStatus(status)
		m.recordAudit(tenantID, status)
		log.Printf("[skill-evolution] tenant %s read automation rule failed: %v", tenantID, err)
		return
	}
	status.AutomationEnabled = rule.Enabled
	status.IntervalSeconds = rule.IntervalSeconds
	status.Limit = rule.Limit
	if !rule.Enabled {
		status.SkipReason = "automation_disabled"
		status.FinishedAt = time.Now().UTC()
		m.recordStatus(status)
		m.recordAudit(tenantID, status)
		return
	}
	if !m.shouldRunTenant(tenantID, rule, now) {
		status.SkipReason = "interval_not_reached"
		status.FinishedAt = time.Now().UTC()
		m.recordStatus(status)
		m.recordAudit(tenantID, status)
		return
	}
	reqCtx := tenantmodule.WithTenantID(ctx, tenantID)
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodPost, "/internal/skillmarket/evolution-run", nil)
	summary, err := m.handler.RunSkillEvolution(req, tenantID, SkillEvolutionRunRequest{Limit: rule.Limit})
	if err != nil {
		status.Error = err.Error()
		log.Printf("[skill-evolution] tenant %s run failed: %v", tenantID, err)
	} else {
		status.Scanned = summary.Scanned
		status.Attempted = summary.Attempted
		status.Published = summary.Published
		status.Skipped = summary.Skipped
		status.Failed = summary.Failed
	}
	status.FinishedAt = time.Now().UTC()
	m.recordStatus(status)
	m.recordAudit(tenantID, status)
}

func (m *SkillEvolutionMonitor) shouldRunTenant(tenantID string, rule SkillEvolutionAutomationRule, now time.Time) bool {
	last, err := m.handler.lastSkillEvolutionRun(context.Background(), tenantID)
	if err != nil || last == nil || strings.TrimSpace(last.FinishedAt) == "" {
		return true
	}
	finishedAt, err := time.Parse(time.RFC3339Nano, last.FinishedAt)
	if err != nil {
		return true
	}
	return !finishedAt.Add(time.Duration(rule.IntervalSeconds) * time.Second).After(now)
}

func (m *SkillEvolutionMonitor) recordStatus(status SkillEvolutionTenantStatus) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statuses == nil {
		m.statuses = map[string]SkillEvolutionTenantStatus{}
	}
	m.statuses[status.TenantID] = status
}

func (m *SkillEvolutionMonitor) recordAudit(tenantID string, status SkillEvolutionTenantStatus) {
	if m == nil || m.handler == nil || m.handler.audit == nil || strings.TrimSpace(tenantID) == "" {
		return
	}
	auditStatus := "ok"
	if strings.TrimSpace(status.Error) != "" {
		auditStatus = "error"
	}
	summary := "skill evolution monitor run"
	if status.SkipReason != "" {
		summary = "skill evolution monitor skipped: " + status.SkipReason
	}
	detail := fmt.Sprintf("tenant=%s enabled=%v interval_seconds=%d limit=%d scanned=%d attempted=%d published=%d skipped=%d failed=%d skip_reason=%s",
		tenantID, status.AutomationEnabled, status.IntervalSeconds, status.Limit, status.Scanned, status.Attempted, status.Published, status.Skipped, status.Failed, status.SkipReason)
	if status.Error != "" {
		detail += " error=" + status.Error
	}
	_ = m.handler.audit.Insert(tenantID, &audit.ProxyLog{
		RequestID:  fmt.Sprintf("skill-evolution-monitor-%s-%d", tenantID, time.Now().UnixNano()),
		ProviderID: "iworkercenter",
		Model:      "skill-evolution-monitor",
		WorkType:   "skill_evolution_monitor",
		CostTier:   "internal",
		Status:     auditStatus,
		Summary:    summary,
		ErrorMsg:   detail,
		CreatedAt:  time.Now().UTC(),
	})
}
