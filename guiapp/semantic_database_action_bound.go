package guiapp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var managedDatabaseReadActions = []string{
	"list_connections", "connect", "disconnect", "inspect", "query", "read_table",
}

var managedDatabaseWriteActions = []string{
	"execute", "batch_execute", "write_table", "export_excel",
}

func managedDatabaseAdapterActions(adapterName string) (map[string]struct{}, bool) {
	switch adapterName {
	case "database_query":
		return databaseActionSet(managedDatabaseReadActions), true
	case "database":
		return databaseActionSet(append(append([]string{}, managedDatabaseReadActions...), managedDatabaseWriteActions...)), true
	default:
		return nil, false
	}
}

func databaseActionSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func databaseManagedInvocationRefusal(selection tool.PlannedSelection, canonicalArgs tool.CanonicalRequest) (string, bool) {
	allowed, bounded := managedDatabaseAdapterActions(selection.AdapterName)
	if !bounded {
		return "", false
	}
	var args map[string]interface{}
	if err := json.Unmarshal(canonicalArgs.CanonicalJSON, &args); err != nil {
		return "database_action_unreadable", true
	}
	action, _ := args["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if _, ok := allowed[action]; ok {
		return "", false
	}
	if selection.AdapterName == "database_query" {
		return "database_action_outside_read_surface", true
	}
	return "database_action_outside_managed_surface", true
}

func semanticManagedDatabaseRefusalText(adapterName, reason string) string {
	allowed, _ := managedDatabaseAdapterActions(adapterName)
	names := make([]string, 0, len(allowed))
	for action := range allowed {
		names = append(names, action)
	}
	sort.Strings(names)
	switch reason {
	case "database_action_outside_read_surface":
		return "[system rejected] " + reason +
			": this tool is the read-only database surface and covers only these actions: " +
			strings.Join(names, ", ") +
			". Writes, batch execute and Excel export need the database tool on this turn."
	case "database_action_outside_managed_surface":
		return "[system rejected] " + reason +
			": this turn's database capability covers only these actions: " +
			strings.Join(names, ", ") + "."
	default:
		return "[system rejected] " + reason
	}
}
