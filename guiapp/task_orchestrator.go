package guiapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	_ "modernc.org/sqlite"
)

// TaskPlan represents a multi-session execution plan.
type TaskPlan struct {
	ID            string                 `json:"id"`
	Description   string                 `json:"description"`
	SubTasks      []PlanSubTask          `json:"sub_tasks"`
	Status        orchestratorTaskStatus `json:"status"`
	RuntimeStatus agentruntime.JobStatus `json:"runtime_status"`
	CreatedAt     time.Time              `json:"created_at"`
}

// PlanSubTask represents a single unit of work within a TaskPlan.
type PlanSubTask struct {
	ID            string                 `json:"id"`
	Description   string                 `json:"description"`
	Tool          string                 `json:"tool"`
	SessionID     string                 `json:"session_id"`
	DependsOn     []string               `json:"depends_on"`
	Status        orchestratorTaskStatus `json:"status"`
	RuntimeStatus agentruntime.JobStatus `json:"runtime_status"`
	Result        string                 `json:"result"`
}

// TaskOrchestrator2 manages multi-session task execution plans.
type TaskOrchestrator2 struct {
	manager        *RemoteSessionManager
	toolSelector   *ToolSelector
	contextBridge  *ContextBridge
	plans          map[string]*TaskPlan
	runtimeJobs    agentruntime.JobRepository
	runtimeEffects agentruntime.JobEffectRepository
	mu             sync.RWMutex
	persistPath    string // optional: path to persist plans on disk
	db             *sql.DB
	closed         bool   // Close has run; never fall back to rewriting the legacy JSON file
}

// NewTaskOrchestrator2 creates a new task orchestrator.
func NewTaskOrchestrator2(manager *RemoteSessionManager, selector *ToolSelector, bridge *ContextBridge) *TaskOrchestrator2 {
	return &TaskOrchestrator2{
		manager:        manager,
		runtimeJobs:    agentruntime.NewMemoryJobRepository(),
		runtimeEffects: agentruntime.NewMemoryJobEffectRepository(),
		toolSelector:   selector,
		contextBridge:  bridge,
		plans:          make(map[string]*TaskPlan),
	}
}

// NewTaskOrchestrator2WithPersist creates a task orchestrator that persists
// plans to disk. It loads any previously saved plans on creation. Rows live in
// task_plans.db next to the legacy task_plans.json, which is imported once and
// kept as a .migrated.bak backup; a later empty database stays empty.
func NewTaskOrchestrator2WithPersist(manager *RemoteSessionManager, selector *ToolSelector, bridge *ContextBridge, persistPath string) *TaskOrchestrator2 {
	o := NewTaskOrchestrator2(manager, selector, bridge)
	o.persistPath = persistPath
	o.initPlanStore()
	_ = o.loadPlans()
	return o
}

// taskOrchestratorDBPath derives the SQLite store path from the legacy JSON
// path so both GUI wiring and tests keep passing the same location.
func taskOrchestratorDBPath(persistPath string) string {
	return filepath.Join(filepath.Dir(persistPath), "task_plans.db")
}

func (o *TaskOrchestrator2) initPlanStore() {
	if o == nil || o.persistPath == "" {
		return
	}
	dbPath := taskOrchestratorDBPath(o.persistPath)
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Printf("[task-orchestrator] create dir failed path=%q err=%v", dbPath, err)
		return
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("[task-orchestrator] open failed path=%q err=%v", dbPath, err)
		return
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := configureTaskOrchestratorDB(db); err != nil {
		log.Printf("[task-orchestrator] schema failed path=%q err=%v", dbPath, err)
		_ = db.Close()
		return
	}
	o.db = db
	o.importLegacyPlans()
}

func configureTaskOrchestratorDB(db *sql.DB) error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			return err
		}
	}
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS task_plans (
    id TEXT PRIMARY KEY,
    payload_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS task_plans_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`)
	return err
}

// importLegacyPlans copies task_plans.json once. A later empty database must
// not read that file again, or deleting every plan would bring the old plans
// back on the next open. The JSON file is kept as a .migrated.bak backup.
func (o *TaskOrchestrator2) importLegacyPlans() {
	if o == nil || o.db == nil {
		return
	}
	var marked int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM task_plans_meta WHERE key = 'legacy_json_imported'`).Scan(&marked); err != nil {
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	if marked > 0 {
		return
	}
	var rows int
	if err := o.db.QueryRow(`SELECT COUNT(*) FROM task_plans`).Scan(&rows); err != nil {
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	if rows > 0 {
		// An older build wrote rows before the marker existed.
		if _, err := o.db.Exec(`INSERT INTO task_plans_meta(key, value) VALUES('legacy_json_imported', '1') ON CONFLICT(key) DO NOTHING`); err != nil {
			log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		}
		return
	}
	data, err := os.ReadFile(o.persistPath)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	var plans map[string]*TaskPlan
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &plans); err != nil {
			log.Printf("[task-orchestrator] ignored invalid plans file path=%q err=%v", o.persistPath, err)
		}
	}
	tx, err := o.db.Begin()
	if err != nil {
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	now := time.Now().UnixNano()
	for id, plan := range plans {
		payload, err := json.Marshal(plan)
		if err != nil {
			_ = tx.Rollback()
			log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
			return
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO task_plans(id, payload_json, updated_at) VALUES(?,?,?)`, id, string(payload), now); err != nil {
			_ = tx.Rollback()
			log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
			return
		}
	}
	if _, err := tx.Exec(`INSERT INTO task_plans_meta(key, value) VALUES('legacy_json_imported', '1') ON CONFLICT(key) DO NOTHING`); err != nil {
		_ = tx.Rollback()
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("[task-orchestrator] import failed path=%q err=%v", o.persistPath, err)
		return
	}
	if err == nil && len(data) > 0 {
		if renameErr := os.Rename(o.persistPath, o.persistPath+".migrated.bak"); renameErr != nil {
			log.Printf("[task-orchestrator] backup rename failed path=%q err=%v", o.persistPath, renameErr)
		}
	}
}

func (o *TaskOrchestrator2) UseDurableRuntimeStores(dataDir string) error {
	if o == nil {
		return fmt.Errorf("task orchestrator is unavailable")
	}
	jobs, effects, err := agentservice.OpenGUIRuntimeJobStores(dataDir)
	if err != nil {
		return err
	}
	o.runtimeJobs = jobs
	o.runtimeEffects = effects
	return o.ReconcileRuntimeJobs(context.Background())
}

func (o *TaskOrchestrator2) ReconcileRuntimeJobs(ctx context.Context) error {
	if o == nil {
		return nil
	}
	return agentruntime.RecoverAndReconcileOpenJobs(ctx, o.runtimeJobs, o.runtimeEffects, "orchestrator.plan", o, nil)
}

// CreatePlan creates a new execution plan.
func (o *TaskOrchestrator2) CreatePlan(description string, subTasks []PlanSubTask) (*TaskPlan, error) {
	if len(subTasks) == 0 {
		return nil, fmt.Errorf("至少需要一个子任务")
	}
	planID := fmt.Sprintf("plan_%d", time.Now().UnixNano())
	for i := range subTasks {
		if subTasks[i].ID == "" {
			subTasks[i].ID = fmt.Sprintf("task_%d", i+1)
		}
		subTasks[i].Status = orchestratorTaskStatusPending
	}
	plan := &TaskPlan{
		ID: planID, Description: description, SubTasks: subTasks,
		Status: orchestratorTaskStatusPlanning, CreatedAt: time.Now(),
	}
	o.mu.Lock()
	o.plans[planID] = plan
	o.mu.Unlock()
	_ = o.savePlans()
	return plan, nil
}

// Execute runs a plan. Ready subtasks are executed one by one to avoid
// simultaneous LLM requests and provider rate-limit bursts.
// Already-completed subtasks are skipped (supports Resume).
func (o *TaskOrchestrator2) Execute(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = orchestratorTaskStatusRunning
	o.mu.Unlock()

	completed := make(map[string]bool)
	// Pre-populate with already-completed subtasks (for Resume).
	o.mu.RLock()
	for _, st := range plan.SubTasks {
		if st.Status.IsCompleted() {
			completed[st.ID] = true
		}
	}
	o.mu.RUnlock()

	var firstErr error

	for {
		var ready []int
		o.mu.RLock()
		for i, st := range plan.SubTasks {
			if !st.Status.IsPending() {
				continue
			}
			allDeps := true
			for _, dep := range st.DependsOn {
				if !completed[dep] {
					allDeps = false
					break
				}
			}
			if allDeps {
				ready = append(ready, i)
			}
		}
		o.mu.RUnlock()

		if len(ready) == 0 {
			allDone := true
			blockedPending := make([]string, 0)
			running := false
			o.mu.RLock()
			for _, st := range plan.SubTasks {
				if st.Status.IsRunning() {
					running = true
				}
				if st.Status.IsPending() {
					blockedPending = append(blockedPending, st.ID)
				}
				if st.Status.IsActive() {
					allDone = false
				}
			}
			o.mu.RUnlock()
			if allDone {
				break
			}
			if !running && len(blockedPending) > 0 {
				firstErr = fmt.Errorf("plan %s has blocked subtasks with unsatisfied dependencies: %s", planID, strings.Join(blockedPending, ", "))
				o.mu.Lock()
				plan.Status = orchestratorTaskStatusFailed
				for i := range plan.SubTasks {
					if plan.SubTasks[i].Status.IsPending() {
						plan.SubTasks[i].Status = orchestratorTaskStatusFailed
						plan.SubTasks[i].Result = firstErr.Error()
					}
				}
				o.mu.Unlock()
				_ = o.savePlans()
				return firstErr
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, i := range ready {
			o.mu.Lock()
			plan.SubTasks[i].Status = orchestratorTaskStatusRunning
			o.mu.Unlock()
			result, err := o.executeSubTask(&plan.SubTasks[i])
			o.mu.Lock()
			if err != nil {
				plan.SubTasks[i].Status = orchestratorTaskStatusFailed
				plan.SubTasks[i].Result = err.Error()
				if firstErr == nil {
					firstErr = err
				}
			} else {
				plan.SubTasks[i].Status = orchestratorTaskStatusCompleted
				plan.SubTasks[i].Result = result
				completed[plan.SubTasks[i].ID] = true
			}
			o.mu.Unlock()
			_ = o.savePlans()
			if firstErr != nil {
				break
			}
		}
		if firstErr != nil {
			o.mu.Lock()
			plan.Status = orchestratorTaskStatusFailed
			o.mu.Unlock()
			_ = o.savePlans()
			return firstErr
		}
	}
	o.mu.Lock()
	plan.Status = orchestratorTaskStatusCompleted
	o.mu.Unlock()
	_ = o.savePlans()
	return nil
}

func (o *TaskOrchestrator2) executeSubTask(st *PlanSubTask) (string, error) {
	if o.manager == nil {
		return "", fmt.Errorf("session manager not available")
	}
	return fmt.Sprintf("子任务 %s (%s) 已提交", st.ID, st.Description), nil
}

// GetStatus returns a snapshot of the current state of a plan.
func (o *TaskOrchestrator2) GetStatus(planID string) (*TaskPlan, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	plan, ok := o.plans[planID]
	if !ok {
		return nil, fmt.Errorf("计划 %s 不存在", planID)
	}
	cp := *plan
	cp.RuntimeStatus = cp.Status.RuntimeStatusValue()
	cp.SubTasks = append([]PlanSubTask(nil), plan.SubTasks...)
	for i := range cp.SubTasks {
		cp.SubTasks[i].RuntimeStatus = cp.SubTasks[i].Status.RuntimeStatusValue()
	}
	return &cp, nil
}

// Cancel cancels a running plan.
func (o *TaskOrchestrator2) Cancel(planID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	plan, ok := o.plans[planID]
	if !ok {
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = orchestratorTaskStatusCancelled
	_ = o.savePlansLocked()
	return nil
}

// Resume restarts execution of a plan from its last checkpoint. Only
// subtasks that are still "pending" or were "running" (interrupted) will
// be executed. Already "completed" subtasks are skipped.
func (o *TaskOrchestrator2) Resume(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	for i := range plan.SubTasks {
		if plan.SubTasks[i].Status.IsRunning() {
			plan.SubTasks[i].Status = orchestratorTaskStatusPending
		}
	}
	plan.Status = orchestratorTaskStatusRunning
	o.mu.Unlock()

	return o.Execute(planID)
}

// ListResumable returns plans that can be resumed (status == "failed" or
// "running" with pending subtasks).
func (o *TaskOrchestrator2) ListResumable() []*TaskPlan {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var result []*TaskPlan
	for _, plan := range o.plans {
		if plan.Status.IsResumable() {
			hasPending := false
			for _, st := range plan.SubTasks {
				if st.Status.IsActive() {
					hasPending = true
					break
				}
			}
			if hasPending {
				cp := *plan
				cp.SubTasks = append([]PlanSubTask(nil), plan.SubTasks...)
				result = append(result, &cp)
			}
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// Plan persistence
// ---------------------------------------------------------------------------

// Close releases the plan database handle. It is safe to call on a nil
// orchestrator or one that never opened a database. After Close the
// orchestrator stops persisting: falling back to rewriting the legacy JSON
// file here could resurrect a migrated-away plans file.
func (o *TaskOrchestrator2) Close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.db != nil {
		_ = o.db.Close()
		o.db = nil
	}
	o.closed = true
}

// savePlans persists all plans to disk (acquires read lock).
func (o *TaskOrchestrator2) savePlans() error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.savePlansLocked()
}

// savePlansLocked persists all plans to disk. Caller must hold at least a
// read lock on o.mu.
func (o *TaskOrchestrator2) savePlansLocked() error {
	o.publishAllRuntimeJobsLocked()
	if o.persistPath == "" || o.closed {
		return nil
	}
	if o.db != nil {
		return o.savePlansToDBLocked()
	}
	dir := filepath.Dir(o.persistPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("task_orchestrator: create dir: %w", err)
	}
	data, err := json.MarshalIndent(o.plans, "", "  ")
	if err != nil {
		return fmt.Errorf("task_orchestrator: marshal: %w", err)
	}
	return os.WriteFile(o.persistPath, data, 0o644)
}

// savePlansToDBLocked syncs the full plan snapshot in one transaction, keeping
// the historical "store = complete snapshot" semantics while writing only the
// rows that changed.
func (o *TaskOrchestrator2) savePlansToDBLocked() error {
	tx, err := o.db.Begin()
	if err != nil {
		return fmt.Errorf("task_orchestrator: begin: %w", err)
	}
	now := time.Now().UnixNano()
	kept := make(map[string]struct{}, len(o.plans))
	for id, plan := range o.plans {
		payload, err := json.Marshal(plan)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("task_orchestrator: marshal: %w", err)
		}
		kept[id] = struct{}{}
		if _, err := tx.Exec(`INSERT INTO task_plans(id, payload_json, updated_at) VALUES(?,?,?)
			ON CONFLICT(id) DO UPDATE SET payload_json = excluded.payload_json, updated_at = excluded.updated_at`, id, string(payload), now); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("task_orchestrator: upsert: %w", err)
		}
	}
	rows, err := tx.Query(`SELECT id FROM task_plans`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("task_orchestrator: list: %w", err)
	}
	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			if _, ok := kept[id]; !ok {
				stale = append(stale, id)
			}
		}
	}
	_ = rows.Close()
	for _, id := range stale {
		if _, err := tx.Exec(`DELETE FROM task_plans WHERE id = ?`, id); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("task_orchestrator: delete: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("task_orchestrator: commit: %w", err)
	}
	return nil
}

func (o *TaskOrchestrator2) publishAllRuntimeJobsLocked() {
	if o == nil {
		return
	}
	for _, plan := range o.plans {
		o.publishRuntimeJobLocked(plan)
	}
}

func (o *TaskOrchestrator2) publishRuntimeJobLocked(plan *TaskPlan) {
	if o == nil || o.runtimeJobs == nil || plan == nil {
		return
	}
	now := plan.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	runtimeStatus := plan.Status.RuntimeStatusValue()
	outcome := agentruntime.JobWorkerOutcome{ErrorCode: "orchestrator_plan_failed", ErrorText: "orchestration plan failed"}
	if runtimeStatus == agentruntime.JobStatusSucceeded {
		if payload, err := json.Marshal(map[string]string{"plan": plan.ID, "status": string(runtimeStatus)}); err == nil {
			outcome.Result = payload
		}
	}
	job := agentruntime.StampHostJobStatus(agentruntime.Job{
		ID:             plan.ID,
		Kind:           "orchestrator.plan",
		RecoveryPolicy: agentruntime.JobRecoveryPolicyReconcile,
		CreatedAt:      plan.CreatedAt,
	}, runtimeStatus, now, outcome)
	_ = agentruntime.UpsertJob(context.Background(), o.runtimeJobs, job)
	o.syncRuntimeEffectLocked(plan)
}

func (o *TaskOrchestrator2) syncRuntimeEffectLocked(plan *TaskPlan) {
	if o == nil || o.runtimeEffects == nil || plan == nil || strings.TrimSpace(plan.ID) == "" {
		return
	}
	ctx := context.Background()
	now := time.Now().UTC()
	prepared, _, err := o.runtimeEffects.Prepare(ctx, agentruntime.JobEffect{
		JobID: plan.ID, Kind: "orchestrator.plan", JobKind: "orchestrator.plan",
		TenantID: "gui", UserID: "local", ResourceID: plan.ID,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return
	}
	runtimeStatus := plan.Status.RuntimeStatusValue()
	next := prepared
	switch runtimeStatus {
	case agentruntime.JobStatusSucceeded:
		next.State = agentruntime.JobEffectCommitted
		next.ReceiptDigest = agentruntime.JobEffectReceiptDigest(plan.ID + ":" + string(runtimeStatus))
		if payload, err := json.Marshal(map[string]string{"plan": plan.ID, "status": string(runtimeStatus)}); err == nil {
			next.Payload = payload
		}
	case agentruntime.JobStatusFailed:
		next.State = agentruntime.JobEffectFailed
		next.ReasonCode = "orchestrator_plan_failed"
	default:
		return
	}
	_, _ = o.runtimeEffects.Update(ctx, prepared.Version, next)
}

func (o *TaskOrchestrator2) ReconcileJob(ctx context.Context, job agentruntime.Job, effects []agentruntime.JobEffect) (agentruntime.JobReconcileResult, error) {
	if o == nil || strings.TrimSpace(job.Kind) != "orchestrator.plan" {
		return agentruntime.JobReconcileResult{}, nil
	}
	if result := agentruntime.ReconcileFromProtectedEffects(effects, "orchestrator.plan", "orchestration plan failed", nil); result.Resolved {
		return result, nil
	}
	plan, err := o.GetStatus(job.ID)
	if err != nil || plan == nil {
		return agentruntime.JobReconcileResult{}, nil
	}
	switch plan.RuntimeStatus {
	case agentruntime.JobStatusSucceeded:
		payload, _ := json.Marshal(map[string]string{"plan": plan.ID, "status": "succeeded"})
		return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusSucceeded, Result: payload}, nil
	case agentruntime.JobStatusFailed:
		return agentruntime.JobReconcileResult{Resolved: true, Status: agentruntime.JobStatusFailed, ErrorCode: "orchestrator_plan_failed", Error: "orchestration plan failed"}, nil
	default:
		return agentruntime.JobReconcileResult{}, nil
	}
}

var _ agentruntime.JobReconciler = (*TaskOrchestrator2)(nil)

// loadPlans reads plans from disk. Called once at creation, before any
// concurrent use.
func (o *TaskOrchestrator2) loadPlans() error {
	if o.persistPath == "" {
		return nil
	}
	if o.db != nil {
		rows, err := o.db.Query(`SELECT payload_json FROM task_plans`)
		if err != nil {
			return fmt.Errorf("task_orchestrator: query: %w", err)
		}
		defer rows.Close()
		plans := make(map[string]*TaskPlan)
		for rows.Next() {
			var payload string
			if err := rows.Scan(&payload); err != nil {
				return fmt.Errorf("task_orchestrator: scan: %w", err)
			}
			var plan TaskPlan
			if err := json.Unmarshal([]byte(payload), &plan); err != nil {
				return fmt.Errorf("task_orchestrator: unmarshal: %w", err)
			}
			if plan.ID != "" {
				plans[plan.ID] = &plan
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("task_orchestrator: rows: %w", err)
		}
		o.plans = plans
		return nil
	}
	// SQLite unavailable (open/schema failed): fall back to the legacy JSON
	// file so a broken database never loses access to existing plans.
	data, err := os.ReadFile(o.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("task_orchestrator: read: %w", err)
	}
	if len(data) == 0 {
		return nil
	}
	var plans map[string]*TaskPlan
	if err := json.Unmarshal(data, &plans); err != nil {
		return fmt.Errorf("task_orchestrator: unmarshal: %w", err)
	}
	o.plans = plans
	return nil
}
