package agent

// tool_registry.go provides a unified tool registry where each tool's
// definition (schema) and handler are registered together in one place.
//
// This is the mechanism-level fix for the "two independent lists" problem:
// previously tool definitions lived in tool_definitions.go and tool dispatch
// lived in a switch-case in app.go. Adding a tool required editing both files
// and keeping them in sync — a classic workaround that breaks on the next change.
//
// Now: register once, get both definition and dispatch automatically.
// GUI extends this registry with GUI-only tools (coding sessions, browser, etc.).
// TUI uses it directly.

import (
	"fmt"
	"sort"
	"sync"
)

// ToolHandler is a function that executes a tool and returns the result.
type ToolHandler func(args map[string]interface{}) string

// ToolEntry binds a tool's LLM-facing definition with its execution handler.
type ToolEntry struct {
	Name        string
	Description string
	Properties  map[string]interface{}
	Required    []string
	Handler     ToolHandler
}

// CoreToolRegistry holds registered tools with their definitions and handlers.
type CoreToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]*ToolEntry
	order []string // insertion order for deterministic iteration
}

// NewCoreToolRegistry creates an empty registry.
func NewCoreToolRegistry() *CoreToolRegistry {
	return &CoreToolRegistry{tools: make(map[string]*ToolEntry)}
}

// Register adds a tool to the registry. If a tool with the same name exists,
// it is replaced.
func (r *CoreToolRegistry) Register(entry ToolEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[entry.Name]; !exists {
		r.order = append(r.order, entry.Name)
	}
	cp := entry
	r.tools[entry.Name] = &cp
}

// Execute dispatches a tool call by name. Returns the result string.
// Returns an error message if the tool is not found.
func (r *CoreToolRegistry) Execute(name string, args map[string]interface{}) string {
	r.mu.RLock()
	entry, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return fmt.Sprintf("未知工具: %s", name)
	}
	if entry.Handler == nil {
		return fmt.Sprintf("工具 %s 未实现 handler", name)
	}
	return entry.Handler(args)
}

// BuildDefinitions returns the OpenAI-compatible tool definitions for all
// registered tools, in registration order.
func (r *CoreToolRegistry) BuildDefinitions() []map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]map[string]interface{}, 0, len(r.order))
	for _, name := range r.order {
		entry := r.tools[name]
		if entry.Description == "" {
			continue // skip alias/internal tools with no description
		}
		defs = append(defs, ToolDef(entry.Name, entry.Description, entry.Properties, entry.Required))
	}
	return defs
}

// Has returns true if a tool with the given name is registered.
func (r *CoreToolRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.tools[name]
	return ok
}

// MissingTools returns tool names that are in the required set but not
// registered in this registry. Hosts should call this after registration
// to detect configuration gaps early.
func (r *CoreToolRegistry) MissingTools(required map[string]bool) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var missing []string
	for name := range required {
		if _, ok := r.tools[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// Names returns all registered tool names in registration order.
func (r *CoreToolRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}
