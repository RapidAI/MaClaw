package guiapp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// OpsAgentJob is one automatic operation carried out by the session agent.
// Desktop install uses it. Another feature starts one by filling this in and
// calling RunOpsAgent: the agent does the remote work, then Probe checks the
// real outcome. Prompt may contain a password. Display is the visible chat
// row and must not.
type OpsAgentJob struct {
	Name    string
	Display string
	Prompt  string
	// Marker is the verdict prefix, without the colon. The agent's last
	// "MARKER: ..." line is the verdict. Desktop install uses DESKTOPD_DEPLOY.
	Marker string
	// Budget bounds the whole job. Every is how often Probe runs. Grace is
	// how long Probe may keep failing after the agent finishes. FailedGrace
	// replaces Grace when the verdict starts with FAILED. Zero uses the
	// desktop install defaults: 90 minutes, 45 seconds, 10 minutes, 30 seconds.
	Budget      time.Duration
	Every       time.Duration
	Grace       time.Duration
	FailedGrace time.Duration
	// Log records a step such as agent-done, probe, or wait.
	Log func(step, detail string)
	// OnTask receives the task directory once it exists.
	OnTask func(projectPath string)
	// Probe checks the machine. A nil error finishes the job. agentDone is
	// false while the turn is still running, so a caller can refuse to trust
	// a half-written install. A nil Probe succeeds when the agent finishes
	// without a FAILED verdict.
	Probe func(agentDone bool, verdict string) error
	// Fatal means another probe will not fix this error. A nil Fatal treats
	// a message containing 正在 or 还未完成 as still in progress.
	Fatal func(error) bool
	// ProbeStep names a probe failure in the log. Nil uses "probe".
	ProbeStep func(error) string
	// OnWaiting runs after the agent has finished while Probe is still failing.
	OnWaiting func(verdict string)
	// Context stops the wait. A nil context runs until the budget.
	Context context.Context

	newTask func(name string) string
	send    func(AIAssistantSendRequest) (*IMAgentResponse, error)
}

// OpsAgentResult is a finished job. Verdict is the text after the marker.
type OpsAgentResult struct {
	ProjectPath string
	Verdict     string
	Response    *IMAgentResponse
}

const (
	OpsAgentPhaseTask  = "task"
	OpsAgentPhaseStart = "start"
	OpsAgentPhaseProbe = "probe"
)

// OpsAgentError is a job that did not finish. Phase is task, start, or probe.
type OpsAgentError struct {
	Phase   string
	Err     error
	Verdict string
}

func (e *OpsAgentError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *OpsAgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RunOpsAgent creates a task, asks the session agent to do the work, and
// checks the outcome until it holds, a fatal error, or the time budget.
func RunOpsAgent(a *App, job OpsAgentJob) (OpsAgentResult, error) {
	if job.Budget <= 0 {
		job.Budget = 90 * time.Minute
	}
	if job.Every <= 0 {
		job.Every = 45 * time.Second
	}
	if job.Grace <= 0 {
		job.Grace = 10 * time.Minute
	}
	if job.FailedGrace <= 0 {
		job.FailedGrace = 30 * time.Second
	}
	// The chat row and a remote-control snapshot show Display. An empty row
	// falls back to the prompt, which is where the SSH password lives.
	display := strings.TrimSpace(job.Display)
	if display == "" {
		return OpsAgentResult{}, &OpsAgentError{Phase: OpsAgentPhaseStart, Err: errors.New("可见说明不能为空")}
	}
	if opsDisplayHasSecret(display) {
		return OpsAgentResult{}, &OpsAgentError{Phase: OpsAgentPhaseStart, Err: errors.New("可见说明不能带密码或令牌")}
	}
	if opsDisplayHasSecret(job.Name) {
		return OpsAgentResult{}, &OpsAgentError{Phase: OpsAgentPhaseStart, Err: errors.New("任务名不能带密码或令牌")}
	}
	job.Display = display
	path := ""
	if job.newTask != nil {
		path = job.newTask(job.Name)
	} else if a != nil {
		path = a.CreateTask(job.Name, "").ProjectPath
	}
	if strings.TrimSpace(path) == "" {
		return OpsAgentResult{}, &OpsAgentError{Phase: OpsAgentPhaseTask, Err: errors.New("无法创建任务")}
	}
	if job.OnTask != nil {
		job.OnTask(path)
	}
	done := make(chan *IMAgentResponse, 1)
	req := AIAssistantSendRequest{
		Text:               job.Prompt,
		ProjectPath:        path,
		BackendDisplayText: job.Display,
		OnDone: func(resp *IMAgentResponse) {
			select {
			case done <- resp:
			default:
			}
		},
	}
	var sendErr error
	if job.send != nil {
		_, sendErr = job.send(req)
	} else if a == nil {
		sendErr = errors.New("AI assistant backend is unavailable")
	} else {
		_, sendErr = a.SendAIAssistantMessage(req)
	}
	if sendErr != nil {
		return OpsAgentResult{ProjectPath: path}, &OpsAgentError{Phase: OpsAgentPhaseStart, Err: sendErr}
	}

	fatal := job.Fatal
	if fatal == nil {
		fatal = func(err error) bool { return !opsAgentRetryable(err) }
	}
	deadline := time.Now().Add(job.Budget)
	var (
		agentDoneAt time.Time
		verdict     string
		response    *IMAgentResponse
		lastProbe   string
	)
	ticker := time.NewTicker(job.Every)
	defer ticker.Stop()
	var stop <-chan struct{}
	if job.Context != nil {
		stop = job.Context.Done()
	}
	cancelled := func() (OpsAgentResult, error) {
		return OpsAgentResult{ProjectPath: path, Verdict: verdict, Response: response}, &OpsAgentError{Phase: OpsAgentPhaseProbe, Err: fmt.Errorf("操作已取消: %w", job.Context.Err()), Verdict: verdict}
	}
	for {
		select {
		case <-stop:
			return cancelled()
		case resp := <-done:
			agentDoneAt = time.Now()
			response = resp
			verdict = opsAgentVerdict(resp, job.Marker)
			switch {
			case verdict == "" && strings.TrimSpace(job.Marker) != "":
				job.log("agent-done", "没有 "+job.Marker+" 结论")
			case verdict == "":
				job.log("agent-done", "没有结论")
			default:
				job.log("agent-done", verdict)
			}
		case <-ticker.C:
		}
		if job.Context != nil && job.Context.Err() != nil {
			return cancelled()
		}
		err := opsAgentProbe(job, !agentDoneAt.IsZero(), verdict)
		if err == nil {
			return OpsAgentResult{ProjectPath: path, Verdict: verdict, Response: response}, nil
		}
		if job.Context != nil && job.Context.Err() != nil {
			return cancelled()
		}
		failed := strings.HasPrefix(strings.ToUpper(strings.TrimSpace(verdict)), "FAILED")
		grace := job.Grace
		if failed {
			grace = job.FailedGrace
		}
		givingUp := fatal(err) || (!agentDoneAt.IsZero() && time.Since(agentDoneAt) > grace) || time.Now().After(deadline)
		if givingUp {
			return OpsAgentResult{ProjectPath: path, Verdict: verdict, Response: response}, &OpsAgentError{Phase: OpsAgentPhaseProbe, Err: err, Verdict: verdict}
		}
		if detail := OpsScrub(err.Error()); detail != lastProbe {
			lastProbe = detail
			step := "probe"
			if job.ProbeStep != nil {
				if named := strings.TrimSpace(job.ProbeStep(err)); named != "" {
					step = named
				}
			}
			job.log(step, detail)
		}
		if !agentDoneAt.IsZero() && job.OnWaiting != nil {
			job.OnWaiting(verdict)
		}
	}
}

func opsDisplayHasSecret(text string) bool {
	return opsScrubBearer.MatchString(text) || opsScrubSecret.MatchString(text) || opsScrubUserinfo.MatchString(text) || opsScrubBlob.MatchString(text)
}

func (job OpsAgentJob) log(step, detail string) {
	if job.Log == nil {
		return
	}
	job.Log(step, OpsScrub(detail))
}

func opsAgentProbe(job OpsAgentJob, agentDone bool, verdict string) error {
	if job.Probe != nil {
		return job.Probe(agentDone, verdict)
	}
	if !agentDone {
		return errors.New("agent 正在执行")
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(verdict)), "FAILED") {
		reason := strings.TrimSpace(verdict)
		if reason == "" {
			reason = "FAILED"
		}
		return errors.New(reason)
	}
	return nil
}

func opsAgentLineVerdict(line, needle string) (string, bool) {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "`")
	line = strings.TrimSpace(line)
	for _, prefix := range []string{"- ", "* "} {
		if strings.HasPrefix(line, prefix) {
			line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			line = strings.Trim(line, "`")
			line = strings.TrimSpace(line)
			break
		}
	}
	if !strings.HasPrefix(line, needle) {
		return "", false
	}
	return strings.TrimSpace(strings.Trim(line[len(needle):], "`")), true
}

func opsAgentRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "正在") || strings.Contains(msg, "还未完成")
}

func opsAgentVerdict(resp *IMAgentResponse, marker string) string {
	if resp == nil {
		return ""
	}
	verdict := ""
	marker = strings.TrimSpace(marker)
	if marker != "" {
		needle := marker + ":"
		text := resp.Text
		if resp.Error != "" {
			text += "\n" + resp.Error
		}
		for _, line := range strings.Split(text, "\n") {
			if value, ok := opsAgentLineVerdict(line, needle); ok {
				verdict = value
			}
		}
	}
	if verdict == "" && resp.Error != "" {
		verdict = resp.Error
	}
	if len([]rune(verdict)) > 300 {
		verdict = string([]rune(verdict)[:300])
	}
	return verdict
}

var (
	opsScrubSecret   = regexp.MustCompile(`(?i)(password|DESKTOPD_TOKEN|MACLAW_ADMIN_SECRET|MACLAW_TOKEN_SECRET|MACLAW_DESKTOP_API_TOKEN|hub_token|hub_secret|node_secret|srv_admin_secret|desktopd_key|BOTNODE_ENROLL_CODE|ENROLL_CODE|authorization|密码|注册码)\s*[:=：]\s*(?:"[^"]*"|'[^']*'|Bearer\s+\S+|\S+)`)
	opsScrubBearer   = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	opsScrubUserinfo = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/@:]+:[^\s/@]+@`)
	opsScrubBlob     = regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`)
)

// OpsScrub redacts passwords, tokens, and long secrets from an operations log
// line and keeps it on one line.
func OpsScrub(text string) string {
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	text = opsScrubBearer.ReplaceAllString(text, "Bearer <redacted>")
	text = opsScrubSecret.ReplaceAllString(text, "$1=<redacted>")
	text = opsScrubUserinfo.ReplaceAllString(text, "$1<redacted>@")
	text = opsScrubBlob.ReplaceAllString(text, "<redacted>")
	if len([]rune(text)) > 2000 {
		text = string([]rune(text)[:2000]) + "..."
	}
	return strings.TrimSpace(text)
}
