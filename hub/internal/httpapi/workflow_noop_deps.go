package httpapi

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/workflow"
)

// noopApprovalDispatcher is a placeholder ApprovalDispatcher that does nothing.
// It will be replaced with a real A2A-based dispatcher once the Hub's A2A
// envelope routing is fully wired to the workflow engine.
type noopApprovalDispatcher struct{}

func (d *noopApprovalDispatcher) Dispatch(ctx context.Context, req *workflow.ApprovalRequest, approverID string) error {
	return nil
}

func (d *noopApprovalDispatcher) DispatchFallback(ctx context.Context, req *workflow.ApprovalRequest, fallbackID string, reason string) error {
	return nil
}

// noopAvailabilityChecker is a placeholder HumanApproverChecker that always
// returns true. It will be replaced with a real implementation that checks
// whether a human approver is online via the Hub's device/presence system.
type noopAvailabilityChecker struct{}

func (c *noopAvailabilityChecker) IsAvailable(ctx context.Context, approverID string) bool {
	return true
}
