package cloudworkspace

import (
	"context"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

func TestTenantSettingsStorageKey(t *testing.T) {
	if got := tenantSettingsStorageKey(""); got != SettingsKey {
		t.Fatalf("empty tenant key=%q", got)
	}
	if got := tenantSettingsStorageKey(store.DefaultTenantID); got != "cloud_workspace" {
		t.Fatalf("default tenant key=%q", got)
	}
	if got := tenantSettingsStorageKey("tenant_a"); got != "tenant:tenant_a:cloud_workspace" {
		t.Fatalf("named tenant key=%q", got)
	}
}

func TestClampQuota(t *testing.T) {
	if got := clampQuota(0); got != 1 {
		t.Fatalf("0→%d want 1", got)
	}
	if got := clampQuota(99); got != 10 {
		t.Fatalf("99→%d want 10", got)
	}
	if got := clampQuota(5); got != 5 {
		t.Fatalf("5→%d want 5", got)
	}
}

func TestPrepareForWriteClampsAndRejectsEmptyDepartments(t *testing.T) {
	got, err := prepareForWrite(Settings{Mode: ModeAllUsers, Quota: 0})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quota != 1 {
		t.Fatalf("quota 0 clamped to %d want 1", got.Quota)
	}
	if got.Mode != ModeAllUsers {
		t.Fatalf("mode=%q", got.Mode)
	}
	if got.MaxWorkspaceBytes != defaultMaxWorkspaceBytes {
		t.Fatalf("max_workspace_bytes=%d", got.MaxWorkspaceBytes)
	}
	if got.TenantMaxTotalBytes != defaultTenantMaxTotalBytes {
		t.Fatalf("tenant_max_total_bytes=%d", got.TenantMaxTotalBytes)
	}
	if got.UpdatedAt == "" {
		t.Fatal("updated_at should be set")
	}

	got, err = prepareForWrite(Settings{Mode: ModeOff, Quota: 99})
	if err != nil {
		t.Fatal(err)
	}
	if got.Quota != 10 {
		t.Fatalf("quota 99 clamped to %d want 10", got.Quota)
	}
	if got.Mode != ModeOff {
		t.Fatal("mode off must keep quota (not zero it)")
	}

	_, err = prepareForWrite(Settings{Mode: ModeDepartments, DepartmentIDs: []string{}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("empty departments err=%v", err)
	}
	_, err = prepareForWrite(Settings{Mode: "nope"})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid mode err=%v", err)
	}
}

func TestLoadAndSaveTenantSettings(t *testing.T) {
	svc := &Service{System: memorySettings{}}
	got := svc.LoadTenantSettings(context.Background(), "t1")
	if got.Mode != ModeOff || got.Quota != 5 {
		t.Fatalf("defaults=%+v", got)
	}
	saved, err := svc.SaveTenantSettings(context.Background(), "t1", Settings{
		Mode:          ModeAllUsers,
		Quota:         7,
		DepartmentIDs: []string{" eng ", "eng", ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.Quota != 7 || saved.Mode != ModeAllUsers {
		t.Fatalf("saved=%+v", saved)
	}
	if len(saved.DepartmentIDs) != 1 || saved.DepartmentIDs[0] != "eng" {
		t.Fatalf("department_ids=%v", saved.DepartmentIDs)
	}
	loaded := svc.LoadTenantSettings(context.Background(), "t1")
	if loaded.Mode != ModeAllUsers || loaded.Quota != 7 {
		t.Fatalf("loaded=%+v", loaded)
	}
	if _, err := svc.SaveTenantSettings(context.Background(), "", Settings{Mode: ModeAllUsers, Quota: 3}); err != nil {
		t.Fatal(err)
	}
	loadedDefault := svc.LoadTenantSettings(context.Background(), store.DefaultTenantID)
	if loadedDefault.Mode != ModeAllUsers || loadedDefault.Quota != 3 {
		t.Fatalf("empty tenant id should persist as default: %+v", loadedDefault)
	}
	if loaded.MaxWorkspaceBytes != defaultMaxWorkspaceBytes || loaded.TenantMaxTotalBytes != defaultTenantMaxTotalBytes {
		t.Fatalf("byte defaults loaded=%+v", loaded)
	}
}

func TestFillSettingsDefaultsDoesNotZeroQuotaWhenOff(t *testing.T) {
	got := fillSettingsDefaults(Settings{Mode: ModeOff, Quota: 8, MaxWorkspaceBytes: defaultMaxWorkspaceBytes, TenantMaxTotalBytes: defaultTenantMaxTotalBytes})
	if got.Quota != 8 {
		t.Fatalf("quota=%d want 8", got.Quota)
	}
}
