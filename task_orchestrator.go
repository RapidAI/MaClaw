package main

import (
	"fmt"
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
		subTasks[i].Status = "pending"
	}
	plan := &TaskPlan{
		ID: planID, Description: description, SubTasks: subTasks,
		Status: "planning", CreatedAt: time.Now(),
	}
	o.mu.Lock()
	o.plans[planID] = plan
	o.mu.Unlock()
	return plan, nil
}

// Execute runs a plan. Subtasks with no dependencies run in parallel.
func (o *TaskOrchestrator2) Execute(planID string) error {
	o.mu.Lock()
	plan, ok := o.plans[planID]
	if !ok {
		o.mu.Unlock()
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = "running"
	o.mu.Unlock()

	completed := make(map[string]bool)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for {
		var ready []int
		o.mu.RLock()
		for i, st := range plan.SubTasks {
			if st.Status != "pending" {
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
			o.mu.RLock()
			for _, st := range plan.SubTasks {
				if st.Status == "pending" || st.Status == "running" {
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
			plan.SubTasks[i].Status = "running"
			o.mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := o.executeSubTask(&plan.SubTasks[i])
				o.mu.Lock()
				if err != nil {
					plan.SubTasks[i].Status = "failed"
					plan.SubTasks[i].Result = err.Error()
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
				} else {
					plan.SubTasks[i].Status = "completed"
					plan.SubTasks[i].Result = result
					completed[plan.SubTasks[i].ID] = true
				}
				o.mu.Unlock()
			}()
		}
		wg.Wait()
		if firstErr != nil {
			o.mu.Lock()
			plan.Status = "failed"
			o.mu.Unlock()
			return firstErr
		}
	}
	o.mu.Lock()
	plan.Status = "completed"
	o.mu.Unlock()
	return nil
}

func (o *TaskOrchestrator2) executeSubTask(st *PlanSubTask) (string, error) {
	if o.manager == nil {
		return "", fmt.Errorf("session manager not available")
	}
	return fmt.Sprintf("子任务 %s (%s) 已提交", st.ID, st.Description), nil
}

// GetStatus returns the current state of a plan.
func (o *TaskOrchestrator2) GetStatus(planID string) (*TaskPlan, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	plan, ok := o.plans[planID]
	if !ok {
		return nil, fmt.Errorf("计划 %s 不存在", planID)
	}
	return plan, nil
}

// Cancel cancels a running plan.
func (o *TaskOrchestrator2) Cancel(planID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	plan, ok := o.plans[planID]
	if !ok {
		return fmt.Errorf("计划 %s 不存在", planID)
	}
	plan.Status = "cancelled"
	return nil
}
