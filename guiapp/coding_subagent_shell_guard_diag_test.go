package guiapp

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodingShellGuardRejectsRawSSH(t *testing.T) {
	cb := &codingSubAgentCallbacks{}
	for _, command := range []string{
		`ssh -p 99 -o BatchMode=yes -o StrictHostKeyChecking=no znsoft@home.rapidai.tech true`,
		`& "C:\Windows\System32\OpenSSH\ssh.exe" -p 99 -o BatchMode=yes znsoft@home.rapidai.tech true`,
		`& "C:\Program Files\OpenSSH\ssh.exe" znsoft@home.rapidai.tech true`,
		`& "ssh" znsoft@home.rapidai.tech true`,
		`& $env:WINDIR\System32\OpenSSH\ssh.exe znsoft@home.rapidai.tech true`,
		`scp app.tgz root@10.58.34.75:/tmp/`,
	} {
		if msg := cb.guardCodingShellCommand(command, "."); msg == "" {
			t.Fatalf("raw ssh through the coding bash tool must be rejected: %s", command)
		}
	}
	for _, command := range []string{
		`Get-ChildItem $env:USERPROFILE\.ssh -Name`,
		`if (Test-Path "C:\Windows\System32\OpenSSH\ssh.exe") { "found" }`,
		`go test ./...`,
	} {
		if msg := cb.guardCodingShellCommand(command, "."); msg != "" {
			t.Fatalf("non-invocation command must pass: %s -> %s", command, msg)
		}
	}
}

func TestCodingRemoteShellGuardRejectsNestedSSH(t *testing.T) {
	cb := &codingSubAgentCallbacks{}
	if msg := cb.guardCodingRemoteShellCommand(`ssh other-host uptime`); msg == "" {
		t.Fatal("a nested ssh hop through the managed remote shell must be rejected")
	}
	if msg := cb.guardCodingRemoteShellCommand(`git status --short`); msg != "" {
		t.Fatalf("ordinary remote command must pass: %s", msg)
	}
}

func TestCodingPowerShellWrapperKeepsObjectOutput(t *testing.T) {
	wrapper := wrapCodingPowerShellCommand(`Get-ChildItem -Force | Select-Object Mode,Length,Name`)
	if !strings.Contains(wrapper, `$host.SetShouldExit($exitCode)`) {
		t.Fatal("wrapper must set the exit code without exit, which drops pending object formatting")
	}
	if strings.Contains(wrapper, "exit $nativeExit") || strings.Contains(wrapper, "\nexit 0") {
		t.Fatal("wrapper still uses exit, which loses Select-Object output")
	}
	if strings.Contains(wrapper, "output was likely consumed by a pipeline filter") {
		t.Fatal("wrapper must not guess that a pipeline filter swallowed the output")
	}
}

func TestCodingPowerShellErrorsAreSilenced(t *testing.T) {
	for _, command := range []string{
		`Get-Process qm26 -ErrorAction SilentlyContinue`,
		`Get-Process qm26 -ErrorAction   SilentlyContinue`,
		`Get-Process qm26 -ErrorAction:SilentlyContinue`,
		`Get-Process qm26 -EA:Ignore`,
		`Get-Process qm26 -ea 0`,
		`Get-Process qm26 -ErrorAction:'SilentlyContinue'`,
		`Get-Process qm26 -ea ignore`,
		`$ErrorActionPreference='SilentlyContinue'; Get-Process qm26`,
		`$ErrorActionPreference = "Ignore"; Get-Process qm26`,
	} {
		if !codingPowerShellErrorsAreSilenced(command) {
			t.Fatalf("expected silenced errors for %q", command)
		}
	}
	for _, command := range []string{
		`Get-Process qm26`,
		`Get-Process qm26 -ErrorAction Stop`,
	} {
		if codingPowerShellErrorsAreSilenced(command) {
			t.Fatalf("did not expect silenced errors for %q", command)
		}
	}
}

func TestCodingCommandExitStatusDoesNotBlamePipelines(t *testing.T) {
	got := appendCodingCommandExitStatus("", 0)
	if strings.Contains(got, "pipeline") {
		t.Fatalf("empty output must not be blamed on pipeline filters: %q", got)
	}
	if !strings.Contains(got, "command exited with code 0") {
		t.Fatalf("missing exit status: %q", got)
	}
}

func TestCodingBashKeepsSelectObjectOutputOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell wrapper only applies on windows")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "alpha.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := executeCodingBashWithContext(nil, map[string]interface{}{
		"command":     "Get-ChildItem -Force | Select-Object Mode,Length,Name",
		"working_dir": dir,
		"timeout":     float64(30),
	}, nil)
	if !strings.Contains(res.Text, "alpha.txt") {
		t.Fatalf("Select-Object output was lost: %q", res.Text)
	}
	if res.Kind != codingCommandResultOK {
		t.Fatalf("kind = %v, want ok: %q", res.Kind, res.Text)
	}
}

func TestCodingBashSilentlyContinueProbeIsNotAFailureOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell wrapper only applies on windows")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not available")
	}
	res := executeCodingBashWithContext(nil, map[string]interface{}{
		"command":     `Get-Process definitely-not-a-process -ErrorAction SilentlyContinue | Select-Object Id`,
		"working_dir": t.TempDir(),
		"timeout":     float64(30),
	}, nil)
	if res.Kind != codingCommandResultOK {
		t.Fatalf("silenced probe must not fail the turn: kind=%v text=%q", res.Kind, res.Text)
	}
}

func TestCodingBashNativeStderrWithExitZeroIsNotAFailureOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell wrapper only applies on windows")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not available")
	}
	// Two stderr lines exercise both NativeCommandError and
	// NativeCommandErrorMessage. The program exits 0 and printed the line the
	// filter asked for, so the turn must not be recorded as failed.
	res := executeCodingBashWithContext(nil, map[string]interface{}{
		"command":     `cmd /c "echo progress-one 1>&2 & echo progress-two 1>&2 & echo good" 2>&1 | Select-String "good"`,
		"working_dir": t.TempDir(),
		"timeout":     float64(30),
	}, nil)
	if res.Kind != codingCommandResultOK {
		t.Fatalf("native stderr with exit 0 must not fail the turn: kind=%v text=%q", res.Kind, res.Text)
	}
	if !strings.Contains(res.Text, "good") {
		t.Fatalf("filtered output missing: %q", res.Text)
	}
}

func TestCodingBashCommandSyntaxErrorDoesNotBreakWrapperOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell wrapper only applies on windows")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe not available")
	}
	wrapper := wrapCodingPowerShellCommand(`Write-Output "unterminated`)
	if strings.Contains(wrapper, "unterminated") {
		t.Fatal("the command must be encoded, not embedded as wrapper source")
	}
	// A model that writes bash-style \" inside a PowerShell string used to
	// close the wrapper's own literals and produce a ParserError about wrapper
	// lines. The error must now be attributed to the command itself.
	res := executeCodingBashWithContext(nil, map[string]interface{}{
		"command": `Write-Output "unterminated`,
		"timeout": float64(30),
	}, nil)
	if res.Kind == codingCommandResultOK {
		t.Fatalf("unterminated quote must fail: %#v", res)
	}
	if !strings.Contains(res.Text, "PowerShell exception") {
		t.Fatalf("syntax error must be caught inside the command: %q", res.Text)
	}
}

// codingPowerShellWrappedCommand returns the command text the wrapper will
// Invoke-Expression. Tests that inspect a wrapper must not look for the raw
// command: it is base64-encoded so its quotes cannot break the wrapper.
func codingPowerShellWrappedCommand(t *testing.T, args []string) string {
	t.Helper()
	if len(args) == 0 {
		t.Fatal("no powershell arguments")
	}
	script := args[len(args)-1]
	const marker = "FromBase64String('"
	start := strings.Index(script, marker)
	if start < 0 {
		t.Fatalf("wrapper does not carry an encoded command: %q", script)
	}
	rest := script[start+len(marker):]
	end := strings.IndexByte(rest, '\'')
	if end < 0 {
		t.Fatalf("encoded command is not terminated: %q", script)
	}
	decoded, err := base64.StdEncoding.DecodeString(rest[:end])
	if err != nil {
		t.Fatalf("decode wrapped command: %v", err)
	}
	return string(decoded)
}

func TestCodingInquiryAllowsQuotedSSHSearch(t *testing.T) {
	cb := &codingSubAgentCallbacks{task: &TaskItem{RequestKind: codingRequestInquiry}}
	command := `Select-String -Pattern "ssh|paramiko|connect|scp" -Path docs\*.md`
	if msg := cb.guardCodingShellCommand(command, "."); msg != "" {
		t.Fatalf("a read-only search containing ssh/scp text must pass: %s", msg)
	}
}

func TestCodingBashKeepsCdScopeInsideCommandOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell wrapper only applies on windows")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inside.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The wrapper runs the command as a child script block; Set-Location must
	// still apply to the later statements of the same command.
	res := executeCodingBashWithContext(nil, map[string]interface{}{
		"command":     `cd sub; Get-ChildItem -Name`,
		"working_dir": dir,
		"timeout":     float64(30),
	}, nil)
	if res.Kind != codingCommandResultOK || !strings.Contains(res.Text, "inside.txt") {
		t.Fatalf("cd did not apply inside the executed block: kind=%v text=%q", res.Kind, res.Text)
	}
}
