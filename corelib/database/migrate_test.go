package database

import (
	"strings"
	"testing"
)

func TestMigratePlaintextSecretsWritesAndClears(t *testing.T) {
	out, err := MigratePlaintextSecrets([]Profile{
		{ID: "crm", Type: SourceMySQL, Host: "127.0.0.1", Database: "d", Password: "secret"},
	}, func(id, secret string) (string, error) {
		if id != "crm" || secret != "secret" {
			t.Fatalf("writer got %s %s", id, secret)
		}
		return "keyring://maclaw/database/crm", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Password != "" || out[0].SecretRef != "keyring://maclaw/database/crm" {
		t.Fatalf("%+v", out[0])
	}
}

func TestMigratePlaintextSecretsFailClosed(t *testing.T) {
	_, err := MigratePlaintextSecrets([]Profile{{ID: "crm", Password: "x"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "no secret writer") {
		t.Fatalf("got %v", err)
	}
}

func TestPlaintextSecretError(t *testing.T) {
	if err := PlaintextSecretError([]Profile{{ID: "p", Password: "x"}}); err == nil {
		t.Fatal("expected error")
	}
	if err := PlaintextSecretError([]Profile{{ID: "p", SecretRef: "keyring://x"}}); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateProfilesFailClosedOnPlaintext(t *testing.T) {
	m := NewManager([]Profile{{ID: "ok", Type: SourceExcel, FilePath: "a.xlsx"}}, nil)
	m.UpdateProfiles([]Profile{{ID: "bad", Type: SourceExcel, FilePath: "a.xlsx", Password: "x"}})
	if m.ToolEnabled() {
		t.Fatal("plaintext password must disable the tool")
	}
	if got := HandleTool(WithRequestScope(t.Context(), RequestScope{OwnerID: "o", SessionID: "s"}), m, map[string]interface{}{"action": "list_connections"}); !strings.Contains(got, "disabled") && !strings.Contains(got, "plaintext") {
		t.Fatalf("got %q", got)
	}
}

func TestKillSwitchClosesConnections(t *testing.T) {
	m := NewManager([]Profile{{ID: "p", Type: SourceExcel, FilePath: "a.xlsx"}}, nil)
	if !m.ToolEnabled() {
		t.Fatal("expected enabled")
	}
	m.SetEnabled(false)
	if m.ToolEnabled() {
		t.Fatal("kill switch should disable")
	}
	m.SetEnabled(true)
	if !m.ToolEnabled() {
		t.Fatal("re-enable failed")
	}
}
