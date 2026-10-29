package codingruntime

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
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
	// Cleanup inside the digest group uses ';'. The end marker must still be a
	// later conjunct, or a failed status would be framed as a clean tree.
	if strings.Contains(cmd, "; printf") {
		t.Fatalf("end marker is not gated on the digest: %s", cmd)
	}
	steps := strings.Split(cmd, " && ")
	if len(steps) != 4 {
		t.Fatalf("steps=%q", steps)
	}
	if !strings.Contains(steps[0], "git -C '/home/testprj' rev-parse HEAD") || !strings.Contains(steps[2], "status --porcelain=v1 --untracked-files=all") || !strings.Contains(steps[2], "hash-object --stdin") || !strings.Contains(steps[2], "mktemp -d") || !strings.Contains(steps[2], "probe_parent=/var/tmp") || !strings.Contains(steps[2], "--no-optional-locks") {
		t.Fatalf("command=%s", cmd)
	}
	if strings.Contains(steps[2], "status --porcelain=v1 --untracked-files=all &&") {
		t.Fatalf("porcelain is still shipped as the attestation: %s", cmd)
	}
	if !strings.Contains(steps[1], `printf '\n%s\n' 'BEGIN_marker'`) || !strings.HasSuffix(steps[3], `'END_marker'`) {
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

func TestRemoteGitProbeCommandIsValidPOSIX(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to syntax-check the remote probe")
	}
	cmd, err := RemoteGitProbeCommand("/tmp/maclaw-probe-syntax", "BEGIN_marker", "END_marker")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", "-n", "-c", cmd).CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n: %v\n%s\n%s", err, out, cmd)
	}
}

func TestRemoteWorkspaceStatusDigestRejectsPorcelain(t *testing.T) {
	const emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	got, err := RemoteWorkspaceStatusDigest("\n" + emptyBlob + "\n")
	if err != nil || got != "gitblob:"+emptyBlob {
		t.Fatalf("digest=%q err=%v", got, err)
	}
	for _, body := range []string{"", "\n", " M real.go\n", emptyBlob + "\nextra\n", "E69DE29BB2D1D6434B8B29AE775AD8C2E48C5391", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"} {
		if _, err := RemoteWorkspaceStatusDigest(body); err == nil {
			t.Fatalf("accepted non-attestation %q", body)
		}
	}
}

func TestRemoteGitProbeAttestationDoesNotShipStatus(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required to run the remote probe")
	}
	setup, err := exec.Command("bash", "-c", `command -v git >/dev/null && command -v mktemp >/dev/null && dir=$(mktemp -d) && git -C "$dir" init -q && git -C "$dir" -c user.name=MaClaw -c user.email=maclaw@localhost -c commit.gpgsign=false commit --allow-empty -q -m init && printf 'secret\n' > "$dir/secret-untracked.txt" && printf '%s' "$dir"`).Output()
	if err != nil {
		t.Skipf("bash git fixture unavailable: %v", err)
	}
	dir := strings.TrimSpace(string(setup))
	t.Cleanup(func() {
		_ = exec.Command("bash", "-c", `rm -rf -- "$1"`, "bash", dir).Run()
	})
	const begin = "BEGIN_marker"
	const end = "END_marker"
	cmd, err := RemoteGitProbeCommand(dir, begin, end)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	probe := exec.Command("bash", "-c", cmd)
	probe.Stdout = &stdout
	probe.Stderr = &stderr
	if err := probe.Run(); err != nil {
		t.Fatalf("probe: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	text := stdout.String()
	if strings.Contains(text, "secret-untracked") {
		t.Fatalf("probe shipped the status listing:\n%s", text)
	}
	bodyStart := strings.LastIndex(text, begin)
	if bodyStart < 0 {
		t.Fatalf("start marker missing: %s", text)
	}
	bodyEnd := strings.Index(text[bodyStart+len(begin):], end)
	if bodyEnd < 0 {
		t.Fatalf("end marker missing: %s", text)
	}
	digest, err := RemoteWorkspaceStatusDigest(text[bodyStart+len(begin) : bodyStart+len(begin)+bodyEnd])
	if err != nil {
		t.Fatalf("digest: %v\n%s", err, text)
	}
	status, err := exec.Command("bash", "-c", `git -C "$1" --no-optional-locks status --porcelain=v1 --untracked-files=all`, "bash", dir).Output()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	// The login cwd for this test is this repository. The digest must still be
	// the SHA-1 blob of the status bytes, not an id derived from this repo.
	if want := "gitblob:" + gitBlobSHA1(status); digest != want {
		t.Fatalf("digest=%s want %s\nstdout=%s", digest, want, text)
	}
	leaked, err := exec.Command("bash", "-c", `find "$1" -name 'maclaw-wsd*' -print`, "bash", dir).Output()
	if err != nil {
		t.Fatalf("find scratch: %v", err)
	}
	if strings.TrimSpace(string(leaked)) != "" {
		t.Fatalf("scratch directory leaked into the work tree:\n%s", leaked)
	}

	missing, err := RemoteGitProbeCommand("/tmp/maclaw-missing-probe-dir-not-real", begin, end)
	if err != nil {
		t.Fatal(err)
	}
	var failed bytes.Buffer
	bad := exec.Command("bash", "-c", missing)
	bad.Stdout = &failed
	if err := bad.Run(); err == nil {
		t.Fatal("missing repository was attested")
	}
	if strings.Contains(failed.String(), end) {
		t.Fatalf("failed probe still printed the end marker:\n%s", failed.String())
	}
}

func gitBlobSHA1(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	_, _ = h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
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
