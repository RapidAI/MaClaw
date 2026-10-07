package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/desktopd"
)

func main() {
	addr := strings.TrimSpace(os.Getenv("DESKTOPD_ADDR"))
	if addr == "" {
		addr = ":18081"
	}
	token := strings.TrimSpace(os.Getenv("DESKTOPD_TOKEN"))
	if token == "" {
		log.Fatal("DESKTOPD_TOKEN is required")
	}
	svc := &desktopd.Service{AdvertiseHost: strings.TrimSpace(os.Getenv("DESKTOPD_ADVERTISE_HOST"))}
	// DESKTOPD_IMAGE_SOURCE: where a missing maclaw-gui:2 comes from. Empty
	// means the public ghcr.io/rapidai/maclaw-gui:2; "off" disables pulls
	// (the image is built on the host by remote_deploy.sh). A present local
	// image is never pulled over or replaced.
	sources, err := desktopd.ParseImageSources(os.Getenv("DESKTOPD_IMAGE_SOURCE"))
	if err != nil {
		log.Fatalf("DESKTOPD_IMAGE_SOURCE is invalid: %v", err)
	}
	svc.ImageSources = sources
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_IMAGE_PULL_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			log.Fatalf("DESKTOPD_IMAGE_PULL_TIMEOUT is invalid: %q", raw)
		}
		svc.ImagePullTimeout = timeout
	}
	// The admin panel keeps its account and extra API keys here. Defaults to
	// ./data, which resolves to the deploy directory under the systemd unit.
	// DESKTOPD_STATE_DIR=off disables the panel entirely.
	stateDir := strings.TrimSpace(os.Getenv("DESKTOPD_STATE_DIR"))
	adminDisabled := strings.EqualFold(stateDir, "off")
	if stateDir == "" {
		stateDir = "data"
	}
	// DESKTOPD_MAX_IDLE stops desktops idle longer than this, catching
	// containers Hub can no longer name (moved assignments, crashed callers).
	// Empty keeps the historical behaviour of never reaping.
	if raw := strings.TrimSpace(os.Getenv("DESKTOPD_MAX_IDLE")); raw != "" {
		maxIdle, err := time.ParseDuration(raw)
		if err != nil {
			log.Fatalf("DESKTOPD_MAX_IDLE is invalid: %v", err)
		}
		if maxIdle > 0 {
			svc.StartReaper(context.Background(), 10*time.Minute, maxIdle)
			log.Printf("[desktopd] idle reaper enabled max_idle=%s", maxIdle)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	panelDir := stateDir
	if adminDisabled {
		panelDir = ""
	}
	server := &http.Server{
		Addr:              addr,
		Handler:           desktopd.Handler(svc, token, panelDir),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	displayAddr := addr
	if strings.HasPrefix(displayAddr, ":") {
		displayAddr = "127.0.0.1" + displayAddr
	}
	if len(sources) == 0 {
		log.Printf("[desktopd] image source pulls disabled (DESKTOPD_IMAGE_SOURCE=off)")
	} else {
		// Background only: /v1/health and the admin panel answer while it runs.
		go svc.PrefetchImages(ctx)
	}
	log.Printf("desktopd listening on %s", addr)
	if adminDisabled {
		log.Printf("[desktopd] admin panel disabled (DESKTOPD_STATE_DIR=off)")
	} else {
		log.Printf("[desktopd] admin panel at http://%s/admin (state dir: %s)", displayAddr, stateDir)
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
