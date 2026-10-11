package guiapp

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// TestTrustedBrowserPublishedWhenLaunchableRuntimeInstalled pins the servable
// ladder's third rung: an app restart leaves no live session and no CDP
// endpoint, but the host owns the managed launcher, so a confident browser
// turn must plan instead of HostRejecting the whole request as
// no_feasible_provider (the 2026-10-10 catalog-refusal incident). The
// override stands in for the installed-runtime probe so the plan exercises
// publication, not Chrome.
func TestTrustedBrowserPublishedWhenLaunchableRuntimeInstalled(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{registry: NewToolRegistry(), app: app}
	h.trustedBrowserServableOverride = func() bool { return true }
	if !semanticTrustedBrowserPublished(h) {
		t.Fatal("host with a launchable browser runtime must publish the trusted browser adapter")
	}
	registerBuiltinTools(h.registry, h)
	defs, surface, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "打开 https://app.uned.es/soid 完成注册", "desktop", "root-browser-launchable", "turn-browser-launchable",
		&intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: .92},
	)
	if err != nil || !handled || surface == nil || len(defs) == 0 {
		t.Fatalf("browser turn on servable host must plan handled=%v err=%v defs=%d", handled, err, len(defs))
	}
	if surface.plan.Selections[0].AdapterName != semanticTrustedBrowserAdapter {
		t.Fatalf("selection adapter=%q, want %s", surface.plan.Selections[0].AdapterName, semanticTrustedBrowserAdapter)
	}
}

// TestTrustedBrowserUnpublishedWhenNoBrowserInstalled pins the one state that
// stays closed: with no runtime alive, nothing discoverable, and no browser
// installed, publication would only re-create the same refusal one turn
// later, so the adapter stays unpublished and the turn keeps its explicit
// unmet contract.
func TestTrustedBrowserUnpublishedWhenNoBrowserInstalled(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{registry: NewToolRegistry(), app: app}
	h.trustedBrowserServableOverride = func() bool { return false }
	if semanticTrustedBrowserPublished(h) {
		t.Fatal("host with no browser runtime and nothing installed must not publish")
	}
	registerBuiltinTools(h.registry, h)
	_, _, handled, err := h.semanticCallSurfaceForSharedTurnWithIdentityAndClassification(
		"user-1", "打开 https://app.uned.es/soid 完成注册", "desktop", "root-browser-absent", "turn-browser-absent",
		&intent.ClassificationResult{Primary: intent.LabelBrowser, Confidence: .92},
	)
	if !handled || err == nil || !strings.Contains(err.Error(), "unmet") {
		t.Fatalf("browser turn on unservable host must stay an explicit unmet handled=%v err=%v", handled, err)
	}
}

// TestTrustedBrowserActionEnumMatchesWhatTheHostPerforms pins the fix for the
// 2026-10-10 rejection: a model handed an unlabelled action string guessed
// "open" and got trusted_browser_action_rejected, with nothing in the schema or
// the error telling it that "navigate" was the name that would have worked.
// The enum is only documentation for the model -- the canonicalizer does not
// read it -- so this holds the two against each other: every name the schema
// advertises must be one the check accepts, and a name outside the set must be
// refused. Drift in either direction is the incident returning.
func TestTrustedBrowserActionEnumMatchesWhatTheHostPerforms(t *testing.T) {
	schema := semanticTrustedBrowserInvocationSchema()
	properties, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no properties: %#v", schema)
	}
	action, ok := properties["action"].(map[string]interface{})
	if !ok {
		t.Fatalf("browser schema has no action property: %#v", properties)
	}
	enum, ok := action["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("action must publish a non-empty enum, got %#v", action["enum"])
	}
	for _, name := range enum {
		if !tool.BrowserControlActionAllowed(name) {
			t.Fatalf("schema advertises action %q but the execution check refuses it", name)
		}
	}
	for _, name := range tool.BrowserControlActions() {
		advertised := false
		for _, candidate := range enum {
			if candidate == name {
				advertised = true
				break
			}
		}
		if !advertised {
			t.Fatalf("execution check accepts action %q but the schema never shows it", name)
		}
	}
	// The guess that failed in production must still be refused at the
	// boundary: the enum is not the boundary, this check is.
	for _, guessed := range []string{"open", "goto", "visit", ""} {
		_, _, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{"action": guessed})
		if err == nil || !strings.Contains(err.Error(), "trusted_browser_action_rejected") {
			t.Fatalf("action %q = %v, want trusted_browser_action_rejected", guessed, err)
		}
		// The refusal must also name the real vocabulary. The legacy merged
		// browser surface answers the same guess with "unknown browser action
		// ... (supported: navigate, ...)"; rejecting the guess with less than
		// that is what turned one silent failure into a dead-end turn.
		for _, valid := range tool.BrowserControlActions() {
			if !strings.Contains(err.Error(), valid) {
				t.Fatalf("refusal for %q = %v does not mention the valid name %q", guessed, err, valid)
			}
		}
	}
	// An action that takes a url must not be accepted without one. The schema
	// says url is optional, so this is the only place the pairing is held.
	for _, name := range enum {
		if !tool.BrowserControlNeedsURL(name) {
			continue
		}
		if _, _, err := semanticTrustedBrowserArgsAllowed(map[string]interface{}{"action": name}); err == nil ||
			!strings.Contains(err.Error(), "trusted_browser_url_required") {
			t.Fatalf("%s without url = %v, want trusted_browser_url_required", name, err)
		}
	}
}
// bootstrap ladder: the launcher seam bypasses the production connect-user /
// managed-persistent attempts, and a seam that yields nothing surfaces as
// unavailable. The stub records which path ran so the bootstrap contract can
// be exercised without Chrome.
func TestEnsureTrustedBrowserSessionFallsBackToManagedLauncher(t *testing.T) {
	app := newProjectSearchTestApp(t)
	defer app.closeSemanticInvocationStore()
	h := &IMMessageHandler{app: app}
	launched := false
	h.trustedBrowserLauncher = func(principalID string) (*browser.BrowserAgentSession, error) {
		launched = principalID == "user-1"
		return &browser.BrowserAgentSession{}, nil
	}
	sess, err := h.ensureTrustedBrowserSession("user-1")
	if err != nil || sess == nil || !launched {
		t.Fatalf("launcher seam must satisfy bootstrap sess=%#v err=%v launched=%v", sess, err, launched)
	}
	h.trustedBrowserLauncher = func(string) (*browser.BrowserAgentSession, error) {
		return nil, nil
	}
	if _, err := h.ensureTrustedBrowserSession("user-1"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("failed bootstrap must surface as unavailable: %v", err)
	}
}
