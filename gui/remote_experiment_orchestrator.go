package main

// remote_experiment_orchestrator.go implements the iterative experiment loop
// for the paper reproduction workflow. It orchestrates the "modify code →
// train → evaluate → check stop conditions" cycle, running autonomously
// until a stop condition is met, then notifying the user via IM.
//
// The orchestrator uses RemoteCodingSubAgent for code modifications and
// SSH background tasks for training. It maintains experiment state in
// results/summary.json on the remote server.

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ExperimentOrchestratorParams are the user-configured parameters for the
// iterative improvement loop.
type ExperimentOrchestratorParams struct {
	TargetExceedance float64       // notify when exceeding paper by this % (default 0.1)
	MaxRuntime       time.Duration // total runtime cap (default 48h)
	MaxRounds        int           // maximum improvement rounds (default 50)
	PlateauTolerance int           // consecutive rounds without improvement before asking user (default 8)
	FailureTolerance int           // consecutive failed rounds before stopping as error (default 3)

	// Baseline from paper_analysis phase output
	BaselineMetricName  string  // e.g. "Accuracy", "F1", "BLEU"
	BaselineMetricValue float64 // paper's best reported value
	MetricHigherBetter  bool    // true = higher is better (Accuracy), false = lower is better (loss)
}

// ExperimentRoundResult records the outcome of a single experiment round.
type ExperimentRoundResult struct {
	RoundNumber         int       `json:"round"`
	StartedAt           time.Time `json:"started_at"`
	CompletedAt         time.Time `json:"completed_at"`
	Modification        string    `json:"modification"` // what was changed
	Reason              string    `json:"reason"`       // why this direction
	MetricValue         float64   `json:"metric_value"`
	DeltaFromBase       float64   `json:"delta_from_baseline"` // positive = improvement
	DeltaFromPaper      float64   `json:"delta_from_paper"`    // positive = exceeds paper
	Config              string    `json:"config_summary"`
	VerificationCommand string    `json:"verification_command,omitempty"`
	VerificationResult  string    `json:"verification_result,omitempty"`
	Status              string    `json:"status"` // "completed", "failed", "timeout"
	Error               string    `json:"error,omitempty"`
}

// ExperimentOrchestratorState is the full state of the experiment loop.
type ExperimentOrchestratorState struct {
	mu sync.Mutex

	Params        ExperimentOrchestratorParams
	Rounds        []ExperimentRoundResult
	BestRound     int     // index of best round
	BestMetric    float64 // best metric value achieved
	BaselineRepro float64 // baseline reproduction value (from phase 4)
	StartedAt     time.Time
	Status        string // "running", "paused", "completed", "stopped"

	// Runtime
	consecutiveNoImprovement int
	consecutiveFailures      int
}

// StopReason describes why the orchestrator stopped.
type StopReason string

const (
	StopReasonTargetReached StopReason = "target_reached"
	StopReasonTimeExpired   StopReason = "time_expired"
	StopReasonMaxRounds     StopReason = "max_rounds"
	StopReasonPlateau       StopReason = "plateau"
	StopReasonUserStop      StopReason = "user_stop"
	StopReasonError         StopReason = "error"
)

// RemoteExperimentOrchestrator drives the iterative improvement loop.
type RemoteExperimentOrchestrator struct {
	handler    *IMMessageHandler
	cfg        corelib.MaclawLLMConfig
	httpClient *http.Client

	// SSH context
	sessionID  string
	projectDir string

	// SubAgent for code modifications
	subAgent *RemoteCodingSubAgent

	// State
	state ExperimentOrchestratorState

	// Callbacks
	onProgress func(string) // progress updates for UI panel
	onNotify   func(string) // IM notification (ask_user)
	onToken    func(string) // streaming text

	// Cancellation
	loopCtx  *LoopContext
	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewRemoteExperimentOrchestrator creates a new orchestrator.
func NewRemoteExperimentOrchestrator(
	handler *IMMessageHandler,
	cfg corelib.MaclawLLMConfig,
	httpClient *http.Client,
	sessionID, projectDir string,
	params ExperimentOrchestratorParams,
	loopCtx *LoopContext,
) *RemoteExperimentOrchestrator {
	// Apply defaults
	if params.TargetExceedance <= 0 {
		params.TargetExceedance = 0.1
	}
	if params.MaxRuntime <= 0 {
		params.MaxRuntime = 48 * time.Hour
	}
	if params.MaxRounds <= 0 {
		params.MaxRounds = 50
	}
	if params.PlateauTolerance <= 0 {
		params.PlateauTolerance = 8
	}
	if params.FailureTolerance <= 0 {
		params.FailureTolerance = 3
	}
	initialBest := 0.0
	initialBaselineRepro := 0.0
	if params.BaselineMetricValue != 0 {
		initialBest = params.BaselineMetricValue
		initialBaselineRepro = params.BaselineMetricValue
	}

	subAgent := NewRemoteCodingSubAgent(handler, cfg, httpClient, sessionID, projectDir, projectDir, loopCtx)

	return &RemoteExperimentOrchestrator{
		handler:    handler,
		cfg:        cfg,
		httpClient: httpClient,
		sessionID:  sessionID,
		projectDir: projectDir,
		subAgent:   subAgent,
		state: ExperimentOrchestratorState{
			Params:        params,
			BestMetric:    initialBest,
			BaselineRepro: initialBaselineRepro,
			Status:        "running",
			StartedAt:     time.Now(),
		},
		loopCtx: loopCtx,
		stopCh:  make(chan struct{}),
	}
}

// SetCallbacks configures notification and progress callbacks.
func (o *RemoteExperimentOrchestrator) SetCallbacks(onProgress, onNotify, onToken func(string)) {
	o.onProgress = onProgress
	o.onNotify = onNotify
	o.onToken = onToken
	o.subAgent.SetCallbacks(onToken, onProgress)
}

// SetKnowledgeStores passes knowledge stores to the SubAgent.
func (o *RemoteExperimentOrchestrator) SetKnowledgeStores(codingKB *knowledge.CodingKnowledgeStore, generalKB *knowledge.SQLiteStore) {
	o.subAgent.SetKnowledgeStores(codingKB, generalKB)
}

// SetBaselineReproduction records the baseline reproduction result from phase 4.
func (o *RemoteExperimentOrchestrator) SetBaselineReproduction(value float64) {
	o.state.BaselineRepro = value
	o.state.BestMetric = value
}

// Run executes the full iterative improvement loop. Blocks until a stop
// condition is met or the user requests stop.
func (o *RemoteExperimentOrchestrator) Run() (StopReason, string) {
	defer func() {
		o.state.mu.Lock()
		if o.state.Status == "running" {
			o.state.Status = "completed"
		}
		o.state.mu.Unlock()
	}()

	params := o.state.Params

	for round := 1; round <= params.MaxRounds; round++ {
		// Check cancellation
		if o.isStopped() {
			return StopReasonUserStop, o.buildProgressSummary()
		}

		// Check time limit
		elapsed := time.Since(o.state.StartedAt)
		if elapsed >= params.MaxRuntime {
			msg := fmt.Sprintf("⏰ 时间到期（已运行 %s）。当前最佳 %.2f%%，共完成 %d 轮\n回复\"继续\"/\"停止\"",
				formatDuration(elapsed), o.state.BestMetric, len(o.state.Rounds))
			o.notify(msg)
			return StopReasonTimeExpired, o.buildProgressSummary()
		}

		// Execute one round
		o.progress(fmt.Sprintf("🔄 第 %d/%d 轮改进...", round, params.MaxRounds))
		result := o.executeOneRound(round)

		// Record result
		o.state.mu.Lock()
		o.state.Rounds = append(o.state.Rounds, result)

		o.applyRoundResultLocked(result)
		consecutiveFailures := o.state.consecutiveFailures
		consecutiveNoImprovement := o.state.consecutiveNoImprovement
		o.state.mu.Unlock()

		// Save round evidence before any stop condition returns so both
		// successes and failures can inform future rounds.
		o.saveRoundExperience(result)

		// Check repeated failures before plateau. Failures usually indicate
		// environment, parsing, or execution problems that should be surfaced
		// directly instead of spending more rounds on similar attempts.
		if consecutiveFailures >= params.FailureTolerance {
			msg := fmt.Sprintf("❌ 连续 %d 轮实验失败，已暂停自动迭代。最近错误：%s\n请检查环境/日志或调整实验方向后继续。",
				consecutiveFailures, o.recentFailureSummary(1))
			o.notify(msg)
			return StopReasonError, o.buildProgressSummary()
		}

		// Check target reached
		deltaFromPaper := result.DeltaFromPaper
		targetDelta := experimentTargetDeltaThreshold(params)
		if result.Status == "completed" && deltaFromPaper >= targetDelta {
			msg := fmt.Sprintf("🎉 目标达成！当前最佳 %.2f%%（论文 %.2f%%，超出 +%.2f%%）\n第 %d 轮，关键改进：%s\n回复\"继续\"继续冲击更高 / \"停止\"生成报告",
				o.state.BestMetric, params.BaselineMetricValue, deltaFromPaper, round, result.Modification)
			o.notify(msg)
			return StopReasonTargetReached, o.buildProgressSummary()
		}

		// Check plateau
		if consecutiveNoImprovement >= params.PlateauTolerance {
			directions := o.recentDirections(3)
			msg := fmt.Sprintf("📊 遇到平台期（连续 %d 轮无进展）。当前 %.2f%%，已尝试：%s\n回复新方向（如\"试试对比学习\"）/ \"继续\" / \"停止\"",
				consecutiveNoImprovement, o.state.BestMetric, directions)
			o.notify(msg)
			return StopReasonPlateau, o.buildProgressSummary()
		}

	}

	// Max rounds exhausted
	msg := fmt.Sprintf("🔄 已完成全部 %d 轮改进。最佳 %.2f%%（基线 %.2f%%，论文 %.2f%%）\n回复\"继续\"追加轮数 / \"停止\"生成报告",
		params.MaxRounds, o.state.BestMetric, o.state.BaselineRepro, params.BaselineMetricValue)
	o.notify(msg)
	return StopReasonMaxRounds, o.buildProgressSummary()
}

// Stop signals the orchestrator to stop after the current round.
func (o *RemoteExperimentOrchestrator) Stop() {
	o.stopOnce.Do(func() {
		close(o.stopCh)
		o.state.mu.Lock()
		o.state.Status = "stopped"
		o.state.mu.Unlock()
	})
}

// GetState returns a snapshot of the current experiment state.
func (o *RemoteExperimentOrchestrator) GetState() ExperimentOrchestratorState {
	o.state.mu.Lock()
	defer o.state.mu.Unlock()
	// Build a copy without the mutex (avoid copying sync.Mutex).
	return ExperimentOrchestratorState{
		Params:                   o.state.Params,
		Rounds:                   append([]ExperimentRoundResult(nil), o.state.Rounds...),
		BestRound:                o.state.BestRound,
		BestMetric:               o.state.BestMetric,
		BaselineRepro:            o.state.BaselineRepro,
		StartedAt:                o.state.StartedAt,
		Status:                   o.state.Status,
		consecutiveNoImprovement: o.state.consecutiveNoImprovement,
		consecutiveFailures:      o.state.consecutiveFailures,
	}
}

// --- Internal ---

func (o *RemoteExperimentOrchestrator) executeOneRound(roundNum int) ExperimentRoundResult {
	result := ExperimentRoundResult{
		RoundNumber: roundNum,
		StartedAt:   time.Now(),
	}

	// Build task description for the SubAgent
	taskDesc := o.buildRoundTaskDescription(roundNum)
	taskCtx := o.buildRoundContext()

	// Execute via RemoteCodingSubAgent
	subResult := o.subAgent.ExecuteTask(taskDesc, taskCtx)

	result.CompletedAt = time.Now()

	if subResult.Status != "success" {
		result.Status = "failed"
		result.Error = subResult.Error
		if result.Error == "" {
			result.Error = subResult.Summary
		}
		return result
	}

	o.populateCompletedRoundResult(&result, subResult.Summary)

	return result
}

func (o *RemoteExperimentOrchestrator) applyRoundResultLocked(result ExperimentRoundResult) bool {
	if o == nil {
		return false
	}
	improved := false
	if o.completedRoundImprovesBestLocked(result) {
		o.state.BestMetric = result.MetricValue
		o.state.BestRound = len(o.state.Rounds) - 1
		improved = true
	}
	if result.Status == "failed" {
		o.state.consecutiveFailures++
	} else {
		o.state.consecutiveFailures = 0
	}
	if improved {
		o.state.consecutiveNoImprovement = 0
	} else {
		o.state.consecutiveNoImprovement++
	}
	return improved
}

func (o *RemoteExperimentOrchestrator) completedRoundImprovesBestLocked(result ExperimentRoundResult) bool {
	if result.Status != "completed" {
		return false
	}
	if o == nil {
		return false
	}
	// With no known baseline and no previous completed round, the first valid
	// metric is the best available evidence regardless of metric direction.
	if o.state.BaselineRepro == 0 && o.state.Params.BaselineMetricValue == 0 && !o.hasPriorCompletedRoundLocked() {
		return true
	}
	if o.state.Params.MetricHigherBetter {
		return result.MetricValue > o.state.BestMetric
	}
	return result.MetricValue < o.state.BestMetric
}

func (o *RemoteExperimentOrchestrator) hasPriorCompletedRoundLocked() bool {
	if o == nil || len(o.state.Rounds) <= 1 {
		return false
	}
	for _, round := range o.state.Rounds[:len(o.state.Rounds)-1] {
		if round.Status == "completed" {
			return true
		}
	}
	return false
}

func (o *RemoteExperimentOrchestrator) populateCompletedRoundResult(result *ExperimentRoundResult, output string) {
	if result == nil {
		return
	}
	result.Status = "completed"
	parsedRound := parseRoundOutput(output)
	result.Modification = parsedRound.Modification
	result.Reason = parsedRound.Reason
	result.MetricValue = parsedRound.MetricValue
	result.Config = parsedRound.Config
	result.VerificationCommand = parsedRound.VerificationCommand
	result.VerificationResult = parsedRound.VerificationResult

	// If metric parsing failed, mark as failed — don't pollute delta calculations.
	if !parsedRound.HasMetric {
		result.Status = "failed"
		result.Error = "无法从 SubAgent 输出中解析实验结果（缺少 [ROUND_RESULT] 块或 metric_value）"
		return
	}
	if strings.TrimSpace(parsedRound.VerificationCommand) == "" || strings.TrimSpace(parsedRound.VerificationResult) == "" {
		result.Status = "failed"
		result.Error = "无法从 SubAgent 输出中解析实验验证证据（缺少 verification_command 或 verification_result）"
		return
	}
	if experimentVerificationResultLooksFailed(parsedRound.VerificationResult) {
		result.Status = "failed"
		result.Error = "SubAgent 报告的验证结果失败: " + strings.TrimSpace(parsedRound.VerificationResult)
		return
	}

	// Calculate deltas (positive = improvement for both directions).
	if o.state.Params.MetricHigherBetter {
		result.DeltaFromBase = result.MetricValue - o.state.BaselineRepro
		result.DeltaFromPaper = result.MetricValue - o.state.Params.BaselineMetricValue
	} else {
		result.DeltaFromBase = o.state.BaselineRepro - result.MetricValue
		result.DeltaFromPaper = o.state.Params.BaselineMetricValue - result.MetricValue
	}
}

func (o *RemoteExperimentOrchestrator) buildRoundTaskDescription(roundNum int) string {
	params := o.state.Params
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# 实验改进第 %d 轮\n\n", roundNum))
	sb.WriteString(fmt.Sprintf("目标：提升 %s 指标（当前最佳: %.4f，论文: %.4f，目标超出: +%.2f%%）\n\n",
		params.BaselineMetricName, o.state.BestMetric, params.BaselineMetricValue, params.TargetExceedance*100))

	sb.WriteString("## 要求\n\n")
	sb.WriteString("1. 分析之前的实验结果，选择最有潜力的改进方向\n")
	sb.WriteString("2. 修改代码实现改进\n")
	sb.WriteString("3. 运行训练/评估（如需长时间训练，使用 ssh_bash 会自动转后台任务）\n")
	sb.WriteString("4. 报告本轮结果\n\n")

	sb.WriteString("## 输出格式\n\n")
	sb.WriteString("请在最后输出以下格式的结果：\n")
	sb.WriteString("[ROUND_RESULT]\n")
	sb.WriteString("modification: <本轮修改了什么>\n")
	sb.WriteString("reason: <为什么选这个方向>\n")
	sb.WriteString(fmt.Sprintf("metric_value: <评估得到的 %s 数值>\n", params.BaselineMetricName))
	sb.WriteString("config: <关键超参数摘要>\n")
	sb.WriteString("verification_command: <实际运行的训练/评估命令>\n")
	sb.WriteString("verification_result: <命令退出状态和关键输出摘要>\n")
	sb.WriteString("[/ROUND_RESULT]\n")

	return sb.String()
}

func (o *RemoteExperimentOrchestrator) buildRoundContext() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("项目目录: %s\n", o.projectDir))
	sb.WriteString(fmt.Sprintf("主指标: %s (%.4f = 论文值)\n", o.state.Params.BaselineMetricName, o.state.Params.BaselineMetricValue))
	sb.WriteString(fmt.Sprintf("基线复现值: %.4f\n", o.state.BaselineRepro))
	sb.WriteString(fmt.Sprintf("当前最佳: %.4f (第 %d 轮)\n\n", o.state.BestMetric, o.state.BestRound+1))

	// Recent rounds history
	if len(o.state.Rounds) > 0 {
		sb.WriteString("## 历史实验记录（最近 5 轮）\n\n")
		sb.WriteString(recentRoundSummary(o.state.Rounds, 5))
		sb.WriteString("\n")
	}

	return sb.String()
}

func (o *RemoteExperimentOrchestrator) buildProgressSummary() string {
	state := o.GetState()
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("完成 %d 轮改进，耗时 %s\n", len(state.Rounds), formatDuration(time.Since(state.StartedAt))))
	sb.WriteString(fmt.Sprintf("最佳结果: %.4f (第 %d 轮)\n", state.BestMetric, state.BestRound+1))
	sb.WriteString(fmt.Sprintf("论文值: %.4f, 基线复现: %.4f\n", state.Params.BaselineMetricValue, state.BaselineRepro))
	if best := bestRoundFromState(state); best != nil {
		sb.WriteString(fmt.Sprintf("最佳轮次详情: 第%d轮 %s=%.4f (Δpaper=%.4f)\n", best.RoundNumber, state.Params.BaselineMetricName, best.MetricValue, best.DeltaFromPaper))
		if strings.TrimSpace(best.Modification) != "" {
			sb.WriteString(fmt.Sprintf("最佳修改: %s\n", best.Modification))
		}
		if strings.TrimSpace(best.Config) != "" {
			sb.WriteString(fmt.Sprintf("最佳配置: %s\n", best.Config))
		}
		if strings.TrimSpace(best.VerificationCommand) != "" {
			sb.WriteString(fmt.Sprintf("最佳验证命令: %s\n", best.VerificationCommand))
		}
		if strings.TrimSpace(best.VerificationResult) != "" {
			sb.WriteString(fmt.Sprintf("最佳验证结果: %s\n", best.VerificationResult))
		}
	}
	if failures := recentFailureSummaryFromRounds(state.Rounds, 3); failures != "无" {
		sb.WriteString(fmt.Sprintf("最近失败: %s\n", failures))
	}
	if recent := recentRoundSummary(state.Rounds, 5); recent != "" {
		sb.WriteString("最近轮次:\n")
		sb.WriteString(recent)
	}
	return sb.String()
}

func bestRoundFromState(state ExperimentOrchestratorState) *ExperimentRoundResult {
	if state.BestRound < 0 || state.BestRound >= len(state.Rounds) {
		return nil
	}
	round := state.Rounds[state.BestRound]
	if round.Status != "completed" {
		return nil
	}
	return &round
}

func recentRoundSummary(rounds []ExperimentRoundResult, n int) string {
	if n <= 0 {
		n = 5
	}
	if len(rounds) == 0 {
		return ""
	}
	start := len(rounds) - n
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	for _, round := range rounds[start:] {
		label := "unknown"
		switch round.Status {
		case "completed":
			label = fmt.Sprintf("completed metric=%.4f Δpaper=%.4f", round.MetricValue, round.DeltaFromPaper)
			if strings.TrimSpace(round.Modification) != "" {
				label += " change=" + strings.TrimSpace(round.Modification)
			}
			if strings.TrimSpace(round.VerificationCommand) != "" {
				label += " verify=" + strings.TrimSpace(round.VerificationCommand)
			}
		case "failed":
			label = "failed"
			if strings.TrimSpace(round.Error) != "" {
				label += " error=" + strings.TrimSpace(round.Error)
			}
		default:
			if strings.TrimSpace(round.Status) != "" {
				label = strings.TrimSpace(round.Status)
			}
		}
		sb.WriteString(fmt.Sprintf("- 第%d轮: %s\n", round.RoundNumber, label))
	}
	return sb.String()
}

func (o *RemoteExperimentOrchestrator) recentDirections(n int) string {
	state := o.GetState()
	if len(state.Rounds) == 0 {
		return "无"
	}
	start := len(state.Rounds) - n
	if start < 0 {
		start = 0
	}
	var dirs []string
	for _, r := range state.Rounds[start:] {
		if r.Modification != "" {
			dirs = append(dirs, r.Modification)
		}
	}
	if len(dirs) == 0 {
		return "无"
	}
	return strings.Join(dirs, ", ")
}

func (o *RemoteExperimentOrchestrator) recentFailureSummary(n int) string {
	state := o.GetState()
	return recentFailureSummaryFromRounds(state.Rounds, n)
}

func recentFailureSummaryFromRounds(rounds []ExperimentRoundResult, n int) string {
	if n <= 0 {
		n = 1
	}
	var failures []string
	for i := len(rounds) - 1; i >= 0 && len(failures) < n; i-- {
		round := rounds[i]
		if round.Status != "failed" {
			continue
		}
		detail := strings.TrimSpace(round.Error)
		if detail == "" {
			detail = "未知错误"
		}
		failures = append(failures, fmt.Sprintf("第%d轮: %s", round.RoundNumber, detail))
	}
	if len(failures) == 0 {
		return "无"
	}
	return strings.Join(failures, "; ")
}

func (o *RemoteExperimentOrchestrator) saveRoundExperience(result ExperimentRoundResult) {
	if o.subAgent == nil {
		return
	}
	exp, ok := o.codingExperienceForRound(result)
	if !ok {
		return
	}
	_ = o.subAgent.SaveExperience(exp)
}

func (o *RemoteExperimentOrchestrator) codingExperienceForRound(result ExperimentRoundResult) (knowledge.CodingExperience, bool) {
	if o == nil {
		return knowledge.CodingExperience{}, false
	}
	params := o.state.Params
	targetDelta := experimentTargetDeltaThreshold(params)
	switch result.Status {
	case "completed":
		content := fmt.Sprintf("方向: %s\n结果: %s=%.4f (Δpaper=%.4f, 目标阈值=%.4f)\n配置: %s", result.Reason, params.BaselineMetricName, result.MetricValue, result.DeltaFromPaper, targetDelta, result.Config)
		if strings.TrimSpace(result.VerificationCommand) != "" {
			content += "\n验证命令: " + strings.TrimSpace(result.VerificationCommand)
		}
		if strings.TrimSpace(result.VerificationResult) != "" {
			content += "\n验证结果: " + strings.TrimSpace(result.VerificationResult)
		}
		exp := knowledge.CodingExperience{
			Title:            fmt.Sprintf("实验改进: %s", result.Modification),
			Content:          content,
			TriggerCondition: fmt.Sprintf("优化 %s 指标", params.BaselineMetricName),
			Scope:            "experiment",
			Category:         "optimization",
			Language:         "python",
			Status:           knowledge.CodingStatusCandidate,
			Confidence:       0.6,
			SuccessCount:     1,
			Labels:           []string{"remote_experiment", "completed"},
		}
		if result.DeltaFromPaper >= targetDelta {
			exp.Status = knowledge.CodingStatusActive
			exp.Confidence = 0.85
			exp.Labels = append(exp.Labels, "target_reached")
		}
		return exp, true
	case "failed":
		detail := strings.TrimSpace(result.Error)
		if detail == "" {
			detail = "未知失败"
		}
		exp := knowledge.CodingExperience{
			Title:             fmt.Sprintf("实验失败: 第%d轮", result.RoundNumber),
			Content:           fmt.Sprintf("远程实验第%d轮失败。\n方向/修改: %s\n原因/假设: %s\n错误: %s\n配置: %s", result.RoundNumber, result.Modification, result.Reason, detail, result.Config),
			TriggerCondition:  fmt.Sprintf("避免 %s 实验失败", params.BaselineMetricName),
			Scope:             "experiment",
			Category:          "pitfall",
			Language:          "python",
			Status:            knowledge.CodingStatusCandidate,
			Confidence:        0.35,
			FailureCount:      1,
			FailedAttempts:    []string{detail},
			Contraindications: []string{"不要在没有修复该错误或补充诊断前重复同一实验方向"},
			Labels:            []string{"remote_experiment", "failed"},
		}
		return exp, true
	default:
		return knowledge.CodingExperience{}, false
	}
}

func experimentTargetDeltaThreshold(params ExperimentOrchestratorParams) float64 {
	if params.BaselineMetricValue != 0 {
		return absFloat(params.BaselineMetricValue) * params.TargetExceedance
	}
	return params.TargetExceedance
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (o *RemoteExperimentOrchestrator) isStopped() bool {
	select {
	case <-o.stopCh:
		return true
	default:
	}
	if o.loopCtx != nil && o.loopCtx.IsCancelled() {
		return true
	}
	return false
}

func (o *RemoteExperimentOrchestrator) progress(text string) {
	if o.onProgress != nil {
		o.onProgress(text)
	}
}

func (o *RemoteExperimentOrchestrator) notify(text string) {
	if o.onNotify != nil {
		o.onNotify(text)
	}
	log.Printf("[experiment-orchestrator] notify: %s", text)
}

// --- Parsing helpers ---

type parsedExperimentRoundOutput struct {
	Modification        string
	Reason              string
	MetricValue         float64
	Config              string
	VerificationCommand string
	VerificationResult  string
	HasMetric           bool
}

var (
	roundMetricValuePattern        = regexp.MustCompile(`[-+]?\d+(?:\.\d+)?(?:[eE][-+]?\d+)?%?`)
	roundVerificationExitCodeRegex = regexp.MustCompile(`(?i)\b(?:exit|exit[_\s-]*code|status|code)\s*[:=]?\s*(-?\d+)\b`)
)

// parseRoundOutput extracts structured result from SubAgent's natural language output.
func parseRoundOutput(output string) parsedExperimentRoundOutput {
	parsed := parsedExperimentRoundOutput{
		Modification: "未知修改",
		Reason:       "自主决策",
	}
	block, ok := roundResultBlock(output)
	if !ok {
		return parsed
	}

	for _, line := range strings.Split(block, "\n") {
		key, value, ok := splitRoundResultLine(line)
		if !ok {
			continue
		}
		switch normalizeRoundResultKey(key) {
		case "modification", "change", "changes":
			if value != "" {
				parsed.Modification = value
			}
		case "reason", "rationale":
			if value != "" {
				parsed.Reason = value
			}
		case "metricvalue", "metric", "score", "result":
			if metric, ok := parseRoundMetricValue(value); ok {
				parsed.MetricValue = metric
				parsed.HasMetric = true
			}
		case "config", "configuration", "hyperparameters":
			parsed.Config = value
		case "verificationcommand", "verifycommand", "command", "evalcommand", "evaluationcommand":
			parsed.VerificationCommand = value
		case "verificationresult", "verifyresult", "resultsummary", "evaluationresult", "evalresult":
			parsed.VerificationResult = value
		}
	}
	return parsed
}

func roundResultBlock(output string) (string, bool) {
	lower := strings.ToLower(output)
	startTag := "[round_result]"
	endTag := "[/round_result]"
	startIdx := strings.Index(lower, startTag)
	endIdx := strings.Index(lower, endTag)
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		return "", false
	}
	return output[startIdx+len(startTag) : endIdx], true
}

func splitRoundResultLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", false
	}
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	key := strings.TrimSpace(parts[0])
	value := strings.TrimSpace(parts[1])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

func normalizeRoundResultKey(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

func parseRoundMetricValue(value string) (float64, bool) {
	match := roundMetricValuePattern.FindString(strings.TrimSpace(value))
	if match == "" {
		return 0, false
	}
	match = strings.TrimSuffix(match, "%")
	metric, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, false
	}
	return metric, true
}

func experimentVerificationResultLooksFailed(result string) bool {
	result = strings.TrimSpace(result)
	if result == "" {
		return false
	}
	for _, match := range roundVerificationExitCodeRegex.FindAllStringSubmatch(result, -1) {
		if len(match) < 2 {
			continue
		}
		code, err := strconv.Atoi(match[1])
		if err == nil && code != 0 {
			return true
		}
	}
	return remoteCodingToolResultLooksFailed(result)
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
