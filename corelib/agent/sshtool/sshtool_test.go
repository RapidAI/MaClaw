package sshtool

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
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
