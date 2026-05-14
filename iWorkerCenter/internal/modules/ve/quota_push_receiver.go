package ve

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// QuotaPushMessage represents a quota update message received from HubCenter
// via the Hub-HubCenter communication channel (heartbeat/notification).
type QuotaPushMessage struct {
	Quota     int    `json:"quota"`      // New quota value (0-10000)
	HubID     string `json:"hub_id"`     // Target Hub ID for validation
	Timestamp int64  `json:"timestamp"`  // Unix milliseconds when the update was issued
}

// QuotaPushReceiver listens for quota update messages from HubCenter
// and re-encrypts/persists the new quota within 5 seconds of receipt.
// It also handles retry logic on failure (60s interval, max 5 attempts).
type QuotaPushReceiver struct {
	mu         sync.Mutex
	quotaStore *QuotaStore
	hubID      string

	// Retry state
	pendingQuota    *QuotaPushMessage
	retryCount      int
	retryTimer      *time.Timer
	maxRetries      int
	retryInterval   time.Duration
	persistDeadline time.Duration

	// Lifecycle
	stopCh chan struct{}
	done   chan struct{}
	started bool
}

const (
	defaultMaxRetries      = 5
	defaultRetryInterval   = 60 * time.Second
	defaultPersistDeadline = 5 * time.Second
)

// NewQuotaPushReceiver creates a new receiver that will persist quota updates
// to the given QuotaStore.
func NewQuotaPushReceiver(quotaStore *QuotaStore, hubID string) *QuotaPushReceiver {
	return &QuotaPushReceiver{
		quotaStore:      quotaStore,
		hubID:           hubID,
		maxRetries:      defaultMaxRetries,
		retryInterval:   defaultRetryInterval,
		persistDeadline: defaultPersistDeadline,
		stopCh:          make(chan struct{}),
		done:            make(chan struct{}),
	}
}

// Start begins the receiver's background retry loop.
// Must be called before HandleQuotaUpdate.
func (r *QuotaPushReceiver) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return
	}
	r.started = true
	close(r.done) // mark as ready (no background loop needed until a retry is scheduled)
	r.done = make(chan struct{})
}

// Stop shuts down the receiver and cancels any pending retries.
func (r *QuotaPushReceiver) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.started {
		return
	}
	r.started = false
	if r.retryTimer != nil {
		r.retryTimer.Stop()
		r.retryTimer = nil
	}
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
}

// HandleQuotaUpdate processes a quota update message received from HubCenter.
// It validates the message and persists the new quota within 5 seconds.
// On failure, it schedules retries (60s interval, max 5 attempts).
//
// This method is safe to call concurrently.
func (r *QuotaPushReceiver) HandleQuotaUpdate(msg QuotaPushMessage) error {
	receiveTime := time.Now()

	// Validate the message
	if err := r.validateMessage(msg); err != nil {
		log.Printf("[ve-quota-push] rejected invalid quota update: %v", err)
		return err
	}

	log.Printf("[ve-quota-push] received quota update: quota=%d hub_id=%s", msg.Quota, msg.HubID)

	// Attempt to persist within the 5-second deadline
	ctx, cancel := context.WithTimeout(context.Background(), r.persistDeadline)
	defer cancel()

	err := r.persistQuota(ctx, msg.Quota)
	elapsed := time.Since(receiveTime)

	if err == nil {
		log.Printf("[ve-quota-push] quota persisted successfully: quota=%d elapsed=%s", msg.Quota, elapsed)
		r.clearPendingRetry()
		return nil
	}

	log.Printf("[ve-quota-push] failed to persist quota (elapsed=%s): %v", elapsed, err)

	// Schedule retry
	r.scheduleRetry(msg)
	return fmt.Errorf("quota persist failed (retry scheduled): %w", err)
}

// validateMessage checks that the quota push message is valid.
func (r *QuotaPushReceiver) validateMessage(msg QuotaPushMessage) error {
	// Validate quota range
	if msg.Quota < 0 || msg.Quota > quotaMaxValue {
		return fmt.Errorf("quota value %d out of valid range [0, %d]", msg.Quota, quotaMaxValue)
	}

	// Validate Hub ID matches this Hub
	if msg.HubID != "" && msg.HubID != r.hubID {
		return fmt.Errorf("hub_id mismatch: message=%q, local=%q", msg.HubID, r.hubID)
	}

	return nil
}

// persistQuota calls QuotaStore.SaveQuota with context awareness.
func (r *QuotaPushReceiver) persistQuota(ctx context.Context, quota int) error {
	// SaveQuota is synchronous; we use the context for deadline tracking.
	done := make(chan error, 1)
	go func() {
		done <- r.quotaStore.SaveQuota(quota)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("persist deadline exceeded: %w", ctx.Err())
	}
}

// scheduleRetry sets up a retry for the failed quota update.
func (r *QuotaPushReceiver) scheduleRetry(msg QuotaPushMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pendingQuota = &msg
	r.retryCount = 0

	// Cancel any existing retry timer
	if r.retryTimer != nil {
		r.retryTimer.Stop()
	}

	r.startRetryTimerLocked()
}

// clearPendingRetry cancels any pending retry.
func (r *QuotaPushReceiver) clearPendingRetry() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pendingQuota = nil
	r.retryCount = 0
	if r.retryTimer != nil {
		r.retryTimer.Stop()
		r.retryTimer = nil
	}
}

// startRetryTimerLocked starts the retry timer. Must be called with r.mu held.
func (r *QuotaPushReceiver) startRetryTimerLocked() {
	r.retryTimer = time.AfterFunc(r.retryInterval, func() {
		r.executeRetry()
	})
}

// executeRetry attempts to persist the pending quota update.
func (r *QuotaPushReceiver) executeRetry() {
	r.mu.Lock()
	if r.pendingQuota == nil {
		r.mu.Unlock()
		return
	}

	r.retryCount++
	count := r.retryCount
	maxRetries := r.maxRetries
	quota := r.pendingQuota.Quota

	if count > maxRetries {
		log.Printf("[ve-quota-push] retry exhausted (%d/%d), giving up on quota=%d — continuing with previously persisted value",
			count-1, maxRetries, quota)
		r.pendingQuota = nil
		r.retryTimer = nil
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()

	log.Printf("[ve-quota-push] retry %d/%d: persisting quota=%d", count, maxRetries, quota)

	ctx, cancel := context.WithTimeout(context.Background(), r.persistDeadline)
	defer cancel()

	if err := r.persistQuota(ctx, quota); err != nil {
		log.Printf("[ve-quota-push] retry %d/%d failed: %v", count, maxRetries, err)

		// Schedule next retry
		r.mu.Lock()
		if r.pendingQuota != nil && count < maxRetries {
			r.startRetryTimerLocked()
		} else {
			log.Printf("[ve-quota-push] retry exhausted (%d/%d), giving up — continuing with previously persisted value",
				count, maxRetries)
			r.pendingQuota = nil
			r.retryTimer = nil
		}
		r.mu.Unlock()
		return
	}

	log.Printf("[ve-quota-push] retry %d/%d succeeded: quota=%d persisted", count, maxRetries, quota)
	r.clearPendingRetry()
}

// PendingRetryCount returns the current retry count for testing/monitoring.
func (r *QuotaPushReceiver) PendingRetryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.retryCount
}

// HasPendingRetry returns true if there is a pending retry scheduled.
func (r *QuotaPushReceiver) HasPendingRetry() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pendingQuota != nil
}
