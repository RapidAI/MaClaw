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
	"github.com/RapidAI/CodeClaw/corelib/trae"
)

const (
	traeStatusPending = "pending"
	traeStatusReady   = "ready"
	traeStatusError   = "error"

	traeLoginKeep = 15 * time.Minute
	traeMaxLogins = 32
)

// TraeLoginStart is the authorization page for one admin login.
type TraeLoginStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
	Edition   string `json:"edition"`
}

// TraeModel is one catalog entry the admin can connect.
type TraeModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TraeLoginStatus is safe to return to the admin UI. It never includes tokens.
type TraeLoginStatus struct {
	Status         string      `json:"status"`
	Edition        string      `json:"edition,omitempty"`
	AuthURL        string      `json:"auth_url,omitempty"`
	Models         []TraeModel `json:"models,omitempty"`
	Error          string      `json:"error,omitempty"`
	CatalogWarning string      `json:"catalog_warning,omitempty"`
}

type traeLogin struct {
	mu        sync.Mutex
	id        string
	edition   string
	profile   trae.Profile
	authURL   string
	status    string
	err       string
	warning   string
	token     *trae.Token
	models    []TraeModel
	session   *trae.LoginSession
	cancel    context.CancelFunc
	keepUntil time.Time
}

func (s *Service) traeMap() map[string]*traeLogin {
	if s.traeLogins == nil {
		s.traeLogins = map[string]*traeLogin{}
	}
	return s.traeLogins
}

// StartTraeLogin begins a domestic or international Trae browser login. The
// console redirects back to a local callback listener inside this process, so
// the session cannot be moved to another node.
func (s *Service) StartTraeLogin(edition string) (TraeLoginStart, error) {
	profile, ok := trae.ProfileByEdition(edition)
	if !ok {
		return TraeLoginStart{}, fmt.Errorf("unknown Trae edition %q", strings.TrimSpace(edition))
	}
	edition = canonicalTraeEdition(edition)
	return s.startTraeLogin(profile, edition)
}

func canonicalTraeEdition(edition string) string {
	profile, ok := trae.ProfileByEdition(edition)
	if !ok {
		return strings.TrimSpace(edition)
	}
	if profile.ID == trae.GlobalProfile().ID {
		return "global"
	}
	return "china"
}

func (s *Service) startTraeLogin(profile trae.Profile, edition string) (TraeLoginStart, error) {
	if s == nil {
		return TraeLoginStart{}, fmt.Errorf("llm service is required")
	}
	// An ephemeral port keeps two concurrent logins from fighting over the
	// fixed production callback port; the listener reports the port it bound
	// and the authorize URL advertises exactly that one.
	session, authURL, err := trae.StartLogin(profile, 0)
	if err != nil {
		return TraeLoginStart{}, err
	}
	if !trae.IsApprovalURL(authURL) {
		session.Cancel()
		return TraeLoginStart{}, fmt.Errorf("Trae 授权地址无效")
	}
	id, err := newLoginSessionID()
	if err != nil {
		session.Cancel()
		return TraeLoginStart{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), trae.LoginLifetime)
	rec := &traeLogin{
		id:        id,
		edition:   edition,
		profile:   profile,
		authURL:   authURL,
		status:    traeStatusPending,
		session:   session,
		cancel:    cancel,
		keepUntil: time.Now().Add(trae.LoginLifetime + 6*time.Minute),
	}
	s.traeMu.Lock()
	s.sweepTraeLoginsLocked(time.Now())
	if len(s.traeMap()) >= traeMaxLogins {
		s.traeMu.Unlock()
		cancel()
		session.Cancel()
		return TraeLoginStart{}, fmt.Errorf("too many Trae logins in progress")
	}
	s.traeMap()[id] = rec
	s.traeMu.Unlock()
	go s.pollTraeLogin(ctx, rec)
	return TraeLoginStart{SessionID: id, AuthURL: authURL, Edition: edition}, nil
}

func (s *Service) pollTraeLogin(ctx context.Context, rec *traeLogin) {
	if rec == nil || rec.session == nil {
		return
	}
	defer func() {
		rec.session.Cancel()
		if rec.cancel != nil {
			rec.cancel()
		}
	}()
	details, err := rec.session.Wait(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failTraeLogin(rec, err.Error())
		return
	}
	token, err := rec.session.Resolve(ctx, rec.profile, details)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failTraeLogin(rec, err.Error())
		return
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		s.failTraeLogin(rec, "Trae 登录未返回访问令牌")
		return
	}
	if strings.TrimSpace(token.UserID) == "" {
		// The display name rides the account id; a missing id means the
		// upstream stripped the account details. Best effort fill.
		if user, name, _, infoErr := trae.GetUserInfo(ctx, rec.profile, token); infoErr == nil {
			token.UserID = strings.TrimSpace(user)
			token.DisplayName = strings.TrimSpace(name)
		} else {
			log.Printf("[llm] trae provider=%s user info unavailable: %v", rec.profile.Name, infoErr)
		}
	}
	s.finishTraeLogin(rec, token)
}

func (s *Service) finishTraeLogin(rec *traeLogin, token *trae.Token) {
	if rec == nil || token == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	warning := ""
	models, _, err := trae.ListModels(ctx, rec.profile, token.AccessToken, token.MachineID, token.DeviceID)
	var specs []TraeModel
	for _, model := range models {
		id := strings.TrimSpace(model.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(model.DisplayName)
		if name == "" {
			name = id
		}
		specs = append(specs, TraeModel{ID: id, Name: name})
	}
	if err != nil || len(specs) == 0 {
		log.Printf("[llm] trae catalog edition=%s: %v", rec.edition, err)
		warning = "live catalog unavailable"
		specs = traeFallbackSpecs(rec.profile)
	}
	if len(specs) == 0 {
		s.failTraeLogin(rec, "Trae returned no models")
		return
	}
	rec.mu.Lock()
	rec.status = traeStatusReady
	rec.token = token
	rec.models = specs
	rec.warning = warning
	rec.err = ""
	rec.keepUntil = time.Now().Add(traeLoginKeep)
	rec.mu.Unlock()
}

func traeFallbackSpecs(profile trae.Profile) []TraeModel {
	return []TraeModel{{ID: profile.DefaultModel, Name: profile.DefaultModel}}
}

func (s *Service) failTraeLogin(rec *traeLogin, message string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	if rec.status == traeStatusReady {
		rec.mu.Unlock()
		return
	}
	rec.status = traeStatusError
	rec.err = strings.TrimSpace(message)
	if rec.err == "" {
		rec.err = "Trae login failed"
	}
	rec.keepUntil = time.Now().Add(2 * time.Minute)
	rec.mu.Unlock()
}

// TraeLoginStatus reports pending, ready (with models), or error.
func (s *Service) TraeLoginStatus(id string) (TraeLoginStatus, error) {
	rec := s.lookupTraeLogin(id)
	if rec == nil {
		return TraeLoginStatus{}, fmt.Errorf("Trae login session not found")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := TraeLoginStatus{
		Status:         rec.status,
		Edition:        rec.edition,
		AuthURL:        rec.authURL,
		CatalogWarning: rec.warning,
		Error:          rec.err,
	}
	if rec.status == traeStatusReady {
		out.Models = append([]TraeModel(nil), rec.models...)
	}
	return out, nil
}

// CancelTraeLogin stops a login the admin abandoned.
func (s *Service) CancelTraeLogin(id string) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.traeMu.Lock()
	rec := s.traeMap()[id]
	delete(s.traeLogins, id)
	s.traeMu.Unlock()
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

func (s *Service) ForgetTraeLogin(id string) {
	s.CancelTraeLogin(id)
}

func (s *Service) lookupTraeLogin(id string) *traeLogin {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil
	}
	s.traeMu.Lock()
	defer s.traeMu.Unlock()
	s.sweepTraeLoginsLocked(time.Now())
	return s.traeMap()[id]
}

func (s *Service) sweepTraeLoginsLocked(now time.Time) {
	for id, rec := range s.traeLogins {
		if rec == nil {
			delete(s.traeLogins, id)
			continue
		}
		rec.mu.Lock()
		dead := now.After(rec.keepUntil)
		cancel := rec.cancel
		rec.mu.Unlock()
		if !dead {
			continue
		}
		delete(s.traeLogins, id)
		if cancel != nil {
			cancel()
		}
	}
}

// ApplyTraeLogin copies a finished login onto a provider and keeps only the
// models the admin selected. creating requires a finished login when the
// provider is new. Updates without a session keep the saved account.
func (s *Service) ApplyTraeLogin(provider *llmpool.ProviderConfig, creating bool) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	sessionID := strings.TrimSpace(provider.TraeSessionID)
	kind := strings.TrimSpace(provider.AuthKind)
	if kind == llmpool.ProviderAuthAPIKey {
		provider.AuthKind = llmpool.ProviderAuthAPIKey
		provider.TraeSessionID = ""
		clearTraeSecrets(provider)
		return nil
	}
	if sessionID == "" {
		provider.TraeSessionID = ""
		if kind != llmpool.ProviderAuthTrae {
			return nil
		}
		profile, ok := trae.ProfileByEdition(provider.TraeEdition)
		if !ok {
			return fmt.Errorf("unknown Trae edition %q", provider.TraeEdition)
		}
		if creating && strings.TrimSpace(provider.APIKey) == "" {
			return fmt.Errorf("Trae login is required")
		}
		provider.AuthKind = llmpool.ProviderAuthTrae
		provider.TraeEdition = canonicalTraeEdition(provider.TraeEdition)
		provider.APIURL = trae.CanonicalChatURL(profile, profile.ChatHost)
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
	rec := s.lookupTraeLogin(sessionID)
	if rec == nil {
		return fmt.Errorf("Trae login session not found")
	}
	rec.mu.Lock()
	status := rec.status
	errText := rec.err
	edition := rec.edition
	var token *trae.Token
	if rec.token != nil {
		copied := *rec.token
		token = &copied
	}
	models := append([]TraeModel(nil), rec.models...)
	rec.mu.Unlock()
	if status != traeStatusReady || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		if strings.TrimSpace(errText) == "" {
			errText = "Trae login is not finished"
		}
		return errors.New(errText)
	}
	if requested := strings.TrimSpace(provider.TraeEdition); requested != "" && !strings.EqualFold(canonicalTraeEdition(requested), edition) {
		return fmt.Errorf("Trae edition does not match the login session")
	}
	selected, err := filterTraeModels(provider.Models, models)
	if err != nil {
		return err
	}
	profile, ok := trae.ProfileByEdition(edition)
	if !ok {
		return fmt.Errorf("unknown Trae edition %q", edition)
	}
	provider.AuthKind = llmpool.ProviderAuthTrae
	provider.TraeEdition = edition
	provider.APIURL = trae.CanonicalChatURL(profile, profile.ChatHost)
	provider.APIKey = token.AccessToken
	provider.TraeRefreshToken = token.RefreshToken
	provider.TraeExpiresAt = token.ExpiresAt
	provider.TraeUserID = token.UserID
	provider.TraeMachineID = token.MachineID
	provider.TraeDeviceID = token.DeviceID
	provider.Protocol = "openai"
	provider.WireAPI = ""
	provider.Models = selected
	provider.TraeSessionID = ""
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = profile.Name
	}
	return nil
}

func filterTraeModels(selected []string, catalog []TraeModel) ([]string, error) {
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
			return nil, fmt.Errorf("model %s is not in the Trae catalog", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("select at least one Trae model")
	}
	return out, nil
}

func clearTraeSecrets(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.TraeEdition = ""
	provider.TraeRefreshToken = ""
	provider.TraeExpiresAt = 0
	provider.TraeUserID = ""
	provider.TraeMachineID = ""
	provider.TraeDeviceID = ""
}
