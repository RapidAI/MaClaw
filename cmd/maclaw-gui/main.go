package main

import "github.com/RapidAI/CodeClaw/guiapp"

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	guiapp.RegisterAgentHandlerFactory()
	guiapp.Main(version)
}
