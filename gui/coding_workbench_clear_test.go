package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodingRequestIsPureWorkspaceClear(t *testing.T) {
	for _, text := range []string{
		"清空当前目录",
		"请清空当前目录",
		"帮我把当前目录清空",
		"clear the current directory",
		"please wipe the workspace",
	} {
		if !codingRequestIsPureWorkspaceClear(text) {
			t.Fatalf("expected pure workspace-clear: %q", text)
		}
	}
	for _, text := range []string{
		"清空当前目录然后写一个 hello world",
		"clear the current directory and add tests",
		"怎么清空当前目录",
		"fix the login bug",
	} {
		if codingRequestIsPureWorkspaceClear(text) {
			t.Fatalf("did not expect pure workspace-clear: %q", text)
		}
	}
}

func TestCodingWorkspaceClearRejectedRoots(t *testing.T) {
	if runtime.GOOS == "windows" {
		if codingWorkspaceClearRejected(`C:\`) == "" {
			t.Fatal("drive root must be rejected")
		}
		if codingWorkspaceClearRejected(`C:\Windows`) == "" {
			t.Fatal("C:\\Windows must be rejected")
		}
	} else {
		if codingWorkspaceClearRejected("/") == "" {
			t.Fatal("filesystem root must be rejected")
		}
		if codingWorkspaceClearRejected("/etc") == "" {
			t.Fatal("/etc must be rejected")
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if codingWorkspaceClearRejected(home) == "" {
			t.Fatal("user home must be rejected")
		}
	}
	if got := codingWorkspaceClearRejected(t.TempDir()); got != "" {
		t.Fatalf("temp workbench path should be allowed, got %q", got)
	}
	if codingRemoteWorkspaceClearRejected("/") == "" || codingRemoteWorkspaceClearRejected(".") == "" || codingRemoteWorkspaceClearRejected("/etc") == "" {
		t.Fatal("remote root/relative/system paths must be rejected")
	}
	if got := codingRemoteWorkspaceClearRejected("/srv/app"); got != "" {
		t.Fatalf("absolute remote project should be allowed, got %q", got)
	}
}

func TestClearLocalCodingWorkspaceContentsKeepsRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.cpp"), []byte("int main(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, failed, err := clearLocalCodingWorkspaceContents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatalf("failed=%v", failed)
	}
	if len(removed) != 2 {
		t.Fatalf("removed=%v", removed)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory should be empty, got %d entries", len(entries))
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("root directory must remain: %v", err)
	}
}

func TestTryHostClearCodingWorkspaceFullControl(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(userID, "full", "local", dir)

	if resp := h.tryHostClearCodingWorkspace(userID, "fix the login bug", dir, nil, nil, nil, nil); resp != nil {
		t.Fatalf("non-clear request must stay on the agent path, got %#v", resp)
	}
	if resp := h.tryHostClearCodingWorkspace(userID, "清空当前目录然后写 hello", dir, nil, nil, nil, nil); resp != nil {
		t.Fatalf("mixed request must stay on the agent path, got %#v", resp)
	}

	resp := h.tryHostClearCodingWorkspace(userID, "清空当前目录", dir, nil, nil, nil, nil)
	if resp == nil || !strings.Contains(resp.Text, "已清空") || !strings.Contains(resp.Text, "hello.exe") {
		t.Fatalf("full control should host-clear, got %#v", resp)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("full control clear left %d entries", len(entries))
	}
}

func TestTryHostClearCodingWorkspaceRequestModeBlocksWithoutApproval(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(userID, "request", "local", dir)

	denied := h.approveCodingWorkspaceClear(userID, dir, nil, nil)
	if denied == "" {
		t.Fatal("request mode without an approval channel must keep the clear blocked")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("blocked clear must not delete files: %v", err)
	}
}

func TestTryHostClearCodingWorkspaceStripsApproveMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &IMMessageHandler{}
	userID := stickyTestUserID(t)
	h.setStickyCodingSessionPermissionMode(userID, "full", "local", dir)
	resp := h.tryHostClearCodingWorkspace(userID, codingPlanApproveExecuteMarker+" 清空当前目录", dir, nil, nil, nil, nil)
	if resp == nil || !strings.Contains(resp.Text, "已清空") {
		t.Fatalf("approve-prefixed wipe should host-clear, got %#v", resp)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("approve-prefixed wipe left %d entries", len(entries))
	}
}
