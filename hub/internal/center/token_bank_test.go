package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func currentTokenBankSpend(t *testing.T, settings llmservice.SystemSettingsRepository, userID, email string, groups []string) int64 {
	t.Helper()
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	return tokenBankChargedSpendableMicro(reg, userID, email, groups)
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

func TestPullTokenBankForAdmissionShortfallPullsWhileBalanceRemains(t *testing.T) {
	var withdraws int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": "tbk-auto", "amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
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
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{{
			ID: "card", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
			CreditsTotal: 5.34, Permanent: true, StartsAt: now, ExpiresAt: now.AddDate(1, 0, 0),
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
		t.Fatalf("heartbeat withdraws = %d, want 0 while the point card still has credits", withdraws)
	}
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"paid"}, currentTokenBankSpend(t, settings, "user-1", "owner@example.com", []string{"paid"}))
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || withdraws != 1 {
		t.Fatalf("pulled=%v withdraws=%d, want one admission pull", pulled, withdraws)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 2 {
		t.Fatalf("grants = %d, want the original card plus the admission pull", len(reg.Grants))
	}
}

func TestRunTokenBankAutoOnceConfirmsAWrittenGrantWhileCreditsRemain(t *testing.T) {
	requestID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	var withdraws, finishes int
	var finishedGrant string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			// The bank already debited this request. Replay must not look like a new grant.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": false, "request_id": requestID,
				"amount_micro": 106_655_500, "grant_id": "", "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			finishes++
			var body struct {
				GrantID string `json:"grant_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			finishedGrant = body.GrantID
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
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
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{
			{
				ID: "grant-stuck", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(requestID),
				CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
			{
				ID: "grant-rest", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: "tbk:later",
				CreditsTotal: 58.4965, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
		},
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
	if withdraws != 0 || finishes != 1 || finishedGrant != "grant-stuck" {
		t.Fatalf("withdraws=%d finishes=%d grant=%q, want a confirm of grant-stuck and no second debit", withdraws, finishes, finishedGrant)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 2 {
		t.Fatalf("grants = %d, want the written grant left in place", len(reg.Grants))
	}
	raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"owner@example.com\npaid":1`) && !strings.Contains(raw, `owner@example.com\npaid":1`) {
		t.Fatalf("auto seq = %s, want the confirmed sequence advanced", raw)
	}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 0 || finishes != 1 {
		t.Fatalf("second tick withdraws=%d finishes=%d, want no further pull while credits remain", withdraws, finishes)
	}
}

func TestStartHeartbeatConfirmsSpentAutoGrantBeforeTheTicker(t *testing.T) {
	requestID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	var withdraws atomic.Int32
	var finishes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/hubs/hub-auto/heartbeat":
			_, _ = w.Write([]byte(`{"ok":true,"status":"online"}`))
		case "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": false, "request_id": requestID,
				"amount_micro": 106_655_500, "grant_id": "", "state": "issued", "hub_id": "hub-auto",
			})
		case "/api/hubs/hub-auto/token-bank/grants":
			finishes.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	cfg.Center.HeartbeatIntervalSec = 3600
	cfg.Server.PublicBaseURL = "https://hub.example.com"
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{
			{
				ID: "grant-stuck", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(requestID),
				CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
			{
				ID: "grant-rest", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: "tbk:later",
				CreditsTotal: 0.4095, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	svc.users = tokenBankUserList{users: []*store.User{{ID: "user-1", Email: "owner@example.com"}}}
	svc.startHeartbeatLoop()
	t.Cleanup(func() {
		svc.mu.Lock()
		cancel := svc.heartbeatCancel
		svc.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	})

	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
		if err == nil && finishes.Load() == 1 && strings.Contains(raw, `owner@example.com\npaid":1`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("startup confirm finishes=%d withdraws=%d seq=%q", finishes.Load(), withdraws.Load(), raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if withdraws.Load() != 0 {
		t.Fatalf("withdraws=%d, want 0 while a remainder is still spendable", withdraws.Load())
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 2 {
		t.Fatalf("grants = %d, want the spent grant left in place", len(reg.Grants))
	}
}

func TestRunTokenBankAutoOnceDoesNotDebitAShareWrittenDuringConfirm(t *testing.T) {
	requestID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	landedID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 1)
	var withdraws int
	settings := newFakeSettingsRepo()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": requestID,
				"amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			// An admission pull lands and moves the head while this confirm is
			// still on the wire. The tick must see that share before it debits.
			reg, err := llmservice.LoadRegistry(r.Context(), settings)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			now := time.Now().UTC()
			reg.Grants = append(reg.Grants, llmservice.Grant{
				ID: "grant-landed", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(landedID),
				CreditsTotal: 20, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			})
			if err := llmservice.SaveRegistry(r.Context(), settings, reg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := settings.Set(r.Context(), "token_bank_auto_seq", "{\"owner@example.com\\npaid\":2}"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{{
			ID: "grant-stuck", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
			Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(requestID),
			CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
			StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
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
		t.Fatalf("withdraws=%d, want no debit after a share landed during confirm", withdraws)
	}
	raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"owner@example.com\npaid":2`) && !strings.Contains(raw, `owner@example.com\npaid":2`) {
		t.Fatalf("auto seq = %s, want the head left where the in-flight share moved it", raw)
	}
}

func TestPullTokenBankForAdmissionShortfallPullsANewShareWhenCurrentSeqIsSpent(t *testing.T) {
	spentID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	var withdraws, finishes int
	var withdrew string
	finished := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			var body struct {
				RequestID string `json:"request_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			withdrew = body.RequestID
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": body.RequestID,
				"amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			finishes++
			var body struct {
				GrantID string `json:"grant_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			finished[body.GrantID]++
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
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
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{
			{
				ID: "grant-spent", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(spentID),
				CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
			{
				ID: "grant-rest", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				CreditsTotal: 0.4095, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"paid"}, currentTokenBankSpend(t, settings, "user-1", "owner@example.com", []string{"paid"}))
	if err != nil {
		t.Fatal(err)
	}
	nextID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 1)
	if !pulled || withdraws != 1 || withdrew != nextID {
		t.Fatalf("pulled=%v withdraws=%d request=%q, want one new share %s", pulled, withdraws, withdrew, nextID)
	}
	if finished["grant-spent"] != 1 {
		t.Fatalf("confirms = %v, want the spent grant bound once", finished)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 3 {
		t.Fatalf("grants = %d, want the spent grant, the remainder, and the new share", len(reg.Grants))
	}
	raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"owner@example.com\npaid":2`) && !strings.Contains(raw, `owner@example.com\npaid":2`) {
		t.Fatalf("auto seq = %s, want the spent id and the new share both advanced", raw)
	}
	if finishes < 2 {
		t.Fatalf("finishes = %d, want the spent grant and the new share", finishes)
	}
}

func TestPullTokenBankForAdmissionShortfallDoesNotDebitAShareWrittenDuringConfirm(t *testing.T) {
	spentID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	landedID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 1)
	var withdraws int
	settings := newFakeSettingsRepo()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": spentID,
				"amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			reg, err := llmservice.LoadRegistry(r.Context(), settings)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			now := time.Now().UTC()
			reg.Grants = append(reg.Grants, llmservice.Grant{
				ID: "grant-landed", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(landedID),
				CreditsTotal: 20, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			})
			if err := llmservice.SaveRegistry(r.Context(), settings, reg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if err := settings.Set(r.Context(), "token_bank_auto_seq", "{\"owner@example.com\\npaid\":2}"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = server.URL
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered: true, HubID: "hub-auto", HubSecret: "secret",
	})); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	if err := llmservice.SaveRegistry(context.Background(), settings, &llmservice.Registry{
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{
			{
				ID: "grant-spent", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(spentID),
				CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
			{
				ID: "grant-rest", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				CreditsTotal: 0.4095, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"paid"}, currentTokenBankSpend(t, settings, "user-1", "owner@example.com", []string{"paid"}))
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || withdraws != 0 {
		t.Fatalf("pulled=%v withdraws=%d, want the landed share to satisfy admission without another debit", pulled, withdraws)
	}
	raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"owner@example.com\npaid":2`) && !strings.Contains(raw, `owner@example.com\npaid":2`) {
		t.Fatalf("auto seq = %s, want the head left where the in-flight share moved it", raw)
	}
}

func TestPullTokenBankForAdmissionShortfallDebitsWhenConfirmFails(t *testing.T) {
	spentID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	var withdraws, grantCalls int
	var withdrew string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			var body struct {
				RequestID string `json:"request_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			withdrew = body.RequestID
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": body.RequestID,
				"amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case r.URL.Path == "/api/hubs/hub-auto/token-bank/grants":
			grantCalls++
			if grantCalls == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": "bind_failed", "message": "confirm unavailable"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
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
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{{
			ID: "grant-spent", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
			Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(spentID),
			CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
			StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	svc.users = tokenBankUserList{users: []*store.User{{ID: "user-1", Email: "owner@example.com"}}}
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"paid"}, currentTokenBankSpend(t, settings, "user-1", "owner@example.com", []string{"paid"}))
	nextID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 1)
	if !pulled || withdraws != 1 || withdrew != nextID {
		t.Fatalf("pulled=%v err=%v withdraws=%d request=%q, want one new share %s while confirm is down", pulled, err, withdraws, withdrew, nextID)
	}
	raw, err := settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, `"owner@example.com\npaid":`) {
		t.Fatalf("auto seq = %s, want the head to stay on the unconfirmed id", raw)
	}
	if err := svc.RunTokenBankAutoOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if withdraws != 1 {
		t.Fatalf("heartbeat withdraws = %d, want no second debit after the share landed", withdraws)
	}
	raw, err = settings.Get(context.Background(), "token_bank_auto_seq")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"owner@example.com\npaid":2`) && !strings.Contains(raw, `owner@example.com\npaid":2`) {
		t.Fatalf("auto seq = %s, want the retried confirm to close the head and the new share", raw)
	}
}

func TestPullTokenBankForAdmissionShortfallDoesNotDebitAShareLandedBeforeWithdraw(t *testing.T) {
	requestID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 0)
	landedID := llmservice.TokenBankAutoRequestID("hub-auto", "owner@example.com", "paid", 1)
	var withdraws int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/hubs/hub-auto/token-bank/withdraw":
			withdraws++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok", "created": true, "request_id": landedID,
				"amount_micro": 20_000_000, "state": "issued", "hub_id": "hub-auto",
			})
		case "/api/hubs/hub-auto/token-bank/grants":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "state": "bound"})
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
		ModelServiceGroups: []llmservice.ModelServiceGroup{{ID: "paid", Name: "Paid"}},
		Grants: []llmservice.Grant{
			{
				ID: "grant-stuck", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(requestID),
				CreditsTotal: 106.6555, CreditsUsed: 106.6555, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
			{
				ID: "grant-landed", UserID: "user-1", Email: "owner@example.com", ServiceGroupID: "paid",
				Source: llmservice.TokenBankGrantSource, CardID: llmservice.TokenBankGrantCardID(landedID),
				CreditsTotal: 20, Permanent: true,
				StartsAt: now, ExpiresAt: now.AddDate(10, 0, 0), CreatedAt: now,
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), "token_bank_auto_withdraw", `{"enabled":true,"service_group_id":"paid"}`); err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg, settings)
	// The caller measured the card before this 20-credit share was stored.
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"paid"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !pulled || withdraws != 0 {
		t.Fatalf("pulled=%v withdraws=%d, want the share already on the card and no second debit", pulled, withdraws)
	}
	reg, err := llmservice.LoadRegistry(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Grants) != 2 {
		t.Fatalf("grants = %d, want no new share", len(reg.Grants))
	}
}

func TestPullTokenBankForAdmissionShortfallSkipsUnchargedGroup(t *testing.T) {
	var withdraws int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		withdraws++
		w.WriteHeader(http.StatusOK)
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
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"other"}, 0)
	if !errors.Is(err, llmservice.ErrTokenBankGroupNotCharged) {
		t.Fatalf("err=%v, want ErrTokenBankGroupNotCharged", err)
	}
	if pulled || withdraws != 0 {
		t.Fatalf("pulled=%v withdraws=%d, want no pull into a group this request does not charge", pulled, withdraws)
	}
}

func TestPullTokenBankForAdmissionShortfallNothingToWithdraw(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPaymentRequired)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    "insufficient_credits",
			"message": "token bank: nothing to withdraw",
		})
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
	pulled, err := svc.PullTokenBankForAdmissionShortfall(context.Background(), "user-1", "owner@example.com", []string{"Paid"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pulled {
		t.Fatal("empty token bank must not look like a pull")
	}
}
