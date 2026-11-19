// Command botnode keeps one intranet Bot node connected to Hub.
// It enrolls once, then accepts Hub-opened streams and forwards them only
// to the local desktopd and MaClawSrv addresses.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/RapidAI/CodeClaw/corelib/edgelink"
)

type nodeConfig struct {
	HubURL      string
	EnrollCode  string
	StatePath   string
	Desktopd    string
	Srv         string
	DesktopdKey string
	SrvAdmin    string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, err := configFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(2)
	}
	if err := run(ctx, cfg, http.DefaultClient); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func configFromEnv() (nodeConfig, error) {
	cfg := nodeConfig{
		HubURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("BOTNODE_HUB_URL")), "/"),
		EnrollCode:  strings.TrimSpace(os.Getenv("BOTNODE_ENROLL_CODE")),
		StatePath:   strings.TrimSpace(os.Getenv("BOTNODE_STATE")),
		Desktopd:    strings.TrimSpace(os.Getenv("BOTNODE_DESKTOPD_ADDR")),
		Srv:         strings.TrimSpace(os.Getenv("BOTNODE_SRV_ADDR")),
		DesktopdKey: strings.TrimSpace(os.Getenv("BOTNODE_DESKTOPD_KEY")),
		SrvAdmin:    strings.TrimSpace(os.Getenv("BOTNODE_SRV_ADMIN")),
	}
	if cfg.StatePath == "" {
		cfg.StatePath = "/var/lib/maclaw/botnode.json"
	}
	if cfg.Desktopd == "" {
		cfg.Desktopd = "127.0.0.1:18081"
	}
	if cfg.Srv == "" {
		cfg.Srv = "127.0.0.1:18080"
	}
	if cfg.Desktopd == "off" || cfg.Desktopd == "-" {
		cfg.Desktopd = ""
	}
	if cfg.Srv == "off" || cfg.Srv == "-" {
		cfg.Srv = ""
	}
	if cfg.DesktopdKey == "" {
		cfg.DesktopdKey = strings.TrimSpace(os.Getenv("DESKTOPD_TOKEN"))
	}
	if cfg.SrvAdmin == "" {
		cfg.SrvAdmin = strings.TrimSpace(os.Getenv("MACLAW_ADMIN_SECRET"))
	}
	if cfg.HubURL == "" {
		return nodeConfig{}, errors.New("BOTNODE_HUB_URL is required")
	}
	if cfg.Desktopd == "" && cfg.Srv == "" {
		return nodeConfig{}, errors.New("set BOTNODE_DESKTOPD_ADDR or BOTNODE_SRV_ADDR")
	}
	if cfg.Desktopd != "" && cfg.DesktopdKey == "" {
		return nodeConfig{}, errors.New("BOTNODE_DESKTOPD_KEY is required")
	}
	return cfg, nil
}

func run(ctx context.Context, cfg nodeConfig, client *http.Client) error {
	st, err := edgelink.LoadCredentials(cfg.StatePath)
	if err != nil {
		return err
	}
	hub, err := edgelink.NormalizeHubURL(cfg.HubURL)
	if err != nil {
		return errors.New("BOTNODE_HUB_URL must be an http:// or https:// address")
	}
	if st.NodeID == "" {
		if cfg.EnrollCode == "" {
			return errors.New("BOTNODE_ENROLL_CODE is required")
		}
		st, err = enroll(ctx, client, cfg)
		if err != nil {
			return err
		}
		if err := edgelink.SaveCredentials(cfg.StatePath, st); err != nil {
			return err
		}
	} else if st.HubURL != "" && st.HubURL != hub {
		return errors.New("BOTNODE_HUB_URL does not match the saved node")
	}
	if st.HubURL == "" {
		st.HubURL = hub
	}
	return edgelink.RunLink(ctx, st, routes(cfg), func(up bool, err error) {
		if !up {
			fmt.Fprintln(os.Stderr, "bot node link closed; reconnecting")
		}
	})
}

func routes(cfg nodeConfig) map[string]string {
	out := map[string]string{}
	if cfg.Desktopd != "" {
		out[edgelink.TargetDesktopd] = cfg.Desktopd
	}
	if cfg.Srv != "" {
		out[edgelink.TargetSrv] = cfg.Srv
	}
	return out
}

func enroll(ctx context.Context, client *http.Client, cfg nodeConfig) (edgelink.Credentials, error) {
	return edgelink.Enroll(ctx, client, cfg.HubURL, edgelink.EnrollRequest{
		Code:        cfg.EnrollCode,
		DesktopdKey: cfg.DesktopdKey,
		SrvAdmin:    cfg.SrvAdmin,
		Version:     "botnode",
		HasDesktopd: cfg.Desktopd != "",
		HasSrv:      cfg.Srv != "",
	})
}
