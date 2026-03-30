package tool

import (
	"fmt"
	"strings"
	"sync"
)

// Registry manages registered tools with thread-safe access.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]*RegisteredTool
	onChange []func()
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]*RegisteredTool)}
}

// Register adds or replaces a tool in the registry.
func (r *Registry) Register(tool RegisteredTool) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if tool.Status == "" {
		tool.Status = StatusAvailable
	}
	// Auto-populate Body from BuiltinBodies when Body is empty.
	if tool.Body == "" {
		if body, ok := BuiltinBodies[tool.Name]; ok {
			tool.Body = body
			tool.BodySummary = TruncateBody(body, DefaultBodyMaxChars)
		}
	} else if tool.BodySummary == "" {
		// Body is set but BodySummary wasn't computed — fill it in.
		tool.BodySummary = TruncateBody(tool.Body, DefaultBodyMaxChars)
	}
	r.mu.Lock()
	cp := tool
	r.tools[tool.Name] = &cp
	cbs := append([]func(){}, r.onChange...)
	r.mu.Unlock()
	for _, fn := range cbs {
		fn()
	}
	return nil
}

// Unregister removes a tool from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	_, existed := r.tools[name]
	delete(r.tools, name)
	cbs := append([]func(){}, r.onChange...)
	r.mu.Unlock()
	if existed {
		for _, fn := range cbs {
			fn()
		}
	}
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (*RegisteredTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all registered tools.
func (r *Registry) List() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegisteredTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, *t)
	}
	return out
}

// ListAvailable returns tools with status "available".
func (r *Registry) ListAvailable() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Status == StatusAvailable {
			out = append(out, *t)
		}
	}
	return out
}

// ListByCategory returns tools matching the given category.
func (r *Registry) ListByCategory(cat Category) []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Category == cat {
			out = append(out, *t)
		}
	}
	return out
}

// ListByTags returns tools that have at least one matching tag.
func (r *Registry) ListByTags(tags []string) []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tagSet := make(map[string]bool, len(tags))
	for _, t := range tags {
		tagSet[strings.ToLower(t)] = true
	}
	var out []RegisteredTool
	for _, t := range r.tools {
		for _, tt := range t.Tags {
			if tagSet[strings.ToLower(tt)] {
				out = append(out, *t)
				break
			}
		}
	}
	return out
}

// UpdateStatus changes the status of a tool.
func (r *Registry) UpdateStatus(name string, status Status) {
	r.mu.Lock()
	if t, ok := r.tools[name]; ok {
		t.Status = status
	}
	r.mu.Unlock()
}

// AvailableTools returns tools available on the current platform.
// In headless environments, tools requiring display or clipboard are filtered out.
func (r *Registry) AvailableTools(platform PlatformChecker) []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Status != StatusAvailable {
			continue
		}
		if t.Caps.RequiresDisplay && !platform.HasDisplay() {
			continue
		}
		if t.Caps.RequiresClipboard && !platform.HasClipboard() {
			continue
		}
		out = append(out, *t)
	}
	return out
}

// OnChange registers a callback invoked when tools are added or removed.
func (r *Registry) OnChange(fn func()) {
	r.mu.Lock()
	r.onChange = append(r.onChange, fn)
	r.mu.Unlock()
}
