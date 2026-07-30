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
	q := shellQuoteCLIValue
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

// shellQuoteCLIValue returns a POSIX shell word. It is used for the automatic
// flag form only; template placeholders are handled by cliCommandForExecution
// so Windows can use delayed environment expansion without reparsing message
// text as command syntax.
func shellQuoteCLIValue(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func cliParamReferences() CLIParams {
	return CLIParams{
		Date:        "!LANXIN_WATCH_DATE!",
		Content:     "!LANXIN_WATCH_CONTENT!",
		SpeakerID:   "!LANXIN_WATCH_SPEAKER_ID!",
		SpeakerName: "!LANXIN_WATCH_SPEAKER_NAME!",
		GroupID:     "!LANXIN_WATCH_GROUP_ID!",
		GroupName:   "!LANXIN_WATCH_GROUP_NAME!",
		Keyword:     "!LANXIN_WATCH_KEYWORD!",
		MessageID:   "!LANXIN_WATCH_MESSAGE_ID!",
	}
}

// cliCommandForExecution incorporates data received from IM safely. On POSIX
// every placeholder becomes one single-quoted shell word. On Windows we keep
// the data in the child environment and expand it once with delayed expansion;
// cmd does not rescan a delayed-expansion value for &, |, %, or quotes.
func cliCommandForExecution(commandLine string, p CLIParams) string {
	commandLine = strings.TrimSpace(commandLine)
	if runtime.GOOS == "windows" {
		if HasCLIPlaceholders(commandLine) {
			return ExpandCLICommand(commandLine, cliParamReferences())
		}
		refs := cliParamReferences()
		return fmt.Sprintf(
			`%s --date %s --content %s --speaker-id %s --group-id %s --keyword %s`,
			commandLine, refs.Date, refs.Content, refs.SpeakerID, refs.GroupID, refs.Keyword,
		)
	}
	if HasCLIPlaceholders(commandLine) {
		return ExpandCLICommand(commandLine, CLIParams{
			Date:        shellQuoteCLIValue(p.Date),
			Content:     shellQuoteCLIValue(p.Content),
			SpeakerID:   shellQuoteCLIValue(p.SpeakerID),
			SpeakerName: shellQuoteCLIValue(p.SpeakerName),
			GroupID:     shellQuoteCLIValue(p.GroupID),
			GroupName:   shellQuoteCLIValue(p.GroupName),
			Keyword:     shellQuoteCLIValue(p.Keyword),
			MessageID:   shellQuoteCLIValue(p.MessageID),
		})
	}
	return AppendStandardCLIFlags(commandLine, p)
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
	expanded := cliCommandForExecution(commandLine, p)

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
		// Delayed expansion is deliberately enabled: see cliCommandForExecution.
		cmd = exec.CommandContext(runCtx, "cmd", "/D", "/V:ON", "/S", "/C", expanded)
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
