package guiapp

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/messenger"
)

func TestMessengerSyncKeepsSendErrorWhileListenerStaysUp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := &corelib.AppConfig{
		MessengerEnabled:         true,
		MessengerPageID:          "page-1",
		MessengerPageAccessToken: "page-token",
		MessengerVerifyToken:     "verify-token",
		MessengerAppSecret:       "app-secret",
		MessengerListenHost:      "127.0.0.1",
		MessengerListenPort:      port,
	}
	app := &App{}
	app.configSnap.Store(cfg)

	account := messengerAccountFromConfig(*cfg)
	gw := messenger.NewGateway(account, nil)
	if err := gw.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	m := newMessengerGatewayManager(app)
	m.gateway = gw
	m.listenKey = account.ListenKey()
	m.status = gatewayConnectionStatusConnected
	const sendErr = "facebook messenger send failed: invalid token"
	m.lastErr = sendErr
	t.Cleanup(func() {
		_ = gw.Stop()
		m.mu.Lock()
		other := m.gateway
		m.mu.Unlock()
		if other != nil && other != gw {
			_ = other.Stop()
		}
	})

	// A non-bind field change still goes through the live-listener path.
	cfg.MessengerPageAccessToken = "page-token-2"
	m.SyncFromConfig()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastErr != sendErr {
		t.Fatalf("lastErr = %q, want the Graph send error kept", m.lastErr)
	}
	if m.gateway != gw {
		t.Fatal("same listen key replaced the running gateway")
	}
	if !m.gateway.Running() {
		t.Fatal("listener stopped")
	}
}

func TestMessengerSendResultClearsErrorOnlyWhileConnected(t *testing.T) {
	m := newMessengerGatewayManager(&App{})
	m.status = gatewayConnectionStatusConnected
	m.lastErr = "facebook messenger send failed: invalid token"
	m.noteSendResult(nil)
	if got := m.connectionError(); got != "" {
		t.Fatalf("connectionError after a successful send = %q", got)
	}

	m.status = gatewayConnectionStatusError
	m.lastErr = "bind: address already in use"
	m.noteSendResult(fmt.Errorf("facebook messenger send failed: invalid token"))
	if got := m.connectionError(); got != "bind: address already in use" {
		t.Fatalf("connectionError = %q, want the listener error kept", got)
	}
}
