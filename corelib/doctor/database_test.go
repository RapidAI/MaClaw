package doctor

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/database"
)

func TestDatabaseChecksDoesNotExposeSecrets(t *testing.T) {
	base := t.TempDir()
	checks := DatabaseChecks(corelib.AppConfig{DatabaseProfiles: []database.Profile{
		{ID: "p", Type: database.SourcePostgres, Host: "db", Database: "app", SecretRef: "keyring://prod/password", DSN: "server=db;password=top-secret"},
	}}, base)
	for _, check := range checks {
		if strings.Contains(strings.ToLower(check.Message), "top-secret") || strings.Contains(strings.ToLower(check.Message), "keyring://") || strings.Contains(strings.ToLower(check.Message), "password=") {
			t.Fatalf("database doctor leaked secret material: %#v", check)
		}
	}
}

func TestDatabaseChecksReportsReplicaWithoutLeakingSessionID(t *testing.T) {
	checks := DatabaseChecks(corelib.AppConfig{DatabaseProfiles: []database.Profile{
		{ID: "crm", Type: database.SourcePostgres, Host: "db", Database: "app", ReplicaHost: "replica", SSHSessionID: "ssh-secret-session", ReplicaSSHSessionID: "ssh-replica-secret"},
	}}, t.TempDir())
	found := false
	for _, check := range checks {
		if check.ID != "database.profile.crm" {
			continue
		}
		found = true
		blob := strings.ToLower(fmt.Sprintf("%v %#v", check.Message, check.Detail))
		if strings.Contains(blob, "ssh-secret-session") || strings.Contains(blob, "ssh-replica-secret") {
			t.Fatalf("doctor leaked ssh session id: %#v", check)
		}
		if check.Detail["has_replica"] != true || check.Detail["has_ssh_tunnel"] != true || check.Detail["has_replica_ssh_tunnel"] != true {
			t.Fatalf("expected replica/ssh flags, got %#v", check.Detail)
		}
	}
	if !found {
		t.Fatalf("profile check missing: %#v", checks)
	}
}

func TestDatabaseChecksReportsMissingSpreadsheetFile(t *testing.T) {
	checks := DatabaseChecks(corelib.AppConfig{DatabaseProfiles: []database.Profile{{ID: "sheet", Type: database.SourceExcel, FilePath: "missing.xlsx"}}}, filepath.Dir(t.TempDir()))
	found := false
	for _, check := range checks {
		if check.ID == "database.profile.sheet" && check.Status == StatusFail {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing spreadsheet file was not reported: %#v", checks)
	}
}
