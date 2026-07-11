package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

func TestMobileAgentSSHHostsFromVaultPassword(t *testing.T) {
	// Isolate in-memory maps for this process (tests share package state).
	mobileServerProfiles.Lock()
	prevProfiles := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	prevVault := mobileSSHVault.secrets
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = prevProfiles
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = prevVault
		mobileSSHVault.Unlock()
	})

	principal := &auth.ViewerPrincipal{
		UserID:   "user-ssh-1",
		TenantID: "tenant-a",
		Email:    "ssh@example.com",
	}
	profileID := "edge-linux"
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles["k1"] = mobileServerProfileRecord{
		ProfileID: profileID,
		TenantID:  "tenant-a",
		OwnerID:   "user-ssh-1",
		Name:      "prod-web",
		Host:      "10.0.0.8",
		Port:      22,
		Username:  "ubuntu",
		UpdatedAt: time.Now().UTC(),
	}
	mobileServerProfiles.Unlock()

	enc, err := mobileSSHVaultEncrypt("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	mobileSSHVault.Lock()
	mobileSSHVault.secrets[mobileSSHVaultMapKey("tenant-a", "user-ssh-1", profileID)] = mobileSSHVaultRecord{
		TenantID:        "tenant-a",
		OwnerID:         "user-ssh-1",
		ProfileID:       profileID,
		AuthMode:        "password",
		EncryptedSecret: enc,
		UpdatedAt:       time.Now().UTC(),
	}
	mobileSSHVault.Unlock()

	dir := t.TempDir()
	hosts := mobileAgentSSHHosts(principal, dir)
	if len(hosts) != 1 {
		t.Fatalf("hosts=%#v", hosts)
	}
	h := hosts[0]
	if h.Label != "prod-web" || h.Host != "10.0.0.8" || h.User != "ubuntu" {
		t.Fatalf("unexpected host: %#v", h)
	}
	if h.AuthMethod != "password" || h.Password != "s3cret" {
		t.Fatalf("password auth not injected: %#v", h)
	}
	hint := mobileAgentSSHSystemHint(hosts, nil)
	if !strings.Contains(hint, `label="prod-web"`) {
		t.Fatalf("hint missing label: %s", hint)
	}
	if !strings.Contains(hint, "LIVE sessions") {
		t.Fatalf("hint should mention live sessions: %s", hint)
	}
	// With a live session, model must be told to reuse session_id.
	liveHint := mobileAgentSSHSystemHint(hosts, []remote.SSHSessionSummary{{
		SessionID: "ssh_sess_1",
		HostID:    "ubuntu@10.0.0.8:22",
		HostLabel: "prod-web",
		Status:    "running",
	}})
	if !strings.Contains(liveHint, `session_id="ssh_sess_1"`) || !strings.Contains(liveHint, "SKIP connect") {
		t.Fatalf("live session hint=%s", liveHint)
	}
}

func TestMobileAgentSSHHostsMaterializePrivateKey(t *testing.T) {
	mobileServerProfiles.Lock()
	prevProfiles := mobileServerProfiles.profiles
	mobileServerProfiles.profiles = make(map[string]mobileServerProfileRecord)
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	prevVault := mobileSSHVault.secrets
	mobileSSHVault.secrets = make(map[string]mobileSSHVaultRecord)
	mobileSSHVault.Unlock()
	t.Cleanup(func() {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = prevProfiles
		mobileServerProfiles.Unlock()
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = prevVault
		mobileSSHVault.Unlock()
	})

	principal := &auth.ViewerPrincipal{UserID: "u2", TenantID: "t2"}
	profileID := "db"
	mobileServerProfiles.Lock()
	mobileServerProfiles.profiles["k2"] = mobileServerProfileRecord{
		ProfileID: profileID,
		TenantID:  "t2",
		OwnerID:   "u2",
		Name:      "db",
		Host:      "db.internal",
		Port:      2222,
		Username:  "ops",
	}
	mobileServerProfiles.Unlock()

	// Minimal PEM-like blob (not a real key; materialize only writes bytes).
	// Vault encrypt/decrypt trims surrounding whitespace.
	pem := "-----BEGIN OPENSSH PRIVATE KEY-----\nfake\n-----END OPENSSH PRIVATE KEY-----"
	enc, err := mobileSSHVaultEncrypt(pem)
	if err != nil {
		t.Fatal(err)
	}
	mobileSSHVault.Lock()
	mobileSSHVault.secrets[mobileSSHVaultMapKey("t2", "u2", profileID)] = mobileSSHVaultRecord{
		TenantID: "t2", OwnerID: "u2", ProfileID: profileID,
		AuthMode: "private_key", EncryptedSecret: enc, UpdatedAt: time.Now().UTC(),
	}
	mobileSSHVault.Unlock()

	dir := t.TempDir()
	hosts := mobileAgentSSHHosts(principal, dir)
	if len(hosts) != 1 {
		t.Fatalf("hosts=%#v", hosts)
	}
	h := hosts[0]
	if h.AuthMethod != "key" || h.KeyPath == "" {
		t.Fatalf("expected key host: %#v", h)
	}
	raw, err := os.ReadFile(h.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != pem {
		t.Fatalf("key file mismatch: %q", raw)
	}
	if filepath.Dir(h.KeyPath) != dir {
		t.Fatalf("key not under materialize dir: %s", h.KeyPath)
	}
}

func TestMobileMergeSSHHostsPrefersInjected(t *testing.T) {
	existing := []corelib.SSHHostEntry{{Label: "prod", Host: "old", User: "a"}}
	injected := []corelib.SSHHostEntry{{Label: "prod", Host: "new", User: "b", Password: "x"}}
	merged := mobileMergeSSHHosts(existing, injected)
	if len(merged) != 1 || merged[0].Host != "new" || merged[0].Password != "x" {
		t.Fatalf("merged=%#v", merged)
	}
}

func TestMobileAgentSSHSystemHintEmpty(t *testing.T) {
	hint := mobileAgentSSHSystemHint(nil, nil)
	if !strings.Contains(hint, "NOT enabled") {
		t.Fatalf("hint=%s", hint)
	}
	if !strings.Contains(hint, "vault") {
		t.Fatalf("hint should mention vault setup: %s", hint)
	}
}
