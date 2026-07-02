package app

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/notification"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
)

// hubServiceNotifResolver adapts hubs.Service to the notification.HubResolver interface.
// It resolves target Hub endpoints for cascade push based on audience type and IDs.
type hubServiceNotifResolver struct {
	hubService *hubs.Service
}

func (r *hubServiceNotifResolver) AllHubs(ctx context.Context) ([]notification.HubEndpoint, error) {
	instances, err := r.hubService.ListHubs(ctx)
	if err != nil {
		return nil, err
	}
	return filterActiveHubEndpoints(instances), nil
}

func (r *hubServiceNotifResolver) HubsByIDs(ctx context.Context, hubIDs []string) ([]notification.HubEndpoint, error) {
	instances, err := r.hubService.ListHubs(ctx)
	if err != nil {
		return nil, err
	}
	idSet := make(map[string]struct{}, len(hubIDs))
	for _, id := range hubIDs {
		idSet[strings.TrimSpace(id)] = struct{}{}
	}
	var endpoints []notification.HubEndpoint
	for _, hub := range instances {
		if _, ok := idSet[hub.ID]; ok {
			if ep, ok := toHubEndpoint(hub); ok {
				endpoints = append(endpoints, ep)
			}
		}
	}
	return endpoints, nil
}

func (r *hubServiceNotifResolver) HubsByTenantPairs(ctx context.Context, pairs []string) ([]notification.HubEndpoint, error) {
	// Extract unique hub IDs from "hub_id:tenant_id" pairs.
	hubIDSet := make(map[string]struct{}, len(pairs))
	for _, pair := range pairs {
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) >= 1 {
			hubID := strings.TrimSpace(parts[0])
			if hubID != "" {
				hubIDSet[hubID] = struct{}{}
			}
		}
	}
	hubIDs := make([]string, 0, len(hubIDSet))
	for id := range hubIDSet {
		hubIDs = append(hubIDs, id)
	}
	return r.HubsByIDs(ctx, hubIDs)
}

// filterActiveHubEndpoints filters hub instances that are active and have a valid URL.
func filterActiveHubEndpoints(instances []*store.HubInstance) []notification.HubEndpoint {
	var endpoints []notification.HubEndpoint
	for _, hub := range instances {
		if ep, ok := toHubEndpoint(hub); ok {
			endpoints = append(endpoints, ep)
		}
	}
	return endpoints
}

// toHubEndpoint converts a store.HubInstance to a notification.HubEndpoint.
// Returns false if the hub is disabled or has no usable URL.
func toHubEndpoint(hub *store.HubInstance) (notification.HubEndpoint, bool) {
	if hub == nil || hub.IsDisabled {
		return notification.HubEndpoint{}, false
	}
	url := strings.TrimSpace(hub.BaseURL)
	if url == "" {
		return notification.HubEndpoint{}, false
	}
	// The GlobalAdminToken for cascade push is derived from the hub's
	// installation ID. On the Hub side, requireGlobalAdmin verifies this
	// token against the configured global admin credentials. In production,
	// this would be a pre-shared secret configured per-hub during registration.
	// For now, we use the hub's installation ID as the cascade token — the Hub's
	// cascade endpoint should be configured to accept this.
	token := hub.InstallationID
	return notification.HubEndpoint{
		ID:               hub.ID,
		Name:             hub.Name,
		URL:              url,
		GlobalAdminToken: token,
	}, true
}
