package agentservice

import (
	"context"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/security"
)

type auditRecord struct {
	TenantID      string
	UserID        string
	Action        string
	ResourceType  string
	ResourceID    string
	ActorType     string
	ActorTenantID string
	ActorUserID   string
	Metadata      map[string]string
}

func (s *Service) ListAuditEvents(ctx context.Context, in ListAuditEventsInput) ([]AuditEvent, error) {
	_ = ctx
	items, err := s.store.ListAuditEvents(strings.TrimSpace(in.TenantID), strings.TrimSpace(in.UserID))
	if err != nil {
		return nil, err
	}
	action := strings.TrimSpace(in.Action)
	resourceType := strings.TrimSpace(in.ResourceType)
	resourceID := strings.TrimSpace(in.ResourceID)
	actorType := strings.TrimSpace(in.ActorType)
	actorTenant := strings.TrimSpace(in.ActorTenant)
	actorUser := strings.TrimSpace(in.ActorUser)
	if action == "" && resourceType == "" && resourceID == "" && actorType == "" && actorTenant == "" && actorUser == "" && in.Since == nil && in.Until == nil {
		return items, nil
	}
	filtered := make([]AuditEvent, 0, len(items))
	for _, item := range items {
		if action != "" && item.Action != action {
			continue
		}
		if resourceType != "" && item.ResourceType != resourceType {
			continue
		}
		if resourceID != "" && item.ResourceID != resourceID {
			continue
		}
		if actorType != "" && item.ActorType != actorType {
			continue
		}
		if actorTenant != "" && item.ActorTenant != actorTenant {
			continue
		}
		if actorUser != "" && item.ActorUser != actorUser {
			continue
		}
		if in.Since != nil && item.CreatedAt.Before(*in.Since) {
			continue
		}
		if in.Until != nil && item.CreatedAt.After(*in.Until) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered, nil
}

func redactAuditEventsForExport(dataRoot string, events []AuditEvent) []AuditEvent {
	if len(events) == 0 {
		return nil
	}
	out := make([]AuditEvent, len(events))
	for i, event := range events {
		event.ResourceID = redactAuditExportValue(dataRoot, event.ResourceID)
		if len(event.Metadata) > 0 {
			event.Metadata = redactAuditExportMetadata(dataRoot, event.Metadata)
		}
		out[i] = event
	}
	return out
}

func redactAuditExportMetadata(dataRoot string, metadata map[string]string) map[string]string {
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if auditExportSensitiveKey(key) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactAuditExportValue(dataRoot, value)
	}
	return out
}

func auditExportSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	for _, marker := range auditExportSecretKeyMarkers() {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return auditExportAuthSecretKey(key)
}

func auditExportAuthSecretKey(key string) bool {
	key = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch key {
	case "auth", "authentication", "auth_header", "authheader", "authentication_header":
		return true
	default:
		return false
	}
}

func auditExportSecretKeyMarkers() []string {
	return auditExportSecretKeyMarkersList[:]
}

var auditExportSecretKeyMarkersList = [...]string{"secret", "token", "password", "passwd", "authorization", "cookie", "bearer", "private", "api_key", "api-key", "apikey", "api_secret", "api-secret", "apisecret"}

func auditExportSecretKeyRegexpPattern() string {
	parts := make([]string, 0, len(auditExportSecretKeyMarkersList)+3)
	for _, marker := range auditExportSecretKeyMarkersList {
		parts = append(parts, regexp.QuoteMeta(marker))
	}
	parts = append(parts, `api[-_\s]?key`, `api[-_\s]?secret`, `auth`)
	sort.Slice(parts, func(i, j int) bool {
		return len(parts[i]) > len(parts[j])
	})
	return strings.Join(parts, "|")
}

func redactAuditExportValue(dataRoot, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	value = redactAuditExportText(dataRoot, value)
	if filepath.IsAbs(value) {
		return filepath.Base(value)
	}
	return value
}

func redactAuditExportText(dataRoot, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}
	if dataRoot = strings.TrimSpace(dataRoot); dataRoot != "" {
		base := auditExportPathBase(dataRoot)
		for _, variant := range auditExportPathRedactionVariants(dataRoot) {
			text = strings.ReplaceAll(text, variant, base)
		}
	}
	text = auditExportBearerPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = auditExportJSONSecretPattern.ReplaceAllString(text, `${1}"[redacted]"`)
	return auditExportInlineSecretPattern.ReplaceAllString(text, `${1}${2}[redacted]`)
}

func auditExportPathBase(value string) string {
	if value = strings.TrimSpace(value); value == "" {
		return value
	}
	base := filepath.Base(value)
	if base != value {
		return base
	}
	return path.Base(strings.ReplaceAll(value, `\`, "/"))
}

func auditExportPathRedactionVariants(value string) []string {
	seen := map[string]bool{}
	variants := make([]string, 0, 6)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		variants = append(variants, v)
	}
	add(value)
	add(filepath.ToSlash(value))
	add(filepath.FromSlash(value))
	for _, v := range append([]string(nil), variants...) {
		add(strings.ReplaceAll(v, `\`, `\\`))
	}
	return variants
}

func maskAPIKeyString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 6 {
		return "******"
	}
	return v[:3] + "***" + v[len(v)-3:]
}

func (s *Service) recordAudit(in auditRecord) error {
	return s.store.SaveAuditEvent(AuditEvent{
		ID:           NewID("audit"),
		TenantID:     strings.TrimSpace(in.TenantID),
		UserID:       strings.TrimSpace(in.UserID),
		ActorType:    defaultString(strings.TrimSpace(in.ActorType), "system"),
		ActorTenant:  strings.TrimSpace(in.ActorTenantID),
		ActorUser:    strings.TrimSpace(in.ActorUserID),
		Action:       strings.TrimSpace(in.Action),
		ResourceType: strings.TrimSpace(in.ResourceType),
		ResourceID:   strings.TrimSpace(in.ResourceID),
		// Fix for P0-3 of the 2026-09-08 review: cloneMap previously passed
		// metadata through verbatim, which would have leaked password / token /
		// api_key values into agentservice_state.json. Apply the same redact
		// rules the AuditLog.Log path uses.
		Metadata:  security.RedactMetadataMap(in.Metadata),
		CreatedAt: s.now(),
	})
}

// RecordAuditEvent stores an externally assembled audit event after applying
// the same defaults used by internal service audit records.
func (s *Service) RecordAuditEvent(ctx context.Context, event AuditEvent) error {
	_ = ctx
	if strings.TrimSpace(event.ID) == "" {
		event.ID = NewID("audit")
	}
	event.TenantID = strings.TrimSpace(event.TenantID)
	event.UserID = strings.TrimSpace(event.UserID)
	event.ActorType = defaultString(strings.TrimSpace(event.ActorType), "system")
	event.ActorTenant = strings.TrimSpace(event.ActorTenant)
	event.ActorUser = strings.TrimSpace(event.ActorUser)
	event.Action = strings.TrimSpace(event.Action)
	event.ResourceType = strings.TrimSpace(event.ResourceType)
	event.ResourceID = strings.TrimSpace(event.ResourceID)
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	// P0-3 (2026-09-08): redact externally-supplied metadata the same way
	// recordAudit does. Previously the field flowed through unchanged.
	event.Metadata = security.RedactMetadataMap(event.Metadata)
	return s.store.SaveAuditEvent(event)
}
