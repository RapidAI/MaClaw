package main

import (
	"os"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestValidateTransportSecurityRequiresTLSOrLoopback(t *testing.T) {
	if err := validateTransportSecurity(":18080", "", "", false); err != nil {
		t.Fatalf("loopback-style addr should allow insecure http: %v", err)
	}
	if err := validateTransportSecurity("127.0.0.1:18080", "", "", false); err != nil {
		t.Fatalf("loopback addr should allow insecure http: %v", err)
	}
	if err := validateTransportSecurity("0.0.0.0:18080", "", "", false); err == nil {
		t.Fatalf("expected non-loopback plain http to be rejected")
	}
	if err := validateTransportSecurity("0.0.0.0:18080", "", "", true); err != nil {
		t.Fatalf("allow insecure http override should pass: %v", err)
	}
	if err := validateTransportSecurity("0.0.0.0:18080", "cert.pem", "key.pem", false); err != nil {
		t.Fatalf("tls config should pass: %v", err)
	}
}

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := map[string]bool{
		":18080":             true,
		"127.0.0.1:18080":    true,
		"localhost:18080":    true,
		"[::1]:18080":        true,
		"0.0.0.0:18080":      false,
		"192.168.1.10:18080": false,
	}
	for addr, want := range cases {
		if got := isLoopbackListenAddr(addr); got != want {
			t.Fatalf("isLoopbackListenAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestValidateCredentialPepper(t *testing.T) {
	if err := validateCredentialPepper(""); err != nil {
		t.Fatalf("empty pepper should be allowed: %v", err)
	}
	if err := validateCredentialPepper("short"); err == nil {
		t.Fatalf("expected short pepper to be rejected")
	}
	if err := validateCredentialPepper("pepper-secret-1234"); err != nil {
		t.Fatalf("expected valid pepper: %v", err)
	}
}

func TestValidateLocalBashScope(t *testing.T) {
	if err := validateLocalBashScope(&agentservice.CoreAgentExecutor{}); err != nil {
		t.Fatalf("disabled local bash should be allowed: %v", err)
	}
	if err := validateLocalBashScope(&agentservice.CoreAgentExecutor{AllowLocalBash: true}); err == nil {
		t.Fatalf("expected trusted single-user requirement")
	}
	if err := validateLocalBashScope(&agentservice.CoreAgentExecutor{AllowLocalBash: true, LocalBashTrustedSingleUser: true}); err == nil {
		t.Fatalf("expected scoped local bash tenant/user requirement")
	}
	if err := validateLocalBashScope(&agentservice.CoreAgentExecutor{AllowLocalBash: true, LocalBashTrustedSingleUser: true, LocalBashTenantID: "tenant_a", LocalBashUserID: "user_a"}); err != nil {
		t.Fatalf("expected scoped local bash config to pass: %v", err)
	}
}

func TestBuildCoreAgentExecutorFromEnv(t *testing.T) {
	keys := []string{
		"MACLAW_ENABLE_LOCAL_BASH",
		"MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER",
		"MACLAW_LOCAL_BASH_TENANT_ID",
		"MACLAW_LOCAL_BASH_USER_ID",
		"MACLAW_ENABLE_DIRECT_SSH",
		"MACLAW_ENABLE_SSH_FILE_TRANSFER",
	}
	original := map[string]string{}
	for _, key := range keys {
		original[key] = os.Getenv(key)
	}
	defer func() {
		for _, key := range keys {
			if original[key] == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, original[key])
			}
		}
	}()

	_ = os.Setenv("MACLAW_ENABLE_LOCAL_BASH", "true")
	_ = os.Setenv("MACLAW_LOCAL_BASH_TRUSTED_SINGLE_USER", "true")
	_ = os.Setenv("MACLAW_LOCAL_BASH_TENANT_ID", "tenant_demo")
	_ = os.Setenv("MACLAW_LOCAL_BASH_USER_ID", "user_demo")
	_ = os.Setenv("MACLAW_ENABLE_DIRECT_SSH", "true")
	_ = os.Setenv("MACLAW_ENABLE_SSH_FILE_TRANSFER", "true")

	executor := buildCoreAgentExecutorFromEnv()
	if executor == nil {
		t.Fatalf("expected executor")
	}
	if !executor.AllowLocalBash || !executor.LocalBashTrustedSingleUser {
		t.Fatalf("unexpected local bash flags: %#v", executor)
	}
	if executor.LocalBashTenantID != "tenant_demo" || executor.LocalBashUserID != "user_demo" {
		t.Fatalf("unexpected local bash scope: %#v", executor)
	}
	if !executor.AllowDirectSSH || !executor.AllowSSHFileTransfer {
		t.Fatalf("unexpected ssh flags: %#v", executor)
	}
}
