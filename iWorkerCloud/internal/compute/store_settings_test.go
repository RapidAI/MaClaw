package compute

import (
	"context"
	"testing"
)

func TestGetSetting_NotFound(t *testing.T) {
	s := newTestStore(t)
	val, err := s.GetSetting(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if val != "" {
		t.Errorf("expected empty string, got %q", val)
	}
}

func TestSetAndGetSetting(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetSetting(ctx, "my_key", "my_value"); err != nil {
		t.Fatal(err)
	}
	val, err := s.GetSetting(ctx, "my_key")
	if err != nil {
		t.Fatal(err)
	}
	if val != "my_value" {
		t.Errorf("expected %q, got %q", "my_value", val)
	}
}

func TestSetSetting_Upsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetSetting(ctx, "k", "v1")
	s.SetSetting(ctx, "k", "v2")

	val, _ := s.GetSetting(ctx, "k")
	if val != "v2" {
		t.Errorf("expected %q after upsert, got %q", "v2", val)
	}
}

func TestComputePermission_DefaultFalse(t *testing.T) {
	s := newTestStore(t)
	perm, err := s.GetComputePermission(context.Background(), "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if perm {
		t.Error("expected default permission to be false")
	}
}

func TestSetAndGetComputePermission(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetComputePermission(ctx, "center-1", true); err != nil {
		t.Fatal(err)
	}
	perm, err := s.GetComputePermission(ctx, "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if !perm {
		t.Error("expected permission to be true")
	}

	// Revoke
	if err := s.SetComputePermission(ctx, "center-1", false); err != nil {
		t.Fatal(err)
	}
	perm, _ = s.GetComputePermission(ctx, "center-1")
	if perm {
		t.Error("expected permission to be false after revoke")
	}
}

func TestForceSync_DefaultFalse(t *testing.T) {
	s := newTestStore(t)
	fs, err := s.GetForceSync(context.Background(), "center-1")
	if err != nil {
		t.Fatal(err)
	}
	if fs {
		t.Error("expected default force_sync to be false")
	}
}

func TestSetAndClearForceSync(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetForceSync(ctx, "center-1", true); err != nil {
		t.Fatal(err)
	}
	fs, _ := s.GetForceSync(ctx, "center-1")
	if !fs {
		t.Error("expected force_sync to be true")
	}

	if err := s.ClearForceSync(ctx, "center-1"); err != nil {
		t.Fatal(err)
	}
	fs, _ = s.GetForceSync(ctx, "center-1")
	if fs {
		t.Error("expected force_sync to be false after clear")
	}
}

func TestPermission_IsolatedPerCenter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.SetComputePermission(ctx, "center-a", true)
	s.SetComputePermission(ctx, "center-b", false)

	pa, _ := s.GetComputePermission(ctx, "center-a")
	pb, _ := s.GetComputePermission(ctx, "center-b")

	if !pa {
		t.Error("center-a should have permission")
	}
	if pb {
		t.Error("center-b should not have permission")
	}
}
