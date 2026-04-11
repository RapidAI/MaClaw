package im

import (
	"context"
	"sync"
	"testing"
	"time"
)

// mockPlugin is a test plugin.
type mockPlugin struct {
	name    string
	handler MessageHandler
	started bool
	stopped bool
}

func (m *mockPlugin) Name() string                                                    { return m.name }
func (m *mockPlugin) Start(ctx context.Context) error                                 { m.started = true; return nil }
func (m *mockPlugin) Stop(ctx context.Context) error                                  { m.stopped = true; return nil }
func (m *mockPlugin) OnMessage(handler MessageHandler)                                { m.handler = handler }
func (m *mockPlugin) SendText(ctx context.Context, target UserTarget, text string) error { return nil }
func (m *mockPlugin) SendMarkdown(ctx context.Context, target UserTarget, md string) error { return nil }
func (m *mockPlugin) Capabilities() Capabilities {
	return Capabilities{SupportsMarkdown: true, MaxTextLength: 4096}
}

// simulateIncoming simulates an incoming message from this plugin.
func (m *mockPlugin) simulateIncoming(msg IncomingMessage) {
	if m.handler != nil {
		m.handler(msg)
	}
}

func TestRouter_RegisterAndPlugins(t *testing.T) {
	r := NewRouter()
	p1 := &mockPlugin{name: "feishu"}
	p2 := &mockPlugin{name: "dingtalk"}

	if err := r.Register(p1); err != nil {
		t.Fatalf("register feishu: %v", err)
	}
	if err := r.Register(p2); err != nil {
		t.Fatalf("register dingtalk: %v", err)
	}

	names := r.Plugins()
	if len(names) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(names))
	}

	// Duplicate registration should fail
	if err := r.Register(&mockPlugin{name: "feishu"}); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRouter_MessageRouting(t *testing.T) {
	r := NewRouter()
	p := &mockPlugin{name: "test"}
	r.Register(p)

	var received IncomingMessage
	var wg sync.WaitGroup
	wg.Add(1)
	r.OnMessage(func(msg IncomingMessage) {
		received = msg
		wg.Done()
	})

	p.simulateIncoming(IncomingMessage{
		Platform: "test",
		Text:     "hello",
	})

	wg.Wait()
	if received.Text != "hello" {
		t.Errorf("expected 'hello', got %q", received.Text)
	}
	if received.Platform != "test" {
		t.Errorf("expected platform 'test', got %q", received.Platform)
	}
}

func TestRouter_SendText(t *testing.T) {
	r := NewRouter()
	p := &mockPlugin{name: "feishu"}
	r.Register(p)

	err := r.SendText(context.Background(), "feishu", UserTarget{PlatformUID: "u1"}, "hi")
	if err != nil {
		t.Errorf("send text: %v", err)
	}

	err = r.SendText(context.Background(), "unknown", UserTarget{PlatformUID: "u1"}, "hi")
	if err == nil {
		t.Error("expected error for unknown plugin")
	}
}

func TestRouter_StartStopAll(t *testing.T) {
	r := NewRouter()
	p1 := &mockPlugin{name: "a"}
	p2 := &mockPlugin{name: "b"}
	r.Register(p1)
	r.Register(p2)

	r.StartAll(context.Background())
	if !p1.started || !p2.started {
		t.Error("expected all plugins started")
	}

	r.StopAll(context.Background())
	if !p1.stopped || !p2.stopped {
		t.Error("expected all plugins stopped")
	}
}

func TestRouter_MultiplePluginMessages(t *testing.T) {
	r := NewRouter()
	feishu := &mockPlugin{name: "feishu"}
	dingtalk := &mockPlugin{name: "dingtalk"}
	r.Register(feishu)
	r.Register(dingtalk)

	var mu sync.Mutex
	var messages []IncomingMessage
	r.OnMessage(func(msg IncomingMessage) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
	})

	feishu.simulateIncoming(IncomingMessage{Platform: "feishu", Text: "from feishu"})
	dingtalk.simulateIncoming(IncomingMessage{Platform: "dingtalk", Text: "from dingtalk"})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}
