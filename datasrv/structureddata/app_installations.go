package structureddata

import (
	"context"
	"fmt"
	"strings"
)

const defaultAppInstallationStatus = "installed"

func (s *Service) UpsertAppInstallation(ctx context.Context, p Principal, appID string, in UpsertAppInstallationInput) (*AppInstallation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	requestedAppID := strings.TrimSpace(appID)
	if requestedAppID == "" {
		requestedAppID = strings.TrimSpace(in.AppID)
	}
	requestedAppID, err := normalizeAppInstallationToken(requestedAppID, "app_id")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.AppID) != "" && strings.TrimSpace(in.AppID) != requestedAppID {
		return nil, fmt.Errorf("%w: app_id must match path app id", ErrInvalidInput)
	}
	blueprintID := strings.TrimSpace(in.BlueprintID)
	installationID := strings.TrimSpace(in.ID)
	if installationID == "" {
		installationID = requestedAppID
	}
	installationID, err = normalizeAppInstallationToken(installationID, "id")
	if err != nil {
		return nil, err
	}
	if installationID != requestedAppID {
		return nil, fmt.Errorf("%w: id must match app_id", ErrInvalidInput)
	}
	roleBindings, err := normalizeAppInstallationRoleBindings(requestedAppID, blueprintID, in.RoleBindings)
	if err != nil {
		return nil, err
	}
	now := formatTime(s.now().UTC())
	app := AppInstallation{
		ID:           installationID,
		TenantID:     p.TenantID,
		AppID:        requestedAppID,
		BlueprintID:  blueprintID,
		Name:         strings.TrimSpace(in.Name),
		Version:      strings.TrimSpace(in.Version),
		Kind:         strings.ToLower(strings.TrimSpace(in.Kind)),
		Status:       normalizeAppInstallationStatus(in.Status),
		Source:       strings.TrimSpace(in.Source),
		RoleBindings: roleBindings,
		Metadata:     cloneJSONMap(in.Metadata),
		InstalledBy:  p.UserID,
		InstalledAt:  now,
		UpdatedAt:    now,
	}
	out, err := s.store.UpsertAppInstallation(ctx, app)
	if err == nil {
		s.audit(ctx, p, "app.installation_upsert", "", "app_installation", requestedAppID, "Upserted app installation "+requestedAppID, map[string]any{"app_id": requestedAppID, "blueprint_id": blueprintID, "role_binding_count": len(roleBindings), "status": app.Status})
	}
	return out, err
}

func (s *Service) ListAppInstallations(ctx context.Context, p Principal, in QueryAppInstallationsInput) ([]AppInstallation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	in.AppID = strings.TrimSpace(in.AppID)
	in.BlueprintID = strings.TrimSpace(in.BlueprintID)
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	in.Before = strings.TrimSpace(in.Before)
	in.BeforeID = strings.TrimSpace(in.BeforeID)
	return s.store.ListAppInstallations(ctx, p.TenantID, in)
}

func (s *Service) GetAppInstallation(ctx context.Context, p Principal, appID string) (*AppInstallation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store.GetAppInstallation(ctx, p.TenantID, strings.TrimSpace(appID))
}

func normalizeAppInstallationToken(value, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	if len(value) > 160 || strings.ContainsAny(value, " \t\r\n") {
		return "", fmt.Errorf("%w: %s must be a compact identifier", ErrInvalidInput, name)
	}
	return value, nil
}

func normalizeAppInstallationStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return defaultAppInstallationStatus
	}
	return value
}

func normalizeAppInstallationRoleBindings(appID, blueprintID string, items []RoleBinding) ([]RoleBinding, error) {
	out := make([]RoleBinding, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		objectRole, err := normalizeIdentifier(item.ObjectRole, "object_role")
		if err != nil {
			return nil, err
		}
		datasetID := strings.ToLower(strings.TrimSpace(item.DatasetID))
		if datasetID == "" {
			return nil, fmt.Errorf("%w: dataset_id is required for role binding %s", ErrInvalidInput, objectRole)
		}
		if err := validateDatasetID(datasetID); err != nil {
			return nil, err
		}
		templateID := strings.ToLower(strings.TrimSpace(item.TemplateID))
		if templateID == "" {
			templateID = datasetID
		}
		if err := validateDatasetID(templateID); err != nil {
			return nil, err
		}
		domain := strings.ToLower(strings.TrimSpace(item.Domain))
		if domain == "" {
			domain = datasetDomain(datasetID)
		}
		if _, err := normalizeIdentifier(domain, "domain"); err != nil {
			return nil, err
		}
		if _, ok := seen[objectRole]; ok {
			return nil, fmt.Errorf("%w: duplicate role binding %s", ErrInvalidInput, objectRole)
		}
		seen[objectRole] = struct{}{}
		out = append(out, RoleBinding{AppID: appID, BlueprintID: blueprintID, ObjectRole: objectRole, Domain: domain, DatasetID: datasetID, TemplateID: templateID, Required: item.Required})
	}
	return out, nil
}
