package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/desktopmcp"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/desktoppool"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// Desktop MCP: Hub serves the cloud desktop to external agents (Claude
// Desktop/Code, Cursor, any MCP client) at /mcp, Streamable HTTP.
//
// A bearer token is one of:
//   - a desktop MCP token (mcpd_...): issued per user, stored as SHA-256 in
//     system settings, scope read or control;
//   - the user's own Hub sign-in token: control of that user's desktop
//     (LLM endpoint API keys are refused);
//   - a Hub admin token: control of any desktop in the admin's tenant (any
//     tenant for a global admin), chosen per call with user_id.

const desktopMCPTokensKey = "desktop_mcp_tokens"
const desktopMCPTokenPrefix = "mcpd_"

type desktopMCPToken struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	TokenSHA256  string    `json:"token_sha256"`
	TenantID     string    `json:"tenant_id"`
	UserID       string    `json:"user_id"`
	Scope        string    `json:"scope"`
	AllowBash    bool      `json:"allow_bash"`
	AllowInstall bool      `json:"allow_install"`
	CreatedBy    string    `json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// view is the listing shape: never the hash.
func (t desktopMCPToken) view() map[string]any {
	return map[string]any{"id": t.ID, "name": t.Name, "tenant_id": t.TenantID, "user_id": t.UserID, "scope": t.Scope,
		"allow_bash": t.AllowBash, "allow_install": t.AllowInstall, "created_by": t.CreatedBy, "created_at": t.CreatedAt}
}

type desktopMCPTokenStore struct {
	system store.SystemSettingsRepository
	mu     sync.Mutex
}

func (s *desktopMCPTokenStore) load(ctx context.Context) ([]desktopMCPToken, error) {
	if s == nil || s.system == nil {
		return nil, errors.New("settings store is unavailable")
	}
	raw, err := s.system.Get(ctx, desktopMCPTokensKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var out []desktopMCPToken
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("desktop MCP tokens are unreadable")
	}
	return out, nil
}

func (s *desktopMCPTokenStore) save(ctx context.Context, tokens []desktopMCPToken) error {
	raw, err := json.Marshal(tokens)
	if err != nil {
		return err
	}
	return s.system.Set(ctx, desktopMCPTokensKey, string(raw))
}

func (s *desktopMCPTokenStore) lookup(ctx context.Context, token string) (desktopMCPToken, bool) {
	tokens, err := s.load(ctx)
	if err != nil {
		return desktopMCPToken{}, false
	}
	hash := desktopmcp.HashToken(token)
	for _, t := range tokens {
		if subtle.ConstantTimeCompare([]byte(t.TokenSHA256), []byte(hash)) == 1 {
			return t, true
		}
	}
	return desktopMCPToken{}, false
}

func (s *desktopMCPTokenStore) create(ctx context.Context, t desktopMCPToken) (string, desktopMCPToken, error) {
	if !desktop.ValidUserID(t.UserID) || strings.TrimSpace(t.TenantID) == "" {
		return "", t, errors.New("a valid tenant_id and user_id are required")
	}
	if t.Scope != "control" {
		t.Scope = "read"
	}
	secret := make([]byte, 32)
	id := make([]byte, 8)
	if _, err := rand.Read(secret); err != nil {
		return "", t, err
	}
	_, _ = rand.Read(id)
	plain := desktopMCPTokenPrefix + hex.EncodeToString(secret)
	t.ID, t.TokenSHA256, t.CreatedAt = hex.EncodeToString(id), desktopmcp.HashToken(plain), time.Now().UTC()
	if strings.TrimSpace(t.Name) == "" {
		t.Name = "mcp"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load(ctx)
	if err != nil {
		return "", t, err
	}
	if len(tokens) >= 1000 {
		return "", t, errors.New("too many desktop MCP tokens")
	}
	if err := s.save(ctx, append(tokens, t)); err != nil {
		return "", t, err
	}
	return plain, t, nil
}

// remove deletes the token id when keep(token) says it is the caller's.
func (s *desktopMCPTokenStore) remove(ctx context.Context, id string, mine func(desktopMCPToken) bool) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load(ctx)
	if err != nil {
		return false, err
	}
	out := tokens[:0]
	found := false
	for _, t := range tokens {
		if t.ID == id && mine(t) {
			found = true
			continue
		}
		out = append(out, t)
	}
	if !found {
		return false, nil
	}
	return true, s.save(ctx, out)
}

func (s *desktopMCPTokenStore) list(ctx context.Context, mine func(desktopMCPToken) bool) ([]map[string]any, error) {
	tokens, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, t := range tokens {
		if mine(t) {
			out = append(out, t.view())
		}
	}
	return out, nil
}

// desktopMCPAuth maps a bearer token to the desktop it may reach.
func desktopMCPAuth(tokens *desktopMCPTokenStore, identity *auth.IdentityService, admins *auth.AdminService) desktopmcp.Authenticator {
	return func(r *http.Request) (desktopmcp.Identity, bool) {
		token := desktopmcp.BearerToken(r)
		if token == "" {
			return desktopmcp.Identity{}, false
		}
		ctx := r.Context()
		if strings.HasPrefix(token, desktopMCPTokenPrefix) {
			t, ok := tokens.lookup(ctx, token)
			if !ok {
				return desktopmcp.Identity{}, false
			}
			return desktopmcp.Identity{Name: "token:" + t.ID, TenantID: t.TenantID, UserID: t.UserID, Scope: t.Scope, AllowBash: t.AllowBash, AllowInstall: t.AllowInstall}, true
		}
		if identity != nil {
			if p, err := authenticateViewerRequest(r, identity); err == nil && p != nil {
				if p.IsLLMEndpointAPIKey() || !desktop.ValidUserID(p.UserID) {
					return desktopmcp.Identity{}, false
				}
				return desktopmcp.Identity{Name: "user:" + p.UserID, TenantID: p.TenantID, UserID: p.UserID, Scope: "control"}, true
			}
		}
		if admins != nil {
			if admin, err := admins.Authenticate(ctx, token); err == nil && admin != nil {
				id := desktopmcp.Identity{Name: "admin:" + admin.Username, Scope: "control", AnyUser: true, AllowBash: true, AllowInstall: true}
				if adminHasTenantScope(admin) {
					id.TenantID = strings.TrimSpace(admin.TenantID)
					if id.TenantID == "" || strings.EqualFold(id.TenantID, auth.ExplicitGlobalAdminTenantScope) {
						return desktopmcp.Identity{}, false
					}
				} else {
					id.TenantID, id.AnyTenant = store.DefaultTenantID, true
				}
				return id, true
			}
		}
		return desktopmcp.Identity{}, false
	}
}

// DesktopMCPHandler serves /mcp.
func DesktopMCPHandler(pool *desktoppool.Pool, tokens *desktopMCPTokenStore, identity *auth.IdentityService, admins *auth.AdminService) http.Handler {
	server := desktopmcp.New(desktopPoolMCP{pool: pool}, desktopmcp.Options{Auth: desktopMCPAuth(tokens, identity, admins)})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", "desktop service store is unavailable")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Accel-Buffering", "no")
		server.ServeHTTP(w, r)
	})
}

// desktopPoolMCP is the MCP backend: Hub's own desktop pool, no loopback.
type desktopPoolMCP struct{ pool *desktoppool.Pool }

func (b desktopPoolMCP) Screenshot(ctx context.Context, id desktopmcp.Identity, opts desktop.ShotOptions) (desktop.Shot, error) {
	shot, _, err := b.pool.ScreenshotWith(ctx, id.TenantID, id.UserID, ":20", "", opts)
	return shot, err
}
func (b desktopPoolMCP) App(ctx context.Context, id desktopmcp.Identity, argv []string) (string, error) {
	return b.pool.RunApp(ctx, id.TenantID, id.UserID, ":20", argv)
}
func (b desktopPoolMCP) Ctl(ctx context.Context, id desktopmcp.Identity, request map[string]any) (map[string]any, error) {
	if op, _ := request["op"].(string); desktop.IsCtlViewOp(strings.TrimSpace(op)) {
		return nil, fmt.Errorf("%s is Hub-only", op)
	}
	return b.pool.Ctl(ctx, id.TenantID, id.UserID, ":20", request)
}
func (b desktopPoolMCP) Open(ctx context.Context, id desktopmcp.Identity, program string, args []string) (string, error) {
	return b.pool.RunOpen(ctx, id.TenantID, id.UserID, ":20", program, args)
}
func (b desktopPoolMCP) File(ctx context.Context, id desktopmcp.Identity, action, path, content string) (string, error) {
	return b.pool.RunFile(ctx, id.TenantID, id.UserID, action, path, content, "", "")
}
func (b desktopPoolMCP) Bash(ctx context.Context, id desktopmcp.Identity, command string) (string, error) {
	return b.pool.RunBash(ctx, id.TenantID, id.UserID, command)
}
func (b desktopPoolMCP) Install(ctx context.Context, id desktopmcp.Identity, packages []string) (string, error) {
	return b.pool.RunInstall(ctx, id.TenantID, id.UserID, packages)
}
func (b desktopPoolMCP) Session(ctx context.Context, id desktopmcp.Identity) (map[string]any, error) {
	s, err := b.pool.OpenDesktop(ctx, id.TenantID, id.UserID)
	if err != nil {
		return nil, err
	}
	return map[string]any{"display": s.Display, "cdp_url": s.CDP}, nil
}

type desktopMCPTokenInput struct {
	Name         string `json:"name"`
	TenantID     string `json:"tenant_id"`
	UserID       string `json:"user_id"`
	Scope        string `json:"scope"`
	AllowBash    bool   `json:"allow_bash"`
	AllowInstall bool   `json:"allow_install"`
}

func decodeDesktopMCPTokenInput(r *http.Request) (desktopMCPTokenInput, error) {
	var in desktopMCPTokenInput
	err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in)
	return in, err
}

func writeDesktopMCPCreated(w http.ResponseWriter, plain string, t desktopMCPToken) {
	out := t.view()
	out["token"] = plain
	out["note"] = "Shown once. Use it as the bearer token for /mcp."
	writeJSON(w, http.StatusCreated, out)
}

// DesktopMCPUserTokensHandler: a signed-in user manages tokens for their own
// desktop (GET list, POST create, DELETE ?id=).
func DesktopMCPUserTokensHandler(tokens *desktopMCPTokenStore, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := authenticateViewerRequest(r, identity)
		if err != nil || p == nil || p.IsLLMEndpointAPIKey() {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mine := func(t desktopMCPToken) bool { return t.TenantID == p.TenantID && t.UserID == p.UserID }
		switch r.Method {
		case http.MethodGet:
			out, err := tokens.list(r.Context(), mine)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
		case http.MethodPost:
			in, err := decodeDesktopMCPTokenInput(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid token request")
				return
			}
			// Users cannot grant themselves bash/install; an admin can.
			plain, t, err := tokens.create(r.Context(), desktopMCPToken{Name: in.Name, TenantID: p.TenantID, UserID: p.UserID, Scope: in.Scope, CreatedBy: "user:" + p.UserID})
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
				return
			}
			writeDesktopMCPCreated(w, plain, t)
		case http.MethodDelete:
			ok, err := tokens.remove(r.Context(), r.URL.Query().Get("id"), mine)
			if err != nil || !ok {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "token not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

// DesktopMCPAdminTokensHandler: a tenant admin manages tokens for users of
// the tenant. The service's desktop API token (server-side, already able to
// drive every desktop) may create tokens too, for provisioning.
func DesktopMCPAdminTokensHandler(tokens *desktopMCPTokenStore, requireTenantAdmin func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	serve := func(w http.ResponseWriter, r *http.Request, tenant, by string) {
		mine := func(t desktopMCPToken) bool { return tenant == "" || t.TenantID == tenant }
		switch r.Method {
		case http.MethodGet:
			out, err := tokens.list(r.Context(), mine)
			if err != nil {
				writeError(w, http.StatusServiceUnavailable, "SETTINGS_UNAVAILABLE", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
		case http.MethodPost:
			in, err := decodeDesktopMCPTokenInput(r)
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid token request")
				return
			}
			if tenant != "" {
				if in.TenantID != "" && in.TenantID != tenant {
					writeError(w, http.StatusForbidden, "TENANT_FORBIDDEN", "Tenant access denied")
					return
				}
				in.TenantID = tenant
			}
			if in.TenantID == "" {
				in.TenantID = store.DefaultTenantID
			}
			plain, t, err := tokens.create(r.Context(), desktopMCPToken{Name: in.Name, TenantID: in.TenantID, UserID: in.UserID, Scope: in.Scope,
				AllowBash: in.AllowBash, AllowInstall: in.AllowInstall, CreatedBy: by})
			if err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
				return
			}
			writeDesktopMCPCreated(w, plain, t)
		case http.MethodDelete:
			ok, err := tokens.remove(r.Context(), r.URL.Query().Get("id"), mine)
			if err != nil || !ok {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "token not found")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
	admin := requireTenantAdmin(func(w http.ResponseWriter, r *http.Request) {
		a := AdminFromContext(r.Context())
		serve(w, r, AdminTenantID(r.Context()), "admin:"+a.Username)
	})
	return func(w http.ResponseWriter, r *http.Request) {
		if service := desktopAPIToken(); service != "" {
			got := desktopmcp.BearerToken(r)
			if len(got) == len(service) && subtle.ConstantTimeCompare([]byte(got), []byte(service)) == 1 {
				serve(w, r, "", "service")
				return
			}
		}
		admin(w, r)
	}
}
