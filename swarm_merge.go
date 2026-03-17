package main

import (
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
)

// MergeController merges worktree branches back to the main branch in
// topological order, verifying compilation after each merge.
type MergeController struct {
	worktreeMgr *WorktreeManager
}

// NewMergeController creates a MergeController.
func NewMergeController(wm *WorktreeManager) *MergeController {
	return &MergeController{worktreeMgr: wm}
}

// TopologicalSort sorts branches by their dependency order. Branches with
// lower Order values are merged first.
func TopologicalSort(branches []BranchInfo) []BranchInfo {
	sorted := make([]BranchInfo, len(branches))
	copy(sorted, branches)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Order < sorted[j].Order
	})
	return sorted
}

// MergeAll merges branches in topological order. For each branch it:
// 1. Merges the branch into the current branch
// 2. Optionally runs a compile command to verify
// If a merge or compile fails, that branch is reverted and recorded as failed.
//
// compileCmd is optional — pass "" to skip compilation checks.
func (m *MergeController) MergeAll(projectPath string, branches []BranchInfo, compileCmd string) (*MergeResult, error) {
	sorted := TopologicalSort(branches)
	result := &MergeResult{Success: true}

	for _, b := range sorted {
		// Attempt merge
		err := runGit(projectPath, "merge", "--no-ff", b.Name, "-m",
			fmt.Sprintf("swarm: merge %s", b.Name))
		if err != nil {
			log.Printf("[MergeController] merge conflict on %s: %v", b.Name, err)
			// Abort the failed merge
			_ = runGit(projectPath, "merge", "--abort")
			result.FailedBranches = append(result.FailedBranches, b.Name)
			result.Success = false
			continue
		}

		// Optional compile check
		if compileCmd != "" {
			if err := m.runCompile(projectPath, compileCmd); err != nil {
				log.Printf("[MergeController] compile failed after merging %s: %v", b.Name, err)
				result.CompileErrors = append(result.CompileErrors, fmt.Sprintf("%s: %v", b.Name, err))
				// Revert this merge
				if revertErr := m.RevertBranch(projectPath, b.Name); revertErr != nil {
					log.Printf("[MergeController] revert failed for %s: %v", b.Name, revertErr)
				}
				result.FailedBranches = append(result.FailedBranches, b.Name)
				result.Success = false
				continue
			}
		}

		result.MergedBranches = append(result.MergedBranches, b.Name)
	}

	return result, nil
}

// RevertBranch reverts the last merge commit (assumes it was a --no-ff merge
// that just happened). Uses git reset instead of revert for reliability.
func (m *MergeController) RevertBranch(projectPath, branchName string) error {
	// Verify HEAD is a merge commit before resetting
	out, err := runGitOutput(projectPath, "cat-file", "-p", "HEAD")
	if err != nil {
		return fmt.Errorf("inspect HEAD: %w", err)
	}
	// A merge commit has two or more "parent" lines
	parentCount := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "parent ") {
			parentCount++
		}
	}
	if parentCount < 2 {
		return fmt.Errorf("HEAD is not a merge commit (expected merge of %s)", branchName)
	}
	return runGit(projectPath, "reset", "--hard", "HEAD~1")
}

// runCompile executes a compile command in the project directory.
func (m *MergeController) runCompile(projectPath, compileCmd string) error {
	parts := strings.Fields(compileCmd)
	if len(parts) == 0 {
		return nil
	}
	return runShellCommand(projectPath, parts[0], parts[1:]...)
}

// runShellCommand executes a command in the given directory.
func runShellCommand(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	hideCommandWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, string(out))
	}
	return nil
}
