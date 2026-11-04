package main

import (
	"context"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// The run admission no longer opens the desktop; these tests model a run
// whose first step needs it, so the step's wake follows the admission.
func occupyUserDesktopWoken(ctx context.Context, tenantID, userID, instanceID string) func() {
	release := occupyUserDesktop(ctx, tenantID, userID, instanceID)
	_, _ = desktopWakeForStep(ctx, agentruntime.Scope{TenantID: tenantID, UserID: userID}, "")
	return release
}

func occupyUserDesktopRunWoken(ctx context.Context, tenantID, userID, instanceID string) (func(), *bool) {
	release, owned := occupyUserDesktopRun(ctx, tenantID, userID, instanceID)
	_, _ = desktopWakeForStep(ctx, agentruntime.Scope{TenantID: tenantID, UserID: userID}, "")
	return release, owned
}
