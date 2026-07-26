package llm

// ModelRouter routes LLM requests to different models based on task type.
// Inspired by OpenHuman's model routing — each task type (intent/fast/reasoning/
// vision/summary) can be served by a different model optimized for that workload.
//
// When no route is configured for a task, falls back to the primary model.
// When the auxiliary LLM is configured and the task is "summary" or "fast",
// automatically uses the auxiliary model.

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

// TaskType identifies the kind of LLM workload for routing purposes.
type TaskType string

const (
	// TaskDefault is the primary agent loop — balanced model.
	TaskDefault TaskType = "default"
	// TaskIntent is intent understanding / classification — fast, cheap.
	TaskIntent TaskType = "intent"
	// TaskFast is simple Q&A, chit-chat — fast, cheap.
	TaskFast TaskType = "fast"
	// TaskReasoning is coding, complex analysis — strong reasoning.
	TaskReasoning TaskType = "reasoning"
	// TaskVision is image recognition, screenshot analysis — multimodal.
	TaskVision TaskType = "vision"
	// TaskSummary is summarization, compression — fast, cheap.
	TaskSummary TaskType = "summary"
)

// ModelRoute defines a model override for a specific task type.
type ModelRoute struct {
	Model    string `json:"model"`              // model name override
	Provider string `json:"provider,omitempty"` // provider name (for multi-provider setups)
	URL      string `json:"url,omitempty"`      // API URL override (empty = use primary)
	Key      string `json:"key,omitempty"`      // API key override (empty = use primary)
	Protocol string `json:"protocol,omitempty"` // protocol override (empty = use primary)
}

// ModelRouter manages task-type-to-model routing.
type ModelRouter struct {
	mu     sync.RWMutex
	routes map[TaskType]ModelRoute
}

// NewModelRouter creates a router with the given routes.
func NewModelRouter(routes map[string]ModelRoute) *ModelRouter {
	r := &ModelRouter{
		routes: make(map[TaskType]ModelRoute),
	}
	for k, v := range routes {
		r.routes[TaskType(strings.ToLower(k))] = v
	}
	return r
}

// Route returns the LLM config for the given task type.
// It starts from the primary config and applies any overrides from the route.
// If no route is configured for the task, returns the primary config unchanged.
func (r *ModelRouter) Route(task TaskType, primary corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if r == nil {
		return primary
	}
	r.mu.RLock()
	route, ok := r.routes[task]
	r.mu.RUnlock()
	if !ok {
		return primary
	}
	return applyRoute(primary, route)
}

// RouteWithAux returns the LLM config for the given task type, considering
// both explicit routes and the auxiliary LLM fallback.
// Priority: explicit route > auxiliary LLM (for fast/summary/intent) > primary.
func (r *ModelRouter) RouteWithAux(task TaskType, primary corelib.MaclawLLMConfig, aux corelib.AuxiliaryLLMConfig) corelib.MaclawLLMConfig {
	// Check explicit route first
	if r != nil {
		r.mu.RLock()
		route, ok := r.routes[task]
		r.mu.RUnlock()
		if ok {
			return applyRoute(primary, route)
		}
	}

	// For lightweight tasks, fall back to auxiliary LLM if configured
	if aux.IsConfigured() && isLightweightTask(task) {
		return applyAuxiliaryLLM(primary, aux)
	}

	return primary
}

// applyAuxiliaryLLM materializes the auxiliary endpoint while retaining the
// user-selected reasoning controls from the primary configuration. Auxiliary
// configs intentionally only contain connection details, so rebuilding a
// config here must not make the global thinking switch disappear.
func applyAuxiliaryLLM(primary corelib.MaclawLLMConfig, aux corelib.AuxiliaryLLMConfig) corelib.MaclawLLMConfig {
	return corelib.MaclawLLMConfig{
		URL:             aux.URL,
		Key:             aux.Key,
		Model:           aux.Model,
		Protocol:        aux.Protocol,
		ThinkingMode:    primary.ThinkingMode,
		ReasoningEffort: primary.ReasoningEffort,
	}
}

// SetRoutes replaces all routes atomically.
func (r *ModelRouter) SetRoutes(routes map[string]ModelRoute) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = make(map[TaskType]ModelRoute)
	for k, v := range routes {
		r.routes[TaskType(strings.ToLower(k))] = v
	}
}

// HasRoute returns true if an explicit route exists for the task type.
func (r *ModelRouter) HasRoute(task TaskType) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	_, ok := r.routes[task]
	r.mu.RUnlock()
	return ok
}

// ListRoutes returns a copy of configured task→route mappings (for UI/status).
func (r *ModelRouter) ListRoutes() map[string]ModelRoute {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.routes) == 0 {
		return nil
	}
	out := make(map[string]ModelRoute, len(r.routes))
	for k, v := range r.routes {
		out[string(k)] = v
	}
	return out
}

// GetRoute returns the route for task if configured.
func (r *ModelRouter) GetRoute(task TaskType) (ModelRoute, bool) {
	if r == nil {
		return ModelRoute{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[task]
	return route, ok
}

// applyRoute overlays route overrides onto the primary config.
func applyRoute(primary corelib.MaclawLLMConfig, route ModelRoute) corelib.MaclawLLMConfig {
	cfg := primary
	if route.Model != "" {
		cfg.Model = route.Model
	}
	if route.URL != "" {
		cfg.URL = route.URL
	}
	if route.Key != "" {
		cfg.Key = route.Key
	}
	if route.Protocol != "" {
		cfg.Protocol = route.Protocol
	}
	if route.Provider != "" {
		cfg.ProviderName = route.Provider
	}
	return cfg
}

// isLightweightTask returns true for task types that benefit from a fast/cheap model.
func isLightweightTask(task TaskType) bool {
	switch task {
	case TaskIntent, TaskFast, TaskSummary:
		return true
	default:
		return false
	}
}
