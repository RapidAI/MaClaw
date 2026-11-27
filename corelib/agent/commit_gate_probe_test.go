package agent

import "testing"

func TestProbeCurlNotPush(t *testing.T) {
	cmd := `$dest = "E:\workprj\maclaw\.maclaw-tmp\BR"; curl.exe -fL -o "$dest\diagnostics.zip" https://example/a.zip`
	if commandPushesGit(cmd) {
		t.Fatalf("diagnostics download was classified as git push: %s", cmd)
	}
	if !commandPushesGit("$git push") {
		t.Fatal("indirect $git push must still be classified as a push")
	}
	if !commandPushesGit("$x = git push") {
		t.Fatal("assignment of git push must still be classified as a push")
	}
	if commandPushesGit(`$x = git status`) {
		t.Fatal("assignment of git status was classified as a push")
	}
}
