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
	"strings"
	"time"
	"unicode/utf16"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

var errJail = errors.New("路径超出工作区")

func jailRoot(dir, kind string) (string, error) {
	workspace := filepath.Join(dir, "workspace")
	if kind != KindCoding {
		if err := os.MkdirAll(workspace, 0o755); err != nil {
			return "", err
		}
		return workspace, nil
	}
	name, ok := checkoutName(workspace)
	if !ok {
		return "", errors.New("还没有检出代码库")
	}
	return filepath.Join(workspace, name), nil
}

func checkoutName(workspace string) (string, bool) {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		gitPath := filepath.Join(workspace, entry.Name(), ".git")
		info, err := os.Stat(gitPath)
		if err == nil && info.IsDir() {
			return entry.Name(), true
		}
	}
	return "", false
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
	if !insideRoot(root, cleaned) || secretBase(cleaned) {
		return "", errJail
	}
	resolved, err := followReparse(root, cleaned, 0)
	if err != nil {
		return "", errJail
	}
	resolved = filepath.Clean(resolved)
	if !insideRoot(root, resolved) || secretBase(resolved) {
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
			if !insideRoot(root, target) || secretBase(target) {
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

func secretBase(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "git.json" || base == "llm.json"
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

func rejectShellGit(argv []string) bool {
	if encoded, ok := powershellEncoded(argv); ok {
		text, err := decodePowerShell(encoded)
		if err != nil || shellTextHasBannedGit(text) {
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
func gitSubcommandBanned(args []string) bool {
	takesValue := map[string]bool{
		"-c": true, "-C": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--config-env": true, "--exec-path": true,
		"--super-prefix": true, "--list-cmds": true,
	}
	banned := map[string]bool{
		"clone": true, "fetch": true, "pull": true, "push": true, "ls-remote": true,
		"add": true, "commit": true, "checkout": true, "switch": true, "reset": true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return banned[strings.ToLower(args[i+1])]
			}
			return false
		}
		if strings.HasPrefix(arg, "-") {
			if takesValue[arg] {
				i++
			}
			continue
		}
		return banned[strings.ToLower(arg)]
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
func RunBash(ctx context.Context, root string, argv []string, secrets ...string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, errors.New("缺少命令")
	}
	if rejectShellGit(argv) {
		return nil, errors.New("shell 不能执行 git")
	}
	encoded, hasEncoded := powershellEncoded(argv)
	for _, arg := range argv {
		if hasEncoded && encoded != "" && arg == encoded {
			continue
		}
		if secretBase(arg) || pathArg(arg) {
			if _, err := resolveInJail(root, arg); err != nil {
				return nil, err
			}
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
