package codingruntime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
)

const (
	remoteGitBaselineMarkerPrefix = "MACLAW_GIT_BASELINE_"
	remoteGitBaselineOKSuffix     = "_OK"
	remoteGitBaselineInitSuffix   = "_INITIALIZED"
	remoteGitBaselineFailSuffix   = "_FAIL"
)

// RemoteGitBaselineRequest is the host-executed setup step that makes a remote
// project pass the read-only Git workspace probe. The probe itself stays
// read-only; hosts must run this command on the already-verified SSH session
// before Runner starts a gated writer.
type RemoteGitBaselineRequest struct {
	Command           string
	OKMarker          string
	InitializedMarker string
	FailMarker        string
}

// NewRemoteGitBaselineRequest builds a single-line POSIX script that creates
// the remote project directory if needed and, when HEAD does not already
// resolve, runs `git init` plus an empty baseline commit. It never cds the
// interactive shell, never uses `exit` (which would kill a shared PTY login),
// and never adds existing files. A per-invocation nonce keeps PTY command echo
// from being mistaken for success.
func NewRemoteGitBaselineRequest(projectPath string) (*RemoteGitBaselineRequest, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return nil, fmt.Errorf("remote git baseline: empty project path")
	}
	if !strings.HasPrefix(projectPath, "/") {
		return nil, fmt.Errorf("remote git baseline: project path must be an absolute POSIX path")
	}
	if strings.ContainsAny(projectPath, "\x00\n\r") {
		return nil, fmt.Errorf("remote git baseline: project path contains invalid characters")
	}
	projectPath = path.Clean(projectPath)
	if projectPath == "/" {
		return nil, fmt.Errorf("remote git baseline: project path must not be the filesystem root")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("remote git baseline: generate marker: %w", err)
	}
	base := remoteGitBaselineMarkerPrefix + hex.EncodeToString(nonce[:])
	req := &RemoteGitBaselineRequest{
		OKMarker:          base + remoteGitBaselineOKSuffix,
		InitializedMarker: base + remoteGitBaselineInitSuffix,
		FailMarker:        base + remoteGitBaselineFailSuffix,
	}
	quoted := posixShellSingleQuote(projectPath)
	gitC := "GIT_TERMINAL_PROMPT=0 git -C " + quoted
	req.Command = strings.Join([]string{
		"mkdir -p -- " + quoted,
		"if " + gitC + " rev-parse --verify HEAD >/dev/null 2>&1; then printf '" + req.OKMarker + "\\n'",
		"elif command -v git >/dev/null 2>&1 && " + gitC + " -c init.defaultBranch=master init && " + gitC + " -c user.name=MaClaw -c user.email=maclaw@localhost -c commit.gpgsign=false commit --allow-empty -m 'chore: initialize workspace baseline'; then printf '" + req.InitializedMarker + "\\n'",
		"else printf '" + req.FailMarker + "\\n'",
		"fi",
	}, "; ")
	return req, nil
}

// Accepted reports whether the remote command output contains a complete
// success marker line. Matching whole lines ignores PTY echo of the script,
// which contains the same marker text inside a printf argument.
func (r *RemoteGitBaselineRequest) Accepted(output string) bool {
	return r.Result(output) == nil
}

// Result is nil when the remote host reported a usable Git HEAD. Failure
// includes the fail-marker line when present so hosts can log a bounded reason
// without persisting the raw PTY transcript.
func (r *RemoteGitBaselineRequest) Result(output string) error {
	if r == nil {
		return fmt.Errorf("remote git baseline request is unavailable")
	}
	sawFail := false
	for _, line := range splitRemoteOutputLines(output) {
		if line == r.OKMarker || line == r.InitializedMarker {
			return nil
		}
		if line == r.FailMarker {
			sawFail = true
		}
	}
	if sawFail {
		return fmt.Errorf("remote git baseline: git is unavailable or could not create an empty HEAD")
	}
	return fmt.Errorf("remote git baseline: command did not report a usable HEAD")
}

func splitRemoteOutputLines(output string) []string {
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func posixShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
