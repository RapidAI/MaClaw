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
		if strings.HasPrefix(trimmed, "/goal ") || trimmed == "/goal" {
			return imCommandGoal
		}
		return imCommandUnknown
	}
}
