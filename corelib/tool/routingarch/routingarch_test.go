package routingarch

import (
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root, err := RepositoryRoot(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func scanRepository(t *testing.T) []Finding {
	t.Helper()
	findings, err := Scan(repositoryRoot(t))
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("scanner found no boundary sites at all, which means the walk or the detectors broke")
	}
	return findings
}

// TestNoUnreviewedBoundaryCrossing is design C-4 and C-6. A new file that
// changes a tool set by name, calls a provider by name, authors a routing
// fact, mints an invocation grant or authors an ArtifactRef must be reviewed
// into baseline.go with a reason and a deletion condition.
func TestNoUnreviewedBoundaryCrossing(t *testing.T) {
	var unreviewed []string
	for _, finding := range scanRepository(t) {
		if _, ok := Baseline[finding.Rule][finding.Key()]; !ok {
			unreviewed = append(unreviewed, finding.String())
		}
	}
	if len(unreviewed) > 0 {
		sort.Strings(unreviewed)
		t.Fatalf("unreviewed routing boundary sites:\n  %s\n\nEach one either belongs in the semantic catalog/planner path, "+
			"or must be added to Baseline in baseline.go with a reason and a deletion condition.",
			strings.Join(unreviewed, "\n  "))
	}
}

// TestBaselineHasNoStaleEntries keeps the allowlist shrinking. Once a family
// migrates and its legacy site disappears, the permission must disappear with
// it, otherwise a later change could silently reintroduce the same bypass.
func TestBaselineHasNoStaleEntries(t *testing.T) {
	live := make(map[Rule]map[string]bool, len(Baseline))
	for _, finding := range scanRepository(t) {
		if live[finding.Rule] == nil {
			live[finding.Rule] = make(map[string]bool)
		}
		live[finding.Rule][finding.Key()] = true
	}
	var stale []string
	for rule, entries := range Baseline {
		for key, reason := range entries {
			if reason == "" {
				t.Errorf("baseline entry %s %s has no reason", rule, key)
			}
			if !live[rule][key] {
				stale = append(stale, string(rule)+" "+key)
			}
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Fatalf("stale baseline entries (the site is gone, remove the permission):\n  %s", strings.Join(stale, "\n  "))
	}
}

// TestLegacySurfaceDoesNotGrow bounds the two rules that represent unfinished
// migration. The counts are the design C-2 debt: they may fall as families
// migrate, never rise. A rise means a new legacy bypass was added instead of
// a capability.
func TestLegacySurfaceDoesNotGrow(t *testing.T) {
	const (
		maxLegacyNameRouter   = 13
		maxLegacyPolicyFilter = 25
		maxProviderNameLegacy = 10
		maxDefinitionStep     = 4
	)
	// maxProviderNameLegacy read 2 while providerNameCallSelectors only knew
	// RunSkill/CallTool/CallMCPTool. Those three names caught a TUI CLI
	// dispatcher and three transport helpers while every model-facing gateway
	// -- toolRunSkill, toolCallMCPTool, StartRunForOwner, CallToolForOwner --
	// used a different verb and went unseen. Widening the selector list did
	// not add debt; it stopped hiding it. The limit may only fall from here.
	counts := map[Reason]int{}
	for _, entries := range Baseline {
		for _, reason := range entries {
			counts[reason]++
		}
	}
	for _, tc := range []struct {
		reason Reason
		limit  int
	}{
		{ReasonLegacyNameRouter, maxLegacyNameRouter},
		{ReasonLegacyPolicyFilter, maxLegacyPolicyFilter},
		{ReasonProviderNameCallLegacy, maxProviderNameLegacy},
		{ReasonInstalledDefinitionStep, maxDefinitionStep},
	} {
		if counts[tc.reason] > tc.limit {
			t.Errorf("legacy sites with reason %q grew to %d, limit %d; migrate the family instead of widening the limit",
				tc.reason, counts[tc.reason], tc.limit)
		}
	}
}

// TestScannerCoversTheRoutingPackages guards the walk itself. A skip rule or
// path bug that silently excludes gui/ or corelib/ would make every other
// assertion in this package vacuous.
func TestScannerCoversTheRoutingPackages(t *testing.T) {
	seen := map[string]bool{}
	for _, finding := range scanRepository(t) {
		seen[strings.SplitN(finding.File, "/", 2)[0]] = true
	}
	for _, tree := range []string{"gui", "corelib"} {
		if !seen[tree] {
			t.Errorf("scanner produced no findings under %s/, the walk is not covering it", tree)
		}
	}
}

// TestProviderNameCallDetectorSeesModelFacingGateways pins the places a model
// can still name a Skill or an MCP tool.
//
// TestEveryRuleIsExercised is not enough for this rule. It only asks whether
// the rule matched something, and the rule did match: three transport helpers
// and a TUI CLI dispatcher kept it green while every gateway a model actually
// reaches went undetected, because those gateways spell the call with another
// verb. A per-file assertion is the only form that fails when the selector
// list stops covering the surface it exists to watch.
// The set of files to watch is read from the baseline rather than repeated
// here. A hand-kept copy had already drifted: gui/coding_subagent.go carries a
// model-facing gateway in the baseline and was missing from the list, so the
// one guard that notices a gateway falling out of detection was not watching
// the coding path at all. Deriving the set means the guard covers whatever the
// baseline currently admits, and shrinks with it as families migrate.
func TestProviderNameCallDetectorSeesModelFacingGateways(t *testing.T) {
	gateways := map[string][]string{}
	for entry, reason := range Baseline[RuleProviderNameCall] {
		if reason != ReasonProviderNameCallLegacy {
			continue
		}
		file := entry
		if index := strings.LastIndex(entry, ":"); index > 0 {
			file = entry[:index]
		}
		gateways[file] = append(gateways[file], entry)
	}
	if len(gateways) == 0 {
		t.Fatal("no model-facing gateways in the baseline; this guard would assert nothing")
	}
	seen := map[string]bool{}
	for _, finding := range scanRepository(t) {
		if finding.Rule == RuleProviderNameCall {
			seen[finding.File] = true
		}
	}
	for file, entries := range gateways {
		if !seen[file] {
			sort.Strings(entries)
			t.Errorf("provider_name_call no longer detects %s (%s); the selector list stopped covering a model-facing gateway",
				file, strings.Join(entries, ", "))
		}
	}
}

// TestEveryRuleIsExercised keeps a detector from silently matching nothing
// after a refactor renames the symbols it looks for.
func TestEveryRuleIsExercised(t *testing.T) {
	found := map[Rule]int{}
	for _, finding := range scanRepository(t) {
		found[finding.Rule]++
	}
	for _, rule := range AllRules() {
		if found[rule] == 0 {
			t.Errorf("rule %q matched nothing; its detector symbols were probably renamed", rule)
		}
	}
}
