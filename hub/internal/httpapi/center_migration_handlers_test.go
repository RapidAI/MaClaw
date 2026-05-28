package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func TestCenterUserMigrationImportRegeneratesLocalUserIdentity(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	provider, err := sqlite.NewProvider(sqlite.Config{DSN: filepath.Join(t.TempDir(), "hub-center-migration.db")})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	st := sqlite.NewStore(provider)
	secret := "hub-secret"
	if err := st.System.Set(ctx, "center_registration", `{"registered":true,"hub_id":"hub-target","hub_secret":"`+secret+`"}`); err != nil {
		t.Fatalf("seed registration: %v", err)
	}
	if err := st.Users.Create(ctx, &store.User{ID: "source-user", TenantID: "tenant_existing", Email: "other@example.com", SN: "SN-SOURCE", Status: "active", EnrollmentStatus: "approved", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed existing user: %v", err)
	}
	if err := st.Machines.Create(ctx, &store.Machine{ID: "machine-source", TenantID: "tenant_existing", UserID: "source-user", ClientID: "client-1", Name: "Existing", Platform: "linux", MachineTokenHash: "hash", Status: "offline", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("seed existing machine: %v", err)
	}

	identity := auth.NewIdentityService(st.Users, st.Enrollments, st.EmailBlocks, st.Machines, st.ViewerTokens, st.LoginTokens, st.System, nil, "open", true, nil, "http://127.0.0.1:8080")
	centerSvc := center.NewService(config.Default(), st.System)
	deviceSvc := device.NewService(st.Machines, device.NewRuntime())
	payload := map[string]any{
		"hub_secret_hash": sha256Hex(secret),
		"tenant_id":       "tenant_target",
		"users": []any{map[string]any{
			"tenant_id": "tenant_source",
			"user":      map[string]any{"ID": "source-user", "TenantID": "tenant_source", "Email": "migrated@example.com", "SN": "SN-SOURCE", "Status": "active", "EnrollmentStatus": "approved", "CreatedAt": now.Format(time.RFC3339), "UpdatedAt": now.Format(time.RFC3339)},
			"machines":  []any{map[string]any{"ID": "machine-source", "TenantID": "tenant_source", "UserID": "source-user", "ClientID": "client-1", "Name": "Migrated", "Platform": "linux", "MachineTokenHash": "hash2", "Status": "online", "CreatedAt": now.Format(time.RFC3339), "UpdatedAt": now.Format(time.RFC3339)}},
		}},
	}
	body, _ := json.Marshal(payload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/center/user-migration/import", bytes.NewReader(body))
	CenterUserMigrationImportHandler(centerSvc, identity, deviceSvc).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}

	imported, err := st.Users.GetByTenantEmail(ctx, "tenant_target", "migrated@example.com")
	if err != nil {
		t.Fatalf("GetByTenantEmail: %v", err)
	}
	if imported == nil || imported.ID == "source-user" || imported.SN == "SN-SOURCE" || !strings.HasPrefix(imported.ID, "u_mig_") || !strings.HasPrefix(imported.SN, "SN-MIG-") {
		t.Fatalf("expected regenerated target user identity, got %+v", imported)
	}
	machines, err := st.Machines.ListByUserID(ctx, imported.ID)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(machines) != 1 || machines[0].TenantID != "tenant_target" || machines[0].ID == "machine-source" || machines[0].Status != "offline" {
		t.Fatalf("expected imported machine remapped to target user/tenant, got %+v", machines)
	}
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
