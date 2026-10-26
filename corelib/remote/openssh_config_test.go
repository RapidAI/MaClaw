package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseOpenSSHGReadsAliasJumpAndIdentity(t *testing.T) {
	parsed := parseOpenSSHG(`
# comment
user wangqian
hostname 10.247.16.143
port 22
proxyjump jump212,other
identityfile ~/.ssh/id_ed25519_gpu84
identityfile ~/.ssh/id_rsa
certificatefile none
userknownhostsfile ~/.ssh/known_hosts
`)
	if parsed.user != "wangqian" || parsed.hostname != "10.247.16.143" || parsed.port != 22 {
		t.Fatalf("parsed endpoint = %+v", parsed)
	}
	if parsed.proxyJump != "jump212,other" {
		t.Fatalf("proxyjump = %q", parsed.proxyJump)
	}
	if len(parsed.identityFiles) != 2 || parsed.identityFiles[0] != "~/.ssh/id_ed25519_gpu84" {
		t.Fatalf("identity files = %#v", parsed.identityFiles)
	}
	if len(parsed.certificateFiles) != 0 {
		t.Fatalf("none certificate must be ignored, got %#v", parsed.certificateFiles)
	}
	multi := parseOpenSSHG("userknownhostsfile /missing/known_hosts ~/.ssh/known_hosts")
	if len(multi.knownHosts) != 2 || multi.knownHosts[1] != "~/.ssh/known_hosts" {
		t.Fatalf("known hosts = %#v", multi.knownHosts)
	}
}

func TestParseProxyJumpToken(t *testing.T) {
	user, host, port := parseProxyJumpToken("wangqian@jump212:2222")
	if user != "wangqian" || host != "jump212" || port != 2222 {
		t.Fatalf("token = %s %s %d", user, host, port)
	}
	user, host, port = parseProxyJumpToken("ssh://jump212")
	if user != "" || host != "jump212" || port != 0 {
		t.Fatalf("uri token = %s %s %d", user, host, port)
	}
}

func TestApplyOpenSSHClientConfigResolvesAliasAndJump(t *testing.T) {
	known := filepath.Join(t.TempDir(), "known_hosts")
	if err := writeFile(known, []byte("gpu84 ssh-ed25519 AAAA\n")); err != nil {
		t.Fatal(err)
	}
	restore := SetOpenSSHConfigQueryForTest(func(host, user string, port int) (string, error) {
		switch host {
		case OpenSSHDefaultsProbeHost:
			return "user bob\nhostname " + host + "\nport 22\nidentityfile ~/.ssh/id_rsa\nproxyjump none\n", nil
		case "gpu84":
			return "user wangqian\nhostname 10.247.16.143\nport 22\nproxyjump jump212\nidentityfile ~/.ssh/id_ed25519_gpu84\nidentityfile ~/.ssh/id_rsa\nuserknownhostsfile " + known + "\n", nil
		case "jump212":
			return "user jump\nhostname 10.58.34.212\nport 22\nidentityfile ~/.ssh/id_ed25519_gpu84\nproxyjump none\n", nil
		default:
			t.Fatalf("unexpected ssh -G %s", host)
			return "", nil
		}
	})
	defer restore()

	got, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: "gpu84"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "10.247.16.143" || got.User != "wangqian" || got.HostAlias != "gpu84" || got.Label != "gpu84" {
		t.Fatalf("resolved target = %+v", got)
	}
	if !got.PreferAgent || len(got.IdentityFiles) != 1 || !strings.Contains(got.IdentityFiles[0], "id_ed25519_gpu84") {
		t.Fatalf("identity = path %q files %#v prefer %v", got.KeyPath, got.IdentityFiles, got.PreferAgent)
	}
	if got.KnownHostsPath != known || len(got.KnownHostsFiles) == 0 || got.KnownHostsFiles[0] != known {
		t.Fatalf("known hosts = %q files %#v", got.KnownHostsPath, got.KnownHostsFiles)
	}
	if len(got.JumpHosts) != 1 || got.JumpHosts[0].Host != "10.58.34.212" || got.JumpHosts[0].User != "jump" {
		t.Fatalf("jumps = %+v", got.JumpHosts)
	}
	if !got.OpenSSHChecked {
		t.Fatal("resolved config must not query ssh -G again")
	}
	spaced, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: " gpu84 "})
	if err != nil || spaced.Host != "10.247.16.143" || spaced.Label != "gpu84" || spaced.User != "wangqian" {
		t.Fatalf("spaced alias = %+v %v", spaced, err)
	}
	again, err := ApplyOpenSSHClientConfig(got)
	if err != nil || again.Host != got.Host || len(again.JumpHosts) != 1 {
		t.Fatalf("second resolve changed config: %+v %v", again, err)
	}
}

func TestApplyOpenSSHClientConfigKeepsExplicitUserAndKey(t *testing.T) {
	restore := SetOpenSSHConfigQueryForTest(func(host, user string, port int) (string, error) {
		switch host {
		case OpenSSHDefaultsProbeHost:
			return "user bob\nhostname " + host + "\nport 22\nidentityfile ~/.ssh/id_rsa\n", nil
		case "gpu84":
			return "user wangqian\nhostname 10.247.16.143\nport 2222\nproxyjump jump212\nidentityfile ~/.ssh/id_ed25519_gpu84\n", nil
		case "jump212":
			return "user jump\nhostname 10.58.34.212\nport 22\n", nil
		default:
			t.Fatalf("unexpected ssh -G %s", host)
			return "", nil
		}
	})
	defer restore()

	got, err := ApplyOpenSSHClientConfig(SSHHostConfig{
		Host:       "gpu84",
		User:       "other",
		Port:       22,
		KeyPath:    "C:/keys/custom",
		AuthMethod: "key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "other" || got.KeyPath != "C:/keys/custom" || got.PreferAgent {
		t.Fatalf("explicit auth was overwritten: %+v", got)
	}
	if got.Host != "10.247.16.143" || got.Port != 22 || len(got.JumpHosts) != 1 {
		t.Fatalf("alias route = host %s port %d jumps %+v", got.Host, got.Port, got.JumpHosts)
	}
}

func TestApplyOpenSSHClientConfigIgnoresGlobalJumpAndDottedHost(t *testing.T) {
	var calls int
	restore := SetOpenSSHConfigQueryForTest(func(host, user string, port int) (string, error) {
		calls++
		return "user bob\nhostname " + host + "\nport 22\nproxyjump bastion\nidentityfile ~/.ssh/id_rsa\n", nil
	})
	defer restore()

	got, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: "gpu84", User: "wangqian"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Host != "gpu84" || len(got.JumpHosts) != 0 || got.PreferAgent {
		t.Fatalf("global ProxyJump must not attach to an unconfigured alias: %+v", got)
	}

	calls = 0
	ip, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: "10.247.16.143", User: "wangqian"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || ip.Host != "10.247.16.143" {
		t.Fatalf("IP dial consulted ssh -G %d times: %+v", calls, ip)
	}

	calls = 0
	bracket, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: "[2001:db8::1]", User: " root ", Label: " loong "})
	if err != nil || calls != 0 || bracket.Host != "2001:db8::1" || bracket.User != "root" || bracket.Label != "loong" {
		t.Fatalf("bracketed IP consulted ssh -G %d times: %+v %v", calls, bracket, err)
	}
}

func TestApplyOpenSSHClientConfigRejectsJumpCycle(t *testing.T) {
	restore := SetOpenSSHConfigQueryForTest(func(host, user string, port int) (string, error) {
		switch host {
		case OpenSSHDefaultsProbeHost:
			return "user bob\nhostname " + host + "\nport 22\nidentityfile ~/.ssh/id_rsa\n", nil
		case "gpu84":
			return "user wangqian\nhostname 10.247.16.143\nproxyjump jump212\n", nil
		case "jump212":
			return "user jump\nhostname 10.58.34.212\nproxyjump gpu84\n", nil
		default:
			return "user bob\nhostname " + host + "\nport 22\n", nil
		}
	})
	defer restore()
	_, err := ApplyOpenSSHClientConfig(SSHHostConfig{Host: "gpu84"})
	if err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle error = %v", err)
	}
}

func TestSSHPoolKeyKeepsDirectPolicyAndAddsJump(t *testing.T) {
	direct := sshPoolKey(SSHHostConfig{Host: "10.0.0.2", User: "a", Port: 22})
	if direct != "a@10.0.0.2:22|legacy-unverified" {
		t.Fatalf("direct key = %q", direct)
	}
	jumped := sshPoolKey(SSHHostConfig{
		Host:      "10.0.0.2",
		User:      "a",
		Port:      22,
		JumpHosts: []SSHHostConfig{{Host: "10.0.0.1", User: "b", Port: 22}},
	})
	if !strings.HasPrefix(jumped, "a@10.0.0.2:22|legacy-unverified|jump=") || !strings.Contains(jumped, "b@10.0.0.1:22") {
		t.Fatalf("jumped key = %q", jumped)
	}
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
