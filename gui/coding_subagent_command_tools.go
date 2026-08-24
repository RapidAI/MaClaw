package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type codingCommandResultKind string

const (
	codingCommandResultOK         codingCommandResultKind = "ok"
	codingCommandResultStartError codingCommandResultKind = "start_error"
	codingCommandResultExitError  codingCommandResultKind = "exit_error"
	codingCommandResultTimeout    codingCommandResultKind = "timeout"
	codingCommandResultCancelled  codingCommandResultKind = "cancelled"
)

type codingCommandExecutionResult struct {
	Text     string
	Kind     codingCommandResultKind
	ExitCode int
}

func (r codingCommandExecutionResult) toolResult() codingToolExecutionResult {
	return codingToolExecutionResult{Text: r.Text, Outcome: r.toolOutcome()}
}

func (r codingCommandExecutionResult) toolResultForCommand(command string) codingToolExecutionResult {
	return codingToolExecutionResult{Text: r.Text, Outcome: r.toolOutcomeForCommand(command)}
}

func (r codingCommandExecutionResult) succeeded() bool {
	return r.toolOutcome() == codingToolOutcomeSuccess
}

func (r codingCommandExecutionResult) succeededForCommand(command string) bool {
	return r.toolOutcomeForCommand(command) == codingToolOutcomeSuccess
}

func (r codingCommandExecutionResult) toolOutcomeForCommand(command string) codingToolOutcome {
	switch r.Kind {
	case codingCommandResultExitError:
		if commandAllowsInformationalExitOne(command) && commandResultExitOneHasNoErrorOutput(r) {
			return codingToolOutcomeSuccess
		}
		return codingToolOutcomeFailed
	default:
		return r.toolOutcome()
	}
}

func (r codingCommandExecutionResult) toolOutcome() codingToolOutcome {
	outcome := codingToolOutcomeSuccess
	switch r.Kind {
	case codingCommandResultOK:
		outcome = codingToolOutcomeSuccess
	case codingCommandResultTimeout:
		outcome = codingToolOutcomeTimeout
	case codingCommandResultCancelled:
		outcome = codingToolOutcomeFailed
	case codingCommandResultExitError:
		outcome = codingToolOutcomeFailed
	default:
		outcome = codingToolOutcomeFailed
	}
	return outcome
}

func commandResultExitOneHasNoErrorOutput(r codingCommandExecutionResult) bool {
	if r.ExitCode != 1 {
		return false
	}
	cleanText := strings.TrimSpace(stripCodingCommandExitStatus(r.Text))
	return !codingCommandOutputHasStderr(cleanText)
}

func codingCommandOutputHasStderr(text string) bool {
	text = strings.TrimSpace(text)
	return strings.HasPrefix(text, "[stderr]") || strings.Contains(text, "\n[stderr]")
}

func stripCodingCommandExitStatus(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	for _, marker := range []string{"\ncommand exited with code", "\r\ncommand exited with code"} {
		if idx := strings.Index(text, marker); idx >= 0 {
			return strings.TrimSpace(text[:idx])
		}
	}
	if strings.HasPrefix(text, "command exited with code") {
		return ""
	}
	return text
}

func commandAllowsInformationalExitOne(command string) bool {
	sawCommand := false
	for _, segment := range shellCommandSegments(strings.ToLower(strings.Join(strings.Fields(command), " "))) {
		segment = stripVerificationCommandPrefixes(segment)
		if len(segment) == 0 {
			continue
		}
		sawCommand = true
		if !commandSegmentAllowsInformationalExitOne(segment) {
			return false
		}
	}
	return sawCommand
}

func commandSegmentAllowsInformationalExitOne(segment []string) bool {
	if len(segment) == 0 {
		return false
	}
	cmd := commandNameBase(segment[0])
	args := segment[1:]
	switch cmd {
	case "rg", "ripgrep":
		return true
	case "grep", "findstr", "select-string":
		return true
	case "git":
		return gitCommandSubcommand(args) == "grep"
	}
	return false
}

func gitCommandSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "" {
			continue
		}
		if arg == "-c" || arg == "-C" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") || strings.HasPrefix(arg, "--namespace=") || strings.HasPrefix(arg, "-c=") || strings.HasPrefix(arg, "-C=") {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func executeCodingBash(args map[string]interface{}, onProgress coretool.ProgressCallback) codingCommandExecutionResult {
	return executeCodingBashWithContext(context.Background(), args, onProgress)
}

func executeCodingBashWithContext(parent context.Context, args map[string]interface{}, onProgress coretool.ProgressCallback) codingCommandExecutionResult {
	command, _ := args["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return codingCommandExecutionResult{Text: "missing command parameter", Kind: codingCommandResultStartError, ExitCode: -1}
	}

	timeout := resolveCodingCommandTimeout(args, command)
	workDir := ""
	if rawWorkDir := strings.TrimSpace(stringVal(args, "working_dir")); rawWorkDir != "" {
		workDir = resolvePath(rawWorkDir)
	}
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return codingCommandExecutionResult{Text: fmt.Sprintf("command cancelled before start: %v", err), Kind: codingCommandResultCancelled, ExitCode: -1}
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName, shellArgs = windowsCodingBashInvocation(command)
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.Command(shellName, shellArgs...)
	cmd.Dir = workDir
	cmd.Env = codingCommandEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)
	coretool.PrepareCommandForTreeKill(cmd)

	if err := cmd.Start(); err != nil {
		return codingCommandExecutionResult{
			Text:     fmt.Sprintf("command start failed: %v", err),
			Kind:     codingCommandResultStartError,
			ExitCode: -1,
		}
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		elapsed := 0
		for {
			select {
			case <-ticker.C:
				elapsed += 30
				displayCmd := command
				if len(displayCmd) > 60 {
					displayCmd = displayCmd[:60] + "..."
				}
				if onProgress != nil {
					onProgress(fmt.Sprintf("command still running (%ds): %s", elapsed, displayCmd))
				}
			case <-done:
				return
			}
		}
	}()

	err := coretool.WaitCommandWithContext(ctx, cmd)
	close(done)

	output := formatCodingCommandOutput(stdout.String(), stderr.String())
	if err == nil {
		if output == "" {
			output = "(command completed with no output)"
		}
		return codingCommandExecutionResult{Text: output, Kind: codingCommandResultOK, ExitCode: 0}
	}
	if ctx.Err() == context.DeadlineExceeded {
		output = appendCodingCommandStatus(output, fmt.Sprintf("command timed out after %d seconds", timeout))
		return codingCommandExecutionResult{Text: output, Kind: codingCommandResultTimeout, ExitCode: -1}
	}
	if ctx.Err() == context.Canceled {
		output = appendCodingCommandStatus(output, "command cancelled")
		return codingCommandExecutionResult{Text: output, Kind: codingCommandResultCancelled, ExitCode: -1}
	}
	exitCode := -1
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	output = appendCodingCommandExitStatus(output, exitCode)
	return codingCommandExecutionResult{Text: output, Kind: codingCommandResultExitError, ExitCode: exitCode}
}

func windowsCodingBashInvocation(command string) (string, []string) {
	if adapted, ok := adaptWindowsUnixInspectCommand(command); ok {
		log.Printf("[coding-bash] adapted unix inspect for Windows: %s", truncateRunesV2(command, 160))
		// Stay on PowerShell. The generated script already has no && / ||,
		// and must not be re-parsed as cmd.exe or an MSVC recipe.
		wrappedPS := wrapCodingPowerShellCommand(adapted)
		return passthroughRuntimeProgram("powershell.exe"), []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + wrappedPS}
	}
	if adapted, ok := adaptWindowsPythonInspectCommand(command); ok {
		log.Printf("[coding-bash] adapted python inspect for Windows: %s", truncateRunesV2(command, 160))
		wrappedPS := wrapCodingPowerShellCommand(adapted)
		return passthroughRuntimeProgram("powershell.exe"), []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + wrappedPS}
	}
	command = mapPython3ToWindows(command)
	if repaired, ok := normalizeWindowsMSVCCompileCommand(command); ok {
		// vcvars+cl must share a cmd.exe environment. Nested
		// `cmd /c "call \"...\" && cl"` loses quotes in PowerShell.
		// Rewrite after unwrap so `; .\hello.exe` inside cmd /c '...' still
		// short-circuits a failed compile.
		repaired = rewriteWindowsCompileThenRunSemicolon(repaired)
		return passthroughRuntimeProgram("cmd.exe"), []string{"/d", "/s", "/c", repaired}
	}
	command = rewriteWindowsCompileThenRunSemicolon(command)
	if hasUnquotedWindowsCmdSyntax(command) {
		// Commands using cmd.exe control operators (for example `||` or
		// `2>nul`) must not be parsed by Windows PowerShell 5.1.
		return passthroughRuntimeProgram("cmd.exe"), []string{"/d", "/s", "/c", command}
	}
	// PowerShell 5.1 has no &&. Translating it to bare `;` would let a failed
	// compile plus a leftover hello.exe look like success. Keep short-circuit.
	psCommand := convertUnquotedAndAndForPowerShell(command)
	// Wrap the command so Go observes the real command result. Native
	// tools often write progress to stderr even when they exit 0, while
	// PowerShell cmdlet errors can be hidden by 2>&1 pipelines. The wrapper
	// keeps stderr visible but classifies success primarily by $LASTEXITCODE.
	wrappedPS := wrapCodingPowerShellCommand(psCommand)
	return passthroughRuntimeProgram("powershell.exe"), []string{"-NoProfile", "-NonInteractive", "-Command",
		"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + wrappedPS}
}

func hasUnquotedWindowsCmdSyntax(command string) bool {
	var b strings.Builder
	var quote rune
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			b.WriteByte(' ')
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	plain := strings.ToLower(b.String())
	return strings.Contains(plain, "||") || strings.Contains(plain, ">nul") || strings.Contains(plain, "> nul")
}

func codingCommandEnv() []string {
	env := coretool.AppendNoWindowEnv(coretool.AppendUTF8Env(os.Environ()))
	if runtime.GOOS != "windows" {
		return env
	}
	systemRoot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	gitDir := ""
	if gitPath := passthroughRuntimeProgram("git"); filepath.IsAbs(gitPath) {
		gitDir = filepath.Dir(gitPath)
	}
	env = mergeWindowsToolchainEnviron(env, windowsMSVCInjectedEnviron())
	if mingw := detectWindowsMinGWBin(); mingw != "" {
		env = appendWindowsPathEntries(env, mingw)
	}
	return appendWindowsPathEntries(env,
		filepath.Join(systemRoot, "System32"),
		filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0"),
		gitDir,
	)
}

func appendWindowsPathEntries(env []string, entries ...string) []string {
	pathIdx := -1
	pathValue := ""
	for i, item := range env {
		if strings.HasPrefix(strings.ToLower(item), "path=") {
			pathIdx = i
			pathValue = item[len("Path="):]
			break
		}
	}
	parts := []string{}
	if pathValue != "" {
		parts = strings.Split(pathValue, string(os.PathListSeparator))
	}
	seen := map[string]struct{}{}
	for _, part := range parts {
		if key := strings.ToLower(strings.TrimSpace(part)); key != "" {
			seen[key] = struct{}{}
		}
	}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key := strings.ToLower(entry)
		if _, ok := seen[key]; ok {
			continue
		}
		parts = append(parts, entry)
		seen[key] = struct{}{}
	}
	newPath := "Path=" + strings.Join(parts, string(os.PathListSeparator))
	if pathIdx >= 0 {
		env[pathIdx] = newPath
		return env
	}
	return append(env, newPath)
}

// windowsPowerShellAndAndStop is the PowerShell 5.1 stand-in for &&.
const windowsPowerShellAndAndStop = `; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE };`

func convertUnquotedAndAndForPowerShell(command string) string {
	if !strings.Contains(command, "&&") {
		return command
	}
	runes := []rune(command)
	var b strings.Builder
	var quote rune
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		if quote != 0 {
			b.WriteRune(ch)
			if ch == '`' && i+1 < len(runes) {
				i++
				b.WriteRune(runes[i])
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			b.WriteRune(ch)
			continue
		}
		if ch == '&' && i+1 < len(runes) && runes[i+1] == '&' {
			b.WriteString(windowsPowerShellAndAndStop)
			i++
			continue
		}
		b.WriteRune(ch)
	}
	return b.String()
}

func wrapCodingPowerShellCommand(psCommand string) string {
	return fmt.Sprintf(`$Error.Clear()
$ErrorActionPreference='Continue'
$global:LASTEXITCODE = 0
try {
	%s
	$psSucceeded = $?
	$nativeExit = $LASTEXITCODE
} catch {
	[Console]::Error.WriteLine("PowerShell exception: " + $_.Exception.Message)
	exit 1
}
if ($null -ne $nativeExit -and $nativeExit -ne 0) {
	if ($Error.Count -gt 0) {
		[Console]::Error.WriteLine("Last error: " + $Error[0].ToString())
	} else {
		[Console]::Error.WriteLine("Process exited with code $nativeExit - output was likely consumed by a pipeline filter. Run the command without pipe operators to see the actual error.")
	}
	exit $nativeExit
}
if (-not $psSucceeded -and $Error.Count -gt 0) {
	$nonNativeErrors = @($Error | Where-Object { $_.FullyQualifiedErrorId -ne 'NativeCommandError' })
	if ($nonNativeErrors.Count -gt 0) {
		[Console]::Error.WriteLine("PowerShell error: " + $nonNativeErrors[0].ToString())
		exit 1
	}
}
exit 0`, psCommand)
}

func resolveCodingCommandTimeout(args map[string]interface{}, command string) int {
	timeout := corelib.DefaultAgentTimeoutSec
	switch v := args["timeout"].(type) {
	case float64:
		if v > 0 {
			timeout = int(v)
		}
	case float32:
		if v > 0 {
			timeout = int(v)
		}
	case int:
		if v > 0 {
			timeout = v
		}
	case int64:
		if v > 0 {
			timeout = int(v)
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			timeout = int(i)
		}
	}
	if timeout > corelib.MaxAgentTimeoutSec {
		return corelib.MaxAgentTimeoutSec
	}
	return timeout
}

func formatCodingCommandOutput(stdoutText, stderrText string) string {
	var b strings.Builder
	if stdoutText != "" {
		if len(stdoutText) > 8192 {
			stdoutText = truncateCodingCommandOutputText(stdoutText, 8192, "\n... (output truncated)")
		}
		b.WriteString(stdoutText)
	}
	if stderrText != "" {
		if len(stderrText) > 4096 {
			stderrText = truncateCodingCommandOutputText(stderrText, 4096, "\n... (stderr truncated)")
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(stderrText)
	}
	return b.String()
}

func truncateCodingCommandOutputText(text string, maxBytes int, suffix string) string {
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	cut := maxBytes
	for cut > 0 && !utf8.ValidString(text[:cut]) {
		cut--
	}
	if cut <= 0 {
		return suffix
	}
	return text[:cut] + suffix
}

func appendCodingCommandStatus(output, status string) string {
	output = strings.TrimRight(output, "\r\n")
	if output == "" {
		return status
	}
	return output + "\n" + status
}

func appendCodingCommandExitStatus(output string, exitCode int) string {
	status := fmt.Sprintf("command exited with code %d", exitCode)
	output = strings.TrimRight(output, "\r\n")
	if output == "" {
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("%s (no stdout/stderr captured). This typically happens when pipeline filters (Select-String, Select-Object) consume all output, or when a cmdlet handles errors internally. Try running the base command without pipe filters to see the actual output or error.", status)
		}
		return fmt.Sprintf("%s (no stdout/stderr). Re-run the command without output filters or with broader diagnostics to capture the real error.", status)
	}
	return output + "\n" + status
}
