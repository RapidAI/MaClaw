package mcp

import "strings"

// FilterTools filters a slice of ToolEntry by query (case-insensitive substring
// match on tool name, description, or server name) and/or serverID (exact match).
// Returns all entries if both query and serverID are empty.
// Returns an empty slice (not nil) when no entries match.
func FilterTools(entries []ToolEntry, query string, serverID string) []ToolEntry {
	if query == "" && serverID == "" {
		return entries
	}

	lowerQuery := strings.ToLower(query)
	result := make([]ToolEntry, 0)

	for _, entry := range entries {
		if serverID != "" && entry.ServerID != serverID {
			continue
		}

		if lowerQuery != "" {
			if !strings.Contains(strings.ToLower(entry.ToolName), lowerQuery) &&
				!strings.Contains(strings.ToLower(entry.Description), lowerQuery) &&
				!strings.Contains(strings.ToLower(entry.ServerName), lowerQuery) {
				continue
			}
		}

		result = append(result, entry)
	}

	return result
}
