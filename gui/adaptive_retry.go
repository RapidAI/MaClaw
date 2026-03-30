package main

import (
	"fmt"
	"strings"
	"time"
)

// FailureCategory 失败分类。
type FailureCategory string

const (
	FailureNetwork    FailureCategory = "network"
	FailurePermission FailureCategory = "permission"
	FailureArgs       FailureCategory = "args"
	FailureLogic      FailureCategory = "logic"
	FailureUnknown    FailureCategory = "unknown"
)

const (
	defaultMaxFailures    = 5
	maxNetworkRetries     = 3
	baseRetryDelay        = 1 * time.Second
)

// RetryDecision 重试决策。
type RetryDecision struct {
	Action       string        // "retry", "fix", "skip", "disable"
	Delay        time.Duration // 重试延迟 (指数退避)
	ErrorContext string        // 注入 LLM 的错误上下文
	Attempt      int           // 当前重试次数
}

// AdaptiveRetry 分析 tool_call 失败并决定重试策略。
type AdaptiveRetry struct {
	failureCounts map[string]int    // toolName → 累计失败次数
	maxFailures   int               // 单工具最大失败次数 (默认 5)
	disabledTools map[string]bool   // 已禁用的工具
	recorder      *TrajectoryRecorder
}

// NewAdaptiveRetry 创建智能重试器。
func NewAdaptiveRetry(recorder *TrajectoryRecorder) *AdaptiveRetry {
	return &AdaptiveRetry{
		failureCounts: make(map[string]int),
		maxFailures:   defaultMaxFailures,
		disabledTools: make(map[string]bool),
		recorder:      recorder,
	}
}

// networkKeywords are substrings that indicate a network-related failure.
var networkKeywords = []string{
	"timeout", "connection refused", "network", "dial",
	"eof", "reset by peer", "i/o timeout",
}

// permissionKeywords are substrings that indicate a permission-related failure.
var permissionKeywords = []string{
	"permission denied", "access denied", "forbidden",
	"unauthorized", "403", "401",
}

// argsKeywords are substrings that indicate an argument/parameter failure.
var argsKeywords = []string{
	"invalid argument", "invalid parameter", "missing required",
	"bad request", "400",
}

// logicKeywords are substrings that indicate a logic-level failure.
var logicKeywords = []string{
	"not found", "already exists", "conflict", "assertion failed",
}

// Classify 分析错误信息，返回失败分类。
func (r *AdaptiveRetry) Classify(toolName string, err error) FailureCategory {
	if err == nil {
		return FailureUnknown
	}
	msg := strings.ToLower(err.Error())

	for _, kw := range networkKeywords {
		if strings.Contains(msg, kw) {
			return FailureNetwork
		}
	}
	for _, kw := range permissionKeywords {
		if strings.Contains(msg, kw) {
			return FailurePermission
		}
	}
	for _, kw := range argsKeywords {
		if strings.Contains(msg, kw) {
			return FailureArgs
		}
	}
	for _, kw := range logicKeywords {
		if strings.Contains(msg, kw) {
			return FailureLogic
		}
	}
	return FailureUnknown
}

// Decide 根据失败分类和历史决定重试策略。
// 注意：此方法不修改内部状态，调用者应在确认执行后调用 RecordFailure。
func (r *AdaptiveRetry) Decide(toolName string, category FailureCategory, attempt int) RetryDecision {
	// Check cumulative failures — disable if threshold reached.
	if r.failureCounts[toolName] >= r.maxFailures {
		return RetryDecision{
			Action:       "disable",
			ErrorContext: fmt.Sprintf("工具 %s 累计失败 %d 次，已标记为不可用。请使用替代方案。", toolName, r.failureCounts[toolName]),
			Attempt:      attempt,
		}
	}

	switch category {
	case FailureNetwork:
		if attempt >= maxNetworkRetries {
			return RetryDecision{
				Action:       "skip",
				ErrorContext: fmt.Sprintf("工具 %s 网络错误重试已达上限 (%d 次)，跳过。", toolName, maxNetworkRetries),
				Attempt:      attempt,
			}
		}
		// Exponential backoff: baseDelay * 2^attempt
		delay := baseRetryDelay * time.Duration(1<<uint(attempt))
		return RetryDecision{
			Action:  "retry",
			Delay:   delay,
			Attempt: attempt,
		}

	case FailureArgs, FailureLogic:
		return RetryDecision{
			Action:       "fix",
			ErrorContext: fmt.Sprintf("工具 %s 执行失败（%s 错误），请根据错误信息修正参数后重试。", toolName, string(category)),
			Attempt:      attempt,
		}

	case FailurePermission:
		return RetryDecision{
			Action:       "skip",
			ErrorContext: fmt.Sprintf("工具 %s 权限不足，跳过此操作。", toolName),
			Attempt:      attempt,
		}

	default: // FailureUnknown
		// Default: retry once then skip.
		if attempt >= 1 {
			return RetryDecision{
				Action:       "skip",
				ErrorContext: fmt.Sprintf("工具 %s 未知错误，已重试 %d 次，跳过。", toolName, attempt),
				Attempt:      attempt,
			}
		}
		return RetryDecision{
			Action:  "retry",
			Delay:   baseRetryDelay,
			Attempt: attempt,
		}
	}
}

// RecordFailure 记录一次失败到 TrajectoryRecorder，并更新内部计数。
func (r *AdaptiveRetry) RecordFailure(toolName string, category FailureCategory, decision RetryDecision) {
	r.failureCounts[toolName]++
	if r.failureCounts[toolName] >= r.maxFailures {
		r.disabledTools[toolName] = true
	}
	if r.recorder == nil {
		return
	}
	content := fmt.Sprintf("tool=%s category=%s action=%s attempt=%d",
		toolName, string(category), decision.Action, decision.Attempt)
	r.recorder.Record("system", content, nil, "", "adaptive_retry")
}

// IsDisabled 检查工具是否已被禁用。
func (r *AdaptiveRetry) IsDisabled(toolName string) bool {
	return r.disabledTools[toolName]
}
