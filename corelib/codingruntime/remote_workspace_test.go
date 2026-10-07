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

func TestNewRemoteGitBaselineRequestBuildsExecChannelScript(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/home/testprj-2")
	if err != nil {
		t.Fatal(err)
	}
	cmd := req.Command
	for _, want := range []string{
		"dir='/home/testprj-2'",
		"leaf='testprj-2'",
		`mkdir -p -- "$dir"`,
		`mkdir -p -- "$alt"`,
		"home=${HOME-}",
		remoteWorkDirMarker,
		`GIT_TERMINAL_PROMPT=0 git -C "$use" rev-parse --verify HEAD`,
		"printf '%s\\n' 'git not found' >&2; exit 127",
		`GIT_TERMINAL_PROMPT=0 git -C "$use" -c init.defaultBranch=master init >/dev/null || exit 1`,
		"-c user.name=MaClaw",
		"-c user.email=maclaw@localhost",
		"-c commit.gpgsign=false",
		"commit --allow-empty --no-verify",
		"exit 0",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("command missing %q:\n%s", want, cmd)
		}
	}
	// Exit status is the result because this script runs as its own process.
	// It must not cd: the exec channel has no login-shell cwd to preserve,
	// and a relative path would escape the declared project.
	if strings.Contains(cmd, "cd ") {
		t.Fatalf("baseline script must not cd: %s", cmd)
	}
	if strings.Contains(cmd, "safe.directory") {
		t.Fatalf("baseline must not disable git ownership checks: %s", cmd)
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

func TestRemoteGitProbeCommandStopsWhenGitFails(t *testing.T) {
	cmd, err := RemoteGitProbeCommand("/home/testprj", "BEGIN_marker", "END_marker")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(cmd, ";") {
		t.Fatalf("a semicolon still prints the end marker after git fails: %s", cmd)
	}
	steps := strings.Split(cmd, " && ")
	if len(steps) != 4 {
		t.Fatalf("steps=%q", steps)
	}
	if !strings.Contains(steps[0], "git -C '/home/testprj' rev-parse HEAD") || !strings.Contains(steps[2], "status --porcelain=v1 --untracked-files=all") {
		t.Fatalf("command=%s", cmd)
	}
	if !strings.Contains(steps[1], `printf '\n%s\n' 'BEGIN_marker'`) || !strings.Contains(steps[3], `'END_marker'`) {
		t.Fatalf("markers=%s", cmd)
	}
	quoted, err := RemoteGitProbeCommand("/tmp/o'repo", "BEGIN_marker", "END_marker")
	if err != nil || !strings.Contains(quoted, `'/tmp/o'\''repo'`) {
		t.Fatalf("quoted=%s err=%v", quoted, err)
	}
	if _, err := RemoteGitProbeCommand("relative", "BEGIN_marker", "END_marker"); err == nil {
		t.Fatal("relative workdir was accepted")
	}
	if _, err := RemoteGitProbeCommand("/tmp/repo", "BEGIN marker", "END_marker"); err == nil {
		t.Fatal("marker with a space was accepted")
	}
}

func TestRemoteProbeExecErrorUsesGitStderr(t *testing.T) {
	err := RemoteProbeExecError(128, "hint: Waiting for your editor\nfatal: Unable to create index.lock\n")
	if err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("err=%v", err)
	}
	bare := RemoteProbeExecError(1, " \n")
	if bare == nil || !strings.Contains(bare.Error(), "exit 1") {
		t.Fatalf("bare=%v", bare)
	}
}

func TestRemoteGitBaselineExecResultUsesExitStatus(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/srv/repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := req.ExecResult(0, req.Command, "echoed script must not matter"); err != nil {
		t.Fatalf("exit 0 is success regardless of echoed text: %v", err)
	}
	fatal := req.ExecResult(1, "", "hint: Using 'master'\nfatal: detected dubious ownership in repository\n")
	if fatal == nil || !strings.Contains(fatal.Error(), "dubious ownership") || !strings.Contains(fatal.Error(), "could not create an empty HEAD") {
		t.Fatalf("stderr detail=%v", fatal)
	}
	followed := req.ExecResult(128, "", "fatal: detected dubious ownership in repository at '/home/testprj'\nTo add an exception for this directory, call:\n\tgit config --global --add safe.directory /home/testprj\n")
	if followed == nil || !strings.Contains(followed.Error(), "fatal: detected dubious ownership") || strings.Contains(followed.Error(), "safe.directory") {
		t.Fatalf("help after fatal=%v", followed)
	}
	missing := req.ExecResult(127, "", "git not found\n")
	if missing == nil || !strings.Contains(missing.Error(), "git not found") {
		t.Fatalf("missing git=%v", missing)
	}
	bare := req.ExecResult(1, "", " \n")
	if bare == nil || !strings.Contains(bare.Error(), "exit 1") {
		t.Fatalf("bare failure=%v", bare)
	}
	if err := req.ExecResult(1, "nothing to commit", ""); err == nil || !strings.Contains(err.Error(), "nothing to commit") {
		t.Fatalf("stdout fallback=%v", err)
	}
}

func TestRemoteGitBaselineEffectiveWorkDir(t *testing.T) {
	req, err := NewRemoteGitBaselineRequest("/home/testprj")
	if err != nil {
		t.Fatal(err)
	}
	declared, err := req.EffectiveWorkDir("", "/home/testprj")
	if err != nil || declared != "/home/testprj" {
		t.Fatalf("declared=%q err=%v", declared, err)
	}
	moved, err := req.EffectiveWorkDir("MACLAW_REMOTE_WORKDIR=/home/znsoft/testprj\n", "/home/testprj")
	if err != nil || moved != "/home/znsoft/testprj" {
		t.Fatalf("moved=%q err=%v", moved, err)
	}
	for _, stdout := range []string{
		"MACLAW_REMOTE_WORKDIR=/etc/passwd\n",
		"MACLAW_REMOTE_WORKDIR=/home/znsoft/other\n",
		"MACLAW_REMOTE_WORKDIR=relative/testprj\n",
		"MACLAW_REMOTE_WORKDIR=/\n",
	} {
		if _, err := req.EffectiveWorkDir(stdout, "/home/testprj"); err == nil {
			t.Fatalf("accepted %q", stdout)
		}
	}
}
