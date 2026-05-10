package main

import "strings"

func localSessionIDFromToolArgs(args map[string]interface{}) string {
	if args == nil {
		return "local"
	}
	for _, key := range []string{"session_id", "browser_session_id"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "local"
}
