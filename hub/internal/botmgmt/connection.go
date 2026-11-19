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
	ID        string `json:"id,omitempty"`
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
		if token, _, err := s.exchangeToken(ctx, rec, rec.Connection.APIKey, rec.Connection.APISecret, rec.Connection.UserID); err == nil {
			return token
		}
	}
	return rec.AccessToken
}

// connectionMatchesToken reports whether the stored connection credential
// still answers for the principal the (re)saved access token names. An
// undecodable token retires the credential, because it can no longer be
// tied to the principal it was minted for.
func connectionMatchesToken(rec record) bool {
	if rec.Connection == nil {
		return false
	}
	claims, ok := decodeTokenClaims(rec.AccessToken)
	return ok && claims.TenantID == rec.Connection.TenantID && claims.UserID == rec.Connection.UserID
}

// issueConnectionCredential creates a fresh MaClawSrv API credential for the
// shared connection user. An existing (broken) connection credential carries
// that user; otherwise the saved access token's claims do. With only the
// admin secret on file there is no token to ask, so the hub provisions its
// own dedicated user instead.
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
		if err := s.provisionConnectionUser(ctx, rec, &target); err != nil {
			return connectionCredential{}, err
		}
	}
	var issued struct {
		ID        string `json:"id"`
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
	target.ID = strings.TrimSpace(issued.ID)
	target.APIKey = strings.TrimSpace(issued.APIKey)
	target.APISecret = strings.TrimSpace(issued.APISecret)
	return target, nil
}

// provisionConnectionUser fills in the MaClawSrv tenant and user the shared
// connection belongs to when no credential and no decodable token can name
// them. The tenant on record wins, then the server's first tenant, then a
// fresh "hub" tenant on an empty server. The user is a dedicated
// hub-connection account, found by email before a new one is created.
func (s *Service) provisionConnectionUser(ctx context.Context, rec record, target *connectionCredential) error {
	if strings.TrimSpace(target.TenantID) == "" {
		var listed struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		if err := s.doAdmin(ctx, rec, http.MethodGet, "/api/v1/admin/tenants", nil, &listed); err != nil {
			return err
		}
		if len(listed.Items) > 0 {
			target.TenantID = strings.TrimSpace(listed.Items[0].ID)
		}
		if target.TenantID == "" {
			var created struct {
				ID string `json:"id"`
			}
			if err := s.doAdmin(ctx, rec, http.MethodPost, "/api/v1/admin/tenants", map[string]string{"name": "hub"}, &created); err != nil {
				return err
			}
			target.TenantID = strings.TrimSpace(created.ID)
		}
		if target.TenantID == "" {
			return fmt.Errorf("%w: maclawsrv tenant missing", ErrSrv)
		}
	}
	if strings.TrimSpace(target.UserID) != "" {
		return nil
	}
	usersPath := "/api/v1/admin/tenants/" + url.PathEscape(target.TenantID) + "/users"
	var users struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := s.doAdmin(ctx, rec, http.MethodGet, usersPath+"?email="+url.QueryEscape(nodeConnectionEmail), nil, &users); err != nil {
		return err
	}
	for _, item := range users.Items {
		if id := strings.TrimSpace(item.ID); id != "" {
			target.UserID = id
			return nil
		}
	}
	var user struct {
		ID string `json:"id"`
	}
	if err := s.doAdmin(ctx, rec, http.MethodPost, usersPath, map[string]string{
		"name":  "hub-connection",
		"email": nodeConnectionEmail,
	}, &user); err != nil {
		return err
	}
	target.UserID = strings.TrimSpace(user.ID)
	if target.UserID == "" {
		return fmt.Errorf("%w: maclawsrv user id missing", ErrSrv)
	}
	return nil
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
	previous := connectionCredential{}
	if rec.Connection != nil {
		previous = *rec.Connection
	}
	cred, err := s.issueConnectionCredential(ctx, rec)
	if err != nil {
		return connectionCredential{}, err
	}
	if err := s.mergeConnection(ctx, tenantID, cred); err != nil {
		return connectionCredential{}, err
	}
	// Revoke only a genuinely different credential: a response without an id
	// would otherwise name the credential just stored.
	if previous.ID != "" && previous.ID != cred.ID {
		s.retireConnectionCredential(ctx, rec, previous)
	}
	return cred, nil
}

// retireConnectionCredential revokes the credential this connection used
// before the fresh one took over, so in-place renewals do not pile up
// hub-connection credentials inside MaClawSrv. A credential that was retired
// by a settings save is already gone from the record, so its ID cannot be
// cited here — the MaClawSrv admin revokes that one by hand. Best effort.
func (s *Service) retireConnectionCredential(ctx context.Context, rec record, previous connectionCredential) {
	previousID := strings.TrimSpace(previous.ID)
	if previousID == "" {
		return
	}
	path := "/api/v1/admin/tenants/" + url.PathEscape(previous.TenantID) + "/users/" + url.PathEscape(previous.UserID) +
		"/credentials/" + url.PathEscape(previousID)
	_ = s.doAdmin(ctx, rec, http.MethodDelete, path, nil, nil)
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
	if strings.TrimSpace(fresh.MaClawTenantID) == "" {
		fresh.MaClawTenantID = cred.TenantID
	}
	return s.save(ctx, tenantID, fresh)
}

// bootstrapConnection provisions the connection credential as soon as the
// admin secret is on file, so the expiring pasted token — when one is saved
// at all — stops being load-bearing. Best effort: a failure surfaces the
// next time the connection is tested. Callers must not hold s.mu.
func (s *Service) bootstrapConnection(ctx context.Context, tenantID string) {
	rec, err := s.load(ctx, tenantID)
	if err != nil || rec.Connection != nil ||
		strings.TrimSpace(rec.AdminSecret) == "" || strings.TrimSpace(rec.BaseURL) == "" {
		return
	}
	_, _ = s.provisionConnectionCredential(ctx, tenantID)
}
