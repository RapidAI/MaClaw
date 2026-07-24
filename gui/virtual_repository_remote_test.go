package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestShouldRetryRemoteGitCheckout(t *testing.T) {
	for _, message := range []string{
		"gnutls_handshake() failed: The TLS connection was non-properly terminated.",
		"fatal: unable to access: connection reset by peer",
		"fatal: unable to access: Could not resolve host: github.com",
		"fatal: unable to access: OpenSSL SSL_read: Connection reset by peer, errno 104",
		"RPC failed; curl 56 Recv failure: Connection reset by peer",
	} {
		if !shouldRetryRemoteGitCheckout(errors.New(message)) {
			t.Errorf("transport failure %q was not retried", message)
		}
	}
	for _, message := range []string{
		"remote: Repository not found.",
		"fatal: Authentication failed for 'https://github.com/example/private.git/'",
	} {
		if shouldRetryRemoteGitCheckout(errors.New(message)) {
			t.Errorf("non-transient failure %q was retried", message)
		}
	}
	if got := remoteGitCheckoutRetryDelay(2); got != 2*time.Second {
		t.Fatalf("retry delay = %s, want 2s", got)
	}
	if remoteGitCheckoutCleanupTimeout != 15*time.Second {
		t.Fatalf("cleanup timeout = %s, want 15s", remoteGitCheckoutCleanupTimeout)
	}
	stagingPath := remoteGitCheckoutStagingPath("/srv/workspace/source")
	if !strings.HasPrefix(stagingPath, "/srv/workspace/source.maclaw-checkout-") || stagingPath == "/srv/workspace/source" {
		t.Fatalf("staging path = %q", stagingPath)
	}
}

func TestRemoteGitCheckoutFinalizeCommandDoesNotNestStagingDirectory(t *testing.T) {
	command := remoteGitCheckoutFinalizeCommand("'/srv/workspace/source'", "'/srv/workspace/source.maclaw-checkout-id'")
	if !strings.Contains(command, "mv -T -- '/srv/workspace/source.maclaw-checkout-id' '/srv/workspace/source'") {
		t.Fatalf("finalize command must use an exact target move: %s", command)
	}
	if !strings.Contains(command, "test ! -e '/srv/workspace/source' && test ! -L '/srv/workspace/source'") {
		t.Fatalf("finalize command must reject an existing target or broken symlink: %s", command)
	}
	if !strings.Contains(command, "rmdir -- '/srv/workspace/source'") {
		t.Fatalf("finalize command must only remove an empty target directory: %s", command)
	}
	if !strings.Contains(command, "test -d '/srv/workspace/source' && test ! -L '/srv/workspace/source'") {
		t.Fatalf("finalize command must not inspect or remove a target symlink: %s", command)
	}
}

func TestSSHHostKeyFingerprintStable(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := sshHostKeyFingerprint(key)
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Fatalf("fingerprint=%q", fingerprint)
	}
	if again := sshHostKeyFingerprint(key); again != fingerprint {
		t.Fatalf("fingerprint changed between calls: %q != %q", fingerprint, again)
	}
}

func TestKnownHostsRejectsUnsupportedVersion(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	path := app.virtualRepositoryStatePath("virtual-repository-known-hosts.json")
	if err := writeJSONFile(path, virtualRepositoryKnownHostFile{Version: 2, Hosts: map[string]string{}}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.loadVirtualRepositoryKnownHosts(); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Fatalf("known-hosts version error = %v", err)
	}
}

func TestRemoteConnectionRejectsInvalidInputBeforeDial(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	input, err := json.Marshal(remoteVirtualRepositoryConnectionInput{
		Remote:   &VirtualRepositoryRemote{Host: "example.com", User: "alice"},
		RootPath: "relative/path",
		Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.TestRemoteVirtualRepositoryConnection(string(input)); err == nil || !strings.Contains(err.Error(), "absolute POSIX") {
		t.Fatalf("remote connection validation error = %v", err)
	}
}

func TestParseRemoteVirtualRepositoryDirectoryStats(t *testing.T) {
	count, size, err := parseRemoteVirtualRepositoryDirectoryStats("12 345\n")
	if err != nil || count != 12 || size != 345 {
		t.Fatalf("valid stats = (%d, %d, %v)", count, size, err)
	}
	for _, invalid := range []string{"", "12", "12 nope", "-1 3", "1 2 trailing"} {
		if _, _, err := parseRemoteVirtualRepositoryDirectoryStats(invalid); err == nil {
			t.Fatalf("invalid stats %q were accepted", invalid)
		}
	}
}

func TestRemoteVirtualRepositoryHostID(t *testing.T) {
	remote := &VirtualRepositoryRemote{Host: " Example.COM ", User: "deploy"}
	if got := remoteVirtualRepositoryHostID(remote); got != "example.com:22" {
		t.Fatalf("host id=%q", got)
	}
}

func TestSameRemoteVirtualRepositoryEndpoint(t *testing.T) {
	base := &VirtualRepositoryRemote{Host: " Example.COM ", Port: 22, User: "deploy"}
	for name, candidate := range map[string]*VirtualRepositoryRemote{
		"same normalized endpoint": {Host: "example.com", User: "deploy"},
		"different host":           {Host: "other.example.com", Port: 22, User: "deploy"},
		"different port":           {Host: "example.com", Port: 2222, User: "deploy"},
		"different user":           {Host: "example.com", Port: 22, User: "other"},
	} {
		got := sameRemoteVirtualRepositoryEndpoint(base, candidate)
		want := name == "same normalized endpoint"
		if got != want {
			t.Errorf("%s: same endpoint=%v, want %v", name, got, want)
		}
	}
	if sameRemoteVirtualRepositoryEndpoint(base, nil) || !sameRemoteVirtualRepositoryEndpoint(nil, nil) {
		t.Fatal("nil endpoint comparison is incorrect")
	}
}

func TestValidateVirtualRepositorySSHHost(t *testing.T) {
	for _, valid := range []string{"example.com", "build-01.internal", "127.0.0.1", "2001:db8::1"} {
		if err := validateVirtualRepositorySSHHost(valid); err != nil {
			t.Errorf("host %q should be valid: %v", valid, err)
		}
	}
	for _, invalid := range []string{"https://example.com", "user@example.com", "example.com:22", "example.com/path", "-bad.example", "bad-.example", "bad_name.example"} {
		if err := validateVirtualRepositorySSHHost(invalid); err == nil {
			t.Errorf("host %q should be rejected", invalid)
		}
	}
}

func TestRemoteVirtualRepositoryManifestOmitsConnectionMetadata(t *testing.T) {
	repo := VirtualRepository{Version: 1, ID: "repo", Name: "remote", RootPath: "/srv/workspace", Remote: &VirtualRepositoryRemote{Host: "secret-host.example", Port: 22, User: "deploy"}, Nodes: []VirtualRepositoryNode{}}
	disk := repo
	disk.RootPath = ""
	disk.Remote = nil
	raw, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-host") || strings.Contains(string(raw), "deploy") || strings.Contains(string(raw), "/srv/workspace") {
		t.Fatalf("remote connection metadata leaked into portable manifest: %s", raw)
	}
}

func TestRemoteVirtualRepositoryNodePath(t *testing.T) {
	if got := remoteVirtualRepositoryNodePath("/srv/workspace/", "services/api"); got != "/srv/workspace/services/api" {
		t.Fatalf("remote node path=%q", got)
	}
	if err := validateRemoteVirtualRepositoryRelativePath("services/api"); err != nil {
		t.Fatal(err)
	}
}
