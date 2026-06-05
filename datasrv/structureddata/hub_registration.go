package structureddata

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const hubPlatformRequestTimeout = 15 * time.Second

type hubRegistrationRecord struct {
	ID                string
	HubBaseURL        string
	PlatformID        string
	PlatformName      string
	CallbackBaseURL   string
	VirtualMailDomain string
	PublicKeyPEM      string
	PrivateKeyPEM     string
	CallbackSecret    string
	Registered        bool
	LastRegisteredAt  *time.Time
	LastSyncedAt      *time.Time
	LastError         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (s *Service) GetHubRegistrationStatus(ctx context.Context, p Principal) (*HubRegistrationResult, error) {
	if !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	record, err := s.store.GetHubRegistration(ctx)
	if err != nil {
		return nil, err
	}
	return &HubRegistrationResult{Status: hubRegistrationStatusFromRecord(record)}, nil
}

func (s *Service) SaveHubRegistration(ctx context.Context, p Principal, in SaveHubRegistrationInput) (*HubRegistrationResult, error) {
	if !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.prepareHubRegistration(ctx, in, true)
	if err != nil {
		return nil, err
	}
	out, err := s.store.SaveHubRegistration(ctx, *record)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "admin.hub_registration_save", "", "hub_registration", out.PlatformID, "Saved Hub registration settings", hubRegistrationAuditMetadata(*out))
	return &HubRegistrationResult{Status: hubRegistrationStatusFromRecord(out)}, nil
}

func (s *Service) RegisterHub(ctx context.Context, p Principal) (*HubRegistrationResult, error) {
	if !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, err := s.prepareHubRegistration(ctx, SaveHubRegistrationInput{}, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(record.HubBaseURL) == "" {
		return nil, fmt.Errorf("%w: hub_base_url is required", ErrInvalidInput)
	}
	payload := map[string]any{
		"platform_id":             record.PlatformID,
		"platform_name":           record.PlatformName,
		"callback_base_url":       record.CallbackBaseURL,
		"public_key":              record.PublicKeyPEM,
		"public_key_fingerprint":  hubPublicKeyFingerprint(record.PublicKeyPEM),
		"virtual_mail_domain":     record.VirtualMailDomain,
		"callback_secret":         record.CallbackSecret,
		"requested_features":      []string{"tenants", "data"},
		"registration_request_id": newID("hubreg"),
	}
	var resp hubRegistrationResponse
	if err := signedHubJSON(ctx, http.MethodPost, record, "/api/platform/providers/register", payload, &resp); err != nil {
		now := s.now().UTC()
		record.LastError = err.Error()
		record.UpdatedAt = now
		_, _ = s.store.SaveHubRegistration(ctx, *record)
		return nil, err
	}
	if err := validateHubRegistrationResponse(record.PlatformID, resp); err != nil {
		now := s.now().UTC()
		record.LastError = err.Error()
		record.UpdatedAt = now
		_, _ = s.store.SaveHubRegistration(ctx, *record)
		return nil, err
	}
	now := s.now().UTC()
	record.Registered = true
	record.LastRegisteredAt = &now
	record.LastError = ""
	record.UpdatedAt = now
	out, err := s.store.SaveHubRegistration(ctx, *record)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "admin.hub_register", "", "hub_registration", out.PlatformID, "Registered DataSrv with Hub", hubRegistrationAuditMetadata(*out))
	return &HubRegistrationResult{Status: hubRegistrationStatusFromRecord(out)}, nil
}

func (s *Service) SyncTenantsFromHub(ctx context.Context, p Principal) (*SyncHubTenantsResult, error) {
	if !principalIsGlobalAdmin(p) {
		return nil, ErrForbidden
	}
	return s.syncTenantsFromHub(ctx, &p)
}

func (s *Service) SyncTenantsFromHubPublic(ctx context.Context) (*SyncHubTenantsResult, error) {
	return s.syncTenantsFromHub(ctx, nil)
}

func (s *Service) syncTenantsFromHub(ctx context.Context, p *Principal) (*SyncHubTenantsResult, error) {
	s.mu.Lock()
	record, err := s.prepareHubRegistration(ctx, SaveHubRegistrationInput{}, false)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if !record.Registered {
		s.mu.Unlock()
		return nil, fmt.Errorf("%w: hub registration is not active", ErrInvalidInput)
	}
	requestRecord := *record
	s.mu.Unlock()

	var resp struct {
		Tenants []hubTenantPayload `json:"tenants"`
	}
	if err := signedHubJSON(ctx, http.MethodPost, &requestRecord, "/api/platform/tenants/list", map[string]any{}, &resp); err != nil {
		s.saveHubRegistrationSyncError(ctx, &requestRecord, err)
		return nil, err
	}
	tenants := make([]DataTenantInfo, 0, len(resp.Tenants))
	for _, item := range resp.Tenants {
		tenants = append(tenants, item.dataTenantInfo())
	}
	if len(tenants) == 0 {
		err := fmt.Errorf("%w: hub returned no tenants", ErrInvalidInput)
		s.saveHubRegistrationSyncError(ctx, &requestRecord, err)
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.currentHubRegistrationForSync(ctx, requestRecord)
	if err != nil {
		s.saveHubRegistrationSyncErrorLocked(ctx, &requestRecord, err)
		return nil, err
	}
	now := s.now().UTC()
	items, err := s.store.UpsertDataTenants(ctx, tenants, "hub", now)
	if err != nil {
		s.saveHubRegistrationSyncErrorLocked(ctx, current, err)
		return nil, err
	}
	current.LastSyncedAt = &now
	current.LastError = ""
	current.UpdatedAt = now
	_, _ = s.store.SaveHubRegistration(ctx, *current)
	if p != nil {
		s.audit(ctx, *p, "admin.hub_tenants_pull", "", "data_tenant", "hub", "Pulled tenants from Hub", map[string]any{"synced": len(items), "platform_id": current.PlatformID})
	}
	return &SyncHubTenantsResult{Synced: len(items), Tenants: items}, nil
}

func (s *Service) saveHubRegistrationSyncError(ctx context.Context, record *hubRegistrationRecord, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saveHubRegistrationSyncErrorLocked(ctx, record, err)
}

func (s *Service) saveHubRegistrationSyncErrorLocked(ctx context.Context, record *hubRegistrationRecord, err error) {
	if record == nil || err == nil {
		return
	}
	current, currentErr := s.currentHubRegistrationForSync(ctx, *record)
	if currentErr == nil {
		record = current
	} else {
		return
	}
	now := s.now().UTC()
	record.LastError = err.Error()
	record.UpdatedAt = now
	_, _ = s.store.SaveHubRegistration(ctx, *record)
}

func (s *Service) currentHubRegistrationForSync(ctx context.Context, requestRecord hubRegistrationRecord) (*hubRegistrationRecord, error) {
	current, err := s.store.GetHubRegistration(ctx)
	if err != nil {
		return nil, err
	}
	if current == nil || !current.Registered || hubRegistrationSettingsChanged(*current, requestRecord) {
		return nil, fmt.Errorf("%w: hub registration changed during tenant sync", ErrInvalidInput)
	}
	return current, nil
}

func (s *Service) prepareHubRegistration(ctx context.Context, in SaveHubRegistrationInput, applyInput bool) (*hubRegistrationRecord, error) {
	existing, err := s.store.GetHubRegistration(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	record := hubRegistrationRecord{ID: "default", PlatformID: "datasrv", PlatformName: "MaClawDataSrv", CreatedAt: now, UpdatedAt: now}
	if existing != nil {
		record = *existing
	}
	if applyInput {
		before := record
		record.HubBaseURL = normalizeHubBaseURL(in.HubBaseURL)
		record.PlatformID = strings.TrimSpace(in.PlatformID)
		record.PlatformName = strings.TrimSpace(in.PlatformName)
		record.CallbackBaseURL = normalizeHubBaseURL(in.CallbackBaseURL)
		record.VirtualMailDomain = normalizeVirtualMailDomain(in.VirtualMailDomain)
		if strings.TrimSpace(record.HubBaseURL) != "" {
			if _, err := hubEndpoint(record.HubBaseURL, "/"); err != nil {
				return nil, err
			}
		}
		if existing != nil && before.Registered && hubRegistrationSettingsChanged(before, record) {
			record.Registered = false
			record.LastRegisteredAt = nil
			record.LastSyncedAt = nil
			record.LastError = "hub registration settings changed; register again"
		}
	}
	if record.PlatformID == "" {
		record.PlatformID = "datasrv"
	}
	if record.PlatformName == "" {
		record.PlatformName = "MaClawDataSrv"
	}
	if record.CallbackSecret == "" {
		record.CallbackSecret = generateAPIKeySecret()
	}
	if strings.TrimSpace(record.PrivateKeyPEM) == "" || strings.TrimSpace(record.PublicKeyPEM) == "" {
		pub, priv, err := generateHubRegistrationKeyPair()
		if err != nil {
			return nil, err
		}
		record.PublicKeyPEM = pub
		record.PrivateKeyPEM = priv
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	return &record, nil
}

func normalizeHubBaseURL(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func normalizeVirtualMailDomain(value string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(value), "."))
}

func hubRegistrationSettingsChanged(before, after hubRegistrationRecord) bool {
	return before.HubBaseURL != after.HubBaseURL ||
		before.PlatformID != after.PlatformID ||
		before.PlatformName != after.PlatformName ||
		before.CallbackBaseURL != after.CallbackBaseURL ||
		before.VirtualMailDomain != after.VirtualMailDomain
}

type hubTenantPayload struct {
	HubTenantID       string   `json:"hub_tenant_id"`
	ID                string   `json:"id"`
	Code              string   `json:"code"`
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Status            string   `json:"status"`
	PrimaryDomain     string   `json:"primary_domain"`
	Domains           []string `json:"domains"`
	VirtualMailDomain string   `json:"virtual_mail_domain"`
	VEEnabled         bool     `json:"ve_enabled"`
	UpdatedAt         string   `json:"updated_at"`
}

type hubRegistrationResponse struct {
	OK                 *bool  `json:"ok"`
	PlatformID         string `json:"platform_id"`
	RegistrationStatus string `json:"registration_status"`
}

func validateHubRegistrationResponse(platformID string, resp hubRegistrationResponse) error {
	if resp.OK != nil && !*resp.OK {
		return fmt.Errorf("%w: hub registration was rejected", ErrInvalidInput)
	}
	if strings.TrimSpace(resp.PlatformID) != "" && !strings.EqualFold(strings.TrimSpace(resp.PlatformID), strings.TrimSpace(platformID)) {
		return fmt.Errorf("%w: hub registered unexpected platform_id %q", ErrInvalidInput, resp.PlatformID)
	}
	status := strings.ToLower(strings.TrimSpace(resp.RegistrationStatus))
	if status != "" && status != "active" {
		return fmt.Errorf("%w: hub registration status is %q", ErrInvalidInput, status)
	}
	return nil
}

func (h hubTenantPayload) dataTenantInfo() DataTenantInfo {
	id := firstTenantValue(h.ID, h.HubTenantID)
	status := strings.ToLower(strings.TrimSpace(h.Status))
	if status == "" {
		status = "active"
	}
	return DataTenantInfo{ID: id, HubTenantID: firstTenantValue(h.HubTenantID, id), Slug: firstTenantValue(h.Slug, h.Code), Name: strings.TrimSpace(h.Name), Status: status, PrimaryDomain: strings.ToLower(strings.TrimSpace(h.PrimaryDomain)), Domains: normalizeStringList(h.Domains), VirtualMailDomain: strings.ToLower(strings.Trim(strings.TrimSpace(h.VirtualMailDomain), ".")), Source: "hub"}
}

func signedHubJSON(ctx context.Context, method string, record *hubRegistrationRecord, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint, err := hubEndpoint(record.HubBaseURL, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	nonce := newID("nonce")
	sig, err := signHubPlatformPayload(method, req.URL.RequestURI(), timestamp, nonce, body, record.PrivateKeyPEM)
	if err != nil {
		return err
	}
	req.Header.Set("X-VE-Platform-ID", record.PlatformID)
	req.Header.Set("X-VE-Signature", sig)
	req.Header.Set("X-VE-Timestamp", timestamp)
	req.Header.Set("X-VE-Nonce", nonce)
	client := &http.Client{Timeout: hubPlatformRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("hub request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("hub returned status %d: %s", resp.StatusCode, truncateHubRemoteResponseDetail(string(respBody)))
	}
	if out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}

func hubEndpoint(baseURL, path string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: hub_base_url is required", ErrInvalidInput)
	}
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("%w: invalid hub_base_url", ErrInvalidInput)
	}
	return baseURL + path, nil
}

func signHubPlatformPayload(method, target, timestamp, nonce string, body []byte, privateKeyPEM string) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("%w: invalid hub private key", ErrInvalidInput)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	privateKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("%w: hub private key must be RSA", ErrInvalidInput)
	}
	digest := sha256.Sum256(hubSignaturePayload(method, target, timestamp, nonce, body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func hubSignaturePayload(method, target, timestamp, nonce string, body []byte) []byte {
	bodyDigest := sha256.Sum256(body)
	text := strings.ToUpper(method) + "\n" + target + "\n" + timestamp + "\n" + nonce + "\n" + hex.EncodeToString(bodyDigest[:])
	return []byte(text)
}

func generateHubRegistrationKeyPair() (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	pub := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	priv := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	return pub, priv, nil
}

func hubPublicKeyFingerprint(publicKeyPEM string) string {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func hubRegistrationStatusFromRecord(record *hubRegistrationRecord) HubRegistrationStatus {
	if record == nil {
		return HubRegistrationStatus{}
	}
	return HubRegistrationStatus{Configured: strings.TrimSpace(record.HubBaseURL) != "", Registered: record.Registered, HubBaseURL: record.HubBaseURL, PlatformID: record.PlatformID, PlatformName: record.PlatformName, CallbackBaseURL: record.CallbackBaseURL, VirtualMailDomain: record.VirtualMailDomain, LastRegisteredAt: record.LastRegisteredAt, LastSyncedAt: record.LastSyncedAt, LastError: record.LastError}
}

func publicHubRegistrationStatusFromRecord(record *hubRegistrationRecord) HubRegistrationStatus {
	if record == nil {
		return HubRegistrationStatus{}
	}
	return HubRegistrationStatus{Configured: strings.TrimSpace(record.HubBaseURL) != "", Registered: record.Registered, LastRegisteredAt: record.LastRegisteredAt, LastSyncedAt: record.LastSyncedAt}
}

func hubRegistrationAuditMetadata(record hubRegistrationRecord) map[string]any {
	return map[string]any{"hub_base_url": record.HubBaseURL, "platform_id": record.PlatformID, "platform_name": record.PlatformName, "registered": record.Registered, "virtual_mail_domain": record.VirtualMailDomain}
}

func truncateHubRemoteResponseDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if len(detail) > 500 {
		return detail[:500] + "..."
	}
	return detail
}
