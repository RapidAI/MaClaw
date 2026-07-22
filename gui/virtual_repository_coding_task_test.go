package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestVirtualRepositoryByIDForCodingTaskUsesIndexedLocalRoot(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	repo := &VirtualRepository{Name: "Workspace", RootPath: t.TempDir(), Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	got, err := app.virtualRepositoryByIDForCodingTask(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != repo.ID || got.RootPath != repo.RootPath {
		t.Fatalf("resolved repo=%#v, want id=%q root=%q", got, repo.ID, repo.RootPath)
	}
}

func TestStartVirtualRepositoryCodingTaskArmsLocalWorkspace(t *testing.T) {
	app := newProjectSearchTestApp(t)
	repo := &VirtualRepository{Name: "Workspace", RootPath: t.TempDir(), Nodes: []VirtualRepositoryNode{}}
	if err := writeVirtualRepository(repo); err != nil {
		t.Fatal(err)
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	launch, err := app.StartVirtualRepositoryCodingTask(repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if launch.AgentMode != "coding_dev" || launch.TaskTitle != "Workspace" || launch.ProjectPath == "" {
		t.Fatalf("launch=%#v", launch)
	}
	if !strings.HasPrefix(filepath.Clean(launch.ProjectPath), filepath.Clean(app.getMaclawBaseDir())) {
		t.Fatalf("task project path %q should be under app data", launch.ProjectPath)
	}
	status, err := app.EnsureCodingWorkbenchArmed(launch.ProjectPath)
	if err != nil {
		t.Fatal(err)
	}
	if status.Kind != "local" || !status.Armed {
		t.Fatalf("coding workbench status=%#v", status)
	}
}

func TestVirtualRepositoryByIDForCodingTaskRejectsMissingID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.virtualRepositoryByIDForCodingTask("missing"); err == nil {
		t.Fatal("missing recent repository should fail")
	}
}

func TestStartVirtualRepositoryCodingTaskDoesNotCreateRemoteTaskWithoutPassword(t *testing.T) {
	keyring.MockInit()
	app := newProjectSearchTestApp(t)
	repo := &VirtualRepository{
		ID:       "vrepo_remote_missing_password",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "example.invalid", Port: 22, User: "developer"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}

	if _, err := app.StartVirtualRepositoryCodingTask(repo.ID); err == nil {
		t.Fatal("remote launch without a saved SSH password should fail")
	}
	if tasks := app.ListTasks(10); len(tasks) != 0 {
		t.Fatalf("failed remote preflight created task records: %#v", tasks)
	}
}

func TestStartVirtualRepositoryCodingTaskDoesNotConnectWithoutTrustedHostKey(t *testing.T) {
	keyring.MockInit()
	app := newProjectSearchTestApp(t)
	repo := &VirtualRepository{
		ID:       "vrepo_remote_untrusted_host",
		Name:     "Remote workspace",
		RootPath: "/srv/workspace",
		Remote:   &VirtualRepositoryRemote{Host: "example.invalid", Port: 22, User: "developer"},
		Nodes:    []VirtualRepositoryNode{},
	}
	if err := app.updateVirtualRepositoryIndex(repo); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(virtualRepositorySSHKeyringService, repo.ID, "secret"); err != nil {
		t.Fatal(err)
	}

	_, err := app.StartVirtualRepositoryCodingTask(repo.ID)
	if err == nil || !strings.Contains(err.Error(), "host key is not trusted") {
		t.Fatalf("error = %v, want local host-key preflight failure", err)
	}
	if tasks := app.ListTasks(10); len(tasks) != 0 {
		t.Fatalf("failed remote preflight created task records: %#v", tasks)
	}
}
