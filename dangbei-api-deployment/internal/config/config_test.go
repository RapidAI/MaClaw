package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTokenFromCookie(t *testing.T) {
	token := extractTokenFromCookie("foo=bar; token=abcdef0123456789; other=1")
	if token != "abcdef0123456789" {
		t.Fatalf("extractTokenFromCookie = %q", token)
	}
	if extractTokenFromCookie("no-token-here") != "" {
		t.Fatal("expected empty token when missing")
	}
}

func TestAddAndRemoveAccount(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	cfg := &Config{Keys: []string{"k1"}, Accounts: nil}
	if err := cfg.AddAccount("acct1", "session=1; token=deadbeefcafebabe"); err != nil {
		t.Fatalf("AddAccount: %v", err)
	}
	if len(cfg.Accounts) != 1 || cfg.Accounts[0].Token != "deadbeefcafebabe" {
		t.Fatalf("accounts after add: %#v", cfg.Accounts)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("expected config.json written: %v", err)
	}

	if err := cfg.AddAccount("dup", "token=deadbeefcafebabe"); err == nil {
		t.Fatal("expected duplicate account error")
	}

	if err := cfg.RemoveAccount("deadbeefcafebabe"); err != nil {
		t.Fatalf("RemoveAccount: %v", err)
	}
	if len(cfg.Accounts) != 0 {
		t.Fatalf("accounts after remove: %#v", cfg.Accounts)
	}
}

func TestIsSupportedDeepSeekModel(t *testing.T) {
	if !IsSupportedDeepSeekModel("deepseek-chat") {
		t.Fatal("expected deepseek-chat supported")
	}
	if IsSupportedDeepSeekModel("gpt-4") {
		t.Fatal("expected gpt-4 unsupported")
	}
}
