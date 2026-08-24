package tool

import "strings"

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
