// Package routingarch is the architecture and boundary scanner for unified
// semantic tool routing (design doc docs/design/semantic-tool-routing-design-zh.md,
// section 9 phase C items 4 and 6).
//
// Phase C item 4 requires a machine-checkable rule that no GUI or business
// layer file modifies a tool set by tool name outside the catalog, the
// materializer and tests, and that no layer other than the execution adapter
// issues a Skill/MCP call by provider name.
//
// Phase C item 6 requires the same for three authorization boundaries: only a
// trusted fact producer may construct authorization/confirmation/health facts,
// only the renderer and executor may mint an invocation grant, and only the
// artifact broker may author an ArtifactRef handed to a provider.
//
// The scanner reports every site that exercises one of those boundaries. It
// deliberately does not decide whether a site is legitimate: the accompanying
// baseline in baseline.go is the reviewed allowlist, and the test fails both
// on a new unreviewed site and on a baseline entry that no longer matches.
// That makes the remaining legacy surface an explicit, shrinking inventory
// instead of a review convention.
package routingarch

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Rule identifies one design-C boundary.
type Rule string

const (
	// RuleToolSurfaceMutation covers design C-4 first clause: changing a tool
	// set by tool name. Every legacy name router, conditional keep table,
	// pin union, deferred activation and policy filter lands here.
	RuleToolSurfaceMutation Rule = "tool_surface_mutation"

	// RuleProviderNameCall covers design C-4 second clause: reaching a Skill
	// or MCP implementation through its discovery name instead of an
	// immutable binding.
	RuleProviderNameCall Rule = "provider_name_call"

	// RuleRoutingFactAuthoring covers design C-6 first clause. A RoutingFact
	// or RoutingConstraint literal is how authorization, confirmation and
	// provider-health claims enter planning, so only a trusted producer may
	// build one.
	RuleRoutingFactAuthoring Rule = "routing_fact_authoring"

	// RuleInvocationGrantMint covers design C-6 second clause. Both a grant
	// literal and an issuer constructor are grant-minting authority.
	RuleInvocationGrantMint Rule = "invocation_grant_mint"

	// RuleArtifactRefAuthoring covers design C-6 third clause. An ArtifactRef
	// or ArtifactPayload literal asserts artifact provenance, which only the
	// broker may do.
	RuleArtifactRefAuthoring Rule = "artifact_ref_authoring"
)

// AllRules lists every scanned boundary in a stable order.
func AllRules() []Rule {
	return []Rule{
		RuleToolSurfaceMutation,
		RuleProviderNameCall,
		RuleRoutingFactAuthoring,
		RuleInvocationGrantMint,
		RuleArtifactRefAuthoring,
	}
}

// Finding is one source site that exercises a boundary. Key intentionally
// excludes the line number so that unrelated edits in the same file do not
// churn the baseline.
type Finding struct {
	Rule   Rule
	File   string // slash-separated, relative to the repository root
	Symbol string
	Line   int
}

// Key is the baseline identity of a finding.
func (f Finding) Key() string { return f.File + ":" + f.Symbol }

func (f Finding) String() string {
	return fmt.Sprintf("%s %s:%d %s", f.Rule, f.File, f.Line, f.Symbol)
}

// toolSurfaceMutationPrefixes are the function-name prefixes that denote a
// tool-name-keyed tool set change. Matching on the declaration and on every
// call site keeps a helper from being smuggled in behind a wrapper.
var toolSurfaceMutationPrefixes = []string{
	"filterTools",
	"FilterTools",
	"ensureTool",
	"EnsureTool",
	"removeTool",
	"RemoveTool",
	"augmentTools",
	"restoreTools",
	"routeTools",
	"RouteTools",
	"conditionalKeep",
	"ConditionalKeep",
	"matchConditionalKeep",
	"pinTool",
	"PinTool",
	"unionTools",
	"mergeTools",
	"injectTool",
}

// providerNameCallSelectors are the name-based Skill/MCP entry points. The
// binding-based CallBoundTool / CallBoundSkill pair is intentionally absent:
// those are the compliant execution path.
//
// The list is the concrete set of selectors that reach a provider by a name
// string rather than by a revalidated binding. Naming them individually is
// deliberate: a prefix rule over "Run"/"Call" would either miss the wrappers
// that carry the name under a different verb, or drown the rule in transport
// helpers that only ever receive an already-bound identity.
var providerNameCallSelectors = map[string]bool{
	// Skill execution reached by name.
	"RunSkill":                   true,
	"toolRunSkill":               true,
	"StartRun":                   true,
	"StartRunForOwner":           true,
	"ExecuteWithArgs":            true,
	"executeSkillByNameDetailed": true,
	"RunSubSkill":                true,
	"skillRunDetailed":           true,
	"skillRunPipelineDetailed":   true,
	// MCP tools reached by server/tool name.
	"CallTool":           true,
	"CallMCPTool":        true,
	"CallToolForOwner":   true,
	"toolCallMCPTool":    true,
	"executeCallMCPTool": true,
}

// grantMintSelectors are the invocation-issuer constructors.
var grantMintSelectors = map[string]bool{
	"NewInvocationIssuer":                true,
	"NewInvocationIssuerWithStore":       true,
	"NewRandomInvocationIssuer":          true,
	"NewRandomInvocationIssuerWithStore": true,
}

// literalRules maps a composite-literal type name to the boundary it crosses.
var literalRules = map[string]Rule{
	"RoutingFact":       RuleRoutingFactAuthoring,
	"RoutingConstraint": RuleRoutingFactAuthoring,
	"InvocationGrant":   RuleInvocationGrantMint,
	"ArtifactRef":       RuleArtifactRefAuthoring,
	"ArtifactPayload":   RuleArtifactRefAuthoring,
}

// skippedDirs are generated, vendored or non-product trees. They are not part
// of the routing architecture and must not enter the baseline.
var skippedDirs = map[string]bool{
	"node_modules":       true,
	"vendor":             true,
	"third_party":        true,
	"build":              true,
	"releases":           true,
	"managed_components": true,
	"dist":               true,
	"frontend":           true,
	"testdata":           true,
}

// exemptPackages are the scanner's own package and the planner evaluation
// harness. Design C-4 exempts tests; routingeval is test-only infrastructure
// that constructs synthetic facts and catalogs by definition.
var exemptPackages = map[string]bool{
	"corelib/tool/routingarch": true,
	"corelib/tool/routingeval": true,
}

// Scan parses every non-test Go file under root and returns the boundary
// sites, sorted by rule then key.
func Scan(root string) ([]Finding, error) {
	var findings []Finding
	seen := make(map[string]bool)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			// Dot directories are tooling state: the module cache, the git
			// database and scratch space. The module cache in particular
			// holds third-party files that do not parse.
			if strings.HasPrefix(entry.Name(), ".") || skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if exemptPackages[filepath.ToSlash(filepath.Dir(rel))] {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// A file this scanner cannot parse must not silently disable the
			// boundary check for its package.
			return fmt.Errorf("parse %s: %w", rel, parseErr)
		}
		for _, found := range scanFile(fset, file, rel) {
			key := string(found.Rule) + "|" + found.Key()
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, found)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Rule != findings[j].Rule {
			return findings[i].Rule < findings[j].Rule
		}
		return findings[i].Key() < findings[j].Key()
	})
	return findings, nil
}

func scanFile(fset *token.FileSet, file *ast.File, rel string) []Finding {
	var findings []Finding
	record := func(rule Rule, symbol string, pos token.Pos) {
		findings = append(findings, Finding{
			Rule:   rule,
			File:   rel,
			Symbol: symbol,
			Line:   fset.Position(pos).Line,
		})
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.FuncDecl:
			if typed.Name != nil && matchesToolSurfacePrefix(typed.Name.Name) {
				record(RuleToolSurfaceMutation, typed.Name.Name, typed.Name.Pos())
			}
		case *ast.CallExpr:
			name := calleeName(typed.Fun)
			if name == "" {
				return true
			}
			if matchesToolSurfacePrefix(name) {
				record(RuleToolSurfaceMutation, name, typed.Pos())
			}
			if providerNameCallSelectors[name] {
				record(RuleProviderNameCall, name, typed.Pos())
			}
			if grantMintSelectors[name] {
				record(RuleInvocationGrantMint, name, typed.Pos())
			}
		case *ast.CompositeLit:
			name := literalTypeName(typed.Type)
			if rule, ok := literalRules[name]; ok {
				record(rule, name, typed.Pos())
			}
		}
		return true
	})
	return findings
}

func matchesToolSurfacePrefix(name string) bool {
	for _, prefix := range toolSurfaceMutationPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// calleeName returns the called identifier for both package-level calls and
// method calls. A call through a variable of function type resolves to the
// variable name, which is enough to catch an aliased helper.
func calleeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		if typed.Sel == nil {
			return ""
		}
		return typed.Sel.Name
	case *ast.IndexExpr:
		return calleeName(typed.X)
	case *ast.IndexListExpr:
		return calleeName(typed.X)
	}
	return ""
}

// literalTypeName resolves the type name of a composite literal for both the
// in-package (RoutingFact{}) and qualified (tool.RoutingFact{}) forms, plus
// slice, array, map and pointer element positions.
func literalTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		if typed.Sel == nil {
			return ""
		}
		return typed.Sel.Name
	case *ast.StarExpr:
		return literalTypeName(typed.X)
	case *ast.ArrayType:
		return literalTypeName(typed.Elt)
	case *ast.MapType:
		return literalTypeName(typed.Value)
	}
	return ""
}

// RepositoryRoot walks up from dir until it finds the module root.
func RepositoryRoot(dir string) (string, error) {
	current := dir
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		current = parent
	}
}
