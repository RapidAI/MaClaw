package llmservice

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

const ExternalComputePermissionServiceGroupID = "__external_compute_permission__"

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
	GetByCardOrderID(ctx context.Context, orderNo string) (*TenantAuthorization, error)
	ListByHubTenant(ctx context.Context, hubID, tenantID string) ([]*TenantAuthorization, error)
	ListByServiceGroup(ctx context.Context, serviceGroupID string) ([]*TenantAuthorization, error)
	ListAll(ctx context.Context) ([]*TenantAuthorization, error)
	Update(ctx context.Context, auth *TenantAuthorization) error
	DeductCredits(ctx context.Context, id string, credits float64, now time.Time) (float64, error)
}

type tenantAuthorizationHubLister interface {
	ListByHub(ctx context.Context, hubID string) ([]*TenantAuthorization, error)
}

// GetAuthorizationByCardOrderID returns the authorization activated by one
// purchase order. It is used to make payment confirmation idempotent.
func GetAuthorizationByCardOrderID(ctx context.Context, repo TenantAuthorizationRepository, orderNo string) (*TenantAuthorization, error) {
	orderNo = strings.TrimSpace(orderNo)
	if repo == nil || orderNo == "" {
		return nil, nil
	}
	return repo.GetByCardOrderID(ctx, orderNo)
}

// CreditDeduction records how a single request charge was applied to a card or
// grant authorization.
type CreditDeduction struct {
	AuthID  string
	Credits float64
}

// InsufficientCreditsError reports a partial or missing card charge for a
// request that already consumed upstream LLM tokens.
type InsufficientCreditsError struct {
	HubID          string
	TenantID       string
	ServiceGroupID string
	Requested      float64
	Deducted       float64
	Remaining      float64
}

func (e *InsufficientCreditsError) Error() string {
	if e == nil {
		return "insufficient credits"
	}
	return fmt.Sprintf("insufficient credits for hub=%s tenant=%s group=%s: deducted %.3f of %.3f, remaining %.3f", e.HubID, e.TenantID, e.ServiceGroupID, e.Deducted, e.Requested, e.Remaining)
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
	auths, err := c.ListByHubTenantAliases(ctx, hubID, tenantID)
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
	auths, err := c.ListByHubTenantAliases(ctx, hubID, tenantID)
	if err != nil {
		return false, err
	}
	if known, allowed := latestExternalProviderAuthorizationState(auths, time.Now().UTC()); known {
		return allowed, nil
	}
	return false, nil
}

func isExternalProviderGrant(auth *TenantAuthorization) bool {
	if auth == nil {
		return false
	}
	if auth.AllowExternalProviders {
		return true
	}
	return auth.Source == "card" && auth.ServiceGroupID == ExternalComputePermissionServiceGroupID
}

func latestExternalProviderAuthorizationState(auths []*TenantAuthorization, current time.Time) (bool, bool) {
	var latestGrant *TenantAuthorization
	var latestRevocation *TenantAuthorization
	for _, auth := range auths {
		if !isExternalProviderAuthorizationState(auth) {
			continue
		}
		if authorizationStateAllowsExternal(auth, current) {
			if latestGrant == nil || compareAuthorizationState(auth, latestGrant, current) > 0 {
				latestGrant = auth
			}
			continue
		}
		if isExternalProviderRevocationState(auth) {
			if latestRevocation == nil || compareAuthorizationState(auth, latestRevocation, current) > 0 {
				latestRevocation = auth
			}
		}
	}
	if latestGrant == nil && latestRevocation == nil {
		return false, false
	}
	if latestGrant == nil {
		return true, false
	}
	if latestRevocation != nil && compareAuthorizationState(latestRevocation, latestGrant, current) > 0 {
		return true, false
	}
	return true, true
}

func compareAuthorizationState(a, b *TenantAuthorization, current time.Time) int {
	if a == b {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	if at, bt := authorizationStateTime(a), authorizationStateTime(b); !at.Equal(bt) {
		if at.After(bt) {
			return 1
		}
		return -1
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		if a.CreatedAt.After(b.CreatedAt) {
			return 1
		}
		return -1
	}
	aAllowed := authorizationStateAllowsExternal(a, current)
	bAllowed := authorizationStateAllowsExternal(b, current)
	if aAllowed != bAllowed {
		if aAllowed {
			return 1
		}
		return -1
	}
	return strings.Compare(a.ID, b.ID)
}

func authorizationStateAllowsExternal(auth *TenantAuthorization, current time.Time) bool {
	if auth == nil {
		return false
	}
	active := auth.IsActive(current)
	if !active && isExternalComputePermissionRecord(auth) && isTimeWindowActive(auth, current) {
		active = true
	}
	return active && isExternalProviderGrant(auth)
}

func isExternalProviderAuthorizationState(auth *TenantAuthorization) bool {
	return auth != nil && (auth.AllowExternalProviders || auth.ServiceGroupID == ExternalComputePermissionServiceGroupID)
}

func isExternalProviderRevocationState(auth *TenantAuthorization) bool {
	if auth == nil || auth.AllowExternalProviders || auth.ServiceGroupID != ExternalComputePermissionServiceGroupID {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(auth.ID), "auth_admin_")
}

func authorizationStateTime(auth *TenantAuthorization) time.Time {
	if auth == nil {
		return time.Time{}
	}
	if !auth.UpdatedAt.IsZero() {
		return auth.UpdatedAt
	}
	if !auth.CreatedAt.IsZero() {
		return auth.CreatedAt
	}
	return auth.StartsAt
}

// DeductCredits subtracts credits from an authorization after a successful LLM request.
func (c *AuthorizationChecker) DeductCredits(ctx context.Context, authID string, credits float64) error {
	_, err := c.repo.DeductCredits(ctx, authID, credits, time.Now().UTC())
	return err
}

// DeductCreditsForServiceGroup subtracts credits from active authorizations for
// the requested hub, tenant, and service group, spreading the charge across
// multiple cards when a single authorization cannot cover the whole request.
func (c *AuthorizationChecker) DeductCreditsForServiceGroup(ctx context.Context, hubID, tenantID, serviceGroupID string, credits float64) ([]CreditDeduction, error) {
	if c == nil || c.repo == nil || credits <= 0 {
		return nil, nil
	}
	auths, err := c.ListByHubTenantAliases(ctx, hubID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list authorizations: %w", err)
	}
	now := time.Now().UTC()
	candidates := make([]*TenantAuthorization, 0, len(auths))
	for _, auth := range auths {
		if auth == nil || !auth.IsActive(now) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(auth.ServiceGroupID), strings.TrimSpace(serviceGroupID)) {
			continue
		}
		candidates = append(candidates, auth)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ExpiresAt.Equal(candidates[j].ExpiresAt) {
			if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
				return candidates[i].ID < candidates[j].ID
			}
			return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
		}
		return candidates[i].ExpiresAt.Before(candidates[j].ExpiresAt)
	})
	remaining := credits
	deductions := make([]CreditDeduction, 0, len(candidates))
	deducted := 0.0
	for _, auth := range candidates {
		available := auth.CreditsRemaining()
		if available <= 0 {
			continue
		}
		use := available
		if remaining < use {
			use = remaining
		}
		if use <= 0 {
			break
		}
		actual, err := c.repo.DeductCredits(ctx, auth.ID, use, now)
		if err != nil {
			return deductions, err
		}
		if actual <= 0 {
			continue
		}
		deductions = append(deductions, CreditDeduction{AuthID: auth.ID, Credits: actual})
		remaining -= actual
		deducted += actual
		if remaining <= 0 {
			break
		}
	}
	if remaining > 1e-9 {
		return deductions, &InsufficientCreditsError{
			HubID:          hubID,
			TenantID:       tenantID,
			ServiceGroupID: serviceGroupID,
			Requested:      credits,
			Deducted:       deducted,
			Remaining:      remaining,
		}
	}
	return deductions, nil
}

// ListAll returns all tenant authorizations (admin use).
func (c *AuthorizationChecker) ListAll(ctx context.Context) ([]*TenantAuthorization, error) {
	return c.repo.ListAll(ctx)
}

// ListByHub returns authorizations for a Hub when the repository supports an
// indexed Hub lookup. It falls back to ListAll for in-memory test repositories.
func (c *AuthorizationChecker) ListByHub(ctx context.Context, hubID string) ([]*TenantAuthorization, error) {
	if repo, ok := c.repo.(tenantAuthorizationHubLister); ok {
		return repo.ListByHub(ctx, hubID)
	}
	all, err := c.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	var out []*TenantAuthorization
	hubID = strings.TrimSpace(hubID)
	for _, auth := range all {
		if auth != nil && strings.TrimSpace(auth.HubID) == hubID {
			out = append(out, auth)
		}
	}
	return out, nil
}

// GetByID returns a single tenant authorization by ID.
func (c *AuthorizationChecker) GetByID(ctx context.Context, id string) (*TenantAuthorization, error) {
	return c.repo.GetByID(ctx, id)
}

// ListByHubTenant returns tenant authorizations for a Hub tenant.
func (c *AuthorizationChecker) ListByHubTenant(ctx context.Context, hubID, tenantID string) ([]*TenantAuthorization, error) {
	return c.repo.ListByHubTenant(ctx, hubID, tenantID)
}

// ListByHubTenantAliases returns authorizations for the requested tenant ID and
// compatible legacy IDs. Hub tenants are usually "tenant_x"; older HubCenter
// grants may store the same tenant as "x".
func (c *AuthorizationChecker) ListByHubTenantAliases(ctx context.Context, hubID, tenantID string) ([]*TenantAuthorization, error) {
	seen := map[string]struct{}{}
	var out []*TenantAuthorization
	for _, candidate := range tenantAuthorizationLookupIDs(tenantID) {
		auths, err := c.repo.ListByHubTenant(ctx, hubID, candidate)
		if err != nil {
			return nil, err
		}
		for _, auth := range auths {
			if auth == nil {
				continue
			}
			key := tenantAuthorizationDedupKey(auth)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, auth)
		}
	}
	return out, nil
}

func tenantAuthorizationDedupKey(auth *TenantAuthorization) string {
	if auth.ID != "" {
		return auth.ID
	}
	return auth.HubID + "\x00" + auth.TenantID + "\x00" + auth.ServiceGroupID + "\x00" + auth.Source
}

func tenantAuthorizationLookupIDs(tenantID string) []string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(id string, out *[]string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		*out = append(*out, id)
	}
	addDefaultStorageID := func(out *[]string) {
		if _, ok := seen[""]; ok {
			return
		}
		seen[""] = struct{}{}
		*out = append(*out, "")
	}
	var out []string
	add(tenantID, &out)
	if tenantID == "tenant_default" {
		add("default", &out)
		addDefaultStorageID(&out)
	}
	if tenantID == "default" {
		add("tenant_default", &out)
		addDefaultStorageID(&out)
	}
	if strings.HasPrefix(tenantID, "tenant_") {
		add(strings.TrimPrefix(tenantID, "tenant_"), &out)
	} else {
		add("tenant_"+tenantID, &out)
	}
	return out
}

// ListByServiceGroup returns tenant authorizations bound to a service group.
func (c *AuthorizationChecker) ListByServiceGroup(ctx context.Context, serviceGroupID string) ([]*TenantAuthorization, error) {
	return c.repo.ListByServiceGroup(ctx, serviceGroupID)
}

// CreateAuthorization creates a new tenant authorization (admin grant).
func (c *AuthorizationChecker) CreateAuthorization(ctx context.Context, auth *TenantAuthorization) error {
	return c.repo.Create(ctx, auth)
}

// UpdateAuthorization updates an existing tenant authorization (admin use).
func (c *AuthorizationChecker) UpdateAuthorization(ctx context.Context, auth *TenantAuthorization) error {
	return c.repo.Update(ctx, auth)
}
