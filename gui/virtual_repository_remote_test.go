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

func TestRemoteHostKeyChangedErrorPreservesObservedFingerprint(t *testing.T) {
	err := &remoteHostKeyChangedError{HostID: "example.com:22", ExpectedFingerprint: "SHA256:old", ObservedFingerprint: "SHA256:new", Algorithm: "ssh-ed25519"}
	if got := err.Error(); !strings.Contains(got, "expected SHA256:old") || !strings.Contains(got, "received SHA256:new") {
		t.Fatalf("changed host key error = %q", got)
	}
}

func TestChangedRemoteHostKeyCannotBeTrustedDirectly(t *testing.T) {
	err := validateRemoteVirtualRepositoryHostKey("example.com:22", "SHA256:old", "SHA256:new", "ssh-ed25519", true)
	var changed *remoteHostKeyChangedError
	if !errors.As(err, &changed) {
		t.Fatalf("trusting a changed key error = %v, want changed-key rejection", err)
	}
}

func TestResetRemoteVirtualRepositoryHostKeyRemovesOnlyTargetHost(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remote := &VirtualRepositoryRemote{Host: "Example.com", Port: 22, User: "deploy"}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: "remote_1", Name: "Remote", RootPath: "/srv/workspace", Remote: remote}}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repository-known-hosts.json"), virtualRepositoryKnownHostFile{Version: 1, Hosts: map[string]string{
		"example.com:22":   "SHA256:old",
		"other.example:22": "SHA256:keep",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := app.ResetRemoteVirtualRepositoryHostKey("remote_1"); err != nil {
		t.Fatal(err)
	}
	knownHosts, err := app.loadVirtualRepositoryKnownHosts()
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := knownHosts.Hosts["example.com:22"]; exists {
		t.Fatal("target host key was not removed")
	}
	if got := knownHosts.Hosts["other.example:22"]; got != "SHA256:keep" {
		t.Fatalf("unrelated host key = %q", got)
	}
}

func TestResetRemoteVirtualRepositoryHostKeyRejectsUnknownRepository(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{}}); err != nil {
		t.Fatal(err)
	}
	if err := app.ResetRemoteVirtualRepositoryHostKey("missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown repository reset error = %v", err)
	}
}

func TestValidateRemoteVirtualRepositoryRepairTargetOnlyAcceptsSavedEndpoint(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	remote := &VirtualRepositoryRemote{Host: "example.com", Port: 22, User: "deploy"}
	index := virtualRepositoryIndex{Version: 1, Items: []virtualRepositoryIndexEntry{{ID: "remote_1", Name: "Remote", RootPath: "/srv/workspace", Remote: remote}}}
	if err := writeJSONFile(app.virtualRepositoryStatePath("virtual-repositories-index.json"), index); err != nil {
		t.Fatal(err)
	}
	valid := remoteVirtualRepositoryConnectionInput{RepositoryID: "remote_1", Remote: &VirtualRepositoryRemote{Host: "EXAMPLE.com", Port: 22, User: "deploy"}, RootPath: "/srv/workspace"}
	if err := app.validateRemoteVirtualRepositoryRepairTarget(valid); err != nil {
		t.Fatalf("saved endpoint was rejected: %v", err)
	}
	valid.Remote.User = "other"
	if err := app.validateRemoteVirtualRepositoryRepairTarget(valid); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("changed endpoint repair error = %v", err)
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
