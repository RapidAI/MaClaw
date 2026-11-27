package cloudagent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var errJail = errors.New("路径超出工作区")

func jailRoot(dir, kind string) (string, error) {
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return "", err
	}
	if kind != KindCoding {
		return workspace, nil
	}
	// git.json is the authority, not the directory scan: it is written by
	// ConnectGit after a successful clone and lives OUTSIDE the jail, so a
	// scripted .git directory (a fake HEAD is trivial to mint via an
	// interpreter) can never shift the root or masquerade as the checkout.
	// Without a binding the bare workspace is the root — repo-less coding
	// is allowed; once bound, the checkout named by the binding is.
	if secret, err := readGit(dir); err == nil {
		if name, nameErr := repoDirName(secret.Repo); nameErr == nil {
			if info, statErr := os.Stat(filepath.Join(workspace, name, ".git", "HEAD")); statErr == nil && !info.IsDir() {
				return filepath.Join(workspace, name), nil
			}
		}
	}
	return workspace, nil
}

func resolveInJail(root, raw string) (string, error) {
	root = filepath.Clean(root)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return "", errJail
	}
	if secretBase(raw) {
		return "", errJail
	}
	joined := raw
	if !filepath.IsAbs(raw) {
		joined = filepath.Join(root, raw)
	}
	cleaned := filepath.Clean(joined)
	if !insideRoot(root, cleaned) || secretBase(cleaned) || gitComponent(root, cleaned) {
		return "", errJail
	}
	resolved, err := followReparse(root, cleaned, 0)
	if err != nil {
		return "", errJail
	}
	resolved = filepath.Clean(resolved)
	if !insideRoot(root, resolved) || secretBase(resolved) || gitComponent(root, resolved) {
		return "", errJail
	}
	return resolved, nil
}

const reparseLimit = 8

// followReparse walks each path element. filepath.EvalSymlinks does not
// follow a Windows directory junction, so a junction out of the jail would
// otherwise look like a normal child. A reparse target must stay inside.
func followReparse(root, cleaned string, depth int) (string, error) {
	if depth > reparseLimit {
		return "", errJail
	}
	rel, err := filepath.Rel(root, cleaned)
	if err != nil || rel == "." {
		return "", errJail
	}
	current := root
	parts := strings.Split(rel, string(os.PathSeparator))
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errJail
		}
		next := filepath.Join(current, part)
		info, statErr := os.Lstat(next)
		if statErr != nil {
			if !os.IsNotExist(statErr) {
				return "", errJail
			}
			return filepath.Join(append([]string{current}, parts[i:]...)...), nil
		}
		if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			target, linkErr := os.Readlink(next)
			if linkErr != nil || target == "" {
				return "", errJail
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(filepath.Dir(next), target)
			}
			target = filepath.Clean(target)
			if !insideRoot(root, target) || secretBase(target) || gitComponent(root, target) {
				return "", errJail
			}
			followed, ferr := followReparse(root, target, depth+1)
			if ferr != nil {
				return "", ferr
			}
			current = followed
			continue
		}
		current = next
	}
	return current, nil
}

// looseWorkspace is the pre-connect workspace when jail is a checkout inside
// it. A turn that captured the bare workspace keeps writing there after
// ConnectGit, and those files sit beside the checkout instead of inside it.
func looseWorkspace(instDir, jail string) (string, bool) {
	workspace := filepath.Clean(filepath.Join(instDir, "workspace"))
	jail = filepath.Clean(jail)
	if jail == workspace || !insideRoot(workspace, jail) {
		return "", false
	}
	return workspace, true
}

// strandedRegular is a regular file recorded relative to the old workspace
// root and left outside the checkout. rel is the canonical slash key.
func strandedRegular(instDir, jail, rel string) (string, os.FileInfo, bool) {
	workspace, ok := looseWorkspace(instDir, jail)
	if !ok {
		return "", nil, false
	}
	path, err := resolveInJail(workspace, rel)
	if err != nil {
		return "", nil, false
	}
	if filepath.Clean(path) == filepath.Clean(jail) || insideRoot(jail, path) {
		return "", nil, false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil, false
	}
	return path, info, true
}


func secretBase(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "git.json" || base == "llm.json"
}

// gitComponent reports whether the portion of path inside root holds a .git
// element. File tools and bash path args may not touch the object database:
// a written hook would execute inside the srv's own git ops, and the object
// store itself must stay out of reach. Only the part inside root is scanned
// so an ancestor named .git cannot brick the jail.
func gitComponent(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return true
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
}

// gitBase catches a bare ".git" argument — ".git/hooks" is a pathArg and
// resolves through the jail, but a lone ".git" has no separator and would
// otherwise reach the process (e.g. `env -C .git cat config`).
func gitBase(arg string) bool {
	return strings.EqualFold(filepath.Base(strings.Trim(arg, `"'`)), ".git")
}

func insideRoot(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func pathArg(arg string) bool {
	return strings.ContainsAny(arg, `/\`) || strings.Contains(arg, "..") || filepath.IsAbs(arg)
}

// pathFlagValueRe pulls the value out of options that smuggle a path inside
// the option text — `-C<path>` (the attached chdir flag shared by git, make
// and tar) and git's `--git-dir=`/`--work-tree=`/`--exec-path=`. They match
// anywhere in an argument, including inside `-Command` strings, where the
// plain pathArg scan sees only one long token.
var pathFlagValueRe = regexp.MustCompile(`(?:--git-dir=|--work-tree=|--exec-path=|-C)(\S+)`)

func rejectShellGit(root string, argv []string) bool {
	if encoded, ok := powershellEncoded(argv); ok {
		text, err := decodePowerShell(encoded)
		if err != nil || shellTextHasBannedGit(text) {
			return true
		}
		if commandFieldsBlocked(root, text) {
			return true
		}
	}
	for i := 0; i < len(argv); i++ {
		base := commandBase(argv[i])
		if (base == "bash" || base == "sh") && i+1 < len(argv) && isShellScriptFlag(argv[i+1]) {
			return true
		}
		if base == "git" && gitSubcommandBanned(argv[i+1:]) {
			return true
		}
		if base != "git" && shellTextHasBannedGit(argv[i]) {
			return true
		}
	}
	return false
}

func powershellEncoded(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	base := commandBase(argv[0])
	if base != "powershell" && base != "pwsh" {
		return "", false
	}
	for i := 1; i < len(argv); i++ {
		flag := strings.ToLower(argv[i])
		switch flag {
		case "-encodedcommand", "/encodedcommand", "-enc", "/enc":
			if i+1 >= len(argv) {
				return "", true
			}
			return argv[i+1], true
		}
	}
	return "", false
}

func decodePowerShell(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(raw) == 0 || len(raw)%2 != 0 {
		if err == nil {
			err = errors.New("编码命令无效")
		}
		return "", err
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	return string(utf16.Decode(units)), nil
}

func commandBase(arg string) string {
	base := strings.ToLower(filepath.Base(strings.Trim(arg, `"'`)))
	return strings.TrimSuffix(base, ".exe")
}

func isShellScriptFlag(arg string) bool {
	switch arg {
	case "-c", "-lc", "--command":
		return true
	default:
		return false
	}
}

// gitSubcommandBanned reports whether the first git subcommand is one the
// cloud shell is not allowed to run. Global options such as -c and -C are
// skipped, so `git -c user.email=x push` is still a push.
//
// The allowlist holds subcommands that are read-only in every form; the
// shell may inspect the repository but every write path belongs to the
// srv's own tools (git_commit / git_push). A denylist kept leaking
// plumbing bypasses — `git update-ref` + `git commit-tree` sidestep the
// commit ban, `git remote set-url` repoints the askpass token at an
// arbitrary host, `git send-pack` is push under another name, and
// `git credential fill` can spill credentials stored on the host.
func gitSubcommandBanned(args []string) bool {
	takesValue := map[string]bool{
		"-c": true, "-C": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--config-env": true, "--exec-path": true,
		"--super-prefix": true, "--list-cmds": true,
	}
	readOnly := map[string]bool{
		"status": true, "diff": true, "diff-index": true, "diff-tree": true,
		"diff-files": true, "log": true, "show": true, "show-branch": true,
		"whatchanged": true, "blame": true, "annotate": true, "grep": true,
		"rev-parse": true, "rev-list": true, "name-rev": true,
		"merge-base": true, "cherry": true, "describe": true, "shortlog": true,
		"range-diff": true, "ls-files": true, "ls-tree": true,
		"cat-file": true, "for-each-ref": true, "show-ref": true,
		"count-objects": true, "fsck": true, "verify-commit": true,
		"verify-tag": true, "verify-pack": true, "check-ignore": true,
		"check-attr": true, "check-mailmap": true, "check-ref-format": true,
		"var": true, "help": true, "version": true, "format-patch": true,
		"fast-export": true, "mailinfo": true, "mailsplit": true,
		"stripspace": true, "interpret-trailers": true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return !readOnly[strings.ToLower(args[i+1])]
			}
			return false
		}
		if strings.HasPrefix(arg, "-") {
			if takesValue[arg] {
				i++
			}
			continue
		}
		return !readOnly[strings.ToLower(arg)]
	}
	return false
}

func normalizeShell(text string) string {
	text = strings.ReplaceAll(text, "`\r\n", "")
	text = strings.ReplaceAll(text, "`\n", "")
	var b strings.Builder
	b.Grow(len(text))
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '`' && i+1 < len(runes) {
			i++
			b.WriteRune(runes[i])
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func shellTextHasBannedGit(text string) bool {
	text = normalizeShell(text)
	if !strings.Contains(strings.ToLower(text), "git") {
		return false
	}
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return r == ';' || r == '&' || r == '|' || r == '\n'
	}) {
		parts := strings.Fields(field)
		for i := range parts {
			if commandBase(parts[i]) == "git" && gitSubcommandBanned(parts[i+1:]) {
				return true
			}
		}
	}
	return false
}

// bashTimeout bounds one shell command; the turn context can cancel sooner.
// Without a ceiling a hung command would pin the turn goroutine and the
// child process forever — the book's interrupt only cancels the context.
const bashTimeout = 5 * time.Minute

// RunBash executes argv with the working directory pinned to the jail root.
// Paths that escape the root, or that name git.json or llm.json, are refused
// before the process starts. Cancelling ctx, or hitting bashTimeout, kills
// the whole process tree — a shell child must not outlive its turn.
func flagValueOutsideJail(root, text string) bool {
	for _, match := range pathFlagValueRe.FindAllStringSubmatch(text, -1) {
		if _, err := resolveInJail(root, match[1]); err != nil {
			return true
		}
	}
	return false
}

// fieldBlocked applies the jail checks to one whitespace-separated token —
// an argv element or a word inside a command string. Quotes are trimmed for
// the name checks since a shell would strip them, and the path check runs
// on the trimmed field so `cat '/etc/passwd'` inside a -Command string
// cannot hide behind quote characters.
func fieldBlocked(root, field string) bool {
	if flagValueOutsideJail(root, field) {
		return true
	}
	trimmed := strings.Trim(field, `"'`)
	if secretBase(trimmed) || gitBase(trimmed) || pathArg(field) {
		_, err := resolveInJail(root, trimmed)
		return err != nil
	}
	return false
}

// commandFieldsBlocked splits a command string the way the invoked shell
// will and jails every token: `cd .. & type git.json` inside a single argv
// element is otherwise one opaque blob the per-argument checks pass as a
// harmless relative name.
func commandFieldsBlocked(root, text string) bool {
	for _, field := range strings.Fields(text) {
		if fieldBlocked(root, field) {
			return true
		}
	}
	return false
}

func RunBash(ctx context.Context, root string, argv []string, secrets ...string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, errors.New("缺少命令")
	}
	if rejectShellGit(root, argv) {
		return nil, errors.New("shell 只允许只读 git 命令(status/diff/log/show 等),提交推送请用工具")
	}
	encoded, hasEncoded := powershellEncoded(argv)
	for _, arg := range argv {
		if hasEncoded && encoded != "" && arg == encoded {
			continue
		}
		if commandFieldsBlocked(root, arg) {
			return nil, errJail
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, bashTimeout)
	defer cancel()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	cmd.Env = bashEnv(secrets...)
	tool.PrepareCommandForTreeKill(cmd)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	err := tool.WaitCommandWithContext(runCtx, cmd)
	if errors.Is(err, context.DeadlineExceeded) {
		err = errors.New("命令超过 5 分钟，已终止")
	}
	return capBashOutput(out.Bytes()), err
}

const bashOutputCap = 32 << 10

func capBashOutput(out []byte) []byte {
	if len(out) <= bashOutputCap {
		return out
	}
	note := []byte("\n…输出已截断")
	kept := append([]byte{}, out[:bashOutputCap]...)
	return append(kept, note...)
}

func secretValues(dir string) []string {
	var out []string
	if raw, err := os.ReadFile(gitPath(dir)); err == nil {
		var secret gitSecret
		if json.Unmarshal(raw, &secret) == nil {
			if token := strings.TrimSpace(secret.Token); token != "" {
				out = append(out, token)
			}
		}
	}
	if raw, err := os.ReadFile(filepathJoinLLM(dir)); err == nil {
		var secret CustomLLM
		if json.Unmarshal(raw, &secret) == nil {
			if key := strings.TrimSpace(secret.Key); key != "" {
				out = append(out, key)
			}
		}
	}
	return out
}

func valueHasSecret(value string, secrets []string) bool {
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if len(secret) < 8 {
			continue
		}
		if strings.Contains(value, secret) {
			return true
		}
	}
	return false
}

func bashEnv(secrets ...string) []string {
	drop := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_INDEX_FILE": true,
		"GIT_OBJECT_DIRECTORY": true, "GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GITHUB_TOKEN": true, "GITLAB_TOKEN": true,
		"MACLAW_ADMIN_SECRET": true,
	}
	var out []string
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if drop[name] || strings.Contains(strings.ToLower(name), "llm") && strings.Contains(strings.ToLower(name), "key") {
			continue
		}
		if valueHasSecret(value, secrets) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
