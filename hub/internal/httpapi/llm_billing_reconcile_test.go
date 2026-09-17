package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
)

func TestReconcileOfficialBillingReservationsReleasesNotFoundAttempt(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"billing attempt not found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: server.URL,
		HubID:        "hub-1",
		MachineToken: "machine-token",
	})})

	system := &testSystemSettingsRepo{values: map[string]string{}}
	sentAt := time.Now().UTC().Add(-officialBillingAttemptNotFoundGrace - time.Minute)
	reg := &llmservice.Registry{
		Grants: []llmservice.Grant{{
			ID:             "card",
			UserID:         "user-1",
			Email:          "user@example.com",
			ServiceGroupID: "official",
			Source:         "new_user_limit_card",
			StartsAt:       sentAt.Add(-time.Hour),
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		}},
		BillingReservations: []llmservice.BillingReservation{{
			RequestID:              "req-lost-1",
			UserID:                 "user-1",
			Email:                  "user@example.com",
			ServiceGroupIDs:        []string{"official"},
			Credits:                12.5,
			ProviderID:             llmservice.MaClawOfficialProviderID,
			BillingGroupMultiplier: 1,
			ExpiresAt:              sentAt.Add(time.Hour),
			CreatedAt:              sentAt,
			SentAt:                 sentAt,
		}},
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	result, err := ReconcileSentOfficialBillingReservations(context.Background(), system)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Released != 1 || result.Scanned != 1 {
		t.Fatalf("result = %+v, want 1 released of 1 scanned", result)
	}
	reloaded, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if got := llmservice.HeldBillingCreditsForServiceGroupsForUserID(reloaded, "user-1", "user@example.com", []string{"official"}, time.Now().UTC()); got != 0 {
		t.Fatalf("held after release = %v, want 0", got)
	}
	// The row is retained as terminal audit evidence rather than deleted.
	if len(reloaded.BillingReservations) != 1 || reloaded.BillingReservations[0].Status != llmservice.BillingReservationUsageUnresolved {
		t.Fatalf("reservations after release = %+v, want one usage_unresolved row", reloaded.BillingReservations)
	}
}

func TestReconcileOfficialBillingReservationsReleasesLegacyRowWithoutGroupMultiplier(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"billing attempt not found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: server.URL,
		HubID:        "hub-1",
		MachineToken: "machine-token",
	})})

	system := &testSystemSettingsRepo{values: map[string]string{}}
	sentAt := time.Now().UTC().Add(-officialBillingAttemptNotFoundGrace - time.Minute)
	reg := &llmservice.Registry{
		BillingReservations: []llmservice.BillingReservation{{
			RequestID:       "req-legacy-1",
			UserID:          "user-1",
			Email:           "user@example.com",
			ServiceGroupIDs: []string{"official"},
			Credits:         7.5,
			ProviderID:      llmservice.MaClawOfficialProviderID,
			// Simulates a crash between mark-sent and detail-freeze (or a row
			// from before the dispatch-identity fields existed).
			BillingGroupMultiplier: 0,
			ExpiresAt:              sentAt.Add(time.Hour),
			CreatedAt:              sentAt,
			SentAt:                 sentAt,
		}},
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	result, err := ReconcileSentOfficialBillingReservations(context.Background(), system)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Released != 1 {
		t.Fatalf("result = %+v, want 1 released for a legacy multiplier-less row", result)
	}
}

func TestReconcileOfficialBillingReservationsKeepsYoungNotFoundAttempt(t *testing.T) {
	previous := GetMaClawModule()
	defer SetMaClawModule(previous)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"billing attempt not found"}`, http.StatusNotFound)
	}))
	defer server.Close()
	SetMaClawModule(&llmservice.MaClawModule{Client: llmservice.NewMaClawProviderClient(llmservice.MaClawProviderConfig{
		HubCenterURL: server.URL,
		HubID:        "hub-1",
		MachineToken: "machine-token",
	})})

	system := &testSystemSettingsRepo{values: map[string]string{}}
	sentAt := time.Now().UTC().Add(-time.Minute)
	reg := &llmservice.Registry{
		BillingReservations: []llmservice.BillingReservation{{
			RequestID:              "req-young-1",
			UserID:                 "user-1",
			Email:                  "user@example.com",
			ServiceGroupIDs:        []string{"official"},
			Credits:                12.5,
			ProviderID:             llmservice.MaClawOfficialProviderID,
			BillingGroupMultiplier: 1,
			ExpiresAt:              sentAt.Add(time.Hour),
			CreatedAt:              sentAt,
			SentAt:                 sentAt,
		}},
	}
	if err := llmservice.SaveRegistry(context.Background(), system, reg); err != nil {
		t.Fatalf("save registry: %v", err)
	}

	result, err := ReconcileSentOfficialBillingReservations(context.Background(), system)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if result.Released != 0 || result.Pending != 1 {
		t.Fatalf("result = %+v, want 0 released / 1 pending", result)
	}
	reloaded, err := llmservice.LoadRegistry(context.Background(), system)
	if err != nil {
		t.Fatalf("reload registry: %v", err)
	}
	if len(reloaded.BillingReservations) != 1 {
		t.Fatalf("reservations after reconcile = %d, want 1", len(reloaded.BillingReservations))
	}
}
