package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

type convSettings struct {
	mu sync.Mutex
	m  map[string]string
}

func (c *convSettings) Get(_ context.Context, key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key], nil
}

func (c *convSettings) Set(_ context.Context, key, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = value
	return nil
}

type convAuth map[string]string // token -> user

func (a convAuth) AuthenticateMachine(_ context.Context, machineID, token string) (*auth.MachinePrincipal, error) {
	if user, ok := a[token]; ok {
		return &auth.MachinePrincipal{TenantID: "tenant_default", UserID: user, MachineID: machineID}, nil
	}
	return nil, errors.New("bad token")
}

func (a convAuth) AuthenticateViewer(context.Context, string) (*auth.ViewerPrincipal, error) {
	return nil, errors.New("no viewer")
}

// Two machines of alice write and read one journal through the route;
// bob's token reads none of it.
func TestBotConversationRouteSyncsClientsOfOnePerson(t *testing.T) {
	settings := &convSettings{m: map[string]string{
		botmgmt.SettingsKey: `{"bots":[{"id":"bot_a","name":"a","instance_id":"i","owner_user_id":"alice"}],"grants":[{"id":"g","scope":"user","target_id":"alice"},{"id":"g2","scope":"user","target_id":"bob"}]}`,
	}}
	h := BotConversationHandler(botmgmt.NewService(settings), convAuth{"tok-alice": "alice", "tok-bob": "bob"})
	call := func(method, target, token, machine string, body any) (int, map[string]any) {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, target, &buf)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Machine-ID", machine)
		rec := httptest.NewRecorder()
		h(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	code, out := call(http.MethodPost, "/api/v1/bots/conversation", "tok-alice", "m1", map[string]any{
		"client_id": "client-A",
		"entries": []map[string]any{{"bot_id": "bot_a", "id": "m-1", "kind": "message", "data": map[string]any{"role": "user", "content": "你好"}}},
	})
	if code != http.StatusOK || out["head"] != float64(1) {
		t.Fatalf("write %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/v1/bots/conversation?after=0", "tok-alice", "m2", nil)
	entries, _ := out["entries"].([]any)
	if code != http.StatusOK || len(entries) != 1 || entries[0].(map[string]any)["origin"] != "client-A" {
		t.Fatalf("read %d %v", code, out)
	}
	code, out = call(http.MethodGet, "/api/v1/bots/conversation?after=0", "tok-bob", "m3", nil)
	entries, _ = out["entries"].([]any)
	if code != http.StatusOK || len(entries) != 0 {
		t.Fatalf("bob read %d %v", code, out)
	}
	if code, _ = call(http.MethodPost, "/api/v1/bots/conversation", "tok-bob", "m3", map[string]any{
		"entries": []map[string]any{{"bot_id": "bot_a", "id": "x", "data": map[string]any{}}},
	}); code != http.StatusNotFound {
		t.Fatalf("bob write to alice's bot: %d", code)
	}
}
