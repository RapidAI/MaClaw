package main

// workflow_v2_remote_subagent.go wires the RemoteExperimentOrchestrator into
// the V2 workflow engine's ExecModeRemoteSubAgent dispatch path.
//
// When the paper_reproduction workflow advances to the "iterative_improvement"
// phase (ExecMode=remote_subagent), this code:
//   1. Extracts SSH credentials from Phase 0's FormData
//   2. Ensures an SSH session is connected (reuses existing or creates new)
//   3. Determines project directory from prior phase outputs
//   4. Extracts baseline metric from Phase 4 (baseline_reproduction) output
//   5. Launches RemoteExperimentOrchestrator.Run() in a background goroutine
//   6. Returns an immediate response to the user

import (
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// Pre-compiled regexps (avoid re-compilation on each call).
var (
	// Session ID pattern: ssh_user@host:port_N. Bracketed IPv6 hosts are allowed.
	sshSessionIDRe      = regexp.MustCompile(`ssh_[A-Za-z0-9_.@:\-\[\]]+_\d+`)
	sshSessionIDFieldRe = regexp.MustCompile(`(?im)(?:会话\s*ID|session[_\s-]*id)\s*[:=]\s*"?([^"\s,]+)"?`)

	// Project directory inference patterns
	projectDirPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?:^|\s)cd\s+(/[^\s;|&]+)`),
		regexp.MustCompile(`(?:项目目录|work_dir|project.?dir)[：:=]\s*(/[^\s,;]+)`),
		regexp.MustCompile(`git\s+clone\s+\S+\s+(/[^\s;|&]+)`),
	}

	// Baseline metric patterns (ordered by specificity — more specific first)
	baselineMetricPatterns = []struct {
		re           *regexp.Regexp
		nameGroup    int
		valueGroup   int
		higherBetter bool
	}{
		{regexp.MustCompile(`(?i)(accuracy|acc)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(f1[\s_-]?score|f1)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(bleu)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(rouge[-_]?l?)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(precision|prec)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(recall|rec)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(auc|auroc)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(mAP|map@\d+)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, true},
		{regexp.MustCompile(`(?i)(val[._]?loss|loss)[:\s=]+(\d+\.?\d*)`), 1, 2, false},
		{regexp.MustCompile(`(?i)(perplexity|ppl)[:\s=]+(\d+\.?\d*)`), 1, 2, false},
		{regexp.MustCompile(`(?i)(error[._]?rate|err)[:\s=]+(\d+\.?\d*)\s*%?`), 1, 2, false},
	}
)

// launchRemoteExperimentOrchestrator starts the iterative improvement loop
// for the paper_reproduction workflow. Returns an IMAgentResponse on success,
// or nil if launch failed (caller should fall through to normal agent loop).
func (h *IMMessageHandler) launchRemoteExperimentOrchestrator(userID string, state *v2.WorkflowState) *IMAgentResponse {
	if state == nil {
		return nil
	}

	// --- Step 1: Extract SSH credentials from paper_analysis phase FormData ---
	sshHost, sshPassword, workDir := extractSSHInfoFromWorkflowState(state)
	if sshHost == "" {
		log.Printf("[workflow-v2-remote] no SSH host found in FormData, cannot launch remote subagent")
		return nil
	}

	// Parse host string: "user@host:port" or "host"
	sshUser, sshHostAddr, sshPort := parseSSHHostString(sshHost)
	if sshHostAddr == "" {
		log.Printf("[workflow-v2-remote] failed to parse ssh_host: %q", sshHost)
		return nil
	}

	// --- Step 2: Ensure SSH connection ---
	if h.ensureSSHManager() == nil {
		log.Printf("[workflow-v2-remote] SSH manager not available")
		return nil
	}

	// Connect (or reuse) SSH session to the target host
	sessionID := h.findOrCreateSSHSession(sshUser, sshHostAddr, sshPort, sshPassword)
	if sessionID == "" {
		log.Printf("[workflow-v2-remote] failed to establish SSH session to %s@%s:%d", sshUser, sshHostAddr, sshPort)
		return &IMAgentResponse{
			Text: fmt.Sprintf("无法连接到远程服务器 %s@%s:%d，请检查网络和凭据。", sshUser, sshHostAddr, sshPort),
		}
	}

	// --- Step 3: Determine project directory ---
	projectDir := workDir
	if projectDir == "" {
		// Try to infer from env_and_data or baseline_reproduction outputs
		projectDir = inferProjectDirFromPhaseOutputs(state)
	}
	if projectDir == "" {
		projectDir = "/tmp/maclaw_experiment"
	}

	// --- Step 4: Extract baseline metric from prior phases ---
	params := extractExperimentParams(state)

	// --- Step 5: Mark phase as executing and persist ---
	if wf := h.getWorkflowV2(); wf != nil {
		if phase := state.ActivePhase(); phase != nil {
			phase.Status = v2.PhaseExecuting
			wf.store.Save(state)
		}
	}
	h.emitWorkflowV2Progress(userID, state)

	// --- Step 6: Launch orchestrator in background goroutine ---
	cfg := h.getMaclawLLMConfig()
	httpClient := h.client
	loopCtx := NewLoopContext("remote-experiment-orchestrator", h.getMaclawAgentMaxIterations(), httpClient)

	orchestrator := NewRemoteExperimentOrchestrator(
		h, cfg, httpClient, sessionID, projectDir, params, loopCtx,
	)
	if _, baselineReproValue, _ := parseBaselineMetric(getPhaseOutput(state, "baseline_reproduction")); baselineReproValue != 0 {
		orchestrator.SetBaselineReproduction(baselineReproValue)
	}

	// Set up callbacks for progress and notifications
	orchestrator.SetCallbacks(
		func(progress string) {
			// Progress updates → emit to frontend workflow panel
			log.Printf("[experiment-orchestrator] progress: %s", progress)
			if h.app != nil {
				emitWorkflowV2Event(h.app, "workflow:experiment_progress", map[string]interface{}{
					"user_id":  userID,
					"progress": progress,
				})
			}
		},
		func(notification string) {
			// Important notifications (target reached, plateau, etc.) → IM message to user
			log.Printf("[experiment-orchestrator] notify: %s", notification)
			// Store notification for next user interaction
			h.pendingExperimentNotification.Store(userID, notification)
			if h.app != nil {
				emitWorkflowV2Event(h.app, "workflow:experiment_notification", map[string]interface{}{
					"user_id":      userID,
					"notification": notification,
				})
			}
		},
		nil, // onToken — not needed for background execution
	)

	// Store orchestrator reference for user control (stop/status)
	h.activeExperimentOrchestrator.Store(userID, orchestrator)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[workflow-v2-remote] PANIC in RemoteExperimentOrchestrator: %v", r)
				h.activeExperimentOrchestrator.Delete(userID)
				h.pendingExperimentNotification.Store(userID,
					fmt.Sprintf("迭代改进异常终止: %v\n\n请检查服务器状态后重试。", r))
			}
		}()

		log.Printf("[workflow-v2-remote] starting RemoteExperimentOrchestrator: user=%s session=%s project=%s", userID, sessionID, projectDir)
		stopReason, summary := orchestrator.Run()
		log.Printf("[workflow-v2-remote] orchestrator finished: user=%s reason=%s", userID, stopReason)

		// Clean up orchestrator reference
		h.activeExperimentOrchestrator.Delete(userID)

		// Record phase output with the experiment summary
		if wf := h.getWorkflowV2(); wf != nil {
			experimentReport := fmt.Sprintf("## 迭代改进结果\n\n停止原因: %s\n\n%s", stopReason, summary)
			wf.machine.RecordOutput(userID, experimentReport)

			// Emit progress update
			if updatedState := wf.machine.GetActive(userID); updatedState != nil {
				h.emitWorkflowV2Progress(userID, updatedState)
			}
		}

		// Notify user that the experiment loop has completed
		completionMsg := fmt.Sprintf("迭代改进阶段已完成（%s）\n\n%s\n\n请回复「继续」进入实验报告阶段，或回复「继续实验」追加更多轮次。", stopReason, summary)
		h.pendingExperimentNotification.Store(userID, completionMsg)
		if h.app != nil {
			emitWorkflowV2Event(h.app, "workflow:experiment_notification", map[string]interface{}{
				"user_id":      userID,
				"notification": completionMsg,
			})
		}
	}()

	return &IMAgentResponse{
		Text: fmt.Sprintf("迭代改进已启动\n\n"+
			"• 服务器: %s@%s\n"+
			"• 项目目录: %s\n"+
			"• 目标: %s 超出论文 %.1f%%\n"+
			"• 最大轮数: %d\n"+
			"• 时间限制: %s\n\n"+
			"系统将在后台自动执行「修改代码→训练→评估→判断」循环。\n"+
			"达成目标或遇到平台期时会通知你。\n\n"+
			"你可以随时发送「停止实验」终止循环。",
			sshUser, sshHostAddr, projectDir,
			params.BaselineMetricName, params.TargetExceedance*100,
			params.MaxRounds, formatDuration(params.MaxRuntime)),
	}
}

// --- Helper functions ---

// extractSSHInfoFromWorkflowState gets SSH credentials from the first phase's FormData.
func extractSSHInfoFromWorkflowState(state *v2.WorkflowState) (sshHost, sshPassword, workDir string) {
	if state == nil || len(state.Phases) == 0 {
		return
	}
	// Look in paper_analysis phase (first phase with FormData containing SSH info)
	for _, phase := range state.Phases {
		if phase.FormData == nil {
			continue
		}
		if host, ok := phase.FormData["ssh_host"].(string); ok && host != "" {
			sshHost = strings.TrimSpace(host)
			if pw, ok := phase.FormData["ssh_password"].(string); ok {
				sshPassword = pw
			}
			if wd, ok := phase.FormData["work_dir"].(string); ok && wd != "" {
				workDir = strings.TrimSpace(wd)
			}
			return
		}
	}
	return
}

// parseSSHHostString parses "user@host:port" into components.
// Defaults: user="root", port=22.
func parseSSHHostString(hostStr string) (user, host string, port int) {
	port = 22
	user = "root"

	hostStr = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(hostStr), "ssh://"))
	if idx := strings.Index(hostStr, "/"); idx >= 0 {
		hostStr = hostStr[:idx]
	}

	// Extract user@
	if idx := strings.LastIndex(hostStr, "@"); idx >= 0 {
		if candidate := strings.TrimSpace(hostStr[:idx]); candidate != "" {
			user = candidate
		}
		hostStr = hostStr[idx+1:]
	}

	hostStr = strings.TrimSpace(hostStr)
	if h, p, ok := splitSSHHostPort(hostStr); ok {
		hostStr = h
		port = p
	}

	host = strings.Trim(strings.TrimSpace(hostStr), "[]")
	return
}

func splitSSHHostPort(hostStr string) (host string, port int, ok bool) {
	if strings.TrimSpace(hostStr) == "" {
		return "", 0, false
	}
	if h, p, err := net.SplitHostPort(hostStr); err == nil {
		port, ok = parseSSHPort(p)
		if !ok {
			return "", 0, false
		}
		return strings.Trim(h, "[]"), port, true
	}
	if strings.Count(hostStr, ":") != 1 {
		return "", 0, false
	}
	idx := strings.LastIndex(hostStr, ":")
	if idx < 0 {
		return "", 0, false
	}
	port, ok = parseSSHPort(hostStr[idx+1:])
	if !ok {
		return "", 0, false
	}
	return strings.TrimSpace(hostStr[:idx]), port, true
}

func parseSSHPort(portStr string) (int, bool) {
	p, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || p <= 0 || p >= 65536 {
		return 0, false
	}
	return p, true
}

// findOrCreateSSHSession finds an existing SSH session to the target host,
// or creates a new one using the provided credentials.
func (h *IMMessageHandler) findOrCreateSSHSession(user, host string, port int, password string) string {
	// Use the ssh tool's connect action directly via the handler's sshConnect
	args := map[string]interface{}{
		"action":   "connect",
		"host":     host,
		"port":     float64(port),
		"user":     user,
		"password": password,
	}
	result := h.sshConnect(args)

	if match := extractSSHSessionIDFromConnectResult(result); match != "" {
		return match
	}

	log.Printf("[workflow-v2-remote] SSH connect result (failed to extract session_id): %s", truncateRunesV2(result, 200))
	return ""
}

func extractSSHSessionIDFromConnectResult(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	if match := sshSessionIDFieldRe.FindStringSubmatch(result); len(match) > 1 {
		candidate := strings.TrimSpace(match[1])
		if sshSessionIDRe.MatchString(candidate) {
			return sshSessionIDRe.FindString(candidate)
		}
	}
	return sshSessionIDRe.FindString(result)
}

// inferProjectDirFromPhaseOutputs tries to extract the remote project directory
// from env_and_data or baseline_reproduction phase outputs.
func inferProjectDirFromPhaseOutputs(state *v2.WorkflowState) string {
	for _, phaseID := range []string{"env_and_data", "baseline_reproduction", "reproduction_plan"} {
		output := getPhaseOutput(state, phaseID)
		if output == "" {
			continue
		}
		for _, re := range projectDirPatterns {
			matches := re.FindAllStringSubmatch(output, -1)
			for _, match := range matches {
				if len(match) < 2 {
					continue
				}
				path := strings.TrimRight(match[1], ".,;\"'`)")
				// Skip URLs (http/https paths captured by regex)
				if strings.Contains(path, "://") {
					continue
				}
				// Must look like a filesystem path (at least 2 path components)
				if strings.Count(path, "/") >= 2 {
					return path
				}
			}
		}
	}
	return ""
}

// extractExperimentParams builds ExperimentOrchestratorParams from the workflow state.
func extractExperimentParams(state *v2.WorkflowState) ExperimentOrchestratorParams {
	params := ExperimentOrchestratorParams{
		TargetExceedance:    0.05, // 5% improvement over paper
		MaxRuntime:          24 * time.Hour,
		MaxRounds:           30,
		PlateauTolerance:    5,
		BaselineMetricName:  "Accuracy",
		BaselineMetricValue: 0,
		MetricHigherBetter:  true,
	}

	// Prefer paper_analysis for the paper's reported metric. This value is the
	// target comparator for "exceeds paper"; baseline_reproduction is a separate
	// local reproduction baseline and is injected into the orchestrator state.
	paperOutput := getPhaseOutput(state, "paper_analysis")
	if paperOutput != "" {
		name, value, higherBetter := parseBaselineMetric(paperOutput)
		if name != "" {
			params.BaselineMetricName = name
			params.BaselineMetricValue = value
			params.MetricHigherBetter = higherBetter
		}
	}

	// Fallback to baseline_reproduction only when the paper metric could not be
	// extracted. This preserves launchability while avoiding target drift.
	baselineOutput := getPhaseOutput(state, "baseline_reproduction")
	if baselineOutput != "" && params.BaselineMetricValue == 0 {
		name, value, higherBetter := parseBaselineMetric(baselineOutput)
		if name != "" {
			params.BaselineMetricName = name
			params.BaselineMetricValue = value
			params.MetricHigherBetter = higherBetter
		}
	}

	return params
}

// parseBaselineMetric attempts to extract metric name and value from phase output text.
// Looks for patterns like "Accuracy: 0.856", "F1 = 92.3%", "BLEU score: 34.5".
// Prefers matches in the second half of the text (closer to conclusions/results).
func parseBaselineMetric(output string) (name string, value float64, higherBetter bool) {
	higherBetter = true // default assumption

	// Split text: prefer matches from the second half (closer to results/conclusions)
	runes := []rune(output)
	midpoint := len(runes) / 2
	secondHalf := string(runes[midpoint:])

	// Try second half first (closer to conclusions/results). If no match in
	// the second half, search the full output to catch metric text that spans
	// the midpoint boundary. Each search takes the last candidate by text
	// position, not regex priority, so final reported metrics win.
	if name, value, higherBetter, ok := findLastBaselineMetricCandidate(secondHalf); ok {
		return name, value, higherBetter
	}
	if name, value, higherBetter, ok := findLastBaselineMetricCandidate(output); ok {
		return name, value, higherBetter
	}
	return
}

func findLastBaselineMetricCandidate(output string) (name string, value float64, higherBetter bool, ok bool) {
	bestEnd := -1
	for _, p := range baselineMetricPatterns {
		matches := p.re.FindAllStringSubmatchIndex(output, -1)
		for _, match := range matches {
			nameStart, nameEnd := matchGroupBounds(match, p.nameGroup)
			valueStart, valueEnd := matchGroupBounds(match, p.valueGroup)
			if nameStart < 0 || valueStart < 0 || valueEnd <= bestEnd {
				continue
			}
			parsed, err := strconv.ParseFloat(output[valueStart:valueEnd], 64)
			if err != nil {
				continue
			}
			name = output[nameStart:nameEnd]
			value = parsed
			higherBetter = p.higherBetter
			bestEnd = valueEnd
			ok = true
		}
	}
	return name, value, higherBetter, ok
}

func matchGroupBounds(match []int, group int) (start, end int) {
	idx := group * 2
	if idx+1 >= len(match) {
		return -1, -1
	}
	return match[idx], match[idx+1]
}

// handleExperimentOrchestratorCommand checks if the user's message is a
// control command for the active experiment orchestrator.
// Returns non-nil IMAgentResponse if the message was handled as a command.
func (h *IMMessageHandler) handleExperimentOrchestratorCommand(userID, text string) *IMAgentResponse {
	orchRaw, ok := h.activeExperimentOrchestrator.Load(userID)
	if !ok {
		return nil
	}
	orch, ok := orchRaw.(*RemoteExperimentOrchestrator)
	if !ok || orch == nil {
		// Stale entry — clean up and fall through
		h.activeExperimentOrchestrator.Delete(userID)
		return nil
	}

	state := orch.GetState()

	// Only intercept messages when the orchestrator is actually running.
	// Completed/stopped orchestrators should have been cleaned up by the
	// background goroutine. If they're still here, clean up and fall through.
	if state.Status != "running" {
		h.activeExperimentOrchestrator.Delete(userID)
		return nil
	}

	lower := strings.ToLower(strings.TrimSpace(text))

	// Stop commands
	if lower == "停止实验" || lower == "停止" || lower == "stop" || lower == "stop experiment" {
		orch.Stop()
		return &IMAgentResponse{
			Text: fmt.Sprintf("正在停止实验（当前轮结束后停止）...\n\n"+
				"已完成 %d 轮，当前最佳: %.4f\n"+
				"等待当前轮完成后将生成实验报告。",
				len(state.Rounds), state.BestMetric),
		}
	}

	// Status commands
	if lower == "实验状态" || lower == "进度" || lower == "status" {
		return &IMAgentResponse{
			Text: fmt.Sprintf("实验进度\n\n"+
				"• 状态: %s\n"+
				"• 已完成: %d/%d 轮\n"+
				"• 运行时间: %s\n"+
				"• 当前最佳: %.4f (第 %d 轮)\n"+
				"• 基线复现: %.4f\n"+
				"• 论文值: %.4f\n"+
				"• 连续未改进: %d 轮",
				state.Status,
				len(state.Rounds), state.Params.MaxRounds,
				formatDuration(time.Since(state.StartedAt)),
				state.BestMetric, state.BestRound+1,
				state.BaselineRepro,
				state.Params.BaselineMetricValue,
				state.consecutiveNoImprovement),
		}
	}

	// Experiment is running and user sent a non-command message.
	// Inform them that the experiment is active.
	return &IMAgentResponse{
		Text: fmt.Sprintf("实验正在运行中（第 %d/%d 轮）\n\n"+
			"可用命令：\n• 「停止实验」— 停止当前循环\n• 「实验状态」— 查看进度\n\n"+
			"如需执行其他任务，请先停止实验。",
			len(state.Rounds)+1, state.Params.MaxRounds),
	}
}
