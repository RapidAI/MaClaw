package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
)

func TestAppendEnterpriseKnowledgeAutoRecall_NoopWhenEmpty(t *testing.T) {
	// Hosts no longer dump enterprise hits into the system prompt.
	// Search stays on knowledge.read.local tools (SearchActiveFromDataDir).
	hits, err := enterpriseknowledge.SearchActiveFromDataDir(context.Background(), t.TempDir(), "hello policy", "")
	if err != nil {
		t.Fatalf("SearchActiveFromDataDir empty dir: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no hits from empty dir, got %+v", hits)
	}
}

func TestEnterpriseKnowledgeAutoRecallHeaderConstant(t *testing.T) {
	if !strings.Contains(agent.EnterpriseKnowledgeAutoRecallHeader, "企业知识库") {
		t.Fatalf("header missing: %q", agent.EnterpriseKnowledgeAutoRecallHeader)
	}
	if !strings.Contains(agent.KnowledgeAutoRecallHeader, "企业知识库") {
		t.Fatalf("personal header priority list should mention enterprise KB")
	}
}

func TestEnterpriseKnowledgeUserSyncToggle(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	_ = os.MkdirAll(app.GetDataDir(), 0o755)
	t.Cleanup(func() {
		if app.enterpriseSync != nil {
			app.enterpriseSync.Stop()
			app.enterpriseSync = nil
		}
		if app.enterpriseClient != nil {
			_ = app.enterpriseClient.Close()
			app.enterpriseClient = nil
		}
	})

	seed, err := enterpriseknowledge.Open(app.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := enterpriseknowledge.SeedLibraryForTest(seed, "lib_toggle", "Toggle Lib", "active", true); err != nil {
		t.Fatal(err)
	}
	seed.Close()

	libs, err := app.EnterpriseKnowledgeListLibraries()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(libs) != 1 {
		t.Fatalf("want 1 lib, got %d", len(libs))
	}
	if !libs[0].UserSyncEnabled {
		t.Fatal("expected user_sync_enabled true by default")
	}
	if !libs[0].HubSyncEnabled {
		t.Fatal("expected hub_sync_enabled true for active")
	}

	if err := app.EnterpriseKnowledgeSetLibraryUserSync("lib_toggle", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	libs, err = app.EnterpriseKnowledgeListLibraries()
	if err != nil {
		t.Fatalf("list after disable: %v", err)
	}
	if len(libs) != 1 || libs[0].UserSyncEnabled {
		t.Fatalf("expected user_sync_enabled false, got %+v", libs)
	}

	if err := app.EnterpriseKnowledgeSetLibraryUserSync("lib_toggle", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	libs, _ = app.EnterpriseKnowledgeListLibraries()
	if len(libs) != 1 || !libs[0].UserSyncEnabled {
		t.Fatalf("expected user_sync_enabled true after re-enable, got %+v", libs)
	}

	if err := app.EnterpriseKnowledgeSetLibraryUserSync("missing_lib", false); err == nil {
		t.Fatal("expected error for unknown library")
	}
}

func TestEnterprisePurgeWhileSyncRunning(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	_ = os.MkdirAll(app.GetDataDir(), 0o755)
	seed, err := enterpriseknowledge.Open(app.GetDataDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = seed.Close() })
	if err := enterpriseknowledge.SeedLibraryForTest(seed, "lib_purge", "Purge Lib", "active", true); err != nil {
		t.Fatal(err)
	}
	app.enterpriseClient = seed

	if err := app.EnterprisePurgeRevokedLibrary(""); err == nil {
		t.Fatal("expected library_id required")
	}

	// Block Hub auth so RunOnce stays in running state.
	releaseAuth := make(chan struct{})
	ag := enterpriseknowledge.NewSyncAgent(seed, func() (string, string, error) {
		<-releaseAuth
		return "", "", nil
	}, "test-purge")
	app.enterpriseSync = ag
	go func() { _ = ag.RunOnce(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for !ag.Status().Running {
		if time.Now().After(deadline) {
			close(releaseAuth)
			t.Fatal("sync agent did not enter running state")
		}
		time.Sleep(5 * time.Millisecond)
	}
	err = app.EnterprisePurgeRevokedLibrary("lib_purge")
	close(releaseAuth)
	if !errors.Is(err, enterpriseknowledge.ErrSyncInProgress) {
		t.Fatalf("expected ErrSyncInProgress while sync running, got %v", err)
	}

	// Wait for RunOnce to finish, then purge succeeds.
	deadline = time.Now().Add(2 * time.Second)
	for ag.Status().Running {
		if time.Now().After(deadline) {
			t.Fatal("sync agent stuck running")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := app.EnterprisePurgeRevokedLibrary("lib_purge"); err != nil {
		t.Fatalf("purge when idle: %v", err)
	}
	libs, err := app.EnterpriseKnowledgeListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 0 {
		t.Fatalf("want empty after purge, got %+v", libs)
	}
}

func TestEnterpriseSyncAgentPause(t *testing.T) {
	dir := t.TempDir()
	c, err := enterpriseknowledge.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ag := enterpriseknowledge.NewSyncAgent(c, func() (string, string, error) {
		return "", "", nil
	}, "test-device")
	ag.SetPaused(true)
	st := ag.Status()
	if !st.Paused {
		t.Fatal("expected paused")
	}
}
