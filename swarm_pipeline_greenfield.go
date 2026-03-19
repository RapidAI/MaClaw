package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// runGreenfield executes the Greenfield mode pipeline using the spec-driven model:
//
//   Phase 1: requirements — 生成结构化需求，暂停等待用户确认
//   Phase 2: design — 架构师生成结构化设计（接口、数据模型、模块依赖）
//   Phase 3: task_split — 基于设计分解任务，每个任务带验收标准
//   Phase 4: development — 并行开发，按验收标准验证
//   Phase 5-8: merge → compile → test → document → report
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
	case input := <-run.userInputCh:
		run.Status = SwarmStatusRunning
		if strings.TrimSpace(input) != "" && input != "ok" && input != "确认" {
			// 用户提供了修改意见，更新需求
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
	case input := <-run.userInputCh:
		run.Status = SwarmStatusRunning
		if strings.TrimSpace(input) != "" && input != "ok" && input != "确认" {
			// 用户提供了修改意见，重新生成设计
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

	// 将需求和设计合并作为任务分解的输入
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
		run.Agents[agentIdx] = *agent // sync status
		return "", fmt.Errorf("architect agent failed: %w", err)
	}

	run.Agents[agentIdx] = *agent // sync status
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

	// Determine compile command based on tech stack
	compileCmd := inferCompileCommand(req.TechStack)

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
	// Build feature list from completed agents
	var featureList []string
	for _, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			featureList = append(featureList, fmt.Sprintf("Task %d: %s", agent.TaskIndex, agent.Output))
		}
	}

	testCmd := inferTestCommand(req.TechStack)

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

	// Parse test output for failures
	failures := parseTestFailures(agent.Output)
	if len(failures) == 0 {
		o.addTimelineEvent(run, "test_pass", "All tests passed", "")
		return nil
	}

	// Classify failures via FeedbackLoop
	o.addTimelineEvent(run, "test_failures",
		fmt.Sprintf("Found %d test failures, classifying...", len(failures)), "")
	_ = o.notifier.NotifyFailure(run, "test",
		fmt.Sprintf("%d test(s) failed", len(failures)))

	classified, err := o.feedbackLoop.ClassifyFailures(failures)
	if err != nil {
		log.Printf("[SwarmOrchestrator] classify failures: %v", err)
		return nil // don't fail the whole run for classification errors
	}

	// Handle classified failures by type
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

	// Handle requirement deviations: pause and wait for user input
	if hasDeviation {
		_ = o.notifier.NotifyWaitingUser(run, "Test failures indicate requirement deviations. Please confirm requirements.")
		o.addTimelineEvent(run, "waiting_user", "Paused for user confirmation on requirement deviations", "")

		run.Status = SwarmStatusPaused
		select {
		case input := <-run.userInputCh:
			run.Status = SwarmStatusRunning
			o.addTimelineEvent(run, "user_input", fmt.Sprintf("User provided input: %s", input), "")
		case <-time.After(24 * time.Hour):
			return fmt.Errorf("timed out waiting for user input on requirement deviations")
		}
	}

	// Handle bugs: trigger maintenance round if feedback loop allows
	if len(bugs) > 0 && o.feedbackLoop.ShouldContinue() {
		o.feedbackLoop.NextRound("bug_fix")
		run.CurrentRound = o.feedbackLoop.Round()
		o.addTimelineEvent(run, "feedback_round",
			fmt.Sprintf("Starting bug fix round %d for %d bugs", run.CurrentRound, len(bugs)), "")

		// Create bug descriptions as tasks for a mini maintenance round
		for _, bug := range bugs {
			bugTask := SubTask{
				Index:       len(run.Tasks),
				Description: fmt.Sprintf("Fix bug: %s — %s", bug.TestName, bug.Reason),
			}
			run.Tasks = append(run.Tasks, bugTask)
		}
	}

	// Handle feature gaps: trigger mini-greenfield if feedback loop allows
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

	// Sync round history to the run
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

// parseTestFailures extracts test failure information from tester agent output.
// It looks for common test failure patterns in the output text.
func parseTestFailures(output string) []TestFailure {
	if output == "" {
		return nil
	}

	var failures []TestFailure
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for common failure patterns: "FAIL:", "--- FAIL:", "FAILED"
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL:") {
			name := strings.TrimPrefix(line, "--- FAIL: ")
			name = strings.TrimPrefix(name, "FAIL: ")
			// Extract test name (first word)
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

// inferCompileCommand returns a compile command based on the tech stack string.
func inferCompileCommand(techStack string) string {
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
		return "" // no compile check
	}
}

// inferTestCommand returns a test command based on the tech stack string.
func inferTestCommand(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "go test ./..."
	case strings.Contains(ts, "rust"):
		return "cargo test"
	case strings.Contains(ts, "node") || strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "npm test"
	case strings.Contains(ts, "python"):
		return "pytest"
	case strings.Contains(ts, "java"):
		return "mvn test"
	default:
		return "echo no test command configured"
	}
}

// generateRequirements uses the LLM to transform raw user requirements into
// a structured requirements document (spec Phase 1).
func (o *SwarmOrchestrator) generateRequirements(run *SwarmRun, req SwarmRunRequest) (string, error) {
	prompt, err := RenderSpecPrompt("requirements", PromptContext{
		Requirements: req.Requirements,
		TechStack:    req.TechStack,
	})
	if err != nil {
		return "", fmt.Errorf("render requirements prompt: %w", err)
	}

	body, err := swarmCallLLM(o.llmConfig, prompt, 0.2, 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("LLM call for requirements: %w", err)
	}
	return string(body), nil
}

// sendDocForReview 生成 PDF 文档并通过 notifier 发送给用户审阅。
// 如果 PDF 生成失败（如缺少字体），回退为纯文本通知。
func (o *SwarmOrchestrator) sendDocForReview(run *SwarmRun, docType DocType, content string) {
	if o.docGenerator == nil || !o.docGenerator.HasFont() {
		// 无字体，回退为纯文本摘要
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
		msg = "📋 需求文档已生成，请查阅 PDF 后回复「确认」继续，或回复修改意见。"
	case DocTypeDesign:
		msg = "🏗️ 设计文档已生成，请查阅 PDF。如需调整请回复修改意见。"
	case DocTypeTaskPlan:
		msg = "📝 任务计划已生成，任务将自动执行。"
	}

	_ = o.notifier.NotifyDocumentForReview(run, b64Data, fileName, "application/pdf", msg)
	o.addTimelineEvent(run, "doc_sent", fmt.Sprintf("已发送 %s PDF: %s", docType, fileName), "")
}

// generateDesign uses the LLM to produce a structured design document
// (interfaces, data models, module dependencies) based on the confirmed
// requirements (spec Phase 2).
func (o *SwarmOrchestrator) generateDesign(run *SwarmRun, req SwarmRunRequest) (string, error) {
	prompt, err := RenderSpecPrompt("design", PromptContext{
		ProjectName:  run.ProjectPath,
		Requirements: run.Requirements,
		TechStack:    req.TechStack,
	})
	if err != nil {
		return "", fmt.Errorf("render design prompt: %w", err)
	}

	body, err := swarmCallLLM(o.llmConfig, prompt, 0.2, 120*time.Second)
	if err != nil {
		return "", fmt.Errorf("LLM call for design: %w", err)
	}
	return string(body), nil
}
