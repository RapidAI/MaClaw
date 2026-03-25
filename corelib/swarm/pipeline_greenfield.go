package swarm

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// RunGreenfieldBridge is an exported entry point for the greenfield pipeline,
// used by the GUI bridge to delegate pipeline execution to corelib.
// This will be removed once the GUI switches to *SwarmOrchestrator directly.
func (o *SwarmOrchestrator) RunGreenfieldBridge(run *SwarmRun, req SwarmRunRequest, maxAgents int) error {
	return o.runGreenfield(run, req, maxAgents)
}

// runGreenfield executes the Greenfield mode pipeline using the spec-driven model:
//
//	Phase 1: requirements — 生成结构化需求，暂停等待用户确认
//	Phase 2: design — 架构师生成结构化设计（接口、数据模型、模块依赖）
//	Phase 3: task_split — 基于设计分解任务，每个任务带验收标准
//	Phase 4: development — 并行开发，按验收标准验证
//	Phase 5-8: merge → compile → test → document → report
//
// Pause/cancel checks are performed between each phase.
func (o *SwarmOrchestrator) runGreenfield(run *SwarmRun, req SwarmRunRequest, maxAgents int) error {
	// ---------------------------------------------------------------
	// Phase 1: requirements — 生成结构化需求，暂停等待用户确认
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseRequirements)

	state, err := o.worktreeMgr.PrepareProject(run.ProjectPath)
	if err != nil {
		return fmt.Errorf("prepare project: %w", err)
	}
	run.ProjectState = state

	structuredReqs, err := o.generateRequirements(run, req)
	if err != nil {
		log.Printf("[SwarmOrchestrator] requirements generation failed: %v, using raw requirements", err)
		structuredReqs = req.Requirements
	}
	run.Requirements = structuredReqs
	o.addTimelineEvent(run, "requirements_done", "结构化需求文档已生成，等待用户确认", "")

	// 生成需求 PDF 并通过 IM 发送给用户
	o.sendDocForReview(run, DocTypeRequirements, structuredReqs)

	// 暂停等待用户确认需求
	_ = o.notifier.NotifyWaitingUser(run, "需求文档已生成，请确认或修改后继续。")
	run.Status = SwarmStatusPaused
	select {
	case input := <-run.UserInputCh:
		run.Status = SwarmStatusRunning
		if strings.TrimSpace(input) != "" && input != "ok" && input != "确认" {
			run.Requirements = input
			o.addTimelineEvent(run, "requirements_updated", "用户修改了需求", "")
		} else {
			o.addTimelineEvent(run, "requirements_confirmed", "用户确认了需求", "")
		}
	case <-time.After(24 * time.Hour):
		return fmt.Errorf("等待用户确认需求超时")
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 2: design — 架构师生成结构化设计
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseDesign)

	designDoc, err := o.generateDesign(run, req)
	if err != nil {
		log.Printf("[SwarmOrchestrator] design generation failed: %v, falling back to architect agent", err)
	}

	// 如果 LLM 直接生成设计失败，回退到 architect agent
	if designDoc == "" {
		archOutput, archErr := o.runArchitectPhase(run, req)
		if archErr != nil {
			log.Printf("[SwarmOrchestrator] architect phase also failed: %v", archErr)
		} else {
			designDoc = archOutput
		}
	}
	run.DesignDoc = designDoc
	o.addTimelineEvent(run, "design_done", "结构化设计文档已生成，等待用户确认", "")

	// 生成设计 PDF 并通过 IM 发送给用户
	o.sendDocForReview(run, DocTypeDesign, designDoc)

	// 暂停等待用户确认设计
	_ = o.notifier.NotifyWaitingUser(run, "设计文档已生成，请确认或修改后继续。")
	run.Status = SwarmStatusPaused
	select {
	case input := <-run.UserInputCh:
		run.Status = SwarmStatusRunning
		if strings.TrimSpace(input) != "" && input != "ok" && input != "确认" {
			run.DesignDoc = input
			o.addTimelineEvent(run, "design_updated", "用户修改了设计", "")
		} else {
			o.addTimelineEvent(run, "design_confirmed", "用户确认了设计", "")
		}
	case <-time.After(24 * time.Hour):
		return fmt.Errorf("等待用户确认设计超时")
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 3: task_split — 基于设计分解任务（含验收标准）
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseTaskSplit)

	splitInput := run.Requirements
	if run.DesignDoc != "" {
		splitInput += "\n\n## 设计文档\n" + run.DesignDoc
	}

	var fallbackArchDesign string
	tasks, err := o.taskSplitter.SplitRequirements(splitInput, req.TechStack)
	if err != nil {
		log.Printf("[SwarmOrchestrator] direct LLM split failed: %v, trying agent-based split", err)
		archOutput, archErr := o.runSingleAgent(run, RoleArchitect, 0, run.ProjectPath, PromptContext{
			ProjectName:  run.ProjectPath,
			TechStack:    req.TechStack,
			Requirements: splitInput,
		})
		if archErr == nil {
			fallbackArchDesign = archOutput
			tasks, err = o.taskSplitter.SplitViaAgent(archOutput)
		}
		if err != nil {
			return fmt.Errorf("split requirements: %w", err)
		}
	}
	run.Tasks = tasks
	o.addTimelineEvent(run, "task_split_done",
		fmt.Sprintf("基于设计分解为 %d 个子任务（含验收标准）", len(tasks)), "")

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 4: development — 并行 Developer agents（按验收标准验证）
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseDevelopment)

	archDesign := run.DesignDoc
	if archDesign == "" {
		archDesign = fallbackArchDesign
	}

	if err := o.runDeveloperAgents(run, tasks, maxAgents, req.Tool, archDesign); err != nil {
		log.Printf("[SwarmOrchestrator] development phase had errors: %v", err)
	}
	o.addTimelineEvent(run, "development_done",
		fmt.Sprintf("Development phase completed (%d agents)", len(run.Agents)), "")

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 5-6: merge + compile
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseMerge)

	mergeResult, err := o.runGreenfieldMerge(run, req)
	if err != nil {
		log.Printf("[SwarmOrchestrator] merge phase error: %v", err)
	}

	o.setPhase(run, PhaseCompile)
	if mergeResult != nil && !mergeResult.Success {
		o.addTimelineEvent(run, "merge_partial",
			fmt.Sprintf("Merged %d/%d branches; failed: %v",
				len(mergeResult.MergedBranches),
				len(mergeResult.MergedBranches)+len(mergeResult.FailedBranches),
				mergeResult.FailedBranches), "")
		_ = o.notifier.NotifyFailure(run, "merge",
			fmt.Sprintf("Failed branches: %v", mergeResult.FailedBranches))
	} else {
		o.addTimelineEvent(run, "compile_success", "All branches merged and compiled successfully", "")
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 7: test — feedback loop
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseTest)

	if err := o.runGreenfieldTest(run, req); err != nil {
		log.Printf("[SwarmOrchestrator] test phase: %v", err)
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 8: document
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseDocument)

	o.runGreenfieldDocument(run, req, archDesign)

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 9: report
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseReport)

	_ = o.worktreeMgr.CleanupRun(run.ProjectPath, run.ID)
	_ = o.worktreeMgr.RestoreProject(run.ProjectPath, run.ProjectState)

	return nil
}

// checkPauseCancelGreenfield blocks while the run is paused and returns an
// error if the run has been cancelled.
func (o *SwarmOrchestrator) checkPauseCancelGreenfield(run *SwarmRun) error {
	for run.Status == SwarmStatusPaused {
		time.Sleep(time.Second)
	}
	if run.Status == SwarmStatusCancelled {
		return fmt.Errorf("run %s cancelled", run.ID)
	}
	return nil
}

// runArchitectPhase creates an Architect agent, waits for it, and returns
// the architecture design output.
func (o *SwarmOrchestrator) runArchitectPhase(run *SwarmRun, req SwarmRunRequest) (string, error) {
	branchName := fmt.Sprintf("swarm/%s/architect-0", run.ID)
	wt, err := o.worktreeMgr.CreateWorktree(run.ProjectPath, run.ID, branchName)
	if err != nil {
		return "", fmt.Errorf("create architect worktree: %w", err)
	}

	ctx := PromptContext{
		ProjectName:  run.ProjectPath,
		TechStack:    req.TechStack,
		Requirements: req.Requirements,
	}

	agent, err := o.createAgent(run, RoleArchitect, 0, wt.Path, branchName, req.Tool, ctx)
	if err != nil {
		return "", fmt.Errorf("create architect agent: %w", err)
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent
		return "", fmt.Errorf("architect agent failed: %w", err)
	}

	run.Agents[agentIdx] = *agent
	_ = o.notifier.NotifyAgentComplete(run, agent)

	archDesign := agent.Output
	o.addTimelineEvent(run, "architect_done", "Architect agent completed design", agent.ID)

	return archDesign, nil
}

// runGreenfieldMerge collects completed developer branches and merges them
// in topological order with optional compile verification.
func (o *SwarmOrchestrator) runGreenfieldMerge(run *SwarmRun, req SwarmRunRequest) (*MergeResult, error) {
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

	if len(branches) == 0 {
		o.addTimelineEvent(run, "merge_skip", "No completed developer branches to merge", "")
		return &MergeResult{Success: true}, nil
	}

	compileCmd := InferCompileCommand(req.TechStack)

	result, err := o.mergeCtrl.MergeAll(run.ProjectPath, branches, compileCmd)
	if err != nil {
		return nil, fmt.Errorf("merge all: %w", err)
	}

	o.addTimelineEvent(run, "merge_done",
		fmt.Sprintf("Merge completed: %d merged, %d failed",
			len(result.MergedBranches), len(result.FailedBranches)), "")

	return result, nil
}

// runGreenfieldTest creates a Tester agent, waits for results, and integrates
// the FeedbackLoop for failure classification and repair strategy.
func (o *SwarmOrchestrator) runGreenfieldTest(run *SwarmRun, req SwarmRunRequest) error {
	var featureList []string
	for _, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			featureList = append(featureList, fmt.Sprintf("Task %d: %s", agent.TaskIndex, agent.Output))
		}
	}

	testCmd := InferTestCommand(req.TechStack)

	branchName := fmt.Sprintf("swarm/%s/tester-0", run.ID)
	ctx := PromptContext{
		ProjectName:  run.ProjectPath,
		TechStack:    req.TechStack,
		Requirements: req.Requirements,
		TestCommand:  testCmd,
		FeatureList:  strings.Join(featureList, "\n"),
	}

	agent, err := o.createAgent(run, RoleTester, 0, run.ProjectPath, branchName, req.Tool, ctx)
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

	failures := ParseTestFailures(agent.Output)
	if len(failures) == 0 {
		o.addTimelineEvent(run, "test_pass", "All tests passed", "")
		return nil
	}

	o.addTimelineEvent(run, "test_failures",
		fmt.Sprintf("Found %d test failures, classifying...", len(failures)), "")
	_ = o.notifier.NotifyFailure(run, "test",
		fmt.Sprintf("%d test(s) failed", len(failures)))

	classified, err := o.feedbackLoop.ClassifyFailures(failures)
	if err != nil {
		log.Printf("[SwarmOrchestrator] classify failures: %v", err)
		return nil
	}

	return o.handleClassifiedFailures(run, req, classified)
}

// handleClassifiedFailures routes each classified failure to the appropriate
// repair strategy based on its type.
func (o *SwarmOrchestrator) handleClassifiedFailures(run *SwarmRun, req SwarmRunRequest, classified []ClassifiedFailure) error {
	var bugs, featureGaps []ClassifiedFailure
	hasDeviation := false

	for _, cf := range classified {
		switch cf.Type {
		case FailureTypeBug:
			bugs = append(bugs, cf)
		case FailureTypeFeatureGap:
			featureGaps = append(featureGaps, cf)
		case FailureTypeRequirementDeviation:
			hasDeviation = true
			o.addTimelineEvent(run, "requirement_deviation",
				fmt.Sprintf("Requirement deviation: %s — %s", cf.TestName, cf.Reason), "")
		}
	}

	if hasDeviation {
		_ = o.notifier.NotifyWaitingUser(run, "Test failures indicate requirement deviations. Please confirm requirements.")
		o.addTimelineEvent(run, "waiting_user", "Paused for user confirmation on requirement deviations", "")

		run.Status = SwarmStatusPaused
		select {
		case input := <-run.UserInputCh:
			run.Status = SwarmStatusRunning
			o.addTimelineEvent(run, "user_input", fmt.Sprintf("User provided input: %s", input), "")
		case <-time.After(24 * time.Hour):
			return fmt.Errorf("timed out waiting for user input on requirement deviations")
		}
	}

	if len(bugs) > 0 && o.feedbackLoop.ShouldContinue() {
		o.feedbackLoop.NextRound("bug_fix")
		run.CurrentRound = o.feedbackLoop.Round()
		o.addTimelineEvent(run, "feedback_round",
			fmt.Sprintf("Starting bug fix round %d for %d bugs", run.CurrentRound, len(bugs)), "")

		for _, bug := range bugs {
			bugTask := SubTask{
				Index:       len(run.Tasks),
				Description: fmt.Sprintf("Fix bug: %s — %s", bug.TestName, bug.Reason),
			}
			run.Tasks = append(run.Tasks, bugTask)
		}
	}

	if len(featureGaps) > 0 && o.feedbackLoop.ShouldContinue() {
		o.feedbackLoop.NextRound("feature_gap")
		run.CurrentRound = o.feedbackLoop.Round()
		o.addTimelineEvent(run, "feedback_round",
			fmt.Sprintf("Starting feature gap round %d for %d gaps", run.CurrentRound, len(featureGaps)), "")

		for _, gap := range featureGaps {
			gapTask := SubTask{
				Index:       len(run.Tasks),
				Description: fmt.Sprintf("Implement missing feature: %s — %s", gap.TestName, gap.Reason),
			}
			run.Tasks = append(run.Tasks, gapTask)
		}
	}

	run.RoundHistory = o.feedbackLoop.History()

	return nil
}

// runGreenfieldDocument creates a Documenter agent to generate project docs.
func (o *SwarmOrchestrator) runGreenfieldDocument(run *SwarmRun, req SwarmRunRequest, archDesign string) {
	branchName := fmt.Sprintf("swarm/%s/documenter-0", run.ID)
	ctx := PromptContext{
		ProjectName:   run.ProjectPath,
		TechStack:     req.TechStack,
		ProjectStruct: archDesign,
	}

	agent, err := o.createAgent(run, RoleDocumenter, 0, run.ProjectPath, branchName, req.Tool, ctx)
	if err != nil {
		log.Printf("[SwarmOrchestrator] create documenter agent: %v", err)
		return
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent
		log.Printf("[SwarmOrchestrator] documenter agent failed: %v", err)
		return
	}

	run.Agents[agentIdx] = *agent
	_ = o.notifier.NotifyAgentComplete(run, agent)
	o.addTimelineEvent(run, "document_done", "Documenter agent completed", agent.ID)
}

// ParseTestFailures extracts test failure information from tester agent output.
func ParseTestFailures(output string) []TestFailure {
	if output == "" {
		return nil
	}

	var failures []TestFailure
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL:") {
			name := strings.TrimPrefix(line, "--- FAIL: ")
			name = strings.TrimPrefix(name, "FAIL: ")
			parts := strings.Fields(name)
			testName := name
			if len(parts) > 0 {
				testName = parts[0]
			}
			failures = append(failures, TestFailure{
				TestName:    testName,
				ErrorOutput: line,
			})
		}
	}
	return failures
}

// InferCompileCommand returns a compile command based on the tech stack string.
func InferCompileCommand(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "go build ./..."
	case strings.Contains(ts, "rust"):
		return "cargo build"
	case strings.Contains(ts, "node") || strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "npm run build"
	case strings.Contains(ts, "python"):
		return "python -m py_compile"
	case strings.Contains(ts, "java"):
		return "mvn compile"
	default:
		return ""
	}
}

// generateRequirements uses the LLM to transform raw user requirements into
// a structured requirements document (spec Phase 1).
func (o *SwarmOrchestrator) generateRequirements(run *SwarmRun, req SwarmRunRequest) (string, error) {
	if o.llmCaller == nil {
		return "", fmt.Errorf("LLM caller not configured, cannot generate requirements")
	}

	prompt, err := RenderSpecPrompt("requirements", PromptContext{
		Requirements: req.Requirements,
		TechStack:    req.TechStack,
	})
	if err != nil {
		return "", fmt.Errorf("render requirements prompt: %w", err)
	}

	body, err := o.llmCaller.CallLLM(prompt, 0.2, 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("LLM call for requirements: %w", err)
	}
	return string(body), nil
}

// sendDocForReview 生成 PDF 文档并通过 notifier 发送给用户审阅。
func (o *SwarmOrchestrator) sendDocForReview(run *SwarmRun, docType DocType, content string) {
	if o.docGenerator == nil || !o.docGenerator.HasFont() {
		summary := content
		if len([]rune(summary)) > 500 {
			summary = string([]rune(summary)[:500]) + "\n...(已截断，完整内容请查看项目目录)"
		}
		_ = o.notifier.NotifyWaitingUser(run, summary)
		return
	}

	b64Data, fileName, err := o.docGenerator.GenerateAndEncode(docType, run.ProjectPath, content)
	if err != nil {
		log.Printf("[SwarmOrchestrator] PDF 生成失败: %v，回退为文本通知", err)
		_ = o.notifier.NotifyWaitingUser(run, content)
		return
	}

	var msg string
	switch docType {
	case DocTypeRequirements:
		msg = "📋 需求文档已生成，请查看 PDF 并确认需求是否准确。回复「确认」继续下一阶段，或回复修改意见。"
	case DocTypeDesign:
		msg = "🏗️ 技术设计文档已生成，请查看 PDF 并确认设计方案。回复「确认」继续下一阶段，或回复修改意见。"
	case DocTypeTaskPlan:
		msg = "📝 任务列表已生成，请查看 PDF 并确认任务拆分是否合理。回复「确认」开始自动执行，或回复修改意见。"
	}

	_ = o.notifier.NotifyDocumentForReview(run, b64Data, fileName, "application/pdf", msg)
	o.addTimelineEvent(run, "doc_sent", fmt.Sprintf("已发送 %s PDF: %s", docType, fileName), "")
}

// generateDesign uses the LLM to produce a structured design document.
func (o *SwarmOrchestrator) generateDesign(run *SwarmRun, req SwarmRunRequest) (string, error) {
	if o.llmCaller == nil {
		return "", fmt.Errorf("LLM caller not configured, cannot generate design")
	}

	prompt, err := RenderSpecPrompt("design", PromptContext{
		ProjectName:  run.ProjectPath,
		Requirements: run.Requirements,
		TechStack:    req.TechStack,
	})
	if err != nil {
		return "", fmt.Errorf("render design prompt: %w", err)
	}

	body, err := o.llmCaller.CallLLM(prompt, 0.2, 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("LLM call for design: %w", err)
	}
	return string(body), nil
}

// runSingleAgent creates a session for a given role, sends the task,
// and waits for completion. Returns the agent's output.
func (o *SwarmOrchestrator) runSingleAgent(run *SwarmRun, role AgentRole, taskIndex int, projectPath string, ctx PromptContext) (string, error) {
	prompt, err := RenderPrompt(role, ctx)
	if err != nil {
		return "", fmt.Errorf("render prompt: %w", err)
	}

	if o.sessionMgr == nil {
		log.Printf("[SwarmOrchestrator] no session manager, skipping agent %s-%d", role, taskIndex)
		return prompt, nil
	}

	spec := SwarmLaunchSpec{
		Tool:         run.Tool,
		ProjectPath:  projectPath,
		LaunchSource: "ai",
		Env: map[string]string{
			"SWARM_SYSTEM_PROMPT": prompt,
			"SWARM_ROLE":         string(role),
		},
	}
	if spec.Tool == "" {
		spec.Tool = "claude"
	}

	session, err := o.sessionMgr.Create(spec)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	o.addTimelineEvent(run, "agent_created",
		fmt.Sprintf("Created %s agent for task %d", role, taskIndex), session.SessionID())

	const pollInterval = 5 * time.Second
	const maxWait = 30 * time.Minute
	deadline := time.After(maxWait)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			_ = o.sessionMgr.Kill(session.SessionID())
			return "", fmt.Errorf("agent %s-%d timed out after %v", role, taskIndex, maxWait)
		case <-ticker.C:
			s, ok := o.sessionMgr.Get(session.SessionID())
			if !ok {
				return "", fmt.Errorf("session %s disappeared", session.SessionID())
			}
			status := s.SessionStatus()
			if !isActiveSessionStatus(status) {
				if status == SessionError {
					return "", fmt.Errorf("agent session %s ended with status: %s", session.SessionID(), status)
				}
				summary := s.SessionSummary()
				return summary.LastResult, nil
			}
		}
	}
}

// isActiveSessionStatus returns true if the session is still running.
func isActiveSessionStatus(status SessionStatus) bool {
	switch status {
	case SessionStarting, SessionRunning, SessionBusy, SessionWaitingInput:
		return true
	default:
		return false
	}
}
