package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
	_ "modernc.org/sqlite"
)

// tokenBankTestEnv is a real SQLite-backed Token Bank HTTP environment. The
// handlers move money through SUM-over-ledger arithmetic, so a mock repository
// would test the mock rather than the invariant: several of the tests below
// assert that conservation holds after a rejected request.
type tokenBankTestEnv struct {
	handlers *SkillMarketHandlers
	repo     *sqlite.TokenBankRepo
	users    *skillmarket.UserService
	auth     *skillmarket.AuthService
	provider *sqlite.Provider
	// adminAuth is only populated by newTokenBankAdminTestEnv; the §6.2 tests
	// wrap the admin verbs in RequireAdmin against it.
	adminAuth  *auth.AdminService
	adminToken string
}

func newTokenBankTestEnv(t *testing.T) *tokenBankTestEnv {
	t.Helper()
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN: filepath.Join(t.TempDir(), "token-bank-http.db"),
		// Match the hubcenter default of one write connection. Connection pragmas
		// are applied on every new connection; this fixture still uses one writer.
		WAL:               true,
		BusyTimeoutMS:     5000,
		MaxReadOpenConns:  4,
		MaxReadIdleConns:  2,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	// The ledger, gift-link and withdrawal tables live in EnsureLLMTables, not
	// in the migration set: the Token Bank is an LLM-module feature.
	if err := sqlite.EnsureLLMTables(provider.Write); err != nil {
		t.Fatalf("ensure llm tables: %v", err)
	}
	smStore, err := skillmarket.NewStore(provider.Write, provider.Read)
	if err != nil {
		t.Fatalf("skillmarket store: %v", err)
	}
	users := skillmarket.NewUserService(smStore, nil)
	auth := skillmarket.NewAuthService(smStore, nil, "")
	repo := sqlite.NewTokenBankRepo(provider)
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: smStore, UserSvc: users, AuthSvc: auth})
	h.SetTokenBankRepo(repo, "hc-test")
	return &tokenBankTestEnv{handlers: h, repo: repo, users: users, auth: auth, provider: provider}
}

// createUser makes a verified account and returns a usable session token. Most
// tests need `verified` because claiming a gift link requires it.
func (e *tokenBankTestEnv) createUser(t *testing.T, email string) (*skillmarket.SkillMarketUser, string) {
	t.Helper()
	ctx := context.Background()
	user, err := e.users.EnsureAccount(ctx, email)
	if err != nil {
		t.Fatalf("ensure account %s: %v", email, err)
	}
	if err := e.auth.AutoVerify(ctx, user.ID); err != nil {
		t.Fatalf("auto verify %s: %v", email, err)
	}
	user, err = e.users.EnsureAccount(ctx, email)
	if err != nil {
		t.Fatalf("reload account %s: %v", email, err)
	}
	session, err := e.auth.CreateSessionForUser(ctx, user.ID, user.Email)
	if err != nil {
		t.Fatalf("create session %s: %v", email, err)
	}
	return user, session.Token
}

// seedCredits writes an earned-movement directly into the ledger. It is the
// only way to give a test account a balance: settlement is a later phase.
func (e *tokenBankTestEnv) seedCredits(t *testing.T, userID string, credits int64) {
	t.Helper()
	_, err := e.repo.AppendLedger(context.Background(), sqlite.TokenBankLedgerEntry{
		ID:          "earned_" + userID,
		UserID:      userID,
		Bucket:      sqlite.TokenBankBucketEarned,
		AmountMicro: credits * 1_000_000,
		BizKey:      "seed:" + userID,
		RefType:     "test_seed",
	})
	if err != nil {
		t.Fatalf("seed credits for %s: %v", userID, err)
	}
}

func (e *tokenBankTestEnv) do(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.route(rec, req)
	return rec
}

// route dispatches through the same handler registration the real router uses,
// so a path/verb mismatch in the route table is caught here rather than in
// production.
func (e *tokenBankTestEnv) route(rec *httptest.ResponseRecorder, req *http.Request) {
	mux := http.NewServeMux()
	h := e.handlers
	mux.HandleFunc("GET /api/v1/token-bank/summary", h.TokenBankSummary)
	mux.HandleFunc("POST /api/v1/token-bank/credits/withdraw", h.TokenBankWithdrawCredits)
	mux.HandleFunc("GET /api/v1/token-bank/credits/withdrawals", h.TokenBankListWithdrawals)
	mux.HandleFunc("GET /api/v1/token-bank/usage/daily", h.TokenBankUsageDaily)
	mux.HandleFunc("GET /api/v1/token-bank/usage.csv", h.TokenBankUsageCSV)
	mux.HandleFunc("POST /api/v1/credits/share-links", h.TokenBankCreateGiftLink)
	mux.HandleFunc("GET /api/v1/credits/share-links", h.TokenBankListGiftLinks)
	mux.HandleFunc("POST /api/v1/credits/share-links/{id}/revoke", h.TokenBankRevokeGiftLink)
	mux.HandleFunc("GET /c/{code}", h.TokenBankGiftLanding)
	mux.HandleFunc("GET /api/v1/credits/share-links/{code}/preview", h.TokenBankPreviewGiftLink)
	mux.HandleFunc("POST /api/v1/credits/share-links/{code}/claim", h.TokenBankClaimGiftLink)
	mux.ServeHTTP(rec, req)
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode response: %v\nbody: %s", err, rec.Body.String())
	}
	return m
}

// --- tests ---

func TestTokenBankCodeAlphabetIsExactly32Symbols(t *testing.T) {
	// The encoding table is built in a package var, so a bad table panics in
	// init() and takes the whole server down at boot rather than failing one
	// endpoint. Pin the length so that mistake cannot come back.
	if got := len(giftLinkCodeEncoding.EncodeToString([]byte{0, 1, 2})); got != 5 {
		t.Fatalf("base32 of 3 bytes without padding = %d chars, want 5", got)
	}
	code, err := newGiftLinkCode()
	if err != nil {
		t.Fatalf("newGiftLinkCode: %v", err)
	}
	if len(code) != 10 {
		t.Fatalf("code %q length = %d, want 10", code, len(code))
	}
}

func TestTokenBankSummaryRequiresSession(t *testing.T) {
	env := newTokenBankTestEnv(t)
	rec := env.do(t, http.MethodGet, "/api/v1/token-bank/summary", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankSummaryExcludesFrozenFromAvailable(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "earner@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodGet, "/api/v1/token-bank/summary", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decodeMap(t, rec)
	if got["earned_micro"].(float64) != 10_000_000 {
		t.Fatalf("earned_micro = %v, want 1e7", got["earned_micro"])
	}
	if got["available_micro"].(float64) != 10_000_000 {
		t.Fatalf("available_micro = %v, want 1e7", got["available_micro"])
	}
	// 50% cap, rounded down.
	if got["gift_share_cap_micro"].(float64) != 5_000_000 {
		t.Fatalf("gift_share_cap_micro = %v, want 5e6", got["gift_share_cap_micro"])
	}
	if got["gift_share_percent"].(float64) != 50 {
		t.Fatalf("gift_share_percent = %v, want 50", got["gift_share_percent"])
	}
	// CountUserHubs tolerates a deployment with no hub_user_links rows and
	// degrades to 1, which keeps the averaging rule defined.
	if got["hub_count"].(float64) < 1 {
		t.Fatalf("hub_count = %v, want >= 1", got["hub_count"])
	}
}

func TestTokenBankGiftCreateFreezesAndShrinksAvailable(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "giver@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decodeMap(t, rec)
	code, _ := created["code"].(string)
	if len(code) != 10 {
		t.Fatalf("code = %q, want 10 chars", code)
	}
	if created["credits_micro"].(float64) != 5_000_000 {
		t.Fatalf("credits_micro = %v, want 5e6", created["credits_micro"])
	}

	// The freeze must show up in the summary as frozen, and must reduce
	// available: otherwise the sender could withdraw the same credits (C1).
	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/summary", token, nil)
	summary := decodeMap(t, rec)
	if summary["frozen_micro"].(float64) != 5_000_000 {
		t.Fatalf("frozen_micro = %v, want 5e6", summary["frozen_micro"])
	}
	if summary["available_micro"].(float64) != 5_000_000 {
		t.Fatalf("available_micro = %v, want 5e6 (frozen must not be spendable)", summary["available_micro"])
	}
	// With only 5 available the cap is 2.5, so a second 5-credit gift must fail.
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("second gift status = %d, want 402 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankGiftRejectsOverCap(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "cap@example.test")
	env.seedCredits(t, user.ID, 10)

	// 50% of 10 is 5; 6 must be refused before any freeze row is written.
	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 6})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (body %s)", rec.Code, rec.Body.String())
	}
	balance, err := env.repo.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance.FrozenMicro != 0 {
		t.Fatalf("frozen_micro = %d after a rejected gift, want 0", balance.FrozenMicro)
	}
}

func TestTokenBankGiftPreviewMasksSenderAndHidesCodeOwner(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "preview-sender@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	code := decodeMap(t, rec)["code"].(string)

	// Preview is public: no session token at all.
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body.String())
	}
	preview := decodeMap(t, rec)
	masked, _ := preview["sender_masked"].(string)
	if masked == "preview-sender@example.test" {
		t.Fatalf("sender email was returned unmasked: %q", masked)
	}
	if masked != "p***@example.test" {
		t.Fatalf("sender_masked = %q, want p***@example.test", masked)
	}
	if preview["claimable"] != true {
		t.Fatalf("claimable = %v, want true", preview["claimable"])
	}
	// The preview must not leak the claimer's identity or the sender's balance.
	if _, present := preview["sender_user_id"]; present {
		t.Fatalf("preview leaked sender_user_id")
	}
	if _, present := preview["claimed_by_email"]; present {
		t.Fatalf("preview leaked claimed_by_email")
	}
}

func TestTokenBankGiftClaimIsSingleWinnerAndForbidsOwnLink(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "owner@example.test")
	_, firstToken := env.createUser(t, "first@example.test")
	_, secondToken := env.createUser(t, "second@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	code := decodeMap(t, rec)["code"].(string)

	// The sender cannot claim their own link.
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", senderToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("own-link claim status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["code"]; got != "own_link" {
		t.Fatalf("own-link code = %v, want own_link", got)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", firstToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first claim status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", secondToken, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second claim status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["code"]; got != "already_claimed" {
		t.Fatalf("second claim code = %v, want already_claimed", got)
	}

	// Claiming moves no money by itself: the freeze is still frozen until the
	// receiver withdraws (§3.6), and the sender must not be credited back.
	balance, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatalf("sender balance: %v", err)
	}
	if balance.FrozenMicro != 5_000_000 {
		t.Fatalf("sender frozen_micro = %d after a claim, want 5e6", balance.FrozenMicro)
	}
	if balance.GrantedMicro != 0 {
		t.Fatalf("sender granted_micro = %d after a claim, want 0", balance.GrantedMicro)
	}
}

func TestTokenBankGiftClaimerCanResumeWithdraw(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "resume-sender@example.test")
	_, claimerToken := env.createUser(t, "resume-claimer@example.test")
	_, strangerToken := env.createUser(t, "resume-stranger@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	created := decodeMap(t, rec)
	code := created["code"].(string)
	id := created["id"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", "", nil)
	public := decodeMap(t, rec)
	if _, present := public["id"]; present {
		t.Fatalf("public preview leaked the link id")
	}
	if _, present := public["withdrawable"]; present {
		t.Fatalf("public preview leaked withdrawable")
	}
	if public["claimable"] != false {
		t.Fatalf("public claimable = %v, want false", public["claimable"])
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", strangerToken, nil)
	if _, present := decodeMap(t, rec)["withdrawable"]; present {
		t.Fatalf("another account was told it could withdraw")
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", claimerToken, nil)
	preview := decodeMap(t, rec)
	if preview["withdrawable"] != true || preview["id"] != id {
		t.Fatalf("claimer preview = %v, want withdrawable id %s", preview, id)
	}
	if _, present := preview["claimed_by_email"]; present {
		t.Fatalf("claimer preview leaked claimed_by_email")
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("resume claim status = %d, body %s", rec.Code, rec.Body.String())
	}
	resumed := decodeMap(t, rec)
	if resumed["resume"] != true || resumed["id"] != id {
		t.Fatalf("resume body = %v, want resume id %s", resumed, id)
	}
	balance, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatalf("sender balance: %v", err)
	}
	if balance.FrozenMicro != 5_000_000 || balance.GrantedMicro != 0 {
		t.Fatalf("resume moved money: frozen=%d granted=%d", balance.FrozenMicro, balance.GrantedMicro)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", strangerToken, nil)
	if rec.Code != http.StatusConflict || decodeMap(t, rec)["code"] != "already_claimed" {
		t.Fatalf("stranger retry status = %d body %s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", claimerToken, nil)
	claimed, _ := decodeMap(t, rec)["claimed_links"].([]any)
	if len(claimed) != 1 {
		t.Fatalf("claimed_links = %d, want 1", len(claimed))
	}
	row := claimed[0].(map[string]any)
	if row["id"] != id {
		t.Fatalf("claimed link id = %v, want %s", row["id"], id)
	}
	if _, present := row["code"]; present {
		t.Fatalf("claimed list leaked the share code")
	}
	if got, _ := row["sender_masked"].(string); got == "" || got == "resume-sender@example.test" {
		t.Fatalf("sender_masked = %q", got)
	}
	senderList := decodeMap(t, env.do(t, http.MethodGet, "/api/v1/credits/share-links", senderToken, nil))
	if held, _ := senderList["claimed_links"].([]any); len(held) != 0 {
		t.Fatalf("sender claimed_links = %d, want 0", len(held))
	}
	if sent, _ := senderList["share_links"].([]any); len(sent) != 1 || sent[0].(map[string]any)["status"] != "claimed" {
		t.Fatalf("sender share_links = %v", senderList["share_links"])
	}

	if _, err := env.repo.SettleClaimedGift(context.Background(), id, time.Now().UTC()); err != nil {
		t.Fatalf("settle: %v", err)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", claimerToken, nil)
	done := decodeMap(t, rec)
	if done["withdrawn"] != true {
		t.Fatalf("withdrawn = %v, want true", done["withdrawn"])
	}
	if _, present := done["withdrawable"]; present {
		t.Fatalf("settled preview still offered withdraw")
	}
	if _, present := done["id"]; present {
		t.Fatalf("settled preview leaked the link id")
	}
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", claimerToken, nil)
	if held, _ := decodeMap(t, rec)["claimed_links"].([]any); len(held) != 0 {
		t.Fatalf("claimed_links after settle = %d, want 0", len(held))
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusConflict || decodeMap(t, rec)["code"] != "already_withdrawn" {
		t.Fatalf("claim after settle status = %d body %s", rec.Code, rec.Body.String())
	}
}

func TestTokenBankGiftClaimRequiresVerifiedAccount(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "verified-sender@example.test")
	env.seedCredits(t, sender.ID, 10)

	ctx := context.Background()
	unverified, err := env.users.EnsureAccount(ctx, "unverified@example.test")
	if err != nil {
		t.Fatalf("ensure unverified account: %v", err)
	}
	session, err := env.auth.CreateSessionForUser(ctx, unverified.ID, unverified.Email)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	code := decodeMap(t, rec)["code"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", session.Token, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unverified claim status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["code"]; got != "unverified_account" {
		t.Fatalf("code = %v, want unverified_account", got)
	}
}

func TestTokenBankGiftRevokeReturnsFrozenCredits(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "revoker@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	created := decodeMap(t, rec)
	id := created["id"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+id+"/revoke", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d, body %s", rec.Code, rec.Body.String())
	}
	balance, err := env.repo.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance.FrozenMicro != 0 {
		t.Fatalf("frozen_micro = %d after revoke, want 0", balance.FrozenMicro)
	}
	if balance.AvailableMicro() != 10_000_000 {
		t.Fatalf("available_micro = %d after revoke, want 1e7", balance.AvailableMicro())
	}

	// Revoking twice must not unfreeze twice — the second call sees a
	// non-active link. Double-unfreezing would mint credits from nothing.
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+id+"/revoke", token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second revoke status = %d, want 409 (body %s)", rec.Code, rec.Body.String())
	}
	balance, _ = env.repo.Balance(context.Background(), user.ID)
	if balance.AvailableMicro() != 10_000_000 {
		t.Fatalf("available_micro = %d after a duplicate revoke, want 1e7", balance.AvailableMicro())
	}
}

func TestTokenBankGiftRevokeClaimedBeforeWithdraw(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "sender@example.test")
	claimer, claimerToken := env.createUser(t, "claimer@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	created := decodeMap(t, rec)
	id := created["id"].(string)
	code := created["code"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+id+"/revoke", senderToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke claimed status = %d, body %s", rec.Code, rec.Body.String())
	}

	balance, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if balance.FrozenMicro != 0 || balance.AvailableMicro() != 10_000_000 {
		t.Fatalf("sender after claimed revoke = frozen %d available %d, want 0 and 1e7", balance.FrozenMicro, balance.AvailableMicro())
	}
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", senderToken, nil)
	row := decodeMap(t, rec)["share_links"].([]any)[0].(map[string]any)
	if got, _ := row["status"].(string); got != "revoked" {
		t.Fatalf("sender list status = %q, want revoked", got)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", claimerToken, nil)
	claimed, _ := decodeMap(t, rec)["claimed_links"].([]any)
	if len(claimed) != 0 {
		t.Fatalf("claimed_links = %d, want 0 after the sender revokes", len(claimed))
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "gift_revoked") {
		t.Fatalf("claim after revoke = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", claimerToken, map[string]any{
		"request_id": "withdraw:gift:revoked", "kind": "gift", "link_id": id, "amount_micro": 5_000_000,
	})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "gift_revoked") {
		t.Fatalf("withdraw after revoke = %d, body %s", rec.Code, rec.Body.String())
	}
	claimerBalance, err := env.repo.Balance(context.Background(), claimer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimerBalance.AvailableMicro() != 0 {
		t.Fatalf("claimer available = %d, want 0", claimerBalance.AvailableMicro())
	}
}

func TestTokenBankGiftRevokeRejectsAnotherUsersLink(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "real-owner@example.test")
	_, attackerToken := env.createUser(t, "attacker@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	id := decodeMap(t, rec)["id"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+id+"/revoke", attackerToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-user revoke status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	balance, _ := env.repo.Balance(context.Background(), sender.ID)
	if balance.FrozenMicro != 5_000_000 {
		t.Fatalf("sender frozen_micro = %d, want 5e6: another user revoked the link", balance.FrozenMicro)
	}
}

func TestTokenBankGiftListShowsOwnLinksOnly(t *testing.T) {
	env := newTokenBankTestEnv(t)
	mine, myToken := env.createUser(t, "mine@example.test")
	theirs, theirsToken := env.createUser(t, "theirs@example.test")
	env.seedCredits(t, mine.ID, 10)
	env.seedCredits(t, theirs.ID, 10)

	env.do(t, http.MethodPost, "/api/v1/credits/share-links", myToken, map[string]any{"credits": 5})
	env.do(t, http.MethodPost, "/api/v1/credits/share-links", theirsToken, map[string]any{"credits": 5})

	rec := env.do(t, http.MethodGet, "/api/v1/credits/share-links", myToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	links, _ := decodeMap(t, rec)["share_links"].([]any)
	if len(links) != 1 {
		t.Fatalf("share_links length = %d, want 1", len(links))
	}
	// The owner list is about the sender, so it should not carry the code back
	// out; the code is returned once, at creation.
	if _, present := links[0].(map[string]any)["code"]; present {
		t.Fatalf("owner list leaked the share code")
	}
}

func TestTokenBankGiftListShowsClaimerAndWithdrawState(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "sender@example.test")
	claimer, claimerToken := env.createUser(t, "claimer@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	created := decodeMap(t, rec)
	code := created["code"].(string)
	id := created["id"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}
	// The claim response goes to the claimer and must stay masked. A missing
	// field would also fail the equality below, so an omitted email cannot
	// pass as "hidden".
	claimBody := decodeMap(t, rec)
	if got, _ := claimBody["claimed_by_email"].(string); got != "c***@example.test" {
		t.Fatalf("claim response claimed_by_email = %q, want the mask", got)
	}
	if _, present := claimBody["claimed_by_user_id"]; present {
		t.Fatalf("claim response leaked claimed_by_user_id")
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", senderToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	row := decodeMap(t, rec)["share_links"].([]any)[0].(map[string]any)
	if _, present := row["code"]; present {
		t.Fatalf("owner list leaked the share code")
	}
	if got, _ := row["claimed_by_email"].(string); got != "claimer@example.test" {
		t.Fatalf("claimed_by_email = %q, want the full claimer address", got)
	}
	if got, _ := row["claimed_by_user_id"].(string); got != claimer.ID {
		t.Fatalf("claimed_by_user_id = %q, want %q", got, claimer.ID)
	}
	if got, _ := row["status"].(string); got != "claimed" {
		t.Fatalf("status = %q, want claimed (withdraw has not happened)", got)
	}
	if row["claimed_at"] == "" {
		t.Fatalf("claimed_at is empty after a claim")
	}

	if _, err := env.repo.SettleClaimedGift(context.Background(), id, time.Now().UTC()); err != nil {
		t.Fatalf("settle: %v", err)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links", senderToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list after withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	row = decodeMap(t, rec)["share_links"].([]any)[0].(map[string]any)
	if _, present := row["code"]; present {
		t.Fatalf("owner list leaked the share code after withdraw")
	}
	if got, _ := row["status"].(string); got != "settled" {
		t.Fatalf("status after withdraw = %q, want settled", got)
	}
	if got, _ := row["claimed_by_email"].(string); got != "claimer@example.test" {
		t.Fatalf("claimed_by_email after withdraw = %q, want the full address", got)
	}
}

func TestTokenBankWithdrawRequiresRequestIDAndIsIdempotent(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "withdrawer@example.test")
	env.seedCredits(t, user.ID, 10)
	env.linkHub(t, "hub-1", user.Email)

	// No request_id: refuse rather than mint a non-replayable debit.
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"amount_micro": 2_000_000, "hub_id": "hub-1"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status without request_id = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}

	body := map[string]any{"request_id": "withdraw:hub-1:1", "amount_micro": 2_000_000, "hub_id": "hub-1", "manual": true}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	first := decodeMap(t, rec)
	if first["created"] != true {
		t.Fatalf("created = %v on the first call, want true", first["created"])
	}
	firstID, _ := first["withdrawal_id"].(string)

	// The replay must return the same record and must not debit twice. This is
	// the whole reason the hub sends an idempotency key.
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", rec.Code, rec.Body.String())
	}
	second := decodeMap(t, rec)
	if second["created"] != false {
		t.Fatalf("created = %v on the replay, want false", second["created"])
	}
	if second["withdrawal_id"] != firstID {
		t.Fatalf("replay withdrawal_id = %v, want %v", second["withdrawal_id"], firstID)
	}

	balance, err := env.repo.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance.WithdrawnMicro != 2_000_000 {
		t.Fatalf("withdrawn_micro = %d after a replay, want 2e6", balance.WithdrawnMicro)
	}
	if balance.AvailableMicro() != 8_000_000 {
		t.Fatalf("available_micro = %d, want 8e6", balance.AvailableMicro())
	}
}

func TestTokenBankWithdrawReplayRejectsAnotherAccount(t *testing.T) {
	env := newTokenBankTestEnv(t)
	owner, ownerToken := env.createUser(t, "withdrawer@example.test")
	other, otherToken := env.createUser(t, "other@example.test")
	env.seedCredits(t, owner.ID, 10)
	env.seedCredits(t, other.ID, 10)
	env.linkHub(t, "hub-1", owner.Email)
	env.linkHub(t, "hub-1", other.Email)

	body := map[string]any{"request_id": "withdraw:hub-1:1", "amount_micro": 2_000_000, "hub_id": "hub-1", "manual": true}
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", ownerToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("owner status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", otherToken, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("other account status = %d, body %s", rec.Code, rec.Body.String())
	}
	if decodeMap(t, rec)["code"] != "withdrawal_mismatch" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	ownerBal, err := env.repo.AvailableMicro(context.Background(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherBal, err := env.repo.AvailableMicro(context.Background(), other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ownerBal != 8_000_000 || otherBal != 10_000_000 {
		t.Fatalf("owner available = %d, other available = %d", ownerBal, otherBal)
	}
}

func TestTokenBankWithdrawCannotExceedAvailable(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "overspend@example.test")
	env.seedCredits(t, user.ID, 3)
	env.linkHub(t, "hub-9", user.Email)

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:hub-9:1", "amount_micro": 9_000_000, "hub_id": "hub-9", "manual": true})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (body %s)", rec.Code, rec.Body.String())
	}
	balance, _ := env.repo.Balance(context.Background(), user.ID)
	if balance.WithdrawnMicro != 0 {
		t.Fatalf("withdrawn_micro = %d after a rejected withdrawal, want 0", balance.WithdrawnMicro)
	}
}

func TestTokenBankWithdrawRefusesFrozenCredits(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "frozen@example.test")
	env.seedCredits(t, user.ID, 10)
	env.linkHub(t, "h", user.Email)

	// Freeze 6 of the 10 credits. 4 remain spendable; the withdrawal must see
	// the freeze even though it was recorded by a different endpoint (C1).
	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	if rec.Code != http.StatusCreated {
		t.Fatalf("gift create status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:frozen:1", "amount_micro": 6_000_000, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402 (body %s)", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:frozen:2", "amount_micro": 4_000_000, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw of the unfrozen remainder status = %d, body %s", rec.Code, rec.Body.String())
	}
	balance, _ := env.repo.Balance(context.Background(), user.ID)
	if got := balance.EarnedMicro + balance.ReceivedMicro - balance.WithdrawnMicro - balance.GrantedMicro - balance.FrozenMicro; got != 1_000_000 {
		t.Fatalf("available = %d, want 1e6 (10 earned - 5 frozen - 4 withdrawn)", got)
	}
}

func TestTokenBankWithdrawAcceptsWholeCreditAmount(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "whole-credits@example.test")
	env.seedCredits(t, user.ID, 10)
	env.linkHub(t, "h", user.Email)

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:whole:1", "amount": 3, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["amount_micro"].(float64); got != 3_000_000 {
		t.Fatalf("amount_micro = %v, want 3e6", got)
	}
	// Sending both units is a caller bug: it is ambiguous which one wins.
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:whole:2", "amount": 3, "amount_micro": 3, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status with both units = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankWithdrawalsAreListedForTheCallerOnly(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "lister@example.test")
	other, otherToken := env.createUser(t, "other-lister@example.test")
	env.seedCredits(t, user.ID, 10)
	env.seedCredits(t, other.ID, 10)
	env.linkHub(t, "hub-a", user.Email)
	env.linkHub(t, "hub-b", other.Email)

	env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:list:m", "amount_micro": 1_000_000, "hub_id": "hub-a", "manual": true})
	env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", otherToken,
		map[string]any{"request_id": "withdraw:list:o", "amount_micro": 1_000_000, "hub_id": "hub-b", "manual": true})

	rec := env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeMap(t, rec)["withdrawals"].([]any)
	if len(items) != 1 {
		t.Fatalf("withdrawals length = %d, want 1", len(items))
	}
	if items[0].(map[string]any)["request_id"] != "withdraw:list:m" {
		t.Fatalf("returned another user's withdrawal: %v", items[0])
	}
	if items[0].(map[string]any)["automatic"] != false {
		t.Fatalf("manual withdrawal marked automatic: %v", items[0])
	}

	// hub_id narrows the list, which is what a hub uses to reconcile its own
	// grants without seeing the user's other hubs.
	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals?hub_id=hub-zzz", token, nil)
	items, _ = decodeMap(t, rec)["withdrawals"].([]any)
	if len(items) != 0 {
		t.Fatalf("filtered withdrawals length = %d, want 0", len(items))
	}
}

func TestTokenBankListKeepsUnboundGiftPastTheHistoryPage(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "unbound-sender@example.test")
	receiver, receiverToken := env.createUser(t, "unbound-receiver@example.test")
	env.seedCredits(t, sender.ID, 20)
	env.seedCredits(t, receiver.ID, 20)
	env.linkHub(t, "hub-1", receiver.Email)

	create := func(credits int) (string, string) {
		t.Helper()
		rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": credits})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
		}
		body := decodeMap(t, rec)
		return body["id"].(string), body["code"].(string)
	}
	claim := func(code string) {
		t.Helper()
		rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", receiverToken, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
		}
	}
	withdrawGift := func(requestID, linkID string) {
		t.Helper()
		rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
			"request_id": requestID, "amount_micro": 1, "hub_id": "hub-1", "manual": true,
			"kind": "gift", "link_id": linkID,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("gift withdraw %s status = %d, body %s", requestID, rec.Code, rec.Body.String())
		}
	}
	boundID, boundCode := create(2)
	claim(boundCode)
	withdrawGift("gift-bound", boundID)
	if err := env.repo.BindGrantID(context.Background(), "gift-bound", "hub-1", "grant-1"); err != nil {
		t.Fatalf("BindGrantID() error = %v", err)
	}
	openID, openCode := create(3)
	claim(openCode)
	withdrawGift("gift-open", openID)
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
		"request_id": "self-new", "amount_micro": 1_000_000, "hub_id": "hub-1", "manual": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("self withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	stamp := func(requestID, at string) {
		t.Helper()
		if _, err := env.provider.Write.ExecContext(context.Background(),
			`UPDATE token_bank_withdrawals SET created_at = ? WHERE request_id = ?`, at, requestID); err != nil {
			t.Fatalf("stamp %s: %v", requestID, err)
		}
	}
	stamp("gift-bound", "2026-01-01T00:00:00Z")
	stamp("gift-open", "2026-01-02T00:00:00Z")
	stamp("self-new", "2026-01-03T00:00:00Z")

	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals?limit=1", receiverToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeMap(t, rec)["withdrawals"].([]any)
	got := map[string]bool{}
	for _, item := range items {
		row := item.(map[string]any)
		got[row["request_id"].(string)] = true
	}
	if len(items) != 2 || !got["self-new"] || !got["gift-open"] || got["gift-bound"] {
		t.Fatalf("withdrawals = %v, want self-new and gift-open only", items)
	}
	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals?limit=1&hub_id=hub-zzz", receiverToken, nil)
	items, _ = decodeMap(t, rec)["withdrawals"].([]any)
	if len(items) != 0 {
		t.Fatalf("other hub withdrawals = %v, want none", items)
	}
}

func TestTokenBankListIncludesLedgerWithdrawalsWithoutALocalRow(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "orphan-lister@example.test")
	env.seedCredits(t, user.ID, 20)
	env.linkHub(t, "hub-a", user.Email)

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:list:local", "amount_micro": 1_000_000, "hub_id": "hub-a", "manual": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	requestID := "tbk-auto:hub-b:orphan-lister@example.test:paid:4"
	if _, err := env.repo.AppendLedger(context.Background(), sqlite.TokenBankLedgerEntry{
		ID:          "wdledger-auto",
		UserID:      user.ID,
		Bucket:      sqlite.TokenBankBucketWithdrawn,
		AmountMicro: 2_500_000,
		BizKey:      "withdraw:" + requestID,
		RefType:     "withdrawal",
		RefID:       requestID,
		Note:        "hub-b",
		CreatedAt:   time.Date(2026, 10, 4, 2, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("AppendLedger() error = %v", err)
	}
	if _, err := env.repo.AppendAdjustment(context.Background(), user.ID, sqlite.TokenBankBucketWithdrawn, 500_000, "adjust:reconcile:orphan", "not a withdrawal"); err != nil {
		t.Fatalf("AppendAdjustment() error = %v", err)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeMap(t, rec)["withdrawals"].([]any)
	if len(items) != 2 {
		t.Fatalf("withdrawals = %v, want the local row and the ledger debit", items)
	}
	var posted map[string]any
	local := 0
	for _, item := range items {
		row := item.(map[string]any)
		switch row["request_id"] {
		case "withdraw:list:local":
			local++
		case requestID:
			posted = row
		default:
			t.Fatalf("unexpected withdrawal %v", row)
		}
	}
	if local != 1 || posted == nil {
		t.Fatalf("withdrawals = %v", items)
	}
	if posted["state"] != "posted" || posted["automatic"] != true || posted["hub_id"] != "hub-b" || posted["amount_micro"].(float64) != 2_500_000 || posted["kind"] != "self" || posted["grant_id"] != "" {
		t.Fatalf("posted withdrawal = %v", posted)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/credits/withdrawals?hub_id=hub-b", token, nil)
	items, _ = decodeMap(t, rec)["withdrawals"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["request_id"] != requestID {
		t.Fatalf("hub-b withdrawals = %v, want the ledger debit only", items)
	}
}

func TestTokenBankGiftNotFoundReturns404(t *testing.T) {
	env := newTokenBankTestEnv(t)
	_, token := env.createUser(t, "404@example.test")

	rec := env.do(t, http.MethodGet, "/api/v1/credits/share-links/NOSUCHCODE/preview", "", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("preview status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/NOSUCHCODE/claim", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("claim status = %d, want 404 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankEndpointsReportUnavailableWithoutRepo(t *testing.T) {
	// A deployment that never initialised the LLM module has no ledger. Every
	// endpoint must answer 503 rather than dereferencing a nil repository.
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open memory sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	smStore, err := skillmarket.NewStore(db, db)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	h := NewSkillMarketHandlers(SkillMarketConfig{Store: smStore, AuthSvc: skillmarket.NewAuthService(smStore, nil, "")})
	env := &tokenBankTestEnv{handlers: h}

	// The preview endpoint needs no session, so it reaches the repo check.
	rec := env.do(t, http.MethodGet, "/api/v1/credits/share-links/NOPE/preview", "", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("preview status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
	// Session-bound endpoints answer 401 first. That ordering is intentional:
	// an unauthenticated caller should not be able to probe whether this node
	// has the Token Bank enabled. Pin it so a future reorder is a deliberate
	// decision rather than an accident.
	rec = env.do(t, http.MethodGet, "/api/v1/token-bank/summary", "whatever", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("summary status = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankMaskEmail(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"a@b.com", "*@b.com"},
		{"alice@example.test", "a***@example.test"},
		{"ALICE@Example.Test", "A***@Example.Test"},
		{"noatsign", "n***"},
		{"x", "*"},
	}
	for _, tc := range cases {
		if got := maskEmail(tc.in); got != tc.want {
			t.Errorf("maskEmail(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestTokenBankListLimitIsClamped(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", tokenBankDefaultListLimit},
		{"?limit=10", 10},
		{"?limit=0", tokenBankDefaultListLimit},
		{"?limit=-5", tokenBankDefaultListLimit},
		{"?limit=abc", tokenBankDefaultListLimit},
		{"?limit=99999", tokenBankMaxListLimit},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x"+tc.query, nil)
		if got := tokenBankListLimit(req); got != tc.want {
			t.Errorf("tokenBankListLimit(%q) = %d, want %d", tc.query, got, tc.want)
		}
	}
}

func TestTokenBankUnwiredRepoAccessorIsNil(t *testing.T) {
	var h *SkillMarketHandlers
	if h.tokenBankRepo() != nil {
		t.Fatalf("nil handler returned a repo")
	}
	h = &SkillMarketHandlers{}
	h.SetTokenBankRepo(nil, "  ")
	if h.tokenBankRepo() != nil {
		t.Fatalf("nil repo was stored")
	}
	if h.tokenBankNodeID() != "" {
		t.Fatalf("node id = %q, want empty", h.tokenBankNodeID())
	}
}

func TestTokenBankGiftExpiryIsReportedNotClaimed(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "expiring@example.test")
	_, claimerToken := env.createUser(t, "late@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token, map[string]any{"credits": 5})
	code := decodeMap(t, rec)["code"].(string)

	// Backdate the expiry rather than sleeping 7 days.
	if _, err := env.provider.Write.ExecContext(context.Background(),
		`UPDATE credit_share_links SET expires_at = ? WHERE code = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), code); err != nil {
		t.Fatalf("backdate expiry: %v", err)
	}

	rec = env.do(t, http.MethodGet, "/api/v1/credits/share-links/"+code+"/preview", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d, body %s", rec.Code, rec.Body.String())
	}
	preview := decodeMap(t, rec)
	if preview["claimable"] != false {
		t.Fatalf("claimable = %v for an expired link, want false", preview["claimable"])
	}
	if preview["remaining_seconds"].(float64) != 0 {
		t.Fatalf("remaining_seconds = %v, want 0", preview["remaining_seconds"])
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", claimerToken, nil)
	if rec.Code != http.StatusGone {
		t.Fatalf("claim status = %d, want 410 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestTokenBankWithdrawZeroAmountTakesTheWholeManualBalance pins the "as much
// as allowed" request. The handler used to reject amount_micro == 0 outright,
// which made the automatic top-up path unreachable over HTTP: an automatic
// caller has no figure to name, it just wants its share.
func TestTokenBankWithdrawZeroAmountTakesTheWholeManualBalance(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "zero-amount@example.test")
	env.seedCredits(t, user.ID, 7)
	env.linkHub(t, "h", user.Email)

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:zero:1", "hub_id": "h", "manual": true})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if got := decodeMap(t, rec)["amount_micro"].(float64); got != 7_000_000 {
		t.Fatalf("amount_micro = %v, want 7e6 (omitting the amount takes everything allowed)", got)
	}
}

// TestTokenBankManualWithdrawRequiresLinkedHub pins the grant authorization.
// A manual withdrawal that names a hub is what "提取到本机" replays into a
// grant. Debiting before the link exists withdraws credits the hub can never
// pick up.
func TestTokenBankManualWithdrawRequiresLinkedHub(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "unlinked@example.test")
	env.seedCredits(t, user.ID, 6)

	body := map[string]any{"request_id": "withdraw:unlinked:1", "amount_micro": 6_000_000, "hub_id": "hub-x", "manual": true}
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if decodeMap(t, rec)["code"] != "hub_not_linked" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 6_000_000 {
		t.Fatalf("available = %d, want the balance untouched", available)
	}

	env.linkHub(t, "hub-x", user.Email)
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("linked status = %d, body %s", rec.Code, rec.Body.String())
	}
	if decodeMap(t, rec)["created"] != true || decodeMap(t, rec)["amount_micro"].(float64) != 6_000_000 {
		t.Fatalf("linked body = %s", rec.Body.String())
	}
}

// TestTokenBankWithdrawDefaultsToCappedMode pins the safe default. The mode is
// not inferable from the request, so a client that does not send `manual` must
// get the capped behaviour — otherwise any caller could drain a balance that
// belongs to hubs it does not own (E6).
func TestTokenBankWithdrawDefaultsToCappedMode(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "default-mode@example.test")
	env.seedCredits(t, user.ID, 10)

	// countUserHubs degrades to 1 with no hub links, so the cap is the whole
	// balance and the request succeeds — but it must be the cap that decided,
	// not an unconditional "take everything". Asking for more than the cap is
	// refused, which proves the cap is in force.
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:default:1", "amount_micro": 11_000_000, "hub_id": "h"})
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402: the capped default must refuse an over-cap amount (body %s)",
			rec.Code, rec.Body.String())
	}

	// And the in-cap request succeeds, so the default mode is usable.
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:default:2", "amount_micro": 10_000_000, "hub_id": "h"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
}

// TestTokenBankWithdrawRejectsNegativeAmount is the one input the zero-means-all
// rule must not swallow: a negative figure is a caller bug, not a request for
// everything.
func TestTokenBankWithdrawRejectsNegativeAmount(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "negative@example.test")
	env.seedCredits(t, user.ID, 5)

	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:neg:1", "amount_micro": -1_000_000, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token,
		map[string]any{"request_id": "withdraw:neg:2", "amount": -3, "hub_id": "h", "manual": true})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("negative whole-credit status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	balance, _ := env.repo.Balance(context.Background(), user.ID)
	if balance.WithdrawnMicro != 0 {
		t.Fatalf("withdrawn_micro = %d after rejected withdrawals, want 0", balance.WithdrawnMicro)
	}
}

// TestCreditsToMicroRefusesOverflow pins the conversion guard. The multiply runs
// on a user-supplied int64, so without the check "share 10^13 credits" wraps to
// a negative number — which the store would then reject as a non-positive
// amount, i.e. the user gets "invalid amount" for a value that is merely too
// large, or worse, a wrong positive figure.
func TestCreditsToMicroRefusesOverflow(t *testing.T) {
	cases := []struct {
		credits int64
		want    int64
		ok      bool
	}{
		{0, 0, false},
		{-1, 0, false},
		{1, 1_000_000, true},
		// math.MaxInt64 / 1e6 == 9223372036854, so that is the largest
		// whole-credit figure that survives the multiply.
		{9223372036854, 9223372036854000000, true},
		{9223372036855, 0, false}, // one past it
		{math.MaxInt64, 0, false},
	}
	for _, tc := range cases {
		got, ok := creditsToMicro(tc.credits)
		if ok != tc.ok {
			t.Errorf("creditsToMicro(%d) ok = %v, want %v", tc.credits, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("creditsToMicro(%d) = %d, want %d", tc.credits, got, tc.want)
		}
	}
}

func TestTokenBankGiftRejectsBothUnitsAndOverflow(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "gift-units@example.test")
	env.seedCredits(t, user.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", token,
		map[string]any{"credits": 5, "credits_micro": 5_000_000})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("both-units status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links", token,
		map[string]any{"credits": 9223372036855})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overflow status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// Neither attempt may have frozen anything.
	balance, err := env.repo.Balance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if balance.FrozenMicro != 0 {
		t.Fatalf("frozen_micro = %d after rejected gift requests, want 0", balance.FrozenMicro)
	}
}
