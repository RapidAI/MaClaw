package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

func TestTokenBankProviderDeniedMatchesHostAndSubdomain(t *testing.T) {
	list := []string{"Blocked.Example", "  ", "api.other.test"}
	if !tokenBankProviderDenied("https://blocked.example/v1", list) {
		t.Fatal("exact host was allowed")
	}
	if !tokenBankProviderDenied("https://API.Blocked.Example/v1/models", list) {
		t.Fatal("subdomain was allowed")
	}
	if tokenBankProviderDenied("https://notblocked.example/v1", list) {
		t.Fatal("a different host that only shares a suffix was denied")
	}
	if tokenBankProviderDenied("https://api.openai.example/v1", list) {
		t.Fatal("an unlisted host was denied")
	}
	if tokenBankProviderDenied("://", list) {
		t.Fatal("an unparseable URL was denied")
	}
}

func TestTokenBankCreateShareRejectsDenylistedProvider(t *testing.T) {
	env := newShareTestEnv(t)
	ctx := context.Background()
	settings := sqlite.DefaultTokenBankSettings()
	settings.ProviderDenylist = []string{" blocked.example ", ""}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.settings.Set(ctx, sqlite.TokenBankSettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	_, token := env.createUser(t, "sharer@example.test")

	denied := env.doWithMux(t, http.MethodPost, "/api/v1/token-bank/shares", token, map[string]any{
		"client_instance_id": "client-deny",
		"display_name":       "Blocked",
		"api_url":            "https://gateway.blocked.example/v1",
		"protocol":           "openai",
		"key_fingerprint":    "fp-denied",
		"models":             []map[string]any{{"model": "gpt-4o", "available": true}},
		"encrypted_payload":  "not-a-real-envelope",
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body %s)", denied.Code, denied.Body.String())
	}
	body := decodeMap(t, denied)
	if body["code"] != "provider_denied" {
		t.Fatalf("code = %v, want provider_denied", body["code"])
	}

	allowed := env.submitShare(t, token, "fp-allowed", "gpt-4o")
	if allowed.Code != http.StatusCreated {
		t.Fatalf("allowed host status = %d, want 201 (body %s)", allowed.Code, allowed.Body.String())
	}
}

func TestValidateTokenBankSettingsDropsBlankDenylistEntries(t *testing.T) {
	in := sqlite.DefaultTokenBankSettings()
	in.ProviderDenylist = []string{" OpenAI.com ", "", "  "}
	out, err := validateTokenBankSettings(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.ProviderDenylist) != 1 || out.ProviderDenylist[0] != "OpenAI.com" {
		t.Fatalf("denylist = %#v", out.ProviderDenylist)
	}
}

func TestTokenBankUsageDailyAndCSV(t *testing.T) {
	env := newTokenBankTestEnv(t)
	ctx := context.Background()
	owner, token := env.createUser(t, "owner@example.test")
	_, otherToken := env.createUser(t, "other@example.test")
	_, err := env.repo.SettleTokenBankUsage(ctx, sqlite.TokenBankSettlement{
		RequestID:       "usage-req-1",
		ShareID:         "share-usage",
		OwnerID:         owner.ID,
		ModelName:       "llama",
		InputTokens:     1000,
		OutputTokens:    500,
		TierMultiplier:  1,
		FeeRate:         0,
		ChargedMicro:    10_000_000,
		UnitInputPer10K: 3,
		CreatedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("settle: %v", err)
	}

	daily := env.do(t, http.MethodGet, "/api/v1/token-bank/usage/daily?days=30", token, nil)
	if daily.Code != http.StatusOK {
		t.Fatalf("daily status = %d body %s", daily.Code, daily.Body.String())
	}
	payload := decodeMap(t, daily)
	if payload["days"] != float64(30) {
		t.Fatalf("days = %v", payload["days"])
	}
	rows, _ := payload["daily"].([]any)
	if len(rows) != 1 {
		t.Fatalf("daily rows = %#v", payload["daily"])
	}
	row, _ := rows[0].(map[string]any)
	if row["day"] == "" || row["net_micro"] == nil {
		t.Fatalf("day payload = %#v, want snake_case day and net_micro", row)
	}
	models, _ := payload["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models = %#v", payload["models"])
	}
	model, _ := models[0].(map[string]any)
	if model["model"] != "llama" {
		t.Fatalf("top model = %#v", model)
	}

	clamped := env.do(t, http.MethodGet, "/api/v1/token-bank/usage/daily?days=9999", token, nil)
	if decodeMap(t, clamped)["days"] != float64(366) {
		t.Fatalf("clamped days = %v, want 366", decodeMap(t, clamped)["days"])
	}

	csvRec := env.do(t, http.MethodGet, "/api/v1/token-bank/usage.csv?days=30", token, nil)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("csv status = %d", csvRec.Code)
	}
	if got := csvRec.Header().Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("content-type = %q", got)
	}
	body := csvRec.Body.String()
	if !strings.Contains(body, "created_at,request_id,model,input_tokens,output_tokens,gross_micro,fee_micro,net_micro,charged_micro,self_use") {
		t.Fatalf("csv header = %q", body)
	}
	if !strings.Contains(body, "usage-req-1") || !strings.Contains(body, "llama") {
		t.Fatalf("csv body = %q", body)
	}

	other := env.do(t, http.MethodGet, "/api/v1/token-bank/usage.csv?days=30", otherToken, nil)
	if strings.Contains(other.Body.String(), "usage-req-1") {
		t.Fatalf("other owner saw the row: %s", other.Body.String())
	}
}
