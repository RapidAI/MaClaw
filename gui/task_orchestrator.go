package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TaskPlan represents a multi-session execution plan.
type TaskPlan struct {
	ID          string        `json:"id"`
	Description string        `json:"description"`
	SubTasks    []PlanSubTask `json:"sub_tasks"`
	Status      string        `json:"status"` // "planning", "running", "completed", "failed", "cancelled"
	CreatedAt   time.Time     `json:"created_at"`
}

// PlanSubTask represents a single unit of work within a TaskPlan.
type PlanSubTask struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Tool        string   `json:"tool"`
	SessionID   string   `json:"session_id"`
	DependsOn   []string `json:"depends_on"`
	Status      string   `json:"status"` // "pending", "running", "completed", "failed"
	Result      string   `json:"result"`
}

// TaskOrchestrator2 manages multi-session task execution plans.
type TaskOrchestrator2 struct {
	manager       *RemoteSessionManager
	toolSelector  *ToolSelector
	contextBridge *ContextBridge
	plans         map[string]*TaskPlan
	mu            sync.RWMutex
	persistPath   string // optional: path to persist plans on disk
}

// NewTaskOrchestrator2 creates a new task orchestrator.
func NewTaskOrchestrator2(manager *RemoteSessionManager, selector *ToolSelector, bridge *ContextBridge) *TaskOrchestrator2 {
	return &TaskOrchestrator2{
		manager:       manager,
		toolSelector:  selector,
		contextBridge: bridge,
		plans:         make(map[string]*TaskPlan),
	}
}

// NewTaskOrchestrator2WithPersist creates a task orchestrator that persists
// plans to disk. It loads any previously saved plans on creation.
func NewTaskOrchestrator2WithPersist(manager *RemoteSessionManager, selector *ToolSelector, bridge *ContextBridge, persistPath string) *TaskOrchestrator2 {
	o := &TaskOrchestrator2{
		manager:       manager,
		toolSelector:  selector,
		contextBridge: bridge,
		plans:         make(map[string]*TaskPlan),
		persistPath:   persistPath,
	}
	_ = o.loadPlans()
	return o
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
		subTasks[i].Status = string(orchestratorTaskStatusPending)
	}
	plan := &TaskPlan{
		ID: planID, Description: description, SubTasks: subTasks,
		Status: string(orchestratorTaskStatusPlanning), CreatedAt: time.Now(),
	}
	o.mu.Lock()
	o.plans[planID] = plan
	o.mu.Unlock()
	_ = o.savePlans()
	return plan, nil
}

// Execute runs a plan. Subtasks with no dependencies run in parallel.
// Already-completed subtasks are skipped (supports Resume).
func (o *TaskOrchestrator2) Execute(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = string(orchestratorTaskStatusRunning)
	o.mu.Unlock()

	completed := make(map[string]bool)
	var completedMu sync.Mutex // protects the completed map
	// Pre-populate with already-completed subtasks (for Resume).
	o.mu.RLock()
	for _, st := range plan.SubTasks {
		if normalizeOrchestratorTaskStatus(st.Status) == orchestratorTaskStatusCompleted {
			completed[st.ID] = true
		}
	}
	o.mu.RUnlock()

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for {
		var ready []int
		o.mu.RLock()
		completedMu.Lock()
		for i, st := range plan.SubTasks {
			if normalizeOrchestratorTaskStatus(st.Status) != orchestratorTaskStatusPending {
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
		completedMu.Unlock()
		o.mu.RUnlock()

		if len(ready) == 0 {
			allDone := true
			o.mu.RLock()
			for _, st := range plan.SubTasks {
				if orchestratorTaskStatusIsActive(st.Status) {
					allDone = false
					break
				}
			}
			o.mu.RUnlock()
			if allDone {
				break
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, idx := range ready {
			i := idx
			o.mu.Lock()
			plan.SubTasks[i].Status = string(orchestratorTaskStatusRunning)
			o.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := o.executeSubTask(&plan.SubTasks[i])
				o.mu.Lock()
				if err != nil {
					plan.SubTasks[i].Status = string(orchestratorTaskStatusFailed)
					plan.SubTasks[i].Result = err.Error()
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
				} else {
					plan.SubTasks[i].Status = string(orchestratorTaskStatusCompleted)
					plan.SubTasks[i].Result = result
					completedMu.Lock()
					completed[plan.SubTasks[i].ID] = true
					completedMu.Unlock()
				}
				o.mu.Unlock()
				_ = o.savePlans()
			}()
		}
		wg.Wait()
		if firstErr != nil {
			o.mu.Lock()
			plan.Status = string(orchestratorTaskStatusFailed)
			o.mu.Unlock()
			_ = o.savePlans()
			return firstErr
		}
	}
	o.mu.Lock()
	plan.Status = string(orchestratorTaskStatusCompleted)
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
	cp.SubTasks = append([]PlanSubTask(nil), plan.SubTasks...)
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
	plan.Status = string(orchestratorTaskStatusCancelled)
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
		if normalizeOrchestratorTaskStatus(plan.SubTasks[i].Status) == orchestratorTaskStatusRunning {
			plan.SubTasks[i].Status = string(orchestratorTaskStatusPending)
		}
	}
	plan.Status = string(orchestratorTaskStatusRunning)
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
		if orchestratorTaskStatusIsResumable(plan.Status) {
			hasPending := false
			for _, st := range plan.SubTasks {
				if orchestratorTaskStatusIsActive(st.Status) {
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

// savePlans persists all plans to disk (acquires read lock).
func (o *TaskOrchestrator2) savePlans() error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.savePlansLocked()
}

// savePlansLocked persists all plans to disk. Caller must hold at least a
// read lock on o.mu.
func (o *TaskOrchestrator2) savePlansLocked() error {
	if o.persistPath == "" {
		return nil
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

// loadPlans reads plans from disk. Called once at creation.
func (o *TaskOrchestrator2) loadPlans() error {
	if o.persistPath == "" {
		return nil
	}
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
