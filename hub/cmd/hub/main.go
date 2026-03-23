package main

import (
	"context"
	"crypto/tls"
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

	configPath := flag.String("config", "", "Path to MaClaw Hub config file")
	if err := flag.CommandLine.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	a, err := app.Bootstrap(cfg, *configPath)
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
			// Manual cert mode: verify files exist before attempting to start.
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

	log.Printf("MaClaw Hub admin credentials reset for username %q", strings.TrimSpace(*username))
	return nil
}
