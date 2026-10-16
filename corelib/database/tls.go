package database

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"strings"

	mysql "github.com/go-sql-driver/mysql"
)

func normalizeTLSMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "disable", "require", "verify-full":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ""
	}
}

func loadTLSConfig(settings TLSSettings, serverName string) (*tls.Config, error) {
	mode := normalizeTLSMode(settings.Mode)
	if mode == "" || mode == "disable" {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
	if mode == "require" {
		cfg.InsecureSkipVerify = true
	}
	caFile := strings.TrimSpace(settings.CAFile)
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("authentication: tls ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("authentication: tls ca_file is not a PEM certificate")
		}
		cfg.RootCAs = pool
		cfg.InsecureSkipVerify = false
	}
	if mode == "verify-full" {
		cfg.InsecureSkipVerify = false
		if strings.TrimSpace(cfg.ServerName) == "" {
			return nil, fmt.Errorf("authentication: verify-full requires a hostname")
		}
	}
	return cfg, nil
}

func applyMySQLTLS(cfg *mysql.Config, settings TLSSettings, serverName, profileID string) error {
	mode := normalizeTLSMode(settings.Mode)
	if mode == "" {
		return nil
	}
	if mode == "disable" {
		cfg.TLSConfig = "false"
		return nil
	}
	tlsCfg, err := loadTLSConfig(settings, serverName)
	if err != nil {
		return err
	}
	if tlsCfg == nil {
		return nil
	}
	if mode == "require" && settings.CAFile == "" {
		cfg.TLSConfig = "skip-verify"
		return nil
	}
	name := "maclaw-db-" + strings.TrimSpace(profileID)
	if name == "maclaw-db-" {
		name = "maclaw-db-default"
	}
	if err := mysql.RegisterTLSConfig(name, tlsCfg); err != nil {
		return fmt.Errorf("authentication: mysql tls: %w", err)
	}
	cfg.TLSConfig = name
	return nil
}

func applyPostgresTLS(u *url.URL, settings TLSSettings) {
	mode := normalizeTLSMode(settings.Mode)
	if mode == "" {
		return
	}
	q := u.Query()
	switch mode {
	case "disable":
		q.Set("sslmode", "disable")
	case "require":
		q.Set("sslmode", "require")
	case "verify-full":
		q.Set("sslmode", "verify-full")
	}
	if ca := strings.TrimSpace(settings.CAFile); ca != "" {
		q.Set("sslrootcert", ca)
	}
	u.RawQuery = q.Encode()
}

func applySQLServerTLS(u *url.URL, settings TLSSettings) {
	mode := normalizeTLSMode(settings.Mode)
	if mode == "" {
		return
	}
	q := u.Query()
	switch mode {
	case "disable":
		q.Set("encrypt", "false")
		q.Set("TrustServerCertificate", "true")
	case "require":
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "true")
	case "verify-full":
		q.Set("encrypt", "true")
		q.Set("TrustServerCertificate", "false")
	}
	u.RawQuery = q.Encode()
}
