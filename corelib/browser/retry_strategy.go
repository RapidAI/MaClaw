package browser

import (
	"fmt"
	"strings"
	"time"
)

// RetryStrategy decides how to handle step failures.
type RetryStrategy struct {
	MaxStepRetries int // per-step max retries (default 3)
	MaxTaskRetries int // whole-task max retries (default 3)
	ocr            OCRProvider
}

// NewRetryStrategy creates a retry strategy. ocr may be nil.
func NewRetryStrategy(maxStepRetries, maxTaskRetries int, ocr OCRProvider) *RetryStrategy {
	if maxStepRetries <= 0 {
		maxStepRetries = 3
	}
	if maxTaskRetries <= 0 {
		maxTaskRetries = 3
	}
	return &RetryStrategy{
		MaxStepRetries: maxStepRetries,
		MaxTaskRetries: maxTaskRetries,
		ocr:            ocr,
	}
}

// Decide returns a retry decision based on failure type and retry count.
func (r *RetryStrategy) Decide(failure FailureType, step StepSpec,
	stepRetryCount int, pageState *PageSnapshot) *RetryDecision {

	if stepRetryCount >= r.MaxStepRetries {
		return &RetryDecision{
			ShouldRetry: false,
			Reason:      fmt.Sprintf("exceeded max step retries (%d)", r.MaxStepRetries),
		}
	}

	switch failure {
	case FailureElementNotFound:
		return r.decideElementNotFound(step, stepRetryCount, pageState)
	case FailureTimeout:
		return r.decideTimeout(step, stepRetryCount)
	case FailurePageChanged:
		return r.decidePageChanged(step, stepRetryCount, pageState)
	case FailureUnknownState:
		return r.decideUnknown(step, stepRetryCount, pageState)
	case FailureVerificationFailed:
		return r.decideVerificationFailed(step, stepRetryCount, pageState)
	default:
		return &RetryDecision{ShouldRetry: false, Reason: "unhandled failure type"}
	}
}

// ClassifyFailure infers a FailureType from an error message and step context.
func (r *RetryStrategy) ClassifyFailure(err error, step StepSpec) FailureType {
	if err == nil {
		return FailureVerificationFailed
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not found") || strings.Contains(msg, "no node"):
		return FailureElementNotFound
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out"):
		return FailureTimeout
	case strings.Contains(msg, "navigated") || strings.Contains(msg, "url changed"):
		return FailurePageChanged
	default:
		return FailureUnknownState
	}
}

func (r *RetryStrategy) decideElementNotFound(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	switch count {
	case 0:
		return &RetryDecision{
			ShouldRetry: true,
			WaitBefore:  5 * time.Second,
			Reason:      "element not found, waiting 5s before retry",
		}
	case 1:
		return &RetryDecision{
			ShouldRetry: true,
			WaitBefore:  10 * time.Second,
			Reason:      "element not found, waiting 10s before retry",
		}
	default:
		return &RetryDecision{
			ShouldRetry: true,
			NeedsLLM:    true,
			WaitBefore:  2 * time.Second,
			Reason:      "element not found after multiple retries, asking LLM",
			LLMContext:  r.buildLLMContext("element_not_found", step, ps),
		}
	}
}

func (r *RetryStrategy) decideTimeout(step StepSpec, count int) *RetryDecision {
	switch count {
	case 0:
		adjusted := step
		if adjusted.Timeout > 0 {
			adjusted.Timeout *= 2
		} else {
			adjusted.Timeout = 60 * time.Second
		}
		return &RetryDecision{
			ShouldRetry:  true,
			AdjustedStep: &adjusted,
			Reason:       "timeout, doubling step timeout",
		}
	case 1:
		adjusted := step
		if adjusted.Timeout > 0 {
			adjusted.Timeout *= 3
		} else {
			adjusted.Timeout = 90 * time.Second
		}
		return &RetryDecision{
			ShouldRetry:  true,
			AdjustedStep: &adjusted,
			Reason:       "timeout, tripling step timeout",
		}
	default:
		return &RetryDecision{ShouldRetry: false, Reason: "timeout after multiple retries"}
	}
}

func (r *RetryStrategy) decidePageChanged(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	if count == 0 {
		return &RetryDecision{
			ShouldRetry: true,
			NeedsLLM:    true,
			WaitBefore:  2 * time.Second,
			Reason:      "page changed unexpectedly, asking LLM to re-plan",
			LLMContext:  r.buildLLMContext("page_changed", step, ps),
		}
	}
	return &RetryDecision{
		ShouldRetry: true,
		NeedsLLM:    true,
		WaitBefore:  2 * time.Second,
		Reason:      "page changed again, LLM re-plan",
		LLMContext:  r.buildLLMContext("page_changed", step, ps),
	}
}

func (r *RetryStrategy) decideUnknown(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	return &RetryDecision{
		ShouldRetry: true,
		NeedsLLM:    true,
		WaitBefore:  2 * time.Second,
		Reason:      "unknown state, asking LLM",
		LLMContext:  r.buildLLMContext("unknown_state", step, ps),
	}
}

func (r *RetryStrategy) decideVerificationFailed(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	if count == 0 {
		return &RetryDecision{
			ShouldRetry: true,
			WaitBefore:  3 * time.Second,
			Reason:      "verification failed, retrying after short wait",
		}
	}
	return &RetryDecision{
		ShouldRetry: true,
		NeedsLLM:    true,
		WaitBefore:  2 * time.Second,
		Reason:      "verification still failing, asking LLM",
		LLMContext:  r.buildLLMContext("verification_failed", step, ps),
	}
}

func (r *RetryStrategy) buildLLMContext(failureKind string, step StepSpec, ps *PageSnapshot) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("浏览器任务步骤失败 (类型: %s)\n", failureKind))
	b.WriteString(fmt.Sprintf("操作: %s, 参数: %v\n", step.Action, step.Params))
	if ps != nil {
		b.WriteString(fmt.Sprintf("当前 URL: %s\n", ps.URL))
		b.WriteString(fmt.Sprintf("页面标题: %s\n", ps.Title))
		if len(ps.OCRText) > 0 {
			b.WriteString("页面 OCR 文本:\n")
			for _, r := range ps.OCRText {
				b.WriteString(fmt.Sprintf("  [%d,%d,%d,%d] %q (%.2f)\n",
					r.BBox[0], r.BBox[1], r.BBox[2], r.BBox[3], r.Text, r.Confidence))
			}
		}
		if ps.DOMSnippet != "" {
			b.WriteString(fmt.Sprintf("DOM 片段: %s\n", ps.DOMSnippet))
		}
	}
	b.WriteString("请根据以上信息决定下一步操作。")
	return b.String()
}
