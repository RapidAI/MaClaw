package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

var (
	serviceVersion = "dev"
	serviceCommit  = ""
	serviceBuiltAt = ""
)

func main() {
	dataRoot := defaultDataRoot()
	adminSecret := os.Getenv("MACLAW_ADMIN_SECRET")
	tokenSecret := os.Getenv("MACLAW_TOKEN_SECRET")
	if err := validateStartupSecrets(adminSecret, tokenSecret); err != nil {
		log.Fatalf("invalid security configuration: %v", err)
	}

	executor := buildCoreAgentExecutorFromEnv()
	if err := validateLocalBashScope(executor); err != nil {
		log.Fatalf("invalid local bash configuration: %v", err)
	}
	credentialPepper := os.Getenv("MACLAW_CREDENTIAL_PEPPER")
	if err := validateCredentialPepper(credentialPepper); err != nil {
		log.Fatalf("invalid credential pepper configuration: %v", err)
	}
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:         dataRoot,
		TokenSecret:      tokenSecret,
		TokenTTL:         12 * time.Hour,
		CredentialPepper: credentialPepper,
	}, nil, executor)
	if err != nil {
		log.Fatalf("create service: %v", err)
	}

	server := NewHTTPServer(svc, adminSecret)
	addr := getenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	tlsCertFile := os.Getenv("MACLAW_TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("MACLAW_TLS_KEY_FILE")
	if err := validateTransportSecurity(addr, tlsCertFile, tlsKeyFile, getenvBool("MACLAW_ALLOW_INSECURE_HTTP", false)); err != nil {
		log.Fatalf("invalid transport configuration: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() {
		log.Printf("MaClawSrv listening on %s with data root %s", addr, svc.DataRoot())
		if tlsCertFile != "" || tlsKeyFile != "" {
			if tlsCertFile == "" || tlsKeyFile == "" {
				errCh <- errors.New("both MACLAW_TLS_CERT_FILE and MACLAW_TLS_KEY_FILE are required when TLS is enabled")
				return
			}
			errCh <- httpServer.ListenAndServeTLS(tlsCertFile, tlsKeyFile)
			return
		}
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	case <-ctx.Done():
		log.Printf("shutdown requested, stopping MaClawSrv")
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

func validateStartupSecrets(adminSecret, tokenSecret string) error {
	if len(adminSecret) < 24 {
		return errors.New("MACLAW_ADMIN_SECRET must be set and at least 24 characters")
	}
	if len(tokenSecret) < 32 {
		return errors.New("MACLAW_TOKEN_SECRET must be set and at least 32 characters")
	}
	if adminSecret == "maclaw-admin-dev" || tokenSecret == "maclaw-token-dev" {
		return errors.New("default development secrets are not allowed")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func defaultDataRoot() string {
	if v := strings.TrimSpace(os.Getenv("MACLAW_DATA_ROOT")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".maclaw_srv"
	}
	return filepath.Join(home, ".maclaw_srv")
}

func buildCoreAgentExecutorFromEnv() *agentservice.CoreAgentExecutor {
	return &agentservice.CoreAgentExecutor{
		AllowLocalBash:             getenvBool("MACLAW_ENABLE_LOCAL_BASH", false),
		LocalBashTrustedSingleUser: getenvBool("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER", false),
		LocalBashTenantID:          strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_TENANT_ID")),
		LocalBashUserID:            strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_USER_ID")),
		AllowDirectSSH:             getenvBool("MACLAW_ENABLE_DIRECT_SSH", false),
		AllowSSHFileTransfer:       getenvBool("MACLAW_ENABLE_SSH_FILE_TRANSFER", false),
	}
}

func getenvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}

func validateTransportSecurity(addr, tlsCertFile, tlsKeyFile string, allowInsecureHTTP bool) error {
	hasTLS := strings.TrimSpace(tlsCertFile) != "" || strings.TrimSpace(tlsKeyFile) != ""
	if hasTLS {
		if strings.TrimSpace(tlsCertFile) == "" || strings.TrimSpace(tlsKeyFile) == "" {
			return errors.New("both MACLAW_TLS_CERT_FILE and MACLAW_TLS_KEY_FILE are required when TLS is enabled")
		}
		return nil
	}
	if allowInsecureHTTP || isLoopbackListenAddr(addr) {
		return nil
	}
	return fmt.Errorf("plain HTTP is only allowed on loopback by default; configure TLS or set MACLAW_ALLOW_INSECURE_HTTP=true for addr %q", addr)
}

func isLoopbackListenAddr(addr string) bool {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return false
	}
	host := trimmed

	if h, _, err := net.SplitHostPort(trimmed); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validateLocalBashScope(executor *agentservice.CoreAgentExecutor) error {
	if executor == nil || !executor.AllowLocalBash {
		return nil
	}
	if !executor.LocalBashTrustedSingleUser {
		return errors.New("MACLAW_ENABLE_LOCAL_BASH requires MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER=true and should only be used for trusted single-user deployments")
	}
	if strings.TrimSpace(executor.LocalBashTenantID) == "" || strings.TrimSpace(executor.LocalBashUserID) == "" {
		return errors.New("MACLAW_ENABLE_LOCAL_BASH requires both MACLAW_LOCAL_BASH_TENANT_ID and MACLAW_LOCAL_BASH_USER_ID")
	}
	return nil
}
func validateCredentialPepper(pepper string) error {
	pepper = strings.TrimSpace(pepper)
	if pepper == "" {
		return nil
	}
	if len(pepper) < 16 {
		return errors.New("MACLAW_CREDENTIAL_PEPPER must be at least 16 characters when set")
	}
	return nil
}
