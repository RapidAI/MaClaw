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
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// ExperimentOrchestratorParams are the user-configured parameters for the
// iterative improvement loop.
type ExperimentOrchestratorParams struct {
	TargetExceedance float64 // notify when exceeding paper by this % (default 0.1)
	MaxRuntime       time.Duration // total runtime cap (default 48h)
	MaxRounds        int     // maximum improvement rounds (default 50)
	PlateauTolerance int     // consecutive rounds without improvement before asking user (default 8)

	// Baseline from paper_analysis phase output
	BaselineMetricName  string  // e.g. "Accuracy", "F1", "BLEU"
	BaselineMetricValue float64 // paper's best reported value
	MetricHigherBetter  bool    // true = higher is better (Accuracy), false = lower is better (loss)
}

// ExperimentRoundResult records the outcome of a single experiment round.
type ExperimentRoundResult struct {
	RoundNumber   int       `json:"round"`
	StartedAt     time.Time `json:"started_at"`
	CompletedAt   time.Time `json:"completed_at"`
	Modification  string    `json:"modification"` // what was changed
	Reason        string    `json:"reason"`       // why this direction
	MetricValue   float64   `json:"metric_value"`
	DeltaFromBase float64   `json:"delta_from_baseline"` // positive = improvement
	DeltaFromPaper float64  `json:"delta_from_paper"`    // positive = exceeds paper
	Config        string    `json:"config_summary"`
	Status        string    `json:"status"` // "completed", "failed", "timeout"
	Error         string    `json:"error,omitempty"`
}

// ExperimentOrchestratorState is the full state of the experiment loop.
type ExperimentOrchestratorState struct {
	mu sync.Mutex

	Params       ExperimentOrchestratorParams
	Rounds       []ExperimentRoundResult
	BestRound    int     // index of best round
	BestMetric   float64 // best metric value achieved
	BaselineRepro float64 // baseline reproduction value (from phase 4)
	StartedAt    time.Time
	Status       string // "running", "paused", "completed", "stopped"

	// Runtime
	consecutiveNoImprovement int
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

	subAgent := NewRemoteCodingSubAgent(handler, cfg, httpClient, sessionID, projectDir, projectDir, loopCtx)

	return &RemoteExperimentOrchestrator{
		handler:    handler,
		cfg:        cfg,
		httpClient: httpClient,
		sessionID:  sessionID,
		projectDir: projectDir,
		subAgent:   subAgent,
		state: ExperimentOrchestratorState{
			Params:    params,
			Status:    "running",
			StartedAt: time.Now(),
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

		// Update best
		improved := false
		if result.Status == "completed" {
			if params.MetricHigherBetter {
				if result.MetricValue > o.state.BestMetric {
					o.state.BestMetric = result.MetricValue
					o.state.BestRound = len(o.state.Rounds) - 1
					improved = true
				}
			} else {
				if result.MetricValue < o.state.BestMetric {
					o.state.BestMetric = result.MetricValue
					o.state.BestRound = len(o.state.Rounds) - 1
					improved = true
				}
			}
		}

		if improved {
			o.state.consecutiveNoImprovement = 0
		} else {
			o.state.consecutiveNoImprovement++
		}
		o.state.mu.Unlock()

		// Check target reached
		deltaFromPaper := result.DeltaFromPaper
		if result.Status == "completed" && deltaFromPaper >= params.TargetExceedance {
			msg := fmt.Sprintf("🎉 目标达成！当前最佳 %.2f%%（论文 %.2f%%，超出 +%.2f%%）\n第 %d 轮，关键改进：%s\n回复\"继续\"继续冲击更高 / \"停止\"生成报告",
				o.state.BestMetric, params.BaselineMetricValue, deltaFromPaper, round, result.Modification)
			o.notify(msg)
			return StopReasonTargetReached, o.buildProgressSummary()
		}

		// Check plateau
		if o.state.consecutiveNoImprovement >= params.PlateauTolerance {
			directions := o.recentDirections(3)
			msg := fmt.Sprintf("📊 遇到平台期（连续 %d 轮无进展）。当前 %.2f%%，已尝试：%s\n回复新方向（如\"试试对比学习\"）/ \"继续\" / \"停止\"",
				o.state.consecutiveNoImprovement, o.state.BestMetric, directions)
			o.notify(msg)
			return StopReasonPlateau, o.buildProgressSummary()
		}

		// Save experience for this round
		o.saveRoundExperience(result)
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

	// Parse the SubAgent's output to extract metric value and modification info
	result.Status = "completed"
	result.Modification, result.Reason, result.MetricValue, result.Config = parseRoundOutput(subResult.Summary)

	// If metric parsing failed (value=0), mark as failed — don't pollute delta calculations
	if result.MetricValue == 0 && result.Modification == "未知修改" {
		result.Status = "failed"
		result.Error = "无法从 SubAgent 输出中解析实验结果（缺少 [ROUND_RESULT] 块）"
		return result
	}

	// Calculate deltas (positive = improvement for both directions)
	if o.state.Params.MetricHigherBetter {
		result.DeltaFromBase = result.MetricValue - o.state.BaselineRepro
		result.DeltaFromPaper = result.MetricValue - o.state.Params.BaselineMetricValue
	} else {
		result.DeltaFromBase = o.state.BaselineRepro - result.MetricValue
		result.DeltaFromPaper = o.state.Params.BaselineMetricValue - result.MetricValue
	}

	return result
}

func (o *RemoteExperimentOrchestrator) buildRoundTaskDescription(roundNum int) string {
	params := o.state.Params
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# 实验改进第 %d 轮\n\n", roundNum))
	sb.WriteString(fmt.Sprintf("目标：提升 %s 指标（当前最佳: %.4f，论文: %.4f，目标超出: +%.2f%%）\n\n",
		params.BaselineMetricName, o.state.BestMetric, params.BaselineMetricValue, params.TargetExceedance))

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
		start := len(o.state.Rounds) - 5
		if start < 0 {
			start = 0
		}
		for _, r := range o.state.Rounds[start:] {
			status := "✅"
			if r.Status == "failed" {
				status = "❌"
			}
			sb.WriteString(fmt.Sprintf("%s 第%d轮: %s → %.4f (Δpaper=%.4f) [%s]\n",
				status, r.RoundNumber, r.Modification, r.MetricValue, r.DeltaFromPaper, r.Reason))
		}
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

func (o *RemoteExperimentOrchestrator) saveRoundExperience(result ExperimentRoundResult) {
	if o.subAgent == nil || result.Status != "completed" {
		return
	}
	// Save successful experiment as knowledge
	exp := knowledge.CodingExperience{
		Title:            fmt.Sprintf("实验改进: %s", result.Modification),
		Content:          fmt.Sprintf("方向: %s\n结果: %s=%.4f (Δpaper=%.4f)\n配置: %s", result.Reason, o.state.Params.BaselineMetricName, result.MetricValue, result.DeltaFromPaper, result.Config),
		TriggerCondition: fmt.Sprintf("优化 %s 指标", o.state.Params.BaselineMetricName),
		Scope:            "experiment",
		Category:         "optimization",
		Language:         "python",
		Status:           knowledge.CodingStatusCandidate,
		Confidence:       0.6,
	}
	if result.DeltaFromPaper > 0 {
		exp.Status = knowledge.CodingStatusActive
		exp.Confidence = 0.8
	}
	_ = o.subAgent.SaveExperience(exp)
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

// parseRoundOutput extracts structured result from SubAgent's natural language output.
func parseRoundOutput(output string) (modification, reason string, metricValue float64, config string) {
	// Look for [ROUND_RESULT] block
	startIdx := strings.Index(output, "[ROUND_RESULT]")
	endIdx := strings.Index(output, "[/ROUND_RESULT]")
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		// Fallback: try to extract from unstructured text
		return "未知修改", "自主决策", 0, ""
	}

	block := output[startIdx+len("[ROUND_RESULT]") : endIdx]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "modification:") {
			modification = strings.TrimSpace(strings.TrimPrefix(line, "modification:"))
		} else if strings.HasPrefix(line, "reason:") {
			reason = strings.TrimSpace(strings.TrimPrefix(line, "reason:"))
		} else if strings.HasPrefix(line, "metric_value:") {
			valStr := strings.TrimSpace(strings.TrimPrefix(line, "metric_value:"))
			fmt.Sscanf(valStr, "%f", &metricValue)
		} else if strings.HasPrefix(line, "config:") {
			config = strings.TrimSpace(strings.TrimPrefix(line, "config:"))
		}
	}
	return
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
