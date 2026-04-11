package im

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// Router manages multiple IM plugins and routes messages through a unified handler.
type Router struct {
	mu      sync.RWMutex
	plugins map[string]Plugin
	handler MessageHandler
}

// NewRouter creates a Router.
func NewRouter() *Router {
	return &Router{plugins: make(map[string]Plugin)}
}

// Register adds a plugin to the router.
func (r *Router) Register(p Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := p.Name()
	if _, exists := r.plugins[name]; exists {
		return fmt.Errorf("plugin %q already registered", name)
	}
	r.plugins[name] = p
	// Wire the plugin's incoming messages to our unified handler
	p.OnMessage(func(msg IncomingMessage) {
		r.mu.RLock()
		h := r.handler
		r.mu.RUnlock()
		if h != nil {
			h(msg)
		}
	})
	return nil
}

// OnMessage sets the unified handler for all incoming messages from all plugins.
func (r *Router) OnMessage(handler MessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handler = handler
}

// SendText sends a text message via the specified platform plugin.
func (r *Router) SendText(ctx context.Context, platform string, target UserTarget, text string) error {
	r.mu.RLock()
	p, ok := r.plugins[platform]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", platform)
	}
	return p.SendText(ctx, target, text)
}

// SendMarkdown sends a markdown message via the specified platform plugin.
func (r *Router) SendMarkdown(ctx context.Context, platform string, target UserTarget, markdown string) error {
	r.mu.RLock()
	p, ok := r.plugins[platform]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("plugin %q not found", platform)
	}
	return p.SendMarkdown(ctx, target, markdown)
}

// StartAll starts all registered plugins.
func (r *Router) StartAll(ctx context.Context) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.plugins {
		if err := p.Start(ctx); err != nil {
			log.Printf("[im-router] failed to start %s: %v", name, err)
		}
	}
}

// StopAll stops all registered plugins.
func (r *Router) StopAll(ctx context.Context) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.plugins {
		if err := p.Stop(ctx); err != nil {
			log.Printf("[im-router] failed to stop %s: %v", name, err)
		}
	}
}

// Plugins returns the names of all registered plugins.
func (r *Router) Plugins() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.plugins))
	for name := range r.plugins {
		names = append(names, name)
	}
	return names
}
