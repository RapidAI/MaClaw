package plugin

import (
	"os"
	"regexp"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ResolveEnvVars recursively traverses a settings map and replaces
// ${VAR_NAME} references in string values with os.Getenv("VAR_NAME").
// If the environment variable is not set, the original ${VAR_NAME} text
// is kept unchanged. The input map is not mutated; a new map is returned.
func ResolveEnvVars(settings map[string]interface{}) map[string]interface{} {
	if settings == nil {
		return nil
	}
	out := make(map[string]interface{}, len(settings))
	for k, v := range settings {
		out[k] = resolveValue(v)
	}
	return out
}

func resolveValue(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return resolveString(val)
	case map[string]interface{}:
		return ResolveEnvVars(val)
	case []interface{}:
		return resolveSlice(val)
	default:
		return v
	}
}

func resolveString(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := envVarPattern.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		if val, ok := os.LookupEnv(sub[1]); ok {
			return val
		}
		return match
	})
}

func resolveSlice(slice []interface{}) []interface{} {
	out := make([]interface{}, len(slice))
	for i, v := range slice {
		out[i] = resolveValue(v)
	}
	return out
}
