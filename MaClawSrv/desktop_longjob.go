package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
)

// Long-running jobs are decoupled from the chat turn that started them
// (macmini training, Nov 11: the bot blocked on bash until the run budget
// expired). The interactive run starts the work in the background, records
// first-class job state, and returns. A watcher (the existing scheduler
// plus a 30s tick, not a new service) polls pid/log/exit, posts progress
// on a cadence or milestone, and on stall/fail/finish resumes the same
// bot so it can report honestly or self-correct. Talking to the bot does
// not cancel the job.

const (
	desktopLongJobTaskType   = "watch"
	desktopLongJobTaskPrefix = "watch-job:"
	desktopLongJobTickEvery  = 30 * time.Second
	desktopLongJobStallAfter = 15 * time.Minute
)

// A running job's progress line goes out at most once per
// desktopLongJobNoticeEvery (first one no sooner than that after the start)
// and only when its measured lines (epoch, step, loss, metrics) changed: a
// new log line is not news (Daniel 11-11 20:38: too frequent). Finish,
// failure and stall go out at once; a job still stuck is repeated once per
// desktopLongJobHeartbeatEvery.
var (
	desktopLongJobCheckMin       = desktopEnvMinutes("MACLAW_LONGJOB_CHECK_MIN_MINUTES", 2*time.Minute)
	desktopLongJobCheckMax       = desktopEnvMinutes("MACLAW_LONGJOB_CHECK_MAX_MINUTES", 6*time.Hour)
	desktopLongJobCheckDefault   = desktopEnvMinutes("MACLAW_LONGJOB_CHECK_DEFAULT_MINUTES", 15*time.Minute)
	desktopLongJobHeartbeatEvery = desktopEnvMinutes("MACLAW_LONGJOB_STALL_REPEAT_MINUTES", 0)
)

var desktopLongJobNow = time.Now

// desktopLongJobTalk starts a short bot turn that can send_to_user. Tests replace it.
var desktopLongJobTalk = desktopLongJobTalkDefault

// desktopLongJobExec, when set, runs a short bash command in the job's desktop.
// Tests replace it. The default goes through operateDesktopBody with an
// internal flag so the start/poll scripts are not themselves handed off.
var desktopLongJobExec func(context.Context, agentruntime.Scope, string) (string, error)

func desktopLongJobRun(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	if fn := desktopLongJobExec; fn != nil {
		return fn(ctx, scope, command)
	}
	return desktopLongJobExecDefault(ctx, scope, command)
}

type desktopLongJobInternalCtxKey struct{}

func withDesktopLongJobInternal(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, desktopLongJobInternalCtxKey{}, true)
}

func desktopLongJobInternal(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(desktopLongJobInternalCtxKey{}).(bool)
	return v
}

type desktopLongJob struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	InstanceID string `json:"instance_id"`
	SessionID  string `json:"session_id"`
	BotID      string `json:"bot_id,omitempty"`
	SourceRun  string `json:"source_run,omitempty"`
	// ToolCallID is the model call that started this job. Resume probes this
	// job, not whichever job the run started last.
	ToolCallID string `json:"tool_call_id,omitempty"`
	Command    string `json:"command"`
	Dir        string `json:"dir"`
	LogFile    string `json:"log_file"`
	PidFile    string `json:"pid_file"`
	ExitFile   string `json:"exit_file"`
	PID        string `json:"pid,omitempty"`
	// PGID is the process group setsid gave this job. ProcStart is the
	// leader's /proc starttime, so a recycled pid is not killed.
	PGID      string `json:"pgid,omitempty"`
	ProcStart string `json:"proc_start,omitempty"`
	Status    string `json:"status"` // running, stalled, failed, done, cancelled
	// EndReason is why a cancelled job stopped. It is not parsed from a log line.
	EndReason string `json:"end_reason,omitempty"`
	Alive     bool   `json:"alive"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	// OutcomeGoal groups terminal jobs that share one conversation goal.
	// OutcomeState is pending, queued, sent, or folded. OutcomeRun is the
	// one summary turn. OutcomeWait is a run that has to end before that
	// turn starts. The raw log stays on LastTail and in the completion file.
	OutcomeGoal  string        `json:"outcome_goal,omitempty"`
	OutcomeState string        `json:"outcome_state,omitempty"`
	OutcomeRun   string        `json:"outcome_run,omitempty"`
	OutcomeWait  string        `json:"outcome_wait,omitempty"`
	Progress     string        `json:"progress,omitempty"`
	LastTail     string        `json:"last_tail,omitempty"`
	Fingerprint  string        `json:"fingerprint,omitempty"`
	ScheduleID   string        `json:"schedule_id,omitempty"`
	Started      time.Time     `json:"started"`
	LastPoll     time.Time     `json:"last_poll,omitempty"`
	LastChange   time.Time     `json:"last_change,omitempty"`
	LastNotice   time.Time     `json:"last_notice,omitempty"`
	NextPoll     time.Time     `json:"next_poll,omitempty"`
	StallAfter   time.Duration `json:"stall_after,omitempty"`
	// KeepRunning is the bot's declaration (keep_running=true) that this
	// process is the deliverable, not pending work. See desktopLongJobKeepsRunning.
	KeepRunning bool `json:"keep_running,omitempty"`
	// PrincipalTenant/User own the bot's runs on this server. TenantID and
	// UserID above are the desktop (Hub) account, which is not a principal
	// here: a resume posted with it found no instance (papers 2026-11-11).
	PrincipalTenant string `json:"principal_tenant,omitempty"`
	PrincipalUser   string `json:"principal_user,omitempty"`
	// ReportedStatus is the last status the person (or the live run that
	// polled the job) was told. A stall, failure or finish is reported until
	// one report gets through, not once per status change.
	ReportedStatus string `json:"reported_status,omitempty"`
	// ReportedMetrics: the measured lines the person was last told.
	ReportedMetrics string `json:"reported_metrics,omitempty"`
	// NextCheck is when the watcher next reads the job for the person: an
	// estimate from the job's own progress (time per epoch/step × what is
	// left), the bot's own check_minutes, or a default that backs off. Polls
	// in between only look for an end or a stall, silently.
	NextCheck   time.Time `json:"next_check,omitempty"`
	CheckReason string    `json:"check_reason,omitempty"`
	CheckSet    bool      `json:"check_set,omitempty"` // the bot set NextCheck
	CheckMisses int       `json:"check_misses,omitempty"`
	// HubMisses counts Hub 404s while the instance row is still here.
	// The watch backs off, then stops. It does not retry forever.
	HubMisses   int       `json:"hub_misses,omitempty"`
	EstimateEnd time.Time `json:"estimate_end,omitempty"`
	OverdueSaid bool      `json:"overdue_said,omitempty"`
	// DeadlineSaid: the person was told the estimate runs past the
	// command's own time limit.
	DeadlineSaid bool `json:"deadline_said,omitempty"`
	// Experiment rounds (desktop_longjob_iterate.go): Lineage is the first
	// round's job id, Round counts from 1; the bot records each round's
	// main metric and its decision.
	Lineage         string   `json:"lineage,omitempty"`
	Round           int      `json:"round,omitempty"`
	RoundMetric     *float64 `json:"round_metric,omitempty"`
	RoundMetricName string   `json:"round_metric_name,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	DecisionReason  string   `json:"decision_reason,omitempty"`
}

func (j *desktopLongJob) stallBound() time.Duration {
	if j != nil && j.StallAfter > 0 {
		return j.StallAfter
	}
	return desktopLongJobStallAfter
}

// desktopLongJobShouldStart: an explicit background flag, a detach-shaped
// command, or a long-running intent (not a short help/version call).
func desktopLongJobShouldStart(args map[string]any) bool {
	if desktopArgBool(args, "background") || desktopArgBool(args, "detach") || desktopArgBool(args, "async") || desktopArgBool(args, "keep_running") {
		return true
	}
	if n := desktopLongJobExpectMinutes(args); n >= 2 {
		return true
	}
	cmd := strings.TrimSpace(desktopArg(args, "command"))
	return desktopLongJobCommand(cmd)
}

func desktopLongJobExpectMinutes(args map[string]any) int {
	for _, key := range []string{"expect_minutes", "minutes", "duration_minutes"} {
		if n, ok := desktopLongJobIntArg(args, key); ok {
			return n
		}
	}
	return 0
}

func desktopLongJobIntArg(args map[string]any, key string) (int, bool) {
	if args == nil {
		return 0, false
	}
	switch v := args[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		return n, err == nil
	}
	return 0, false
}

var (
	desktopLongJobDetachRe = regexp.MustCompile(`(?i)(?:^|[;&\n]\s*)nohup\b|(?:^|[;&\n]\s*)systemd-run\b|(?:^|[;&\n]\s*)(?:tmux\s+new|screen\s+-d?m)\b|(?:^|[\s;])&(?:\s*(?:#.*)?)$`)
	desktopLongJobIntentRe = regexp.MustCompile(`(?i)(^|[^\w])(train|training|finetune|fine-?tune|compile|cmake\s+--build|cargo\s+build|mvn\s+(?:package|install)|gradle(?:w)?\s+build|make\s+-j|torchrun|deepspeed|huggingface-cli\s+download|git\s+clone|rsync\s|训练|编译|微调|下载)([^\w]|$)`)
	desktopLongJobShortRe  = regexp.MustCompile(`(?i)(^|\s)(--help|-h|--version|-V|help|version)(\s|$)`)
	desktopLongJobSSHRe    = regexp.MustCompile(`(?i)(?:^|[\s;])(?:sshpass\b.*\s)?ssh\s`)
	desktopLongJobNohupRe  = regexp.MustCompile(`(?i)nohup\b`)
	desktopLongJobQuoteRe  = regexp.MustCompile(`('([^']*)'|"([^"]*)")\s*$`)
	desktopLongJobMileRe   = regexp.MustCompile(`(?i)(epoch\s*\d|step\s+\d|iter(ation)?\s*\d|loss\s*[:=]|acc(uracy)?\s*[:=]|val_|f1\s*[:=]|auc\s*[:=]|\beta\b|checkpoint|saved (to|model)|oom|exception|traceback|error:|failed|finished|training complete|completed successfully)`)
)

// desktopLongJobCommand reports whether cmd runs a genuinely long external
// job (training, a build, a big download) or detaches one. Only the command
// words count: text written through a heredoc, echo or cat, a loop over
// names like "train dev test", or a sleep/tail wait never does (papers
// 2026-11-11: a 小红书 draft written with cat <<EOF mentioned 训练 and the
// publish run ended "running"; "sleep 780; tail log" waits became jobs).
func desktopLongJobCommand(cmd string) bool {
	cmd = strings.TrimSpace(desktopLongJobStripHeredocs(cmd))
	if cmd == "" {
		return false
	}
	for _, seg := range desktopLongJobSegments(cmd, 0) {
		if desktopLongJobSegmentLong(seg) {
			return true
		}
	}
	return false
}

var (
	desktopLongJobHeredocRe = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	desktopLongJobSplitRe   = regexp.MustCompile(`\s*(?:&&|\|\||;|\n|\|)\s*`)
	desktopLongJobRunnerRe  = regexp.MustCompile(`(?i)^(?:\S*/)?(?:python[0-9.]*|torchrun|deepspeed|accelerate|bash|sh)$`)
	desktopLongJobScriptRe  = regexp.MustCompile(`(?i)(train|finetune|fine-?tune|训练|微调)`)
	desktopLongJobBuildRe   = regexp.MustCompile(`(?i)^(?:cmake\s+--build|cargo\s+build|mvn\s+(?:package|install)|\S*gradlew?\s+build|make\s+-j|ninja\s|npm\s+run\s+build|docker\s+build|huggingface-cli\s+download|hf\s+download|git\s+clone|rsync\s|wget\s|ollama\s+pull)`)
	desktopLongJobPrefixRe  = regexp.MustCompile(`^(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+|nohup\s+|nice(?:\s+-n\s*-?\d+)?\s+|time\s+|timeout\s+(?:-\S+\s+)*\d+[smh]?\s+|env\s+|exec\s+|setsid\s+|stdbuf\s+\S+\s+)+`)
)

// desktopLongJobStripHeredocs drops heredoc bodies: they are data.
func desktopLongJobStripHeredocs(cmd string) string {
	lines := strings.Split(cmd, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		out = append(out, lines[i])
		m := desktopLongJobHeredocRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != m[1] {
			i++
		}
		if i+1 < len(lines) {
			i++
		}
	}
	return strings.Join(out, "\n")
}

// desktopLongJobSegments splits cmd into simple commands, unwrapping the
// quoted remote command of ssh (and bash -c) so it is judged by its words.
func desktopLongJobSegments(cmd string, depth int) []string {
	var segs []string
	for _, seg := range desktopLongJobSplitRe.Split(desktopLongJobMaskQuotes(cmd), -1) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		segs = append(segs, seg)
	}
	if depth > 2 {
		return segs
	}
	// Remote and nested shells: judge the quoted command too.
	for _, q := range desktopLongJobInnerCommands(cmd) {
		// A heredoc inside the quoted remote command is data too (papers
		// 11-11 20:35: ssh host 'cat > common.py <<"PYEOF" …' became a job).
		segs = append(segs, desktopLongJobSegments(desktopLongJobStripHeredocs(q), depth+1)...)
	}
	return segs
}

var desktopLongJobInnerRe = regexp.MustCompile(`(?:\bssh\b[^'"]*?|\b(?:bash|sh)\s+-l?c\s+)(?:'([^']*)'|"([^"]*)")`)

func desktopLongJobInnerCommands(cmd string) []string {
	var out []string
	for _, m := range desktopLongJobInnerRe.FindAllStringSubmatch(cmd, -1) {
		if m[1] != "" {
			out = append(out, m[1])
		} else if m[2] != "" {
			out = append(out, m[2])
		}
	}
	return out
}

// desktopLongJobMaskQuotes blanks quoted text so separators and words inside
// quotes (an echo'd sentence, a python -c program) are not commands.
func desktopLongJobMaskQuotes(cmd string) string {
	b := []rune(cmd)
	var q rune
	for i, r := range b {
		switch {
		case q == 0 && (r == '\'' || r == '"'):
			q = r
		case q != 0 && r == q:
			q = 0
		case q != 0:
			b[i] = 'x'
		}
	}
	return string(b)
}

func desktopLongJobSegmentLong(seg string) bool {
	seg = strings.TrimSpace(strings.TrimLeft(seg, "({ "))
	if desktopLongJobShortRe.MatchString(seg) {
		return false
	}
	detached := strings.HasSuffix(seg, "&") || strings.HasPrefix(seg, "nohup ") || strings.HasPrefix(seg, "setsid ") || strings.HasPrefix(seg, "systemd-run ") || strings.HasPrefix(seg, "tmux new") || strings.HasPrefix(seg, "screen -dm")
	body := strings.TrimSpace(desktopLongJobPrefixRe.ReplaceAllString(seg, ""))
	if body == "" {
		return false
	}
	if detached {
		// The command already puts its process in the background and
		// returns at once: the wrapper would only see its own shell end
		// (papers 11-11 20:33: `nohup python3 snake.py &` — a launched game
		// — became a job that "failed", the step took 1m57s and returned
		// no output; Bot 1's nohup training was reported 已结束 while it
		// still ran). It runs as a plain step; the bot checks the log.
		return false
	}
	if desktopLongJobBuildRe.MatchString(body) {
		return true
	}
	fields := strings.Fields(body)
	if desktopLongJobRunnerRe.MatchString(fields[0]) {
		// A runner counts when what it runs is training (script name or
		// arguments).
		return desktopLongJobScriptRe.MatchString(strings.Join(fields[1:], " "))
	}
	return false
}

func desktopLongJobScratch(scope agentruntime.Scope, id string) string {
	bot := "job"
	if _, b := desktop.SplitDesktopUser(scope.UserID); b != "" {
		bot = b
	} else if scope.InstanceID != "" {
		bot = scope.InstanceID
	}
	return path.Join("/tmp", "maclaw-"+bot, "jobs", id)
}

func desktopLongJobQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// desktopLongJobInjectRemoteNohup keeps a remote command alive if the local
// ssh client later drops (SIGHUP). A command that already uses nohup is left.
func desktopLongJobInjectRemoteNohup(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if !desktopLongJobSSHRe.MatchString(cmd) || desktopLongJobNohupRe.MatchString(cmd) {
		return cmd
	}
	loc := desktopLongJobQuoteRe.FindStringSubmatchIndex(cmd)
	if loc == nil {
		return cmd
	}
	q := cmd[loc[0]:loc[1]]
	inner := ""
	if q[0] == '\'' {
		inner = strings.TrimSuffix(strings.TrimPrefix(q, "'"), "'")
	} else {
		inner = strings.TrimSuffix(strings.TrimPrefix(q, `"`), `"`)
	}
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return cmd
	}
	// The remote job is detached so it survives a dropped ssh, but this
	// session follows it to the end: the local wrapper stays alive exactly as
	// long as the remote job, streams its log and carries its exit code.
	// Returning at once (echo $!) left the watcher a job that "finished" in a
	// second while training went on unwatched (papers 11-11 20:39-20:45).
	rlog := "/tmp/maclaw-job-$$.log"
	wrapped := "nohup bash -lc " + desktopLongJobQuote(inner) + " >" + rlog + " 2>&1 & p=$!; echo LONGJOB_REMOTE_PID=$p; " +
		"tail -f " + rlog + " & t=$!; wait $p; rc=$?; sleep 1; kill $t 2>/dev/null; exit $rc"
	if q[0] == '\'' {
		return cmd[:loc[0]] + desktopLongJobQuote(wrapped) + cmd[loc[1]:]
	}
	return cmd[:loc[0]] + `"` + strings.ReplaceAll(wrapped, `"`, `\"`) + `"` + cmd[loc[1]:]
}

func desktopLongJobStartScript(jobID, cmd, dir, logFile, pidFile, exitFile string) string {
	cmd = desktopLongJobInjectRemoteNohup(cmd)
	return strings.Join([]string{
		"set -e",
		"mkdir -p " + desktopLongJobQuote(dir),
		"cmd=" + desktopLongJobQuote(cmd),
		"log=" + desktopLongJobQuote(logFile),
		"pidf=" + desktopLongJobQuote(pidFile),
		"exf=" + desktopLongJobQuote(exitFile),
		"job=" + desktopLongJobQuote(jobID),
		// setsid is the process group. The command and its children share
		// that pgid, so cancelling the job can signal them together. nohup
		// on a lone bash left those children running after the wrapper died.
		// The marker is in the environment at exec: /proc does not show a
		// variable exported later by the shell.
		"MACLAW_LONGJOB_ID=\"$job\" setsid bash -lc \"$cmd; echo \\$? > \\\"$exf\\\"\" > \"$log\" 2>&1 < /dev/null &",
		"echo $! > \"$pidf\"",
		"pid=$(cat \"$pidf\")",
		"start=$(awk -F ')' '{print $NF}' /proc/$pid/stat | awk '{print $20}')",
		"echo LONGJOB_STARTED pid=$pid pgid=$pid start=$start log=$log exit_file=$exf",
	}, "\n")
}

func desktopLongJobPollScript(j *desktopLongJob) string {
	return strings.Join([]string{
		"pidf=" + desktopLongJobQuote(j.PidFile),
		"exf=" + desktopLongJobQuote(j.ExitFile),
		"log=" + desktopLongJobQuote(j.LogFile),
		`pid=""; [ -f "$pidf" ] && pid=$(cat "$pidf")`,
		`if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then echo LONGJOB_ALIVE pid=$pid; else echo LONGJOB_DEAD pid=$pid; fi`,
		`if [ -f "$exf" ]; then echo LONGJOB_EXIT=$(cat "$exf"); fi`,
		`if [ -f "$log" ]; then echo LONGJOB_LOG_BYTES=$(wc -c < "$log"); echo LONGJOB_LOG_TAIL; tail -n 40 "$log"; fi`,
	}, "\n")
}

type desktopLongJobStarted struct {
	PID, PGID, ProcStart, Log, Exit string
}

func desktopLongJobParseStart(out string) desktopLongJobStarted {
	var started desktopLongJobStarted
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "LONGJOB_STARTED") {
			continue
		}
		for _, f := range strings.Fields(line) {
			k, v, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			switch k {
			case "pid":
				started.PID = v
			case "pgid":
				started.PGID = v
			case "start":
				started.ProcStart = v
			case "log":
				started.Log = v
			case "exit_file":
				started.Exit = v
			}
		}
	}
	return started
}

type desktopLongJobPoll struct {
	Alive    bool
	PID      string
	ExitCode *int
	Tail     string
	Bytes    int
	Raw      string
}

func desktopLongJobParsePoll(out string) desktopLongJobPoll {
	p := desktopLongJobPoll{Raw: out}
	lines := strings.Split(out, "\n")
	tailAt := -1
	for i, line := range lines {
		s := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(s, "LONGJOB_ALIVE"):
			p.Alive = true
			if _, v, ok := strings.Cut(s, "pid="); ok {
				p.PID = strings.TrimSpace(v)
			}
		case strings.HasPrefix(s, "LONGJOB_DEAD"):
			p.Alive = false
			if _, v, ok := strings.Cut(s, "pid="); ok {
				p.PID = strings.TrimSpace(v)
			}
		case strings.HasPrefix(s, "LONGJOB_EXIT="):
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "LONGJOB_EXIT=")))
			if err == nil {
				p.ExitCode = &n
			}
		case strings.HasPrefix(s, "LONGJOB_LOG_BYTES="):
			n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(s, "LONGJOB_LOG_BYTES=")))
			p.Bytes = n
		case s == "LONGJOB_LOG_TAIL":
			tailAt = i + 1
		}
	}
	if tailAt >= 0 {
		p.Tail = strings.TrimSpace(strings.Join(lines[tailAt:], "\n"))
	}
	return p
}

func desktopLongJobFingerprint(p desktopLongJobPoll) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%t|%d|%s", p.Alive, p.Bytes, p.Tail)))
	return hex.EncodeToString(sum[:8])
}

func desktopLongJobInterval(age time.Duration) time.Duration {
	switch {
	case age < 10*time.Minute:
		return time.Minute
	case age < 30*time.Minute:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// desktopLongJobGrace: the start waits this long, polling, before handing
// the job off. A command that ends inside it is an ordinary bash step and
// its output comes back inline (papers 2026-11-11: a 1 s sed on a remote
// README was "training" by name, became a job and cost a job_status round
// trip each time). Tests replace it.
var desktopLongJobGrace = []time.Duration{2 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second}

var desktopLongJobSleep = func(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// desktopLongJobStart starts cmd detached. principal is the run's own scope
// (before the desktop account replaced its user), kept for the resume.
func desktopLongJobStart(ctx context.Context, scope, principal agentruntime.Scope, args map[string]any) (string, error) {
	cmd := strings.TrimSpace(desktopArg(args, "command"))
	if cmd == "" {
		return "", fmt.Errorf("job start needs command")
	}
	var lineage string
	round := 0
	if parent := desktopLongJobWatchParent(scope.RunID); parent != nil {
		r, refuse := desktopLongJobIterationGate(parent)
		if refuse != "" {
			return "", fmt.Errorf("%s", refuse)
		}
		lineage, round = desktopLongJobLineage(parent), r
	} else if prev := desktopLongJobForRun(scope.RunID); prev != nil && prev.Status != "running" && prev.Status != "stalled" &&
		desktopRoundTrainRe.MatchString(cmd) && desktopRoundTrainRe.MatchString(prev.Command) {
		// The run itself starts the next training round: same lineage, so the
		// rounds line shows every round. Not gated here; desktopRoundNote asks
		// once for the previous round's record.
		lineage, round = desktopLongJobLineage(prev), desktopLongJobRoundNo(prev)+1
	}
	id := strings.ReplaceAll(agentservice.NewID("job"), "-", "")
	dir := desktopLongJobScratch(scope, id)
	j := &desktopLongJob{
		ID:          id,
		TenantID:    scope.TenantID,
		UserID:      scope.UserID,
		InstanceID:  scope.InstanceID,
		SessionID:   scope.SessionID,
		SourceRun:   scope.RunID,
		ToolCallID:  strings.TrimSpace(agentruntime.ToolCallID(ctx)),
		Command:     cmd,
		Dir:         dir,
		LogFile:     path.Join(dir, "job.log"),
		PidFile:     path.Join(dir, "job.pid"),
		ExitFile:    path.Join(dir, "job.exit"),
		Status:      "running",
		Alive:       true,
		Started:     desktopLongJobNow(),
		LastChange:  desktopLongJobNow(),
		NextPoll:    desktopLongJobNow().Add(time.Minute),
		StallAfter:  desktopLongJobStallAfter,
		Lineage:     lineage,
		Round:       round,
		KeepRunning: desktopArgBool(args, "keep_running"),
	}
	if _, bot := desktop.SplitDesktopUser(scope.UserID); bot != "" {
		j.BotID = bot
	}
	if principal.TenantID != scope.TenantID || principal.UserID != scope.UserID {
		j.PrincipalTenant, j.PrincipalUser = principal.TenantID, principal.UserID
	} else if a, ok := desktopInstancePrincipal(scope.InstanceID); ok {
		j.PrincipalTenant, j.PrincipalUser = a.TenantID, a.UserID
	}
	j.ReportedStatus = "running"
	desktopLongJobSetCheck(j, args, j.Started)
	script := desktopLongJobStartScript(j.ID, cmd, dir, j.LogFile, j.PidFile, j.ExitFile)
	out, err := desktopLongJobRun(ctx, scope, script)
	if err != nil {
		return "", fmt.Errorf("could not start the background job: %w", err)
	}
	started := desktopLongJobParseStart(out)
	if started.PID == "" {
		return "", fmt.Errorf("the background job did not report a pid:\n%s", desktopRuneHead(out, 400))
	}
	j.PID = started.PID
	j.PGID = started.PGID
	if j.PGID == "" {
		j.PGID = started.PID
	}
	j.ProcStart = started.ProcStart
	if started.Log != "" {
		j.LogFile = started.Log
	}
	if started.Exit != "" {
		j.ExitFile = started.Exit
	}
	if inline, err, ended := desktopLongJobGraceWait(ctx, j); ended {
		return inline, err
	}
	if j.NextCheck.IsZero() {
		desktopLongJobPlanCheck(j, desktopLongJobNow())
	}
	if !desktopLongJobKeepsRunning(j) {
		// A relaunch replaces open jobs of the same goal. The old watches
		// must not keep posting after the new one is the job.
		desktopLongJobSupersede(j)
		j.ScheduleID = desktopLongJobEnsureWatch(j)
	}
	if err := desktopLongJobSave(j); err != nil {
		return "", err
	}
	desktopLongJobEnsureTicker()
	return "后台任务已启动，这次对话不用等它结束。\n" +
		"任务 ID: " + j.ID + "\n" +
		"PID: " + j.PID + "\n" +
		"日志: " + j.LogFile + "\n" +
		"下次查看: " + j.NextCheck.Format("15:04") + "（" + j.CheckReason + "）。\n" +
		"巡查只在有阶段结果、结束、失败或停滞时告诉用户，不按固定间隔播报。你对耗时有更好的估计时，用 action=job_status，job_id=" + j.ID + "，next_check_minutes=分钟数，check_reason=理由 调整下次查看时间（2 分钟到 6 小时）。\n" +
		"请结束这一轮，不要再用 bash 等待它跑完。需要立刻查看可用 action=job_status，job_id=" + j.ID + "。", nil
}

// desktopLongJobGraceWait polls the fresh job for a few seconds. A job that
// already ended is stored as reported and returned like a plain bash step:
// its output, and an error for a non-zero exit.
func desktopLongJobGraceWait(ctx context.Context, j *desktopLongJob) (string, error, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withoutDesktopBashSecrets(ctx)
	for _, d := range desktopLongJobGrace {
		if !desktopLongJobSleep(ctx, d) {
			return "", nil, false
		}
		if err := desktopLongJobPollAndStore(ctx, j); err != nil {
			return "", nil, false
		}
		if j.Status != "done" && j.Status != "failed" {
			continue
		}
		j.ReportedStatus = j.Status
		_ = desktopLongJobSave(j)
		out := strings.TrimSpace(desktopLongJobInlineOutput(ctx, j))
		if j.Status == "failed" {
			code := "unknown"
			if j.ExitCode != nil {
				code = strconv.Itoa(*j.ExitCode)
			}
			if out == "" {
				out = "(no output)"
			}
			return "", fmt.Errorf("%s\n[exit %s]", out, code), true
		}
		return out, nil, true
	}
	return "", nil, false
}

func desktopLongJobInlineOutput(ctx context.Context, j *desktopLongJob) string {
	out, err := desktopLongJobRun(ctx, desktopLongJobScope(j), "tail -c 60000 "+desktopLongJobQuote(j.LogFile)+" 2>/dev/null")
	if err != nil {
		return j.LastTail
	}
	return out
}

func desktopLongJobScope(j *desktopLongJob) agentruntime.Scope {
	return agentruntime.Scope{TenantID: j.TenantID, UserID: j.UserID, InstanceID: j.InstanceID, SessionID: j.SessionID, RunID: j.SourceRun}
}

func desktopLongJobStatus(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	id := strings.TrimSpace(desktopArg(args, "job_id"))
	if id == "" {
		id = strings.TrimSpace(desktopArg(args, "id"))
	}
	j := desktopLongJobGet(id)
	if j == nil {
		j = desktopLongJobLatestFor(scope)
	}
	if j == nil {
		return "", fmt.Errorf("job_status: no background job")
	}
	if err := desktopLongJobPollAndStore(ctx, j); err != nil {
		return desktopLongJobView(j) + "\n轮询失败：" + err.Error(), nil
	}
	// A "stalled" job whose log is quiet may be working elsewhere: the bot's
	// own estimate counts for it too, and the next poll inside that estimate
	// sees it running.
	if (j.Status == "running" || j.Status == "stalled") && desktopLongJobSetCheck(j, args, desktopLongJobNow()) {
		_ = desktopLongJobSave(j)
	}
	if desktopLongJobRecordRound(j, args) {
		_ = desktopLongJobSave(j)
		desktopRoundNoteRecorded(scope.RunID)
	}
	if strings.TrimSpace(scope.SessionID) != "" && scope.SessionID == j.SessionID && j.Status != "running" {
		// The live run in this conversation read the stall, failure or
		// finish itself; it tells the person in its own reply.
		j.ReportedStatus = j.Status
		_ = desktopLongJobSave(j)
	}
	return desktopLongJobView(j), nil
}

func desktopUnwatchedJobsPrompt(scope agentruntime.Scope) string {
	var b strings.Builder
	for _, j := range desktopLongJobListFor(scope) {
		if j == nil || j.Status != "unwatched" {
			continue
		}
		fmt.Fprintf(&b, "Background job %s (%s) is still running and was not stopped. Hub delivery stopped: %s. Tell the person. Do not kill the process and do not start the command again.\n", j.ID, desktopLongJobLabel(j.Command), strings.TrimSpace(j.EndReason))
	}
	return b.String()
}

func desktopLongJobList(scope agentruntime.Scope) string {
	jobs := desktopLongJobListFor(scope)
	if len(jobs) == 0 {
		return "没有正在跟踪的后台任务。"
	}
	var b strings.Builder
	for _, j := range jobs {
		b.WriteString(desktopLongJobView(j))
		b.WriteString("\n---\n")
	}
	return strings.TrimSpace(b.String())
}

func desktopLongJobView(j *desktopLongJob) string {
	if j == nil {
		return ""
	}
	alive := "否"
	if j.Alive {
		alive = "是"
	}
	exit := "-"
	if j.ExitCode != nil {
		exit = strconv.Itoa(*j.ExitCode)
	}
	age := desktopLongJobNow().Sub(j.Started).Round(time.Second)
	text := fmt.Sprintf("任务 %s\n命令: %s\n状态: %s\n进程存活: %s\nPID: %s\n退出码: %s\n已运行: %s\n日志: %s",
		j.ID, j.Command, j.Status, alive, j.PID, exit, age, j.LogFile)
	if !j.NextCheck.IsZero() && (j.Status == "running" || j.Status == "stalled") {
		text += "\n下次查看: " + j.NextCheck.Format("15:04") + "（" + j.CheckReason + "）"
	}
	if j.Lineage != "" || j.RoundMetric != nil || j.Decision != "" || j.Status == "done" || j.Status == "failed" {
		text += "\n" + desktopLongJobRoundsLine(j)
	}
	if l := desktopLongJobLimitLine(j); l != "" && (j.Status == "running" || j.Status == "stalled") {
		text += "\n" + l
	}
	if f := desktopLongJobInnerFailures(j.LastTail); len(f) > 0 {
		text += "\n输出中没成功的步骤: " + strings.Join(f, "，")
	}
	if n := desktopLongJobKilledNote(j); n != "" && j.Status == "failed" {
		text += "\n说明: " + n
	}
	if j.Status == "unwatched" {
		why := strings.TrimSpace(j.EndReason)
		if why == "" {
			why = "hub assignment missing"
		}
		text += "\n无人值守: " + why + "。进程没有被停，Hub 不再收到巡查。不要杀掉它，也不要重新启动。"
	}
	if j.LastTail != "" {
		text += "\n--- 最近日志 ---\n" + desktopRuneHead(j.LastTail, 2000)
	}
	return text
}

func desktopLongJobPollAndStore(ctx context.Context, j *desktopLongJob) error {
	if j == nil {
		return fmt.Errorf("no job")
	}
	out, err := desktopLongJobRun(ctx, desktopLongJobScope(j), desktopLongJobPollScript(j))
	if err != nil {
		return err
	}
	p := desktopLongJobParsePoll(out)
	now := desktopLongJobNow()
	fp := desktopLongJobFingerprint(p)
	j.LastPoll = now
	j.Alive = p.Alive
	if p.PID != "" {
		j.PID = p.PID
	}
	j.ExitCode = p.ExitCode
	j.LastTail = p.Tail
	if fp != j.Fingerprint {
		j.Fingerprint = fp
		j.LastChange = now
	}
	keepUnwatched := j.Status == "unwatched"
	switch {
	case p.Alive:
		if keepUnwatched {
			j.Status = "unwatched"
			break
		}
		// A quiet log is not a stall while the job is inside its own estimate:
		// many jobs write progress to their own files (papers 11-11 23:05:
		// BERT training logged to its run folder, job.log stayed at "training
		// start", and the bot was told it had stalled with 34 minutes to go).
		end := j.EstimateEnd
		if j.CheckSet && j.NextCheck.After(end) {
			end = j.NextCheck
		}
		inEstimate := !end.IsZero() && now.Before(end)
		if !j.LastChange.IsZero() && now.Sub(j.LastChange) >= j.stallBound() && !inEstimate {
			j.Status = "stalled"
		} else {
			j.Status = "running"
		}
	case p.ExitCode != nil && *p.ExitCode == 0:
		j.Status = "done"
		j.Alive = false
	default:
		j.Status = "failed"
		j.Alive = false
	}
	j.Progress = desktopLongJobProgressLine(j)
	j.NextPoll = now.Add(desktopLongJobInterval(now.Sub(j.Started)))
	return desktopLongJobSave(j)
}

func desktopLongJobProgressLine(j *desktopLongJob) string {
	if j == nil {
		return ""
	}
	switch j.Status {
	case "done":
		return desktopLongJobDoneHead(j) + "。"
	case "failed":
		code := "未知"
		if j.ExitCode != nil {
			code = strconv.Itoa(*j.ExitCode)
		}
		return "后台任务失败（退出码 " + code + "）。"
	case "stalled":
		return "后台任务还在，但已经 " + j.stallBound().Round(time.Second).String() + " 没有新的日志进展。"
	case "unwatched":
		return "后台任务仍在运行，但 Hub 不再收到巡查（" + strings.TrimSpace(j.EndReason) + "）。进程没有被停。"
	default:
		return "后台任务仍在运行（进程存活）。"
	}
}

type desktopLongJobDecision string

const (
	longJobSilent desktopLongJobDecision = "silent"
	longJobNotice desktopLongJobDecision = "notice"
	longJobResume desktopLongJobDecision = "resume"
	// longJobHeartbeat: nothing changed for desktopLongJobHeartbeatEvery;
	// one honest line that the job is still there and has not moved.
	longJobHeartbeat desktopLongJobDecision = "heartbeat"
)

func desktopLongJobDecide(j *desktopLongJob, prevStatus, prevFP string, prevNotice time.Time) desktopLongJobDecision {
	if j == nil {
		return longJobSilent
	}
	now := desktopLongJobNow()
	switch j.Status {
	case "done", "failed", "stalled":
		if j.Status != prevStatus {
			return longJobResume
		}
		// A still-stalled job is not repeated in chat (Daniel 11-11 21:10):
		// the status area shows it; chat had the stall once.
		if desktopLongJobHeartbeatEvery > 0 && j.Status == "stalled" && j.Alive && !prevNotice.IsZero() && now.Sub(prevNotice) >= desktopLongJobHeartbeatEvery {
			return longJobHeartbeat
		}
		// A quiet job.log is no proof of a stall for a job that writes its
		// progress elsewhere: at its check the bot reads the real progress.
		if j.Status == "stalled" && j.Alive && !j.NextCheck.IsZero() && !now.Before(j.NextCheck) && !desktopLongJobTailMeasurable(j.LastTail) && strings.TrimSpace(j.SessionID) != "" {
			return longJobCheck
		}
		return longJobSilent
	}
	if !j.Alive {
		return longJobResume
	}
	// No fixed-interval updates: the person hears about a running job only
	// at its estimated check, and only for a new stage result, or once when
	// it is far past its estimate.
	if j.NextCheck.IsZero() || now.Before(j.NextCheck) {
		return longJobSilent
	}
	if !desktopLongJobTailMeasurable(j.LastTail) && strings.TrimSpace(j.SessionID) != "" {
		return longJobCheck
	}
	if m := desktopRunProgressLines(j.LastTail); m != "" && m != j.ReportedMetrics {
		return longJobNotice
	}
	if desktopLongJobOverdue(j, now) || desktopLongJobPastDeadline(j) {
		return longJobNotice
	}
	return longJobSilent
}

var desktopLongJobFracRe = regexp.MustCompile(`(?:^|[^0-9./])([0-9]{1,7})\s*/\s*([0-9]{1,7})(?:[^0-9./]|$)`)
var desktopLongJobPctRe = regexp.MustCompile(`(?:^|[^0-9.])([0-9]{1,3}(?:\.[0-9]+)?)\s*%`)

// desktopLongJobProgress reads how far a job is from the last log line that
// states it: nested counters such as "epoch 2/10 … step 400/803" or
// "[12/100]", or a percentage. units is the outer counter's total (0 for a
// percentage). Notation, not commands: any job that prints its progress.
func desktopLongJobProgress(tail string) (frac float64, units int, ok bool) {
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
		line := lines[i]
		var pairs [][2]int
		for _, m := range desktopLongJobFracRe.FindAllStringSubmatch(line, 4) {
			a, _ := strconv.Atoi(m[1])
			b, _ := strconv.Atoi(m[2])
			if b >= 2 && a <= b {
				pairs = append(pairs, [2]int{a, b})
			}
		}
		if len(pairs) > 0 {
			// Outer counters name the unit in progress; the innermost
			// counts what is done inside it.
			f := float64(pairs[len(pairs)-1][0]) / float64(pairs[len(pairs)-1][1])
			for k := len(pairs) - 2; k >= 0; k-- {
				cur := pairs[k][0] - 1
				if cur < 0 {
					cur = 0
				}
				f = (float64(cur) + f) / float64(pairs[k][1])
			}
			if f > 0 && f < 1 {
				return f, pairs[0][1], true
			}
			continue
		}
		if m := desktopLongJobPctRe.FindAllStringSubmatch(line, -1); len(m) > 0 {
			v, err := strconv.ParseFloat(m[len(m)-1][1], 64)
			if err == nil && v > 0 && v < 100 {
				return v / 100, 0, true
			}
		}
	}
	return 0, 0, false
}

func desktopLongJobClampCheck(d time.Duration) time.Duration {
	if d < desktopLongJobCheckMin {
		return desktopLongJobCheckMin
	}
	if d > desktopLongJobCheckMax {
		return desktopLongJobCheckMax
	}
	return d
}

// desktopLongJobPlanCheck sets the next check from the job's observed rate:
// the end of the current outer unit (an epoch) when there are few units,
// else half of what is left; without a progress signal, a default that
// doubles after each check that found nothing new.
func desktopLongJobPlanCheck(j *desktopLongJob, now time.Time) {
	elapsed := now.Sub(j.Started)
	if f, units, ok := desktopLongJobProgress(j.LastTail); ok && elapsed > 0 {
		total := time.Duration(float64(elapsed) / f)
		left := total - elapsed
		next := left / 2
		why := fmt.Sprintf("按日志进度 %.0f%%，已用 %s，预计还要 %s", f*100, elapsed.Round(time.Minute), left.Round(time.Minute))
		if units >= 2 && units <= 1000 {
			per := total / time.Duration(units)
			done := time.Duration(f * float64(units))
			if toNext := time.Duration(float64(per)*(float64(done)+1)) - elapsed; toNext > 0 {
				next = toNext
				why += fmt.Sprintf("；每个单元约 %s，下一个单元结束时查看", per.Round(time.Minute))
			}
		}
		if next > left {
			next = left
		}
		j.NextCheck = now.Add(desktopLongJobClampCheck(next + 30*time.Second))
		j.EstimateEnd = j.Started.Add(total)
		j.CheckReason = why
		return
	}
	d := desktopLongJobCheckDefault
	for i := 0; i < j.CheckMisses && d < desktopLongJobCheckMax; i++ {
		d *= 2
	}
	j.NextCheck = now.Add(desktopLongJobClampCheck(d))
	j.CheckReason = "日志里没有可估算的进度，按默认间隔查看"
}

// desktopLongJobSetCheck is the bot's own estimate (check_minutes /
// next_check_minutes), bounded.
func desktopLongJobSetCheck(j *desktopLongJob, args map[string]any, now time.Time) bool {
	mins := 0.0
	for _, key := range []string{"next_check_minutes", "check_minutes"} {
		var f float64
		switch v := args[key].(type) {
		case float64:
			f = v
		case int:
			f = float64(v)
		case int64:
			f = float64(v)
		case json.Number:
			f, _ = v.Float64()
		case string:
			f, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
		}
		if f > 0 {
			mins = f
			break
		}
	}
	if mins <= 0 {
		return false
	}
	j.NextCheck = now.Add(desktopLongJobClampCheck(time.Duration(mins * float64(time.Minute))))
	j.CheckSet = true
	j.CheckReason = strings.TrimSpace(desktopArg(args, "check_reason"))
	if j.CheckReason == "" {
		j.CheckReason = "bot 自己估算的查看时间"
	}
	// The bot's newer estimate moves the end out (papers 11-11: the first
	// estimate said 23:10, the bot re-measured at 23:03 and said 23:49).
	if j.EstimateEnd.IsZero() || j.NextCheck.After(j.EstimateEnd) {
		j.EstimateEnd = j.NextCheck
	}
	return true
}

// desktopLongJobOverdue: running far past its estimate (half again, at
// least 30 minutes), said once.
func desktopLongJobOverdue(j *desktopLongJob, now time.Time) bool {
	if j.OverdueSaid || j.EstimateEnd.IsZero() {
		return false
	}
	slack := j.EstimateEnd.Sub(j.Started) / 2
	if slack < 30*time.Minute {
		slack = 30 * time.Minute
	}
	return now.After(j.EstimateEnd.Add(slack))
}

func desktopLongJobWatchTick(ctx context.Context, jobID string) (string, error) {
	j := desktopLongJobGet(jobID)
	if j == nil {
		return "", fmt.Errorf("watch: job %s not found", jobID)
	}
	if desktopLongJobKeepsRunning(j) || j.Status == "cancelled" || j.Status == "unwatched" || ((j.Status == "done" || j.Status == "failed") && j.ReportedStatus == j.Status) {
		desktopLongJobStopWatch(j)
		return desktopLongJobView(j), nil
	}
	now := desktopLongJobNow()
	if j.HubMisses > 0 && !j.NextPoll.IsZero() && now.Before(j.NextPoll) {
		return "", nil
	}
	unreported := j.ReportedStatus != "" && j.ReportedStatus != j.Status
	if !unreported && !j.NextPoll.IsZero() && now.Before(j.NextPoll.Add(-5*time.Second)) && !j.LastPoll.IsZero() {
		return "", nil
	}
	prevFP, prevNotice := j.Fingerprint, j.LastNotice
	prevStatus := j.ReportedStatus
	if prevStatus == "" {
		prevStatus = "running"
	}
	if j.Status == "running" || j.Status == "stalled" {
		if err := desktopLongJobPollAndStore(ctx, j); err != nil {
			return "", err
		}
		j = desktopLongJobGet(j.ID)
		if j.Status == "running" && j.ReportedStatus == "stalled" {
			// The stall is over: the status area shows it running again;
			// an old "stalled" report must not force a poll every tick.
			j.ReportedStatus = "running"
			_ = desktopLongJobSave(j)
		}
	}
	dec := desktopLongJobDecide(j, prevStatus, prevFP, prevNotice)
	if (j.Status == "running" || dec == longJobCheck) && !j.NextCheck.IsZero() && !now.Before(j.NextCheck) {
		// The check happened: plan the next one silently.
		overdue := desktopLongJobOverdue(j, now)
		pastDeadline := desktopLongJobPastDeadline(j)
		if dec == longJobSilent || dec == longJobCheck {
			j.CheckMisses++
		} else {
			j.CheckMisses = 0
		}
		j.CheckSet = false
		desktopLongJobPlanCheck(j, now)
		if overdue && dec == longJobNotice {
			j.OverdueSaid = true
		}
		if pastDeadline && dec == longJobNotice {
			j.DeadlineSaid = true
		}
		_ = desktopLongJobSave(j)
	} else if j.Status == "running" && j.NextCheck.IsZero() {
		desktopLongJobPlanCheck(j, now)
		_ = desktopLongJobSave(j)
	}
	if dec == longJobSilent {
		return "", nil
	}
	if err := desktopLongJobTalk(ctx, j, dec); err != nil {
		if errors.Is(err, ErrLongJobOwnerGone) {
			// The bot or assignment is gone. The watch must not fire again.
			desktopLongJobAbandon(j, "orphaned: owner gone")
			return desktopLongJobView(desktopLongJobGet(j.ID)), nil
		}
		if desktopHubNotFound(err) && desktopLongJobInstancePresent(j) {
			fresh := desktopLongJobGet(j.ID)
			if fresh == nil {
				fresh = j
			}
			fresh.HubMisses++
			if fresh.HubMisses >= desktopLongJobHubMissLimit {
				// The process is still the user's job. Stop telling Hub about
				// it. Do not mark it dead and do not kill it.
				desktopLongJobUnwatch(fresh, "hub assignment missing")
				log.Printf("[desktop-longjob] job=%s left running unwatched after %d Hub 404s; the instance row is still stored", fresh.ID, fresh.HubMisses)
				return desktopLongJobView(desktopLongJobGet(fresh.ID)), nil
			}
			fresh.NextPoll = desktopLongJobNow().Add(desktopLongJobHubBackoff(fresh.HubMisses))
			fresh.LastPoll = desktopLongJobNow()
			_ = desktopLongJobSave(fresh)
			log.Printf("[desktop-longjob] job=%s %s not delivered (hub 404 %d/%d, next %s): %v", fresh.ID, dec, fresh.HubMisses, desktopLongJobHubMissLimit, fresh.NextPoll.Format(time.RFC3339), err)
			return desktopLongJobView(fresh), err
		}
		// A transient failure is retried on the next tick.
		log.Printf("[desktop-longjob] job=%s %s not delivered: %v", j.ID, dec, err)
		return desktopLongJobView(j), err
	}
	j.HubMisses = 0
	if dec == longJobCheck {
		// A bot turn reads the job; it speaks for itself if anything is new.
		log.Printf("[desktop-longjob] job=%s check turn started (log has no measurable progress)", j.ID)
		return desktopLongJobView(j), nil
	}
	j.LastNotice = desktopLongJobNow()
	j.ReportedStatus = j.Status
	j.ReportedMetrics = desktopRunProgressLines(j.LastTail)
	_ = desktopLongJobSave(j)
	if j.Status == "done" || j.Status == "failed" {
		desktopLongJobStopWatch(j)
	}
	return desktopLongJobView(j), nil
}

// desktopLongJobTalkDefault tells the person through Hub's say (the same
// path a bot's own action=say uses), with the last poll as evidence, so a
// progress line needs no model turn and cannot be lost in a run nobody
// polls. On a stall, failure or finish it then resumes the bot in the same
// conversation, when no run is already working there, so it can diagnose
// and continue; that run speaks with action=say too.
func desktopLongJobTalkDefault(ctx context.Context, j *desktopLongJob, dec desktopLongJobDecision) error {
	if j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return nil
	}
	if dec == longJobCheck {
		_, err := desktopLongJobResumeAs(ctx, j, longJobCheck)
		return err
	}
	if dec == longJobResume && desktopLongJobTerminal(j) {
		// The raw log is context for one summary turn. It is not a chat message.
		// A job whose owning run already concluded does not speak on its own.
		return desktopLongJobFinishOwned(ctx, j)
	}
	sayJob := j
	if dec == longJobHeartbeat {
		// Same status and output as the last line: its own key, so Hub's
		// dedupe does not swallow it, one per heartbeat window.
		cp := *j
		cp.Fingerprint += "|hb" + strconv.FormatInt(desktopLongJobNow().Unix()/int64(desktopLongJobHeartbeatEvery/time.Second), 10)
		sayJob = &cp
	}
	scheduled := false
	var resumeErr error
	if dec == longJobResume {
		scheduled, resumeErr = desktopLongJobResumeAs(ctx, j, dec)
	}
	text := desktopLongJobNoticeText(j, dec)
	if scheduled {
		text += "我接着看一下原因和下一步。"
	}
	if err := desktopLongJobSay(ctx, sayJob, text); err != nil {
		// A 404 on this say is not the decision. Only a missing resume
		// target (the job's instance) ends the watch.
		if desktopHubNotFound(err) && desktopLongJobResumeTargetGone(j) {
			return ErrLongJobOwnerGone
		}
		return err
	}
	if errors.Is(resumeErr, ErrLongJobOwnerGone) {
		return resumeErr
	}
	return nil
}

// desktopLongJobSayFunc posts one say; tests replace it.
var desktopLongJobSayFunc = func(ctx context.Context, body map[string]any) error {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out := map[string]any{}
	if desktopRemoteAssign != nil {
		_, err := desktopRemoteAssign(cctx, "/api/v1/desktop-services/say", body)
		return err
	}
	if client := desktopHubClientFromEnv(); client != nil {
		return client.post(cctx, "/api/v1/desktop-services/say", body, &out)
	}
	return fmt.Errorf("Hub is not configured on this server")
}

func desktopLongJobSay(ctx context.Context, j *desktopLongJob, text string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	body := map[string]any{
		"tenant_id":   j.TenantID,
		"user_id":     desktopOwnerUserID(j.UserID),
		"instance_id": j.InstanceID,
		"room":        "",
		"text":        text,
		"key":         desktopMessageKey(j.InstanceID+"|"+j.ID, "longjob", j.Status, j.Fingerprint),
	}
	err := desktopLongJobSayFunc(ctx, body)
	if err != nil && (strings.Contains(err.Error(), "ALREADY_SAID") || strings.Contains(err.Error(), "409")) {
		return nil
	}
	return err
}

// desktopLongJobNoticeText is the honest line from the last poll.
func desktopLongJobNoticeText(j *desktopLongJob, dec desktopLongJobDecision) string {
	head := "后台任务进度"
	switch j.Status {
	case "done":
		head = desktopLongJobDoneHead(j) + "，下面是最后的输出"
	case "failed":
		code := "未知"
		if j.ExitCode != nil {
			code = strconv.Itoa(*j.ExitCode)
		}
		head = "后台任务失败了（退出码 " + code + "），下面是最后的输出"
		if n := desktopLongJobKilledNote(j); n != "" {
			head = "后台任务被外部结束了（" + n + "），下面是最后的输出"
		}
	case "stalled":
		head = "后台任务的进程还在，但已经 " + j.stallBound().Round(time.Minute).String() + " 没有新的输出"
	default:
		if !j.Alive {
			head = "后台任务的进程已不在"
		}
	}
	if dec == longJobNotice && j.Status == "running" {
		if l := desktopLongJobLimitLine(j); strings.Contains(l, "会超过时限") {
			head += "（" + strings.TrimPrefix(l, "命令自带时限: ") + "）"
		}
	}
	if dec == longJobHeartbeat {
		since := j.LastChange
		if since.IsZero() {
			since = j.Started
		}
		head = "后台任务还在，自 " + since.Format("15:04") + " 起没有新的输出（不是新进展）"
	}
	age := desktopLongJobNow().Sub(j.Started).Round(time.Minute)
	text := head + "（任务 " + desktopLongJobShortID(j.ID) + "，已运行 " + age.String() + "，最近一次检查 " + j.LastPoll.Format("15:04") + "）。"
	if tail := desktopLongJobTailLines(j.LastTail, 8, 900); tail != "" {
		text += "\n" + tail
	}
	return text
}

func desktopLongJobShortID(id string) string {
	id = strings.TrimPrefix(id, "job_")
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func desktopLongJobTailLines(tail string, n, max int) string {
	lines := strings.Split(strings.TrimSpace(tail), "\n")
	kept := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(kept) < n; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.Contains(l, "Permanently added") {
			continue
		}
		kept = append([]string{l}, kept...)
	}
	out := strings.Join(kept, "\n")
	if r := []rune(out); len(r) > max {
		out = "…" + string(r[len(r)-max:])
	}
	return out
}

// desktopLongJobResume starts one bot turn in the job's conversation, unless
// a run is already working there (that run polls the job itself).
func desktopLongJobResume(ctx context.Context, j *desktopLongJob) error {
	_, err := desktopLongJobResumeAs(ctx, j, longJobResume)
	return err
}

// desktopLongJobResumeAs reports whether a follow-up turn was actually
// scheduled. A run that is already going does not drop the follow-up: it
// waits for that run to end. The bool is false when nothing was scheduled.
func desktopLongJobResumeAs(ctx context.Context, j *desktopLongJob, dec desktopLongJobDecision) (bool, error) {
	svc := desktopLongJobService
	if j.PrincipalUser == "" {
		if a, ok := desktopInstancePrincipal(j.InstanceID); ok {
			j.PrincipalTenant, j.PrincipalUser = a.TenantID, a.UserID
		} else if svc != nil {
			if p, err := svc.InstanceOwner(j.InstanceID); err == nil {
				// After a restart: the instance's owner from the service store.
				// A store error is not "no owner".
				j.PrincipalTenant, j.PrincipalUser = p.TenantID, p.UserID
			}
		}
	}
	if svc == nil || j.PrincipalUser == "" {
		log.Printf("[desktop-longjob] job=%s follow-up skipped: no service principal", j.ID)
		return false, fmt.Errorf("no service principal for this job")
	}
	principal := agentservice.Principal{TenantID: j.PrincipalTenant, UserID: j.PrincipalUser}
	if j.SessionID != "" {
		runs, err := svc.ListRuns(ctx, principal, j.InstanceID, agentservice.ListRunsInput{Status: agentservice.RunStatusRunning, SessionID: j.SessionID})
		if err != nil {
			return false, desktopLongJobPermanent(err)
		}
		if len(runs) > 0 {
			j.OutcomeWait = runs[0].ID
			j.OutcomeState = "queued"
			_ = desktopLongJobSave(j)
			log.Printf("[desktop-longjob] job=%s follow-up queued until run %s ends", j.ID, runs[0].ID)
			return true, nil
		}
	}
	in := agentservice.SendMessageInput{Content: desktopLongJobWatchPrompt(j, dec), SessionID: j.SessionID, Metadata: map[string]string{"long_job_id": j.ID, "long_job_watch": string(dec)}}
	// The turn gets its own file slot, so action=deliver works here too; the
	// follower says the files with the turn's reply (desktop_longjob_files.go).
	files := &desktopWatchFiles{}
	in.PrepareExecution = files.prepare
	_, run, err := svc.SendMessageAsync(context.WithoutCancel(ctx), principal, j.InstanceID, in)
	if err != nil {
		log.Printf("[desktop-longjob] job=%s follow-up skipped: %v", j.ID, err)
		return false, desktopLongJobPermanent(err)
	}
	if run != nil {
		desktopLongJobNoteWatchRun(run.ID, j.ID)
		go desktopLongJobFollowRunFiles(context.WithoutCancel(ctx), principal, j, run.ID, dec, files)
	}
	return true, nil
}

func desktopOwnerUserID(userID string) string {
	owner, _ := desktop.SplitDesktopUser(userID)
	if owner != "" {
		return owner
	}
	return userID
}

func desktopLongJobWatchPrompt(j *desktopLongJob, dec desktopLongJobDecision) string {
	goal := "若与上次通知相比没有新的进展，就结束，不要调用 send_to_user。"
	switch dec {
	case longJobResume:
		goal = "这不是新任务，不要重新启动同一个命令。这个人已经收到了这次轮询的状态和最后输出。用这些证据判断：结束了就核对结果（指标、产物）并告诉这个人；结果是训练、评估、基准或其它实验的数字时，报告里必须有结果评估：与基线或目标对比，检查结果是否可信（验证集与测试集的差距、类别不平衡、过拟合迹象、抽几条样本实际推理），明确说能否使用，并给出建议（继续训练、调整数据或超参、或停止）和理由；" + desktopIterPolicy + "失败或停滞就查明原因并换一种做法继续，并说明。你的最后回复会转给这个人（已用 action=say 说过就不再转）；没有要说的新内容时最后回复只写 NO_UPDATE。不要把已经说过的再说一遍。"
	case longJobNotice:
		goal = "这不是新任务，不要重新启动该命令。这是按预计时间安排的查看，出现了新的阶段结果、已经远超预计时间，或预计会超过命令自带的时限。用 send_to_user 简短告诉这个人这一阶段的结果（指标或日志要点）或比预计晚了多少，附上这次轮询的证据（进程存活与否、最近日志）。不要报步数。对剩余时间有更好的估计时，用 action=job_status，next_check_minutes 和 check_reason 设定下次查看。"
	}
	if dec == longJobResume && j.RoundMetric == nil && strings.TrimSpace(j.Decision) == "" && j.Status != "running" {
		if m := desktopRoundMetrics(j.LastTail); m != "" {
			goal += "srv 从日志末尾读到的指标（请核对）：" + m + "。"
		}
		goal += "先记录这一轮：action=job_status，job_id=" + j.ID + "，round_metric、round_metric_name、decision（continue/stop/ask_user）、reason，再决定下一步；最后回复里写明评估：能否使用、验证集与测试集的差距、与基线或前几轮的对比、继续还是停止及理由。"
	}
	if dec == longJobCheck {
		goal = "这不是新任务，不要重新启动该命令，也不要动它的进程。这是按预计时间安排的查看：这个任务的日志里看不到可测的进度，进度写在它自己的文件里（只有你知道在哪）。用一两条只读命令（如 tail 它的训练/运行日志、看进程）查实际进度。只有出现新的阶段结果（指标）、明显变慢、按实测速度会超过命令自带时限、或出错时，才告诉这个人：先用 action=job_status（job_id、next_check_minutes、check_reason）按实测速度设定下次查看，然后你的最后回复就是发给这个人的消息（系统会转给他），简短写结果和证据（指标、速度、预计结束时间），不要写“已告知”之类的话。没有新情况时，同样设定下次查看，最后回复只写 NO_UPDATE。不要报步数。"
	}
	return "这一轮是后台长任务的定时巡查，没有人在等一句套话。\n" + goal + "\n\n" + desktopLongJobView(j)
}

func desktopLongJobExecDefault(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	args := map[string]any{"action": "bash", "command": command}
	return operateDesktopBody(desktopLongJobInternalCall(ctx), scope, args)
}

var desktopLongJobCallSeq atomic.Int64

// desktopLongJobInternalCall marks a start/poll script as internal and gives
// it its own machine-call id. The model's tool-call id names the journal slot
// of the step that started the job; reusing it made every grace poll replay
// the start output ("LONGJOB_STARTED …", no alive/exit lines), so each
// wrapped job was judged failed with an unknown exit code within seconds
// (papers 11-11 20:33 and 20:39-20:45).
func desktopLongJobInternalCall(ctx context.Context) context.Context {
	ctx = withDesktopLongJobInternal(ctx)
	if parent := agentruntime.ToolCallID(ctx); parent != "" {
		ctx = agentruntime.WithToolCallID(ctx, parent+"~job"+strconv.FormatInt(desktopLongJobCallSeq.Add(1), 10))
	}
	return ctx
}

func desktopLongJobEnsureWatch(j *desktopLongJob) string {
	mgr := getSrvSchedulerManager()
	if mgr == nil || j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return ""
	}
	name := desktopLongJobTaskPrefix + j.ID
	for _, t := range mgr.List() {
		if t.Name == name && t.InstanceID == j.InstanceID {
			return t.ID
		}
	}
	id, err := mgr.Add(scheduler.ScheduledTask{
		Name:            name,
		Action:          "LONGJOB " + j.ID + " 巡查后台任务：检查进程、日志和指标；有进展、停滞、失败或结束时告知这个人。不要重新启动。",
		TaskType:        scheduler.TaskTypeReminder,
		Hour:            0,
		Minute:          0,
		DayOfWeek:       -1,
		DayOfMonth:      -1,
		IntervalMinutes: 1,
		InstanceID:      j.InstanceID,
		OwnerTenantID:   j.TenantID,
		OwnerUserID:     desktopOwnerUserID(j.UserID),
		DesktopBotID:    j.BotID,
		Status:          "active",
	})
	if err != nil {
		log.Printf("[desktop-longjob] job=%s watch schedule not added: %v", j.ID, err)
		return ""
	}
	return id
}

func desktopLongJobStopWatch(j *desktopLongJob) {
	if j == nil {
		return
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		j.ScheduleID = ""
		return
	}
	if id := strings.TrimSpace(j.ScheduleID); id != "" {
		_ = mgr.ForceDelete(id)
	}
	// A restart can leave the row under its name after the id was lost.
	_ = mgr.DeleteByName(desktopLongJobTaskPrefix + j.ID)
	j.ScheduleID = ""
}

const desktopLongJobHubMissLimit = 5

func desktopLongJobHubBackoff(misses int) time.Duration {
	if misses < 1 {
		misses = 1
	}
	if misses > desktopLongJobHubMissLimit {
		misses = desktopLongJobHubMissLimit
	}
	return time.Minute << (misses - 1)
}

func desktopLongJobInstancePresent(j *desktopLongJob) bool {
	svc := desktopLongJobService
	if svc == nil || j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return false
	}
	_, err := svc.InstanceOwner(j.InstanceID)
	return err == nil
}

// ErrLongJobOwnerGone is permanent: the instance this job resumes does not
// exist. A Hub 404, including NOT_FOUND on the say call, is not this error
// by itself. Callers tell it apart with errors.Is, not with the message text.
var ErrLongJobOwnerGone = errors.New("long job owner is gone")

func desktopLongJobResumeTargetGone(j *desktopLongJob) bool {
	svc := desktopLongJobService
	if svc == nil || j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return false
	}
	_, err := svc.InstanceOwner(j.InstanceID)
	return errors.Is(err, agentservice.ErrInstanceNotFound)
}

func desktopLongJobPermanent(err error) error {
	if errors.Is(err, ErrLongJobOwnerGone) || errors.Is(err, agentservice.ErrInstanceNotFound) {
		return ErrLongJobOwnerGone
	}
	return err
}

// longJobOwnerFate is what startup reconcile should do with one job. It is
// not a sentence compared later.
type longJobOwnerFate int

const (
	longJobFateKeep longJobOwnerFate = iota
	longJobFateMissing
	longJobFateRunCancelled
)

func desktopLongJobOwnerFate(svc *agentservice.Service, j *desktopLongJob) (longJobOwnerFate, error) {
	if svc == nil || j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return longJobFateKeep, nil
	}
	p, err := svc.InstanceOwner(j.InstanceID)
	if err != nil {
		if errors.Is(err, agentservice.ErrInstanceNotFound) {
			return longJobFateMissing, nil
		}
		return longJobFateKeep, err
	}
	if strings.TrimSpace(j.SourceRun) == "" {
		return longJobFateKeep, nil
	}
	run, err := svc.GetRun(context.Background(), p, j.InstanceID, j.SourceRun)
	if errors.Is(err, agentservice.ErrRunNotFound) {
		return longJobFateKeep, nil
	}
	if err != nil {
		return longJobFateKeep, err
	}
	if run != nil && run.Status == agentservice.RunStatusCancelled {
		return longJobFateRunCancelled, nil
	}
	return longJobFateKeep, nil
}

func desktopLongJobOpen(j *desktopLongJob) bool {
	return j != nil && (j.Status == "running" || j.Status == "stalled" || j.Status == "")
}

// desktopLongJobShouldStopProcess is an open job or one whose process is
// still alive. A finished job is not this, even if its watch row remains.
func desktopLongJobShouldStopProcess(j *desktopLongJob) bool {
	return desktopLongJobOpen(j) || (j != nil && j.Alive)
}

// desktopLongJobUnwatch stops Hub delivery for a job whose process is still
// the one the user started. Alive is left as it was.
func desktopLongJobUnwatch(j *desktopLongJob, reason string) {
	if j == nil {
		return
	}
	fresh := desktopLongJobGet(j.ID)
	if fresh != nil {
		j = fresh
	}
	j.Status = "unwatched"
	j.EndReason = reason
	j.ReportedStatus = ""
	desktopLongJobStopWatch(j)
	_ = desktopLongJobSave(j)
}

// desktopLongJobAbandon ends a job whose owner is gone and takes its watch
// off the scheduler.
func desktopLongJobAbandon(j *desktopLongJob, reason string) {
	if j == nil {
		return
	}
	fresh := desktopLongJobGet(j.ID)
	if fresh != nil {
		j = fresh
	}
	j.Status = "cancelled"
	j.Alive = false
	j.EndReason = reason
	j.ReportedStatus = "cancelled"
	desktopLongJobStopWatch(j)
	_ = desktopLongJobSave(j)
}

func desktopLongJobSameTenant(j *desktopLongJob, tenantID string) bool {
	if j == nil || strings.TrimSpace(tenantID) == "" {
		return false
	}
	if strings.TrimSpace(j.PrincipalTenant) != "" {
		return j.PrincipalTenant == tenantID
	}
	return j.TenantID == tenantID
}

func desktopLongJobCancelInstance(tenantID, instanceID string) {
	tenantID = strings.TrimSpace(tenantID)
	instanceID = strings.TrimSpace(instanceID)
	if tenantID == "" || instanceID == "" {
		return
	}
	for _, j := range desktopLongJobAll() {
		if j.InstanceID != instanceID || !desktopLongJobSameTenant(j, tenantID) {
			continue
		}
		if desktopLongJobOpen(j) || j.ScheduleID != "" {
			desktopLongJobAbandon(j, "orphaned: instance deleted")
		}
	}
}

// desktopLongJobKillPolls is how many times cancel confirms the group is
// gone before it escalates to KILL. The interval is the poll, not a wait
// that stands in for the signal.
const desktopLongJobKillPolls = 25

func desktopLongJobProcID(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// desktopLongJobKillProcess signals the job's process group, and only after
// the leader's start time and job marker still match this job.
func desktopLongJobKillProcess(ctx context.Context, j *desktopLongJob) {
	if !desktopLongJobShouldStopProcess(j) {
		return
	}
	pgid := strings.TrimSpace(j.PGID)
	if pgid == "" {
		pgid = strings.TrimSpace(j.PID)
	}
	if !desktopLongJobProcID(pgid) || strings.TrimSpace(j.ProcStart) == "" || strings.TrimSpace(j.ID) == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	script := strings.Join([]string{
		"pgid=" + desktopLongJobQuote(pgid),
		"want=" + desktopLongJobQuote(strings.TrimSpace(j.ProcStart)),
		"marker=" + desktopLongJobQuote("MACLAW_LONGJOB_ID="+strings.TrimSpace(j.ID)),
		`if [ ! -r "/proc/$pgid/stat" ] || [ ! -r "/proc/$pgid/environ" ]; then exit 0; fi`,
		`start=$(awk -F ')' '{print $NF}' "/proc/$pgid/stat" | awk '{print $20}')`,
		`if [ "$start" != "$want" ]; then exit 0; fi`,
		`if ! tr '\0' '\n' < "/proc/$pgid/environ" | grep -qxF "$marker"; then exit 0; fi`,
		`kill -TERM -- "-$pgid" 2>/dev/null || true`,
		`i=0`,
		fmt.Sprintf(`while [ "$i" -lt %d ]; do`, desktopLongJobKillPolls),
		`  if ! kill -0 -- "-$pgid" 2>/dev/null; then exit 0; fi`,
		`  sleep 0.1`,
		`  i=$((i+1))`,
		`done`,
		`kill -KILL -- "-$pgid" 2>/dev/null || true`,
	}, "\n")
	_, _ = desktopLongJobRun(ctx, desktopLongJobScope(j), script)
}

func desktopLongJobDropWatch(j *desktopLongJob) {
	if j == nil {
		return
	}
	fresh := desktopLongJobGet(j.ID)
	if fresh != nil {
		j = fresh
	}
	desktopLongJobStopWatch(j)
	_ = desktopLongJobSave(j)
}

func desktopLongJobStopStartedRun(j *desktopLongJob) {
	if desktopLongJobShouldStopProcess(j) {
		desktopLongJobKillProcess(context.Background(), j)
		desktopLongJobAbandon(j, "orphaned: run cancelled")
		return
	}
	desktopLongJobDropWatch(j)
}

func desktopLongJobCancelSourceRun(instanceID, runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	// A watch-report run is not the job. Cancelling it leaves the process
	// and its watch in place. Only the run that started the job stops it.
	for _, j := range desktopLongJobAll() {
		if j.SourceRun != runID {
			continue
		}
		if instanceID != "" && j.InstanceID != "" && j.InstanceID != instanceID {
			continue
		}
		desktopLongJobStopStartedRun(j)
	}
	desktopLongJobReleaseOutcomes(runID)
}

// desktopLongJobReconcileOwners drops watches whose instance is gone.
// It runs once when the service is bound, not on a timer.
func desktopLongJobReconcileOwners() {
	svc := desktopLongJobService
	if svc == nil {
		return
	}
	for _, j := range desktopLongJobAll() {
		if j.InstanceID == "" {
			continue
		}
		if !desktopLongJobOpen(j) && j.ScheduleID == "" {
			continue
		}
		fate, err := desktopLongJobOwnerFate(svc, j)
		if err != nil {
			log.Printf("[desktop-longjob] reconcile skipped: %v", err)
			return
		}
		switch fate {
		case longJobFateMissing:
			if desktopLongJobShouldStopProcess(j) {
				desktopLongJobAbandon(j, "orphaned: owner missing at startup")
			} else {
				desktopLongJobDropWatch(j)
			}
		case longJobFateRunCancelled:
			desktopLongJobStopStartedRun(j)
		}
	}
	mgr := getSrvSchedulerManager()
	if mgr == nil {
		return
	}
	for _, task := range mgr.List() {
		id := desktopLongJobIDFromTask(&task)
		if id == "" {
			continue
		}
		j := desktopLongJobGet(id)
		if j == nil || j.Status == "cancelled" {
			_ = mgr.ForceDelete(task.ID)
		}
	}
}

func desktopLongJobHandleScheduled(ctx context.Context, task *scheduler.ScheduledTask) (string, error, bool) {
	if task == nil {
		return "", nil, false
	}
	id := desktopLongJobIDFromTask(task)
	if id == "" {
		return "", nil, false
	}
	out, err := desktopLongJobWatchTick(ctx, id)
	return out, err, true
}

func desktopLongJobIDFromTask(task *scheduler.ScheduledTask) string {
	if task == nil {
		return ""
	}
	if task.TaskType == desktopLongJobTaskType || strings.HasPrefix(task.Name, desktopLongJobTaskPrefix) {
		id := strings.TrimPrefix(task.Name, desktopLongJobTaskPrefix)
		if id != "" && !strings.Contains(id, " ") {
			return id
		}
	}
	if f := strings.Fields(task.Action); len(f) >= 2 && f[0] == "LONGJOB" {
		return f[1]
	}
	return ""
}

var (
	desktopLongJobTickOnce sync.Once
	desktopLongJobService  *agentservice.Service
)

func bindDesktopLongJobService(svc *agentservice.Service) {
	desktopLongJobService = svc
	desktopLongJobReconcileOwners()
}

func startDesktopLongJobClock() {
	desktopLongJobEnsureTicker()
}

func init() {
	agentservice.AfterInstanceDeleted = desktopLongJobCancelInstance
	agentservice.AfterRunCancelled = desktopLongJobCancelSourceRun
}

// desktopLongJobTickerDisabled is set by tests so the process-wide ticker
// does not poll a shared store while unit tests mutate it.
var desktopLongJobTickerDisabled bool

func desktopLongJobEnsureTicker() {
	if desktopLongJobTickerDisabled {
		return
	}
	desktopLongJobTickOnce.Do(func() {
		go desktopLongJobTicker()
	})
}

func desktopLongJobTicker() {
	t := time.NewTicker(desktopLongJobTickEvery)
	defer t.Stop()
	for range t.C {
		desktopLongJobTickDue()
	}
}

func desktopLongJobTickDue() {
	desktopLongJobSweepOutcomes()
	now := desktopLongJobNow()
	for _, j := range desktopLongJobRunning() {
		if j.NextPoll.IsZero() || !now.Before(j.NextPoll) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			_, _ = desktopLongJobWatchTick(ctx, j.ID)
			cancel()
		}
	}
}

// desktopLongJobKeepsRunning: the bot started this process as the result
// itself (a demo, a game, a server the person asked to leave open), declared
// with keep_running=true on the bash call. It is not unfinished work, so the
// watcher does not follow it and the run's outcome is not "running". The
// default is false: an undeclared background job is pending work and stays
// watched, whatever its command looks like.
func desktopLongJobKeepsRunning(j *desktopLongJob) bool {
	return j != nil && j.KeepRunning
}

// desktopLongJobOutcome: a run that handed off a still-running job is not
// "done" and is not a time-budget failure of that job. A served app is not
// that job: the run can finish once it has delivered the result.
func desktopLongJobOutcome(runID string) (string, string, bool) {
	j := desktopLongJobForRun(runID)
	if j == nil || (j.Status != "running" && j.Status != "stalled") {
		return "", "", false
	}
	if desktopLongJobKeepsRunning(j) {
		return "", "", false
	}
	why := "后台在跑：" + desktopLongJobLabel(j.Command) + "（任务 " + desktopLongJobShortID(j.ID) + "，" + j.Status + "）"
	if j.Progress != "" {
		why += "; " + j.Progress
	}
	if t := strings.TrimSpace(j.LastTail); t != "" {
		why += "；已测得 " + desktopRuneHead(strings.ReplaceAll(t, "\n", " "), 180)
	}
	return "running", why, true
}

// desktopLongJobLabel names the job for the person: the command without
// secrets or ssh plumbing, cut short.
func desktopLongJobLabel(cmd string) string {
	cmd = desktopLongJobStripHeredocs(cmd)
	if inner := desktopLongJobInnerCommands(cmd); len(inner) > 0 {
		cmd = inner[len(inner)-1]
	}
	cmd = desktopLongJobSecretRe.ReplaceAllString(cmd, "$1***")
	cmd = strings.Join(strings.Fields(cmd), " ")
	return desktopRuneHead(cmd, 80)
}

var desktopLongJobSecretRe = regexp.MustCompile(`(?i)((?:SSHPASS|PASSWORD|TOKEN|SECRET|API_KEY)\w*=|sshpass\s+-p\s*)('[^']*'|"[^"]*"|\S+)`)
