package main

import (
	"sync"
	"testing"
)

func TestToolRegistry_RegisterAndGet(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(RegisteredTool{
		Name: "test_tool", Description: "a test tool",
		Category: ToolCategoryBuiltin, Status: RegToolAvailable,
		Handler: func(args map[string]interface{}) string { return "ok" },
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	tool, ok := r.Get("test_tool")
	if !ok {
		t.Fatal("Get returned false")
	}
	if tool.Name != "test_tool" {
		t.Errorf("Name = %q, want test_tool", tool.Name)
	}
	if tool.Handler == nil {
		t.Error("Handler is nil")
	}
}

func TestToolRegistry_RegisterEmptyName(t *testing.T) {
	r := NewToolRegistry()
	err := r.Register(RegisteredTool{Name: ""})
	if err == nil {
		t.Error("expected error for empty name")
	}
}

func TestToolRegistry_Unregister(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x", Category: ToolCategoryBuiltin})
	r.Unregister("x")
	if _, ok := r.Get("x"); ok {
		t.Error("tool should be unregistered")
	}
}

func TestToolRegistry_ListAvailable(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Status: RegToolAvailable})
	r.Register(RegisteredTool{Name: "b", Status: RegToolUnavailable})
	r.Register(RegisteredTool{Name: "c", Status: RegToolDegraded})

	avail := r.ListAvailable()
	if len(avail) != 1 {
		t.Errorf("ListAvailable len = %d, want 1", len(avail))
	}
	if avail[0].Name != "a" {
		t.Errorf("ListAvailable[0].Name = %q, want a", avail[0].Name)
	}
}

func TestToolRegistry_ListByCategory(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Category: ToolCategoryBuiltin})
	r.Register(RegisteredTool{Name: "b", Category: ToolCategoryMCP})
	r.Register(RegisteredTool{Name: "c", Category: ToolCategoryBuiltin})

	builtins := r.ListByCategory(ToolCategoryBuiltin)
	if len(builtins) != 2 {
		t.Errorf("ListByCategory(builtin) len = %d, want 2", len(builtins))
	}
}

func TestToolRegistry_ListByTags(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "a", Tags: []string{"git", "vcs"}})
	r.Register(RegisteredTool{Name: "b", Tags: []string{"file", "read"}})
	r.Register(RegisteredTool{Name: "c", Tags: []string{"git", "commit"}})

	gitTools := r.ListByTags([]string{"git"})
	if len(gitTools) != 2 {
		t.Errorf("ListByTags(git) len = %d, want 2", len(gitTools))
	}
}

func TestToolRegistry_UpdateStatus(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x", Status: RegToolAvailable})
	r.UpdateStatus("x", RegToolUnavailable)
	tool, _ := r.Get("x")
	if tool.Status != RegToolUnavailable {
		t.Errorf("Status = %q, want unavailable", tool.Status)
	}
}

func TestToolRegistry_OnChange(t *testing.T) {
	r := NewToolRegistry()
	called := 0
	r.OnChange(func() { called++ })
	r.Register(RegisteredTool{Name: "x"})
	if called != 1 {
		t.Errorf("OnChange called %d times, want 1", called)
	}
	r.Unregister("x")
	if called != 2 {
		t.Errorf("OnChange called %d times after unregister, want 2", called)
	}
}

func TestToolRegistry_DefaultStatus(t *testing.T) {
	r := NewToolRegistry()
	r.Register(RegisteredTool{Name: "x"}) // no status set
	tool, _ := r.Get("x")
	if tool.Status != RegToolAvailable {
		t.Errorf("default Status = %q, want available", tool.Status)
	}
}

func TestToolRegistry_ConcurrentAccess(t *testing.T) {
	r := NewToolRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := "tool_" + string(rune('a'+n%26))
			r.Register(RegisteredTool{Name: name, Category: ToolCategoryBuiltin})
			r.Get(name)
			r.ListAvailable()
			r.ListByCategory(ToolCategoryBuiltin)
		}(i)
	}
	wg.Wait()
}
