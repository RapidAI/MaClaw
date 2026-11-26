package cloudagent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
