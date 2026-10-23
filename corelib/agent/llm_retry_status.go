package agent

import (
	"fmt"
	"strings"
	"time"
)

// LLM retry progress shares one chat row. The prefix is the row's identity.
// The desktop strips it before display and replaces the previous sentence
// instead of stacking a new line. A repeated sentence is dropped, so each
// phase (wait, then the request itself) has to say something different.
const LLMRetryLiveStatusPrefix = "[LiveStatus:llm-retry] "

func llmRetryLiveStatus(text string) string {
	return LLMRetryLiveStatusPrefix + text
}

// LLMRetryLiveStatusText reports the sentence inside a live-status row.
// The prefix is a row id for the chat tray. Surfaces that store the progress
// string as copy have to use the sentence, or the marker is shown to the user.
func LLMRetryLiveStatusText(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	marker := strings.TrimSpace(LLMRetryLiveStatusPrefix)
	if marker == "" || !strings.HasPrefix(trimmed, marker) {
		return "", false
	}
	visible := strings.TrimSpace(strings.TrimPrefix(trimmed, marker))
	if visible == "" {
		return "", false
	}
	return visible, true
}

func llmRetryWaitSeconds(wait time.Duration) int {
	if wait <= 0 {
		return 0
	}
	seconds := int((wait + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

// LLMRetryWaitStatus is the fast exponential wait, 重试 1/5 through 5/5.
func LLMRetryWaitStatus(attempt, max int, wait time.Duration, auth bool) string {
	pattern := "重试 %d/%d：API 暂时不可用，%d 秒后重试"
	if auth {
		pattern = "重试 %d/%d：模型认证暂时失败，%d 秒后重试"
	}
	return llmRetryLiveStatus(fmt.Sprintf(pattern, attempt, max, llmRetryWaitSeconds(wait)))
}

// llmRetryKeepGoingStatus continues the same count after the fast budget.
// 第 6 次 follows 重试 5/5. The tail does not start again at 1.
func llmRetryKeepGoingStatus(attempt int, wait time.Duration) string {
	return llmRetryLiveStatus(fmt.Sprintf("仍在重试，第 %d 次：服务暂时不可用，%d 秒后继续", attempt, llmRetryWaitSeconds(wait)))
}

// LLMRetryAuthRefreshStatus is the same chat row as the countdown.
// Credential refresh skips the sleep, so this is the sentence on screen
// until the next request starts. An ordinary progress line would retire
// the row and then sit beside the in-flight sentence.
func LLMRetryAuthRefreshStatus() string {
	return llmRetryLiveStatus("已刷新模型登录状态，即将重试...")
}

// LLMRetryInFlightStatus replaces the countdown once the wait is over.
// The next HTTP call can outlast the announced delay. Leaving "32 秒后继续"
// on screen through that call tells the user the pause is still running.
func LLMRetryInFlightStatus(attempt int) string {
	if attempt < 1 {
		attempt = 1
	}
	return llmRetryLiveStatus(fmt.Sprintf("正在重新请求模型（第 %d 次）", attempt))
}

func continuationAttempt(fast, extra int) int {
	if extra > 0 {
		return maxLLMRetries + extra
	}
	if fast > 0 {
		return fast
	}
	return 1
}
