package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandCredentialPathTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}

	got, err := ExpandCredentialPath("~/.ssh/id_rsa")
	if err != nil {
		t.Fatalf("ExpandCredentialPath error: %v", err)
	}
	want := filepath.Clean(filepath.Join(home, ".ssh", "id_rsa"))
	if got != want {
		t.Fatalf("ExpandCredentialPath(~/.ssh/id_rsa) = %q, want %q", got, want)
	}
	if strings.Contains(got, "~") {
		t.Fatalf("path still contains tilde: %q", got)
	}
}

func TestSignerFromKeyFileExpandsHomeTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("UserHomeDir unavailable")
	}
	_, err = signerFromKeyFile("~/.ssh/id_rsa_does_not_exist_aicoder", "", nil)
	if err == nil {
		t.Fatal("missing key must fail")
	}
	want := filepath.Join(home, ".ssh", "id_rsa_does_not_exist_aicoder")
	if strings.Contains(err.Error(), "~") || !strings.Contains(err.Error(), want) {
		t.Fatalf("signerFromKeyFile error = %v, want expanded path %s", err, want)
	}
}

func TestExpandCredentialPathDoesNotExpandOtherUser(t *testing.T) {
	// ~other must not be concatenated onto home (old bug: home + path[1:]).
	got, err := ExpandCredentialPath("~other/keys")
	if err != nil {
		t.Fatalf("ExpandCredentialPath error: %v", err)
	}
	if strings.Contains(got, "other") && filepath.IsAbs(got) {
		// Relative ~other becomes Abs(cwd/~other/keys) — must not equal Join(home,"other","keys").
		home, _ := os.UserHomeDir()
		bad := filepath.Clean(filepath.Join(home, "other", "keys"))
		if got == bad {
			t.Fatalf("~other was incorrectly expanded under home: %q", got)
		}
	}
}

func TestExpandCredentialPathEmpty(t *testing.T) {
	if _, err := ExpandCredentialPath("  "); err == nil {
		t.Fatal("expected error for empty path")
	}
}
