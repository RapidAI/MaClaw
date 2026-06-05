package remote

import (
	"strings"
	"unicode"
)

// RemoteToolInfo describes a remote tool's static metadata (no closures).
type RemoteToolInfo struct {
	Name                  string
	DisplayName           string
	BinaryName            string
	DefaultTitle          string
	UsesOpenAICompat      bool
	RequiresSessionConfig bool
	SupportsProxy         bool
	SupportsRemote        bool
	ReadinessHint         string
	SmokeHint             string
}

// BuiltinToolInfos is the static metadata for all known remote tools.
var BuiltinToolInfos = map[string]RemoteToolInfo{
	"claude": {
		Name: "claude", DisplayName: "Claude", BinaryName: "claude",
		DefaultTitle: "Claude Session", SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks Anthropic-compatible auth, Claude launch command, and SDK stream-json readiness.",
		SmokeHint:     "Runs registration, launch, real session start, and Hub visibility verification for Claude (SDK mode).",
	},
	"codex": {
		Name: "codex", DisplayName: "Codex", BinaryName: "codex",
		DefaultTitle: "Codex Session", UsesOpenAICompat: true, SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks OpenAI-compatible auth, Codex command resolution, and exec --json SDK readiness.",
		SmokeHint:     "Runs registration, launch, real session start, and Hub visibility verification for Codex (SDK mode).",
	},
	"opencode": {
		Name: "opencode", DisplayName: "OpenCode", BinaryName: "opencode",
		DefaultTitle: "OpenCode Session", UsesOpenAICompat: true, RequiresSessionConfig: true,
		SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks OpenCode config sync, OpenAI-compatible endpoints, and isolated session config.",
		SmokeHint:     "Runs registration, PTY, launch, real session start, and Hub visibility verification for OpenCode.",
	},
	"iflow": {
		Name: "iflow", DisplayName: "iFlow", BinaryName: "iflow",
		DefaultTitle: "iFlow Session", UsesOpenAICompat: true, RequiresSessionConfig: true,
		SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks iFlow config sync plus IFLOW and OpenAI-compatible environment wiring.",
		SmokeHint:     "Runs registration, PTY, launch, real session start, and Hub visibility verification for iFlow.",
	},
	"kilo": {
		Name: "kilo", DisplayName: "Kilo", BinaryName: "kilo",
		DefaultTitle: "Kilo Session", UsesOpenAICompat: true, RequiresSessionConfig: true,
		SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks Kilo config sync plus KILO and OpenAI-compatible environment wiring.",
		SmokeHint:     "Runs registration, PTY, launch, real session start, and Hub visibility verification for Kilo.",
	},
	"codebuddy": {
		Name: "codebuddy", DisplayName: "CodeBuddy", BinaryName: "codebuddy",
		DefaultTitle: "CodeBuddy Session", UsesOpenAICompat: true, RequiresSessionConfig: true,
		SupportsProxy: true, SupportsRemote: true,
		ReadinessHint: "Checks CodeBuddy CLI installation, SDK stream-json readiness, and remote capability.",
		SmokeHint:     "Runs registration, launch, real session start, and Hub visibility verification for CodeBuddy (SDK mode).",
	},
}

// NormalizeRemoteToolName normalizes a tool name to lowercase.
func NormalizeRemoteToolName(toolName string) string {
	tool := strings.ToLower(strings.TrimSpace(toolName))
	if tool == "" {
		return "claude"
	}
	return tool
}

// LookupRemoteToolInfo returns the static metadata for a tool.
func LookupRemoteToolInfo(toolName string) (RemoteToolInfo, bool) {
	tool := NormalizeRemoteToolName(toolName)
	meta, ok := BuiltinToolInfos[tool]
	return meta, ok
}

// RemoteToolDisplayName returns the display name for a tool.
func RemoteToolDisplayName(toolName string) string {
	meta, ok := LookupRemoteToolInfo(toolName)
	if ok {
		return meta.DisplayName
	}
	name := NormalizeRemoteToolName(toolName)
	if len(name) == 0 {
		return name
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// RemoteToolSupported returns true if the tool supports remote mode.
func RemoteToolSupported(toolName string) bool {
	meta, ok := LookupRemoteToolInfo(toolName)
	if !ok {
		return false
	}
	return meta.SupportsRemote
}

// ToolOrder is the canonical display order for remote tools.
var ToolOrder = []string{"claude", "codex", "opencode", "codebuddy", "iflow", "kilo"}
