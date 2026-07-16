package lansengerwatch

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// CLIParams are substituted into command templates and passed as env/flags.
type CLIParams struct {
	Date        string // RFC3339
	Content     string
	SpeakerID   string
	SpeakerName string
	GroupID     string
	GroupName   string
	Keyword     string
	MessageID   string
}

// ExpandCLICommand replaces {{placeholders}} in the command template.
func ExpandCLICommand(template string, p CLIParams) string {
	replacer := strings.NewReplacer(
		"{{date}}", p.Date,
		"{{content}}", p.Content,
		"{{speaker_id}}", p.SpeakerID,
		"{{speaker_name}}", p.SpeakerName,
		"{{group_id}}", p.GroupID,
		"{{group_name}}", p.GroupName,
		"{{keyword}}", p.Keyword,
		"{{message_id}}", p.MessageID,
		// Aliases
		"{{speakerId}}", p.SpeakerID,
		"{{groupId}}", p.GroupID,
		"{{messageId}}", p.MessageID,
	)
	return replacer.Replace(template)
}

// HasCLIPlaceholders reports whether the template uses {{...}} expansion.
func HasCLIPlaceholders(template string) bool {
	return strings.Contains(template, "{{")
}

// BuildCLIEnv returns environment variables for the child process.
func BuildCLIEnv(p CLIParams) []string {
	return []string{
		"LANXIN_WATCH_DATE=" + p.Date,
		"LANXIN_WATCH_CONTENT=" + p.Content,
		"LANXIN_WATCH_SPEAKER_ID=" + p.SpeakerID,
		"LANXIN_WATCH_SPEAKER_NAME=" + p.SpeakerName,
		"LANXIN_WATCH_GROUP_ID=" + p.GroupID,
		"LANXIN_WATCH_GROUP_NAME=" + p.GroupName,
		"LANXIN_WATCH_KEYWORD=" + p.Keyword,
		"LANXIN_WATCH_MESSAGE_ID=" + p.MessageID,
	}
}

// AppendStandardCLIFlags adds --date/--content/... when the template has no placeholders.
func AppendStandardCLIFlags(command string, p CLIParams) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	if HasCLIPlaceholders(command) {
		return ExpandCLICommand(command, p)
	}
	// Quote-safe enough for shell: wrap values in double quotes and escape.
	q := func(s string) string {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		return `"` + s + `"`
	}
	return fmt.Sprintf(
		`%s --date %s --content %s --speaker-id %s --group-id %s --keyword %s`,
		command,
		q(p.Date),
		q(p.Content),
		q(p.SpeakerID),
		q(p.GroupID),
		q(p.Keyword),
	)
}

// CLIResult is the outcome of running a watch CLI command.
type CLIResult struct {
	Command string
	Stdout  string
	Stderr  string
	Err     error
}

// RunCLI executes commandLine via the platform shell with timeout.
func RunCLI(ctx context.Context, commandLine string, p CLIParams, timeoutSec int) CLIResult {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return CLIResult{Err: fmt.Errorf("empty cli command")}
	}
	if timeoutSec <= 0 {
		timeoutSec = DefaultCLITimeoutSec
	}
	if timeoutSec > MaxCLITimeoutSec {
		timeoutSec = MaxCLITimeoutSec
	}
	expanded := AppendStandardCLIFlags(commandLine, p)

	// Respect parent budget (gateway Process timeout) over rule CLITimeoutSec.
	timeout := time.Duration(timeoutSec) * time.Second
	if ctx == nil {
		ctx = context.Background()
	}
	if dl, ok := ctx.Deadline(); ok {
		if rem := time.Until(dl); rem <= 0 {
			return CLIResult{Command: expanded, Err: context.DeadlineExceeded}
		} else if rem < timeout {
			timeout = rem
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(runCtx, "cmd", "/C", expanded)
	} else {
		cmd = exec.CommandContext(runCtx, "sh", "-c", expanded)
	}
	cmd.Env = append(os.Environ(), BuildCLIEnv(p)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return CLIResult{
		Command: expanded,
		Stdout:  strings.TrimSpace(stdout.String()),
		Stderr:  strings.TrimSpace(stderr.String()),
		Err:     err,
	}
}

// PreferCLIStdout decides the IM reply text for a keyword rule.
func PreferCLIStdout(rule KeywordRule, cli CLIResult) (reply string, usedCLI bool) {
	wantCLI := rule.CLICommand != ""
	if rule.ReplyWithCLIStdout != nil {
		wantCLI = wantCLI && *rule.ReplyWithCLIStdout
	}
	if wantCLI && cli.Err == nil && strings.TrimSpace(cli.Stdout) != "" {
		return strings.TrimSpace(cli.Stdout), true
	}
	return strings.TrimSpace(rule.ReplyText), false
}
