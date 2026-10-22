package desktopbot

import (
	"strings"
	"testing"
)

func TestInstallRunsInOwnContainerByDefault(t *testing.T) {
	ResetDangerState()
	t.Cleanup(ResetDangerState)
	t.Setenv("MACLAW_DESKTOP_INSTALL_ASK", "")
	call := DangerCall{Action: "install", Command: "install pandoc"}
	if r := GateDanger(DangerInput{Call: call, Key: "a"}); !r.Run || r.Ask.Text != "" {
		t.Fatalf("default install = %+v", r)
	}
	if r := GateDanger(DangerInput{Call: call, Verdict: "allow", VerdictOK: true, Key: "b"}); !r.Run {
		t.Fatalf("allowed install = %+v", r)
	}
	r := GateDanger(DangerInput{Call: call, Verdict: "block", VerdictOK: true, Key: "c"})
	if r.Run || !strings.Contains(r.Ask.Text, "install pandoc") || strings.Contains(r.Ask.Text, "15 秒") {
		t.Fatalf("blocked install = %+v", r)
	}
	t.Setenv("MACLAW_DESKTOP_INSTALL_ASK", "on")
	if r := GateDanger(DangerInput{Call: call, Verdict: "allow", VerdictOK: true, Key: "d"}); r.Run {
		t.Fatalf("ask-on install ran: %+v", r)
	}
}
