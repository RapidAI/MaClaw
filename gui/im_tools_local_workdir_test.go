package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// Unit tests for Task 9.1: Wire projectPath to tool execution working directory
//
// **Validates: Requirements 8.3**
// ---------------------------------------------------------------------------

func TestProjectPathFromUserID_ProjectTab(t *testing.T) {
	tests := []struct {
		name     string
		userID   string
		expected string
	}{
		{
			name:     "project tab with Windows path",
			userID:   "desktop-user:D:\\workprj\\test5",
			expected: "D:\\workprj\\test5",
		},
		{
			name:     "project tab with Unix path",
			userID:   "desktop-user:/home/user/project",
			expected: "/home/user/project",
		},
		{
			name:     "local tab (no colon suffix)",
			userID:   "desktop-user",
			expected: "",
		},
		{
			name:     "empty userID",
			userID:   "",
			expected: "",
		},
		{
			name:     "IM user (not desktop)",
			userID:   "feishu_ou_abc123",
			expected: "",
		},
		{
			name:     "desktop-user: with empty path after colon",
			userID:   "desktop-user:",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectPathFromUserID(tt.userID)
			if got != tt.expected {
				t.Errorf("projectPathFromUserID(%q) = %q, want %q", tt.userID, got, tt.expected)
			}
		})
	}
}

func TestProjectTabWorkDir_ValidDirectory(t *testing.T) {
	// Create a temporary directory to simulate a valid project path.
	tmpDir := t.TempDir()

	h := &IMMessageHandler{
		lastUserID: "desktop-user:" + tmpDir,
	}

	got := h.projectTabWorkDir()
	if got != tmpDir {
		t.Errorf("projectTabWorkDir() = %q, want %q", got, tmpDir)
	}
}

func TestProjectTabWorkDir_UsesRuntimeOwner(t *testing.T) {
	tmpDir := t.TempDir()
	h := &IMMessageHandler{
		lastUserID:     desktopUserID,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-project", PolicyOwnerID: desktopUserID + ":" + tmpDir}},
	}

	if got := h.projectTabWorkDir(); got != tmpDir {
		t.Fatalf("projectTabWorkDir() = %q, want runtime owner project %q", got, tmpDir)
	}
}

func TestProjectTabWorkDir_EmptyRuntimeOwnerDoesNotFallbackToLastUser(t *testing.T) {
	tmpDir := t.TempDir()
	h := &IMMessageHandler{
		lastUserID:     desktopUserID + ":" + tmpDir,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-no-owner"}},
	}

	if got := h.projectTabWorkDir(); got != "" {
		t.Fatalf("projectTabWorkDir() = %q, want no fallback to lastUserID project", got)
	}
	if got := h.resolveToolWorkDir(""); got == tmpDir {
		t.Fatalf("resolveToolWorkDir() inherited lastUserID project %q without runtime owner", got)
	}
}

func TestProjectTabWorkDir_InvalidDirectory_FallsBackToHome(t *testing.T) {
	// Use a non-existent path as projectPath.
	nonExistent := filepath.Join(t.TempDir(), "does_not_exist_xyz")

	h := &IMMessageHandler{
		lastUserID: "desktop-user:" + nonExistent,
	}

	got := h.projectTabWorkDir()
	home, _ := os.UserHomeDir()
	if got != home {
		t.Errorf("projectTabWorkDir() with invalid path = %q, want home dir %q", got, home)
	}
}

func TestProjectTabWorkDir_NotProjectTab(t *testing.T) {
	h := &IMMessageHandler{
		lastUserID: "desktop-user",
	}

	got := h.projectTabWorkDir()
	if got != "" {
		t.Errorf("projectTabWorkDir() for local tab = %q, want empty string", got)
	}
}

func TestResolveToolWorkDir_ExplicitWorkDir(t *testing.T) {
	// When an explicit working_dir is provided, it should be used regardless
	// of whether we're in a Project Tab.
	tmpDir := t.TempDir()

	h := &IMMessageHandler{
		lastUserID: "desktop-user:" + tmpDir,
	}

	// Provide an explicit absolute path as working_dir.
	explicitDir := t.TempDir()
	got := h.resolveToolWorkDir(explicitDir)
	if got != filepath.Clean(explicitDir) {
		t.Errorf("resolveToolWorkDir(%q) = %q, want %q", explicitDir, got, filepath.Clean(explicitDir))
	}
}

func TestResolveToolWorkDir_EmptyWorkDir_ProjectTab(t *testing.T) {
	// When working_dir is empty and we're in a Project Tab, use projectPath.
	tmpDir := t.TempDir()

	h := &IMMessageHandler{
		lastUserID: "desktop-user:" + tmpDir,
	}

	got := h.resolveToolWorkDir("")
	if got != tmpDir {
		t.Errorf("resolveToolWorkDir(\"\") in Project Tab = %q, want %q", got, tmpDir)
	}
}

func TestResolveToolWorkDir_ExplicitOwnerOverridesGlobalLoop(t *testing.T) {
	desktopDir := t.TempDir()
	mobileDir := t.TempDir()
	h := &IMMessageHandler{
		lastUserID:     desktopUserID + ":" + desktopDir,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: desktopUserID + ":" + desktopDir}},
	}

	got := h.resolveToolWorkDirForOwner("", desktopUserID+":"+mobileDir)
	if got != mobileDir {
		t.Fatalf("resolveToolWorkDirForOwner() = %q, want explicit owner project %q", got, mobileDir)
	}
}

func TestResolveToolWorkDir_EmptyWorkDir_LocalTab(t *testing.T) {
	// When working_dir is empty and we're in the local tab, use default workspace.
	h := &IMMessageHandler{
		lastUserID: "desktop-user",
	}

	got := h.resolveToolWorkDir("")
	// Should return the default workspace directory (resolvePath(""))
	expected := resolvePath("")
	if got != expected {
		t.Errorf("resolveToolWorkDir(\"\") in local tab = %q, want %q", got, expected)
	}
}
