package httpapi

import (
	"context"

	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type TenantConnectionStopper interface {
	StopTenantConnections(ctx context.Context, tenantID string)
}

type ConnectionStopper interface {
	TenantConnectionStopper
	StopUserConnections(ctx context.Context, tenantID, userID string)
}

type tenantConnectionStopper struct {
	devices  *device.Service
	gateway  *ws.Gateway
	notifier *chat.Notifier
}

func newTenantConnectionStopper(devices *device.Service, gateway *ws.Gateway, notifier *chat.Notifier) ConnectionStopper {
	if devices == nil && gateway == nil && notifier == nil {
		return nil
	}
	return &tenantConnectionStopper{devices: devices, gateway: gateway, notifier: notifier}
}

func (s *tenantConnectionStopper) StopTenantConnections(_ context.Context, tenantID string) {
	if s == nil {
		return
	}
	if s.devices != nil {
		s.devices.CloseTenant(tenantID)
	}
	if s.gateway != nil {
		s.gateway.CloseTenantViewers(tenantID)
	}
	if s.notifier != nil {
		s.notifier.CloseTenant(tenantID)
	}
	localRelay.CloseTenant(tenantID)
	mobileRealtimeCloseTenant(tenantID)
}

func (s *tenantConnectionStopper) StopUserConnections(_ context.Context, tenantID, userID string) {
	if s == nil {
		return
	}
	if s.devices != nil {
		s.devices.CloseUser(tenantID, userID)
	}
	if s.gateway != nil {
		s.gateway.CloseUserViewers(tenantID, userID)
	}
	if s.notifier != nil {
		s.notifier.CloseUserForTenant(tenantID, userID)
	}
	localRelay.CloseUser(localRelayKey(tenantID, userID))
	mobileRealtimeCloseUser(tenantID, userID)
}

func stopTenantConnections(ctx context.Context, tenantID string, stoppers []any) {
	for _, item := range stoppers {
		if stopper, ok := item.(TenantConnectionStopper); ok && stopper != nil {
			stopper.StopTenantConnections(ctx, tenantID)
		}
	}
}
