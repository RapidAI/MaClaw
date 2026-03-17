package main

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Tests for IMMessageHandler dynamic tool integration (Task 6.3)
// ---------------------------------------------------------------------------

// TestGetTools_FallbackWithoutGenerator verifies that getTools() returns
// the hardcoded buildToolDefinitions() output when no generator is set.
func TestGetTools_FallbackWithoutGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.getTools()
	if len(tools) != 14 {
		t.Fatalf("expected 14 builtin tools, got %d", len(tools))
	}

	// Verify first tool is list_sessions.
	name := extractToolName(tools[0])
	if name != "list_sessions" {
		t.Errorf("expected first tool to be list_sessions, got %s", name)
	}
}

// TestGetTools_UsesGeneratorWhenSet verifies that getTools() delegates to
// the ToolDefinitionGenerator when configured.
func TestGetTools_UsesGeneratorWhenSet(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	tools := handler.getTools()
	// With nil registry, generator returns only builtins.
	if len(tools) != 14 {
		t.Fatalf("expected 14 tools from generator (nil registry), got %d", len(tools))
	}
}

// TestGetTools_CacheWithin5Seconds verifies that repeated calls within 5s
// return the cached result without regenerating.
func TestGetTools_CacheWithin5Seconds(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// First call populates cache.
	tools1 := handler.getTools()
	// Second call should return same slice from cache.
	tools2 := handler.getTools()

	if len(tools1) != len(tools2) {
		t.Fatalf("cached tools length mismatch: %d vs %d", len(tools1), len(tools2))
	}

	// Verify cache timestamp was set.
	handler.toolsMu.RLock()
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be set after getTools()")
	}
}

// TestGetTools_CacheInvalidatedBySetGenerator verifies that calling
// SetToolDefGenerator invalidates the cache.
func TestGetTools_CacheInvalidatedBySetGenerator(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	builtins := handler.buildToolDefinitions()
	gen := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen)

	// Populate cache.
	_ = handler.getTools()

	// Set a new generator — should invalidate cache.
	gen2 := NewToolDefinitionGenerator(nil, builtins)
	handler.SetToolDefGenerator(gen2)

	handler.toolsMu.RLock()
	cached := handler.cachedTools
	cacheTime := handler.toolsCacheTime
	handler.toolsMu.RUnlock()

	if cached != nil {
		t.Error("expected cachedTools to be nil after SetToolDefGenerator")
	}
	if !cacheTime.IsZero() {
		t.Error("expected toolsCacheTime to be zero after SetToolDefGenerator")
	}
}

// TestRouteTools_NoRouterReturnsAll verifies that routeTools returns all
// tools unchanged when no router is configured.
func TestRouteTools_NoRouterReturnsAll(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("hello", tools)

	if len(routed) != len(tools) {
		t.Fatalf("expected %d tools without router, got %d", len(tools), len(routed))
	}
}

// TestRouteTools_WithRouterFilters verifies that routeTools delegates to
// the ToolRouter when configured.
func TestRouteTools_WithRouterFilters(t *testing.T) {
	handler := &IMMessageHandler{
		app: &App{},
	}

	gen := NewToolDefinitionGenerator(nil, handler.buildToolDefinitions())
	router := NewToolRouter(gen)
	handler.SetToolRouter(router)

	// With only 12 tools (below threshold of 20), router returns all.
	tools := handler.buildToolDefinitions()
	routed := handler.routeTools("test message", tools)

	if len(routed) != len(tools) {
		t.Fatalf("expected %d tools (below threshold), got %d", len(tools), len(routed))
	}
}

// TestToolsCacheTTL_Value verifies the cache TTL constant is 5 seconds.
func TestToolsCacheTTL_Value(t *testing.T) {
	expected := 5 * time.Second
	if toolsCacheTTL != expected {
		t.Errorf("expected toolsCacheTTL = %v, got %v", expected, toolsCacheTTL)
	}
}
