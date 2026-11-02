package tool

import "sync"

// The host command-environment seam.
//
// Several host surfaces run shell commands with the process environment:
// background tasks (LocalBackgroundTaskManager), craft skill scripts, and the
// general assistant's bash tool. A machine regularly has a build toolchain
// installed outside the PATH this process inherits, so those surfaces fail on
// `go build` while the coding workbench — which repairs its own environment —
// succeeds. The augmenter below lets the host install one environment repair
// that every corelib shell shares; without it these surfaces run unchanged,
// so headless and test contexts keep their exact previous behavior.

var (
	commandEnvAugmenterMu sync.Mutex
	commandEnvAugmenter   func(env []string) []string
)

// SetCommandEnvAugmenter installs the host's command environment repair.
// Call once at host startup; the last installed function wins.
func SetCommandEnvAugmenter(fn func(env []string) []string) {
	commandEnvAugmenterMu.Lock()
	defer commandEnvAugmenterMu.Unlock()
	commandEnvAugmenter = fn
}

// AugmentCommandEnv applies the installed augmenter, or returns env unchanged
// when the host never installed one.
func AugmentCommandEnv(env []string) []string {
	commandEnvAugmenterMu.Lock()
	fn := commandEnvAugmenter
	commandEnvAugmenterMu.Unlock()
	if fn == nil {
		return env
	}
	return fn(env)
}
