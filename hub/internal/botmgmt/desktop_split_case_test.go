package botmgmt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDesktopBotSplitCases(t *testing.T) {
	t.Run("admit_does_not_open", func(t *testing.T) {
		for _, isolated := range []bool{false, true} {
			name := "shared"
			mode := ""
			if isolated {
				name = "isolated"
				mode = desktop.BotDesktopModeIsolated
			}
			t.Run(name, func(t *testing.T) {
				var gate sync.Mutex
				ready := false
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch {
					case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
						_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
					case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/runs/"):
						gate.Lock()
						goNow := ready
						gate.Unlock()
						if !goNow {
							_, _ = w.Write([]byte(`{"id":"run_admit","status":"running"}`))
							return
						}
						if isolated {
							_, _ = w.Write([]byte(`{"id":"run_admit","status":"succeeded","desktop_handoff":true,"attention_reason":"login_wall","message":{"content":"需要登录"}}`))
							return
						}
						_, _ = w.Write([]byte(`{"id":"run_admit","status":"succeeded","message":{"content":"页面已打开"}}`))
					case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
						writeInstanceSettings(w, r)
					case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
						w.WriteHeader(http.StatusOK)
					case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
						if r.Header.Get("Prefer") != "respond-async" {
							http.Error(w, "prefer", http.StatusBadRequest)
							return
						}
						w.WriteHeader(http.StatusAccepted)
						_, _ = w.Write([]byte(`{"async":true,"run":{"id":"run_admit","status":"running"}}`))
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
					Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice", DesktopMode: mode}},
					Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := settings.Set(context.Background(), storageKey("tenant-a"), string(raw)); err != nil {
					t.Fatal(err)
				}
				svc := NewService(settings)
				svc.HTTP = srv.Client()
				svc.admitPollInterval = 10 * time.Millisecond
				svc.admitReadyGrace = 30 * time.Millisecond
				desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
				svc.Desktop = desk

				admission, err := svc.AdmitDesktopMessage(context.Background(), "tenant-a", "alice", "bot_alice", "打开首页", "execute", "")
				if err != nil {
					t.Fatal(err)
				}
				if !admission.Accepted || admission.RunID != "run_admit" || desk.open != 0 || desk.stop != 0 {
					t.Fatalf("admission open=%d stop=%d admission=%#v", desk.open, desk.stop, admission)
				}
				gate.Lock()
				ready = true
				gate.Unlock()
				deadline := time.Now().Add(2 * time.Second)
				for {
					_, done, err := svc.DesktopRunResult(context.Background(), "tenant-a", "alice", "bot_alice", admission.RunID)
					if err != nil {
						t.Fatal(err)
					}
					if done {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("run result did not arrive")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if desk.open != 0 || desk.stop != 0 {
					t.Fatalf("the admit opened the desktop open=%d stop=%d", desk.open, desk.stop)
				}
			})
		}
	})
}
