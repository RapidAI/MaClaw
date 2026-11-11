package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/localrelay"
)

func postRelay(t *testing.T, h http.HandlerFunc, body map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/relay", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer relay-test-token")
	rec := httptest.NewRecorder()
	h(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	return out
}

func TestLocalRelayHandlerOfflineAndOnline(t *testing.T) {
	t.Setenv("MACLAW_DESKTOP_API_TOKEN", "relay-test-token")
	old := localRelay
	localRelay = &localrelay.Broker{}
	defer func() { localRelay = old }()
	h := PostLocalRelayHandler(nil)

	// Unauthenticated callers are refused.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/desktop-services/relay", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d", rec.Code)
	}

	// No GUI link: the answer says so.
	out := postRelay(t, h, map[string]any{"tenant_id": "t1", "user_id": "u1", "instance_id": "bot-a", "op": "list", "path": "/tmp"})
	if out["ok"] != false || out["code"] != localrelay.CodeOffline {
		t.Fatalf("offline: %v", out)
	}

	// Attach a GUI that allows a temp folder.
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hi"), 0o644)
	agent := &localrelay.Agent{Home: dir, Load: func() localrelay.Settings {
		return localrelay.Settings{FilesEnabled: true, Roots: []localrelay.Root{{Path: dir, Write: true}}}
	}, Ask: func(context.Context, localrelay.Prompt) localrelay.Answer { return localrelay.AnswerOnce }}
	hubSide, guiSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go agent.ServeGUI(ctx, guiSide)
	go localRelay.Attach(ctx, localRelayKey("t1", "u1::desk::bot-a"), "m1", hubSide)
	deadline := time.Now().Add(2 * time.Second)
	for {
		if on, _, _ := localRelay.Online(localRelayKey("t1", "u1")); on {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("link did not come up")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// An isolated bot user id (owner::desk::bot) reaches the owner's link.
	out = postRelay(t, h, map[string]any{"tenant_id": "t1", "user_id": "u1::desk::bot-a", "instance_id": "bot-a", "op": "list", "path": dir})
	if out["ok"] != true {
		t.Fatalf("list: %v", out)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries: %v", out)
	}
	out = postRelay(t, h, map[string]any{"tenant_id": "t1", "user_id": "u1", "instance_id": "bot-a", "op": "grant_to_cloud", "path": filepath.Join(dir, "a.txt")})
	if out["ok"] != true || out["token"] == "" || out["size"].(float64) != 2 {
		t.Fatalf("grant: %v", out)
	}
	// Another person never sees this link.
	out = postRelay(t, h, map[string]any{"tenant_id": "t1", "user_id": "u2", "instance_id": "bot-a", "op": "list", "path": dir})
	if out["code"] != localrelay.CodeOffline {
		t.Fatalf("other user: %v", out)
	}
}
