package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/enterpriseknowledge"
)

func TestEnterpriseTenantProgressEmptyService(t *testing.T) {
	c := &enterpriseSyncCoordinator{stopCh: make(chan struct{})}
	_, err := c.TenantProgress(context.Background(), "", false)
	if err == nil {
		t.Fatal("expected error without svc")
	}
}

func TestEnterprisePurgeLibraryRequiresID(t *testing.T) {
	c := &enterpriseSyncCoordinator{stopCh: make(chan struct{}), svc: nil}
	if err := c.PurgeLibrary(agentservice.Principal{TenantID: "t", UserID: "u"}, ""); err == nil {
		t.Fatal("expected library_id required")
	}
}

func TestEnterprisePurgeHTTPStatus(t *testing.T) {
	if got := enterprisePurgeHTTPStatus(fmt.Errorf("library not found: x")); got != 404 {
		t.Fatalf("not found status: %d", got)
	}
	if got := enterprisePurgeHTTPStatus(fmt.Errorf("library_id required")); got != 400 {
		t.Fatalf("required status: %d", got)
	}
	if got := enterprisePurgeHTTPStatus(fmt.Errorf("other")); got != 400 {
		t.Fatalf("other status: %d", got)
	}
}

func TestEnterpriseSyncCoordinatorStatusDisabled(t *testing.T) {
	c := &enterpriseSyncCoordinator{disabled: true, deviceID: "test", stopCh: make(chan struct{})}
	st := c.Status()
	if st.Enabled {
		t.Fatal("expected disabled")
	}
	if st.DeviceID != "test" {
		t.Fatalf("device id: %s", st.DeviceID)
	}
	_, err := c.SyncAll(context.Background())
	if err == nil {
		t.Fatal("expected error when disabled")
	}
}

func TestEnterpriseSyncCoordinatorListAndUserSync(t *testing.T) {
	// Use a lightweight coordinator with a real service file layout is heavy;
	// exercise OpenMetaOnly seed path via package helpers and coordinator methods
	// that only need UserDataRoot — skip full Service by testing SetUserSync through package.
	dir := t.TempDir()
	client, err := enterpriseknowledge.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := enterpriseknowledge.SeedLibraryForTest(client, "lib1", "Lib One", "active", true); err != nil {
		t.Fatal(err)
	}
	if err := client.SetUserSync("lib1", false); err != nil {
		t.Fatal(err)
	}
	libs, err := client.ListLibraries()
	if err != nil {
		t.Fatal(err)
	}
	if len(libs) != 1 || libs[0].UserSyncEnabled {
		t.Fatalf("toggle failed: %+v", libs)
	}
	client.Close()

	// Coordinator with nil svc should error cleanly.
	c := &enterpriseSyncCoordinator{stopCh: make(chan struct{})}
	if _, err := c.ListLibraries(agentservice.Principal{TenantID: "t", UserID: "u"}); err == nil {
		t.Fatal("expected error without svc")
	}
}
