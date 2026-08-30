//go:build !windows

package tool

import (
	"context"
	"os/exec"
)

// NewWindowsShellCommand is retained on non-Windows targets so callers that
// select the Windows path at runtime remain buildable. It is not used there.
func NewWindowsShellCommand(ctx context.Context, command, workDir string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workDir
	return cmd
}
