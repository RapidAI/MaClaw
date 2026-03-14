package entry

import (
	"context"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

type ProbeResult struct {
	Email          string `json:"email"`
	Status         string `json:"status"`
	Bound          bool   `json:"bound"`
	CanLogin       bool   `json:"can_login"`
	EnrollmentMode string `json:"enrollment_mode,omitempty"`
	PWAURL         string `json:"pwa_url,omitempty"`
	Message        string `json:"message,omitempty"`
}

type Service struct {
	identity *auth.IdentityService
}

func NewService(identity *auth.IdentityService) *Service {
	return &Service{identity: identity}
}

func (s *Service) ProbeByEmail(ctx context.Context, email string) (*ProbeResult, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" {
		return &ProbeResult{
			Email:   email,
			Status:  "invalid_email",
			Message: "Email is required",
		}, nil
	}

	blocked, err := s.identity.IsEmailBlocked(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:   email,
				Status:  "invalid_email",
				Message: err.Error(),
			}, nil
		}
		return nil, err
	}
	if blocked {
		return &ProbeResult{
			Email:   email,
			Status:  "blocked",
			Message: "Email is blocked",
		}, nil
	}

	user, err := s.identity.LookupUserByEmail(ctx, email)
	if err != nil {
		if err == auth.ErrInvalidEmail {
			return &ProbeResult{
				Email:   email,
				Status:  "invalid_email",
				Message: err.Error(),
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
			Email:          email,
			Status:         "not_found",
			Bound:          false,
			CanLogin:       false,
			EnrollmentMode: enrollmentMode,
			Message:        "Email is not bound to this hub",
		}, nil
	}

	if strings.EqualFold(user.EnrollmentStatus, "pending") || !strings.EqualFold(user.Status, "active") {
		return &ProbeResult{
			Email:          email,
			Status:         "pending_approval",
			Bound:          false,
			CanLogin:       false,
			EnrollmentMode: enrollmentMode,
			Message:        "Email exists but is not ready for login",
		}, nil
	}

	return &ProbeResult{
		Email:          email,
		Status:         "bound",
		Bound:          true,
		CanLogin:       true,
		EnrollmentMode: enrollmentMode,
		PWAURL:         s.identity.BuildPWAEntryURL(email),
	}, nil
}
