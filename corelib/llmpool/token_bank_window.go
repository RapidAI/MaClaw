package llmpool

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TokenBankShareWindow is when a shared model may be dialed.
// The zero value means always available, so a share saved before windows
// stays online. This is not a billing schedule: CreditMultiplier and
// CreditMultiplierSchedule stay unset, and the window cannot change what
// the consumer is charged.
//
// Days use Go weekday numbers: 0=Sunday ... 6=Saturday. Empty Days means
// every day. Start and End are Asia/Shanghai clock times "HH:MM". End is
// exclusive. "24:00" is the end of that day (1440 minutes) and is only
// valid as End. If Start is later than End, the window crosses midnight:
// the evening belongs to the checked day, and the morning belongs to the
// following day.
type TokenBankShareWindow struct {
	Days  []int  `json:"days,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Always reports that no day or clock restriction is stored.
func (w TokenBankShareWindow) Always() bool {
	return len(w.Days) == 0 && strings.TrimSpace(w.Start) == "" && strings.TrimSpace(w.End) == ""
}

// NormalizeTokenBankShareWindow canonicalizes a window.
// A nil window, a zero window, and every day from 00:00 to 24:00 are always
// available. A restricted window is returned with "HH:MM" clocks. 24:00 is
// kept only as an exclusive end. ok is false when the clock cannot be read,
// the start equals the end, or every listed day is outside 0..6.
func NormalizeTokenBankShareWindow(in *TokenBankShareWindow) (TokenBankShareWindow, bool) {
	if in == nil || in.Always() {
		return TokenBankShareWindow{}, true
	}
	startRaw := strings.TrimSpace(in.Start)
	endRaw := strings.TrimSpace(in.End)
	if startRaw == "" {
		startRaw = "00:00"
	}
	if endRaw == "" {
		endRaw = "24:00"
	}
	start, ok := parseShareClockMinutes(startRaw)
	if !ok || start >= 24*60 {
		return TokenBankShareWindow{}, false
	}
	end, ok := parseShareClockMinutes(endRaw)
	if !ok || start == end {
		return TokenBankShareWindow{}, false
	}
	days := uniqueWeekdays(in.Days)
	if len(in.Days) > 0 && len(days) == 0 {
		return TokenBankShareWindow{}, false
	}
	if len(days) == 7 {
		days = nil
	}
	if len(days) == 0 && start == 0 && end == 24*60 {
		return TokenBankShareWindow{}, true
	}
	return TokenBankShareWindow{
		Days:  days,
		Start: formatShareClock(start),
		End:   formatShareClock(end),
	}, true
}

// MarshalTokenBankShareWindow stores a normalized window as JSON.
// Always-available windows are an empty string. A window that cannot be
// read returns an error so the caller can reject the share instead of
// publishing a member that would be dialed all day.
func MarshalTokenBankShareWindow(in *TokenBankShareWindow) (string, error) {
	normalized, ok := NormalizeTokenBankShareWindow(in)
	if !ok {
		return "", fmt.Errorf("share window must use Asia/Shanghai HH:MM, with 24:00 only as the end of the day")
	}
	if normalized.Always() {
		return "", nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ParseTokenBankShareWindow reads a stored window. Empty text is always
// available. Text that is not a usable window does not match any clock, so
// a corrupt row is not dialed outside the hours the owner asked for.
func ParseTokenBankShareWindow(raw string) TokenBankShareWindow {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" || raw == "{}" {
		return TokenBankShareWindow{}
	}
	var window TokenBankShareWindow
	if err := json.Unmarshal([]byte(raw), &window); err != nil {
		return tokenBankShareWindowClosed()
	}
	normalized, ok := NormalizeTokenBankShareWindow(&window)
	if !ok {
		return tokenBankShareWindowClosed()
	}
	return normalized
}

// TokenBankShareWindowAllows reports whether now, in Asia/Shanghai, falls
// inside the window. A zero window is always allowed. now's zero value is
// the current time.
func TokenBankShareWindowAllows(window TokenBankShareWindow, now time.Time) bool {
	normalized, ok := NormalizeTokenBankShareWindow(&window)
	if !ok {
		return false
	}
	if normalized.Always() {
		return true
	}
	if now.IsZero() {
		now = time.Now()
	}
	local := now.In(loadCreditMultiplierLocation(DefaultCreditMultiplierTimezone))
	weekday := int(local.Weekday())
	minutes := local.Hour()*60 + local.Minute()
	start, ok := parseShareClockMinutes(normalized.Start)
	if !ok {
		return false
	}
	end, ok := parseShareClockMinutes(normalized.End)
	if !ok || start == end || start >= 24*60 {
		return false
	}
	if start < end {
		return weekdayMatches(normalized.Days, weekday) && minutes >= start && minutes < end
	}
	// Overnight: the evening is the checked day. The morning is the next day,
	// so it matches only when yesterday was checked.
	if minutes >= start {
		return weekdayMatches(normalized.Days, weekday)
	}
	if minutes < end {
		return weekdayMatches(normalized.Days, (weekday+6)%7)
	}
	return false
}

func tokenBankShareWindowClosed() TokenBankShareWindow {
	return TokenBankShareWindow{Start: "99:99"}
}

func parseShareClockMinutes(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	hour, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || hour < 0 || hour > 24 {
		return 0, false
	}
	minute, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || minute < 0 || minute > 59 {
		return 0, false
	}
	if hour == 24 && minute != 0 {
		return 0, false
	}
	if len(parts) == 3 {
		second, secErr := strconv.Atoi(strings.TrimSpace(parts[2]))
		if secErr != nil || second != 0 {
			return 0, false
		}
	}
	return hour*60 + minute, true
}

func formatShareClock(minutes int) string {
	if minutes == 24*60 {
		return "24:00"
	}
	if minutes < 0 {
		minutes = 0
	}
	if minutes > 23*60+59 {
		minutes = 23*60 + 59
	}
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}
