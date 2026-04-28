package goalwatch

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
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
)

type Config struct {
	StalledAfter    time.Duration
	PushCooldown    time.Duration
	TickInterval    time.Duration
	WorkersPerShard int
	MaxWatchers     int
}

type Service struct {
	collabRepo   *collaboration.Repo
	agentRuntime *agentruntime.Service
	config       Config
}

type Push struct {
	EventID                     string    `json:"event_id,omitempty"`
	TaskID                      string    `json:"task_id"`
	Title                       string    `json:"title"`
	ToColleagueID               string    `json:"to_colleague_id"`
	ToRoleCode                  string    `json:"to_role_code"`
	Status                      string    `json:"status"`
	Reason                      string    `json:"reason"`
	RecommendedAction           string    `json:"recommended_action"`
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
	WorkersPerShard     int   `json:"workers_per_shard"`
	MaxWatchers         int   `json:"max_watchers"`
}

type MonitorShardStatus struct {
	ShardIndex int    `json:"shard_index"`
	ShardCount int    `json:"shard_count"`
	Checked    int    `json:"checked"`
	Pushed     int    `json:"pushed"`
	Error      string `json:"error,omitempty"`
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
	return &Service{collabRepo: collabRepo, config: cfg}
}

func (s *Service) Config() Config { return s.config }

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
	push := Push{TaskID: task.ID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: reason, RecommendedAction: recommendedActionFor(reason, task.Status), AgeSeconds: int64(age.Seconds()), ExecutorStatus: executor.Status, ExecutorHeartbeatAgeSeconds: executor.HeartbeatAgeSeconds, CreatedAt: now}
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
			action := firstNonEmpty(parseNoteValue(event.Note, "recommended_action"), recommendedActionFor(reason, task.Status))
			pushes = append(pushes, Push{EventID: event.ID, TaskID: task.ID, Title: task.Title, ToColleagueID: task.ToColleagueID, ToRoleCode: task.ToRoleCode, Status: task.Status, Reason: reason, RecommendedAction: action, AgeSeconds: parseNoteInt64(event.Note, "age_seconds"), ExecutorStatus: parseNoteValue(event.Note, "executor_status"), ExecutorHeartbeatAgeSeconds: parseNoteInt64(event.Note, "executor_heartbeat_age_seconds"), CreatedAt: event.CreatedAt})
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
	note := fmt.Sprintf("reason=%s recommended_action=%s age_seconds=%d to_colleague_id=%s role_code=%s", push.Reason, firstNonEmpty(push.RecommendedAction, recommendedActionFor(push.Reason, push.Status)), push.AgeSeconds, strings.TrimSpace(push.ToColleagueID), strings.TrimSpace(push.ToRoleCode))
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
	status.Config = MonitorConfigStatus{TickIntervalSeconds: int64(cfg.TickInterval.Seconds()), StalledAfterSeconds: int64(cfg.StalledAfter.Seconds()), PushCooldownSeconds: int64(cfg.PushCooldown.Seconds()), WorkersPerShard: cfg.WorkersPerShard, MaxWatchers: cfg.MaxWatchers}
	m.mu.RLock()
	defer m.mu.RUnlock()
	status.Tenants = make([]TenantMonitorStatus, 0, len(m.statuses))
	for _, tenantStatus := range m.statuses {
		status.Tenants = append(status.Tenants, tenantStatus)
	}
	sort.SliceStable(status.Tenants, func(i, j int) bool { return status.Tenants[i].TenantID < status.Tenants[j].TenantID })
	return status
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
		result, err := m.svc.CheckTenant(tenantID, now)
		if err != nil {
			log.Printf("[goalwatch] check tenant %s failed: %v", tenantID, err)
			status.Error = err.Error()
			status.FinishedAt = time.Now().UTC()
			m.recordStatus(status)
			return
		}
		status.Checked = result.Checked
		status.Pushed = result.Pushed
		status.FinishedAt = time.Now().UTC()
		status.ShardStatuses = append(status.ShardStatuses, MonitorShardStatus{ShardIndex: 0, ShardCount: 1, Checked: result.Checked, Pushed: result.Pushed})
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
			result, err := m.svc.CheckTenantShard(tenantID, now, shard, shardCount)
			if err != nil {
				log.Printf("[goalwatch] check tenant %s shard=%d/%d failed: %v", tenantID, shard, shardCount, err)
				results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount, Error: err.Error()}
				return
			}
			results <- MonitorShardStatus{ShardIndex: shard, ShardCount: shardCount, Checked: result.Checked, Pushed: result.Pushed}
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
