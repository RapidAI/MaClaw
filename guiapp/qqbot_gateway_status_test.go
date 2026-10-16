package guiapp

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
)

func TestQQDisablePublishesDisconnectedBeforeStop(t *testing.T) {
	app := &App{}
	app.configSnap.Store(&corelib.AppConfig{
		QQBotEnabled:   false,
		QQBotAppID:     "1020",
		QQBotAppSecret: "secret",
	})

	m := newQQBotGatewayManager(app)
	m.gateway = qqbot.NewGateway(qqbot.Config{AppID: "1020", AppSecret: "secret"}, nil)
	m.status = gatewayConnectionStatusConnected

	started := make(chan struct{})
	release := make(chan struct{})
	m.stopHook = func(*qqbot.Gateway) error {
		close(started)
		<-release
		return nil
	}
	var published string
	m.statusNotify = func(status string) {
		published = status
	}

	done := make(chan struct{})
	go func() {
		m.SyncFromConfig()
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("gateway stop was not started")
	}
	if published != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("status published before stop returned = %q, want disconnected", published)
	}
	if got := m.Status(); got != gatewayConnectionStatusDisconnected.String() {
		t.Fatalf("status while stop is still running = %q, want disconnected", got)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SyncFromConfig did not return after stop finished")
	}
}
