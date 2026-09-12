package codingruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EnsureLocalGitBaseline makes sure projectPath has a usable Git baseline so a
// gated writer can pass the read-only workspace probe. A repository whose HEAD
// already resolves (including one inherited from a parent directory) is left
// untouched; otherwise it runs `git init` plus an empty baseline commit using a
// host-supplied identity, independent of the user's global git config.
func EnsureLocalGitBaseline(ctx context.Context, projectPath string) error {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return fmt.Errorf("git baseline: empty project path")
	}
	info, err := os.Stat(projectPath)
	if err != nil {
		return fmt.Errorf("git baseline: stat project path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("git baseline: project path is not a directory")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git baseline: git is unavailable: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runGit := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = projectPath
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := runGit("rev-parse", "HEAD"); err == nil {
		return nil
	}
	// core.longpaths lets the baseline and later probes survive workspace paths
	// past the Windows MAX_PATH limit; it is a no-op elsewhere.
	if err := runGit("-c", "core.longpaths=true", "init"); err != nil {
		return fmt.Errorf("git baseline init: %w", err)
	}
	if err := runGit("-c", "core.longpaths=true", "-c", "user.name=MaClaw", "-c", "user.email=maclaw@localhost", "commit", "--allow-empty", "-m", "chore: initialize workspace baseline"); err != nil {
		return fmt.Errorf("git baseline commit: %w", err)
	}
	return nil
}

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
			// core.longpaths keeps read-only inspection working for workspaces
			// beyond the Windows MAX_PATH limit; it is a no-op elsewhere.
			cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.longpaths=true"}, args...)...)
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
