package agent

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
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

// LLMEndpointLabel is the person-facing name of a model route.
// The URL, the key, and any query string stay out of it.
func LLMEndpointLabel(cfg corelib.MaclawLLMConfig) string {
	model := strings.TrimSpace(cfg.Model)
	provider := strings.TrimSpace(cfg.ProviderName)
	switch {
	case provider != "" && model != "":
		return provider + "/" + model
	case model != "":
		return model
	default:
		return provider
	}
}

func llmEndpointHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// LLMSwitchPhrase is the evidence sentence for a real route change.
// ok is false when the saved selection did not change, or when the only
// difference is an id or a token the person cannot see. The caller must
// not claim a switch in that case.
func LLMSwitchPhrase(from, to corelib.MaclawLLMConfig) (string, bool) {
	if !LLMSelectionChanged(from, to) {
		return "", false
	}
	fromLabel := LLMEndpointLabel(from)
	toLabel := LLMEndpointLabel(to)
	if fromLabel == "" || toLabel == "" {
		return "", false
	}
	if fromLabel != toLabel {
		return fmt.Sprintf("已从「%s」改用「%s」，", fromLabel, toLabel), true
	}
	fromHost := llmEndpointHost(from.URL)
	toHost := llmEndpointHost(to.URL)
	if fromHost != "" && toHost != "" && fromHost != toHost {
		return fmt.Sprintf("已改用「%s」的另一个服务，", toLabel), true
	}
	return "", false
}

// LLMRetrySwitchedInFlightStatus is the one live-status row for a retry
// that is about to be sent on a different model. It names both endpoints.
// A selection that did not visibly change returns ok=false, and the caller
// keeps the ordinary in-flight sentence.
func LLMRetrySwitchedInFlightStatus(attempt int, from, to corelib.MaclawLLMConfig) (string, bool) {
	phrase, ok := LLMSwitchPhrase(from, to)
	if !ok {
		return "", false
	}
	if attempt < 1 {
		attempt = 1
	}
	return llmRetryLiveStatus(phrase + fmt.Sprintf("正在重新请求（第 %d 次）。", attempt)), true
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
