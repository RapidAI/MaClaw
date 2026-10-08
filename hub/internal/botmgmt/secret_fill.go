package botmgmt

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// FillBotSecret forwards a locally held secret to the desktop password field.
// The returned code is empty on success. A refusal is not_password_field,
// no_focus, or unavailable, and neither the code nor a returned error contains
// the secret value.
func (s *Service) FillBotSecret(ctx context.Context, tenantID, userID, botID, name, value string) (string, error) {
	name = strings.TrimSpace(name)
	if !validBotSecretName(name) || strings.TrimSpace(value) == "" {
		return "unavailable", nil
	}
	rec, err := s.loadForUser(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 || rec.Bots[index].OwnerUserID != userID {
		return "", ErrNotFound
	}
	if err := configured(rec); err != nil {
		return "", err
	}
	fresh, token, tokenErr := s.ownerToken(ctx, tenantID, userID)
	if tokenErr != nil {
		return "", tokenErr
	}
	path := "/api/v1/instances/" + url.PathEscape(rec.Bots[index].InstanceID) + "/secret-fill"
	callErr := s.doAuth(ctx, fresh, token, http.MethodPost, path, map[string]string{
		"name":  name,
		"value": value,
	}, nil)
	if callErr == nil {
		return "", nil
	}
	if errors.Is(callErr, ErrSrv) || errors.Is(callErr, ErrSrvNotFound) {
		return secretFillCode(callErr, value), nil
	}
	return "", callErr
}

func secretFillCode(err error, value string) string {
	code := "unavailable"
	var statusErr *upstream.StatusError
	if errors.As(err, &statusErr) {
		detail := strings.TrimSpace(statusErr.Detail)
		switch detail {
		case "not_password_field", "no_focus", "unavailable":
			code = detail
		}
	}
	if value != "" && strings.Contains(code, value) {
		return "unavailable"
	}
	return code
}

func validBotSecretName(name string) bool {
	if name == "" || utf8.RuneCountInString(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case i > 0 && ((r >= '0' && r <= '9') || r == '_'):
		default:
			return false
		}
	}
	return true
}
