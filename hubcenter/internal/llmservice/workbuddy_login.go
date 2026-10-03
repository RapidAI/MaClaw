package llmservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/workbuddy"
)

const (
	workBuddyStatusPending = "pending"
	workBuddyStatusReady   = "ready"
	workBuddyStatusError   = "error"

	workBuddyLoginKeep = 15 * time.Minute
	workBuddyMaxLogins = 32
)

// WorkBuddyLoginStart is the authorization page for one admin login.
type WorkBuddyLoginStart struct {
	SessionID string `json:"session_id"`
	AuthURL   string `json:"auth_url"`
	Edition   string `json:"edition"`
}

// WorkBuddyModel is one catalog entry the admin can connect.
type WorkBuddyModel struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	ContextLength int64  `json:"context_length,omitempty"`
	MaxOutput     int64  `json:"max_output,omitempty"`
}

// WorkBuddyLoginStatus is safe to return to the admin UI. It never includes tokens.
type WorkBuddyLoginStatus struct {
	Status         string           `json:"status"`
	Edition        string           `json:"edition,omitempty"`
	AuthURL        string           `json:"auth_url,omitempty"`
	Models         []WorkBuddyModel `json:"models,omitempty"`
	Error          string           `json:"error,omitempty"`
	CatalogWarning string           `json:"catalog_warning,omitempty"`
}

type workBuddyLogin struct {
	mu        sync.Mutex
	id        string
	edition   string
	profile   workbuddy.Profile
	authURL   string
	status    string
	err       string
	warning   string
	cred      *workbuddy.AccountCredential
	models    []WorkBuddyModel
	session   *workbuddy.LoginSession
	cancel    context.CancelFunc
	keepUntil time.Time
}

func (s *Service) workBuddyMap() map[string]*workBuddyLogin {
	if s.workBuddyLogins == nil {
		s.workBuddyLogins = map[string]*workBuddyLogin{}
	}
	return s.workBuddyLogins
}

// StartWorkBuddyLogin begins a domestic or international WorkBuddy login.
// The returned URL is opened by the admin's browser. The session itself stays
// on this process because the upstream binds it to cookies.
func (s *Service) StartWorkBuddyLogin(edition string) (WorkBuddyLoginStart, error) {
	profile, ok := workbuddy.ProfileByEdition(edition)
	if !ok {
		return WorkBuddyLoginStart{}, fmt.Errorf("unknown WorkBuddy edition %q", strings.TrimSpace(edition))
	}
	edition = canonicalWorkBuddyEdition(edition)
	return s.startWorkBuddyLogin(profile, edition)
}

func canonicalWorkBuddyEdition(edition string) string {
	profile, ok := workbuddy.ProfileByEdition(edition)
	if !ok {
		return strings.TrimSpace(edition)
	}
	if profile.ID == workbuddy.GlobalProfile().ID {
		return workbuddy.EditionGlobal
	}
	return workbuddy.EditionChina
}

func (s *Service) startWorkBuddyLogin(profile workbuddy.Profile, edition string) (WorkBuddyLoginStart, error) {
	if s == nil {
		return WorkBuddyLoginStart{}, fmt.Errorf("llm service is required")
	}
	session, err := workbuddy.BeginLogin(context.Background(), profile, nil)
	if err != nil {
		return WorkBuddyLoginStart{}, err
	}
	id, err := newWorkBuddySessionID()
	if err != nil {
		session.Close()
		return WorkBuddyLoginStart{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	rec := &workBuddyLogin{
		id:        id,
		edition:   edition,
		profile:   profile,
		authURL:   session.AuthURL(),
		status:    workBuddyStatusPending,
		session:   session,
		cancel:    cancel,
		keepUntil: time.Now().Add(6 * time.Minute),
	}
	s.workBuddyMu.Lock()
	s.sweepWorkBuddyLoginsLocked(time.Now())
	if len(s.workBuddyMap()) >= workBuddyMaxLogins {
		s.workBuddyMu.Unlock()
		cancel()
		session.Close()
		return WorkBuddyLoginStart{}, fmt.Errorf("too many WorkBuddy logins in progress")
	}
	s.workBuddyMap()[id] = rec
	s.workBuddyMu.Unlock()
	go s.pollWorkBuddyLogin(ctx, rec)
	return WorkBuddyLoginStart{SessionID: id, AuthURL: rec.authURL, Edition: edition}, nil
}

func (s *Service) pollWorkBuddyLogin(ctx context.Context, rec *workBuddyLogin) {
	if rec == nil || rec.session == nil {
		return
	}
	defer rec.session.Close()
	defer func() {
		if rec.cancel != nil {
			rec.cancel()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cred, pending, err := rec.session.Poll(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			if errors.Is(err, context.DeadlineExceeded) {
				s.failWorkBuddyLogin(rec, workbuddy.ErrLoginTimeout.Error())
				return
			}
			s.failWorkBuddyLogin(rec, err.Error())
			return
		}
		if cred != nil {
			s.finishWorkBuddyLogin(rec, cred)
			return
		}
		if !pending {
			s.failWorkBuddyLogin(rec, "WorkBuddy login did not finish")
			return
		}
		select {
		case <-ctx.Done():
			if ctx.Err() != nil && !errors.Is(ctx.Err(), context.Canceled) {
				s.failWorkBuddyLogin(rec, workbuddy.ErrLoginTimeout.Error())
			}
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) finishWorkBuddyLogin(rec *workBuddyLogin, cred *workbuddy.AccountCredential) {
	if rec == nil || cred == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	warning := ""
	upstream, err := workbuddy.FetchModels(ctx, rec.profile, *cred, nil)
	var specs []workbuddy.ModelSpec
	if err != nil {
		log.Printf("[llm] workbuddy catalog edition=%s: %v", rec.edition, err)
		warning = "live catalog unavailable"
		specs = workbuddy.Allowlist(rec.profile.ID)
	} else {
		specs = workbuddy.MergeCatalog(rec.profile.ID, upstream)
	}
	models := make([]WorkBuddyModel, 0, len(specs))
	seen := map[string]bool{}
	for _, spec := range specs {
		id := strings.TrimSpace(spec.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			name = id
		}
		models = append(models, WorkBuddyModel{
			ID:            id,
			Name:          name,
			ContextLength: spec.ContextLength,
			MaxOutput:     spec.MaxOutput,
		})
	}
	if len(models) == 0 {
		s.failWorkBuddyLogin(rec, "WorkBuddy returned no models")
		return
	}
	rec.mu.Lock()
	rec.status = workBuddyStatusReady
	rec.cred = cred
	rec.models = models
	rec.warning = warning
	rec.err = ""
	rec.keepUntil = time.Now().Add(workBuddyLoginKeep)
	rec.mu.Unlock()
}

func (s *Service) failWorkBuddyLogin(rec *workBuddyLogin, message string) {
	if rec == nil {
		return
	}
	rec.mu.Lock()
	if rec.status == workBuddyStatusReady {
		rec.mu.Unlock()
		return
	}
	rec.status = workBuddyStatusError
	rec.err = strings.TrimSpace(message)
	if rec.err == "" {
		rec.err = "WorkBuddy login failed"
	}
	rec.keepUntil = time.Now().Add(2 * time.Minute)
	rec.mu.Unlock()
}

// WorkBuddyLoginStatus reports pending, ready (with models), or error.
func (s *Service) WorkBuddyLoginStatus(id string) (WorkBuddyLoginStatus, error) {
	rec := s.lookupWorkBuddyLogin(id)
	if rec == nil {
		return WorkBuddyLoginStatus{}, fmt.Errorf("WorkBuddy login session not found")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := WorkBuddyLoginStatus{
		Status:         rec.status,
		Edition:        rec.edition,
		AuthURL:        rec.authURL,
		CatalogWarning: rec.warning,
		Error:          rec.err,
	}
	if rec.status == workBuddyStatusReady {
		out.Models = append([]WorkBuddyModel(nil), rec.models...)
	}
	return out, nil
}

// CancelWorkBuddyLogin stops a login that the admin abandoned.
func (s *Service) CancelWorkBuddyLogin(id string) {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return
	}
	s.workBuddyMu.Lock()
	rec := s.workBuddyMap()[id]
	delete(s.workBuddyLogins, id)
	s.workBuddyMu.Unlock()
	if rec != nil && rec.cancel != nil {
		rec.cancel()
	}
}

func (s *Service) ForgetWorkBuddyLogin(id string) {
	s.CancelWorkBuddyLogin(id)
}

func (s *Service) lookupWorkBuddyLogin(id string) *workBuddyLogin {
	id = strings.TrimSpace(id)
	if s == nil || id == "" {
		return nil
	}
	s.workBuddyMu.Lock()
	defer s.workBuddyMu.Unlock()
	s.sweepWorkBuddyLoginsLocked(time.Now())
	return s.workBuddyMap()[id]
}

func (s *Service) sweepWorkBuddyLoginsLocked(now time.Time) {
	for id, rec := range s.workBuddyLogins {
		if rec == nil {
			delete(s.workBuddyLogins, id)
			continue
		}
		rec.mu.Lock()
		dead := now.After(rec.keepUntil)
		cancel := rec.cancel
		rec.mu.Unlock()
		if !dead {
			continue
		}
		delete(s.workBuddyLogins, id)
		if cancel != nil {
			cancel()
		}
	}
}

// ApplyWorkBuddyLogin copies a finished login onto a provider and keeps only
// the models the admin selected. creating requires a finished login when the
// provider is new. Updates without a session keep the saved account.
func (s *Service) ApplyWorkBuddyLogin(provider *llmpool.ProviderConfig, creating bool) error {
	if provider == nil {
		return fmt.Errorf("provider is required")
	}
	sessionID := strings.TrimSpace(provider.WorkBuddySessionID)
	kind := strings.TrimSpace(provider.AuthKind)
	if kind == llmpool.ProviderAuthAPIKey {
		provider.AuthKind = llmpool.ProviderAuthAPIKey
		provider.WorkBuddySessionID = ""
		clearWorkBuddySecrets(provider)
		return nil
	}
	if sessionID == "" {
		provider.WorkBuddySessionID = ""
		if kind != llmpool.ProviderAuthWorkBuddy {
			return nil
		}
		profile, ok := workbuddy.ProfileByEdition(provider.WorkBuddyEdition)
		if !ok {
			return fmt.Errorf("unknown WorkBuddy edition %q", provider.WorkBuddyEdition)
		}
		if creating && strings.TrimSpace(provider.APIKey) == "" {
			return fmt.Errorf("WorkBuddy login is required")
		}
		provider.AuthKind = llmpool.ProviderAuthWorkBuddy
		provider.WorkBuddyEdition = canonicalWorkBuddyEdition(provider.WorkBuddyEdition)
		provider.APIURL = profile.ChatURL
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
	rec := s.lookupWorkBuddyLogin(sessionID)
	if rec == nil {
		return fmt.Errorf("WorkBuddy login session not found")
	}
	rec.mu.Lock()
	status := rec.status
	errText := rec.err
	edition := rec.edition
	var cred *workbuddy.AccountCredential
	if rec.cred != nil {
		copied := *rec.cred
		cred = &copied
	}
	models := append([]WorkBuddyModel(nil), rec.models...)
	rec.mu.Unlock()
	if status != workBuddyStatusReady || cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		if strings.TrimSpace(errText) == "" {
			errText = "WorkBuddy login is not finished"
		}
		return errors.New(errText)
	}
	if requested := strings.TrimSpace(provider.WorkBuddyEdition); requested != "" && !strings.EqualFold(canonicalWorkBuddyEdition(requested), edition) {
		return fmt.Errorf("WorkBuddy edition does not match the login session")
	}
	selected, err := filterWorkBuddyModels(provider.Models, models)
	if err != nil {
		return err
	}
	profile, ok := workbuddy.ProfileByEdition(edition)
	if !ok {
		return fmt.Errorf("unknown WorkBuddy edition %q", edition)
	}
	provider.AuthKind = llmpool.ProviderAuthWorkBuddy
	provider.WorkBuddyEdition = edition
	provider.APIURL = profile.ChatURL
	provider.APIKey = cred.AccessToken
	provider.WorkBuddyRefreshToken = cred.RefreshToken
	provider.WorkBuddyExpiresAt = cred.ExpiresAt
	provider.WorkBuddyUserID = cred.UserID
	provider.WorkBuddyEnterpriseID = cred.EnterpriseID
	provider.WorkBuddyDomain = cred.Domain
	provider.Protocol = "openai"
	provider.WireAPI = ""
	provider.Models = selected
	provider.WorkBuddySessionID = ""
	if strings.TrimSpace(provider.Name) == "" {
		provider.Name = profile.Name
	}
	return nil
}

func filterWorkBuddyModels(selected []string, catalog []WorkBuddyModel) ([]string, error) {
	allowed := make(map[string]struct{}, len(catalog))
	for _, model := range catalog {
		id := strings.TrimSpace(model.ID)
		if id != "" {
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
			return nil, fmt.Errorf("model %s is not in the WorkBuddy catalog", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("select at least one WorkBuddy model")
	}
	return out, nil
}

func clearWorkBuddySecrets(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.WorkBuddyEdition = ""
	provider.WorkBuddyRefreshToken = ""
	provider.WorkBuddyExpiresAt = 0
	provider.WorkBuddyUserID = ""
	provider.WorkBuddyEnterpriseID = ""
	provider.WorkBuddyDomain = ""
}

func mergeWorkBuddyAuth(existing, incoming llmpool.ProviderConfig) llmpool.ProviderConfig {
	switch strings.TrimSpace(incoming.AuthKind) {
	case llmpool.ProviderAuthAPIKey:
		incoming.AuthKind = ""
		clearWorkBuddySecrets(&incoming)
		return incoming
	case llmpool.ProviderAuthWorkBuddy:
		if strings.TrimSpace(incoming.WorkBuddyRefreshToken) == "" {
			incoming.WorkBuddyRefreshToken = existing.WorkBuddyRefreshToken
			incoming.WorkBuddyExpiresAt = existing.WorkBuddyExpiresAt
			incoming.WorkBuddyUserID = existing.WorkBuddyUserID
			incoming.WorkBuddyEnterpriseID = existing.WorkBuddyEnterpriseID
			incoming.WorkBuddyDomain = existing.WorkBuddyDomain
			if strings.TrimSpace(incoming.WorkBuddyEdition) == "" {
				incoming.WorkBuddyEdition = existing.WorkBuddyEdition
			}
		}
		if strings.TrimSpace(incoming.APIKey) == "" {
			incoming.APIKey = existing.APIKey
		}
		return incoming
	case "":
		if existing.AuthKind == llmpool.ProviderAuthWorkBuddy {
			incoming.AuthKind = existing.AuthKind
			if strings.TrimSpace(incoming.WorkBuddyRefreshToken) == "" {
				incoming.WorkBuddyRefreshToken = existing.WorkBuddyRefreshToken
				incoming.WorkBuddyExpiresAt = existing.WorkBuddyExpiresAt
				incoming.WorkBuddyUserID = existing.WorkBuddyUserID
				incoming.WorkBuddyEnterpriseID = existing.WorkBuddyEnterpriseID
				incoming.WorkBuddyDomain = existing.WorkBuddyDomain
			}
			if strings.TrimSpace(incoming.WorkBuddyEdition) == "" {
				incoming.WorkBuddyEdition = existing.WorkBuddyEdition
			}
			if strings.TrimSpace(incoming.APIKey) == "" {
				incoming.APIKey = existing.APIKey
			}
		}
		return incoming
	default:
		return incoming
	}
}

func normalizeProviderAuth(provider *llmpool.ProviderConfig) {
	if provider == nil {
		return
	}
	provider.WorkBuddySessionID = ""
	kind := strings.TrimSpace(provider.AuthKind)
	if kind == llmpool.ProviderAuthAPIKey || (kind != "" && kind != llmpool.ProviderAuthWorkBuddy) {
		provider.AuthKind = ""
		clearWorkBuddySecrets(provider)
		return
	}
	if kind != llmpool.ProviderAuthWorkBuddy {
		clearWorkBuddySecrets(provider)
		provider.AuthKind = ""
	}
}

func newWorkBuddySessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
