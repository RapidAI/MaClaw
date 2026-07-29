package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

const (
	codingRoutePrefAuto      = "auto"
	codingRoutePrefPrimary   = "primary"
	codingRoutePrefReasoning = "reasoning"
	codingRoutePrefVision    = "vision"
)

// codingRouteCapability describes whether a workbench route pref can resolve
// to a dedicated ModelRouter (or primary) model — for UI tooltips / status.
type codingRouteCapability struct {
	Pref      string `json:"pref"`             // auto | primary | reasoning | vision
	Available bool   `json:"available"`        // true when this pref is usable
	Model     string `json:"model,omitempty"`  // resolved model name when known
	Source    string `json:"source,omitempty"` // primary | route | fallback
	Note      string `json:"note,omitempty"`   // human-readable caveat
}

func normalizeCodingRoutePref(pref string) string {
	switch strings.ToLower(strings.TrimSpace(pref)) {
	case codingRoutePrefPrimary, "default", "main":
		return codingRoutePrefPrimary
	case codingRoutePrefReasoning, "code", "coding":
		return codingRoutePrefReasoning
	case codingRoutePrefVision, "image", "multimodal":
		return codingRoutePrefVision
	case codingRoutePrefAuto, "", "default_auto":
		return codingRoutePrefAuto
	default:
		return codingRoutePrefAuto
	}
}

func (h *IMMessageHandler) getStickyCodingRoutePref(userID string) string {
	if h == nil {
		return codingRoutePrefAuto
	}
	return normalizeCodingRoutePref(h.getStickyCodingWorkbenchMemory(userID).RoutePref)
}

func (h *IMMessageHandler) setStickyCodingRoutePref(userID, pref string) {
	if h == nil {
		return
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	pref = normalizeCodingRoutePref(pref)
	h.updateStickyCodingWorkbenchMemory(userID, func(mem *stickyCodingWorkbenchMemory) {
		mem.RoutePref = pref
	})
}

// applyCodingRoutePreference applies sticky route preference before SubAgent run.
func (h *IMMessageHandler) applyCodingRoutePreference(userID string, cfg corelib.MaclawLLMConfig, hasImages bool) corelib.MaclawLLMConfig {
	if h == nil {
		return cfg
	}
	pref := h.getStickyCodingRoutePref(userID)
	switch pref {
	case codingRoutePrefPrimary:
		return cfg
	case codingRoutePrefVision:
		return h.routeLLMConfigForCodingVision(cfg)
	case codingRoutePrefReasoning:
		if h.app != nil && h.app.ohModules.modelRouter != nil && h.app.ohModules.modelRouter.HasRoute(llm.TaskReasoning) {
			return h.routeLLMConfig(llm.TaskReasoning)
		}
		return cfg
	default: // auto
		if hasImages {
			return h.routeLLMConfigForCodingVision(cfg)
		}
		if h.app != nil && h.app.ohModules.modelRouter != nil && h.app.ohModules.modelRouter.HasRoute(llm.TaskReasoning) {
			return h.routeLLMConfig(llm.TaskReasoning)
		}
		return cfg
	}
}

// routeLLMConfigForCodingVision prefers a vision-capable model when the router
// has a vision (or multimodal) route; otherwise returns cfg unchanged.
func (h *IMMessageHandler) routeLLMConfigForCodingVision(cfg corelib.MaclawLLMConfig) corelib.MaclawLLMConfig {
	if h == nil {
		return cfg
	}
	// Try explicit vision task routes first.
	tasks := []llm.TaskType{llm.TaskVision, llm.TaskType("multimodal"), llm.TaskType("image")}
	for _, task := range tasks {
		if h.app != nil && h.app.ohModules.modelRouter != nil && h.app.ohModules.modelRouter.HasRoute(task) {
			routed := h.routeLLMConfig(task)
			if strings.TrimSpace(routed.URL) != "" && strings.TrimSpace(routed.Model) != "" {
				// Ensure SupportsVision is true when we intentionally pick vision.
				routed.SupportsVision = true
				return routed
			}
		}
	}
	// Fall back to current cfg; still mark vision if the host already set it.
	return cfg
}

func (h *IMMessageHandler) primaryMaclawModelName() string {
	if h == nil {
		return ""
	}
	return strings.TrimSpace(h.getMaclawLLMConfig().Model)
}

func (h *IMMessageHandler) modelRouterHasReasoning() bool {
	return h != nil && h.app != nil && h.app.ohModules.modelRouter != nil &&
		h.app.ohModules.modelRouter.HasRoute(llm.TaskReasoning)
}

func (h *IMMessageHandler) modelRouterHasVision() bool {
	if h == nil || h.app == nil || h.app.ohModules.modelRouter == nil {
		return false
	}
	for _, task := range []llm.TaskType{llm.TaskVision, llm.TaskType("multimodal"), llm.TaskType("image")} {
		if h.app.ohModules.modelRouter.HasRoute(task) {
			return true
		}
	}
	return false
}

// Short TTL cache so GetCodingWorkbenchStatus polls do not recompute route caps
// on every focus tick. Invalidated when ModelRouter is reloaded.
const codingRouteCapabilitiesCacheTTL = 2 * time.Second

var (
	codingRouteCapsCacheMu sync.Mutex
	codingRouteCapsCache   []codingRouteCapability
	codingRouteCapsCacheAt time.Time
	// fingerprint of primary model + route keys to avoid serving stale caps
	// after provider switch without router reload.
	codingRouteCapsCacheKey string
)

func invalidateCodingRouteCapabilitiesCache() {
	codingRouteCapsCacheMu.Lock()
	codingRouteCapsCache = nil
	codingRouteCapsCacheAt = time.Time{}
	codingRouteCapsCacheKey = ""
	codingRouteCapsCacheMu.Unlock()
}

func (h *IMMessageHandler) codingRouteCapabilitiesCacheKey() string {
	primary := h.primaryMaclawModelName()
	var b strings.Builder
	b.WriteString(primary)
	if h != nil && h.app != nil && h.app.ohModules.modelRouter != nil {
		if routes := h.app.ohModules.modelRouter.ListRoutes(); len(routes) > 0 {
			keys := make([]string, 0, len(routes))
			for k, v := range routes {
				keys = append(keys, k+"="+strings.TrimSpace(v.Model))
			}
			sort.Strings(keys)
			b.WriteByte('|')
			b.WriteString(strings.Join(keys, ","))
		}
	}
	return b.String()
}

// codingRouteCapabilities reports how each workbench route pref maps to ModelRouter.
func (h *IMMessageHandler) codingRouteCapabilities() []codingRouteCapability {
	key := h.codingRouteCapabilitiesCacheKey()
	codingRouteCapsCacheMu.Lock()
	if len(codingRouteCapsCache) > 0 &&
		codingRouteCapsCacheKey == key &&
		time.Since(codingRouteCapsCacheAt) < codingRouteCapabilitiesCacheTTL {
		out := append([]codingRouteCapability(nil), codingRouteCapsCache...)
		codingRouteCapsCacheMu.Unlock()
		return out
	}
	codingRouteCapsCacheMu.Unlock()

	primary := h.primaryMaclawModelName()
	hasReasoning := h.modelRouterHasReasoning()
	hasVision := h.modelRouterHasVision()

	// Prefer GetRoute metadata only (no full routeLLMConfig / primary reload)
	// so status polls stay cheap.
	reasoningModel := primary
	reasoningSource := "fallback"
	if hasReasoning && h != nil && h.app != nil && h.app.ohModules.modelRouter != nil {
		if route, ok := h.app.ohModules.modelRouter.GetRoute(llm.TaskReasoning); ok {
			if m := strings.TrimSpace(route.Model); m != "" {
				reasoningModel = m
			}
			reasoningSource = "route"
		}
	} else if !hasReasoning {
		reasoningSource = "primary"
	}

	visionModel := primary
	visionSource := "fallback"
	if hasVision && h != nil && h.app != nil && h.app.ohModules.modelRouter != nil {
		for _, task := range []llm.TaskType{llm.TaskVision, llm.TaskType("multimodal"), llm.TaskType("image")} {
			if route, ok := h.app.ohModules.modelRouter.GetRoute(task); ok {
				if m := strings.TrimSpace(route.Model); m != "" {
					visionModel = m
				}
				visionSource = "route"
				break
			}
		}
	} else {
		visionSource = "primary"
	}

	caps := []codingRouteCapability{
		{
			Pref:      codingRoutePrefAuto,
			Available: true,
			Model:     primary,
			Source:    "auto",
			Note:      "images→vision when configured; else reasoning route or primary",
		},
		{
			Pref:      codingRoutePrefPrimary,
			Available: true,
			Model:     primary,
			Source:    "primary",
			Note:      "always use main Maclaw model",
		},
		{
			Pref:      codingRoutePrefReasoning,
			Available: true, // always selectable; falls back to primary if no route
			Model:     reasoningModel,
			Source:    reasoningSource,
		},
		{
			Pref:      codingRoutePrefVision,
			Available: true,
			Model:     visionModel,
			Source:    visionSource,
		},
	}
	if !hasReasoning {
		caps[2].Note = "no ModelRoutes.reasoning — falls back to primary"
	}
	if !hasVision {
		caps[3].Note = "no ModelRoutes.vision — falls back to primary"
	}

	codingRouteCapsCacheMu.Lock()
	codingRouteCapsCache = append([]codingRouteCapability(nil), caps...)
	codingRouteCapsCacheAt = time.Now()
	codingRouteCapsCacheKey = key
	codingRouteCapsCacheMu.Unlock()
	return caps
}

func (h *IMMessageHandler) formatCodingRouteCapabilitiesMarkdown() string {
	caps := h.codingRouteCapabilities()
	var b strings.Builder
	b.WriteString("### ModelRouter 能力\n\n")
	b.WriteString("| Pref | Model | Source | Note |\n|------|-------|--------|------|\n")
	for _, c := range caps {
		note := c.Note
		if note == "" {
			note = "—"
		}
		b.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | %s |\n",
			c.Pref, emptyDash(c.Model), emptyDash(c.Source), note))
	}
	if h != nil && h.app != nil && h.app.ohModules.modelRouter != nil {
		if routes := h.app.ohModules.modelRouter.ListRoutes(); len(routes) > 0 {
			b.WriteString("\n配置的 routes：")
			keys := make([]string, 0, len(routes))
			for k := range routes {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString(strings.Join(keys, ", "))
			b.WriteString("\n")
		} else {
			b.WriteString("\n_（未配置 ModelRoutes — 全部走主模型）_\n")
		}
	} else {
		b.WriteString("\n_（ModelRouter 未初始化）_\n")
	}
	return b.String()
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
