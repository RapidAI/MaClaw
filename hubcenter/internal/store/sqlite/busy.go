package sqlite

import (
	"context"
	"errors"
	"strings"
	"time"
)

// IsBusy reports whether err is a SQLite SQLITE_BUSY / database-is-locked
// failure. modernc.org/sqlite surfaces these as plain errors whose text
// contains "SQLITE_BUSY" or "database is locked".
func IsBusy(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sqlite_busy") || strings.Contains(msg, "database is locked")
}

// RetryBusy runs fn and, when it returns a busy error, retries with a short
// backoff. Callers use this for single autocommit statements (lease writes,
// cursor upserts). Do not wrap a multi-statement transaction: retry the whole
// transaction from the start instead.
func RetryBusy(ctx context.Context, fn func() error) error {
	const attempts = 6
	delay := 20 * time.Millisecond
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil || !IsBusy(err) {
			return err
		}
		if ctx != nil {
			if ctx.Err() != nil {
				return err
			}
		}
		timer := time.NewTimer(delay)
		if ctx == nil {
			<-timer.C
		} else {
			select {
			case <-ctx.Done():
				timer.Stop()
				return err
			case <-timer.C:
			}
		}
		if delay < 320*time.Millisecond {
			delay *= 2
		}
	}
	return err
}
