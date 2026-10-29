package codingruntime

import (
	"fmt"
	"path"
	"strings"
)

// RemoteGitBaselineRequest is the host-executed setup step that makes a remote
// project pass the read-only Git workspace probe. The probe itself stays
// read-only. Hosts run Command on a non-interactive SSH exec channel of the
// already-verified connection — the same shape as `ssh host <script>` — and
// accept the result only through ExecResult.
//
// The script must not be written into the shared login PTY. That shell echoes
// its input into the same stream a caller would treat as the result, keeps
// options such as `set -e`, and has no process exit status. A failing git
// command there exits the verified session, after which runtime correctly
// refuses to reconnect.
type RemoteGitBaselineRequest struct {
	Command string
}

// remoteWorkDirMarker is printed only when the declared directory cannot be
// created by the SSH login and the script continues in that user's home.
// Git's own stdout is discarded so this line is the only success marker.
const remoteWorkDirMarker = "MACLAW_REMOTE_WORKDIR="

// NewRemoteGitBaselineRequest builds a POSIX script that creates the remote
// project directory if needed and, when HEAD does not already resolve, runs
// `git init` plus an empty baseline commit. It never cds, and it never adds
// existing files. Success is exit status 0. Git's own stderr is left intact
// so a failure is the remote git error.
//
// A login that cannot create the declared path (for example znsoft cannot
// mkdir /home/testprj) continues in $HOME/<basename> and prints
// remoteWorkDirMarker. An existing path that is not a writable directory is
// left untouched: relocating would fork a second tree away from files the
// user can already see.
func NewRemoteGitBaselineRequest(projectPath string) (*RemoteGitBaselineRequest, error) {
	projectPath, err := cleanRemoteProjectPath(projectPath, "remote git baseline")
	if err != nil {
		return nil, err
	}
	quoted := posixShellSingleQuote(projectPath)
	quotedLeaf := posixShellSingleQuote(path.Base(projectPath))
	gitC := `GIT_TERMINAL_PROMPT=0 git -C "$use"`
	command := strings.Join([]string{
		"dir=" + quoted,
		"leaf=" + quotedLeaf,
		"use=$dir",
		`if [ -d "$dir" ] && [ -w "$dir" ]; then`,
		"  use=$dir",
		`elif [ -e "$dir" ]; then`,
		`  printf '%s\n' "not a writable directory: $dir" >&2`,
		"  exit 1",
		`elif mkdir -p -- "$dir" 2>/dev/null && [ -w "$dir" ]; then`,
		"  use=$dir",
		"else",
		"  home=${HOME-}",
		"  alt=",
		`  case "$home" in`,
		`    /*) alt="${home%/}/$leaf" ;;`,
		"  esac",
		`  if [ -n "$alt" ] && [ "$alt" != "$dir" ] && mkdir -p -- "$alt" && [ -w "$alt" ]; then`,
		"    use=$alt",
		"  else",
		`    mkdir -p -- "$dir"`,
		"    exit 1",
		"  fi",
		"fi",
		"if " + gitC + " rev-parse --verify HEAD >/dev/null 2>&1; then",
		`  if [ "$use" != "$dir" ]; then printf '%s\n' "` + remoteWorkDirMarker + `$use"; fi`,
		"  exit 0",
		"fi",
		"command -v git >/dev/null 2>&1 || { printf '%s\\n' 'git not found' >&2; exit 127; }",
		gitC + " -c init.defaultBranch=master init >/dev/null || exit 1",
		gitC + " -c user.name=MaClaw -c user.email=maclaw@localhost -c commit.gpgsign=false commit --allow-empty --no-verify -m 'chore: initialize workspace baseline' >/dev/null || exit 1",
		`if [ "$use" != "$dir" ]; then printf '%s\n' "` + remoteWorkDirMarker + `$use"; fi`,
		"exit 0",
	}, "\n")
	return &RemoteGitBaselineRequest{Command: command}, nil
}

// EffectiveWorkDir is the directory the baseline script actually prepared.
// Stdout without the relocation marker means the declared path was used.
// A marker whose basename differs from the declared project is rejected so a
// noisy or hostile exec channel cannot retarget the writer.
func (r *RemoteGitBaselineRequest) EffectiveWorkDir(stdout, requested string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("remote git baseline request is unavailable")
	}
	requested, err := cleanRemoteProjectPath(requested, "remote git baseline")
	if err != nil {
		return "", err
	}
	found := ""
	for _, line := range splitRemoteOutputLines(stdout) {
		if strings.HasPrefix(line, remoteWorkDirMarker) {
			found = strings.TrimSpace(strings.TrimPrefix(line, remoteWorkDirMarker))
		}
	}
	if found == "" {
		return requested, nil
	}
	found, err = cleanRemoteProjectPath(found, "remote git baseline")
	if err != nil || path.Base(found) != path.Base(requested) {
		return "", fmt.Errorf("remote git baseline: relocated workdir is invalid")
	}
	return found, nil
}

// ExecResult is nil only when the exec channel reported exit status 0.
// A non-zero status includes one line of the remote git error when the
// channel captured it. Stderr wins over stdout; both are already command
// output, not a transcript of the script.
func (r *RemoteGitBaselineRequest) ExecResult(exitCode int, stdout, stderr string) error {
	if r == nil {
		return fmt.Errorf("remote git baseline request is unavailable")
	}
	if exitCode == 0 {
		return nil
	}
	detail := RemoteFailureLine(stderr)
	if detail == "" {
		detail = RemoteFailureLine(stdout)
	}
	if detail == "" {
		return fmt.Errorf("remote git baseline: git is unavailable or could not create an empty HEAD (exit %d)", exitCode)
	}
	return fmt.Errorf("remote git baseline: git is unavailable or could not create an empty HEAD: %s", detail)
}

// RemoteGitProbeCommand is the read-only exec-channel probe. Each step is
// joined with &&, so a failing rev-parse or status does not print the closing
// marker. A semicolon chain would still print that marker, and the parser
// would treat the empty body as a clean tree.
//
// The attestation is a fixed-size digest, not the porcelain listing. The exec
// channel keeps only a bounded prefix of stdout, and the end marker is written
// after the payload. Shipping `git status --untracked-files=all` makes any
// tree whose listing exceeds that prefix — a home directory, a dependency
// folder — look like a missing baseline even though git itself succeeded.
// The digest is the SHA-1 git blob id of those exact status bytes, computed
// on the host that has them. hash-object is not passed -w, so it does not
// store an object. --no-optional-locks keeps status from refreshing the index.
func RemoteGitProbeCommand(workDir, markerStart, markerEnd string) (string, error) {
	workDir, err := cleanRemoteProjectPath(workDir, "remote git probe")
	if err != nil {
		return "", err
	}
	if !validRemoteProbeMarker(markerStart) || !validRemoteProbeMarker(markerEnd) || markerStart == markerEnd {
		return "", fmt.Errorf("remote git probe: markers are invalid")
	}
	quoted := posixShellSingleQuote(workDir)
	gitC := "git -C " + quoted
	// Scratch lives outside the work tree. A file under $TMPDIR, or under /tmp
	// when the project is /tmp, would be an untracked path and every probe
	// would differ. hash-object then runs in that empty directory with a
	// ceiling, reading stdin: the login repo cannot change the hash algorithm,
	// apply a clean filter, or reject the digest. Semicolons stay inside this
	// group so a failed status still deletes the scratch directory. The end
	// marker is a later && step, so that failure cannot print it.
	digest := "{ dir=" + quoted + "; probe_parent=/tmp; sample=$probe_parent/maclaw-ws; rest=${sample#\"$dir\"/}; " +
		"if [ \"$dir\" = \"$probe_parent\" ] || [ \"$rest\" != \"$sample\" ]; then probe_parent=/var/tmp; fi; " +
		"probe_hold=$(mktemp -d \"$probe_parent/maclaw-wsd.XXXXXX\") || exit 1; " +
		gitC + " --no-optional-locks status --porcelain=v1 --untracked-files=all > \"$probe_hold/status\"; probe_st=$?; " +
		"if [ \"$probe_st\" -eq 0 ]; then GIT_CEILING_DIRECTORIES=\"$probe_hold\" git -C \"$probe_hold\" hash-object --stdin < \"$probe_hold/status\"; probe_st=$?; fi; " +
		"rm -rf -- \"$probe_hold\"; [ \"$probe_st\" -eq 0 ]; }"
	return strings.Join([]string{
		gitC + " rev-parse HEAD",
		"printf '\\n%s\\n' " + posixShellSingleQuote(markerStart),
		digest,
		"printf '\\n%s\\n' " + posixShellSingleQuote(markerEnd),
	}, " && "), nil
}

// RemoteWorkspaceStatusDigest accepts only the fixed-size attestation printed
// between the probe markers. A porcelain listing is rejected: treating it as
// the baseline is what made a truncated capture indistinguishable from a
// failed read. hash-object prints one object name and does not write it.
func RemoteWorkspaceStatusDigest(body string) (string, error) {
	lines := splitRemoteOutputLines(body)
	if len(lines) != 1 || !remoteGitObjectName(lines[0]) {
		return "", fmt.Errorf("remote read-only git probe returned no status digest")
	}
	return "gitblob:" + lines[0], nil
}

func remoteGitObjectName(value string) bool {
	// The probe hashes outside any repository, so the id is SHA-1. A 64-hex
	// SHA-256 id means a surrounding repo leaked into the digest; accepting it
	// would let two probes of one tree disagree.
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// RemoteProbeExecError reports a non-zero probe. Git writes the reason to
// stderr; a partial stdout frame is not a workspace result.
func RemoteProbeExecError(exitCode int, stderr string) error {
	if detail := RemoteFailureLine(stderr); detail != "" {
		return fmt.Errorf("remote read-only git probe failed: %s", detail)
	}
	return fmt.Errorf("remote read-only git probe failed (exit %d)", exitCode)
}

// RemoteFailureLine is the remote command's failure, not its help text.
// Init hints are printed before fatal:, but messages such as dubious
// ownership print the fatal line first and the remediation after it.
// Prefer the last fatal/error line, then the last line of any kind.
func RemoteFailureLine(output string) string {
	lines := splitRemoteOutputLines(output)
	var last, failure string
	for _, line := range lines {
		detail := sanitizeRemoteBaselineDetail(line)
		if detail == "" {
			continue
		}
		last = detail
		if remoteFailurePrefix(detail) {
			failure = detail
		}
	}
	if failure != "" {
		return failure
	}
	return last
}

func remoteFailurePrefix(line string) bool {
	return strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") || strings.HasPrefix(line, "git:")
}

func sanitizeRemoteBaselineDetail(detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range detail {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
		n++
		if n >= 160 {
			break
		}
	}
	return strings.TrimSpace(b.String())
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

func cleanRemoteProjectPath(projectPath, what string) (string, error) {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return "", fmt.Errorf("%s: empty project path", what)
	}
	if !strings.HasPrefix(projectPath, "/") {
		return "", fmt.Errorf("%s: project path must be an absolute POSIX path", what)
	}
	if strings.ContainsAny(projectPath, "\x00\n\r") {
		return "", fmt.Errorf("%s: project path contains invalid characters", what)
	}
	projectPath = path.Clean(projectPath)
	if projectPath == "/" {
		return "", fmt.Errorf("%s: project path must not be the filesystem root", what)
	}
	return projectPath, nil
}

func validRemoteProbeMarker(marker string) bool {
	if marker == "" || len(marker) > 128 {
		return false
	}
	for _, r := range marker {
		switch {
		case r >= '0' && r <= '9', r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r == '_':
		default:
			return false
		}
	}
	return true
}

func posixShellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
