package llmservice

import (
	"fmt"
	"strings"
	"sync"
)

// Token Bank consecutive-failure pause.
//
// The threshold and the pause action are installed by app at bootstrap. This
// package does not read system settings: a zero threshold, or a missing
// callback, leaves the hook as a counter with no side effect.

type tokenBankAutoPauseState struct {
	mu        sync.Mutex
	threshold func() int
	pause     func(shareID, model, reason string)
}

var tokenBankAutoPause tokenBankAutoPauseState

// ConfigureTokenBankAutoPause installs the pause hook. threshold 0, returned
// by the function, disables the pause. A nil function disables it too.
func ConfigureTokenBankAutoPause(threshold func() int, pause func(shareID, model, reason string)) {
	tokenBankAutoPause.mu.Lock()
	defer tokenBankAutoPause.mu.Unlock()
	tokenBankAutoPause.threshold = threshold
	tokenBankAutoPause.pause = pause
}

func resetTokenBankAutoPause() {
	ConfigureTokenBankAutoPause(nil, nil)
}

func noteTokenBankAutoPause(providerID string, status int, errMsg string, failures int) {
	if failures <= 0 || !IsTokenBankMemberID(providerID) || !memberAttemptFailed(status, errMsg) {
		return
	}
	tokenBankAutoPause.mu.Lock()
	thresholdFn := tokenBankAutoPause.threshold
	pause := tokenBankAutoPause.pause
	tokenBankAutoPause.mu.Unlock()
	if thresholdFn == nil || pause == nil {
		return
	}
	threshold := thresholdFn()
	if threshold <= 0 || failures < threshold {
		return
	}
	// Fires at the threshold and again on every failure past it. Traffic that
	// keeps arriving past the threshold means the pause did not land (a
	// transient store error swallowed it) — retrying is the recovery path,
	// because the counter only resets on success and a streak that never
	// equals the threshold again would never pause. A landed pause stops
	// dispatch, so the notes stop and the retries stop with them. Repeat
	// calls are cheap: SetSharePaused is idempotent and SetTokenBankSharePaused
	// persists nothing when no member flips.
	shareID, model, ok := ParseTokenBankMemberID(providerID)
	if !ok {
		return
	}
	pause(shareID, model, fmt.Sprintf("paused after %d consecutive failures", failures))
}

func memberAttemptSucceeded(status int, errMsg string) bool {
	return status >= 200 && status < 400 && strings.TrimSpace(errMsg) == ""
}

// memberAttemptFailed counts an upstream or credential failure. A client 400
// does not count: one bad prompt must not pause the share.
func memberAttemptFailed(status int, errMsg string) bool {
	if memberAttemptSucceeded(status, errMsg) {
		return false
	}
	switch status {
	case 400, 404, 422:
		return false
	}
	if status == 0 || status == 401 || status == 402 || status == 403 || status == 408 || status == 429 || status >= 500 {
		return true
	}
	return strings.TrimSpace(errMsg) != ""
}
