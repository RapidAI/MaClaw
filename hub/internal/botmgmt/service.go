package botmgmt

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const SettingsKey = "bot_management"

var (
	ErrInvalidInput        = errors.New("invalid bot settings")
	ErrSettingsUnavailable = errors.New("bot settings store is unavailable")
	ErrNotConfigured       = errors.New("maclawsrv connection is not configured")
	ErrNotFound            = errors.New("bot not found")
	ErrSrv                 = errors.New("maclawsrv request failed")
	ErrDisabled            = errors.New("bot feature is disabled")
)

// Bot is one Hub bot. It is one MaClawSrv agent instance of OwnerUserID.
type Bot struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InstanceID  string `json:"instance_id"`
	OwnerUserID string `json:"owner_user_id,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type record struct {
	BaseURL        string           `json:"base_url"`
	AccessToken    string           `json:"access_token,omitempty"`
	AdminSecret    string           `json:"admin_secret,omitempty"`
	MaClawTenantID string           `json:"maclaw_tenant_id,omitempty"`
	Principals     []ownerPrincipal `json:"principals,omitempty"`
	Bots           []Bot            `json:"bots"`
	Grants         []Grant          `json:"grants,omitempty"`
	Desktop        *desktopStateRecord `json:"desktop,omitempty"`
}

// SettingsView is the admin payload. The access token is never returned.
type SettingsView struct {
	BaseURL        string  `json:"base_url"`
	TokenSet       bool    `json:"token_set"`
	AdminSecretSet bool    `json:"admin_secret_set"`
	Bots           []Bot   `json:"bots"`
	Grants         []Grant `json:"grants"`
}

// Service stores the tenant MaClawSrv connection and the bots created there.
type Service struct {
	System    store.SystemSettingsRepository
	Directory Directory
	Desktop   DesktopControl
	HTTP      *http.Client
	Now       func() time.Time
	// messageTimeout overrides the wait for one bot command. Tests use it.
	// A timed-out command must not stop the desktop: the instance may already
	// have handed that browser to the person.
	messageTimeout time.Duration

	mu          sync.Mutex
	handoff     map[string]handoffView
	desktopView map[string]desktopWatch
	// desktopHeld maps a user to the bot that handed the desktop over.
	// A later failed message, or another bot finishing, must not stop it.
	desktopHeld map[string]string
	// desktopAwaiting is true only while that person still has the keyboard.
	// The continuation takes the keyboard back as soon as it starts, but the
	// desktop stays up until that bot finishes.
	desktopAwaiting map[string]bool
	// desktopKeyboardTaken is the bot whose continuation just took the
	// keyboard. A failure gives it back only to that bot. A view-only
	// timeout pin is not a login, so a later failure must not hand the
	// keyboard over and let the person type on a page the agent still owns.
	desktopKeyboardTaken map[string]string
	// desktopOpening counts bot commands that have this user's desktop open.
	// One command failing must not stop the desktop another command is using.
	desktopOpening map[string]int
	// desktopOpenInstance is the MaClaw instance that opened the desktop.
	// Another instance finishing must not stop the browser this command is using.
	desktopOpenInstance map[string]map[string]int
	// desktopGates serializes one user's desktop open and stop. The count is
	// raised before Open returns, and a stop checks it again under the same
	// gate, so a failure cannot shut a desktop the next command just opened.
	desktopGates sync.Map
	// desktopHydrated marks tenants whose persisted desktop pins/views have
	// been restored into the maps above after (re)construction.
	desktopHydrated map[string]bool
	// beforeDesktopStop is a test hook. It runs after a command drops its
	// count and before that stop takes the user's gate.
	beforeDesktopStop func()
}

// desktopWatch is the noVNC page for a user's running desktop.
// The gated path stays the same while that desktop stays up, so the chat
// picture does not reconnect on every poll.
type desktopWatch struct {
	raw   string
	gated string
}

func NewService(system store.SystemSettingsRepository) *Service {
	return &Service{System: system, Now: time.Now}
}

func (s *Service) View(ctx context.Context, tenantID string) (SettingsView, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	return viewOf(rec), nil
}

func (s *Service) SaveConnection(ctx context.Context, tenantID, baseURL, token string, tokenProvided bool) (SettingsView, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL != "" {
		if err := validateBaseURL(baseURL); err != nil {
			return SettingsView{}, err
		}
		baseURL = strings.TrimRight(baseURL, "/")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	rec.BaseURL = baseURL
	if tokenProvided {
		rec.AccessToken = strings.TrimSpace(token)
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		return SettingsView{}, err
	}
	return viewOf(rec), nil
}

// SaveAdminSecret stores the MaClawSrv admin secret used to create one
// MaClawSrv user per Hub user. The secret is never returned.
func (s *Service) SaveAdminSecret(ctx context.Context, tenantID, secret string) (SettingsView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return SettingsView{}, err
	}
	rec.AdminSecret = strings.TrimSpace(secret)
	if err := s.save(ctx, tenantID, rec); err != nil {
		return SettingsView{}, err
	}
	return viewOf(rec), nil
}

func (s *Service) TestConnection(ctx context.Context, tenantID string) (int, error) {
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	if err := configured(rec); err != nil {
		return 0, err
	}
	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := s.do(ctx, rec, http.MethodGet, "/api/v1/instances", nil, &payload); err != nil {
		return 0, err
	}
	return len(payload.Items), nil
}

func (s *Service) CreateBot(ctx context.Context, tenantID, name, description string) (Bot, error) {
	return s.createBot(ctx, tenantID, "", name, description, false)
}

func (s *Service) UpdateBot(ctx context.Context, tenantID, botID, name, description string) (Bot, error) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" || len([]rune(name)) > 80 {
		return Bot{}, fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return Bot{}, err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		return Bot{}, ErrNotFound
	}
	if err := configured(rec); err != nil {
		return Bot{}, err
	}
	bot := rec.Bots[index]
	token, err := s.ensureOwnerToken(ctx, tenantID, &rec, bot.OwnerUserID)
	if err != nil {
		return Bot{}, err
	}
	body := map[string]any{"name": name, "description": description}
	if err := s.doAuth(ctx, rec, token, http.MethodPatch, "/api/v1/instances/"+url.PathEscape(bot.InstanceID), body, nil); err != nil {
		return Bot{}, err
	}
	bot.Name = name
	bot.Description = description
	rec.Bots[index] = bot
	if err := s.save(ctx, tenantID, rec); err != nil {
		return Bot{}, err
	}
	return bot, nil
}

func (s *Service) DeleteBot(ctx context.Context, tenantID, botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		return err
	}
	index := indexOf(rec.Bots, botID)
	if index < 0 {
		return ErrNotFound
	}
	bot := rec.Bots[index]
	if err := configured(rec); err != nil {
		return err
	}
	token, err := s.ensureOwnerToken(ctx, tenantID, &rec, bot.OwnerUserID)
	if err != nil {
		return err
	}
	if err := s.doAuth(ctx, rec, token, http.MethodDelete, "/api/v1/instances/"+url.PathEscape(bot.InstanceID), nil, nil); err != nil && !errors.Is(err, errSrvNotFound) {
		return err
	}
	rec.Bots = append(rec.Bots[:index], rec.Bots[index+1:]...)
	key := desktopViewKey(tenantID, bot.OwnerUserID)
	stopHeld := false
	s.ensureDesktopHydrated(tenantID)
	if holder, ok := s.desktopHeld[key]; ok && holder == bot.ID {
		delete(s.desktopHeld, key)
		delete(s.desktopAwaiting, key)
		delete(s.desktopKeyboardTaken, key)
		delete(s.desktopView, key)
		stopHeld = true
	}
	s.persistDesktopState(tenantID)
	// The desktop belongs to the user, not this bot. Another bot of the same
	// user may still be logged in there. MaClawSrv stops it when no run is left.
	shared := false
	for _, item := range rec.Bots {
		if item.OwnerUserID != "" && item.OwnerUserID == bot.OwnerUserID {
			shared = true
			break
		}
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		return err
	}
	if stopHeld && !shared && s.Desktop != nil {
		_ = s.Desktop.Stop(ctx, tenantID, bot.OwnerUserID)
	}
	return nil
}

func (s *Service) load(ctx context.Context, tenantID string) (record, error) {
	out := record{Bots: []Bot{}}
	if s == nil || s.System == nil {
		return out, ErrSettingsUnavailable
	}
	raw, err := s.System.Get(ctx, storageKey(tenantID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return record{Bots: []Bot{}}, nil
	}
	if out.Bots == nil {
		out.Bots = []Bot{}
	}
	if out.Grants == nil {
		out.Grants = []Grant{}
	}
	return out, nil
}

func (s *Service) save(ctx context.Context, tenantID string, rec record) error {
	if s == nil || s.System == nil {
		return ErrSettingsUnavailable
	}
	if rec.Bots == nil {
		rec.Bots = []Bot{}
	}
	if rec.Grants == nil {
		rec.Grants = []Grant{}
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return s.System.Set(ctx, storageKey(tenantID), string(raw))
}

func (s *Service) do(ctx context.Context, rec record, method, path string, body any, dest any) error {
	return s.call(ctx, rec, rec.AccessToken, "", method, path, body, dest)
}

func (s *Service) doAuth(ctx context.Context, rec record, bearer, method, path string, body any, dest any) error {
	return s.call(ctx, rec, bearer, "", method, path, body, dest)
}

func (s *Service) doAdmin(ctx context.Context, rec record, method, path string, body any, dest any) error {
	return s.call(ctx, rec, "", rec.AdminSecret, method, path, body, dest)
}

func (s *Service) call(ctx context.Context, rec record, bearer, adminSecret, method, path string, body any, dest any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, rec.BaseURL+path, reader)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	if strings.TrimSpace(bearer) != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if strings.TrimSpace(adminSecret) != "" {
		req.Header.Set("X-MaClaw-Admin-Secret", adminSecret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.httpClient(path).Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return errSrvNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: status %d", ErrSrv, resp.StatusCode)
	}
	if dest == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, dest); err != nil {
		return fmt.Errorf("%w: %s", ErrSrv, err.Error())
	}
	return nil
}

func (s *Service) client() *http.Client {
	if s != nil && s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: refuseCrossHostRedirect}
}

func (s *Service) httpClient(path string) *http.Client {
	base := s.client()
	if !strings.Contains(path, "/messages") {
		return base
	}
	timeout := 8 * time.Minute
	if s != nil && s.messageTimeout > 0 {
		timeout = s.messageTimeout
	}
	return &http.Client{Timeout: timeout, Transport: base.Transport, CheckRedirect: base.CheckRedirect}
}

func desktopCallTimedOut(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "timeout") || strings.Contains(text, "deadline exceeded")
}

func (s *Service) now() time.Time {
	if s != nil && s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

var errSrvNotFound = errors.New("maclawsrv instance not found")

func configured(rec record) error {
	if strings.TrimSpace(rec.BaseURL) == "" || strings.TrimSpace(rec.AccessToken) == "" {
		return ErrNotConfigured
	}
	return nil
}

func viewOf(rec record) SettingsView {
	bots := rec.Bots
	if bots == nil {
		bots = []Bot{}
	}
	grants := rec.Grants
	if grants == nil {
		grants = []Grant{}
	}
	return SettingsView{
		BaseURL:        rec.BaseURL,
		TokenSet:       strings.TrimSpace(rec.AccessToken) != "",
		AdminSecretSet: strings.TrimSpace(rec.AdminSecret) != "",
		Bots:           bots,
		Grants:         grants,
	}
}

func storageKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return SettingsKey
	}
	return "tenant:" + tenantID + ":" + SettingsKey
}

func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("%w: base_url is invalid", ErrInvalidInput)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: base_url must be http or https", ErrInvalidInput)
	}
	return nil
}

func indexOf(bots []Bot, id string) int {
	id = strings.TrimSpace(id)
	for i := range bots {
		if bots[i].ID == id {
			return i
		}
	}
	return -1
}

func newBotID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "bot_" + hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return "bot_" + hex.EncodeToString(buf[:])
}

func refuseCrossHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 0 && req.URL.Host != via[0].URL.Host {
		return http.ErrUseLastResponse
	}
	if len(via) >= 3 {
		return http.ErrUseLastResponse
	}
	return nil
}
