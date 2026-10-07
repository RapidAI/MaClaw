package llmpool

import "time"

// ServeWindows is when a provider may answer requests. Multiple windows are a
// union: one match opens the provider, and an empty list means always
// available. The shape and clock rules come from TokenBankShareWindow:
// Days use Go weekday numbers (0=Sunday ... 6=Saturday), empty Days means
// every day, Start and End are Asia/Shanghai clock times "HH:MM", End is
// exclusive and "24:00" is the end of that day, and a Start later than End
// wraps past midnight. This is a dial gate, not a billing schedule: it never
// changes what the consumer is charged.

// NormalizeServeWindows canonicalizes a serve-window policy.
// An unreadable window fails the whole policy, so a save with a broken clock
// is rejected instead of silently widening the provider's availability.
// A window that reads as always open (every day 00:00–24:00) collapses the
// whole policy to always open, because the union cannot be narrower.
func NormalizeServeWindows(in []TokenBankShareWindow) ([]TokenBankShareWindow, bool) {
	if len(in) == 0 {
		return nil, true
	}
	out := make([]TokenBankShareWindow, 0, len(in))
	for i := range in {
		normalized, ok := NormalizeTokenBankShareWindow(&in[i])
		if !ok {
			return nil, false
		}
		if normalized.Always() {
			return nil, true
		}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, true
	}
	return out, true
}

// ServeWindowsAllows reports whether now falls inside at least one window.
// An empty policy is always allowed; a policy that failed normalization is
// never allowed, so a corrupt row keeps the provider offline outside the
// hours its owner asked for.
func ServeWindowsAllows(windows []TokenBankShareWindow, now time.Time) bool {
	if len(windows) == 0 {
		return true
	}
	for i := range windows {
		if TokenBankShareWindowAllows(windows[i], now) {
			return true
		}
	}
	return false
}
