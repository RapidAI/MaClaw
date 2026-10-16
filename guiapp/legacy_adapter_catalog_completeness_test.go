package guiapp

import (
	"testing"
)

// TestLegacyAdapterCatalogCoversFullHostSurface is the migration-completeness
// audit for the closed legacy replacement boundary. Every static host
// definition the desktop handler can expose must carry a live reviewed
// provision. An unprovisioned name is dropped from the model surface (it
// cannot become authority without a review); the remainder still renders.
// This audit still fails if a registered host tool was never catalogued,
// which is how "database is selected then missing from the tool list" happens.
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
	present := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		present[extractToolName(definition)] = true
	}
	for _, name := range []string{"database", "database_query"} {
		if !present[name] {
			t.Errorf("host surface omitted required tool %q", name)
		}
	}
	missing := legacyDefinitionsWithoutLiveProvisions(definitions)
	for _, name := range missing {
		t.Errorf("host tool %q is model-visible but has no reviewed legacy adapter provision", name)
	}
}
