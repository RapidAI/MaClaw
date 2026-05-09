package main

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDataSrvCommandUsesImplementationPackage(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "github.com/RapidAI/CodeClaw/corelib/structureddata") {
		t.Fatal("cmd/maclaw-data-srv must not import corelib/structureddata for service construction; use datasrv/structureddata")
	}
	if !strings.Contains(text, "github.com/RapidAI/CodeClaw/datasrv/structureddata") {
		t.Fatal("cmd/maclaw-data-srv must import datasrv/structureddata as the concrete implementation package")
	}
	for _, forbidden := range []string{
		`"database/sql"`,
		`"github.com/mattn/go-sqlite3"`,
		`"modernc.org/sqlite"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("cmd/maclaw-data-srv must not import %s directly; storage implementation belongs in datasrv/structureddata", forbidden)
		}
	}
}

func TestDataSrvCommandReadmeDocumentsPackageBoundary(t *testing.T) {
	data, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatal("README.md must remain valid UTF-8")
	}
	text := string(data)
	for _, want := range []string{
		"datasrv/structureddata",
		"corelib/structureddata",
		"docs/datasrv-structureddata-boundary.md",
		"MACLAW_DATA_TOKEN",
		"optional service bearer token",
		"first-time administrator setup",
		"MACLAW_DATA_HTTP_ADDR",
		"MACLAW_DATA_SQLITE_PATH",
		"MACLAW_DATA_ROOT",
		"MACLAW_DATA_API_KEYS",
		"JSON array",
		"allowed_domains",
		"allowed_views",
		"allowed_reports",
		"loopback",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("README.md missing %s", want)
		}
	}
}
