package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

// shareTestEnv extends the Token Bank environment with a real registry, because
// the share endpoints do two things at once: they write the store row and they
// publish registry members. Testing only the first would miss the failure that
// matters — a share that is recorded but unreachable.
type shareTestEnv struct {
	*tokenBankTestEnv
	llmSvc   *llmservice.Service
	key      *rsa.PrivateKey
	settings *memSettings
}

func newShareTestEnv(t *testing.T) *shareTestEnv {
	t.Helper()
	base := newTokenBankTestEnv(t)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	settings := newMemSettings()
	llmSvc := llmservice.NewService(settings)
	ctx := context.Background()
	if err := llmSvc.EnsureTokenBankArrays(ctx); err != nil {
		t.Fatalf("ensure token bank arrays: %v", err)
	}
	if err := llmSvc.AddServiceGroup(ctx, llmpool.ServiceGroup{ID: "default", Name: "default", AccessPolicy: "free"}); err != nil {
		t.Fatalf("add service group: %v", err)
	}

	// Rebuild the handler set with the settings repository and the RSA key the
	// envelope decryptor needs.
	h := NewSkillMarketHandlers(SkillMarketConfig{
		Store:      base.handlers.store,
		UserSvc:    base.users,
		AuthSvc:    base.auth,
		Settings:   settings,
		RSAPrivKey: key,
	})
	h.SetTokenBankRepo(base.repo, "hc-test")
	h.SetTokenBankPublisher(llmSvc)
	base.handlers = h

	return &shareTestEnv{tokenBankTestEnv: base, llmSvc: llmSvc, key: key, settings: settings}
}

// wrapKeyEnvelope encrypts a Token Bank envelope the way the client does: a
// JSON document carrying the api key, sealed with the HubCenter public key.
func (e *shareTestEnv) wrapKeyEnvelope(t *testing.T, apiKey string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"api_key": apiKey, "protocol": "openai"})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	pkg, err := skillmarket.EncryptForDownload(payload, tokenBankEnvelopeSaltOwner, e.key)
	if err != nil {
		t.Fatalf("encrypt envelope: %v", err)
	}
	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal package: %v", err)
	}
	return string(raw)
}

func (e *shareTestEnv) submitShare(t *testing.T, token, fingerprint string, models ...string) *httptest.ResponseRecorder {
	t.Helper()
	items := make([]map[string]any, 0, len(models))
	for _, m := range models {
		items = append(items, map[string]any{"model": m, "available": true})
	}
	return e.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"client_instance_id": "client-1",
		"display_name":       "My OpenAI",
		"api_url":            "https://api.openai.example/v1",
		"protocol":           "openai",
		"service_group_id":   "default",
		"key_fingerprint":    fingerprint,
		"models":             items,
		"encrypted_payload":  e.wrapKeyEnvelope(t, "sk-plaintext-upstream"),
	})
}

// doWithMux routes share requests through the same table the real router uses.
func (e *shareTestEnv) doWithMux(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
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
	mux := http.NewServeMux()
	h := e.handlers
	mux.HandleFunc("POST /api/v1/token-bank/shares", h.TokenBankCreateShare)
	mux.HandleFunc("GET /api/v1/token-bank/shares", h.TokenBankListShares)
	mux.HandleFunc("GET /api/v1/token-bank/shares/{id}/models", h.TokenBankListShareModels)
	mux.HandleFunc("PUT /api/v1/token-bank/shares/{id}/models", h.TokenBankSyncShareModels)
	mux.HandleFunc("PUT /api/v1/token-bank/shares/{id}/paused", h.TokenBankSetSharePaused)
	mux.HandleFunc("PUT /api/v1/token-bank/shares/{id}/key", h.TokenBankRotateShareKey)
	mux.HandleFunc("PUT /api/v1/token-bank/shares/{id}/visibility", h.TokenBankSetShareVisibility)
	mux.HandleFunc("POST /api/v1/token-bank/shares/{id}/keys", h.TokenBankAddShareKey)
	mux.HandleFunc("DELETE /api/v1/token-bank/shares/{id}/keys/{fingerprint}", h.TokenBankRemoveShareKey)
	mux.HandleFunc("DELETE /api/v1/token-bank/shares/{id}", h.TokenBankTakeOutShare)
	mux.ServeHTTP(rec, req)
	return rec
}

func TestTokenBankSubmitSharePublishesReachableMembers(t *testing.T) {
	env := newShareTestEnv(t)
	user, token := env.createUser(t, "sharer@example.test")
	_ = user

	rec := env.submitShare(t, token, "fp-abc", "gpt-4o", "gpt-4o-mini")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeMap(t, rec)
	shareID, _ := body["id"].(string)
	if shareID == "" {
		t.Fatalf("response has no share id: %s", rec.Body.String())
	}
	// The storable envelope must come back, never the plaintext key.
	if _, leaked := body["api_key"]; leaked {
		t.Fatal("the response echoed an api_key field")
	}
	if hasKey, _ := body["has_key"].(bool); !hasKey {
		t.Fatal("has_key = false, want true")
	}

	// Both models must now exist as registry members, or the share is
	// published-but-unreachable.
	for _, model := range []string{"gpt-4o", "gpt-4o-mini"} {
		memberID := llmservice.TokenBankMemberID(shareID, model)
		provider, err := env.llmSvc.GetProvider(context.Background(), memberID)
		if err != nil {
			t.Fatalf("GetProvider(%s) error = %v", memberID, err)
		}
		if provider == nil {
			t.Fatalf("model %s was not published as a registry member", model)
		}
		if provider.APIKey != "sk-plaintext-upstream" {
			t.Fatalf("member %s did not receive the decrypted credential", memberID)
		}
	}
}

func TestTokenBankSubmitShareIsIdempotentOnFingerprint(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")

	first := env.submitShare(t, token, "fp-same", "gpt-4o")
	if first.Code != http.StatusCreated {
		t.Fatalf("first submit status = %d (body %s)", first.Code, first.Body.String())
	}
	second := env.submitShare(t, token, "fp-same", "gpt-4o")
	// A double-click, or a retry after a dropped response, must not create a
	// second share for the same key: 200 signals "already existed".
	if second.Code != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (body %s)", second.Code, second.Body.String())
	}
	firstID, _ := decodeMap(t, first)["id"].(string)
	secondID, _ := decodeMap(t, second)["id"].(string)
	if firstID != secondID {
		t.Fatalf("replay returned a different share: %s vs %s", firstID, secondID)
	}
}

func TestTokenBankSubmitShareRejectsUnverifiedAccount(t *testing.T) {
	env := newShareTestEnv(t)
	// An account that never verified. Publishing a credential is gated (§10
	// P0-8) precisely so this cannot happen.
	ctx := context.Background()
	user, err := env.users.EnsureAccount(ctx, "unverified@example.test")
	if err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	session, err := env.auth.CreateSessionForUser(ctx, user.ID, user.Email)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rec := env.submitShare(t, session.Token, "fp-unverified", "gpt-4o")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}
	if code, _ := decodeMap(t, rec)["code"].(string); code != "identity_not_verified" {
		t.Fatalf("code = %q, want identity_not_verified", code)
	}
}

func TestTokenBankSubmitShareRejectsBadEnvelope(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")

	rec := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"display_name":      "My OpenAI",
		"api_url":           "https://api.openai.example/v1",
		"key_fingerprint":   "fp-bad",
		"models":            []map[string]any{{"model": "gpt-4o", "available": true}},
		"encrypted_payload": "not-an-envelope",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
	// And nothing may have been recorded.
	shares, err := env.repo.ListShares(context.Background(), "", "", 10, 0)
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 0 {
		t.Fatalf("a rejected submit left %d share rows behind", len(shares))
	}
}

func TestTokenBankShareLifecyclePauseRotateTakeOut(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	ctx := context.Background()

	rec := env.submitShare(t, token, "fp-life", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")

	// Pause: the record and the registry must agree, or the share reads paused
	// while still receiving traffic.
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": true}); resp.Code != http.StatusOK {
		t.Fatalf("pause status = %d (body %s)", resp.Code, resp.Body.String())
	}
	provider, err := env.llmSvc.GetProvider(ctx, memberID)
	if err != nil || provider == nil {
		t.Fatalf("GetProvider after pause: provider=%v err=%v", provider, err)
	}
	if !provider.Paused {
		t.Fatal("the registry member was not paused with the share")
	}

	// Resume.
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": false}); resp.Code != http.StatusOK {
		t.Fatalf("resume status = %d", resp.Code)
	}
	provider, _ = env.llmSvc.GetProvider(ctx, memberID)
	if provider == nil || provider.Paused {
		t.Fatal("the registry member was not resumed")
	}

	// Rotate the key: identity must survive, credentials must change.
	rotate := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/key", token, map[string]any{
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-rotated-key"),
		"key_fingerprint":   "fp-life-v2",
	})
	if rotate.Code != http.StatusOK {
		t.Fatalf("rotate status = %d (body %s)", rotate.Code, rotate.Body.String())
	}
	provider, _ = env.llmSvc.GetProvider(ctx, memberID)
	if provider == nil || provider.APIKey != "sk-rotated-key" {
		t.Fatal("key rotation did not reach the registry member")
	}

	// Take out: the member must disappear entirely, not merely be paused.
	if resp := env.doWithMux(t, http.MethodDelete, "/api/v1/token-bank/shares/"+shareID, token, nil); resp.Code != http.StatusOK {
		t.Fatalf("take-out status = %d (body %s)", resp.Code, resp.Body.String())
	}
	if provider, _ := env.llmSvc.GetProvider(ctx, memberID); provider != nil {
		t.Fatal("taking out a share left its registry member behind")
	}
}

func TestTokenBankPausedShareStaysPausedWhenRepublished(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	ctx := context.Background()

	rec := env.submitShare(t, token, "fp-paused-replay", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": true}); resp.Code != http.StatusOK {
		t.Fatalf("pause status = %d (body %s)", resp.Code, resp.Body.String())
	}
	// The member is gone, as it is after a registry reset. The next publish
	// has to add it back, and the share is still paused.
	if _, err := env.llmSvc.UnpublishTokenBankShare(ctx, shareID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	replay := env.submitShare(t, token, "fp-paused-replay", "gpt-4o")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d (body %s)", replay.Code, replay.Body.String())
	}
	provider, err := env.llmSvc.GetProvider(ctx, memberID)
	if err != nil || provider == nil {
		t.Fatalf("provider after replay = %v err=%v", provider, err)
	}
	if !provider.Paused {
		t.Fatal("republish put a paused share back into traffic")
	}
}

// pauseStoreFailRepo fails the next store pause after the registry write, so
// the handler's revert can be observed.
type pauseStoreFailRepo struct {
	*sqlite.TokenBankRepo
	failNext bool
}

func (r *pauseStoreFailRepo) SetSharePaused(ctx context.Context, shareID, scopeOwner, reason string, paused bool, now time.Time) error {
	if r.failNext {
		r.failNext = false
		return errors.New("disk full")
	}
	return r.TokenBankRepo.SetSharePaused(ctx, shareID, scopeOwner, reason, paused, now)
}

func TestTokenBankPauseStoreFailureRestoresRegistry(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	ctx := context.Background()

	rec := env.submitShare(t, token, "fp-pause-revert", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	repo := &pauseStoreFailRepo{TokenBankRepo: env.repo, failNext: true}
	env.handlers.tokenBank = repo

	// Pause reaches the registry first. A store failure must put that member
	// back: an active row whose member stays paused never receives traffic
	// again, because republish keeps the registry pause.
	resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": true, "reason": "manual"})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("pause status = %d, want 500 (body %s)", resp.Code, resp.Body.String())
	}
	provider, err := env.llmSvc.GetProvider(ctx, memberID)
	if err != nil || provider == nil {
		t.Fatalf("GetProvider after failed pause: provider=%v err=%v", provider, err)
	}
	if provider.Paused {
		t.Fatal("failed pause left the registry member paused")
	}
	share, err := env.repo.LoadShare(ctx, shareID, "")
	if err != nil {
		t.Fatalf("LoadShare: %v", err)
	}
	if share.Status != sqlite.TokenBankShareStatusActive {
		t.Fatalf("status = %q, want active", share.Status)
	}

	resp = env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": true, "reason": "manual"})
	if resp.Code != http.StatusOK {
		t.Fatalf("pause retry status = %d (body %s)", resp.Code, resp.Body.String())
	}
	provider, _ = env.llmSvc.GetProvider(ctx, memberID)
	if provider == nil || !provider.Paused {
		t.Fatal("pause retry did not pause the registry member")
	}

	// Resume is the other direction: a store failure must not leave the member
	// serving while the row still says paused.
	repo.failNext = true
	resp = env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": false})
	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("resume status = %d, want 500 (body %s)", resp.Code, resp.Body.String())
	}
	provider, _ = env.llmSvc.GetProvider(ctx, memberID)
	if provider == nil || !provider.Paused {
		t.Fatal("failed resume left the registry member serving")
	}
	share, err = env.repo.LoadShare(ctx, shareID, "")
	if err != nil {
		t.Fatalf("LoadShare after resume: %v", err)
	}
	if share.Status != sqlite.TokenBankShareStatusPaused {
		t.Fatalf("status = %q, want paused", share.Status)
	}
}

func TestTokenBankShareEndpointsScopeToTheOwner(t *testing.T) {
	env := newShareTestEnv(t)
	_, ownerToken := env.createUser(t, "owner@example.test")
	_, otherToken := env.createUser(t, "other@example.test")

	rec := env.submitShare(t, ownerToken, "fp-scope", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)

	// Another account must not be able to read, pause, rotate or delete it.
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/token-bank/shares/" + shareID + "/models", nil},
		{http.MethodPut, "/api/v1/token-bank/shares/" + shareID + "/models", map[string]any{"models": []map[string]any{{"model": "gpt-4o", "available": true}}}},
		{http.MethodPut, "/api/v1/token-bank/shares/" + shareID + "/paused", map[string]any{"paused": true}},
		{http.MethodPut, "/api/v1/token-bank/shares/" + shareID + "/key", map[string]any{"encrypted_payload": env.wrapKeyEnvelope(t, "sk-stolen"), "key_fingerprint": "fp-x"}},
		{http.MethodDelete, "/api/v1/token-bank/shares/" + shareID, nil},
	} {
		resp := env.doWithMux(t, tc.method, tc.path, otherToken, tc.body)
		if resp.Code != http.StatusNotFound {
			t.Fatalf("%s %s as another user = %d, want 404 (body %s)", tc.method, tc.path, resp.Code, resp.Body.String())
		}
	}
	// The share must be untouched.
	share, err := env.repo.LoadShare(context.Background(), shareID, "")
	if err != nil {
		t.Fatalf("LoadShare: %v", err)
	}
	if share.Status != sqlite.TokenBankShareStatusActive {
		t.Fatalf("share status = %q after another user's requests", share.Status)
	}
}

func TestTokenBankListSharesReturnsOnlyOwnShares(t *testing.T) {
	env := newShareTestEnv(t)
	_, mineToken := env.createUser(t, "mine@example.test")
	_, theirsToken := env.createUser(t, "theirs@example.test")

	if rec := env.submitShare(t, mineToken, "fp-mine", "gpt-4o"); rec.Code != http.StatusCreated {
		t.Fatalf("my submit status = %d", rec.Code)
	}
	if rec := env.submitShare(t, theirsToken, "fp-theirs", "gpt-4o"); rec.Code != http.StatusCreated {
		t.Fatalf("their submit status = %d", rec.Code)
	}

	rec := env.doWithMux(t, http.MethodGet, "/api/v1/token-bank/shares", mineToken, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d (body %s)", rec.Code, rec.Body.String())
	}
	items, _ := decodeMap(t, rec)["shares"].([]any)
	if len(items) != 1 {
		t.Fatalf("list returned %d shares, want exactly my own", len(items))
	}
}

// TestTokenBankSubmitPublishesOnlyAvailableModels pins the probe contract: an
// unavailable model is recorded (so the owner sees why it is missing) but must
// not be published, or dispatch would carry a member known to fail.
func TestTokenBankSubmitPublishesOnlyAvailableModels(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	ctx := context.Background()

	rec := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"display_name":     "My OpenAI",
		"api_url":          "https://api.openai.example/v1",
		"key_fingerprint":  "fp-avail",
		"service_group_id": "default",
		"models": []map[string]any{
			{"model": "gpt-4o", "available": true},
			{"model": "gpt-broken", "available": false, "probe_error": "connection refused"},
		},
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-plaintext-upstream"),
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)

	live, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4o"))
	if err != nil || live == nil {
		t.Fatal("the available model was not published")
	}
	dead, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-broken"))
	if err != nil {
		t.Fatalf("GetProvider(gpt-broken) error = %v", err)
	}
	if dead != nil {
		t.Fatal("an unavailable model was published into the dispatch pool")
	}
	// It must still be recorded, with its probe error, so the owner can act.
	models, err := env.repo.ListModels(ctx, shareID)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	found := false
	for _, m := range models {
		if m.ModelName == "gpt-broken" {
			found = true
			if m.Available {
				t.Fatal("gpt-broken was recorded as available")
			}
			if m.LastProbeError != "connection refused" {
				t.Fatalf("probe error = %q, want it preserved", m.LastProbeError)
			}
		}
	}
	if !found {
		t.Fatal("the unavailable model was not recorded at all")
	}
}

// TestTokenBankTakeOutLeavesSiblingShareAlone guards the shared-array hazard:
// two owners in the same tier share one array route, so withdrawing one must
// not take the other's model offline.
func TestTokenBankTakeOutLeavesSiblingShareAlone(t *testing.T) {
	env := newShareTestEnv(t)
	_, tokenA := env.createUser(t, "a@example.test")
	_, tokenB := env.createUser(t, "b@example.test")
	ctx := context.Background()

	recA := env.submitShare(t, tokenA, "fp-a", "gpt-4o")
	recB := env.submitShare(t, tokenB, "fp-b", "gpt-4o")
	if recA.Code != http.StatusCreated || recB.Code != http.StatusCreated {
		t.Fatalf("submit A=%d B=%d", recA.Code, recB.Code)
	}
	shareA, _ := decodeMap(t, recA)["id"].(string)
	shareB, _ := decodeMap(t, recB)["id"].(string)

	if resp := env.doWithMux(t, http.MethodDelete, "/api/v1/token-bank/shares/"+shareA, tokenA, nil); resp.Code != http.StatusOK {
		t.Fatalf("take out A status = %d (body %s)", resp.Code, resp.Body.String())
	}
	survivor, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareB, "gpt-4o"))
	if err != nil || survivor == nil {
		t.Fatal("taking out one share removed a sibling share's member from the same tier array")
	}
	// And B's model must still be routable.
	_, model, err := env.llmSvc.FindServiceGroupForModel(ctx, "default", "gpt-4o")
	if err != nil || model == nil {
		t.Fatalf("the sibling model is no longer reachable: err=%v", err)
	}
	routedToArray := false
	for _, id := range model.ProviderIDs {
		if strings.EqualFold(strings.TrimSpace(id), llmservice.TokenBankArrayMid) {
			routedToArray = true
		}
	}
	if !routedToArray {
		t.Fatalf("the tier array route was dropped while a member still exists: %v", model.ProviderIDs)
	}
}

func TestTokenBankShareRequiresSession(t *testing.T) {
	env := newShareTestEnv(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/token-bank/shares"},
		{http.MethodGet, "/api/v1/token-bank/shares"},
		{http.MethodPut, "/api/v1/token-bank/shares/share-x/models"},
	} {
		rec := env.doWithMux(t, tc.method, tc.path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s without a session = %d, want 401", tc.method, tc.path, rec.Code)
		}
	}
}

func TestTokenBankShareUnavailableWithoutPublisher(t *testing.T) {
	// A node with the store but not the LLM module must 503 the share
	// endpoints rather than record a share that can never be reached.
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	env.handlers.SetTokenBankPublisher(nil)

	rec := env.submitShare(t, token, "fp-nopub", "gpt-4o")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankListSharesSplitsEarningsByRange(t *testing.T) {
	env := newShareTestEnv(t)
	user, token := env.createUser(t, "range@example.test")
	rec := env.submitShare(t, token, "fp-range", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	now := time.Now().UTC()
	dayStart, monthStart := sqlite.TokenBankCivilBounds(now)
	_, _, net := sqlite.ComputeTokenBankSettlement(10_000, 0, 0, 0, 1, 0, 0, 0, 1, 0.1)
	if net <= 0 {
		t.Fatalf("settlement net = %d", net)
	}
	settle := func(requestID, model string, at time.Time) {
		t.Helper()
		_, err := env.repo.SettleTokenBankUsage(context.Background(), sqlite.TokenBankSettlement{
			RequestID:       requestID,
			ShareID:         shareID,
			OwnerID:         user.ID,
			ModelName:       model,
			InputTokens:     10_000,
			Tier:            "mid",
			TierMultiplier:  1,
			FeeRate:         0.1,
			ChargedMicro:    net,
			UnitInputPer10K: 1,
			CreatedAt:       at,
		})
		if err != nil {
			t.Fatalf("settle %s: %v", requestID, err)
		}
	}
	settle("today-1", "gpt-4o", dayStart.Add(time.Minute))
	settle("yesterday-1", "gpt-old", dayStart.Add(-time.Minute))
	settle("prev-month-1", "gpt-4o", monthStart.Add(-time.Minute))

	yesterdayInMonth := !dayStart.Add(-time.Minute).Before(monthStart)
	wantToday := net
	wantMonth := net
	if yesterdayInMonth {
		wantMonth += net
	}
	wantAll := net * 3

	list := func(query string) map[string]any {
		t.Helper()
		resp := env.doWithMux(t, http.MethodGet, "/api/v1/token-bank/shares"+query, token, nil)
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s = %d (body %s)", query, resp.Code, resp.Body.String())
		}
		return decodeMap(t, resp)
	}
	asInt := func(v any) int64 {
		t.Helper()
		n, ok := v.(float64)
		if !ok {
			t.Fatalf("value %v (%T) is not a number", v, v)
		}
		return int64(n)
	}
	shareOf := func(body map[string]any) map[string]any {
		t.Helper()
		items, _ := body["shares"].([]any)
		if len(items) != 1 {
			t.Fatalf("shares = %d, want 1", len(items))
		}
		item, _ := items[0].(map[string]any)
		return item
	}

	allBody := list("")
	if allBody["range"] != "all" {
		t.Fatalf("default range = %v, want all", allBody["range"])
	}
	allShare := shareOf(allBody)
	if asInt(allShare["today_earned_micro"]) != wantToday || asInt(allShare["range_earned_micro"]) != wantAll || asInt(allShare["all_earned_micro"]) != wantAll {
		t.Fatalf("all window = today %v range %v all %v, want %d / %d / %d", allShare["today_earned_micro"], allShare["range_earned_micro"], allShare["all_earned_micro"], wantToday, wantAll, wantAll)
	}
	if asInt(allShare["month_earned_micro"]) != wantMonth {
		t.Fatalf("month earned = %v, want %d", allShare["month_earned_micro"], wantMonth)
	}
	if allShare["stats_ready"] != true {
		t.Fatal("stats_ready was not set")
	}

	todayBody := list("?range=today")
	todayShare := shareOf(todayBody)
	if todayBody["range"] != "today" || asInt(todayShare["range_earned_micro"]) != wantToday || asInt(todayShare["range_gross_micro"]) <= asInt(todayShare["range_earned_micro"]) {
		t.Fatalf("today window = %+v", todayShare)
	}
	// The fee is the gap between consumed credits and what the owner keeps.
	if asInt(todayShare["range_gross_micro"])-asInt(todayShare["range_fee_micro"]) != asInt(todayShare["range_earned_micro"]) {
		t.Fatalf("gross %v - fee %v != earned %v", todayShare["range_gross_micro"], todayShare["range_fee_micro"], todayShare["range_earned_micro"])
	}

	bad := env.doWithMux(t, http.MethodGet, "/api/v1/token-bank/shares?range=week", token, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad range = %d, want 400 (body %s)", bad.Code, bad.Body.String())
	}

	modelsRec := env.doWithMux(t, http.MethodGet, "/api/v1/token-bank/shares/"+shareID+"/models?range=all", token, nil)
	if modelsRec.Code != http.StatusOK {
		t.Fatalf("models = %d (body %s)", modelsRec.Code, modelsRec.Body.String())
	}
	modelsBody := decodeMap(t, modelsRec)
	models, _ := modelsBody["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %d, want the shared model only", len(models))
	}
	gpt, _ := models[0].(map[string]any)
	usage, _ := gpt["usage"].(map[string]any)
	if gpt["model_name"] != "gpt-4o" || asInt(usage["calls"]) != 2 || asInt(usage["net_micro"]) != net*2 {
		t.Fatalf("gpt-4o usage = %+v", usage)
	}
	orphans, _ := modelsBody["usage_models"].([]any)
	if len(orphans) != 1 {
		t.Fatalf("usage_models = %+v, want the removed model gpt-old", orphans)
	}
	orphan, _ := orphans[0].(map[string]any)
	orphanUsage, _ := orphan["usage"].(map[string]any)
	if orphan["model_name"] != "gpt-old" || asInt(orphanUsage["calls"]) != 1 || asInt(orphanUsage["net_micro"]) != net {
		t.Fatalf("orphan = %+v", orphan)
	}

	todayModels := env.doWithMux(t, http.MethodGet, "/api/v1/token-bank/shares/"+shareID+"/models?range=today", token, nil)
	if todayModels.Code != http.StatusOK {
		t.Fatalf("today models = %d (body %s)", todayModels.Code, todayModels.Body.String())
	}
	todayModelBody := decodeMap(t, todayModels)
	todayItems, _ := todayModelBody["models"].([]any)
	todayGPT, _ := todayItems[0].(map[string]any)
	todayUsage, _ := todayGPT["usage"].(map[string]any)
	if asInt(todayUsage["calls"]) != 1 || asInt(todayUsage["net_micro"]) != net {
		t.Fatalf("today gpt-4o usage = %+v", todayUsage)
	}
	// gpt-old earned yesterday, so it is not a today detail row.
	if extra, _ := todayModelBody["usage_models"].([]any); len(extra) != 0 {
		t.Fatalf("today usage_models = %+v, want none", extra)
	}
}

func TestTokenBankAutomationTierPatchPersistsRoute(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	rec := env.submitShare(t, token, "fp-tier", "gpt-4o", "gpt-4o-mini")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit = %d, body %s", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	gptID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	miniID := llmservice.TokenBankMemberID(shareID, "gpt-4o-mini")
	ctx := context.Background()

	both := callMemberPatch(t, env, gptID, `{"token_bank_tier":"high","array_id":"token_bank_high"}`)
	if both.Code != http.StatusBadRequest || !strings.Contains(both.Body.String(), "not both") {
		t.Fatalf("both fields = %d, body %s", both.Code, both.Body.String())
	}
	ghost := callMemberPatch(t, env, llmservice.TokenBankMemberID(shareID, "no-such-model"), `{"token_bank_tier":"high"}`)
	if ghost.Code != http.StatusNotFound {
		t.Fatalf("missing model = %d, body %s", ghost.Code, ghost.Body.String())
	}
	still, err := env.llmSvc.GetProvider(ctx, gptID)
	if err != nil || still == nil || still.ArrayID != llmservice.TokenBankArrayMid {
		t.Fatalf("rejected patch moved the member: %+v err=%v", still, err)
	}

	graded := callMemberPatch(t, env, gptID, `{"token_bank_tier":"high","dispatch_weight":7}`)
	if graded.Code != http.StatusOK || !strings.Contains(graded.Body.String(), `"token_bank_tier":"high"`) || !strings.Contains(graded.Body.String(), `"array_id":"token_bank_high"`) {
		t.Fatalf("grade = %d, body %s", graded.Code, graded.Body.String())
	}
	moved, err := env.llmSvc.GetProvider(ctx, gptID)
	if err != nil || moved == nil {
		t.Fatalf("load gpt: %v", err)
	}
	if moved.ArrayID != llmservice.TokenBankArrayHigh || moved.TokenBankTier != "high" || moved.TokenBankTierMultiplier != 2 || moved.DispatchWeight != 7 {
		t.Fatalf("gpt member = array %s tier %s rate %v weight %d", moved.ArrayID, moved.TokenBankTier, moved.TokenBankTierMultiplier, moved.DispatchWeight)
	}
	if moved.CreditMultiplier != 1 {
		t.Fatalf("consumer multiplier = %v, want 1", moved.CreditMultiplier)
	}
	assertShareModelTier(t, env, shareID, "gpt-4o", "high", 2, llmservice.TokenBankArrayHigh)
	assertShareModelTier(t, env, shareID, "gpt-4o-mini", "mid", 1, llmservice.TokenBankArrayMid)
	reg, err := env.llmSvc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	assertHTTPModelRoute(t, reg, "gpt-4o", llmservice.TokenBankArrayHigh, true)
	assertHTTPModelRoute(t, reg, "gpt-4o", llmservice.TokenBankArrayMid, false)
	assertHTTPModelRoute(t, reg, "gpt-4o-mini", llmservice.TokenBankArrayMid, true)

	low := callMemberPatch(t, env, miniID, `{"array_id":"token_bank_low"}`)
	if low.Code != http.StatusOK || !strings.Contains(low.Body.String(), `"token_bank_tier":"low"`) {
		t.Fatalf("array_id grade = %d, body %s", low.Code, low.Body.String())
	}
	assertShareModelTier(t, env, shareID, "gpt-4o-mini", "low", 0.5, llmservice.TokenBankArrayLow)
	mini, err := env.llmSvc.GetProvider(ctx, miniID)
	if err != nil || mini == nil || mini.TokenBankTierMultiplier != 0.5 || mini.ArrayID != llmservice.TokenBankArrayLow {
		t.Fatalf("mini member = %+v err=%v", mini, err)
	}

	if err := env.handlers.republishTokenBankShare(ctx, shareID); err != nil {
		t.Fatalf("republish: %v", err)
	}
	republished, err := env.llmSvc.GetProvider(ctx, gptID)
	if err != nil || republished == nil || republished.ArrayID != llmservice.TokenBankArrayHigh || republished.TokenBankTierMultiplier != 2 {
		t.Fatalf("republish restored the old tier: %+v err=%v", republished, err)
	}
	reg, err = env.llmSvc.LoadRegistry(ctx)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	assertHTTPModelRoute(t, reg, "gpt-4o", llmservice.TokenBankArrayHigh, true)
	assertHTTPModelRoute(t, reg, "gpt-4o", llmservice.TokenBankArrayMid, false)
	assertHTTPModelRoute(t, reg, "gpt-4o-mini", llmservice.TokenBankArrayLow, true)
	assertHTTPModelRoute(t, reg, "gpt-4o-mini", llmservice.TokenBankArrayMid, false)
}

func callMemberPatch(t *testing.T, env *shareTestEnv, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	h := adminPatchLLMProviderMember(env.llmSvc, env.handlers)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/llm/providers/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	h(rec, req)
	return rec
}

func assertShareModelTier(t *testing.T, env *shareTestEnv, shareID, model, tier string, multiplier float64, arrayID string) {
	t.Helper()
	models, err := env.repo.ListModels(context.Background(), shareID)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	for _, row := range models {
		if row.ModelName != model {
			continue
		}
		if row.Tier != tier || row.TierMultiplier != multiplier || row.ArrayID != arrayID {
			t.Fatalf("%s row = tier %s rate %v array %s, want %s/%v/%s", model, row.Tier, row.TierMultiplier, row.ArrayID, tier, multiplier, arrayID)
		}
		return
	}
	t.Fatalf("model %s is not on share %s", model, shareID)
}

func assertHTTPModelRoute(t *testing.T, reg *llmservice.Registry, model, arrayID string, want bool) {
	t.Helper()
	var group *llmpool.ServiceGroup
	for i := range reg.ServiceGroups {
		if reg.ServiceGroups[i].ID == "default" {
			group = &reg.ServiceGroups[i]
			break
		}
	}
	if group == nil {
		t.Fatal("default service group disappeared")
	}
	for _, cfg := range group.Models {
		if !strings.EqualFold(cfg.Name, model) {
			continue
		}
		got := false
		for _, id := range cfg.ProviderIDs {
			if strings.EqualFold(id, arrayID) {
				got = true
			}
		}
		if got != want {
			t.Fatalf("route %s includes %s = %v, want %v (%v)", model, arrayID, got, want, cfg.ProviderIDs)
		}
		return
	}
	t.Fatalf("model %s is not on the default group", model)
}

// TestTokenBankSetShareVisibilityReachesRegistry closes the last half of the
// publish blind spot (key rotation is covered by the lifecycle test): the
// visibility endpoint mutates the store row and then republishes, and the
// registry member must end up carrying exactly the new visibility — not the
// one from the first publish, and not a half-applied merge. A member that
// keeps the old scope either leaks a private share to everyone or locks a
// public one to nobody.
func TestTokenBankSetShareVisibilityReachesRegistry(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "visibility@example.test")
	ctx := context.Background()

	rec := env.submitShare(t, token, "fp-visibility", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")

	provider, err := env.llmSvc.GetProvider(ctx, memberID)
	if err != nil || provider == nil {
		t.Fatalf("GetProvider after submit: provider=%v err=%v", provider, err)
	}
	if provider.TokenBankVisibility != sqlite.TokenBankVisibilityPublic {
		t.Fatalf("initial visibility = %q, want public", provider.TokenBankVisibility)
	}

	// Private with one audience: the member's scope must flip to exactly that
	// audience list.
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/visibility", token, map[string]any{
		"visibility": "private",
		"audiences":  []map[string]any{{"hub_id": "hub-a"}},
	}); resp.Code != http.StatusOK {
		t.Fatalf("visibility status = %d (body %s)", resp.Code, resp.Body.String())
	}
	provider, err = env.llmSvc.GetProvider(ctx, memberID)
	if err != nil || provider == nil {
		t.Fatalf("GetProvider after private: provider=%v err=%v", provider, err)
	}
	if provider.TokenBankVisibility != sqlite.TokenBankVisibilityPrivate {
		t.Fatalf("registry visibility = %q, want private", provider.TokenBankVisibility)
	}
	if len(provider.TokenBankAudiences) != 1 || provider.TokenBankAudiences[0].HubID != "hub-a" {
		t.Fatalf("registry audiences = %+v, want exactly [{hub-a}]", provider.TokenBankAudiences)
	}

	// Back to public: the audience list must be cleared, not merely ignored —
	// a stale audience on a public member is dead data at best and a scoped
	// dispatch surprise at worst.
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/visibility", token, map[string]any{
		"visibility": "public",
	}); resp.Code != http.StatusOK {
		t.Fatalf("back-to-public status = %d (body %s)", resp.Code, resp.Body.String())
	}
	provider, _ = env.llmSvc.GetProvider(ctx, memberID)
	if provider == nil || provider.TokenBankVisibility != sqlite.TokenBankVisibilityPublic {
		t.Fatalf("registry visibility after public = %+v, want public", provider)
	}
	if len(provider.TokenBankAudiences) != 0 {
		t.Fatalf("registry audiences after public = %+v, want empty", provider.TokenBankAudiences)
	}

	// Private with nobody named must be refused before any write: allowing it
	// would strand the share between scopes.
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/visibility", token, map[string]any{
		"visibility": "private",
	}); resp.Code != http.StatusBadRequest {
		t.Fatalf("private-without-audience status = %d, want 400 (body %s)", resp.Code, resp.Body.String())
	}
}

func TestTokenBankSyncShareModelsAddsRemovesAndKeepsGrade(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "sharer@example.test")
	ctx := context.Background()

	rec := env.submitShare(t, token, "fp-sync", "gpt-4o", "gpt-4o-mini")
	if rec.Code != http.StatusCreated {
		t.Fatalf("submit status = %d (body %s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	if err := env.repo.SetModelTier(ctx, shareID, "gpt-4o", "high", 2, "token_bank_high", time.Now().UTC()); err != nil {
		t.Fatalf("SetModelTier: %v", err)
	}
	if _, err := env.provider.Write.Exec(`UPDATE token_bank_models SET earned_micro = 42 WHERE share_id = ? AND model_name = ?`, shareID, "gpt-4o"); err != nil {
		t.Fatalf("seed earnings: %v", err)
	}
	if resp := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/paused", token, map[string]any{"paused": true}); resp.Code != http.StatusOK {
		t.Fatalf("pause status = %d (body %s)", resp.Code, resp.Body.String())
	}

	sync := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/models", token, map[string]any{
		"models": []map[string]any{
			{"model": "GPT-4o", "available": true, "share_window": map[string]any{"days": []int{1, 2, 3, 4, 5}, "start": "22:00", "end": "08:00"}},
			{"model": "gpt-4.1", "available": true},
		},
	})
	if sync.Code != http.StatusOK {
		t.Fatalf("sync status = %d, want 200 (body %s)", sync.Code, sync.Body.String())
	}

	models, err := env.repo.ListModels(ctx, shareID)
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	byName := map[string]sqlite.TokenBankShareModel{}
	for _, model := range models {
		byName[model.ModelName] = model
	}
	kept := byName["gpt-4o"]
	if !kept.Enabled || kept.Tier != "high" || kept.TierMultiplier != 2 || kept.ArrayID != "token_bank_high" || kept.EarnedMicro != 42 {
		t.Fatalf("kept model = %+v, want enabled high/2 with earnings 42", kept)
	}
	if kept.ShareWindowJSON == "" || !strings.Contains(kept.ShareWindowJSON, "22:00") {
		t.Fatalf("kept window = %q", kept.ShareWindowJSON)
	}
	if mini := byName["gpt-4o-mini"]; mini.Enabled {
		t.Fatalf("omitted model stayed enabled: %+v", mini)
	}
	added := byName["gpt-4.1"]
	if !added.Enabled || added.Tier != "mid" || !added.Available {
		t.Fatalf("added model = %+v, want enabled mid", added)
	}

	keptMember, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4o"))
	if err != nil || keptMember == nil {
		t.Fatalf("kept member missing: %v", err)
	}
	if !keptMember.Paused || keptMember.TokenBankShareWindow.Start != "22:00" || keptMember.TokenBankShareWindow.End != "08:00" {
		t.Fatalf("kept member pause/window = paused %v window %+v", keptMember.Paused, keptMember.TokenBankShareWindow)
	}
	if removed, _ := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4o-mini")); removed != nil {
		t.Fatal("removed model is still dispatched")
	}
	addedMember, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4.1"))
	if err != nil || addedMember == nil || !addedMember.Paused {
		t.Fatalf("added member = %+v err=%v, want a paused member", addedMember, err)
	}

	empty := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/models", token, map[string]any{
		"models": []map[string]any{},
	})
	if empty.Code != http.StatusBadRequest {
		t.Fatalf("empty sync status = %d, want 400 (body %s)", empty.Code, empty.Body.String())
	}
	badWindow := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/models", token, map[string]any{
		"models": []map[string]any{{"model": "gpt-4o", "available": true, "share_window": map[string]any{"start": "25:00", "end": "26:00"}}},
	})
	if badWindow.Code != http.StatusBadRequest {
		t.Fatalf("bad window status = %d, want 400 (body %s)", badWindow.Code, badWindow.Body.String())
	}
}

func TestTokenBankCanaryWindowFollowsSettingsForNewModelsOnly(t *testing.T) {
	env := newShareTestEnv(t)
	ctx := context.Background()
	_, token := env.createUser(t, "canary@example.test")

	settings := sqlite.DefaultTokenBankSettings()
	settings.CanaryWindowHours = 2
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.settings.Set(ctx, sqlite.TokenBankSettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}

	before := time.Now().UTC()
	rec := env.submitShare(t, token, "fp-canary-hours", "gpt-4o")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", rec.Code, rec.Body.String())
	}
	shareID, _ := decodeMap(t, rec)["id"].(string)
	first, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4o"))
	if err != nil || first == nil {
		t.Fatalf("provider: %v %v", first, err)
	}
	until, err := time.Parse(time.RFC3339, first.TokenBankCanaryUntil)
	if err != nil {
		t.Fatalf("canary until %q: %v", first.TokenBankCanaryUntil, err)
	}
	if until.Before(before.Add(2*time.Hour-time.Minute)) || until.After(time.Now().UTC().Add(2*time.Hour+time.Minute)) {
		t.Fatalf("canary until %s, want about 2h after publish", until)
	}

	settings.CanaryWindowHours = 0
	raw, err = json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.settings.Set(ctx, sqlite.TokenBankSettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	sync := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/models", token, map[string]any{
		"models": []map[string]any{
			{"model": "gpt-4o", "available": true},
			{"model": "gpt-4.1", "available": true},
		},
	})
	if sync.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", sync.Code, sync.Body.String())
	}
	kept, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4o"))
	if err != nil || kept == nil || kept.TokenBankCanaryUntil != first.TokenBankCanaryUntil {
		t.Fatalf("existing canary moved to %q, want %q", kept.TokenBankCanaryUntil, first.TokenBankCanaryUntil)
	}
	added, err := env.llmSvc.GetProvider(ctx, llmservice.TokenBankMemberID(shareID, "gpt-4.1"))
	if err != nil || added == nil {
		t.Fatalf("added provider: %v %v", added, err)
	}
	if added.TokenBankCanaryUntil != "" {
		t.Fatalf("a zero canary window published %q, want full share", added.TokenBankCanaryUntil)
	}
}
