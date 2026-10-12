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
	"github.com/RapidAI/CodeClaw/corelib/qoder"
)

const (
	qoderStatusPending = "pending"
	qoderStatusReady   = "ready"
	qoderStatusError   = "error"

	qoderLoginKeep = 15 * time.Minute
	qoderMaxLogins = 32
)

// QoderLoginStart is the authorization page for one admin login.
type QoderLoginStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
	Edition   string `json:"edition"`
}

// QoderModel is one catalog entry the admin can connect.
type QoderModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length,omitempty"`
	MaxOutput     int64  `json:"max_output,omitempty"`
}

// QoderLoginStatus is safe to return to the admin UI. It never includes tokens.
type QoderLoginStatus struct {
	Status         string       `json:"status"`
	Edition        string       `json:"edition,omitempty"`
	AuthURL        string       `json:"auth_url,omitempty"`
	Models         []QoderModel `json:"models,omitempty"`
	Error          string       `json:"error,omitempty"`
	CatalogWarning string       `json:"catalog_warning,omitempty"`
}

type qoderLogin struct {
	mu        sync.Mutex
	id        string
	edition   string
	profile   qoder.Profile
	authURL   string
	status    string
	err       string
	warning   string
	token     *qoder.Token
	models    []QoderModel
	login     *qoder.DeviceLogin
	cancel    context.CancelFunc
	keepUntil time.Time
}

func (s *Service) qoderMap() map[string]*qoderLogin {
	if s.qoderLogins == nil {
		s.qoderLogins = map[string]*qoderLogin{}
	}
	return s.qoderLogins
}

// StartQoderLogin begins a domestic or international Qoder device login. The
// returned URL is opened by the admin's browser; the poll runs server-side
// against the OpenAPI host, so the session must stay on this process.
func (s *Service) StartQoderLogin(edition string) (QoderLoginStart, error) {
	profile, ok := qoder.ProfileByEdition(edition)
	if !ok {
		return QoderLoginStart{}, fmt.Errorf("unknown Qoder edition %q", strings.TrimSpace(edition))
	}
	edition = canonicalQoderEdition(edition)
	return s.startQoderLogin(profile, edition)
}

func canonicalQoderEdition(edition string) string {
	profile, ok := qoder.ProfileByEdition(edition)
	if !ok {
		return strings.TrimSpace(edition)
	}
	if profile.ID == qoder.GlobalProfile().ID {
		return "global"
	}
	return "china"
}

func (s *Service) startQoderLogin(profile qoder.Profile, edition string) (QoderLoginStart, error) {
	if s == nil {
		return QoderLoginStart{}, fmt.Errorf("llm service is required")
	}
	login, err := qoder.StartLogin(qoder.MachineID())
	if err != nil {
		return QoderLoginStart{}, err
	}
	approvalURL := login.AuthURL(profile)
	if !qoder.IsApprovalURL(approvalURL) {
		return QoderLoginStart{}, fmt.Errorf("Qoder 授权地址无效")
	}
	id, err := newQoderSessionID()
	if err != nil {
		return QoderLoginStart{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), qoder.LoginLifetime())
	rec := &qoderLogin{
		id:        id,
		edition:   edition,
		profile:   profile,
		authURL:   approvalURL,
		status:    qoderStatusPending,
		login:     login,
		cancel:    cancel,
		keepUntil: time.Now().Add(qoder.LoginLifetime() + 6*time.Minute),
	}
	s.qoderMu.Lock()
	s.sweepQoderLoginsLocked(time.Now())
	if len(s.qoderMap()) >= qoderMaxLogins {
		s.qoderMu.Unlock()
		cancel()
		return QoderLoginStart{}, fmt.Errorf("too many Qoder logins in progress")
	}
	s.qoderMap()[id] = rec
	s.qoderMu.Unlock()
	go s.pollQoderLogin(ctx, rec)
	return QoderLoginStart{SessionID: id, AuthURL: approvalURL, Edition: edition}, nil
}

func (s *Service) pollQoderLogin(ctx context.Context, rec *qoderLogin) {
	if rec == nil || rec.login == nil {
		return
	}
	defer func() {
		if rec.cancel != nil {
			rec.cancel()
		}
	}()
	token, err := qoder.PollUntilAuthorized(ctx, rec.profile, rec.login)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		s.failQoderLogin(rec, err.Error())
		return
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		s.failQoderLogin(rec, "Qoder 登录未返回访问令牌")
		return
	}
	copied := token
	s.finishQoderLogin(rec, &copied)
}

func (s *Service) finishQoderLogin(rec *qoderLogin, token *qoder.Token) {
	if rec == nil || token == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	warning := ""
	// The catalog rides the signed /algo contract on the site inference host;
	// plain fetches can 404, so keep the CLI's own default model when that
	// happens instead of failing a login the user already approved.
	models, defaultKey, err := qoder.ListModels(ctx, rec.profile, token.UserID, token.AccessToken)
	specs := qoderCatalogSpecs(models)
	if err != nil || len(specs) == 0 {
		log.Printf("[llm] qoder catalog edition=%s: %v", rec.edition, err)
		warning = "live catalog unavailable"
		specs = qoderFallbackSpecs(defaultKey)
	}
	out := make([]QoderModel, 0, len(specs))
	seen := map[string]bool{}
	for _, spec := range specs {
		if spec.ID == "" || seen[spec.ID] {
			continue
		}
		seen[spec.ID] = true
		out = append(out, QoderModel{
			ID:            spec.ID,
			Name:          spec.Name,
			ContextLength: spec.ContextLength,
			MaxOutput:     spec.MaxOutput,
		})
	}
	if len(out) == 0 {
		s.failQoderLogin(rec, "Qoder returned no models")
		return
	}
	rec.mu.Lock()
	rec.status = qoderStatusReady
	rec.token = token
	rec.models = out
	rec.warning = warning
	rec.err = ""
	rec.keepUntil = time.Now().Add(qoderLoginKeep)
	rec.mu.Unlock()
}

func (s *Service) failQoderLogin(rec *qoderLogin, message string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	if rec.status == qoderStatusReady {
		rec.mu.Unlock()
		return
	}
	rec.status = qoderStatusError
	rec.err = strings.TrimSpace(message)
	if rec.err == "" {
		rec.err = "Qoder login failed"
	}
	rec.keepUntil = time.Now().Add(2 * time.Minute)
	rec.mu.Unlock()
}

// QoderLoginStatus reports pending, ready (with models), or error.
func (s *Service) QoderLoginStatus(id string) (QoderLoginStatus, error) {
	rec := s.lookupQoderLogin(id)
	if rec == nil {
		return QoderLoginStatus{}, fmt.Errorf("Qoder login session not found")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := QoderLoginStatus{
		Status:         rec.status,
		Edition:        rec.edition,
		AuthURL:        rec.authURL,
		CatalogWarning: rec.warning,
		Error:          rec.err,
	}
	if rec.status == qoderStatusReady {
		out.Models = append([]QoderModel(nil), rec.models...)
	}
	return out, nil
}

// CancelQoderLogin stops a login the admin abandoned.
func (s *Service) CancelQoderLogin(id string) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.qoderMu.Lock()
	rec := s.qoderMap()[id]
	delete(s.qoderLogins, id)
	s.qoderMu.Unlock()
	if rec != nil && rec.cancel != nil {
		rec.cancel()
	}
}

func (s *Service) ForgetQoderLogin(id string) {
	s.CancelQoderLogin(id)
}

func (s *Service) lookupQoderLogin(id string) *qoderLogin {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil
	}
	s.qoderMu.Lock()
	defer s.qoderMu.Unlock()
	s.sweepQoderLoginsLocked(time.Now())
	return s.qoderMap()[id]
}

func (s *Service) sweepQoderLoginsLocked(now time.Time) {
	for id, rec := range s.qoderLogins {
		if rec == nil {
			delete(s.qoderLogins, id)
			continue
		}
		rec.mu.Lock()
		dead := now.After(rec.keepUntil)
		cancel := rec.cancel
		rec.mu.Unlock()
		if !dead {
			continue
		}
		delete(s.qoderLogins, id)
		if cancel != nil {
			cancel()
		}
	}
}

// qoderCatalogSpecs flattens the server catalog into the login view. The
// catalog repeats one model across scenes, so each key appears once with the
// widest token window reported for it.
func qoderCatalogSpecs(models []qoder.Model) []QoderModel {
	type entry struct {
		name    string
		enabled bool
		maxIn   int
		maxOut  int
	}
	byKey := map[string]*entry{}
	order := make([]string, 0, len(models))
	for _, model := range models {
		key := strings.TrimSpace(model.Key)
		if key == "" {
			continue
		}
		got, ok := byKey[key]
		if !ok {
			got = &entry{}
			byKey[key] = got
			order = append(order, key)
		}
		if name := strings.TrimSpace(model.DisplayName); name != "" && got.name == "" {
			got.name = name
		}
		if model.Enabled {
			got.enabled = true
		}
		if model.MaxInput > got.maxIn {
			got.maxIn = model.MaxInput
		}
		if model.MaxOutput > got.maxOut {
			got.maxOut = model.MaxOutput
		}
	}
	out := make([]QoderModel, 0, len(order))
	for _, key := range order {
		got := byKey[key]
		if !got.enabled {
			continue
		}
		name := got.name
		if name == "" {
			name = key
		}
		out = append(out, QoderModel{
			ID:            key,
			Name:          name,
			ContextLength: int64(got.maxIn),
			MaxOutput:     int64(got.maxOut),
		})
	}
	return out
}

// qoderFallbackSpecs is the CLI's own default model, kept when the live catalog
// cannot be read. defaultKey is the server's default when the call did answer.
func qoderFallbackSpecs(defaultKey string) []QoderModel {
	key := strings.TrimSpace(defaultKey)
	if key == "" {
		key = qoder.DefaultModel
	}
	return []QoderModel{{ID: key, Name: key}}
}

// ApplyQoderLogin copies a finished login onto a provider and keeps only the
// models the admin selected. creating requires a finished login when the
// provider is new. Updates without a session keep the saved account.
func (s *Service) ApplyQoderLogin(provider *llmpool.ProviderConfig, creating bool) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	sessionID := strings.TrimSpace(provider.QoderSessionID)
	kind := strings.TrimSpace(provider.AuthKind)
	if kind == llmpool.ProviderAuthAPIKey {
		provider.AuthKind = llmpool.ProviderAuthAPIKey
		provider.QoderSessionID = ""
		clearQoderSecrets(provider)
		return nil
	}
	if sessionID == "" {
		provider.QoderSessionID = ""
		if kind != llmpool.ProviderAuthQoder {
			return nil
		}
		profile, ok := qoder.ProfileByEdition(provider.QoderEdition)
		if !ok {
			return fmt.Errorf("unknown Qoder edition %q", provider.QoderEdition)
		}
		if creating && strings.TrimSpace(provider.APIKey) == "" {
			return fmt.Errorf("Qoder login is required")
		}
		provider.AuthKind = llmpool.ProviderAuthQoder
		provider.QoderEdition = canonicalQoderEdition(provider.QoderEdition)
		provider.APIURL = qoder.CanonicalChatURL(profile.ChatURL)
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
	rec := s.lookupQoderLogin(sessionID)
	if rec == nil {
		return fmt.Errorf("Qoder login session not found")
	}
	rec.mu.Lock()
	status := rec.status
	errText := rec.err
	edition := rec.edition
	var token *qoder.Token
	if rec.token != nil {
		copied := *rec.token
		token = &copied
	}
	models := append([]QoderModel(nil), rec.models...)
	rec.mu.Unlock()
	if status != qoderStatusReady || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		if strings.TrimSpace(errText) == "" {
			errText = "Qoder login is not finished"
		}
		return errors.New(errText)
	}
	if requested := strings.TrimSpace(provider.QoderEdition); requested != "" && !strings.EqualFold(canonicalQoderEdition(requested), edition) {
		return fmt.Errorf("Qoder edition does not match the login session")
	}
	selected, err := filterQoderModels(provider.Models, models)
	if err != nil {
		return err
	}
	profile, ok := qoder.ProfileByEdition(edition)
	if !ok {
		return fmt.Errorf("unknown Qoder edition %q", edition)
	}
	provider.AuthKind = llmpool.ProviderAuthQoder
	provider.QoderEdition = edition
	provider.APIURL = qoder.CanonicalChatURL(profile.ChatURL)
	provider.APIKey = token.AccessToken
	provider.QoderRefreshToken = token.RefreshToken
	provider.QoderExpiresAt = token.ExpiresAt
	provider.QoderUserID = token.UserID
	provider.QoderMachineID = qoder.MachineID()
	provider.Protocol = "openai"
	provider.WireAPI = ""
	provider.Models = selected
	provider.QoderSessionID = ""
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = profile.Name
	}
	return nil
}

func filterQoderModels(selected []string, catalog []QoderModel) ([]string, error) {
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
			return nil, fmt.Errorf("model %s is not in the Qoder catalog", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("select at least one Qoder model")
	}
	return out, nil
}

func clearQoderSecrets(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.QoderEdition = ""
	provider.QoderRefreshToken = ""
	provider.QoderExpiresAt = 0
	provider.QoderUserID = ""
	provider.QoderMachineID = ""
}

func newQoderSessionID() (string, error) {
	return newLoginSessionID()
}
