package commands

import "strings"

// InstallCLICommandCatalog describes top-level and nested actions accepted by
// the embedded install CLIs (RunSkill / RunMCP / RunPlugin).
//
// Keep this in lock-step with the switch cases in skill.go, mcp.go, and
// plugin.go. The GUI shared allowlist (installCommandAllowlist.json) MUST be
// a subset of this catalog so chat never routes an action the CLI rejects.
//
// Note: the chat allowlist is intentionally a curated subset (e.g. skill
// backup/export are CLI-only and not exposed in the AI panel).
type InstallCLICommandCatalog struct {
	// Actions are top-level subcommands (and aliases).
	Actions []string
	// Nested maps parent action (and aliases) → allowed sub-actions.
	Nested map[string][]string
}

// Shared nested sub-actions for plugin marketplace|market (one slice, two keys).
var pluginMarketplaceNestedActions = []string{
	"add", "list", "ls",
	"remove", "rm", "delete",
	"refresh", "update",
}

// InstallCLICatalog is the source of truth for "what the CLI accepts".
// When adding a case to RunSkill/RunMCP/RunPlugin, update this catalog too.
var InstallCLICatalog = map[string]InstallCLICommandCatalog{
	"skill": {
		// skill.go RunSkill switch
		Actions: []string{
			"list", "search", "install", "add",
			"delete", "remove", "uninstall", "rm",
			"backup", "restore", "import", "export",
		},
	},
	"mcp": {
		// mcp.go RunMCP switch
		Actions: []string{
			"list", "search", "install", "add",
			"remove", "uninstall", "rm",
			"health-check", "tools", "call-tool",
		},
	},
	"plugin": {
		// plugin.go RunPlugin switch
		Actions: []string{
			"list", "info", "search",
			"add", "install",
			"remove", "uninstall",
			"enable", "disable", "create",
			"marketplace", "market",
			"installed",
			"help", "--help", "-h",
		},
		// plugin.go pluginMarketplaceCmd switch
		Nested: map[string][]string{
			"marketplace": pluginMarketplaceNestedActions,
			"market":      pluginMarketplaceNestedActions,
		},
	},
}

// Precomputed O(1) lookups built once from InstallCLICatalog.
type installCLILookup struct {
	actions map[string]struct{}
	nested  map[string]map[string]struct{} // parent → sub set
}

var installCLILookups = buildInstallCLILookups()

func buildInstallCLILookups() map[string]installCLILookup {
	out := make(map[string]installCLILookup, len(InstallCLICatalog))
	for cmd, spec := range InstallCLICatalog {
		acts := make(map[string]struct{}, len(spec.Actions))
		for _, a := range spec.Actions {
			acts[strings.ToLower(strings.TrimSpace(a))] = struct{}{}
		}
		nested := make(map[string]map[string]struct{}, len(spec.Nested))
		for parent, subs := range spec.Nested {
			p := strings.ToLower(strings.TrimSpace(parent))
			set := make(map[string]struct{}, len(subs))
			for _, s := range subs {
				set[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
			}
			nested[p] = set
		}
		out[cmd] = installCLILookup{actions: acts, nested: nested}
	}
	return out
}

func installCLILookupFor(cmd string) (installCLILookup, bool) {
	lu, ok := installCLILookups[strings.ToLower(strings.TrimSpace(cmd))]
	return lu, ok
}

// InstallCLIHasAction reports whether cmd accepts the given top-level action.
func InstallCLIHasAction(cmd, action string) bool {
	lu, ok := installCLILookupFor(cmd)
	if !ok {
		return false
	}
	_, ok = lu.actions[strings.ToLower(strings.TrimSpace(action))]
	return ok
}

// InstallCLIHasNestedAction reports whether cmd accepts parent+sub nested action.
func InstallCLIHasNestedAction(cmd, parent, sub string) bool {
	lu, ok := installCLILookupFor(cmd)
	if !ok {
		return false
	}
	subs, ok := lu.nested[strings.ToLower(strings.TrimSpace(parent))]
	if !ok {
		return false
	}
	_, ok = subs[strings.ToLower(strings.TrimSpace(sub))]
	return ok
}

// InstallCLIHasNestedParent reports whether parent is a known nested group for cmd.
func InstallCLIHasNestedParent(cmd, parent string) bool {
	lu, ok := installCLILookupFor(cmd)
	if !ok {
		return false
	}
	_, ok = lu.nested[strings.ToLower(strings.TrimSpace(parent))]
	return ok
}

// InstallCLICommandNames returns sorted-ish canonical command names in the catalog.
func InstallCLICommandNames() []string {
	names := make([]string, 0, len(InstallCLICatalog))
	for cmd := range InstallCLICatalog {
		names = append(names, cmd)
	}
	return names
}
