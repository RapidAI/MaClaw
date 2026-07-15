package workflow

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// TimeoutTicker periodically checks for approval nodes that have exceeded
// their configured timeout and triggers the HandleTimeout flow.
type TimeoutTicker struct {
	executor      *WorkflowExecutor
	instanceStore InstanceStore
	interval      time.Duration
	stopCh        chan struct{}
	startOnce     sync.Once
	stopOnce      sync.Once
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
	t.startOnce.Do(func() { go t.loop() })
}

// Stop terminates the background timeout checking loop.
func (t *TimeoutTicker) Stop() {
	t.stopOnce.Do(func() { close(t.stopCh) })
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
		// The resolved approval configuration is persisted in Result when the
		// node is dispatched. Respect its timeout rather than treating every
		// approval as a 24-hour approval.
		elapsed := now.Sub(exec.StartedAt)
		if elapsed > approvalExecutionTimeout(exec) {
			execCtx := store.WithTenant(ctx, exec.TenantID)
			if err := t.executor.HandleTimeout(execCtx, exec.InstanceID, exec.NodeID); err != nil {
				log.Printf("[workflow-timeout] error handling timeout for instance=%s node=%s: %v",
					exec.InstanceID, exec.NodeID, err)
			}
		}
	}
}

const defaultApprovalTimeout = 24 * time.Hour

func approvalExecutionTimeout(exec NodeExecution) time.Duration {
	hours := extractApprovalTimeoutHours(exec)
	if hours <= 0 {
		return defaultApprovalTimeout
	}
	return time.Duration(hours) * time.Hour
}
