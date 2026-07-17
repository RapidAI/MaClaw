package commands

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstallCLICatalogNonEmpty(t *testing.T) {
	if len(InstallCLICatalog) == 0 {
		t.Fatal("empty InstallCLICatalog")
	}
	if len(installCLILookups) != len(InstallCLICatalog) {
		t.Fatalf("lookups=%d catalog=%d", len(installCLILookups), len(InstallCLICatalog))
	}
	for cmd, spec := range InstallCLICatalog {
		if len(spec.Actions) == 0 {
			t.Fatalf("%s: no actions", cmd)
		}
		// Nested parents must also appear as top-level actions (CLI routes
		// `plugin marketplace ...` via the top-level switch first).
		for parent := range spec.Nested {
			if !InstallCLIHasAction(cmd, parent) {
				t.Fatalf("%s: nested parent %q missing from top-level Actions", cmd, parent)
			}
			if len(spec.Nested[parent]) == 0 {
				t.Fatalf("%s/%s: empty nested actions", cmd, parent)
			}
		}
		// Lookup tables must answer true for every catalog entry.
		for _, a := range spec.Actions {
			if !InstallCLIHasAction(cmd, a) {
				t.Fatalf("lookup miss %s %s", cmd, a)
			}
		}
		for parent, subs := range spec.Nested {
			if !InstallCLIHasNestedParent(cmd, parent) {
				t.Fatalf("nested parent lookup miss %s %s", cmd, parent)
			}
			for _, s := range subs {
				if !InstallCLIHasNestedAction(cmd, parent, s) {
					t.Fatalf("nested action lookup miss %s %s %s", cmd, parent, s)
				}
			}
		}
	}
	// Negative checks.
	if InstallCLIHasAction("skill", "nope") || InstallCLIHasNestedAction("plugin", "marketplace", "destroy") {
		t.Fatal("false positive lookup")
	}
}

func TestPluginMarketplaceNestedActionsShared(t *testing.T) {
	// marketplace and market must reference the same sub-action list (DRY).
	a := InstallCLICatalog["plugin"].Nested["marketplace"]
	b := InstallCLICatalog["plugin"].Nested["market"]
	if len(a) == 0 || len(a) != len(b) {
		t.Fatalf("len marketplace=%d market=%d", len(a), len(b))
	}
	// Same underlying slice (identity), not just equal content.
	if &a[0] != &b[0] {
		t.Fatal("marketplace/market nested actions should share one slice")
	}
}

var (
	// Match `case "a", "b":` lines and capture the quoted list.
	installCaseLineRe = regexp.MustCompile(`(?m)^\s*case\s+((?:"[^"]+"\s*,\s*)*"[^"]+")\s*:`)
	installCaseTokRe  = regexp.MustCompile(`"([^"]+)"`)
	// Slice RunX function body: from `func RunX` to the next top-level `func `.
	// (?m) so ^ matches line starts; (?s) so . matches newlines.
	runSkillBodyRe          = regexp.MustCompile(`(?ms)func RunSkill\b.*?^func `)
	runMCPBodyRe            = regexp.MustCompile(`(?ms)func RunMCP\b.*?^func `)
	runPluginBodyRe         = regexp.MustCompile(`(?ms)func RunPlugin\b.*?^func `)
	pluginMarketplaceBodyRe = regexp.MustCompile(`(?ms)func pluginMarketplaceCmd\b.*?^func `)
)

func readCommandsFile(t *testing.T, name string) string {
	t.Helper()
	// go test runs with package directory as cwd.
	data, err := os.ReadFile(name)
	if err != nil {
		data, err = os.ReadFile(filepath.Join("tui", "commands", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
	}
	return string(data)
}

func caseTokens(src string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range installCaseLineRe.FindAllStringSubmatch(src, -1) {
		for _, tok := range installCaseTokRe.FindAllStringSubmatch(m[1], -1) {
			out[tok[1]] = struct{}{}
		}
	}
	return out
}

func extractFuncBody(src string, re *regexp.Regexp) string {
	m := re.FindString(src)
	if m == "" {
		return ""
	}
	// Drop the trailing `func ` that closed the match.
	if i := strings.LastIndex(m, "\nfunc "); i >= 0 {
		return m[:i]
	}
	return m
}

// TestInstallCLICatalogMatchesSwitchSources keeps catalog ↔ switch cases aligned.
//
// Direction A: every catalog action appears as a case "..." in the relevant
// Run*/marketplace function bodies (not the whole file — avoids false matches
// from unrelated switches in the same file).
// Direction B: every case token in those bodies appears in the catalog.
func TestInstallCLICatalogMatchesSwitchSources(t *testing.T) {
	type check struct {
		cmd    string
		file   string
		bodies []*regexp.Regexp
	}
	checks := []check{
		{cmd: "skill", file: "skill.go", bodies: []*regexp.Regexp{runSkillBodyRe}},
		{cmd: "mcp", file: "mcp.go", bodies: []*regexp.Regexp{runMCPBodyRe}},
		{cmd: "plugin", file: "plugin.go", bodies: []*regexp.Regexp{runPluginBodyRe, pluginMarketplaceBodyRe}},
	}

	for _, c := range checks {
		src := readCommandsFile(t, c.file)
		spec, ok := InstallCLICatalog[c.cmd]
		if !ok {
			t.Fatalf("missing catalog for %s", c.cmd)
			continue
		}

		// Union of case tokens from relevant function bodies only.
		fromBodies := map[string]struct{}{}
		for _, bodyRe := range c.bodies {
			body := extractFuncBody(src, bodyRe)
			if body == "" {
				t.Errorf("%s: could not extract function body via %v in %s", c.cmd, bodyRe, c.file)
				continue
			}
			for tok := range caseTokens(body) {
				fromBodies[tok] = struct{}{}
			}
		}
		if len(fromBodies) == 0 {
			t.Fatalf("%s: no case tokens in switch bodies of %s", c.cmd, c.file)
		}

		// A) catalog → switch bodies
		for _, a := range spec.Actions {
			if _, ok := fromBodies[a]; !ok {
				t.Errorf("%s catalog action %q not found as case literal in %s bodies", c.cmd, a, c.file)
			}
		}
		for parent, subs := range spec.Nested {
			if _, ok := fromBodies[parent]; !ok {
				t.Errorf("%s nested parent %q not found as case literal in %s bodies", c.cmd, parent, c.file)
			}
			for _, s := range subs {
				if _, ok := fromBodies[s]; !ok {
					t.Errorf("%s nested action %q not found as case literal in %s bodies", c.cmd, s, c.file)
				}
			}
		}

		// B) switch bodies → catalog
		catalogSet := map[string]struct{}{}
		for _, a := range spec.Actions {
			catalogSet[a] = struct{}{}
		}
		for parent, subs := range spec.Nested {
			catalogSet[parent] = struct{}{}
			for _, s := range subs {
				catalogSet[s] = struct{}{}
			}
		}
		for tok := range fromBodies {
			if _, ok := catalogSet[tok]; !ok {
				t.Errorf("%s case %q in switch body missing from InstallCLICatalog", c.cmd, tok)
			}
		}
	}
}
