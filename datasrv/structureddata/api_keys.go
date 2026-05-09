package structureddata

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const apiKeyExpiringSoonWindow = 7 * 24 * time.Hour

func (s *Service) CreateAPIKeyPolicy(ctx context.Context, p Principal, in CreateAPIKeyPolicyInput) (*CreateAPIKeyPolicyResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	id := strings.ToLower(strings.TrimSpace(in.ID))
	if id == "" {
		id = newID("key")
	}
	if err := validateAccessKeyID(id); err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = generateAPIKeySecret()
	}
	if len(key) < 24 {
		return nil, fmt.Errorf("%w: api key must be at least 24 characters", ErrInvalidInput)
	}
	expiresAt, err := parseAPIKeyExpiresAt(in.ExpiresAt)
	if err != nil {
		return nil, err
	}
	role, err := normalizeAPIKeyRole(in.Role, "data_user")
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	record := APIKeyPolicyRecord{
		ID:                id,
		TenantID:          p.TenantID,
		UserID:            strings.TrimSpace(in.UserID),
		Role:              role,
		KeyPrefix:         keyPrefix(key),
		Enabled:           true,
		AllowedDomains:    normalizeStringList(in.AllowedDomains),
		AllowedDatasets:   normalizeStringList(in.AllowedDatasets),
		AllowedActions:    normalizeStringList(in.AllowedActions),
		AllowedViews:      normalizeStringList(in.AllowedViews),
		AllowedReports:    normalizeStringList(in.AllowedReports),
		AllowedDashboards: normalizeStringList(in.AllowedDashboards),
		AllowRawData:      in.AllowRawData,
		AllowSensitive:    in.AllowSensitive,
		AllowAdmin:        in.AllowAdmin,
		Note:              strings.TrimSpace(in.Note),
		ExpiresAt:         expiresAt,
		CreatedBy:         p.UserID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	out, err := s.store.CreateAPIKeyPolicy(ctx, record, apiKeyHash(key))
	if err != nil {
		return nil, err
	}
	s.audit(ctx, p, "access.api_key_create", "", "api_key_policy", out.ID, "Created scoped API key policy "+out.ID, apiKeyAuditMetadata(*out))
	return &CreateAPIKeyPolicyResult{Policy: enrichAPIKeyPolicyRecord(now, *out), Key: key}, nil
}

func (s *Service) ListAPIKeyPolicies(ctx context.Context, p Principal, in QueryAPIKeyPoliciesInput) ([]APIKeyPolicyRecord, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	wantStatus, err := normalizeAPIKeyStatusFilter(in.Status)
	if err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	storeQuery := in
	storeQuery.Before = strings.TrimSpace(storeQuery.Before)
	storeQuery.BeforeID = strings.TrimSpace(storeQuery.BeforeID)
	if wantStatus != "" {
		storeQuery.Limit = 500
	} else {
		storeQuery.Limit = limit
	}
	now := s.now().UTC()
	out := make([]APIKeyPolicyRecord, 0, limit)
	for {
		items, err := s.store.ListAPIKeyPolicies(ctx, p.TenantID, storeQuery)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			item = enrichAPIKeyPolicyRecord(now, item)
			if wantStatus != "" && item.Status != wantStatus {
				continue
			}
			out = append(out, item)
			if len(out) >= limit {
				return out, nil
			}
		}
		if wantStatus == "" || len(items) < storeQuery.Limit || len(items) == 0 {
			break
		}
		last := items[len(items)-1]
		storeQuery.Before = last.UpdatedAt.Format(time.RFC3339Nano)
		storeQuery.BeforeID = last.ID
	}
	return out, nil
}

func (s *Service) GetAPIKeyPolicy(ctx context.Context, p Principal, keyID string) (*APIKeyPolicyRecord, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	out, err := s.store.GetAPIKeyPolicy(ctx, p.TenantID, strings.ToLower(strings.TrimSpace(keyID)))
	if err != nil {
		return nil, err
	}
	item := enrichAPIKeyPolicyRecord(s.now().UTC(), *out)
	return &item, nil
}

func (s *Service) GetAPIKeyPolicyCapabilities(ctx context.Context, p Principal, keyID string) (*DataCapabilities, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	record, err := s.store.GetAPIKeyPolicy(ctx, p.TenantID, strings.ToLower(strings.TrimSpace(keyID)))
	if err != nil {
		return nil, err
	}
	preview := Principal{
		TenantID: record.TenantID,
		UserID:   record.UserID,
		Role:     record.Role,
		APIKeyID: record.ID,
		Policy:   apiKeyPolicyFromRecord(*record),
	}
	return s.Capabilities(ctx, preview)
}

func (s *Service) UpdateAPIKeyPolicy(ctx context.Context, p Principal, keyID string, in UpdateAPIKeyPolicyInput) (*APIKeyPolicyRecord, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	id := strings.ToLower(strings.TrimSpace(keyID))
	existing, err := s.store.GetAPIKeyPolicy(ctx, p.TenantID, id)
	if err != nil {
		return nil, err
	}
	enabled := existing.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	expiresAt := existing.ExpiresAt
	if in.ExpiresAt != nil {
		expiresAt, err = parseAPIKeyExpiresAt(*in.ExpiresAt)
		if err != nil {
			return nil, err
		}
	}
	role := existing.Role
	if strings.TrimSpace(in.Role) != "" {
		role, err = normalizeAPIKeyRole(in.Role, existing.Role)
		if err != nil {
			return nil, err
		}
	}
	userID := existing.UserID
	if in.UserID != nil {
		userID = strings.TrimSpace(*in.UserID)
	}
	allowedDomains := existing.AllowedDomains
	if in.AllowedDomains != nil {
		allowedDomains = normalizeStringList(in.AllowedDomains)
	}
	allowedDatasets := existing.AllowedDatasets
	if in.AllowedDatasets != nil {
		allowedDatasets = normalizeStringList(in.AllowedDatasets)
	}
	allowedActions := existing.AllowedActions
	if in.AllowedActions != nil {
		allowedActions = normalizeStringList(in.AllowedActions)
	}
	allowedViews := existing.AllowedViews
	if in.AllowedViews != nil {
		allowedViews = normalizeStringList(in.AllowedViews)
	}
	allowedReports := existing.AllowedReports
	if in.AllowedReports != nil {
		allowedReports = normalizeStringList(in.AllowedReports)
	}
	allowedDashboards := existing.AllowedDashboards
	if in.AllowedDashboards != nil {
		allowedDashboards = normalizeStringList(in.AllowedDashboards)
	}
	allowRawData := existing.AllowRawData
	if in.AllowRawData != nil {
		allowRawData = *in.AllowRawData
	}
	allowSensitive := existing.AllowSensitive
	if in.AllowSensitive != nil {
		allowSensitive = *in.AllowSensitive
	}
	allowAdmin := existing.AllowAdmin
	if in.AllowAdmin != nil {
		allowAdmin = *in.AllowAdmin
	}
	note := existing.Note
	if in.Note != nil {
		note = strings.TrimSpace(*in.Note)
	}
	record := APIKeyPolicyRecord{
		ID:                existing.ID,
		TenantID:          existing.TenantID,
		UserID:            userID,
		Role:              role,
		KeyPrefix:         existing.KeyPrefix,
		Enabled:           enabled,
		AllowedDomains:    allowedDomains,
		AllowedDatasets:   allowedDatasets,
		AllowedActions:    allowedActions,
		AllowedViews:      allowedViews,
		AllowedReports:    allowedReports,
		AllowedDashboards: allowedDashboards,
		AllowRawData:      allowRawData,
		AllowSensitive:    allowSensitive,
		AllowAdmin:        allowAdmin,
		Note:              note,
		ExpiresAt:         expiresAt,
		CreatedBy:         existing.CreatedBy,
		CreatedAt:         existing.CreatedAt,
		UpdatedAt:         s.now().UTC(),
	}
	out, err := s.store.UpdateAPIKeyPolicy(ctx, record)
	if err == nil {
		item := enrichAPIKeyPolicyRecord(s.now().UTC(), *out)
		out = &item
		s.audit(ctx, p, "access.api_key_update", "", "api_key_policy", out.ID, "Updated scoped API key policy "+out.ID, apiKeyAuditMetadata(*out))
	}
	return out, err
}

func (s *Service) RotateAPIKeyPolicySecret(ctx context.Context, p Principal, keyID string) (*CreateAPIKeyPolicyResult, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	key := generateAPIKeySecret()
	out, err := s.store.RotateAPIKeyPolicySecret(ctx, p.TenantID, strings.ToLower(strings.TrimSpace(keyID)), apiKeyHash(key), keyPrefix(key), s.now().UTC())
	if err != nil {
		return nil, err
	}
	item := enrichAPIKeyPolicyRecord(s.now().UTC(), *out)
	s.audit(ctx, p, "access.api_key_rotate", "", "api_key_policy", item.ID, "Rotated scoped API key secret "+item.ID, apiKeyAuditMetadata(item))
	return &CreateAPIKeyPolicyResult{Policy: item, Key: key}, nil
}

func (s *Service) DisableAPIKeyPolicy(ctx context.Context, p Principal, keyID string) (*APIKeyPolicyRecord, error) {
	if !principalCanAdmin(p) {
		return nil, ErrForbidden
	}
	out, err := s.store.DisableAPIKeyPolicy(ctx, p.TenantID, strings.ToLower(strings.TrimSpace(keyID)), p.UserID, s.now().UTC())
	if err == nil {
		item := enrichAPIKeyPolicyRecord(s.now().UTC(), *out)
		out = &item
		s.audit(ctx, p, "access.api_key_disable", "", "api_key_policy", out.ID, "Disabled scoped API key policy "+out.ID, apiKeyAuditMetadata(*out))
	}
	return out, err
}

func (s *Service) FindAPIKeyPolicyBySecret(ctx context.Context, secret string) (*APIKeyPolicy, error) {
	record, err := s.store.FindAPIKeyPolicyByHash(ctx, apiKeyHash(secret))
	if err != nil {
		return nil, err
	}
	if !record.Enabled {
		return nil, ErrUnauthorized
	}
	if record.ExpiresAt != nil && !record.ExpiresAt.After(s.now().UTC()) {
		return nil, ErrUnauthorized
	}
	return apiKeyPolicyFromRecord(*record), nil
}

func (s *Service) TouchAPIKeyPolicyUse(ctx context.Context, policy APIKeyPolicy, ip, userAgent string) {
	if strings.TrimSpace(policy.ID) == "" || strings.TrimSpace(policy.TenantID) == "" {
		return
	}
	_ = s.store.TouchAPIKeyPolicyUse(ctx, policy.TenantID, policy.ID, ip, userAgent, s.now().UTC())
}

func apiKeyPolicyFromRecord(record APIKeyPolicyRecord) *APIKeyPolicy {
	return &APIKeyPolicy{
		ID:                record.ID,
		TenantID:          record.TenantID,
		UserID:            record.UserID,
		Role:              record.Role,
		AllowedDomains:    append([]string(nil), record.AllowedDomains...),
		AllowedDatasets:   append([]string(nil), record.AllowedDatasets...),
		AllowedActions:    append([]string(nil), record.AllowedActions...),
		AllowedViews:      append([]string(nil), record.AllowedViews...),
		AllowedReports:    append([]string(nil), record.AllowedReports...),
		AllowedDashboards: append([]string(nil), record.AllowedDashboards...),
		AllowRawData:      record.AllowRawData,
		AllowSensitive:    record.AllowSensitive,
		AllowAdmin:        record.AllowAdmin,
	}
}

func apiKeyAuditMetadata(record APIKeyPolicyRecord) map[string]any {
	metadata := map[string]any{
		"role":                    record.Role,
		"enabled":                 record.Enabled,
		"status":                  record.Status,
		"key_prefix":              record.KeyPrefix,
		"user_id":                 record.UserID,
		"created_by":              record.CreatedBy,
		"allow_admin":             record.AllowAdmin,
		"allow_raw_data":          record.AllowRawData,
		"allow_sensitive":         record.AllowSensitive,
		"allowed_domain_count":    len(record.AllowedDomains),
		"allowed_dataset_count":   len(record.AllowedDatasets),
		"allowed_action_count":    len(record.AllowedActions),
		"allowed_view_count":      len(record.AllowedViews),
		"allowed_report_count":    len(record.AllowedReports),
		"allowed_dashboard_count": len(record.AllowedDashboards),
	}
	if record.ExpiresAt != nil {
		metadata["expires_at"] = record.ExpiresAt.UTC().Format(time.RFC3339Nano)
		metadata["expires_in_days"] = record.ExpiresInDays
	}
	return metadata
}

func apiKeyHash(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func generateAPIKeySecret() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return strings.ReplaceAll(newID("mcds"), "-", "")
	}
	return "mcds_" + base64.RawURLEncoding.EncodeToString(raw[:])
}

func keyPrefix(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 12 {
		return secret
	}
	return secret[:8] + "..." + secret[len(secret)-4:]
}

func normalizeAPIKeyRole(role, defaultRole string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	switch role {
	case "data_admin", "data_auditor", "data_user":
		return role, nil
	case "":
		defaultRole = strings.ToLower(strings.TrimSpace(defaultRole))
		switch defaultRole {
		case "data_admin", "data_auditor", "data_user":
			return defaultRole, nil
		default:
			return "data_user", nil
		}
	default:
		return "", fmt.Errorf("%w: api key role must be data_admin, data_auditor, or data_user", ErrInvalidInput)
	}
}

func validateAccessKeyID(id string) error {
	if !identifierPattern.MatchString(strings.ReplaceAll(id, ".", "_")) {
		return fmt.Errorf("%w: invalid api key id", ErrInvalidInput)
	}
	return nil
}

func parseAPIKeyExpiresAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			if layout == "2006-01-02" {
				parsed = parsed.Add(24 * time.Hour)
			}
			parsed = parsed.UTC()
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("%w: invalid api key expires_at", ErrInvalidInput)
}

func enrichAPIKeyPolicyRecord(now time.Time, item APIKeyPolicyRecord) APIKeyPolicyRecord {
	item.Status = apiKeyPolicyRecordStatus(now, item)
	item.ExpiresInDays = 0
	if item.ExpiresAt != nil {
		remaining := item.ExpiresAt.Sub(now)
		if remaining > 0 {
			item.ExpiresInDays = int((remaining + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
		}
	}
	return item
}

func apiKeyPolicyRecordStatus(now time.Time, item APIKeyPolicyRecord) string {
	if !item.Enabled {
		return "disabled"
	}
	if item.ExpiresAt != nil {
		remaining := item.ExpiresAt.Sub(now)
		if remaining <= 0 {
			return "expired"
		}
		if remaining <= apiKeyExpiringSoonWindow {
			return "expiring_soon"
		}
	}
	return "active"
}

func normalizeAPIKeyStatusFilter(raw string) (string, error) {
	status := strings.ToLower(strings.TrimSpace(raw))
	switch status {
	case "", "all":
		return "", nil
	case "active", "expired", "disabled", "expiring_soon":
		return status, nil
	default:
		return "", fmt.Errorf("%w: invalid api key status", ErrInvalidInput)
	}
}
