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
		return s.sharedBearer(ctx, *rec), nil
	}
	if strings.TrimSpace(rec.AdminSecret) == "" {
		return "", fmt.Errorf("%w: 请先在 Bot 管理里保存 MaClawSrv 管理密钥", ErrAdminSecretMissing)
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
	return s.exchangeOwnerToken(ctx, *rec, *principal)
}

// ownerToken loads the settings and returns a bearer for owner, without
// holding s.mu across the token exchange. That exchange is a remote call on
// the path of every bot message, and only a first-time provisioning has to
// mutate the record under the lock.
func (s *Service) ownerToken(ctx context.Context, tenantID, owner string) (record, string, error) {
	owner = strings.TrimSpace(owner)
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, "", err
	}
	if owner == "" {
		return rec, s.sharedBearer(ctx, rec), nil
	}
	if strings.TrimSpace(rec.AdminSecret) == "" {
		return record{}, "", fmt.Errorf("%w: 请先在 Bot 管理里保存 MaClawSrv 管理密钥", ErrAdminSecretMissing)
	}
	if principal := findPrincipal(rec.Principals, owner); principal != nil {
		token, err := s.exchangeOwnerToken(ctx, rec, *principal)
		return rec, token, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Another command may have provisioned this owner while the lock was free.
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return record{}, "", err
	}
	principal := findPrincipal(fresh.Principals, owner)
	if principal == nil {
		created, err := s.provisionOwner(ctx, &fresh, owner)
		if err != nil {
			return record{}, "", err
		}
		fresh.Principals = append(fresh.Principals, created)
		if err := s.save(ctx, tenantID, fresh); err != nil {
			return record{}, "", err
		}
		principal = &fresh.Principals[len(fresh.Principals)-1]
	}
	token, err := s.exchangeOwnerToken(ctx, fresh, *principal)
	return fresh, token, err
}

// exchangeOwnerToken trades the stored API credential for a bearer.
func (s *Service) exchangeOwnerToken(ctx context.Context, rec record, principal ownerPrincipal) (string, error) {
	return s.exchangeToken(ctx, rec, principal.APIKey, principal.APISecret, principal.UserID)
}

// exchangeToken trades an API credential for a bearer. wantUser guards
// against a credential that answers for a different account.
func (s *Service) exchangeToken(ctx context.Context, rec record, apiKey, apiSecret, wantUser string) (string, error) {
	var issued struct {
		AccessToken string `json:"access_token"`
		Principal   struct {
			UserID string `json:"user_id"`
		} `json:"principal"`
	}
	if err := s.call(ctx, rec, "", "", http.MethodPost, "/api/v1/auth/token", map[string]string{
		"api_key":    apiKey,
		"api_secret": apiSecret,
	}, &issued); err != nil {
		return "", err
	}
	if strings.TrimSpace(issued.AccessToken) == "" {
		return "", fmt.Errorf("%w: access token missing", ErrSrv)
	}
	if userID := strings.TrimSpace(issued.Principal.UserID); wantUser != "" && userID != "" && userID != wantUser {
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
		// The /me probe rides the shared bearer and has no 401 repair
		// loop: do() cannot renew the connection credential here, because
		// provisioning runs under s.mu and the renewal saves need that
		// lock too. What keeps this bearer mintable is the connection
		// credential provisioned by TestConnection or a settings save;
		// a brand-new tenant whose token expired before its first
		// successful probe surfaces MaClawSrv's 401 to the caller.
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
