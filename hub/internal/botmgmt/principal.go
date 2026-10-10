package botmgmt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ownerPrincipal is the MaClawSrv user that owns one Hub user's bot instances.
// The API secret stays in settings storage and is never copied into SettingsView.
type ownerPrincipal struct {
	HubUserID string `json:"hub_user_id"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
	// LLMViewerToken is the Hub viewer token for this owner. MaClawSrv calls
	// Hub LLM with it, so usage statistics stay on this Hub user.
	LLMViewerToken    string `json:"llm_viewer_token,omitempty"`
	LLMViewerIssuedAt string `json:"llm_viewer_issued_at,omitempty"`
	// LLMEndpoint is the Hub LLM base last written for LLMViewerToken.
	// Empty means that token still has to be pushed.
	LLMEndpoint string `json:"llm_endpoint,omitempty"`
	// LLMConfigStamp is the hash of the extra providers last written with
	// LLMEndpoint. Empty means only the default system-free route was written.
	LLMConfigStamp string `json:"llm_config_stamp,omitempty"`
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

// ownerBearerSkew is how early a cached MaClaw bearer is retired. A command
// that starts just before expiry can still finish its instance read.
const ownerBearerSkew = 2 * time.Minute

type cachedBearer struct {
	token string
	until time.Time
}

// exchangeOwnerToken trades the stored API credential for a bearer.
func (s *Service) exchangeOwnerToken(ctx context.Context, rec record, principal ownerPrincipal) (string, error) {
	if token, ok := s.cachedOwnerBearer(principal.APIKey); ok {
		return token, nil
	}
	token, exp, err := s.exchangeToken(ctx, rec, principal.APIKey, principal.APISecret, principal.UserID)
	if err != nil {
		return "", err
	}
	s.rememberOwnerBearer(principal.APIKey, token, exp)
	return token, nil
}

func (s *Service) cachedOwnerBearer(apiKey string) (string, bool) {
	apiKey = strings.TrimSpace(apiKey)
	if s == nil || apiKey == "" {
		return "", false
	}
	s.bearerMu.Lock()
	defer s.bearerMu.Unlock()
	item, ok := s.ownerBearers[apiKey]
	if !ok || strings.TrimSpace(item.token) == "" || !s.now().Add(ownerBearerSkew).Before(item.until) {
		return "", false
	}
	return item.token, true
}

func (s *Service) rememberOwnerBearer(apiKey, token string, until time.Time) {
	apiKey = strings.TrimSpace(apiKey)
	token = strings.TrimSpace(token)
	if s == nil || apiKey == "" || token == "" || !until.After(s.now().Add(ownerBearerSkew)) {
		return
	}
	s.bearerMu.Lock()
	defer s.bearerMu.Unlock()
	if s.ownerBearers == nil {
		s.ownerBearers = map[string]cachedBearer{}
	}
	s.ownerBearers[apiKey] = cachedBearer{token: token, until: until.UTC()}
}

func (s *Service) dropOwnerBearer(token string) {
	token = strings.TrimSpace(token)
	if s == nil || token == "" {
		return
	}
	s.bearerMu.Lock()
	defer s.bearerMu.Unlock()
	for key, item := range s.ownerBearers {
		if item.token == token {
			delete(s.ownerBearers, key)
		}
	}
}

// exchangeToken trades an API credential for a bearer. wantUser guards
// against a credential that answers for a different account. The expiry is
// zero when MaClawSrv does not name one; callers must not cache that token.
func (s *Service) exchangeToken(ctx context.Context, rec record, apiKey, apiSecret, wantUser string) (string, time.Time, error) {
	var issued struct {
		AccessToken string    `json:"access_token"`
		ExpiresAt   time.Time `json:"expires_at"`
		Principal   struct {
			UserID string `json:"user_id"`
		} `json:"principal"`
	}
	if err := s.call(ctx, rec, "", "", http.MethodPost, "/api/v1/auth/token", map[string]string{
		"api_key":    apiKey,
		"api_secret": apiSecret,
	}, &issued); err != nil {
		return "", time.Time{}, err
	}
	if strings.TrimSpace(issued.AccessToken) == "" {
		return "", time.Time{}, fmt.Errorf("%w: access token missing", ErrSrv)
	}
	if userID := strings.TrimSpace(issued.Principal.UserID); wantUser != "" && userID != "" && userID != wantUser {
		return "", time.Time{}, fmt.Errorf("%w: token user mismatch", ErrSrv)
	}
	return issued.AccessToken, issued.ExpiresAt, nil
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
	if i := principalIndex(items, owner); i >= 0 {
		return &items[i]
	}
	return nil
}

// principalIndex is the provisioned row for this Hub user. A stub that only
// shares HubUserID is not that row: writing the viewer token there makes the
// next command miss it and mint another token.
func principalIndex(items []ownerPrincipal, owner string) int {
	for i := range items {
		if items[i].HubUserID == owner && items[i].APIKey != "" && items[i].APISecret != "" && items[i].UserID != "" {
			return i
		}
	}
	return -1
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
