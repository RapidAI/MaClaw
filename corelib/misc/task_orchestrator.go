package misc

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
	Status      string        `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
}

// PlanSubTask represents a single unit of work within a TaskPlan.
type PlanSubTask struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Tool        string   `json:"tool"`
	SessionID   string   `json:"session_id"`
	DependsOn   []string `json:"depends_on"`
	Status      string   `json:"status"`
	Result      string   `json:"result"`
}

// SubTaskExecutor executes a single subtask and returns the result.
type SubTaskExecutor func(st *PlanSubTask) (string, error)

// TaskOrchestrator manages multi-session task execution plans.
type TaskOrchestrator struct {
	executor    SubTaskExecutor
	plans       map[string]*TaskPlan
	mu          sync.RWMutex
	persistPath string
}

// NewTaskOrchestrator creates a new task orchestrator.
func NewTaskOrchestrator(executor SubTaskExecutor) *TaskOrchestrator {
	return &TaskOrchestrator{
		executor: executor,
		plans:    make(map[string]*TaskPlan),
	}
}

// NewTaskOrchestratorWithPersist creates a task orchestrator that persists plans to disk.
func NewTaskOrchestratorWithPersist(executor SubTaskExecutor, persistPath string) *TaskOrchestrator {
	o := &TaskOrchestrator{
		executor:    executor,
		plans:       make(map[string]*TaskPlan),
		persistPath: persistPath,
	}
	_ = o.loadPlans()
	return o
}

// CreatePlan creates a new execution plan.
func (o *TaskOrchestrator) CreatePlan(description string, subTasks []PlanSubTask) (*TaskPlan, error) {
	if len(subTasks) == 0 {
		return nil, fmt.Errorf("至少需要一个子任务")
	}
	planID := fmt.Sprintf("plan_%d", time.Now().UnixNano())
	for i := range subTasks {
		if subTasks[i].ID == "" {
			subTasks[i].ID = fmt.Sprintf("task_%d", i+1)
		}
		subTasks[i].Status = taskOrchestratorStatusPending.String()
	}
	plan := &TaskPlan{
		ID: planID, Description: description, SubTasks: subTasks,
		Status: taskOrchestratorStatusPlanning.String(), CreatedAt: time.Now(),
	}
	o.mu.Lock()
	o.plans[planID] = plan
	o.mu.Unlock()
	_ = o.savePlans()
	return plan, nil
}

// Execute runs a plan. Subtasks with no dependencies run in parallel.
func (o *TaskOrchestrator) Execute(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = taskOrchestratorStatusRunning.String()
	o.mu.Unlock()

	completed := make(map[string]bool)
	var completedMu sync.Mutex
	o.mu.RLock()
	for _, st := range plan.SubTasks {
		if normalizeTaskOrchestratorStatus(st.Status).IsCompleted() {
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
			if !normalizeTaskOrchestratorStatus(st.Status).IsPending() {
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
				if normalizeTaskOrchestratorStatus(st.Status).IsActive() {
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
			plan.SubTasks[i].Status = taskOrchestratorStatusRunning.String()
			o.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				var result string
				var err error
				if o.executor != nil {
					result, err = o.executor(&plan.SubTasks[i])
				} else {
					result = fmt.Sprintf("子任务 %s (%s) 已提交", plan.SubTasks[i].ID, plan.SubTasks[i].Description)
				}
				o.mu.Lock()
				if err != nil {
					plan.SubTasks[i].Status = taskOrchestratorStatusFailed.String()
					plan.SubTasks[i].Result = err.Error()
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
				} else {
					plan.SubTasks[i].Status = taskOrchestratorStatusCompleted.String()
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
			plan.Status = taskOrchestratorStatusFailed.String()
			o.mu.Unlock()
			_ = o.savePlans()
			return firstErr
		}
	}
	o.mu.Lock()
	plan.Status = taskOrchestratorStatusCompleted.String()
	o.mu.Unlock()
	_ = o.savePlans()
	return nil
}

// GetStatus returns a snapshot of a plan.
func (o *TaskOrchestrator) GetStatus(planID string) (*TaskPlan, error) {
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
func (o *TaskOrchestrator) Cancel(planID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	plan, ok := o.plans[planID]
	if !ok {
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = taskOrchestratorStatusCancelled.String()
	return o.savePlansLocked()
}

// Resume restarts execution of a plan from its last checkpoint.
func (o *TaskOrchestrator) Resume(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	for i := range plan.SubTasks {
		if normalizeTaskOrchestratorStatus(plan.SubTasks[i].Status).IsRunning() {
			plan.SubTasks[i].Status = taskOrchestratorStatusPending.String()
		}
	}
	plan.Status = taskOrchestratorStatusRunning.String()
	o.mu.Unlock()
	return o.Execute(planID)
}

// ListResumable returns plans that can be resumed.
func (o *TaskOrchestrator) ListResumable() []*TaskPlan {
	o.mu.RLock()
	defer o.mu.RUnlock()
	var result []*TaskPlan
	for _, plan := range o.plans {
		if normalizeTaskOrchestratorStatus(plan.Status).IsResumablePlan() {
			hasPending := false
			for _, st := range plan.SubTasks {
				if normalizeTaskOrchestratorStatus(st.Status).IsActive() {
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

func (o *TaskOrchestrator) savePlans() error {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.savePlansLocked()
}

func (o *TaskOrchestrator) savePlansLocked() error {
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

func (o *TaskOrchestrator) loadPlans() error {
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
