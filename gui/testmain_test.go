package main

import (
	"os"
	"strings"
	"testing"
)

// TestMain keeps the package's default agent-loop path on the legacy IM loop
// unless a test explicitly opts into the shared loop via MACLAW_SHARED_AGENT_LOOP.
// SharedAgentLoopEnabled defaults true for new installs, which would otherwise
// divert most RunAgentLoop trace/recover tests onto the shared path and fail
// features not yet parity-complete there (trial reflect, token estimate, etc.).
func TestMain(m *testing.M) {
	if strings.TrimSpace(os.Getenv("MACLAW_SHARED_AGENT_LOOP")) == "" {
		_ = os.Setenv("MACLAW_SHARED_AGENT_LOOP", "0")
	}
	os.Exit(m.Run())
}
