package main

import "strings"

type imCommandKind int

const (
	imCommandUnknown imCommandKind = iota
	imCommandReset
	imCommandExit
	imCommandSessions
	imCommandCompress
	imCommandMemory
	imCommandHelp
	imCommandBTW
	imCommandCancel
	imCommandLoop
	imCommandWorkflow
	imCommandGoal
	imCommandBranch
	imCommandMoA
	imCommandCodingWorkbench
	imCommandSkill   // /skill search|install|list|remove
	imCommandMCP     // /mcp search|install|list|remove|add
	imCommandPlugin  // /plugin marketplace|add|remove|search|installed
	imCommandInstall // other commands added only via shared allowlist JSON
)

func classifyImmediateIMCommand(trimmed string) imCommandKind {
	trimmed = strings.TrimSpace(trimmed)
	switch trimmed {
	case "/new", "/reset", "/clear":
		return imCommandReset
	case "/exit", "/quit":
		return imCommandExit
	case "/sessions", "/status":
		return imCommandSessions
	case "/compress":
		return imCommandCompress
	case "/memory":
		return imCommandMemory
	case "/help":
		return imCommandHelp
	case "/cancel", "/取消", "/鍙栨秷":
		return imCommandCancel
	default:
		if strings.HasPrefix(trimmed, "/btw ") || trimmed == "/btw" {
			return imCommandBTW
		}
		if strings.HasPrefix(trimmed, "/loop ") || trimmed == "/loop" {
			return imCommandLoop
		}
		if strings.HasPrefix(trimmed, "/workflow ") || trimmed == "/workflow" {
			return imCommandWorkflow
		}
		// Case-insensitive: /goal and /Goal (compose / typed).
		if lowerGoal := strings.ToLower(trimmed); lowerGoal == "/goal" || strings.HasPrefix(lowerGoal, "/goal ") {
			return imCommandGoal
		}
		if strings.HasPrefix(trimmed, "/branch ") || trimmed == "/branch" {
			return imCommandBranch
		}
		// Case-insensitive /moa (compose mode and typed slash).
		lower := strings.ToLower(trimmed)
		if lower == "/moa" || strings.HasPrefix(lower, "/moa ") {
			return imCommandMoA
		}
		// Skill / MCP / Plugin install commands (slash or bare CLI-style).
		if kind := classifyInstallIMCommand(trimmed); kind != imCommandUnknown {
			return kind
		}
		// Pure-coding workbench helpers (/plan /review /test /commit /pr /map /checkpoint /agents).
		if isCodingWorkbenchSlash(trimmed) {
			return imCommandCodingWorkbench
		}
		return imCommandUnknown
	}
}
