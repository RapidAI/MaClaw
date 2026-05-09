package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/datasrv/structureddata"
)

var serviceVersion = "dev"
var secureRandomReader io.Reader = rand.Reader

func main() {
	if len(os.Args) > 1 && isHelpArg(os.Args[1]) {
		printMainUsage(os.Stdout)
		os.Exit(0)
	}
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		os.Exit(runAdminCommand(os.Args[2:], os.Stdout, os.Stderr))
	}
	addr := getenv("MACLAW_DATA_HTTP_ADDR", "127.0.0.1:18180")
	token := strings.TrimSpace(os.Getenv("MACLAW_DATA_TOKEN"))
	if err := validateServiceToken(token); err != nil {
		log.Fatal(err)
	}
	if err := validateListenAddr(addr); err != nil {
		log.Fatal(err)
	}
	store, err := structureddata.NewSQLiteStore(defaultDBPath())
	if err != nil {
		log.Fatalf("create sqlite store: %v", err)
	}
	defer store.Close()
	svc := structureddata.NewService(store, "sqlite")
	apiKeys, err := structureddata.ParseAPIKeyPolicies(os.Getenv("MACLAW_DATA_API_KEYS"))
	if err != nil {
		log.Fatalf("parse MACLAW_DATA_API_KEYS: %v", err)
	}
	server := structureddata.NewHTTPServerWithAPIKeys(svc, token, serviceVersion, apiKeys)
	httpServer := &http.Server{Addr: addr, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("MaClawDataSrv listening on %s", addr)
		errCh <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown server: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}
}

func runAdminCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printAdminUsage(stderr)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" {
		printAdminUsage(stdout)
		return 0
	}
	if args[0] == "help" {
		if len(args) == 1 {
			printAdminUsage(stdout)
			return 0
		}
		switch args[1] {
		case "list":
			printAdminListUsage(stdout)
			return 0
		case "reset-password":
			printAdminResetPasswordUsage(stdout)
			return 0
		default:
			printAdminUsage(stderr)
			return 2
		}
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("admin list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { printAdminListUsage(stderr) }
		dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
		tenantID := fs.String("tenant", "default", "tenant id")
		jsonOutput := fs.Bool("json", false, "write machine-readable JSON")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		store, err := openAdminStore(*dbPath)
		if err != nil {
			fmt.Fprintf(stderr, "open sqlite store: %v\n", err)
			return 1
		}
		defer store.Close()
		result, err := structureddata.NewService(store, "sqlite").ListAdminAccounts(context.Background(), *tenantID)
		if err != nil {
			fmt.Fprintf(stderr, "list administrators: %v\n", err)
			return 1
		}
		if *jsonOutput {
			writeAdminJSON(stdout, result)
			return 0
		}
		if len(result.Items) == 0 {
			scope := strings.TrimSpace(*tenantID)
			if strings.EqualFold(scope, "all") || scope == "*" {
				scope = "all tenants"
			} else {
				scope = "tenant " + scope
			}
			fmt.Fprintf(stdout, "no administrators found for %s\n", scope)
			return 0
		}
		fmt.Fprintln(stdout, "TENANT\tUSERNAME\tROLE\tENABLED\tDISPLAY_NAME")
		for _, item := range result.Items {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%t\t%s\n", tsvField(item.TenantID), tsvField(item.Username), tsvField(item.Role), item.Enabled, tsvField(item.DisplayName))
		}
		return 0
	case "reset-password":
		fs := flag.NewFlagSet("admin reset-password", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { printAdminResetPasswordUsage(stderr) }
		dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
		tenantID := fs.String("tenant", "default", "tenant id")
		username := fs.String("username", "", "administrator username")
		password := fs.String("password", "", "new password; generated when omitted")
		jsonOutput := fs.Bool("json", false, "write machine-readable JSON")
		if err := fs.Parse(args[1:]); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			return 2
		}
		if strings.TrimSpace(*username) == "" {
			fmt.Fprintln(stderr, "-username is required")
			return 2
		}
		generated := false
		if strings.TrimSpace(*password) == "" {
			generatedPassword, err := generateResetPassword()
			if err != nil {
				fmt.Fprintf(stderr, "generate temporary password: %v; provide -password explicitly\n", err)
				return 1
			}
			*password = generatedPassword
			generated = true
		}
		store, err := openAdminStore(*dbPath)
		if err != nil {
			fmt.Fprintf(stderr, "open sqlite store: %v\n", err)
			return 1
		}
		defer store.Close()
		result, err := structureddata.NewService(store, "sqlite").ResetAdminPassword(context.Background(), structureddata.ResetAdminPasswordInput{
			TenantID: *tenantID,
			Username: *username,
			Password: *password,
		})
		if err != nil {
			if errors.Is(err, structureddata.ErrAdminNotFound) {
				fmt.Fprintf(stderr, "reset administrator password: administrator not found for tenant %q username %q\n", strings.TrimSpace(*tenantID), strings.TrimSpace(*username))
				return 1
			}
			fmt.Fprintf(stderr, "reset administrator password: %v\n", err)
			if errors.Is(err, structureddata.ErrInvalidInput) {
				return 2
			}
			return 1
		}
		if *jsonOutput {
			out := map[string]any{
				"tenant_id":  result.TenantID,
				"username":   result.Username,
				"updated_at": result.UpdatedAt,
			}
			if generated {
				out["generated_password"] = *password
			}
			writeAdminJSON(stdout, out)
			return 0
		}
		fmt.Fprintf(stdout, "password reset for %s/%s at %s\n", result.TenantID, result.Username, result.UpdatedAt.Format(time.RFC3339))
		if generated {
			fmt.Fprintf(stdout, "new password: %s\n", *password)
		}
		return 0
	default:
		printAdminUsage(stderr)
		return 2
	}
}

func writeAdminJSON(w io.Writer, out any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

func openAdminStore(path string) (*structureddata.SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultDBPath()
	}
	if info, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("database file does not exist: %s", path)
		}
		return nil, err
	} else if info.IsDir() {
		return nil, fmt.Errorf("database path is a directory: %s", path)
	}
	return structureddata.NewSQLiteStore(path)
}

func tsvField(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return strings.Join(strings.Fields(value), " ")
}

func isHelpArg(arg string) bool {
	switch strings.TrimSpace(arg) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func printMainUsage(w io.Writer) {
	fmt.Fprintln(w, "MaClawDataSrv - enterprise structured data service")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  maclaw-data-srv")
	fmt.Fprintln(w, "      Start the HTTP service using environment variables.")
	fmt.Fprintln(w, "  maclaw-data-srv admin <command> [flags]")
	fmt.Fprintln(w, "      Run offline administrator maintenance commands against the SQLite database.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Service environment:")
	fmt.Fprintln(w, "  MACLAW_DATA_TOKEN        Optional service bearer token. When set, it must be at least 24 characters.")
	fmt.Fprintln(w, "  MACLAW_DATA_HTTP_ADDR    Optional listen address. Default: 127.0.0.1:18180. Plain HTTP is loopback-only.")
	fmt.Fprintln(w, "  MACLAW_DATA_SQLITE_PATH  Optional explicit SQLite database path.")
	fmt.Fprintln(w, "  MACLAW_DATA_ROOT         Optional data root. Default: $HOME/.maclaw_data; database file is data.db.")
	fmt.Fprintln(w, "  MACLAW_DATA_API_KEYS     Optional JSON array of static API key policies.")
	fmt.Fprintln(w, "  MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH")
	fmt.Fprintln(w, "                           Optional local administrator password minimum length. Default: 8, range: 8-128.")
	fmt.Fprintln(w, "  MACLAW_DATA_ADMIN_LOGIN_MAX_FAILURES")
	fmt.Fprintln(w, "                           Optional failed-login lockout threshold. Default: 0 disabled, range: 0-100.")
	fmt.Fprintln(w, "  MACLAW_DATA_ADMIN_LOGIN_LOCKOUT_MINUTES")
	fmt.Fprintln(w, "                           Optional lockout duration when threshold is enabled. Default: 15, range: 1-1440.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "HTTP setup endpoints:")
	fmt.Fprintln(w, "  GET  /api/v1/setup/status     Check whether the first administrator has been initialized.")
	fmt.Fprintln(w, "                                 Response includes password_policy with min_length, lockout, and offline reset availability.")
	fmt.Fprintln(w, "  POST /api/v1/setup/admin      Create the first local administrator account.")
	fmt.Fprintln(w, "  POST /api/v1/login            Login with administrator username/password and receive a bearer token.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Offline admin commands:")
	fmt.Fprintln(w, "  maclaw-data-srv admin list")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username admin")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Help:")
	fmt.Fprintln(w, "  maclaw-data-srv --help")
	fmt.Fprintln(w, "  maclaw-data-srv admin --help")
	fmt.Fprintln(w, "  maclaw-data-srv admin help list")
	fmt.Fprintln(w, "  maclaw-data-srv admin help reset-password")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes:")
	fmt.Fprintln(w, "  0  Success, including help output.")
	fmt.Fprintln(w, "  1  Runtime failure, for example database open error or reset target not found.")
	fmt.Fprintln(w, "  2  Invalid command, invalid flags, or invalid command input.")
}

func printAdminUsage(w io.Writer) {
	fmt.Fprintln(w, "MaClawDataSrv offline administrator maintenance")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  maclaw-data-srv admin list [-db path] [-tenant tenant-id] [-json]")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username name [-password new-password] [-db path] [-tenant tenant-id] [-json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  list")
	fmt.Fprintln(w, "      Query local administrator accounts in the SQLite database.")
	fmt.Fprintln(w, "  reset-password")
	fmt.Fprintln(w, "      Reset a local administrator password when the Web Console password is forgotten.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Common flags:")
	fmt.Fprintln(w, "  -db path")
	fmt.Fprintln(w, "      SQLite database path. Defaults to MACLAW_DATA_SQLITE_PATH, or MACLAW_DATA_ROOT/data.db, or $HOME/.maclaw_data/data.db.")
	fmt.Fprintln(w, "  -tenant tenant-id")
	fmt.Fprintln(w, "      Tenant id for the administrator account. Default: default. Use all to query every tenant for list.")
	fmt.Fprintln(w, "  -json")
	fmt.Fprintln(w, "      Write machine-readable JSON for command output.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  maclaw-data-srv admin list -db D:\\data\\maclaw\\data.db")
	fmt.Fprintln(w, "  maclaw-data-srv admin list -json")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -db D:\\data\\maclaw\\data.db -username admin")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username admin -json")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -tenant default -username admin -password NewPassword-123")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Notes for agents:")
	fmt.Fprintln(w, "  These commands do not require MACLAW_DATA_TOKEN and do not start the HTTP service.")
	fmt.Fprintln(w, "  The SQLite database file must already exist; admin commands refuse to create a new empty database.")
	fmt.Fprintln(w, "  The database is migrated before the command runs because it opens the normal datasrv SQLite store.")
	fmt.Fprintln(w, "  Use command-specific help for exact output fields:")
	fmt.Fprintln(w, "    maclaw-data-srv admin help list")
	fmt.Fprintln(w, "    maclaw-data-srv admin help reset-password")
}

func printAdminListUsage(w io.Writer) {
	fmt.Fprintln(w, "Command: maclaw-data-srv admin list")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Purpose:")
	fmt.Fprintln(w, "  Query administrator account names and metadata from the local datasrv SQLite database.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  maclaw-data-srv admin list [-db path] [-tenant tenant-id] [-json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -db path")
	fmt.Fprintln(w, "      SQLite database path. If omitted, the command uses the same default path as the service.")
	fmt.Fprintln(w, "  -tenant tenant-id")
	fmt.Fprintln(w, "      Tenant id to query. Default: default. Use all or * to query every tenant.")
	fmt.Fprintln(w, "  -json")
	fmt.Fprintln(w, "      Write machine-readable JSON instead of TSV/text output.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Output:")
	fmt.Fprintln(w, "  When accounts exist, stdout is tab-separated with this header:")
	fmt.Fprintln(w, "    TENANT<TAB>USERNAME<TAB>ROLE<TAB>ENABLED<TAB>DISPLAY_NAME")
	fmt.Fprintln(w, "  When no accounts exist, stdout contains:")
	fmt.Fprintln(w, "    no administrators found for tenant <tenant-id>")
	fmt.Fprintln(w, "    no administrators found for all tenants")
	fmt.Fprintln(w, "  With -json, stdout contains a JSON object with an items array.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  maclaw-data-srv admin list")
	fmt.Fprintln(w, "  maclaw-data-srv admin list -db D:\\data\\maclaw\\data.db -tenant default")
	fmt.Fprintln(w, "  maclaw-data-srv admin list -tenant all")
	fmt.Fprintln(w, "  maclaw-data-srv admin list -json")
}

func printAdminResetPasswordUsage(w io.Writer) {
	fmt.Fprintln(w, "Command: maclaw-data-srv admin reset-password")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Purpose:")
	fmt.Fprintln(w, "  Reset a local administrator password directly in the datasrv SQLite database.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username name [-password new-password] [-db path] [-tenant tenant-id] [-json]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -username name")
	fmt.Fprintln(w, "      Required administrator username. Use 'admin list' first when the name is unknown.")
	fmt.Fprintln(w, "  -password new-password")
	fmt.Fprintln(w, "      Optional new password. Must meet MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH, default 8; query /api/v1/setup/status for the active password_policy. If omitted, a temporary password is generated and printed once.")
	fmt.Fprintln(w, "  -db path")
	fmt.Fprintln(w, "      SQLite database path. If omitted, the command uses the same default path as the service.")
	fmt.Fprintln(w, "  -tenant tenant-id")
	fmt.Fprintln(w, "      Tenant id for the administrator account. Default: default.")
	fmt.Fprintln(w, "  -json")
	fmt.Fprintln(w, "      Write machine-readable JSON instead of text output. Includes generated_password only when -password is omitted.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Output:")
	fmt.Fprintln(w, "  On success:")
	fmt.Fprintln(w, "    password reset for <tenant>/<username> at <RFC3339 timestamp>")
	fmt.Fprintln(w, "  If -password is omitted, stdout also includes:")
	fmt.Fprintln(w, "    new password: <generated password>")
	fmt.Fprintln(w, "  With -json, stdout contains tenant_id, username, updated_at, and optional generated_password.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username admin")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -db D:\\data\\maclaw\\data.db -tenant default -username admin")
	fmt.Fprintln(w, "  maclaw-data-srv admin reset-password -username admin -password NewPassword-123")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Safety:")
	fmt.Fprintln(w, "  The command hashes the password with bcrypt and never prints a provided -password value.")
	fmt.Fprintln(w, "  Resetting a password revokes existing sessions for that administrator.")
	fmt.Fprintln(w, "  After reset, sign in with the new password, issue fresh API keys if needed, and retire copied temporary passwords.")
	fmt.Fprintln(w, "  For best results, stop the HTTP service before running offline password maintenance.")
}

func generateResetPassword() (string, error) {
	const prefix = "Mds-"
	minLength := resetPasswordMinLengthFromEnv()
	rawSize := 18
	for len(prefix)+base64.RawURLEncoding.EncodedLen(rawSize) < minLength {
		rawSize++
	}
	raw := make([]byte, rawSize)
	if _, err := io.ReadFull(secureRandomReader, raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func resetPasswordMinLengthFromEnv() int {
	minLength := 8
	if raw := strings.TrimSpace(os.Getenv("MACLAW_DATA_ADMIN_PASSWORD_MIN_LENGTH")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			minLength = parsed
		}
	}
	if minLength < 8 {
		return 8
	}
	if minLength > 128 {
		return 128
	}
	return minLength
}

func defaultDBPath() string {
	if path := strings.TrimSpace(os.Getenv("MACLAW_DATA_SQLITE_PATH")); path != "" {
		return path
	}
	root := strings.TrimSpace(os.Getenv("MACLAW_DATA_ROOT"))
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			root = ".maclaw_data"
		} else {
			root = filepath.Join(home, ".maclaw_data")
		}
	}
	return filepath.Join(root, "data.db")
}

func getenv(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func validateListenAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("MaClawDataSrv defaults to loopback-only; refusing plain HTTP addr %q", addr)
	}
	return nil
}

func validateServiceToken(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if len(token) < 24 {
		return fmt.Errorf("MACLAW_DATA_TOKEN must be at least 24 characters when set")
	}
	return nil
}
