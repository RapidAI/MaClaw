package main

import "strings"

type craftLanguageKind string

const (
	craftLanguageBash       craftLanguageKind = "bash"
	craftLanguagePython     craftLanguageKind = "python"
	craftLanguageNode       craftLanguageKind = "node"
	craftLanguagePowerShell craftLanguageKind = "powershell"
)

func normalizeCraftLanguageKind(language string) craftLanguageKind {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "javascript", "node":
		return craftLanguageNode
	case "pwsh", "powershell":
		return craftLanguagePowerShell
	case "python":
		return craftLanguagePython
	case "bash":
		return craftLanguageBash
	default:
		return craftLanguageBash
	}
}

func (kind craftLanguageKind) String() string {
	return string(kind)
}

func (kind craftLanguageKind) ScriptExtension() string {
	switch kind {
	case craftLanguagePython:
		return ".py"
	case craftLanguageNode:
		return ".js"
	case craftLanguagePowerShell:
		return ".ps1"
	default:
		return ".sh"
	}
}
