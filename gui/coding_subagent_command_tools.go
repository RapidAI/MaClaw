package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

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
	outcome := codingToolOutcomeSuccess
	switch r.Kind {
	case codingCommandResultOK:
		outcome = codingToolOutcomeSuccess
	case codingCommandResultTimeout:
		outcome = codingToolOutcomeTimeout
	default:
		outcome = codingToolOutcomeFailed
	}
	return codingToolExecutionResult{Text: r.Text, Outcome: outcome}
}

func executeCodingBash(args map[string]interface{}, onProgress coretool.ProgressCallback) codingCommandExecutionResult {
	command, _ := args["command"].(string)
	if command == "" {
		return codingCommandExecutionResult{Text: "missing command parameter", Kind: codingCommandResultStartError, ExitCode: -1}
	}

	timeout := resolveBashTimeout(args, command)
	workDir := resolvePath(stringVal(args, "working_dir"))
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	var shellName string
	var shellArgs []string
	if runtime.GOOS == "windows" {
		shellName = "powershell"
		shellArgs = []string{"-NoProfile", "-NonInteractive", "-Command",
			"[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; " + command}
	} else {
		shellName = "bash"
		shellArgs = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, shellName, shellArgs...)
	cmd.Dir = workDir
	cmd.Env = coretool.AppendUTF8Env(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	hideCommandWindow(cmd)

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

	err := cmd.Wait()
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
	output = appendCodingCommandStatus(output, fmt.Sprintf("command exited with code %d", exitCode))
	return codingCommandExecutionResult{Text: output, Kind: codingCommandResultExitError, ExitCode: exitCode}
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
