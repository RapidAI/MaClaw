package agent

import (
	"context"
	"strings"
	"testing"
)

func containsAnyFold(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(s, sub) || strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func TestCoreToolRegistry_RegisterAndExecute(t *testing.T) {
	r := NewCoreToolRegistry()
	r.Register(ToolEntry{
		Name:        "echo",
		Description: "echoes input",
		Properties:  map[string]interface{}{"text": map[string]string{"type": "string"}},
		Required:    []string{"text"},
		Handler:     func(args map[string]interface{}) string { return StringArg(args, "text") },
	})

	result := r.Execute("echo", map[string]interface{}{"text": "hello"})
	if result != "hello" {
		t.Errorf("Execute(echo) = %q, want %q", result, "hello")
	}

	result = r.Execute("unknown", nil)
	if !containsAnyFold(result, "未知工具", "unknown tool") {
		t.Errorf("Execute(unknown) = %q, want error message", result)
	}
}

func TestCoreToolRegistry_ExecuteCtxPrefersContextHandler(t *testing.T) {
	r := NewCoreToolRegistry()
	r.Register(ToolEntry{
		Name:    "ctx_echo",
		Handler: func(args map[string]interface{}) string { return "plain" },
		HandlerCtx: func(ctx context.Context, args map[string]interface{}) string {
			if err := ctx.Err(); err != nil {
				return "cancelled"
			}
			return StringArg(args, "text")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if got := r.ExecuteCtx(ctx, "ctx_echo", map[string]interface{}{"text": "hello"}); got != "cancelled" {
		t.Fatalf("ExecuteCtx(ctx_echo) = %q, want cancelled", got)
	}
	if got := r.Execute("ctx_echo", map[string]interface{}{"text": "hello"}); got != "hello" {
		t.Fatalf("Execute(ctx_echo) = %q, want hello", got)
	}
}

func TestCoreToolRegistry_BuildDefinitions(t *testing.T) {
	r := NewCoreToolRegistry()
	r.Register(ToolEntry{
		Name:        "tool_a",
		Description: "first tool",
		Handler:     func(args map[string]interface{}) string { return "" },
	})
	r.Register(ToolEntry{
		Name:        "tool_b",
		Description: "second tool",
		Required:    []string{"x"},
		Handler:     func(args map[string]interface{}) string { return "" },
	})
	// Internal tool with no description should be skipped.
	r.Register(ToolEntry{
		Name:    "internal",
		Handler: func(args map[string]interface{}) string { return "" },
	})

	defs := r.BuildDefinitions()
	if len(defs) != 2 {
		t.Fatalf("BuildDefinitions() returned %d defs, want 2", len(defs))
	}

	fn0 := defs[0]["function"].(map[string]interface{})
	fn1 := defs[1]["function"].(map[string]interface{})
	if fn0["name"] != "tool_a" {
		t.Errorf("defs[0].name = %v, want tool_a", fn0["name"])
	}
	if fn1["name"] != "tool_b" {
		t.Errorf("defs[1].name = %v, want tool_b", fn1["name"])
	}
}

func TestCoreToolRegistry_DefinitionAndHandlerBound(t *testing.T) {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})

	defs := r.BuildDefinitions()
	if len(defs) == 0 {
		t.Fatal("RegisterCoreTools produced 0 definitions")
	}

	for _, def := range defs {
		fn := def["function"].(map[string]interface{})
		name := fn["name"].(string)
		if !r.Has(name) {
			t.Errorf("definition for %q but not in registry", name)
		}
		result := r.Execute(name, map[string]interface{}{})
		if result == "" {
			t.Errorf("Execute(%q, {}) returned empty string", name)
		}
	}
}

func TestCoreToolRegistry_NilSSHHandler(t *testing.T) {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})

	result := r.Execute("ssh", map[string]interface{}{"action": "list"})
	if !containsAnyFold(result, "未初始化", "not initialized") {
		t.Errorf("ssh with nil handler = %q, want not-initialized message", result)
	}
}

func TestCoreToolRegistry_NilTaskStore(t *testing.T) {
	r := NewCoreToolRegistry()
	RegisterCoreTools(r, CoreToolDeps{})

	result := r.Execute("task", map[string]interface{}{"action": "list"})
	if !containsAnyFold(result, "未初始化", "not initialized") {
		t.Errorf("task with nil store = %q, want not-initialized message", result)
	}
}

func TestCoreToolRegistry_ReplaceExisting(t *testing.T) {
	r := NewCoreToolRegistry()
	r.Register(ToolEntry{
		Name:        "x",
		Description: "v1",
		Handler:     func(args map[string]interface{}) string { return "v1" },
	})
	r.Register(ToolEntry{
		Name:        "x",
		Description: "v2",
		Handler:     func(args map[string]interface{}) string { return "v2" },
	})

	if r.Execute("x", nil) != "v2" {
		t.Error("Replace did not update handler")
	}
	if len(r.Names()) != 1 {
		t.Errorf("Names() = %v, want 1 entry", r.Names())
	}
}
