package ve

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const (
	// quotaPushRetryInterval is the interval between retry attempts when a
	// quota push reception fails (network error or decryption error).
	quotaPushRetryInterval = 60 * time.Second

	// quotaPushMaxRetries is the maximum number of retry attempts before
	// giving up and continuing with the previously persisted quota.
	quotaPushMaxRetries = 5
)

// QuotaPushError categorizes the type of failure during quota push reception.
type QuotaPushError struct {
	Type    string // "network", "decryption", "invalid_value"
	Message string
	Err     error
}

func (e *QuotaPushError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("quota push %s error: %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("quota push %s error: %s", e.Type, e.Message)
}

func (e *QuotaPushError) Unwrap() error { return e.Err }

// QuotaPushFetcher is the interface for fetching the latest quota from HubCenter.
// Implementations handle the actual network communication and decryption.
type QuotaPushFetcher interface {
	// FetchLatestQuota attempts to fetch and decrypt the latest VE quota
	// from HubCenter. Returns the quota value or a QuotaPushError on failure.
	FetchLatestQuota(ctx context.Context) (int, error)
}

// QuotaPushRetrier manages retry logic for failed quota push receptions.
// When a quota push from HubCenter fails (network error, decryption error,
// or invalid value), it retries at 60-second intervals for a maximum of 5
// attempts. During retries, the previously persisted quota remains in effect.
type QuotaPushRetrier struct {
	mu         sync.Mutex
	store      *QuotaStore
	fetcher    QuotaPushFetcher
	cancelFunc context.CancelFunc // cancels the current retry loop if active
	retrying   bool
	attempts   int
	lastError  error
}

// NewQuotaPushRetrier creates a new retrier bound to the given QuotaStore.
func NewQuotaPushRetrier(store *QuotaStore, fetcher QuotaPushFetcher) *QuotaPushRetrier {
	return &QuotaPushRetrier{
		store:   store,
		fetcher: fetcher,
	}
}

// HandlePushResult processes the result of a quota push reception attempt.
// If the push succeeded, it persists the new quota immediately.
// If the push failed, it initiates the retry mechanism.
func (r *QuotaPushRetrier) HandlePushResult(quota int, err error) {
	if err == nil {
		// Push succeeded — persist immediately and cancel any active retry loop.
		r.cancelRetry()
		if saveErr := r.store.SaveQuota(quota); saveErr != nil {
			log.Printf("[ve-quota-push] WARNING: push received quota=%d but failed to persist: %v", quota, saveErr)
		} else {
			r.store.InvalidateCache()
			log.Printf("[ve-quota-push] quota updated successfully: %d", quota)
		}
		return
	}

	// Push failed — start retry loop if not already retrying.
	log.Printf("[ve-quota-push] push reception failed: %v — initiating retry (max %d attempts, interval %s)",
		err, quotaPushMaxRetries, quotaPushRetryInterval)
	r.startRetry()
}

// IsRetrying returns true if a retry loop is currently active.
func (r *QuotaPushRetrier) IsRetrying() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retrying
}

// Attempts returns the number of retry attempts made in the current cycle.
func (r *QuotaPushRetrier) Attempts() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts
}

// LastError returns the last error encountered during retry attempts.
func (r *QuotaPushRetrier) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}

// Stop cancels any active retry loop. Safe to call multiple times.
func (r *QuotaPushRetrier) Stop() {
	r.cancelRetry()
}

// startRetry begins the retry loop in a background goroutine.
// If a retry loop is already active, this is a no-op.
func (r *QuotaPushRetrier) startRetry() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.retrying {
		// Already retrying — don't start a second loop.
		return
	}

	r.retrying = true
	r.attempts = 0
	r.lastError = nil

	ctx, cancel := context.WithCancel(context.Background())
	r.cancelFunc = cancel

	go r.retryLoop(ctx)
}

// cancelRetry stops the active retry loop if one is running.
func (r *QuotaPushRetrier) cancelRetry() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cancelFunc != nil {
		r.cancelFunc()
		r.cancelFunc = nil
	}
	r.retrying = false
	r.attempts = 0
}

// retryLoop runs in a goroutine, attempting to fetch the latest quota at
// 60-second intervals for up to 5 attempts.
func (r *QuotaPushRetrier) retryLoop(ctx context.Context) {
	defer func() {
		r.mu.Lock()
		r.retrying = false
		r.cancelFunc = nil
		r.mu.Unlock()
	}()

	for attempt := 1; attempt <= quotaPushMaxRetries; attempt++ {
		// Wait for the retry interval (or context cancellation).
		select {
		case <-ctx.Done():
			log.Printf("[ve-quota-push] retry loop cancelled")
			return
		case <-time.After(quotaPushRetryInterval):
		}

		// Check if context was cancelled during the wait.
		if ctx.Err() != nil {
			return
		}

		r.mu.Lock()
		r.attempts = attempt
		r.mu.Unlock()

		log.Printf("[ve-quota-push] retry attempt %d/%d", attempt, quotaPushMaxRetries)

		quota, err := r.fetcher.FetchLatestQuota(ctx)
		if err != nil {
			r.mu.Lock()
			r.lastError = err
			r.mu.Unlock()

			log.Printf("[ve-quota-push] retry attempt %d/%d failed: %v", attempt, quotaPushMaxRetries, err)

			if attempt == quotaPushMaxRetries {
				log.Printf("[ve-quota-push] WARNING: all %d retry attempts exhausted — continuing with previously persisted quota", quotaPushMaxRetries)
			}
			continue
		}

		// Retry succeeded — persist the new quota.
		if saveErr := r.store.SaveQuota(quota); saveErr != nil {
			log.Printf("[ve-quota-push] retry attempt %d succeeded (quota=%d) but failed to persist: %v", attempt, quota, saveErr)
			r.mu.Lock()
			r.lastError = saveErr
			r.mu.Unlock()
			continue
		}

		r.store.InvalidateCache()
		log.Printf("[ve-quota-push] retry attempt %d/%d succeeded: quota updated to %d", attempt, quotaPushMaxRetries, quota)
		return
	}
}
