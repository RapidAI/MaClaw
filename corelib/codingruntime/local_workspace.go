package codingruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// NewLocalGitWorkspaceProber returns a read-only Git workspace prober for a
// host-owned local project path. It invokes only `git rev-parse HEAD` and
// `git status --porcelain`; it never initializes a repository, writes a file,
// or attempts a recovery action. GUI, TUI, and MaClawSrv may use the same
// prober without importing one another's host packages.
func NewLocalGitWorkspaceProber(projectPath string) WorkspaceProber {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil
	}
	return WorkspaceProberFunc(func(ctx context.Context, task Task, _ Attempt) (*WorkspaceProbe, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		readGit := func(args ...string) (string, error) {
			cmd := exec.CommandContext(ctx, "git", args...)
			cmd.Dir = projectPath
			out, err := cmd.Output()
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(out)), nil
		}
		head, err := readGit("rev-parse", "HEAD")
		if err != nil {
			return nil, fmt.Errorf("read-only git baseline: %w", err)
		}
		status, err := readGit("status", "--porcelain=v1", "--untracked-files=all")
		if err != nil {
			return nil, fmt.Errorf("read-only git status: %w", err)
		}
		projectRef := strings.TrimSpace(task.ProjectRef)
		if projectRef == "" {
			projectRef = projectPath
		}
		sum := sha256.Sum256([]byte(status))
		return &WorkspaceProbe{
			ProjectRef: projectRef,
			Head:       head,
			StatusHash: fmt.Sprintf("sha256:%x", sum[:]),
			ObservedAt: time.Now().UTC(),
		}, nil
	})
}
