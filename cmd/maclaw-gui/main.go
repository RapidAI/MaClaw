package main

import "github.com/RapidAI/CodeClaw/guiapp"

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	guiapp.RegisterAgentHandlerFactory()
	if guiapp.Wails3Enabled() {
		guiapp.Main3(version)
		return
	}
	guiapp.Main(version)
}
