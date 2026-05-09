package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultDBPathPrefersExplicitSQLitePath(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "custom.db")
	t.Setenv("MACLAW_DATA_SQLITE_PATH", " "+explicit+" ")
	t.Setenv("MACLAW_DATA_ROOT", filepath.Join(t.TempDir(), "root"))

	if got := defaultDBPath(); got != explicit {
		t.Fatalf("defaultDBPath()=%q, want explicit sqlite path %q", got, explicit)
	}
}

func TestDefaultDBPathUsesConfiguredRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "maclaw-data")
	t.Setenv("MACLAW_DATA_SQLITE_PATH", "")
	t.Setenv("MACLAW_DATA_ROOT", " "+root+" ")

	want := filepath.Join(root, "data.db")
	if got := defaultDBPath(); got != want {
		t.Fatalf("defaultDBPath()=%q, want %q", got, want)
	}
}

func TestGetenvTrimsAndFallsBack(t *testing.T) {
	t.Setenv("MACLAW_TEST_EMPTY", "   ")
	if got := getenv("MACLAW_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Fatalf("getenv empty=%q, want fallback", got)
	}

	t.Setenv("MACLAW_TEST_VALUE", "  value  ")
	if got := getenv("MACLAW_TEST_VALUE", "fallback"); got != "value" {
		t.Fatalf("getenv value=%q, want trimmed value", got)
	}
}

func TestValidateListenAddrAllowsOnlyLoopback(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:18180", "localhost:18180", "[::1]:18180"} {
		if err := validateListenAddr(addr); err != nil {
			t.Fatalf("validateListenAddr(%q) returned %v", addr, err)
		}
	}

	for _, addr := range []string{"0.0.0.0:18180", "192.168.1.10:18180", ":18180", "not-an-addr"} {
		if err := validateListenAddr(addr); err == nil {
			t.Fatalf("validateListenAddr(%q) unexpectedly succeeded", addr)
		}
	}
}

func TestValidateServiceTokenIsOptionalButMustBeLongWhenSet(t *testing.T) {
	if err := validateServiceToken(""); err != nil {
		t.Fatalf("empty service token should be allowed for admin-login startup: %v", err)
	}
	if err := validateServiceToken("short"); err == nil {
		t.Fatal("short configured service token unexpectedly allowed")
	}
	if err := validateServiceToken("test-token-0123456789012345"); err != nil {
		t.Fatalf("long service token rejected: %v", err)
	}
}

func TestCompatibilityHelpDocumentsSetupAndIndependentAdminCLI(t *testing.T) {
	if !isHelpArg("--help") || !isHelpArg("-h") || !isHelpArg("help") {
		t.Fatal("expected common help arguments to be recognized")
	}
	var out bytes.Buffer
	printUsage(&out)
	for _, want := range []string{
		"compatibility service entry point",
		"Optional service bearer token",
		"POST /api/v1/setup/admin",
		"POST /api/v1/login",
		"go run ./cmd/maclaw-data-srv admin --help",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("compatibility help missing %q in:\n%s", want, out.String())
		}
	}
}
