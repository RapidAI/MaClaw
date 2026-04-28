package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/app"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/backup"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runServer(args)
	}
	if len(args) >= 2 && args[0] == "admin" && args[1] == "reset" {
		return runAdminReset(args[2:])
	}
	if len(args) >= 1 && args[0] == "backup" {
		return runBackup(args[1:])
	}
	if len(args) >= 1 && args[0] == "restore" {
		return runRestore(args[1:])
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage()
		return nil
	}
	return runServer(args)
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "Path to MaClaw Hub config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedConfigPath := resolveConfigPath(*configPath)
	if *configPath == "" && resolvedConfigPath != "" {
		log.Printf("[hub] auto-detected config file: %s", resolvedConfigPath)
	}

	cfg, err := config.Load(resolvedConfigPath)
	if err != nil {
		return err
	}

	a, err := app.Bootstrap(cfg, resolvedConfigPath)
	if err != nil {
		return err
	}
	a.StartBackgroundTasks()
	addr := cfg.Server.ListenHost + ":" + strconv.Itoa(cfg.Server.ListenPort)

	if cfg.TLS.Enabled {
		if cfg.TLS.AutoGenerate {
			if err := app.EnsureSelfSignedCert(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil {
				return fmt.Errorf("auto-generate TLS cert: %w", err)
			}
		} else {
			if _, err := os.Stat(cfg.TLS.CertFile); err != nil {
				return fmt.Errorf("TLS cert file not found: %s (set auto_generate=true or provide valid cert)", cfg.TLS.CertFile)
			}
			if _, err := os.Stat(cfg.TLS.KeyFile); err != nil {
				return fmt.Errorf("TLS key file not found: %s (set auto_generate=true or provide valid key)", cfg.TLS.KeyFile)
			}
		}
		log.Printf("MaClaw Hub listening on %s (TLS)", addr)
		log.Printf("  Clients should use: https://<host>:%d", cfg.Server.ListenPort)
		log.Printf("  To disable TLS (e.g. behind nginx), set tls.enabled=false in config")
		srv := &http.Server{
			Addr:    addr,
			Handler: a.HTTPHandler,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
		return srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile)
	}

	log.Printf("MaClaw Hub listening on %s", addr)
	return http.ListenAndServe(addr, a.HTTPHandler)
}

func runBackup(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printBackupUsage()
		return nil
	}
	switch args[0] {
	case "create":
		return runBackupCreate(args[1:])
	case "inspect", "list":
		return runBackupInspect(args[1:])
	default:
		return fmt.Errorf("unknown backup command %q", args[0])
	}
}

func runBackupCreate(args []string) error {
	if hasHelpArg(args) {
		printBackupCreateUsage()
		return nil
	}
	fs := flag.NewFlagSet("backup create", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "Path to MaClaw Hub config file")
	outPath := fs.String("out", "", "Backup tar.gz output path")
	includeLogs := fs.Bool("include-logs", false, "Include runtime .log files")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedConfigPath := resolveConfigPath(*configPath)
	cfg, err := config.Load(resolvedConfigPath)
	if err != nil {
		return err
	}
	result, err := backup.Create(context.Background(), cfg, backup.CreateOptions{
		ConfigPath:  resolvedConfigPath,
		OutputPath:  *outPath,
		IncludeLogs: *includeLogs,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(result)
	}
	fmt.Fprintf(os.Stdout, "backup created: %s\n", result.ArchivePath)
	fmt.Fprintf(os.Stdout, "entries: %d\n", len(result.Manifest.Entries))
	fmt.Fprintln(os.Stdout, "restore: hub restore --file <archive.tar.gz> --target-root <hub-dir> --force")
	return nil
}

func runBackupInspect(args []string) error {
	if hasHelpArg(args) {
		printBackupInspectUsage()
		return nil
	}
	fs := flag.NewFlagSet("backup inspect", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	filePath := fs.String("file", "", "Backup tar.gz path")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*filePath) == "" {
		return fmt.Errorf("backup inspect requires --file")
	}
	manifest, err := backup.Inspect(*filePath)
	if err != nil {
		return err
	}
	if *jsonOut {
		return printJSON(manifest)
	}
	fmt.Fprintf(os.Stdout, "app: %s\ncreated_at: %s\nentries: %d\n", manifest.App, manifest.CreatedAt, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		fmt.Fprintf(os.Stdout, "%s\t%s\t%d\n", entry.Kind, entry.Path, entry.Size)
	}
	return nil
}

func runRestore(args []string) error {
	if hasHelpArg(args) {
		printRestoreUsage()
		return nil
	}
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	filePath := fs.String("file", "", "Backup tar.gz path")
	targetRoot := fs.String("target-root", ".", "Hub root directory to restore into")
	force := fs.Bool("force", false, "Overwrite existing restored files")
	dryRun := fs.Bool("dry-run", false, "Validate and show what would be restored")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := backup.Restore(backup.RestoreOptions{
		ArchivePath: *filePath,
		TargetRoot:  *targetRoot,
		Force:       *force,
		DryRun:      *dryRun,
	})
	if *jsonOut && result != nil {
		_ = printJSON(result)
	}
	if err != nil {
		return err
	}
	if *jsonOut {
		return nil
	}
	verb := "restored"
	if *dryRun {
		verb = "would restore"
	}
	fmt.Fprintf(os.Stdout, "%s %d entries into %s\n", verb, len(result.Restored), result.TargetRoot)
	if len(result.Skipped) > 0 {
		fmt.Fprintf(os.Stdout, "skipped existing entries: %d\n", len(result.Skipped))
	}
	return nil
}

func runAdminReset(args []string) error {
	fs := flag.NewFlagSet("admin reset", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to MaClaw Hub config file")
	username := fs.String("username", "", "New admin username")
	password := fs.String("password", "", "New admin password")
	fs.SetOutput(os.Stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*username) == "" || strings.TrimSpace(*password) == "" {
		return fmt.Errorf("admin reset requires -username and -password")
	}

	cfg, err := config.Load(resolveConfigPath(*configPath))
	if err != nil {
		return err
	}

	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               cfg.Database.DSN,
		WAL:               cfg.Database.WAL,
		BusyTimeoutMS:     cfg.Database.BusyTimeoutMS,
		MaxReadOpenConns:  cfg.Database.MaxReadOpenConns,
		MaxReadIdleConns:  cfg.Database.MaxReadIdleConns,
		MaxWriteOpenConns: cfg.Database.MaxWriteOpenConns,
		MaxWriteIdleConns: cfg.Database.MaxWriteIdleConns,
		BatchFlushMS:      cfg.Database.BatchFlushMS,
		BatchMaxSize:      cfg.Database.BatchMaxSize,
		BatchQueueSize:    cfg.Database.BatchQueueSize,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = provider.Close()
	}()

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return err
	}

	st := sqlite.NewStore(provider)
	adminService := auth.NewAdminService(st.Admins, st.System, st.AdminAudit)
	if err := adminService.ResetAdminCredentials(context.Background(), *username, *password); err != nil {
		return err
	}

	log.Printf("MaClaw Hub admin credentials reset for username %q", strings.TrimSpace(*username))
	return nil
}

func resolveConfigPath(configPath string) string {
	if strings.TrimSpace(configPath) != "" {
		return configPath
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "config.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "help" || arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func printUsage() {
	fmt.Fprintln(os.Stdout, "MaClaw Hub")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hub --config <config.yaml>")
	fmt.Fprintln(os.Stdout, "  hub backup <command> [options]")
	fmt.Fprintln(os.Stdout, "  hub restore --file <backup.tar.gz> --target-root <hub-dir> [options]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  admin reset       Reset local admin credentials")
	fmt.Fprintln(os.Stdout, "  backup create     Create a disaster-recovery tar.gz archive")
	fmt.Fprintln(os.Stdout, "  backup inspect    Read manifest.json from an archive")
	fmt.Fprintln(os.Stdout, "  restore           Restore an archive into a Hub root directory")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "More help:")
	fmt.Fprintln(os.Stdout, "  hub backup help")
	fmt.Fprintln(os.Stdout, "  hub backup create --help")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --help")
	fmt.Fprintln(os.Stdout, "  hub restore --help")
}

func printBackupUsage() {
	fmt.Fprintln(os.Stdout, "MaClaw Hub backup tools")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Purpose:")
	fmt.Fprintln(os.Stdout, "  Create, inspect, and restore Hub disaster-recovery archives.")
	fmt.Fprintln(os.Stdout, "  Archives are gzip-compressed tar files, easy to scp/rsync/download/upload.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  hub backup create --config <config.yaml> [--out <backup.tar.gz>] [--include-logs] [--json]")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --file <backup.tar.gz> [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "AI/CI examples:")
	fmt.Fprintln(os.Stdout, "  hub backup create --config ./configs/config.yaml --json")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --json")
	fmt.Fprintln(os.Stdout, "  hub restore --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Default archive name:")
	fmt.Fprintln(os.Stdout, "  maclaw-hub-backup-YYYY-MM-DD-HHMMSS.tar.gz")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Included data:")
	fmt.Fprintln(os.Stdout, "  config file, consistent SQLite snapshot, data directory assets, skills,")
	fmt.Fprintln(os.Stdout, "  user/device/session/chat/IM/LLM state, and configured TLS cert/key files.")
	fmt.Fprintln(os.Stdout, "  Runtime .log files are skipped unless --include-logs is set.")
}

func printBackupCreateUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hub backup create --config <config.yaml> [--out <backup.tar.gz>] [--include-logs] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Creates a gzip-compressed tar archive for disaster recovery.")
	fmt.Fprintln(os.Stdout, "When --out is omitted, the file is created in the current directory using:")
	fmt.Fprintln(os.Stdout, "  maclaw-hub-backup-YYYY-MM-DD-HHMMSS.tar.gz")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --config <path>       Hub config file. If omitted, hub may auto-detect config.yaml next to the binary.")
	fmt.Fprintln(os.Stdout, "  --out <path>          Output .tar.gz path. Parent directories are created automatically.")
	fmt.Fprintln(os.Stdout, "  --include-logs        Include runtime .log files for incident forensics.")
	fmt.Fprintln(os.Stdout, "  --json                Print machine-readable JSON for AI/CI automation.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "JSON output fields:")
	fmt.Fprintln(os.Stdout, "  archive_path          Absolute path of the created archive.")
	fmt.Fprintln(os.Stdout, "  manifest.entries      Files included, with path/kind/size.")
	fmt.Fprintln(os.Stdout, "  manifest.instructions Restore hints embedded for AI agents.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "CI example:")
	fmt.Fprintln(os.Stdout, "  hub backup create --config ./configs/config.yaml --out ./data/backups/maclaw-hub-backup-$(Get-Date -Format yyyyMMdd-HHmmss).tar.gz --json")
}

func printBackupInspectUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --file <backup.tar.gz> [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Reads manifest.json without restoring files. Use this before transfer or restore.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --file <path>         Backup archive to inspect.")
	fmt.Fprintln(os.Stdout, "  --json                Print full manifest JSON for AI/CI automation.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Example:")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --json")
}

func printRestoreUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hub restore --file <backup.tar.gz> --target-root <hub-dir> [--dry-run] [--force] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Restores a Hub archive. Stop Hub before a real restore.")
	fmt.Fprintln(os.Stdout, "By default, restore refuses to overwrite existing files. Use --dry-run first,")
	fmt.Fprintln(os.Stdout, "then rerun with --force when the target path is correct.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --file <path>         Backup archive to restore.")
	fmt.Fprintln(os.Stdout, "  --target-root <dir>   Hub root directory where configs/ and data/ should be restored.")
	fmt.Fprintln(os.Stdout, "  --dry-run             Validate and report what would be restored; writes nothing.")
	fmt.Fprintln(os.Stdout, "  --force               Overwrite existing files. Use only after stopping Hub.")
	fmt.Fprintln(os.Stdout, "  --json                Print machine-readable restore result.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Safe restore flow:")
	fmt.Fprintln(os.Stdout, "  hub backup inspect --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --json")
	fmt.Fprintln(os.Stdout, "  hub restore --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json")
	fmt.Fprintln(os.Stdout, "  hub restore --file ./maclaw-hub-backup-2026-04-28-153012.tar.gz --target-root . --force --json")
}
