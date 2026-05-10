package main

import "strings"

type remoteToolNameKind string

const (
	remoteToolNameUnknown   remoteToolNameKind = ""
	remoteToolNameClaude    remoteToolNameKind = "claude"
	remoteToolNameGemini    remoteToolNameKind = "gemini"
	remoteToolNameCodex     remoteToolNameKind = "codex"
	remoteToolNameOpencode  remoteToolNameKind = "opencode"
	remoteToolNameCodeBuddy remoteToolNameKind = "codebuddy"
	remoteToolNameIFlow     remoteToolNameKind = "iflow"
	remoteToolNameKilo      remoteToolNameKind = "kilo"
	remoteToolNameKiloCode  remoteToolNameKind = "kilocode"
	remoteToolNameCursor    remoteToolNameKind = "cursor"
	remoteToolNameBrowser   remoteToolNameKind = "browser"
)

func normalizeRemoteToolNameKind(toolName string) remoteToolNameKind {
	switch remoteToolNameKind(strings.ToLower(strings.TrimSpace(toolName))) {
	case remoteToolNameUnknown:
		return remoteToolNameClaude
	case remoteToolNameClaude:
		return remoteToolNameClaude
	case remoteToolNameGemini:
		return remoteToolNameGemini
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
	case remoteToolNameCursor:
		return remoteToolNameCursor
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
	return kind == remoteToolNameGemini || kind == remoteToolNameCodex
}

func (kind remoteToolNameKind) ConfigDirName() string {
	switch kind {
	case remoteToolNameClaude:
		return ".claude"
	case remoteToolNameGemini:
		return ".gemini"
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
