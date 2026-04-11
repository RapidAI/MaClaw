package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/app"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/auth"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/config"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store/sqlite"
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

	configPath := flag.String("config", "", "Path to iWorkerCloud config file")
	if err := flag.CommandLine.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	a, err := app.Bootstrap(cfg)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	defer a.Close()

	addr := cfg.Server.Host + ":" + strconv.Itoa(cfg.Server.Port)
	server := &http.Server{Addr: addr, Handler: a.Handler}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[iWorkerCloud] shutting down...")
		cancel()
		_ = server.Close()
	}()

	log.Printf("[iWorkerCloud] listening on %s", addr)
	log.Printf("[iWorkerCloud] Admin UI: http://localhost:%d/admin/", cfg.Server.Port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("listen: %w", err)
	}
	_ = ctx
	return nil
}

func runAdminReset(args []string) error {
	fs := flag.NewFlagSet("admin reset", flag.ContinueOnError)
	configPath := fs.String("config", "", "Path to iWorkerCloud config file")
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

	provider, err := sqlite.NewProvider(cfg.Database.DSN)
	if err != nil {
		return err
	}
	defer provider.Close()

	if err := sqlite.RunMigrations(provider.Write); err != nil {
		return err
	}

	st := sqlite.NewStore(provider)
	authSvc := auth.NewService(st.Admins)
	if err := authSvc.Setup(context.Background(), *username, *password); err != nil {
		return fmt.Errorf("reset admin: %w", err)
	}

	log.Printf("[iWorkerCloud] admin credentials reset for username %q", strings.TrimSpace(*username))
	return nil
}
