package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

type codingCommandResultKind string

const (
	codingCommandResultOK         codingCommandResultKind = "ok"
	codingCommandResultStartError codingCommandResultKind = "start_error"
	codingCommandResultExitError  codingCommandResultKind = "exit_error"
	codingCommandResultTimeout    codingCommandResultKind = "timeout"
)

type codingCommandExecutionResult struct {
	Text     string
	Kind     codingCommandResultKind
	ExitCode int
}

func (r codingCommandExecutionResult) toolResult() codingToolExecutionResult {
	return codingToolExecutionResult{Text: r.Text, Outcome: r.toolOutcome()}
}

func (r codingCommandExecutionResult) succeeded() bool {
	return r.toolOutcome() == codingToolOutcomeSuccess
}

func (r codingCommandExecutionResult) toolOutcome() codingToolOutcome {
	outcome := codingToolOutcomeSuccess
	switch r.Kind {
	case codingCommandResultOK:
		outcome = codingToolOutcomeSuccess
	case codingCommandResultTimeout:
		outcome = codingToolOutcomeTimeout
	case codingCommandResultExitError:
		// Exit code 1 with meaningful stdout is informational (not a real error).
		// The "command exited with code N" suffix doesn't count as meaningful output.
		cleanText := r.Text
		if idx := strings.Index(cleanText, "\ncommand exited with code"); idx >= 0 {
			cleanText = cleanText[:idx]
		} else if strings.HasPrefix(cleanText, "command exited with code") {
			cleanText = ""
		}
		if r.ExitCode == 1 && !strings.HasPrefix(cleanText, "[stderr]") && len(strings.TrimSpace(cleanText)) > 10 {
			outcome = codingToolOutcomeSuccess
		} else {
			outcome = codingToolOutcomeFailed
		}
	default:
		outcome = codingToolOutcomeFailed
	}
	return outcome
}

func executeCodingBash(args map[string]interface{}, onProgress coretool.ProgressCallback) codingCommandExecutionResult {
	command, _ := args["command"].(string)
	if command == "" {
		return codingCommandExecutionResult{Text: "missing command parameter", Kind: codingCommandResultStartError, ExitCode: -1}
	}

	timeout := resolveCodingCommandTimeout(args, command)
	workDir := resolvePath(stringVal(args, "working_dir"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		// Auto-convert bash-style && to PowerShell-compatible ; (sequential execution).
		// LLMs frequently generate && despite being told to use PowerShell syntax.
		// PowerShell 5.1 doesn't support && (only 7+ does).
		psCommand := strings.ReplaceAll(command, " && ", " ; ")
		shellName = "powershell"
		// Wrap the command so Go observes the real command result. Native
		// tools often write progress to stderr even when they exit 0, while
		// PowerShell cmdlet errors can be hidden by 2>&1 pipelines. The wrapper
		// keeps stderr visible but classifies success primarily by $LASTEXITCODE.
		wrappedPS := wrapCodingPowerShellCommand(psCommand)
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + wrappedPS}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.Command(shellName, shellArgs...)
	cmd.Dir = workDir
	cmd.Env = coretool.AppendUTF8Env(os.Environ())
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
	exitCode := -1
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	}
	output = appendCodingCommandExitStatus(output, exitCode)
	return codingCommandExecutionResult{Text: output, Kind: codingCommandResultExitError, ExitCode: exitCode}
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
			stdoutText = stdoutText[:8192] + "\n... (output truncated)"
		}
		b.WriteString(stdoutText)
	}
	if stderrText != "" {
		if len(stderrText) > 4096 {
			stderrText = stderrText[:4096] + "\n... (stderr truncated)"
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("[stderr] ")
		b.WriteString(stderrText)
	}
	return b.String()
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
