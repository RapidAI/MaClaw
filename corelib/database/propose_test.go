package database

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileSummariesExposeHostNotSecrets(t *testing.T) {
	m := NewManager([]Profile{
		{ID: "crm", Name: "CRM", Type: SourceMySQL, Host: "192.168.1.242", Port: 3306, Database: "crm", Username: "root", SecretRef: "keyring://maclaw/database/crm", DSN: "server=x;password=secret"},
		{ID: "bi", Name: "BI", Type: SourcePostgres, Host: "10.0.0.8", Database: "analytics", Username: "reader"},
	}, nil)
	listed := HandleTool(context.Background(), m, map[string]interface{}{"action": "list_connections"})
	if !strings.Contains(listed, `"host":"192.168.1.242"`) || !strings.Contains(listed, `"id":"bi"`) {
		t.Fatalf("summaries = %s", listed)
	}
	if !strings.Contains(listed, `"write_enabled":false`) {
		t.Fatalf("list_connections must project write_enabled even when false: %s", listed)
	}
	if strings.Contains(listed, "keyring") || strings.Contains(listed, "password") || strings.Contains(listed, "server=") {
		t.Fatalf("summary leaked secrets: %s", listed)
	}
	filtered := HandleTool(context.Background(), m, map[string]interface{}{"action": "list_connections", "host": "192.168.1.242"})
	if !strings.Contains(filtered, `"crm"`) || strings.Contains(filtered, `"bi"`) {
		t.Fatalf("host filter = %s", filtered)
	}
}

func TestConnectResolvesUniqueHost(t *testing.T) {
	m := NewManager([]Profile{
		{ID: "crm", Type: SourceExcel, FilePath: "crm.xlsx", Host: "192.168.1.242"},
	}, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{"action": "connect", "host": "192.168.1.242"})
	if strings.Contains(got, "profile_not_found") || strings.Contains(got, "ambiguous") {
		t.Fatalf("unique host connect = %s", got)
	}
}

func TestProposeProfileRejectsPasswordAndReturnsDraft(t *testing.T) {
	m := NewManager(nil, nil)
	denied := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root", "password": "nope",
	})
	if !strings.Contains(denied, "credentials") && !strings.Contains(denied, "password") {
		t.Fatalf("password propose = %s", denied)
	}
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root", "database": "crm",
	})
	if !strings.Contains(got, `"needs_secret":true`) || !strings.Contains(got, `"host":"192.168.1.242"`) || strings.Contains(got, "nope") {
		t.Fatalf("propose = %s", got)
	}
}

func TestProposeProfileReusesConfiguredSecret(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Database: "mysql", Username: "root",
		SecretRef: "keyring://maclaw/database/mysql-192-168-1-242",
	}}, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root",
	})
	if strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("configured profile still asked for a secret: %s", got)
	}
	if !strings.Contains(got, `"needs_secret":false`) || !strings.Contains(got, `"mysql-192-168-1-242"`) {
		t.Fatalf("reuse = %s", got)
	}
}

func TestProposeProfileReusesUniqueHostWhenIDDiffers(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "crm", Type: SourceMySQL, Host: "192.168.1.242", Database: "crm", Username: "root",
		SecretRef: "keyring://maclaw/database/crm",
	}}, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root",
	})
	if strings.Contains(got, `"needs_secret":true`) || !strings.Contains(got, `"crm"`) {
		t.Fatalf("host reuse = %s", got)
	}
}

func TestProposeProfileStillAsksWhenHostIsAmbiguous(t *testing.T) {
	m := NewManager([]Profile{
		{ID: "a", Type: SourceMySQL, Host: "192.168.1.242", Database: "a", Username: "root", SecretRef: "keyring://maclaw/database/a"},
		{ID: "b", Type: SourceMySQL, Host: "192.168.1.242", Database: "b", Username: "root", SecretRef: "keyring://maclaw/database/b"},
	}, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "propose_profile", "host": "192.168.1.242", "username": "root",
	})
	if !strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("ambiguous host should still ask: %s", got)
	}
}

func TestDraftProposedProfileSlugsHost(t *testing.T) {
	p := DraftProposedProfile(map[string]interface{}{"host": "192.168.1.242", "username": "root", "port": 3306})
	if p.ID != "mysql-192-168-1-242" || p.Name != "192.168.1.242" || p.Type != SourceMySQL || p.Port != 3306 || p.Username != "root" {
		t.Fatalf("draft = %#v", p)
	}
}

func TestConnectAuthFailureAsksForSecret(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Port: 3306,
		Database: "mysql", Username: "root", SecretRef: "keyring://maclaw/database/mysql-192-168-1-242",
	}}, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "connect", "profile_id": "mysql-192-168-1-242",
	})
	if !strings.Contains(got, `"needs_secret":true`) || !strings.Contains(got, `"error":"authentication"`) {
		t.Fatalf("auth failure = %s", got)
	}
	if !strings.Contains(got, `"update_secret":true`) {
		t.Fatalf("auth failure should mark password retry: %s", got)
	}
	if strings.Count(got, "authentication:") > 1 {
		t.Fatalf("double-wrapped auth error: %s", got)
	}
	if !strings.Contains(got, `"host":"192.168.1.242"`) || strings.Contains(got, "keyring://") {
		t.Fatalf("draft leaked or missing host: %s", got)
	}
	inspect := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "inspect", "profile_id": "mysql-192-168-1-242",
	})
	if !strings.Contains(inspect, `"needs_secret":true`) {
		t.Fatalf("inspect auth failure = %s", inspect)
	}
}

func TestConnectEmptySecretAsksForSecret(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Port: 3306,
		Database: "mysql", Username: "root", SecretRef: "keyring://maclaw/database/mysql-192-168-1-242",
	}}, func(context.Context, string) (string, error) { return "  ", nil })
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "connect", "profile_id": "mysql-192-168-1-242",
	})
	if !strings.Contains(got, `"needs_secret":true`) || !strings.Contains(got, "bound secret is empty") {
		t.Fatalf("empty secret = %s", got)
	}
}

func TestConnectTLSFailureDoesNotAskForSecret(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "tls-db", Type: SourceMySQL, Host: "127.0.0.1", Port: 3306,
		Database: "mysql", Username: "root", SecretRef: "keyring://maclaw/database/tls-db",
		TLS: TLSSettings{Mode: "require", CAFile: filepath.Join("no", "such", "ca.pem")},
	}}, func(context.Context, string) (string, error) { return "x", nil })
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "connect", "profile_id": "tls-db",
	})
	if strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("tls failure opened a password form: %s", got)
	}
	if !strings.Contains(got, `"error":"authentication"`) && !strings.Contains(got, "tls") {
		t.Fatalf("tls failure = %s", got)
	}
}

func TestConnectTCPFailureDoesNotAskForSecret(t *testing.T) {
	m := NewManager([]Profile{{
		ID: "closed", Type: SourceMySQL, Host: "127.0.0.1", Port: 1,
		Database: "mysql", Username: "root", SecretRef: "keyring://maclaw/database/closed",
	}}, func(context.Context, string) (string, error) { return "x", nil })
	got := HandleTool(context.Background(), m, map[string]interface{}{
		"action": "connect", "profile_id": "closed",
	})
	if strings.Contains(got, `"needs_secret":true`) {
		t.Fatalf("tcp failure opened a password form: %s", got)
	}
	if !strings.Contains(got, `"error":"connection"`) && !strings.Contains(got, `"ok":false`) {
		t.Fatalf("tcp failure = %s", got)
	}
}

func TestQueryRejectsHostOverride(t *testing.T) {
	m := NewManager(nil, nil)
	got := HandleTool(context.Background(), m, map[string]interface{}{"action": "query", "host": "db", "sql": "select 1"})
	if !strings.Contains(got, "host and username must be configured on the profile") {
		t.Fatalf("query host override = %s", got)
	}
}
