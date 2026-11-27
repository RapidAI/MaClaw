package main

import (
	"context"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// srvCloudSessionNeeds adapts the process semantic resolver to a cloud-agent
// turn. The cloud host never classifies text itself.
type srvCloudSessionNeeds struct {
	svc *agentservice.Service
}

func (s srvCloudSessionNeeds) ResolveSessionNeeds(ctx context.Context, tenantID, userID, taskID, userText string) (agentservice.DynamicCapabilityNeedResolution, error) {
	if s.svc == nil {
		return agentservice.DynamicCapabilityNeedResolution{}, fmt.Errorf("semantic routing is unavailable")
	}
	return s.svc.ResolveDynamicCapabilityNeeds(ctx, agentservice.Principal{TenantID: tenantID, UserID: userID}, taskID, userText)
}
