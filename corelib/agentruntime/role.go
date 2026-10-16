package agentruntime

import "strings"

// DefaultRoleName is the shared Agent identity used when a host does not
// supply a custom role. GUI and srv must resolve empty config to this value.
const DefaultRoleName = "MaClaw"

// DefaultRoleDescription is the shared default role text. Headless hosts must
// not substitute a transport-specific identity or prompt digest parity fails.
const DefaultRoleDescription = "你的全能数智伴侣MaClaw"

// ResolveRole returns the configured role, falling back to the shared defaults.
func ResolveRole(name, description string) (string, string) {
	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)
	if name == "" {
		name = DefaultRoleName
	}
	if description == "" {
		description = DefaultRoleDescription
	}
	return name, description
}
