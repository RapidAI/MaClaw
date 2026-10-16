package sshtool

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestResolveSSHHostByLabelRequiresExactLabel(t *testing.T) {
	hosts := []corelib.SSHHostEntry{{Label: "prod-web", Host: "10.0.0.10", User: "deploy"}}
	if got := ResolveSSHHostByLabel(hosts, "prod-web"); got == nil || got.Host != "10.0.0.10" {
		t.Fatalf("expected exact label match, got %#v", got)
	}
	if got := ResolveSSHHostByLabel(hosts, "prod"); got != nil {
		t.Fatalf("expected partial label not to match, got %#v", got)
	}
}

func TestResolveSSHHostByLabelTrimsAndIgnoresCase(t *testing.T) {
	hosts := []corelib.SSHHostEntry{{Label: "Prod-Web", Host: "10.0.0.10", User: "deploy"}}
	if got := ResolveSSHHostByLabel(hosts, " prod-web "); got == nil || got.Label != "Prod-Web" {
		t.Fatalf("expected case-insensitive trimmed label match, got %#v", got)
	}
}

func TestSSHConnectOpenSSHAliasDoesNotRequireUser(t *testing.T) {
	restore := remote.SetOpenSSHConfigQueryForTest(func(host, user string, port int) (string, error) {
		switch host {
		case "gpu84":
			return "user wangqian\nhostname 127.0.0.1\nport 22\nproxyjump jump212\nidentityfile C:/keys/id_ed25519_gpu84\n", nil
		case "jump212":
			return "user jump\nhostname 127.0.0.1\nport 1\nidentityfile C:/keys/id_ed25519_gpu84\n", nil
		default:
			return "user bob\nhostname " + host + "\nport 22\nidentityfile C:/keys/id_rsa\n", nil
		}
	})
	defer restore()

	got := SSHConnect(SSHToolDeps{Manager: remote.NewSSHSessionManager(nil)}, map[string]interface{}{
		"action": "connect",
		"host":   "gpu84",
	})
	if strings.Contains(got, "需要 host 和 user") {
		t.Fatalf("alias connect still required a user: %s", got)
	}
	if !strings.Contains(got, "127.0.0.1") && !strings.Contains(got, "wangqian") && !strings.Contains(got, "jump") {
		t.Fatalf("alias was not resolved before dial: %s", got)
	}
}

func TestBackgroundTaskActionsRequirePolicyOwner(t *testing.T) {
	deps := SSHToolDeps{BGTaskMgr: remote.NewSSHBackgroundTaskManager(nil)}
	for name, got := range map[string]string{
		"list":  SSHListTasks(deps),
		"check": SSHCheckTask(deps, map[string]interface{}{"task_id": "bg_1"}),
		"kill":  SSHKillTask(deps, map[string]interface{}{"task_id": "bg_1"}),
	} {
		if !strings.Contains(got, "runtime owner is missing") {
			t.Fatalf("%s should fail closed without PolicyOwnerID, got %q", name, got)
		}
	}
}
