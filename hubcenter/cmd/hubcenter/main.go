package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/app"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/backup"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
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
	if len(args) >= 1 && args[0] == "maintenance" {
		return runMaintenance(args[1:])
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage()
		return nil
	}
	return runServer(args)
}

type haPruneCLIResult struct {
	DeletedOps        int64  `json:"deleted_ops"`
	DeletedAppliedOps int64  `json:"deleted_applied_ops"`
	RemainingOps      int64  `json:"remaining_ops"`
	MaxSeq            int64  `json:"max_seq"`
	DatabasePath      string `json:"database_path"`
	SizeBeforeBytes   int64  `json:"size_before_bytes"`
	SizeAfterBytes    int64  `json:"size_after_bytes"`
	Vacuum            bool   `json:"vacuum"`
}

func runMaintenance(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printMaintenanceUsage()
		return nil
	}
	switch args[0] {
	case "ha-prune":
		return runMaintenanceHAPrune(args[1:])
	default:
		return fmt.Errorf("unknown maintenance command %q", args[0])
	}
}

func runMaintenanceHAPrune(args []string) error {
	if hasHelpArg(args) {
		printMaintenanceHAPruneUsage()
		return nil
	}
	fs := flag.NewFlagSet("maintenance ha-prune", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "Path to MaClaw Hub Center config file")
	retentionDays := fs.Float64("retention-days", 0.5, "Delete HA history older than this many days while keeping latest op per entity")
	maxRetainedOps := fs.Int64("max-retained-ops", 50000, "Also cap HA history to approximately this many recent ops")
	batchSize := fs.Int64("batch-size", 20000, "Rows to delete per SQLite batch")
	vacuum := fs.Bool("vacuum", false, "Run VACUUM and truncate WAL after pruning to reclaim disk space; stop Hub Center first")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	sizeBefore := fileSize(cfg.Database.DSN)
	provider, err := sqlite.NewProvider(sqlite.Config{
		DSN:               cfg.Database.DSN,
		WAL:               cfg.Database.WAL,
		BusyTimeoutMS:     cfg.Database.BusyTimeoutMS,
		MaxReadOpenConns:  1,
		MaxReadIdleConns:  1,
		MaxWriteOpenConns: 1,
		MaxWriteIdleConns: 1,
	})
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()
	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return err
	}
	var cutoff time.Time
	if *retentionDays > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(*retentionDays * float64(24*time.Hour)))
	}
	result, err := sqlite.PruneHAHistory(context.Background(), provider.Write, cutoff, *maxRetainedOps, *batchSize)
	if err != nil {
		return err
	}
	if *vacuum {
		if err := sqlite.Vacuum(context.Background(), provider.Write); err != nil {
			return err
		}
	}
	out := haPruneCLIResult{DatabasePath: cfg.Database.DSN, SizeBeforeBytes: sizeBefore, SizeAfterBytes: fileSize(cfg.Database.DSN), Vacuum: *vacuum}
	if result != nil {
		out.DeletedOps = result.DeletedOps
		out.DeletedAppliedOps = result.DeletedAppliedOps
		out.RemainingOps = result.RemainingOps
		out.MaxSeq = result.MaxSeq
	}
	if *jsonOut {
		return printJSON(out)
	}
	fmt.Fprintf(os.Stdout, "HA history pruned: ops=%d applied_ops=%d remaining_ops=%d max_seq=%d\n", out.DeletedOps, out.DeletedAppliedOps, out.RemainingOps, out.MaxSeq)
	fmt.Fprintf(os.Stdout, "database: %s\nsize: %d -> %d bytes\n", out.DatabasePath, out.SizeBeforeBytes, out.SizeAfterBytes)
	if !*vacuum {
		fmt.Fprintln(os.Stdout, "disk space is not reclaimed until VACUUM/WAL checkpoint; rerun with --vacuum after stopping Hub Center")
	}
	return nil
}

func fileSize(path string) int64 {
	if stat, err := os.Stat(path); err == nil {
		return stat.Size()
	}
	return 0
}

func runServer(args []string) error {
	fs := flag.NewFlagSet("hubcenter", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "Path to MaClaw Hub Center config file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	a, err := app.Bootstrap(cfg)
	if err != nil {
		return err
	}
	addr := cfg.Server.ListenHost + ":" + strconv.Itoa(cfg.Server.ListenPort)
	log.Printf("MaClaw Hub Center listening on %s", addr)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{Addr: addr, Handler: a.HTTPHandler}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = a.Close()
			return err
		}
		return a.Close()
	case err := <-errCh:
		_ = a.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
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
	configPath := fs.String("config", "", "Path to MaClaw Hub Center config file")
	outPath := fs.String("out", "", "Backup tar.gz output path")
	includeLogs := fs.Bool("include-logs", false, "Include runtime .log files")
	jsonOut := fs.Bool("json", false, "Print machine-readable JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	result, err := backup.Create(context.Background(), cfg, backup.CreateOptions{
		ConfigPath:  *configPath,
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
	fmt.Fprintln(os.Stdout, "restore: hubcenter restore --file <archive.tar.gz> --target-root <hubcenter-dir> --force")
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
	targetRoot := fs.String("target-root", ".", "Hub Center root directory to restore into")
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
	configPath := fs.String("config", "", "Path to MaClaw Hub Center config file")
	username := fs.String("username", "", "New admin username")
	password := fs.String("password", "", "New admin password")
	fs.SetOutput(os.Stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*username) == "" || strings.TrimSpace(*password) == "" {
		return fmt.Errorf("admin reset requires -username and -password")
	}

	cfg, err := config.Load(*configPath)
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

	log.Printf("MaClaw Hub Center admin credentials reset for username %q", strings.TrimSpace(*username))
	return nil
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
	fmt.Fprintln(os.Stdout, "MaClaw Hub Center")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hubcenter --config <config.yaml>")
	fmt.Fprintln(os.Stdout, "  hubcenter backup <command> [options]")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --file <backup.tar.gz> --target-root <hubcenter-dir> [options]")
	fmt.Fprintln(os.Stdout, "  hubcenter maintenance <command> [options]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  admin reset       Reset local admin credentials")
	fmt.Fprintln(os.Stdout, "  backup create     Create a disaster-recovery tar.gz archive")
	fmt.Fprintln(os.Stdout, "  backup inspect    Read manifest.json from an archive")
	fmt.Fprintln(os.Stdout, "  maintenance       Run offline maintenance tasks")
	fmt.Fprintln(os.Stdout, "  restore           Restore an archive into a Hub Center root directory")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "More help:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup help")
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --help")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --help")
	fmt.Fprintln(os.Stdout, "  hubcenter maintenance ha-prune --help")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --help")
}

func printMaintenanceUsage() {
	fmt.Fprintln(os.Stdout, "MaClaw Hub Center maintenance tools")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  hubcenter maintenance ha-prune --config <config.yaml> [--retention-days 0.5] [--max-retained-ops 50000] [--batch-size 20000] [--vacuum] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Safe emergency flow:")
	fmt.Fprintln(os.Stdout, "  stop Hub Center")
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --config ./configs/config.yaml --out ./data/backups/pre-ha-prune.tar.gz --json")
	fmt.Fprintln(os.Stdout, "  hubcenter maintenance ha-prune --config ./configs/config.yaml --retention-days 0.5 --max-retained-ops 50000 --batch-size 20000 --vacuum --json")
	fmt.Fprintln(os.Stdout, "  start Hub Center")
}

func printMaintenanceHAPruneUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hubcenter maintenance ha-prune --config <config.yaml> [--retention-days 0.5] [--max-retained-ops 50000] [--batch-size 20000] [--vacuum] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Prunes old ha_sync_ops and ha_applied_ops rows while keeping the newest op per entity.")
	fmt.Fprintln(os.Stdout, "Run with --vacuum only while Hub Center is stopped; VACUUM plus WAL checkpoint reclaims SQLite disk space.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --config <path>             Hub Center config file.")
	fmt.Fprintln(os.Stdout, "  --retention-days <n>        Delete historical rows older than n days; decimals allowed, e.g. 0.5. 0 disables time-based pruning.")
	fmt.Fprintln(os.Stdout, "  --max-retained-ops <n>      Also cap retained ops by recent seq count. 0 disables count-based pruning.")
	fmt.Fprintln(os.Stdout, "  --batch-size <n>            Delete rows in bounded chunks to avoid long SQLite write locks.")
	fmt.Fprintln(os.Stdout, "  --vacuum                    Rebuild SQLite file and truncate WAL after pruning to reclaim disk space.")
	fmt.Fprintln(os.Stdout, "  --json                      Print machine-readable JSON.")
}

func printBackupUsage() {
	fmt.Fprintln(os.Stdout, "MaClaw Hub Center backup tools")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Purpose:")
	fmt.Fprintln(os.Stdout, "  Create, inspect, and restore Hub Center disaster-recovery archives.")
	fmt.Fprintln(os.Stdout, "  Archives are gzip-compressed tar files, easy to scp/rsync/download/upload.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Commands:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --config <config.yaml> [--out <backup.tar.gz>] [--include-logs] [--json]")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --file <backup.tar.gz> [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "AI/CI examples:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --config ./configs/config.yaml --json")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --json")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Default archive name:")
	fmt.Fprintln(os.Stdout, "  maclaw-hubcenter-backup-YYYY-MM-DD-HHMMSS.tar.gz")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Included data:")
	fmt.Fprintln(os.Stdout, "  config file, consistent SQLite snapshot, data directory assets, RSA/certificate files,")
	fmt.Fprintln(os.Stdout, "  skills, skill-market workspaces, gossip cache, user data, and Hub Center runtime state.")
	fmt.Fprintln(os.Stdout, "  Runtime .log files are skipped unless --include-logs is set.")
}

func printBackupCreateUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --config <config.yaml> [--out <backup.tar.gz>] [--include-logs] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Creates a gzip-compressed tar archive for disaster recovery.")
	fmt.Fprintln(os.Stdout, "When --out is omitted, the file is created in the current directory using:")
	fmt.Fprintln(os.Stdout, "  maclaw-hubcenter-backup-YYYY-MM-DD-HHMMSS.tar.gz")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --config <path>       Hub Center config file.")
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
	fmt.Fprintln(os.Stdout, "  hubcenter backup create --config ./configs/config.yaml --out ./data/backups/maclaw-hubcenter-backup-$(Get-Date -Format yyyyMMdd-HHmmss).tar.gz --json")
}

func printBackupInspectUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --file <backup.tar.gz> [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Reads manifest.json without restoring files. Use this before transfer or restore.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --file <path>         Backup archive to inspect.")
	fmt.Fprintln(os.Stdout, "  --json                Print full manifest JSON for AI/CI automation.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Example:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --json")
}

func printRestoreUsage() {
	fmt.Fprintln(os.Stdout, "Usage:")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --file <backup.tar.gz> --target-root <hubcenter-dir> [--dry-run] [--force] [--json]")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Restores a Hub Center archive. Stop Hub Center before a real restore.")
	fmt.Fprintln(os.Stdout, "By default, restore refuses to overwrite existing files. Use --dry-run first,")
	fmt.Fprintln(os.Stdout, "then rerun with --force when the target path is correct.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Options:")
	fmt.Fprintln(os.Stdout, "  --file <path>         Backup archive to restore.")
	fmt.Fprintln(os.Stdout, "  --target-root <dir>   Hub Center root directory where configs/ and data/ should be restored.")
	fmt.Fprintln(os.Stdout, "  --dry-run             Validate and report what would be restored; writes nothing.")
	fmt.Fprintln(os.Stdout, "  --force               Overwrite existing files. Use only after stopping Hub Center.")
	fmt.Fprintln(os.Stdout, "  --json                Print machine-readable restore result.")
	fmt.Fprintln(os.Stdout, "")
	fmt.Fprintln(os.Stdout, "Safe restore flow:")
	fmt.Fprintln(os.Stdout, "  hubcenter backup inspect --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --json")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --target-root . --dry-run --json")
	fmt.Fprintln(os.Stdout, "  hubcenter restore --file ./maclaw-hubcenter-backup-2026-04-28-153012.tar.gz --target-root . --force --json")
}
