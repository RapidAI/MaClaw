package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
)

type tokenBankHubAuthStub struct {
	err error
}

func (s tokenBankHubAuthStub) VerifyHubSecret(context.Context, string, string) error {
	return s.err
}

type tokenBankGroupStub struct {
	hosts bool
}

func (s tokenBankGroupStub) ServiceGroupHostsTokenBank(context.Context, string) (bool, error) {
	return s.hosts, nil
}

func TestTokenBankHubWithdrawRejectsGroupWithoutTokenBank(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, _ := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 5)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: false}
	env.linkHub(t, "hub-1", user.Email)

	rec := env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-1", "service_group_id": "paid", "amount_micro": 1_000_000, "manual": true,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 5_000_000 {
		t.Fatalf("available = %d, want the balance untouched", available)
	}
}

func TestTokenBankHubWithdrawDebitsOnceAndBindsGrant(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, _ := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 4)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}
	env.linkHub(t, "hub-1", user.Email)
	auth := tokenBankHubAuthStub{}

	first := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-1", "service_group_id": "paid", "manual": true,
	})
	if first.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d, body %s", first.Code, first.Body.String())
	}
	body := decodeMap(t, first)
	if body["created"] != true {
		t.Fatalf("created = %v, want true", body["created"])
	}
	amount, _ := body["amount_micro"].(float64)
	if amount != 4_000_000 {
		t.Fatalf("amount = %v, want 4000000", body["amount_micro"])
	}
	second := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-1", "service_group_id": "paid", "manual": true,
	})
	if second.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", second.Code, second.Body.String())
	}
	replay := decodeMap(t, second)
	if replay["created"] != false || replay["amount_micro"].(float64) != 4_000_000 {
		t.Fatalf("replay = %v", replay)
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 0 {
		t.Fatalf("available after replay = %d, want 0", available)
	}
	bind := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/grants", map[string]any{
		"request_id": "req-1", "grant_id": "grant-1",
	})
	if bind.Code != http.StatusOK {
		t.Fatalf("bind status = %d, body %s", bind.Code, bind.Body.String())
	}
	again := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/grants", map[string]any{
		"request_id": "req-1", "grant_id": "grant-1",
	})
	if again.Code != http.StatusOK {
		t.Fatalf("bind replay status = %d, body %s", again.Code, again.Body.String())
	}
	sum, err := env.repo.SumWithdrawalMicro(context.Background(), user.ID, "hub-1")
	if err != nil {
		t.Fatal(err)
	}
	if sum != 4_000_000 {
		t.Fatalf("hub sum = %d, want 4000000", sum)
	}
}

func TestTokenBankHubWithdrawGiftSettlesBeforeDebit(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "hub-gift-sender@example.test")
	receiver, _ := env.createUser(t, "hub-gift-receiver@example.test")
	env.seedCredits(t, sender.ID, 10)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}
	env.linkHub(t, "hub-1", receiver.Email)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 4})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decodeMap(t, rec)
	code := created["code"].(string)
	linkID := created["id"].(string)
	session, err := env.auth.CreateSessionForUser(context.Background(), receiver.ID, receiver.Email)
	if err != nil {
		t.Fatal(err)
	}
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", session.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-2/token-bank/withdraw", map[string]any{
		"email": receiver.Email, "request_id": "gift-hub", "service_group_id": "paid",
		"amount_micro": 4_000_000, "manual": true, "kind": "gift", "link_id": linkID,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unlinked gift withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	senderBal, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if senderBal.FrozenMicro != 4_000_000 || senderBal.GrantedMicro != 0 {
		t.Fatalf("unlinked pull moved the gift: frozen=%d granted=%d", senderBal.FrozenMicro, senderBal.GrantedMicro)
	}

	rec = env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": receiver.Email, "request_id": "gift-hub", "service_group_id": "paid",
		"amount_micro": 4_000_000, "manual": true, "kind": "gift", "link_id": linkID,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("hub gift withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	senderBal, err = env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if senderBal.FrozenMicro != 0 || senderBal.GrantedMicro != 4_000_000 {
		t.Fatalf("sender frozen/granted = %d/%d, want 0/4000000", senderBal.FrozenMicro, senderBal.GrantedMicro)
	}
	sum, err := env.repo.SumWithdrawalMicro(context.Background(), receiver.ID, "hub-1")
	if err != nil {
		t.Fatal(err)
	}
	if sum != 4_000_000 {
		t.Fatalf("receiver hub withdrawal = %d, want 4000000", sum)
	}
	again := env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": receiver.Email, "request_id": "gift-hub-again", "service_group_id": "paid",
		"amount_micro": 4_000_000, "manual": true, "kind": "gift", "link_id": linkID,
	})
	if again.Code != http.StatusConflict {
		t.Fatalf("second gift withdraw status = %d, body %s", again.Code, again.Body.String())
	}
	sum, err = env.repo.SumWithdrawalMicro(context.Background(), receiver.ID, "hub-1")
	if err != nil {
		t.Fatal(err)
	}
	if sum != 4_000_000 {
		t.Fatalf("receiver hub withdrawal after second request = %d, want 4000000", sum)
	}
}

func TestTokenBankHubWithdrawRejectsBadSecretWithoutDebit(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, _ := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 2)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}
	rec := env.hubPull(t, tokenBankHubAuthStub{err: hubs.ErrHubUnauthorized}, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-1", "service_group_id": "paid", "manual": true,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 2_000_000 {
		t.Fatalf("available = %d, want untouched", available)
	}
}

func TestTokenBankHubWithdrawRejectsUnlinkedAccount(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, _ := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 5)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}

	rec := env.hubPull(t, tokenBankHubAuthStub{}, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-unlinked", "service_group_id": "paid", "amount_micro": 1_000_000,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if decodeMap(t, rec)["code"] != "hub_not_linked" {
		t.Fatalf("body = %s", rec.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 5_000_000 {
		t.Fatalf("available = %d, want untouched", available)
	}
}

func TestTokenBankHubWithdrawReplayStaysOnTheSameHub(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, _ := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 4)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}
	env.linkHub(t, "hub-1", user.Email)
	env.linkHub(t, "hub-2", user.Email)
	auth := tokenBankHubAuthStub{}

	first := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-shared", "service_group_id": "paid",
	})
	if first.Code != http.StatusOK {
		t.Fatalf("first status = %d, body %s", first.Code, first.Body.String())
	}
	// Two linked hubs: the automatic cap is half of 4 credits.
	if decodeMap(t, first)["amount_micro"].(float64) != 2_000_000 {
		t.Fatalf("first amount = %s", first.Body.String())
	}

	replay := env.hubPull(t, auth, "/api/hubs/hub-2/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-shared", "service_group_id": "paid",
	})
	if replay.Code != http.StatusConflict {
		t.Fatalf("cross-hub replay status = %d, body %s", replay.Code, replay.Body.String())
	}
	if decodeMap(t, replay)["code"] != "withdrawal_mismatch" {
		t.Fatalf("replay body = %s", replay.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 2_000_000 {
		t.Fatalf("available after cross-hub replay = %d, want 2000000", available)
	}

	bind := env.hubPull(t, auth, "/api/hubs/hub-2/token-bank/grants", map[string]any{
		"request_id": "req-shared", "grant_id": "grant-stolen",
	})
	if bind.Code != http.StatusNotFound {
		t.Fatalf("cross-hub bind status = %d, body %s", bind.Code, bind.Body.String())
	}

	second := env.hubPull(t, auth, "/api/hubs/hub-2/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-hub-2", "service_group_id": "paid",
	})
	if second.Code != http.StatusOK {
		t.Fatalf("second hub status = %d, body %s", second.Code, second.Body.String())
	}
	if decodeMap(t, second)["created"] != true || decodeMap(t, second)["amount_micro"].(float64) != 1_000_000 {
		t.Fatalf("second hub body = %s", second.Body.String())
	}
}

func TestTokenBankManualSessionLetsTheHubReplayTheWholeBalance(t *testing.T) {
	env := newTokenBankTestEnv(t)
	user, token := env.createUser(t, "owner@example.com")
	env.seedCredits(t, user.ID, 4)
	env.handlers.tokenBankGroups = tokenBankGroupStub{hosts: true}
	env.linkHub(t, "hub-1", user.Email)
	env.linkHub(t, "hub-2", user.Email)
	auth := tokenBankHubAuthStub{}

	// The hub secret still cannot lift the cap, even when the body says manual.
	refused := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-all", "service_group_id": "paid", "amount_micro": 4_000_000, "manual": true,
	})
	if refused.Code != http.StatusPaymentRequired {
		t.Fatalf("capped status = %d, body %s", refused.Code, refused.Body.String())
	}
	available, err := env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 4_000_000 {
		t.Fatalf("available after capped pull = %d, want 4000000", available)
	}

	// The signed-in user authorizes the whole balance for this hub.
	rec := env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", token, map[string]any{
		"request_id": "req-all", "amount_micro": 4_000_000, "hub_id": "hub-1", "manual": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("session status = %d, body %s", rec.Code, rec.Body.String())
	}
	if decodeMap(t, rec)["created"] != true || decodeMap(t, rec)["amount_micro"].(float64) != 4_000_000 {
		t.Fatalf("session body = %s", rec.Body.String())
	}

	// The same request id replays onto the grant. It does not debit again.
	replay := env.hubPull(t, auth, "/api/hubs/hub-1/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-all", "service_group_id": "paid", "amount_micro": 4_000_000,
	})
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", replay.Code, replay.Body.String())
	}
	replayed := decodeMap(t, replay)
	if replayed["created"] != false || replayed["amount_micro"].(float64) != 4_000_000 {
		t.Fatalf("replay body = %s", replay.Body.String())
	}
	available, err = env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 0 {
		t.Fatalf("available after replay = %d, want 0", available)
	}

	other := env.hubPull(t, auth, "/api/hubs/hub-2/token-bank/withdraw", map[string]any{
		"email": user.Email, "request_id": "req-all", "service_group_id": "paid", "amount_micro": 4_000_000,
	})
	if other.Code != http.StatusConflict {
		t.Fatalf("other hub status = %d, body %s", other.Code, other.Body.String())
	}
	available, err = env.repo.AvailableMicro(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if available != 0 {
		t.Fatalf("available after other hub = %d, want 0", available)
	}
}

func (e *tokenBankTestEnv) linkHub(t *testing.T, hubID, email string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := e.provider.Write.ExecContext(context.Background(),
		`INSERT INTO hub_user_links (id, hub_id, email, is_default, created_at, updated_at) VALUES (?, ?, ?, 1, ?, ?)`,
		"link-"+hubID+"-"+email, hubID, email, now, now); err != nil {
		t.Fatalf("link hub %s to %s: %v", hubID, email, err)
	}
}

func (e *tokenBankTestEnv) hubPull(t *testing.T, auth hubSecretVerifier, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h := e.handlers
	mux.HandleFunc("POST /api/hubs/{id}/token-bank/withdraw", h.TokenBankHubWithdraw(auth))
	mux.HandleFunc("POST /api/hubs/{id}/token-bank/grants", h.TokenBankHubBindGrant(auth))
	mux.HandleFunc("POST /api/hubs/{id}/token-bank/reconcile", h.TokenBankHubReconcile(auth))
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer hub-secret")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
