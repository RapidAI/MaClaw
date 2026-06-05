package structureddata

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (s *Service) ListDataTenants(ctx context.Context, p Principal) ([]DataTenantInfo, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	items, err := s.store.ListDataTenants(ctx)
	if err != nil {
		return nil, err
	}
	if principalIsGlobalAdmin(p) {
		return items, nil
	}
	out := make([]DataTenantInfo, 0, 1)
	for _, item := range items {
		if dataTenantMatchesPrincipal(item, p.TenantID) {
			out = append(out, item)
		}
	}
	return out, nil
}

func dataTenantMatchesPrincipal(item DataTenantInfo, tenantID string) bool {
	tenantID = normalizedAdminTenant(tenantID)
	for _, candidate := range []string{item.ID, item.HubTenantID, item.Slug} {
		if strings.EqualFold(normalizedAdminTenant(candidate), tenantID) {
			return true
		}
	}
	return false
}

func (s *Service) SyncHubTenants(ctx context.Context, p Principal, in SyncHubTenantsInput) (*SyncHubTenantsResult, error) {
	if !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	if len(in.Tenants) == 0 {
		return nil, fmt.Errorf("%w: tenants are required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.store.UpsertDataTenants(ctx, in.Tenants, strings.TrimSpace(in.Source), s.now().UTC())
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "admin.tenants_sync", "", "data_tenant", "hub", "Synced tenants from Hub", map[string]any{"synced": len(items)})
	return &SyncHubTenantsResult{Synced: len(items), Tenants: items}, nil
}

func normalizeDataTenantInfo(in DataTenantInfo, source string, now time.Time) (DataTenantInfo, error) {
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = strings.TrimSpace(in.HubTenantID)
	}
	if id == "" {
		return DataTenantInfo{}, fmt.Errorf("%w: tenant id is required", ErrInvalidInput)
	}
	if source == "" {
		source = strings.TrimSpace(in.Source)
	}
	if source == "" {
		source = "hub"
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status == "" {
		status = "active"
	}
	domains := make([]string, 0, len(in.Domains))
	seen := map[string]struct{}{}
	for _, domain := range in.Domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		if _, ok := seen[domain]; ok {
			continue
		}
		seen[domain] = struct{}{}
		domains = append(domains, domain)
	}
	return DataTenantInfo{ID: id, HubTenantID: firstTenantValue(strings.TrimSpace(in.HubTenantID), id), Slug: strings.TrimSpace(in.Slug), Name: trimForStorage(in.Name, 160), Status: status, PrimaryDomain: strings.ToLower(strings.TrimSpace(in.PrimaryDomain)), Domains: domains, VirtualMailDomain: strings.ToLower(strings.Trim(strings.TrimSpace(in.VirtualMailDomain), ".")), Source: source, SyncedAt: now, UpdatedAt: now}, nil
}

func firstTenantValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
