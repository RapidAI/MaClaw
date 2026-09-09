package database

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileSearchTokensOmitsSecretsAndDisabled(t *testing.T) {
	tokens := ProfileSearchTokens([]Profile{
		{
			ID:             "mysql-192-168-1-242",
			Name:           "192.168.1.242-legacy",
			Type:           SourceMySQL,
			Host:           "192.168.1.242",
			Port:           3306,
			Database:       "mysql",
			Username:       "root",
			SecretRef:      "keyring://maclaw/database/mysql-192-168-1-242",
			Password:       "sunion123",
			DSN:            "root:sunion123@tcp(192.168.1.242:3306)/mysql",
			DefaultSchema:  "rapidbi",
			AllowedSchemas: []string{"rapidbi", "mysql"},
		},
		{
			ID:       "disabled-src",
			Name:     "should-omit",
			Host:     "10.0.0.1",
			Disabled: true,
		},
	})
	joined := strings.Join(tokens, " ")
	for _, want := range []string{"mysql-192-168-1-242", "192.168.1.242-legacy", "192.168.1.242", "mysql", "rapidbi"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tokens %q missing %q", joined, want)
		}
	}
	for _, blocked := range []string{"sunion123", "keyring://", "root:sunion123", "should-omit", "10.0.0.1", "root", "3306"} {
		if strings.Contains(joined, blocked) {
			t.Fatalf("tokens leaked %q: %q", blocked, joined)
		}
	}
}

func TestManagerSearchTokensIncludeRememberedCatalogNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog.json")
	manager := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Database: "mysql",
	}}, nil)
	manager.SetCatalogNamePath(path)
	manager.RememberCatalogNames("mysql-192-168-1-242", "rapidbi", "information_schema", "3306")
	joined := strings.Join(manager.SearchTokens(), " ")
	if !strings.Contains(joined, "rapidbi") {
		t.Fatalf("remembered schema missing: %q", joined)
	}
	if strings.Contains(joined, "information_schema") || strings.Contains(joined, "3306") {
		t.Fatalf("system/port tokens leaked: %q", joined)
	}
	reloaded := NewManager([]Profile{{
		ID: "mysql-192-168-1-242", Type: SourceMySQL, Host: "192.168.1.242", Database: "mysql",
	}}, nil)
	reloaded.SetCatalogNamePath(path)
	if !strings.Contains(strings.Join(reloaded.SearchTokens(), " "), "rapidbi") {
		t.Fatal("catalog names should survive reload")
	}
}

func TestSearchTokensOmitDisabledAndMissingProfiles(t *testing.T) {
	manager := NewManager([]Profile{
		{ID: "live", Type: SourceMySQL, Host: "10.0.0.2", Database: "app"},
		{ID: "gone", Type: SourceMySQL, Host: "10.0.0.3", Database: "old", Disabled: true},
	}, nil)
	manager.RememberCatalogNames("live", "rapidbi")
	manager.RememberCatalogNames("gone", "secretlib")
	manager.RememberCatalogNames("missing", "ghost")
	joined := strings.Join(manager.SearchTokens(), " ")
	if !strings.Contains(joined, "rapidbi") {
		t.Fatalf("live schema missing: %q", joined)
	}
	if strings.Contains(joined, "secretlib") || strings.Contains(joined, "ghost") || strings.Contains(joined, "10.0.0.3") {
		t.Fatalf("disabled/missing profile leaked: %q", joined)
	}
}

func TestCatalogNamesFromSchemaUsesTableSchemaOnly(t *testing.T) {
	got := catalogNamesFromSchema(SchemaInfo{
		ProfileID: "mysql-192-168-1-242",
		Tables: []TableInfo{
			{Schema: "rapidbi", Name: "users"},
			{Schema: "rapidbi", Name: "orders"},
			{Schema: "information_schema", Name: "tables"},
			{Schema: "", Name: "no_schema"},
		},
	})
	if len(got) != 1 || got[0] != "rapidbi" {
		t.Fatalf("schema names = %#v, want [rapidbi]", got)
	}
}
