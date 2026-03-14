package main

// ProviderAdapter describes how a managed CLI provider is launched and controlled.
type ProviderAdapter interface {
	ProviderName() string
	BuildCommand(spec LaunchSpec) (CommandSpec, error)

	// ExecutionMode returns how this provider should be launched.
	// "sdk" means structured JSON stdin/stdout (Claude Code stream-json).
	// "pty" means interactive pseudo-terminal (default for most tools).
	ExecutionMode() ExecutionMode
}
