package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/database"
)

func TestFormatDatabaseProfileSummary(t *testing.T) {
	got := formatDatabaseProfileSummary("crm", "postgres", false, true, false, "", "", "")
	if got != "crm:postgres(ro)" {
		t.Fatalf("got %q", got)
	}
	got = formatDatabaseProfileSummary("crm", "mysql", true, false, true, "replica.internal", "ssh-1", "ssh-replica")
	if got != "crm:mysql(rw,off,replica,ssh,replica-ssh)" {
		t.Fatalf("got %q", got)
	}
	if formatDatabaseProfileSummary("", "mysql", false, true, false, "", "", "") != "" {
		t.Fatal("empty id must be omitted")
	}
}

func TestDatabaseProfilesConfigFieldIncludesReplicaFlag(t *testing.T) {
	var get func(*corelib.AppConfig) string
	for _, field := range allConfigFields {
		if field.Key == "database_profiles" {
			get = field.Get
			break
		}
	}
	if get == nil {
		t.Fatal("database_profiles field missing")
	}
	got := get(&corelib.AppConfig{DatabaseProfiles: []database.Profile{
		{ID: "crm", Type: database.SourcePostgres, ReadOnly: true, ReplicaHost: "10.0.0.2"},
	}})
	if !strings.Contains(got, "crm:postgres(ro,replica)") {
		t.Fatalf("summary = %q", got)
	}
}
