package workflow

import (
	"context"
	"log"
	"time"
)

// TimeoutTicker periodically checks for approval nodes that have exceeded
// their configured timeout and triggers the HandleTimeout flow.
type TimeoutTicker struct {
	executor      *WorkflowExecutor
	instanceStore InstanceStore
	interval      time.Duration
	stopCh        chan struct{}
}

// NewTimeoutTicker creates a new TimeoutTicker that checks for timed-out
// approval nodes at the given interval (default: 5 minutes).
func NewTimeoutTicker(executor *WorkflowExecutor, instanceStore InstanceStore) *TimeoutTicker {
	return &TimeoutTicker{
		executor:      executor,
		instanceStore: instanceStore,
		interval:      5 * time.Minute,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the background timeout checking loop.
func (t *TimeoutTicker) Start() {
	go t.loop()
}

// Stop terminates the background timeout checking loop.
func (t *TimeoutTicker) Stop() {
	close(t.stopCh)
}

func (t *TimeoutTicker) loop() {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			t.checkTimeouts()
		}
	}
}

func (t *TimeoutTicker) checkTimeouts() {
	ctx := context.Background()
	// Query all running approval node executions that have exceeded their timeout.
	pendingExecs, err := t.instanceStore.GetPendingApprovals(ctx, "")
	if err != nil {
		log.Printf("[workflow-timeout] error querying pending approvals: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, exec := range pendingExecs {
		// Check if the node has been running longer than the configured timeout.
		// Default timeout is 24 hours if not configured on the node.
		elapsed := now.Sub(exec.StartedAt)
		if elapsed > 24*time.Hour {
			if err := t.executor.HandleTimeout(ctx, exec.InstanceID, exec.NodeID); err != nil {
				log.Printf("[workflow-timeout] error handling timeout for instance=%s node=%s: %v",
					exec.InstanceID, exec.NodeID, err)
			}
		}
	}
}
