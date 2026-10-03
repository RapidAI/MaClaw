package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
	"golang.org/x/crypto/bcrypt"
)

// --- test settings repository ---

// memSettings is a minimal in-memory SystemSettingsRepository. The admin tests
// need one because the Token Bank settings blob lives in system settings, and
// the real SQLite implementation is not part of this package's test fixtures.
type memSettings struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemSettings() *memSettings {
	return &memSettings{data: map[string]string{}}
}

func (m *memSettings) Set(_ context.Context, key, valueJSON string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = valueJSON
	return nil
}

func (m *memSettings) Get(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.data[key], nil
}

func (m *memSettings) List(_ context.Context) ([]*store.SystemSettingEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*store.SystemSettingEntry, 0, len(m.data))
	for k, v := range m.data {
		out = append(out, &store.SystemSettingEntry{Key: k, ValueJSON: v})
	}
	return out, nil
}

// --- admin test environment ---

// tokenBankAdminTestEnv extends the client env with an admin session and a
// settings store, and registers the §6.2 verbs behind RequireAdmin exactly the
// way router.go does.
type tokenBankAdminTestEnv struct {
	*tokenBankTestEnv
	settings   *memSettings
	adminToken string
}

func newTokenBankAdminTestEnv(t *testing.T) *tokenBankAdminTestEnv {
	t.Helper()
	env := newTokenBankTestEnv(t)
	settings := newMemSettings()

	// Rebuild the handlers with the settings repository wired in; the client
	// env does not need one, but the admin settings endpoints do.
	store2, err := skillmarket.NewStore(env.provider.Write, env.provider.Read)
	if err != nil {
		t.Fatalf("skillmarket store: %v", err)
	}
	authSvc2 := skillmarket.NewAuthService(store2, nil, "")
	h := NewSkillMarketHandlers(SkillMarketConfig{
		Store:    store2,
		UserSvc:  env.users,
		AuthSvc:  authSvc2,
		Settings: settings,
	})
	h.SetTokenBankRepo(env.repo, "hc-test")
	env.handlers = h
	env.auth = authSvc2

	// Create an admin and log in. Login is the only path that mints a usable
	// admin token, so the test goes through the real credential check.
	adminStore := newStubAdminRepo()
	adminSvc := auth.NewAdminService(adminStore, settings, nil)
	if err := adminStore.Create(context.Background(), &store.AdminUser{
		ID: "adm-1", Username: "root", Status: "active",
		PasswordHash: hashForTest(t, "secret"), CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("create admin: %v", err)
	}
	token, _, err := adminSvc.Login(context.Background(), "root", "secret")
	if err != nil {
		t.Fatalf("admin login: %v", err)
	}

	env.adminAuth = adminSvc
	env.adminToken = token
	return &tokenBankAdminTestEnv{tokenBankTestEnv: env, settings: settings, adminToken: token}
}

// stubAdminRepo is the minimum AdminUserRepository the admin service needs.
// Only GetByUsername matters for Login; the rest are stubs so a future caller
// fails loudly instead of silently succeeding.
type stubAdminRepo struct {
	mu     sync.Mutex
	admins map[string]*store.AdminUser
}

func newStubAdminRepo() *stubAdminRepo {
	return &stubAdminRepo{admins: map[string]*store.AdminUser{}}
}

func (r *stubAdminRepo) Create(_ context.Context, admin *store.AdminUser) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.admins[admin.Username] = admin
	return nil
}

func (r *stubAdminRepo) GetByUsername(_ context.Context, username string) (*store.AdminUser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.admins[username], nil
}

func (r *stubAdminRepo) Count(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.admins), nil
}

func (r *stubAdminRepo) UpdatePassword(_ context.Context, username, passwordHash string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.admins[username]; a != nil {
		a.PasswordHash = passwordHash
	}
	return nil
}

func (r *stubAdminRepo) UpdateEmail(_ context.Context, username, email string, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a := r.admins[username]; a != nil {
		a.Email = email
	}
	return nil
}

func (r *stubAdminRepo) DeleteAll(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.admins = map[string]*store.AdminUser{}
	return nil
}

func hashForTest(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return string(hash)
}

// routeAdmin dispatches through the §6.2 route table wrapped in RequireAdmin,
// mirroring router.go so a verb/path mismatch fails here.
func (e *tokenBankAdminTestEnv) doAdmin(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = strings.NewReader(string(raw))
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if e.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+e.adminToken)
	}
	rec := httptest.NewRecorder()

	mux := http.NewServeMux()
	h := e.handlers
	adm := e.adminAuth
	mux.HandleFunc("GET /api/admin/token-bank/settings", RequireAdmin(adm, h.TokenBankGetSettings))
	mux.HandleFunc("PUT /api/admin/token-bank/settings", RequireAdmin(adm, h.TokenBankPutSettings))
	mux.HandleFunc("GET /api/admin/token-bank/overview", RequireAdmin(adm, h.TokenBankAdminOverview))
	mux.HandleFunc("GET /api/admin/token-bank/leaderboard", RequireAdmin(adm, h.TokenBankAdminLeaderboard))
	mux.HandleFunc("GET /api/admin/token-bank/users", RequireAdmin(adm, h.TokenBankAdminUsers))
	mux.HandleFunc("GET /api/admin/token-bank/shares", RequireAdmin(adm, h.TokenBankAdminShares))
	mux.HandleFunc("PUT /api/admin/token-bank/shares/{id}/paused", RequireAdmin(adm, h.TokenBankAdminSetSharePaused))
	mux.HandleFunc("DELETE /api/admin/token-bank/shares/{id}", RequireAdmin(adm, h.TokenBankAdminTakeOutShare))
	mux.HandleFunc("PUT /api/admin/token-bank/shares/{id}/models/{model}/tier", RequireAdmin(adm, h.TokenBankAdminSetModelTier))
	mux.HandleFunc("GET /api/admin/token-bank/price-book", RequireAdmin(adm, h.TokenBankAdminListPriceBook))
	mux.HandleFunc("POST /api/admin/token-bank/price-book", RequireAdmin(adm, h.TokenBankAdminUpsertPriceRule))
	mux.HandleFunc("PUT /api/admin/token-bank/price-book", RequireAdmin(adm, h.TokenBankAdminUpsertPriceRule))
	mux.HandleFunc("DELETE /api/admin/token-bank/price-book/{id}", RequireAdmin(adm, h.TokenBankAdminDeletePriceRule))
	mux.HandleFunc("GET /api/admin/token-bank/credit-shares", RequireAdmin(adm, h.TokenBankAdminListCreditShares))
	mux.HandleFunc("POST /api/admin/token-bank/credit-shares/{id}/revoke", RequireAdmin(adm, h.TokenBankAdminRevokeCreditShare))
	mux.ServeHTTP(rec, req)
	return rec
}

// --- seed helpers ---

func (e *tokenBankAdminTestEnv) seedShare(t *testing.T, id, owner, fingerprint, model string) {
	t.Helper()
	_, _, err := e.repo.CreateShare(context.Background(), sqlite.TokenBankShare{
		ID:             id,
		OwnerUserID:    owner,
		OwnerEmail:     owner + "@example.com",
		DisplayName:    "Share " + id,
		APIURL:         "https://api.example.com/v1",
		Protocol:       "openai",
		EncryptedKey:   "enc:" + fingerprint,
		KeyFingerprint: fingerprint,
	}, []sqlite.TokenBankShareModel{{ModelName: model, MemberID: "member-" + model, Enabled: true, Available: true}},
		0, time.Now().UTC())
	if err != nil {
		t.Fatalf("seed share %s: %v", id, err)
	}
}

// --- tests ---

func TestTokenBankAdminEndpointsRequireAdmin(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	// No Authorization header at all must be 401, and a user session token is
	// not an admin token.
	cases := []struct{ method, path string }{
		{"GET", "/api/admin/token-bank/settings"},
		{"PUT", "/api/admin/token-bank/settings"},
		{"GET", "/api/admin/token-bank/overview"},
		{"GET", "/api/admin/token-bank/leaderboard"},
		{"GET", "/api/admin/token-bank/users"},
		{"GET", "/api/admin/token-bank/shares"},
		{"PUT", "/api/admin/token-bank/shares/x/paused"},
		{"DELETE", "/api/admin/token-bank/shares/x"},
		{"PUT", "/api/admin/token-bank/shares/x/models/m/tier"},
		{"GET", "/api/admin/token-bank/price-book"},
		{"POST", "/api/admin/token-bank/price-book"},
		{"DELETE", "/api/admin/token-bank/price-book/x"},
		{"GET", "/api/admin/token-bank/credit-shares"},
		{"POST", "/api/admin/token-bank/credit-shares/x/revoke"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(""))
		rec := httptest.NewRecorder()
		mux := http.NewServeMux()
		h := env.handlers
		adm := env.adminAuth
		mux.HandleFunc("GET /api/admin/token-bank/settings", RequireAdmin(adm, h.TokenBankGetSettings))
		mux.HandleFunc("PUT /api/admin/token-bank/settings", RequireAdmin(adm, h.TokenBankPutSettings))
		mux.HandleFunc("GET /api/admin/token-bank/overview", RequireAdmin(adm, h.TokenBankAdminOverview))
		mux.HandleFunc("GET /api/admin/token-bank/leaderboard", RequireAdmin(adm, h.TokenBankAdminLeaderboard))
		mux.HandleFunc("GET /api/admin/token-bank/users", RequireAdmin(adm, h.TokenBankAdminUsers))
		mux.HandleFunc("GET /api/admin/token-bank/shares", RequireAdmin(adm, h.TokenBankAdminShares))
		mux.HandleFunc("PUT /api/admin/token-bank/shares/{id}/paused", RequireAdmin(adm, h.TokenBankAdminSetSharePaused))
		mux.HandleFunc("DELETE /api/admin/token-bank/shares/{id}", RequireAdmin(adm, h.TokenBankAdminTakeOutShare))
		mux.HandleFunc("PUT /api/admin/token-bank/shares/{id}/models/{model}/tier", RequireAdmin(adm, h.TokenBankAdminSetModelTier))
		mux.HandleFunc("GET /api/admin/token-bank/price-book", RequireAdmin(adm, h.TokenBankAdminListPriceBook))
		mux.HandleFunc("POST /api/admin/token-bank/price-book", RequireAdmin(adm, h.TokenBankAdminUpsertPriceRule))
		mux.HandleFunc("DELETE /api/admin/token-bank/price-book/{id}", RequireAdmin(adm, h.TokenBankAdminDeletePriceRule))
		mux.HandleFunc("GET /api/admin/token-bank/credit-shares", RequireAdmin(adm, h.TokenBankAdminListCreditShares))
		mux.HandleFunc("POST /api/admin/token-bank/credit-shares/{id}/revoke", RequireAdmin(adm, h.TokenBankAdminRevokeCreditShare))
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a token = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestTokenBankAdminSettingsServeDefaultsThenPersist(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)

	// Before anything is saved the form must render, so GET returns defaults.
	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET settings = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	settings, _ := body["settings"].(map[string]any)
	if settings == nil {
		t.Fatalf("GET settings body has no settings object: %s", rec.Body.String())
	}
	if fee, _ := settings["fee_rate"].(float64); fee != 0.10 {
		t.Fatalf("default fee_rate = %v, want 0.10", settings["fee_rate"])
	}
	if hours, _ := settings["canary_window_hours"].(float64); hours != 24 {
		t.Fatalf("default canary_window_hours = %v, want 24", settings["canary_window_hours"])
	}
	if target, _ := settings["fee_target"].(string); target != "provider" {
		t.Fatalf("default fee_target = %v, want provider", settings["fee_target"])
	}

	// A valid update persists and comes back.
	payload := map[string]any{"settings": map[string]any{
		"fee_rate": 0.25, "fee_target": "provider",
		"default_unit_input_credits_per_10k": 4.5,
		"credit_share_max_ratio":             0.4,
		"credit_share_link_ttl_hours":        72,
		"max_shares_per_user":                5,
		"canary_window_hours":                48,
	}}
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/settings", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT settings = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/settings", nil)
	body = decodeMap(t, rec)
	settings, _ = body["settings"].(map[string]any)
	if fee, _ := settings["fee_rate"].(float64); fee != 0.25 {
		t.Fatalf("persisted fee_rate = %v, want 0.25", settings["fee_rate"])
	}
	if ratio, _ := settings["credit_share_max_ratio"].(float64); ratio != 0.4 {
		t.Fatalf("persisted credit_share_max_ratio = %v, want 0.4", settings["credit_share_max_ratio"])
	}
	if hours, _ := settings["canary_window_hours"].(float64); hours != 48 {
		t.Fatalf("persisted canary_window_hours = %v, want 48", settings["canary_window_hours"])
	}
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/settings", map[string]any{
		"settings": map[string]any{"canary_window_hours": 0},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT canary 0 = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body = decodeMap(t, rec)
	settings, _ = body["settings"].(map[string]any)
	hours, ok := settings["canary_window_hours"].(float64)
	if !ok || hours != 0 {
		t.Fatalf("canary_window_hours 0 = %#v, want 0", settings["canary_window_hours"])
	}
	// The blob must actually be in the settings store, not just echoed back.
	raw, err := env.settings.Get(context.Background(), sqlite.TokenBankSettingsKey)
	if err != nil || !strings.Contains(raw, "0.25") {
		t.Fatalf("stored settings blob = %q (err %v), want it to contain the new fee", raw, err)
	}
}

func TestTokenBankAdminSettingsOmittedFieldKeepsStored(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	stored := sqlite.DefaultTokenBankSettings()
	stored.FeeRate = 0.1
	stored.ClearingNodeID = "hc-clearing"
	stored.ProviderDenylist = []string{"blocked.example"}
	blob, err := json.Marshal(stored)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := env.settings.Set(context.Background(), sqlite.TokenBankSettingsKey, string(blob)); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	// The admin form does not edit the clearing node or the denylist. A save
	// that only changes the fee must not blank them, and it must not zero a
	// unit price the body left out.
	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/settings", map[string]any{
		"settings": map[string]any{"fee_rate": 0.2},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT fee only = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	settings, _ := body["settings"].(map[string]any)
	if fee, _ := settings["fee_rate"].(float64); fee != 0.2 {
		t.Fatalf("fee_rate = %v, want 0.2", settings["fee_rate"])
	}
	if settings["clearing_node_id"] != "hc-clearing" {
		t.Fatalf("clearing_node_id = %v, want hc-clearing", settings["clearing_node_id"])
	}
	deny, _ := settings["provider_denylist"].([]any)
	if len(deny) != 1 || deny[0] != "blocked.example" {
		t.Fatalf("provider_denylist = %#v, want [blocked.example]", settings["provider_denylist"])
	}
	if unit, _ := settings["default_unit_input_credits_per_10k"].(float64); unit != 3 {
		t.Fatalf("omitted input unit = %v, want the stored 3", settings["default_unit_input_credits_per_10k"])
	}

	// An explicit empty value still clears. Absence is what means "leave it".
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/settings", map[string]any{
		"settings": map[string]any{
			"clearing_node_id":  "",
			"provider_denylist": []any{},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT clear = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body = decodeMap(t, rec)
	settings, _ = body["settings"].(map[string]any)
	if settings["clearing_node_id"] != "" {
		t.Fatalf("cleared clearing_node_id = %v, want empty", settings["clearing_node_id"])
	}
	if deny, _ = settings["provider_denylist"].([]any); len(deny) != 0 {
		t.Fatalf("cleared provider_denylist = %#v, want empty", settings["provider_denylist"])
	}
	if fee, _ := settings["fee_rate"].(float64); fee != 0.2 {
		t.Fatalf("fee_rate after clear = %v, want the fee written above", settings["fee_rate"])
	}
}

func TestTokenBankAdminSettingsRejectInvalidValues(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	cases := []struct {
		name    string
		payload map[string]any
	}{
		{"fee above 1", map[string]any{"fee_rate": 1.5}},
		{"negative fee", map[string]any{"fee_rate": -0.1}},
		// §5 takes the fee from the sharer. Accepting "consumer" would move the
		// cost to a different person, which is a money decision not a setting.
		{"fee on the consumer", map[string]any{"fee_rate": 0.1, "fee_target": "consumer"}},
		{"ratio above 1", map[string]any{"credit_share_max_ratio": 1.2}},
		{"zero ttl", map[string]any{"credit_share_link_ttl_hours": 0}},
		{"zero max shares", map[string]any{"max_shares_per_user": 0}},
		{"negative unit price", map[string]any{"default_unit_input_credits_per_10k": -1}},
		{"negative canary window", map[string]any{"canary_window_hours": -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/settings",
				map[string]any{"settings": tc.payload})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s = %d, want 400; body %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTokenBankAdminSettingsRejectMalformedBody(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	req := httptest.NewRequest("PUT", "/api/admin/token-bank/settings", strings.NewReader("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.adminToken)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/admin/token-bank/settings",
		RequireAdmin(env.adminAuth, env.handlers.TokenBankPutSettings))
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
}

func TestTokenBankAdminOverviewAndUsers(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	env.seedShare(t, "share-2", "user-1", "fp-2", "claude-sonnet")
	env.seedShare(t, "share-3", "user-2", "fp-3", "gemini-pro")

	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/overview", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("overview = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	overview, _ := body["overview"].(map[string]any)
	if overview == nil {
		t.Fatalf("overview body missing: %s", rec.Body.String())
	}
	if n, _ := overview["ShareCount"].(float64); n != 3 {
		t.Fatalf("overview ShareCount = %v, want 3", overview["ShareCount"])
	}
	if n, _ := overview["ShareOwners"].(float64); n != 2 {
		t.Fatalf("overview ShareOwners = %v, want 2", overview["ShareOwners"])
	}
	// The overview carries the live fee so the admin page can show the rate it
	// is reporting against without a second request.
	if fee, _ := body["fee_rate"].(float64); fee != 0.10 {
		t.Fatalf("overview fee_rate = %v, want the default 0.10", body["fee_rate"])
	}

	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("users = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body = decodeMap(t, rec)
	users, _ := body["users"].([]any)
	if len(users) != 2 {
		t.Fatalf("users = %d rows, want 2", len(users))
	}
	first, _ := users[0].(map[string]any)
	if first["OwnerUserID"] != "user-1" {
		t.Fatalf("users[0] = %v, want user-1 (highest earner first)", first["OwnerUserID"])
	}
	if n, _ := first["ShareCount"].(float64); n != 2 {
		t.Fatalf("user-1 ShareCount = %v, want 2", first["ShareCount"])
	}
}

func TestTokenBankAdminSharesMasksEmailAndNeverReturnsTheKey(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/shares", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("shares = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	shares, _ := body["shares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(shares))
	}
	share, _ := shares[0].(map[string]any)

	// The admin endpoint must never hand back the key material, even to an
	// admin: an endpoint that returns it is an endpoint that can leak it.
	if key, ok := share["EncryptedKey"]; ok {
		if s, _ := key.(string); s != "" {
			t.Fatalf("share payload exposed EncryptedKey = %q, want empty", s)
		}
	}
	if has, _ := share["has_key"].(bool); !has {
		t.Fatalf("has_key = %v, want true (the share does have a key)", share["has_key"])
	}
	if masked, _ := share["owner_email_masked"].(string); masked != "u***@example.com" {
		t.Fatalf("owner_email_masked = %q, want u***@example.com", masked)
	}
	models, _ := share["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("share models = %d, want 1 (models are included for the tier editor)", len(models))
	}
}

func TestTokenBankAdminEarningsComeFromUsageAndAccounts(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	ctx := context.Background()
	// The denormalized counters are the wrong source. A call settled before
	// they were maintained left the credit on the usage row and the account.
	if _, err := env.provider.Write.ExecContext(ctx,
		`UPDATE token_bank_shares SET total_earned_micro = 1 WHERE id = ?`, "share-1"); err != nil {
		t.Fatalf("seed share counter: %v", err)
	}
	if _, err := env.provider.Write.ExecContext(ctx,
		`UPDATE token_bank_models SET earned_micro = 1 WHERE share_id = ?`, "share-1"); err != nil {
		t.Fatalf("seed model counter: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := env.provider.Write.ExecContext(ctx,
		`INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, model_name, net_micro, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		"req-1", "share-1", "user-1", "GPT-4o", int64(9_000_000), now); err != nil {
		t.Fatalf("seed usage: %v", err)
	}
	if _, err := env.repo.AppendLedger(ctx, sqlite.TokenBankLedgerEntry{
		ID: "earn:user-1", UserID: "user-1", Bucket: sqlite.TokenBankBucketEarned,
		AmountMicro: 9_000_000, BizKey: "earn:user-1", CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
	earned := func(v any) int64 {
		t.Helper()
		n, ok := v.(float64)
		if !ok {
			t.Fatalf("earned type %T = %v", v, v)
		}
		return int64(n)
	}

	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/shares", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("shares = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	shares, _ := body["shares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(shares))
	}
	share, _ := shares[0].(map[string]any)
	if earned(share["TotalEarnedMicro"]) != 9_000_000 {
		t.Fatalf("share earned = %v, want 9000000 from usage", share["TotalEarnedMicro"])
	}
	models, _ := share["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %d, want 1", len(models))
	}
	model, _ := models[0].(map[string]any)
	if earned(model["EarnedMicro"]) != 9_000_000 {
		t.Fatalf("model earned = %v, want 9000000 (case-insensitive usage)", model["EarnedMicro"])
	}

	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/users", nil)
	body = decodeMap(t, rec)
	users, _ := body["users"].([]any)
	if len(users) != 1 {
		t.Fatalf("users = %d, want 1", len(users))
	}
	user, _ := users[0].(map[string]any)
	if earned(user["EarnedMicro"]) != 9_000_000 {
		t.Fatalf("user earned = %v, want 9000000 from the account", user["EarnedMicro"])
	}

	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/overview", nil)
	body = decodeMap(t, rec)
	overview, _ := body["overview"].(map[string]any)
	if earned(overview["EarnedMicro"]) != 9_000_000 {
		t.Fatalf("overview earned = %v, want 9000000 from the account", overview["EarnedMicro"])
	}
}

func TestTokenBankAdminSharesFilterByUserAndStatus(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	env.seedShare(t, "share-2", "user-2", "fp-2", "claude-sonnet")

	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/shares?user_id=user-1", nil)
	body := decodeMap(t, rec)
	shares, _ := body["shares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("filtered shares = %d, want 1", len(shares))
	}

	// Pause user-2's share, then filter by status.
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-2/paused",
		map[string]any{"paused": true, "reason": "bad key"})
	if rec.Code != http.StatusOK {
		t.Fatalf("pause = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/shares?status=paused", nil)
	body = decodeMap(t, rec)
	shares, _ = body["shares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("paused shares = %d, want 1", len(shares))
	}
	paused, _ := shares[0].(map[string]any)
	if paused["ID"] != "share-2" {
		t.Fatalf("paused share = %v, want share-2", paused["ID"])
	}
	if reason, _ := paused["PausedReason"].(string); reason != "bad key" {
		t.Fatalf("PausedReason = %q, want bad key", reason)
	}
}

func TestTokenBankAdminPausedDefaultsToPausing(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	// An empty body must not be read as paused=false: a bare PUT is the admin
	// reaching for the pause switch, and resuming a quarantined share by
	// accident is the worse failure.
	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/paused", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("pause with empty body = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	share, err := env.repo.LoadShare(context.Background(), "share-1", "")
	if err != nil {
		t.Fatalf("LoadShare: %v", err)
	}
	if share.Status != sqlite.TokenBankShareStatusPaused {
		t.Fatalf("status = %q, want paused", share.Status)
	}

	// Resuming requires an explicit false.
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/paused", map[string]any{"paused": false})
	if rec.Code != http.StatusOK {
		t.Fatalf("resume = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	share, _ = env.repo.LoadShare(context.Background(), "share-1", "")
	if share.Status != sqlite.TokenBankShareStatusActive {
		t.Fatalf("status after resume = %q, want active", share.Status)
	}
}

func TestTokenBankAdminPauseUnknownShareIs404(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/ghost/paused", map[string]any{"paused": true})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("pause unknown = %d, want 404", rec.Code)
	}
}

func TestTokenBankAdminTakeOutShareRemovesIt(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	rec := env.doAdmin(t, "DELETE", "/api/admin/token-bank/shares/share-1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("takeout = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if _, err := env.repo.LoadShare(context.Background(), "share-1", ""); err == nil {
		t.Fatalf("share still exists after admin takeout")
	}
	rec = env.doAdmin(t, "DELETE", "/api/admin/token-bank/shares/share-1", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second takeout = %d, want 404", rec.Code)
	}
}

func TestTokenBankAdminSetModelTierUsesCanonicalPair(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/models/gpt-4o/tier",
		map[string]any{"tier": "high"})
	if rec.Code != http.StatusOK {
		t.Fatalf("tier = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	// The canonical pair comes from §3.4, not from the caller: a tier is a
	// meaning, and letting the caller pair "high" with 1.0 would pay high rates
	// for mid work.
	if mult, _ := body["tier_multiplier"].(float64); mult != 2.0 {
		t.Fatalf("tier_multiplier = %v, want the canonical 2.0", body["tier_multiplier"])
	}
	if array, _ := body["array_id"].(string); array != "token_bank_high" {
		t.Fatalf("array_id = %q, want token_bank_high", body["array_id"])
	}

	models, err := env.repo.ListModels(context.Background(), "share-1")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if models[0].Tier != "high" || models[0].TierMultiplier != 2.0 || models[0].ArrayID != "token_bank_high" {
		t.Fatalf("persisted model = %+v, want high/2.0/token_bank_high", models[0])
	}
}

func TestTokenBankAdminSetModelTierRejectsBadTier(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	cases := []map[string]any{
		{"tier": "legendary"},
		{"tier": ""},
		// custom without numbers is meaningless
		{"tier": "custom"},
		{"tier": "custom", "tier_multiplier": 0},
		// a negative or NaN multiplier must not reach the price arithmetic
		{"tier": "high", "tier_multiplier": -2},
	}
	for _, payload := range cases {
		rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/models/gpt-4o/tier", payload)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("tier payload %v = %d, want 400; body %s", payload, rec.Code, rec.Body.String())
		}
	}
}

func TestTokenBankAdminSetModelTierCustomTakesTheExplicitPair(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/models/gpt-4o/tier",
		map[string]any{"tier": "custom", "tier_multiplier": 1.75, "array_id": "token_bank_high"})
	if rec.Code != http.StatusOK {
		t.Fatalf("custom tier = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	if mult, _ := body["tier_multiplier"].(float64); mult != 1.75 {
		t.Fatalf("custom tier_multiplier = %v, want 1.75", body["tier_multiplier"])
	}
	if array, _ := body["array_id"].(string); array != "token_bank_high" {
		t.Fatalf("custom array_id = %q, want the explicit token_bank_high", body["array_id"])
	}
}

func TestTokenBankAdminSetModelTierUnknownTargetsAre404(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	rec := env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/share-1/models/ghost/tier",
		map[string]any{"tier": "high"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown model = %d, want 404", rec.Code)
	}
	rec = env.doAdmin(t, "PUT", "/api/admin/token-bank/shares/ghost/models/gpt-4o/tier",
		map[string]any{"tier": "high"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown share = %d, want 404", rec.Code)
	}
}

func TestTokenBankAdminPriceBookCRUD(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)

	rec := env.doAdmin(t, "POST", "/api/admin/token-bank/price-book", map[string]any{
		"model_pattern":                    "gpt-4o",
		"unit_input_credits_per_10k":       3.0,
		"unit_output_credits_per_10k":      6.0,
		"unit_cached_read_credits_per_10k": 0.3,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("upsert = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	rule, _ := body["rule"].(map[string]any)
	ruleID, _ := rule["ID"].(string)
	if ruleID == "" {
		t.Fatalf("rule id is empty: %s", rec.Body.String())
	}

	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/price-book", nil)
	body = decodeMap(t, rec)
	rules, _ := body["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %d, want 1", len(rules))
	}

	// DELETE accepts the rule id.
	rec = env.doAdmin(t, "DELETE", "/api/admin/token-bank/price-book/"+ruleID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/price-book", nil)
	body = decodeMap(t, rec)
	rules, _ = body["rules"].([]any)
	if len(rules) != 0 {
		t.Fatalf("rules after delete = %d, want 0", len(rules))
	}
	rec = env.doAdmin(t, "DELETE", "/api/admin/token-bank/price-book/"+ruleID, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

func TestTokenBankAdminPriceBookOmittedUnitKeepsExisting(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	rec := env.doAdmin(t, "POST", "/api/admin/token-bank/price-book", map[string]any{
		"model_pattern":                    "gpt-4o",
		"unit_input_credits_per_10k":       3.0,
		"unit_output_credits_per_10k":      6.0,
		"unit_cached_read_credits_per_10k": 0.3,
		"unit_cache_write_credits_per_10k": 3.75,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	// The admin form leaves untouched units blank. Those fields arrive as JSON
	// null and must not zero the prices already stored for this pattern.
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/price-book", map[string]any{
		"model_pattern":               "gpt-4o",
		"unit_input_credits_per_10k":  9.0,
		"unit_output_credits_per_10k": nil,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("partial update = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rule, _ := decodeMap(t, rec)["rule"].(map[string]any)
	if rule["UnitInputPer10K"] != 9.0 || rule["UnitOutputPer10K"] != 6.0 ||
		rule["UnitCachedReadPer10K"] != 0.3 || rule["UnitCacheWritePer10K"] != 3.75 {
		t.Fatalf("partial update rewrote stored units: %#v", rule)
	}

	// An explicit 0 is a free unit, distinct from a field the form left blank.
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/price-book", map[string]any{
		"model_pattern":               "gpt-4o",
		"unit_output_credits_per_10k": 0,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("zero update = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rule, _ = decodeMap(t, rec)["rule"].(map[string]any)
	if rule["UnitOutputPer10K"] != 0.0 || rule["UnitInputPer10K"] != 9.0 {
		t.Fatalf("explicit zero did not stick: %#v", rule)
	}

	// A pattern that has never been stored still treats a missing unit as 0.
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/price-book", map[string]any{
		"model_pattern":              "new-model",
		"unit_input_credits_per_10k": 1.5,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("create partial = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	rule, _ = decodeMap(t, rec)["rule"].(map[string]any)
	if rule["UnitInputPer10K"] != 1.5 || rule["UnitOutputPer10K"] != 0.0 ||
		rule["UnitCachedReadPer10K"] != 0.0 || rule["UnitCacheWritePer10K"] != 0.0 {
		t.Fatalf("new pattern did not default blank units to 0: %#v", rule)
	}
}

func TestTokenBankAdminPriceBookRejectsMidPatternWildcard(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	// Resolution only honours a trailing `*`. Accepting `a*b` would store a rule
	// that silently prices nothing.
	for _, pattern := range []string{"a*b", "a*b*", "gpt-4o?"} {
		rec := env.doAdmin(t, "POST", "/api/admin/token-bank/price-book",
			map[string]any{"model_pattern": pattern, "unit_input_credits_per_10k": 1})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("pattern %q = %d, want 400; body %s", pattern, rec.Code, rec.Body.String())
		}
	}
	// A plain trailing glob is fine.
	rec := env.doAdmin(t, "POST", "/api/admin/token-bank/price-book",
		map[string]any{"model_pattern": "gpt-4o-*", "unit_input_credits_per_10k": 1})
	if rec.Code != http.StatusOK {
		t.Fatalf("trailing glob = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
}

func TestTokenBankAdminPriceBookRejectsNegativeAndMissingPattern(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	rec := env.doAdmin(t, "POST", "/api/admin/token-bank/price-book",
		map[string]any{"model_pattern": "gpt-4o", "unit_input_credits_per_10k": -1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative price = %d, want 400", rec.Code)
	}
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/price-book",
		map[string]any{"unit_input_credits_per_10k": 1})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing pattern = %d, want 400", rec.Code)
	}
}

func TestTokenBankAdminCreditSharesAuditMasksBothSides(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	sender, senderToken := env.createUser(t, "sender@example.com")
	_, claimerToken := env.createUser(t, "claimer@example.com")
	env.seedCredits(t, sender.ID, 100)

	// Create a link through the client API, then claim it as the other user so
	// the audit row has both sides populated.
	rec := env.do(t, "POST", "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 30})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create gift link = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	code, _ := body["code"].(string)
	if code == "" {
		t.Fatalf("gift link has no code: %s", rec.Body.String())
	}
	rec = env.do(t, "POST", "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	rec = env.doAdmin(t, "GET", "/api/admin/token-bank/credit-shares", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("audit = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	body = decodeMap(t, rec)
	rows, _ := body["credit_shares"].([]any)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	// Both sides masked: the admin correlates, they do not harvest.
	if got, _ := row["sender_email_masked"].(string); got != "s***@example.com" {
		t.Fatalf("sender_email_masked = %q, want s***@example.com", got)
	}
	if got, _ := row["claimed_by_email_masked"].(string); got != "c***@example.com" {
		t.Fatalf("claimed_by_email_masked = %q, want c***@example.com", got)
	}
	if credits, _ := row["credits_micro"].(float64); credits != 30_000_000 {
		t.Fatalf("credits_micro = %v, want 30000000", row["credits_micro"])
	}
	// Claimed and not withdrawn is still revocable: the credits are frozen
	// on the sender until the receiver withdraws them.
	if rev, _ := row["revocable"].(bool); !rev {
		t.Fatalf("revocable = false for a claimed link, want true")
	}
}

func TestTokenBankAdminAuditListsEverySender(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	// Two different senders. The store's ListGiftLinks must treat an empty
	// sender filter as "everyone" rather than "sender_user_id = ''", which
	// would show an admin an empty audit table.
	for _, email := range []string{"a@example.com", "b@example.com"} {
		user, token := env.createUser(t, email)
		env.seedCredits(t, user.ID, 100)
		rec := env.do(t, "POST", "/api/v1/credits/share-links", token, map[string]any{"credits": 10})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create link for %s = %d, want 201; body %s", email, rec.Code, rec.Body.String())
		}
	}
	rec := env.doAdmin(t, "GET", "/api/admin/token-bank/credit-shares", nil)
	body := decodeMap(t, rec)
	rows, _ := body["credit_shares"].([]any)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2 (one per sender); an empty filter must mean everyone", len(rows))
	}
}

func TestTokenBankAdminRevokeCreditShareReturnsFreezeToSender(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	sender, senderToken := env.createUser(t, "sender@example.com")
	env.seedCredits(t, sender.ID, 100)

	rec := env.do(t, "POST", "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 30})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	linkID, _ := body["id"].(string)

	// The freeze must be visible to the client balance before the revoke...
	rec = env.do(t, "GET", "/api/v1/token-bank/summary", senderToken, nil)
	body = decodeMap(t, rec)
	if frozen, _ := body["frozen_micro"].(float64); frozen != 30_000_000 {
		t.Fatalf("frozen_micro before revoke = %v, want 30000000", body["frozen_micro"])
	}

	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/credit-shares/"+linkID+"/revoke", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin revoke = %d, want 200; body %s", rec.Code, rec.Body.String())
	}

	// ...and the credits are back to the sender afterwards. The unfreeze targets
	// the owner recorded on the row, so an admin revoke cannot redirect funds.
	rec = env.do(t, "GET", "/api/v1/token-bank/summary", senderToken, nil)
	body = decodeMap(t, rec)
	if frozen, _ := body["frozen_micro"].(float64); frozen != 0 {
		t.Fatalf("frozen_micro after revoke = %v, want 0", body["frozen_micro"])
	}
	if avail, _ := body["available_micro"].(float64); avail != 100_000_000 {
		t.Fatalf("available_micro after revoke = %v, want 100000000 (the freeze returned)", body["available_micro"])
	}
}

func TestTokenBankAdminRevokeCreditShareReportsNotActive(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	sender, senderToken := env.createUser(t, "sender@example.com")
	env.seedCredits(t, sender.ID, 100)

	rec := env.do(t, "POST", "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 30})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body %s", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	linkID, _ := body["id"].(string)

	// The sender revokes their own link first.
	rec = env.do(t, "POST", "/api/v1/credits/share-links/"+linkID+"/revoke", senderToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sender revoke = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	// A second revoke — now from the admin — is a conflict, not a 500: the link
	// is simply no longer active.
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/credit-shares/"+linkID+"/revoke", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second revoke = %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAdmin(t, "POST", "/api/admin/token-bank/credit-shares/ghost/revoke", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("revoke unknown = %d, want 404", rec.Code)
	}
}

func TestTokenBankAdminPageIsClamped(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/admin/token-bank/shares?limit=9999&offset=5", nil)
	limit, offset := tokenBankAdminPage(r)
	if limit != 200 {
		t.Fatalf("limit = %d, want it clamped to 200", limit)
	}
	if offset != 5 {
		t.Fatalf("offset = %d, want 5", offset)
	}
	r = httptest.NewRequest("GET", "/api/admin/token-bank/shares?limit=abc&offset=-3", nil)
	limit, offset = tokenBankAdminPage(r)
	if limit != 20 {
		t.Fatalf("default limit = %d, want 20", limit)
	}
	if offset != 0 {
		t.Fatalf("negative offset = %d, want 0", offset)
	}
}

func TestTokenBankAdminEndpointsReportUnavailableWithoutRepo(t *testing.T) {
	// A node where the LLM module never initialised: the admin endpoints must
	// answer 503 rather than panicking on a nil repository. The settings pair is
	// excluded because it degrades to in-memory defaults by design.
	h := NewSkillMarketHandlers(SkillMarketConfig{})
	if h.tokenBankAdminRepo() != nil {
		t.Fatalf("admin repo accessor = non-nil, want nil without wiring")
	}
	req := httptest.NewRequest("GET", "/api/admin/token-bank/overview", nil)
	rec := httptest.NewRecorder()
	mux := http.NewServeMux()
	// RequireAdmin is skipped here: the point is the handler's own nil-repo path.
	mux.HandleFunc("GET /api/admin/token-bank/overview", h.TokenBankAdminOverview)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("overview without repo = %d, want 503", rec.Code)
	}
}

func TestTokenBankSettingsValidationAcceptsTheDefaults(t *testing.T) {
	// The documented defaults must survive their own validator: an admin who
	// opens the form and saves it unchanged must not get a 400.
	if _, err := validateTokenBankSettings(sqlite.DefaultTokenBankSettings()); err != nil {
		t.Fatalf("defaults rejected by the validator: %v", err)
	}
	// fee_rate 0 is a legitimate "no fee".
	zero := sqlite.DefaultTokenBankSettings()
	zero.FeeRate = 0
	if _, err := validateTokenBankSettings(zero); err != nil {
		t.Fatalf("fee_rate 0 rejected: %v", err)
	}
}
