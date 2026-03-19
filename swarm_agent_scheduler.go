package main

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Default agent configuration constants.
const (
	DefaultMaxDeveloperAgents = 5
	MinMaxDeveloperAgents     = 1
	MaxMaxDeveloperAgents     = 10
	DefaultAgentTimeout       = 30 * time.Minute
	MaxAgentRetries           = 2
	agentPollInterval         = 3 * time.Second
)

// createAgent creates a single SwarmAgent backed by a RemoteSession.
// It renders the role-specific system prompt, creates a RemoteSession via
// RemoteSessionManager.Create with ProjectPath pointing to the worktree,
// and returns the populated SwarmAgent.
func (o *SwarmOrchestrator) createAgent(
	run *SwarmRun,
	role AgentRole,
	taskIndex int,
	worktreePath string,
	branchName string,
	tool string,
	ctx PromptContext,
) (*SwarmAgent, error) {
	prompt, err := RenderPrompt(role, ctx)
	if err != nil {
		return nil, fmt.Errorf("render prompt for %s: %w", role, err)
	}

	agent := &SwarmAgent{
		ID:           fmt.Sprintf("%s-%s-%d", run.ID, role, taskIndex),
		Role:         role,
		TaskIndex:    taskIndex,
		WorktreePath: worktreePath,
		BranchName:   branchName,
		Status:       "pending",
	}

	if o.manager == nil {
		// No session manager (testing mode) — mark as completed immediately.
		log.Printf("[SwarmScheduler] no session manager, skipping agent %s", agent.ID)
		now := time.Now()
		agent.Status = "completed"
		agent.StartedAt = &now
		agent.CompletedAt = &now
		agent.Output = prompt
		return agent, nil
	}

	spec := LaunchSpec{
		Tool:         tool,
		ProjectPath:  worktreePath,
		LaunchSource: RemoteLaunchSourceAI,
		Env: map[string]string{
			"SWARM_SYSTEM_PROMPT": prompt,
			"SWARM_ROLE":         string(role),
			"SWARM_RUN_ID":       run.ID,
			"SWARM_TASK_INDEX":   fmt.Sprintf("%d", taskIndex),
		},
	}

	session, err := o.manager.Create(spec)
	if err != nil {
		return nil, fmt.Errorf("create session for %s: %w", agent.ID, err)
	}

	now := time.Now()
	agent.SessionID = session.ID
	agent.Status = "running"
	agent.StartedAt = &now

	o.addTimelineEvent(run, "agent_created",
		fmt.Sprintf("Created %s agent (task %d) → session %s", role, taskIndex, session.ID),
		agent.ID)

	return agent, nil
}

// waitForAgent polls the RemoteSession status until the agent completes,
// errors out, or the timeout expires. On timeout the session is killed and
// the agent is marked as failed.
func (o *SwarmOrchestrator) waitForAgent(run *SwarmRun, agent *SwarmAgent, timeout time.Duration) error {
	if o.manager == nil || agent.SessionID == "" {
		return nil // nothing to wait for in test mode
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(agentPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			// Timeout — kill the session and mark agent as failed.
			log.Printf("[SwarmScheduler] agent %s timed out after %v", agent.ID, timeout)
			_ = o.manager.Kill(agent.SessionID)
			now := time.Now()
			agent.Status = "failed"
			agent.Error = fmt.Sprintf("agent timed out after %v", timeout)
			agent.CompletedAt = &now
			o.addTimelineEvent(run, "agent_timeout",
				fmt.Sprintf("Agent %s timed out after %v", agent.ID, timeout), agent.ID)
			return fmt.Errorf("agent %s timed out", agent.ID)

		case <-ticker.C:
			session, ok := o.manager.Get(agent.SessionID)
			if !ok {
				now := time.Now()
				agent.Status = "failed"
				agent.Error = "session not found"
				agent.CompletedAt = &now
				return fmt.Errorf("session %s not found", agent.SessionID)
			}

			session.mu.RLock()
			status := session.Status
			session.mu.RUnlock()

			switch status {
			case SessionExited:
				// Agent completed successfully.
				now := time.Now()
				agent.Status = "completed"
				agent.CompletedAt = &now
				session.mu.RLock()
				agent.Output = session.Summary.ProgressSummary
				session.mu.RUnlock()
				return nil

			case SessionError:
				// Agent encountered an error.
				now := time.Now()
				agent.Status = "failed"
				agent.CompletedAt = &now
				session.mu.RLock()
				agent.Error = session.Summary.LastResult
				session.mu.RUnlock()
				return fmt.Errorf("agent %s session error: %s", agent.ID, agent.Error)

			default:
				// Still running — check if the run was cancelled.
				if run.Status == SwarmStatusCancelled {
					_ = o.manager.Kill(agent.SessionID)
					now := time.Now()
					agent.Status = "failed"
					agent.Error = "run cancelled"
					agent.CompletedAt = &now
					return fmt.Errorf("run cancelled")
				}
				// Continue polling.
			}
		}
	}
}

// runDeveloperAgents runs developer agents for the given tasks with
// concurrency control. A buffered channel acts as a semaphore to limit
// the number of simultaneously active Developer agents to maxAgents.
// Tasks that exceed the concurrency limit are queued and dispatched as
// slots become available. Each agent has a timeout (DefaultAgentTimeout)
// and will be retried up to MaxAgentRetries times on error.
func (o *SwarmOrchestrator) runDeveloperAgents(
	run *SwarmRun,
	tasks []SubTask,
	maxAgents int,
	tool string,
	archDesign string,
) error {
	maxAgents = ValidateMaxAgents(maxAgents)

	sem := make(chan struct{}, maxAgents)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	for _, task := range tasks {
		// Check if run is cancelled before scheduling.
		if run.Status == SwarmStatusCancelled {
			break
		}
		// Wait while paused.
		for run.Status == SwarmStatusPaused {
			time.Sleep(time.Second)
		}

		sem <- struct{}{} // acquire semaphore slot (blocks if at capacity)
		wg.Add(1)

		go func(t SubTask) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore slot

			err := o.runDeveloperAgentWithRetry(run, t, tool, archDesign, &mu)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		}(task)
	}

	wg.Wait()

	// We don't fail the whole phase for individual agent failures — the
	// merge phase will handle partial results. Log if there were errors.
	if firstErr != nil {
		log.Printf("[SwarmScheduler] some developer agents failed: %v", firstErr)
	}
	return nil
}

// runDeveloperAgentWithRetry creates a developer agent, waits for it, and
// retries up to MaxAgentRetries times on error. Uses ToolSelector to pick
// the best tool for the task, and TaskVerifier to validate the output.
//
// TDD 模式：如果任务有验收标准，先创建 test_writer agent 生成测试代码，
// 然后 developer agent 编写实现使测试通过，最后运行测试命令做硬性验收。
func (o *SwarmOrchestrator) runDeveloperAgentWithRetry(
	run *SwarmRun,
	task SubTask,
	tool string,
	archDesign string,
	mu *sync.Mutex,
) error {
	branchName := fmt.Sprintf("swarm/%s/developer-%d", run.ID, task.Index)

	wt, err := o.worktreeMgr.CreateWorktree(run.ProjectPath, run.ID, branchName)
	if err != nil {
		log.Printf("[SwarmScheduler] create worktree for task %d: %v", task.Index, err)
		return fmt.Errorf("create worktree: %w", err)
	}

	// Smart tool selection: if no fixed tool, pick the best one for this task.
	agentTool := tool
	if agentTool == "" {
		selected, reason := o.selectToolForTask(run, task)
		agentTool = selected
		o.addTimelineEvent(run, "tool_selected",
			fmt.Sprintf("Task %d → %s (%s)", task.Index, selected, reason), "")
	}

	testCmd := inferTestCommand(run.TechStack)

	// ---------------------------------------------------------------
	// TDD Step 1: 先写测试（Red phase）
	// ---------------------------------------------------------------
	if len(task.AcceptanceCriteria) > 0 {
		testCode, testFile := o.runTestWriterAgent(run, task, wt.Path, branchName, agentTool, archDesign, mu)
		if testCode != "" {
			task.TestCode = testCode
			task.TestFile = testFile
			o.addTimelineEvent(run, "tdd_tests_written",
				fmt.Sprintf("Task %d: 测试代码已生成 → %s (Red phase)", task.Index, testFile), "")
		}
	}

	// ---------------------------------------------------------------
	// TDD Step 2: 写实现代码（Green phase）
	// ---------------------------------------------------------------
	ctx := PromptContext{
		ProjectName:        run.ProjectPath,
		TechStack:          run.TechStack,
		TaskDesc:           task.Description,
		ArchDesign:         archDesign,
		AcceptanceCriteria: formatCriteria(task.AcceptanceCriteria),
		TestFile:           task.TestFile,
		TestCode:           task.TestCode,
		TestCommand:        testCmd,
	}

	var agent *SwarmAgent
	var lastErr error

	for attempt := 0; attempt <= MaxAgentRetries; attempt++ {
		if run.Status == SwarmStatusCancelled {
			return fmt.Errorf("run cancelled")
		}

		agent, err = o.createAgent(run, RoleDeveloper, task.Index, wt.Path, branchName, agentTool, ctx)
		if err != nil {
			lastErr = err
			log.Printf("[SwarmScheduler] create agent attempt %d for task %d failed: %v",
				attempt+1, task.Index, err)
			if attempt < MaxAgentRetries {
				agent = &SwarmAgent{
					ID:         fmt.Sprintf("%s-developer-%d", run.ID, task.Index),
					Role:       RoleDeveloper,
					TaskIndex:  task.Index,
					RetryCount: attempt + 1,
					Status:     "pending",
				}
				continue
			}
			break
		}

		// Register agent in the run.
		mu.Lock()
		agent.RetryCount = attempt
		run.Agents = append(run.Agents, *agent)
		agentIdx := len(run.Agents) - 1
		mu.Unlock()

		// Wait for agent completion with timeout.
		waitErr := o.waitForAgent(run, agent, DefaultAgentTimeout)

		mu.Lock()
		run.Agents[agentIdx] = *agent // sync back status changes
		mu.Unlock()

		if waitErr == nil {
			// ---------------------------------------------------------------
			// TDD Step 3: 运行测试验证（硬性验收）
			// ---------------------------------------------------------------
			verdict := o.verifyAgentOutput(run, agent, task)
			if verdict != nil && !verdict.Pass {
				o.addTimelineEvent(run, "task_verify_fail",
					fmt.Sprintf("Task %d 验收未通过 (score=%d): %s — 缺少: %s",
						task.Index, verdict.Score, verdict.Reason, verdict.Missing), agent.ID)
				_ = o.notifier.NotifyFailure(run, "task_verify",
					fmt.Sprintf("Task %d 验收未通过: %s", task.Index, verdict.Reason))

				// If we have retries left and score is very low, retry.
				if verdict.Score < 30 && attempt < MaxAgentRetries {
					lastErr = fmt.Errorf("task verification failed: %s", verdict.Reason)
					log.Printf("[SwarmScheduler] task %d verification failed (score=%d), retrying",
						task.Index, verdict.Score)
					// TDD 重试：将失败信息注入 prompt，让 developer 知道哪些测试失败了
					if task.TestCode != "" {
						ctx.CompileErrors = fmt.Sprintf("上一次尝试的测试结果：\n%s\n缺少: %s",
							verdict.Reason, verdict.Missing)
					}
					o.addTimelineEvent(run, "agent_retry",
						fmt.Sprintf("Retrying task %d due to low verification score", task.Index), agent.ID)
					continue
				}
				o.addTimelineEvent(run, "task_verify_partial",
					fmt.Sprintf("Task %d 部分完成 (score=%d)，继续流程", task.Index, verdict.Score), agent.ID)
			} else if verdict != nil {
				o.addTimelineEvent(run, "task_verify_pass",
					fmt.Sprintf("Task %d 验收通过 (score=%d)", task.Index, verdict.Score), agent.ID)
			}

			_ = o.notifier.NotifyAgentComplete(run, agent)
			return nil
		}

		lastErr = waitErr
		log.Printf("[SwarmScheduler] agent %s failed (attempt %d/%d): %v",
			agent.ID, attempt+1, MaxAgentRetries+1, waitErr)

		if agent.Status == "failed" && attempt < MaxAgentRetries {
			o.addTimelineEvent(run, "agent_retry",
				fmt.Sprintf("Retrying agent %s (attempt %d)", agent.ID, attempt+2), agent.ID)
			agent.RetryCount = attempt + 1
			mu.Lock()
			run.Agents[agentIdx].RetryCount = attempt + 1
			mu.Unlock()
			continue
		}
		break
	}

	// All retries exhausted.
	if agent != nil {
		_ = o.notifier.NotifyFailure(run, "agent_failed",
			fmt.Sprintf("Agent for task %d failed after %d attempts: %v",
				task.Index, agent.RetryCount+1, lastErr))
	}
	return lastErr
}

// runTestWriterAgent 创建 test_writer agent 为任务生成测试代码（TDD Red phase）。
// 返回测试代码内容和测试文件路径。如果生成失败，返回空字符串（回退到 LLM 验证）。
func (o *SwarmOrchestrator) runTestWriterAgent(
	run *SwarmRun,
	task SubTask,
	worktreePath, branchName, tool, archDesign string,
	mu *sync.Mutex,
) (testCode, testFile string) {
	ctx := PromptContext{
		ProjectName:        run.ProjectPath,
		TechStack:          run.TechStack,
		TaskDesc:           task.Description,
		ArchDesign:         archDesign,
		AcceptanceCriteria: formatCriteria(task.AcceptanceCriteria),
	}

	agent, err := o.createAgent(run, RoleTestWriter, task.Index, worktreePath, branchName, tool, ctx)
	if err != nil {
		log.Printf("[SwarmScheduler] create test_writer agent for task %d failed: %v", task.Index, err)
		return "", ""
	}

	mu.Lock()
	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1
	mu.Unlock()

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		mu.Lock()
		run.Agents[agentIdx] = *agent
		mu.Unlock()
		log.Printf("[SwarmScheduler] test_writer agent for task %d failed: %v", task.Index, err)
		return "", ""
	}

	mu.Lock()
	run.Agents[agentIdx] = *agent
	mu.Unlock()

	_ = o.notifier.NotifyAgentComplete(run, agent)

	// 从 agent 输出中提取测试代码和文件路径
	testCode, testFile = extractTestArtifacts(agent.Output, run.TechStack)
	return testCode, testFile
}

// extractTestArtifacts 从 test_writer agent 的输出中提取测试代码和文件路径。
func extractTestArtifacts(output, techStack string) (testCode, testFile string) {
	if output == "" {
		return "", ""
	}

	// 尝试从输出中提取代码块
	testCode = extractCodeBlock(output)
	if testCode == "" {
		testCode = output // 整个输出作为测试代码
	}

	// 尝试从输出中提取文件路径
	testFile = extractTestFilePath(output)
	if testFile == "" {
		// 根据技术栈推断默认测试文件名
		testFile = inferTestFileName(techStack)
	}

	return testCode, testFile
}

// extractCodeBlock 从文本中提取第一个代码块的内容。
func extractCodeBlock(text string) string {
	// 查找 ```lang\n...\n``` 模式
	start := strings.Index(text, "```")
	if start < 0 {
		return ""
	}
	// 跳过 ``` 和可能的语言标识
	rest := text[start+3:]
	nlIdx := strings.Index(rest, "\n")
	if nlIdx < 0 {
		return ""
	}
	rest = rest[nlIdx+1:]

	end := strings.Index(rest, "```")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// extractTestFilePath 从 agent 输出中提取测试文件路径。
func extractTestFilePath(output string) string {
	// 常见模式: "文件: xxx_test.go" 或 "File: test_xxx.py"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if (strings.Contains(lower, "文件") || strings.Contains(lower, "file")) &&
			(strings.Contains(line, "_test.") || strings.Contains(line, "test_") || strings.Contains(line, ".test.") || strings.Contains(line, ".spec.")) {
			// 提取路径部分
			for _, part := range strings.Fields(line) {
				part = strings.Trim(part, ":`\"'")
				if strings.Contains(part, "_test.") || strings.Contains(part, "test_") || strings.Contains(part, ".test.") || strings.Contains(part, ".spec.") {
					return part
				}
			}
		}
	}
	return ""
}

// inferTestFileName 根据技术栈推断默认测试文件名。
func inferTestFileName(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "task_test.go"
	case strings.Contains(ts, "python"):
		return "test_task.py"
	case strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "task.test.ts"
	case strings.Contains(ts, "rust"):
		return "tests/task_test.rs"
	case strings.Contains(ts, "java"):
		return "src/test/java/TaskTest.java"
	default:
		return "test_task"
	}
}

// verifyAgentOutput uses the TaskVerifier to check if the agent's output
// matches the task description and acceptance criteria.
// TDD 模式：如果任务有 TestFile/TestCode，优先运行实际测试命令做硬性验收；
// 否则回退到 LLM 语义验证。
func (o *SwarmOrchestrator) verifyAgentOutput(run *SwarmRun, agent *SwarmAgent, task SubTask) *TaskVerdict {
	if o.taskVerifier == nil || agent.Output == "" {
		return nil
	}

	// TDD 模式：有测试代码时，运行实际测试
	if task.TestCode != "" && task.TestFile != "" {
		testCmd := inferTestCommand(run.TechStack)
		verdict := o.taskVerifier.VerifyByTest(agent.WorktreePath, testCmd, task.TestFile)
		o.addTimelineEvent(run, "tdd_verify",
			fmt.Sprintf("Task %d TDD 验证: pass=%v score=%d — %s",
				task.Index, verdict.Pass, verdict.Score, verdict.Reason), agent.ID)
		return verdict
	}

	// 回退到 LLM 语义验证
	verdict, err := o.taskVerifier.Verify(task.Description, agent.Output, task.AcceptanceCriteria...)
	if err != nil {
		log.Printf("[SwarmScheduler] task verification error for task %d: %v", task.Index, err)
		return nil
	}
	return verdict
}

// formatCriteria joins acceptance criteria into a numbered list string.
// Returns empty string if no criteria are provided.
func formatCriteria(criteria []string) string {
	if len(criteria) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, c := range criteria {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
	}
	return sb.String()
}
