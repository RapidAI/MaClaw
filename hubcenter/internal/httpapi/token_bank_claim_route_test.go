package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type claimOriginStub struct {
	url       string
	reachable bool
	secret    string
	hits      int
}

func (s *claimOriginStub) AccessPeer(string) (string, bool, int64) {
	s.hits++
	return s.url, s.reachable, 1
}

func (s *claimOriginStub) ClusterSecret() string { return s.secret }

func (e *tokenBankTestEnv) claim(t *testing.T, h *SkillMarketHandlers, code, token string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", nil)
	req.SetPathValue("code", code)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	rec := httptest.NewRecorder()
	h.TokenBankClaimGiftLink(rec, req)
	return rec
}

func TestTokenBankGiftWithdrawSettlesThenDebitsReceiver(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "gift-sender@example.test")
	receiver, receiverToken := env.createUser(t, "gift-receiver@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decodeMap(t, rec)
	linkID := created["id"].(string)
	code := created["code"].(string)

	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
		"request_id": "gift-early", "amount_micro": 5_000_000, "manual": true, "kind": "gift", "link_id": linkID,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("unclaimed withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", receiverToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}

	_, strangerToken := env.createUser(t, "gift-stranger@example.test")
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", strangerToken, map[string]any{
		"request_id": "gift-stranger", "amount_micro": 5_000_000, "manual": true, "kind": "gift", "link_id": linkID,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}

	env.linkHub(t, "hub-1", receiver.Email)
	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
		"request_id": "gift-wd", "amount_micro": 5_000_000, "manual": true, "kind": "gift", "link_id": linkID, "hub_id": "hub-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("withdraw status = %d, body %s", rec.Code, rec.Body.String())
	}
	if createdFlag, _ := decodeMap(t, rec)["created"].(bool); !createdFlag {
		t.Fatal("first gift withdraw created = false, want true")
	}

	senderBal, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if senderBal.FrozenMicro != 0 || senderBal.GrantedMicro != 5_000_000 {
		t.Fatalf("sender frozen/granted = %d/%d, want 0/5000000", senderBal.FrozenMicro, senderBal.GrantedMicro)
	}
	receiverBal, err := env.repo.Balance(context.Background(), receiver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if receiverBal.ReceivedMicro != 5_000_000 || receiverBal.WithdrawnMicro != 5_000_000 {
		t.Fatalf("receiver received/withdrawn = %d/%d, want 5000000/5000000", receiverBal.ReceivedMicro, receiverBal.WithdrawnMicro)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
		"request_id": "gift-wd", "amount_micro": 5_000_000, "manual": true, "kind": "gift", "link_id": linkID, "hub_id": "hub-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d, body %s", rec.Code, rec.Body.String())
	}
	if createdFlag, _ := decodeMap(t, rec)["created"].(bool); createdFlag {
		t.Fatal("replay created = true, want false")
	}
	senderBal, _ = env.repo.Balance(context.Background(), sender.ID)
	if senderBal.GrantedMicro != 5_000_000 {
		t.Fatalf("sender granted after replay = %d, want 5000000", senderBal.GrantedMicro)
	}
}

func TestTokenBankManualGiftWithdrawRequiresLinkBeforeSettle(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "gift-unlink-sender@example.test")
	receiver, receiverToken := env.createUser(t, "gift-unlink-receiver@example.test")
	env.seedCredits(t, sender.ID, 10)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 5})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decodeMap(t, rec)
	linkID := created["id"].(string)
	code := created["code"].(string)
	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links/"+code+"/claim", receiverToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, body %s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, "/api/v1/token-bank/credits/withdraw", receiverToken, map[string]any{
		"request_id": "gift-unlink", "amount_micro": 5_000_000, "manual": true, "kind": "gift", "link_id": linkID, "hub_id": "hub-x",
	})
	if rec.Code != http.StatusForbidden || decodeMap(t, rec)["code"] != "hub_not_linked" {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	senderBal, err := env.repo.Balance(context.Background(), sender.ID)
	if err != nil {
		t.Fatal(err)
	}
	if senderBal.FrozenMicro != 5_000_000 || senderBal.GrantedMicro != 0 {
		t.Fatalf("sender frozen/granted = %d/%d, want 5000000/0", senderBal.FrozenMicro, senderBal.GrantedMicro)
	}
	receiverBal, err := env.repo.Balance(context.Background(), receiver.ID)
	if err != nil {
		t.Fatal(err)
	}
	if receiverBal.ReceivedMicro != 0 || receiverBal.WithdrawnMicro != 0 {
		t.Fatalf("receiver received/withdrawn = %d/%d, want 0/0", receiverBal.ReceivedMicro, receiverBal.WithdrawnMicro)
	}
}

func TestTokenBankClaimRoutesToOriginAndRefusesLocalFallback(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, senderToken := env.createUser(t, "origin-sender@example.test")
	_, receiverToken := env.createUser(t, "origin-receiver@example.test")
	env.seedCredits(t, sender.ID, 20)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/credits/share-links/{code}/claim", env.handlers.TokenBankClaimGiftLink)
	originSrv := httptest.NewServer(mux)
	t.Cleanup(originSrv.Close)

	replica := NewSkillMarketHandlers(SkillMarketConfig{
		Store: env.handlers.store, UserSvc: env.users, AuthSvc: env.auth,
		TokenBank: env.repo, NodeID: "hc-replica",
	})
	stub := &claimOriginStub{url: originSrv.URL, reachable: true, secret: "peer-secret"}
	replica.SetTokenBankOrigin(stub)

	rec := env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 1})
	code := decodeMap(t, rec)["code"].(string)
	rec = env.claim(t, replica, code, receiverToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxied claim status = %d, body %s", rec.Code, rec.Body.String())
	}
	if stub.hits != 1 {
		t.Fatalf("origin lookups = %d, want 1", stub.hits)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 1})
	missCode := decodeMap(t, rec)["code"].(string)
	stub.reachable = false
	rec = env.claim(t, replica, missCode, receiverToken, map[string]string{tokenBankPeerHopHeader: "1"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("spoofed hop status = %d, body %s", rec.Code, rec.Body.String())
	}
	if msg, _ := decodeMap(t, rec)["message"].(string); msg != tokenBankClaimRetryMessage {
		t.Fatalf("message = %q", msg)
	}
	link, err := env.repo.GiftLinkByCode(context.Background(), missCode)
	if err != nil {
		t.Fatal(err)
	}
	if link.Status != sqlite.TokenBankGiftStatusActive {
		t.Fatalf("spoofed hop claimed locally, status = %s", link.Status)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 1})
	hopCode := decodeMap(t, rec)["code"].(string)
	before := stub.hits
	rec = env.claim(t, replica, hopCode, receiverToken, map[string]string{
		tokenBankPeerHopHeader:    "1",
		tokenBankPeerSecretHeader: "peer-secret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted hop status = %d, body %s", rec.Code, rec.Body.String())
	}
	if stub.hits != before {
		t.Fatalf("trusted hop looked up the peer (%d -> %d)", before, stub.hits)
	}

	rec = env.do(t, http.MethodPost, "/api/v1/credits/share-links", senderToken, map[string]any{"credits": 1})
	localCode := decodeMap(t, rec)["code"].(string)
	if _, err := env.provider.Write.ExecContext(context.Background(), `UPDATE credit_share_links SET origin_node_id = '' WHERE code = ?`, localCode); err != nil {
		t.Fatal(err)
	}
	stub.reachable = false
	_, otherToken := env.createUser(t, "origin-other@example.test")
	rec = env.claim(t, replica, localCode, otherToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty origin status = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestTokenBankGiftExpiryRunsWithoutWaitingForTheTicker(t *testing.T) {
	env := newTokenBankTestEnv(t)
	sender, _ := env.createUser(t, "expire-sender@example.test")
	env.seedCredits(t, sender.ID, 10)
	if _, err := env.repo.CreateGiftLink(context.Background(), sqlite.TokenBankGiftLink{
		ID: "link-exp", Code: "code-exp", SenderUserID: sender.ID, CreditsMicro: 1_000_000,
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}, sqlite.GiftLinkPolicy{}, time.Now().UTC().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunTokenBankGiftExpiry(ctx, env.repo, time.Hour)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		balance, err := env.repo.Balance(context.Background(), sender.ID)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if balance.FrozenMicro == 0 {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("frozen still %d", balance.FrozenMicro)
		}
		time.Sleep(15 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expiry loop did not stop")
	}
}
