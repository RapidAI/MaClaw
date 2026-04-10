package security

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// SmartApproval uses a lightweight LLM to determine whether a flagged tool
// invocation is actually safe (false positive from regex-based detection).
// This avoids unnecessary user confirmations for benign operations like
// "rm -rf node_modules/" or "rm -rf dist/".
//
// It also maintains a session-scoped cache: once a tool+pattern combination
// is deemed safe, subsequent identical invocations skip the LLM call.
type SmartApproval struct {
	llm     LLMSecurityCaller
	timeout time.Duration

	mu             sync.RWMutex
	sessionAllowed map[string]bool // "tool:pattern" → true
}

// SmartApprovalVerdict is the result of a smart approval check.
type SmartApprovalVerdict string

const (
	SmartVerdictSafe    SmartApprovalVerdict = "safe"
	SmartVerdictUnsafe  SmartApprovalVerdict = "unsafe"
	SmartVerdictUnknown SmartApprovalVerdict = "unknown"
)

// SmartApprovalResult contains the verdict and explanation.
type SmartApprovalResult struct {
	Verdict     SmartApprovalVerdict
	Explanation string
	Elapsed     time.Duration
}

// NewSmartApproval creates a SmartApproval instance.
// If timeout is 0, defaults to 5 seconds.
func NewSmartApproval(llm LLMSecurityCaller, timeout time.Duration) *SmartApproval {
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return &SmartApproval{
		llm:            llm,
		timeout:        timeout,
		sessionAllowed: make(map[string]bool),
	}
}

// IsConfigured reports whether the LLM backend is available for smart approval.
func (sa *SmartApproval) IsConfigured() bool {
	return sa.llm != nil && sa.llm.IsConfigured()
}

// ResetSession clears the session-scoped approval cache.
func (sa *SmartApproval) ResetSession() {
	sa.mu.Lock()
	sa.sessionAllowed = make(map[string]bool)
	sa.mu.Unlock()
}

const smartApprovalSystemPrompt = `You are a security reviewer for an AI coding assistant.
A tool invocation matched a dangerous command pattern, but it might be a false positive.

Common false positives (SAFE):
- "rm -rf node_modules/" or "rm -rf dist/" or "rm -rf .cache/" (cleaning build artifacts)
- "rm -rf __pycache__/" or "rm -rf .pytest_cache/" (cleaning Python caches)
- "DROP TABLE IF EXISTS temp_*" (expected migration cleanup)
- "kill -9 <specific-pid>" (killing a known stuck process)
- "sudo apt install <package>" (installing packages, not system destruction)

Genuinely dangerous (UNSAFE):
- "rm -rf /" or "rm -rf ~/" or "rm -rf /*" (system/home destruction)
- "DROP TABLE users" without IF EXISTS (production data loss)
- "curl ... | sh" (remote code execution)
- "chmod 777 /" (security degradation)

Respond with ONLY one word: SAFE or UNSAFE`

// Evaluate asks the LLM whether the flagged operation is a false positive.
// Returns SmartVerdictSafe if the LLM determines the operation is benign,
// SmartVerdictUnsafe if genuinely dangerous, SmartVerdictUnknown on error/timeout.
//
// Results are cached per session: identical tool+pattern combos skip the LLM.
func (sa *SmartApproval) Evaluate(toolName string, args map[string]interface{}, assessment RiskAssessment) SmartApprovalResult {
	start := time.Now()

	// Build cache key from tool name + risk factors.
	cacheKey := toolName + ":" + strings.Join(assessment.Factors, ",")

	// Check session cache first.
	sa.mu.RLock()
	if sa.sessionAllowed[cacheKey] {
		sa.mu.RUnlock()
		return SmartApprovalResult{
			Verdict:     SmartVerdictSafe,
			Explanation: "previously approved this session",
			Elapsed:     time.Since(start),
		}
	}
	sa.mu.RUnlock()

	if !sa.IsConfigured() {
		return SmartApprovalResult{
			Verdict:     SmartVerdictUnknown,
			Explanation: "LLM not configured for smart approval",
			Elapsed:     time.Since(start),
		}
	}

	prompt := buildSmartApprovalPrompt(toolName, args, assessment)

	type llmResult struct {
		content string
		err     error
	}
	ch := make(chan llmResult, 1)
	go func() {
		content, err := sa.llm.SecurityReview(smartApprovalSystemPrompt, prompt)
		ch <- llmResult{content, err}
	}()

	select {
	case res := <-ch:
		elapsed := time.Since(start)
		if res.err != nil {
			log.Printf("[SmartApproval] LLM call failed: %v", res.err)
			return SmartApprovalResult{
				Verdict:     SmartVerdictUnknown,
				Explanation: fmt.Sprintf("LLM call failed: %v", res.err),
				Elapsed:     elapsed,
			}
		}
		verdict, explanation := parseSmartVerdict(res.content)
		if verdict == SmartVerdictSafe {
			sa.mu.Lock()
			sa.sessionAllowed[cacheKey] = true
			sa.mu.Unlock()
			log.Printf("[SmartApproval] auto-approved: tool=%s factors=%v", toolName, assessment.Factors)
		}
		return SmartApprovalResult{
			Verdict:     verdict,
			Explanation: explanation,
			Elapsed:     elapsed,
		}
	case <-time.After(sa.timeout):
		return SmartApprovalResult{
			Verdict:     SmartVerdictUnknown,
			Explanation: fmt.Sprintf("smart approval timed out after %v", sa.timeout),
			Elapsed:     time.Since(start),
		}
	}
}

func buildSmartApprovalPrompt(toolName string, args map[string]interface{}, assessment RiskAssessment) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Tool: %s\n", toolName)
	fmt.Fprintf(&sb, "Risk level: %s\n", assessment.Level)
	fmt.Fprintf(&sb, "Risk factors: %s\n", strings.Join(assessment.Factors, ", "))
	argStr := flattenArgs(args)
	if len(argStr) > 500 {
		argStr = argStr[:500] + "..."
	}
	fmt.Fprintf(&sb, "Arguments: %s\n", argStr)
	return sb.String()
}

func parseSmartVerdict(content string) (SmartApprovalVerdict, string) {
	upper := strings.TrimSpace(strings.ToUpper(content))
	if strings.HasPrefix(upper, "SAFE") {
		return SmartVerdictSafe, content
	}
	if strings.HasPrefix(upper, "UNSAFE") || strings.HasPrefix(upper, "DANGEROUS") {
		return SmartVerdictUnsafe, content
	}
	return SmartVerdictUnknown, content
}
