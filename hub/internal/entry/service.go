package entry

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// InvitationCodeChecker checks whether invitation codes are required.
type InvitationCodeChecker interface {
	IsRequired(ctx context.Context) (bool, error)
}

type ProbeResult struct {
	Email                  string `json:"email"`
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

	invCodeRequired := false
	if s.invitationCode != nil {
		req, err := s.invitationCode.IsRequired(ctx)
		if err == nil {
			invCodeRequired = req
		}
	}

	if email == "" {
		return &ProbeResult{
			Email:                  email,
			Status:                 "invalid_email",
			Message:                "Email is required",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	blocked, err := s.identity.IsEmailBlocked(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:                  email,
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
			Status:                 "blocked",
			Message:                "Email is blocked",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	user, err := s.identity.LookupUserByEmail(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:                  email,
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
			Status:                 "not_found",
			Bound:                  false,
			CanLogin:               false,
			EnrollmentMode:         enrollmentMode,
			Message:                "Email is not bound to this hub",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	if strings.EqualFold(user.EnrollmentStatus, "pending") || !strings.EqualFold(user.Status, "active") {
		return &ProbeResult{
			Email:                  email,
			Status:                 "pending_approval",
			Bound:                  false,
			CanLogin:               false,
			EnrollmentMode:         enrollmentMode,
			Message:                "Email exists but is not ready for login",
			InvitationCodeRequired: invCodeRequired,
		}, nil
	}

	return &ProbeResult{
		Email:                  email,
		Status:                 "bound",
		Bound:                  true,
		CanLogin:               true,
		EnrollmentMode:         enrollmentMode,
		PWAURL:                 s.identity.BuildPWAEntryURL(email),
		InvitationCodeRequired: invCodeRequired,
	}, nil
}
