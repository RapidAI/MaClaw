package main

import (
	"fmt"
	"strings"
	"sync"
)

type ToolCategory string

const (
	ToolCategoryBuiltin ToolCategory = "builtin"
	ToolCategoryMCP     ToolCategory = "mcp"
	ToolCategorySkill   ToolCategory = "skill"
	ToolCategoryNonCode ToolCategory = "non_code"
)

type RegToolStatus string

const (
	RegToolAvailable   RegToolStatus = "available"
	RegToolDegraded    RegToolStatus = "degraded"
	RegToolUnavailable RegToolStatus = "unavailable"
)

type ToolHandler func(args map[string]interface{}) string
type ToolHandlerWithProgress func(args map[string]interface{}, onProgress ProgressCallback) string

type RegisteredTool struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Category    ToolCategory            `json:"category"`
	Tags        []string                `json:"tags"`
	Priority    int                     `json:"priority"`
	Status      RegToolStatus           `json:"status"`
	InputSchema map[string]interface{}  `json:"input_schema"`
	Required    []string                `json:"required"`
	Source      string                  `json:"source"`
	Handler     ToolHandler             `json:"-"`
	HandlerProg ToolHandlerWithProgress `json:"-"`
}

type ToolRegistry struct {
	mu       sync.RWMutex
	tools    map[string]*RegisteredTool
	onChange []func()
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[string]*RegisteredTool)}
}

func (r *ToolRegistry) Register(tool RegisteredTool) error {
	if tool.Name == "" {
		return fmt.Errorf("tool name cannot be empty")
	}
	if tool.Status == "" {
		tool.Status = RegToolAvailable
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

func (r *ToolRegistry) Unregister(name string) {
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

func (r *ToolRegistry) Get(name string) (*RegisteredTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

func (r *ToolRegistry) List() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegisteredTool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, *t)
	}
	return out
}

func (r *ToolRegistry) ListAvailable() []RegisteredTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RegisteredTool
	for _, t := range r.tools {
		if t.Status == RegToolAvailable {
			out = append(out, *t)
		}
	}
	return out
}

func (r *ToolRegistry) ListByCategory(cat ToolCategory) []RegisteredTool {
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

func (r *ToolRegistry) ListByTags(tags []string) []RegisteredTool {
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

func (r *ToolRegistry) UpdateStatus(name string, status RegToolStatus) {
	r.mu.Lock()
	if t, ok := r.tools[name]; ok {
		t.Status = status
	}
	r.mu.Unlock()
}

func (r *ToolRegistry) OnChange(fn func()) {
	r.mu.Lock()
	r.onChange = append(r.onChange, fn)
	r.mu.Unlock()
}
