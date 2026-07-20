package main

import (
	"context"
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

func TestProjectPathFromUserID_NormalizesProjectTabOwner(t *testing.T) {
	got := projectPathFromUserID(`desktop-user:d:\workprj\test5\.`)
	if got != `D:\workprj\test5` {
		t.Fatalf("projectPathFromUserID normalized = %q, want %q", got, `D:\workprj\test5`)
	}
}

func TestProjectTabWorkDir_ValidDirectory(t *testing.T) {
	// Create a temporary directory to simulate a valid project path.
	tmpDir := t.TempDir()

	h := &IMMessageHandler{}

	got := h.projectTabWorkDirForOwner(desktopUserID + ":" + tmpDir)
	if got != tmpDir {
		t.Errorf("projectTabWorkDirForOwner() = %q, want %q", got, tmpDir)
	}
}

func TestProjectTabWorkDir_NoRuntimeOwnerDoesNotUseLastUserID(t *testing.T) {
	tmpDir := t.TempDir()
	h := &IMMessageHandler{lastUserID: desktopUserID + ":" + tmpDir}

	if got := h.projectTabWorkDir(); got != "" {
		t.Fatalf("projectTabWorkDir() = %q, want empty without runtime owner", got)
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

func TestToolBashEmptyRuntimeOwnerFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	h := &IMMessageHandler{
		lastUserID: desktopUserID + ":" + tmpDir,
	}

	got := h.toolBash(context.Background(), map[string]interface{}{
		"command":                        "pwd",
		registeredToolPolicyOwnerIDField: "",
	}, nil)
	if got == "" || !containsText(got, "runtime owner is missing") {
		t.Fatalf("bash with empty runtime owner should fail closed, got %q", got)
	}
}

func TestProjectTabWorkDir_InvalidDirectoryFailsClosed(t *testing.T) {
	// Use a non-existent path as projectPath.
	nonExistent := filepath.Join(t.TempDir(), "does_not_exist_xyz")

	h := &IMMessageHandler{}

	got := h.projectTabWorkDirForOwner(desktopUserID + ":" + nonExistent)
	if got != nonExistent {
		t.Errorf("projectTabWorkDirForOwner() with invalid path = %q, want missing project path %q", got, nonExistent)
	}
	if _, err := os.Stat(nonExistent); !os.IsNotExist(err) {
		t.Fatalf("invalid non-task project path must not be auto-created, stat err=%v", err)
	}
}

func TestProjectTabWorkDir_RepairsManagedRecentTaskWorkspace(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	t.Cleanup(func() {
		app.stopMemoryPipelineSchedule("test-cleanup")
		if app.memoryStore != nil {
			app.memoryStore.Stop()
		}
	})
	projectPath := filepath.Join(app.GetDataDir(), "tasks", "missing-task")
	wantWS := filepath.Join(projectPath, "workspace")
	h := &IMMessageHandler{app: app}

	// Managed tasks must execute under workspace/, not the task metadata root.
	if got := h.projectTabWorkDirForOwner(desktopUserID + ":" + projectPath); got != wantWS {
		t.Fatalf("projectTabWorkDirForOwner() = %q, want workspace %q", got, wantWS)
	}
	if info, err := os.Stat(projectPath); err != nil || !info.IsDir() {
		t.Fatalf("managed task root was not repaired, info=%v err=%v", info, err)
	}
	if info, err := os.Stat(wantWS); err != nil || !info.IsDir() {
		t.Fatalf("managed task workspace/ was not prepared, info=%v err=%v", info, err)
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

	h := &IMMessageHandler{}

	got := h.resolveToolWorkDirForOwner("", desktopUserID+":"+tmpDir)
	if got != tmpDir {
		t.Errorf("resolveToolWorkDirForOwner(\"\") in Project Tab = %q, want %q", got, tmpDir)
	}
}

func TestResolveToolWorkDir_TaskOwnerUsesTaskWorkingDirectory(t *testing.T) {
	app := newProjectSearchTestApp(t)
	workingDir := t.TempDir()
	task := app.CreateTask("Use isolated session with shared workdir", workingDir)
	if task.ProjectPath == "" {
		t.Fatalf("CreateTask returned empty project path: %+v", task)
	}
	h := &IMMessageHandler{app: app}

	got := h.resolveToolWorkDirForOwner("", projectSessionOwnerID(task.ProjectPath))
	if got != filepath.Clean(workingDir) {
		t.Fatalf("resolveToolWorkDirForOwner() = %q, want task working dir %q", got, filepath.Clean(workingDir))
	}
	if ownerPath := projectPathFromUserID(projectSessionOwnerID(task.ProjectPath)); ownerPath != task.ProjectPath {
		t.Fatalf("project owner path = %q, want isolated task path %q", ownerPath, task.ProjectPath)
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

func TestFileToolsUseRuntimeOwnerProjectForRelativePaths(t *testing.T) {
	projectDir := t.TempDir()
	h := &IMMessageHandler{lastUserID: desktopUserID}
	args := map[string]interface{}{
		"path":                           "notes.txt",
		"content":                        "project-owned",
		registeredToolPolicyOwnerIDField: desktopUserID + ":" + projectDir,
	}

	result := h.toolWriteFile(args)
	if !containsText(result, filepath.Join(projectDir, "notes.txt")) {
		t.Fatalf("toolWriteFile result = %q, want project path", result)
	}
	data, err := os.ReadFile(filepath.Join(projectDir, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile project notes: %v", err)
	}
	if string(data) != "project-owned" {
		t.Fatalf("project notes = %q", data)
	}
}

func TestExecuteFileToolInjectsRuntimeOwnerForRelativePaths(t *testing.T) {
	desktopDir := t.TempDir()
	projectDir := t.TempDir()
	h := &IMMessageHandler{
		lastUserID:     desktopUserID + ":" + desktopDir,
		currentLoopCtx: &LoopContext{Runtime: RuntimeContext{RequestID: "req-desktop", PolicyOwnerID: desktopUserID + ":" + desktopDir}},
		registry:       NewToolRegistry(),
	}
	registerBuiltinTools(h.registry, h)

	result := h.executeToolDetailedWithRuntime(desktopUserID+":"+projectDir, "", "write_file", `{"path":"notes.txt","content":"project-owned"}`, "", nil)
	if result.Outcome != toolOutcomeSucceeded {
		t.Fatalf("write_file outcome = %v text=%q", result.Outcome, result.Text)
	}
	projectData, err := os.ReadFile(filepath.Join(projectDir, "notes.txt"))
	if err != nil {
		t.Fatalf("ReadFile project notes: %v", err)
	}
	if string(projectData) != "project-owned" {
		t.Fatalf("project notes = %q", projectData)
	}
	if _, err := os.Stat(filepath.Join(desktopDir, "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("write_file leaked into desktop dir, stat err=%v", err)
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
