package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/workflow"
)

// newTestAdapter creates a minimal GUIWorkflowAdapter for testing with the
// given workingDir, workflowType, and startDate. Fields are set directly
// (protected by mu in production, safe in single-goroutine tests).
// A bare &App{} is provided so that workflowProjectPath() passes the nil check
// and uses the workingDir field directly.
func newTestAdapter(workingDir string, wfType workflow.WorkflowType, startDate time.Time) *GUIWorkflowAdapter {
	a := &GUIWorkflowAdapter{
		app: &App{}, // minimal App so workflowProjectPath() doesn't short-circuit
	}
	a.workingDir = workingDir
	a.activeWorkflowType = wfType
	a.workflowStartDate = startDate
	return a
}

// --- Tests for publishToProjectStorage ---

func TestPublishToProjectStorage_CreatesCorrectDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	content := "# Requirements\n\nThis is the requirements document."
	a.publishToProjectStorage("requirements", content)

	expectedPath := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01", "01-requirements.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedPath, err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

func TestPublishToProjectStorage_EmptyContentSkipped(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	// Empty content (only whitespace)
	a.publishToProjectStorage("requirements", "   \n\t  ")

	// The docs directory should not be created at all
	docsDir := filepath.Join(tmpDir, "docs")
	if _, err := os.Stat(docsDir); !os.IsNotExist(err) {
		t.Errorf("expected docs directory to not exist when content is empty, but it does")
	}
}

func TestPublishToProjectStorage_EmptyWorkingDirSkipped(t *testing.T) {
	// workingDir is empty and app is nil — publish should be a no-op
	a := &GUIWorkflowAdapter{}
	a.activeWorkflowType = workflow.WorkflowCoding
	a.workflowStartDate = time.Now()

	// Should not panic or create any files
	a.publishToProjectStorage("requirements", "# Some content")

	// resolveProjectStorageDir should return empty
	dir := a.resolveProjectStorageDir()
	if dir != "" {
		t.Errorf("expected empty dir when workingDir is empty, got %q", dir)
	}
}

func TestPublishToProjectStorage_OverwriteBehavior(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	// First publish
	content1 := "# Requirements v1\n\nFirst version."
	a.publishToProjectStorage("requirements", content1)

	// Second publish with different content (same phaseID)
	content2 := "# Requirements v2\n\nUpdated version with more details."
	a.publishToProjectStorage("requirements", content2)

	// Verify only the latest content is on disk
	expectedPath := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01", "01-requirements.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedPath, err)
	}
	if string(data) != content2 {
		t.Errorf("expected latest content after overwrite:\ngot:  %q\nwant: %q", string(data), content2)
	}
}

// --- Tests for resolveProjectStorageDir ---

func TestResolveProjectStorageDir_CachesOnSecondCall(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	// First call resolves and caches
	dir1 := a.resolveProjectStorageDir()
	if dir1 == "" {
		t.Fatal("expected non-empty dir on first call")
	}

	// Second call should return the same cached value
	dir2 := a.resolveProjectStorageDir()
	if dir1 != dir2 {
		t.Errorf("expected cached value on second call:\nfirst:  %q\nsecond: %q", dir1, dir2)
	}

	// Verify the path structure
	expectedDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01")
	if dir1 != expectedDir {
		t.Errorf("unexpected resolved dir:\ngot:  %q\nwant: %q", dir1, expectedDir)
	}
}

func TestResolveProjectStorageDir_EmptyWorkingDir(t *testing.T) {
	// app is nil → workflowProjectPath returns empty → resolveProjectStorageDir returns empty
	a := &GUIWorkflowAdapter{}
	a.activeWorkflowType = workflow.WorkflowCoding
	a.workflowStartDate = time.Now()

	dir := a.resolveProjectStorageDir()
	if dir != "" {
		t.Errorf("expected empty string when workingDir is empty, got %q", dir)
	}
}

func TestResolveProjectStorageDir_EmptyWorkflowType(t *testing.T) {
	tmpDir := t.TempDir()
	a := &GUIWorkflowAdapter{app: &App{}}
	a.workingDir = tmpDir
	a.workflowStartDate = time.Now()
	// activeWorkflowType is zero value (empty string)

	dir := a.resolveProjectStorageDir()
	if dir != "" {
		t.Errorf("expected empty string when activeWorkflowType is empty, got %q", dir)
	}
}

func TestResolveProjectStorageDir_ProductDesignKebabCase(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 3, 15, 10, 0, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowProductDesign, startDate)

	dir := a.resolveProjectStorageDir()
	expectedDir := filepath.Join(tmpDir, "docs", "workflow", "product-design", "2026-03-15")
	if dir != expectedDir {
		t.Errorf("unexpected resolved dir for product_design:\ngot:  %q\nwant: %q", dir, expectedDir)
	}
}

func TestResolveProjectStorageDir_CollisionAvoidance(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)

	// Pre-create the base date directory to simulate a collision
	baseDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01")
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatal(err)
	}

	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	dir := a.resolveProjectStorageDir()
	expectedDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01-2")
	if dir != expectedDir {
		t.Errorf("expected collision-free dir with suffix -2:\ngot:  %q\nwant: %q", dir, expectedDir)
	}
}

func TestPublishToProjectStorage_MultiplePhases(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	phases := map[string]string{
		"requirements":   "# Requirements\n\nReq content.",
		"tech_design":    "# Technical Design\n\nDesign content.",
		"task_breakdown": "# Task Breakdown\n\nTasks content.",
	}

	for phaseID, content := range phases {
		a.publishToProjectStorage(phaseID, content)
	}

	// Verify all files exist with correct content
	dateDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01")
	expectedFiles := map[string]string{
		"01-requirements.md":     phases["requirements"],
		"02-technical-design.md": phases["tech_design"],
		"03-task-breakdown.md":   phases["task_breakdown"],
	}

	for fileName, expectedContent := range expectedFiles {
		filePath := filepath.Join(dateDir, fileName)
		data, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("expected file %s, got error: %v", filePath, err)
			continue
		}
		if string(data) != expectedContent {
			t.Errorf("content mismatch for %s:\ngot:  %q\nwant: %q", fileName, string(data), expectedContent)
		}
	}
}

func TestPublishToProjectStorage_UnknownPhaseID(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	content := "# Custom Phase\n\nSome custom content."
	a.publishToProjectStorage("my_custom_phase", content)

	// Unknown phase IDs get sanitized: my_custom_phase → my-custom-phase.md
	expectedPath := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01", "my-custom-phase.md")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedPath, err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch:\ngot:  %q\nwant: %q", string(data), content)
	}
}

func TestPublishToProjectStorage_ContentWithWhitespaceOnly(t *testing.T) {
	tmpDir := t.TempDir()
	startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
	a := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

	// Various whitespace-only strings should all be skipped
	whitespaceInputs := []string{"", " ", "\t", "\n", "  \n\t  \n  "}
	for _, ws := range whitespaceInputs {
		a.publishToProjectStorage("requirements", ws)
	}

	// No file should be created
	docsDir := filepath.Join(tmpDir, "docs")
	if _, err := os.Stat(docsDir); !os.IsNotExist(err) {
		t.Errorf("expected docs directory to not exist for whitespace-only content")
	}
}

func TestResolveProjectStorageDir_PathContainsKebabWorkflowType(t *testing.T) {
	tests := []struct {
		wfType   workflow.WorkflowType
		expected string
	}{
		{workflow.WorkflowCoding, "coding"},
		{workflow.WorkflowProductDesign, "product-design"},
		{workflow.WorkflowBusinessPlan, "business-plan"},
		{workflow.WorkflowPresentationDesign, "presentation-design"},
		{workflow.WorkflowOpsMaintenance, "ops-maintenance"},
		{workflow.WorkflowChangjiangScholarReview, "changjiang-scholar-review"},
	}

	for _, tt := range tests {
		t.Run(string(tt.wfType), func(t *testing.T) {
			tmpDir := t.TempDir()
			startDate := time.Date(2026, 1, 15, 10, 0, 0, 0, time.Local)
			a := newTestAdapter(tmpDir, tt.wfType, startDate)

			dir := a.resolveProjectStorageDir()
			if !strings.Contains(dir, tt.expected) {
				t.Errorf("expected dir to contain %q, got %q", tt.expected, dir)
			}
		})
	}
}

func TestCleanPersistedWorkflowDocs_PreservesProjectStorage(t *testing.T) {
	// Setup: create a temp directory as the project root.
	tmpDir := t.TempDir()

	// Create Internal_Storage files: {tmpDir}/.maclaw/workflow/test.md, design.md
	internalDir := filepath.Join(tmpDir, ".maclaw", "workflow")
	if err := os.MkdirAll(internalDir, 0755); err != nil {
		t.Fatalf("failed to create internal storage dir: %v", err)
	}
	internalFile1 := filepath.Join(internalDir, "test.md")
	internalFile2 := filepath.Join(internalDir, "design.md")
	if err := os.WriteFile(internalFile1, []byte("# Internal Test Doc"), 0644); err != nil {
		t.Fatalf("failed to write internal file 1: %v", err)
	}
	if err := os.WriteFile(internalFile2, []byte("# Internal Design Doc"), 0644); err != nil {
		t.Fatalf("failed to write internal file 2: %v", err)
	}

	// Create Project_Storage files: {tmpDir}/docs/workflow/coding/2026-05-01/01-requirements.md
	projectStorageDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01")
	if err := os.MkdirAll(projectStorageDir, 0755); err != nil {
		t.Fatalf("failed to create project storage dir: %v", err)
	}
	projectFile := filepath.Join(projectStorageDir, "01-requirements.md")
	projectFileContent := "# Requirements\n\nThis is the published requirements document."
	if err := os.WriteFile(projectFile, []byte(projectFileContent), 0644); err != nil {
		t.Fatalf("failed to write project storage file: %v", err)
	}

	// Create a GUIWorkflowAdapter with workingDir set to tmpDir.
	adapter := &GUIWorkflowAdapter{
		app:        &App{},
		workingDir: tmpDir,
	}

	// Call CleanPersistedWorkflowDocs.
	adapter.CleanPersistedWorkflowDocs()

	// Verify Internal_Storage files are removed.
	if _, err := os.Stat(internalFile1); !os.IsNotExist(err) {
		t.Errorf("Internal_Storage file %s should have been removed, but still exists", internalFile1)
	}
	if _, err := os.Stat(internalFile2); !os.IsNotExist(err) {
		t.Errorf("Internal_Storage file %s should have been removed, but still exists", internalFile2)
	}

	// Verify Project_Storage files still exist with original content.
	data, err := os.ReadFile(projectFile)
	if err != nil {
		t.Fatalf("Project_Storage file %s should still exist, but got error: %v", projectFile, err)
	}
	if string(data) != projectFileContent {
		t.Errorf("Project_Storage file content changed.\nGot:  %q\nWant: %q", string(data), projectFileContent)
	}

	// Also verify the Project_Storage directory structure is intact.
	entries, err := os.ReadDir(projectStorageDir)
	if err != nil {
		t.Fatalf("Project_Storage directory should still exist: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("Project_Storage directory should have exactly 1 file, got %d", len(entries))
	}
}

func TestCleanPersistedWorkflowDocs_RemovesSubdirectories(t *testing.T) {
	// Setup: create a temp directory as the project root.
	tmpDir := t.TempDir()

	// Create Internal_Storage with a workflow-ID subdirectory.
	wfSubDir := filepath.Join(tmpDir, ".maclaw", "workflow", "wf_abc123")
	if err := os.MkdirAll(wfSubDir, 0755); err != nil {
		t.Fatalf("failed to create workflow subdirectory: %v", err)
	}
	subFile := filepath.Join(wfSubDir, "01-requirements.md")
	if err := os.WriteFile(subFile, []byte("# Req"), 0644); err != nil {
		t.Fatalf("failed to write file in subdirectory: %v", err)
	}

	// Create Project_Storage files that must survive.
	projectStorageDir := filepath.Join(tmpDir, "docs", "workflow", "coding", "2026-05-01")
	if err := os.MkdirAll(projectStorageDir, 0755); err != nil {
		t.Fatalf("failed to create project storage dir: %v", err)
	}
	projectFile := filepath.Join(projectStorageDir, "02-technical-design.md")
	projectContent := "# Technical Design\n\nArchitecture overview."
	if err := os.WriteFile(projectFile, []byte(projectContent), 0644); err != nil {
		t.Fatalf("failed to write project storage file: %v", err)
	}

	adapter := &GUIWorkflowAdapter{
		app:        &App{},
		workingDir: tmpDir,
	}

	adapter.CleanPersistedWorkflowDocs()

	// Verify the workflow-ID subdirectory is removed.
	if _, err := os.Stat(wfSubDir); !os.IsNotExist(err) {
		t.Errorf("Internal_Storage subdirectory %s should have been removed", wfSubDir)
	}

	// Verify Project_Storage is untouched.
	data, err := os.ReadFile(projectFile)
	if err != nil {
		t.Fatalf("Project_Storage file should still exist: %v", err)
	}
	if string(data) != projectContent {
		t.Errorf("Project_Storage file content changed.\nGot:  %q\nWant: %q", string(data), projectContent)
	}
}

func TestCleanPersistedWorkflowDocs_EmptyWorkingDir_NoOp(t *testing.T) {
	// When workingDir is empty, CleanPersistedWorkflowDocs should be a no-op.
	// This test verifies it doesn't panic or error.
	adapter := &GUIWorkflowAdapter{
		app:        &App{},
		workingDir: "", // empty — workflowProjectPath will fall through to GetCurrentProjectPath
	}

	// Should not panic. The method will try GetCurrentProjectPath on a bare App,
	// which may return a home dir or empty. Either way, it should not crash.
	adapter.CleanPersistedWorkflowDocs()
}

func TestWriteWorkflowManifest(t *testing.T) {
	t.Run("completed_status_writes_valid_manifest", func(t *testing.T) {
		tmpDir := t.TempDir()
		startDate := time.Date(2026, 5, 1, 14, 30, 0, 0, time.Local)
		adapter := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

		phases := []ManifestPhaseEntry{
			{PhaseID: "requirements", FileName: "01-requirements.md", Title: "需求分析"},
			{PhaseID: "tech_design", FileName: "02-technical-design.md", Title: "技术设计"},
			{PhaseID: "task_breakdown", FileName: "03-task-breakdown.md", Title: "任务拆分"},
		}

		adapter.writeWorkflowManifest("completed", phases)

		// Read back the manifest file
		manifestPath := filepath.Join(tmpDir, "docs", "workflow", "coding",
			startDate.Format("2006-01-02"), "workflow-manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest file: %v", err)
		}

		var manifest WorkflowManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to unmarshal manifest: %v", err)
		}

		// Verify all fields
		if manifest.WorkflowType != "coding" {
			t.Errorf("WorkflowType = %q, want %q", manifest.WorkflowType, "coding")
		}
		if manifest.TemplateName != "coding" {
			t.Errorf("TemplateName = %q, want %q", manifest.TemplateName, "coding")
		}
		if manifest.Status != "completed" {
			t.Errorf("Status = %q, want %q", manifest.Status, "completed")
		}

		// Verify StartedAt is RFC3339 parseable
		if _, err := time.Parse(time.RFC3339, manifest.StartedAt); err != nil {
			t.Errorf("StartedAt %q is not valid RFC3339: %v", manifest.StartedAt, err)
		}
		// Verify CompletedAt is RFC3339 parseable
		if _, err := time.Parse(time.RFC3339, manifest.CompletedAt); err != nil {
			t.Errorf("CompletedAt %q is not valid RFC3339: %v", manifest.CompletedAt, err)
		}

		// Verify phases array matches input
		if len(manifest.Phases) != 3 {
			t.Fatalf("Phases length = %d, want 3", len(manifest.Phases))
		}
		for i, expected := range phases {
			got := manifest.Phases[i]
			if got.PhaseID != expected.PhaseID {
				t.Errorf("Phases[%d].PhaseID = %q, want %q", i, got.PhaseID, expected.PhaseID)
			}
			if got.FileName != expected.FileName {
				t.Errorf("Phases[%d].FileName = %q, want %q", i, got.FileName, expected.FileName)
			}
			if got.Title != expected.Title {
				t.Errorf("Phases[%d].Title = %q, want %q", i, got.Title, expected.Title)
			}
		}
	})

	t.Run("cancelled_status_with_partial_phases", func(t *testing.T) {
		tmpDir := t.TempDir()
		startDate := time.Date(2026, 6, 15, 9, 0, 0, 0, time.Local)
		adapter := newTestAdapter(tmpDir, workflow.WorkflowProductDesign, startDate)

		phases := []ManifestPhaseEntry{
			{PhaseID: "problem_discovery", FileName: "01-problem-discovery.md", Title: "问题发现"},
			{PhaseID: "solution_design", FileName: "02-solution-design.md", Title: "方案设计"},
		}

		adapter.writeWorkflowManifest("cancelled", phases)

		manifestPath := filepath.Join(tmpDir, "docs", "workflow", "product-design",
			startDate.Format("2006-01-02"), "workflow-manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest file: %v", err)
		}

		var manifest WorkflowManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to unmarshal manifest: %v", err)
		}

		if manifest.Status != "cancelled" {
			t.Errorf("Status = %q, want %q", manifest.Status, "cancelled")
		}
		if manifest.WorkflowType != "product_design" {
			t.Errorf("WorkflowType = %q, want %q", manifest.WorkflowType, "product_design")
		}

		if len(manifest.Phases) != 2 {
			t.Fatalf("Phases length = %d, want 2", len(manifest.Phases))
		}
		if manifest.Phases[0].PhaseID != "problem_discovery" {
			t.Errorf("Phases[0].PhaseID = %q, want %q", manifest.Phases[0].PhaseID, "problem_discovery")
		}
		if manifest.Phases[1].PhaseID != "solution_design" {
			t.Errorf("Phases[1].PhaseID = %q, want %q", manifest.Phases[1].PhaseID, "solution_design")
		}
	})

	t.Run("timestamps_are_RFC3339_format", func(t *testing.T) {
		tmpDir := t.TempDir()
		startDate := time.Date(2026, 3, 20, 10, 15, 30, 0, time.FixedZone("CST", 8*3600))
		adapter := newTestAdapter(tmpDir, workflow.WorkflowCoding, startDate)

		phases := []ManifestPhaseEntry{
			{PhaseID: "requirements", FileName: "01-requirements.md", Title: "需求"},
		}

		beforeWrite := time.Now()
		adapter.writeWorkflowManifest("completed", phases)
		afterWrite := time.Now()

		manifestPath := filepath.Join(tmpDir, "docs", "workflow", "coding",
			startDate.Format("2006-01-02"), "workflow-manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest file: %v", err)
		}

		var manifest WorkflowManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to unmarshal manifest: %v", err)
		}

		// Verify StartedAt matches the configured start date
		parsedStart, err := time.Parse(time.RFC3339, manifest.StartedAt)
		if err != nil {
			t.Fatalf("StartedAt %q failed RFC3339 parse: %v", manifest.StartedAt, err)
		}
		if !parsedStart.Equal(startDate) {
			t.Errorf("StartedAt parsed = %v, want %v", parsedStart, startDate)
		}

		// Verify CompletedAt is between beforeWrite and afterWrite
		parsedCompleted, err := time.Parse(time.RFC3339, manifest.CompletedAt)
		if err != nil {
			t.Fatalf("CompletedAt %q failed RFC3339 parse: %v", manifest.CompletedAt, err)
		}
		if parsedCompleted.Before(beforeWrite.Add(-time.Second)) || parsedCompleted.After(afterWrite.Add(time.Second)) {
			t.Errorf("CompletedAt %v not in expected range [%v, %v]", parsedCompleted, beforeWrite, afterWrite)
		}
	})

	t.Run("phases_array_matches_input_exactly", func(t *testing.T) {
		tmpDir := t.TempDir()
		startDate := time.Date(2026, 7, 4, 8, 0, 0, 0, time.Local)
		adapter := newTestAdapter(tmpDir, workflow.WorkflowInnovation, startDate)

		phases := []ManifestPhaseEntry{
			{PhaseID: "opportunity", FileName: "01-opportunity.md", Title: "机会发现"},
			{PhaseID: "ideation", FileName: "02-ideation.md", Title: "创意构思"},
			{PhaseID: "validation", FileName: "03-validation.md", Title: "验证"},
			{PhaseID: "roadmap", FileName: "04-roadmap.md", Title: "路线图"},
			{PhaseID: "action_plan", FileName: "05-action-plan.md", Title: "行动计划"},
		}

		adapter.writeWorkflowManifest("completed", phases)

		manifestPath := filepath.Join(tmpDir, "docs", "workflow", "innovation",
			startDate.Format("2006-01-02"), "workflow-manifest.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("failed to read manifest file: %v", err)
		}

		var manifest WorkflowManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("failed to unmarshal manifest: %v", err)
		}

		if len(manifest.Phases) != len(phases) {
			t.Fatalf("Phases length = %d, want %d", len(manifest.Phases), len(phases))
		}

		for i, expected := range phases {
			got := manifest.Phases[i]
			if got.PhaseID != expected.PhaseID || got.FileName != expected.FileName || got.Title != expected.Title {
				t.Errorf("Phases[%d] = {%q, %q, %q}, want {%q, %q, %q}",
					i, got.PhaseID, got.FileName, got.Title,
					expected.PhaseID, expected.FileName, expected.Title)
			}
		}
	})
}
