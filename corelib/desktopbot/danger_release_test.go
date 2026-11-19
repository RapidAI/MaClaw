package desktopbot

import (
	"strings"
	"testing"
)

func TestReleaseAndPipelineAlwaysAsk(t *testing.T) {
	ResetDangerState()
	t.Cleanup(ResetDangerState)
	for _, action := range []string{"release", "dispatch", "pr", "merge"} {
		got := GateDanger(DangerInput{Call: DangerCall{Action: action, Command: "acme/app"}, Key: action})
		if got.Run || got.Ask.Text == "" {
			t.Fatalf("%s ran without a question: %+v", action, got)
		}
	}
}

func TestGitPushAskNamesTheBranch(t *testing.T) {
	ResetDangerState()
	t.Cleanup(ResetDangerState)
	named := GateDanger(DangerInput{Call: DangerCall{Action: "bash", Command: "git push origin fix-login"}, Key: "push-named"})
	if named.Run || !strings.Contains(named.Ask.Text, "git push fix-login") {
		t.Fatalf("named: %+v", named)
	}
	plain := GateDanger(DangerInput{Call: DangerCall{Action: "bash", Command: "git push"}, Key: "push-plain"})
	if plain.Run || !strings.Contains(plain.Ask.Text, "「git push」") || strings.Contains(plain.Ask.Text, "fix-login") {
		t.Fatalf("plain: %+v", plain)
	}
}
