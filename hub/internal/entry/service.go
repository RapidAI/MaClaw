package entry

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// InvitationCodeChecker checks whether invitation codes are required.
type InvitationCodeChecker interface {
	IsRequired(ctx context.Context) (bool, error)
	IsRequiredForTenant(ctx context.Context, tenantID string) (bool, error)
}

type ProbeResult struct {
	Email                  string `json:"email"`
	TenantID               string `json:"tenant_id,omitempty"`
	TenantName             string `json:"tenant_name,omitempty"`
	Status                 string `json:"status"`
	Bound                  bool   `json:"bound"`
	CanLogin               bool   `json:"can_login"`
	EnrollmentMode         string `json:"enrollment_mode,omitempty"`
	PWAURL                 string `json:"pwa_url,omitempty"`
	Message                string `json:"message,omitempty"`
	InvitationCodeRequired bool   `json:"invitation_code_required"`
}

type Service struct {
	identity       *auth.IdentityService
	invitationCode InvitationCodeChecker
}

func NewService(identity *auth.IdentityService, invitationCode InvitationCodeChecker) *Service {
	return &Service{identity: identity, invitationCode: invitationCode}
}

func (s *Service) ProbeByEmail(ctx context.Context, email string) (*ProbeResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	tenantID := auth.TenantIDFromContext(ctx)
	tenantName := ""
	if s != nil && s.identity != nil {
		tenantName = s.identity.TenantDisplayName(ctx, tenantID)
	}

	invCodeRequired := false
	if s.invitationCode != nil {
		req, err := s.invitationCode.IsRequiredForTenant(ctx, tenantID)
		if err == nil {
			invCodeRequired = req
		}
	}

	if email == "" {
		return &ProbeResult{
			Email:                  email,
			TenantID:               tenantID,
			TenantName:             tenantName,
			Status:                 "invalid_email",
			Message:                "Account is required",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	blocked, err := s.identity.IsEmailBlocked(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:                  email,
				TenantID:               tenantID,
				TenantName:             tenantName,
				Status:                 "invalid_email",
				Message:                err.Error(),
				InvitationCodeRequired: invCodeRequired,
			}, nil
		}
		return nil, err
	}
	if blocked {
		return &ProbeResult{
			Email:                  email,
			TenantID:               tenantID,
			TenantName:             tenantName,
			Status:                 "blocked",
			Message:                "Account is blocked",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	user, err := s.identity.LookupUserByEmail(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:                  email,
				TenantID:               tenantID,
				TenantName:             tenantName,
				Status:                 "invalid_email",
				Message:                err.Error(),
				InvitationCodeRequired: invCodeRequired,
			}, nil
		}
		return nil, err
	}

	enrollmentMode, err := s.identity.EnrollmentMode(ctx)
	if err != nil {
		return nil, err
	}

	if user == nil {
		return &ProbeResult{
			Email:                  email,
			TenantID:               tenantID,
			TenantName:             tenantName,
			Status:                 "not_found",
			Bound:                  false,
			CanLogin:               false,
			EnrollmentMode:         enrollmentMode,
			Message:                "Account is not bound to this hub",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	if strings.EqualFold(user.EnrollmentStatus, "pending") || !strings.EqualFold(user.Status, "active") {
		return &ProbeResult{
			Email:                  email,
			TenantID:               user.TenantID,
			TenantName:             s.identity.TenantDisplayName(ctx, user.TenantID),
			Status:                 "pending_approval",
			Bound:                  false,
			CanLogin:               false,
			EnrollmentMode:         enrollmentMode,
			Message:                "Account exists but is not ready for login",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	return &ProbeResult{
		Email:                  email,
		TenantID:               user.TenantID,
		TenantName:             s.identity.TenantDisplayName(ctx, user.TenantID),
		Status:                 "bound",
		Bound:                  true,
		CanLogin:               true,
		EnrollmentMode:         enrollmentMode,
		PWAURL:                 s.identity.BuildPWAEntryURL(email),
		InvitationCodeRequired: invCodeRequired,
	}, nil
}

func (s *Service) ResolveTenantByEmail(ctx context.Context, email string) (tenantID string, found bool, ambiguous bool, err error) {
	if s == nil || s.identity == nil {
		return "", false, false, nil
	}
	return s.identity.ResolveTenantByEmail(ctx, email)
}
