package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func TestTokenBankPrivateShareRequiresAudienceAndStaysPrivate(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "private@example.test")

	missing := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"display_name":      "Lab",
		"api_url":           "https://api.openai.example/v1",
		"key_fingerprint":   "fp-private",
		"visibility":        "private",
		"models":            []map[string]any{{"model": "gpt-4o", "available": true}},
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-plaintext-upstream"),
	})
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("private without audience = %d, want 400 (%s)", missing.Code, missing.Body.String())
	}
	if code, _ := decodeMap(t, missing)["code"].(string); code != "invalid_audience" {
		t.Fatalf("code = %q", code)
	}

	first := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"display_name":      "Lab",
		"api_url":           "https://api.openai.example/v1",
		"key_fingerprint":   "fp-private",
		"visibility":        "private",
		"audiences":         []map[string]any{{"hub_id": "hub-a", "tenant_id": "ten-1"}},
		"models":            []map[string]any{{"model": "gpt-4o", "available": true}},
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-plaintext-upstream"),
	})
	if first.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", first.Code, first.Body.String())
	}
	body := decodeMap(t, first)
	shareID, _ := body["id"].(string)
	if body["visibility"] != "private" || body["has_key"] != true {
		t.Fatalf("payload = %s", first.Body.String())
	}
	if strings.Contains(first.Body.String(), "sk-plaintext-upstream") {
		t.Fatal("the response contained the upstream key")
	}
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	provider, err := env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil {
		t.Fatalf("provider: %v %v", provider, err)
	}
	if provider.TokenBankVisibility != "private" || provider.TokenBankCanaryUntil == "" {
		t.Fatalf("member access = %s canary %q", provider.TokenBankVisibility, provider.TokenBankCanaryUntil)
	}
	until := provider.TokenBankCanaryUntil

	replay := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"display_name":      "Lab",
		"api_url":           "https://api.openai.example/v1",
		"key_fingerprint":   "fp-private",
		"visibility":        "public",
		"models":            []map[string]any{{"model": "gpt-4o", "available": true}},
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-plaintext-upstream"),
	})
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d (%s)", replay.Code, replay.Body.String())
	}
	if decodeMap(t, replay)["visibility"] != "private" {
		t.Fatalf("replay flipped visibility: %s", replay.Body.String())
	}
	again, err := env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || again == nil || again.TokenBankVisibility != "private" || again.TokenBankCanaryUntil != until {
		t.Fatalf("registry after replay = %+v err=%v", again, err)
	}
}

func TestTokenBankExtraKeyRotatesOntoTheMember(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "keys@example.test")
	created := env.submitShare(t, token, "fp-primary", "gpt-4o")
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", created.Code, created.Body.String())
	}
	shareID, _ := decodeMap(t, created)["id"].(string)

	added := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares/"+shareID+"/keys", token, map[string]any{
		"key_fingerprint":   "fp-extra",
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-extra-upstream"),
	})
	if added.Code != http.StatusOK {
		t.Fatalf("add key = %d (%s)", added.Code, added.Body.String())
	}
	if strings.Contains(added.Body.String(), "sk-extra-upstream") {
		t.Fatal("the extra key was returned in plaintext")
	}
	if count, _ := decodeMap(t, added)["extra_key_count"].(float64); count != 1 {
		t.Fatalf("extra_key_count = %v", decodeMap(t, added)["extra_key_count"])
	}
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	provider, err := env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || len(provider.TokenBankExtraKeys) != 1 || provider.TokenBankExtraKeys[0] != "sk-extra-upstream" {
		t.Fatalf("extra keys on the member = %v err=%v", provider, err)
	}

	again := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares/"+shareID+"/keys", token, map[string]any{
		"key_fingerprint":   "fp-extra",
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-extra-upstream"),
	})
	if again.Code != http.StatusOK {
		t.Fatalf("retry add = %d (%s)", again.Code, again.Body.String())
	}
	if count, _ := decodeMap(t, again)["extra_key_count"].(float64); count != 1 {
		t.Fatalf("retry extra_key_count = %v", decodeMap(t, again)["extra_key_count"])
	}
	missing := env.doWithMux(t, http.MethodDelete, "/api/v1/token-bank/shares/"+shareID+"/keys/fp-missing", token, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing key = %d (%s)", missing.Code, missing.Body.String())
	}
	provider, err = env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || len(provider.TokenBankExtraKeys) != 1 {
		t.Fatalf("missing delete changed the member: %+v err=%v", provider, err)
	}

	// A retry that names a different model must not drop the stored member or
	// the extra key. The first create wins.
	replay := env.submitShare(t, token, "fp-primary", "not-stored")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay = %d (%s)", replay.Code, replay.Body.String())
	}
	provider, err = env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || len(provider.TokenBankExtraKeys) != 1 || provider.TokenBankExtraKeys[0] != "sk-extra-upstream" {
		t.Fatalf("replay dropped the extra key: %+v err=%v", provider, err)
	}
	if ghost, _ := env.llmSvc.GetProvider(context.Background(), llmservice.TokenBankMemberID(shareID, "not-stored")); ghost != nil {
		t.Fatalf("replay published a model that is not stored: %+v", ghost)
	}
	blocked := env.submitShare(t, token, "fp-extra", "gpt-4o")
	if blocked.Code != http.StatusConflict {
		t.Fatalf("share reusing an extra key = %d (%s)", blocked.Code, blocked.Body.String())
	}
	rotated := env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/key", token, map[string]any{
		"key_fingerprint":   "fp-extra",
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-extra-upstream"),
	})
	if rotated.Code != http.StatusConflict {
		t.Fatalf("rotate onto an extra key = %d (%s)", rotated.Code, rotated.Body.String())
	}
	rotated = env.doWithMux(t, http.MethodPut, "/api/v1/token-bank/shares/"+shareID+"/key", token, map[string]any{
		"key_fingerprint":   "fp-primary-v2",
		"api_url":           "https://api.openai.example/v2",
		"protocol":          "anthropic",
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-rotated-primary"),
	})
	if rotated.Code != http.StatusOK {
		t.Fatalf("rotate primary = %d (%s)", rotated.Code, rotated.Body.String())
	}
	provider, err = env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || provider.APIKey != "sk-rotated-primary" || provider.APIURL != "https://api.openai.example/v2" || provider.Protocol != "anthropic" {
		t.Fatalf("rotated member = %+v err=%v", provider, err)
	}
	if len(provider.TokenBankExtraKeys) != 1 || provider.TokenBankExtraKeys[0] != "sk-extra-upstream" {
		t.Fatalf("rotation dropped the extra key: %+v", provider.TokenBankExtraKeys)
	}

	removed := env.doWithMux(t, http.MethodDelete, "/api/v1/token-bank/shares/"+shareID+"/keys/fp-extra", token, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove = %d (%s)", removed.Code, removed.Body.String())
	}
	provider, err = env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || len(provider.TokenBankExtraKeys) != 0 {
		t.Fatalf("keys after remove = %v err=%v", provider, err)
	}
}

func TestTokenBankExtraKeyCap(t *testing.T) {
	env := newShareTestEnv(t)
	_, token := env.createUser(t, "cap@example.test")
	created := env.submitShare(t, token, "fp-cap", "gpt-4o")
	shareID, _ := decodeMap(t, created)["id"].(string)
	keys := make([]sqlite.TokenBankStoredKey, sqlite.TokenBankMaxExtraKeys)
	for i := range keys {
		keys[i] = sqlite.TokenBankStoredKey{Fingerprint: "fp-filled-" + string(rune('a'+i)), Encrypted: "blob"}
	}
	encoded, err := sqlite.MarshalTokenBankExtraKeys(keys)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.repo.UpdateShareExtraKeys(context.Background(), shareID, "", encoded, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	rec := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares/"+shareID+"/keys", token, map[string]any{
		"key_fingerprint":   "fp-one-more",
		"encrypted_payload": env.wrapKeyEnvelope(t, "sk-one-more-key"),
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("over cap = %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankAutomationCreateAndBatch(t *testing.T) {
	env := newShareTestEnv(t)
	owner, _ := env.createUser(t, "scripted@example.test")
	secret := "sk-automation-secret"
	body := map[string]any{
		"owner_user_id": owner.ID,
		"display_name":  "Scripted",
		"api_url":       "https://api.openai.example/v1",
		"api_key":       secret,
		"models":        []string{"gpt-4o"},
		"visibility":    "private",
		"audiences":     []map[string]any{{"hub_id": "hub-a"}},
	}
	created := callAutomation(t, env, http.MethodPost, "/api/admin/llm/token-bank/shares", body)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d (%s)", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), secret) {
		t.Fatal("automation response contained the plaintext key")
	}
	payload := decodeMap(t, created)
	if payload["has_key"] != true || payload["visibility"] != "private" {
		t.Fatalf("payload = %s", created.Body.String())
	}
	shareID, _ := payload["id"].(string)
	memberID := llmservice.TokenBankMemberID(shareID, "gpt-4o")
	provider, err := env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || provider == nil || provider.APIKey != secret || provider.TokenBankCanaryUntil == "" {
		t.Fatalf("published member = %+v err=%v", provider, err)
	}
	if provider.TokenBankVisibility != "private" {
		t.Fatalf("visibility = %s", provider.TokenBankVisibility)
	}

	replay := callAutomation(t, env, http.MethodPost, "/api/admin/llm/token-bank/shares", map[string]any{
		"owner_user_id": owner.ID,
		"display_name":  "Scripted",
		"api_url":       "https://api.openai.example/v1",
		"api_key":       secret,
		"models":        []string{"not-stored"},
		"visibility":    "public",
	})
	if replay.Code != http.StatusOK || decodeMap(t, replay)["visibility"] != "private" {
		t.Fatalf("replay = %d %s", replay.Code, replay.Body.String())
	}
	again, err := env.llmSvc.GetProvider(context.Background(), memberID)
	if err != nil || again == nil || again.APIKey != secret || again.TokenBankVisibility != "private" {
		t.Fatalf("replay rewrote the stored member: %+v err=%v", again, err)
	}
	if ghost, _ := env.llmSvc.GetProvider(context.Background(), llmservice.TokenBankMemberID(shareID, "not-stored")); ghost != nil {
		t.Fatalf("replay published a model that is not stored: %+v", ghost)
	}

	unverified, err := env.users.EnsureAccount(context.Background(), "unverified-script@example.test")
	if err != nil {
		t.Fatal(err)
	}
	denied := callAutomation(t, env, http.MethodPost, "/api/admin/llm/token-bank/shares", map[string]any{
		"owner_user_id": unverified.ID,
		"display_name":  "Nope",
		"api_url":       "https://api.openai.example/v1",
		"api_key":       "sk-other-secret",
		"models":        []string{"gpt-4o"},
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("unverified = %d (%s)", denied.Code, denied.Body.String())
	}

	batch := callAutomation(t, env, http.MethodPost, "/api/admin/llm/token-bank/shares/batch", map[string]any{
		"shares": []any{
			map[string]any{
				"owner_user_id": owner.ID,
				"display_name":  "Second",
				"api_url":       "https://api.other.example/v1",
				"api_key":       "sk-batch-secret",
				"models":        []string{"gpt-4o-mini"},
			},
			map[string]any{
				"owner_user_id": "missing-user",
				"display_name":  "Missing",
				"api_url":       "https://api.other.example/v1",
				"api_key":       "sk-missing-secret",
				"models":        []string{"gpt-4o-mini"},
			},
		},
	})
	if batch.Code != http.StatusOK {
		t.Fatalf("batch = %d (%s)", batch.Code, batch.Body.String())
	}
	if strings.Contains(batch.Body.String(), "sk-batch-secret") || strings.Contains(batch.Body.String(), "sk-missing-secret") {
		t.Fatal("batch response contained a plaintext key")
	}
	var decoded struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(batch.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Results) != 2 || decoded.Results[0]["ok"] != true || decoded.Results[1]["status"] != float64(http.StatusNotFound) {
		t.Fatalf("results = %+v", decoded.Results)
	}
}

func TestTokenBankAutomationBatchRequiresWriteScope(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	reader, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "reader", Scopes: []string{"read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "writer", Scopes: []string{"write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := env.automationMux(llmSvc)
	rec := env.doAutomation(t, mux, http.MethodPost, "/api/admin/llm/token-bank/shares/batch", reader.APIKey, map[string]any{
		"shares": []any{map[string]any{"owner_user_id": "u", "display_name": "n", "api_url": "https://api.example/v1", "api_key": "sk", "models": []string{"m"}}},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read scope = %d (%s)", rec.Code, rec.Body.String())
	}
	rec = env.doAutomation(t, mux, http.MethodPost, "/api/admin/llm/token-bank/shares", writer.APIKey, map[string]any{
		"owner_user_id": "missing",
		"display_name":  "n",
		"api_url":       "https://api.example/v1",
		"api_key":       "sk-writer",
		"models":        []string{"m"},
	})
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusNotFound {
		t.Fatalf("write scope should pass the authorizer, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestTokenBankAdminLeaderboard(t *testing.T) {
	env := newTokenBankAdminTestEnv(t)
	env.seedShare(t, "s-a", "owner-a", "fp-a", "gpt")
	env.seedShare(t, "s-b", "owner-b", "fp-b", "gpt")
	env.seedShare(t, "s-c", "owner-c", "fp-c", "gpt")
	now := time.Now().UTC().Format(time.RFC3339)
	insert := `INSERT INTO token_bank_usage (request_id, share_id, owner_user_id, model_name, net_micro, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, 'gpt', ?, 3, 1, ?)`
	if _, err := env.provider.Write.Exec(insert, "req-a", "s-a", "owner-a", int64(5_000_000), now); err != nil {
		t.Fatal(err)
	}
	if _, err := env.provider.Write.Exec(insert, "req-c", "s-c", "owner-c", int64(5_000_000), now); err != nil {
		t.Fatal(err)
	}
	if _, err := env.provider.Write.Exec(insert, "req-b", "s-b", "owner-b", int64(1_000_000), now); err != nil {
		t.Fatal(err)
	}
	if _, err := env.provider.Write.Exec(insert, "req-zero", "s-b", "owner-b", int64(0), now); err != nil {
		t.Fatal(err)
	}
	rec := env.doAdmin(t, http.MethodGet, "/api/admin/token-bank/leaderboard", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("leaderboard = %d (%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Leaders []map[string]any `json:"leaders"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Leaders) < 2 {
		t.Fatalf("leaders = %+v", body.Leaders)
	}
	if body.Leaders[0]["owner_email"] != "owner-a@example.com" || body.Leaders[0]["rank_badge"] != "gold" {
		t.Fatalf("first = %+v", body.Leaders[0])
	}
	if body.Leaders[0]["lifetime_badge"] != "sprout" {
		t.Fatalf("lifetime = %v", body.Leaders[0]["lifetime_badge"])
	}
	if len(body.Leaders) != 3 {
		t.Fatalf("leaders = %+v, want the tie and the lower score, with the zero row omitted", body.Leaders)
	}
	if body.Leaders[1]["owner_user_id"] != "owner-c" || body.Leaders[1]["rank"] != float64(1) || body.Leaders[1]["rank_badge"] != "gold" {
		t.Fatalf("tied second = %+v, want rank 1 gold", body.Leaders[1])
	}
	if body.Leaders[2]["owner_user_id"] != "owner-b" || body.Leaders[2]["rank"] != float64(3) {
		t.Fatalf("third = %+v, want rank 3", body.Leaders[2])
	}
}

func callAutomation(t *testing.T, env *shareTestEnv, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	switch path {
	case "/api/admin/llm/token-bank/shares/batch":
		env.handlers.TokenBankAutomationBatchShares(rec, req)
	default:
		env.handlers.TokenBankAutomationCreateShare(rec, req)
	}
	return rec
}
