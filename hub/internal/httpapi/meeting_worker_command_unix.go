//go:build !windows

package httpapi

import (
	"context"
	"os/exec"
)

// newMeetingWorkerCommand runs one operator-configured worker command line
// through the platform shell.
func newMeetingWorkerCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", command)
}
