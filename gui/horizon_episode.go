package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/longhorizon"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func horizonRolesForNext(next longhorizon.NextStep) (execRole, auditRole, label string) {
	switch next {
	case longhorizon.NextCLI:
		return longhorizon.RoleCLIExecutor, longhorizon.RoleCLIAuditor, "CLI"
	case longhorizon.NextGUI:
		return longhorizon.RoleGUIExecutor, longhorizon.RoleGUIAuditor, "GUI"
	case longhorizon.NextBrowser:
		return longhorizon.RoleBrowserExecutor, longhorizon.RoleBrowserAuditor, "Browser"
	default:
		return "", "", ""
	}
}

func (h *IMMessageHandler) runHorizonEpisode(ctx context.Context, sess *horizonSession, ep longhorizon.EpisodeContext) string {
	if h == nil || sess == nil {
		return "horizon episode: missing session"
	}
	sess.mu.Lock()
	if sess.cancelled || sess.state == nil {
		sess.mu.Unlock()
		return "horizon episode: cancelled"
	}
	ownerID := sess.ownerID
	taskID := sess.state.TaskID
	projectRoot := sess.state.Policy.ProjectRoot
	sess.mu.Unlock()

	maxIter := horizonEpisodeMaxIterations(ep)
	horizonLog(sess, "episode_start", horizonLogKV(
		"role="+ep.Role,
		fmt.Sprintf("max_iter=%d", maxIter),
		horizonLogField("goal", ep.Goal),
	))
	ended := false
	defer func() {
		if !ended {
			horizonLog(sess, "episode_end", horizonLogKV("role="+ep.Role, "status=aborted"))
		}
	}()
	cfg := h.applyCodingRoutePreference(ownerID, h.getCodingLLMConfig(), false)
	loopCtx := NewLoopContext("horizon-"+taskID, maxIter, h.client)
	loopCtx.UserID = ownerID
	loopCtx.HorizonRole = ep.Role
	loopCtx.Runtime.RequestID = "horizon-" + taskID
	loopCtx.ComputerUseOwner = computerUseOwnerFromLoop(loopCtx, ownerID)
	loopCtx.ComputerUseRoutingText = ep.Goal
	if ep.Role == longhorizon.RoleGUIExecutor {
		loopCtx.ComputerUseActive = true
		loopCtx.ComputerUseFresh = true
		loopCtx.ComputerUseGateSettled = true
	}
	loopCtx.BindParentContext(ctx)
	restoreLoop := h.installHorizonLoopCtx(loopCtx, ownerID)
	defer restoreLoop()

	sess.mu.Lock()
	if sess.cancelled {
		sess.mu.Unlock()
		loopCtx.Done()
		ended = true
		horizonLog(sess, "episode_end", horizonLogKV("role="+ep.Role, "status=cancelled"))
		return "horizon episode: cancelled"
	}
	sess.loopCtx = loopCtx
	sess.mu.Unlock()
	defer func() {
		loopCtx.Done()
		sess.mu.Lock()
		if sess.loopCtx == loopCtx {
			sess.loopCtx = nil
		}
		sess.mu.Unlock()
	}()

	cuOwner := loopCtx.ComputerUseOwner
	if ep.Role == longhorizon.RoleGUIExecutor {
		if !armHorizonGUIEpisode(sess, loopCtx, ownerID, cuOwner, ep.Goal) {
			ended = true
			horizonLog(sess, "episode_end", horizonLogKV("role="+ep.Role, "status=cancelled"))
			return "horizon episode: cancelled"
		}
	} else {
		sess.mu.Lock()
		sess.computerUseOwner = ""
		sess.mu.Unlock()
	}

	sa := NewCodingSubAgent(h, cfg, h.client, projectRoot, loopCtx)
	sa.SetHorizonEpisode(ep)
	sa.SetCallbacks(nil, func(text string) {
		h.emitHorizonEvent("ai-assistant-progress", "", ownerID, text)
	})
	task := &TaskItem{
		Title:              ep.Goal,
		Description:        ep.Goal,
		AcceptanceCriteria: splitHorizonLines(ep.Acceptance),
		Status:             TaskExecPending,
	}
	result := sa.ExecuteTask(task, "", ep.Acceptance, ep.RelatedAudits)
	if result == nil {
		ended = true
		horizonLog(sess, "episode_end", horizonLogKV("role="+ep.Role, "status=nil"))
		return "executor returned nil"
	}
	result.HorizonOwned = true
	if result.RuntimeTaskID == "" {
		result.RuntimeTaskID = "horizon:" + taskID
	}
	if loopCtx.IsCancelled() || sess.isCancelled() {
		result.Status = TaskExecInterrupted
		if strings.TrimSpace(result.Summary) == "" {
			result.Summary = "uncertain abort"
		}
	}
	ended = true
	horizonLog(sess, "episode_end", horizonLogKV(
		"role="+ep.Role,
		"status="+string(result.Status),
		fmt.Sprintf("iters=%d", result.Iterations),
		fmt.Sprintf("tools=%d", result.ToolCalls),
	))
	return formatHorizonEpisodeResult(result)
}

func armHorizonGUIEpisode(sess *horizonSession, loopCtx *LoopContext, ownerID, cuOwner, goal string) bool {
	if sess == nil {
		return false
	}
	sess.mu.Lock()
	if sess.cancelled {
		sess.mu.Unlock()
		return false
	}
	sess.computerUseOwner = cuOwner
	sess.mu.Unlock()
	// Keep claim-only until releaseHorizonGUIAfterProbe so capacity
	// eviction cannot drop LastValidObserve before the auditor probe.
	setHorizonComputerUseClaimOnly(cuOwner, true)
	if sess.isCancelled() {
		setHorizonComputerUseClaimOnly(cuOwner, false)
		return false
	}
	resetComputerUseSessionForOwner(cuOwner)
	if sess.isCancelled() {
		setHorizonComputerUseClaimOnly(cuOwner, false)
		return false
	}
	maybeBeginComputerUseTask(loopCtx, ownerID, goal)
	return true
}

func formatHorizonEpisodeResult(result *CodingSubAgentResult) string {
	if result == nil {
		return "executor returned nil"
	}
	if result.Status == TaskExecInterrupted {
		summary := strings.TrimSpace(result.Summary)
		if result.Error != "" {
			summary = strings.TrimSpace(summary + "\nerror: " + result.Error)
		}
		if summary == "" {
			summary = "uncertain abort"
		}
		return fmt.Sprintf("status=%s\n%s", result.Status, summary)
	}
	if q := strings.TrimSpace(result.AskQuestion); q != "" {
		return "status=ask\n" + q
	}
	summary := strings.TrimSpace(result.Summary)
	if result.Error != "" {
		summary = strings.TrimSpace(summary + "\nerror: " + result.Error)
	}
	return fmt.Sprintf("status=%s\n%s", result.Status, summary)
}

const horizonAuditClaimCap = 8000

func horizonAuditEvidence(claim, probeDigest string) string {
	probe := strings.TrimSpace(probeDigest)
	claim = longhorizon.Clip(strings.TrimSpace(claim), horizonAuditClaimCap)
	if probe == "" {
		return claim
	}
	if claim == "" {
		return "Probe:\n" + probe
	}
	return "Probe:\n" + probe + "\nClaim:\n" + claim
}

func (h *IMMessageHandler) installHorizonLoopCtx(loopCtx *LoopContext, userID string) func() {
	if h == nil || loopCtx == nil {
		return func() {}
	}
	stealGlobal := loopCtx.HorizonRole == longhorizon.RoleGUIExecutor
	var prev *LoopContext
	prevUser := ""
	prevOwner := ""
	if stealGlobal {
		h.globalLoopMu.Lock()
		prev = h.currentLoopCtx
		prevUser = h.lastUserID
		h.currentLoopCtx = loopCtx
		h.lastUserID = userID
		h.globalLoopMu.Unlock()
		prevOwner = computerUseOwnerKey()
		setComputerUseOwner(computerUseOwnerFromLoop(loopCtx, userID))
	}
	return func() {
		if !stealGlobal {
			return
		}
		restoreOwner := false
		h.globalLoopMu.Lock()
		if h.currentLoopCtx == loopCtx {
			h.currentLoopCtx = prev
			h.lastUserID = prevUser
			restoreOwner = true
		}
		h.globalLoopMu.Unlock()
		// Only unwind sticky when this episode still owns currentLoopCtx.
		// A later overlapping GUI episode must keep its owner.
		if restoreOwner {
			setComputerUseOwner(prevOwner)
		}
	}
}

type horizonHostExecutionContextKey struct{}

func withHorizonHostExecution(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, horizonHostExecutionContextKey{}, true)
}

func isHorizonHostExecution(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(horizonHostExecutionContextKey{}).(bool)
	return v
}

// loopContextForComputerUseFence prefers the Horizon GUI episode loop over a
// leftover IM session loop. The local-file fence is per-turn on LoopContext;
// a prior attachment chat for the same UserID must not reject computer_*.
func (h *IMMessageHandler) loopContextForComputerUseFence(policyUserID, toolName string) *LoopContext {
	sessionCtx := h.runtimeLoopContextForOwner(policyUserID)
	if h == nil || !isComputerUseToolDefinition(toolName) {
		return sessionCtx
	}
	h.globalLoopMu.RLock()
	cur := h.currentLoopCtx
	h.globalLoopMu.RUnlock()
	if cur == nil || cur.HorizonRole != longhorizon.RoleGUIExecutor {
		return sessionCtx
	}
	policyUserID = strings.TrimSpace(policyUserID)
	if policyUserID == "" {
		return cur
	}
	if strings.TrimSpace(cur.UserID) == policyUserID {
		return cur
	}
	if computerUseOwnerFromLoop(cur, cur.UserID) == policyUserID {
		return cur
	}
	return sessionCtx
}

func (h *IMMessageHandler) horizonProbeForRole(ctx context.Context, role, projectRoot, ownerID string) longhorizon.ProbeResult {
	switch role {
	case longhorizon.RoleGUIAuditor:
		return horizonGUIProbe(ownerID)
	case longhorizon.RoleBrowserAuditor:
		return h.horizonBrowserProbe(ownerID)
	default:
		return h.horizonProbe(ctx, projectRoot)
	}
}

func horizonGUIProbe(ownerID string) longhorizon.ProbeResult {
	sess := cuSessionForOwner(ownerID)
	if sess == nil {
		return longhorizon.ProbeResult{}
	}
	obs := sess.LastValidObserve()
	if obs == nil {
		return longhorizon.ProbeResult{}
	}
	digest := stripHorizonProbeImages(computerUseProbeDigest(obs))
	if digest == "" {
		return longhorizon.ProbeResult{}
	}
	return longhorizon.ProbeResult{Digest: longhorizon.Clip(digest, 4000), OK: true}
}

func (h *IMMessageHandler) horizonBrowserProbe(ownerID string) longhorizon.ProbeResult {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return longhorizon.ProbeResult{}
	}
	preferredID := ""
	if h != nil {
		if sess := h.loadHorizonSessionOrRunning(ownerID); sess != nil {
			preferredID = sess.latestBrowserSessionID()
		}
	}
	var chosen *browser.BrowserAgentSession
	var preferred *browser.BrowserAgentSession
	for _, sess := range browser.ListAgentSessions() {
		if sess == nil {
			continue
		}
		if strings.TrimSpace(sess.OwnerID) != ownerID {
			continue
		}
		if preferredID != "" && sess.ID == preferredID {
			preferred = sess
		}
		if chosen == nil || sess.CreatedAt.After(chosen.CreatedAt) {
			chosen = sess
		}
	}
	if preferred != nil {
		chosen = preferred
	}
	if chosen == nil {
		return longhorizon.ProbeResult{}
	}
	state := chosen.State()
	var b strings.Builder
	if state.CurrentURL != "" {
		b.WriteString("url=")
		b.WriteString(state.CurrentURL)
		b.WriteByte('\n')
	}
	if state.CurrentTitle != "" {
		b.WriteString("title=")
		b.WriteString(state.CurrentTitle)
		b.WriteByte('\n')
	}
	if snap := latestBrowserSnapshot(state); snap != nil {
		if snap.PageFlags.Captcha || snap.PageFlags.LoginWall {
			b.WriteString("flags=captcha_or_login\n")
		}
		text := strings.TrimSpace(snap.PageTextExcerpt)
		if text == "" {
			text = strings.TrimSpace(snap.VisionExcerpt)
		}
		text = stripHorizonProbeImages(text)
		if text != "" {
			b.WriteString(text)
		}
	}
	digest := stripHorizonProbeImages(strings.TrimSpace(b.String()))
	if digest == "" {
		return longhorizon.ProbeResult{}
	}
	return longhorizon.ProbeResult{Digest: longhorizon.Clip(digest, 4000), OK: state.CurrentURL != ""}
}

func latestBrowserSnapshot(state browser.BrowserAgentState) *browser.BrowserSnapshot {
	if state.LastSnapshotID != "" {
		for i := range state.Snapshots {
			if state.Snapshots[i].SnapshotID == state.LastSnapshotID {
				snap := state.Snapshots[i]
				return &snap
			}
		}
	}
	if len(state.Snapshots) == 0 {
		return nil
	}
	snap := state.Snapshots[len(state.Snapshots)-1]
	return &snap
}

func stripHorizonProbeImages(s string) string {
	s = longhorizon.StripUntrustedMedia(s)
	if s == "" || s == "[image omitted]" {
		return ""
	}
	return longhorizon.Clip(s, 4000)
}

const horizonShortAcceptanceMaxRunes = 48

func horizonRoleMaxIterations(role string) int {
	switch role {
	case longhorizon.RoleGUIExecutor, longhorizon.RoleGUIAuditor:
		return longhorizon.GUIMaxIterations
	case longhorizon.RoleBrowserExecutor, longhorizon.RoleBrowserAuditor:
		return longhorizon.BrowserMaxIterations
	default:
		return longhorizon.CLIMaxIterations
	}
}

func horizonDefaultSystemPrompt(role string) string {
	switch role {
	case longhorizon.RoleGUIExecutor:
		return longhorizon.GUIExecutorSystemPrompt()
	case longhorizon.RoleGUIAuditor:
		return longhorizon.GUIAuditorSystemPrompt()
	case longhorizon.RoleBrowserExecutor:
		return longhorizon.BrowserExecutorSystemPrompt()
	case longhorizon.RoleBrowserAuditor:
		return longhorizon.BrowserAuditorSystemPrompt()
	default:
		return longhorizon.CLIExecutorSystemPrompt()
	}
}

func horizonEpisodeMaxIterations(ep longhorizon.EpisodeContext) int {
	if ep.Budget.MaxIterations > 0 {
		return ep.Budget.MaxIterations
	}
	return horizonRoleMaxIterations(ep.Role)
}

func horizonEpisodeUncertain(result string) bool {
	first, rest, _ := strings.Cut(strings.TrimSpace(result), "\n")
	status := strings.ToLower(strings.TrimSpace(first))
	switch status {
	case "status=interrupted", "status=cancelled":
		return true
	}
	return strings.EqualFold(strings.TrimSpace(first), "uncertain abort") || strings.EqualFold(strings.TrimSpace(rest), "uncertain abort")
}

func stripHorizonAcceptanceBullet(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*') && (s[1] == ' ' || s[1] == '	') {
		return strings.TrimSpace(s[1:])
	}
	return s
}

func shortHorizonGUIAcceptance(acceptance string) []string {
	lines := splitHorizonLines(acceptance)
	items := make([]string, 0, len(lines))
	for _, raw := range lines {
		item := stripHorizonAcceptanceBullet(raw)
		if item == "" {
			continue
		}
		if utf8.RuneCountInString(item) > horizonShortAcceptanceMaxRunes {
			return nil
		}
		items = append(items, item)
	}
	return normalizeComputerUseAcceptance(items)
}

func accelerateHorizonGUIAuditPass(acceptance, probeDigest string) bool {
	if strings.TrimSpace(probeDigest) == "" {
		return false
	}
	bullets := shortHorizonGUIAcceptance(acceptance)
	if len(bullets) == 0 {
		return false
	}
	for _, item := range bullets {
		if !computerUseCorpusHasAcceptance(probeDigest, item) {
			return false
		}
	}
	return true
}

func shouldAccelerateHorizonGUIAudit(execResult, acceptance, probeDigest string) bool {
	if !horizonExecutorClaimOK(execResult) {
		return false
	}
	return accelerateHorizonGUIAuditPass(acceptance, probeDigest)
}

func horizonExecutorClaimOK(result string) bool {
	if strings.TrimSpace(result) == "" || horizonEpisodeAskQuestion(result) != "" || horizonEpisodeUncertain(result) {
		return false
	}
	first, _, _ := strings.Cut(strings.TrimSpace(result), "\n")
	return strings.EqualFold(strings.TrimSpace(first), "status=passed")
}

func releaseHorizonGUIAfterProbe(ownerID string) {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID != "" {
		horizonLogOwner(ownerID, "gui_release", "")
	}
	clearComputerUseSessionActive()
	clearComputerUseTaskStateForOwner(ownerID)
	setHorizonComputerUseClaimOnly(ownerID, false)
}

func horizonBrowserSessionIDsForOwner(ownerID string) map[string]bool {
	ownerID = strings.TrimSpace(ownerID)
	out := map[string]bool{}
	if ownerID == "" {
		return out
	}
	for _, sess := range browser.ListAgentSessions() {
		if sess == nil || strings.TrimSpace(sess.OwnerID) != ownerID {
			continue
		}
		if id := strings.TrimSpace(sess.ID); id != "" {
			out[id] = true
		}
	}
	return out
}

func parseHorizonBrowserSessionID(text string) string {
	payload := extractHorizonJSONObject(text)
	if payload == nil {
		return ""
	}
	if id := horizonJSONString(payload["session_id"]); id != "" {
		return id
	}
	data, _ := payload["data"].(map[string]interface{})
	if data == nil {
		return ""
	}
	return horizonJSONString(data["session_id"])
}

func extractHorizonJSONObject(text string) map[string]interface{} {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	start := strings.Index(text, "{")
	if start < 0 {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(text[start:]))
	var payload map[string]interface{}
	if dec.Decode(&payload) != nil || len(payload) == 0 {
		return nil
	}
	return payload
}

func horizonJSONString(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

var (
	horizonHostToolDescOnce sync.Once
	horizonHostToolDesc     map[string]string
)

func horizonFallbackHostToolDescription(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	horizonHostToolDescOnce.Do(func() {
		horizonHostToolDesc = map[string]string{}
		reg := tool.NewRegistry()
		browser.RegisterTools(reg)
		for _, item := range reg.ListAvailable() {
			if item.Name == "" || strings.TrimSpace(item.Description) == "" {
				continue
			}
			horizonHostToolDesc[item.Name] = item.Description
		}
	})
	if d := strings.TrimSpace(horizonHostToolDesc[name]); d != "" {
		return d
	}
	return name
}

func (h *IMMessageHandler) releaseHorizonBrowserSessions(sess *horizonSession) {
	if h == nil || sess == nil {
		return
	}
	ids := sess.takeCreatedBrowserSessionIDs()
	if len(ids) == 0 {
		return
	}
	horizonLog(sess, "browser_release", horizonLogKV(fmt.Sprintf("count=%d", len(ids)), horizonLogField("ids", strings.Join(ids, ","))))
	for _, id := range ids {
		_ = browser.StopAgentSession(id, false)
	}
}

func horizonEpisodeAskQuestion(result string) string {
	result = strings.TrimSpace(result)
	if result == "" {
		return ""
	}
	if IsAskUserResult(result) {
		if req, ok := ParseAskUserResult(result); ok && req != nil {
			return firstNonEmptyHorizon(req.Question, req.Context)
		}
		return "Need more information."
	}
	first, rest, found := strings.Cut(result, "\n")
	if strings.EqualFold(strings.TrimSpace(first), "status=ask") {
		if found {
			return firstNonEmptyHorizon(strings.TrimSpace(rest), "Need more information.")
		}
		return "Need more information."
	}
	return ""
}

func recordHorizonExecutorAsk(sess *horizonSession, question string) {
	question = strings.TrimSpace(question)
	if sess == nil || question == "" {
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.cancelled || sess.state == nil {
		return
	}
	sess.state.Carryover = append(sess.state.Carryover, longhorizon.Clip("Executor asked: "+question, longhorizon.CarryoverItemCap))
	sess.state.Carryover = longhorizon.ClipCarryover(sess.state.Carryover)
	sess.persistLocked()
}

func recordHorizonExecutorStartLocked(sess *horizonSession, round int, next longhorizon.NextStep, goal string) {
	if sess == nil || sess.state == nil || sess.cancelled {
		return
	}
	goal = longhorizon.Clip(strings.TrimSpace(goal), 400)
	line := fmt.Sprintf("Executor round %d started (%s): %s", round, next, goal)
	sess.state.Carryover = append(sess.state.Carryover, longhorizon.Clip(line, longhorizon.CarryoverItemCap))
	sess.state.Carryover = longhorizon.ClipCarryover(sess.state.Carryover)
}
