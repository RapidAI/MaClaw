package diworkerauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

type AuthCredential struct {
	TenantID string
	Username string
	Password string
	WorkerID string
}

type AuthResult struct {
	Authenticated bool
	Identifier    string
	Error         string
}

type AuthProvider interface {
	Method() string
	Status() AuthMethodStatus
	Authenticate(credential AuthCredential) AuthResult
}

type localAuthProvider struct{ h *Handler }

func (p localAuthProvider) Method() string { return "local" }

func (p localAuthProvider) Status() AuthMethodStatus {
	return AuthMethodStatus{Method: "local", Label: "Local account", Enabled: true, Implemented: true, Status: "ready", Description: "Manual or CSV-imported username/password accounts for smaller companies."}
}

func (p localAuthProvider) Authenticate(credential AuthCredential) AuthResult {
	ok, identifier, errMsg := p.h.authenticateLocalAccount(credential.TenantID, credential.Username, credential.Password)
	return AuthResult{Authenticated: ok, Identifier: identifier, Error: errMsg}
}

type ldapAuthProvider struct{ h *Handler }

func (p ldapAuthProvider) Method() string { return "ldap" }

func (p ldapAuthProvider) Status() AuthMethodStatus {
	cfg := p.h.loadLDAPConfig()
	return AuthMethodStatus{Method: "ldap", Label: "LDAP", Enabled: cfg.Enabled, Implemented: true, Status: boolStatus(cfg.Enabled), Description: "Enterprise directory bind for domain environments."}
}

func (p ldapAuthProvider) Authenticate(credential AuthCredential) AuthResult {
	cfg := p.h.loadLDAPConfig()
	if !cfg.Enabled || cfg.Host == "" {
		return AuthResult{Authenticated: false, Error: "LDAP is not configured"}
	}
	if err := ldapBind(&cfg, credential.Username, credential.Password); err != nil {
		return AuthResult{Authenticated: false, Error: err.Error()}
	}
	return AuthResult{Authenticated: true, Identifier: credential.Username}
}

type oidcAuthProvider struct{ h *Handler }

func (p oidcAuthProvider) Method() string { return "oidc" }

func (p oidcAuthProvider) Status() AuthMethodStatus {
	cfg := p.h.loadOIDCConfig()
	return AuthMethodStatus{Method: "oidc", Label: "OIDC / OAuth SSO", Enabled: cfg.Enabled, Implemented: false, Status: "reserved", Description: "Reserved for zero-trust IdP integration such as Okta, Azure AD, Google Workspace, or Keycloak."}
}

func (p oidcAuthProvider) Authenticate(credential AuthCredential) AuthResult {
	cfg := p.h.loadOIDCConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.IssuerURL) == "" {
		return AuthResult{Authenticated: false, Error: "OIDC/OAuth SSO is not configured"}
	}
	return AuthResult{Authenticated: false, Error: "OIDC/OAuth SSO is reserved but not implemented yet"}
}

func (h *Handler) authProviders() map[string]AuthProvider {
	providers := []AuthProvider{localAuthProvider{h: h}, ldapAuthProvider{h: h}, oidcAuthProvider{h: h}}
	out := make(map[string]AuthProvider, len(providers))
	for _, provider := range providers {
		out[provider.Method()] = provider
	}
	return out
}

func (h *Handler) authProvider(method string) (AuthProvider, bool) {
	provider, ok := h.authProviders()[normalizeAuthMethod(method)]
	return provider, ok
}

func (h *Handler) loadOIDCConfig() OIDCConfig {
	var raw string
	err := h.read.QueryRow(`SELECT value_json FROM system_settings WHERE key=?`, oidcConfigKey).Scan(&raw)
	if err != nil || raw == "" {
		return OIDCConfig{Scopes: []string{"openid", "profile", "email"}}
	}
	var cfg OIDCConfig
	_ = json.Unmarshal([]byte(raw), &cfg)
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	return cfg
}

func (h *Handler) saveOIDCConfig(cfg OIDCConfig) error {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	data, _ := json.Marshal(cfg)
	now := time.Now().Format(time.RFC3339)
	_, err := h.write.Exec(
		`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		oidcConfigKey, string(data), now)
	return err
}

func (h *Handler) handleOIDC(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := h.loadOIDCConfig()
		cfg.ClientSecret = ""
		response.OK(w, cfg)
	case http.MethodPost:
		var req OIDCConfig
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		current := h.loadOIDCConfig()
		if strings.TrimSpace(req.ClientSecret) == "" {
			req.ClientSecret = current.ClientSecret
		}
		if err := h.saveOIDCConfig(req); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]any{"status": "reserved", "implemented": false})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleMethods(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	methods := []AuthMethodStatus{}
	for _, method := range []string{"local", "ldap", "oidc"} {
		provider, ok := h.authProviders()[method]
		if !ok {
			continue
		}
		methods = append(methods, provider.Status())
	}
	response.OK(w, map[string]any{"methods": methods})
}

func boolStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func unknownAuthMethodError(method string) string {
	return fmt.Sprintf("unknown auth method %q; registered methods are local, ldap, oidc", normalizeAuthMethod(method))
}
