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

// A terminal response emits the same tail the local loop does: the open
// thinking block closes with its full text, stream_done marks the stream
// end, and only then the response frame lands.
func TestStreamResponseSealsThinkingAndEndsStream(t *testing.T) {
	previous := streamCoalesce
	streamCoalesce = 0
	t.Cleanup(func() { streamCoalesce = previous })

	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(raw))
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
	rt.publish(inst, "task-1", "req-1", "thinking", "reasoning", map[string]any{"state": "delta"})
	rt.publish(inst, "task-1", "req-1", "response", "answer", map[string]any{"terminal": map[string]any{"text": "answer"}})

	// A second turn settles the same way minus the thinking close.
	rt.publish(inst, "task-1", "req-2", "round_started", "继续", nil)
	rt.publish(inst, "task-1", "req-2", "response", "again", nil)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 7 {
		t.Fatalf("frames = %d %v", len(bodies), bodies)
	}
	var events, lanes []string
	var cursors []float64
	for _, body := range bodies {
		var frame map[string]any
		if err := json.Unmarshal([]byte(body), &frame); err != nil {
			t.Fatal(err)
		}
		events = append(events, fmtString(frame["event"])+":"+fmtString(frame["state"]))
		lanes = append(lanes, fmtString(frame["lane"]))
		cursors = append(cursors, frame["cursor"].(float64))
	}
	want := []string{"thinking:delta", "thinking:done", "stream_done:", "response:", "round_started:", "stream_done:", "response:"}
	for i, got := range events {
		if got != want[i] {
			t.Fatalf("events = %v", events)
		}
	}
	wantLanes := []string{"reasoning", "reasoning", "control", "control", "control", "control", "control"}
	for i, got := range lanes {
		if got != wantLanes[i] {
			t.Fatalf("lanes = %v", lanes)
		}
	}
	for i := 1; i < len(cursors); i++ {
		if cursors[i] != cursors[i-1]+1 {
			t.Fatalf("cursors = %v", cursors)
		}
	}
	var done map[string]any
	if err := json.Unmarshal([]byte(bodies[1]), &done); err != nil {
		t.Fatal(err)
	}
	if done["text"] != "reasoning" {
		t.Fatalf("thinking done frame = %s", bodies[1])
	}
}
