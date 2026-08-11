package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

func TestLansengerBotRuntimeKeepsDurableStatePerProfile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	first, err := newLansengerBotRuntime(app, nil, "support")
	if err != nil {
		t.Fatal(err)
	}
	defer first.stop()
	second, err := newLansengerBotRuntime(app, nil, "sales")
	if err != nil {
		t.Fatal(err)
	}
	defer second.stop()
	if first.handler == second.handler || first.memory == second.memory || first.confirm == second.confirm {
		t.Fatal("bot runtimes shared mutable agent state")
	}
	if first.handler.lansengerBotProfileID != "support" || second.handler.lansengerBotProfileID != "sales" {
		t.Fatalf("runtime handler profile identities = %q/%q", first.handler.lansengerBotProfileID, second.handler.lansengerBotProfileID)
	}
	if got, want := lansengerBotDataDir(app, "support"), filepath.Join(app.GetDataDir(), "lansenger", "bots", safeFileToken("support")); got != want {
		t.Fatalf("data dir = %q, want %q", got, want)
	}
}

func TestLansengerProfileArtifactsUsePrivateDirectories(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	support := &IMMessageHandler{app: app, lansengerBotProfileID: "support"}
	sales := &IMMessageHandler{app: app, lansengerBotProfileID: "sales"}

	fileData := base64.StdEncoding.EncodeToString([]byte("profile artifact"))
	supportFile, err := support.saveFileDataToLocal("answer.txt", fileData)
	if err != nil {
		t.Fatal(err)
	}
	salesFile, err := sales.saveFileDataToLocal("answer.txt", fileData)
	if err != nil {
		t.Fatal(err)
	}
	imageData := base64.StdEncoding.EncodeToString([]byte("profile screenshot"))
	supportImage, err := support.saveScreenshotToFile(imageData)
	if err != nil {
		t.Fatal(err)
	}
	salesImage, err := sales.saveScreenshotToFile(imageData)
	if err != nil {
		t.Fatal(err)
	}

	for _, artifact := range []struct {
		profile string
		path    string
		kind    string
	}{
		{"support", supportFile, "files"},
		{"sales", salesFile, "files"},
		{"support", supportImage, "screenshots"},
		{"sales", salesImage, "screenshots"},
	} {
		wantDir := filepath.Join(lansengerBotDataDir(app, artifact.profile), "artifacts", artifact.kind)
		if filepath.Dir(artifact.path) != wantDir {
			t.Fatalf("%s %s dir = %q, want %q", artifact.profile, artifact.kind, filepath.Dir(artifact.path), wantDir)
		}
		if !strings.HasPrefix(filepath.Clean(artifact.path), filepath.Clean(wantDir)+string(filepath.Separator)) {
			t.Fatalf("artifact escaped profile directory: %q", artifact.path)
		}
	}
	if supportFile == salesFile || supportImage == salesImage {
		t.Fatalf("profile artifact paths collided: files=%q/%q images=%q/%q", supportFile, salesFile, supportImage, salesImage)
	}
	if data, err := os.ReadFile(supportFile); err != nil || string(data) != "profile artifact" {
		t.Fatalf("support artifact = %q, err=%v", data, err)
	}
}

func TestLansengerBotTurnQueueIsFIFOAndStops(t *testing.T) {
	q := newLansengerBotTurnQueue()
	var mu sync.Mutex
	var got []int
	finished := make(chan struct{})
	for i := 0; i < 3; i++ {
		n := i
		if !q.submit(nil, func(context.Context) {
			mu.Lock()
			got = append(got, n)
			mu.Unlock()
			if n == 2 {
				close(finished)
			}
		}) {
			t.Fatal("submit failed")
		}
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("queue did not drain")
	}
	mu.Lock()
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("FIFO result = %#v", got)
	}
	mu.Unlock()
	q.stop()
	if q.submit(nil, func(context.Context) {}) {
		t.Fatal("stopped queue accepted a turn")
	}
}

func TestLansengerBotTurnQueueRejectsOverloadWithoutBlocking(t *testing.T) {
	q := newLansengerBotTurnQueue()
	defer q.stop()
	started := make(chan struct{})
	release := make(chan struct{})
	if !q.submit(nil, func(context.Context) {
		close(started)
		<-release
	}) {
		t.Fatal("submit initial blocking turn")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("initial turn did not start")
	}
	for i := 0; i < cap(q.turns); i++ {
		if !q.submit(nil, func(context.Context) {}) {
			t.Fatalf("submit queued turn %d", i)
		}
	}
	start := time.Now()
	if q.submit(nil, func(context.Context) {}) {
		t.Fatal("overloaded queue accepted another turn")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("overloaded submit blocked for %s", elapsed)
	}
	close(release)
}

func TestLansengerBotTurnQueueStopCancelsActiveTurnAndDropsPendingTurns(t *testing.T) {
	q := newLansengerBotTurnQueue()
	started := make(chan struct{})
	canceled := make(chan struct{})
	queuedRan := make(chan struct{}, 1)
	if !q.submit(context.Background(), func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(canceled)
	}) {
		t.Fatal("submit active turn")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active turn did not start")
	}
	if !q.submit(nil, func(context.Context) { queuedRan <- struct{}{} }) {
		t.Fatal("submit pending turn")
	}
	stopped := make(chan struct{})
	go func() {
		q.stop()
		close(stopped)
	}()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel the active turn")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop did not wait for the worker to finish")
	}
	select {
	case <-queuedRan:
		t.Fatal("stop executed a pending turn")
	default:
	}
}

func TestLansengerBotTurnQueueStopWinsBeforeDequeuedTurnStarts(t *testing.T) {
	q := newLansengerBotTurnQueue()
	defer q.stop()
	// Simulate a worker that has just dequeued a turn while shutdown obtains
	// the state lock. The canceled context must prevent executing its callback.
	q.mu.Lock()
	q.stopped = true
	q.cancel()
	q.mu.Unlock()
	ctx, cancel := q.turnContext(context.Background())
	defer cancel()
	if ctx.Err() == nil {
		t.Fatal("turn context remained active after queue shutdown")
	}
}

func TestLansengerBotTurnQueueCancelsCompletedTurnBeforeTransportDispatch(t *testing.T) {
	q := newLansengerBotTurnQueue()
	started := make(chan struct{})
	finished := make(chan struct{})
	resultC := make(chan context.Context, 1)
	if !q.submit(nil, func(ctx context.Context) {
		close(started)
		resultC <- ctx
		close(finished)
	}) {
		t.Fatal("submit turn")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("turn did not start")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("turn did not finish")
	}
	turnCtx := <-resultC
	q.stop()
	if err := contextErr(turnCtx); err == nil {
		t.Fatal("completed turn context should be canceled before later transport dispatch")
	}
}

func TestLansengerCanceledTurnDoesNotEnterLocalProcessing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager := &lansengerGatewayManager{}
	// A live turn dereferences app while handling this local shortcut. The nil
	// app makes the assertion crisp: a canceled turn must return before any
	// processing side effect or routing dependency is reached.
	manager.handleLocalMessageNow(ctx, lansenger.IncomingMessage{ChatType: "p2p", Text: "/startmenu"})
}

func TestLansengerProfileOptionsMentionByDefault(t *testing.T) {
	profile := corelib.LansengerBotProfile{ID: "p", Enabled: true}
	cfg := lansengerBotProfileGroupOptions(profile)
	if !cfg.LansengerAutoMentionReply {
		t.Fatal("profile default must @ group asker")
	}
}

func TestLansengerBotProfileFingerprintIncludesPermissionAndContext(t *testing.T) {
	base := corelib.LansengerBotProfile{ID: "p", AppID: "app", AppSecret: "secret", Enabled: true}
	changed := base
	changed.DocumentDirectories = []string{"docs"}
	if reflect.DeepEqual(lansengerBotProfileFingerprint(base), lansengerBotProfileFingerprint(changed)) {
		t.Fatal("document directory change must replace the runtime")
	}
	changed = base
	changed.AutoQuoteReply = true
	if reflect.DeepEqual(lansengerBotProfileFingerprint(base), lansengerBotProfileFingerprint(changed)) {
		t.Fatal("group policy change must replace the runtime")
	}
}

func TestLansengerRegistryCompatibilityFacadeOnlyUsesMigratedDefault(t *testing.T) {
	registry := newLansengerGatewayRegistry(nil)
	registry.bots["alpha"] = &lansengerGatewayManager{}
	if got := registry.defaultManager(); got != nil {
		t.Fatalf("custom bot must not become the implicit legacy default: %#v", got)
	}
	defaultManager := &lansengerGatewayManager{}
	registry.bots[corelib.DefaultLansengerBotProfileID] = defaultManager
	if got := registry.defaultManager(); got != defaultManager {
		t.Fatalf("default manager = %#v, want migrated default %#v", got, defaultManager)
	}
}

func TestLansengerProfileRequiresLocalProcessingWhenHubModeIsSelected(t *testing.T) {
	hubMode := false
	cfg := corelib.AppConfig{LansengerLocalMode: &hubMode}
	profileManager := newLansengerGatewayManagerForProfile(nil, corelib.LansengerBotProfile{ID: "support"})
	if !profileManager.requiresLocalProcessing(cfg) {
		t.Fatal("profile bot must remain local until Hub routing carries bot profile identity")
	}
	legacyManager := newLansengerGatewayManager(nil)
	if legacyManager.requiresLocalProcessing(cfg) {
		t.Fatal("legacy singleton should retain explicit Hub-mode compatibility")
	}
}

func TestLansengerProfileHandlerNeverFallsBackToLegacyHandler(t *testing.T) {
	manager := newLansengerGatewayManagerForProfile(nil, corelib.LansengerBotProfile{ID: "support"})
	legacy := &IMMessageHandler{}
	manager.localHandler = legacy
	if got := manager.ensureLocalHandler(); got != nil {
		t.Fatalf("profile handler = %#v, want nil when private runtime cannot initialize", got)
	}
	if got := manager.currentLocalHandler(); got != nil {
		t.Fatalf("uninitialized profile handler = %#v, want nil instead of legacy handler", got)
	}

	private := &IMMessageHandler{}
	manager.runtime = &lansengerBotRuntime{handler: private}
	if got := manager.currentLocalHandler(); got != private {
		t.Fatalf("profile handler = %#v, want runtime-owned handler %#v", got, private)
	}
}

func TestLansengerAssistantBindingCarriesFileBoundary(t *testing.T) {
	profile := corelib.LansengerBotProfile{
		ID: "support", WorkingDirectory: "work", DocumentDirectories: []string{"docs"},
		AllowedDirectories: []string{"assets"}, AllowAllDirectories: false,
	}
	binding := lansengerAssistantBinding(profile)
	if binding == nil || binding.WorkingDirectory != "work" || !reflect.DeepEqual(binding.DocumentDirectories, []string{"docs"}) || !reflect.DeepEqual(binding.AllowedDirectories, []string{"assets"}) || binding.AllowAllDirectories {
		t.Fatalf("binding = %#v", binding)
	}
}

func TestScheduledTaskHandlerUsesOnlyOwningProfileRuntime(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	defaultHandler := &IMMessageHandler{}
	support := newLansengerGatewayManagerForProfile(app, corelib.LansengerBotProfile{ID: "support"})
	registry := newLansengerGatewayRegistry(app)
	registry.bots["support"] = support
	app.lansengerGateways = registry

	task := &scheduler.ScheduledTask{Delivery: &scheduler.TaskDelivery{BotProfileID: "support"}}
	handler, binding, err := app.scheduledTaskHandler(task, defaultHandler)
	if err != nil {
		t.Fatal(err)
	}
	if handler == nil || handler == defaultHandler || binding == nil || binding.BotProfileID != "support" {
		t.Fatalf("handler=%#v binding=%#v", handler, binding)
	}
	missing := &scheduler.ScheduledTask{Delivery: &scheduler.TaskDelivery{BotProfileID: "missing"}}
	if handler, _, err := app.scheduledTaskHandler(missing, defaultHandler); err == nil || handler != nil {
		t.Fatalf("missing profile handler=%#v err=%v; must fail closed", handler, err)
	}
}

func TestScheduledTaskConversationOwnerScopesProfileAndTask(t *testing.T) {
	legacy := &scheduler.ScheduledTask{ID: "desktop-task"}
	if got := scheduledTaskConversationOwner(legacy); got != "scheduled_task" {
		t.Fatalf("legacy owner = %q, want scheduled_task", got)
	}
	supportA := &scheduler.ScheduledTask{ID: "task-a", BotProfileID: "support"}
	supportB := &scheduler.ScheduledTask{ID: "task-b", BotProfileID: "support"}
	salesA := &scheduler.ScheduledTask{ID: "task-a", Delivery: &scheduler.TaskDelivery{BotProfileID: "sales"}}
	owners := map[string]struct{}{
		scheduledTaskConversationOwner(supportA): {},
		scheduledTaskConversationOwner(supportB): {},
		scheduledTaskConversationOwner(salesA):   {},
	}
	if len(owners) != 3 {
		t.Fatalf("profile task owners collided: %#v", owners)
	}
	if got := scheduledTaskConversationOwner(salesA); got == "scheduled_task" {
		t.Fatalf("delivery-owned profile task used shared owner: %q", got)
	}
}
