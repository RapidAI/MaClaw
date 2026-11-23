package cloudagent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestStreamPublishesViewerFrameWithoutProjectPath(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	var secrets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
		secrets = append(secrets, r.Header.Get("X-MaClaw-Admin-Secret"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	spec.HubLoopback = srv.URL
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt.noteGrant(spec.ID, "grant-cloud")
	rt.publish(inst, "task-1", "req-1", "progress", "running", nil)
	rt.publish(inst, "task-1", "req-1", "progress", "running", nil)
	rt.publish(inst, "task-1", "req-1", "token", "hi", nil)
	rt.publish(inst, "task-1", "req-1", "round_started", "改 README", map[string]any{"display_text": "改 README"})

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("frames = %d %v", len(bodies), bodies)
	}
	for _, secret := range secrets {
		if secret != "admin-secret-value" {
			t.Fatalf("secret = %q", secret)
		}
	}
	var progress map[string]any
	if err := json.Unmarshal([]byte(bodies[0]), &progress); err != nil {
		t.Fatal(err)
	}
	if progress["grant_id"] != "grant-cloud" || progress["session_key"] != "cloud-agent:"+spec.ID+":task-1" || progress["project_path"] != "" || progress["cursor"] != float64(1) {
		t.Fatalf("progress = %s", bodies[0])
	}
	var token map[string]any
	if err := json.Unmarshal([]byte(bodies[1]), &token); err != nil {
		t.Fatal(err)
	}
	if token["event"] != "token" || token["text"] != "hi" || token["cursor"] != float64(2) || token["proto"] != float64(2) || token["project_path"] != "" {
		t.Fatalf("token = %s", bodies[1])
	}
	if strings.Contains(bodies[1], "ai-assistant-foreground-round-started") {
		t.Fatalf("foreground round leaked: %s", bodies[1])
	}
	var started map[string]any
	if err := json.Unmarshal([]byte(bodies[2]), &started); err != nil {
		t.Fatal(err)
	}
	if started["display_text"] != "〔云端〕改 README" || started["cursor"] != float64(3) {
		t.Fatalf("round = %s", bodies[2])
	}
}
