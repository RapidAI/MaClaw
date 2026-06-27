package main

import (
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestNewRemoteExperimentOrchestratorInitializesKnownBaselineAsBest(t *testing.T) {
	o := NewRemoteExperimentOrchestrator(nil, corelib.MaclawLLMConfig{}, nil, "session", "/repo", ExperimentOrchestratorParams{
		BaselineMetricName:  "Accuracy",
		BaselineMetricValue: 88.5,
		MetricHigherBetter:  true,
	}, nil)

	state := o.GetState()
	if state.BestMetric != 88.5 || state.BaselineRepro != 88.5 {
		t.Fatalf("known baseline should seed best and baseline reproduction, got best=%v baseline=%v", state.BestMetric, state.BaselineRepro)
	}
	if state.Params.FailureTolerance != 3 {
		t.Fatalf("failure tolerance should default to 3, got %d", state.Params.FailureTolerance)
	}
}

func TestParseRoundOutputAcceptsCommonModelFormattingVariants(t *testing.T) {
	output := `
The run completed.

[ROUND_RESULT]
Modification: added dropout to the classifier head
Reason: previous rounds overfit after epoch 3
Metric Value: 92.35% on validation set
Configuration: dropout=0.2, lr=3e-4
Verification Command: python train.py --eval-only
Verification Result: exit=0, val accuracy 92.35%
[/ROUND_RESULT]
`

	got := parseRoundOutput(output)
	if got.Modification != "added dropout to the classifier head" {
		t.Fatalf("modification = %q", got.Modification)
	}
	if got.Reason != "previous rounds overfit after epoch 3" {
		t.Fatalf("reason = %q", got.Reason)
	}
	if !got.HasMetric || got.MetricValue != 92.35 {
		t.Fatalf("metric parse failed: %#v", got)
	}
	if got.Config != "dropout=0.2, lr=3e-4" {
		t.Fatalf("config = %q", got.Config)
	}
	if got.VerificationCommand != "python train.py --eval-only" {
		t.Fatalf("verification command = %q", got.VerificationCommand)
	}
	if got.VerificationResult != "exit=0, val accuracy 92.35%" {
		t.Fatalf("verification result = %q", got.VerificationResult)
	}
}

func TestParseRoundOutputRejectsMissingMetric(t *testing.T) {
	got := parseRoundOutput(`
[round_result]
modification: changed augmentation
reason: improve robustness
config: aug=strong
[/round_result]
`)
	if got.HasMetric {
		t.Fatalf("missing metric should not be marked as parsed: %#v", got)
	}
	if got.Modification != "changed augmentation" {
		t.Fatalf("should still preserve parsed modification for diagnostics, got %#v", got)
	}
}

func TestRemoteExperimentOrchestratorPopulateCompletedRoundResultRequiresMetric(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
			},
			BaselineRepro: 88,
			BestMetric:    88,
		},
	}

	var result ExperimentRoundResult
	o.populateCompletedRoundResult(&result, `
[ROUND_RESULT]
modification: changed optimizer
reason: lower variance
config: adamw
[/ROUND_RESULT]
`)

	if result.Status != "failed" || result.Error == "" {
		t.Fatalf("missing metric should fail round result instead of completing with zero metric: %#v", result)
	}
	if result.Modification != "changed optimizer" {
		t.Fatalf("failed result should preserve parsed diagnostics, got %#v", result)
	}
}

func TestRemoteExperimentOrchestratorPopulateCompletedRoundResultRequiresVerificationEvidence(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
			},
			BaselineRepro: 88,
		},
	}

	var result ExperimentRoundResult
	o.populateCompletedRoundResult(&result, `
[ROUND_RESULT]
modification: changed optimizer
reason: lower variance
metric_value: 91.5
config: adamw
[/ROUND_RESULT]
`)

	if result.Status != "failed" || !strings.Contains(result.Error, "验证证据") {
		t.Fatalf("missing verification evidence should fail round result, got %#v", result)
	}
	if result.MetricValue != 91.5 || result.Modification != "changed optimizer" {
		t.Fatalf("failed result should preserve parsed diagnostics, got %#v", result)
	}
	if result.DeltaFromPaper != 0 || result.DeltaFromBase != 0 {
		t.Fatalf("missing verification evidence should not calculate deltas, got %#v", result)
	}
}

func TestRemoteExperimentOrchestratorPopulateCompletedRoundResultCalculatesDeltas(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
			},
			BaselineRepro: 88,
		},
	}

	var result ExperimentRoundResult
	o.populateCompletedRoundResult(&result, `
[ROUND_RESULT]
modification: changed optimizer
reason: lower variance
metric_value: 91.5
config: adamw
verification_command: pytest tests/eval_test.py
verification_result: exit=0 metric=91.5
[/ROUND_RESULT]
`)

	if result.Status != "completed" || result.MetricValue != 91.5 || result.DeltaFromBase != 3.5 || result.DeltaFromPaper != 1.5 {
		t.Fatalf("completed round should parse metric and calculate deltas, got %#v", result)
	}
	if result.VerificationCommand != "pytest tests/eval_test.py" || result.VerificationResult != "exit=0 metric=91.5" {
		t.Fatalf("completed round should preserve verification evidence, got %#v", result)
	}
}

func TestRemoteExperimentOrchestratorPopulateCompletedRoundResultFailsFailedVerification(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
			},
			BaselineRepro: 88,
		},
	}

	var result ExperimentRoundResult
	o.populateCompletedRoundResult(&result, `
[ROUND_RESULT]
modification: changed optimizer
reason: lower variance
metric_value: 91.5
config: adamw
verification_command: pytest tests/eval_test.py
verification_result: exit=1 metric=91.5 but assertions failed
[/ROUND_RESULT]
`)

	if result.Status != "failed" || !strings.Contains(result.Error, "验证结果失败") {
		t.Fatalf("failed verification should fail round despite metric, got %#v", result)
	}
	if result.MetricValue != 91.5 || result.VerificationCommand != "pytest tests/eval_test.py" || !strings.Contains(result.VerificationResult, "exit=1") {
		t.Fatalf("failed verification round should preserve diagnostics, got %#v", result)
	}
	if result.DeltaFromPaper != 0 || result.DeltaFromBase != 0 {
		t.Fatalf("failed verification should not calculate deltas, got %#v", result)
	}
}

func TestExperimentVerificationResultLooksFailed(t *testing.T) {
	failures := []string{
		"exit=1",
		"exit code 2",
		"status: -1",
		"Traceback (most recent call last):",
		"ERROR: evaluation failed",
	}
	for _, result := range failures {
		if !experimentVerificationResultLooksFailed(result) {
			t.Fatalf("verification result %q should look failed", result)
		}
	}
	successes := []string{
		"",
		"exit=0 accuracy=91.5",
		"status: 0, 0 errors and 0 warnings",
	}
	for _, result := range successes {
		if experimentVerificationResultLooksFailed(result) {
			t.Fatalf("verification result %q should not look failed", result)
		}
	}
}

func TestRemoteExperimentOrchestratorUnknownBaselineFirstCompletedRoundBecomesBest(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName: "Loss",
				MetricHigherBetter: false,
			},
			Rounds: []ExperimentRoundResult{
				{Status: "completed", MetricValue: 1.25},
			},
		},
	}

	if !o.completedRoundImprovesBestLocked(o.state.Rounds[0]) {
		t.Fatalf("first completed lower-better round should become best when no baseline is known")
	}
	o.state.BestMetric = 1.25
	o.state.BestRound = 0
	o.state.Rounds = append(o.state.Rounds, ExperimentRoundResult{Status: "completed", MetricValue: 1.4})
	if o.completedRoundImprovesBestLocked(o.state.Rounds[1]) {
		t.Fatalf("worse lower-better metric should not replace established first best")
	}
}

func TestRemoteExperimentOrchestratorApplyRoundResultTracksConsecutiveFailures(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
				FailureTolerance:    2,
			},
			BestMetric:    90,
			BaselineRepro: 90,
		},
	}

	o.state.Rounds = append(o.state.Rounds, ExperimentRoundResult{RoundNumber: 1, Status: "failed", Error: "training crashed"})
	if improved := o.applyRoundResultLocked(o.state.Rounds[0]); improved {
		t.Fatalf("failed round should not improve best")
	}
	if o.state.consecutiveFailures != 1 || o.state.consecutiveNoImprovement != 1 {
		t.Fatalf("first failure should increment counters, got failures=%d noImprovement=%d", o.state.consecutiveFailures, o.state.consecutiveNoImprovement)
	}

	o.state.Rounds = append(o.state.Rounds, ExperimentRoundResult{RoundNumber: 2, Status: "failed", Error: "metric missing"})
	o.applyRoundResultLocked(o.state.Rounds[1])
	if o.state.consecutiveFailures != 2 {
		t.Fatalf("second consecutive failure should reach tolerance, got %d", o.state.consecutiveFailures)
	}
	if summary := o.recentFailureSummary(2); summary != "第2轮: metric missing; 第1轮: training crashed" {
		t.Fatalf("recent failure summary should include latest failures first, got %q", summary)
	}

	o.state.Rounds = append(o.state.Rounds, ExperimentRoundResult{RoundNumber: 3, Status: "completed", MetricValue: 91})
	if improved := o.applyRoundResultLocked(o.state.Rounds[2]); !improved {
		t.Fatalf("better completed round should improve best")
	}
	if o.state.consecutiveFailures != 0 || o.state.consecutiveNoImprovement != 0 {
		t.Fatalf("successful improvement should reset counters, got failures=%d noImprovement=%d", o.state.consecutiveFailures, o.state.consecutiveNoImprovement)
	}
}

func TestRemoteExperimentOrchestratorProgressSummaryIncludesBestAndFailureDiagnostics(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				MetricHigherBetter:  true,
			},
			StartedAt:     time.Now().Add(-2 * time.Hour),
			BestRound:     1,
			BestMetric:    92.4,
			BaselineRepro: 89.5,
			Rounds: []ExperimentRoundResult{
				{RoundNumber: 1, Status: "failed", Error: "training crashed"},
				{RoundNumber: 2, Status: "completed", Modification: "added dropout", Reason: "reduce overfit", MetricValue: 92.4, DeltaFromPaper: 2.4, Config: "dropout=0.2", VerificationCommand: "pytest eval.py", VerificationResult: "exit=0 accuracy=92.4"},
				{RoundNumber: 3, Status: "failed", Error: "metric missing"},
			},
		},
	}

	summary := o.buildProgressSummary()
	for _, want := range []string{
		"最佳轮次详情: 第2轮 Accuracy=92.4000",
		"最佳修改: added dropout",
		"最佳配置: dropout=0.2",
		"最佳验证命令: pytest eval.py",
		"最佳验证结果: exit=0 accuracy=92.4",
		"最近失败: 第3轮: metric missing; 第1轮: training crashed",
		"- 第1轮: failed error=training crashed",
		"- 第2轮: completed metric=92.4000",
		"verify=pytest eval.py",
		"- 第3轮: failed error=metric missing",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestRemoteExperimentOrchestratorRoundContextIncludesFailureDiagnostics(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		projectDir: "/repo/project",
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
			},
			BestRound:     1,
			BestMetric:    92.4,
			BaselineRepro: 89.5,
			Rounds: []ExperimentRoundResult{
				{RoundNumber: 1, Status: "failed", Modification: "changed optimizer", Reason: "lower variance", MetricValue: 91.5, Error: "SubAgent 报告的验证结果失败: exit=1 assertions failed", VerificationCommand: "pytest eval.py", VerificationResult: "exit=1 assertions failed"},
				{RoundNumber: 2, Status: "completed", Modification: "added dropout", MetricValue: 92.4, DeltaFromPaper: 2.4, VerificationCommand: "pytest eval.py", VerificationResult: "exit=0 accuracy=92.4"},
			},
		},
	}

	context := o.buildRoundContext()
	for _, want := range []string{
		"项目目录: /repo/project",
		"## 历史实验记录（最近 5 轮）",
		"- 第1轮: failed error=SubAgent 报告的验证结果失败: exit=1 assertions failed",
		"- 第2轮: completed metric=92.4000",
		"verify=pytest eval.py",
	} {
		if !strings.Contains(context, want) {
			t.Fatalf("round context missing %q:\n%s", want, context)
		}
	}
	if strings.Contains(context, "changed optimizer → 91.5000") {
		t.Fatalf("failed round context should prefer error diagnostics over old metric-arrow format:\n%s", context)
	}
}

func TestRemoteExperimentOrchestratorRoundTaskDescriptionFormatsTargetAndVerificationEvidence(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				TargetExceedance:    0.05,
				MetricHigherBetter:  true,
			},
			BestMetric: 91.2,
		},
	}

	task := o.buildRoundTaskDescription(3)
	for _, want := range []string{
		"# 实验改进第 3 轮",
		"目标超出: +5.00%",
		"metric_value: <评估得到的 Accuracy 数值>",
		"verification_command: <实际运行的训练/评估命令>",
		"verification_result: <命令退出状态和关键输出摘要>",
	} {
		if !strings.Contains(task, want) {
			t.Fatalf("round task description missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, "目标超出: +0.05%") {
		t.Fatalf("target exceedance should be formatted as percent points, got:\n%s", task)
	}
}

func TestExperimentTargetDeltaThresholdUsesRelativePaperMetric(t *testing.T) {
	got := experimentTargetDeltaThreshold(ExperimentOrchestratorParams{
		BaselineMetricValue: 90,
		TargetExceedance:    0.05,
	})
	if got != 4.5 {
		t.Fatalf("5%% target over paper metric 90 should require 4.5 delta points, got %.4f", got)
	}
	got = experimentTargetDeltaThreshold(ExperimentOrchestratorParams{
		BaselineMetricValue: 0,
		TargetExceedance:    0.05,
	})
	if got != 0.05 {
		t.Fatalf("unknown paper metric should fall back to raw target delta, got %.4f", got)
	}
}

func TestRemoteExperimentOrchestratorCodingExperienceForRoundClassifiesOutcomes(t *testing.T) {
	o := &RemoteExperimentOrchestrator{
		state: ExperimentOrchestratorState{
			Params: ExperimentOrchestratorParams{
				BaselineMetricName:  "Accuracy",
				BaselineMetricValue: 90,
				TargetExceedance:    0.05,
				MetricHigherBetter:  true,
			},
		},
	}

	exp, ok := o.codingExperienceForRound(ExperimentRoundResult{
		RoundNumber:         1,
		Status:              "completed",
		Modification:        "added dropout",
		Reason:              "reduce overfit",
		MetricValue:         91,
		DeltaFromPaper:      1,
		Config:              "dropout=0.2",
		VerificationCommand: "pytest eval.py",
		VerificationResult:  "exit=0 accuracy=91",
	})
	if !ok || exp.Status != knowledge.CodingStatusCandidate || exp.Category != "optimization" || exp.Confidence >= 0.85 {
		t.Fatalf("non-target completed round should be candidate optimization knowledge, got ok=%v exp=%#v", ok, exp)
	}
	if exp.SuccessCount != 1 || !experimentTestContainsString(exp.Labels, "completed") || experimentTestContainsString(exp.Labels, "target_reached") {
		t.Fatalf("completed candidate should record success labels without target_reached, got %#v", exp)
	}
	if !strings.Contains(exp.Content, "验证命令: pytest eval.py") || !strings.Contains(exp.Content, "验证结果: exit=0 accuracy=91") {
		t.Fatalf("completed experience should include verification evidence, got %q", exp.Content)
	}
	if !strings.Contains(exp.Content, "目标阈值=4.5000") {
		t.Fatalf("completed experience should record relative target as metric delta, got %q", exp.Content)
	}

	exp, ok = o.codingExperienceForRound(ExperimentRoundResult{
		RoundNumber:    2,
		Status:         "completed",
		Modification:   "label smoothing",
		Reason:         "better calibration",
		MetricValue:    95,
		DeltaFromPaper: 5,
		Config:         "smoothing=0.1",
	})
	if !ok || exp.Status != knowledge.CodingStatusActive || exp.Confidence < 0.85 || !experimentTestContainsString(exp.Labels, "target_reached") {
		t.Fatalf("target-reaching completed round should be active knowledge, got ok=%v exp=%#v", ok, exp)
	}

	exp, ok = o.codingExperienceForRound(ExperimentRoundResult{
		RoundNumber:  3,
		Status:       "failed",
		Modification: "switched optimizer",
		Reason:       "try faster convergence",
		Error:        "training crashed",
		Config:       "adamw",
	})
	if !ok || exp.Status != knowledge.CodingStatusCandidate || exp.Category != "pitfall" || exp.FailureCount != 1 {
		t.Fatalf("failed round should be candidate pitfall knowledge, got ok=%v exp=%#v", ok, exp)
	}
	if len(exp.FailedAttempts) != 1 || exp.FailedAttempts[0] != "training crashed" || len(exp.Contraindications) == 0 {
		t.Fatalf("failed round should preserve failed attempt and contraindication, got %#v", exp)
	}
}

func experimentTestContainsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
