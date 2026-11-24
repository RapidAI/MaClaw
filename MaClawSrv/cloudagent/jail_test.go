package cloudagent

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestJailRejectsParentGitJSONAndSymlink(t *testing.T) {
	inst := t.TempDir()
	repo := filepath.Join(inst, "workspace", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	const loadedToken = "loaded-forge-token-91c4"
	const loadedKey = "loaded-provider-key-77ab"
	secret := filepath.Join(inst, "git.json")
	if err := os.WriteFile(secret, []byte(`{"token":"`+loadedToken+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, "llm.json"), []byte(`{"url":"https://llm.example/v1","key":"`+loadedKey+`","model":"m"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := jailRoot(inst, KindCoding)
	if err != nil || root != repo {
		t.Fatalf("jail root = %s err=%v", root, err)
	}
	if _, err := RunBash(context.Background(), root, []string{"cat", "../git.json"}); err == nil {
		t.Fatal("cat ../git.json was allowed")
	}
	if _, err := resolveInJail(root, filepath.Join("..", "llm.json")); err == nil {
		t.Fatal("llm.json via .. was allowed")
	}
	link := filepath.Join(root, "leak")
	if err := os.Symlink(secret, link); err != nil {
		// A file symlink needs an extra privilege on this Windows host.
		// A directory junction does not, and EvalSymlinks follows it the
		// same way, so the escape check still runs on a real reparse point.
		if out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, inst).CombinedOutput(); jerr != nil {
			t.Fatalf("symlink %v; junction %v %s", err, jerr, out)
		}
		if got, rerr := resolveInJail(root, filepath.Join("leak", "notes.txt")); rerr == nil {
			t.Fatalf("junction resolved to %s", got)
		}
	} else if got, rerr := resolveInJail(root, "leak"); rerr == nil {
		t.Fatalf("symlink resolved to %s", got)
	}
	t.Setenv("GITHUB_TOKEN", loadedToken)
	t.Setenv("MACLAW_ADMIN_SECRET", "admin-secret-value")
	t.Setenv("SIDE_CHANNEL", loadedToken+" "+loadedKey)
	for _, kv := range bashEnv(secretValues(inst)...) {
		if strings.Contains(kv, loadedToken) || strings.Contains(kv, loadedKey) || strings.HasPrefix(kv, "GITHUB_TOKEN=") || strings.HasPrefix(kv, "MACLAW_ADMIN_SECRET=") || strings.HasPrefix(kv, "SIDE_CHANNEL=") {
			t.Fatalf("secret env leaked: %s", kv)
		}
	}
	out, bashErr := RunBash(context.Background(), root, []string{"powershell", "-NoProfile", "-Command", "Write-Output $env:SIDE_CHANNEL"}, secretValues(inst)...)
	if bashErr != nil {
		t.Fatal(bashErr)
	}
	if strings.Contains(string(out), loadedToken) || strings.Contains(string(out), loadedKey) {
		t.Fatalf("process env leaked: %s", out)
	}
	sessionRoot, err := jailRoot(inst, KindSession)
	if err != nil || sessionRoot != filepath.Join(inst, "workspace") {
		t.Fatalf("session root = %s err=%v", sessionRoot, err)
	}
}

func TestShellRejectsWrappedGit(t *testing.T) {
	root := t.TempDir()
	denied := [][]string{
		{"git", "-c", "user.email=ada@example.com", "push"},
		{"git.exe", "push"},
		{"bash.exe", "-c", "git push"},
		{"sh", "-c", "echo hi"},
		{"powershell", "-NoProfile", "-Command", "git push origin main"},
		{"powershell", "-NoProfile", "-Command", "git -c user.email=ada@example.com push"},
		{"powershell", "-NoProfile", "-Command", "g`it push"},
		{"powershell", "-EncodedCommand", encodePowerShellCommand("git push")},
	}
	for _, argv := range denied {
		if _, err := RunBash(context.Background(), root, argv); err == nil || !strings.Contains(err.Error(), "shell 不能执行 git") {
			t.Fatalf("argv %q err=%v", argv, err)
		}
	}
	if rejectShellGit([]string{"git", "-C", "src", "status"}) {
		t.Fatal("git status was rejected")
	}
	if rejectShellGit([]string{"powershell", "-NoProfile", "-Command", "Write-Output ok"}) {
		t.Fatal("plain powershell was rejected")
	}
	if rejectShellGit([]string{"powershell", "-EncodedCommand", encodePowerShellCommand("Write-Output ok")}) {
		t.Fatal("benign encoded command was rejected")
	}
}

func TestBashOutputIsCapped(t *testing.T) {
	root := t.TempDir()
	out, err := RunBash(context.Background(), root, []string{"powershell", "-NoProfile", "-Command", "Write-Output ('A'*40000)"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "输出已截断") || len(out) > bashOutputCap+len("\n…输出已截断") {
		t.Fatalf("len=%d tail=%q", len(out), tail(out))
	}
}

func TestBashCancelledContextKillsProcess(t *testing.T) {
	root := t.TempDir()
	argv := []string{"sleep", "300"}
	if runtime.GOOS == "windows" {
		argv = []string{"powershell", "-NoProfile", "-Command", "Start-Sleep -Seconds 300"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := RunBash(ctx, root, argv)
	if err == nil {
		t.Fatal("cancelled command should fail")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Fatalf("cancelled command ran %s, expected prompt kill", elapsed)
	}
}

func encodePowerShellCommand(text string) string {
	raw := utf16.Encode([]rune(text))
	buf := make([]byte, len(raw)*2)
	for i, unit := range raw {
		binary.LittleEndian.PutUint16(buf[i*2:], unit)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func tail(out []byte) string {
	if len(out) > 24 {
		out = out[len(out)-24:]
	}
	return string(out)
}
