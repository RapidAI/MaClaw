package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// codingWorkbenchGitSummary is a compact git status for slash/UI.
type codingWorkbenchGitSummary struct {
	IsRepo      bool   `json:"is_repo"`
	Branch      string `json:"branch,omitempty"`
	StatusShort string `json:"status_short,omitempty"`
	DiffStat    string `json:"diff_stat,omitempty"`
	Error       string `json:"error,omitempty"`
}

func runGitInProject(ctx context.Context, projectPath string, args ...string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", fmt.Errorf("empty project path")
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = projectPath
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if out != "" {
			return out, fmt.Errorf("%s", msg)
		}
		return "", fmt.Errorf("%s", msg)
	}
	return out, nil
}

func codingWorkbenchGitIsRepo(projectPath string) bool {
	out, err := runGitInProject(nil, projectPath, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func codingWorkbenchCollectGitSummary(projectPath string) codingWorkbenchGitSummary {
	s := codingWorkbenchGitSummary{}
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		s.Error = "empty project path"
		return s
	}
	if !codingWorkbenchGitIsRepo(projectPath) {
		s.Error = "not a git repository"
		return s
	}
	s.IsRepo = true
	if branch, err := runGitInProject(nil, projectPath, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		s.Branch = strings.TrimSpace(branch)
	}
	if st, err := runGitInProject(nil, projectPath, "status", "--short"); err == nil {
		s.StatusShort = st
	} else {
		s.Error = err.Error()
	}
	if ds, err := runGitInProject(nil, projectPath, "diff", "--stat", "--"); err == nil {
		s.DiffStat = ds
	}
	// Cap large outputs.
	if utf8.RuneCountInString(s.StatusShort) > 3000 {
		s.StatusShort = truncateRunesForSubAgent(s.StatusShort, 3000)
	}
	if utf8.RuneCountInString(s.DiffStat) > 2000 {
		s.DiffStat = truncateRunesForSubAgent(s.DiffStat, 2000)
	}
	return s
}

func formatCodingWorkbenchGitSummary(s codingWorkbenchGitSummary) string {
	if !s.IsRepo {
		if s.Error != "" {
			return "Git: " + s.Error
		}
		return "Git: not a repository"
	}
	var b strings.Builder
	b.WriteString("## Git status\n\n")
	if s.Branch != "" {
		b.WriteString("**Branch**: `")
		b.WriteString(s.Branch)
		b.WriteString("`\n\n")
	}
	if strings.TrimSpace(s.StatusShort) == "" {
		b.WriteString("Working tree clean.\n")
	} else {
		b.WriteString("```\n")
		b.WriteString(s.StatusShort)
		b.WriteString("\n```\n")
	}
	if strings.TrimSpace(s.DiffStat) != "" {
		b.WriteString("\n**Diff stat**\n```\n")
		b.WriteString(s.DiffStat)
		b.WriteString("\n```\n")
	}
	return b.String()
}

// codingWorkbenchGitCommit stages all and commits with message (no push).
// Returns commit hash on success.
func codingWorkbenchGitCommit(projectPath, message string) (hash string, err error) {
	projectPath = strings.TrimSpace(projectPath)
	message = strings.TrimSpace(message)
	if projectPath == "" {
		return "", fmt.Errorf("empty project path")
	}
	if message == "" {
		return "", fmt.Errorf("commit message required")
	}
	if !codingWorkbenchGitIsRepo(projectPath) {
		return "", fmt.Errorf("not a git repository")
	}
	if _, err := runGitInProject(nil, projectPath, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}
	// Allow empty? No — refuse empty commit.
	st, _ := runGitInProject(nil, projectPath, "status", "--porcelain")
	if strings.TrimSpace(st) == "" {
		// Check staged.
		staged, _ := runGitInProject(nil, projectPath, "diff", "--cached", "--name-only")
		if strings.TrimSpace(staged) == "" {
			return "", fmt.Errorf("nothing to commit")
		}
	}
	if _, err := runGitInProject(nil, projectPath, "commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}
	hash, err = runGitInProject(nil, projectPath, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", nil // commit succeeded; hash optional
	}
	return strings.TrimSpace(hash), nil
}

// codingWorkbenchSuggestPRBody builds a markdown PR body from sticky memory + git.
func codingWorkbenchSuggestPRBody(projectPath string, mem stickyCodingWorkbenchMemory) string {
	var b strings.Builder
	b.WriteString("## Summary\n\n")
	if s := strings.TrimSpace(mem.SessionPlan); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	} else if s := strings.TrimSpace(mem.LastUserText); s != "" {
		b.WriteString(s)
		b.WriteString("\n\n")
	} else {
		b.WriteString("Pure-coding workbench changes.\n\n")
	}
	if s := strings.TrimSpace(mem.LastSummary); s != "" {
		b.WriteString("## Notes\n\n")
		b.WriteString(truncateRunesForSubAgent(s, 800))
		b.WriteString("\n\n")
	}
	gs := codingWorkbenchCollectGitSummary(projectPath)
	if gs.IsRepo {
		b.WriteString("## Diff stat\n\n```\n")
		if gs.DiffStat != "" {
			b.WriteString(gs.DiffStat)
		} else {
			b.WriteString("(clean or unstaged only)")
		}
		b.WriteString("\n```\n\n")
		b.WriteString("## Test plan\n\n")
		if cmd := detectProjectVerifyCommand(projectPath); cmd != "" {
			b.WriteString("- [ ] `")
			b.WriteString(cmd)
			b.WriteString("`\n")
		} else {
			b.WriteString("- [ ] Manual verification\n")
		}
	}
	return b.String()
}

// codingWorkbenchTryOpenPR uses gh pr create when available; otherwise returns draft body.
func codingWorkbenchTryOpenPR(projectPath, title, body string) (result string, err error) {
	projectPath = strings.TrimSpace(projectPath)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Coding workbench changes"
	}
	if body == "" {
		body = "Automated PR body from pure-coding workbench."
	}
	// Prefer GitHub CLI when present.
	if _, lookErr := exec.LookPath("gh"); lookErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "gh", "pr", "create", "--title", title, "--body", body)
		cmd.Dir = projectPath
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		if runErr == nil {
			url := strings.TrimSpace(stdout.String())
			if url == "" {
				url = "(PR created)"
			}
			return "PR created: " + url, nil
		}
		// Fall through to draft if gh fails (auth, etc.).
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = runErr.Error()
		}
		return fmt.Sprintf("## PR draft (gh failed: %s)\n\n**Title**: %s\n\n%s", errMsg, title, body), nil
	}
	return fmt.Sprintf("## PR draft (install GitHub CLI `gh` to auto-create)\n\n**Title**: %s\n\n%s\n\n_Repo_: `%s`", title, body, filepath.Base(projectPath)), nil
}
