package goalwatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/agentruntime"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/collaboration"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
)

const (
	EventGoalPush          = "goal_push"
	EventGoalPushAck       = "goal_push_ack"
	DefaultStalledAfter    = 5 * time.Minute
	DefaultPushCooldown    = 10 * time.Minute
	DefaultTickInterval    = 1 * time.Minute
	DefaultWorkersPerShard = 50
	DefaultMaxWatchers     = 16
	DefaultLeaseTTL        = 2 * time.Minute
)

// TenantPolicy is the persisted per-tenant GoalWatcher run-loop policy.
// It is stored in system_settings so hot-standby Center nodes can read the same
// organization runtime intent without inventing a separate config store.
type TenantPolicy struct {
	Enabled             bool  `json:"enabled"`
	SingleFlight        bool  `json:"single_flight"`
	MaxRunSeconds       int   `json:"max_run_seconds"`
	ScaleByWorkerCount  bool  `json:"scale_by_worker_count"`
	TickIntervalSeconds int64 `json:"tick_interval_seconds"`
	StalledAfterSeconds int64 `json:"stalled_after_seconds"`
	PushCooldownSeconds int64 `json:"push_cooldown_seconds"`
	LeaseTTLSeconds     int64 `json:"lease_ttl_seconds"`
	WorkersPerShard     int   `json:"workers_per_shard"`
	MaxWatchers         int   `json:"max_watchers"`
}
type Config struct {
	StalledAfter    time.Duration
	PushCooldown    time.Duration
	TickInterval    time.Duration
	WorkersPerShard int
	MaxWatchers     int
	LeaseTTL        time.Duration
}

type Service struct {
	collabRepo   *collaboration.Repo
	agentRuntime *agentruntime.Service
	config       Config
}

type Push struct {
	EventID                     string    `json:"event_id,omitempty"`
	TaskID                      string    `json:"task_id"`
	WorkflowStepInstanceID      string    `json:"workflow_step_instance_id,omitempty"`
	Title                       string    `json:"title"`
	ToColleagueID               string    `json:"to_colleague_id"`
	ToRoleCode                  string    `json:"to_role_code"`
	Status                      string    `json:"status"`
	Reason                      string    `json:"reason"`
	RecommendedAction           string    `json:"recommended_action"`
	RecoveryAction              string    `json:"recovery_action,omitempty"`
	RecoveryMethod              string    `json:"recovery_method,omitempty"`
	RecoveryPath                string    `json:"recovery_path,omitempty"`
	AgeSeconds                  int64     `json:"age_seconds"`
	ExecutorStatus              string    `json:"executor_status,omitempty"`
	ExecutorHeartbeatAgeSeconds int64     `json:"executor_heartbeat_age_seconds,omitempty"`
	CreatedAt                   time.Time `json:"created_at"`
}

type executorHealth struct {
	Known               bool
	Unavailable         bool
	Status              string
	HeartbeatAgeSeconds int64
}

type CheckResult struct {
	TenantID string `json:"tenant_id"`
	Checked  int    `json:"checked"`
	Pushed   int    `json:"pushed"`
	Pushes   []Push `json:"pushes"`
}

type MonitorConfigStatus struct {
	TickIntervalSeconds int64 `json:"tick_interval_seconds"`
	StalledAfterSeconds int64 `json:"stalled_after_seconds"`
	PushCooldownSeconds int64 `json:"push_cooldown_seconds"`
	LeaseTTLSeconds     int64 `json:"lease_ttl_seconds"`
	WorkersPerShard     int   `json:"workers_per_shard"`
	MaxWatchers         int   `json:"max_watchers"`
}

type MonitorShardStatus struct {
	ShardIndex int    `json:"shard_index"`
	ShardCount int    `json:"shard_count"`
	Checked    int    `json:"checked"`
	Pushed     int    `json:"pushed"`
	LeaseOwner string `json:"lease_owner,omitempty"`
	LeaseHeld  bool   `json:"lease_held"`
	Error      string `json:"error,omitempty"`
}

type shardLease struct {
	Owner     string `json:"owner"`
	ExpiresAt string `json:"expires_at"`
}

type TenantMonitorStatus struct {
	TenantID      string               `json:"tenant_id"`
	StartedAt     time.Time            `json:"started_at"`
	FinishedAt    time.Time            `json:"finished_at"`
	IWorkerCount  int                  `json:"iworker_count"`
	ShardCount    int                  `json:"shard_count"`
	Checked       int                  `json:"checked"`
	Pushed        int                  `json:"pushed"`
	Error         string               `json:"error,omitempty"`
	ShardStatuses []MonitorShardStatus `json:"shards"`
}

type MonitorStatus struct {
	Config  MonitorConfigStatus   `json:"config"`
	Tenants []TenantMonitorStatus `json:"tenants"`
}

type MonitorHealth struct {
	Level                 string              `json:"level"`
	Reasons               []string            `json:"reasons"`
	RecommendedActions    []string            `json:"recommended_actions"`
	Config                MonitorConfigStatus `json:"config"`
	TenantCount           int                 `json:"tenant_count"`
	LastRunAgeSeconds     int64               `json:"last_run_age_seconds,omitempty"`
	StaleThresholdSeconds int64               `json:"stale_threshold_seconds,omitempty"`
	Status                MonitorStatus       `json:"status"`
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
	if cfg.WorkersPerShard <= 0 {
		cfg.WorkersPerShard = DefaultWorkersPerShard
	}
	if cfg.MaxWatchers <= 0 {
		cfg.MaxWatchers = DefaultMaxWatchers
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = DefaultLeaseTTL
	}
	return &Service{collabRepo: collabRepo, config: cfg}
}

func (s *Service) Config() Config { return s.config }
func (s *Service) DefaultTenantPolicy() TenantPolicy {
	cfg := s.Config()
	return TenantPolicy{
		Enabled:             true,
		SingleFlight:        true,
		MaxRunSeconds:       int(cfg.TickInterval.Seconds()),
		ScaleByWorkerCount:  true,
		TickIntervalSeconds: int64(cfg.TickInterval.Seconds()),
		StalledAfterSeconds: int64(cfg.StalledAfter.Seconds()),
		PushCooldownSeconds: int64(cfg.PushCooldown.Seconds()),
		LeaseTTLSeconds:     int64(cfg.LeaseTTL.Seconds()),
		WorkersPerShard:     cfg.WorkersPerShard,
		MaxWatchers:         cfg.MaxWatchers,
	}
}

func (s *Service) SaveTenantPolicy(ctx context.Context, tenantID string, policy TenantPolicy) (TenantPolicy, error) {
	if s == nil || s.collabRepo == nil || s.collabRepo.WriteDB() == nil {
		return policy, fmt.Errorf("goalwatch settings store is unavailable")
	}
	policy = s.normalizeTenantPolicy(policy)
	data, err := json.Marshal(policy)
	if err != nil {
		return policy, err
	}
	key := goalWatchTenantPolicyKey(tenantID)
	res, err := s.collabRepo.WriteDB().ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
	if err != nil {
		return policy, err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return policy, nil
	}
	_, err = s.collabRepo.WriteDB().ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data))
	return policy, err
}

func (s *Service) GetTenantPolicy(ctx context.Context, tenantID string) (TenantPolicy, bool, error) {
	if s == nil || s.collabRepo == nil || s.collabRepo.ReadDB() == nil {
		return TenantPolicy{}, false, fmt.Errorf("goalwatch settings store is unavailable")
	}
	var raw string
	err := s.collabRepo.ReadDB().QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, goalWatchTenantPolicyKey(tenantID)).Scan(&raw)
	if err == sql.ErrNoRows {
		return s.DefaultTenantPolicy(), false, nil
	}
	if err != nil {
		return TenantPolicy{}, false, err
	}
	var policy TenantPolicy
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &policy); err != nil {
			return TenantPolicy{}, false, err
		}
	}
	return s.normalizeTenantPolicy(policy), true, nil
}

func (s *Service) normalizeTenantPolicy(policy TenantPolicy) TenantPolicy {
	defaults := s.DefaultTenantPolicy()
	if policy.MaxRunSeconds <= 0 {
		policy.MaxRunSeconds = defaults.MaxRunSeconds
	}
	if policy.TickIntervalSeconds <= 0 {
		policy.TickIntervalSeconds = defaults.TickIntervalSeconds
	}
	if policy.StalledAfterSeconds <= 0 {
		policy.StalledAfterSeconds = defaults.StalledAfterSeconds
	}
	if policy.PushCooldownSeconds <= 0 {
		policy.PushCooldownSeconds = defaults.PushCooldownSeconds
	}
	if policy.LeaseTTLSeconds <= 0 {
		policy.LeaseTTLSeconds = defaults.LeaseTTLSeconds
	}
	if policy.WorkersPerShard <= 0 {
		policy.WorkersPerShard = defaults.WorkersPerShard
	}
	if policy.MaxWatchers <= 0 {
		policy.MaxWatchers = defaults.MaxWatchers
	}
	if !policy.Enabled {
		policy.Enabled = defaults.Enabled
	}
	if !policy.SingleFlight {
		policy.SingleFlight = defaults.SingleFlight
	}
	if !policy.ScaleByWorkerCount {
		policy.ScaleByWorkerCount = defaults.ScaleByWorkerCount
	}
	return policy
}

func (s *Service) SetAgentRuntime(runtime *agentruntime.Service) {
	if s != nil {
		s.agentRuntime = runtime
	}
}

func (s *Service) CheckTenant(tenantID string, now time.Time) (CheckResult, error) {
	return s.CheckTenantShard(tenantID, now, 0, 1)
}

func (s *Service) CheckTenantShard(tenantID string, now time.Time, shardIndex, shardCount int) (CheckResult, error) {
	if s == nil || s.collabRepo == nil {
		return CheckResult{}, fmt.Errorf("goalwatch collaboration repo is unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if shardCount <= 0 {
		shardCount = 1
	}
	if shardIndex < 0 || shardIndex >= shardCount {
		shardIndex = 0
	}
	tenantID = normalizeTenantID(tenantID)
	result := CheckResult{TenantID: tenantID, Pushes: []Push{}}
	tasks, err := s.collabRepo.ListAll(tenantID)
	if err != nil {
		return result, err
	}
	for _, task := range tasks {
		if task == nil || task.IsTerminal() || taskShard(task.ID, shardCount) != shardIndex {
			continue
		}
		pushed, push, err := s.checkTask(tenantID, task, now)
		if err != nil {
			return result, err
		}
		result.Checked++
		if pushed {
			result.Pushes = append(result.Pushes, push)
			result.Pushed++
		}
	}
	return result, nil
}

func (s *Service) checkTask(tenantID string, task *collaboration.Task, now time.Time) (bool, Push, error) {
	age := now.Sub(task.UpdatedAt)
	if age < 0 {
		age = 0
	}
	executor := s.assignedExecutorHealth(tenantID, task.ToColleagueID, now)
	if age < s.config.StalledAfter && !executor.Unavailable {
		return false, Push{}, nil
	}
	if s.hasRecentPush(tenantID, task.ID, now) {
		return false, Push{}, nil
	}
	reason := reasonForStatus(task.Status)
	if executor.Unavailable {
		reason = "assigned_executor_offline"
	}
	push := Push{TaskID: task.ID, WorkflowStepInstanceID: task.WorkflowStepInstanceID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: reason, RecommendedAction: recommendedActionForTask(task, reason), AgeSeconds: int64(age.Seconds()), ExecutorStatus: executor.Status, ExecutorHeartbeatAgeSeconds: executor.HeartbeatAgeSeconds, CreatedAt: now}
	push = enrichRecoveryFields(push)
	inserted, err := s.recordPush(tenantID, task, push, now)
	if err != nil {
		return false, Push{}, err
	}
	return inserted, push, nil
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
			reason := parseNoteValue(event.Note, "reason")
			workflowStepID := firstNonEmpty(parseNoteValue(event.Note, "workflow_step_instance_id"), task.WorkflowStepInstanceID)
			action := firstNonEmpty(parseNoteValue(event.Note, "recommended_action"), recommendedActionForTask(task, reason))
			push := Push{EventID: event.ID, TaskID: task.ID, WorkflowStepInstanceID: workflowStepID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: reason, RecommendedAction: normalizeRecommendedActionForWorkflow(action, reason, task.Status, workflowStepID), RecoveryAction: parseNoteValue(event.Note, "recovery_action"), RecoveryMethod: parseNoteValue(event.Note, "recovery_method"), RecoveryPath: parseNoteValue(event.Note, "recovery_path"), AgeSeconds: parseNoteInt64(event.Note, "age_seconds"), ExecutorStatus: parseNoteValue(event.Note, "executor_status"), ExecutorHeartbeatAgeSeconds: parseNoteInt64(event.Note, "executor_heartbeat_age_seconds"), CreatedAt: event.CreatedAt}
			pushes = append(pushes, enrichRecoveryFields(push))
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

func (s *Service) assignedExecutorHealth(tenantID, workerID string, now time.Time) executorHealth {
	if s == nil || s.agentRuntime == nil || strings.TrimSpace(workerID) == "" {
		return executorHealth{}
	}
	instances, err := s.agentRuntime.ListWithHealth(tenantID, workerID, now, agentruntime.DefaultOfflineAfter)
	if err != nil || len(instances) == 0 {
		return executorHealth{}
	}
	for _, instance := range instances {
		if instance.Role != "executor" {
			continue
		}
		status := firstNonEmpty(instance.EffectiveStatus, instance.Status, "unknown")
		health := executorHealth{Known: true, Status: status, HeartbeatAgeSeconds: instance.HeartbeatAgeSeconds}
		switch status {
		case "online", "busy", "idle":
			health.Unavailable = false
		default:
			health.Unavailable = true
		}
		return health
	}
	return executorHealth{Known: true, Unavailable: true, Status: "missing"}
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

func (s *Service) recordPush(tenantID string, task *collaboration.Task, push Push, now time.Time) (bool, error) {
	if s.hasRecentPush(tenantID, task.ID, now) {
		return false, nil
	}
	push = enrichRecoveryFields(push)
	note := fmt.Sprintf("reason=%s recommended_action=%s age_seconds=%d to_colleague_id=%s role_code=%s", push.Reason, firstNonEmpty(push.RecommendedAction, recommendedActionFor(push.Reason, push.Status)), push.AgeSeconds, strings.TrimSpace(push.ToColleagueID), strings.TrimSpace(push.ToRoleCode))
	if strings.TrimSpace(push.WorkflowStepInstanceID) != "" {
		note += fmt.Sprintf(" workflow_step_instance_id=%s", strings.TrimSpace(push.WorkflowStepInstanceID))
	}
	if strings.TrimSpace(push.RecoveryAction) != "" {
		note += fmt.Sprintf(" recovery_action=%s recovery_method=%s recovery_path=%s", strings.TrimSpace(push.RecoveryAction), strings.TrimSpace(push.RecoveryMethod), strings.TrimSpace(push.RecoveryPath))
	}
	if strings.TrimSpace(push.ExecutorStatus) != "" {
		note += fmt.Sprintf(" executor_status=%s executor_heartbeat_age_seconds=%d", strings.TrimSpace(push.ExecutorStatus), push.ExecutorHeartbeatAgeSeconds)
	}
	eventID := deterministicPushEventID(tenantID, task.ID, now, s.config.PushCooldown)
	err := s.collabRepo.InsertEvent(tenantID, &collaboration.TaskEvent{ID: eventID, TaskID: task.ID, Event: EventGoalPush, ActorID: "iworkercenter.goalwatch", Note: note, CreatedAt: now})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func deterministicPushEventID(tenantID, taskID string, now time.Time, cooldown time.Duration) string {
	if cooldown <= 0 {
		cooldown = DefaultPushCooldown
	}
	bucket := now.Unix() / int64(cooldown.Seconds())
	return fmt.Sprintf("gpush_%x", hashString(normalizeTenantID(tenantID)+"|"+strings.TrimSpace(taskID)+"|"+strconv.FormatInt(bucket, 10)))
}

func taskShard(taskID string, shardCount int) int {
	if shardCount <= 1 {
		return 0
	}
	return int(hashString(taskID) % uint64(shardCount))
}

func hashString(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
}

func recommendedActionFor(reason, status string) string {
	switch strings.TrimSpace(reason) {
	case "assigned_executor_offline":
		return "restart_executor"
	case "task_not_accepted":
		return "accept_task"
	case "task_not_started":
		return "start_task"
	case "task_in_progress_stalled":
		return "resume_task"
	default:
		switch strings.TrimSpace(status) {
		case collaboration.StatusPending:
			return "accept_task"
		case collaboration.StatusAccepted:
			return "start_task"
		default:
			return "resume_task"
		}
	}
}

func recommendedActionForTask(task *collaboration.Task, reason string) string {
	if task == nil {
		return recommendedActionFor(reason, "")
	}
	return normalizeRecommendedActionForWorkflow(recommendedActionFor(reason, task.Status), reason, task.Status, task.WorkflowStepInstanceID)
}

func normalizeRecommendedActionForWorkflow(action, reason, status, workflowStepID string) string {
	if strings.TrimSpace(workflowStepID) == "" {
		return action
	}
	switch strings.TrimSpace(action) {
	case "accept_task", "start_task":
		return "start_workflow_step"
	case "resume_task":
		return "resume_workflow_step"
	}
	switch strings.TrimSpace(reason) {
	case "task_not_accepted", "task_not_started":
		return "start_workflow_step"
	case "task_in_progress_stalled":
		return "resume_workflow_step"
	}
	switch strings.TrimSpace(status) {
	case collaboration.StatusPending, collaboration.StatusAccepted:
		return "start_workflow_step"
	case collaboration.StatusInProgress:
		return "resume_workflow_step"
	default:
		return action
	}
}

func enrichRecoveryFields(push Push) Push {
	if strings.TrimSpace(push.WorkflowStepInstanceID) == "" || strings.TrimSpace(push.RecommendedAction) == "" {
		return push
	}
	if strings.TrimSpace(push.RecoveryAction) == "" {
		push.RecoveryAction = push.RecommendedAction
	}
	if strings.TrimSpace(push.RecoveryMethod) == "" {
		push.RecoveryMethod = httpMethodForRecoveryAction(push.RecoveryAction)
	}
	if strings.TrimSpace(push.RecoveryPath) == "" {
		push.RecoveryPath = recoveryPathForWorkflowAction(push.RecoveryAction, push.WorkflowStepInstanceID)
	}
	return push
}

func httpMethodForRecoveryAction(action string) string {
	switch strings.TrimSpace(action) {
	case "start_workflow_step", "resume_workflow_step":
		return "POST"
	default:
		return ""
	}
}

func recoveryPathForWorkflowAction(action, workflowStepID string) string {
	workflowStepID = strings.TrimSpace(workflowStepID)
	if workflowStepID == "" {
		return ""
	}
	switch strings.TrimSpace(action) {
	case "start_workflow_step":
		return "/runtime/workflows/steps/" + workflowStepID + "/start"
	case "resume_workflow_step":
		return "/runtime/workflows/steps/" + workflowStepID + "/resume"
	default:
		return ""
	}
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
	mu        sync.RWMutex
	statuses  map[string]TenantMonitorStatus
}

func NewMonitor(svc *Service, tenants *tenant.TenantService) *Monitor {
	return &Monitor{svc: svc, tenants: tenants, stop: make(chan struct{}), statuses: map[string]TenantMonitorStatus{}}
}

func (m *Monitor) Status() MonitorStatus {
	status := MonitorStatus{}
	if m == nil || m.svc == nil {
		return status
	}
	cfg := m.svc.Config()
	status.Config = MonitorConfigStatus{TickIntervalSeconds: int64(cfg.TickInterval.Seconds()), StalledAfterSeconds: int64(cfg.StalledAfter.Seconds()), PushCooldownSeconds: int64(cfg.PushCooldown.Seconds()), LeaseTTLSeconds: int64(cfg.LeaseTTL.Seconds()), WorkersPerShard: cfg.WorkersPerShard, MaxWatchers: cfg.MaxWatchers}
	m.mu.RLock()
	defer m.mu.RUnlock()
	status.Tenants = make([]TenantMonitorStatus, 0, len(m.statuses))
	for _, tenantStatus := range m.statuses {
		status.Tenants = append(status.Tenants, tenantStatus)
	}
	sort.SliceStable(status.Tenants, func(i, j int) bool { return status.Tenants[i].TenantID < status.Tenants[j].TenantID })
	return status
}

func (m *Monitor) Health(now time.Time) MonitorHealth {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	status := m.Status()
	health := MonitorHealth{Level: "healthy", Reasons: []string{}, RecommendedActions: []string{}, Config: status.Config, TenantCount: len(status.Tenants), Status: status}
	threshold := goalWatchStaleThreshold(time.Duration(status.Config.TickIntervalSeconds) * time.Second)
	health.StaleThresholdSeconds = int64(threshold.Seconds())
	if len(status.Tenants) == 0 {
		health.Level = "warning"
		health.Reasons = append(health.Reasons, "no_recent_goalwatch_runs")
		health.RecommendedActions = append(health.RecommendedActions, "check_goalwatch_monitor_startup_and_tenant_configuration")
		return health
	}
	var latest time.Time
	allShardsSkippedByLease := true
	for _, tenantStatus := range status.Tenants {
		if tenantStatus.FinishedAt.After(latest) {
			latest = tenantStatus.FinishedAt
		}
		if strings.TrimSpace(tenantStatus.Error) != "" {
			health.Level = "critical"
			health.Reasons = appendUniqueString(health.Reasons, "goalwatch_errors_detected")
			health.RecommendedActions = appendUniqueString(health.RecommendedActions, "inspect_goalwatch_status_shard_errors")
		}
		for _, shard := range tenantStatus.ShardStatuses {
			if strings.TrimSpace(shard.Error) != "" {
				health.Level = "critical"
				health.Reasons = appendUniqueString(health.Reasons, "goalwatch_errors_detected")
				health.RecommendedActions = appendUniqueString(health.RecommendedActions, "inspect_goalwatch_status_shard_errors")
			}
			if shard.LeaseHeld || shard.Checked > 0 || shard.Pushed > 0 {
				allShardsSkippedByLease = false
			}
		}
	}
	if !latest.IsZero() {
		age := int64(now.Sub(latest).Seconds())
		if age < 0 {
			age = 0
		}
		health.LastRunAgeSeconds = age
		if time.Duration(age)*time.Second > threshold {
			if health.Level == "healthy" {
				health.Level = "warning"
			}
			health.Reasons = appendUniqueString(health.Reasons, "goalwatch_stale")
			health.RecommendedActions = appendUniqueString(health.RecommendedActions, "check_goalwatch_scheduler_and_process_health")
		}
	}
	if allShardsSkippedByLease {
		if health.Level == "healthy" {
			health.Level = "warning"
		}
		health.Reasons = appendUniqueString(health.Reasons, "all_goalwatch_shards_skipped_by_active_lease")
		health.RecommendedActions = appendUniqueString(health.RecommendedActions, "check_peer_iworkercenter_goalwatch_status")
	}
	return health
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
		m.checkTenantSharded(t.ID, now)
	}
}

func (m *Monitor) checkTenantSharded(tenantID string, now time.Time) {
	startedAt := time.Now().UTC()
	iworkerCount := m.knownIWorkerCount(tenantID, now)
	shardCount := m.recommendedShardCountForIWorkers(iworkerCount)
	status := TenantMonitorStatus{TenantID: normalizeTenantID(tenantID), StartedAt: startedAt, IWorkerCount: iworkerCount, ShardCount: shardCount, ShardStatuses: []MonitorShardStatus{}}
	if shardCount <= 1 {
		owner, acquired, err := m.svc.acquireShardLease(context.Background(), tenantID, 0, 1, now)
		if err != nil {
			log.Printf("[goalwatch] acquire tenant %s shard=0/1 lease failed: %v", tenantID, err)
			status.Error = err.Error()
			status.FinishedAt = time.Now().UTC()
			status.ShardStatuses = append(status.ShardStatuses, MonitorShardStatus{ShardIndex: 0, ShardCount: 1, Error: err.Error()})
			m.recordStatus(status)
			return
		}
		if !acquired {
			status.FinishedAt = time.Now().UTC()
			status.ShardStatuses = append(status.ShardStatuses, MonitorShardStatus{ShardIndex: 0, ShardCount: 1})
			m.recordStatus(status)
			return
		}
		defer m.svc.releaseShardLease(context.Background(), tenantID, 0, 1, owner)
		result, err := m.svc.CheckTenant(tenantID, now)
		if err != nil {
			log.Printf("[goalwatch] check tenant %s failed: %v", tenantID, err)
			status.Error = err.Error()
			status.FinishedAt = time.Now().UTC()
			status.ShardStatuses = append(status.ShardStatuses, MonitorShardStatus{ShardIndex: 0, ShardCount: 1, LeaseOwner: owner, LeaseHeld: true, Error: err.Error()})
			m.recordStatus(status)
			return
		}
		status.Checked = result.Checked
		status.Pushed = result.Pushed
		status.FinishedAt = time.Now().UTC()
		status.ShardStatuses = append(status.ShardStatuses, MonitorShardStatus{ShardIndex: 0, ShardCount: 1, Checked: result.Checked, Pushed: result.Pushed, LeaseOwner: owner, LeaseHeld: true})
		m.recordStatus(status)
		if result.Pushed > 0 {
			log.Printf("[goalwatch] tenant=%s pushed=%d checked=%d watchers=1", result.TenantID, result.Pushed, result.Checked)
		}
		return
	}
	var wg sync.WaitGroup
	results := make(chan MonitorShardStatus, shardCount)
	for shard := 0; shard < shardCount; shard++ {
		shard := shard
		wg.Add(1)
		go func() {
			defer wg.Done()
			owner, acquired, err := m.svc.acquireShardLease(context.Background(), tenantID, shard, shardCount, now)
			if err != nil {
				log.Printf("[goalwatch] acquire tenant %s shard=%d/%d lease failed: %v", tenantID, shard, shardCount, err)
				results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount, Error: err.Error()}
				return
			}
			if !acquired {
				results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount}
				return
			}
			defer m.svc.releaseShardLease(context.Background(), tenantID, shard, shardCount, owner)
			result, err := m.svc.CheckTenantShard(tenantID, now, shard, shardCount)
			if err != nil {
				log.Printf("[goalwatch] check tenant %s shard=%d/%d failed: %v", tenantID, shard, shardCount, err)
				results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount, LeaseOwner: owner, LeaseHeld: true, Error: err.Error()}
				return
			}
			results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount, Checked: result.Checked, Pushed: result.Pushed, LeaseOwner: owner, LeaseHeld: true}
		}()
	}
	wg.Wait()
	close(results)
	for shardStatus := range results {
		status.Checked += shardStatus.Checked
		status.Pushed += shardStatus.Pushed
		if shardStatus.Error != "" {
			status.Error = firstNonEmpty(status.Error, "one or more shards failed")
		}
		status.ShardStatuses = append(status.ShardStatuses, shardStatus)
	}
	sort.SliceStable(status.ShardStatuses, func(i, j int) bool { return status.ShardStatuses[i].ShardIndex < status.ShardStatuses[j].ShardIndex })
	status.FinishedAt = time.Now().UTC()
	m.recordStatus(status)
	if status.Pushed > 0 {
		log.Printf("[goalwatch] tenant=%s pushed=%d checked=%d watchers=%d", status.TenantID, status.Pushed, status.Checked, shardCount)
	}
}

func (m *Monitor) recommendedShardCount(tenantID string, now time.Time) int {
	return m.recommendedShardCountForIWorkers(m.knownIWorkerCount(tenantID, now))
}

func (m *Monitor) recommendedShardCountForIWorkers(iworkers int) int {
	cfg := m.svc.Config()
	workersPerShard := cfg.WorkersPerShard
	if workersPerShard <= 0 {
		workersPerShard = DefaultWorkersPerShard
	}
	maxWatchers := cfg.MaxWatchers
	if maxWatchers <= 0 {
		maxWatchers = DefaultMaxWatchers
	}
	if iworkers <= 0 {
		return 1
	}
	shards := (iworkers + workersPerShard - 1) / workersPerShard
	if shards < 1 {
		shards = 1
	}
	if shards > maxWatchers {
		shards = maxWatchers
	}
	return shards
}

func (s *Service) acquireShardLease(ctx context.Context, tenantID string, shardIndex, shardCount int, now time.Time) (string, bool, error) {
	if s == nil || s.collabRepo == nil || s.collabRepo.WriteDB() == nil || s.collabRepo.ReadDB() == nil {
		return "", true, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ttl := s.config.LeaseTTL
	if ttl <= 0 {
		ttl = DefaultLeaseTTL
	}
	owner := goalWatchLeaseOwner()
	key := goalWatchShardLeaseKey(tenantID, shardIndex, shardCount)
	expiresAt := now.Add(ttl).Format(time.RFC3339Nano)
	tx, err := s.collabRepo.WriteDB().BeginTx(ctx, nil)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		data, _ := json.Marshal(shardLease{Owner: owner, ExpiresAt: expiresAt})
		if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, key, string(data)); err != nil {
			return "", false, err
		}
		if err := tx.Commit(); err != nil {
			return "", false, err
		}
		return owner, true, nil
	}
	if err != nil {
		return "", false, err
	}
	var current shardLease
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &current)
	}
	if expires, err := time.Parse(time.RFC3339Nano, current.ExpiresAt); err == nil && expires.After(now) && current.Owner != owner {
		return "", false, nil
	}
	data, _ := json.Marshal(shardLease{Owner: owner, ExpiresAt: expiresAt})
	if _, err := tx.ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return owner, true, nil
}

func (s *Service) releaseShardLease(ctx context.Context, tenantID string, shardIndex, shardCount int, owner string) {
	if s == nil || s.collabRepo == nil || s.collabRepo.WriteDB() == nil || s.collabRepo.ReadDB() == nil || strings.TrimSpace(owner) == "" {
		return
	}
	key := goalWatchShardLeaseKey(tenantID, shardIndex, shardCount)
	var raw string
	if err := s.collabRepo.ReadDB().QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key=?`, key).Scan(&raw); err != nil {
		return
	}
	var current shardLease
	if err := json.Unmarshal([]byte(raw), &current); err != nil || current.Owner != owner {
		return
	}
	data, _ := json.Marshal(shardLease{Owner: owner, ExpiresAt: time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)})
	_, _ = s.collabRepo.WriteDB().ExecContext(ctx, `UPDATE system_settings SET value_json=?, updated_at=datetime('now') WHERE key=?`, string(data), key)
}

func goalWatchTenantPolicyKey(tenantID string) string {
	return "goalwatch_policy:" + normalizeTenantID(tenantID)
}
func goalWatchShardLeaseKey(tenantID string, shardIndex, shardCount int) string {
	return fmt.Sprintf("goalwatch_shard_lease:%s:%d:%d", normalizeTenantID(tenantID), shardCount, shardIndex)
}

func goalWatchLeaseOwner() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", strings.TrimSpace(host), os.Getpid())
}

func goalWatchStaleThreshold(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = DefaultTickInterval
	}
	threshold := interval * 3
	minimum := interval + 2*time.Minute
	if threshold < minimum {
		threshold = minimum
	}
	return threshold
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (m *Monitor) recordStatus(status TenantMonitorStatus) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.statuses == nil {
		m.statuses = map[string]TenantMonitorStatus{}
	}
	m.statuses[status.TenantID] = status
}

func (m *Monitor) knownIWorkerCount(tenantID string, now time.Time) int {
	if m == nil || m.svc == nil || m.svc.agentRuntime == nil {
		return 0
	}
	instances, err := m.svc.agentRuntime.ListWithHealth(tenantID, "", now, agentruntime.DefaultOfflineAfter)
	if err != nil {
		return 0
	}
	seen := map[string]bool{}
	for _, instance := range instances {
		if strings.TrimSpace(instance.WorkerID) != "" {
			seen[strings.TrimSpace(instance.WorkerID)] = true
		}
	}
	return len(seen)
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
	case "accepted", "resumed", "blocked", "recovered":
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeTenantID(tenantID string) string {
	if strings.TrimSpace(tenantID) == "" {
		return "default"
	}
	return strings.TrimSpace(tenantID)
}
