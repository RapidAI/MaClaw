package llmservice

import (
	"context"
	"fmt"
	"time"
)

// TenantAuthorization represents a tenant's LLM credits allocation
// bound to a specific service group.
type TenantAuthorization struct {
	ID                     string    `json:"id"`
	HubID                  string    `json:"hub_id"`
	TenantID               string    `json:"tenant_id"`
	AdminEmail             string    `json:"admin_email"`
	ServiceGroupID         string    `json:"service_group_id"`
	CreditsTotal           float64   `json:"credits_total"`
	CreditsUsed            float64   `json:"credits_used"`
	StartsAt               time.Time `json:"starts_at"`
	ExpiresAt              time.Time `json:"expires_at"`
	Status                 string    `json:"status"` // "active" / "expired" / "exhausted"
	Source                 string    `json:"source"` // "card" / "admin_grant"
	CardOrderID            string    `json:"card_order_id,omitempty"`
	AllowExternalProviders bool      `json:"allow_external_providers"`
	BoundNodeID            string    `json:"bound_node_id,omitempty"`
	BoundAt                time.Time `json:"bound_at,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// CreditsRemaining returns available credits for this authorization.
func (a *TenantAuthorization) CreditsRemaining() float64 {
	r := a.CreditsTotal - a.CreditsUsed
	if r < 0 {
		return 0
	}
	return r
}

// IsActive returns true if the authorization is within its validity period
// and has remaining credits.
func (a *TenantAuthorization) IsActive(now time.Time) bool {
	if a.Status == "expired" || a.Status == "exhausted" {
		return false
	}
	if now.Before(a.StartsAt) || now.After(a.ExpiresAt) {
		return false
	}
	return a.CreditsRemaining() > 0
}

// TenantAuthorizationRepository is the storage interface for tenant authorizations.
type TenantAuthorizationRepository interface {
	Create(ctx context.Context, auth *TenantAuthorization) error
	GetByID(ctx context.Context, id string) (*TenantAuthorization, error)
	ListByHubTenant(ctx context.Context, hubID, tenantID string) ([]*TenantAuthorization, error)
	ListByServiceGroup(ctx context.Context, serviceGroupID string) ([]*TenantAuthorization, error)
	ListAll(ctx context.Context) ([]*TenantAuthorization, error)
	Update(ctx context.Context, auth *TenantAuthorization) error
	DeductCredits(ctx context.Context, id string, credits float64, now time.Time) error
}

// AuthorizationChecker validates tenant access to LLM services.
type AuthorizationChecker struct {
	repo TenantAuthorizationRepository
}

// NewAuthorizationChecker creates an authorization checker.
func NewAuthorizationChecker(repo TenantAuthorizationRepository) *AuthorizationChecker {
	return &AuthorizationChecker{repo: repo}
}

// CheckAccess finds an active authorization for the given hub+tenant that
// covers the requested service group. Returns the authorization to deduct from.
func (c *AuthorizationChecker) CheckAccess(ctx context.Context, hubID, tenantID, serviceGroupID string) (*TenantAuthorization, error) {
	auths, err := c.repo.ListByHubTenant(ctx, hubID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list authorizations: %w", err)
	}
	now := time.Now().UTC()
	for _, auth := range auths {
		if !auth.IsActive(now) {
			continue
		}
		if auth.ServiceGroupID == serviceGroupID {
			return auth, nil
		}
	}
	return nil, fmt.Errorf("no active authorization for hub=%s tenant=%s group=%s", hubID, tenantID, serviceGroupID)
}

// HasExternalProviderAccess checks if a tenant has any authorization with
// AllowExternalProviders=true (needed for Hub to unlock third-party providers).
func (c *AuthorizationChecker) HasExternalProviderAccess(ctx context.Context, hubID, tenantID string) (bool, error) {
	auths, err := c.repo.ListByHubTenant(ctx, hubID, tenantID)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC()
	for _, auth := range auths {
		if auth.IsActive(now) && auth.AllowExternalProviders {
			return true, nil
		}
	}
	return false, nil
}

// DeductCredits subtracts credits from an authorization after a successful LLM request.
func (c *AuthorizationChecker) DeductCredits(ctx context.Context, authID string, credits float64) error {
	return c.repo.DeductCredits(ctx, authID, credits, time.Now().UTC())
}

// ListAll returns all tenant authorizations (admin use).
func (c *AuthorizationChecker) ListAll(ctx context.Context) ([]*TenantAuthorization, error) {
	return c.repo.ListAll(ctx)
}

// ListByServiceGroup returns tenant authorizations bound to a service group.
func (c *AuthorizationChecker) ListByServiceGroup(ctx context.Context, serviceGroupID string) ([]*TenantAuthorization, error) {
	return c.repo.ListByServiceGroup(ctx, serviceGroupID)
}

// CreateAuthorization creates a new tenant authorization (admin grant).
func (c *AuthorizationChecker) CreateAuthorization(ctx context.Context, auth *TenantAuthorization) error {
	return c.repo.Create(ctx, auth)
}
