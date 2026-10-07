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
	"github.com/RapidAI/CodeClaw/hub/internal/upstream"
)

const SettingsKey = "bot_management"

var (
	ErrInvalidInput        = errors.New("invalid bot settings")
	ErrSettingsUnavailable = errors.New("bot settings store is unavailable")
	ErrNotConfigured       = errors.New("maclawsrv connection is not configured")
	ErrNotFound            = errors.New("bot not found")
	ErrSrv                 = errors.New("maclawsrv request failed")
	// ErrSrvNotFound is a 404 from MaClawSrv: the instance, or the endpoint
	// itself, is not there. It is not wrapped in ErrSrv, because callers such
	// as bot deletion treat a missing instance as success.
	ErrSrvNotFound = errors.New("maclawsrv instance not found")
	ErrDisabled    = errors.New("bot feature is disabled")
	// ErrAdminSecretMissing is ErrNotConfigured for the MaClawSrv admin secret.
	// It is a separate sentinel so callers do not have to match on the message
	// text to explain what is missing.
	ErrAdminSecretMissing = fmt.Errorf("%w: maclawsrv admin secret missing", ErrNotConfigured)
)

// SrvRejectionMessage turns a MaClawSrv failure into a message an admin can
// act on, instead of the generic "rejected the instance request".
func SrvRejectionMessage(err error) string {
	base := "MaClawSrv rejected the instance request"
	switch {
	case err == nil:
		return base
	case errors.Is(err, ErrSrvNotFound):
		return base + ": instance or endpoint not found, check the MaClawSrv URL"
	case errors.Is(err, ErrSrv) && err.Error() == ErrSrv.Error():
		return base
	}
	return upstream.Message(err, base)
}

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
	BaseURL        string                `json:"base_url"`
	AccessToken    string                `json:"access_token,omitempty"`
	AdminSecret    string                `json:"admin_secret,omitempty"`
	MaClawTenantID string                `json:"maclaw_tenant_id,omitempty"`
	Connection     *connectionCredential `json:"connection,omitempty"`
	Principals     []ownerPrincipal      `json:"principals,omitempty"`
	Bots           []Bot                 `json:"bots"`
	Grants         []Grant               `json:"grants,omitempty"`
	Desktop        *desktopStateRecord   `json:"desktop,omitempty"`
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
	// desktopAdminView is when an admin last opened this user's desktop in
	// the admin console. While that hold is fresh, a bot command finishing
	// must leave the desktop up, or the picture the admin is watching goes
	// black halfway through the check.
	desktopAdminView map[string]time.Time
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
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		s.mu.Unlock()
		return SettingsView{}, err
	}
	urlChanged := rec.BaseURL != baseURL
	rec.BaseURL = baseURL
	if tokenProvided {
		rec.AccessToken = strings.TrimSpace(token)
	}
	// A connection credential was minted for the previous URL's server and
	// the previous token's user, so a change to either retires it. Re-saving
	// the same token keeps it: there is nothing to re-provision. Retiring for
	// a changed principal orphans the credential on the MaClawSrv side — by
	// then it answers for an account this connection no longer uses, and
	// revoking it there is the other system's admin job.
	if urlChanged || (tokenProvided && !connectionMatchesToken(rec)) {
		rec.Connection = nil
	}
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return SettingsView{}, err
	}
	s.mu.Unlock()
	// With the admin secret on file this provisions the durable connection
	// credential, so the just-saved token never has to be re-pasted.
	s.bootstrapConnection(ctx, tenantID)
	return viewOf(rec), nil
}

// SaveAdminSecret stores the MaClawSrv admin secret used to create one
// MaClawSrv user per Hub user. The secret is never returned.
func (s *Service) SaveAdminSecret(ctx context.Context, tenantID, secret string) (SettingsView, error) {
	s.mu.Lock()
	rec, err := s.load(ctx, tenantID)
	if err != nil {
		s.mu.Unlock()
		return SettingsView{}, err
	}
	rec.AdminSecret = strings.TrimSpace(secret)
	if err := s.save(ctx, tenantID, rec); err != nil {
		s.mu.Unlock()
		return SettingsView{}, err
	}
	s.mu.Unlock()
	// With an access token on file this provisions the durable connection
	// credential, so the expiring token stops being load-bearing.
	s.bootstrapConnection(ctx, tenantID)
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
	count, err := s.countInstances(ctx, rec, s.sharedBearer(ctx, rec))
	if err == nil {
		return count, nil
	}
	if !upstreamUnauthorized(err) {
		return 0, err
	}
	// MaClawSrv access tokens expire with the server-side TTL, so a stored
	// connection eventually 401s. With the admin secret the hub re-issues
	// the connection credential and retries once; without it the honest 401
	// is all there is. A renewal failure only replaces the 401 when
	// MaClawSrv named a concrete problem (say, a revoked connection user) —
	// anything else, like an undecodable token, is less actionable than the
	// token-rejected message the admin already sees.
	cred, renewErr := s.provisionConnectionCredential(ctx, tenantID)
	if renewErr != nil {
		var statusErr *upstream.StatusError
		if !errors.Is(renewErr, ErrAdminSecretMissing) && errors.As(renewErr, &statusErr) {
			return 0, renewErr
		}
		return 0, err
	}
	bearer, err := s.exchangeToken(ctx, rec, cred.APIKey, cred.APISecret, cred.UserID)
	if err != nil {
		return 0, err
	}
	return s.countInstances(ctx, rec, bearer)
}

// instancePage is the paged shape the instance list returns; only the item
// count matters here.
type instancePage struct {
	Items []json.RawMessage `json:"items"`
}

func (s *Service) countInstances(ctx context.Context, rec record, bearer string) (int, error) {
	var page instancePage
	if err := s.call(ctx, rec, bearer, "", http.MethodGet, "/api/v1/instances", nil, &page); err != nil {
		return 0, err
	}
	return len(page.Items), nil
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
	if err := s.doAuth(ctx, rec, token, http.MethodDelete, "/api/v1/instances/"+url.PathEscape(bot.InstanceID), nil, nil); err != nil && !errors.Is(err, ErrSrvNotFound) {
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
	return s.call(ctx, rec, s.sharedBearer(ctx, rec), "", method, path, body, dest)
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
		return ErrSrvNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return upstream.NewStatusError(ErrSrv, resp.StatusCode, payload)
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
