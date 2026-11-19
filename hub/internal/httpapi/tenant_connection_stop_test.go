package httpapi

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type recordingTenantConnectionStopper struct {
	tenantID string
}

func (s *recordingTenantConnectionStopper) StopTenantConnections(_ context.Context, tenantID string) {
	s.tenantID = tenantID
}

type recordingUserConnectionStopper struct {
	tenantID string
	userID   string
}

func (s *recordingUserConnectionStopper) StopUserConnections(_ context.Context, tenantID, userID string) {
	s.tenantID = tenantID
	s.userID = userID
}

func TestUserPurgerClosesUserConnections(t *testing.T) {
	stopper := &recordingUserConnectionStopper{}
	purger := &UserDataPurger{Connections: stopper}
	_, err := purger.PurgeAll(context.Background(), &store.User{ID: "user-1", TenantID: "tenant-a", Email: "user@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if stopper.tenantID != "tenant-a" || stopper.userID != "user-1" {
		t.Fatalf("stopper received %q/%q, want tenant-a/user-1", stopper.tenantID, stopper.userID)
	}
}

func TestStopTenantConnectionsInvokesStopper(t *testing.T) {
	stopper := &recordingTenantConnectionStopper{}
	stopTenantConnections(context.Background(), "tenant-a", []any{stopper})
	if stopper.tenantID != "tenant-a" {
		t.Fatalf("stopper tenant = %q, want tenant-a", stopper.tenantID)
	}
}
