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

// TestNoneDesktopBotWithMissingNodeSrvDoesNotPostToCloud guards the instance
// boundary: a bot created on a node must not be reposted to the cloud srv when
// that node link disappears. The cloud srv does not have that instance and
// would return 404; posting there hides the real configuration failure.
func TestNoneDesktopBotWithMissingNodeSrvDoesNotPostToCloud(t *testing.T) {
	const endpoint = "https://hub.example/api/llm/v1"
	const nodeID = "node_dead_beef"

	var (
		mu       sync.Mutex
		configs  []map[string]any
		messages int
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
			mu.Lock()
			messages++
			mu.Unlock()
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

	_, postErr := svc.PostMessage(context.Background(), "tenant-a", "alice", "bot_alice", "北京天气，生成pdf")
	if postErr == nil || !strings.Contains(postErr.Error(), "这个节点还没有 srv") {
		t.Fatalf("post err = %v", postErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(configs) != 0 || messages != 0 {
		t.Fatalf("the missing-node bot wrote the cloud srv configs=%v messages=%d", configs, messages)
	}
}
