package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// SetMailboxSecretDir stores the 0600 mailbox secret root and points the
// desktop agent executor at the same directory. Passwords are not accepted
// over HTTP; the agent writes them in-process when it configures a mailbox.
func (s *HTTPServer) SetMailboxSecretDir(dir string, executor *agentservice.CoreAgentExecutor) {
	if executor != nil {
		executor.MailboxSecretDir = strings.TrimSpace(dir)
	}
}
