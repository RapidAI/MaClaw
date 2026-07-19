package main

import (
	"path/filepath"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestInFlightLifecycleUsesProjectTabPathFromUserID(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)

	// A real App is required: SetOnce resolves the project path through
	// App.EffectiveWorkingDirForOwner, which maps a Project Tab owner ID back
	// to its encoded project path.
	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.memory = memory

	projectPath := `D:\tasks\managed-task-instance`
	userID := desktopUserID + ":" + projectPath

	lifecycle := h.newInFlightLifecycle(userID, "成都天气")
	lifecycle.SetOnce()

	task, gotProjectPath := memory.ConsumeInFlightTask(userID)
	if task != "成都天气" {
		t.Fatalf("task = %q, want 成都天气", task)
	}
	if gotProjectPath != projectPath {
		t.Fatalf("project path = %q, want %q", gotProjectPath, projectPath)
	}
}

func TestInFlightLifecycleDoesNotInventProjectPathForLocalUser(t *testing.T) {
	memory := agent.NewConversationMemory()
	t.Cleanup(memory.Stop)

	h, _ := setupWorkflowTestHandler(&mockLLMCallerGUI{})
	h.memory = memory

	// The local (non project-tab) session records the effective working
	// directory, and must never invent a project path from the Projects list.
	workDir := filepath.Join(t.TempDir(), "workspace")
	projectFromList := filepath.Join(t.TempDir(), "projects-entry")
	if err := h.app.SaveConfig(corelib.AppConfig{
		Projects:         []corelib.ProjectConfig{{Id: "p1", Name: "P", Path: projectFromList}},
		CurrentProject:   "p1",
		WorkingDirectory: workDir,
	}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	corelib.SetWorkspaceDir(workDir)

	lifecycle := h.newInFlightLifecycle(desktopUserID, "本地任务")
	lifecycle.SetOnce()

	_, gotProjectPath := memory.ConsumeInFlightTask(desktopUserID)
	want := normalizeProjectSessionPath(workDir)
	if gotProjectPath != want {
		t.Fatalf("local project path = %q, want effective working dir %q (not Projects-list path %q)", gotProjectPath, want, projectFromList)
	}
}
