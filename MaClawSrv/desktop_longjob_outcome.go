package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A finished or failed background job is one structured completion. The raw
// log is context for a single summary turn and a file on the job record.
// It is not posted in the chat. Jobs that share a goal share that one turn.

func desktopLongJobTerminal(j *desktopLongJob) bool {
	return j != nil && (j.Status == "done" || j.Status == "failed")
}

// desktopLongJobGoalKey is the conversation goal, not a phrase in the command.
// Rounds of one experiment share a lineage. Other jobs share the session they
// were started in.
func desktopLongJobGoalKey(j *desktopLongJob) string {
	if j == nil {
		return ""
	}
	if lin := strings.TrimSpace(j.Lineage); lin != "" {
		return "lineage:" + lin
	}
	if sid := strings.TrimSpace(j.SessionID); sid != "" {
		return "session:" + strings.TrimSpace(j.InstanceID) + ":" + sid
	}
	return "job:" + j.ID
}

// desktopLongJobExitKind is why a failed job has the exit it has.
// code: the process wrote job.exit. no_exit: the process is gone and job.exit
// was not. process_gone: there is no process id to read. unknown: none of those.
func desktopLongJobExitKind(j *desktopLongJob) string {
	if j == nil || j.Status != "failed" {
		if j != nil && j.ExitCode != nil {
			return "code"
		}
		return ""
	}
	if j.ExitCode != nil {
		return "code"
	}
	if strings.TrimSpace(j.PID) == "" && !j.Alive {
		return "process_gone"
	}
	if !j.Alive {
		return "no_exit"
	}
	return "unknown"
}

func desktopLongJobOutcomeMode(jobs []*desktopLongJob) string {
	ask, cont := false, false
	for _, j := range jobs {
		switch strings.ToLower(strings.TrimSpace(j.Decision)) {
		case "ask_user":
			ask = true
		case "continue":
			cont = true
		}
	}
	if ask {
		return "ask"
	}
	if cont {
		return "continue"
	}
	return "judge"
}

func desktopLongJobGoalJobs(goal string) []*desktopLongJob {
	var out []*desktopLongJob
	for _, j := range desktopLongJobAll() {
		if desktopLongJobGoalKey(j) != goal || !desktopLongJobTerminal(j) {
			continue
		}
		out = append(out, j)
	}
	return out
}

func desktopLongJobWriteCompletion(j *desktopLongJob) {
	if j == nil || strings.TrimSpace(j.Dir) == "" {
		return
	}
	rec := map[string]any{
		"job_id":    j.ID,
		"goal":      desktopLongJobGoalKey(j),
		"status":    j.Status,
		"exit_kind": desktopLongJobExitKind(j),
		"exit_code": j.ExitCode,
		"tail":      j.LastTail,
		"log_file":  j.LogFile,
		"command":   j.Command,
		"recorded":  desktopLongJobNow().Format(time.RFC3339),
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(j.Dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(j.Dir, "completion.json"), raw, 0o644)
}

// desktopLongJobScheduleOutcome records the completion and starts at most
// one summary turn for the goal. A run that is already going does not drop
// the summary: it is queued until that run ends.
func desktopLongJobScheduleOutcome(ctx context.Context, j *desktopLongJob) error {
	if !desktopLongJobTerminal(j) {
		return nil
	}
	if cur := desktopLongJobGet(j.ID); cur != nil {
		if j.OutcomeState == "" {
			j.OutcomeState = cur.OutcomeState
			j.OutcomeRun = cur.OutcomeRun
			j.OutcomeWait = cur.OutcomeWait
		}
	}
	goal := desktopLongJobGoalKey(j)
	j.OutcomeGoal = goal
	desktopLongJobWriteCompletion(j)
	if j.OutcomeState == "sent" || j.OutcomeState == "folded" || j.OutcomeState == "queued" || j.OutcomeState == "record" {
		_ = desktopLongJobSave(j)
		return nil
	}
	_ = desktopLongJobSave(j)
	siblings := desktopLongJobGoalJobs(goal)
	var sentRun, waitRun string
	for _, s := range siblings {
		if s.OutcomeRun != "" {
			sentRun = s.OutcomeRun
		}
		if s.OutcomeWait != "" && s.OutcomeRun == "" {
			waitRun = s.OutcomeWait
		}
	}
	if sentRun != "" {
		j.OutcomeState = "folded"
		j.OutcomeRun = sentRun
		_ = desktopLongJobSave(j)
		log.Printf("[desktop-longjob] job=%s folded into outcome run %s for %s", j.ID, sentRun, goal)
		return desktopLongJobSupplementOutcome(ctx, j, sentRun)
	}
	if waitRun != "" {
		j.OutcomeState = "queued"
		j.OutcomeWait = waitRun
		_ = desktopLongJobSave(j)
		log.Printf("[desktop-longjob] job=%s outcome queued with %s until run %s ends", j.ID, goal, waitRun)
		return nil
	}
	block, err := desktopLongJobBlockingRun(ctx, j)
	if err != nil {
		log.Printf("[desktop-longjob] job=%s outcome skipped: %v", j.ID, err)
		return err
	}
	if block != "" {
		for _, s := range siblings {
			s.OutcomeGoal = goal
			s.OutcomeState = "queued"
			s.OutcomeWait = block
			_ = desktopLongJobSave(s)
		}
		log.Printf("[desktop-longjob] goal=%s outcome queued until run %s ends (%d jobs)", goal, block, len(siblings))
		return nil
	}
	return desktopLongJobStartOutcome(ctx, siblings)
}

func desktopLongJobStartOutcome(ctx context.Context, jobs []*desktopLongJob) error {
	if len(jobs) == 0 {
		return nil
	}
	fresh := make([]*desktopLongJob, 0, len(jobs))
	for _, j := range jobs {
		if j.OutcomeRun != "" || j.OutcomeState == "record" {
			continue
		}
		fresh = append(fresh, j)
	}
	if len(fresh) == 0 {
		return nil
	}
	anchor := fresh[0]
	prompt := desktopLongJobOutcomePrompt(fresh)
	for _, j := range fresh {
		j.OutcomeGoal = desktopLongJobGoalKey(anchor)
		j.OutcomeState = "sent"
		j.OutcomeWait = ""
		_ = desktopLongJobSave(j)
	}
	runID, err := desktopLongJobOutcomeSend(ctx, anchor, prompt)
	if err != nil {
		for _, j := range fresh {
			j.OutcomeState = "pending"
			_ = desktopLongJobSave(j)
		}
		log.Printf("[desktop-longjob] job=%s outcome skipped: %v", anchor.ID, err)
		return err
	}
	for _, j := range fresh {
		j.OutcomeRun = runID
		j.OutcomeState = "sent"
		_ = desktopLongJobSave(j)
	}
	log.Printf("[desktop-longjob] goal=%s outcome run %s covers %d jobs", desktopLongJobGoalKey(anchor), runID, len(fresh))
	return nil
}

// desktopLongJobOutcomeSend starts the summary turn. Tests replace it.
var desktopLongJobOutcomeSend = desktopLongJobOutcomeSendDefault

func desktopLongJobOutcomeSendDefault(ctx context.Context, j *desktopLongJob, prompt string) (string, error) {
	svc := desktopLongJobService
	if j.PrincipalUser == "" {
		if a, ok := desktopInstancePrincipal(j.InstanceID); ok {
			j.PrincipalTenant, j.PrincipalUser = a.TenantID, a.UserID
		} else if svc != nil {
			if p, err := svc.InstanceOwner(j.InstanceID); err == nil {
				j.PrincipalTenant, j.PrincipalUser = p.TenantID, p.UserID
			}
		}
	}
	if svc == nil || j.PrincipalUser == "" {
		return "", fmt.Errorf("no service principal for this job")
	}
	principal := agentservice.Principal{TenantID: j.PrincipalTenant, UserID: j.PrincipalUser}
	in := agentservice.SendMessageInput{
		Content:   prompt,
		SessionID: j.SessionID,
		Metadata:  map[string]string{"long_job_id": j.ID, "long_job_outcome": desktopLongJobGoalKey(j)},
	}
	files := &desktopWatchFiles{}
	in.PrepareExecution = files.prepare
	_, run, err := svc.SendMessageAsync(context.WithoutCancel(ctx), principal, j.InstanceID, in)
	if err != nil {
		return "", desktopLongJobPermanent(err)
	}
	if run == nil {
		return "", fmt.Errorf("outcome run was not started")
	}
	desktopLongJobNoteWatchRun(run.ID, j.ID)
	go desktopLongJobFollowRunFiles(context.WithoutCancel(ctx), principal, j, run.ID, longJobResume, files)
	return run.ID, nil
}

func desktopLongJobBlockingRun(ctx context.Context, j *desktopLongJob) (string, error) {
	svc := desktopLongJobService
	if j == nil || strings.TrimSpace(j.SessionID) == "" {
		return "", nil
	}
	if j.PrincipalUser == "" {
		if a, ok := desktopInstancePrincipal(j.InstanceID); ok {
			j.PrincipalTenant, j.PrincipalUser = a.TenantID, a.UserID
		} else if svc != nil {
			if p, err := svc.InstanceOwner(j.InstanceID); err == nil {
				j.PrincipalTenant, j.PrincipalUser = p.TenantID, p.UserID
			}
		}
	}
	// No owner on record means there is no running run to wait for. The
	// summary send reports a missing principal itself.
	if svc == nil || j.PrincipalUser == "" {
		return "", nil
	}
	principal := agentservice.Principal{TenantID: j.PrincipalTenant, UserID: j.PrincipalUser}
	runs, err := svc.ListRuns(ctx, principal, j.InstanceID, agentservice.ListRunsInput{Status: agentservice.RunStatusRunning, SessionID: j.SessionID})
	if err != nil {
		return "", desktopLongJobPermanent(err)
	}
	if len(runs) == 0 {
		return "", nil
	}
	return runs[0].ID, nil
}

func desktopLongJobSupplementOutcome(ctx context.Context, j *desktopLongJob, runID string) error {
	svc := desktopLongJobService
	if svc == nil || j == nil || strings.TrimSpace(runID) == "" || j.PrincipalUser == "" {
		return nil
	}
	principal := agentservice.Principal{TenantID: j.PrincipalTenant, UserID: j.PrincipalUser}
	text := "background job completion, for the summary only, do not paste the log to the user\n" + desktopLongJobCompletionEvent(j)
	receipt, err := svc.AppendRunSupplement(ctx, principal, j.InstanceID, runID, text)
	if err != nil || receipt == nil || !receipt.Accepted {
		log.Printf("[desktop-longjob] job=%s outcome supplement for run %s not accepted: %v", j.ID, runID, err)
	}
	return nil
}

// desktopLongJobReleaseOutcomes starts the queued summary for a goal once
// the run it was waiting on has ended.
func desktopLongJobReleaseOutcomes(runID string) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return
	}
	seen := map[string]bool{}
	for _, j := range desktopLongJobAll() {
		if j.OutcomeWait != runID || j.OutcomeRun != "" || !desktopLongJobTerminal(j) {
			continue
		}
		goal := desktopLongJobGoalKey(j)
		if seen[goal] {
			continue
		}
		seen[goal] = true
		var batch []*desktopLongJob
		for _, s := range desktopLongJobGoalJobs(goal) {
			if s.OutcomeRun != "" || (s.OutcomeWait != "" && s.OutcomeWait != runID) {
				continue
			}
			s.OutcomeWait = ""
			batch = append(batch, s)
		}
		if err := desktopLongJobStartOutcome(context.Background(), batch); err != nil {
			log.Printf("[desktop-longjob] goal=%s outcome after run %s: %v", goal, runID, err)
		}
	}
	for _, j := range desktopLongJobAll() {
		if j.OutcomeWait != runID || j.OutcomeState != "queued" || desktopLongJobTerminal(j) {
			continue
		}
		j.OutcomeWait = ""
		j.OutcomeState = ""
		_ = desktopLongJobSave(j)
		if _, err := desktopLongJobResumeAs(context.Background(), j, longJobResume); err != nil {
			log.Printf("[desktop-longjob] job=%s follow-up after run %s: %v", j.ID, runID, err)
		}
	}
}

func desktopLongJobSweepOutcomes() {
	seen := map[string]bool{}
	for _, j := range desktopLongJobAll() {
		if j.OutcomeWait == "" || j.OutcomeRun != "" {
			continue
		}
		if seen[j.OutcomeWait] {
			continue
		}
		seen[j.OutcomeWait] = true
		running, err := desktopLongJobRunIsRunning(j, j.OutcomeWait)
		if err != nil || running {
			continue
		}
		desktopLongJobReleaseOutcomes(j.OutcomeWait)
	}
}

func desktopLongJobRunIsRunning(j *desktopLongJob, runID string) (bool, error) {
	svc := desktopLongJobService
	if svc == nil || j == nil || j.PrincipalUser == "" || strings.TrimSpace(runID) == "" {
		return false, nil
	}
	principal := agentservice.Principal{TenantID: j.PrincipalTenant, UserID: j.PrincipalUser}
	run, err := svc.GetRun(context.Background(), principal, j.InstanceID, runID)
	if err != nil {
		return false, err
	}
	if run == nil {
		return false, nil
	}
	return run.Status == agentservice.RunStatusRunning, nil
}

func desktopLongJobOutcomePrompt(jobs []*desktopLongJob) string {
	var b strings.Builder
	b.WriteString("这一轮是后台任务的完成汇报，不是新任务。不要把日志原文贴给这个人。\n")
	b.WriteString("用一条消息说明：成没成、关键结果（用这个任务自己的目标指标，说人话）、有没有改进建议。\n")
	switch desktopLongJobOutcomeMode(jobs) {
	case "ask":
		b.WriteString("这一步需要这个人做决定。问清楚，不要自己开下一轮。\n")
	case "continue":
		b.WriteString("改进不需要这个人做决定。记录 decision=continue 后自己启动下一轮，不要先问。\n")
	default:
		b.WriteString("你来判断。不需要这个人做决定的改进，记录 decision=continue 并自己继续。只有花费、目标改变或数据选择这类需要他判断的事，记录 decision=ask_user 并问他。不要先问就能做的改进。\n")
	}
	b.WriteString("原始输出只在下面和任务记录里，供你阅读。发给这个人的那一条里不要出现这些原文。\n")
	for _, j := range jobs {
		b.WriteString("\n")
		b.WriteString(desktopLongJobCompletionEvent(j))
	}
	return b.String()
}

func desktopLongJobCompletionEvent(j *desktopLongJob) string {
	if j == nil {
		return ""
	}
	exit := "-"
	if j.ExitCode != nil {
		exit = fmt.Sprintf("%d", *j.ExitCode)
	}
	kind := desktopLongJobExitKind(j)
	if kind == "" {
		kind = "none"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "COMPLETION job=%s status=%s exit_kind=%s exit_code=%s\n", j.ID, j.Status, kind, exit)
	if j.Dir != "" {
		fmt.Fprintf(&b, "record=%s\n", filepath.Join(j.Dir, "completion.json"))
	}
	if j.LastTail != "" {
		b.WriteString("raw_log_for_you_only:\n")
		b.WriteString(j.LastTail)
		b.WriteString("\n")
	}
	return b.String()
}
