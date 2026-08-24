package routingarch

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests prove each detector actually fires. Without them the repository
// scan could pass simply because the walk or the matchers stopped working,
// which would turn the whole boundary check into a no-op.

func writeSource(t *testing.T, root, rel, source string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scanKeys(t *testing.T, root string) map[Rule]map[string]bool {
	t.Helper()
	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	keys := make(map[Rule]map[string]bool)
	for _, finding := range findings {
		if keys[finding.Rule] == nil {
			keys[finding.Rule] = make(map[string]bool)
		}
		keys[finding.Rule][finding.Key()] = true
	}
	return keys
}

func TestScannerDetectsEveryBoundary(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "gui/surface.go", `package gui

import coretool "example.com/corelib/tool"

func filterToolsForProbe(tools []string) []string { return tools }

func useSurface(tools []string) []string {
	_ = coretool.RoutingFact{}
	_ = coretool.RoutingConstraint{}
	_ = coretool.InvocationGrant{}
	_ = coretool.ArtifactRef{}
	_ = []coretool.ArtifactPayload{{}}
	issuer, _ := coretool.NewRandomInvocationIssuer()
	_ = issuer
	bridge.RunSkill(ctx, principal, "skill-name", nil)
	bridge.CallTool(ctx, principal, "server", "tool", nil)
	return filterToolsForProbe(tools)
}
`)

	keys := scanKeys(t, root)
	for _, tc := range []struct {
		rule Rule
		key  string
	}{
		{RuleToolSurfaceMutation, "gui/surface.go:filterToolsForProbe"},
		{RuleRoutingFactAuthoring, "gui/surface.go:RoutingFact"},
		{RuleRoutingFactAuthoring, "gui/surface.go:RoutingConstraint"},
		{RuleInvocationGrantMint, "gui/surface.go:InvocationGrant"},
		{RuleInvocationGrantMint, "gui/surface.go:NewRandomInvocationIssuer"},
		{RuleArtifactRefAuthoring, "gui/surface.go:ArtifactRef"},
		{RuleArtifactRefAuthoring, "gui/surface.go:ArtifactPayload"},
		{RuleProviderNameCall, "gui/surface.go:RunSkill"},
		{RuleProviderNameCall, "gui/surface.go:CallTool"},
	} {
		if !keys[tc.rule][tc.key] {
			t.Errorf("rule %q did not detect %s", tc.rule, tc.key)
		}
	}
}

// A helper reached only through a wrapper must still be reported, otherwise
// renaming the call site would be enough to leave the inventory.
func TestScannerReportsDeclarationAndCallSiteSeparately(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "corelib/owner/decl.go", `package owner

func filterToolsForOwner(tools []string) []string { return tools }
`)
	writeSource(t, root, "gui/caller.go", `package gui

func wrap(tools []string) []string { return owner.filterToolsForOwner(tools) }
`)

	keys := scanKeys(t, root)
	if !keys[RuleToolSurfaceMutation]["corelib/owner/decl.go:filterToolsForOwner"] {
		t.Error("declaration site not reported")
	}
	if !keys[RuleToolSurfaceMutation]["gui/caller.go:filterToolsForOwner"] {
		t.Error("call site not reported")
	}
}

// Tests are exempt per design C-4, and the binding-based execution path is
// the compliant one. Neither may enter the inventory or the baseline would
// grow with sites that are not violations.
func TestScannerSkipsTestsAndBindingCalls(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "gui/surface_test.go", `package gui

func filterToolsInTest(tools []string) []string { return tools }
`)
	writeSource(t, root, "gui/bound.go", `package gui

func execute() {
	bridge.CallBoundTool(ctx, principal, binding, nil)
	bridge.CallBoundSkill(ctx, principal, binding, nil)
}
`)

	findings, err := Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("unexpected finding %s", finding)
	}
}

func TestScannerFailsOnUnparsableSource(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "gui/broken.go", "package gui\n\nfunc broken( {\n")
	if _, err := Scan(root); err == nil {
		t.Fatal("a file the scanner cannot parse must fail the scan, not silently skip its package")
	}
}
