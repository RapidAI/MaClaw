package main

import (
	"os"
	"strings"
	"testing"
)

func TestDataSrvCommandUsesLocalModuleImplementationPackage(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "github.com/RapidAI/CodeClaw/corelib/structureddata") {
		t.Fatal("datasrv command must not import corelib/structureddata for service construction; use datasrv/structureddata")
	}
	if !strings.Contains(text, "github.com/RapidAI/CodeClaw/datasrv/structureddata") {
		t.Fatal("datasrv command must import its local module implementation package")
	}
	for _, forbidden := range []string{
		`"database/sql"`,
		`"github.com/mattn/go-sqlite3"`,
		`"modernc.org/sqlite"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("datasrv command must not import %s directly; storage implementation belongs in structureddata", forbidden)
		}
	}
}
