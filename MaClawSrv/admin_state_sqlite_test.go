package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestAdminSaveLeavesUnreadLegacyUsers(t *testing.T) {
	dataRoot := t.TempDir()
	stateDir := filepath.Join(dataRoot, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`[{"id":"u1","username":"ada","role":"owner","status":"active"}]`)
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveAdminUsers(dataRoot, []adminUserRecord{{ID: "u2", Username: "new", Role: "owner", Status: "active"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "admin_users.json")); err != nil {
		t.Fatalf("save removed unread legacy users: %v", err)
	}
	loaded, err := loadAdminUsers(dataRoot)
	if err != nil || len(loaded) != 1 || loaded[0].Username != "ada" || loaded[0].ID != "u1" {
		t.Fatalf("import after unread save = %+v %v", loaded, err)
	}
}

func TestAdminStateSQLiteImportsLegacyAndKeepsRows(t *testing.T) {
	dataRoot := t.TempDir()
	stateDir := filepath.Join(dataRoot, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	users := []adminUserRecord{{
		ID: "u1", Username: "ada", Role: "owner", Status: "active",
		CreatedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}}
	raw, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAdminUsers(dataRoot)
	if err != nil || len(loaded) != 1 || loaded[0].Username != "ada" || loaded[0].ID != "u1" {
		t.Fatalf("import users=%+v err=%v", loaded, err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "admin_users.json")); !os.IsNotExist(err) {
		t.Fatalf("legacy users json should be removed, stat err=%v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept, err := loadAdminUsers(dataRoot)
	if err != nil || len(kept) != 1 || kept[0].ID != "u1" {
		t.Fatalf("missing-import marker let a stale file replace rows: %+v %v", kept, err)
	}

	db, err := sql.Open("sqlite", adminStateDBPath(dataRoot))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM admin_meta WHERE key = 'users'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	kept, err = loadAdminUsers(dataRoot)
	if err != nil || len(kept) != 1 || kept[0].Username != "ada" {
		t.Fatalf("corrupt json wiped rows: %+v %v", kept, err)
	}

	if err := saveAdminUsers(dataRoot, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "admin_users.json")); !os.IsNotExist(err) {
		t.Fatalf("save left corrupt json in place: %v", err)
	}
	db, err = sql.Open("sqlite", adminStateDBPath(dataRoot))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM admin_users`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM admin_meta WHERE key = 'users'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAdminUsers(dataRoot); err == nil {
		t.Fatal("corrupt json with an empty table should still return the parse error")
	}

	strict := false
	cfg := adminRuntimeConfig{SandboxMode: "enforce", SandboxStrict: &strict, UpdatedBy: "ada", Reason: "test"}
	if err := saveAdminRuntimeConfig(dataRoot, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := loadAdminRuntimeConfig(dataRoot)
	if err != nil || got.SandboxMode != "enforce" || got.SandboxStrict == nil || *got.SandboxStrict {
		t.Fatalf("runtime config = %+v err=%v", got, err)
	}
	draft := adminServiceConfigDraft{Values: map[string]any{"sandbox_report_retention": float64(12)}, UpdatedBy: "ada"}
	if err := saveAdminServiceConfigDraft(dataRoot, draft); err != nil {
		t.Fatal(err)
	}
	loadedDraft, err := loadAdminServiceConfigDraft(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	n, ok := intAny(loadedDraft.Values["sandbox_report_retention"])
	if !ok || n != 12 {
		t.Fatalf("draft number = %#v", loadedDraft.Values["sandbox_report_retention"])
	}
}

func TestAdminDuplicateIDsKeepLast(t *testing.T) {
	dataRoot := t.TempDir()
	stateDir := filepath.Join(dataRoot, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	users := []adminUserRecord{
		{ID: "u1", Username: "first"},
		{ID: "u1", Username: "second"},
		{ID: "", Username: "empty"},
		{ID: "~1", Username: "tilde"},
		{ID: "u2", Username: "other"},
	}
	raw, err := json.Marshal(users)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_users.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAdminUsers(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 4 || loaded[0].ID != "u1" || loaded[0].Username != "second" || loaded[1].Username != "empty" || loaded[2].Username != "tilde" || loaded[3].ID != "u2" {
		t.Fatalf("users = %+v", loaded)
	}

	sessions := []adminSessionRecord{
		{ID: "s1", TokenHash: "old", Username: "first"},
		{ID: "s2", TokenHash: "keep", Username: "other"},
		{ID: "s1", TokenHash: "new", Username: "second"},
	}
	sessionRaw, err := json.Marshal(sessions)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin_sessions.json"), sessionRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	loadedSessions, err := loadAdminSessions(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedSessions) != 2 || loadedSessions[0].ID != "s2" || loadedSessions[1].ID != "s1" || loadedSessions[1].TokenHash != "new" {
		t.Fatalf("sessions = %+v", loadedSessions)
	}
}

func TestAdminDuplicateSessionKeepsLaterPlace(t *testing.T) {
	dataRoot := t.TempDir()
	now := time.Now().UTC()
	sessions := make([]adminSessionRecord, 0, maxAdminSessionsPerUser+2)
	sessions = append(sessions, adminSessionRecord{
		ID: "current", UserID: "u", TokenHash: "old", ExpiresAt: now.Add(time.Hour),
	})
	for i := 0; i < maxAdminSessionsPerUser; i++ {
		sessions = append(sessions, adminSessionRecord{
			ID: "s" + strconv.Itoa(i), UserID: "u", TokenHash: "x", ExpiresAt: now.Add(time.Hour),
		})
	}
	sessions = append(sessions, adminSessionRecord{
		ID: "current", UserID: "u", TokenHash: "new", ExpiresAt: now.Add(2 * time.Hour),
	})
	if err := saveAdminSessions(dataRoot, sessions); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadAdminSessions(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	pruned := pruneAdminSessions(loaded, "u", now)
	for _, session := range pruned {
		if session.ID == "current" {
			if session.TokenHash != "new" {
				t.Fatalf("session = %+v", session)
			}
			return
		}
	}
	t.Fatalf("refreshed session was pruned: %+v", pruned)
}
