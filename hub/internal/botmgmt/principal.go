package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ownerPrincipal is the MaClawSrv user that owns one Hub user's bot instances.
// The API secret stays in settings storage and is never copied into SettingsView.
type ownerPrincipal struct {
	HubUserID string `json:"hub_user_id"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

// ensureOwnerToken returns a bearer for the MaClawSrv user bound to owner.
// An empty owner keeps the shared connection token. A Hub user requires the
// admin secret, so that user's bots are instances of that user's account.
// The caller must hold s.mu. A newly created principal is saved before return.
func (s *Service) ensureOwnerToken(ctx context.Context, tenantID string, rec *record, owner string) (string, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return rec.AccessToken, nil
	}
	if strings.TrimSpace(rec.AdminSecret) == "" {
		return "", fmt.Errorf("%w: 请先在 Bot 管理里保存 MaClawSrv 管理密钥", ErrNotConfigured)
	}
	principal := findPrincipal(rec.Principals, owner)
	if principal == nil {
		created, err := s.provisionOwner(ctx, rec, owner)
		if err != nil {
			return "", err
		}
		rec.Principals = append(rec.Principals, created)
		if err := s.save(ctx, tenantID, *rec); err != nil {
			return "", err
		}
		principal = &rec.Principals[len(rec.Principals)-1]
	}
	var issued struct {
		AccessToken string `json:"access_token"`
		Principal   struct {
			UserID string `json:"user_id"`
		} `json:"principal"`
	}
	if err := s.call(ctx, *rec, "", "", http.MethodPost, "/api/v1/auth/token", map[string]string{
		"api_key":    principal.APIKey,
		"api_secret": principal.APISecret,
	}, &issued); err != nil {
		return "", err
	}
	if strings.TrimSpace(issued.AccessToken) == "" {
		return "", fmt.Errorf("%w: access token missing", ErrSrv)
	}
	if userID := strings.TrimSpace(issued.Principal.UserID); userID != "" && userID != principal.UserID {
		return "", fmt.Errorf("%w: token user mismatch", ErrSrv)
	}
	return issued.AccessToken, nil
}

func (s *Service) provisionOwner(ctx context.Context, rec *record, owner string) (ownerPrincipal, error) {
	tenantID := strings.TrimSpace(rec.MaClawTenantID)
	if tenantID == "" {
		var me struct {
			TenantID string `json:"tenant_id"`
		}
		if err := s.do(ctx, *rec, http.MethodGet, "/api/v1/me", nil, &me); err != nil {
			return ownerPrincipal{}, err
		}
		tenantID = strings.TrimSpace(me.TenantID)
		if tenantID == "" {
			return ownerPrincipal{}, fmt.Errorf("%w: maclawsrv tenant missing", ErrSrv)
		}
		rec.MaClawTenantID = tenantID
	}
	var created struct {
		ID string `json:"id"`
	}
	userPath := "/api/v1/admin/tenants/" + url.PathEscape(tenantID) + "/users"
	if err := s.doAdmin(ctx, *rec, http.MethodPost, userPath, map[string]string{
		"name":  ownerName(owner),
		"email": ownerEmail(owner),
	}, &created); err != nil {
		return ownerPrincipal{}, err
	}
	if strings.TrimSpace(created.ID) == "" {
		return ownerPrincipal{}, fmt.Errorf("%w: maclawsrv user id missing", ErrSrv)
	}
	var cred struct {
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	credPath := userPath + "/" + url.PathEscape(created.ID) + "/credentials"
	if err := s.doAdmin(ctx, *rec, http.MethodPost, credPath, map[string]string{"name": "hub-bot"}, &cred); err != nil {
		return ownerPrincipal{}, err
	}
	if strings.TrimSpace(cred.APIKey) == "" || strings.TrimSpace(cred.APISecret) == "" {
		return ownerPrincipal{}, fmt.Errorf("%w: maclawsrv credential missing", ErrSrv)
	}
	return ownerPrincipal{
		HubUserID: owner,
		TenantID:  tenantID,
		UserID:    strings.TrimSpace(created.ID),
		APIKey:    cred.APIKey,
		APISecret: cred.APISecret,
	}, nil
}

func findPrincipal(items []ownerPrincipal, owner string) *ownerPrincipal {
	for i := range items {
		if items[i].HubUserID == owner && items[i].APIKey != "" && items[i].APISecret != "" && items[i].UserID != "" {
			return &items[i]
		}
	}
	return nil
}

func ownerName(owner string) string {
	runes := []rune(strings.TrimSpace(owner))
	if len(runes) > 80 {
		runes = runes[:80]
	}
	if len(runes) == 0 {
		return "hub-user"
	}
	return string(runes)
}

func ownerEmail(owner string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(owner)))
	return "hub-" + hex.EncodeToString(sum[:8]) + "@bots.maclaw.local"
}
