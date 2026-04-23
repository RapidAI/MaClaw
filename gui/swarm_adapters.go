package main

// swarm_adapters.go — GUI adapter implementations for corelib/swarm interfaces.
// These adapters bridge GUI-specific types (RemoteSessionManager, App, etc.)
// to the abstract interfaces expected by swarm.SwarmOrchestrator.

import (
	"github.com/RapidAI/CodeClaw/corelib"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/swarm"
)

// ---------------------------------------------------------------------------
// SwarmSessionManager adapter
// ---------------------------------------------------------------------------

// guiSessionAdapter adapts RemoteSessionManager to swarm.SwarmSessionManager.
type guiSessionAdapter struct {
	manager *RemoteSessionManager
}

func (a *guiSessionAdapter) Create(spec swarm.SwarmLaunchSpec) (swarm.SwarmSession, error) {
	ls := LaunchSpec{
		Tool:         spec.Tool,
		ProjectPath:  spec.ProjectPath,
		Env:          spec.Env,
		LaunchSource: RemoteLaunchSourceAI,
	}
	session, err := a.manager.Create(ls)
	if err != nil {
		return nil, err
	}
	return &guiSessionWrapper{session: session}, nil
}

func (a *guiSessionAdapter) Get(sessionID string) (swarm.SwarmSession, bool) {
	s, ok := a.manager.Get(sessionID)
	if !ok {
		return nil, false
	}
	return &guiSessionWrapper{session: s}, true
}

func (a *guiSessionAdapter) Kill(sessionID string) error {
	return a.manager.Kill(sessionID)
}

func (a *guiSessionAdapter) WriteInput(sessionID, text string) error {
	return a.manager.WriteInput(sessionID, text)
}

// ---------------------------------------------------------------------------
// SwarmSession adapter
// ---------------------------------------------------------------------------

// guiSessionWrapper adapts RemoteSession to swarm.SwarmSession.
type guiSessionWrapper struct {
	session *RemoteSession
}

func (w *guiSessionWrapper) SessionID() string {
	return w.session.ID
}

func (w *guiSessionWrapper) SessionStatus() swarm.SessionStatus {
	w.session.mu.RLock()
	defer w.session.mu.RUnlock()
	return swarm.SessionStatus(w.session.Status)
}

func (w *guiSessionWrapper) SessionSummary() swarm.SwarmSessionSummary {
	w.session.mu.RLock()
	defer w.session.mu.RUnlock()
	return swarm.SwarmSessionSummary{
		Status:          string(w.session.Status),
		ProgressSummary: w.session.Summary.ProgressSummary,
		LastResult:      w.session.Summary.LastResult,
	}
}

func (w *guiSessionWrapper) SessionOutput() string {
	w.session.mu.RLock()
	defer w.session.mu.RUnlock()
	return w.session.Summary.LastResult
}

// ---------------------------------------------------------------------------
// SwarmAppContext adapter
// ---------------------------------------------------------------------------

// guiAppContext adapts *App to swarm.SwarmAppContext.
type guiAppContext struct {
	app *App
}

func (c *guiAppContext) ListInstalledTools() []swarm.InstalledToolInfo {
	views := c.app.ListRemoteToolMetadata()
	var result []swarm.InstalledToolInfo
	for _, v := range views {
		if v.Installed && v.CanStart {
			result = append(result, swarm.InstalledToolInfo{
				Name: v.Name, CanStart: v.CanStart,
			})
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// SwarmLLMCaller adapter
// ---------------------------------------------------------------------------

// guiLLMCaller adapts MaclawLLMConfig + doSimpleLLMRequest to swarm.SwarmLLMCaller.
type guiLLMCaller struct {
	cfg corelib.MaclawLLMConfig
}

func (c *guiLLMCaller) CallLLM(prompt string, temperature float64, timeout time.Duration) ([]byte, error) {
	if c.cfg.URL == "" {
		return nil, fmt.Errorf("LLM not configured")
	}
	messages := []interface{}{
		map[string]string{"role": "user", "content": prompt},
	}
	client := &http.Client{Timeout: timeout}
	result, err := doSimpleLLMRequest(context.Background(), c.cfg, messages, client, timeout)
	if err != nil {
		return nil, err
	}
	return []byte(result.Content), nil
}

// ---------------------------------------------------------------------------
// Notifier adapter
// ---------------------------------------------------------------------------

// guiNotifierAdapter adapts the corelib swarm.DefaultNotifier for GUI use.
// The GUI constructs a swarm.DefaultNotifier directly (via swarm.NewDefaultNotifier),
// so this adapter is only needed if the GUI SwarmNotifier interface is still
// referenced. For the direct integration, we use swarm.DefaultNotifier.
