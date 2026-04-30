package centers

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

var (
	ErrNotFound               = errors.New("center not found")
	ErrDisabled               = errors.New("center disabled")
	ErrUnauthorized           = errors.New("unauthorized")
	ErrInvalidServiceIdentity = errors.New("invalid iWorkerCenter service identity")
	ErrProvisionNotAllowed    = errors.New("center provisioning is not allowed")
)

type RegisterRequest struct {
	CompanyName         string `json:"company_name"`
	AdminEmail          string `json:"admin_email"`
	AdminPhone          string `json:"admin_phone"`
	Address             string `json:"address"`
	LegalPerson         string `json:"legal_person"`
	BaseURL             string `json:"base_url"`
	SupportsMultiTenant bool   `json:"supports_multi_tenant"`
	TenantCount         int    `json:"tenant_count"`
	CloudControlMode    string `json:"cloud_control_mode"`
}

type HeartbeatRequest struct {
	Secret       string `json:"secret"`
	RuntimeType  string `json:"runtime_type"`
	ProductKind  string `json:"product_kind"`
	AdminConsole string `json:"admin_console"`
}

func (r HeartbeatRequest) serviceStatus() centerServiceStatus {
	return centerServiceStatus{RuntimeType: r.RuntimeType, ProductKind: r.ProductKind, AdminConsole: r.AdminConsole}
}

type RegisterResult struct {
	CenterID string `json:"center_id"`
	Secret   string `json:"secret"`
	Status   string `json:"status"`
	Message  string `json:"message,omitempty"`
}

type ManagementSummary struct {
	TotalCenters       int `json:"total_centers"`
	PendingCenters     int `json:"pending_centers"`
	ActiveLicenses     int `json:"active_licenses"`
	ReadyCenters       int `json:"ready_centers"`
	NeedsSetup         int `json:"needs_setup"`
	ProbeFailures      int `json:"probe_failures"`
	MultiTenantCenters int `json:"multi_tenant_centers"`
	TenantCount        int `json:"tenant_count"`
	UnlicensedCenters  int `json:"unlicensed_centers"`
}

type CenterManagement struct {
	Center             *store.Center       `json:"center"`
	ActiveLicense      *store.License      `json:"active_license,omitempty"`
	Ready              bool                `json:"ready"`
	Issues             []string            `json:"issues"`
	RecommendedActions []RecommendedAction `json:"recommended_actions"`
	ManagementPosture  string              `json:"management_posture"`
	CommercialStatus   string              `json:"commercial_status"`
	Connectivity       string              `json:"connectivity"`
}

type RecommendedAction struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

// ProvisionReadiness describes whether Cloud may perform service-management tenant provisioning.
type ProvisionReadiness struct {
	Allowed       bool                `json:"allowed"`
	Center        *store.Center       `json:"center"`
	ActiveLicense *store.License      `json:"active_license,omitempty"`
	Issues        []string            `json:"issues"`
	Actions       []RecommendedAction `json:"recommended_actions"`
}
type ManagementReport struct {
	Summary ManagementSummary  `json:"summary"`
	Items   []CenterManagement `json:"items"`
}

type Service struct {
	centers    store.CenterRepository
	licenseSvc *license.Service
	privKey    *rsa.PrivateKey
	httpClient *http.Client
}

func NewService(centers store.CenterRepository, licenseSvc *license.Service) *Service {
	return &Service{centers: centers, licenseSvc: licenseSvc, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

// SetPrivateKey sets the RSA private key for signing provision requests.
func (s *Service) SetPrivateKey(key *rsa.PrivateKey) {
	s.privKey = key
}

// Register creates a new center in pending status.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	if strings.TrimSpace(req.CompanyName) == "" || strings.TrimSpace(req.AdminEmail) == "" {
		return nil, fmt.Errorf("company_name and admin_email are required")
	}

	now := time.Now()
	rawSecret, err := randomToken()
	if err != nil {
		return nil, err
	}

	id := fmt.Sprintf("ctr_%d", now.UnixNano())
	c := &store.Center{
		ID:                  id,
		CompanyName:         strings.TrimSpace(req.CompanyName),
		AdminEmail:          strings.TrimSpace(req.AdminEmail),
		AdminPhone:          strings.TrimSpace(req.AdminPhone),
		Address:             strings.TrimSpace(req.Address),
		LegalPerson:         strings.TrimSpace(req.LegalPerson),
		BaseURL:             strings.TrimSpace(req.BaseURL),
		SupportsMultiTenant: req.SupportsMultiTenant,
		TenantCount:         req.TenantCount,
		CloudControlMode:    normalizeControlMode(req.CloudControlMode),
		LastSyncStatus:      "registered",
		Status:              "pending",
		SecretHash:          hashSecret(rawSecret),
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.centers.Create(ctx, c); err != nil {
		return nil, err
	}

	return &RegisterResult{
		CenterID: id,
		Secret:   rawSecret,
		Status:   "pending",
		Message:  "registration succeeded, waiting for admin approval",
	}, nil
}

// ConfirmWithTrial approves a center and issues a 30-day trial license (email confirm path).
func (s *Service) ConfirmWithTrial(ctx context.Context, centerID string) error {
	if err := s.centers.UpdateStatus(ctx, centerID, "active"); err != nil {
		return err
	}
	_, err := s.licenseSvc.IssueTrial(ctx, centerID)
	return err
}

// ConfirmManual approves a center and issues a manual license with custom duration.
func (s *Service) ConfirmManual(ctx context.Context, centerID string, modules []string, days int) error {
	if err := s.centers.UpdateStatus(ctx, centerID, "active"); err != nil {
		return err
	}
	_, err := s.licenseSvc.IssueManual(ctx, centerID, modules, days)
	return err
}

// UpdateIntegration updates how Cloud connects to a multi-tenant iWorkerCenter deployment.
func (s *Service) UpdateIntegration(ctx context.Context, centerID string, patch store.Center) (*store.Center, error) {
	current, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return nil, ErrNotFound
	}
	current.BaseURL = strings.TrimSpace(patch.BaseURL)
	current.SupportsMultiTenant = patch.SupportsMultiTenant
	if patch.TenantCount >= 0 {
		current.TenantCount = patch.TenantCount
	}
	current.CloudControlMode = normalizeControlMode(patch.CloudControlMode)
	current.LastSyncStatus = strings.TrimSpace(patch.LastSyncStatus)
	if current.LastSyncStatus == "" {
		current.LastSyncStatus = "configured"
	}
	if err := s.centers.UpdateIntegration(ctx, current); err != nil {
		return nil, err
	}
	return s.centers.GetByID(ctx, centerID)
}

// Disable disables a center.
func (s *Service) Disable(ctx context.Context, centerID string) error {
	return s.centers.UpdateStatus(ctx, centerID, "disabled")
}

// Enable re-enables a center.
func (s *Service) Enable(ctx context.Context, centerID string) error {
	return s.centers.UpdateStatus(ctx, centerID, "active")
}

// Heartbeat updates the last heartbeat time after verifying this is a real iWorkerCenter service.
func (s *Service) Heartbeat(ctx context.Context, centerID string, req HeartbeatRequest) error {
	c, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return ErrNotFound
	}
	if c.Status == "disabled" {
		return ErrDisabled
	}
	if c.SecretHash != hashSecret(req.Secret) {
		return ErrUnauthorized
	}
	if !req.serviceStatus().isIWorkerCenterService() {
		c.LastSyncStatus = "heartbeat_not_iworkercenter"
		_ = s.centers.UpdateIntegration(ctx, c)
		return ErrInvalidServiceIdentity
	}
	return s.centers.UpdateHeartbeat(ctx, centerID)
}

// AuthenticateCenter validates the center secret and returns the center record.
// Returns ErrNotFound if the center does not exist, ErrUnauthorized if the secret
// is wrong, and ErrDisabled if the center is disabled (caller should handle 403).
func (s *Service) AuthenticateCenter(ctx context.Context, centerID, rawSecret string) (*store.Center, error) {
	c, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return nil, ErrNotFound
	}
	if c.SecretHash != hashSecret(rawSecret) {
		return nil, ErrUnauthorized
	}
	return c, nil
}

// List returns all centers.
func (s *Service) List(ctx context.Context) ([]*store.Center, error) {
	return s.centers.List(ctx)
}

func (s *Service) Management(ctx context.Context) (*ManagementReport, error) {
	centers, err := s.centers.List(ctx)
	if err != nil {
		return nil, err
	}
	licenses, err := s.licenseSvc.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	activeByCenter := map[string]*store.License{}
	activeLicenses := 0
	now := time.Now()
	for _, lic := range licenses {
		if isActiveLicense(lic, now) {
			activeLicenses++
			if current, ok := activeByCenter[lic.CenterID]; !ok || lic.CreatedAt.After(current.CreatedAt) {
				activeByCenter[lic.CenterID] = lic
			}
		}
	}

	report := &ManagementReport{
		Summary: ManagementSummary{
			TotalCenters:   len(centers),
			ActiveLicenses: activeLicenses,
		},
		Items: make([]CenterManagement, 0, len(centers)),
	}
	for _, center := range centers {
		managementItem := buildCenterManagement(center, activeByCenter[center.ID])
		if center.Status == "pending" {
			report.Summary.PendingCenters++
		}
		if center.SupportsMultiTenant {
			report.Summary.MultiTenantCenters++
		}
		report.Summary.TenantCount += center.TenantCount
		if managementItem.Ready {
			report.Summary.ReadyCenters++
		}
		if containsIssue(managementItem.Issues, "missing_base_url") || containsIssue(managementItem.Issues, "multi_tenant_not_confirmed") || containsIssue(managementItem.Issues, "service_identity_not_verified") {
			report.Summary.NeedsSetup++
		}
		if containsIssue(managementItem.Issues, "probe_failed") || containsIssue(managementItem.Issues, "probe_missing_base_url") || containsIssue(managementItem.Issues, "probe_not_iworkercenter") || containsIssue(managementItem.Issues, "heartbeat_not_iworkercenter") {
			report.Summary.ProbeFailures++
		}
		if containsIssue(managementItem.Issues, "no_active_license") {
			report.Summary.UnlicensedCenters++
		}
		report.Items = append(report.Items, managementItem)
	}
	return report, nil
}

// Get returns a center by ID.
func (s *Service) Get(ctx context.Context, id string) (*store.Center, error) {
	return s.centers.GetByID(ctx, id)
}

// Delete removes a center.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.centers.Delete(ctx, id)
}

// ProvisionRequest is sent to an iWorkerCenter to create a tenant.
type ProvisionRequest struct {
	CompanyName   string `json:"company_name"`
	LegalPerson   string `json:"legal_person"`
	Email         string `json:"email"`
	Address       string `json:"address"`
	AdminUsername string `json:"admin_username"`
	AdminPassword string `json:"admin_password"`
	Timestamp     int64  `json:"timestamp"`
	Nonce         string `json:"nonce"`
	Signature     string `json:"signature"`
}

// ProvisionResult is returned by iWorkerCenter.
type ProvisionResult struct {
	TenantID string `json:"tenant_id"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

// ProbeResult describes a cloud-side connectivity check to a Center.
type ProbeResult struct {
	OK                  bool                     `json:"ok"`
	StatusCode          int                      `json:"status_code"`
	Message             string                   `json:"message"`
	BaseURL             string                   `json:"base_url"`
	RuntimeType         string                   `json:"runtime_type,omitempty"`
	ProductKind         string                   `json:"product_kind,omitempty"`
	AdminConsole        string                   `json:"admin_console,omitempty"`
	ProviderCount       int                      `json:"provider_count,omitempty"`
	RuntimeProviderMode string                   `json:"runtime_provider_mode,omitempty"`
	ComputeSource       string                   `json:"compute_source,omitempty"`
	ComputePermission   bool                     `json:"compute_permission,omitempty"`
	CloudProviderCount  int                      `json:"cloud_provider_count,omitempty"`
	ComputeSyncStatus   *centerComputeSyncStatus `json:"compute_sync_status,omitempty"`
}

type centerComputeSyncStatus struct {
	LastSyncAt    string `json:"last_sync_at"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
	ProviderCount int    `json:"provider_count"`
}

type centerServiceStatus struct {
	Status              string                   `json:"status"`
	RuntimeType         string                   `json:"runtime_type"`
	ProductKind         string                   `json:"product_kind"`
	AdminConsole        string                   `json:"admin_console"`
	ProviderCount       int                      `json:"provider_count"`
	RuntimeProviderMode string                   `json:"runtime_provider_mode"`
	ComputeSource       string                   `json:"compute_source"`
	ComputePermission   bool                     `json:"compute_permission"`
	CloudProviderCount  int                      `json:"cloud_provider_count"`
	ComputeSyncStatus   *centerComputeSyncStatus `json:"compute_sync_status"`
}

func (s centerServiceStatus) isIWorkerCenterService() bool {
	return strings.EqualFold(strings.TrimSpace(s.RuntimeType), "service") &&
		strings.EqualFold(strings.TrimSpace(s.ProductKind), "iworkercenter") &&
		strings.EqualFold(strings.TrimSpace(s.AdminConsole), "web_console")
}

// Probe checks whether the configured iWorkerCenter endpoint is reachable.
func (s *Service) Probe(ctx context.Context, centerID string) (*ProbeResult, *store.Center, error) {
	center, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return nil, nil, ErrNotFound
	}
	baseURL := strings.TrimSpace(center.BaseURL)
	if baseURL == "" {
		center.LastSyncStatus = "probe_missing_base_url"
		_ = s.centers.UpdateIntegration(ctx, center)
		return &ProbeResult{OK: false, Message: "center base_url is not configured"}, center, nil
	}

	url := strings.TrimRight(baseURL, "/") + "/api/center/status"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		center.LastSyncStatus = "probe_failed"
		_ = s.centers.UpdateIntegration(ctx, center)
		return &ProbeResult{OK: false, Message: err.Error(), BaseURL: baseURL}, center, nil
	}
	defer resp.Body.Close()

	result := &ProbeResult{OK: false, StatusCode: resp.StatusCode, BaseURL: baseURL}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Message = fmt.Sprintf("center service status endpoint returned %d", resp.StatusCode)
		center.LastSyncStatus = "probe_failed"
		_ = s.centers.UpdateIntegration(ctx, center)
		updated, _ := s.centers.GetByID(ctx, centerID)
		return result, updated, nil
	}
	var serviceStatus centerServiceStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&serviceStatus); err != nil {
		result.Message = "center service status response is not valid JSON: " + err.Error()
		center.LastSyncStatus = "probe_failed"
		_ = s.centers.UpdateIntegration(ctx, center)
		updated, _ := s.centers.GetByID(ctx, centerID)
		return result, updated, nil
	}
	result.RuntimeType = serviceStatus.RuntimeType
	result.ProductKind = serviceStatus.ProductKind
	result.AdminConsole = serviceStatus.AdminConsole
	result.ProviderCount = serviceStatus.ProviderCount
	result.RuntimeProviderMode = serviceStatus.RuntimeProviderMode
	result.ComputeSource = serviceStatus.ComputeSource
	result.ComputePermission = serviceStatus.ComputePermission
	result.CloudProviderCount = serviceStatus.CloudProviderCount
	result.ComputeSyncStatus = serviceStatus.ComputeSyncStatus
	if serviceStatus.isIWorkerCenterService() {
		result.OK = true
		result.Message = "iWorkerCenter service identity verified"
		center.LastSyncStatus = "probe_ok"
	} else {
		result.Message = fmt.Sprintf("endpoint is not an iWorkerCenter service: runtime_type=%q product_kind=%q admin_console=%q", serviceStatus.RuntimeType, serviceStatus.ProductKind, serviceStatus.AdminConsole)
		center.LastSyncStatus = "probe_not_iworkercenter"
	}
	_ = s.centers.UpdateIntegration(ctx, center)
	updated, _ := s.centers.GetByID(ctx, centerID)
	return result, updated, nil
}

// ProvisionReadiness checks whether Cloud may manage tenant provisioning for this Center.
// iWorkerCloud remains a vendor management plane: it may provision tenants only when
// the target iWorkerCenter is active, licensed, multi-tenant capable, and reachable by URL.
func (s *Service) ProvisionReadiness(ctx context.Context, centerID string) (*ProvisionReadiness, error) {
	center, err := s.centers.GetByID(ctx, centerID)
	if err != nil {
		return nil, ErrNotFound
	}
	var activeLicense *store.License
	if s.licenseSvc != nil {
		activeLicense, _ = s.licenseSvc.GetActive(ctx, centerID)
	}
	management := buildCenterManagement(center, activeLicense)
	allowed := center.Status == "active" &&
		strings.TrimSpace(center.BaseURL) != "" &&
		center.SupportsMultiTenant &&
		isServiceIdentityVerified(center.LastSyncStatus) &&
		activeLicense != nil
	issues := append([]string(nil), management.Issues...)
	if !allowed && len(issues) == 0 {
		issues = append(issues, "center_not_ready")
	}
	return &ProvisionReadiness{
		Allowed:       allowed,
		Center:        center,
		ActiveLicense: activeLicense,
		Issues:        issues,
		Actions:       management.RecommendedActions,
	}, nil
}

// EnsureProvisionAllowed returns the Center only when Cloud-side tenant provisioning is allowed.
func (s *Service) EnsureProvisionAllowed(ctx context.Context, centerID string) (*store.Center, error) {
	readiness, err := s.ProvisionReadiness(ctx, centerID)
	if err != nil {
		return nil, err
	}
	if !readiness.Allowed {
		return nil, fmt.Errorf("%w: %s", ErrProvisionNotAllowed, strings.Join(readiness.Issues, ","))
	}
	return readiness.Center, nil
}

// ProvisionRemote sends a signed provision request to an iWorkerCenter.
func (s *Service) ProvisionRemote(ctx context.Context, centerBaseURL string, companyName, legalPerson, email, address, adminUser, adminPass string) (*ProvisionResult, error) {
	if s.privKey == nil {
		return nil, fmt.Errorf("private key not set")
	}

	timestamp := time.Now().Unix()
	nonce, err := randomToken()
	if err != nil {
		return nil, err
	}

	// Build body without signature
	bodyMap := map[string]any{
		"company_name":   companyName,
		"legal_person":   legalPerson,
		"email":          email,
		"address":        address,
		"admin_username": adminUser,
		"admin_password": adminPass,
		"timestamp":      timestamp,
		"nonce":          nonce,
	}
	bodyJSON, err := json.Marshal(bodyMap)
	if err != nil {
		return nil, err
	}

	// Sign: sha256(timestamp:nonce:sha256hex(body))
	bodyHash := sha256.Sum256(bodyJSON)
	bodyHashHex := hex.EncodeToString(bodyHash[:])
	payload := fmt.Sprintf("%d:%s:%s", timestamp, nonce, bodyHashHex)
	payloadHash := sha256.Sum256([]byte(payload))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.privKey, crypto.SHA256, payloadHash[:])
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// Add signature to body
	bodyMap["signature"] = base64.StdEncoding.EncodeToString(sig)
	finalBody, _ := json.Marshal(bodyMap)

	url := strings.TrimRight(centerBaseURL, "/") + "/api/tenants/provision"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(finalBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("provision request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provision failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result ProvisionResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decode provision response: %w", err)
	}
	return &result, nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashSecret(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func buildCenterManagement(center *store.Center, activeLicense *store.License) CenterManagement {
	issues := make([]string, 0)
	if center.Status != "active" {
		issues = append(issues, "center_not_active")
	}
	if strings.TrimSpace(center.BaseURL) == "" {
		issues = append(issues, "missing_base_url")
	}
	if !center.SupportsMultiTenant {
		issues = append(issues, "multi_tenant_not_confirmed")
	}
	if strings.TrimSpace(center.BaseURL) != "" && !isServiceIdentityVerified(center.LastSyncStatus) {
		switch center.LastSyncStatus {
		case "probe_failed", "probe_missing_base_url", "probe_not_iworkercenter", "heartbeat_not_iworkercenter":
			issues = append(issues, center.LastSyncStatus)
		default:
			issues = append(issues, "service_identity_not_verified")
		}
	}
	if activeLicense == nil {
		issues = append(issues, "no_active_license")
	}

	ready := len(issues) == 0
	connectivity := "unknown"
	switch center.LastSyncStatus {
	case "probe_ok", "tenant_provisioned", "heartbeat_ok":
		connectivity = "reachable"
	case "probe_failed", "probe_missing_base_url", "probe_not_iworkercenter", "heartbeat_not_iworkercenter":
		connectivity = "failed"
	case "registered", "configured":
		connectivity = "not_probed"
	}

	commercialStatus := "unlicensed"
	if activeLicense != nil {
		commercialStatus = "licensed"
	}

	ManagementPosture := "watch"
	if ready {
		ManagementPosture = "ready"
	} else if containsIssue(issues, "missing_base_url") || containsIssue(issues, "multi_tenant_not_confirmed") {
		ManagementPosture = "needs_setup"
	} else if containsIssue(issues, "service_identity_not_verified") {
		ManagementPosture = "needs_setup"
	} else if containsIssue(issues, "probe_failed") || containsIssue(issues, "probe_missing_base_url") || containsIssue(issues, "probe_not_iworkercenter") || containsIssue(issues, "heartbeat_not_iworkercenter") {
		ManagementPosture = "connectivity_risk"
	} else if containsIssue(issues, "no_active_license") {
		ManagementPosture = "commercial_hold"
	}

	return CenterManagement{
		Center:             center,
		ActiveLicense:      activeLicense,
		Ready:              ready,
		Issues:             issues,
		RecommendedActions: buildRecommendedActions(issues),
		ManagementPosture:  ManagementPosture,
		CommercialStatus:   commercialStatus,
		Connectivity:       connectivity,
	}
}

func buildRecommendedActions(issues []string) []RecommendedAction {
	if len(issues) == 0 {
		return []RecommendedAction{{
			Code:        "ready_for_service_management",
			Label:       "Ready for system service",
			Description: "This Center is active, licensed, reachable, and multi-tenant capable. Cloud may manage iWorkerCenter services such as authorization, compute distribution, skill entitlement, upgrades, and connectivity.",
			Priority:    "info",
		}}
	}
	actions := make([]RecommendedAction, 0, len(issues))
	for _, issue := range issues {
		switch issue {
		case "center_not_active":
			actions = append(actions, RecommendedAction{Code: "activate_center", Label: "Activate Center", Description: "Approve this iWorkerCenter instance and issue a trial or manual authorization before iWorkerCenter management services are enabled.", Priority: "high"})
		case "missing_base_url":
			actions = append(actions, RecommendedAction{Code: "configure_base_url", Label: "Configure Base URL", Description: "Set the reachable iWorkerCenter base URL so Cloud can perform licensing, connectivity probes, compute distribution, and skill entitlement.", Priority: "high"})
		case "multi_tenant_not_confirmed":
			actions = append(actions, RecommendedAction{Code: "confirm_multi_tenant", Label: "Confirm Multi-tenant Support", Description: "Mark this Center as multi-tenant capable only after tenant isolation and Cloud integration are verified. Customer enterprise operations remain inside iWorkerCenter.", Priority: "medium"})
		case "probe_failed", "probe_missing_base_url", "service_identity_not_verified":
			actions = append(actions, RecommendedAction{Code: "test_connection", Label: "Test Connection", Description: "Run a connection probe after checking network, DNS, TLS, and the Center /api/center/status endpoint before enabling provisioning.", Priority: "high"})
		case "probe_not_iworkercenter", "heartbeat_not_iworkercenter":
			actions = append(actions, RecommendedAction{Code: "verify_center_service_identity", Label: "Verify Center Service Identity", Description: "The configured endpoint did not identify itself as an iWorkerCenter service. Check the URL before enabling cloud-side authorization, compute distribution, or skill entitlement.", Priority: "high"})
		case "no_active_license":
			actions = append(actions, RecommendedAction{Code: "issue_license", Label: "Issue License", Description: "Create or renew the Center authorization so iWorkerCenter compute distribution, skill entitlement, and platform access are commercially valid.", Priority: "high"})
		}
	}
	return dedupeActions(actions)
}

func dedupeActions(actions []RecommendedAction) []RecommendedAction {
	seen := map[string]bool{}
	out := make([]RecommendedAction, 0, len(actions))
	for _, action := range actions {
		if seen[action.Code] {
			continue
		}
		seen[action.Code] = true
		out = append(out, action)
	}
	return out
}

func isActiveLicense(lic *store.License, now time.Time) bool {
	if lic == nil || lic.RevokedAt != nil {
		return false
	}
	return lic.IsLongTerm || lic.ExpiresAt.After(now)
}

func isServiceIdentityVerified(status string) bool {
	switch strings.TrimSpace(status) {
	case "probe_ok", "heartbeat_ok", "tenant_provisioned":
		return true
	default:
		return false
	}
}
func containsIssue(issues []string, target string) bool {
	for _, issue := range issues {
		if issue == target {
			return true
		}
	}
	return false
}

func normalizeControlMode(mode string) string {
	mode = strings.TrimSpace(mode)
	switch mode {
	case "cloud_managed", "self_managed", "hybrid":
		return mode
	default:
		return "cloud_managed"
	}
}
