package guiapp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// A background chat task that polls an unregistered remote job used to put
// `sleep N` on the SSH session. That holds the PTY and the agent iteration
// for the whole wait, the card can only show the sleep line, and a no-tool
// "still running" reply marks the task done while the remote job continues.
//
// The sleep is not written. The tool runs the check immediately and returns.
// A repeat of that same check inside the interval returns the last result
// and does not open SSH again when the check was written, including a
// started failure such as a timeout. A check that was not started is run
// again. When the model stops with the watch still armed, the host waits
// the remainder of the interval outside the model turn and re-runs only
// the check. Identical output does not start another model turn. The watch ends when the reply starts with remoteWatchDoneToken,
// when a non-check SSH command is actually written, when the park ceiling
// is reached, or when the task is canceled. A refusal before any effect
// leaves the watch armed. An empty check is a quiet sample. A bare sleep
// while the watch is armed is not written, and a follow such as tail -f
// is refused before it reaches the session.

const (
	remoteWatchDoneToken    = "远端任务已结束"
	remoteWatchMinInterval  = time.Second
	remoteWatchMaxInterval  = 30 * time.Minute
	remoteWatchParkCeiling  = 6 * time.Hour
	remoteWatchCheckTimeout = 60 * time.Second
	remoteWatchTick         = 30 * time.Second
	remoteWatchEmptyOutput  = "（无输出）"
)

// chatTaskRemoteWatch is the host-side poll for one background chat task.
// It is in memory only. A restart already marks the task interrupted.
type chatTaskRemoteWatch struct {
	Principal  string
	Check      string
	Interval   time.Duration
	LastOutput string
	LastError  string
	// LastStarted is true when the last attempt wrote the command or
	// succeeded. A not-started error is not a reusable sample. Read it
	// only together with LastError: the same disconnect text is used both
	// before the write and after it.
	LastStarted bool
	ArmedAt     time.Time
	// CheckedAt is the last time this check ran. The park waits
	// Interval minus that age. A restart does not restore the watch.
	CheckedAt time.Time
	// Shown is the model-facing command result, without the host note.
	// A fresh repeat returns it unchanged.
	Shown string
	armed bool
}

type chatTaskWaveOutcome struct {
	resp      *IMAgentResponse
	loop      *LoopContext
	cancelled bool
}

type chatTaskWaveRunner func(task *chatTaskRecord, wave int, text string) chatTaskWaveOutcome

type chatTaskStopKind int

const (
	chatTaskStopFinish chatTaskStopKind = iota
	chatTaskStopPark
	chatTaskStopCeiling
)

type remoteWatchWaitFunc func(ctx context.Context, d time.Duration, onTick func()) error

var (
	remoteWatchWaitHook = defaultRemoteWatchWait
	remoteWatchWaitMu   sync.RWMutex
)

func waitRemoteWatch(ctx context.Context, d time.Duration, onTick func()) error {
	remoteWatchWaitMu.RLock()
	fn := remoteWatchWaitHook
	remoteWatchWaitMu.RUnlock()
	if fn == nil {
		fn = defaultRemoteWatchWait
	}
	return fn(ctx, d, onTick)
}

func defaultRemoteWatchWait(ctx context.Context, d time.Duration, onTick func()) error {
	if d <= 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	var tick <-chan time.Time
	if onTick != nil && d >= remoteWatchTick {
		ticker := time.NewTicker(remoteWatchTick)
		defer ticker.Stop()
		tick = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		case <-tick:
			onTick()
		}
	}
}

func chatTaskParentPrincipal(principal string) string {
	principal = strings.TrimSpace(principal)
	const mark = ":task:"
	cut := strings.LastIndex(principal, mark)
	if cut <= 0 {
		return principal
	}
	parent := strings.TrimSpace(principal[:cut])
	if parent == "" {
		return principal
	}
	return parent
}

func chatTaskReplyClosesRemoteWatch(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), remoteWatchDoneToken)
}

func remoteWatchHostNote(interval time.Duration) string {
	secs := waitedSeconds(interval)
	return fmt.Sprintf("[system] Host did not write sleep to the SSH session. Only the check command ran. The host checks again every %d seconds. A reply with no tool call keeps this watch. Identical output is rechecked on the host without another model turn. To finish the watch, the reply must start with %s.", secs, remoteWatchDoneToken)
}

func remoteWatchToolBody(shown string, interval time.Duration) string {
	shown = strings.TrimSpace(shown)
	note := remoteWatchHostNote(interval)
	if strings.Contains(shown, "did not write sleep") {
		return shown
	}
	if shown == "" {
		return note
	}
	return shown + "\n" + note
}

// remoteWatchNormalizeEmpty turns a check that printed nothing into a sample.
// An empty tail is a normal poll, and reporting it as a failed command makes
// the model replace the watch.
func remoteWatchNormalizeEmpty(text string, err error) (string, error) {
	if err != nil && strings.TrimSpace(err.Error()) == "trusted_ssh_empty" {
		return remoteWatchEmptyOutput, nil
	}
	return text, err
}

func remoteWatchShown(text string, err error, timeout time.Duration) string {
	if err == nil {
		return trustedSSHCommandResult(text, nil, timeout)
	}
	// The ordinary timeout hint tells the model to raise timeout_seconds or
	// to sleep inside the command. This check is already the short poll.
	msg := strings.TrimSpace(err.Error())
	var shown string
	if msg == "trusted_ssh_timeout" || strings.HasPrefix(msg, "trusted_ssh_timeout:") {
		shown = semanticPrefixedRejection(err)
	} else {
		shown = trustedSSHCommandResult("", err, timeout)
	}
	return remoteWatchSoftenResend(shown)
}

// sshEffectStarted reports that the SSH handler got past the point where the
// command could still be refused with no effect. A not-started error must
// not drop a watch for a job that is still running.
func sshEffectStarted(err error) bool {
	if err == nil {
		return true
	}
	var idle semanticEffectNotStarted
	return !errors.As(err, &idle)
}

// remoteWatchRemaining is how long the park should wait before the next
// check. A watch that has not been checked yet waits the whole interval.
func remoteWatchRemaining(watch chatTaskRemoteWatch, now time.Time) time.Duration {
	interval := watch.Interval
	if interval < remoteWatchMinInterval {
		interval = remoteWatchMinInterval
	}
	if interval > remoteWatchMaxInterval {
		interval = remoteWatchMaxInterval
	}
	if watch.CheckedAt.IsZero() || now.Before(watch.CheckedAt) {
		return interval
	}
	remaining := interval - now.Sub(watch.CheckedAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func remoteWatchProgressLine(interval time.Duration, check string) string {
	check = strings.Join(strings.Fields(strings.TrimSpace(check)), " ")
	check = truncateChatTaskText(check, 80)
	return fmt.Sprintf("宿主将在 %d 秒后再次查看：%s", waitedSeconds(interval), check)
}

func remoteWatchLivenessLine() string {
	return "命令仍在执行：宿主等待远端任务，不占用 SSH 会话"
}

func remoteWatchChangedPrompt(check, output string, execErr error) string {
	var b strings.Builder
	b.WriteString("[系统续轮] 远端检查有新情况。从当前结果接着做，不要重做已经成功的步骤。\n检查命令：")
	b.WriteString(strings.TrimSpace(check))
	b.WriteString("\n")
	if execErr != nil {
		b.WriteString("错误：")
		b.WriteString(remoteWatchErrorForPrompt(execErr))
		b.WriteString("\n")
	} else {
		b.WriteString("输出：\n")
		b.WriteString(truncateChatTaskText(strings.TrimSpace(output), 4000))
		b.WriteString("\n")
	}
	b.WriteString("如果远端任务还在进行，直接回复即可，宿主会继续查看。要结束监视，回复必须以「")
	b.WriteString(remoteWatchDoneToken)
	b.WriteString("」开头。")
	return b.String()
}

func remoteWatchErrorForPrompt(err error) string {
	if err == nil {
		return ""
	}
	return remoteWatchSoftenResend(strings.TrimSpace(err.Error()))
}

// remoteWatchSameFailure reports two failed checks that differ only by a
// clock banner inside a timeout's captured tail. The stored error stays raw.
func remoteWatchSameFailure(prev, next string) bool {
	return remoteWatchFailureKey(prev) == remoteWatchFailureKey(next)
}

func remoteWatchFailureKey(msg string) string {
	msg = strings.TrimSpace(msg)
	const prefix = "trusted_ssh_timeout:"
	if strings.HasPrefix(msg, prefix) {
		body := strings.TrimSpace(strings.TrimPrefix(msg, prefix))
		return prefix + "\n" + normalizeRemoteWatchOutput(body)
	}
	return normalizeRemoteWatchOutput(msg)
}

// remoteWatchSoftenResend rewrites the interactive recovery note. That note
// tells the model not to send the command again. This watch resends the same
// check itself, both in the tool result and in the park prompt.
func remoteWatchSoftenResend(msg string) string {
	return strings.ReplaceAll(msg, "不要直接重发同一条命令", "宿主会再次执行同一条检查")
}

func remoteWatchCeilingNote() string {
	hours := int(remoteWatchParkCeiling / time.Hour)
	if hours < 1 {
		hours = 1
	}
	return fmt.Sprintf("宿主已连续监视 %d 小时，停止自动查看。", hours)
}

func waitedSeconds(d time.Duration) int {
	secs := int(d / time.Second)
	if secs < 1 {
		return 1
	}
	return secs
}

// parseRemoteSleepPoll reports a command whose only wait is a leading sleep
// followed by a status check. The sleep may sit inside one bash -lc / bash -c
// / sh -c wrapper. A bare sleep, a sleep after another command, or a sleep
// followed by anything other than a status check is left on the SSH session.
func parseRemoteSleepPoll(command string) (wait time.Duration, check string, ok bool) {
	body := unwrapRemoteShellCommand(strings.TrimSpace(command))
	wait, rest, ok := splitLeadingSleep(body)
	if !ok {
		return 0, "", false
	}
	check = strings.TrimSpace(rest)
	if check == "" || !remoteCommandIsStatusCheck(check) {
		return 0, "", false
	}
	if wait < remoteWatchMinInterval {
		return 0, "", false
	}
	if wait > remoteWatchMaxInterval {
		wait = remoteWatchMaxInterval
	}
	return wait, check, true
}

func unwrapRemoteShellCommand(command string) string {
	s := strings.TrimSpace(command)
	for i := 0; i < 2; i++ {
		inner, ok := unwrapOneRemoteShell(s)
		if !ok {
			return s
		}
		s = strings.TrimSpace(inner)
	}
	return s
}

func unwrapOneRemoteShell(s string) (string, bool) {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "bash") && shellWordBoundary(s, len("bash")):
		s = strings.TrimSpace(s[len("bash"):])
	case strings.HasPrefix(s, "sh") && shellWordBoundary(s, len("sh")):
		s = strings.TrimSpace(s[len("sh"):])
	default:
		return "", false
	}
	flag := ""
	switch {
	case strings.HasPrefix(s, "-lc") && shellWordBoundary(s, len("-lc")):
		flag = "-lc"
	case strings.HasPrefix(s, "-c") && shellWordBoundary(s, len("-c")):
		flag = "-c"
	default:
		return "", false
	}
	s = strings.TrimSpace(s[len(flag):])
	body, ok := quotedShellBody(s)
	if !ok {
		return "", false
	}
	return body, true
}

func shellWordBoundary(s string, n int) bool {
	if len(s) == n {
		return true
	}
	return unicode.IsSpace(rune(s[n]))
}

func quotedShellBody(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	q := s[0]
	if q != '\'' && q != '"' {
		return "", false
	}
	if q == '\'' {
		end := strings.IndexByte(s[1:], '\'')
		if end < 0 {
			return "", false
		}
		end++
		if strings.TrimSpace(s[end+1:]) != "" {
			return "", false
		}
		return s[1:end], true
	}
	var b strings.Builder
	escape := false
	for i := 1; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			if strings.TrimSpace(s[i+1:]) != "" {
				return "", false
			}
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

func splitLeadingSleep(body string) (time.Duration, string, bool) {
	s := strings.TrimSpace(body)
	const key = "sleep"
	if !strings.HasPrefix(s, key) || !shellWordBoundary(s, len(key)) {
		return 0, "", false
	}
	rest := strings.TrimSpace(s[len(key):])
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 || i > 7 {
		return 0, "", false
	}
	n := 0
	for _, c := range rest[:i] {
		n = n*10 + int(c-'0')
	}
	if n <= 0 {
		return 0, "", false
	}
	// Cap before scaling by hours so the duration cannot overflow.
	if n > 100000 {
		n = 100000
	}
	rest = rest[i:]
	unit := time.Second
	if rest != "" {
		switch rest[0] {
		case 's':
			if !sleepUnitBoundary(rest) {
				return 0, "", false
			}
			rest = rest[1:]
		case 'm':
			if !sleepUnitBoundary(rest) {
				return 0, "", false
			}
			unit = time.Minute
			rest = rest[1:]
		case 'h':
			if !sleepUnitBoundary(rest) {
				return 0, "", false
			}
			unit = time.Hour
			rest = rest[1:]
		}
	}
	rest = strings.TrimSpace(rest)
	switch {
	case strings.HasPrefix(rest, "&&"):
		rest = strings.TrimSpace(rest[2:])
	case strings.HasPrefix(rest, ";"):
		rest = strings.TrimSpace(rest[1:])
	case strings.HasPrefix(rest, "\n"):
		rest = strings.TrimSpace(rest)
	default:
		return 0, "", false
	}
	if rest == "" {
		return 0, "", false
	}
	return time.Duration(n) * unit, rest, true
}

func sleepUnitBoundary(rest string) bool {
	if len(rest) <= 1 {
		return true
	}
	next := rest[1]
	return next == ';' || next == '&' || next == '\n' || next == '\r' || unicode.IsSpace(rune(next))
}

func remoteCommandIsStatusCheck(command string) bool {
	parts := splitShellList(command)
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		segs := splitShellPipes(part)
		if len(segs) == 0 {
			return false
		}
		for _, seg := range segs {
			if !remoteStatusSegment(seg) {
				return false
			}
		}
	}
	return true
}

func remoteStatusSegment(seg string) bool {
	seg = strings.TrimSpace(seg)
	if seg == "" || shellSegmentWrites(seg) {
		return false
	}
	tok := firstShellToken(seg)
	if tok == "" {
		return false
	}
	if strings.Contains(tok, "/") {
		tok = path.Base(tok)
	}
	if tok == "tail" && tailFollows(seg) {
		return false
	}
	if tok == "sed" {
		return !sedInPlace(seg)
	}
	return remoteStatusCommands[tok]
}

// tailFollows reports tail -f / -F / --follow, including a short cluster
// such as -0f or -fn. Those do not exit, so a host check would interrupt
// them and can leave the shared shell unusable.
func tailFollows(seg string) bool {
	for _, tok := range shellTokens(seg) {
		if isTailFollowFlag(tok) {
			return true
		}
	}
	return false
}

func isTailFollowFlag(tok string) bool {
	switch tok {
	case "-f", "-F", "--follow":
		return true
	}
	if strings.HasPrefix(tok, "--follow=") {
		return true
	}
	if len(tok) < 2 || tok[0] != '-' || tok[1] == '-' {
		return false
	}
	for _, r := range tok[1:] {
		if r == 'f' || r == 'F' {
			return true
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r < 'A' || (r > 'Z' && r < 'a') || r > 'z' {
			return false
		}
	}
	return false
}

func commandFollowsForever(command string) bool {
	body := unwrapRemoteShellCommand(strings.TrimSpace(command))
	for _, part := range splitShellList(body) {
		for _, seg := range splitShellPipes(part) {
			tok := firstShellToken(seg)
			if strings.Contains(tok, "/") {
				tok = path.Base(tok)
			}
			if tok == "tail" && tailFollows(seg) {
				return true
			}
		}
	}
	return false
}

// parseBareSleep reports a command that is only sleep, including one
// bash -lc / sh -c wrapper. A sleep that is followed by another command is
// not bare.
func parseBareSleep(command string) (time.Duration, bool) {
	body := strings.TrimSpace(unwrapRemoteShellCommand(strings.TrimSpace(command)))
	if body == "" {
		return 0, false
	}
	for {
		trimmed := strings.TrimSpace(strings.TrimSuffix(body, "&&"))
		trimmed = strings.TrimRight(trimmed, ";\r\n\t ")
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == body {
			break
		}
		body = trimmed
	}
	// splitLeadingSleep requires a following command. "true" is only the
	// probe, and a real command leaves something else in front of it.
	wait, rest, ok := splitLeadingSleep(body + "; true")
	if !ok || strings.TrimSpace(rest) != "true" {
		return 0, false
	}
	if wait < remoteWatchMinInterval {
		return 0, false
	}
	if wait > remoteWatchMaxInterval {
		wait = remoteWatchMaxInterval
	}
	return wait, true
}

var remoteStatusCommands = map[string]bool{
	"echo": true, "printf": true, "date": true, "true": true, "pwd": true,
	"whoami": true, "id": true, "uname": true, "hostname": true, "nproc": true,
	"tail": true, "head": true, "cat": true, "wc": true, "ls": true, "stat": true,
	"df": true, "du": true, "free": true, "uptime": true,
	"ps": true, "pgrep": true, "pidof": true, "jobs": true, "nvidia-smi": true,
	"grep": true, "egrep": true, "fgrep": true, "awk": true, "cut": true,
	"sort": true, "uniq": true, "tr": true, "test": true, "[": true,
	"basename": true, "dirname": true, "readlink": true, "realpath": true,
	"file": true, "md5sum": true, "sha256sum": true, "cksum": true, "sed": true,
}

func sedInPlace(seg string) bool {
	for _, tok := range shellTokens(seg) {
		if tok == "-i" || strings.HasPrefix(tok, "-i.") || tok == "--in-place" || strings.HasPrefix(tok, "--in-place=") {
			return true
		}
	}
	return false
}

func shellSegmentWrites(seg string) bool {
	tokens := shellTokens(seg)
	for i, tok := range tokens {
		if tok == "" {
			continue
		}
		if strings.Contains(tok, "&") && !isDupRedirect(tok) && !isNullRedirect(tok) {
			return true
		}
		if !strings.Contains(tok, ">") {
			continue
		}
		if isNullRedirect(tok) || isDupRedirect(tok) {
			continue
		}
		if (tok == ">" || tok == ">>" || tok == "2>" || tok == "2>>") && i+1 < len(tokens) && tokens[i+1] == "/dev/null" {
			continue
		}
		return true
	}
	return false
}

func isNullRedirect(tok string) bool {
	switch tok {
	case ">/dev/null", ">>/dev/null", "2>/dev/null", "2>>/dev/null", "&>/dev/null":
		return true
	default:
		return false
	}
}

func isDupRedirect(tok string) bool {
	return tok == "2>&1" || tok == ">&1" || tok == "1>&2"
}

func firstShellToken(seg string) string {
	seg = strings.TrimSpace(seg)
	for seg != "" {
		tok, rest := nextShellToken(seg)
		if tok == "" {
			return ""
		}
		if isEnvAssign(tok) {
			seg = rest
			continue
		}
		return tok
	}
	return ""
}

func isEnvAssign(tok string) bool {
	if tok == "" || strings.HasPrefix(tok, "=") || strings.HasPrefix(tok, "'") || strings.HasPrefix(tok, "\"") {
		return false
	}
	i := strings.IndexByte(tok, '=')
	return i > 0
}

func shellTokens(seg string) []string {
	var out []string
	rest := strings.TrimSpace(seg)
	for rest != "" {
		tok, next := nextShellToken(rest)
		if tok == "" {
			break
		}
		out = append(out, tok)
		rest = next
	}
	return out
}

func nextShellToken(s string) (string, string) {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return "", ""
	}
	var b strings.Builder
	inSingle, inDouble, escape := false, false, false
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if inSingle {
			b.WriteByte(c)
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if inDouble {
			b.WriteByte(c)
			if c == '\\' {
				escape = true
				continue
			}
			if c == '"' {
				inDouble = false
			}
			continue
		}
		if unicode.IsSpace(rune(c)) {
			break
		}
		switch c {
		case '\\':
			b.WriteByte(c)
			escape = true
		case '\'':
			b.WriteByte(c)
			inSingle = true
		case '"':
			b.WriteByte(c)
			inDouble = true
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), strings.TrimLeftFunc(s[i:], unicode.IsSpace)
}

func splitShellList(command string) []string {
	return splitShell(command, true)
}

func splitShellPipes(part string) []string {
	return splitShell(part, false)
}

func splitShell(command string, list bool) []string {
	var parts []string
	var b strings.Builder
	inSingle, inDouble, escape := false, false, false
	paren := 0
	flush := func() {
		part := strings.TrimSpace(b.String())
		b.Reset()
		if part != "" {
			parts = append(parts, part)
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if inSingle {
			b.WriteByte(c)
			if c == '\'' {
				inSingle = false
			}
			continue
		}
		if escape {
			b.WriteByte(c)
			escape = false
			continue
		}
		if inDouble {
			b.WriteByte(c)
			switch c {
			case '\\':
				escape = true
			case '"':
				inDouble = false
			}
			continue
		}
		switch c {
		case '\\':
			b.WriteByte(c)
			escape = true
		case '\'':
			b.WriteByte(c)
			inSingle = true
		case '"':
			b.WriteByte(c)
			inDouble = true
		case '$':
			b.WriteByte(c)
			if paren == 0 && i+1 < len(command) && command[i+1] == '(' {
				paren++
				b.WriteByte('(')
				i++
			}
		case '(':
			b.WriteByte(c)
			if paren > 0 {
				paren++
			}
		case ')':
			b.WriteByte(c)
			if paren > 0 {
				paren--
			}
		case '\n', ';':
			if list && paren == 0 {
				flush()
				continue
			}
			b.WriteByte(c)
		case '&':
			if list && paren == 0 && i+1 < len(command) && command[i+1] == '&' {
				flush()
				i++
				continue
			}
			b.WriteByte(c)
		case '|':
			if paren == 0 && i+1 < len(command) && command[i+1] == '|' {
				if list {
					flush()
					i++
					continue
				}
				b.WriteByte(c)
				continue
			}
			if !list && paren == 0 {
				flush()
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	flush()
	return parts
}

var remoteWatchClockLine = regexp.MustCompile(`^[=\-\s]*\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}[=\-\s]*$`)

// normalizeRemoteWatchOutput drops blank lines and a pure clock banner.
// A poll that prints the time before the same log tail is not a new result.
func normalizeRemoteWatchOutput(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || remoteWatchClockLine.MatchString(trim) {
			continue
		}
		kept = append(kept, trim)
	}
	return strings.Join(kept, "\n")
}

func (r *chatTaskRegistry) armRemoteWatch(id string, next chatTaskRemoteWatch) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil || task.Status.terminal() {
		return
	}
	if task.watchWake == nil {
		task.watchWake = make(chan struct{}, 1)
	}
	now := time.Now()
	next.armed = true
	next.ArmedAt = now
	if prev := task.remoteWatch; prev != nil && prev.armed && prev.Check == next.Check && prev.Interval == next.Interval && !prev.ArmedAt.IsZero() {
		next.ArmedAt = prev.ArmedAt
	}
	task.remoteWatch = &next
}

func (r *chatTaskRegistry) disarmRemoteWatch(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil || task.remoteWatch == nil {
		return
	}
	task.remoteWatch.armed = false
}

func (r *chatTaskRegistry) copyRemoteWatch(id string) (chatTaskRemoteWatch, bool) {
	if r == nil {
		return chatTaskRemoteWatch{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil || task.remoteWatch == nil || !task.remoteWatch.armed {
		return chatTaskRemoteWatch{}, false
	}
	return *task.remoteWatch, true
}

func (r *chatTaskRegistry) recordRemoteWatchResult(id, output string, execErr error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil || task.remoteWatch == nil || !task.remoteWatch.armed {
		return
	}
	output, execErr = remoteWatchNormalizeEmpty(output, execErr)
	task.remoteWatch.CheckedAt = time.Now()
	task.remoteWatch.Shown = remoteWatchShown(output, execErr, remoteWatchCheckTimeout)
	task.remoteWatch.LastStarted = sshEffectStarted(execErr)
	if execErr != nil {
		task.remoteWatch.LastError = execErr.Error()
		return
	}
	task.remoteWatch.LastOutput = output
	task.remoteWatch.LastError = ""
}

func (r *chatTaskRegistry) noteRemoteWatchCheck(id, check, output string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[strings.TrimSpace(id)]
	if task == nil || task.remoteWatch == nil || !task.remoteWatch.armed {
		return
	}
	if strings.TrimSpace(check) != "" {
		task.remoteWatch.Check = strings.TrimSpace(check)
	}
	task.remoteWatch.LastOutput = output
	task.remoteWatch.LastError = ""
	task.remoteWatch.LastStarted = true
	task.remoteWatch.Shown = trustedSSHCommandResult(output, nil, remoteWatchCheckTimeout)
	task.remoteWatch.CheckedAt = time.Now()
}

func (c *sharedAgentLoopCallbacks) isChatTaskLoop() bool {
	return c != nil && c.loopCtx != nil && strings.TrimSpace(c.loopCtx.ChatTaskID) != ""
}

// trustedSSHPrincipal is the SSH owner for this call. A background chat task
// reuses the parent conversation's session. Other tools keep semanticPrincipalID.
func (c *sharedAgentLoopCallbacks) trustedSSHPrincipal() string {
	id := c.semanticPrincipalID()
	if !c.isChatTaskLoop() {
		return id
	}
	return chatTaskParentPrincipal(id)
}

func (c *sharedAgentLoopCallbacks) maybeHostRemoteSleepPoll(principal, command string, timeout time.Duration) (string, bool) {
	if !c.isChatTaskLoop() {
		return "", false
	}
	pollWait, check, ok := parseRemoteSleepPoll(command)
	if ok {
		return c.runChatTaskSleepPoll(principal, pollWait, check, timeout), true
	}
	if commandFollowsForever(command) {
		log.Printf("[remote-watch] follow check refused task=%s", chatTaskIDOf(c))
		msg := semanticUnexecutedRejection(fmt.Errorf("remote_watch_check_must_exit: use a check that exits, such as tail -n"))
		if extra, ok := c.armedWatchReply(); ok {
			msg += "\n" + extra
		}
		return msg, true
	}
	if _, bare := parseBareSleep(command); bare {
		if body, ok := c.answerArmedBareSleep(principal); ok {
			return body, true
		}
	}
	return "", false
}

func chatTaskIDOf(c *sharedAgentLoopCallbacks) string {
	if c == nil || c.loopCtx == nil {
		return ""
	}
	return strings.TrimSpace(c.loopCtx.ChatTaskID)
}

func (c *sharedAgentLoopCallbacks) copyOwnRemoteWatch() (chatTaskRemoteWatch, bool) {
	if !c.isChatTaskLoop() || c.handler == nil {
		return chatTaskRemoteWatch{}, false
	}
	reg := c.handler.ensureChatTasks()
	if reg == nil {
		return chatTaskRemoteWatch{}, false
	}
	return reg.copyRemoteWatch(c.loopCtx.ChatTaskID)
}

// answerArmedBareSleep keeps a bare sleep off the parent SSH session.
// The host already waits between checks. A due sample is checked now.
func (c *sharedAgentLoopCallbacks) answerArmedBareSleep(principal string) (string, bool) {
	watch, ok := c.copyOwnRemoteWatch()
	if !ok {
		return "", false
	}
	log.Printf("[remote-watch] bare sleep kept on host task=%s", chatTaskIDOf(c))
	// A sample that was written is reused, including a started failure.
	// A check that never started runs now, so a reconnect is not answered
	// with the old error.
	remain := remoteWatchRemaining(watch, time.Now())
	if remain > 0 && strings.TrimSpace(watch.Shown) != "" && (watch.LastError == "" || watch.LastStarted) {
		c.emitRemoteWatchProgress(remoteWatchProgressLine(remain, watch.Check))
		return remoteWatchToolBody(watch.Shown, watch.Interval), true
	}
	return c.runChatTaskSleepPoll(principal, watch.Interval, watch.Check, remoteWatchCheckTimeout), true
}

func (c *sharedAgentLoopCallbacks) armedWatchReply() (string, bool) {
	watch, ok := c.copyOwnRemoteWatch()
	if !ok {
		return "", false
	}
	shown := strings.TrimSpace(watch.Shown)
	if shown == "" {
		shown = strings.TrimSpace(watch.LastOutput)
	}
	return remoteWatchToolBody(shown, watch.Interval), true
}

func (c *sharedAgentLoopCallbacks) runChatTaskSleepPoll(principal string, wait time.Duration, check string, timeout time.Duration) string {
	taskID := ""
	if c != nil && c.loopCtx != nil {
		taskID = strings.TrimSpace(c.loopCtx.ChatTaskID)
	}
	if body, remain, ok := c.freshRemoteWatchBody(principal, check, wait); ok {
		log.Printf("[remote-watch] fresh check reused task=%s", taskID)
		c.emitRemoteWatchProgress(remoteWatchProgressLine(remain, check))
		return body
	}
	checkTimeout := remoteWatchCheckTimeout
	if timeout > 0 && timeout < checkTimeout {
		checkTimeout = timeout
	}
	log.Printf("[remote-watch] check now task=%s", taskID)
	c.emitRemoteWatchProgress(remoteWatchProgressLine(wait, check))
	text, err := c.handler.executeTrustedSSH(principal, check, checkTimeout)
	text, err = remoteWatchNormalizeEmpty(text, err)
	c.armOwnRemoteWatch(principal, check, wait, text, err, checkTimeout)
	return remoteWatchToolBody(remoteWatchShown(text, err, checkTimeout), wait)
}

// freshRemoteWatchBody returns the last result when this same check was
// already written inside the interval. A started failure is that result:
// running it again would interrupt the shared shell. A check that was not
// started is run again, so a reconnect is not answered with the old error.
// The tool must not sleep and must not open SSH for a fresh sample.
func (c *sharedAgentLoopCallbacks) freshRemoteWatchBody(principal, check string, wait time.Duration) (string, time.Duration, bool) {
	if c == nil || c.handler == nil || c.loopCtx == nil || wait <= 0 {
		return "", 0, false
	}
	reg := c.handler.ensureChatTasks()
	if reg == nil {
		return "", 0, false
	}
	watch, ok := reg.copyRemoteWatch(c.loopCtx.ChatTaskID)
	if !ok || watch.Check != check || watch.Interval != wait {
		return "", 0, false
	}
	if p := strings.TrimSpace(watch.Principal); p != "" && p != principal {
		return "", 0, false
	}
	if watch.CheckedAt.IsZero() || strings.TrimSpace(watch.Shown) == "" || (watch.LastError != "" && !watch.LastStarted) {
		return "", 0, false
	}
	remain := remoteWatchRemaining(watch, time.Now())
	if remain <= 0 {
		return "", 0, false
	}
	return remoteWatchToolBody(watch.Shown, wait), remain, true
}

func (c *sharedAgentLoopCallbacks) emitRemoteWatchProgress(line string) {
	if c == nil || c.onProgress == nil || strings.TrimSpace(line) == "" {
		return
	}
	c.onProgress(line)
}

func (c *sharedAgentLoopCallbacks) armOwnRemoteWatch(principal, check string, wait time.Duration, output string, execErr error, timeout time.Duration) {
	if c == nil || c.handler == nil || c.loopCtx == nil {
		return
	}
	reg := c.handler.ensureChatTasks()
	if reg == nil {
		return
	}
	next := chatTaskRemoteWatch{
		Principal:   principal,
		Check:       check,
		Interval:    wait,
		CheckedAt:   time.Now(),
		Shown:       remoteWatchShown(output, execErr, timeout),
		LastStarted: sshEffectStarted(execErr),
	}
	if execErr != nil {
		next.LastError = execErr.Error()
	} else {
		next.LastOutput = output
	}
	reg.armRemoteWatch(c.loopCtx.ChatTaskID, next)
}

func (c *sharedAgentLoopCallbacks) disarmOwnRemoteWatch() {
	if !c.isChatTaskLoop() || c.handler == nil {
		return
	}
	if reg := c.handler.ensureChatTasks(); reg != nil {
		reg.disarmRemoteWatch(c.loopCtx.ChatTaskID)
	}
}

// noteOrDisarmRemoteWatch keeps a status check on the open watch and drops
// the watch only after some other command was written. A not-started error
// leaves the watch armed.
func (c *sharedAgentLoopCallbacks) noteOrDisarmRemoteWatch(command, text string, err error) {
	if !c.isChatTaskLoop() {
		return
	}
	if remoteCommandIsStatusCheck(command) {
		if err == nil {
			c.noteChatTaskRemoteWatchOutput(command, text)
		}
		return
	}
	if sshEffectStarted(err) {
		c.disarmOwnRemoteWatch()
	}
}

// disarmOwnRemoteWatchAfterEffect drops the watch when a download got past
// argument refusal. A path check that never copied leaves the watch armed.
func (c *sharedAgentLoopCallbacks) disarmOwnRemoteWatchAfterEffect(result string) {
	if !c.isChatTaskLoop() {
		return
	}
	if tool.ArgumentRejectionWithoutEffect(tool.SelectionExecutionResult{Result: result}) {
		return
	}
	c.disarmOwnRemoteWatch()
}

func (c *sharedAgentLoopCallbacks) noteChatTaskRemoteWatchOutput(check, output string) {
	if !c.isChatTaskLoop() || c.handler == nil {
		return
	}
	reg := c.handler.ensureChatTasks()
	if reg == nil {
		return
	}
	reg.noteRemoteWatchCheck(c.loopCtx.ChatTaskID, check, output)
}

func (h *IMMessageHandler) driveChatTaskWaves(task *chatTaskRecord, run chatTaskWaveRunner) chatTaskRunResult {
	if h == nil || task == nil || run == nil {
		return chatTaskRunResult{Err: fmt.Errorf("chat task handler is missing")}
	}
	text := task.Prompt
	if strings.TrimSpace(task.resumePrompt) != "" {
		text = task.resumePrompt
	}
	kind := chatTaskWaveFirst
	runs := 0
	var last chatTaskWaveOutcome
	for {
		if runs >= sessionRoundContinuationMaxWaves {
			return h.chatTaskWaveCapResult(task, last)
		}
		if kind == chatTaskWaveSessionRound {
			h.bindPendingMaxRoundsSlot(task.MemoryKey)
			text = sessionRoundContinuationPrompt(task.Prompt)
		}
		outcome := run(task, runs, text)
		runs++
		last = outcome
		if outcome.cancelled || (outcome.loop != nil && outcome.loop.IsCancelled()) {
			h.disarmChatTaskRemoteWatch(task.ID)
			return chatTaskRunResult{Err: fmt.Errorf("canceled")}
		}
		if outcome.resp != nil && outcome.resp.sessionRoundContinue {
			kind = chatTaskWaveSessionRound
			continue
		}
		prompt, result, finished := h.parkChatTaskUntilChange(task, outcome)
		if finished {
			return result
		}
		text = prompt
		kind = chatTaskWaveRemoteWatch
	}
}

const (
	chatTaskWaveFirst = iota
	chatTaskWaveSessionRound
	chatTaskWaveRemoteWatch
)

func (h *IMMessageHandler) parkChatTaskUntilChange(task *chatTaskRecord, outcome chatTaskWaveOutcome) (string, chatTaskRunResult, bool) {
	for {
		if h.chatTaskStopRequested(task, outcome.loop) {
			h.disarmChatTaskRemoteWatch(task.ID)
			return "", chatTaskRunResult{Err: fmt.Errorf("canceled")}, true
		}
		switch h.decideChatTaskStop(task, outcome.resp) {
		case chatTaskStopCeiling:
			return "", h.chatTaskRemoteWatchCeilingResult(task, outcome), true
		case chatTaskStopFinish:
			h.disarmChatTaskRemoteWatch(task.ID)
			return "", chatTaskResultFromAgentResponse(outcome.resp, outcome.loop), true
		}
		if prompt, ok := h.remoteWatchSteerPrompt(task); ok {
			log.Printf("[remote-watch] steer wake task=%s", task.ID)
			return prompt, chatTaskRunResult{}, false
		}
		watch, ok := h.copyChatTaskRemoteWatch(task.ID)
		if !ok {
			h.disarmChatTaskRemoteWatch(task.ID)
			return "", chatTaskResultFromAgentResponse(outcome.resp, outcome.loop), true
		}
		delay := remoteWatchRemaining(watch, time.Now())
		if delay > 0 {
			h.noteChatTaskRemoteWatchProgress(task.ID, watch, delay)
			log.Printf("[remote-watch] host wait %ds task=%s", waitedSeconds(delay), task.ID)
			switch h.waitChatTaskRemoteWatch(task, outcome.loop, delay) {
			case remoteWatchWaitCanceled:
				h.disarmChatTaskRemoteWatch(task.ID)
				return "", chatTaskRunResult{Err: fmt.Errorf("canceled")}, true
			case remoteWatchWaitSteer:
				continue
			}
		}
		if h.chatTaskStopRequested(task, outcome.loop) {
			h.disarmChatTaskRemoteWatch(task.ID)
			return "", chatTaskRunResult{Err: fmt.Errorf("canceled")}, true
		}
		changed, prompt := h.recheckChatTaskRemoteWatch(task)
		if h.chatTaskStopRequested(task, outcome.loop) {
			h.disarmChatTaskRemoteWatch(task.ID)
			return "", chatTaskRunResult{Err: fmt.Errorf("canceled")}, true
		}
		if changed {
			log.Printf("[remote-watch] output changed task=%s", task.ID)
			return prompt, chatTaskRunResult{}, false
		}
	}
}

func (h *IMMessageHandler) decideChatTaskStop(task *chatTaskRecord, resp *IMAgentResponse) chatTaskStopKind {
	if task == nil || resp == nil {
		return chatTaskStopFinish
	}
	// The shared loop clears this error before the response is built. If a
	// wave still carries it, the iteration budget ran out while the remote
	// job is open. Parking keeps the host check. Any other error finishes.
	if strings.TrimSpace(resp.Error) == "max iterations reached" {
		if _, armed := h.copyChatTaskRemoteWatch(task.ID); armed {
			resp.Error = ""
		}
	}
	if strings.TrimSpace(resp.Error) != "" {
		return chatTaskStopFinish
	}
	if chatTaskReplyClosesRemoteWatch(resp.Text) {
		return chatTaskStopFinish
	}
	watch, ok := h.copyChatTaskRemoteWatch(task.ID)
	if !ok {
		return chatTaskStopFinish
	}
	if !watch.ArmedAt.IsZero() && time.Since(watch.ArmedAt) >= remoteWatchParkCeiling {
		return chatTaskStopCeiling
	}
	return chatTaskStopPark
}

func (h *IMMessageHandler) chatTaskRemoteWatchCeilingResult(task *chatTaskRecord, outcome chatTaskWaveOutcome) chatTaskRunResult {
	h.disarmChatTaskRemoteWatch(task.ID)
	resp := outcome.resp
	if resp == nil {
		resp = &IMAgentResponse{}
	}
	note := remoteWatchCeilingNote()
	text := strings.TrimSpace(resp.Text)
	if text == "" {
		text = note
	} else if !strings.Contains(text, note) {
		text = text + "\n" + note
	}
	resp.Text = text
	resp.sessionRoundContinue = false
	log.Printf("[remote-watch] park ceiling task=%s", task.ID)
	return chatTaskResultFromAgentResponse(resp, outcome.loop)
}

func (h *IMMessageHandler) chatTaskWaveCapResult(task *chatTaskRecord, last chatTaskWaveOutcome) chatTaskRunResult {
	if last.resp == nil {
		if task != nil {
			h.disarmChatTaskRemoteWatch(task.ID)
		}
		return chatTaskResultFromAgentResponse(nil, last.loop)
	}
	last.resp.sessionRoundContinue = false
	last.resp.Text = sessionRoundManualContinueText
	if task != nil {
		if watch, ok := h.copyChatTaskRemoteWatch(task.ID); ok {
			h.disarmChatTaskRemoteWatch(task.ID)
			last.resp.Text += "\n宿主停止了自动查看。"
			if tail := strings.TrimSpace(normalizeRemoteWatchOutput(watch.LastOutput)); tail != "" {
				last.resp.Text += "\n" + truncateChatTaskText(tail, 400)
			}
		}
	}
	return chatTaskResultFromAgentResponse(last.resp, last.loop)
}

func (h *IMMessageHandler) recheckChatTaskRemoteWatch(task *chatTaskRecord) (bool, string) {
	if h == nil || task == nil {
		return false, ""
	}
	watch, ok := h.copyChatTaskRemoteWatch(task.ID)
	if !ok {
		return false, ""
	}
	principal := strings.TrimSpace(watch.Principal)
	if principal == "" {
		principal = chatTaskParentPrincipal(task.MemoryKey)
	}
	text, err := h.executeTrustedSSH(principal, watch.Check, remoteWatchCheckTimeout)
	text, err = remoteWatchNormalizeEmpty(text, err)
	reg := h.ensureChatTasks()
	if err != nil {
		same := watch.LastError != "" && remoteWatchSameFailure(watch.LastError, err.Error())
		if reg != nil {
			reg.recordRemoteWatchResult(task.ID, "", err)
		}
		if same {
			return false, ""
		}
		return true, remoteWatchChangedPrompt(watch.Check, "", err)
	}
	if watch.LastError == "" && normalizeRemoteWatchOutput(text) == normalizeRemoteWatchOutput(watch.LastOutput) {
		if reg != nil {
			reg.recordRemoteWatchResult(task.ID, text, nil)
		}
		return false, ""
	}
	if reg != nil {
		reg.recordRemoteWatchResult(task.ID, text, nil)
	}
	return true, remoteWatchChangedPrompt(watch.Check, text, nil)
}

func (h *IMMessageHandler) copyChatTaskRemoteWatch(id string) (chatTaskRemoteWatch, bool) {
	if h == nil {
		return chatTaskRemoteWatch{}, false
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return chatTaskRemoteWatch{}, false
	}
	return reg.copyRemoteWatch(id)
}

func (h *IMMessageHandler) disarmChatTaskRemoteWatch(id string) {
	if h == nil {
		return
	}
	if reg := h.ensureChatTasks(); reg != nil {
		reg.disarmRemoteWatch(id)
	}
}

func (h *IMMessageHandler) noteChatTaskRemoteWatchProgress(id string, watch chatTaskRemoteWatch, delay time.Duration) {
	reg := h.ensureChatTasks()
	if reg == nil {
		return
	}
	reg.noteProgress(id, remoteWatchProgressLine(delay, watch.Check))
}

type remoteWatchWaitStop int

const (
	remoteWatchWaitDone remoteWatchWaitStop = iota
	remoteWatchWaitCanceled
	remoteWatchWaitSteer
)

func (h *IMMessageHandler) remoteWatchSteerPrompt(task *chatTaskRecord) (string, bool) {
	if h == nil || task == nil {
		return "", false
	}
	if strings.TrimSpace(h.pendingSteerInjectionNote(task.MemoryKey)) == "" {
		return "", false
	}
	return "[系统续轮] 用户补充了一条要求。把这条要求纳入当前监视，不要重做已经成功的步骤。如果远端任务还在进行，直接回复即可，宿主会继续查看。要结束监视，回复必须以「" + remoteWatchDoneToken + "」开头。", true
}

func (h *IMMessageHandler) waitChatTaskRemoteWatch(task *chatTaskRecord, loop *LoopContext, d time.Duration) remoteWatchWaitStop {
	ctx, cancel := chatTaskCancelContext(task, loop)
	defer cancel()
	var wake <-chan struct{}
	if h != nil && task != nil {
		if reg := h.ensureChatTasks(); reg != nil {
			wake = reg.signalArmedRemoteWatch(task.MemoryKey)
		}
	}
	steered := make(chan struct{}, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-wake:
			select {
			case steered <- struct{}{}:
			default:
			}
			cancel()
		}
	}()
	err := waitRemoteWatch(ctx, d, func() {
		if h == nil || task == nil {
			return
		}
		if reg := h.ensureChatTasks(); reg != nil {
			reg.noteLiveness(task.ID, remoteWatchLivenessLine())
		}
	})
	select {
	case <-steered:
		return remoteWatchWaitSteer
	default:
	}
	if err != nil || h.chatTaskStopRequested(task, loop) {
		return remoteWatchWaitCanceled
	}
	return remoteWatchWaitDone
}

func (h *IMMessageHandler) chatTaskStopRequested(task *chatTaskRecord, loop *LoopContext) bool {
	if loop != nil && loop.IsCancelled() {
		return true
	}
	if h == nil || task == nil {
		return false
	}
	reg := h.ensureChatTasks()
	if reg == nil {
		return false
	}
	return reg.isTerminal(task.ID)
}

func chatTaskCancelContext(task *chatTaskRecord, loop *LoopContext) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	var loopC <-chan struct{}
	if loop != nil {
		loopC = loop.CancelC
	}
	var taskC <-chan struct{}
	if task != nil {
		taskC = task.cancelCh
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-loopC:
			cancel()
		case <-taskC:
			cancel()
		}
	}()
	return ctx, cancel
}
