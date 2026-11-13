package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

// A watcher turn (a check, or the resume after a job ends) is started by
// this server, not by Hub, so nobody fetches its final reply: papers 11-12
// 00:18, Bot 1's check read the real training log (dev F1 0.9109, 24.6 → 4–5
// samples/s, will hit the 2-hour limit), wrote it as its final reply, said
// 「已把这一情况告知本人」, and the person never saw it (no say was made).
// The final reply of a watcher turn is therefore delivered through the same
// say path, unless the turn already said something or has nothing new
// (empty reply or the silent token).
const desktopLongJobSilentToken = "NO_UPDATE"

var (
	desktopLongJobFollowEvery = 10 * time.Second
	desktopLongJobFollowMax   = 45 * time.Minute
)

// desktopLongJobRunGetter reads a run and its reply; tests replace it.
var desktopLongJobRunGetter = func(ctx context.Context, p agentservice.Principal, instanceID, sessionID, runID string) (status agentservice.RunStatus, reply string, err error) {
	svc := desktopLongJobService
	if svc == nil {
		return "", "", nil
	}
	run, err := svc.GetRun(ctx, p, instanceID, runID)
	if err != nil || run == nil {
		return "", "", err
	}
	if run.Status == agentservice.RunStatusRunning || strings.TrimSpace(run.AssistantMessageID) == "" {
		return run.Status, "", nil
	}
	msg, err := svc.GetMessage(ctx, p, instanceID, sessionID, run.AssistantMessageID)
	if err != nil || msg == nil {
		return run.Status, "", err
	}
	return run.Status, msg.Content, nil
}

// desktopLongJobRunSaid: the turn already spoke with action=say.
func desktopLongJobRunSaid(instanceID, runID string) bool {
	key := instanceID + "|" + runID + "|"
	desktopSayRuns.mu.Lock()
	defer desktopSayRuns.mu.Unlock()
	return desktopSayRuns.runs[key].n > 0
}

// desktopLongJobWatchReply is what of a watcher turn's final reply goes to
// the person: "" when there is nothing to send.
func desktopLongJobWatchReply(reply string) string {
	reply = strings.TrimSpace(reply)
	if reply == "" || strings.Contains(reply, desktopLongJobSilentToken) {
		return ""
	}
	return correctDesktopAttachClaim(reply, 0)
}

// desktopLongJobWatchReplyWith: attached is how many files go out with it.
func desktopLongJobWatchReplyWith(reply string, attached int) string {
	reply = strings.TrimSpace(reply)
	if reply == "" || strings.Contains(reply, desktopLongJobSilentToken) {
		return ""
	}
	return correctDesktopAttachClaim(reply, attached)
}

func desktopLongJobFollowRun(ctx context.Context, p agentservice.Principal, j *desktopLongJob, runID string, dec desktopLongJobDecision) {
	desktopLongJobFollowRunFiles(ctx, p, j, runID, dec, nil)
}

func desktopLongJobFollowRunFiles(ctx context.Context, p agentservice.Principal, j *desktopLongJob, runID string, dec desktopLongJobDecision, files *desktopWatchFiles) {
	if j == nil || strings.TrimSpace(runID) == "" {
		return
	}
	jobID, instanceID, sessionID := j.ID, j.InstanceID, j.SessionID
	deadline := desktopLongJobNow().Add(desktopLongJobFollowMax)
	for {
		if !desktopLongJobSleep(ctx, desktopLongJobFollowEvery) {
			return
		}
		status, reply, err := desktopLongJobRunGetter(ctx, p, instanceID, sessionID, runID)
		if err != nil {
			log.Printf("[desktop-longjob] job=%s watch run %s: %v", jobID, runID, err)
		}
		if status == agentservice.RunStatusRunning || status == "" {
			if desktopLongJobNow().After(deadline) {
				log.Printf("[desktop-longjob] job=%s watch run %s still running after %s; not followed further", jobID, runID, desktopLongJobFollowMax)
				return
			}
			continue
		}
		if status != agentservice.RunStatusSucceeded {
			log.Printf("[desktop-longjob] job=%s watch run %s ended %s", jobID, runID, status)
			return
		}
		attached := files.take()
		spoke := desktopLongJobRunSaid(instanceID, runID)
		if spoke && len(attached) == 0 {
			log.Printf("[desktop-longjob] job=%s watch run %s (%s) spoke itself", jobID, runID, dec)
			return
		}
		text := ""
		if !spoke {
			text = desktopLongJobWatchReplyWith(reply, len(attached))
		}
		if text == "" && len(attached) == 0 {
			log.Printf("[desktop-longjob] job=%s watch run %s (%s): nothing new, silent", jobID, runID, dec)
			return
		}
		text = desktopLongJobHideRaw(text, desktopLongJobRecordedRaws())
		if text == "" && len(attached) == 0 {
			log.Printf("[desktop-longjob] job=%s watch run %s (%s): reply was only the raw log", jobID, runID, dec)
			return
		}
		text = desktopWatchFilesText(text, attached)
		cur := desktopLongJobGet(jobID)
		if cur == nil {
			cur = j
		}
		body := map[string]any{
			"tenant_id":   cur.TenantID,
			"user_id":     desktopOwnerUserID(cur.UserID),
			"instance_id": instanceID,
			"room":        "",
			"text":        text,
			"key":         desktopMessageKey(instanceID+"|"+runID, "watch-reply", text),
		}
		if len(attached) > 0 {
			body["files"] = desktopWatchFilesBody(attached)
		}
		err = desktopLongJobSayFunc(context.WithoutCancel(ctx), body)
		if err != nil && len(attached) > 0 && desktopSayRefusedFiles(err) {
			// An older Hub reads a small say body only: say the text and
			// name the files that stayed on the computer.
			delete(body, "files")
			body["text"] = correctDesktopAttachClaim(text, 0) + "\n" + desktopWatchFilesMissed(attached)
			body["key"] = desktopMessageKey(instanceID+"|"+runID, "watch-reply-nofiles", text)
			err = desktopLongJobSayFunc(context.WithoutCancel(ctx), body)
			attached = nil
		}
		if err != nil && !strings.Contains(err.Error(), "ALREADY_SAID") && !strings.Contains(err.Error(), "409") {
			log.Printf("[desktop-longjob] job=%s watch run %s reply not delivered: %v", jobID, runID, err)
			return
		}
		log.Printf("[desktop-longjob] job=%s watch run %s (%s) reply delivered len=%d files=%d", jobID, runID, dec, len([]rune(text)), len(attached))
		if cur != nil {
			cur.LastNotice = desktopLongJobNow()
			_ = desktopLongJobSave(cur)
		}
		return
	}
}
