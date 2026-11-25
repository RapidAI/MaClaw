package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// TestIsolatedBotMessageReachesSrv: a brand-new isolated bot — no prior run,
// its display not yet ensured — must still have its GUI message delivered to
// MaClawSrv. The delivery regression we are guarding against is the message
// dying before the .../messages POST: a silent settle, a parked offline queue
// item, or a bare error that leaves the GUI with nothing admitted.
//
// The srv stub answers the exact endpoints deliverDesktopMessage calls for a
// cloud isolated bot (NodeID==""):
//   POST /api/v1/auth/token          owner bearer exchange (ownerTokenPublic)
//   POST .../bot-threads/route       routing probe (routeDesktopMessage)
//   GET  /api/v1/instances/{id}      instance read (desktopIdentityMetadata)
//   PATCH /api/v1/instances/{id}     metadata rewrite
//   POST /api/v1/instances/{id}/messages   the actual turn post
// The test asserts the messages endpoint is reached at all — that is the bit
// the gap dropped — and that the admission reports an accepted/async run
// rather than an empty, queued, or errored result.
func TestIsolatedBotMessageReachesSrv(t *testing.T) {
	var posted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/bot-threads/route"):
			// No live threads for a fresh bot: route to main.
			_, _ = w.Write([]byte(`{"session":"bot_iso","via":"main"}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			atomic.AddInt32(&posted, 1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_iso","status":"running"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		// A newly created isolated bot: DesktopMode isolated, a real
		// InstanceID, no NodeID (cloud), and no admit/run state seeded.
		Bots:   []Bot{{ID: "bot_iso", Name: "iso", InstanceID: "inst_iso", OwnerUserID: "alice", DesktopMode: desktop.BotDesktopModeIsolated}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)

	admission, err := svc.AdmitDesktopMessageAct(context.Background(), "tenant-a", "alice", "bot_iso", "打开首页", "", "cm-iso-1", "", "")
	if err != nil {
		t.Fatalf("isolated bot message dropped before reaching srv: %v", err)
	}
	if atomic.LoadInt32(&posted) == 0 {
		t.Fatalf("message never reached MaClawSrv: admission=%+v", admission)
	}
	if !admission.Accepted && !admission.Settled && strings.TrimSpace(admission.RunID) == "" && !admission.Queued {
		t.Fatalf("isolated bot message produced no run and no queue: admission=%+v", admission)
	}
}

// TestIsolatedBotMessageOnUnprovisionedNodeIsNotSilentlyDropped pins the fix
// for the T2 drop: an isolated bot pinned to a NodeID whose node-link has no
// srv makes ownerTokenOn -> finishNodeOwnerToken fail with ErrSrv before any
// run is admitted. That "node not ready yet" is transient — the node is
// expected to link or be configured — so the message must park on the node
// queue (WaitKind=node) and retry, not die as a bare error. Before the fix the
// same call returned BotAdmission{}, routeErr and the message was gone.
func TestIsolatedBotMessageOnUnprovisionedNodeIsNotSilentlyDropped(t *testing.T) {
	var posted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages") {
			atomic.AddInt32(&posted, 1)
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_iso","status":"running"}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	settings := &memSettings{}
	raw, err := json.Marshal(record{
		BaseURL:     srv.URL,
		AccessToken: "secret-token",
		AdminSecret: "admin-secret",
		Principals: []ownerPrincipal{{
			HubUserID: "alice", TenantID: "maclaw-tenant", UserID: "user_alice", APIKey: "key-alice", APISecret: "sec-alice",
		}},
		// Isolated bot pinned to a node that has no nodeLink/srv recorded.
		Bots:   []Bot{{ID: "bot_iso", Name: "iso", InstanceID: "inst_iso", OwnerUserID: "alice", NodeID: "node-gone", DesktopMode: desktop.BotDesktopModeIsolated}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)

	admission, err := svc.AdmitDesktopMessageAct(context.Background(), "tenant-a", "alice", "bot_iso", "打开首页", "", "cm-iso-2", "", "")
	// Fixed behavior: the unprovisioned-node token error is transient, so the
	// message is parked on the node queue and retried — it is not dropped. The
	// admit reports a queued admission (err==nil, Queued==true) and nothing is
	// posted to a node that has no srv yet.
	if err != nil {
		t.Fatalf("unprovisioned-node message should park, not error: %v (admission=%+v)", err, admission)
	}
	if !admission.Queued {
		t.Fatalf("unprovisioned-node message was not parked for retry: admission=%+v", admission)
	}
	if atomic.LoadInt32(&posted) != 0 {
		t.Fatalf("message post fired despite unresolved node token: admission=%+v", admission)
	}
}
