package guiapp

import (
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// swapExpertPermissionListFunc stubs the expert-listing seam for the
// duration of a test.
func swapExpertPermissionListFunc(t *testing.T, fn func() ([]ExpertDefinition, error)) {
	t.Helper()
	prev := expertPermissionListFunc
	expertPermissionListFunc = fn
	t.Cleanup(func() {
		expertPermissionListFunc = prev
	})
}

func TestExpertDefinitionsToPermissionRules(t *testing.T) {
	defs := []ExpertDefinition{
		{ID: "expert-a", Tools: []string{"bash", " read_file ", "bash", ""}},
		{ID: "expert-open"}, // empty whitelist → unrestricted, no rules
		{ID: "  "},          // no id → skipped
		{ID: "expert-nil-tools", Tools: []string{""}}, // only blanks → no effective whitelist
	}
	rules := expertDefinitionsToPermissionRules(defs)
	if len(rules) != 2 {
		t.Fatalf("rules = %d, want 2 (deduped whitelist for expert-a only): %+v", len(rules), rules)
	}
	for _, r := range rules {
		if r.Source != expertPermissionRuleSource {
			t.Fatalf("Source = %q, want %q", r.Source, expertPermissionRuleSource)
		}
		if r.Subject != "expert-a" || r.Effect != permission.EffectAllow {
			t.Fatalf("unexpected rule: %+v", r)
		}
	}
	if rules[0].Tool != "bash" || rules[1].Tool != "read_file" {
		t.Fatalf("tools = %q,%q, want bash,read_file", rules[0].Tool, rules[1].Tool)
	}
}

func TestExpertDefinitionsUnionStoreShadowsBuiltin(t *testing.T) {
	// A store copy of a builtin id must FULLY replace the builtin def
	// (mirrors loadExpertDefByID store-first resolution). A narrower store
	// whitelist must not leave phantom allow rules for the builtin's wider
	// tool list.
	store := []ExpertDefinition{
		{ID: "builtin-a", Tools: []string{"bash"}},
		{ID: "store-only", Tools: []string{"read_file"}},
	}
	builtins := []ExpertDefinition{
		{ID: "builtin-a", Tools: []string{"bash", "web_search", "write_file"}},
		{ID: "builtin-only", Tools: []string{"bash"}},
		{ID: "  "}, // no id → dropped
	}
	union := expertDefinitionsUnion(store, builtins)
	if len(union) != 3 {
		t.Fatalf("union = %d defs, want 3 (store-only, builtin-a, builtin-only): %+v", len(union), union)
	}
	if union[0].ID != "builtin-a" || len(union[0].Tools) != 1 || union[0].Tools[0] != "bash" {
		t.Fatalf("store def must win for builtin-a, got %+v", union[0])
	}

	// Translated rules contain ONLY the store whitelist for the shadowed id.
	rules := expertDefinitionsToPermissionRules(union)
	var tools []string
	for _, r := range rules {
		if r.Subject == "builtin-a" {
			tools = append(tools, r.Tool)
		}
	}
	if len(tools) != 1 || tools[0] != "bash" {
		t.Fatalf("builtin-a rules = %v, want [bash] only (store whitelist; no phantom builtin allows)", tools)
	}
}

// TestExpertDefinitionsUnionStoreEmptyToolsShadowsBuiltin pins the full
// shadow: a store copy with an EMPTY whitelist replaces the builtin def, so
// the union treats the expert as unrestricted and emits no rules — matching
// the legacy gate, which sees the store def (empty Tools = all tools).
func TestExpertDefinitionsUnionStoreEmptyToolsShadowsBuiltin(t *testing.T) {
	store := []ExpertDefinition{{ID: "builtin-a"}}
	builtins := []ExpertDefinition{{ID: "builtin-a", Tools: []string{"bash"}}}
	rules := expertDefinitionsToPermissionRules(expertDefinitionsUnion(store, builtins))
	if len(rules) != 0 {
		t.Fatalf("rules = %+v, want none (store copy with empty whitelist shadows builtin)", rules)
	}
}

func TestAppPermissionSnapshotIncludesExpertRules(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	swapExpertPermissionListFunc(t, func() ([]ExpertDefinition, error) {
		return []ExpertDefinition{
			{ID: "expert-a", Tools: []string{"bash"}},
			{ID: "expert-open"},
		}, nil
	})
	app := &App{testHomeDir: t.TempDir()}
	snap := app.permissionSnapshot()
	if snap == nil {
		t.Fatal("permissionSnapshot() = nil, want non-nil")
	}

	// Whitelisted tool → subject-scoped allow from expert-definitions.
	d := snap.DecideFor("expert-a", "bash", permission.KindExecute, nil)
	if d.Default || d.Effect != permission.EffectAllow {
		t.Fatalf("whitelisted bash: %+v, want allow", d)
	}
	if d.Rule.Source != expertPermissionRuleSource || d.Rule.Subject != "expert-a" {
		t.Fatalf("rule provenance/subject: %+v", d.Rule)
	}
	// Non-whitelisted tool → Default: the legacy reject is not representable
	// (a subject deny catch-all would outrank the expert's own allows under
	// deny>ask>allow), so the snapshot reports no opinion.
	if d := snap.DecideFor("expert-a", "web_search", permission.KindRead, nil); !d.Default {
		t.Fatalf("non-whitelisted tool should default, got %+v", d)
	}
	// Expert without a whitelist → Default (unrestricted legacy gate).
	if d := snap.DecideFor("expert-open", "bash", permission.KindExecute, nil); !d.Default {
		t.Fatalf("whitelist-less expert should default, got %+v", d)
	}
}

func TestAppPermissionSnapshotGlobalDenyBeatsExpertAllow(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{
			ManagedRules: []permission.Rule{{Tool: "bash", Effect: permission.EffectDeny}},
		})
	})
	swapExpertPermissionListFunc(t, func() ([]ExpertDefinition, error) {
		return []ExpertDefinition{{ID: "expert-a", Tools: []string{"bash"}}}, nil
	})
	app := &App{testHomeDir: t.TempDir()}
	snap := app.permissionSnapshot()
	if d := snap.DecideFor("expert-a", "bash", permission.KindExecute, nil); d.Default || d.Effect != permission.EffectDeny {
		t.Fatalf("global deny must beat expert allow: %+v", d)
	}
}

func TestAppPermissionSnapshotExpertRulesSurviveYoloPin(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	doc := struct {
		Permission struct {
			Rules []permission.Rule `json:"rules"`
		} `json:"permission"`
	}{}
	doc.Permission.Rules = []permission.Rule{{Tool: "*", Effect: permission.EffectAllow}}
	if err := writeJSONFile(cfgPath, doc); err != nil {
		t.Fatalf("write config: %v", err)
	}
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{UserConfigPath: cfgPath, ManagedYoloPin: true})
	})
	swapExpertPermissionListFunc(t, func() ([]ExpertDefinition, error) {
		return []ExpertDefinition{{ID: "expert-a", Tools: []string{"bash"}}}, nil
	})
	app := &App{testHomeDir: t.TempDir()}
	snap := app.permissionSnapshot()
	// The pin stripped the global catch-all allow...
	if d := snap.Decide("web_search", permission.KindRead, nil); !d.Default {
		t.Fatalf("global catch-all allow must be pinned: %+v", d)
	}
	// ...but the appended subject-scoped expert allow survives.
	if d := snap.DecideFor("expert-a", "bash", permission.KindExecute, nil); d.Default || d.Effect != permission.EffectAllow {
		t.Fatalf("expert allow must survive the pin: %+v", d)
	}
}

func TestAppPermissionSnapshotExpertLoadFailureDegrades(t *testing.T) {
	swapPermissionLoadFunc(t, func(permission.Options) (*permission.Snapshot, error) {
		return permission.Load(permission.Options{})
	})
	swapExpertPermissionListFunc(t, func() ([]ExpertDefinition, error) {
		return nil, errors.New("experts store unreadable")
	})
	app := &App{testHomeDir: t.TempDir()}
	snap := app.permissionSnapshot()
	if snap == nil {
		t.Fatal("expert load failure must not break the snapshot")
	}
	if d := snap.DecideFor("expert-a", "bash", permission.KindExecute, nil); !d.Default {
		t.Fatalf("without expert rules every decision should default, got %+v", d)
	}
}

// captureExpertGateDualEvalHook swaps the mismatch hook and returns a
// collector plus a restore func.
func captureExpertGateDualEvalHook(t *testing.T) *[][]interface{} {
	t.Helper()
	var captured [][]interface{}
	prev := expertGateDualEvalMismatchHook
	expertGateDualEvalMismatchHook = func(expertID, tool string, legacyEffect, newEffect permission.Effect, rule *permission.Rule) {
		captured = append(captured, []interface{}{expertID, tool, legacyEffect, newEffect, rule})
	}
	t.Cleanup(func() { expertGateDualEvalMismatchHook = prev })
	return &captured
}

func TestExpertGateDualEvalLogsGlobalDenyDivergence(t *testing.T) {
	swapExpertStoreForTest(t)
	if err := defaultExpertStore.Save(ExpertDefinition{ID: "expert-a", Tools: []string{"bash"}}); err != nil {
		t.Fatalf("save expert: %v", err)
	}
	invalidateExpertDefCache("expert-a")

	base, err := permission.Load(permission.Options{
		ManagedRules: []permission.Rule{{Tool: "bash", Effect: permission.EffectDeny, Reason: "global no-shell"}},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	snap := base.WithAdditionalRules(expertDefinitionsToPermissionRules([]ExpertDefinition{{ID: "expert-a", Tools: []string{"bash"}}}))

	captured := captureExpertGateDualEvalHook(t)
	userID := expertSessionUserID("expert-a")

	// Legacy passes bash (whitelisted) but the snapshot denies it globally:
	// divergence on a concrete rule must be logged.
	if got := expertToolExecutionRejectionWithSnapshot(userID, "bash", "{}", snap); got != "" {
		t.Fatalf("legacy outcome must be unchanged, got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("mismatches = %d, want 1", len(*captured))
	}
	m := (*captured)[0]
	if m[0] != "expert-a" || m[1] != "bash" || m[2] != permission.EffectAllow || m[3] != permission.EffectDeny {
		t.Fatalf("mismatch fields: %v", m)
	}
	if rule, ok := m[4].(*permission.Rule); !ok || rule.Source == expertPermissionRuleSource {
		t.Fatalf("divergence should come from the global deny rule, got %+v", m[4])
	}

	// Agreement: whitelisted tool with no conflicting rule → no log.
	snapAllow, err := permission.Load(permission.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	snapAllow = snapAllow.WithAdditionalRules(expertDefinitionsToPermissionRules([]ExpertDefinition{{ID: "expert-a", Tools: []string{"bash"}}}))
	if got := expertToolExecutionRejectionWithSnapshot(userID, "bash", "{}", snapAllow); got != "" {
		t.Fatalf("legacy outcome must be unchanged, got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("agreeing decisions must not log, got %d entries", len(*captured))
	}

	// Non-expert session: def nil → no dual-eval, no log.
	if got := expertToolExecutionRejectionWithSnapshot(desktopUserID, "bash", "{}", snap); got != "" {
		t.Fatalf("desktop session should not be gated, got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("non-expert session must not log, got %d entries", len(*captured))
	}

	// Nil snapshot (TUI standalone / kill switch): silent skip.
	if got := expertToolExecutionRejection(userID, "bash", "{}"); got != "" {
		t.Fatalf("nil snapshot must not change the outcome, got %q", got)
	}
	if len(*captured) != 1 {
		t.Fatalf("nil snapshot must not log, got %d entries", len(*captured))
	}
}

// TestExpertGateDualEvalLogShape pins the uniform log shape: base fields in
// the same positions/order as corelib/agent's hook, with the expert id
// appended at the END of the line.
func TestExpertGateDualEvalLogShape(t *testing.T) {
	swapExpertStoreForTest(t)
	if err := defaultExpertStore.Save(ExpertDefinition{ID: "expert-a", Tools: []string{"bash"}}); err != nil {
		t.Fatalf("save expert: %v", err)
	}
	invalidateExpertDefCache("expert-a")

	base, err := permission.Load(permission.Options{
		ManagedRules: []permission.Rule{{Tool: "bash", Effect: permission.EffectDeny, Reason: "global no-shell"}},
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	// Capture the real default hook's log line (no hook swap).
	var buf syncBuffer
	prevOut := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	userID := expertSessionUserID("expert-a")
	if got := expertToolExecutionRejectionWithSnapshot(userID, "bash", "{}", base); got != "" {
		t.Fatalf("legacy outcome must be unchanged, got %q", got)
	}
	line := buf.String()
	wantPrefix := `[permission-dual-eval] gate=expert_whitelist tool="bash" legacy=allow new=deny rule_source=`
	if !strings.Contains(line, wantPrefix) {
		t.Fatalf("log line = %q, want prefix %q", line, wantPrefix)
	}
	if !strings.HasSuffix(line, `expert="expert-a"`+"\n") {
		t.Fatalf("log line = %q, want expert id as the final field", line)
	}
	reasonIdx := strings.Index(line, "reason=")
	expertIdx := strings.Index(line, "expert=")
	if reasonIdx < 0 || expertIdx < 0 || reasonIdx >= expertIdx {
		t.Fatalf("expert must come after reason in %q", line)
	}
}

func TestExpertGateMethodSkipsDualEvalWhenNoApp(t *testing.T) {
	swapExpertStoreForTest(t)
	if err := defaultExpertStore.Save(ExpertDefinition{ID: "expert-a", Tools: []string{"bash"}}); err != nil {
		t.Fatalf("save expert: %v", err)
	}
	invalidateExpertDefCache("expert-a")
	captured := captureExpertGateDualEvalHook(t)

	h := &IMMessageHandler{} // no app → TUI standalone shape
	if got := h.expertToolExecutionRejection(expertSessionUserID("expert-a"), "bash", "{}"); got != "" {
		t.Fatalf("legacy outcome must be unchanged, got %q", got)
	}
	if len(*captured) != 0 {
		t.Fatalf("nil-app handler must skip dual-eval, got %d entries", len(*captured))
	}
}
