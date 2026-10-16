package guiapp

import "github.com/RapidAI/CodeClaw/corelib/agentruntime"

func parseScreenshotDisplayIndex(raw interface{}) (int, error) {
	return agentruntime.ParseDesktopDisplayIndex(raw)
}
