package main

import "strings"

type remoteToolNameKind string

const (
	remoteToolNameUnknown   remoteToolNameKind = ""
	remoteToolNameClaude    remoteToolNameKind = "claude"
	remoteToolNameCodex     remoteToolNameKind = "codex"
	remoteToolNameOpencode  remoteToolNameKind = "opencode"
	remoteToolNameCodeBuddy remoteToolNameKind = "codebuddy"
	remoteToolNameIFlow     remoteToolNameKind = "iflow"
	remoteToolNameKilo      remoteToolNameKind = "kilo"
	remoteToolNameKiloCode  remoteToolNameKind = "kilocode"
	remoteToolNameBrowser   remoteToolNameKind = "browser"
)

func normalizeRemoteToolNameKind(toolName string) remoteToolNameKind {
	switch remoteToolNameKind(strings.ToLower(strings.TrimSpace(toolName))) {
	case remoteToolNameUnknown:
		return remoteToolNameClaude
	case remoteToolNameClaude:
		return remoteToolNameClaude
	case remoteToolNameCodex:
		return remoteToolNameCodex
	case remoteToolNameOpencode:
		return remoteToolNameOpencode
	case remoteToolNameCodeBuddy:
		return remoteToolNameCodeBuddy
	case remoteToolNameIFlow:
		return remoteToolNameIFlow
	case remoteToolNameKilo:
		return remoteToolNameKilo
	case remoteToolNameKiloCode:
		return remoteToolNameKiloCode
	case remoteToolNameBrowser:
		return remoteToolNameBrowser
	default:
		return remoteToolNameKind(strings.ToLower(strings.TrimSpace(toolName)))
	}
}

func (kind remoteToolNameKind) String() string {
	return string(kind)
}

func (kind remoteToolNameKind) IsClaude() bool {
	return kind == remoteToolNameClaude
}

func (kind remoteToolNameKind) IsDesktopRemoteLaunchCapableBuiltin() bool {
	switch kind {
	case remoteToolNameClaude, remoteToolNameCodex, remoteToolNameOpencode, remoteToolNameIFlow, remoteToolNameKilo:
		return true
	default:
		return false
	}
}

func (kind remoteToolNameKind) RequiresZipSkills() bool {
	return kind == remoteToolNameCodex
}

func (kind remoteToolNameKind) ConfigDirName() string {
	switch kind {
	case remoteToolNameClaude:
		return ".claude"
	case remoteToolNameCodex:
		return ".codex"
	case remoteToolNameOpencode:
		return ".opencode"
	case remoteToolNameCodeBuddy:
		return ".codebuddy"
	case remoteToolNameIFlow:
		return ".iflow"
	case remoteToolNameKilo, remoteToolNameKiloCode:
		return ".kilocode"
	default:
		return "." + kind.String()
	}
}
