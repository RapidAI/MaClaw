package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatCardDecisionReachesHub(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/decision"):
			gotPath = r.URL.Path
			gotAuth = r.Header.Get("Authorization")
			raw, _ := io.ReadAll(r.Body)
			gotBody = string(raw)
			_, _ = w.Write([]byte(`{"status":"processing","resume":true,"mode":"verify","task_id":"task-a","run_id":"run_42","request_id":"card_1","bot_id":"bot_alice","done":true,"granted":["purchase"]}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/desktop"):
			gotPath = r.URL.Path
			_, _ = w.Write([]byte(`{"bot_id":"bot_alice","task_id":"task-a","run_id":"run_42","lease_id":"lease-a","desktop_mode":"isolated","attach":"watch","wake":true}`))
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
		Bots:   []Bot{{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"}},
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

	body := ChatCardBody(map[string]any{
		"task_id": "task-a", "bot_id": "bot_alice", "choice_ids": []any{"completed"},
		"user_id": "someone-else", "done": true, "resume": true, "granted": []any{"purchase"},
	})
	if _, ok := body["user_id"]; ok || body["done"] != nil || body["resume"] != nil {
		t.Fatalf("caller identity survived: %#v", body)
	}
	out, err := svc.DecideChatCard(context.Background(), "tenant-a", "alice", "bot_alice", "run_42", "card_1", body)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Resume || out.Done || out.Mode != "verify" || out.RunID != "run_42" || out.TaskID != "task-a" || len(out.Granted) != 0 {
		t.Fatalf("decision = %#v", out)
	}
	if !strings.HasSuffix(gotPath, "/api/v1/instances/inst_alice/runs/run_42/interactions/card_1/decision") {
		t.Fatalf("path = %s", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("auth = %q", gotAuth)
	}
	if strings.Contains(gotBody, "someone-else") || strings.Contains(gotBody, "purchase") || strings.Contains(gotBody, "user_id") {
		t.Fatalf("forwarded body = %s", gotBody)
	}
	if _, err := svc.DecideChatCard(context.Background(), "tenant-a", "alice", "bot_other", "run_42", "card_1", body); err == nil {
		t.Fatal("wrong bot was forwarded")
	}
	desk, err := svc.OpenChatCardDesktop(context.Background(), "tenant-a", "alice", "bot_alice", "run_42", "card_1", map[string]any{"bot_id": "bot_alice", "lease_id": "lease-a"})
	if err != nil {
		t.Fatal(err)
	}
	if desk.Wake || desk.Attach != "watch" || desk.BotID != "bot_alice" || desk.LeaseID != "lease-a" || desk.DesktopMode != "isolated" {
		t.Fatalf("desktop = %#v", desk)
	}
	reply := desktopSentenceReply(desktopCommandBody{Metadata: map[string]string{
		"ask_user_question": "下一步？",
		"interaction_json":  `{"id":"card_1","kind":"native_handoff"}`,
	}})
	if reply.AskUserQuestion != "下一步？" || !strings.Contains(reply.InteractionJSON, "card_1") {
		t.Fatalf("older ask fields = %#v", reply)
	}
	older := desktopSentenceReply(desktopCommandBody{Metadata: map[string]string{"ask_user_question": "下一步？"}})
	if older.InteractionJSON != "" || older.AskUserQuestion != "下一步？" {
		t.Fatalf("older srv = %#v", older)
	}
	if !desktopAsking(map[string]string{"interaction_json": `{"id":"card_1"}`}) {
		t.Fatal("a card was treated as a finished turn")
	}
}
