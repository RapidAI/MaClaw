package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
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
	desktopLongJobTaskType    = "watch"
	desktopLongJobTaskPrefix  = "watch-job:"
	desktopLongJobTickEvery   = 30 * time.Second
	desktopLongJobStallAfter  = 15 * time.Minute
	desktopLongJobNoticeEvery = 5 * time.Minute
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
	ID          string        `json:"id"`
	TenantID    string        `json:"tenant_id"`
	UserID      string        `json:"user_id"`
	InstanceID  string        `json:"instance_id"`
	SessionID   string        `json:"session_id"`
	BotID       string        `json:"bot_id,omitempty"`
	SourceRun   string        `json:"source_run,omitempty"`
	Command     string        `json:"command"`
	Dir         string        `json:"dir"`
	LogFile     string        `json:"log_file"`
	PidFile     string        `json:"pid_file"`
	ExitFile    string        `json:"exit_file"`
	PID         string        `json:"pid,omitempty"`
	Status      string        `json:"status"` // running, stalled, failed, done
	Alive       bool          `json:"alive"`
	ExitCode    *int          `json:"exit_code,omitempty"`
	Progress    string        `json:"progress,omitempty"`
	LastTail    string        `json:"last_tail,omitempty"`
	Fingerprint string        `json:"fingerprint,omitempty"`
	ScheduleID  string        `json:"schedule_id,omitempty"`
	Started     time.Time     `json:"started"`
	LastPoll    time.Time     `json:"last_poll,omitempty"`
	LastChange  time.Time     `json:"last_change,omitempty"`
	LastNotice  time.Time     `json:"last_notice,omitempty"`
	NextPoll    time.Time     `json:"next_poll,omitempty"`
	StallAfter  time.Duration `json:"stall_after,omitempty"`
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
	if desktopArgBool(args, "background") || desktopArgBool(args, "detach") || desktopArgBool(args, "async") {
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

func desktopLongJobCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	if desktopLongJobDetachRe.MatchString(cmd) {
		return true
	}
	if desktopLongJobIntentRe.MatchString(cmd) && !desktopLongJobShortRe.MatchString(cmd) {
		return true
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
	wrapped := "nohup bash -lc " + desktopLongJobQuote(inner) + " >/tmp/maclaw-job.log 2>&1 & echo $!"
	if q[0] == '\'' {
		return cmd[:loc[0]] + desktopLongJobQuote(wrapped) + cmd[loc[1]:]
	}
	return cmd[:loc[0]] + `"` + strings.ReplaceAll(wrapped, `"`, `\"`) + `"` + cmd[loc[1]:]
}

func desktopLongJobStartScript(cmd, dir, logFile, pidFile, exitFile string) string {
	cmd = desktopLongJobInjectRemoteNohup(cmd)
	return strings.Join([]string{
		"set -e",
		"mkdir -p " + desktopLongJobQuote(dir),
		"cmd=" + desktopLongJobQuote(cmd),
		"log=" + desktopLongJobQuote(logFile),
		"pidf=" + desktopLongJobQuote(pidFile),
		"exf=" + desktopLongJobQuote(exitFile),
		"nohup bash -lc \"$cmd; echo \\$? > \\\"$exf\\\"\" > \"$log\" 2>&1 &",
		"echo $! > \"$pidf\"",
		"echo LONGJOB_STARTED pid=$(cat \"$pidf\") log=$log exit_file=$exf",
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

func desktopLongJobParseStart(out string) (pid, log, exit string) {
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
				pid = v
			case "log":
				log = v
			case "exit_file":
				exit = v
			}
		}
	}
	return pid, log, exit
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

func desktopLongJobStart(ctx context.Context, scope agentruntime.Scope, args map[string]any) (string, error) {
	cmd := strings.TrimSpace(desktopArg(args, "command"))
	if cmd == "" {
		return "", fmt.Errorf("job start needs command")
	}
	id := strings.ReplaceAll(agentservice.NewID("job"), "-", "")
	dir := desktopLongJobScratch(scope, id)
	j := &desktopLongJob{
		ID:         id,
		TenantID:   scope.TenantID,
		UserID:     scope.UserID,
		InstanceID: scope.InstanceID,
		SessionID:  scope.SessionID,
		SourceRun:  scope.RunID,
		Command:    cmd,
		Dir:        dir,
		LogFile:    path.Join(dir, "job.log"),
		PidFile:    path.Join(dir, "job.pid"),
		ExitFile:   path.Join(dir, "job.exit"),
		Status:     "running",
		Alive:      true,
		Started:    desktopLongJobNow(),
		LastChange: desktopLongJobNow(),
		NextPoll:   desktopLongJobNow().Add(time.Minute),
		StallAfter: desktopLongJobStallAfter,
	}
	if _, bot := desktop.SplitDesktopUser(scope.UserID); bot != "" {
		j.BotID = bot
	}
	script := desktopLongJobStartScript(cmd, dir, j.LogFile, j.PidFile, j.ExitFile)
	out, err := desktopLongJobRun(ctx, scope, script)
	if err != nil {
		return "", fmt.Errorf("could not start the background job: %w", err)
	}
	pid, log, exit := desktopLongJobParseStart(out)
	if pid == "" {
		return "", fmt.Errorf("the background job did not report a pid:\n%s", desktopRuneHead(out, 400))
	}
	j.PID = pid
	if log != "" {
		j.LogFile = log
	}
	if exit != "" {
		j.ExitFile = exit
	}
	j.ScheduleID = desktopLongJobEnsureWatch(j)
	if err := desktopLongJobSave(j); err != nil {
		return "", err
	}
	desktopLongJobEnsureTicker()
	return "后台任务已启动，这次对话不用等它结束。\n" +
		"任务 ID: " + j.ID + "\n" +
		"PID: " + j.PID + "\n" +
		"日志: " + j.LogFile + "\n" +
		"状态会单独保存，定时巡查看进程是否存活、日志和指标是否在推进；有进展、停滞、失败或结束时会告诉你，并附上最近一次轮询的证据。\n" +
		"请结束这一轮，不要再用 bash 等待它跑完。需要立刻查看可用 action=job_status，job_id=" + j.ID + "。", nil
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
	return desktopLongJobView(j), nil
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
	if j.LastTail != "" {
		text += "\n--- 最近日志 ---\n" + desktopRuneHead(j.LastTail, 2000)
	}
	return text
}

func desktopLongJobPollAndStore(ctx context.Context, j *desktopLongJob) error {
	if j == nil {
		return fmt.Errorf("no job")
	}
	scope := agentruntime.Scope{TenantID: j.TenantID, UserID: j.UserID, InstanceID: j.InstanceID, SessionID: j.SessionID, RunID: j.SourceRun}
	out, err := desktopLongJobRun(ctx, scope, desktopLongJobPollScript(j))
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
	switch {
	case p.Alive:
		if !j.LastChange.IsZero() && now.Sub(j.LastChange) >= j.stallBound() {
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
		return "后台任务已结束（退出码 0）。"
	case "failed":
		code := "未知"
		if j.ExitCode != nil {
			code = strconv.Itoa(*j.ExitCode)
		}
		return "后台任务失败（退出码 " + code + "）。"
	case "stalled":
		return "后台任务还在，但已经 " + j.stallBound().Round(time.Second).String() + " 没有新的日志进展。"
	default:
		return "后台任务仍在运行（进程存活）。"
	}
}

type desktopLongJobDecision string

const (
	longJobSilent desktopLongJobDecision = "silent"
	longJobNotice desktopLongJobDecision = "notice"
	longJobResume desktopLongJobDecision = "resume"
)

func desktopLongJobDecide(j *desktopLongJob, prevStatus, prevFP string, prevNotice time.Time) desktopLongJobDecision {
	if j == nil {
		return longJobSilent
	}
	switch j.Status {
	case "done", "failed", "stalled":
		if j.Status != prevStatus {
			return longJobResume
		}
		return longJobSilent
	}
	if !j.Alive {
		return longJobResume
	}
	now := desktopLongJobNow()
	if prevNotice.IsZero() {
		return longJobNotice
	}
	if now.Sub(prevNotice) >= desktopLongJobNoticeEvery && j.Fingerprint != prevFP {
		return longJobNotice
	}
	if desktopLongJobMileRe.MatchString(j.LastTail) && j.Fingerprint != prevFP && now.Sub(prevNotice) >= time.Minute {
		return longJobNotice
	}
	return longJobSilent
}

func desktopLongJobWatchTick(ctx context.Context, jobID string) (string, error) {
	j := desktopLongJobGet(jobID)
	if j == nil {
		return "", fmt.Errorf("watch: job %s not found", jobID)
	}
	if j.Status == "done" || j.Status == "failed" {
		desktopLongJobStopWatch(j)
		return desktopLongJobView(j), nil
	}
	now := desktopLongJobNow()
	if !j.NextPoll.IsZero() && now.Before(j.NextPoll.Add(-5*time.Second)) && !j.LastPoll.IsZero() {
		return "", nil
	}
	prevStatus, prevFP, prevNotice := j.Status, j.Fingerprint, j.LastNotice
	if err := desktopLongJobPollAndStore(ctx, j); err != nil {
		return "", err
	}
	j = desktopLongJobGet(j.ID)
	dec := desktopLongJobDecide(j, prevStatus, prevFP, prevNotice)
	if dec == longJobSilent {
		return "", nil
	}
	j.LastNotice = desktopLongJobNow()
	_ = desktopLongJobSave(j)
	if err := desktopLongJobTalk(ctx, j, dec); err != nil {
		return desktopLongJobView(j), err
	}
	if j.Status == "done" || j.Status == "failed" {
		desktopLongJobStopWatch(j)
	}
	return desktopLongJobView(j), nil
}

func desktopLongJobTalkDefault(ctx context.Context, j *desktopLongJob, dec desktopLongJobDecision) error {
	svc := desktopLongJobService
	if svc == nil || j == nil || strings.TrimSpace(j.InstanceID) == "" {
		return nil
	}
	principal := agentservice.Principal{TenantID: j.TenantID, UserID: desktopOwnerUserID(j.UserID)}
	content := desktopLongJobWatchPrompt(j, dec)
	in := agentservice.SendMessageInput{Content: content, SessionID: j.SessionID, Metadata: map[string]string{"long_job_id": j.ID, "long_job_watch": string(dec)}}
	_, _, _, err := svc.SendMessage(ctx, principal, j.InstanceID, in)
	return err
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
		goal = "这不是新任务，不要重新启动该命令。用这次轮询的证据，向这个人如实说明任务已结束、失败或停滞，并做自我纠正或说明下一步。必须调用 send_to_user，不要只把说明留在心里。"
	case longJobNotice:
		goal = "这不是新任务，不要重新启动该命令。用 send_to_user 告诉这个人当前进度（阶段、指标或日志要点），并附上这次轮询的证据（进程存活与否、最近日志）。"
	}
	return "这一轮是后台长任务的定时巡查，没有人在等一句套话。\n" + goal + "\n\n" + desktopLongJobView(j)
}

func desktopLongJobExecDefault(ctx context.Context, scope agentruntime.Scope, command string) (string, error) {
	args := map[string]any{"action": "bash", "command": command}
	return operateDesktopBody(withDesktopLongJobInternal(ctx), scope, args)
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
		TaskType:        desktopLongJobTaskType,
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
		return ""
	}
	return id
}

func desktopLongJobStopWatch(j *desktopLongJob) {
	if j == nil || j.ScheduleID == "" {
		return
	}
	if mgr := getSrvSchedulerManager(); mgr != nil {
		_ = mgr.Pause(j.ScheduleID)
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
	desktopLongJobEnsureTicker()
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
	now := desktopLongJobNow()
	for _, j := range desktopLongJobRunning() {
		if j.NextPoll.IsZero() || !now.Before(j.NextPoll) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			_, _ = desktopLongJobWatchTick(ctx, j.ID)
			cancel()
		}
	}
}

// desktopLongJobOutcome: a run that handed off a still-running job is not
// "done" and is not a time-budget failure of that job.
func desktopLongJobOutcome(runID string) (string, string, bool) {
	j := desktopLongJobForRun(runID)
	if j == nil || (j.Status != "running" && j.Status != "stalled") {
		return "", "", false
	}
	why := "background job " + j.ID + " " + j.Status
	if j.Progress != "" {
		why += "; " + j.Progress
	}
	if t := strings.TrimSpace(j.LastTail); t != "" {
		why += "；已测得 " + desktopRuneHead(strings.ReplaceAll(t, "\n", " "), 180)
	}
	return "running", why, true
}
