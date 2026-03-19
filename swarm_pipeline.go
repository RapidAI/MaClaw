package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// runMaintenance executes the Maintenance mode pipeline:
// requirements(lite) → task_split → conflict_detect → development → merge → compile → test → feedback → document → report
//
// The requirements phase is lighter than greenfield — it enriches parsed tasks
// with acceptance criteria rather than generating a full requirements document.
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

	// 为维护模式的任务补充验收标准
	tasks = o.enrichTasksWithCriteria(tasks)
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

	// Phase 6: Test + Feedback Loop
	o.setPhase(run, PhaseTest)
	if err := o.runMaintenanceTest(run, req); err != nil {
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

// runMaintenanceTest runs the test phase for maintenance mode with feedback
// loop integration — same quality control as greenfield mode.
func (o *SwarmOrchestrator) runMaintenanceTest(run *SwarmRun, req SwarmRunRequest) error {
	testCmd := inferTestCommand(req.TechStack)

	// Build task summary from completed agents.
	var taskSummary []string
	for _, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			taskSummary = append(taskSummary, fmt.Sprintf("Task %d: %s", agent.TaskIndex, agent.Output))
		}
	}

	branchName := fmt.Sprintf("swarm/%s/tester-0", run.ID)
	ctx := PromptContext{
		ProjectName: run.ProjectPath,
		TechStack:   req.TechStack,
		TestCommand: testCmd,
		FeatureList: strings.Join(taskSummary, "\n"),
	}

	// Use run.Tool if set, otherwise fall back to "claude".
	testerTool := run.Tool
	if testerTool == "" {
		testerTool = "claude"
	}

	agent, err := o.createAgent(run, RoleTester, 0, run.ProjectPath, branchName, testerTool, ctx)
	if err != nil {
		return fmt.Errorf("create tester agent: %w", err)
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent
		return fmt.Errorf("tester agent failed: %w", err)
	}

	run.Agents[agentIdx] = *agent
	_ = o.notifier.NotifyAgentComplete(run, agent)
	o.addTimelineEvent(run, "test_done", "Tester agent completed", agent.ID)

	// Parse and classify failures via FeedbackLoop (same as greenfield).
	failures := parseTestFailures(agent.Output)
	if len(failures) == 0 {
		o.addTimelineEvent(run, "test_pass", "All tests passed", "")
		return nil
	}

	o.addTimelineEvent(run, "test_failures",
		fmt.Sprintf("Found %d test failures, classifying...", len(failures)), "")
	_ = o.notifier.NotifyFailure(run, "test",
		fmt.Sprintf("%d test(s) failed", len(failures)))

	if o.feedbackLoop == nil {
		return nil
	}

	classified, err := o.feedbackLoop.ClassifyFailures(failures)
	if err != nil {
		log.Printf("[SwarmOrchestrator] classify failures: %v", err)
		return nil
	}

	// Handle classified failures — reuse the greenfield handler.
	return o.handleClassifiedFailures(run, req, classified)
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
// Uses smart tool selection and task verification via runDeveloperAgentWithRetry.
func (o *SwarmOrchestrator) runSingleDevTask(run *SwarmRun, task SubTask, req SwarmRunRequest) {
	var mu sync.Mutex
	err := o.runDeveloperAgentWithRetry(run, task, req.Tool, "", &mu)
	if err != nil {
		log.Printf("[SwarmOrchestrator] task %d failed: %v", task.Index, err)
	}
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

	// Wait for session to complete with a timeout.
	// Poll session status every 5 seconds until it reaches a terminal state.
	const pollInterval = 5 * time.Second
	const maxWait = 30 * time.Minute
	deadline := time.After(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			_ = o.manager.Kill(session.ID)
			return "", fmt.Errorf("agent %s-%d timed out after %v", role, taskIndex, maxWait)
		case <-ticker.C:
			s, ok := o.manager.Get(session.ID)
			if !ok {
				return "", fmt.Errorf("session %s disappeared", session.ID)
			}
			s.mu.RLock()
			status := s.Status
			lastResult := s.Summary.LastResult
			s.mu.RUnlock()
			if !isActiveRemoteSessionStatus(status) {
				// Session has reached a terminal state.
				if status == SessionError {
					return "", fmt.Errorf("agent session %s ended with status: %s", session.ID, status)
				}
				// Collect output from session summary.
				return lastResult, nil
			}
		}
	}
}

// enrichTasksWithCriteria uses the LLM to generate acceptance criteria for
// maintenance-mode tasks that were parsed from plain text and lack criteria.
func (o *SwarmOrchestrator) enrichTasksWithCriteria(tasks []SubTask) []SubTask {
	for i := range tasks {
		if len(tasks[i].AcceptanceCriteria) > 0 {
			continue // already has criteria
		}
		prompt := fmt.Sprintf(`为以下开发任务生成 2-4 条简洁的验收标准。每条标准必须是具体可验证的。

任务描述：%s

只返回 JSON 数组（字符串数组），不要其他内容。例如：["条件1","条件2"]`, tasks[i].Description)

		body, err := swarmCallLLM(o.llmConfig, prompt, 0.2, 30*time.Second)
		if err != nil {
			log.Printf("[SwarmOrchestrator] enrich criteria for task %d failed: %v", i, err)
			continue
		}
		var criteria []string
		cleaned := extractJSON(body)
		if err := json.Unmarshal(cleaned, &criteria); err != nil {
			log.Printf("[SwarmOrchestrator] parse criteria for task %d failed: %v", i, err)
			continue
		}
		tasks[i].AcceptanceCriteria = criteria
	}
	return tasks
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


