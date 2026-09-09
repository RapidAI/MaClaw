package tool

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

// legacyDynamicGatewayNames are the tools that hand a model an open-ended
// provider selector: name a server and a tool, or name a skill and an action,
// and the host will run it.
//
// They are the thing a governed capability surface exists to replace. On a
// managed turn the planner has already chosen which provider satisfies which
// need, and every grant is bound to that choice; a gateway would let an
// implementation name regain the authority the planner just took away. They
// remain on unmigrated paths because those paths have no replacement yet, so
// the ban is enforced where the closed surface is built rather than by
// deleting the tools outright.
//
// The list lives here because both hosts enforce it and there is no third
// place to look. It was previously written out twice -- a named predicate in
// the GUI and three inline string comparisons in the service -- so adding a
// fourth gateway meant remembering a second, differently-shaped copy that
// searching for the first one would not find.
var legacyDynamicGatewayNames = []string{"call_mcp_tool", "manage_skill", "discover_tool"}

// IsLegacyDynamicGatewayName reports whether a tool name is one of the
// open-ended gateways that must never appear on a managed semantic surface.
func IsLegacyDynamicGatewayName(name string) bool {
	name = strings.TrimSpace(name)
	for _, gateway := range legacyDynamicGatewayNames {
		if name == gateway {
			return true
		}
	}
	return false
}

// LegacyDynamicGatewayNames lists the banned gateways so a host can assert its
// own surface excludes all of them rather than the ones its author recalled.
func LegacyDynamicGatewayNames() []string {
	out := make([]string, len(legacyDynamicGatewayNames))
	copy(out, legacyDynamicGatewayNames)
	return out
}

// ClosedManagedDefinitions is the Phase C safety subset: a managed surface
// may not list open-ended gateways. When grants is non-nil the model may only
// see names that currently hold a grant; an empty grant table yields nothing.
// Headless unmigrated close passes a nil grant table so only gateways drop.
func ClosedManagedDefinitions(defs []map[string]interface{}, grants map[string]InvocationGrant) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	if grants != nil && len(grants) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(tooldef.Name(def))
		if name == "" || IsLegacyDynamicGatewayName(name) {
			continue
		}
		if grants != nil {
			if _, ok := grants[name]; !ok {
				continue
			}
		}
		out = append(out, def)
	}
	return out
}

// GrantSelectionIsLightPromptSafe reports whether name currently holds a grant
// whose planned selection may execute under a light prompt. Unknown names fail
// closed so a host cannot admit a mutating grant by spelling.
func GrantSelectionIsLightPromptSafe(plan ToolPlan, grants map[string]InvocationGrant, name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	grant, ok := grants[name]
	if !ok {
		return false
	}
	selection, found := PlanSelectionByID(plan, grant.SelectionID)
	return found && IsLightPromptSafeSelection(selection)
}

// FilterLightPromptSafeDefinitions keeps definitions whose live grant is
// light-safe. Hosts that pre-filter the model list before RunLoop share this
// so a light turn never enters with tools the authorizer would immediately drop.
func FilterLightPromptSafeDefinitions(defs []map[string]interface{}, plan ToolPlan, grants map[string]InvocationGrant) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		name := strings.TrimSpace(tooldef.Name(def))
		if !GrantSelectionIsLightPromptSafe(plan, grants, name) {
			continue
		}
		out = append(out, def)
	}
	return out
}

// ClosedManagedDefinitionsForProfile is the grant close used at loop start,
// then the light-safe selection filter when light is set. Full profile keeps
// every currently granted non-gateway name.
func ClosedManagedDefinitionsForProfile(defs []map[string]interface{}, plan ToolPlan, grants map[string]InvocationGrant, light bool) []map[string]interface{} {
	closed := ClosedManagedDefinitions(defs, grants)
	if !light {
		return closed
	}
	return FilterLightPromptSafeDefinitions(closed, plan, grants)
}
