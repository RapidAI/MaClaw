package guiapp

// Expert-whitelist → permission.Snapshot rule translation (Phase 1 of
// docs/design/tool-routing-improvement-plan-zh.md). The expert allow-list
// gate (expert_session_policy.go) stays the enforcement point; this file only
// mirrors expert definitions into subject-scoped rules so the dual-eval
// harness in the gate can compare legacy outcomes against the snapshot.
//
// Rule modeling:
//
//   - Expert with a non-empty whitelist W: one allow rule per tool in W,
//     each scoped {Tool: t, Effect: allow, Subject: expertID}, provenance
//     Source "expert-definitions".
//   - Expert with an empty/missing whitelist: NO rules. The legacy gate
//     treats an empty whitelist as "all tools" (see ExpertDefinition.Tools
//     and filterToolsForExpert), so the snapshot must contribute nothing and
//     DecideFor falls through to Decision.Default / global rules.
//
// Store/builtin shadowing: loadExpertDefByID (expert_session_policy.go)
// resolves store-first — a store copy of a builtin id fully replaces the
// builtin def. The union here MUST mirror that: for each id, emit rules for
// the store def if present, else the builtin def. Emitting rules for both
// copies (e.g. when a store copy narrows the builtin's whitelist) would
// create phantom allow rules that the legacy gate never grants, producing
// permanent divergence noise and wrong flip data.
//
// NOTE (skill-level whitelist is NOT representable): the legacy gate rejects
// manage_skill{action:"run",name:X} when X is outside def.Skills
// (expertToolCallRejectionWithDef), but the current rule model can only
// express a tool-level subject allow for manage_skill — there is no rule
// kind or ArgsPredicate path for the nested {action,name} pair. The flip
// slice MUST add skill-predicate support (ArgsPredicate on a nested field,
// or a skill-specific rule kind) BEFORE flipping this gate; until then the
// flip is BLOCKED for experts with a non-empty Skills list.
//
// There is deliberately NO subject-scoped deny catch-all ({Tool:"*",
// Effect:deny, Subject: expertID}): under the engine's deny > ask > allow
// priority, effect rank dominates tool specificity, so the expert's own "*"
// deny would also outrank their whitelisted allows and deny every tool. The
// legacy "non-whitelisted tool → reject" outcome is therefore not
// representable in the snapshot; DecideFor reports Default for it, and
// dual-eval treats Default as "no opinion" (nothing is logged). Global/user
// deny rules still beat the expert's allows, as intended.

import (
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/permission"
)

// expertPermissionRuleSource is the provenance label for translated rules.
const expertPermissionRuleSource = "expert-definitions"

// expertPermissionListFunc is the loader seam: it lists every expert
// definition (user store plus in-binary builtins) for rule translation.
// Tests stub it to avoid touching the real ~/.maclaw/experts/experts.json.
var expertPermissionListFunc = func() ([]ExpertDefinition, error) {
	defs, _, err := defaultExpertStore.List()
	if err != nil {
		return nil, err
	}
	// Union mirrors loadExpertDefByID: the store def shadows a builtin of
	// the same id, so rules are emitted for the store copy only — never for
	// the shadowed builtin (see file header).
	return expertDefinitionsUnion(defs, builtinExperts()), nil
}

// expertDefinitionsUnion merges store defs over builtin defs with
// store-first shadowing per id, matching loadExpertDefByID: for each
// expert id the store def wins if present, otherwise the builtin def is
// used. Ids without a trimmed id are dropped.
func expertDefinitionsUnion(storeDefs, builtinDefs []ExpertDefinition) []ExpertDefinition {
	out := make([]ExpertDefinition, 0, len(storeDefs)+len(builtinDefs))
	seen := make(map[string]bool, len(storeDefs)+len(builtinDefs))
	appendFresh := func(defs []ExpertDefinition) {
		for _, def := range defs {
			id := strings.TrimSpace(def.ID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, def)
		}
	}
	appendFresh(storeDefs)
	appendFresh(builtinDefs)
	return out
}

// loadExpertPermissionRules translates the current expert definitions into
// snapshot rules. A store read failure is returned so the caller can log and
// proceed without expert rules — dual-eval degradation must never break the
// snapshot.
func loadExpertPermissionRules() ([]permission.Rule, error) {
	defs, err := expertPermissionListFunc()
	if err != nil {
		return nil, err
	}
	return expertDefinitionsToPermissionRules(defs), nil
}

// expertDefinitionsToPermissionRules is the pure translation; see the file
// header for the modeling rationale.
func expertDefinitionsToPermissionRules(defs []ExpertDefinition) []permission.Rule {
	var out []permission.Rule
	for _, def := range defs {
		id := strings.TrimSpace(def.ID)
		if id == "" {
			continue
		}
		// Mirror loadExpertDefByID: an inactive managed-industry expert
		// resolves to nil (unrestricted legacy gate), so it contributes no
		// rules either.
		if isManagedIndustryExpert(id) && !isActiveManagedIndustryExpert(id) {
			continue
		}
		if len(def.Tools) == 0 {
			continue
		}
		seen := make(map[string]bool, len(def.Tools))
		for _, name := range def.Tools {
			n := strings.TrimSpace(name)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, permission.Rule{
				Tool:    n,
				Effect:  permission.EffectAllow,
				Subject: id,
				Source:  expertPermissionRuleSource,
				Reason:  fmt.Sprintf("expert %q tool whitelist", id),
			})
		}
	}
	return out
}
