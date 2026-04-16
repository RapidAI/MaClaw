package mcp

import (
	"fmt"
	"sort"
	"strings"
)

// FormatToolParameters formats an InputSchema into a human-readable string
// for display in list_mcp_tools output.
// Returns "(no parameters)" for nil/empty schemas.
func FormatToolParameters(schema map[string]interface{}) string {
	if len(schema) == 0 {
		return "(no parameters)"
	}

	properties, _ := schema["properties"].(map[string]interface{})
	if len(properties) == 0 {
		return "(no parameters)"
	}

	// Build required set for quick lookup.
	requiredSet := make(map[string]bool)
	if reqRaw, ok := schema["required"]; ok {
		if reqSlice, ok := reqRaw.([]interface{}); ok {
			for _, r := range reqSlice {
				if name, ok := r.(string); ok {
					requiredSet[name] = true
				}
			}
		}
	}

	// Sort parameter names for deterministic output.
	paramNames := make([]string, 0, len(properties))
	for name := range properties {
		paramNames = append(paramNames, name)
	}
	sort.Strings(paramNames)

	var sb strings.Builder
	sb.WriteString("  Parameters:\n")

	for _, name := range paramNames {
		propRaw := properties[name]
		propDef, ok := propRaw.(map[string]interface{})
		if !ok {
			// Non-object property definition; show name only.
			sb.WriteString(fmt.Sprintf("    - %s\n", name))
			continue
		}

		typeName, _ := propDef["type"].(string)
		if typeName == "" {
			typeName = "any"
		}

		reqMarker := "optional"
		if requiredSet[name] {
			reqMarker = "required"
		}

		desc, _ := propDef["description"].(string)

		// Build the line: "    - name (type, required/optional): description [enum: ...] [nested object]"
		var line strings.Builder
		line.WriteString(fmt.Sprintf("    - %s (%s, %s)", name, typeName, reqMarker))

		if desc != "" {
			line.WriteString(": ")
			line.WriteString(desc)
		}

		// Append enum values if present.
		if enumRaw, ok := propDef["enum"]; ok {
			if enumSlice, ok := enumRaw.([]interface{}); ok && len(enumSlice) > 0 {
				line.WriteString(" ")
				line.WriteString(formatEnumForDisplay(enumSlice))
			}
		}

		// Mark nested object parameters.
		if typeName == "object" {
			line.WriteString(" [nested object]")
		}

		line.WriteString("\n")
		sb.WriteString(line.String())
	}

	return sb.String()
}

// FormatToolList formats a list of ToolEntry items with their parameters,
// grouped by server. Each server group includes a header with server metadata
// (server name, ID, source type, health status).
//
// The query and serverID parameters are included for display context only;
// actual filtering should be done via FilterTools before calling this function.
func FormatToolList(entries []ToolEntry, query string, serverID string) string {
	if len(entries) == 0 {
		return ""
	}

	// Group entries by server key (ServerName + ServerID + SourceType + HealthStatus).
	type serverKey struct {
		Name         string
		ID           string
		SourceType   string
		HealthStatus string
	}

	type serverGroup struct {
		key     serverKey
		entries []ToolEntry
	}

	// Use a slice to preserve insertion order.
	var groups []serverGroup
	groupIndex := make(map[serverKey]int)

	for _, entry := range entries {
		sk := serverKey{
			Name:         entry.ServerName,
			ID:           entry.ServerID,
			SourceType:   entry.SourceType,
			HealthStatus: entry.HealthStatus,
		}
		if idx, ok := groupIndex[sk]; ok {
			groups[idx].entries = append(groups[idx].entries, entry)
		} else {
			groupIndex[sk] = len(groups)
			groups = append(groups, serverGroup{
				key:     sk,
				entries: []ToolEntry{entry},
			})
		}
	}

	var sb strings.Builder

	for i, group := range groups {
		if i > 0 {
			sb.WriteString("\n")
		}

		// Server header: ## ServerName (ServerID) [SourceType] status=HealthStatus
		sb.WriteString(fmt.Sprintf("## %s (%s) [%s] status=%s\n",
			group.key.Name,
			group.key.ID,
			group.key.SourceType,
			group.key.HealthStatus,
		))

		for _, entry := range group.entries {
			sb.WriteString(fmt.Sprintf("\n- **%s**: %s\n", entry.ToolName, entry.Description))
			sb.WriteString(FormatToolParameters(entry.InputSchema))
		}
	}

	return sb.String()
}

// formatEnumForDisplay formats an enum slice as "[enum: val1, val2, ...]".
func formatEnumForDisplay(enumSlice []interface{}) string {
	var vals []string
	for _, e := range enumSlice {
		if s, ok := e.(string); ok {
			vals = append(vals, s)
		} else {
			vals = append(vals, fmt.Sprintf("%v", e))
		}
	}
	if len(vals) == 0 {
		return ""
	}
	return "[enum: " + strings.Join(vals, ", ") + "]"
}
