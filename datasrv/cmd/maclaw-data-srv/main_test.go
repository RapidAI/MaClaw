package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/datasrv/structureddata"
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

func TestDefaultDBPathUsesProgramDataOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}
	t.Setenv("MACLAW_DATA_SQLITE_PATH", "")
	t.Setenv("MACLAW_DATA_ROOT", "")
	pd := os.Getenv("ProgramData")
	if pd == "" {
		t.Skip("ProgramData env not set")
	}
	want := filepath.Join(pd, "MaClawDataSrv", "data.db")
	if got := defaultDBPath(); got != want {
		t.Fatalf("defaultDBPath()=%q, want ProgramData path %q", got, want)
	}
}

func TestMaybeMigrateLegacyDataDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("migration only applies on Windows")
	}
	t.Setenv("MACLAW_DATA_SQLITE_PATH", "")
	t.Setenv("MACLAW_DATA_ROOT", "")

	// Simulate legacy dir with a data.db file.
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome) // os.UserHomeDir reads this on Windows
	legacyDir := filepath.Join(tmpHome, ".maclaw_data")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "data.db"), []byte("test-db-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also create a WAL file to test multi-file copy.
	if err := os.WriteFile(filepath.Join(legacyDir, "data.db-wal"), []byte("wal-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Target: use a temp ProgramData so we don't pollute the real one.
	fakePD := filepath.Join(t.TempDir(), "ProgramData")
	if err := os.MkdirAll(fakePD, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramData", fakePD)

	dbPath := defaultDBPath()
	result := maybeMigrateLegacyDataDir(dbPath)

	// After migration, data.db should be at the new path.
	if result != dbPath {
		t.Fatalf("maybeMigrateLegacyDataDir returned %q, want %q", result, dbPath)
	}
	newDB := filepath.Join(fakePD, "MaClawDataSrv", "data.db")
	content, err := os.ReadFile(newDB)
	if err != nil {
		t.Fatalf("data.db should exist at new location: %v", err)
	}
	if string(content) != "test-db-content" {
		t.Fatalf("data.db content mismatch: %q", content)
	}
	// WAL should also be copied.
	walContent, err := os.ReadFile(filepath.Join(fakePD, "MaClawDataSrv", "data.db-wal"))
	if err != nil {
		t.Fatalf("WAL should be copied: %v", err)
	}
	if string(walContent) != "wal-content" {
		t.Fatalf("WAL content mismatch: %q", walContent)
	}
	// Legacy dir should be renamed to .migrated.
	if _, err := os.Stat(legacyDir + ".migrated"); err != nil {
		t.Fatalf("legacy dir should be renamed to .migrated: %v", err)
	}
}

func TestMaybeMigrateLegacyDataDirNoopWhenNewPathExists(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("migration only applies on Windows")
	}
	// If new path already exists, migration should be a no-op.
	newDir := filepath.Join(t.TempDir(), "MaClawDataSrv")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(newDir, "data.db")
	if err := os.WriteFile(dbPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := maybeMigrateLegacyDataDir(dbPath)
	if result != dbPath {
		t.Fatalf("should return dbPath unchanged when new path exists: got %q", result)
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

func TestAdminHelpIsDetailedForAgents(t *testing.T) {
	var mainHelp bytes.Buffer
	printMainUsage(&mainHelp)
	for _, want := range []string{
		"Optional service bearer token",
		"GET  /api/v1/setup/status",
		"Response includes password_policy",
		"Invalid command, invalid flags, or invalid command input.",
	} {
		if !strings.Contains(mainHelp.String(), want) {
			t.Fatalf("main help missing %q in:\n%s", want, mainHelp.String())
		}
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin --help code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{
		"offline administrator maintenance",
		"These commands do not require MACLAW_DATA_TOKEN",
		"machine-readable JSON",
		"maclaw-data-srv admin help list",
		"maclaw-data-srv admin help reset-password",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("admin help missing %q in:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"help", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin help list code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"admin list [-db path] [-tenant tenant-id] [-json]", "TENANT<TAB>USERNAME", "no administrators found", "-db path", "-tenant all", "-json"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("admin list help missing %q in:\n%s", want, stdout.String())
		}
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"help", "reset-password"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin help reset-password code=%d stderr=%s", code, stderr.String())
	}
	for _, want := range []string{"reset-password -username name [-password new-password] [-db path] [-tenant tenant-id] [-json]", "Use 'admin list' first", "query /api/v1/setup/status", "new password:", "bcrypt", "revokes existing sessions", "issue fresh API keys", "-json", "stop the HTTP service"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("admin reset-password help missing %q in:\n%s", want, stdout.String())
		}
	}
}

func TestAdminCommandListsAndResetsPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	svc := structureddata.NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username:    "admin",
		Password:    "old-password-123",
		DisplayName: "Primary\tAdmin\nOperator",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"list", "-db", dbPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin list code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "admin") {
		t.Fatalf("admin list missing username: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Primary\tAdmin") || strings.Contains(stdout.String(), "Admin\nOperator") || !strings.Contains(stdout.String(), "Primary Admin Operator") {
		t.Fatalf("admin list should sanitize TSV display fields: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"list", "-db", dbPath, "-json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin list json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var listJSON structureddata.ListAdminAccountsResult
	if err := json.Unmarshal(stdout.Bytes(), &listJSON); err != nil {
		t.Fatalf("admin list json decode failed: %v output=%s", err, stdout.String())
	}
	if len(listJSON.Items) != 1 || listJSON.Items[0].Username != "admin" || listJSON.Items[0].DisplayName != "Primary\tAdmin\nOperator" {
		t.Fatalf("unexpected admin list json: %#v", listJSON)
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"list", "-db", dbPath, "-tenant", "all"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin list all code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "default\tadmin") {
		t.Fatalf("admin list all missing default/admin: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin", "-password", "new-password-456"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin reset-password code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "password reset for default/admin") {
		t.Fatalf("admin reset output=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "new-password-456") {
		t.Fatalf("provided password must not be echoed: %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin", "-password", "new-password-789", "-json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin reset-password json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var resetJSON map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &resetJSON); err != nil {
		t.Fatalf("admin reset json decode failed: %v output=%s", err, stdout.String())
	}
	if resetJSON["username"] != "admin" || resetJSON["generated_password"] != nil || strings.Contains(stdout.String(), "new-password-789") {
		t.Fatalf("reset json should not echo provided password: %#v raw=%s", resetJSON, stdout.String())
	}

	store, err = structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer store.Close()
	svc = structureddata.NewService(store, "sqlite")
	if _, err := svc.Login(context.Background(), structureddata.LoginInput{Username: "admin", Password: "old-password-123"}); err == nil {
		t.Fatal("old password still works after reset")
	}
	if _, err := svc.Login(context.Background(), structureddata.LoginInput{Username: "admin", Password: "new-password-789"}); err != nil {
		t.Fatalf("new password login failed: %v", err)
	}
}

func TestAdminResetPasswordCanGenerateUsablePassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	svc := structureddata.NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin generated reset code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	const prefix = "new password: "
	idx := strings.Index(stdout.String(), prefix)
	if idx < 0 {
		t.Fatalf("generated reset output missing new password line: %s", stdout.String())
	}
	generated := strings.TrimSpace(stdout.String()[idx+len(prefix):])
	if len(generated) < 8 {
		t.Fatalf("generated password too short: %q", generated)
	}
	store, err = structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := structureddata.NewService(store, "sqlite").Login(context.Background(), structureddata.LoginInput{Username: "admin", Password: generated}); err != nil {
		t.Fatalf("generated password login failed: %v", err)
	}
}

func TestAdminResetPasswordGeneratedPasswordHonorsConfiguredMinimumLength(t *testing.T) {
	t.Setenv("MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH", "64")
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	svc := structureddata.NewService(store, "sqlite")
	if _, err := svc.InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123456789012345678901234567890123456789012345678901",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin generated reset with configured minimum code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	const prefix = "new password: "
	idx := strings.Index(stdout.String(), prefix)
	if idx < 0 {
		t.Fatalf("generated reset output missing new password line: %s", stdout.String())
	}
	generated := strings.TrimSpace(stdout.String()[idx+len(prefix):])
	if len(generated) < 64 {
		t.Fatalf("generated password length=%d, want at least configured minimum 64: %q", len(generated), generated)
	}
	store, err = structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := structureddata.NewService(store, "sqlite").Login(context.Background(), structureddata.LoginInput{Username: "admin", Password: generated}); err != nil {
		t.Fatalf("generated password honoring configured minimum login failed: %v", err)
	}
}

func TestAdminResetPasswordFailsWhenSecurePasswordGenerationFails(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := structureddata.NewService(store, "sqlite").InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	originalReader := secureRandomReader
	secureRandomReader = errReader{err: errors.New("entropy unavailable")}
	defer func() { secureRandomReader = originalReader }()

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("entropy failure code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "ChangeMe-") || !strings.Contains(stderr.String(), "provide -password explicitly") {
		t.Fatalf("entropy failure should not emit a weak generated password, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestAdminResetPasswordJSONIncludesGeneratedPasswordOnlyWhenGenerated(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := structureddata.NewService(store, "sqlite").InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin", "-json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("admin generated reset json code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode generated reset json: %v output=%s", err, stdout.String())
	}
	generated, _ := payload["generated_password"].(string)
	if len(generated) < 8 || payload["username"] != "admin" {
		t.Fatalf("unexpected generated reset json: %#v", payload)
	}
	store, err = structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen NewSQLiteStore: %v", err)
	}
	defer store.Close()
	if _, err := structureddata.NewService(store, "sqlite").Login(context.Background(), structureddata.LoginInput{Username: "admin", Password: generated}); err != nil {
		t.Fatalf("generated json password login failed: %v", err)
	}
}

type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestAdminCommandDoesNotCreateMissingDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing", "data.db")
	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"list", "-db", dbPath}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("admin list missing db code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "database file does not exist") {
		t.Fatalf("missing db error should be explicit, got stderr=%s", stderr.String())
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("admin list should not create missing database, stat err=%v", err)
	}
}

func TestAdminResetPasswordInvalidInputReturnsUsageExitCode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := structureddata.NewService(store, "sqlite").InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "admin", "-password", "short"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("short password should return usage/input code 2, got code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid input") {
		t.Fatalf("short password should report invalid input, stderr=%s", stderr.String())
	}
}

func TestAdminResetPasswordUnknownUserIsDiagnostic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "data.db")
	store, err := structureddata.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	if _, err := structureddata.NewService(store, "sqlite").InitializeAdmin(context.Background(), structureddata.InitializeAdminInput{
		Username: "admin",
		Password: "old-password-123",
	}); err != nil {
		t.Fatalf("InitializeAdmin: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runAdminCommand([]string{"reset-password", "-db", dbPath, "-username", "missing", "-password", "new-password-456"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("unknown user should return runtime failure code 1, got code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, want := range []string{`administrator not found`, `tenant "default"`, `username "missing"`} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("unknown user diagnostic missing %q in stderr=%s", want, stderr.String())
		}
	}
	if strings.Contains(stderr.String(), "unauthorized") {
		t.Fatalf("unknown user diagnostic should avoid token-style unauthorized wording: %s", stderr.String())
	}
}
