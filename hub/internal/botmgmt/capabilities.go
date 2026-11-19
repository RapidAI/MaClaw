package botmgmt

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/botcapability"
)

// GetOwnerCapabilities is the redacted MCP and skill list for one owner.
func (s *Service) GetOwnerCapabilities(ctx context.Context, tenantID, hubUserID string) (botcapability.View, error) {
	doc, err := s.ownerCapabilities(ctx, tenantID, hubUserID)
	if err != nil {
		return botcapability.View{}, err
	}
	return botcapability.ViewOf(doc), nil
}

// UpdateOwnerCapabilities stores one change for every bot of this owner and
// pushes it to MaClawSrv. A push failure is Warning: the choice is kept and
// the next bot open tries again.
func (s *Service) UpdateOwnerCapabilities(ctx context.Context, tenantID, hubUserID string, up botcapability.Update) (botcapability.View, error) {
	if s == nil || s.System == nil {
		return botcapability.View{}, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return botcapability.View{}, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		s.mu.Unlock()
		return botcapability.View{}, err
	}
	current := botcapability.Document{}
	if rec.OwnerCapabilities != nil && rec.OwnerCapabilities[hubUserID] != nil {
		current = *rec.OwnerCapabilities[hubUserID]
	}
	next, err := botcapability.Apply(current, up)
	if err != nil {
		s.mu.Unlock()
		return botcapability.View{}, fmt.Errorf("%w: %s", ErrInvalidInput, err.Error())
	}
	if rec.OwnerCapabilities == nil {
		rec.OwnerCapabilities = map[string]*botcapability.Document{}
	}
	stored := next
	rec.OwnerCapabilities[hubUserID] = &stored
	if rec.OwnerCapabilityStamps != nil {
		delete(rec.OwnerCapabilityStamps, hubUserID)
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return botcapability.View{}, err
	}
	s.mu.Unlock()

	view := botcapability.ViewOf(next)
	if pushErr := s.pushOwnerCapabilities(ctx, tenantID, hubUserID); pushErr != nil {
		view.Warning = pushErr.Error()
	}
	return view, nil
}

func (s *Service) ownerCapabilities(ctx context.Context, tenantID, hubUserID string) (botcapability.Document, error) {
	if s == nil || s.System == nil {
		return botcapability.Document{}, ErrSettingsUnavailable
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return botcapability.Document{}, fmt.Errorf("%w: hub user id is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return botcapability.Document{}, err
	}
	if rec.OwnerCapabilities == nil || rec.OwnerCapabilities[hubUserID] == nil {
		return botcapability.Document{}, nil
	}
	return *rec.OwnerCapabilities[hubUserID], nil
}

// ensureBotCapabilities pushes a stored document that MaClawSrv does not
// have yet. It does not fail the bot turn: the tools read whatever was last
// pushed, and the next open tries again.
func (s *Service) ensureBotCapabilities(ctx context.Context, tenantID, hubUserID string) error {
	if s == nil || s.System == nil {
		return nil
	}
	hubUserID = strings.TrimSpace(hubUserID)
	if hubUserID == "" {
		return nil
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	s.mu.Unlock()
	if err != nil || rec.OwnerCapabilities == nil || rec.OwnerCapabilities[hubUserID] == nil {
		return err
	}
	if rec.OwnerCapabilityStamps[hubUserID] == botcapability.Stamp(*rec.OwnerCapabilities[hubUserID]) {
		return nil
	}
	return s.pushOwnerCapabilities(ctx, tenantID, hubUserID)
}

func (s *Service) pushOwnerCapabilities(ctx context.Context, tenantID, hubUserID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	doc := botcapability.Document{}
	if rec.OwnerCapabilities != nil && rec.OwnerCapabilities[hubUserID] != nil {
		doc = *rec.OwnerCapabilities[hubUserID]
	}
	principal := findPrincipal(rec.Principals, hubUserID)
	s.mu.Unlock()
	if principal == nil || strings.TrimSpace(principal.UserID) == "" || strings.TrimSpace(principal.TenantID) == "" {
		return fmt.Errorf("bot account is not provisioned yet; the choice is saved and applies on the next bot open")
	}
	path := "/api/v1/admin/tenants/" + url.PathEscape(principal.TenantID) + "/users/" + url.PathEscape(principal.UserID) + "/config"
	var current struct {
		AppConfig map[string]any `json:"app_config"`
	}
	if err := s.doAdmin(ctx, rec, http.MethodGet, path, nil, &current); err != nil {
		return fmt.Errorf("read bot config: %s", err.Error())
	}
	if current.AppConfig == nil {
		current.AppConfig = map[string]any{}
	}
	if doc.Empty() {
		delete(current.AppConfig, "desktop_bot_capabilities")
	} else {
		current.AppConfig["desktop_bot_capabilities"] = doc
	}
	if err := s.doAdmin(ctx, rec, http.MethodPut, path, map[string]any{"app_config": current.AppConfig}, nil); err != nil {
		return fmt.Errorf("write bot config: %s", err.Error())
	}
	return s.markCapabilityStamp(ctx, tenantID, hubUserID, botcapability.Stamp(doc))
}

func (s *Service) markCapabilityStamp(ctx context.Context, tenantID, hubUserID, stamp string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	doc := botcapability.Document{}
	if rec.OwnerCapabilities != nil && rec.OwnerCapabilities[hubUserID] != nil {
		doc = *rec.OwnerCapabilities[hubUserID]
	}
	if botcapability.Stamp(doc) != stamp {
		return nil
	}
	if rec.OwnerCapabilityStamps == nil {
		rec.OwnerCapabilityStamps = map[string]string{}
	}
	rec.OwnerCapabilityStamps[hubUserID] = stamp
	return s.save(ctx, tenantID, rec)
}
