package tool

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func TestLocalBackgroundTaskManagerSubmitWithOwnerCarriesOwner(t *testing.T) {
	mgr := NewLocalBackgroundTaskManager(t.TempDir())
	command := "echo done"
	if runtime.GOOS == "windows" {
		command = "Write-Output done"
	}

	task, err := mgr.SubmitWithOwner(command, "", "command", "owner-a")
	if err != nil {
		t.Fatalf("SubmitWithOwner: %v", err)
	}
	status, err := mgr.Wait(context.Background(), task.TaskID, 5*time.Second, 10)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if status.OwnerID != "owner-a" {
		t.Fatalf("status OwnerID = %q, want owner-a", status.OwnerID)
	}
	listed := mgr.List()
	if len(listed) != 1 || listed[0].OwnerID != "owner-a" {
		t.Fatalf("List owner snapshot = %#v", listed)
	}
}

func TestLocalBackgroundTaskManagerOwnerAuthorization(t *testing.T) {
	mgr := NewLocalBackgroundTaskManager(t.TempDir())
	command := "echo done"
	if runtime.GOOS == "windows" {
		command = "Write-Output done"
	}
	task, err := mgr.SubmitWithOwner(command, "", "command", "owner-a")
	if err != nil {
		t.Fatalf("SubmitWithOwner: %v", err)
	}

	if err := mgr.AuthorizeTaskOwner(task.TaskID, "owner-a"); err != nil {
		t.Fatalf("same owner should be allowed: %v", err)
	}
	if err := mgr.AuthorizeTaskOwner(task.TaskID, "owner-b"); err == nil {
		t.Fatal("different owner should be rejected")
	}
	if _, err := mgr.CheckForOwner(task.TaskID, 10, "owner-b"); err == nil {
		t.Fatal("CheckForOwner should reject different owner")
	}
	if got := mgr.ListForOwner("owner-b"); len(got) != 0 {
		t.Fatalf("ListForOwner owner-b = %#v, want empty", got)
	}
	if got := mgr.ListForOwner("owner-a"); len(got) != 1 || got[0].TaskID != task.TaskID {
		t.Fatalf("ListForOwner owner-a = %#v", got)
	}
}
