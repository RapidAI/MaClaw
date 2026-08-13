package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// hardwareAgentRuntime owns the mutable Agent state for exactly one physical
// hardware client. Shared App services (configuration, local files, LLM
// credentials and automation backends) remain shared, but conversation memory,
// loop/interrupt state, tool registry and HTTP connection pool do not cross a
// hardware boundary.
type hardwareAgentRuntime struct {
	clientID          string
	handler           *IMMessageHandler
	memoryStore       *memory.Store
	confirmationStore *aiConfirmationStore
}

type hardwareAgentRuntimeRegistry struct {
	app       *App
	manager   *RemoteSessionManager
	configure func(*IMMessageHandler)

	mu       sync.Mutex
	runtimes map[string]*hardwareAgentRuntime
	// initializing lets different hardware clients create their private
	// runtimes concurrently while still coalescing concurrent first messages
	// from the same client into one handler. Constructing a handler can open
	// durable stores and register tools, so holding mu for that work would make
	// an unrelated device's first turn wait behind it.
	initializing map[string]chan struct{}
	stopped      bool
	// cleanupInFlight covers a runtime that has been removed from runtimes but
	// is still cancelling loops or closing its stores. Without it, stopAll can
	// observe an empty map and return while a concurrent unbind is still holding
	// that device's private resources.
	cleanupInFlight int
	cleanupIdle     chan struct{}
	// stoppedDone closes only after every published and initializing runtime has
	// released its private resources. Concurrent lifecycle callers must wait for
	// the same boundary rather than treating the first caller's stopped flag as
	// completed teardown.
	stoppedDone chan struct{}
}

// handlers returns a stable snapshot for lifecycle-wide wiring such as
// embedding activation. The snapshot prevents registry locking from covering
// handler mutation or any external work.
func (r *hardwareAgentRuntimeRegistry) handlers() []*IMMessageHandler {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	handlers := make([]*IMMessageHandler, 0, len(r.runtimes))
	for _, runtime := range r.runtimes {
		if runtime != nil && runtime.handler != nil {
			handlers = append(handlers, runtime.handler)
		}
	}
	return handlers
}

// configureHardwareAgent wires only shared host services that are safe to use
// behind the hardware client's private session identity. It deliberately does
// not install App-wide mutable routing/usage/activity state: each runtime keeps
// the registry, dynamic tool builder, queues and HTTP transports created by
// NewIMMessageHandler.
func (a *App) configureHardwareAgent(h *IMMessageHandler) {
	if a == nil || h == nil {
		return
	}
	a.ensureInteractionInfra()
	// Initialize once before copying it into the private handler. Falling back
	// to a.app.unifiedClassifier would make a reconnect-time initialization
	// race visible to an already-running device runtime.
	a.initEarlyClassifier()
	if a.contextResolver == nil {
		a.ensureContextResolver()
	}
	if a.sessionPrecheck == nil {
		a.ensureSessionPrecheck()
	}
	// Capability-gap resolution can mutate application-wide Skill state. Keep
	// that optional feature disabled for hardware runtimes rather than sharing
	// an App-wide detector between physical devices.
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(cloneHardwareToolDefinitionGenerator(a.toolDefGenerator))
	}
	h.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
	if a.configManager != nil {
		h.SetConfigManager(a.configManager)
	}
	if a.templateManager != nil {
		h.SetTemplateManager(a.templateManager)
	}
	if a.contextResolver != nil {
		h.SetContextResolver(a.contextResolver)
	}
	if a.sessionPrecheck != nil {
		h.SetSessionPrecheck(a.sessionPrecheck)
	}
	a.ensureStartupFeedback()
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall == nil {
		a.ensureSecurityFirewall()
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		return a.forwardDesktopFileToIMRequest(a.hubClient(), req)
	})
	if a.unifiedClassifier != nil {
		h.unifiedClassifier = a.newHardwareIntentClassifier()
	}
}

// cloneHardwareToolDefinitionGenerator gives a hardware runtime its own
// deferred-tool activation state. The source generator is App-wide, and its
// activatedDeferred map changes when discover_tool unlocks a tool; sharing it
// would let an action on one physical device alter another device's prompt.
// MCP discovery sources remain shared host services, while all definition and
// deferred-state containers are copied for the runtime.
func cloneHardwareToolDefinitionGenerator(source *ToolDefinitionGenerator) *ToolDefinitionGenerator {
	if source == nil {
		return nil
	}
	deferred, activated := source.deferredStateSnapshot()
	clone := NewToolDefinitionGenerator(source.registry, cloneToolDefinitionMaps(source.builtinDefs))
	clone.SetLocalMCPManager(source.localMCPManager)
	clone.deferredMu.Lock()
	clone.deferredTools = deferred
	clone.activatedDeferred = activated
	clone.deferredMu.Unlock()
	return clone
}

// cloneToolDefinitionMaps copies the JSON-shaped builtin definition trees so
// an incidental schema adjustment in a device runtime cannot alias the App's
// generator or another device's runtime.
func cloneToolDefinitionMaps(definitions []map[string]interface{}) []map[string]interface{} {
	if definitions == nil {
		return nil
	}
	cloned := make([]map[string]interface{}, len(definitions))
	for i, definition := range definitions {
		cloned[i] = cloneToolDefinitionMap(definition)
	}
	return cloned
}

// newHardwareIntentClassifier keeps the per-message classifier caches scoped
// to one physical device while sharing only the immutable configuration and the
// process's read-only embedding model. This prevents concurrent devices from
// invalidating each other's cache epochs or retaining each other's text.
func (a *App) newHardwareIntentClassifier() *intent.UnifiedIntentClassifier {
	if a == nil {
		return nil
	}
	var emb = a.activeInterruptEmbedder()
	if emb == nil {
		emb = embedding.NoopEmbedder{}
	}
	return intent.New(intent.Config{
		Embedder:       emb,
		LLMFunc:        a.buildUICLLMFunc(),
		LLMContextFunc: a.buildUICLLMContextFunc(),
		LLMTimeout:     30 * time.Second,
	})
}

func newHardwareAgentRuntimeRegistry(app *App, manager *RemoteSessionManager, configure func(*IMMessageHandler)) *hardwareAgentRuntimeRegistry {
	if configure == nil && app != nil {
		// Tests and lightweight hosts can construct the registry without the App
		// wiring callback. Preserve the same per-device classifier boundary there.
		configure = func(h *IMMessageHandler) {
			h.unifiedClassifier = app.newHardwareIntentClassifier()
		}
	}
	return &hardwareAgentRuntimeRegistry{
		app:          app,
		manager:      manager,
		configure:    configure,
		runtimes:     make(map[string]*hardwareAgentRuntime),
		initializing: make(map[string]chan struct{}),
		cleanupIdle:  closedHardwareRuntimeChannel(),
	}
}

func closedHardwareRuntimeChannel() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// beginRuntimeCleanupLocked and finishRuntimeCleanup bracket resource shutdown
// after a runtime is removed from the registry map. The mutex keeps the idle
// snapshot stable for stopAll callers that need a complete teardown boundary.
func (r *hardwareAgentRuntimeRegistry) beginRuntimeCleanupLocked() {
	if r.cleanupInFlight == 0 {
		r.cleanupIdle = make(chan struct{})
	}
	r.cleanupInFlight++
}

func (r *hardwareAgentRuntimeRegistry) finishRuntimeCleanup() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cleanupInFlight--
	if r.cleanupInFlight == 0 {
		close(r.cleanupIdle)
	}
	r.mu.Unlock()
}

func (r *hardwareAgentRuntimeRegistry) stopRuntime(runtime *hardwareAgentRuntime) {
	if runtime == nil {
		return
	}
	runtime.stop()
	r.finishRuntimeCleanup()
}

func (r *hardwareAgentRuntimeRegistry) handler(clientID string) (*IMMessageHandler, error) {
	if r == nil || r.app == nil {
		return nil, fmt.Errorf("hardware agent runtime is not configured")
	}
	clientID = normalizeHardwareRuntimeClientID(clientID)
	if clientID == "" {
		return nil, fmt.Errorf("hardware client ID is required")
	}

	for {
		r.mu.Lock()
		if r.stopped {
			r.mu.Unlock()
			return nil, fmt.Errorf("hardware agent runtime registry is stopped")
		}
		if runtime := r.runtimes[clientID]; runtime != nil && runtime.handler != nil {
			r.mu.Unlock()
			return runtime.handler, nil
		}
		if ready := r.initializing[clientID]; ready != nil {
			r.mu.Unlock()
			<-ready
			// The creator either published the runtime, failed, or the registry
			// was stopped. Inspect the result under the mutex on the next pass.
			continue
		}
		ready := make(chan struct{})
		r.initializing[clientID] = ready
		r.mu.Unlock()

		// newHandler creates a separate ConversationMemory, AgentActivityStore,
		// interrupt handler, tool registry, task orchestration registry and HTTP
		// connection pools for this physical hardware identity. Do this outside
		// the registry lock so an alpha device cannot delay beta's startup.
		conversationMemory := newHardwareConversationMemory(r.app, clientID)
		confirmationStore := newHardwareConfirmationStore(r.app, clientID)
		h := newIMMessageHandler(r.app, r.manager, conversationMemory, confirmationStore)
		var store *memory.Store
		var err error
		if h == nil {
			err = fmt.Errorf("hardware agent runtime is not configured")
		} else {
			if r.configure != nil {
				r.configure(h)
			}
			store, err = r.app.newHardwareMemoryStore(clientID)
			if err == nil {
				h.SetMemoryStore(store)
			}
		}

		r.mu.Lock()
		stopped := r.stopped
		if err == nil && !stopped {
			r.runtimes[clientID] = &hardwareAgentRuntime{
				clientID:          clientID,
				handler:           h,
				memoryStore:       store,
				confirmationStore: confirmationStore,
			}
			delete(r.initializing, clientID)
			close(ready)
			r.mu.Unlock()
			return h, nil
		}
		// Keep the initialization marker until cleanup completes. It prevents a
		// same-client caller from starting a replacement while the failed or
		// stopped runtime still owns files and HTTP resources.
		r.mu.Unlock()

		if h != nil {
			(&hardwareAgentRuntime{handler: h, memoryStore: store}).stop()
		}
		r.mu.Lock()
		delete(r.initializing, clientID)
		close(ready)
		r.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("hardware agent runtime registry is stopped")
	}
}

// isActiveHandler confirms that a delivery still belongs to the exact runtime
// that was originally selected. It rejects both a removed runtime and an old
// runtime after a same-ID replacement is published.
func (r *hardwareAgentRuntimeRegistry) isActiveHandler(clientID string, handler *IMMessageHandler) bool {
	if r == nil || handler == nil {
		return false
	}
	clientID = normalizeHardwareRuntimeClientID(clientID)
	r.mu.Lock()
	defer r.mu.Unlock()
	runtime := r.runtimes[clientID]
	return runtime != nil && runtime.handler == handler
}

func (r *hardwareAgentRuntimeRegistry) remove(clientID string) {
	if r == nil {
		return
	}
	clientID = normalizeHardwareRuntimeClientID(clientID)
	if clientID == "" {
		return
	}
	for {
		r.mu.Lock()
		if initializing := r.initializing[clientID]; initializing != nil {
			r.mu.Unlock()
			// Do not let an unbind race an in-progress first message and leave a
			// newly-created runtime behind after this method returns.
			<-initializing
			continue
		}
		runtime := r.runtimes[clientID]
		delete(r.runtimes, clientID)
		if runtime != nil {
			r.beginRuntimeCleanupLocked()
		}
		r.mu.Unlock()
		if runtime != nil {
			r.stopRuntime(runtime)
		}
		return
	}
}

// removeAll removes a private runtime from every transport registry in the
// current process. A device can briefly be reachable through both the direct
// gateway and Hub while routing changes; unbinding must tear down either
// runtime so no stale Agent/session/resources survive that ownership change.
func (a *App) removeHardwareAgentRuntime(clientID string) {
	if a == nil {
		return
	}
	clientID = normalizeHardwareRuntimeClientID(clientID)
	if clientID == "" {
		return
	}
	if hub := a.hubClient(); hub != nil {
		hub.removeHardwareAgent(clientID)
	}
	if gateway := a.thirdPartyGateway; gateway != nil {
		gateway.removeLocalHardwareAgent(clientID)
	}
}

func (r *hardwareAgentRuntimeRegistry) stopAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		done := r.stoppedDone
		r.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	r.stopped = true
	done := make(chan struct{})
	r.stoppedDone = done
	runtimes := r.runtimes
	r.runtimes = make(map[string]*hardwareAgentRuntime)
	for _, runtime := range runtimes {
		if runtime != nil {
			r.beginRuntimeCleanupLocked()
		}
	}
	initializing := make([]chan struct{}, 0, len(r.initializing))
	for _, ready := range r.initializing {
		if ready != nil {
			initializing = append(initializing, ready)
		}
	}
	r.mu.Unlock()
	defer close(done)
	for _, runtime := range runtimes {
		if runtime != nil {
			r.stopRuntime(runtime)
		}
	}
	// A disconnect/unbind lifecycle only completes once a concurrently-created
	// runtime has cancelled and closed its private resources as well. Runtime
	// construction is local file/tool setup, not an LLM request, so waiting here
	// gives callers a truthful teardown boundary without holding the mutex.
	for _, ready := range initializing {
		<-ready
	}
	r.mu.Lock()
	cleanupIdle := r.cleanupIdle
	r.mu.Unlock()
	<-cleanupIdle
}

func (r *hardwareAgentRuntimeRegistry) count() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runtimes)
}

func (r *hardwareAgentRuntimeRegistry) onlyHandler() *IMMessageHandler {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.runtimes) != 1 {
		return nil
	}
	for _, runtime := range r.runtimes {
		if runtime != nil {
			return runtime.handler
		}
	}
	return nil
}

// existingHandler returns a published runtime without creating a new one. It
// is used by control-plane actions such as cancellation: receiving a stale
// cancel must never restart an Agent for an unbound or idle device.
func (r *hardwareAgentRuntimeRegistry) existingHandler(clientID string) *IMMessageHandler {
	if r == nil {
		return nil
	}
	clientID = normalizeHardwareRuntimeClientID(clientID)
	if clientID == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if runtime := r.runtimes[clientID]; runtime != nil {
		return runtime.handler
	}
	return nil
}

func (runtime *hardwareAgentRuntime) stop() {
	if runtime == nil || runtime.handler == nil {
		return
	}
	h := runtime.handler
	// Tear down starts with cancellation. Closing memory/HTTP resources while a
	// loop is still streaming can otherwise leave a detached hardware task
	// running after its device was unbound or the transport disconnected.
	h.cancelAllSessionsForShutdown()
	// The handler's per-device workflow and activity state is otherwise only
	// reachable through this runtime after unbind. Drop it promptly so an old
	// hardware task cannot reappear if callers retained a handler reference.
	if h.taskOrchestratorRegistry != nil {
		h.taskOrchestratorRegistry.Clear()
	}
	// Checkpoints are keyed independently from adapters, so release both maps.
	// Do not delete persisted workflow documents here: unbinding is reversible
	// and must not destroy user data without an explicit cleanup request.
	h.codingExecCheckpoint.Range(func(key, _ any) bool {
		if ownerID, ok := key.(string); ok {
			h.clearCodingExecCheckpoint(ownerID)
		}
		return true
	})
	h.workflowV2Adapters.Range(func(key, _ any) bool {
		h.workflowV2Adapters.Delete(key)
		return true
	})
	h.frozenMemorySnapshots.Range(func(key, _ any) bool {
		h.frozenMemorySnapshots.Delete(key)
		return true
	})
	h.snapshotInitialized.Range(func(key, _ any) bool {
		h.snapshotInitialized.Delete(key)
		return true
	})
	h.snapshotEpoch.Range(func(key, _ any) bool {
		h.snapshotEpoch.Delete(key)
		return true
	})
	if h.agentActivity != nil {
		h.agentActivity.ClearAll()
	}
	if runtime.confirmationStore != nil {
		runtime.confirmationStore.stop()
	}
	if h.memory != nil {
		h.memory.Stop()
	}
	if h.client != nil {
		if transport, ok := h.client.Transport.(interface{ CloseIdleConnections() }); ok {
			transport.CloseIdleConnections()
		}
	}
	if h.taskClient != nil {
		if transport, ok := h.taskClient.Transport.(interface{ CloseIdleConnections() }); ok {
			transport.CloseIdleConnections()
		}
	}
	if runtime.memoryStore != nil {
		runtime.memoryStore.Stop()
	}
}

// normalizeHardwareRuntimeClientID is the registry key for a physical device.
// Client IDs are case-insensitive in the hardware protocol, so case variants
// must resolve to one runtime rather than creating competing Agents.
func normalizeHardwareRuntimeClientID(clientID string) string {
	return strings.ToLower(normalizeThirdPartyID(clientID))
}

func hardwareConversationMemoryPath(app *App, clientID string) string {
	if app == nil {
		return ""
	}
	return filepath.Join(app.GetDataDir(), "hardware_agents", safeFileToken(clientID), "conversation.json")
}

// newHardwareConversationMemory creates a durable, client-private history.
// It is intentionally separate from the desktop conversation file so deleting
// a hardware binding can remove the complete session without touching other
// devices or the desktop assistant.
func newHardwareConversationMemory(app *App, clientID string) *agent.ConversationMemory {
	path := hardwareConversationMemoryPath(app, clientID)
	if path == "" {
		return agent.NewConversationMemory()
	}
	return agent.NewPersistentConversationMemory(path)
}

func hardwareAgentDataDir(app *App, clientID string) string {
	if app == nil {
		return ""
	}
	return filepath.Join(app.GetDataDir(), "hardware_agents", safeFileToken(clientID))
}

func newHardwareConfirmationStore(app *App, clientID string) *aiConfirmationStore {
	baseDir := hardwareAgentDataDir(app, clientID)
	if baseDir == "" {
		return newAIConfirmationStore("")
	}
	return newAIConfirmationStore(filepath.Join(baseDir, "confirmations.json"))
}
