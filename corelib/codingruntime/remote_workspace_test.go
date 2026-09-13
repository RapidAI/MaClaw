package codingruntime

import (
	"os/exec"
	"strings"
	"testing"
)

func TestNewRemoteGitBaselineRequestRejectsInvalidPaths(t *testing.T) {
	for _, path := range []string{"", "  ", "relative/repo", "/", "//", "/tmp/repo\n/etc", "/tmp/repo\x00"} {
		if _, err := NewRemoteGitBaselineRequest(path); err == nil {
			t.Fatalf("path %q should be rejected", path)
		}
	}
}

func TestNewRemoteGitBaselineRequestBuildsIdempotentNonInteractiveScript(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/home/testprj-2")
	if err != nil {
		t.Fatal(err)
	}
	if req.OKMarker == req.InitializedMarker || req.OKMarker == req.FailMarker || !strings.HasPrefix(req.OKMarker, remoteGitBaselineMarkerPrefix) {
		t.Fatalf("markers=%#v", req)
	}
	cmd := req.Command
	for _, want := range []string{
		"mkdir -p -- '/home/testprj-2'",
		"GIT_TERMINAL_PROMPT=0 git -C '/home/testprj-2' rev-parse --verify HEAD",
		"GIT_TERMINAL_PROMPT=0 git -C '/home/testprj-2' -c init.defaultBranch=master init",
		"-c user.name=MaClaw",
		"-c user.email=maclaw@localhost",
		"-c commit.gpgsign=false",
		"commit --allow-empty",
		"printf '" + req.OKMarker + "\\n'",
		"printf '" + req.InitializedMarker + "\\n'",
		"printf '" + req.FailMarker + "\\n'",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q: %s", want, cmd)
		}
	}
	if strings.Contains(cmd, " exit ") || strings.HasPrefix(cmd, "exit ") || strings.Contains(cmd, ";exit ") {
		t.Fatalf("baseline script must not exit the shared SSH login shell: %s", cmd)
	}
	if strings.Contains(cmd, "cd ") {
		t.Fatalf("baseline script must not change the shared SSH cwd: %s", cmd)
	}
	second, err := NewRemoteGitBaselineRequest("/home/testprj-2")
	if err != nil {
		t.Fatal(err)
	}
	if second.OKMarker == req.OKMarker {
		t.Fatal("baseline markers must be unique per invocation")
	}
}

func TestNewRemoteGitBaselineRequestQuotesEmbeddedSingleQuotes(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/tmp/o'repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Command, `'/tmp/o'\''repo'`) {
		t.Fatalf("quoted path missing: %s", req.Command)
	}
}

func TestRemoteGitBaselineCommandIsValidPOSIX(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to syntax-check the remote baseline script")
	}
	req, err := NewRemoteGitBaselineRequest("/tmp/maclaw-baseline-syntax")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", "-c", req.Command).CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n: %v\n%s\n%s", err, out, req.Command)
	}
}

func TestRemoteGitBaselineRequestAcceptsExactMarkerLineNotCommandEcho(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/srv/repo")
	if err != nil {
		t.Fatal(err)
	}
	echo := "deploy@host:~$ " + req.Command + "\n"
	if req.Accepted(echo) {
		t.Fatal("PTY command echo must not count as a successful baseline")
	}
	if err := req.Result(echo); err == nil {
		t.Fatal("echo-only output should fail")
	}
	if !req.Accepted(echo + req.OKMarker + "\n") {
		t.Fatal("exact OK marker line should be accepted")
	}
	if !req.Accepted("hint: Using 'master'\n" + req.InitializedMarker + "\n") {
		t.Fatal("exact initialized marker line should be accepted")
	}
	if req.Accepted(echo + req.FailMarker + "\n") {
		t.Fatal("fail marker is not success")
	}
	if err := req.Result(echo + req.FailMarker + "\n"); err == nil || !strings.Contains(err.Error(), "could not create an empty HEAD") {
		t.Fatalf("fail marker err=%v", err)
	}
	wrapped := "[ssh-1] status: running\n$ " + req.Command + "\n" + req.OKMarker + "\n"
	if err := req.Result(wrapped); err != nil {
		t.Fatalf("bound-wrapped success should be accepted: %v", err)
	}
}
