package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
)

var errHorizonContextPolluted = errors.New("horizon context polluted")

func (h *IMMessageHandler) runHorizonSupervisor(sess *horizonSession) {
	if h == nil || sess == nil || sess.state == nil {
		return
	}
	defer h.releaseHorizonBrowserSessions(sess)
	ctx, cancel := context.WithCancel(context.Background())
	sess.mu.Lock()
	if sess.cancelled {
		lang := sess.lang
		sess.mu.Unlock()
		cancel()
		h.finishHorizonCancelled(sess, lang)
		return
	}
	sess.cancel = cancel
	ownerID := sess.ownerID
	lang := sess.lang
	sess.mu.Unlock()
	startedAt := time.Now()
	horizonLog(sess, "supervisor_start", "")
	defer func() {
		extra := fmt.Sprintf("elapsed=%s", time.Since(startedAt).Truncate(time.Millisecond))
		if sess.mu.TryLock() {
			task, owner, status, round := sess.horizonLogIDsLocked()
			sess.mu.Unlock()
			horizonLogIDs(task, owner, status, round, "supervisor_exit", extra)
			return
		}
		horizonLogIDs("", ownerID, "", 0, "supervisor_exit", extra)
	}()
	defer cancel()
	defer h.dropHorizonSessionIf(sess)
	defer func() {
		if rec := recover(); rec != nil {
			horizonLogIDs("", ownerID, "", 0, "supervisor_panic", horizonLogField("rec", fmt.Sprint(rec)))
			cuOwner := ownerID
			cancelled := false
			done := false
			locked := sess.mu.TryLock()
			if locked {
				if o := strings.TrimSpace(sess.computerUseOwner); o != "" {
					cuOwner = o
				}
				done = sess.state != nil && (sess.state.Completed || sess.state.Status == longhorizon.StatusDone)
				cancelled = sess.cancelled
				sess.mu.Unlock()
			}
			releaseHorizonGUIAfterProbe(cuOwner)
			if !locked {
				return
			}
			if cancelled {
				h.finishHorizonCancelled(sess, lang)
			} else if !done {
				h.horizonStopBlocked(sess, lang, "\u5185\u90e8\u9519\u8bef\uff0c\u957f\u7a0b\u4efb\u52a1\u5df2\u505c\u6b62\u3002", "Internal error; long-horizon task stopped.")
			}
		}
	}()

	h.startHorizonHeartbeat(ctx, ownerID)
	h.emitHorizonEvent("ai-assistant-progress", "", ownerID, "[Status] "+horizonText(lang, "\u957f\u7a0b\u8c03\u5ea6\u5df2\u542f\u52a8", "Long-horizon supervisor started"))

	sess.mu.Lock()
	maxRounds := longhorizon.ClampMaxRounds(sess.state.MaxRounds)
	sess.state.MaxRounds = maxRounds
	round := sess.state.RoundIndex
	sess.mu.Unlock()

	idleAsks := 0
	for {
		if ctx.Err() != nil || sess.isCancelled() {
			h.finishHorizonCancelled(sess, lang)
			return
		}
		sess.mu.Lock()
		if sess.cancelled || sess.state == nil {
			sess.mu.Unlock()
			h.finishHorizonCancelled(sess, lang)
			return
		}
		sess.state.Status = longhorizon.StatusManaging
		sess.persistLocked()
		sess.mu.Unlock()

		_ = sess.drainInbox()
		plan, err := h.horizonManagerPlan(ctx, sess)
		if err != nil {
			if ctx.Err() != nil || sess.isCancelled() {
				h.finishHorizonCancelled(sess, lang)
				return
			}
			if errors.Is(err, errHorizonContextPolluted) {
				h.horizonStopBlocked(sess, lang, "\u5185\u73af\u4e0a\u4e0b\u6587\u88ab\u6c61\u67d3\uff0c\u5df2\u505c\u6b62\u3002", "Inner episode context was polluted; stopped.")
				return
			}
			horizonLog(sess, "manager_fail", horizonLogField("err", err.Error()))
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Status = longhorizon.StatusBlocked
				sess.persistLocked()
			}
			sess.mu.Unlock()
			h.emitHorizonAssistantReply(sess, horizonText(lang, "\u8c03\u5ea6\u5931\u8d25\uff1a", "Manager failed: ")+longhorizon.Clip(err.Error(), 240))
			return
		}
		sess.mu.Lock()
		if sess.cancelled || sess.state == nil {
			sess.mu.Unlock()
			h.finishHorizonCancelled(sess, lang)
			return
		}
		sess.state.ManagerNext = plan.Next
		sess.mu.Unlock()
		horizonLog(sess, "manager_next", horizonLogKV("next="+string(plan.Next), horizonLogField("goal", plan.Goal), horizonLogField("q", plan.Question)))
		if plan.Next == longhorizon.NextAsk {
			idleAsks++
			if idleAsks >= maxRounds {
				h.horizonStopRoundLimit(sess, lang)
				return
			}
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Status = longhorizon.StatusAsking
				sess.persistLocked()
			}
			sess.mu.Unlock()
			h.emitHorizonAssistantReply(sess, firstNonEmptyHorizon(plan.Question, horizonText(lang, "\u9700\u8981\u4f60\u8865\u5145\u4fe1\u606f\u3002", "Need more information.")))
			sess.discardPendingInbox()
			if _, ok := h.waitHorizonInbox(ctx, sess); !ok {
				h.finishHorizonCancelled(sess, lang)
				return
			}
			continue
		}
		if plan.Next == longhorizon.NextBlocked {
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Status = longhorizon.StatusBlocked
				sess.persistLocked()
			}
			sess.mu.Unlock()
			horizonLog(sess, "blocked", horizonLogKV("next=blocked", horizonLogField("q", plan.Question)))
			h.emitHorizonAssistantReply(sess, firstNonEmptyHorizon(plan.Question, horizonText(lang, "\u957f\u7a0b\u4efb\u52a1\u5df2\u963b\u585e\u3002", "Long-horizon task blocked.")))
			return
		}
		if plan.Next == longhorizon.NextDone {
			sess.mu.Lock()
			if sess.cancelled || sess.state == nil {
				sess.mu.Unlock()
				h.finishHorizonCancelled(sess, lang)
				return
			}
			completed := longhorizon.MarkCompleted(sess.state)
			sess.persistLocked()
			sess.mu.Unlock()
			if completed {
				horizonLog(sess, "done", "")
				h.emitHorizonAssistantReply(sess, horizonText(lang, "\u957f\u7a0b\u4efb\u52a1\u5df2\u5b8c\u6210\u3002", "Long-horizon task completed."))
				return
			}
			horizonLog(sess, "done_rejected", "")
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Carryover = append(sess.state.Carryover, "Manager said done, but the latest real audit is not complete+clean+aligned.")
				sess.state.Carryover = longhorizon.ClipCarryover(sess.state.Carryover)
				sess.persistLocked()
			}
			sess.mu.Unlock()
			idleAsks++
			if idleAsks >= maxRounds {
				h.horizonStopRoundLimit(sess, lang)
				return
			}
			continue
		}
		if execRole, auditRole, label := horizonRolesForNext(plan.Next); execRole != "" {
			idleAsks = 0
			if round >= maxRounds {
				h.horizonStopRoundLimit(sess, lang)
				return
			}
			round++
			sess.mu.Lock()
			if sess.cancelled || sess.state == nil {
				sess.mu.Unlock()
				h.finishHorizonCancelled(sess, lang)
				return
			}
			sess.state.RoundIndex = round
			sess.state.Policy.RoundIndex = round
			stateCopy := longhorizon.CloneTaskState(sess.state)
			sess.mu.Unlock()
			ep := longhorizon.AssembleEpisodeContext(execRole, plan, stateCopy, "", stateCopy.Policy)
			if !longhorizon.AssembleIsClean(ep) {
				h.horizonStopBlocked(sess, lang, "\u5185\u73af\u4e0a\u4e0b\u6587\u88ab\u6c61\u67d3\uff0c\u5df2\u505c\u6b62\u3002", "Inner episode context was polluted; stopped.")
				return
			}
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Status = longhorizon.StatusExecuting
				recordHorizonExecutorStartLocked(sess, round, plan.Next, plan.Goal)
				sess.persistLocked()
			}
			projectRoot := ""
			taskID := ""
			if sess.state != nil {
				projectRoot = sess.state.Policy.ProjectRoot
				taskID = sess.state.TaskID
			}
			sess.mu.Unlock()
			h.emitHorizonEvent("ai-assistant-progress", "", ownerID, "[Status] "+label+" round "+fmt.Sprintf("%d", round))
			execResult := h.runHorizonEpisode(ctx, sess, ep)
			askQ := ""
			var probe longhorizon.ProbeResult
			probeOwner := ownerID
			if auditRole == longhorizon.RoleGUIAuditor {
				probeOwner = sess.computerUseOwnerOr(ownerID)
			}
			if ctx.Err() == nil && !sess.isCancelled() {
				askQ = horizonEpisodeAskQuestion(execResult)
				probe = h.horizonProbeForRole(ctx, auditRole, projectRoot, probeOwner)
				horizonLog(sess, "probe", horizonLogKV(
					"role="+auditRole,
					fmt.Sprintf("ok=%v", probe.OK),
					fmt.Sprintf("chars=%d", utf8.RuneCountInString(probe.Digest)),
					fmt.Sprintf("challenge=%v", longhorizon.ProbeShowsUserChallenge(probe.Digest)),
				))
				if askQ == "" && longhorizon.ProbeShowsUserChallenge(probe.Digest) {
					askQ = horizonText(lang, "\u9875\u9762\u9700\u8981\u9a8c\u8bc1\u7801\u6216\u767b\u5f55\uff0c\u8bf7\u5b8c\u6210\u540e\u56de\u590d\u7ee7\u7eed\u3002", "Captcha or login is blocking the page. Complete it and reply to continue.")
				}
			}
			if auditRole == longhorizon.RoleGUIAuditor {
				releaseHorizonGUIAfterProbe(probeOwner)
			}
			if ctx.Err() != nil || sess.isCancelled() {
				h.finishHorizonCancelled(sess, lang)
				return
			}
			auditEvidence := horizonAuditEvidence(execResult, probe.Digest)
			auditEp := longhorizon.AssembleEpisodeContext(auditRole, plan, stateCopy, auditEvidence, stateCopy.Policy)
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Status = longhorizon.StatusAuditing
				sess.persistLocked()
			}
			sess.mu.Unlock()
			var auditRaw string
			accelerated := false
			var auditErr error
			if askQ != "" {
				auditRaw = "Status: incomplete\nIntegrity: suspect\nAlignment: drifted\nSummary: executor asked the user."
			} else if horizonEpisodeUncertain(execResult) {
				auditRaw = "Status: incomplete\nIntegrity: suspect\nAlignment: drifted\nSummary: executor episode aborted uncertain."
			} else if !longhorizon.AssembleIsClean(auditEp) {
				auditRaw = "Status: incomplete\nIntegrity: suspect\nAlignment: drifted\nSummary: auditor context was polluted."
			} else if auditRole == longhorizon.RoleGUIAuditor && shouldAccelerateHorizonGUIAudit(execResult, plan.Acceptance, probe.Digest) {
				accelerated = true
				auditRaw = "Status: complete\nIntegrity: clean\nAlignment: aligned\nSummary: short host acceptance matched the latest observe."
			} else {
				auditRaw, auditErr = h.horizonRoleCompletion(ctx, auditEp, auditEvidence)
			}
			if ctx.Err() != nil || sess.isCancelled() {
				h.finishHorizonCancelled(sess, lang)
				return
			}
			if auditErr != nil {
				auditRaw = "Status: incomplete\nIntegrity: suspect\nAlignment: drifted\nSummary: auditor failed: " + auditErr.Error()
			}
			report := longhorizon.ParseAuditReport(auditRaw, probe)
			report.RoundIndex = round
			report.Mechanical = accelerated
			report.Digest = longhorizon.DigestOf(taskID, fmt.Sprintf("%d", round), report.Summary, probe.Digest)
			horizonLog(sess, "audit", horizonLogKV(
				"status="+report.Status,
				"integrity="+report.Integrity,
				"alignment="+report.Alignment,
				fmt.Sprintf("mechanical=%v", report.Mechanical),
				fmt.Sprintf("has_probe=%v", report.HasProbe),
				horizonLogField("summary", report.Summary),
			))
			sess.mu.Lock()
			if !sess.cancelled && sess.state != nil {
				sess.state.Rounds = append(sess.state.Rounds, longhorizon.ManagedRound{
					RoundIndex: round,
					Next:       plan.Next,
					Goal:       plan.Goal,
					Acceptance: plan.Acceptance,
					Audit:      &report,
				})
				sess.persistLocked()
			}
			sess.mu.Unlock()
			h.writeHorizonExperience(ctx, sess, &report)
			if askQ != "" {
				recordHorizonExecutorAsk(sess, askQ)
				sess.mu.Lock()
				if !sess.cancelled && sess.state != nil {
					sess.state.Status = longhorizon.StatusAsking
					sess.persistLocked()
				}
				sess.mu.Unlock()
				h.emitHorizonAssistantReply(sess, askQ)
				sess.discardPendingInbox()
				if _, ok := h.waitHorizonInbox(ctx, sess); !ok {
					h.finishHorizonCancelled(sess, lang)
					return
				}
			}
			continue
		}
		idleAsks++
		if idleAsks >= maxRounds {
			h.horizonStopRoundLimit(sess, lang)
			return
		}
		sess.mu.Lock()
		if !sess.cancelled && sess.state != nil {
			sess.state.Status = longhorizon.StatusAsking
			sess.persistLocked()
		}
		sess.mu.Unlock()
		h.emitHorizonAssistantReply(sess, firstNonEmptyHorizon(plan.Question, horizonText(lang, "\u9700\u8981\u4f60\u8865\u5145\u4fe1\u606f\u3002", "Need more information.")))
		sess.discardPendingInbox()
		if _, ok := h.waitHorizonInbox(ctx, sess); !ok {
			h.finishHorizonCancelled(sess, lang)
			return
		}
	}
}

func (h *IMMessageHandler) horizonManagerPlan(ctx context.Context, sess *horizonSession) (longhorizon.ManagerPlan, error) {
	sess.mu.Lock()
	if sess.state == nil {
		sess.mu.Unlock()
		return longhorizon.ManagerPlan{}, fmt.Errorf("missing task state")
	}
	stateCopy := longhorizon.CloneTaskState(sess.state)
	sess.mu.Unlock()
	plan := longhorizon.ManagerPlan{Goal: stateCopy.UserGoal}
	evidence := h.retrieveHorizonManagerEvidence(ctx, sess, stateCopy)
	ep, ok := longhorizon.AssembleManagerContext(plan, stateCopy, evidence, stateCopy.Policy)
	retrieved := utf8.RuneCountInString(evidence)
	used := utf8.RuneCountInString(ep.Evidence)
	notes := fmt.Sprintf("chars=%d used=%d", retrieved, used)
	if retrieved > 0 && used == 0 {
		notes += " dropped=true"
	}
	horizonLog(sess, "manager_notes", notes)
	if !ok {
		return longhorizon.ManagerPlan{}, errHorizonContextPolluted
	}
	user := "User goal:\n" + stateCopy.UserGoal
	if hist := ep.History; hist != "" {
		user += "\n\nTask state:\n" + hist
	}
	if strings.TrimSpace(ep.Evidence) != "" {
		user += "\n\nHorizon notes:\n" + ep.Evidence
	}
	raw, err := h.horizonRoleCompletion(ctx, ep, user)
	if err != nil {
		return longhorizon.ManagerPlan{}, err
	}
	return longhorizon.ParseManagerPlan(raw), nil
}

func (h *IMMessageHandler) horizonRoleCompletion(ctx context.Context, ep longhorizon.EpisodeContext, user string) (string, error) {
	cfg := h.getCodingLLMConfig()
	if strings.TrimSpace(cfg.Model) == "" && strings.TrimSpace(cfg.URL) == "" {
		cfg = h.getMaclawLLMConfig()
	}
	cfg = h.applyCodingRoutePreference(ep.Policy.OwnerID, cfg, false)
	messages := []interface{}{
		map[string]string{"role": "system", "content": ep.SystemPrompt},
		map[string]string{"role": "user", "content": user},
	}
	resp, err := agent.DoSimpleLLMRequestContext(ctx, cfg, messages, h.client, 2*time.Minute)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("empty completion")
	}
	return agent.StripThinkingTags(resp.Content), nil
}

func horizonWaitAborted(ctx context.Context, sess *horizonSession) bool {
	if sess == nil || sess.isCancelled() {
		return true
	}
	return ctx != nil && ctx.Err() != nil
}

func (h *IMMessageHandler) waitHorizonInbox(ctx context.Context, sess *horizonSession) (string, bool) {
	if sess == nil || sess.notify == nil {
		return "", false
	}
	var done <-chan struct{}
	if ctx != nil {
		done = ctx.Done()
	}
	for {
		if horizonWaitAborted(ctx, sess) {
			return "", false
		}
		if items := sess.drainInbox(); len(items) > 0 {
			if horizonWaitAborted(ctx, sess) {
				return "", false
			}
			joined := joinHorizonInbox(items)
			if joined == "" {
				continue
			}
			return joined, true
		}
		select {
		case <-done:
			return "", false
		case <-sess.notify:
		}
	}
}

func (h *IMMessageHandler) horizonStopRoundLimit(sess *horizonSession, lang string) {
	h.horizonStopBlocked(sess, lang, "\u5df2\u8fbe\u8f6e\u6b21\u4e0a\u9650\u3002", "Reached the round limit.")
}

func (h *IMMessageHandler) horizonStopBlocked(sess *horizonSession, lang, zh, en string) {
	if sess == nil {
		return
	}
	if sess.isCancelled() {
		h.finishHorizonCancelled(sess, lang)
		return
	}
	sess.mu.Lock()
	if sess.cancelled || sess.state == nil {
		sess.mu.Unlock()
		h.finishHorizonCancelled(sess, lang)
		return
	}
	maxRounds := longhorizon.ClampMaxRounds(sess.state.MaxRounds)
	sess.state.MaxRounds = maxRounds
	sess.state.RoundIndex = maxRounds
	sess.state.Policy.RoundIndex = maxRounds
	sess.state.Status = longhorizon.StatusBlocked
	sess.persistLocked()
	sess.mu.Unlock()
	horizonLog(sess, "blocked", horizonLogField("reason", en))
	h.emitHorizonAssistantReply(sess, horizonText(lang, zh, en))
}

func (h *IMMessageHandler) finishHorizonCancelled(sess *horizonSession, lang string) {
	if sess == nil {
		return
	}
	sess.mu.Lock()
	if sess.finalized {
		sess.mu.Unlock()
		return
	}
	held := true
	defer func() {
		if held {
			sess.mu.Unlock()
		}
	}()
	sess.finalized = true
	sess.cancelled = true
	if sess.state != nil && sess.state.Status != longhorizon.StatusCancelled {
		sess.state.Status = longhorizon.StatusCancelled
		sess.state.Completed = false
		sess.persistLocked()
	}
	emit := !sess.cancelNotified
	cuOwner := strings.TrimSpace(sess.computerUseOwner)
	if cuOwner == "" {
		cuOwner = strings.TrimSpace(sess.ownerID)
	}
	held = false
	sess.mu.Unlock()
	horizonLog(sess, "cancelled", horizonLogKV(fmt.Sprintf("notify=%v", emit), horizonLogField("cu_owner", cuOwner)))
	if cuOwner != "" {
		setHorizonComputerUseClaimOnly(cuOwner, false)
	}
	if emit {
		h.emitHorizonAssistantReply(sess, horizonText(lang, "\u957f\u7a0b\u4efb\u52a1\u5df2\u53d6\u6d88\u3002", "Long-horizon task cancelled."))
	}
}

func (h *IMMessageHandler) horizonProbe(ctx context.Context, projectRoot string) longhorizon.ProbeResult {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return longhorizon.ProbeResult{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if _, err := os.Stat(filepath.Join(projectRoot, ".git")); err == nil {
		cmd := exec.CommandContext(probeCtx, "git", "-C", projectRoot, "status", "--porcelain", "-b")
		out, err := cmd.CombinedOutput()
		digest := longhorizon.Clip(string(out), 4000)
		if digest == "" && err == nil {
			digest = "git status clean"
		}
		if digest != "" {
			return longhorizon.ProbeResult{Digest: digest, OK: err == nil}
		}
	}
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return longhorizon.ProbeResult{}
	}
	var b strings.Builder
	b.WriteString("workspace listing:\n")
	n := 0
	for _, entry := range entries {
		b.WriteString(entry.Name())
		b.WriteByte('\n')
		n++
		if n >= 40 {
			break
		}
	}
	digest := longhorizon.Clip(b.String(), 4000)
	if digest == "" {
		return longhorizon.ProbeResult{}
	}
	return longhorizon.ProbeResult{Digest: digest, OK: true}
}

func firstNonEmptyHorizon(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
