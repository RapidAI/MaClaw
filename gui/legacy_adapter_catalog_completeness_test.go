package main

import (
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestLegacyAdapterCatalogCoversFullHostSurface is the migration-completeness
// audit for the closed legacy replacement boundary. Every static host
// definition the desktop handler can expose must carry a live reviewed
// provision: renderClosedLegacyReplacementSurface is fail-closed, so a single
// unprovisioned name empties the entire model tool surface at runtime
// ("tools unavailable" replies while routing logs show a healthy selection).
//
// Dynamic client/MCP/skill definitions are intentionally out of scope: they
// are bound per request after the host plan closes and never need a name
// provision. If a name appears here that is genuinely dynamic, remove it from
// the static registration path instead of weakening this audit.
func TestLegacyAdapterCatalogCoversFullHostSurface(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	handler := NewIMMessageHandler(app, nil)
	// Mirror the late desktop registration pass in app.go so the audit covers
	// the complete production host surface (GUI automation + computer use).
	statusC := make(chan StatusEvent, 32)
	blm := NewBackgroundLoopManager(statusC)
	registerGUIAutomationTools(handler.registry, blm, handler.agentActivity, statusC, app)
	registerComputerUseTools(handler.registry, app)
	registerGroupDiscussionTools(handler.registry, app, handler)
	handler.toolBuilder = NewDynamicToolBuilder(handler.registry)

	definitions := handler.getTools()
	if len(definitions) == 0 {
		t.Fatal("audit requires the full host tool surface, got none")
	}
	missing := legacyDefinitionsWithoutLiveProvisions(definitions)
	if len(missing) == 0 {
		return
	}
	for _, name := range missing {
		if _, ok := tool.LegacyAdapterProvisionForTool(name, time.Now().UTC()); ok {
			continue
		}
		t.Errorf("host tool %q is model-visible but has no reviewed legacy adapter provision", name)
	}
}
