package structureddata

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestStructuredDataBoundaryDocIsReadableAndComplete(t *testing.T) {
	data, err := os.ReadFile("../../docs/datasrv-structureddata-boundary.md")
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatal("docs/datasrv-structureddata-boundary.md must remain valid UTF-8")
	}
	text := string(data)
	for _, want := range []string{
		"`corelib/structureddata`",
		"`datasrv/structureddata`",
		"`cmd/maclaw-data-srv`",
		"independently buildable Go module",
		"module path `github.com/RapidAI/CodeClaw/datasrv`",
		"`go.mod`",
		"`go.sum`",
		"NewSQLiteStore",
		"NewHTTPServerWithAPIKeys",
		"ParseAPIKeyPolicies",
		"sentinel error values",
		"ErrUnauthorized",
		"ErrInvalidInput",
		"httpStatusForError",
		"*_alias.go",
		"instead of importing `corelib/structureddata` directly",
		"Struct-only access contracts with explicit JSON field tags",
		"Behavioral interfaces or implementation aliases",
		"downloadOpenAPIMetadataByRoute",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("boundary doc missing %s", want)
		}
	}
}

func TestDocsIndexLinksStructuredDataBoundary(t *testing.T) {
	data, err := os.ReadFile("../../docs/README.md")
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatal("docs/README.md must remain valid UTF-8")
	}
	text := string(data)
	for _, want := range []string{
		"datasrv-structureddata-boundary.md",
		"datasrv-production-ops-guide.md",
		"`corelib/structureddata`",
		"`datasrv/structureddata`",
		"`cmd/maclaw-data-srv`",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("docs index missing %s", want)
		}
	}
}

func TestProductionOpsGuideIsReadableAndComplete(t *testing.T) {
	data, err := os.ReadFile("../../docs/datasrv-production-ops-guide.md")
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatal("docs/datasrv-production-ops-guide.md must remain valid UTF-8")
	}
	text := string(data)
	for _, want := range []string{
		"MACLAW_DATA_SQLITE_PATH",
		"MACLAW_DATA_HTTP_ADDR",
		"MACLAW_DATA_TOKEN",
		"MACLAW_DATA_API_KEYS",
		"MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH",
		"MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES",
		"MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES",
		"password_policy.min_length",
		"offline_reset_available",
		"reverse proxy",
		"Terminate TLS",
		"admin list",
		"admin reset-password",
		"Password reset revokes active sessions",
		"/api/v1/data/backups",
		"Get-FileHash",
		"SHA-256",
		"confirm=true",
		"integrity_check",
		"vacuum",
		"/api/v1/data/audit/export.csv",
		"governance/evidence-pack",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("production ops guide missing %s", want)
		}
	}
}

func TestPackageReadmesDocumentStructuredDataBoundary(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{
			path: "../README.md",
			want: []string{
				"independently buildable Go module",
				"module path is `github.com/RapidAI/CodeClaw/datasrv`",
				"replace github.com/RapidAI/CodeClaw => ..",
				"go build ./cmd/maclaw-data-srv",
				"`datasrv/structureddata` owns the concrete structured data service implementation",
				"SQLite schema, migrations, and store methods",
				"HTTP API, OpenAPI document, and embedded Web Console",
				"`corelib/structureddata`",
				"`cmd/maclaw-data-srv`",
				"NewSQLiteStore",
				"*_alias.go",
				"datasrv-production-ops-guide.md",
				"backup verification",
				"reverse proxy/TLS deployment",
				"password_policy",
				"retire any copied temporary passwords",
			},
		},
		{
			path: "../../corelib/structureddata/README.md",
			want: []string{
				"`corelib/structureddata` is the caller-facing contract package",
				"shared struct DTOs",
				"JSON-tagged access surface definitions",
				"behavioral interfaces",
				"without pulling in",
				"data service implementation",
				"implementation",
				"`datasrv/structureddata`",
				"concrete service, store, HTTP server, OpenAPI, Web Console",
				"migration code",
				"`cmd/maclaw-data-srv`",
			},
		},
	} {
		t.Run(tc.path, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(data) {
				t.Fatalf("%s must remain valid UTF-8", tc.path)
			}
			text := string(data)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("%s missing %s", tc.path, want)
				}
			}
		})
	}
}

func TestDataSrvModuleFilesExist(t *testing.T) {
	for _, path := range []string{
		"../go.mod",
		"../go.sum",
		"../cmd/maclaw-data-srv/main.go",
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("datasrv independent module file %s missing: %v", path, err)
		}
		if !utf8.Valid(data) {
			t.Fatalf("%s must remain valid UTF-8", path)
		}
	}
	mod, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	text := string(mod)
	for _, want := range []string{
		"module github.com/RapidAI/CodeClaw/datasrv",
		"github.com/RapidAI/CodeClaw v0.0.0",
		"replace github.com/RapidAI/CodeClaw => ..",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("datasrv/go.mod missing %s", want)
		}
	}
}
