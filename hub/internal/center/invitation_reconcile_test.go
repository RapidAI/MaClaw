package center

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/config"
)

type countingInvitationReconciler struct {
	calls chan struct{}
}

func (c *countingInvitationReconciler) ReconcileInvitationCodes(context.Context) error {
	c.calls <- struct{}{}
	return nil
}

func TestReconcileInvitationCodesDoesNotRunOnEveryHeartbeat(t *testing.T) {
	reconciler := &countingInvitationReconciler{calls: make(chan struct{}, 2)}
	svc := NewService(nil, nil)
	svc.SetInvitationCodeReconciler(reconciler)

	svc.reconcileInvitationCodes(context.Background())
	select {
	case <-reconciler.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("first reconcile did not run")
	}

	svc.reconcileInvitationCodes(context.Background())
	select {
	case <-reconciler.calls:
		t.Fatal("invitation codes were reconciled again inside the interval")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestSyncInvitationCodesRequiresEveryCenterNode(t *testing.T) {
	var qualityHits atomic.Int32
	var failSecond atomic.Bool
	failSecond.Store(true)

	readReplicate := func(r *http.Request) bool {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode sync body: %v", err)
			return false
		}
		replicate, _ := payload["replicate"].(bool)
		return replicate
	}
	quality := func(w http.ResponseWriter, score int) {
		qualityHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quality_score":` + strconv.Itoa(score) + `,"routable":true,"service_status":"healthy"}`))
	}

	var gotFirst, gotSecond atomic.Bool
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			quality(w, 99)
			return
		}
		gotFirst.Store(readReplicate(r))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			quality(w, 10)
			return
		}
		gotSecond.Store(readReplicate(r))
		if failSecond.Load() {
			http.Error(w, `{"message":"node down"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer second.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = first.URL
	cfg.Center.BaseURLs = []string{first.URL, second.URL}
	settings := newFakeSettingsRepo()
	bare := NewService(cfg, newFakeSettingsRepo())
	accepted, _, err := bare.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	if accepted || err != nil {
		t.Fatalf("unregistered sync accepted=%v err=%v, want skipped", accepted, err)
	}

	svc := NewService(cfg, settings)
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered:  true,
		HubID:       "hub_invite",
		HubSecret:   "secret",
		LastBaseURL: first.URL,
	})); err != nil {
		t.Fatalf("save registration: %v", err)
	}

	accepted, _, err = svc.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	if accepted || err != nil {
		t.Fatalf("partial sync accepted=%v err=%v, want retry without failing the heartbeat interval", accepted, err)
	}
	if qualityHits.Load() != 2 {
		t.Fatalf("quality probes = %d, want one per node", qualityHits.Load())
	}
	if !gotFirst.Load() || gotSecond.Load() {
		t.Fatalf("replicate first=%v second=%v, want only the first successful node to publish", gotFirst.Load(), gotSecond.Load())
	}

	accepted, _, err = svc.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	if accepted || err != nil {
		t.Fatalf("cached partial sync accepted=%v err=%v", accepted, err)
	}
	if qualityHits.Load() != 2 {
		t.Fatalf("quality probes = %d, want the reconcile to reuse the first probe", qualityHits.Load())
	}

	failSecond.Store(false)
	accepted, _, err = svc.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	if !accepted || err != nil {
		t.Fatalf("full sync accepted=%v err=%v", accepted, err)
	}
	if qualityHits.Load() != 2 {
		t.Fatalf("quality probes = %d, want the cached order for the successful retry", qualityHits.Load())
	}
}

func TestInvitationSyncDoesNotRedialCoolingNode(t *testing.T) {
	var firstHits, secondHits atomic.Int32
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":99,"routable":true}`))
			return
		}
		firstHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/client/quality" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"quality_score":10,"routable":true}`))
			return
		}
		secondHits.Add(1)
		time.Sleep(time.Second)
	}))
	defer second.Close()

	cfg := config.Default()
	cfg.Center.Enabled = true
	cfg.Center.BaseURL = first.URL
	cfg.Center.BaseURLs = []string{first.URL, second.URL}
	settings := newFakeSettingsRepo()
	if err := settings.Set(context.Background(), systemKeyCenterRegistration, mustJSON(registrationRecord{
		Registered:  true,
		HubID:       "hub_invite",
		HubSecret:   "secret",
		LastBaseURL: first.URL,
	})); err != nil {
		t.Fatalf("save registration: %v", err)
	}
	svc := NewService(cfg, settings)
	svc.client.Timeout = 200 * time.Millisecond

	accepted, _, err := svc.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	if accepted || err != nil {
		t.Fatalf("partial sync accepted=%v err=%v", accepted, err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("hits first=%d second=%d, want both nodes once", firstHits.Load(), secondHits.Load())
	}

	started := time.Now()
	accepted, _, err = svc.SyncInvitationCodesToCenter(context.Background(), []string{"PNZPES758F"}, "vantagics")
	elapsed := time.Since(started)
	if accepted || err != nil {
		t.Fatalf("cooled sync accepted=%v err=%v", accepted, err)
	}
	if firstHits.Load() != 1 || secondHits.Load() != 1 {
		t.Fatalf("hits first=%d second=%d, want the healthy node cached and the down node skipped", firstHits.Load(), secondHits.Load())
	}
	if elapsed > time.Second {
		t.Fatalf("cooled sync took %s", elapsed)
	}
}
