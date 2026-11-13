package main

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestTimeoutOnALoginURLAsksInsteadOfSayingSlow(t *testing.T) {
	prevURL := desktopTimeoutURL
	prevDlg := desktopNativeDialogTitles
	t.Cleanup(func() {
		desktopTimeoutURL = prevURL
		desktopNativeDialogTitles = prevDlg
	})
	desktopNativeDialogTitles = func(context.Context, agentruntime.Scope) []string { return nil }
	desktopTimeoutURL = func(agentruntime.Scope) string { return "https://plogin.m.jd.com/login/login" }
	text, ask, ok := desktopTimeoutHandoff(agentruntime.Scope{TenantID: "t", UserID: "u"}, "navigate")
	if !ok || !ask || !strings.Contains(text, "login_wall") || strings.Contains(text, "slow") {
		t.Fatalf("ask=%v ok=%v text=%q", ask, ok, text)
	}
	out, err := finishDesktopActionPhase(agentruntime.Scope{TenantID: "t", UserID: "u", InstanceID: "i"}, text, nil, "execute")
	if err != nil || !strings.Contains(out, "__ASK_USER__") {
		t.Fatalf("handoff %q %v", out, err)
	}
}

func TestProbeReportsAnExternalProtocolDialog(t *testing.T) {
	prev := desktopNativeDialogTitles
	t.Cleanup(func() { desktopNativeDialogTitles = prev })
	desktopNativeDialogTitles = func(context.Context, agentruntime.Scope) []string {
		return []string{"Open xdg-open?"}
	}
	text, ask, ok := desktopTimeoutHandoff(agentruntime.Scope{}, "probe")
	if !ok || ask || !strings.Contains(text, "external_protocol") || strings.Contains(text, "slow") {
		t.Fatalf("ask=%v ok=%v text=%q", ask, ok, text)
	}
	if desktopNativeDialogKind("Save password?") != "" {
		t.Fatal("an ordinary browser bubble is not a blocking dialog")
	}
}
