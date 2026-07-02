package main

import (
	"testing"
	"time"
)

func newTestAIAssistantApp() (*App, *RemoteHubClient) {
	app := &App{}
	manager := NewRemoteSessionManager(app)
	app.remoteSessions = manager
	client := NewRemoteHubClient(app, manager)
	manager.SetHubClient(client)
	return app, client
}

func TestIsAIAssistantReadyRequiresWarmupAndIMHandler(t *testing.T) {
	app, client := newTestAIAssistantApp()

	if app.IsAIAssistantReady() {
		t.Fatal("expected assistant to be unready before warmup")
	}

	app.interactionInfraDone.Store(true)
	app.warmupDone.Store(true)
	if app.IsAIAssistantReady() {
		t.Fatal("expected assistant to stay unready until IM handler is preinitialized")
	}

	client.ensureIMHandler()
	if !app.IsAIAssistantReady() {
		t.Fatal("expected assistant to become ready after warmup, interaction infra, and IM handler init")
	}
}

func TestGetAIAssistantInitStatusTracksPreheatLifecycle(t *testing.T) {
	app, client := newTestAIAssistantApp()

	if got := app.GetAIAssistantInitStatus(); got != "warming" {
		t.Fatalf("status without warmup = %q, want %q", got, "warming")
	}

	app.warmupDone.Store(true)
	if got := app.GetAIAssistantInitStatus(); got != "loading" {
		t.Fatalf("status without IM handler = %q, want %q", got, "loading")
	}

	app.interactionInfraDone.Store(true)
	if got := app.GetAIAssistantInitStatus(); got != "loading" {
		t.Fatalf("status without IM handler after infra ready = %q, want %q", got, "loading")
	}

	client.ensureIMHandler()
	if got := app.GetAIAssistantInitStatus(); got != "ready" {
		t.Fatalf("status after full preheat = %q, want %q", got, "ready")
	}
}

func TestAIAssistantReadySelfHealsMissingHubClientAfterWarmup(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	defer func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	}()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)
	app.warmupDone.Store(true)

	if app.remoteSessions.GetHubClient() != nil {
		t.Fatal("test setup expected missing hub client")
	}
	if !app.IsAIAssistantReady() {
		t.Fatal("expected readiness check to create a local/degraded hub client after warmup")
	}
	client := app.remoteSessions.GetHubClient()
	if client == nil {
		t.Fatal("expected local/degraded hub client to be created")
	}
	if client.imHandler == nil {
		t.Fatal("expected IM handler to be preinitialized")
	}
	if got := app.GetAIAssistantInitStatus(); got != "ready" {
		t.Fatalf("status after self-heal = %q, want %q", got, "ready")
	}
}

func TestAIAssistantControlBindingsSelfHealMissingHubClient(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	defer func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	}()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)
	app.warmupDone.Store(true)

	if got := app.ListBackgroundLoops(); len(got) != 0 {
		t.Fatalf("background loops before any loop = %#v, want empty", got)
	}
	if app.remoteSessions.GetHubClient() != nil {
		t.Fatal("read-only background loop listing should not initialize hub client")
	}
	if err := app.ClearAIAssistantHistory(); err != nil {
		t.Fatalf("ClearAIAssistantHistory self-heal failed: %v", err)
	}
	client := app.remoteSessions.GetHubClient()
	if client == nil {
		t.Fatal("expected control binding to create a local/degraded hub client")
	}
	if client.imHandler == nil {
		t.Fatal("expected control binding self-heal to preinitialize IM handler")
	}
}

func TestPrepareHubClientSyncIsIdempotent(t *testing.T) {
	app := &App{testHomeDir: t.TempDir(), disableBackgroundEmbeddingForTest: true}
	defer func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	}()
	app.remoteSessions = NewRemoteSessionManager(app)
	app.interactionInfraDone.Store(true)

	first := app.ensureHubClient()
	if first == nil {
		t.Fatal("expected first ensureHubClient to create hub client")
	}
	app.prepareHubClientSync()
	second := app.remoteSessions.GetHubClient()
	if second != first {
		t.Fatal("prepareHubClientSync should not replace an existing hub client")
	}
	if third := app.ensureHubClient(); third != first {
		t.Fatal("ensureHubClient should reuse the existing hub client")
	}
}

func TestMarkAIAssistantReadyResetsFirstChatTelemetry(t *testing.T) {
	app, _ := newTestAIAssistantApp()
	app.aiAssistantFirstChatLogged.Store(true)

	before := time.Now().UnixNano()
	app.markAIAssistantReady()

	if !app.warmupDone.Load() {
		t.Fatal("expected warmup flag to be set")
	}
	if app.aiAssistantFirstChatLogged.Load() {
		t.Fatal("expected first-chat telemetry flag to reset")
	}
	if got := app.aiAssistantReadyAt.Load(); got < before {
		t.Fatalf("ready timestamp not updated: got %d want >= %d", got, before)
	}
}

func TestBeginFirstAIAssistantChatTelemetryOnlyClaimsFirstChatAfterReady(t *testing.T) {
	app, _ := newTestAIAssistantApp()

	if _, shouldLog := app.beginFirstAIAssistantChatTelemetry(); shouldLog {
		t.Fatal("expected telemetry to stay disabled before ready")
	}

	app.markAIAssistantReady()
	readyAt, shouldLog := app.beginFirstAIAssistantChatTelemetry()
	if !shouldLog {
		t.Fatal("expected first chat after ready to claim telemetry")
	}
	if readyAt == 0 {
		t.Fatal("expected ready timestamp to be captured")
	}

	if _, shouldLog := app.beginFirstAIAssistantChatTelemetry(); shouldLog {
		t.Fatal("expected only one first-chat telemetry claim per ready cycle")
	}
}
