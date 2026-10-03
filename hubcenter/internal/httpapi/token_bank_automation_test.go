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

// The §6.3 surface is the one place where an hck_ key reaches Token Bank data.
// These tests mint a real key through the LLM service rather than stubbing the
// authorizer, because the whole point of §6.3 is that the scope check runs:
// a test with a fake authorizer would not prove that read cannot delete.
func newTokenBankAutomationEnv(t *testing.T) (*tokenBankAdminTestEnv, *llmservice.Service) {
	t.Helper()
	env := newTokenBankAdminTestEnv(t)
	llmSvc := llmservice.NewService(&llmDeleteTestSettings{data: map[string]string{}})
	return env, llmSvc
}

// automationMux wires the §6.3 routes the way llm_routes.go does.
func (e *tokenBankAdminTestEnv) automationMux(llmSvc *llmservice.Service) *http.ServeMux {
	mux := http.NewServeMux()
	h := e.handlers
	mux.HandleFunc("GET /api/admin/llm/token-bank/shares",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "read", h.TokenBankAdminShares))
	mux.HandleFunc("POST /api/admin/llm/token-bank/shares",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "write", h.TokenBankAutomationCreateShare))
	mux.HandleFunc("POST /api/admin/llm/token-bank/shares/batch",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "write", h.TokenBankAutomationBatchShares))
	mux.HandleFunc("PUT /api/admin/llm/token-bank/shares/{id}/paused",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "write", h.TokenBankAdminSetSharePaused))
	mux.HandleFunc("PUT /api/admin/llm/token-bank/shares/{id}/models/{model}/tier",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "write", h.TokenBankAdminSetModelTier))
	mux.HandleFunc("DELETE /api/admin/llm/token-bank/shares/{id}",
		RequireLLMAdminScope(e.adminAuth, llmSvc, "delete", h.TokenBankAdminTakeOutShare))
	return mux
}

func (e *tokenBankAdminTestEnv) doAutomation(t *testing.T, mux *http.ServeMux, method, path, key string, body any) *httptest.ResponseRecorder {
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
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestTokenBankAutomationReadScopeCanList(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	created, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "reader", Scopes: []string{"read"},
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	mux := env.automationMux(llmSvc)
	rec := env.doAutomation(t, mux, "GET", "/api/admin/llm/token-bank/shares", created.APIKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-scoped list = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	shares, _ := body["shares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("shares = %d, want 1", len(shares))
	}
	// The automation response must redact the key material exactly like the
	// admin one: an hck_ key is not a stronger credential than an admin session.
	share, _ := shares[0].(map[string]any)
	if key, _ := share["EncryptedKey"].(string); key != "" {
		t.Fatalf("automation listing exposed EncryptedKey = %q, want empty", key)
	}
}

func TestTokenBankAutomationScopeIsEnforcedPerVerb(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	// A read-only key must not be able to pause or delete: that is the entire
	// reason §6.3 routes through RequireLLMAdminScope instead of RequireAdmin.
	reader, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "reader", Scopes: []string{"read"},
	})
	if err != nil {
		t.Fatalf("create read key: %v", err)
	}
	mux := env.automationMux(llmSvc)

	rec := env.doAutomation(t, mux, "PUT", "/api/admin/llm/token-bank/shares/share-1/paused",
		reader.APIKey, map[string]any{"paused": true})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read key pausing = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAutomation(t, mux, "DELETE", "/api/admin/llm/token-bank/shares/share-1", reader.APIKey, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read key deleting = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAutomation(t, mux, "PUT", "/api/admin/llm/token-bank/shares/share-1/models/gpt-4o/tier",
		reader.APIKey, map[string]any{"tier": "high"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read key setting tier = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	// The share must be untouched by both attempts.
	share, err := env.repo.LoadShare(context.Background(), "share-1", "")
	if err != nil {
		t.Fatalf("share was removed by a read-only key: %v", err)
	}
	if share.Status != sqlite.TokenBankShareStatusActive {
		t.Fatalf("status after refused pause = %q, want active", share.Status)
	}
}

func TestTokenBankAutomationWriteAndDeleteScopesWork(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	env.seedShare(t, "share-2", "user-1", "fp-2", "gpt-4o-mini")

	writer, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "writer", Scopes: []string{"write"},
	})
	if err != nil {
		t.Fatalf("create write key: %v", err)
	}
	deleter, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "deleter", Scopes: []string{"delete"},
	})
	if err != nil {
		t.Fatalf("create delete key: %v", err)
	}
	mux := env.automationMux(llmSvc)

	rec := env.doAutomation(t, mux, "PUT", "/api/admin/llm/token-bank/shares/share-1/paused",
		writer.APIKey, map[string]any{"paused": true, "reason": "automation"})
	if rec.Code != http.StatusOK {
		t.Fatalf("write key pausing = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	share, _ := env.repo.LoadShare(context.Background(), "share-1", "")
	if share.Status != sqlite.TokenBankShareStatusPaused {
		t.Fatalf("status = %q, want paused", share.Status)
	}
	rec = env.doAutomation(t, mux, "PUT", "/api/admin/llm/token-bank/shares/share-1/models/gpt-4o/tier",
		writer.APIKey, map[string]any{"tier": "high"})
	if rec.Code != http.StatusOK {
		t.Fatalf("write key setting tier = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	models, err := env.repo.ListModels(context.Background(), "share-1")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].Tier != "high" || models[0].TierMultiplier != 2 || models[0].ArrayID != "token_bank_high" {
		t.Fatalf("tier row = %+v, want high/2/token_bank_high", models)
	}

	// A write key must not be able to delete.
	rec = env.doAutomation(t, mux, "DELETE", "/api/admin/llm/token-bank/shares/share-2", writer.APIKey, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("write key deleting = %d, want 403; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAutomation(t, mux, "DELETE", "/api/admin/llm/token-bank/shares/share-2", deleter.APIKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete key deleting = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	if _, err := env.repo.LoadShare(context.Background(), "share-2", ""); err == nil {
		t.Fatalf("share-2 still exists after an authorized delete")
	}
}

func TestTokenBankAutomationRejectsUnknownKey(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")
	mux := env.automationMux(llmSvc)

	rec := env.doAutomation(t, mux, "GET", "/api/admin/llm/token-bank/shares", "hck_not-a-real-key", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bogus hck_ key = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
	rec = env.doAutomation(t, mux, "GET", "/api/admin/llm/token-bank/shares", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no credential = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}

func TestTokenBankAutomationExpiredKeyIsRejected(t *testing.T) {
	env, llmSvc := newTokenBankAutomationEnv(t)
	env.seedShare(t, "share-1", "user-1", "fp-1", "gpt-4o")

	// Expiry is set slightly in the future, then rewind the clock semantics by
	// writing the record with a past expiry directly: CreateAdminAPIKeySpec
	// refuses a past ExpiresAt, which is itself worth asserting.
	if _, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "expired", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(-time.Hour),
	}); err == nil {
		t.Fatalf("creating a key that is already expired should be refused")
	}

	soon, err := llmSvc.CreateAdminAPIKeySpec(context.Background(), llmservice.AdminAPIKeySpec{
		Name: "soon", Scopes: []string{"read"}, ExpiresAt: time.Now().Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	mux := env.automationMux(llmSvc)
	if rec := env.doAutomation(t, mux, "GET", "/api/admin/llm/token-bank/shares", soon.APIKey, nil); rec.Code != http.StatusOK {
		t.Fatalf("valid key before expiry = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	time.Sleep(2100 * time.Millisecond)
	rec := env.doAutomation(t, mux, "GET", "/api/admin/llm/token-bank/shares", soon.APIKey, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired key = %d, want 401; body %s", rec.Code, rec.Body.String())
	}
}
