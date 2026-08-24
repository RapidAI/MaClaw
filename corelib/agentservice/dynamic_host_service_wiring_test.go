package agentservice

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A capability this host declares must be one this host can actually attach.
//
// The two halves live far apart. reviewedHostOwnedServices names a service per
// capability; prepareReviewedDynamicSemanticCatalog attaches a provider when
// that service is non-nil; reviewedHostOwnedServices() decides which ones get
// populated for a turn. Nothing connected them, and the gap was not
// theoretical: build.verify.local had a registry descriptor, an attach site, a
// projected provider and a working executor on coreAgentCallbacks, and the
// service field was never assigned by anything. Every Core Agent turn that
// needed it planned zero selections and handed the model an empty tool surface.
//
// That failure is invisible from either end. The attach site reads as
// conditional rather than dead, and a rule naming the capability looks served.
// It only shows up as turns that quietly do nothing, which is also what a
// correctly withheld capability looks like.
//
// The check is a source scan rather than a maximally-wired fixture. Building
// one would mean faking twenty-odd host services, and the resulting test would
// assert that the fixture is complete at least as much as that the wiring is.
// What actually needs guarding is narrower: no field may be left with no
// assignment anywhere in the package.

// serviceFieldsSetOutsideTheWiringFunction are populated per turn, after
// reviewedHostOwnedServices() has run, because they depend on what the request
// carried rather than on what the instance provides.
var serviceFieldsSetOutsideTheWiringFunction = map[string]string{
	"DocumentRead":    "bound from a trusted current-turn attachment (dynamic_semantic_routing.go)",
	"AudioTranscribe": "bound from trusted current-turn audio plus a ready speech engine (dynamic_semantic_routing.go)",
	"DestinationID":   "a string, not a service; carries the trusted destination alongside the senders",
}

func hostServiceFieldNames(t *testing.T) []string {
	t.Helper()
	typ := reflect.TypeOf(reviewedHostOwnedServices{})
	names := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		names = append(names, typ.Field(i).Name)
	}
	if len(names) == 0 {
		t.Fatal("reviewedHostOwnedServices has no fields; this check is looking at the wrong type")
	}
	return names
}

// assignedServiceFields collects field names appearing as an assignment target
// anywhere in the package's non-test sources.
//
// Matching on the selector name alone slightly over-approximates: an unrelated
// struct with a field of the same name would satisfy the check. That direction
// is the safe one. The failure being guarded against is a field with no
// assignment at all, and a name that appears nowhere cannot be shadowed into
// looking assigned.
func assignedServiceFields(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	assigned := make(map[string]bool)
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(".", name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		scanned++
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assign.Lhs {
				if selector, ok := target.(*ast.SelectorExpr); ok {
					assigned[selector.Sel.Name] = true
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no package sources were scanned; the check would pass vacuously")
	}
	return assigned
}

func TestEveryHostServiceCanActuallyBeWired(t *testing.T) {
	assigned := assignedServiceFields(t)
	for _, field := range hostServiceFieldNames(t) {
		if reason, exempt := serviceFieldsSetOutsideTheWiringFunction[field]; exempt {
			// The exemption still has to be true, or it becomes a place to
			// park real dead wiring.
			if !assigned[field] {
				t.Fatalf("field %q is exempted as %q but nothing assigns it anywhere", field, reason)
			}
			continue
		}
		if !assigned[field] {
			t.Fatalf("reviewedHostOwnedServices.%s has an attach site but no code ever populates it: "+
				"every turn needing that capability plans zero selections and gets an empty tool surface", field)
		}
	}
}

// The other half of the same seam: a rule may only name a capability the
// registry declares. This is checked at need-resolution time today
// (resolveIntentLabelCapabilityNeeds), which means a misconfigured rule set is
// discovered by a user's turn rather than at startup.
func TestEveryReviewedRuleNamesADeclaredCapability(t *testing.T) {
	registry, err := NewReviewedDynamicCapabilityRegistry()
	if err != nil {
		t.Fatalf("build reviewed registry: %v", err)
	}
	for label, templates := range ReviewedDynamicIntentCapabilityNeedRules() {
		if len(templates) == 0 {
			t.Fatalf("label %q has an empty rule, which reads as migrated but resolves to nothing", label)
		}
		for _, template := range templates {
			if _, ok := registry.Lookup(template.Capability); !ok {
				t.Fatalf("rule %q names capability %q, which the reviewed registry does not declare", label, template.Capability)
			}
		}
	}
}

// A rule's capability must also have somewhere to come from on this host.
// Anything host-owned needs an attach site in
// prepareReviewedDynamicSemanticCatalog; anything else has to be servable by a
// published MCP/Skill contract.
//
// Both sides are compared as Go identifiers rather than capability ID strings.
// Resolving the constants would mean following CapabilityX = coretool.CapabilityY
// through two packages to learn something the identifier already says, and the
// rules source names the same constants the attach sites do.
func TestEveryReviewedRuleCapabilityHasAProviderRoute(t *testing.T) {
	// information.lookup is served only by published MCP/Skill contracts. It
	// is the one family with no host provider by design, which is why it was
	// chosen as the first migrated family on the service hosts.
	dynamicOnly := map[string]bool{"CapabilityInformationLookup": true}

	attached := capabilityIdentifiersIn(t, "dynamic_host_clock.go", "prepareReviewedDynamicSemanticCatalog")
	declared := capabilityIdentifiersIn(t, "reviewed_dynamic_capabilities.go", "ReviewedDynamicIntentCapabilityNeedRules")

	for name := range declared {
		if dynamicOnly[name] || attached[name] {
			continue
		}
		t.Fatalf("a rule needs %s, but no host attach site mentions it and it is not marked dynamic-only: "+
			"turns for that family plan zero selections on every instance", name)
	}
}

// capabilityIdentifiersIn returns the Capability* identifiers named inside one
// function.
func capabilityIdentifiersIn(t *testing.T, filename, function string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(".", filename), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	names := make(map[string]bool)
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			return true
		}
		// A struct-literal key is an identifier too, and the rules are written
		// as `Capability: CapabilityFileRead`. Walking keys would collect the
		// field name itself, so keys are skipped at every depth.
		var walk func(ast.Node)
		walk = func(node ast.Node) {
			ast.Inspect(node, func(inner ast.Node) bool {
				if pair, ok := inner.(*ast.KeyValueExpr); ok {
					walk(pair.Value)
					return false
				}
				if ident, ok := inner.(*ast.Ident); ok && strings.HasPrefix(ident.Name, "Capability") {
					names[ident.Name] = true
				}
				return true
			})
		}
		walk(fn.Body)
		return false
	})
	if len(names) == 0 {
		t.Fatalf("no capability identifiers found in %s/%s; the scan is looking in the wrong place", filename, function)
	}
	return names
}
