package botmgmt

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

// connectionCredential is the long-lived MaClawSrv API credential of the
// shared connection user. MaClawSrv access tokens expire with the server-side
// TTL (12 hours), so a pasted token stops working no matter how carefully it
// is stored. The hub therefore keeps this credential — provisioned with the
// admin secret, issued without an expiry — and mints a fresh bearer for every
// shared call. It lives in settings storage and is never copied into
// SettingsView.
type connectionCredential struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	APIKey    string `json:"api_key"`
	APISecret string `json:"api_secret"`
}

// maclawTokenClaims mirrors the public part of a MaClawSrv access token. The
// hub cannot verify the signature; it only reads which principal the saved
// token belongs to, so credential provisioning can target that user.
type maclawTokenClaims struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
}

// decodeTokenClaims reads tenant and user out of a saved MaClawSrv access
// token. An expired token still carries its claims, so a connection that
// broke because the token expired can be repaired without the admin finding
// a fresh token first.
func decodeTokenClaims(token string) (maclawTokenClaims, bool) {
	parts := strings.SplitN(strings.TrimSpace(token), ".", 2)
	if len(parts) != 2 || parts[0] == "" {
		return maclawTokenClaims{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return maclawTokenClaims{}, false
	}
	var claims maclawTokenClaims
	if json.Unmarshal(payload, &claims) != nil {
		return maclawTokenClaims{}, false
	}
	claims.TenantID = strings.TrimSpace(claims.TenantID)
	claims.UserID = strings.TrimSpace(claims.UserID)
	if claims.TenantID == "" || claims.UserID == "" {
		return maclawTokenClaims{}, false
	}
	return claims, true
}

// upstreamUnauthorized reports a 401 from MaClawSrv: the bearer it saw was
// rejected, which is what an expired access token looks like.
func upstreamUnauthorized(err error) bool {
	var statusErr *upstream.StatusError
	return errors.As(err, &statusErr) && statusErr.Status == http.StatusUnauthorized
}

// sharedBearer returns the bearer for the shared connection user. A minted
// connection credential wins, because the pasted access token expires; the
// token is the fallback until the admin secret has provisioned a credential.
// It never fails: the raw token is the best there is to fall back to.
func (s *Service) sharedBearer(ctx context.Context, rec record) string {
	if rec.Connection != nil {
		if token, err := s.exchangeToken(ctx, rec, rec.Connection.APIKey, rec.Connection.APISecret, rec.Connection.UserID); err == nil {
			return token
		}
	}
	return rec.AccessToken
}

// issueConnectionCredential creates a fresh MaClawSrv API credential for the
// shared connection user. An existing (broken) connection credential carries
// that user; otherwise the saved access token's claims do.
func (s *Service) issueConnectionCredential(ctx context.Context, rec record) (connectionCredential, error) {
	target := connectionCredential{}
	if rec.Connection != nil {
		target = *rec.Connection
	} else if claims, ok := decodeTokenClaims(rec.AccessToken); ok {
		target = connectionCredential{TenantID: claims.TenantID, UserID: claims.UserID}
	}
	if tenantID := strings.TrimSpace(rec.MaClawTenantID); tenantID != "" && target.TenantID == "" {
		target.TenantID = tenantID
	}
	if target.TenantID == "" || target.UserID == "" {
		return connectionCredential{}, fmt.Errorf("%w: cannot tell which MaClawSrv user the connection belongs to", ErrSrv)
	}
	var issued struct {
		APIKey    string `json:"api_key"`
		APISecret string `json:"api_secret"`
	}
	path := "/api/v1/admin/tenants/" + url.PathEscape(target.TenantID) + "/users/" + url.PathEscape(target.UserID) + "/credentials"
	if err := s.doAdmin(ctx, rec, http.MethodPost, path, map[string]string{"name": "hub-connection"}, &issued); err != nil {
		return connectionCredential{}, err
	}
	if strings.TrimSpace(issued.APIKey) == "" || strings.TrimSpace(issued.APISecret) == "" {
		return connectionCredential{}, fmt.Errorf("%w: maclawsrv credential missing", ErrSrv)
	}
	target.APIKey = strings.TrimSpace(issued.APIKey)
	target.APISecret = strings.TrimSpace(issued.APISecret)
	return target, nil
}

// provisionConnectionCredential issues and stores a fresh connection
// credential. Callers must not hold s.mu.
func (s *Service) provisionConnectionCredential(ctx context.Context, tenantID string) (connectionCredential, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return connectionCredential{}, err
	}
	if strings.TrimSpace(rec.AdminSecret) == "" {
		return connectionCredential{}, ErrAdminSecretMissing
	}
	cred, err := s.issueConnectionCredential(ctx, rec)
	if err != nil {
		return connectionCredential{}, err
	}
	if err := s.mergeConnection(ctx, tenantID, cred); err != nil {
		return connectionCredential{}, err
	}
	return cred, nil
}

// mergeConnection stores a freshly issued credential without disturbing
// whatever else changed on the record in the meantime.
func (s *Service) mergeConnection(ctx context.Context, tenantID string, cred connectionCredential) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fresh, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	fresh.Connection = &cred
	return s.save(ctx, tenantID, fresh)
}

// bootstrapConnection provisions the connection credential as soon as the
// admin secret and an access token are both on file, so the expiring pasted
// token stops being load-bearing. Best effort: a failure surfaces the next
// time the connection is tested. Callers must not hold s.mu.
func (s *Service) bootstrapConnection(ctx context.Context, tenantID string) {
	rec, err := s.load(ctx, tenantID)
	if err != nil || rec.Connection != nil ||
		strings.TrimSpace(rec.AdminSecret) == "" || strings.TrimSpace(rec.AccessToken) == "" {
		return
	}
	if _, ok := decodeTokenClaims(rec.AccessToken); !ok {
		return
	}
	_, _ = s.provisionConnectionCredential(ctx, tenantID)
}
