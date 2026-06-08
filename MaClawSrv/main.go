package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/internal/servicehost"
)

var (
	serviceVersion = "dev"
	serviceCommit  = ""
	serviceBuiltAt = ""
)

func main() {
	if err := servicehost.Run("MaClawSrv", runServer); err != nil {
		log.Fatal(err)
	}
}

func runServer(ctx context.Context) error {
	dataRoot := defaultDataRoot()
	adminSecret := os.Getenv("MACLAW_ADMIN_SECRET")
	tokenSecret := os.Getenv("MACLAW_TOKEN_SECRET")
	if err := validateStartupSecrets(adminSecret, tokenSecret); err != nil {
		return fmt.Errorf("invalid security configuration: %w", err)
	}
	if err := configureServiceLogging(dataRoot); err != nil {
		return fmt.Errorf("configure logging: %w", err)
	}

	executor, err := buildCoreAgentExecutorFromEnv()
	if err != nil {
		return fmt.Errorf("invalid executor environment configuration: %w", err)
	}
	if err := validateLocalBashScope(executor); err != nil {
		return fmt.Errorf("invalid local bash configuration: %w", err)
	}
	credentialPepper := os.Getenv("MACLAW_CREDENTIAL_PEPPER")
	if err := validateCredentialPepper(credentialPepper); err != nil {
		return fmt.Errorf("invalid credential pepper configuration: %w", err)
	}
	svc, err := agentservice.NewService(agentservice.Config{
		DataRoot:         dataRoot,
		TokenSecret:      tokenSecret,
		TokenTTL:         12 * time.Hour,
		CredentialPepper: credentialPepper,
	}, nil, executor)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	// Wire skill source control — three-level resolution (global/tenant/user).
	// Uses the same SourceControlService as the hub, backed by a file-based KVStore.
	skillSourceSvc := cskill.NewSourceControlService(newFileKVStore(filepath.Join(dataRoot, "skill_source_control.json")))
	wireSkillSourceFilter(svc, skillSourceSvc)

	// Initialize knowledge store (non-fatal: degrades to no-knowledge mode).
	var knowledgeMgr *knowledgeStoreManager
	km, kmErr := newKnowledgeStoreManager(dataRoot)
	if kmErr != nil {
		log.Printf("[knowledge] initialization failed (non-fatal, knowledge features disabled): %v", kmErr)
	} else {
		knowledgeMgr = km
		executor.SetKnowledgeStore(km.AgentStore())
		log.Printf("[knowledge] initialized successfully")
	}

	// Wire MCP tool provider — enables agent to discover and invoke MCP tools
	// registered via the REST API. Newly installed MCP servers take effect on
	// the next agent Execute() call without restart.
	executor.SetMCPToolProvider(agentservice.NewMCPToolBridge(svc))

	// Wire Skill tool provider — enables agent to list, search, and run Skills
	// installed via the REST API.
	executor.SetSkillToolProvider(agentservice.NewSkillToolBridge(svc))

	server := NewHTTPServer(svc, adminSecret, knowledgeMgr, skillSourceSvc)
	addr := getenv("MACLAW_HTTP_ADDR", "127.0.0.1:18080")

	// Initialize optional scheduler (MACLAW_ENABLE_SCHEDULER=true).
	schMgr := initScheduler(dataRoot, svc, executor)
	if schMgr != nil {
		defer schMgr.Stop()
	}

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
	allowInsecureHTTP, err := getenvBoolStrict("MACLAW_ALLOW_INSECURE_HTTP", false)
	if err != nil {
		return fmt.Errorf("invalid transport environment configuration: %w", err)
	}
	if err := validateTransportSecurity(addr, tlsCertFile, tlsKeyFile, allowInsecureHTTP); err != nil {
		return fmt.Errorf("invalid transport configuration: %w", err)
	}

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
			return err
		}
	case <-ctx.Done():
		log.Printf("shutdown requested, stopping MaClawSrv")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		server.Close()
		// Close knowledge store after HTTP server has drained all in-flight requests.
		if knowledgeMgr != nil {
			knowledgeMgr.Close()
		}
	}
	return nil
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

func wireSkillSourceFilter(svc *agentservice.Service, skillSourceSvc *cskill.SourceControlService) {
	if svc == nil || skillSourceSvc == nil {
		return
	}
	svc.SkillSourceFilter = func(tenantID, userID string) []string {
		return skillSourceSvc.ResolveForUser(context.Background(), strings.TrimSpace(userID), tenantID)
	}
}

func configureServiceLogging(dataRoot string) error {
	logPath := defaultServiceLogPath(dataRoot)
	if strings.TrimSpace(logPath) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	log.SetOutput(f)
	log.Printf("MaClawSrv logging initialized path=%s", logPath)
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseCommaSeparated(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
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

func buildCoreAgentExecutorFromEnv() (*agentservice.CoreAgentExecutor, error) {
	allowLocalBash, err := getenvBoolStrict("MACLAW_ENABLE_LOCAL_BASH", false)
	if err != nil {
		return nil, err
	}
	trustedSingleUser, err := getenvBoolStrict("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER", false)
	if err != nil {
		return nil, err
	}
	allowDirectSSH, err := getenvBoolStrict("MACLAW_ENABLE_DIRECT_SSH", false)
	if err != nil {
		return nil, err
	}
	allowSSHFileTransfer, err := getenvBoolStrict("MACLAW_ENABLE_SSH_FILE_TRANSFER", false)
	if err != nil {
		return nil, err
	}
	return &agentservice.CoreAgentExecutor{
		AllowLocalBash:             allowLocalBash,
		LocalBashTrustedSingleUser: trustedSingleUser,
		LocalBashTenantID:          strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_TENANT_ID")),
		LocalBashUserID:            strings.TrimSpace(os.Getenv("MACLAW_LOCAL_BASH_USER_ID")),
		AllowDirectSSH:             allowDirectSSH,
		AllowSSHFileTransfer:       allowSSHFileTransfer,
	}, nil
}

func getenvBoolStrict(key string, fallback bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true, nil
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
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
