package store

import (
	"strings"
	"time"
)

// ParseStoredClock reads a timestamp stored in a TEXT column.
// Account rows are written as RFC3339. Older rows and SQLite datetime()
// values use "YYYY-MM-DD HH:MM:SS" with no zone; those are UTC.
// A value this cannot read is the zero time. Callers that still have the
// original text must keep it, so a stored clock is not reported as missing.
func ParseStoredClock(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}
