package main

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// runMaintenance executes the Maintenance mode pipeline:
// task_split → conflict_detect → development → merge → compile → test → document → report
func (o *SwarmOrchestrator) runMaintenance(run *SwarmRun, req SwarmRunRequest, maxAgents int) error {
	// Phase 1: Task Split (parse task list)
	o.setPhase(run, PhaseTaskSplit)
	state, err := o.worktreeMgr.PrepareProject(run.ProjectPath)
	if err != nil {
		return fmt.Errorf("prepare project: %w", err)
	}
	run.ProjectState = state

	tasks, err := o.taskSplitter.ParseTaskList(*req.TaskInput)
	if err != nil {
		return fmt.Errorf("parse task list: %w", err)
	}
	run.Tasks = tasks

	// Phase 2: Conflict Detection
	o.setPhase(run, PhaseConflictDetect)
	groups, err := o.conflictDet.DetectConflicts(tasks)
	if err != nil {
		return fmt.Errorf("detect conflicts: %w", err)
	}
	run.TaskGroups = groups

	// Phase 3: Development
	o.setPhase(run, PhaseDevelopment)
	if err := o.runDevelopmentPhaseGrouped(run, tasks, groups, req, maxAgents); err != nil {
		return fmt.Errorf("development phase: %w", err)
	}

	// Phase 4-5: Merge + Compile
	o.setPhase(run, PhaseMerge)
	if err := o.runMergePhase(run); err != nil {
		log.Printf("[SwarmOrchestrator] merge phase had issues: %v", err)
	}

	o.setPhase(run, PhaseCompile)

	// Phase 6: Test
	o.setPhase(run, PhaseTest)
	if err := o.runTestPhase(run, req); err != nil {
		log.Printf("[SwarmOrchestrator] test phase: %v", err)
	}

	// Phase 7: Document
	o.setPhase(run, PhaseDocument)
	_, _ = o.runSingleAgent(run, RoleDocumenter, 0, run.ProjectPath, PromptContext{
		ProjectName: run.ProjectPath,
		TechStack:   req.TechStack,
	})

	// Phase 8: Report
	o.setPhase(run, PhaseReport)

	// Cleanup
	_ = o.worktreeMgr.CleanupRun(run.ProjectPath, run.ID)
	_ = o.worktreeMgr.RestoreProject(run.ProjectPath, run.ProjectState)

	return nil
}

// runDevelopmentPhaseGrouped runs tasks respecting TaskGroup constraints:
// tasks within the same group run serially, different groups run in parallel.
func (o *SwarmOrchestrator) runDevelopmentPhaseGrouped(run *SwarmRun, tasks []SubTask, groups []TaskGroup, req SwarmRunRequest, maxAgents int) error {
	taskMap := make(map[int]SubTask)
	for _, t := range tasks {
		taskMap[t.Index] = t
	}

	sem := make(chan struct{}, maxAgents)
	var wg sync.WaitGroup

	for _, group := range groups {
		wg.Add(1)
		go func(g TaskGroup) {
			defer wg.Done()
			// Tasks within a group run serially
			for _, taskIdx := range g.TaskIndices {
				sem <- struct{}{}
				task := taskMap[taskIdx]
				o.runSingleDevTask(run, task, req)
				<-sem
			}
		}(group)
	}

	wg.Wait()
	return nil
}

// runSingleDevTask creates a worktree and runs a single developer task.
func (o *SwarmOrchestrator) runSingleDevTask(run *SwarmRun, task SubTask, req SwarmRunRequest) {
	branchName := fmt.Sprintf("swarm/%s/developer-%d", run.ID, task.Index)
	wt, err := o.worktreeMgr.CreateWorktree(run.ProjectPath, run.ID, branchName)
	if err != nil {
		log.Printf("[SwarmOrchestrator] create worktree for task %d: %v", task.Index, err)
		return
	}

	agentID := fmt.Sprintf("%s-dev-%d", run.ID, task.Index)
	agent := SwarmAgent{
		ID:           agentID,
		Role:         RoleDeveloper,
		TaskIndex:    task.Index,
		WorktreePath: wt.Path,
		BranchName:   branchName,
		Status:       "pending",
	}

	o.mu.Lock()
	run.Agents = append(run.Agents, agent)
	o.mu.Unlock()

	output, err := o.runSingleAgent(run, RoleDeveloper, task.Index, wt.Path, PromptContext{
		ProjectName: run.ProjectPath,
		TechStack:   req.TechStack,
		TaskDesc:    task.Description,
	})

	o.mu.Lock()
	for i := range run.Agents {
		if run.Agents[i].ID == agentID {
			if err != nil {
				run.Agents[i].Status = "failed"
				run.Agents[i].Error = err.Error()
			} else {
				run.Agents[i].Status = "completed"
				run.Agents[i].Output = output
			}
			now := time.Now()
			run.Agents[i].CompletedAt = &now
			break
		}
	}
	o.mu.Unlock()
}

// runSingleAgent creates a RemoteSession for a given role, sends the task,
// and waits for completion. Returns the agent's output.
func (o *SwarmOrchestrator) runSingleAgent(run *SwarmRun, role AgentRole, taskIndex int, projectPath string, ctx PromptContext) (string, error) {
	prompt, err := RenderPrompt(role, ctx)
	if err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}

	if o.manager == nil {
		// No session manager available (testing mode) — just return the prompt
		log.Printf("[SwarmOrchestrator] no session manager, skipping agent %s-%d", role, taskIndex)
		return prompt, nil
	}

	// Create a RemoteSession via the existing manager
	spec := LaunchSpec{
		Tool:        run.Tool,
		ProjectPath: projectPath,
		Env: map[string]string{
			"SWARM_SYSTEM_PROMPT": prompt,
			"SWARM_ROLE":         string(role),
		},
	}
	if spec.Tool == "" {
		spec.Tool = "claude" // fallback default
	}

	session, err := o.manager.Create(spec)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	o.addTimelineEvent(run, "agent_created",
		fmt.Sprintf("Created %s agent for task %d", role, taskIndex), session.ID)

	// Wait for session to complete (simplified — real implementation would
	// monitor session status changes)
	// TODO: implement proper session monitoring with timeout
	_ = session
	return "", nil
}

// runMergePhase collects all developer branches and merges them.
func (o *SwarmOrchestrator) runMergePhase(run *SwarmRun) error {
	o.mu.RLock()
	var branches []BranchInfo
	for i, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			branches = append(branches, BranchInfo{
				Name:      agent.BranchName,
				AgentID:   agent.ID,
				TaskIndex: agent.TaskIndex,
				Order:     i,
			})
		}
	}
	o.mu.RUnlock()

	if len(branches) == 0 {
		return nil
	}

	result, err := o.mergeCtrl.MergeAll(run.ProjectPath, branches, "")
	if err != nil {
		return err
	}

	if !result.Success {
		o.addTimelineEvent(run, "merge_partial",
			fmt.Sprintf("Merged %d/%d branches", len(result.MergedBranches), len(branches)), "")
		_ = o.notifier.NotifyFailure(run, "merge", fmt.Sprintf("Failed branches: %v", result.FailedBranches))
	}

	return nil
}

// runTestPhase creates a tester agent and handles feedback loop.
func (o *SwarmOrchestrator) runTestPhase(run *SwarmRun, req SwarmRunRequest) error {
	_, err := o.runSingleAgent(run, RoleTester, 0, run.ProjectPath, PromptContext{
		ProjectName:  run.ProjectPath,
		TechStack:    req.TechStack,
		Requirements: req.Requirements,
		TestCommand:  "go test ./...",
	})
	if err != nil {
		return err
	}

	// TODO: parse test output, classify failures, trigger feedback loop
	return nil
}
