package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func appendSkillRunSummary(b *strings.Builder, status *SkillRunStatus, runID string) {
	if b == nil {
		return
	}
	b.WriteString("## 运行信息\n")
	b.WriteString(fmt.Sprintf("- run_id: %s\n", runID))
	if status == nil {
		b.WriteString("- status: unknown\n")
		return
	}
	b.WriteString(fmt.Sprintf("- skill: %s\n", status.Skill))
	b.WriteString(fmt.Sprintf("- status: %s\n", status.Status))
	// Include elapsed time when the skill is still running. This serves two purposes:
	// 1. Gives the LLM useful context about how long the task has been running
	// 2. Makes each status poll return a different result (elapsed changes),
	//    preventing the drift detector's freqResultsAreProgressing from
	//    seeing identical results and triggering a false frequency anomaly.
	if status.IsRunning() && status.StartedAt != "" {
		if startT, err := time.Parse(time.RFC3339, status.StartedAt); err == nil {
			elapsed := time.Since(startT).Truncate(time.Second)
			b.WriteString(fmt.Sprintf("- elapsed: %s\n", elapsed))
		}
	}
	if len(status.Warnings) > 0 {
		b.WriteString("## Warnings\n")
		for _, warning := range status.Warnings {
			warning = strings.TrimSpace(warning)
			if warning == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("- %s\n", warning))
		}
	}
	// session_ready: explicit signal for callers to know if session_id is available.
	// Only emit when the skill actually involves sessions to avoid confusing the
	// LLM with "session_ready: false" on pure-bash skills like weather-query.
	sessionReady := status.Session != nil && strings.TrimSpace(status.Session.SessionID) != ""
	if sessionReady || status.IsRunning() {
		b.WriteString(fmt.Sprintf("- session_ready: %v\n", sessionReady))
	}
	if status.Summary.CurrentStep != "" {
		b.WriteString(fmt.Sprintf("- current_step: %s (%s)\n", status.Summary.CurrentStep, status.Summary.CurrentStepStatus.OrElse(skillStepStatusRunning)))
	}
	if status.Summary.LastCompletedStep != "" {
		b.WriteString(fmt.Sprintf("- last_completed_step: %s\n", status.Summary.LastCompletedStep))
	}
	if status.Summary.NeedsArtifactVerification {
		b.WriteString("## 结果说明\n")
		b.WriteString("- 这是一个仅提供 SKILL.md 指导的 skill；当前结果只表示脚本已生成并执行。\n")
		if status.Summary.ArtifactPath != "" {
			b.WriteString(fmt.Sprintf("- 目标产物: %s\n", status.Summary.ArtifactPath))
			switch status.Summary.ArtifactStatus {
			case skillArtifactStatusVerified:
				b.WriteString("- 产物已自动验证存在。\n")
			case skillArtifactStatusMissing:
				b.WriteString("- 目标产物尚未生成到该路径；当前不能算成功交付。\n")
			default:
				b.WriteString("- 宿主尚未完成产物验证，请继续观察。\n")
			}
		} else {
			b.WriteString("- 宿主尚未定位目标产物路径；如果目标是 PPT/PDF，请继续检查输出文件。\n")
		}
	}
	if status.Summary.ArtifactPath != "" {
		b.WriteString(fmt.Sprintf("- artifact_path: %s\n", status.Summary.ArtifactPath))
	}
	if status.Summary.ArtifactStatus != "" {
		b.WriteString(fmt.Sprintf("- artifact_status: %s\n", status.Summary.ArtifactStatus))
	}
	if status.Summary.LastErrorSnippet != "" {
		b.WriteString(fmt.Sprintf("- last_error: %s\n", status.Summary.LastErrorSnippet))
	}
	// Step-level progress: show each step's status AND output for visibility
	// into intermediate execution stages (addresses P1-5 from LibTV report).
	// Previously only status/duration/error were shown; step.Output was stored
	// but never returned to the LLM, causing "session_ready: false" confusion
	// for non-session skills (e.g. weather-query) where the actual result is
	// in stdout.
	//
	// Budget: cap total step output to ~4096 chars to avoid bloating LLM context
	// when a skill has many steps. Individual steps are capped at 2048 chars.
	const maxStepOutputLen = 2048
	const maxTotalOutputLen = 4096
	totalOutputLen := 0
	if len(status.Steps) > 0 {
		b.WriteString(fmt.Sprintf("- total_steps: %d\n", len(status.Steps)))
		b.WriteString("## 步骤进度\n")
		for i, step := range status.Steps {
			label := step.Name
			if label == "" {
				label = step.Action
			}
			line := fmt.Sprintf("- step %d: %s → %s", i+1, label, step.Status)
			if step.DurationMs > 0 {
				line += fmt.Sprintf(" (%dms)", step.DurationMs)
			}
			if step.Error != "" {
				errSnippet := step.Error
				if len(errSnippet) > 200 {
					errSnippet = errSnippet[:200] + "..."
				}
				line += fmt.Sprintf(" [error: %s]", errSnippet)
			}
			b.WriteString(line + "\n")
			// Return step output so the LLM can see actual results (e.g.
			// weather data, API responses) instead of just "success".
			// Truncation takes from the TAIL: error messages, file paths,
			// and final status lines typically appear at the end of output.
			if stepOut := strings.TrimSpace(step.Output); stepOut != "" && totalOutputLen < maxTotalOutputLen {
				remaining := maxTotalOutputLen - totalOutputLen
				limit := maxStepOutputLen
				if remaining < limit {
					limit = remaining
				}
				runes := []rune(stepOut)
				b.WriteString("```\n")
				if len(runes) > limit {
					b.WriteString("... (truncated, showing last ")
					b.WriteString(fmt.Sprintf("%d", limit))
					b.WriteString(" chars)\n")
					b.WriteString(string(runes[len(runes)-limit:]))
					b.WriteString("\n")
					totalOutputLen += limit
				} else {
					b.WriteString(stepOut)
					b.WriteString("\n")
					totalOutputLen += len(runes)
				}
				b.WriteString("```\n")
			}
			// For failed steps: surface dedicated stderr lines if available.
			// Stderr contains the most diagnostic content (tracebacks, compile
			// errors) but may be drowned out by verbose stdout in the combined
			// output. Showing it separately ensures the LLM can diagnose.
			if step.IsFailed() && len(step.StderrLastLines) > 0 && totalOutputLen < maxTotalOutputLen {
				stderrText := strings.TrimSpace(strings.Join(step.StderrLastLines, "\n"))
				if stderrText != "" {
					remaining := maxTotalOutputLen - totalOutputLen
					limit := 512
					if remaining < limit {
						limit = remaining
					}
					runes := []rune(stderrText)
					if len(runes) > limit {
						stderrText = string(runes[len(runes)-limit:])
					}
					b.WriteString("stderr:\n```\n")
					b.WriteString(stderrText)
					b.WriteString("\n```\n")
					totalOutputLen += len([]rune(stderrText))
				}
			}
		}
	}
	if status.Session != nil {
		b.WriteString("## 会话信息\n")
		if strings.TrimSpace(status.Session.SessionID) != "" {
			b.WriteString(fmt.Sprintf("- session_id: %s\n", status.Session.SessionID))
		}
		if strings.TrimSpace(status.Session.Tool) != "" {
			b.WriteString(fmt.Sprintf("- tool: %s\n", status.Session.Tool))
		}
		if strings.TrimSpace(status.Session.ProjectPath) != "" {
			b.WriteString(fmt.Sprintf("- project_path: %s\n", status.Session.ProjectPath))
		}
		if strings.TrimSpace(status.Session.Status.String()) != "" {
			b.WriteString(fmt.Sprintf("- session_status: %s\n", status.Session.Status))
		}
		if strings.TrimSpace(status.Session.ResumeSessionID) != "" {
			b.WriteString(fmt.Sprintf("- resume_session_id: %s\n", status.Session.ResumeSessionID))
		}
	}
	// Session progress: show what the session's internal AI agent is doing.
	if status.SessionProgress != nil {
		sp := status.SessionProgress
		b.WriteString("## 会话内部进度\n")
		b.WriteString(fmt.Sprintf("- session_status: %s\n", sp.SessionStatus))
		if sp.CurrentTask != "" {
			b.WriteString(fmt.Sprintf("- current_action: %s\n", sp.CurrentTask))
		}
		if sp.ProgressSummary != "" {
			b.WriteString(fmt.Sprintf("- progress: %s\n", sp.ProgressSummary))
		}
		if sp.LastResult != "" {
			b.WriteString(fmt.Sprintf("- last_result: %s\n", sp.LastResult))
		}
		if sp.LastCommand != "" {
			b.WriteString(fmt.Sprintf("- last_command: %s\n", sp.LastCommand))
		}
		if sp.WaitingForUser {
			b.WriteString("- 会话内部 agent 正在等待输入\n")
		}
		b.WriteString(fmt.Sprintf("- poll_count: %d\n", sp.PollCount))
		if sp.UpdatedAt != "" {
			b.WriteString(fmt.Sprintf("- updated_at: %s\n", sp.UpdatedAt))
		}
	}
	b.WriteString("## 下一步\n")
	if sessionReady && status.SessionProgress != nil {
		b.WriteString("- session 内部进度已自动监控，继续调用 get_skill_run(run_id) 即可查看最新状态。\n")
	} else if sessionReady {
		b.WriteString("- session_id 来自旧外部会话路径；继续调用 get_skill_run(run_id) 观察状态。新编程任务请走内部 CodingSubAgent，不再使用外部会话续接工具。\n")
	} else if status.IsRunning() {
		b.WriteString("- still_running: true\n")
		b.WriteString(fmt.Sprintf("- poll_hint: manage_skill(action=\"status\", run_id=%q, wait_seconds=30)\n", runID))
		b.WriteString("- 使用 manage_skill(action=\"status\", run_id=...) 继续观察；建议 wait_seconds=15~60，不要在任务仍 running 时去 Hub 安装替代下载 skill。\n")
	} else if status.IsFinished() {
		// Skill has finished — step outputs are already shown above.
		// No need to direct the LLM to poll or wait for session_ready.
		if status.IsFailed() {
			// Include action hint from the last failed step to guide the LLM
			// on what to do next (retry, patch, search alternative, etc.).
			for i := len(status.Steps) - 1; i >= 0; i-- {
				if status.Steps[i].IsFailed() && status.Steps[i].Error != "" {
					ce := cskill.ClassifyStepError(
						status.Steps[i].ExitCode,
						status.Steps[i].Output,
						status.Steps[i].Error,
						status.Steps[i].CommandResolved,
					)
					if ce.ActionHint != "" {
						b.WriteString(fmt.Sprintf("- 建议操作: %s\n", ce.ActionHint))
					}
					if ce.Retryable {
						b.WriteString("- 此错误可重试（transient error）\n")
					}
					break
				}
			}
			if status.SelfRepairPending {
				b.WriteString("- 系统正在自动修复此 Skill，建议等待 10 秒后使用 manage_skill(action=\"status\", run_id=\"" + runID + "\") 检查修复状态，再重试执行。\n")
			} else {
				b.WriteString("- Skill 执行失败，步骤输出已在上方显示。请根据建议操作决定下一步。\n")
			}
		} else {
			b.WriteString("- Skill 已执行完毕，步骤输出已在上方显示。请直接基于输出内容回复用户。\n")
		}
	} else {
		b.WriteString("- 使用 get_skill_run(run_id) 查看最终结果。\n")
	}
}

// emitSkillRunProgress sends a progress callback to the frontend streaming
// indicator (not the agent view panel).
func emitSkillRunProgress(onProgress tool.ProgressCallback, status *SkillRunStatus) {
	if onProgress == nil || status == nil {
		return
	}
	switch {
	case status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "":
		onProgress("Skill 已绑定会话，可继续观察输出...")
	case status.LifecycleStatus() == skillRunStatusSuccess:
		onProgress("Skill 已执行完成，正在整理结果...")
	case status.IsFailed():
		onProgress("Skill 执行失败，正在整理错误摘要...")
	case status.Summary.CurrentStep != "":
		onProgress(fmt.Sprintf("Skill 正在执行步骤：%s", status.Summary.CurrentStep))
	default:
		onProgress("Skill 正在运行，等待状态快照...")
	}
}

func normalizeSkillRunWaitSeconds(raw interface{}) time.Duration {
	seconds := 15.0
	switch v := raw.(type) {
	case float64:
		if v >= 0 {
			seconds = v
		}
	case int:
		if v >= 0 {
			seconds = float64(v)
		}
	}
	if seconds > 0 && seconds < 0.1 {
		seconds = 0.1
	}
	if seconds > 120 {
		seconds = 120
	}
	return time.Duration(seconds * float64(time.Second))
}

func normalizeInitialSkillRunWaitSeconds(raw interface{}) time.Duration {
	if raw == nil {
		return 20 * time.Second
	}
	return normalizeSkillRunWaitSeconds(raw)
}

func waitForSkillRunnerSnapshot(ctx context.Context, runner *SkillRunner, runID string, timeout time.Duration) (*SkillRunStatus, error) {
	if runner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	startedAt := time.Now()
	polls := 0
	extendedForTerminalStep := false
	var lastStatus string
	var lastOwner string
	var lastSkill string
	deadline := time.Now().Add(timeout)
	log.Printf("[skill-runner-wait] start run=%s timeout=%s", runID, timeout.Round(time.Millisecond))
	finish := func(reason string, status *SkillRunStatus) (*SkillRunStatus, error) {
		if status != nil {
			lastStatus = status.Status.String()
			lastOwner = status.OwnerID
			lastSkill = status.Skill
		}
		log.Printf("[skill-runner-wait] done run=%s owner=%q skill=%q reason=%s status=%q polls=%d elapsed=%s extended_terminal=%v",
			runID, lastOwner, lastSkill, reason, lastStatus, polls, time.Since(startedAt).Round(time.Millisecond), extendedForTerminalStep)
		return status, nil
	}
	// Track whether we've seen any step progress to decide if we should
	// extend the wait for session_id binding.
	sawStepProgress := false
	for {
		polls++
		status, err := runner.GetRunStatus(runID)
		if err != nil {
			log.Printf("[skill-runner-wait] error run=%s polls=%d elapsed=%s err=%v", runID, polls, time.Since(startedAt).Round(time.Millisecond), err)
			return nil, err
		}
		if status != nil {
			lastStatus = status.Status.String()
			lastOwner = status.OwnerID
			lastSkill = status.Skill
			if status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "" {
				return finish("session_ready", status)
			}
			if !status.IsRunning() {
				return finish("finished", status)
			}
			if status.Summary.ArtifactStatus.IsDecided() {
				return finish("artifact_decided", status)
			}
			expectsSession := skillRunStatusExpectsSession(status)
			for _, step := range status.Steps {
				if expectsSession && step.IsTerminal() {
					// A step completed but session_id not yet bound — extend
					// deadline by up to 10s to give legacy session binding time to
					// propagate the session meta. This addresses P0-1 where
					// run_skill returns session_id=null because the snapshot
					// was taken before SetRunSessionMeta completed.
					if !sawStepProgress {
						sawStepProgress = true
						extended := time.Now().Add(10 * time.Second)
						if extended.After(deadline) {
							deadline = extended
							extendedForTerminalStep = true
							log.Printf("[skill-runner-wait] extend run=%s owner=%q skill=%q reason=terminal_step_without_session deadline_in=%s",
								runID, lastOwner, lastSkill, time.Until(deadline).Round(time.Millisecond))
						}
					}
					// If session is still not bound after extension, return
					// what we have so the caller can poll via get_skill_run.
					if time.Now().After(deadline) {
						return finish("terminal_step_deadline", status)
					}
					break // check again after sleep
				}
			}
		}
		if time.Now().After(deadline) {
			return finish("deadline", status)
		}
		select {
		case <-ctx.Done():
			return finish("cancelled", status)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func skillRunStatusExpectsSession(status *SkillRunStatus) bool {
	if status == nil {
		return false
	}
	if status.Session != nil {
		return true
	}
	for _, step := range status.Steps {
		switch classifySkillStepAction(step.Action) {
		case skillStepActionCreateSession, skillStepActionSendInput, skillStepActionSendAndObserve, skillStepActionControlSession:
			return true
		}
	}
	return false
}

func (h *IMMessageHandler) toolRunSkill(ctx context.Context, args map[string]interface{}, onProgress tool.ProgressCallback) string {
	h.ensureSkillRunner()
	runner := h.getSkillRunner()
	if runner == nil {
		return "Skill Runner 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	// When the agent (LLM) calls run_skill with missing parameters, return a
	// structured error so the LLM can extract the required info from user
	// context and retry. Do NOT pop up an AgentView form — the agent should
	// auto-fill parameters, not delegate to the user.
	if errMsg := h.checkSkillRunMissingParams(name, args); errMsg != "" {
		return errMsg
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("正在启动 Skill「%s」...", name))
	}
	waitDuration := normalizeInitialSkillRunWaitSeconds(args["wait_seconds"])
	ownerID, explicitRuntime := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args)
	if ownerID == "" && explicitRuntime {
		return "Skill 启动失败: runtime owner is missing; isolated runtime will not fall back to desktop owner"
	}
	toolStartedAt := time.Now()
	runArgs := buildRunSkillArgs(args)
	log.Printf("[run_skill] start owner=%q explicit_runtime=%v skill=%q wait=%s args=%d", ownerID, explicitRuntime, name, waitDuration.Round(time.Millisecond), len(runArgs))
	// Wire up dependency installation progress reporting: when PipFixer/NpmFixer
	// installs packages during PrepareRunnerExecution, the progress callback
	// surfaces real-time status to the user (e.g. "正在安装 Python 包 pymupdf...").
	if onProgress != nil {
		runner.prepProgressByOwner.Store(ownerID, cskill.FixProgressCallback(func(msg string) { onProgress(msg) }))
	}
	runID, err := runner.StartRunForOwner(ownerID, name, runArgs)
	runner.prepProgressByOwner.Delete(ownerID) // clear after call returns
	if err != nil {
		log.Printf("[run_skill] start failed owner=%q skill=%q elapsed=%s err=%v", ownerID, name, time.Since(toolStartedAt).Round(time.Millisecond), err)
	} else {
		log.Printf("[run_skill] launched owner=%q skill=%q run=%s launch_elapsed=%s", ownerID, name, runID, time.Since(toolStartedAt).Round(time.Millisecond))
	}
	if err != nil {
		return fmt.Sprintf("Skill 启动失败: %s", err.Error())
	}
	if onProgress != nil {
		onProgress("Skill 已启动，正在等待状态快照...")
	}
	waitStartedAt := time.Now()
	status, err := waitForSkillRunnerSnapshot(ctx, runner, runID, waitDuration)
	if err != nil {
		log.Printf("[run_skill] wait failed owner=%q skill=%q run=%s wait_elapsed=%s total=%s err=%v", ownerID, name, runID, time.Since(waitStartedAt).Round(time.Millisecond), time.Since(toolStartedAt).Round(time.Millisecond), err)
	} else {
		statusLabel := ""
		if status != nil {
			statusLabel = status.Status.String()
		}
		log.Printf("[run_skill] done owner=%q skill=%q run=%s status=%q wait_elapsed=%s total=%s", ownerID, name, runID, statusLabel, time.Since(waitStartedAt).Round(time.Millisecond), time.Since(toolStartedAt).Round(time.Millisecond))
	}
	if err != nil {
		return fmt.Sprintf("Skill 已启动，但读取状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	emitSkillRunProgress(onProgress, status)
	var b strings.Builder
	b.WriteString("Skill 已启动\n")
	appendSkillRunSummary(&b, status, runID)
	return strings.TrimRight(b.String(), "\n")
}

func (h *IMMessageHandler) toolGetSkillRun(args map[string]interface{}) string {
	h.ensureSkillRunner()
	runner := h.getSkillRunner()
	if runner == nil {
		return "Skill Runner 未初始化"
	}
	runID, _ := args["run_id"].(string)
	if strings.TrimSpace(runID) == "" {
		return "缺少 run_id 参数"
	}
	waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
	startedAt := time.Now()
	log.Printf("[get_skill_run] start run=%s wait=%s", runID, waitDuration.Round(time.Millisecond))
	status, err := waitForSkillRunnerSnapshot(context.Background(), runner, runID, waitDuration)
	if err != nil {
		log.Printf("[get_skill_run] failed run=%s elapsed=%s err=%v", runID, time.Since(startedAt).Round(time.Millisecond), err)
	} else {
		statusLabel := ""
		ownerID := ""
		if status != nil {
			statusLabel = status.Status.String()
			ownerID = status.OwnerID
		}
		log.Printf("[get_skill_run] done run=%s owner=%q status=%q elapsed=%s", runID, ownerID, statusLabel, time.Since(startedAt).Round(time.Millisecond))
	}
	if err != nil {
		// Distinguish "not found" (may have been pruned after completion) from
		// other errors to avoid confusing the LLM into thinking the skill crashed.
		if strings.Contains(err.Error(), "not found") {
			return fmt.Sprintf("run_id %q 不存在（可能已执行完毕并被清理）。如果 Skill 之前已返回成功结果，无需再次查询。", runID)
		}
		return fmt.Sprintf("读取 Skill 状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	var b strings.Builder
	b.WriteString("Skill 状态查询结果\n")
	appendSkillRunSummary(&b, status, runID)
	return strings.TrimRight(b.String(), "\n")
}

func buildRunSkillArgs(args map[string]interface{}) map[string]interface{} {
	runArgs := map[string]interface{}{}
	for k, v := range args {
		if cskill.IsManageSkillRunnerControlKey(k) {
			continue
		}
		runArgs[k] = v
	}
	if len(runArgs) == 0 {
		return nil
	}
	return runArgs
}

// checkSkillRunMissingParams checks whether the agent-provided args satisfy
// all required parameters for the skill. If not, it returns a structured error
// message that tells the LLM exactly which parameters are missing and how to
// provide them. Returns "" when all parameters are satisfied.
//
// This replaces the old emitSkillRunAgentViewIfNeeded approach which popped up
// a UI form for the user to fill — a mechanism-level error because the agent
// should auto-fill parameters from user context, not delegate to the user.
func (h *IMMessageHandler) checkSkillRunMissingParams(name string, args map[string]interface{}) string {
	if h == nil || h.app == nil {
		return ""
	}
	target := h.app.findSkillForAgentView(name)
	if target == nil {
		return ""
	}
	runArgs := buildRunSkillArgs(args)
	vars := normalizeSkillRunVars(runArgs)
	// Apply the same input inference that PrepareRunnerExecution uses.
	// Without this, we'd false-positive on params that can be inferred from
	// aliases (e.g. "text" → "input", "query" → "input").
	cskill.ApplyRunInputInference(target, vars, runArgs)
	params, missing := skillRunParameterContract(target, vars, runArgs)
	if unknown := skillRunUnconsumedArgs(target, params, vars, runArgs); len(unknown) > 0 {
		return skillRunUnconsumedArgsMessage(name, unknown, params, target)
	}
	if len(missing) == 0 {
		return ""
	}
	// Build a structured error message that the LLM can act on.
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Skill「%s」缺少必要参数，无法执行。\n", name))
	b.WriteString("\n## 缺少的参数\n")
	for _, key := range missing {
		desc := findParamDescription(params, key)
		if desc != "" {
			b.WriteString(fmt.Sprintf("- **%s**: %s\n", key, desc))
		} else {
			b.WriteString(fmt.Sprintf("- **%s**\n", key))
		}
	}
	if len(params) > 0 {
		b.WriteString("\n## 参数契约\n")
		if schema := cskill.FormatParamSchema(params); schema != "" {
			b.WriteString(schema)
		}
		if js := cskill.FormatParamSchemaJSON(params); js != "" {
			b.WriteString("JSON Schema: ")
			b.WriteString(js)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n## 如何修复\n")
	b.WriteString("请从用户的对话上下文中提取所需信息，然后重新调用：\n")
	b.WriteString(fmt.Sprintf("```\nmanage_skill(action=\"run\", name=\"%s\"", name))
	for _, key := range missing {
		b.WriteString(fmt.Sprintf(", %s=\"<从用户请求中提取>\"", key))
	}
	b.WriteString(")\n```\n")
	if desc := strings.TrimSpace(target.Description); desc != "" {
		b.WriteString(fmt.Sprintf("\n## Skill 描述\n%s\n", desc))
	}
	b.WriteString("\n[action: provide_args]")
	return b.String()
}

func skillRunUnconsumedArgsMessage(name string, unknown []string, params []corelib.NLSkillParam, target *corelib.NLSkillEntry) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Skill 启动失败：Skill「%s」未声明或消费这些运行参数。\n", name))
	b.WriteString("\n## 未消费参数\n")
	for _, key := range unknown {
		b.WriteString(fmt.Sprintf("- %s\n", key))
	}
	b.WriteString("\n## 原因\n")
	b.WriteString("为了避免参数被静默忽略导致错误结果，运行时要求传入参数必须出现在 Skill 参数契约、别名、required_args 或步骤模板占位符中。\n")
	if len(params) > 0 {
		b.WriteString("\n## 该 Skill 可消费参数\n")
		for _, p := range params {
			name := strings.TrimSpace(p.Name)
			if name == "" {
				continue
			}
			b.WriteString("- " + name)
			if len(p.Aliases) > 0 {
				b.WriteString(" (aliases: " + strings.Join(p.Aliases, ", ") + ")")
			}
			b.WriteString("\n")
		}
	} else {
		b.WriteString("\n该 Skill 当前没有声明任何可消费运行参数。\n")
	}
	if target != nil && strings.TrimSpace(target.Description) != "" {
		b.WriteString("\n## Skill 描述\n")
		b.WriteString(strings.TrimSpace(target.Description))
		b.WriteString("\n")
	}
	b.WriteString("\n[action: contract_mismatch]")
	return b.String()
}

// findParamDescription looks up the description for a parameter by name.
func findParamDescription(params []corelib.NLSkillParam, name string) string {
	for _, p := range params {
		if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(name)) {
			return strings.TrimSpace(p.Description)
		}
	}
	return ""
}
