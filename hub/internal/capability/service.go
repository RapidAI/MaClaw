package capability

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

var ErrNotFound = errors.New("capability not found")
var ErrInvalidState = errors.New("capability invalid state")

type Service struct {
	db *sql.DB
}

type tenantContextKey struct{}

// WithTenant scopes capability-market operations to a Hub tenant. Empty tenant
// values fall back to the migration/default tenant for old clients and data.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	tenantID = normalizeTenantID(tenantID)
	return context.WithValue(ctx, tenantContextKey{}, tenantID)
}

func tenantIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return store.DefaultTenantID
	}
	if tenantID, ok := ctx.Value(tenantContextKey{}).(string); ok {
		return normalizeTenantID(tenantID)
	}
	return store.DefaultTenantID
}

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return store.DefaultTenantID
	}
	return tenantID
}

type CapabilitySummary struct {
	TenantID          string `json:"tenant_id,omitempty"`
	ID                string `json:"id"`
	CapabilityType    string `json:"capability_type"`
	Publisher         string `json:"publisher"`
	CapabilityID      string `json:"capability_id"`
	DisplayName       string `json:"display_name"`
	Description       string `json:"description,omitempty"`
	Source            string `json:"source"`
	ManagedBy         string `json:"managed_by"`
	Status            string `json:"status"`
	RelationToOrigin  string `json:"relation_to_origin,omitempty"`
	GlobalKey         string `json:"global_key"`
	CurrentVersionKey string `json:"current_version_key,omitempty"`
	OriginKey         string `json:"origin_key,omitempty"`
	MetadataJSON      string `json:"metadata_json,omitempty"`
}

type Deployment struct {
	TenantID             string `json:"tenant_id,omitempty"`
	ID                   string `json:"id"`
	CapabilityRef        string `json:"capability_ref"`
	CapabilityVersionKey string `json:"capability_version_key,omitempty"`
	ScopeJSON            string `json:"scope_json"`
	DeploymentPolicy     string `json:"deployment_policy"`
	ReinstallIfRemoved   bool   `json:"reinstall_if_removed"`
	RetryIntervalMinutes int    `json:"retry_interval_minutes"`
}

type Recommendation struct {
	TenantID             string `json:"tenant_id,omitempty"`
	ID                   string `json:"id"`
	CapabilityRef        string `json:"capability_ref"`
	CapabilityVersionKey string `json:"capability_version_key,omitempty"`
	ScopeJSON            string `json:"scope_json"`
	Reason               string `json:"recommendation_reason,omitempty"`
	AllowUserDismiss     bool   `json:"allow_user_dismiss"`
}

type UserCapabilityInventoryItem struct {
	TenantID             string `json:"tenant_id,omitempty"`
	ID                   string `json:"id"`
	UserID               string `json:"user_id,omitempty"`
	UserEmail            string `json:"user_email"`
	CapabilityRef        string `json:"capability_ref"`
	CapabilityVersionKey string `json:"capability_version_key,omitempty"`
	CapabilityType       string `json:"capability_type,omitempty"`
	InstallStatus        string `json:"install_status"`
	Installed            bool   `json:"installed"`
	MetadataJSON         string `json:"metadata_json"`
	LastSeenAt           string `json:"last_seen_at"`
}

type UserCapabilityInventoryInput struct {
	UserID               string
	UserEmail            string
	CapabilityRef        string
	CapabilityVersionKey string
	CapabilityType       string
	InstallStatus        string
	Installed            bool
	MetadataJSON         string
	LastSeenAt           string
}

type VersionSummary struct {
	TenantID          string `json:"tenant_id,omitempty"`
	ID                string `json:"id"`
	CapabilityRef     string `json:"capability_ref"`
	Version           string `json:"version"`
	VersionKey        string `json:"version_key"`
	PackageURL        string `json:"package_url,omitempty"`
	PackageChecksum   string `json:"package_checksum,omitempty"`
	PackageSignature  string `json:"package_signature,omitempty"`
	ManifestJSON      string `json:"manifest_json"`
	TypeConfigJSON    string `json:"type_config_json"`
	PermissionsJSON   string `json:"permissions_json"`
	PricingJSON       string `json:"pricing_json"`
	LicenseJSON       string `json:"license_json"`
	CompatibilityJSON string `json:"compatibility_json"`
	Status            string `json:"status"`
}

type AcquisitionRequest struct {
	TenantID            string `json:"tenant_id,omitempty"`
	ID                  string `json:"id"`
	RequesterUserID     string `json:"requester_user_id"`
	CapabilityType      string `json:"capability_type"`
	Source              string `json:"source"`
	SourceCapabilityKey string `json:"source_capability_key"`
	SourceVersionKey    string `json:"source_version_key,omitempty"`
	RequestKind         string `json:"request_kind"`
	Status              string `json:"status"`
	Reason              string `json:"reason,omitempty"`
	PriceJSON           string `json:"price_json"`
	LicenseJSON         string `json:"license_json"`
	HubCustomerID       string `json:"hub_customer_id,omitempty"`
	ApprovalJSON        string `json:"approval_json"`
	PurchaseJSON        string `json:"purchase_json"`
	ResultCapabilityID  string `json:"result_capability_id,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type MCPSecretRequirement struct {
	TenantID      string `json:"tenant_id,omitempty"`
	ID            string `json:"id"`
	CapabilityRef string `json:"capability_ref"`
	VersionKey    string `json:"version_key"`
	Name          string `json:"name"`
	Label         string `json:"label,omitempty"`
	Scope         string `json:"scope"`
	StoragePolicy string `json:"storage_policy"`
	Required      bool   `json:"required"`
	HelpURL       string `json:"help_url,omitempty"`
	MetadataJSON  string `json:"metadata_json"`
}

type MCPHubSecret struct {
	TenantID        string `json:"tenant_id,omitempty"`
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	MCPServerID     string `json:"mcp_server_id"`
	RequirementName string `json:"requirement_name"`
	SecretDigest    string `json:"secret_digest"`
	MetadataJSON    string `json:"metadata_json"`
	UpdatedAt       string `json:"updated_at"`
}

type MCPHubSecretInput struct {
	UserID          string
	MCPServerID     string
	RequirementName string
	SecretValue     string
	MetadataJSON    string
}

type MCPSecretBinding struct {
	TenantID        string `json:"tenant_id,omitempty"`
	ID              string `json:"id"`
	UserID          string `json:"user_id"`
	MCPServerID     string `json:"mcp_server_id"`
	RequirementName string `json:"requirement_name"`
	Storage         string `json:"storage"`
	HubSecretRef    string `json:"hub_secret_ref,omitempty"`
	LocalSecretRef  string `json:"local_secret_ref,omitempty"`
	Status          string `json:"status"`
	LastVerifiedAt  string `json:"last_verified_at,omitempty"`
}

type MCPSecretBindingInput struct {
	UserID          string
	MCPServerID     string
	RequirementName string
	Storage         string
	HubSecretRef    string
	LocalSecretRef  string
	Status          string
	LastVerifiedAt  string
}

func NewService(db *sql.DB) *Service {
	if db == nil {
		return nil
	}
	return &Service{db: db}
}

func (s *Service) List(ctx context.Context, capabilityType string) ([]CapabilitySummary, error) {
	if s == nil || s.db == nil {
		return []CapabilitySummary{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	args := []any{tenantID}
	where := " WHERE tenant_id = ?"
	capabilityType = strings.TrimSpace(capabilityType)
	if capabilityType != "" {
		where += " AND capability_type = ?"
		args = append(args, capabilityType)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, capability_type, publisher, capability_id, display_name, description, source, managed_by, status, relation_to_origin, global_key, current_version_key, origin_key, metadata_json FROM capabilities`+where+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CapabilitySummary{}
	for rows.Next() {
		var item CapabilitySummary
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityType, &item.Publisher, &item.CapabilityID, &item.DisplayName, &item.Description, &item.Source, &item.ManagedBy, &item.Status, &item.RelationToOrigin, &item.GlobalKey, &item.CurrentVersionKey, &item.OriginKey, &item.MetadataJSON); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) Get(ctx context.Context, id string) (*CapabilitySummary, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotFound
	}
	id = strings.TrimSpace(id)
	tenantID := tenantIDFromContext(ctx)
	var item CapabilitySummary
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, id, capability_type, publisher, capability_id, display_name, description, source, managed_by, status, relation_to_origin, global_key, current_version_key, origin_key, metadata_json FROM capabilities WHERE tenant_id = ? AND (id = ? OR global_key = ? OR capability_id = ?) LIMIT 1`, tenantID, id, id, id).Scan(&item.TenantID, &item.ID, &item.CapabilityType, &item.Publisher, &item.CapabilityID, &item.DisplayName, &item.Description, &item.Source, &item.ManagedBy, &item.Status, &item.RelationToOrigin, &item.GlobalKey, &item.CurrentVersionKey, &item.OriginKey, &item.MetadataJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) ListVersions(ctx context.Context, capabilityRef string) ([]VersionSummary, error) {
	if s == nil || s.db == nil {
		return []VersionSummary{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, capability_ref, version, version_key, package_url, package_checksum, package_signature, manifest_json, type_config_json, permissions_json, pricing_json, license_json, compatibility_json, status FROM capability_versions WHERE tenant_id = ? AND capability_ref = ? ORDER BY created_at DESC`, tenantID, strings.TrimSpace(capabilityRef))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []VersionSummary{}
	for rows.Next() {
		var item VersionSummary
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.Version, &item.VersionKey, &item.PackageURL, &item.PackageChecksum, &item.PackageSignature, &item.ManifestJSON, &item.TypeConfigJSON, &item.PermissionsJSON, &item.PricingJSON, &item.LicenseJSON, &item.CompatibilityJSON, &item.Status); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type UpsertCapabilityInput struct {
	ID                string
	CapabilityType    string
	Publisher         string
	CapabilityID      string
	DisplayName       string
	Description       string
	Source            string
	ManagedBy         string
	Status            string
	RelationToOrigin  string
	GlobalKey         string
	OriginKey         string
	OriginJSON        string
	ProvenanceJSON    string
	MetadataJSON      string
	Version           string
	VersionKey        string
	PackageURL        string
	PackageChecksum   string
	PackageSignature  string
	ManifestJSON      string
	TypeConfigJSON    string
	PermissionsJSON   string
	PricingJSON       string
	LicenseJSON       string
	CompatibilityJSON string
	VersionStatus     string
	SetCurrentVersion bool
}

func (s *Service) UpsertCapability(ctx context.Context, in UpsertCapabilityInput) (*CapabilitySummary, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("capability service is not configured")
	}
	in = normalizeUpsertInput(in)
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var existingID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM capabilities WHERE tenant_id = ? AND (id = ? OR global_key = ?) LIMIT 1`, tenantID, in.ID, in.GlobalKey).Scan(&existingID)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO capabilities (tenant_id, id, capability_type, publisher, capability_id, display_name, description, source, managed_by, status, relation_to_origin, global_key, current_version_key, origin_key, origin_json, provenance_json, metadata_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenantID, in.ID, in.CapabilityType, in.Publisher, in.CapabilityID, in.DisplayName, in.Description, in.Source, in.ManagedBy, in.Status, in.RelationToOrigin, in.GlobalKey, currentVersionKey(in), in.OriginKey, in.OriginJSON, in.ProvenanceJSON, in.MetadataJSON, now, now)
		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else {
		in.ID = existingID
		_, err = tx.ExecContext(ctx, `UPDATE capabilities SET capability_type = ?, publisher = ?, capability_id = ?, display_name = ?, description = ?, source = ?, managed_by = ?, status = ?, relation_to_origin = ?, global_key = ?, current_version_key = CASE WHEN ? <> '' THEN ? ELSE current_version_key END, origin_key = ?, origin_json = ?, provenance_json = ?, metadata_json = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, in.CapabilityType, in.Publisher, in.CapabilityID, in.DisplayName, in.Description, in.Source, in.ManagedBy, in.Status, in.RelationToOrigin, in.GlobalKey, currentVersionKey(in), currentVersionKey(in), in.OriginKey, in.OriginJSON, in.ProvenanceJSON, in.MetadataJSON, now, tenantID, in.ID)
		if err != nil {
			return nil, err
		}
	}

	if strings.TrimSpace(in.Version) != "" {
		versionID := newID("cap_ver")
		_, err = tx.ExecContext(ctx, `INSERT INTO capability_versions (tenant_id, id, capability_ref, version, version_key, package_url, package_checksum, package_signature, manifest_json, type_config_json, permissions_json, pricing_json, license_json, compatibility_json, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(tenant_id, version_key) DO UPDATE SET capability_ref = excluded.capability_ref, package_url = excluded.package_url, package_checksum = excluded.package_checksum, package_signature = excluded.package_signature, manifest_json = excluded.manifest_json, type_config_json = excluded.type_config_json, permissions_json = excluded.permissions_json, pricing_json = excluded.pricing_json, license_json = excluded.license_json, compatibility_json = excluded.compatibility_json, status = excluded.status, updated_at = excluded.updated_at`, tenantID, versionID, in.ID, in.Version, in.VersionKey, in.PackageURL, in.PackageChecksum, in.PackageSignature, in.ManifestJSON, in.TypeConfigJSON, in.PermissionsJSON, in.PricingJSON, in.LicenseJSON, in.CompatibilityJSON, in.VersionStatus, now, now)
		if err != nil {
			return nil, err
		}
		if in.SetCurrentVersion {
			if _, err := tx.ExecContext(ctx, `UPDATE capabilities SET current_version_key = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, in.VersionKey, now, tenantID, in.ID); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, in.ID)
}

func (s *Service) SetCapabilityStatus(ctx context.Context, id, status string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	res, err := s.db.ExecContext(ctx, `UPDATE capabilities SET status = ?, updated_at = ? WHERE tenant_id = ? AND (id = ? OR global_key = ? OR capability_id = ?)`, strings.TrimSpace(status), time.Now().UTC().Format(time.RFC3339), tenantID, strings.TrimSpace(id), strings.TrimSpace(id), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ReviewCapabilityVersion(ctx context.Context, id, status, metadataJSON string) (*CapabilitySummary, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("capability service is not configured")
	}
	id = strings.TrimSpace(id)
	status = strings.TrimSpace(status)
	if id == "" || status == "" {
		return nil, ErrNotFound
	}
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var capabilityID, currentVersionKey string
	err = tx.QueryRowContext(ctx, `SELECT id, current_version_key FROM capabilities WHERE tenant_id = ? AND (id = ? OR global_key = ? OR capability_id = ?) LIMIT 1`, tenantID, id, id, id).Scan(&capabilityID, &currentVersionKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(metadataJSON) == "" {
		_, err = tx.ExecContext(ctx, `UPDATE capabilities SET status = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, status, now, tenantID, capabilityID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE capabilities SET status = ?, metadata_json = ?, updated_at = ? WHERE tenant_id = ? AND id = ?`, status, metadataJSON, now, tenantID, capabilityID)
	}
	if err != nil {
		return nil, err
	}
	if currentVersionKey != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE capability_versions SET status = ?, updated_at = ? WHERE tenant_id = ? AND version_key = ?`, status, now, tenantID, currentVersionKey); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Get(ctx, capabilityID)
}

func (s *Service) ListManagedDeployments(ctx context.Context) ([]Deployment, error) {
	if s == nil || s.db == nil {
		return []Deployment{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, capability_ref, capability_version_key, scope_json, deployment_policy, reinstall_if_removed, retry_interval_minutes FROM managed_capability_deployments WHERE tenant_id = ? AND enabled = 1 ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Deployment{}
	for rows.Next() {
		var item Deployment
		var reinstall int
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.CapabilityVersionKey, &item.ScopeJSON, &item.DeploymentPolicy, &reinstall, &item.RetryIntervalMinutes); err != nil {
			return nil, err
		}
		item.DeploymentPolicy = NormalizeManagedDeploymentPolicy(item.DeploymentPolicy)
		item.ReinstallIfRemoved = reinstall != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListRecommendations(ctx context.Context) ([]Recommendation, error) {
	if s == nil || s.db == nil {
		return []Recommendation{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, capability_ref, capability_version_key, scope_json, recommendation_reason, allow_user_dismiss FROM recommended_capabilities WHERE tenant_id = ? AND enabled = 1 ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Recommendation{}
	for rows.Next() {
		var item Recommendation
		var allowDismiss int
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.CapabilityVersionKey, &item.ScopeJSON, &item.Reason, &allowDismiss); err != nil {
			return nil, err
		}
		item.AllowUserDismiss = allowDismiss != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

type AcquisitionRequestInput struct {
	RequesterUserID     string
	CapabilityType      string
	Source              string
	SourceCapabilityKey string
	SourceVersionKey    string
	RequestKind         string
	Status              string
	Reason              string
	PriceJSON           string
	LicenseJSON         string
	HubCustomerID       string
}

func (s *Service) CreateAcquisitionRequest(ctx context.Context, in AcquisitionRequestInput) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("capability service is not configured")
	}
	id := newID("cap_req")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.RequesterUserID) == "" {
		in.RequesterUserID = "anonymous"
	}
	if strings.TrimSpace(in.Status) == "" {
		in.Status = "pending_review"
	}
	if strings.TrimSpace(in.PriceJSON) == "" {
		in.PriceJSON = "{}"
	}
	if strings.TrimSpace(in.LicenseJSON) == "" {
		in.LicenseJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO capability_acquisition_requests (tenant_id, id, requester_user_id, capability_type, source, source_capability_key, source_version_key, request_kind, status, reason, price_json, license_json, hub_customer_id, approval_json, purchase_json, result_capability_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', '{}', '', ?, ?)`, tenantID, id, in.RequesterUserID, in.CapabilityType, in.Source, in.SourceCapabilityKey, in.SourceVersionKey, in.RequestKind, in.Status, in.Reason, in.PriceJSON, in.LicenseJSON, in.HubCustomerID, now, now)
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) ListAcquisitionRequests(ctx context.Context, status string) ([]AcquisitionRequest, error) {
	if s == nil || s.db == nil {
		return []AcquisitionRequest{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	args := []any{tenantID}
	where := " WHERE tenant_id = ?"
	if strings.TrimSpace(status) != "" {
		where += " AND status = ?"
		args = append(args, strings.TrimSpace(status))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, requester_user_id, capability_type, source, source_capability_key, source_version_key, request_kind, status, reason, price_json, license_json, hub_customer_id, approval_json, purchase_json, result_capability_id, created_at, updated_at FROM capability_acquisition_requests`+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AcquisitionRequest{}
	for rows.Next() {
		var item AcquisitionRequest
		if err := rows.Scan(&item.TenantID, &item.ID, &item.RequesterUserID, &item.CapabilityType, &item.Source, &item.SourceCapabilityKey, &item.SourceVersionKey, &item.RequestKind, &item.Status, &item.Reason, &item.PriceJSON, &item.LicenseJSON, &item.HubCustomerID, &item.ApprovalJSON, &item.PurchaseJSON, &item.ResultCapabilityID, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) GetAcquisitionRequest(ctx context.Context, requestID string) (*AcquisitionRequest, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotFound
	}
	var item AcquisitionRequest
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, id, requester_user_id, capability_type, source, source_capability_key, source_version_key, request_kind, status, reason, price_json, license_json, hub_customer_id, approval_json, purchase_json, result_capability_id, created_at, updated_at FROM capability_acquisition_requests WHERE tenant_id = ? AND id = ? LIMIT 1`, tenantID, strings.TrimSpace(requestID)).Scan(&item.TenantID, &item.ID, &item.RequesterUserID, &item.CapabilityType, &item.Source, &item.SourceCapabilityKey, &item.SourceVersionKey, &item.RequestKind, &item.Status, &item.Reason, &item.PriceJSON, &item.LicenseJSON, &item.HubCustomerID, &item.ApprovalJSON, &item.PurchaseJSON, &item.ResultCapabilityID, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) ApproveAcquisitionRequest(ctx context.Context, requestID, adminUserID, approvalJSON string) error {
	return s.updateAcquisitionStatus(ctx, requestID, "approved", approvalJSON, "")
}

func (s *Service) RejectAcquisitionRequest(ctx context.Context, requestID, adminUserID, approvalJSON string) error {
	return s.updateAcquisitionStatus(ctx, requestID, "rejected", approvalJSON, "")
}

func (s *Service) CompleteAcquisitionRequest(ctx context.Context, requestID, resultCapabilityID, purchaseJSON string) error {
	return s.updateAcquisitionStatus(ctx, requestID, "completed", "", purchaseJSON, resultCapabilityID)
}

func (s *Service) updateAcquisitionStatus(ctx context.Context, requestID, status, approvalJSON, purchaseJSON string, resultCapabilityID ...string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	if strings.TrimSpace(approvalJSON) == "" {
		approvalJSON = "{}"
	}
	if strings.TrimSpace(purchaseJSON) == "" {
		purchaseJSON = "{}"
	}
	resultID := ""
	if len(resultCapabilityID) > 0 {
		resultID = strings.TrimSpace(resultCapabilityID[0])
	}
	requestID = strings.TrimSpace(requestID)
	tenantID := tenantIDFromContext(ctx)
	res, err := s.db.ExecContext(ctx, `UPDATE capability_acquisition_requests SET status = ?, approval_json = CASE WHEN ? <> '{}' THEN ? ELSE approval_json END, purchase_json = CASE WHEN ? <> '{}' THEN ? ELSE purchase_json END, result_capability_id = CASE WHEN ? <> '' THEN ? ELSE result_capability_id END, updated_at = ? WHERE tenant_id = ? AND id = ? AND LOWER(TRIM(status)) NOT IN ('completed', 'rejected')`, strings.TrimSpace(status), approvalJSON, approvalJSON, purchaseJSON, purchaseJSON, resultID, resultID, time.Now().UTC().Format(time.RFC3339), tenantID, requestID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var currentStatus string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM capability_acquisition_requests WHERE tenant_id = ? AND id = ? LIMIT 1`, tenantID, requestID).Scan(&currentStatus)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if acquisitionStatusTerminal(currentStatus) {
			return ErrInvalidState
		}
		return ErrNotFound
	}
	return nil
}

func acquisitionStatusTerminal(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return status == "completed" || status == "rejected"
}

type ManagedDeploymentInput struct {
	CapabilityRef        string
	CapabilityVersionKey string
	ScopeJSON            string
	DeploymentPolicy     string
	ReinstallIfRemoved   bool
	RetryIntervalMinutes int
	CreatedBy            string
	Enabled              bool
}

// NormalizeManagedDeploymentPolicy maps unknown or empty deployment policies to required.
func NormalizeManagedDeploymentPolicy(policy string) string {
	switch strings.ToLower(strings.TrimSpace(policy)) {
	case "blocked", "recommended":
		return strings.ToLower(strings.TrimSpace(policy))
	default:
		return "required"
	}
}

func (s *Service) CreateManagedDeployment(ctx context.Context, in ManagedDeploymentInput) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("capability service is not configured")
	}
	id := newID("cap_dep")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.ScopeJSON) == "" {
		in.ScopeJSON = "{}"
	}
	in.DeploymentPolicy = NormalizeManagedDeploymentPolicy(in.DeploymentPolicy)
	if in.RetryIntervalMinutes <= 0 {
		in.RetryIntervalMinutes = 60
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		in.CreatedBy = "admin"
	}
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	reinstall := 0
	if in.ReinstallIfRemoved {
		reinstall = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO managed_capability_deployments (tenant_id, id, capability_ref, capability_version_key, scope_json, deployment_policy, reinstall_if_removed, retry_interval_minutes, enabled, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenantID, id, in.CapabilityRef, in.CapabilityVersionKey, in.ScopeJSON, in.DeploymentPolicy, reinstall, in.RetryIntervalMinutes, enabled, in.CreatedBy, now, now)
	return id, err
}

func (s *Service) DisableManagedDeployment(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	res, err := s.db.ExecContext(ctx, `UPDATE managed_capability_deployments SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND id = ?`, time.Now().UTC().Format(time.RFC3339), tenantID, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type RecommendationInput struct {
	CapabilityRef        string
	CapabilityVersionKey string
	ScopeJSON            string
	Reason               string
	AllowUserDismiss     bool
	CreatedBy            string
	Enabled              bool
}

func (s *Service) CreateRecommendation(ctx context.Context, in RecommendationInput) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("capability service is not configured")
	}
	id := newID("cap_rec")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.ScopeJSON) == "" {
		in.ScopeJSON = "{}"
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		in.CreatedBy = "admin"
	}
	enabled := 0
	if in.Enabled {
		enabled = 1
	}
	allowDismiss := 0
	if in.AllowUserDismiss {
		allowDismiss = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO recommended_capabilities (tenant_id, id, capability_ref, capability_version_key, scope_json, recommendation_reason, allow_user_dismiss, enabled, created_by, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, tenantID, id, in.CapabilityRef, in.CapabilityVersionKey, in.ScopeJSON, in.Reason, allowDismiss, enabled, in.CreatedBy, now, now)
	return id, err
}

func (s *Service) DisableRecommendation(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return errors.New("capability service is not configured")
	}
	tenantID := tenantIDFromContext(ctx)
	res, err := s.db.ExecContext(ctx, `UPDATE recommended_capabilities SET enabled = 0, updated_at = ? WHERE tenant_id = ? AND id = ?`, time.Now().UTC().Format(time.RFC3339), tenantID, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) UpsertUserCapabilityInventory(ctx context.Context, in UserCapabilityInventoryInput) (*UserCapabilityInventoryItem, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("capability service is not configured")
	}
	in.UserEmail = strings.ToLower(strings.TrimSpace(in.UserEmail))
	in.CapabilityRef = strings.TrimSpace(in.CapabilityRef)
	if in.UserEmail == "" || in.CapabilityRef == "" {
		return nil, errors.New("user_email and capability_ref are required")
	}
	if strings.TrimSpace(in.InstallStatus) == "" {
		if in.Installed {
			in.InstallStatus = "installed"
		} else {
			in.InstallStatus = "missing"
		}
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.LastSeenAt) == "" {
		in.LastSeenAt = now
	}
	tenantID := tenantIDFromContext(ctx)
	id := newID("cap_inv")
	installed := 0
	if in.Installed {
		installed = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_capability_inventory (tenant_id, id, user_id, user_email, capability_ref, capability_version_key, capability_type, install_status, installed, metadata_json, last_seen_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(tenant_id, user_email, capability_ref) DO UPDATE SET user_id = excluded.user_id, capability_version_key = excluded.capability_version_key, capability_type = excluded.capability_type, install_status = excluded.install_status, installed = excluded.installed, metadata_json = excluded.metadata_json, last_seen_at = excluded.last_seen_at, updated_at = excluded.updated_at`, tenantID, id, strings.TrimSpace(in.UserID), in.UserEmail, in.CapabilityRef, strings.TrimSpace(in.CapabilityVersionKey), strings.TrimSpace(in.CapabilityType), strings.TrimSpace(in.InstallStatus), installed, in.MetadataJSON, in.LastSeenAt, now, now)
	if err != nil {
		return nil, err
	}
	return s.GetUserCapabilityInventoryItem(ctx, in.UserEmail, in.CapabilityRef)
}

func (s *Service) GetUserCapabilityInventoryItem(ctx context.Context, email, capabilityRef string) (*UserCapabilityInventoryItem, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotFound
	}
	var item UserCapabilityInventoryItem
	var installed int
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, id, user_id, user_email, capability_ref, capability_version_key, capability_type, install_status, installed, metadata_json, last_seen_at FROM user_capability_inventory WHERE tenant_id = ? AND user_email = ? AND capability_ref = ? LIMIT 1`, tenantID, strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(capabilityRef)).Scan(&item.TenantID, &item.ID, &item.UserID, &item.UserEmail, &item.CapabilityRef, &item.CapabilityVersionKey, &item.CapabilityType, &item.InstallStatus, &installed, &item.MetadataJSON, &item.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Installed = installed != 0
	return &item, nil
}

func (s *Service) ListUserCapabilityInventory(ctx context.Context, email string) ([]UserCapabilityInventoryItem, error) {
	if s == nil || s.db == nil {
		return []UserCapabilityInventoryItem{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, user_id, user_email, capability_ref, capability_version_key, capability_type, install_status, installed, metadata_json, last_seen_at FROM user_capability_inventory WHERE tenant_id = ? AND user_email = ? ORDER BY last_seen_at DESC`, tenantID, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []UserCapabilityInventoryItem{}
	for rows.Next() {
		var item UserCapabilityInventoryItem
		var installed int
		if err := rows.Scan(&item.TenantID, &item.ID, &item.UserID, &item.UserEmail, &item.CapabilityRef, &item.CapabilityVersionKey, &item.CapabilityType, &item.InstallStatus, &installed, &item.MetadataJSON, &item.LastSeenAt); err != nil {
			return nil, err
		}
		item.Installed = installed != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) MarkUserCapabilityInventoryMissingExcept(ctx context.Context, email string, capabilityRefs []string) error {
	if s == nil || s.db == nil {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return errors.New("user_email is required")
	}
	keep := map[string]bool{}
	for _, ref := range capabilityRefs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			keep[ref] = true
		}
	}
	items, err := s.ListUserCapabilityInventory(ctx, email)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tenantID := tenantIDFromContext(ctx)
	for _, item := range items {
		if keep[item.CapabilityRef] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE user_capability_inventory SET installed = 0, install_status = 'missing', last_seen_at = ?, updated_at = ? WHERE tenant_id = ? AND user_email = ? AND capability_ref = ?`, now, now, tenantID, email, item.CapabilityRef); err != nil {
			return err
		}
	}
	return nil
}

type MCPSecretRequirementInput struct {
	CapabilityRef string
	VersionKey    string
	Name          string
	Label         string
	Scope         string
	StoragePolicy string
	Required      bool
	HelpURL       string
	MetadataJSON  string
}

func (s *Service) UpsertMCPSecretRequirement(ctx context.Context, in MCPSecretRequirementInput) (string, error) {
	if s == nil || s.db == nil {
		return "", errors.New("capability service is not configured")
	}
	id := newID("mcp_req")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(in.Scope) == "" {
		in.Scope = "user"
	}
	if strings.TrimSpace(in.StoragePolicy) == "" {
		in.StoragePolicy = "hub_or_local"
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	required := 0
	if in.Required {
		required = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_secret_requirements (tenant_id, id, capability_ref, version_key, name, label, scope, storage_policy, required, help_url, metadata_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(tenant_id, capability_ref, version_key, name) DO UPDATE SET label = excluded.label, scope = excluded.scope, storage_policy = excluded.storage_policy, required = excluded.required, help_url = excluded.help_url, metadata_json = excluded.metadata_json, updated_at = excluded.updated_at`, tenantID, id, in.CapabilityRef, in.VersionKey, in.Name, in.Label, in.Scope, in.StoragePolicy, required, in.HelpURL, in.MetadataJSON, now, now)
	return id, err
}

func (s *Service) ListMCPSecretRequirements(ctx context.Context, capabilityRef, versionKey string) ([]MCPSecretRequirement, error) {
	if s == nil || s.db == nil {
		return []MCPSecretRequirement{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, capability_ref, version_key, name, label, scope, storage_policy, required, help_url, metadata_json FROM mcp_secret_requirements WHERE tenant_id = ? AND capability_ref = ? AND (? = '' OR version_key = ?) ORDER BY name`, tenantID, strings.TrimSpace(capabilityRef), strings.TrimSpace(versionKey), strings.TrimSpace(versionKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MCPSecretRequirement{}
	for rows.Next() {
		var item MCPSecretRequirement
		var required int
		if err := rows.Scan(&item.TenantID, &item.ID, &item.CapabilityRef, &item.VersionKey, &item.Name, &item.Label, &item.Scope, &item.StoragePolicy, &required, &item.HelpURL, &item.MetadataJSON); err != nil {
			return nil, err
		}
		item.Required = required != 0
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) UpsertMCPHubSecret(ctx context.Context, in MCPHubSecretInput) (*MCPHubSecret, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("capability service is not configured")
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.MCPServerID = strings.TrimSpace(in.MCPServerID)
	in.RequirementName = strings.TrimSpace(in.RequirementName)
	if in.UserID == "" || in.MCPServerID == "" || in.RequirementName == "" {
		return nil, errors.New("user_id, mcp_server_id and requirement_name are required")
	}
	if strings.TrimSpace(in.SecretValue) == "" {
		return nil, errors.New("secret_value is required")
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	secretValue := base64.StdEncoding.EncodeToString([]byte(in.SecretValue))
	digestBytes := sha256.Sum256([]byte(in.SecretValue))
	digest := hex.EncodeToString(digestBytes[:])
	id := newID("hub_secret")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_hub_secrets (tenant_id, id, user_id, mcp_server_id, requirement_name, secret_value, secret_digest, metadata_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(tenant_id, user_id, mcp_server_id, requirement_name) DO UPDATE SET secret_value = excluded.secret_value, secret_digest = excluded.secret_digest, metadata_json = excluded.metadata_json, updated_at = excluded.updated_at`, tenantID, id, in.UserID, in.MCPServerID, in.RequirementName, secretValue, digest, in.MetadataJSON, now, now)
	if err != nil {
		return nil, err
	}
	if _, err := s.UpsertMCPSecretBinding(ctx, MCPSecretBindingInput{UserID: in.UserID, MCPServerID: in.MCPServerID, RequirementName: in.RequirementName, Storage: "hub", HubSecretRef: "hub://mcp-secrets/" + in.MCPServerID + "/" + in.RequirementName, Status: "configured"}); err != nil {
		return nil, err
	}
	return s.GetMCPHubSecret(ctx, in.UserID, in.MCPServerID, in.RequirementName)
}

func (s *Service) GetMCPHubSecret(ctx context.Context, userID, mcpServerID, requirementName string) (*MCPHubSecret, error) {
	if s == nil || s.db == nil {
		return nil, ErrNotFound
	}
	var item MCPHubSecret
	tenantID := tenantIDFromContext(ctx)
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, id, user_id, mcp_server_id, requirement_name, secret_digest, metadata_json, updated_at FROM mcp_hub_secrets WHERE tenant_id = ? AND user_id = ? AND mcp_server_id = ? AND requirement_name = ? LIMIT 1`, tenantID, strings.TrimSpace(userID), strings.TrimSpace(mcpServerID), strings.TrimSpace(requirementName)).Scan(&item.TenantID, &item.ID, &item.UserID, &item.MCPServerID, &item.RequirementName, &item.SecretDigest, &item.MetadataJSON, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Service) ListMCPHubSecrets(ctx context.Context, userID, mcpServerID string) ([]MCPHubSecret, error) {
	if s == nil || s.db == nil {
		return []MCPHubSecret{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	args := []any{tenantID, strings.TrimSpace(userID)}
	where := " WHERE tenant_id = ? AND user_id = ?"
	if strings.TrimSpace(mcpServerID) != "" {
		where += " AND mcp_server_id = ?"
		args = append(args, strings.TrimSpace(mcpServerID))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, user_id, mcp_server_id, requirement_name, secret_digest, metadata_json, updated_at FROM mcp_hub_secrets`+where+` ORDER BY mcp_server_id, requirement_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MCPHubSecret{}
	for rows.Next() {
		var item MCPHubSecret
		if err := rows.Scan(&item.TenantID, &item.ID, &item.UserID, &item.MCPServerID, &item.RequirementName, &item.SecretDigest, &item.MetadataJSON, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) ListMCPSecretBindings(ctx context.Context, userID, mcpServerID string) ([]MCPSecretBinding, error) {
	if s == nil || s.db == nil {
		return []MCPSecretBinding{}, nil
	}
	tenantID := tenantIDFromContext(ctx)
	args := []any{tenantID, strings.TrimSpace(userID)}
	where := " WHERE tenant_id = ? AND user_id = ?"
	if strings.TrimSpace(mcpServerID) != "" {
		where += " AND mcp_server_id = ?"
		args = append(args, strings.TrimSpace(mcpServerID))
	}
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, id, user_id, mcp_server_id, requirement_name, storage, hub_secret_ref, local_secret_ref, status, last_verified_at FROM mcp_secret_bindings`+where+` ORDER BY mcp_server_id, requirement_name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []MCPSecretBinding{}
	for rows.Next() {
		var item MCPSecretBinding
		if err := rows.Scan(&item.TenantID, &item.ID, &item.UserID, &item.MCPServerID, &item.RequirementName, &item.Storage, &item.HubSecretRef, &item.LocalSecretRef, &item.Status, &item.LastVerifiedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) UpsertMCPSecretBinding(ctx context.Context, in MCPSecretBindingInput) (*MCPSecretBinding, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("capability service is not configured")
	}
	in.UserID = strings.TrimSpace(in.UserID)
	in.MCPServerID = strings.TrimSpace(in.MCPServerID)
	in.RequirementName = strings.TrimSpace(in.RequirementName)
	if in.UserID == "" || in.MCPServerID == "" || in.RequirementName == "" {
		return nil, errors.New("user_id, mcp_server_id and requirement_name are required")
	}
	if strings.TrimSpace(in.Storage) == "" {
		in.Storage = "local"
	}
	if strings.TrimSpace(in.Status) == "" {
		in.Status = "configured"
	}
	in.Storage = strings.TrimSpace(strings.ToLower(in.Storage))
	if in.Storage == "hub" && (strings.TrimSpace(in.HubSecretRef) != "" || strings.EqualFold(strings.TrimSpace(in.Status), "configured") || strings.EqualFold(strings.TrimSpace(in.Status), "ready")) {
		secret, err := s.GetMCPHubSecret(ctx, in.UserID, in.MCPServerID, in.RequirementName)
		if err != nil {
			return nil, fmt.Errorf("hub secret is required before configuring hub secret binding: %w", err)
		}
		if strings.TrimSpace(secret.SecretDigest) == "" {
			return nil, errors.New("hub secret digest is required before configuring hub secret binding")
		}
	}
	id := newID("mcp_secret")
	tenantID := tenantIDFromContext(ctx)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `INSERT INTO mcp_secret_bindings (tenant_id, id, user_id, mcp_server_id, requirement_name, storage, hub_secret_ref, local_secret_ref, status, last_verified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(tenant_id, user_id, mcp_server_id, requirement_name) DO UPDATE SET storage = excluded.storage, hub_secret_ref = excluded.hub_secret_ref, local_secret_ref = excluded.local_secret_ref, status = excluded.status, last_verified_at = excluded.last_verified_at, updated_at = excluded.updated_at`, tenantID, id, in.UserID, in.MCPServerID, in.RequirementName, in.Storage, in.HubSecretRef, in.LocalSecretRef, in.Status, in.LastVerifiedAt, now, now)
	if err != nil {
		return nil, err
	}
	var item MCPSecretBinding
	err = s.db.QueryRowContext(ctx, `SELECT tenant_id, id, user_id, mcp_server_id, requirement_name, storage, hub_secret_ref, local_secret_ref, status, last_verified_at FROM mcp_secret_bindings WHERE tenant_id = ? AND user_id = ? AND mcp_server_id = ? AND requirement_name = ?`, tenantID, in.UserID, in.MCPServerID, in.RequirementName).Scan(&item.TenantID, &item.ID, &item.UserID, &item.MCPServerID, &item.RequirementName, &item.Storage, &item.HubSecretRef, &item.LocalSecretRef, &item.Status, &item.LastVerifiedAt)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func normalizeUpsertInput(in UpsertCapabilityInput) UpsertCapabilityInput {
	if strings.TrimSpace(in.ID) == "" {
		in.ID = newID("cap")
	}
	if strings.TrimSpace(in.CapabilityType) == "" {
		in.CapabilityType = "skill"
	}
	if strings.TrimSpace(in.Publisher) == "" {
		in.Publisher = "enterprise"
	}
	if strings.TrimSpace(in.CapabilityID) == "" {
		in.CapabilityID = in.ID
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		in.DisplayName = in.CapabilityID
	}
	if strings.TrimSpace(in.Source) == "" {
		in.Source = "enterprise_hub"
	}
	if strings.TrimSpace(in.ManagedBy) == "" {
		in.ManagedBy = "admin"
	}
	if strings.TrimSpace(in.Status) == "" {
		in.Status = "draft"
	}
	if strings.TrimSpace(in.GlobalKey) == "" {
		in.GlobalKey = in.Source + ":" + in.CapabilityType + ":" + in.Publisher + ":" + in.CapabilityID
	}
	if strings.TrimSpace(in.OriginJSON) == "" {
		in.OriginJSON = "{}"
	}
	if strings.TrimSpace(in.ProvenanceJSON) == "" {
		in.ProvenanceJSON = "{}"
	}
	if strings.TrimSpace(in.MetadataJSON) == "" {
		in.MetadataJSON = "{}"
	}
	if strings.TrimSpace(in.Version) != "" {
		if strings.TrimSpace(in.VersionKey) == "" {
			in.VersionKey = in.GlobalKey + "@" + in.Version
		}
		if strings.TrimSpace(in.ManifestJSON) == "" {
			in.ManifestJSON = "{}"
		}
		if strings.TrimSpace(in.TypeConfigJSON) == "" {
			in.TypeConfigJSON = "{}"
		}
		if strings.TrimSpace(in.PermissionsJSON) == "" {
			in.PermissionsJSON = "{}"
		}
		if strings.TrimSpace(in.PricingJSON) == "" {
			in.PricingJSON = "{}"
		}
		if strings.TrimSpace(in.LicenseJSON) == "" {
			in.LicenseJSON = "{}"
		}
		if strings.TrimSpace(in.CompatibilityJSON) == "" {
			in.CompatibilityJSON = "{}"
		}
		if strings.TrimSpace(in.VersionStatus) == "" {
			in.VersionStatus = "approved"
		}
	}
	return in
}

func currentVersionKey(in UpsertCapabilityInput) string {
	if in.SetCurrentVersion && strings.TrimSpace(in.VersionKey) != "" {
		return in.VersionKey
	}
	return ""
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_fallback"
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
