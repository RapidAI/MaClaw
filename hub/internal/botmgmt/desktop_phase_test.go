package botmgmt

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPlanPhaseIsMessageMetadataAndAttentionStaysWithTheBot(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("MACLAW_BOT_LOG_DIR", logDir)
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/token":
			_, _ = w.Write([]byte(`{"access_token":"bearer-alice","principal":{"user_id":"user_alice"}}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			writeInstanceSettings(w, r)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/api/v1/instances/"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/messages"):
			raw, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(raw))
			switch len(bodies) {
			case 1:
				_, _ = w.Write([]byte(`{"message":{"content":"安排：先看首页。","metadata":{"ask_user_input_type":"secret","ask_user_secret_name":"SITE_PASSWORD","ask_user_question":"保存登录密码"}},"attention_reason":""}`))
			case 2:
				_, _ = w.Write([]byte(`{"message":{"content":"需要支付"},"desktop_handoff":true,"attention_reason":"payment_confirm"}`))
			default:
				_, _ = w.Write([]byte(`{"message":{"content":"继续看首页"}}`))
			}
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
		Bots: []Bot{
			{ID: "bot_alice", Name: "duty", InstanceID: "inst_alice", OwnerUserID: "alice"},
			{ID: "bot_other", Name: "other", InstanceID: "inst_other", OwnerUserID: "alice"},
		},
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
	svc.Desktop = &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	ctx := context.Background()

	reply, err := svc.PostMessagePhase(ctx, "tenant-a", "alice", "bot_alice", "先看首页", "plan")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodies[0], `"bot_phase":"plan"`) || strings.Contains(bodies[0], "s3cret-value") {
		t.Fatalf("plan body=%s", bodies[0])
	}
	if reply.Handoff || reply.AttentionReason != "" || reply.AskUserSecretName != "SITE_PASSWORD" || reply.AskUserInputType != "secret" {
		t.Fatalf("plan reply=%#v", reply)
	}
	if svc.DesktopAttention("tenant-a", "alice", "bot_alice") != "" {
		t.Fatal("plan stored an attention reason")
	}

	paid, err := svc.PostMessagePhase(ctx, "tenant-a", "alice", "bot_alice", "按这个安排执行", "execute")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bodies[1], `"bot_phase":"execute"`) || !paid.Handoff || paid.AttentionReason != "payment_confirm" {
		t.Fatalf("execute reply=%#v body=%s", paid, bodies[1])
	}
	if svc.DesktopAttention("tenant-a", "alice", "bot_alice") != "payment_confirm" {
		t.Fatal("payment attention was not kept for the next poll")
	}
	if svc.DesktopAttention("tenant-a", "alice", "bot_other") != "" {
		t.Fatal("another bot saw this handoff reason")
	}
	_, control, err := svc.DesktopWatch(ctx, "tenant-a", "alice", "bot_alice")
	if err != nil || !control {
		t.Fatalf("watch control=%v err=%v", control, err)
	}

	plain, err := svc.PostMessage(ctx, "tenant-a", "alice", "bot_alice", "继续看")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Handoff || plain.AttentionReason != "" || strings.Contains(bodies[2], "bot_phase") {
		t.Fatalf("plain reply=%#v body=%s", plain, bodies[2])
	}
	if svc.DesktopAttention("tenant-a", "alice", "bot_alice") != "" {
		t.Fatal("a finished reply left the attention reason up")
	}
	logged, err := os.ReadFile(filepath.Join(logDir, "bot_alice.log"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(logged)
	for _, stage := range []string{"hub.post_begin", "hub.open_desktop", "hub.owner_token", "hub.read_instance", "hub.patch_instance", "hub.post_message", "hub.post_end"} {
		if !strings.Contains(text, "stage="+stage) {
			t.Fatalf("bot log missing %s\n%s", stage, text)
		}
	}
	if strings.Contains(text, "secret-token") || strings.Contains(text, "bearer-alice") || strings.Contains(text, "key-alice") || strings.Contains(text, "admin-secret") {
		t.Fatalf("bot log leaked a credential\n%s", text)
	}
	if !strings.Contains(bodies[0], `"bot_id":"bot_alice"`) || strings.Contains(bodies[2], "bot_phase") {
		t.Fatalf("bot id metadata missing or plain message gained a phase: %s", bodies[2])
	}
}

func TestReleaseUserDesktopViewStopsOnlyAnIdleDesktop(t *testing.T) {
	settings := &memSettings{}
	if err := settings.Set(context.Background(), storageKey("tenant-a"), botViewRecordJSON(t)); err != nil {
		t.Fatal(err)
	}
	svc := NewService(settings)
	desk := &desktopCounter{url: "http://dockerd.example:6081/vnc.html?autoconnect=1"}
	svc.Desktop = desk
	ctx := context.Background()

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 1 {
		t.Fatalf("idle release stop=%d err=%v", desk.stop, err)
	}

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	svc.beginDesktopUse("tenant-a", "alice", "inst_alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 1 {
		t.Fatalf("release during open stop=%d err=%v", desk.stop, err)
	}
	svc.endDesktopUse("tenant-a", "alice", "inst_alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 2 {
		t.Fatalf("release after open stop=%d err=%v", desk.stop, err)
	}

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	svc.noteDesktopHeld("tenant-a", "alice", "bot_alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 2 {
		t.Fatalf("release during login stop=%d err=%v", desk.stop, err)
	}
	svc.clearDesktopHeld("tenant-a", "alice", "bot_alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 3 {
		t.Fatalf("release after login stop=%d err=%v", desk.stop, err)
	}

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	svc.keepDesktopWithoutKeyboard("tenant-a", "alice", "bot_alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 3 {
		t.Fatalf("release while the login pin is held stop=%d err=%v", desk.stop, err)
	}
	svc.clearDesktopHeld("tenant-a", "alice", "bot_alice")

	if _, _, err := svc.HoldDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil {
		t.Fatal(err)
	}
	svc.NoteDesktopAdminView("tenant-a", "alice")
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 3 {
		t.Fatalf("release during an admin hold stop=%d err=%v", desk.stop, err)
	}

	svc.Now = func() time.Time { return time.Now().Add(AdminDesktopViewHold + time.Minute) }
	key := desktopViewKey("tenant-a", "alice")
	svc.mu.Lock()
	if svc.desktopOpenInstance == nil {
		svc.desktopOpenInstance = map[string]map[string]int{}
	}
	svc.desktopOpenInstance[key] = map[string]int{"inst_other": 1}
	if svc.desktopOpening != nil {
		delete(svc.desktopOpening, key)
	}
	svc.mu.Unlock()
	if err := svc.ReleaseUserDesktopView(ctx, "tenant-a", "alice", "bot_alice"); err != nil || desk.stop != 3 {
		t.Fatalf("release while another instance is using the desktop stop=%d err=%v", desk.stop, err)
	}
}
