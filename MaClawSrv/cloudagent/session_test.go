package cloudagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func openRuntime(t *testing.T) *Runtime {
	t.Helper()
	rt, err := Open(t.TempDir(), "admin-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt
}

func putSession(t *testing.T, rt *Runtime, loopback string) Spec {
	t.Helper()
	spec := Spec{
		ID:          newID("cag_"),
		TenantID:    "tenant-a",
		OwnerUserID: "user-a",
		Name:        "session",
		Kind:        KindSession,
		HubOrigin:   "https://hub.example",
		HubLoopback: loopback,
	}
	if err := rt.Put(spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

type capturedCall struct {
	Path   string
	Auth   string
	Secret string
	Agent  string
	Body   string
}

type llmGate struct {
	hits       atomic.Int32
	mu         sync.Mutex
	calls      []capturedCall
	heartbeats []string
	started    chan struct{}
	once       sync.Once
	release    chan struct{}
	reply      string
}

func (g *llmGate) handler(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	if r.URL.Path == "/api/internal/cloud-agents/heartbeat" {
		g.mu.Lock()
		g.heartbeats = append(g.heartbeats, string(raw))
		g.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"enabled":true}`))
		return
	}
	g.hits.Add(1)
	g.mu.Lock()
	g.calls = append(g.calls, capturedCall{
		Path:   r.URL.Path,
		Auth:   r.Header.Get("Authorization"),
		Secret: r.Header.Get("X-MaClaw-Admin-Secret"),
		Agent:  r.Header.Get("X-MaClaw-Cloud-Agent"),
		Body:   string(raw),
	})
	g.mu.Unlock()
	g.once.Do(func() { close(g.started) })
	if g.release != nil {
		<-g.release
	}
	if strings.HasPrefix(strings.TrimSpace(g.reply), "event:") {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	_, _ = w.Write([]byte(g.reply))
}

func newGate(t *testing.T, reply string, block bool) (*llmGate, *httptest.Server, func()) {
	t.Helper()
	g := &llmGate{started: make(chan struct{}), reply: reply}
	// srv.Close must REGISTER before the gate's release: cleanups run LIFO,
	// so the handler parked at <-g.release is always freed before the server
	// waits on outstanding connections — otherwise a failed assertion deadlocks
	// Close instead of failing the test.
	srv := httptest.NewServer(http.HandlerFunc(g.handler))
	t.Cleanup(srv.Close)
	release := func() {}
	if block {
		g.release = make(chan struct{})
		var once sync.Once
		release = func() { once.Do(func() { close(g.release) }) }
		t.Cleanup(release)
	}
	return g, srv, release
}

func send(t *testing.T, rt *Runtime, id, clientID, text string) (requestID, taskID string) {
	t.Helper()
	raw, err := rt.Control(context.Background(), id, "user-a", "cloud_send_message", []byte(`{"client_message_id":"`+clientID+`","text":"`+text+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Accepted  bool   `json:"accepted"`
		RequestID string `json:"request_id"`
		TaskID    string `json:"task_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted || ack.RequestID == "" || ack.TaskID == "" {
		t.Fatalf("ack: %s", raw)
	}
	return ack.RequestID, ack.TaskID
}

func waitCall(t *testing.T, g *llmGate) {
	t.Helper()
	select {
	case <-g.started:
	case <-time.After(2 * time.Minute):
		t.Fatal("session turn did not reach the model")
	}
}

func transcript(t *testing.T, rt *Runtime, id, taskID string) string {
	t.Helper()
	lines, err := rt.Messages(id, taskID)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func TestPutRejectsKindChange(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	spec.Kind = KindCoding
	spec.Name = "renamed"
	if err := rt.Put(spec); !errors.Is(err, errKind) {
		t.Fatalf("kind change err=%v", err)
	}
	raw, err := os.ReadFile(filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), spec.ID, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "renamed") || !strings.Contains(string(raw), `"kind":"session"`) {
		t.Fatalf("meta changed: %s", raw)
	}
}

func TestCustomLLMRejectsHubURL(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	err := rt.SaveLLM(spec.ID, CustomLLM{URL: "http://127.0.0.1:9/api/llm/v1", Key: "custom-secret", Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "官方接口") {
		t.Fatalf("loopback hub url err=%v", err)
	}
	err = rt.SaveLLM(spec.ID, CustomLLM{URL: "https://hub.example/api/llm/v1/chat/completions", Key: "custom-secret", Model: "m"})
	if err == nil {
		t.Fatal("origin hub url was accepted")
	}
	secretPath := filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), spec.ID, "llm.json")
	if _, statErr := os.Stat(secretPath); !os.IsNotExist(statErr) {
		t.Fatalf("llm.json exists after rejection: %v", statErr)
	}
	if err := rt.SaveLLM(spec.ID, CustomLLM{URL: "https://llm.example/v1", Key: "custom-secret", Model: "custom-model"}); err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(filepath.Dir(secretPath), "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(secretPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "custom-secret") {
		t.Fatalf("secret landed in meta.json: %s", meta)
	}
	if !strings.Contains(string(saved), "custom-secret") {
		t.Fatalf("llm.json missing secret: %s", saved)
	}
}

func TestCustomLLMCarriesWireFieldsToConfig(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(spec.ID, CustomLLM{
		URL:           "https://open.bigmodel.cn/api/anthropic",
		Key:           "zhipu-key",
		Model:         "glm-5.3-flash",
		Label:         "智谱编程",
		Protocol:      "anthropic",
		AgentType:     "claude code 2.0",
		ContextLength: 400000,
	}); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	cfg := rt.maclawCfg(inst)
	if cfg.Protocol != "anthropic" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	if cfg.AgentType != "claude code 2.0" || cfg.ContextLength != 400000 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.URL != "https://open.bigmodel.cn/api/anthropic" || cfg.Model != "glm-5.3-flash" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestCustomLLMLegacyFileInfersAnthropicProtocol(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	// llm.json written before the protocol field existed carries none; an
	// Anthropic-shaped endpoint must still resolve to the Messages wire.
	if err := rt.SaveLLM(spec.ID, CustomLLM{URL: "https://open.bigmodel.cn/api/anthropic", Key: "k", Model: "glm-5.3-flash"}); err != nil {
		t.Fatal(err)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Protocol != "anthropic" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	other := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(other.ID, CustomLLM{URL: "https://llm.example/v1", Key: "k", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Protocol != "openai" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	// Desktop providers can carry the dialect on wire_api alone; llm.json
	// saved from one must resolve anthropic the same way agentservice does.
	wire := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(wire.ID, CustomLLM{URL: "https://llm.example/anthropic", Key: "k", Model: "m", WireAPI: "anthropic"}); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(wire.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Protocol != "anthropic" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	google := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(google.ID, CustomLLM{URL: "https://generativelanguage.googleapis.com/v1beta", Key: "k", Model: "gemini-3.8-flash"}); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(google.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Protocol != "google" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	openaiCompat := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(openaiCompat.ID, CustomLLM{URL: "https://generativelanguage.googleapis.com/v1beta/openai", Key: "k", Model: "gemini-3.8-flash"}); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(openaiCompat.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Protocol != "openai" {
		t.Fatalf("protocol = %q", cfg.Protocol)
	}
	// A llm.json pushed before a Zhipu default-model rename resolves the
	// preset model the same way the desktop migration does.
	legacy := putSession(t, rt, "http://127.0.0.1:9")
	if err := rt.SaveLLM(legacy.ID, CustomLLM{URL: "https://open.bigmodel.cn/api/anthropic", Key: "k", Model: "glm-5.2", Label: "智谱编程"}); err != nil {
		t.Fatal(err)
	}
	inst, err = rt.instance(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cfg := rt.maclawCfg(inst); cfg.Model != corelib.ZhipuCodingDefaultModel {
		t.Fatalf("model = %q", cfg.Model)
	}
}

const officialSessionReply = "event: response.completed\n" +
	"data: {\"response\":{\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"cloud-session-ok\"}]}]}}\n\n"

func TestSessionOfficialTurn(t *testing.T) {
	g, srv, _ := newGate(t, officialSessionReply, false)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	other := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-1", "session-turn-alpha")
	again, _ := send(t, rt, spec.ID, "client-1", "session-turn-alpha")
	if again != requestID {
		t.Fatalf("repeated client id request=%s want %s", again, requestID)
	}
	var text string
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		text = transcript(t, rt, spec.ID, taskID)
		if strings.Contains(text, "cloud-session-ok") || strings.Contains(text, "未接通") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(text, "cloud-session-ok") {
		t.Fatalf("transcript=%s calls=%d", text, g.hits.Load())
	}
	hits := g.hits.Load()
	if hits < 1 {
		t.Fatal("model was not called")
	}
	time.Sleep(300 * time.Millisecond)
	if got := g.hits.Load(); got != hits {
		t.Fatalf("repeated client id called the model again: %d -> %d", hits, got)
	}
	g.mu.Lock()
	call := g.calls[0]
	g.mu.Unlock()
	if call.Path != officialChatPath {
		t.Fatalf("path=%s", call.Path)
	}
	if call.Secret != "admin-secret-value" || call.Agent != spec.ID {
		t.Fatalf("headers secret=%q agent=%q", call.Secret, call.Agent)
	}
	if call.Auth != "" {
		t.Fatalf("authorization leaked: %q", call.Auth)
	}
	if !strings.Contains(call.Body, "session-turn-alpha") || !strings.Contains(call.Body, `"auto"`) || !strings.Contains(call.Body, `"input"`) {
		t.Fatalf("body=%s", call.Body)
	}
	meta, err := os.ReadFile(filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), spec.ID, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), "admin-secret-value") || strings.Contains(string(meta), officialKey) {
		t.Fatalf("meta holds a secret: %s", meta)
	}
	if otherText := transcript(t, rt, other.ID, taskID); otherText != "" {
		t.Fatalf("other instance saw the transcript: %s", otherText)
	}
	status, err := rt.TurnStatus(spec.ID, requestID)
	if err != nil || status != "finished" {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

func TestSecondSendWhileRunningIsQueued(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, _ := send(t, rt, spec.ID, "client-hold", "hold-this-turn")
	waitCall(t, g)

	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_send_message", []byte(`{"client_message_id":"client-next","text":"another turn"}`))
	if err != nil {
		t.Fatalf("queued send err=%v", err)
	}
	var ack struct {
		Accepted bool   `json:"accepted"`
		Queued   bool   `json:"queued"`
		QueueID  string `json:"queue_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || !ack.Accepted || !ack.Queued || ack.QueueID == "" {
		t.Fatalf("queued ack=%s err=%v", raw, err)
	}
	// The same client id stays parked: no second row, no second model call.
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_send_message", []byte(`{"client_message_id":"client-next","text":"another turn"}`))
	if err != nil {
		t.Fatalf("queued dedup err=%v", err)
	}
	var dup struct {
		QueueID string `json:"queue_id"`
	}
	if err := json.Unmarshal(raw, &dup); err != nil || dup.QueueID != ack.QueueID {
		t.Fatalf("dedup ack=%s err=%v", raw, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cloud-agents/"+spec.ID+"/control", strings.NewReader(`{"op":"cloud_send_message","body":{"client_message_id":"client-other","text":"via http"},"owner_user_id":"user-a"}`))
	req.SetPathValue("id", spec.ID)
	rec := httptest.NewRecorder()
	rt.ServeControl(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"queued":true`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if g.hits.Load() != 1 {
		t.Fatalf("queued send still called the model: %d", g.hits.Load())
	}
	again, _ := send(t, rt, spec.ID, "client-hold", "hold-this-turn")
	if again != requestID {
		t.Fatalf("retry request=%s want %s", again, requestID)
	}

	release()
	deadline := time.Now().Add(30 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		status, err = rt.TurnStatus(spec.ID, requestID)
		if err == nil && status == "finished" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if status != "finished" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	// The finish drains the queue: both parked turns run in order.
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if g.hits.Load() >= 3 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if g.hits.Load() != 3 {
		t.Fatalf("queued turns did not run after the first finished: %d calls", g.hits.Load())
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok, _ := inst.book.PeekOldest(); !ok {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if _, ok, _ := inst.book.PeekOldest(); ok {
		t.Fatal("queue did not drain")
	}
	deadline = time.Now().Add(5 * time.Second)
	var notice string
	for time.Now().Before(deadline) {
		g.mu.Lock()
		for _, body := range g.heartbeats {
			if strings.Contains(body, `"finished":true`) && strings.Contains(body, requestID) {
				notice = body
			}
		}
		g.mu.Unlock()
		if notice != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if notice == "" {
		t.Fatal("finished turn did not tell Hub")
	}
	if g.hits.Load() != 3 {
		t.Fatalf("finish notice was counted as a model call: %d", g.hits.Load())
	}
	// A fresh send begins once every queued turn has finished.
	deadline = time.Now().Add(30 * time.Second)
	var next string
	var created bool
	for time.Now().Before(deadline) {
		next, created, err = inst.book.Begin("client-after", "", "after-finish", time.Now())
		if err == nil {
			break
		}
		if !errors.Is(err, errBusy) {
			t.Fatalf("follow-up begin err=%v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if err != nil || !created || next == "" || next == requestID {
		t.Fatalf("follow-up request=%s created=%v err=%v", next, created, err)
	}
}

func TestSessionOriginMismatchSkipsModel(t *testing.T) {
	g, srv, _ := newGate(t, `{"choices":[{"message":{"role":"assistant","content":"should-not-run"},"finish_reason":"stop"}]}`, false)
	t.Setenv("MACLAW_HUB_URL", "https://other.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	_, taskID := send(t, rt, spec.ID, "client-mismatch", "do-not-bill")
	deadline := time.Now().Add(30 * time.Second)
	var text string
	for time.Now().Before(deadline) {
		text = transcript(t, rt, spec.ID, taskID)
		if strings.Contains(text, "未接通") {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if !strings.Contains(text, "未接通") {
		t.Fatalf("transcript=%s", text)
	}
	if g.hits.Load() != 0 {
		t.Fatalf("mismatch still called the model %d times", g.hits.Load())
	}
}

func TestSessionRestartAndHeartbeatDoNotReplay(t *testing.T) {
	t.Run("restart", func(t *testing.T) {
		assertInterrupted(t, true)
	})
	t.Run("heartbeat", func(t *testing.T) {
		assertInterrupted(t, false)
	})
}

func assertInterrupted(t *testing.T, restart bool) {
	t.Helper()
	g, srv, release := newGate(t, `{"choices":[{"message":{"role":"assistant","content":"late-model-text"},"finish_reason":"stop"}]}`, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-block", "please-wait")
	waitCall(t, g)
	again, _ := send(t, rt, spec.ID, "client-block", "please-wait")
	if again != requestID {
		t.Fatalf("request id changed: %s", again)
	}
	before := g.hits.Load()
	if before < 1 {
		t.Fatal("blocked turn never called the model")
	}
	if restart {
		if err := rt.Recover(); err != nil {
			t.Fatal(err)
		}
	} else {
		if err := rt.InterruptMissed(time.Now()); err != nil {
			t.Fatal(err)
		}
		if status, err := rt.TurnStatus(spec.ID, requestID); err != nil || status != "running" {
			t.Fatalf("fresh turn status=%s err=%v", status, err)
		}
		if err := rt.InterruptMissed(time.Now().Add(91 * time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	release()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if g.hits.Load() != before {
			t.Fatalf("model calls grew after interrupt: %d -> %d", before, g.hits.Load())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if g.hits.Load() != before {
		t.Fatalf("model calls grew after interrupt: %d -> %d", before, g.hits.Load())
	}
	status, err := rt.TurnStatus(spec.ID, requestID)
	if err != nil || status != "interrupted" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	text := transcript(t, rt, spec.ID, taskID)
	if !strings.Contains(text, "没有跑完") || strings.Contains(text, "late-model-text") {
		t.Fatalf("transcript=%s", text)
	}
}

func TestPulseHonorsHeartbeat(t *testing.T) {
	t.Run("miss", func(t *testing.T) {
		assertPulse(t, false)
	})
	t.Run("disabled", func(t *testing.T) {
		assertPulse(t, true)
	})
}

func assertPulse(t *testing.T, disabled bool) {
	t.Helper()
	g := &llmGate{started: make(chan struct{}), reply: `{"choices":[{"message":{"role":"assistant","content":"late-model-text"},"finish_reason":"stop"}]}`}
	g.release = make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(g.release) }) }
	var beats atomic.Int32
	// Register srv.Close BEFORE release: cleanups run LIFO, so the parked
	// handler is freed before the server waits on outstanding connections.
	// The old order deadlocked Close whenever an assertion fired early.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/internal/cloud-agents/heartbeat" {
			beats.Add(1)
			if disabled {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"ok":true,"enabled":false}`))
				return
			}
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		g.handler(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(release)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-pulse", "please-wait")
	waitCall(t, g)
	now := time.Now()
	rt.pulse(context.Background(), now)
	if beats.Load() < 1 {
		t.Fatal("pulse did not post a heartbeat")
	}
	if disabled {
		assertTurnCancelled(t, rt, spec.ID, requestID, taskID, g)
		release()
		return
	}
	if status, err := rt.TurnStatus(spec.ID, requestID); err != nil || status != "running" {
		t.Fatalf("fresh miss status=%s err=%v", status, err)
	}
	rt.mu.Lock()
	_, live := rt.cancels[requestID]
	rt.mu.Unlock()
	if !live {
		t.Fatal("a failed heartbeat cancelled a fresh turn")
	}
	rt.pulse(context.Background(), now.Add(91*time.Second))
	assertTurnCancelled(t, rt, spec.ID, requestID, taskID, g)
	release()
}

func assertTurnCancelled(t *testing.T, rt *Runtime, id, requestID, taskID string, g *llmGate) {
	t.Helper()
	status, err := rt.TurnStatus(id, requestID)
	if err != nil || status != "interrupted" {
		t.Fatalf("status=%s err=%v", status, err)
	}
	// 871dd51d moved the cancels-entry delete to untrack, which runs when the
	// turn goroutine has fully unwound — cancel itself is synchronous but the
	// removal lands milliseconds later. Poll briefly instead of failing the
	// instant pulse returns.
	deadline := time.Now().Add(8 * time.Second)
	for {
		rt.mu.Lock()
		_, live := rt.cancels[requestID]
		rt.mu.Unlock()
		if !live {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("running turn was not cancelled")
		}
		time.Sleep(20 * time.Millisecond)
	}
	before := g.hits.Load()
	text := transcript(t, rt, id, taskID)
	if !strings.Contains(text, "没有跑完") || strings.Contains(text, "late-model-text") {
		t.Fatalf("transcript=%s", text)
	}
	if g.hits.Load() != before {
		t.Fatalf("model calls grew: %d", g.hits.Load())
	}
}

func TestWatchReturnsInFlightSnapshot(t *testing.T) {
	g, srv, release := newGate(t, `{"choices":[{"message":{"role":"assistant","content":"late-model-text"},"finish_reason":"stop"}]}`, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-watch", "please-wait")
	waitCall(t, g)
	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_watch_task", []byte(`{"task_id":"`+taskID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap liveSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.ProjectPath != "" {
		t.Fatalf("project_path=%q", snap.ProjectPath)
	}
	if snap.SessionKey != "cloud-agent:"+spec.ID+":"+taskID {
		t.Fatalf("session_key=%s", snap.SessionKey)
	}
	if snap.RequestID != requestID {
		t.Fatalf("request_id=%s", snap.RequestID)
	}
	if snap.Cursor == 0 || !strings.Contains(snap.DisplayText, "please-wait") {
		t.Fatalf("snapshot=%s", raw)
	}
	if snap.Parts == nil {
		t.Fatal("parts missing")
	}
	unwatch, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_unwatch_task", []byte(`{"task_id":"`+taskID+`"}`))
	if err != nil || !strings.Contains(string(unwatch), `"ok":true`) {
		t.Fatalf("unwatch=%s err=%v", unwatch, err)
	}
	release()
}

func TestPushConfirmationLivesInsideTerminal(t *testing.T) {
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	inst.meta.HubLoopback = srv.URL
	rt.noteGrant(spec.ID, "grant-1")
	question := "请确认推送"
	rt.publish(inst, "task-1", "req-1", "response", question, map[string]any{
		"terminal": askTerminal(question, "task-1"),
	})
	if len(got) == 0 {
		t.Fatal("no frame posted")
	}
	var frame map[string]any
	if err := json.Unmarshal(got, &frame); err != nil {
		t.Fatal(err)
	}
	if _, ok := frame["ask_user_question"]; ok {
		t.Fatalf("ask fields sit beside terminal: %s", got)
	}
	terminal, _ := frame["terminal"].(map[string]any)
	if terminal == nil {
		t.Fatalf("terminal missing: %s", got)
	}
	if terminal["ask_user_question"] != question || terminal["ask_user_input_type"] != "choice" {
		t.Fatalf("terminal=%v", terminal)
	}
	if !strings.Contains(fmtString(terminal["ask_user_options_json"]), "仅这一次") {
		t.Fatalf("options=%v", terminal["ask_user_options_json"])
	}
	confirmation, _ := terminal["confirmation"].(map[string]any)
	if confirmation == nil || strings.TrimSpace(fmtString(confirmation["id"])) == "" {
		t.Fatalf("confirmation=%v", terminal["confirmation"])
	}
	actions, _ := terminal["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions=%v", terminal["actions"])
	}
}

func TestWatchRestoresPushConfirmation(t *testing.T) {
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	question := "请确认推送"
	rt.publish(inst, "task-1", "req-1", "response", question, map[string]any{
		"terminal": askTerminal(question, "task-1"),
	})
	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_watch_task", []byte(`{"task_id":"task-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if _, ok := snap["ask_user_question"]; ok {
		t.Fatalf("ask fields sit beside the snapshot: %s", raw)
	}
	terminal, _ := snap["terminal"].(map[string]any)
	if terminal == nil || terminal["ask_user_question"] != question {
		t.Fatalf("terminal=%v", snap["terminal"])
	}
	confirmation, _ := terminal["confirmation"].(map[string]any)
	if confirmation == nil || strings.TrimSpace(fmtString(confirmation["id"])) == "" {
		t.Fatalf("confirmation=%v", terminal["confirmation"])
	}
	actions, _ := terminal["actions"].([]any)
	if len(actions) != 3 {
		t.Fatalf("actions=%v", terminal["actions"])
	}
}

func fmtString(value any) string {
	text, _ := value.(string)
	return text
}

func TestMetaRejectsLoadedSecret(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	const key = "provider-key-77ab31"
	if err := rt.SaveLLM(spec.ID, CustomLLM{URL: "https://llm.example/v1", Key: key, Model: "m"}); err != nil {
		t.Fatal(err)
	}
	spec.Name = "name " + key
	if err := rt.Put(spec); err == nil || !strings.Contains(err.Error(), "密钥") {
		t.Fatalf("put err=%v", err)
	}
	meta, err := os.ReadFile(filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), spec.ID, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(meta), key) || strings.Contains(string(meta), officialKey) {
		t.Fatalf("meta holds a secret: %s", meta)
	}
	kept, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kept.meta.Name, key) {
		t.Fatalf("memory kept the secret name: %s", kept.meta.Name)
	}
}

func TestStopInterruptsRunningTurnAndDeleteTombstones(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, created, err := inst.book.Begin("client-1", "task_1", "跑一轮", time.Now())
	if err != nil || !created {
		t.Fatalf("begin: created=%v err=%v", created, err)
	}
	if _, running := inst.book.Running(); running != requestID {
		t.Fatalf("running = %q", running)
	}
	if err := rt.Stop(spec.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	var status string
	if err := inst.book.db.QueryRow(`SELECT status FROM turns WHERE request_id = ?`, requestID).Scan(&status); err != nil || status != "interrupted" {
		t.Fatalf("turn status = %q %v", status, err)
	}
	if err := rt.Stop("cag_missing"); !errors.Is(err, errNotFound) {
		t.Fatalf("stop missing = %v", err)
	}
	if err := rt.Delete(spec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := rt.instance(spec.ID); !errors.Is(err, errNotFound) {
		t.Fatalf("instance after delete: %v", err)
	}
	tombs, err := filepath.Glob(filepath.Join(rt.root, slug(spec.TenantID), slug(spec.OwnerUserID), ".deleted-"+spec.ID+"-*"))
	if err != nil || len(tombs) == 0 {
		t.Fatalf("tombstone dir: %v %d", err, len(tombs))
	}
}

func TestServeFileOnlyServesProducedPaths(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	inst := &instance{dir: dir, meta: Spec{Kind: KindSession}, book: b}
	rt := &Runtime{}

	workspace := filepath.Join(dir, "workspace")
	for name, body := range map[string]string{"report.txt": "ok-bytes", "stray.txt": "not-produced"} {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	requestID, _, err := b.Begin("c1", "task_1", "写报告", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(requestID, "报告好了", false, []string{"report.txt"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	raw, err := rt.serveFile(inst, "report.txt")
	if err != nil {
		t.Fatalf("serve produced: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["name"] != "report.txt" {
		t.Fatalf("name %v", body["name"])
	}
	got, err := base64.StdEncoding.DecodeString(fmt.Sprint(body["data_base64"]))
	if err != nil || string(got) != "ok-bytes" {
		t.Fatalf("bytes %q %v", got, err)
	}
	if _, err := rt.serveFile(inst, "stray.txt"); err == nil {
		t.Fatal("an unrecorded file must not be served")
	}
}

// The response frame is published before Finish writes messages.files, and
// the desktop does not retry a failed auto-fetch. The staged key has to be
// servable in that gap, then the book takes over once Finish stores it.
func TestServeFileServesStagedPathBeforeFinish(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	inst := &instance{dir: dir, meta: Spec{ID: "cag_stage", Kind: KindSession}, book: b}
	rt := &Runtime{}

	workspace := filepath.Join(dir, "workspace")
	bodies := map[string]string{
		"report.txt": "ok-bytes",
		"extra.txt":  "extra-bytes",
		"stray.txt":  "not-produced",
		"held.txt":   "held-bytes",
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range bodies {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	requestID, _, err := b.Begin("c1", "task_1", "写报告", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.serveFile(inst, "report.txt"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("unstaged before finish: %v", err)
	}
	inst.stageProduced([]string{"report.txt", "extra.txt", "", "  "})
	if !inst.stagedHas(" report.txt ") {
		t.Fatal("staged lookup did not trim")
	}
	assertServedFile(t, rt, inst, "report.txt", "ok-bytes")
	if _, err := rt.serveFile(inst, "stray.txt"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("stray: %v", err)
	}
	inst.stageProduced([]string{"../git.json"})
	if _, err := rt.serveFile(inst, "../git.json"); err == nil || !strings.Contains(err.Error(), "路径超出工作区") {
		t.Fatalf("staged escape: %v", err)
	}

	rt.finishTurn(inst, requestID, "task_1", "报告好了", false, []string{"report.txt"}, time.Now())
	if inst.stagedHas("report.txt") {
		t.Fatal("successful finish left the stored path staged")
	}
	if !inst.stagedHas("extra.txt") {
		t.Fatal("finish cleared a path it did not store")
	}
	if !b.producedPaths()["report.txt"] || b.producedPaths()["extra.txt"] {
		t.Fatalf("book keys = %#v", b.producedPaths())
	}
	assertServedFile(t, rt, inst, "report.txt", "ok-bytes")
	assertServedFile(t, rt, inst, "extra.txt", "extra-bytes")
	if _, err := rt.serveFile(inst, "stray.txt"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("stray after finish: %v", err)
	}

	heldID, _, err := b.Begin("c2", "task_2", "再写", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	inst.stageProduced([]string{"held.txt"})
	if _, err := b.interrupt(time.Now(), "中断", time.Time{}); err != nil {
		t.Fatal(err)
	}
	rt.finishTurn(inst, heldID, "task_2", "晚了", false, []string{"held.txt"}, time.Now())
	if !inst.stagedHas("held.txt") {
		t.Fatal("failed finish dropped the staged path")
	}
	if b.producedPaths()["held.txt"] {
		t.Fatal("interrupted turn recorded the file")
	}
	assertServedFile(t, rt, inst, "held.txt", "held-bytes")
}

func TestRetargetStagedFollowsMovedKey(t *testing.T) {
	inst := &instance{}
	inst.stageProduced([]string{"snake/game.cpp", "stay.txt"})
	inst.retargetStaged(map[string]string{"snake": "snake.pre-checkout"})
	if inst.stagedHas("snake/game.cpp") {
		t.Fatal("old staged key still fetchable")
	}
	if !inst.stagedHas("snake.pre-checkout/game.cpp") || !inst.stagedHas("stay.txt") {
		t.Fatal("staged keys did not follow the book rename")
	}
	inst.retargetStaged(nil)
	if !inst.stagedHas("stay.txt") {
		t.Fatal("empty rename dropped a staged key")
	}
}

// The desktop reloads history when the response frame arrives, which is
// before finishTurn. The assistant row and its files have to be in the book
// while the turn is still the running lock, and a fetch must succeed from
// that row alone.
func TestServeFileServesSealedReplyBeforeFinish(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	inst := &instance{dir: dir, meta: Spec{ID: "cag_seal", Kind: KindSession}, book: b}
	rt := &Runtime{}
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "weather.pdf"), []byte("%PDF-seal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "stray.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	requestID, _, err := b.Begin("c1", "task_1", "生成pdf", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.serveFile(inst, "weather.pdf"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("before seal: %v", err)
	}
	stored, err := b.sealReply(requestID, "天气已写入 PDF", false, []string{"weather.pdf"}, time.Now())
	if err != nil || !stored {
		t.Fatalf("seal %v %v", err, stored)
	}
	status, err := b.Status(requestID)
	if err != nil || status != "running" {
		t.Fatalf("status after seal %q %v", status, err)
	}
	if _, _, err := b.Begin("c2", "task_1", "另一轮", time.Now()); !errors.Is(err, errBusy) {
		t.Fatalf("seal released the running lock: %v", err)
	}
	lines, _, files, requestIDs, _, err := b.history("task_1")
	if err != nil || len(lines) != 2 || lines[1] != "assistant\n天气已写入 PDF" {
		t.Fatalf("history %#v %v", lines, err)
	}
	if len(requestIDs) != 2 || requestIDs[0] != requestID || requestIDs[1] != requestID {
		t.Fatalf("request ids %#v", requestIDs)
	}
	if len(files) != 2 || len(files[1]) != 1 || files[1][0] != "weather.pdf" {
		t.Fatalf("files %#v", files)
	}
	assertServedFile(t, rt, inst, "weather.pdf", "%PDF-seal")
	if _, err := rt.serveFile(inst, "stray.txt"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("stray after seal: %v", err)
	}
	again, err := b.sealReply(requestID, "另一份正文", false, []string{"stray.txt"}, time.Now())
	if err != nil || !again {
		t.Fatalf("second seal %v %v", err, again)
	}
	lines, _, files, requestIDs, _, err = b.history("task_1")
	if err != nil || len(lines) != 2 || lines[1] != "assistant\n天气已写入 PDF" || files[1][0] != "weather.pdf" {
		t.Fatalf("second seal rewrote history %#v %#v %v", lines, files, err)
	}
	wrote, err := b.Finish(requestID, "晚到的另一份正文", false, nil, time.Now())
	if err != nil || !wrote {
		t.Fatalf("finish %v %v", err, wrote)
	}
	lines, err = b.History("task_1")
	if err != nil || len(lines) != 2 || lines[1] != "assistant\n天气已写入 PDF" {
		t.Fatalf("finish duplicated %#v %v", lines, err)
	}
	if status, err = b.Status(requestID); err != nil || status != "finished" {
		t.Fatalf("status after finish %q %v", status, err)
	}
	assertServedFile(t, rt, inst, "weather.pdf", "%PDF-seal")

	heldID, _, err := b.Begin("c3", "task_2", "再生成", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "held.pdf"), []byte("%PDF-held"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.sealReply(heldID, "已生成", false, []string{"held.pdf"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.interrupt(time.Now(), "已停止", time.Time{}); err != nil {
		t.Fatal(err)
	}
	wrote, err = b.Finish(heldID, "晚了", false, []string{"held.pdf"}, time.Now())
	if err != nil || wrote {
		t.Fatalf("finish after stop %v %v", err, wrote)
	}
	if !b.producedPaths()["held.pdf"] {
		t.Fatal("stop after seal dropped the stored file")
	}
	heldLines, err := b.History("task_2")
	if err != nil || len(heldLines) != 3 || heldLines[1] != "assistant\n已生成" {
		t.Fatalf("stopped history %#v %v", heldLines, err)
	}
	assertServedFile(t, rt, inst, "held.pdf", "%PDF-held")
}

func assertServedFile(t *testing.T, rt *Runtime, inst *instance, rel, want string) {
	t.Helper()
	raw, err := rt.serveFile(inst, rel)
	if err != nil {
		t.Fatalf("serve %s: %v", rel, err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(fmt.Sprint(body["data_base64"]))
	if err != nil || string(got) != want {
		t.Fatalf("serve %s bytes %q %v", rel, got, err)
	}
}

// A coding turn that started before ConnectGit keeps writing beside the
// checkout. The book still names snake/game.cpp, and the checkout itself
// can be empty. Fetch and export have to see the stranded file.
func TestServeFileFindsFilesStrandedByMidTurnGitConnect(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	inst := &instance{dir: dir, meta: Spec{ID: "cag_strand", Kind: KindCoding}, book: b}
	rt := &Runtime{}

	repo := filepath.Join(dir, "workspace", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const token = "strand-forge-token"
	if err := writeGit(dir, gitSecret{Token: token, Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
		t.Fatal(err)
	}
	snake := filepath.Join(dir, "workspace", "snake")
	if err := os.MkdirAll(snake, 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "int game(){return 1;}\n"
	if err := os.WriteFile(filepath.Join(snake, "game.cpp"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workspace", "stray.txt"), []byte("not-produced"), 0o644); err != nil {
		t.Fatal(err)
	}
	requestID, _, err := b.Begin("c1", "task_1", "写蛇", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(requestID, "写好了", false, []string{"snake/game.cpp", "gone.cpp"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	raw, err := rt.serveFile(inst, "snake/game.cpp")
	if err != nil {
		t.Fatalf("serve stranded: %v", err)
	}
	var served map[string]any
	if err := json.Unmarshal(raw, &served); err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(fmt.Sprint(served["data_base64"]))
	if err != nil || string(got) != body {
		t.Fatalf("bytes %q %v", got, err)
	}
	if _, err := rt.serveFile(inst, "stray.txt"); err == nil || !strings.Contains(err.Error(), "没有找到这个文件") {
		t.Fatalf("unrecorded stranded file err=%v", err)
	}
	if _, err := rt.serveFile(inst, filepath.Join("..", "git.json")); err == nil || strings.Contains(err.Error(), token) {
		t.Fatalf("git.json err=%v", err)
	}
	if _, err := rt.serveFile(inst, "gone.cpp"); err == nil || !strings.Contains(err.Error(), "这个文件已经不在了") {
		t.Fatalf("missing recorded file err=%v", err)
	}
	fetched, err := rt.fetchFile(inst, "snake/game.cpp")
	if err != nil || !strings.Contains(string(fetched), "int game()") {
		t.Fatalf("fetch stranded: %s %v", fetched, err)
	}

	packed, err := rt.browsePackDir(inst, "/")
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	var pack struct {
		Data string `json:"data_base64"`
	}
	if err := json.Unmarshal(packed, &pack); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(pack.Data)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	found := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(hdr.Name, "git.json") {
			t.Fatalf("pack leaked %s", hdr.Name)
		}
		if hdr.Name == "snake/game.cpp" {
			raw, err := io.ReadAll(tr)
			if err != nil || string(raw) != body {
				t.Fatalf("packed %q %v", raw, err)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("export omitted snake/game.cpp")
	}
}

// ConnectGit parks a colliding deliverable as <name>.pre-checkout and rewrites
// the book key in the same publication as git.json. Fetch then uses the path
// the file landed at. An empty checkout must not download as a successful
// empty archive.
func TestConnectGitRetargetsParkedArtifact(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	inst := &instance{dir: dir, meta: Spec{ID: "cag_park", Kind: KindCoding}, book: b}
	rt := &Runtime{}
	workspace := filepath.Join(dir, "workspace")
	repo := filepath.Join(workspace, "repo")
	const body = "agent-bytes\n"
	const cloneBody = "clone-bytes\n"
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "snake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "snake", "game.cpp"), []byte(cloneBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(cloneBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "snake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "snake", "game.cpp"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	requestID, _, err := b.Begin("c1", "task_1", "写蛇", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(requestID, "写好了", false, []string{"snake/game.cpp", "README.md"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	renames := mergeWorkspaceLoose(workspace, repo, "", inst.meta.ID)
	if err := b.retargetProducedPaths(renames); err != nil {
		t.Fatal(err)
	}
	if err := writeGit(dir, gitSecret{Token: "park-token", Clone: "https://github.com/acme/repo.git", Repo: "acme/repo"}); err != nil {
		t.Fatal(err)
	}
	if !b.producedPaths()["snake/game.cpp.pre-checkout"] || !b.producedPaths()["README.md.pre-checkout"] {
		t.Fatalf("book keys = %#v", b.producedPaths())
	}
	if _, err := rt.serveFile(inst, "snake/game.cpp"); err == nil {
		t.Fatal("old card path still served the clone file")
	}

	for _, rel := range []string{"snake/game.cpp.pre-checkout", "README.md.pre-checkout"} {
		raw, err := rt.serveFile(inst, rel)
		if err != nil {
			t.Fatalf("serve %s: %v", rel, err)
		}
		var served map[string]any
		if err := json.Unmarshal(raw, &served); err != nil {
			t.Fatal(err)
		}
		got, err := base64.StdEncoding.DecodeString(fmt.Sprint(served["data_base64"]))
		if err != nil || string(got) != body {
			t.Fatalf("%s bytes %q %v", rel, got, err)
		}
	}
	packed, err := rt.browsePackDir(inst, "/")
	if err != nil {
		t.Fatalf("pack parked: %v", err)
	}
	var pack struct {
		Data string `json:"data_base64"`
	}
	if err := json.Unmarshal(packed, &pack); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(pack.Data)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	gotNames := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeReg {
			raw, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			gotNames[hdr.Name] = string(raw)
		}
	}
	if gotNames["snake/game.cpp.pre-checkout"] != body || gotNames["README.md.pre-checkout"] != body {
		t.Fatalf("export missed parked files: %#v", gotNames)
	}
	if gotNames["snake/game.cpp"] != cloneBody || gotNames["README.md"] != cloneBody {
		t.Fatalf("export replaced clone bytes: %#v", gotNames)
	}

	empty := t.TempDir()
	emptyBook, err := openBook(empty)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = emptyBook.Close() })
	emptyInst := &instance{dir: empty, meta: Spec{ID: "cag_empty", Kind: KindSession}, book: emptyBook}
	if err := os.MkdirAll(filepath.Join(empty, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.browsePackDir(emptyInst, "/"); err == nil || !strings.Contains(err.Error(), "没有可导出的文件") {
		t.Fatalf("empty pack err=%v", err)
	}
}

func TestRetargetFollowsParkedDirectory(t *testing.T) {
	dir := t.TempDir()
	b, err := openBook(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	workspace := filepath.Join(dir, "workspace")
	repo := filepath.Join(workspace, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "snake"), []byte("clone-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "snake"), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "int game;\n"
	if err := os.WriteFile(filepath.Join(workspace, "snake", "game.cpp"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	requestID, _, err := b.Begin("c1", "task_1", "写蛇", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Finish(requestID, "写好了", false, []string{"snake/game.cpp"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	renames := mergeWorkspaceLoose(workspace, repo, "", "cag_dir")
	if err := b.retargetProducedPaths(renames); err != nil {
		t.Fatal(err)
	}
	if !b.producedPaths()["snake.pre-checkout/game.cpp"] || b.producedPaths()["snake/game.cpp"] {
		t.Fatalf("book keys = %#v renames=%#v", b.producedPaths(), renames)
	}
}

// The reported path is a coding turn that finished before any repository was
// connected, then ConnectGit. The card still names snake/game.cpp and
// README.md. The clone already has both, so publication parks the deliverables
// and rewrites the card. Fetch and export must follow that card, and a
// workspace with no regular file must not download as an empty archive.
func TestConnectGitPublicationServesAndExportsParkedArtifact(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("git is not installed")
	}
	bare := seedBare(t)
	pointCloneAt(t, bare)
	rt := openRuntime(t)
	spec := putKind(t, rt, KindCoding)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(instanceDir(rt, spec), "workspace")
	const body = "int game() { return 1; }\n"
	for _, rel := range []string{"snake/game.cpp", "README.md"} {
		path := filepath.Join(workspace, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	taskRaw, err := rt.Control(context.Background(), spec.ID, spec.OwnerUserID, "cloud_create_task", []byte(`{"title":"写蛇"}`))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(taskRaw, &created); err != nil || created.TaskID == "" {
		t.Fatalf("task: %s", taskRaw)
	}
	requestID, _, err := inst.book.Begin("c1", created.TaskID, "写贪吃蛇", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inst.book.Finish(requestID, "写好了", false, []string{"snake/game.cpp", "README.md"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := commitBareFile(t, bare, "snake/game.cpp", "clone-bytes\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.ConnectGit(context.Background(), spec.ID, demoConnect()); err != nil {
		t.Fatal(err)
	}
	card := publishedFiles(t, rt, spec, created.TaskID)
	if len(card) != 2 || !containsAll(card, "snake/game.cpp.pre-checkout", "README.md.pre-checkout") {
		t.Fatalf("card = %#v", card)
	}
	for _, rel := range card {
		raw, err := rt.Control(context.Background(), spec.ID, spec.OwnerUserID, "cloud_fetch_file", []byte(`{"path":"`+rel+`"}`))
		if err != nil {
			t.Fatalf("fetch %s: %v", rel, err)
		}
		var served struct {
			Data string `json:"data_base64"`
		}
		if err := json.Unmarshal(raw, &served); err != nil {
			t.Fatal(err)
		}
		got, err := base64.StdEncoding.DecodeString(served.Data)
		if err != nil || string(got) != body {
			t.Fatalf("%s bytes %q %v", rel, got, err)
		}
	}
	if _, err := rt.Control(context.Background(), spec.ID, spec.OwnerUserID, "cloud_fetch_file", []byte(`{"path":"snake/game.cpp"}`)); err == nil {
		t.Fatal("old card path still fetched the clone file")
	}
	packed, err := rt.Control(context.Background(), spec.ID, spec.OwnerUserID, "cloud_pack_dir", []byte(`{"path":"/"}`))
	if err != nil {
		t.Fatal(err)
	}
	names := unpackTarGz(t, packed)
	for _, rel := range card {
		if names[rel] != body {
			t.Fatalf("export missed %s: %#v", rel, names)
		}
	}
	if names["README.md"] == body || names["snake/game.cpp"] == body || !strings.Contains(names["snake/game.cpp"], "clone-bytes") {
		t.Fatalf("export used the parked bytes as the clone file: %#v", names)
	}
	empty := putKind(t, rt, KindCoding)
	if _, err := rt.Control(context.Background(), empty.ID, empty.OwnerUserID, "cloud_pack_dir", []byte(`{"path":"/"}`)); err == nil || !strings.Contains(err.Error(), "没有可导出的文件") {
		t.Fatalf("empty export err=%v", err)
	}
}

func containsAll(got []string, want ...string) bool {
	have := map[string]bool{}
	for _, item := range got {
		have[item] = true
	}
	for _, item := range want {
		if !have[item] {
			return false
		}
	}
	return true
}

func commitBareFile(t *testing.T, bare, name, body string) error {
	t.Helper()
	work := t.TempDir()
	run := func(dir string, args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Seed", "GIT_AUTHOR_EMAIL=seed@example.com", "GIT_COMMITTER_NAME=Seed", "GIT_COMMITTER_EMAIL=seed@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	if err := run(work, "clone", bare, "."); err != nil {
		return err
	}
	path := filepath.Join(work, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	if err := run(work, "add", name); err != nil {
		return err
	}
	if err := run(work, "commit", "-m", "add "+name); err != nil {
		return err
	}
	return run(work, "push", "origin", "HEAD:main")
}

func publishedFiles(t *testing.T, rt *Runtime, spec Spec, taskID string) []string {
	t.Helper()
	raw, err := rt.Control(context.Background(), spec.ID, spec.OwnerUserID, "cloud_get_task", []byte(`{"task_id":"`+taskID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Files [][]string `json:"files"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, row := range body.Files {
		out = append(out, row...)
	}
	return out
}

func unpackTarGz(t *testing.T, packed json.RawMessage) map[string]string {
	t.Helper()
	var pack struct {
		Data string `json:"data_base64"`
	}
	if err := json.Unmarshal(packed, &pack); err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(pack.Data)
	if err != nil {
		t.Fatal(err)
	}
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != 0 {
			continue
		}
		raw, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		out[hdr.Name] = string(raw)
	}
}

func waitQueueEmpty(t *testing.T, rt *Runtime, instanceID, taskID string) {
	t.Helper()
	inst, err := rt.instance(instanceID)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, parked, err := inst.book.PeekOldest()
		if err == nil && !parked {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("queued row did not drain; transcript=%s", transcript(t, rt, instanceID, taskID))
}

// Admission and cancellation are separate control paths. Tracking a row that
// stop already settled must immediately cancel its not-yet-started context.
func TestTrackTurnHonoursAlreadyTerminalRow(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, created, err := inst.book.Begin("client-terminal-admit", "task-terminal-admit", "turn", time.Now())
	if err != nil || !created {
		t.Fatalf("begin request=%s created=%v err=%v", requestID, created, err)
	}
	if ids, err := inst.book.CancelRunning("task-terminal-admit", time.Now()); err != nil || len(ids) != 1 {
		t.Fatalf("cancel ids=%v err=%v", ids, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt.trackTurn(inst, requestID, cancel)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("terminal row was tracked without cancelling its context")
	}
	rt.untrack(requestID)
}

// An interrupted turn does not finish through the normal drain path. A later
// send must still join the durable tail and kick the idle queue back up in
// FIFO order instead of remaining fenced behind the older parked rows.
func TestSendRevivesIdleDurableQueue(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-revive-first", "first turn")
	waitCall(t, g)
	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-revive-second","task_id":"`+taskID+`","text":"second turn"}`)
	queuedAck(t, raw)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := inst.book.interrupt(time.Now(), heartbeatNote, time.Time{})
	if err != nil || len(ids) != 1 {
		t.Fatalf("interrupt ids=%v err=%v", ids, err)
	}
	rt.cancelIDs(ids)
	release()
	if status, err := inst.book.Status(requestID); err != nil || status != "interrupted" {
		t.Fatalf("first status=%s err=%v", status, err)
	}

	raw = sendRaw(t, rt, spec.ID, `{"client_message_id":"client-revive-third","task_id":"`+taskID+`","text":"third turn"}`)
	queuedAck(t, raw)
	waitHits(t, g, 3)
	waitQueueEmpty(t, rt, spec.ID, taskID)
	text := transcript(t, rt, spec.ID, taskID)
	if !strings.Contains(text, "second turn") || !strings.Contains(text, "third turn") {
		t.Fatalf("transcript=%s", text)
	}
}

// Stopping the instance interrupts the live row without discarding parked
// input. Resume is the explicit point where that durable input may continue,
// after the interrupted goroutine has unwound.
func TestResumeDrainsQueueAfterInstanceStop(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	_, taskID := send(t, rt, spec.ID, "client-resume-first", "first turn")
	waitCall(t, g)
	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-resume-second","task_id":"`+taskID+`","text":"second turn"}`)
	queuedAck(t, raw)
	if err := rt.Stop(spec.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := rt.Resume(spec.ID); err != nil {
		t.Fatalf("resume: %v", err)
	}
	release()
	waitHits(t, g, 2)
	waitQueueEmpty(t, rt, spec.ID, taskID)
}

// Stopping the live row must not strand the user's parked input. The next
// durable queue row is promoted only after the cancelled turn has unwound.
func TestStopTaskDrainsQueuedSendAfterCancelledTurn(t *testing.T) {
	g, srv, release := newGate(t, officialSessionReply, true)
	t.Setenv("MACLAW_HUB_URL", "https://hub.example")
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	requestID, taskID := send(t, rt, spec.ID, "client-stop-first", "first turn")
	waitCall(t, g)

	raw := sendRaw(t, rt, spec.ID, `{"client_message_id":"client-stop-next","task_id":"`+taskID+`","text":"second turn"}`)
	if _, queuedTask := queuedAck(t, raw); queuedTask != taskID {
		t.Fatalf("queued task=%s want %s", queuedTask, taskID)
	}
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_stop_task", []byte(`{"task_id":"`+taskID+`"}`)); err != nil {
		t.Fatalf("stop task: %v", err)
	}
	release()
	waitHits(t, g, 2)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, parked, peekErr := inst.book.PeekOldest()
		status, statusErr := inst.book.Status(requestID)
		if peekErr == nil && !parked && statusErr == nil && status == "cancelled" {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	text := transcript(t, rt, spec.ID, taskID)
	t.Fatalf("queued row did not drain after stop; transcript=%s", text)
}

// Finish bookkeeping is durable before the Hub beat. A dead or slow Hub may
// retry in the background, but it must not hold back the next parked turn.
func TestFinishTurnDoesNotBlockOnHubHeartbeatRetry(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/internal/cloud-agents/heartbeat" {
			<-release
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true,"enabled":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(unblock)
	rt := openRuntime(t)
	spec := putSession(t, rt, srv.URL)
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	requestID, created, err := inst.book.Begin("client-finish-async", "task-finish-async", "finish", time.Now())
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin request=%s created=%v err=%v", requestID, created, err)
	}

	done := make(chan struct{})
	go func() {
		rt.finishTurn(inst, requestID, "task-finish-async", "done", false, nil, time.Now())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("finishTurn blocked behind the Hub heartbeat retry")
	}
	unblock()
}

// 手机页：cloud_get_task 窗口化返回尾部并按请求把过程行挂到最后一行，
// cloud_list_tasks 带状态，cloud_delete_task 先取消在跑的回合再整链清除。
func TestCloudGetTaskWindowEventsAndDelete(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	// 每回合一个互不重叠的 10 秒窗：用户行在窗头、回复在窗尾，时间序天然成立。
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	runTurn := func(taskID, text, reply string, at time.Time) string {
		t.Helper()
		requestID, created, err := inst.book.Begin("", taskID, text, at)
		if err != nil || !created || requestID == "" {
			t.Fatalf("begin %s: %v %v", taskID, err, created)
		}
		wrote, err := inst.book.Finish(requestID, reply, false, nil, at.Add(time.Second))
		if err != nil || !wrote {
			t.Fatalf("finish %s: %v %v", taskID, err, wrote)
		}
		return requestID
	}
	var lastReq string
	for i := 0; i < 3; i++ {
		lastReq = runTurn("task-a", fmt.Sprintf("第%d步", i+1), fmt.Sprintf("完成%d", i+1), base.Add(time.Duration(i)*10*time.Second))
	}
	if err := inst.book.AppendTurnEvent(lastReq, "task-a", "progress", "读取 README", base.Add(31*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.AppendTurnEvent(lastReq, "task-a", "progress", "改写标题", base.Add(32*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := inst.book.SealThinking(lastReq, "task-a", "先看结构", base.Add(33*time.Second)); err != nil {
		t.Fatal(err)
	}
	runTurn("task-b", "另一件事", "搞定", base.Add(40*time.Second))

	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_list_tasks", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Tasks []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Tasks) != 2 {
		t.Fatalf("list: %s", raw)
	}
	for _, row := range list.Tasks {
		if row.Status != "finished" {
			t.Fatalf("list status %s = %q", row.ID, row.Status)
		}
	}

	// 窗口=4：回合 2、3。过程行挂在回合 3 的助手行（该请求在本窗口的最后一行）。
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_get_task", []byte(`{"task_id":"task-a","history_limit":4}`))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Messages     []string `json:"messages"`
		MessageTotal int      `json:"message_total"`
		Events       [][]struct {
			Seq  int    `json:"seq"`
			Kind string `json:"kind"`
			Text string `json:"text"`
		} `json:"events"`
		TurnStatus string `json:"turn_status"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if detail.MessageTotal != 6 || len(detail.Messages) != 4 || len(detail.Events) != 4 {
		t.Fatalf("window total=%d msgs=%d events=%d", detail.MessageTotal, len(detail.Messages), len(detail.Events))
	}
	if detail.Messages[2] != "user\n第3步" || detail.Messages[3] != "assistant\n完成3" {
		t.Fatalf("window rows %#v", detail.Messages)
	}
	if len(detail.Events[3]) != 3 || detail.Events[3][0].Text != "读取 README" ||
		detail.Events[3][1].Text != "改写标题" || detail.Events[3][2].Text != "先看结构" {
		t.Fatalf("aligned events %#v", detail.Events[3])
	}
	if len(detail.Events[0]) != 0 || len(detail.Events[2]) != 0 {
		t.Fatalf("stray events %#v", detail.Events)
	}
	if detail.TurnStatus != "finished" {
		t.Fatalf("turn status %q", detail.TurnStatus)
	}

	// 删除带在跑回合与排队输入的任务：先取消，再全链清除。
	if _, _, err := inst.book.Begin("", "task-b", "第三件事", base.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := inst.book.Enqueue("", "task-a", "排队的输入", base.Add(70*time.Second)); err != nil {
		t.Fatal(err)
	}
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_delete_task", []byte(`{"task_id":"task-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	var deleted struct {
		OK        bool     `json:"ok"`
		Cancelled []string `json:"cancelled"`
	}
	if err := json.Unmarshal(raw, &deleted); err != nil || !deleted.OK {
		t.Fatalf("delete: %s", raw)
	}
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_list_tasks", []byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Tasks) != 1 || list.Tasks[0].ID != "task-b" {
		t.Fatalf("list after delete: %s", raw)
	}
	raw, err = rt.Control(context.Background(), spec.ID, "user-a", "cloud_get_task", []byte(`{"task_id":"task-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &detail); err != nil || len(detail.Messages) != 0 || detail.MessageTotal != 0 || detail.TurnStatus != "" {
		t.Fatalf("deleted task detail: %s", raw)
	}
	var queued int
	if err := inst.book.db.QueryRow(`SELECT COUNT(*) FROM queued_turns WHERE task_id = 'task-a'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("queued rows after delete %d %v", queued, err)
	}
	// 在跑的回合被取消（book 状态），删除本身不让位给任何残留 running。
	status, err := inst.book.latestTurn("task-b")
	if err != nil || status != "running" {
		t.Fatalf("task-b turn after deleting task-a: %q %v", status, err)
	}
}

// 请求方的 history_limit 必须在服务端钳制：无界的窗口会直接冲进 hub 的
// 1MB op 中继，被静默截断成解析失败的 JSON。
func TestCloudGetTaskClampsHistoryLimit(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := inst.book.EnsureTask("task-big", "长任务"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 450; i++ {
		stamp := formatTS(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Millisecond))
		if _, err := inst.book.db.Exec(
			`INSERT INTO messages (id, task_id, role, content, as_error, created_at) VALUES (?, ?, 'user', ?, 0, ?)`,
			fmt.Sprintf("msg-%04d", i), "task-big", fmt.Sprintf("行%d", i), stamp,
		); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_get_task", []byte(`{"task_id":"task-big","history_limit":100000}`))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Messages     []string `json:"messages"`
		MessageTotal int      `json:"message_total"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Messages) != cloudHistoryWindowMax || detail.MessageTotal != 450 {
		t.Fatalf("clamped window %d/%d", len(detail.Messages), detail.MessageTotal)
	}
	if detail.Messages[0] != "user\n行50" || detail.Messages[len(detail.Messages)-1] != "user\n行449" {
		t.Fatalf("window ends %#v / %#v", detail.Messages[0], detail.Messages[len(detail.Messages)-1])
	}
}

// 过程行回放与 live lane 同规：一个回合几百条进度只回末尾 40 条，否则一
// 个长编码回合就能把 cloud_get_task 顶过 hub 的 1MB 中继。
func TestCloudGetTaskReplaysLastEventsOnly(t *testing.T) {
	rt := openRuntime(t)
	spec := putSession(t, rt, "http://127.0.0.1:9")
	inst, err := rt.instance(spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	requestID, created, err := inst.book.Begin("", "task-a", "长回合", base)
	if err != nil || !created || requestID == "" {
		t.Fatalf("begin %v %v", err, created)
	}
	for i := 0; i < 45; i++ {
		if err := inst.book.AppendTurnEvent(requestID, "task-a", "progress", fmt.Sprintf("步骤%d", i), base.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := rt.Control(context.Background(), spec.ID, "user-a", "cloud_get_task", []byte(`{"task_id":"task-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Events [][]struct {
			Seq  int    `json:"seq"`
			Text string `json:"text"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatal(err)
	}
	// 回合未结束，唯一一行是用户行；过程行按请求挂在这最后一行上。
	if len(detail.Events) != 1 {
		t.Fatalf("events rows %d", len(detail.Events))
	}
	list := detail.Events[0]
	if len(list) != cloudTurnEventsMax {
		t.Fatalf("replayed events %d", len(list))
	}
	if list[0].Text != "步骤5" || list[len(list)-1].Text != "步骤44" {
		t.Fatalf("replay ends %#v / %#v", list[0], list[len(list)-1])
	}
	if list[0].Seq != 6 {
		t.Fatalf("first replayed seq %d", list[0].Seq)
	}
}
