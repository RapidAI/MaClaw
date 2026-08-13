package remote

import (
	"net"
	"os"
	"path/filepath"
	"testing"

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
