package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type tokenBankUserList struct {
	users []*store.User
}

func (l tokenBankUserList) ListUsers(context.Context) ([]*store.User, error) {
	return l.users, nil
}

func TestWithdrawTokenBankOverCapIsInsufficient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    "insufficient_credits",
			"message": "token bank: insufficient available credits: requested 4000000, allowed 2000000",
		})
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.BaseURL = server.URL
	cfg.Center.BaseURLs = nil
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	_, err := svc.WithdrawTokenBank(context.Background(), llmservice.TokenBankCenterWithdraw{
		Email: "owner@example.com", RequestID: "req-1", ServiceGroupID: "paid", AmountMicro: 4_000_000, Manual: true,
	})
	if !errors.Is(err, llmservice.ErrTokenBankInsufficient) {
		t.Fatalf("err = %v, want insufficient cap", err)
	}
}

func TestRunTokenBankAutoOncePullsBelowThreshold(t *testing.T) {
	var withdraws int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": "tbk-auto", "amount_micro": 1_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
	}); err != nil {
		t.Fatal(err)
	}
	// A stored threshold used to top up until that balance. One available
	// credit must now keep the next tick idle.
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"threshold_micro":5000000,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	svc.users = tokenBankUserList{users: []*store.User{{ID: "user-1", Email: "owner@example.com"}}}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 1 {
		t.Fatalf("withdraws = %d, want 1", withdraws)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 || reg.Grants[0].Source != llmservice.TokenBankGrantSource || reg.Grants[0].CreditsTotal != 1 {
		t.Fatalf("grants = %+v", reg.Grants)
	}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 1 {
		t.Fatalf("second tick withdraws = %d, want 1 while a credit remains", withdraws)
	}
	reg, err = llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 1 {
		t.Fatalf("grant count after second tick = %d, want 1", len(reg.Grants))
	}
	if llmservice.SumTokenBankGrantMicro(reg.Grants, "owner@example.com") != 1_000_000 {
		t.Fatalf("grant micro = %d, want 1000000", llmservice.SumTokenBankGrantMicro(reg.Grants, "owner@example.com"))
	}
}

func TestRunTokenBankAutoOnceWaitsForNewUserPeriod(t *testing.T) {
	var withdraws int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": "tbk-auto", "amount_micro": 1_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{
			{ID: "system-free", Name: "System Free", AccessPolicy: llmservice.AccessPolicyFree},
			{ID: "paid", Name: "Paid", AccessPolicy: llmservice.AccessPolicyGrantRequired},
		},
		DefaultNewUserLimitCard: llmservice.NewUserLimitCard{
			ServiceGroupIDs: []string{"system-free"},
			PeriodLimits:    llmservice.CreditPeriodLimits{FiveHour: 1000, Daily: 2000},
		},
		Grants: []llmservice.Grant{{
			ID: "welcome", UserID: "user-1", Email: "owner@example.com",
			ServiceGroupID: "system-free", Source: "new_user_limit_card",
			StartsAt: now, CreatedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	svc.users = tokenBankUserList{users: []*store.User{{ID: "user-1", Email: "owner@example.com"}}}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 0 {
		t.Fatalf("withdraws = %d, want 0 while the new-user window remains", withdraws)
	}

	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Now().UTC().Add(-time.Minute)
	for i := range reg.Grants {
		if reg.Grants[i].Source != "new_user_limit_card" {
			continue
		}
		reg.Grants[i].PeriodUsage.FiveHour = llmservice.GrantUsageWindow{WindowStart: anchor, CreditsUsed: 1000}
	}
	if err := llmservice.SaveRegistry(context.Background(), settings, reg); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 1 {
		t.Fatalf("withdraws = %d, want 1 after the new-user window is exhausted", withdraws)
	}
}

func TestRunTokenBankAutoOnceSkipsSystemAccount(t *testing.T) {
	var emails []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			var body struct {
				Email string `json:"email"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			emails = append(emails, body.Email)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": "tbk-auto", "amount_micro": 1_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	svc.users = tokenBankUserList{users: []*store.User{
		{ID: "sys_user_default", Email: llmservice.SystemLLMUserEmail},
		{ID: "user-1", Email: "owner@example.com"},
	}}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(emails) != 1 || emails[0] != "owner@example.com" {
		t.Fatalf("withdraw emails = %#v, want only the real user", emails)
	}
}
