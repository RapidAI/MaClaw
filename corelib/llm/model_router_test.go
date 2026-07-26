package llm

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestRoute_NoRouter_ReturnsPrimary(t *testing.T) {
	var r *ModelRouter
	primary := corelib.MaclawLLMConfig{URL: "https://api.example.com", Model: "gpt-4", Key: "key1"}
	got := r.Route(TaskDefault, primary)
	if got.Model != "gpt-4" {
		t.Errorf("nil router should return primary, got model=%s", got.Model)
	}
}

func TestRoute_NoMatchingRoute_ReturnsPrimary(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"intent": {Model: "gpt-4-mini"},
	})
	primary := corelib.MaclawLLMConfig{URL: "https://api.example.com", Model: "gpt-4", Key: "key1"}
	got := r.Route(TaskDefault, primary)
	if got.Model != "gpt-4" {
		t.Errorf("unmatched task should return primary, got model=%s", got.Model)
	}
}

func TestRoute_MatchingRoute_OverridesModel(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"intent": {Model: "gpt-4-mini"},
	})
	primary := corelib.MaclawLLMConfig{URL: "https://api.example.com", Model: "gpt-4", Key: "key1"}
	got := r.Route(TaskIntent, primary)
	if got.Model != "gpt-4-mini" {
		t.Errorf("intent route should override model, got %s", got.Model)
	}
	if got.URL != "https://api.example.com" {
		t.Errorf("URL should remain from primary, got %s", got.URL)
	}
	if got.Key != "key1" {
		t.Errorf("Key should remain from primary, got %s", got.Key)
	}
}

func TestRoute_FullOverride(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"reasoning": {
			Model:    "deepseek-coder",
			URL:      "https://deepseek.example.com/v1",
			Key:      "ds-key",
			Protocol: "openai",
			Provider: "DeepSeek",
		},
	})
	primary := corelib.MaclawLLMConfig{URL: "https://api.example.com", Model: "gpt-4", Key: "key1", Protocol: "openai"}
	got := r.Route(TaskReasoning, primary)
	if got.Model != "deepseek-coder" || got.URL != "https://deepseek.example.com/v1" || got.Key != "ds-key" || got.ProviderName != "DeepSeek" {
		t.Errorf("full override failed: %+v", got)
	}
}

func TestRoute_CaseInsensitive(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"INTENT": {Model: "fast-model"},
	})
	primary := corelib.MaclawLLMConfig{Model: "gpt-4"}
	got := r.Route(TaskIntent, primary)
	if got.Model != "fast-model" {
		t.Errorf("case-insensitive route failed, got model=%s", got.Model)
	}
}

func TestRouteWithAux_ExplicitRouteTakesPrecedence(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"summary": {Model: "explicit-summary-model"},
	})
	primary := corelib.MaclawLLMConfig{URL: "https://primary.com", Model: "gpt-4", Key: "pk"}
	aux := corelib.AuxiliaryLLMConfig{URL: "https://aux.com", Key: "ak", Model: "aux-model"}

	got := r.RouteWithAux(TaskSummary, primary, aux)
	if got.Model != "explicit-summary-model" {
		t.Errorf("explicit route should take precedence over aux, got model=%s", got.Model)
	}
}

func TestRouteWithAux_FallsBackToAuxForLightweightTasks(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{}) // no explicit routes
	primary := corelib.MaclawLLMConfig{URL: "https://primary.com", Model: "gpt-4", Key: "pk", ThinkingMode: "disabled", ReasoningEffort: "minimal"}
	aux := corelib.AuxiliaryLLMConfig{URL: "https://aux.com", Key: "ak", Model: "aux-model", Protocol: "openai"}

	for _, task := range []TaskType{TaskIntent, TaskFast, TaskSummary} {
		got := r.RouteWithAux(task, primary, aux)
		if got.Model != "aux-model" || got.URL != "https://aux.com" {
			t.Errorf("task %s should fall back to aux, got model=%s url=%s", task, got.Model, got.URL)
		}
		if got.ThinkingMode != "disabled" || got.ReasoningEffort != "minimal" {
			t.Errorf("task %s lost global reasoning controls: %+v", task, got)
		}
	}
}

func TestRouteWithAux_DoesNotUseAuxForHeavyTasks(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{})
	primary := corelib.MaclawLLMConfig{URL: "https://primary.com", Model: "gpt-4", Key: "pk"}
	aux := corelib.AuxiliaryLLMConfig{URL: "https://aux.com", Key: "ak", Model: "aux-model"}

	for _, task := range []TaskType{TaskDefault, TaskReasoning, TaskVision} {
		got := r.RouteWithAux(task, primary, aux)
		if got.Model != "gpt-4" {
			t.Errorf("task %s should NOT fall back to aux, got model=%s", task, got.Model)
		}
	}
}

func TestRouteWithAux_AuxNotConfigured_ReturnsPrimary(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{})
	primary := corelib.MaclawLLMConfig{URL: "https://primary.com", Model: "gpt-4", Key: "pk"}
	aux := corelib.AuxiliaryLLMConfig{} // not configured

	got := r.RouteWithAux(TaskSummary, primary, aux)
	if got.Model != "gpt-4" {
		t.Errorf("unconfigured aux should return primary, got model=%s", got.Model)
	}
}

func TestHasRoute(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"intent": {Model: "fast"},
	})
	if !r.HasRoute(TaskIntent) {
		t.Error("should have intent route")
	}
	if r.HasRoute(TaskVision) {
		t.Error("should not have vision route")
	}
}

func TestListRoutesAndGetRoute(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"reasoning": {Model: "r1"},
		"vision":    {Model: "v1"},
	})
	list := r.ListRoutes()
	if len(list) != 2 || list["reasoning"].Model != "r1" {
		t.Fatalf("%+v", list)
	}
	route, ok := r.GetRoute(TaskVision)
	if !ok || route.Model != "v1" {
		t.Fatalf("%+v %v", route, ok)
	}
	if _, ok := r.GetRoute(TaskFast); ok {
		t.Fatal("fast should be missing")
	}
	if NewModelRouter(nil).ListRoutes() != nil {
		t.Fatal("empty router list")
	}
}

func TestSetRoutes_ReplacesAll(t *testing.T) {
	r := NewModelRouter(map[string]ModelRoute{
		"intent": {Model: "old"},
	})
	r.SetRoutes(map[string]ModelRoute{
		"vision": {Model: "new-vision"},
	})
	if r.HasRoute(TaskIntent) {
		t.Error("old route should be gone")
	}
	if !r.HasRoute(TaskVision) {
		t.Error("new route should exist")
	}
}

func TestIsLightweightTask(t *testing.T) {
	lightweight := []TaskType{TaskIntent, TaskFast, TaskSummary}
	heavy := []TaskType{TaskDefault, TaskReasoning, TaskVision}

	for _, task := range lightweight {
		if !isLightweightTask(task) {
			t.Errorf("%s should be lightweight", task)
		}
	}
	for _, task := range heavy {
		if isLightweightTask(task) {
			t.Errorf("%s should NOT be lightweight", task)
		}
	}
}
