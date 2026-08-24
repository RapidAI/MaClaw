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
	if isPolicyDenied(err) {
		return FailureNetworkBlocked
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "browser policy blocked"):
		return FailureNetworkBlocked
	case strings.Contains(msg, "step verification failed") || strings.Contains(msg, "success criteria not met") || strings.Contains(msg, "did not change the page"):
		return FailureVerificationFailed
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
			WaitBefore:  300 * time.Millisecond,
			Reason:      "element not found, short settle before retry",
		}
	case 1:
		return &RetryDecision{
			ShouldRetry: true,
			WaitBefore:  700 * time.Millisecond,
			Reason:      "element not found, one more short retry",
		}
	default:
		return &RetryDecision{ShouldRetry: false, Reason: "element not found after short retries; observe again for fresh refs"}
	}
}

func (r *RetryStrategy) decideTimeout(step StepSpec, count int) *RetryDecision {
	switch count {
	case 0:
		adjusted := step
		if adjusted.Timeout > 0 {
			adjusted.Timeout *= 2
		} else {
			adjusted.Timeout = 10 * time.Second
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
			adjusted.Timeout = 20 * time.Second
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
			WaitBefore:  500 * time.Millisecond,
			Reason:      "page changed, short settle before retry",
		}
	}
	return &RetryDecision{ShouldRetry: false, Reason: "page changed again; observe current page before continuing"}
}

func (r *RetryStrategy) decideUnknown(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	if count > 0 {
		return &RetryDecision{ShouldRetry: false, Reason: "unknown browser state after retry; observe current page before continuing"}
	}
	return &RetryDecision{
		ShouldRetry: true,
		WaitBefore:  500 * time.Millisecond,
		Reason:      "unknown state, short retry after settle",
	}
}

func (r *RetryStrategy) decideVerificationFailed(step StepSpec, count int, ps *PageSnapshot) *RetryDecision {
	if count == 0 {
		return &RetryDecision{
			ShouldRetry: true,
			WaitBefore:  700 * time.Millisecond,
			Reason:      "verification failed, retrying after short wait",
		}
	}
	return &RetryDecision{ShouldRetry: false, Reason: "verification still failing after short retry"}
}

func (r *RetryStrategy) buildLLMContext(failureKind string, step StepSpec, ps *PageSnapshot) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("browser task step failed (type: %s)\n", failureKind))
	b.WriteString(fmt.Sprintf("action: %s, params: %v\n", step.Action, step.Params))
	if ps != nil {
		b.WriteString(fmt.Sprintf("current URL: %s\n", ps.URL))
		b.WriteString(fmt.Sprintf("page title: %s\n", ps.Title))
		if len(ps.OCRText) > 0 {
			b.WriteString("page OCR text:\n")
			for _, r := range ps.OCRText {
				b.WriteString(fmt.Sprintf("  [%d,%d,%d,%d] %q (%.2f)\n",
					r.BBox[0], r.BBox[1], r.BBox[2], r.BBox[3], r.Text, r.Confidence))
			}
		}
		if ps.DOMSnippet != "" {
			b.WriteString(fmt.Sprintf("DOM snippet: %s\n", ps.DOMSnippet))
		}
	}
	b.WriteString("Decide the next browser action from this state.")
	return b.String()
}
