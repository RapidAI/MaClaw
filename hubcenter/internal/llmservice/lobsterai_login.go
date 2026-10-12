package llmservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
)

const (
	lobsterAIStatusPending = "pending"
	lobsterAIStatusReady   = "ready"
	lobsterAIStatusError   = "error"

	lobsterAILoginKeep = 15 * time.Minute
	lobsterAIMaxLogins = 32
)

// LobsterAILoginStart is the portal page for one admin login.
type LobsterAILoginStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
}

// LobsterAIModel is one catalog entry the admin can connect.
type LobsterAIModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// LobsterAILoginStatus is safe to return to the admin UI. It never includes tokens.
type LobsterAILoginStatus struct {
	Status         string          `json:"status"`
	AuthURL        string          `json:"auth_url,omitempty"`
	Models         []LobsterAIModel `json:"models,omitempty"`
	Error          string          `json:"error,omitempty"`
	CatalogWarning string          `json:"catalog_warning,omitempty"`
}

type lobsterAILogin struct {
	mu        sync.Mutex
	id        string
	authURL   string
	status    string
	err       string
	warning   string
	token     *lobsterai.Token
	models    []LobsterAIModel
	session   *lobsterai.LoginSession
	cancel    context.CancelFunc
	keepUntil time.Time
}

func (s *Service) lobsterAIMap() map[string]*lobsterAILogin {
	if s.lobsterAILogins == nil {
		s.lobsterAILogins = map[string]*lobsterAILogin{}
	}
	return s.lobsterAILogins
}

// StartLobsterAILogin begins a portal browser login. The SPA redirects back to
// a local callback listener inside this process, so the session cannot be
// moved to another node.
func (s *Service) StartLobsterAILogin() (LobsterAILoginStart, error) {
	return s.startLobsterAILogin()
}

func (s *Service) startLobsterAILogin() (LobsterAILoginStart, error) {
	if s == nil {
		return LobsterAILoginStart{}, fmt.Errorf("llm service is required")
	}
	// An ephemeral port keeps two concurrent logins from fighting over a fixed
	// callback port; the listener reports the port it bound and the login URL
	// advertises exactly that one.
	session, err := lobsterai.StartLogin(0)
	if err != nil {
		return LobsterAILoginStart{}, err
	}
	loginURL := session.LoginURL
	if !lobsteraiIsApprovalURL(loginURL) {
		session.Cancel()
		return LobsterAILoginStart{}, fmt.Errorf("LobsterAI 授权地址无效")
	}
	id, err := newLoginSessionID()
	if err != nil {
		session.Cancel()
		return LobsterAILoginStart{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), lobsterai.LoginLifetime)
	rec := &lobsterAILogin{
		id:        id,
		authURL:   loginURL,
		status:    lobsterAIStatusPending,
		session:   session,
		cancel:    cancel,
		keepUntil: time.Now().Add(lobsterai.LoginLifetime + 6*time.Minute),
	}
	s.lobsterAIMu.Lock()
	s.sweepLobsterAILoginsLocked(time.Now())
	if len(s.lobsterAIMap()) >= lobsterAIMaxLogins {
		s.lobsterAIMu.Unlock()
		cancel()
		session.Cancel()
		return LobsterAILoginStart{}, fmt.Errorf("too many LobsterAI logins in progress")
	}
	s.lobsterAIMap()[id] = rec
	s.lobsterAIMu.Unlock()
	go s.pollLobsterAILogin(ctx, rec)
	return LobsterAILoginStart{SessionID: id, AuthURL: loginURL}, nil
}

// lobsteraiIsApprovalURL accepts the portal page on its own https origin, or
// loopback http for a local test server.
func lobsteraiIsApprovalURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	host := strings.ToLower(trimmed)
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	if !strings.HasPrefix(host, "https://") {
		return strings.HasPrefix(host, "http://127.0.0.1") || strings.HasPrefix(host, "http://localhost") || strings.HasPrefix(host, "http://[::1]")
	}
	return strings.HasPrefix(host, lobsterai.PortalBase+"/")
}

func (s *Service) pollLobsterAILogin(ctx context.Context, rec *lobsterAILogin) {
	if rec == nil || rec.session == nil {
		return
	}
	defer func() {
		rec.session.Cancel()
		if rec.cancel != nil {
			rec.cancel()
		}
	}()
	code, err := rec.session.Wait(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failLobsterAILogin(rec, err.Error())
		return
	}
	token, err := rec.session.Resolve(ctx, code)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failLobsterAILogin(rec, err.Error())
		return
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		s.failLobsterAILogin(rec, "LobsterAI 登录未返回访问令牌")
		return
	}
	s.finishLobsterAILogin(rec, token)
}

func (s *Service) finishLobsterAILogin(rec *lobsterAILogin, token *lobsterai.Token) {
	if rec == nil || token == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	warning := ""
	models, err := lobsterai.ListModels(ctx, token.AccessToken)
	var specs []LobsterAIModel
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		specs = append(specs, LobsterAIModel{ID: id, Name: name})
	}
	if err != nil || len(specs) == 0 {
		log.Printf("[llm] lobsterai catalog: %v", err)
		warning = "live catalog unavailable"
		specs = lobsterAIFallbackSpecs()
	}
	if len(specs) == 0 {
		s.failLobsterAILogin(rec, "LobsterAI returned no models")
		return
	}
	rec.mu.Lock()
	rec.status = lobsterAIStatusReady
	rec.token = token
	rec.models = specs
	rec.warning = warning
	rec.err = ""
	rec.keepUntil = time.Now().Add(lobsterAILoginKeep)
	rec.mu.Unlock()
}

func lobsterAIFallbackSpecs() []LobsterAIModel {
	out := make([]LobsterAIModel, 0, 8)
	for _, model := range lobsterai.ModelList() {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(model.Name)
		if name == "" {
			name = id
		}
		out = append(out, LobsterAIModel{ID: id, Name: name})
	}
	return out
}

func (s *Service) failLobsterAILogin(rec *lobsterAILogin, message string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	if rec.status == lobsterAIStatusReady {
		rec.mu.Unlock()
		return
	}
	rec.status = lobsterAIStatusError
	rec.err = strings.TrimSpace(message)
	if rec.err == "" {
		rec.err = "LobsterAI login failed"
	}
	rec.keepUntil = time.Now().Add(2 * time.Minute)
	rec.mu.Unlock()
}

// LobsterAILoginStatus reports pending, ready (with models), or error.
func (s *Service) LobsterAILoginStatus(id string) (LobsterAILoginStatus, error) {
	rec := s.lookupLobsterAILogin(id)
	if rec == nil {
		return LobsterAILoginStatus{}, fmt.Errorf("LobsterAI login session not found")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := LobsterAILoginStatus{
		Status:         rec.status,
		AuthURL:        rec.authURL,
		CatalogWarning: rec.warning,
		Error:          rec.err,
	}
	if rec.status == lobsterAIStatusReady {
		out.Models = append([]LobsterAIModel(nil), rec.models...)
	}
	return out, nil
}

// CancelLobsterAILogin stops a login the admin abandoned.
func (s *Service) CancelLobsterAILogin(id string) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.lobsterAIMu.Lock()
	rec := s.lobsterAIMap()[id]
	delete(s.lobsterAILogins, id)
	s.lobsterAIMu.Unlock()
	if rec == nil {
		return
	}
	if rec.session != nil {
		rec.session.Cancel()
	}
	if rec.cancel != nil {
		rec.cancel()
	}
}

func (s *Service) ForgetLobsterAILogin(id string) {
	s.CancelLobsterAILogin(id)
}

func (s *Service) lookupLobsterAILogin(id string) *lobsterAILogin {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil
	}
	s.lobsterAIMu.Lock()
	defer s.lobsterAIMu.Unlock()
	s.sweepLobsterAILoginsLocked(time.Now())
	return s.lobsterAIMap()[id]
}

func (s *Service) sweepLobsterAILoginsLocked(now time.Time) {
	for id, rec := range s.lobsterAILogins {
		if rec == nil {
			delete(s.lobsterAILogins, id)
			continue
		}
		rec.mu.Lock()
		dead := now.After(rec.keepUntil)
		cancel := rec.cancel
		rec.mu.Unlock()
		if !dead {
			continue
		}
		delete(s.lobsterAILogins, id)
		if cancel != nil {
			cancel()
		}
	}
}

// ApplyLobsterAILogin copies a finished login onto a provider and keeps only
// the models the admin selected. creating requires a finished login when the
// provider is new. Updates without a session keep the saved account.
func (s *Service) ApplyLobsterAILogin(provider *llmpool.ProviderConfig, creating bool) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	sessionID := strings.TrimSpace(provider.LobsterAISessionID)
	kind := strings.TrimSpace(provider.AuthKind)
	if kind == llmpool.ProviderAuthAPIKey {
		provider.AuthKind = llmpool.ProviderAuthAPIKey
		provider.LobsterAISessionID = ""
		clearLobsterAISecrets(provider)
		return nil
	}
	if sessionID == "" {
		provider.LobsterAISessionID = ""
		if kind != llmpool.ProviderAuthLobsterAI {
			return nil
		}
		if creating && strings.TrimSpace(provider.APIKey) == "" {
			return fmt.Errorf("LobsterAI login is required")
		}
		profile := lobsterai.ProductProfile()
		provider.AuthKind = llmpool.ProviderAuthLobsterAI
		provider.APIURL = lobsterai.CanonicalChatURL(profile.APIBase)
		provider.Protocol = "openai"
		provider.WireAPI = ""
		if strings.TrimSpace(provider.Name) == "" {
			provider.Name = profile.Name
		}
		return nil
	}
	if s == nil {
		return fmt.Errorf("llm service is required")
	}
	rec := s.lookupLobsterAILogin(sessionID)
	if rec == nil {
		return fmt.Errorf("LobsterAI login session not found")
	}
	rec.mu.Lock()
	status := rec.status
	errText := rec.err
	var token *lobsterai.Token
	if rec.token != nil {
		copied := *rec.token
		token = &copied
	}
	models := append([]LobsterAIModel(nil), rec.models...)
	rec.mu.Unlock()
	if status != lobsterAIStatusReady || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		if strings.TrimSpace(errText) == "" {
			errText = "LobsterAI login is not finished"
		}
		return errors.New(errText)
	}
	selected, err := filterLobsterAIModels(provider.Models, models)
	if err != nil {
		return err
	}
	profile := lobsterai.ProductProfile()
	provider.AuthKind = llmpool.ProviderAuthLobsterAI
	provider.APIURL = lobsterai.CanonicalChatURL(profile.APIBase)
	provider.APIKey = token.AccessToken
	provider.LobsterAIRefreshToken = token.RefreshToken
	provider.LobsterAIExpiresAt = token.ExpiresAt
	provider.LobsterAISessionUUID = token.UUID
	provider.LobsterAIFirstKeyfrom = token.FirstKeyfrom
	provider.LobsterAIUserID = token.UserID
	provider.Protocol = "openai"
	provider.WireAPI = ""
	provider.Models = selected
	provider.LobsterAISessionID = ""
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = profile.Name
	}
	return nil
}

func filterLobsterAIModels(selected []string, catalog []LobsterAIModel) ([]string, error) {
	allowed := make(map[string]struct{}, len(catalog))
	for _, model := range catalog {
		if id := strings.TrimSpace(model.ID); id != "" {
			allowed[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(selected))
	seen := map[string]bool{}
	for _, raw := range selected {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		if _, ok := allowed[id]; !ok {
			return nil, fmt.Errorf("model %s is not in the LobsterAI catalog", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("select at least one LobsterAI model")
	}
	return out, nil
}

func clearLobsterAISecrets(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.LobsterAIRefreshToken = ""
	provider.LobsterAIExpiresAt = 0
	provider.LobsterAISessionUUID = ""
	provider.LobsterAIFirstKeyfrom = ""
	provider.LobsterAIUserID = ""
}
