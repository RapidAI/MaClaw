package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestNoneDesktopBotPushesLLMConfigToCloudSrv guards the report that a
// no-computer bot ran an unmanaged model. Such a bot still carries a node pin
// from before it lost its computer; pushing the owner's model config at that
// node left the cloud srv with a stale, empty config, so the turn ran whatever
// the srv defaulted to instead of the Bot-management pin.
func TestNoneDesktopBotPushesLLMConfigToCloudSrv(t *testing.T) {
	const endpoint = "https://hub.example/api/llm/v1"
	const nodeID = "node_dead_beef"

	var (
		mu      sync.Mutex
		configs []map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/config"):
			_, _ = w.Write([]byte(`{"app_config":{"language":"zh"}}`))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/config"):
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				AppConfig map[string]any `json:"app_config"`
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("config body: %v", err)
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			configs = append(configs, body.AppConfig)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
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
		// The bot has no computer but still names the node it used to live on.
		Bots: []Bot{{
			ID: "bot_alice", Name: "test", InstanceID: "inst_alice", OwnerUserID: "alice",
			DesktopMode: "none", NodeID: nodeID,
		}},
		Grants:    []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
		OwnerNode: map[string]string{"alice": nodeID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	svc.HTTP = srv.Client()
	svc.Now = func() time.Time { return time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC) }
	svc.OwnerLLM = &countingOwnerLLM{url: endpoint}

	if _, err := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "北京天气，生成pdf"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	// The node has no srv link in this record, so the only reachable push is
	// the cloud one. Without the fix the cloud config is never written.
	if len(configs) == 0 {
		t.Fatal("no-computer bot pushed no model config to the cloud srv")
	}
	app := configs[len(configs)-1]
	for _, key := range []string{"maclaw_llm_providers", "maclaw_bot_llm_mode"} {
		if _, ok := app[key]; !ok {
			t.Fatalf("cloud config missing %s: %+v", key, app)
		}
	}
	if app["maclaw_llm_url"] != endpoint {
		t.Fatalf("cloud llm url = %v, want %s", app["maclaw_llm_url"], endpoint)
	}
}
