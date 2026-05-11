package main

import (
	"fmt"
	"strings"
	"time"

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
				if len(errSnippet) > 80 {
					errSnippet = errSnippet[:80] + "..."
				}
				line += fmt.Sprintf(" [error: %s]", errSnippet)
			}
			b.WriteString(line + "\n")
			// Return step output so the LLM can see actual results (e.g.
			// weather data, API responses) instead of just "success".
			if stepOut := strings.TrimSpace(step.Output); stepOut != "" && totalOutputLen < maxTotalOutputLen {
				remaining := maxTotalOutputLen - totalOutputLen
				limit := maxStepOutputLen
				if remaining < limit {
					limit = remaining
				}
				runes := []rune(stepOut)
				b.WriteString("```\n")
				if len(runes) > limit {
					b.WriteString(string(runes[:limit]))
					b.WriteString("\n... (truncated)\n")
					totalOutputLen += limit
				} else {
					b.WriteString(stepOut)
					b.WriteString("\n")
					totalOutputLen += len(runes)
				}
				b.WriteString("```\n")
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
			b.WriteString("- ⚠️ 会话内部 agent 正在等待输入\n")
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
		b.WriteString("- session_id 已就绪；先调用 get_skill_run(run_id) 确认当前状态，再使用 query_session / send_and_observe 观察会话输出。\n")
	} else if status.IsRunning() {
		b.WriteString("- 使用 get_skill_run(run_id) 继续观察执行进度。\n")
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
				b.WriteString("- ⚙️ 系统正在自动修复此 Skill，建议等待 10 秒后使用 manage_skill(action=\"status\", run_id=\"" + runID + "\") 检查修复状态，再重试执行。\n")
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

func emitSkillRunStatusAgentView(h *IMMessageHandler, status *SkillRunStatus, runID string) {
	if h == nil || h.app == nil {
		return
	}
	h.app.emitAgentView(buildSkillRunStatusAgentView(status, runID))
}

func emitSkillRunProgress(onProgress tool.ProgressCallback, status *SkillRunStatus) {
	if onProgress == nil || status == nil {
		return
	}
	switch {
	case status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "":
		onProgress("🚀 Skill 已绑定会话，可继续观察输出...")
	case status.LifecycleStatus() == skillRunStatusSuccess:
		onProgress("✅ Skill 已执行完成，正在整理结果...")
	case status.IsFailed():
		onProgress("❌ Skill 执行失败，正在整理错误摘要...")
	case status.Summary.CurrentStep != "":
		onProgress(fmt.Sprintf("⏳ Skill 正在执行步骤：%s", status.Summary.CurrentStep))
	default:
		onProgress("⏳ Skill 正在运行，等待状态快照...")
	}
}

func normalizeSkillRunWaitSeconds(raw interface{}) time.Duration {
	seconds := 2.0
	switch v := raw.(type) {
	case float64:
		if v > 0 {
			seconds = v
		}
	case int:
		if v > 0 {
			seconds = float64(v)
		}
	}
	if seconds < 5 {
		seconds = 5
	}
	if seconds > 30 {
		seconds = 30
	}
	return time.Duration(seconds * float64(time.Second))
}

func waitForSkillRunnerSnapshot(runner *SkillRunner, runID string, timeout time.Duration) (*SkillRunStatus, error) {
	if runner == nil {
		return nil, fmt.Errorf("skill runner not initialized")
	}
	deadline := time.Now().Add(timeout)
	// Track whether we've seen any step progress to decide if we should
	// extend the wait for session_id binding.
	sawStepProgress := false
	for {
		status, err := runner.GetRunStatus(runID)
		if err != nil {
			return nil, err
		}
		if status != nil {
			if status.Session != nil && strings.TrimSpace(status.Session.SessionID) != "" {
				return status, nil
			}
			if !status.IsRunning() {
				return status, nil
			}
			if status.Summary.ArtifactStatus.IsDecided() {
				return status, nil
			}
			for _, step := range status.Steps {
				if step.IsTerminal() {
					// A step completed but session_id not yet bound — extend
					// deadline by up to 10s to give create_session time to
					// propagate the session meta. This addresses P0-1 where
					// run_skill returns session_id=null because the snapshot
					// was taken before SetRunSessionMeta completed.
					if !sawStepProgress {
						sawStepProgress = true
						extended := time.Now().Add(10 * time.Second)
						if extended.After(deadline) {
							deadline = extended
						}
					}
					// If session is still not bound after extension, return
					// what we have so the caller can poll via get_skill_run.
					if time.Now().After(deadline) {
						return status, nil
					}
					break // check again after sleep
				}
			}
		}
		if time.Now().After(deadline) {
			return status, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (h *IMMessageHandler) toolRunSkill(args map[string]interface{}, onProgress tool.ProgressCallback) string {
	h.ensureSkillRunner()
	runner := h.getSkillRunner()
	if runner == nil {
		return "Skill Runner 未初始化"
	}
	name, _ := args["name"].(string)
	if name == "" {
		return "缺少 name 参数"
	}
	if h.emitSkillRunAgentViewIfNeeded(name, args) {
		return fmt.Sprintf("Skill「%s」需要补充结构化参数。请在右侧任务面板填写后提交。", name)
	}
	if onProgress != nil {
		onProgress(fmt.Sprintf("🚀 正在启动 Skill「%s」...", name))
	}
	waitDuration := normalizeSkillRunWaitSeconds(args["wait_seconds"])
	runID, err := runner.StartRun(name, buildRunSkillArgs(args))
	if err != nil {
		return fmt.Sprintf("Skill 启动失败: %s", err.Error())
	}
	if onProgress != nil {
		onProgress("⏳ Skill 已启动，正在等待状态快照...")
	}
	status, err := waitForSkillRunnerSnapshot(runner, runID, waitDuration)
	if err != nil {
		return fmt.Sprintf("Skill 已启动，但读取状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	emitSkillRunProgress(onProgress, status)
	var b strings.Builder
	b.WriteString("✅ Skill 已启动\n")
	emitSkillRunStatusAgentView(h, status, runID)
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
	status, err := waitForSkillRunnerSnapshot(runner, runID, waitDuration)
	if err != nil {
		return fmt.Sprintf("读取 Skill 状态失败: %s（run_id=%s）", err.Error(), runID)
	}
	var b strings.Builder
	b.WriteString("🔎 Skill 状态查询结果\n")
	emitSkillRunStatusAgentView(h, status, runID)
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
