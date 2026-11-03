package codingruntime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// gitCommandTimeout bounds every host git invocation. Windows hosts with
// aggressive antivirus or stuck index locks have been observed to hang a git
// child for minutes; a probe or baseline must fail instead of blocking the
// caller forever.
const (
	gitBaselineCommandTimeout = 5 * time.Minute
	gitProbeCommandTimeout    = 2 * time.Minute
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
		cmdCtx, cancel := context.WithTimeout(ctx, gitBaselineCommandTimeout)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, "git", args...)
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
			cmdCtx, cancel := context.WithTimeout(ctx, gitProbeCommandTimeout)
			defer cancel()
			cmd := exec.CommandContext(cmdCtx, "git", append([]string{"-c", "core.longpaths=true"}, args...)...)
			cmd.Dir = projectPath
			out, err := cmd.CombinedOutput()
			if err != nil {
				return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
			return strings.TrimSpace(string(out)), nil
		}
		head, err := readGit("--no-optional-locks", "rev-parse", "HEAD")
		if err != nil {
			return nil, fmt.Errorf("read-only git baseline: %w", err)
		}
		// Status is limited to projectPath. A project inside a home repository
		// must not hash the rest of that repository. --no-optional-locks avoids
		// an index lock. Untracked mode is chosen in readLocalGitStatus.
		status, err := readLocalGitStatus(ctx, projectPath)
		if err != nil {
			return nil, err
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

// readLocalGitStatus runs porcelain status and keeps stdout when git only
// warns that a directory could not be opened. Those warnings are not a dirty
// tree and must not skip the turn or be pasted into the chat.
func readLocalGitStatus(ctx context.Context, projectPath string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmdCtx, cancel := context.WithTimeout(ctx, gitProbeCommandTimeout)
	defer cancel()
	// "." is the project directory, not the repository root. Without it, git
	// status from a task folder still lists every untracked path in a parent
	// home repository.
	cmd := exec.CommandContext(cmdCtx, "git", "-c", "core.longpaths=true", "--no-optional-locks", "status", "--porcelain=v1", localGitStatusUntrackedMode(projectPath), "--", ".")
	cmd.Dir = projectPath
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	status := strings.TrimSpace(stdout.String())
	if err == nil {
		return status, nil
	}
	if cmdCtx.Err() != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("read-only git status: timed out")
	}
	if gitStatusStderrIsDirectoryNoise(stderr.String()) {
		return status, nil
	}
	return "", fmt.Errorf("read-only git status: %s", gitStatusFailureLine(stderr.String(), err))
}

// localGitStatusUntrackedMode keeps a repository rooted at the user home on
// tracked files only. normal would still stat every top-level untracked path
// (AppData, Temp, caches). Writes are attested by the host file-activity
// digest when the porcelain does not change. Any other directory keeps
// normal, so a new untracked file still changes the digest.
func localGitStatusUntrackedMode(projectPath string) string {
	if localGitProjectIsHome(projectPath) {
		return "--untracked-files=no"
	}
	return "--untracked-files=normal"
}

func localGitProjectIsHome(projectPath string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	return sameLocalPath(projectPath, home)
}

func sameLocalPath(a, b string) bool {
	a = strings.TrimRight(filepath.Clean(strings.TrimSpace(a)), `/\`)
	b = strings.TrimRight(filepath.Clean(strings.TrimSpace(b)), `/\`)
	if a == "." || b == "." || a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// gitStatusStderrIsDirectoryNoise is true when every stderr line is git's
// unreadable-directory warning. A fatal line, or an empty failure, is not.
func gitStatusStderrIsDirectoryNoise(stderr string) bool {
	saw := false
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		saw = true
		if !strings.HasPrefix(line, "warning: could not open directory ") && !strings.HasPrefix(line, "warning: unable to access ") {
			return false
		}
	}
	return saw
}

// gitStatusFailureLine is the one line a probe failure may show. The raw
// warning list from a home directory is not a user-facing error.
func gitStatusFailureLine(stderr string, err error) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") {
			return line
		}
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	if err == nil {
		return "git status failed"
	}
	return err.Error()
}
