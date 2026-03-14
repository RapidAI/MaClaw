package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/app"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) >= 2 && args[0] == "admin" && args[1] == "reset" {
		return runAdminReset(args[2:])
	}

	configPath := flag.String("config", "", "Path to CodeClaw Hub config file")
	if err := flag.CommandLine.Parse(args); err != nil {
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
	a.StartBackgroundTasks()
	addr := cfg.Server.ListenHost + ":" + strconv.Itoa(cfg.Server.ListenPort)
	log.Printf("CodeClaw Hub listening on %s", addr)
	return http.ListenAndServe(addr, a.HTTPHandler)
}

func runAdminReset(args []string) error {
	fs := flag.NewFlagSet("admin reset", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to CodeClaw Hub config file")
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

	log.Printf("CodeClaw Hub admin credentials reset for username %q", strings.TrimSpace(*username))
	return nil
}
