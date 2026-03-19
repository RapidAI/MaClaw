package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type WorkspaceMode string

const (
	WorkspaceModeDirect      WorkspaceMode = "direct"
	WorkspaceModeGitWorktree WorkspaceMode = "git_worktree"
)

type PreparedWorkspace struct {
	ProjectPath string
	RootPath    string
	Mode        WorkspaceMode
	IsGitRepo   bool
	GitRoot     string
	Release     func()
}

type WorkspacePreparer interface {
	Prepare(sessionID string, spec LaunchSpec) (*PreparedWorkspace, error)
}

type DefaultWorkspacePreparer struct {
	mu         sync.Mutex
	active     map[string]string
	worktreeMu sync.Mutex
}

func NewDefaultWorkspacePreparer() *DefaultWorkspacePreparer {
	return &DefaultWorkspacePreparer{
		active: map[string]string{},
	}
}

func (p *DefaultWorkspacePreparer) Prepare(sessionID string, spec LaunchSpec) (*PreparedWorkspace, error) {
	projectPath, err := canonicalWorkspacePath(spec.ProjectPath)
	if err != nil {
		return nil, err
	}

	gitRoot, isGitRepo := detectGitWorkspaceRoot(projectPath)
	if !isGitRepo {
		return p.prepareLockedDirect(sessionID, projectPath, "", false)
	}

	if !gitRepoHasCommits(gitRoot) {
		return p.prepareLockedDirect(sessionID, projectPath, gitRoot, true)
	}

	return p.prepareGitWorktree(sessionID, projectPath, gitRoot)
}

func (p *DefaultWorkspacePreparer) prepareLockedDirect(sessionID, projectPath, gitRoot string, isGitRepo bool) (*PreparedWorkspace, error) {
	lockRoot := projectPath
	if isGitRepo && gitRoot != "" {
		lockRoot = gitRoot
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if owner, exists := p.active[lockRoot]; exists {
		if isGitRepo {
			return nil, fmt.Errorf("workspace is busy: git workspace %s is already in use by session %s", lockRoot, owner)
		}
		return nil, fmt.Errorf("workspace is busy: directory %s is already in use by session %s", lockRoot, owner)
	}

	p.active[lockRoot] = sessionID

	released := false
	release := func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if released {
			return
		}
		released = true
		if owner, exists := p.active[lockRoot]; exists && owner == sessionID {
			delete(p.active, lockRoot)
		}
	}

	return &PreparedWorkspace{
		ProjectPath: projectPath,
		RootPath:    lockRoot,
		Mode:        WorkspaceModeDirect,
		IsGitRepo:   isGitRepo,
		GitRoot:     gitRoot,
		Release:     release,
	}, nil
}

func (p *DefaultWorkspacePreparer) prepareGitWorktree(sessionID, projectPath, gitRoot string) (*PreparedWorkspace, error) {
	worktreeRoot, err := ensureWorktreeRootDir()
	if err != nil {
		return nil, err
	}

	branchName := "maclaw/" + sanitizeWorkspaceName(sessionID)
	worktreePath := filepath.Join(worktreeRoot, sanitizeWorkspaceName(sessionID))
	if err := os.RemoveAll(worktreePath); err != nil {
		return nil, fmt.Errorf("cleanup stale worktree path: %w", err)
	}

	relativeProjectPath, err := filepath.Rel(gitRoot, projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path inside git repo: %w", err)
	}
	if relativeProjectPath == "." {
		relativeProjectPath = ""
	}

	p.worktreeMu.Lock()
	defer p.worktreeMu.Unlock()

	if err := runGit(gitRoot, "branch", "-D", branchName); err != nil && !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "cannot delete branch") {
		// Ignore common "branch doesn't exist" cases, but fail on anything else.
	}

	if err := runGit(gitRoot, "worktree", "add", "-b", branchName, worktreePath, "HEAD"); err != nil {
		return nil, fmt.Errorf("create git worktree: %w", err)
	}

	preparedProjectPath := worktreePath
	if relativeProjectPath != "" {
		preparedProjectPath = filepath.Join(worktreePath, relativeProjectPath)
	}
	if err := ensurePreparedProjectPath(projectPath, preparedProjectPath); err != nil {
		_ = runGit(gitRoot, "worktree", "remove", "--force", worktreePath)
		_ = os.RemoveAll(worktreePath)
		_ = runGit(gitRoot, "branch", "-D", branchName)
		return nil, err
	}

	released := false
	release := func() {
		p.worktreeMu.Lock()
		defer p.worktreeMu.Unlock()
		if released {
			return
		}
		released = true
		_ = runGit(gitRoot, "worktree", "remove", "--force", worktreePath)
		_ = os.RemoveAll(worktreePath)
		_ = runGit(gitRoot, "branch", "-D", branchName)
		_ = runGit(gitRoot, "worktree", "prune")
	}

	return &PreparedWorkspace{
		ProjectPath: preparedProjectPath,
		RootPath:    worktreePath,
		Mode:        WorkspaceModeGitWorktree,
		IsGitRepo:   true,
		GitRoot:     gitRoot,
		Release:     release,
	}, nil
}

func canonicalWorkspacePath(path string) (string, error) {
	cleaned := strings.TrimSpace(path)
	if cleaned == "" {
		return "", fmt.Errorf("project path is empty")
	}

	absPath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}

	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Auto-create the project directory so that remote sessions
			// targeting a new project path don't fail at launch.
			if mkErr := os.MkdirAll(absPath, 0o755); mkErr != nil {
				return "", fmt.Errorf("project path does not exist and could not be created: %w", mkErr)
			}
			// Re-stat after creation.
			info, err = os.Stat(absPath)
			if err != nil {
				return "", fmt.Errorf("project path not accessible after creation: %w", err)
			}
		} else {
			return "", fmt.Errorf("project path not accessible: %w", err)
		}
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project path is not a directory: %s", absPath)
	}

	return filepath.Clean(absPath), nil
}

func detectGitWorkspaceRoot(path string) (string, bool) {
	output, err := runGitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}

	root := strings.TrimSpace(output)
	if root == "" {
		return "", false
	}

	if realPath, err := filepath.EvalSymlinks(root); err == nil {
		root = realPath
	}

	return filepath.Clean(root), true
}

func gitRepoHasCommits(path string) bool {
	_, err := runGitOutput(path, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func ensureWorktreeRootDir() (string, error) {
	root := filepath.Join(os.TempDir(), "maclaw-worktrees")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create worktree root: %w", err)
	}
	return root, nil
}

func ensurePreparedProjectPath(sourcePath, preparedPath string) error {
	if _, err := os.Stat(preparedPath); err == nil {
		return nil
	}

	if err := os.MkdirAll(preparedPath, 0o755); err != nil {
		return fmt.Errorf("create prepared project path: %w", err)
	}

	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}

		targetPath := filepath.Join(preparedPath, relPath)
		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return err
		}
		return copyFile(path, targetPath, info.Mode())
	})
}

func copyFile(sourcePath, targetPath string, mode os.FileMode) error {
	input, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	return os.WriteFile(targetPath, input, mode)
}

func sanitizeWorkspaceName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "session"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func runGit(repoPath string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	hideCommandWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return nil
}

func runGitOutput(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	hideCommandWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
