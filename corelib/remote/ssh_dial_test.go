package remote

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHHostKeyCallbackCapturesFingerprint(t *testing.T) {
	key := testSSHSigner(t)
	var captured string
	callback, err := sshHostKeyCallback(SSHHostConfig{
		HostKeyFingerprintCapture: func(fingerprint string) { captured = fingerprint },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := callback("example.com:22", nil, key.PublicKey()); err != nil {
		t.Fatalf("callback error = %v", err)
	}
	if want := ssh.FingerprintSHA256(key.PublicKey()); captured != want {
		t.Fatalf("captured fingerprint = %q, want %q", captured, want)
	}
}

func TestSSHHostKeyCallbackKnownHostsVerifiesBeforeCapture(t *testing.T) {
	trusted, untrusted := testSSHSigner(t), testSSHSigner(t)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	knownHostLine := "[example.com]:22 " + string(ssh.MarshalAuthorizedKey(trusted.PublicKey()))
	if err := os.WriteFile(knownHostsPath, []byte(knownHostLine), 0o600); err != nil {
		t.Fatal(err)
	}

	var captured string
	callback, err := sshHostKeyCallback(SSHHostConfig{
		KnownHostsPath:            knownHostsPath,
		HostKeyFingerprintCapture: func(fingerprint string) { captured = fingerprint },
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 22}
	if err := callback("example.com:22", remoteAddr, trusted.PublicKey()); err != nil {
		t.Fatalf("known host rejected: %v", err)
	}
	if want := ssh.FingerprintSHA256(trusted.PublicKey()); captured != want {
		t.Fatalf("captured fingerprint = %q, want %q", captured, want)
	}
	captured = ""
	if err := callback("example.com:22", remoteAddr, untrusted.PublicKey()); err == nil {
		t.Fatal("unknown host key should be rejected by known_hosts")
	}
	if captured != "" {
		t.Fatalf("capture ran for rejected known_hosts key: %q", captured)
	}
}

func TestSSHHostKeyCallbackAcceptsConfigAlias(t *testing.T) {
	key := testSSHSigner(t)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	line := "gpu84 " + string(ssh.MarshalAuthorizedKey(key.PublicKey()))
	if err := os.WriteFile(knownHostsPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	callback, err := sshHostKeyCallback(SSHHostConfig{
		HostAlias:      "gpu84",
		Port:           22,
		KnownHostsPath: knownHostsPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.247.16.143"), Port: 22}
	if err := callback("10.247.16.143:22", remoteAddr, key.PublicKey()); err != nil {
		t.Fatalf("alias known_hosts entry rejected: %v", err)
	}
}

func TestSSHHostKeyCallbackAliasAllowsUnknownAndRejectsChange(t *testing.T) {
	trusted, presented := testSSHSigner(t), testSSHSigner(t)
	knownHostsPath := filepath.Join(t.TempDir(), "known_hosts")
	line := "gpu84 " + string(ssh.MarshalAuthorizedKey(trusted.PublicKey()))
	if err := os.WriteFile(knownHostsPath, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	callback, err := sshHostKeyCallback(SSHHostConfig{
		HostAlias:      "gpu84",
		Port:           22,
		KnownHostsPath: knownHostsPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.247.16.143"), Port: 22}
	if err := callback("10.247.16.143:22", remoteAddr, presented.PublicKey()); err == nil {
		t.Fatal("a different key for the alias was accepted")
	}
	other := filepath.Join(t.TempDir(), "empty_known_hosts")
	if err := os.WriteFile(other, []byte("unrelated.example "+string(ssh.MarshalAuthorizedKey(trusted.PublicKey()))), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown, err := sshHostKeyCallback(SSHHostConfig{HostAlias: "gpu84", Port: 22, KnownHostsPath: other})
	if err != nil {
		t.Fatal(err)
	}
	if err := unknown("10.247.16.143:22", remoteAddr, presented.PublicKey()); err != nil {
		t.Fatalf("unknown alias key rejected: %v", err)
	}
}

func TestSSHHostKeyCallbackReadsEveryKnownHostsFile(t *testing.T) {
	trusted, presented := testSSHSigner(t), testSSHSigner(t)
	dir := t.TempDir()
	systemFile := filepath.Join(dir, "system_known_hosts")
	userFile := filepath.Join(dir, "user_known_hosts")
	if err := os.WriteFile(systemFile, []byte("other.example "+string(ssh.MarshalAuthorizedKey(trusted.PublicKey()))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("gpu84 "+string(ssh.MarshalAuthorizedKey(trusted.PublicKey()))), 0o600); err != nil {
		t.Fatal(err)
	}
	callback, err := sshHostKeyCallback(SSHHostConfig{
		HostAlias:       "gpu84",
		Port:            22,
		KnownHostsFiles: []string{systemFile, userFile},
	})
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.247.16.143"), Port: 22}
	if err := callback("10.247.16.143:22", remoteAddr, trusted.PublicKey()); err != nil {
		t.Fatalf("key stored in the second known_hosts file was rejected: %v", err)
	}
	if err := callback("10.247.16.143:22", remoteAddr, presented.PublicKey()); err == nil {
		t.Fatal("a different key was accepted even though the second file pins gpu84")
	}
}

func TestSSHHostKeyCallbackAliasFallsBackWhenKnownHostsUnreadable(t *testing.T) {
	key := testSSHSigner(t)
	garbage := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(garbage, []byte("this is not a known_hosts line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := sshHostKeyCallback(SSHHostConfig{KnownHostsPath: garbage}); err == nil {
		t.Fatal("a caller that pinned one known_hosts file should see a load error")
	}
	callback, err := sshHostKeyCallback(SSHHostConfig{HostAlias: "gpu84", Port: 22, KnownHostsPath: garbage})
	if err != nil {
		t.Fatalf("alias login should fall back when known_hosts cannot be parsed: %v", err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("10.247.16.143"), Port: 22}
	if err := callback("10.247.16.143:22", remoteAddr, key.PublicKey()); err != nil {
		t.Fatalf("alias fallback rejected the key: %v", err)
	}
}

type publicKeySigner struct {
	ssh.Signer
	pub ssh.PublicKey
}

func (s publicKeySigner) PublicKey() ssh.PublicKey { return s.pub }

func TestMatchingAgentSignersKeepsConfiguredKey(t *testing.T) {
	configured, other := testSSHSigner(t), testSSHSigner(t)
	now := time.Now()
	expired := &ssh.Certificate{
		Key:         configured.PublicKey(),
		ValidBefore: uint64(now.Add(-time.Hour).Unix()),
	}
	live := &ssh.Certificate{
		Key:         configured.PublicKey(),
		ValidAfter:  uint64(now.Add(-time.Minute).Unix()),
		ValidBefore: uint64(now.Add(time.Hour).Unix()),
	}
	agent := []ssh.Signer{
		publicKeySigner{Signer: other, pub: other.PublicKey()},
		publicKeySigner{Signer: configured, pub: expired},
		publicKeySigner{Signer: configured, pub: live},
		configured,
	}
	got := matchingAgentSigners(agent, publicKeyFingerprints([]ssh.Signer{configured}), now)
	if len(got) != 2 {
		t.Fatalf("matched %d signers, want the live certificate and the raw key", len(got))
	}
	if _, ok := got[0].PublicKey().(*ssh.Certificate); !ok {
		t.Fatal("live certificate should be offered before the raw key")
	}
	if ssh.FingerprintSHA256(rawPublicKey(got[1].PublicKey())) != ssh.FingerprintSHA256(configured.PublicKey()) {
		t.Fatal("second signer is not the configured key")
	}
}

func TestCertificateUsableRejectsExpired(t *testing.T) {
	now := time.Now()
	expired := &ssh.Certificate{ValidBefore: uint64(now.Add(-time.Hour).Unix())}
	if certificateUsable(expired, now) {
		t.Fatal("expired certificate was usable")
	}
	current := &ssh.Certificate{
		ValidAfter:  uint64(now.Add(-time.Minute).Unix()),
		ValidBefore: uint64(now.Add(time.Hour).Unix()),
	}
	if !certificateUsable(current, now) {
		t.Fatal("current certificate was not usable")
	}
}
