package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/config"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/llm"
	corememory "github.com/RapidAI/CodeClaw/corelib/memory"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// VEMessageHandler processes incoming A2A messages when this maclaw instance
// is acting as a digital employee. It receives GroupEnvelope messages (Type=discussion_message)
// from the Hub, extracts content, processes them through the local AI agent (reusing
// the IMMessageHandler agent loop pattern), and sends streaming responses back.
type VEMessageHandler struct {
	app            *App
	mu             sync.Mutex
	activeSessions map[string]*veSession // key: consultation/session ID
}

// veSession tracks an active VE conversation session.
type veSession struct {
	SessionID    string
	RequesterID  string
	LastActivity time.Time
	Cancel       context.CancelFunc
	ctx          context.Context
	History      []agent.ConversationEntry
}

// NewVEMessageHandler creates a new VE message handler.
func NewVEMessageHandler(app *App) *VEMessageHandler {
	return &VEMessageHandler{
		app:            app,
		activeSessions: make(map[string]*veSession),
	}
}

func (h *VEMessageHandler) ensureSSHManager() *remote.SSHSessionManager {
	if h == nil || h.app == nil {
		return nil
	}
	localHandler := h.app.ensureLocalIMHandler()
	if localHandler == nil {
		return nil
	}
	return localHandler.ensureSSHManager()
}

// HandleGroupEnvelope processes an incoming GroupEnvelope when this maclaw instance
// is acting as a digital employee. It validates the envelope type is discussion_message,
// extracts the content from the embedded GroupDiscussionMessage, and invokes the local
// AI agent (reusing the IMMessageHandler agent loop pattern).
func (h *VEMessageHandler) HandleGroupEnvelope(envelope a2a.GroupEnvelope) {
	switch envelope.Type {
	case a2a.GroupMessageDiscussionMessage:
		// Existing discussion message handling
		if envelope.Message == nil {
			return
		}
		// Allow messages with attachments even if content is empty
		if strings.TrimSpace(envelope.Message.Content) == "" && !HasAttachments(*envelope.Message) {
			return
		}

		sessionID := envelope.SessionID
		if sessionID == "" {
			sessionID = envelope.Message.SessionID
		}
		if sessionID == "" {
			log.Printf("[ve-handler] received envelope without session ID, ignoring")
			return
		}

		h.HandleIncomingMessage(sessionID, *envelope.Message)

	case a2a.GroupMessageApprovalRequest:
		h.handleApprovalRequest(envelope)

	default:
		return
	}
}

// handleApprovalRequest processes an incoming approval_request envelope by
// deserializing the payload and routing it to the VE approval handler.
// Auto decisions are submitted to Hub Decision API (ResumeInstance).
// require_human enqueues a local pending instance for the human operator.
func (h *VEMessageHandler) handleApprovalRequest(envelope a2a.GroupEnvelope) {
	if len(envelope.Payload) == 0 {
		log.Printf("[ve-handler] received approval_request with empty payload, ignoring")
		return
	}

	var req veApprovalRequestPayload
	if err := json.Unmarshal(envelope.Payload, &req); err != nil {
		log.Printf("[ve-handler] failed to parse approval request payload: %v", err)
		return
	}
	// SessionID on the envelope is the Hub workflow instance id (see HubApprovalDispatcher).
	if strings.TrimSpace(req.InstanceID) == "" {
		req.InstanceID = strings.TrimSpace(envelope.SessionID)
	}

	cfg := h.loadVEApprovalConfig()
	if cfg == nil || !cfg.Enabled {
		log.Printf("[ve-handler] approval capability disabled, rejecting request %s", req.ID)
		h.submitHubApprovalDecision(req, "reject", "approval capability is disabled on this VE", "")
		return
	}

	details, err := decodeVEApprovalDetails(req.Details)
	if err != nil {
		log.Printf("[ve-handler] failed to parse approval request details: %v", err)
		h.submitHubApprovalDecision(req, "reject", "invalid approval request details: "+err.Error(), "")
		return
	}

	handler := NewVEApprovalHandler(cfg)
	detailsAny := map[string]any(details)
	veReq := &VEApprovalRequest{
		ID:                  req.ID,
		RequesterID:         req.RequesterID,
		RequesterName:       req.RequesterName,
		RequesterDepartment: firstNonEmptyMaclawAppString(maclawAppStringValue(detailsAny, "requester_department", "requesterDepartment", "department"), req.RequesterDepartment),
		RequesterRole:       firstNonEmptyMaclawAppString(maclawAppStringValue(detailsAny, "requester_role", "requesterRole", "role"), req.RequesterRole),
		RequesterSkills:     firstNonEmptyMaclawAppStringList(req.RequesterSkills, maclawAppStringListFromAny(firstNonEmptyMaclawAppAny(details["requester_skills"], details["requesterSkills"], details["skills"]))),
		Payload:             details,
	}

	decision, err := handler.HandleApprovalRequest(context.Background(), veReq)
	if err != nil {
		log.Printf("[ve-handler] approval request %s rejected: %v", req.ID, err)
		h.submitHubApprovalDecision(req, "reject", err.Error(), "")
		return
	}

	matchedRule := ""
	if decision.MatchedRule != nil {
		matchedRule = decision.MatchedRule.Name
	}
	// Two-phase role policy (digital_suggest / digital_review): digital output is
	// advisory or pre-check; human must finalize unless digital_review auto-rejects.
	execMode := normalizeVEExecutionMode(firstNonEmptyMaclawAppString(
		maclawAppStringValue(detailsAny, "execution_mode", "executionMode"),
		req.ExecutionMode,
	))
	decision = applyVETwoPhaseExecutionPolicy(execMode, decision)

	switch decision.Decision {
	case DecisionAutoApprove:
		h.submitHubApprovalDecision(req, "approve", decision.Rationale, matchedRule)
	case DecisionAutoReject:
		h.submitHubApprovalDecision(req, "reject", decision.Rationale, matchedRule)
	case DecisionRequireHuman:
		h.enqueueRequireHumanApproval(req, decision.Rationale, matchedRule, details)
	default:
		log.Printf("[ve-handler] unknown routing decision %q for request %s; escalating to human", decision.Decision, req.ID)
		h.enqueueRequireHumanApproval(req, decision.Rationale, matchedRule, details)
	}
}

// veApprovalRequestPayload is the JSON structure within an approval_request envelope payload.
// It mirrors hub/internal/workflow.ApprovalRequest plus optional requester org context.
type veApprovalRequestPayload struct {
	ID                  string          `json:"id"`
	InstanceID          string          `json:"instance_id"`
	NodeID              string          `json:"node_id"`
	RequesterID         string          `json:"requester_id"`
	RequesterName       string          `json:"requester_name"`
	RequesterDepartment string          `json:"requester_department,omitempty"`
	RequesterRole       string          `json:"requester_role,omitempty"`
	RequesterSkills     []string        `json:"requester_skills,omitempty"`
	WorkflowName        string          `json:"workflow_name"`
	Title               string          `json:"title"`
	Summary             string          `json:"summary"`
	ExecutionMode       string          `json:"execution_mode,omitempty"`
	Details             json.RawMessage `json:"details"`
}

func normalizeVEExecutionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "manual", "human":
		return "manual"
	case "digital_suggest", "digital-suggest", "suggest":
		return "digital_suggest"
	case "digital_review", "digital-review", "review":
		return "digital_review"
	case "auto", "automatic", "auto_approve":
		return "auto"
	default:
		return strings.ToLower(strings.TrimSpace(mode))
	}
}

// applyVETwoPhaseExecutionPolicy enforces digital_suggest / digital_review semantics:
//   - digital_suggest: never finalize on VE; auto_* becomes require_human with suggestion text
//   - digital_review: auto_reject may finalize; auto_approve still needs human confirmation
//   - auto / empty: leave decision unchanged
func applyVETwoPhaseExecutionPolicy(execMode string, decision *ApprovalDecision) *ApprovalDecision {
	if decision == nil {
		return &ApprovalDecision{Decision: DecisionRequireHuman, Rationale: "nil decision", DecidedAt: time.Now()}
	}
	switch normalizeVEExecutionMode(execMode) {
	case "digital_suggest":
		if decision.Decision == DecisionAutoApprove || decision.Decision == DecisionAutoReject {
			suggestion := decision.Rationale
			if suggestion == "" {
				suggestion = string(decision.Decision)
			}
			decision.Decision = DecisionRequireHuman
			decision.Rationale = "digital_suggest: " + suggestion
		}
	case "digital_review":
		if decision.Decision == DecisionAutoApprove {
			suggestion := decision.Rationale
			if suggestion == "" {
				suggestion = "digital pre-check passed"
			}
			decision.Decision = DecisionRequireHuman
			decision.Rationale = "digital_review: " + suggestion
		}
		// auto_reject stays reject (digital may fail the pre-check).
	}
	return decision
}

func decodeVEApprovalDetails(raw json.RawMessage) (map[string]interface{}, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]interface{}{}, nil
	}
	var details map[string]interface{}
	if err := json.Unmarshal(raw, &details); err != nil {
		return nil, err
	}
	if details == nil {
		details = map[string]interface{}{}
	}
	return details, nil
}

// submitHubApprovalDecision advances Hub WorkflowExecutor via Decision API.
// decision must be Hub wire format: approve | reject | escalate.
func (h *VEMessageHandler) submitHubApprovalDecision(req veApprovalRequestPayload, decision, rationale, matchedRule string) {
	if h == nil || h.app == nil {
		return
	}
	instanceID := strings.TrimSpace(req.InstanceID)
	nodeID := strings.TrimSpace(req.NodeID)
	decision = normalizeMaclawAppHubDecisionWire(decision)
	if instanceID == "" || nodeID == "" {
		log.Printf("[ve-handler] cannot submit hub decision: missing instance_id/node_id request=%s", req.ID)
		return
	}
	if decision == "" {
		log.Printf("[ve-handler] cannot submit hub decision: invalid decision for request %s", req.ID)
		return
	}
	// Machine token is required: workflowUserAuth sets X-Owner-ID = machine_id (the resolved approver identity).
	hubURL, token, err := h.app.getHubCredentials()
	if err != nil {
		log.Printf("[ve-handler] hub credentials unavailable for request %s: %v", req.ID, err)
		maclawAppApprovalTrace("ve_decision_no_creds", map[string]any{
			"request_id": req.ID, "hub_instance_id": instanceID, "hub_node_id": nodeID, "error": err.Error(),
		})
		return
	}
	path := "/api/v1/instances/" + url.PathEscape(instanceID) + "/nodes/" + url.PathEscape(nodeID) + "/decision"
	body := map[string]any{
		"decision":     decision,
		"rationale":    strings.TrimSpace(rationale),
		"matched_rule": strings.TrimSpace(matchedRule),
		"request_id":   strings.TrimSpace(req.ID),
	}
	maclawAppApprovalTrace("ve_decision_submit", map[string]any{
		"request_id": req.ID, "hub_instance_id": instanceID, "hub_node_id": nodeID, "decision": decision,
	})
	if _, err := h.app.postHubJSON(hubURL, token, path, body); err != nil {
		log.Printf("[ve-handler] hub decision failed request=%s instance=%s node=%s decision=%s: %v", req.ID, instanceID, nodeID, decision, err)
		maclawAppApprovalTrace("ve_decision_error", map[string]any{
			"request_id": req.ID, "hub_instance_id": instanceID, "hub_node_id": nodeID, "error": err.Error(),
		})
		// Keep a local attention record so the operator can retry from the App panel.
		h.recordHubApprovalAttention(req, err.Error())
		return
	}
	log.Printf("[ve-handler] hub decision ok request=%s instance=%s node=%s decision=%s", req.ID, instanceID, nodeID, decision)
	maclawAppApprovalTrace("ve_decision_ok", map[string]any{
		"request_id": req.ID, "hub_instance_id": instanceID, "hub_node_id": nodeID, "decision": decision,
	})
}

// enqueueRequireHumanApproval parks a hub-bound local projection for human decision
// without calling ResumeInstance (Hub stays blocked on the approval node).
func (h *VEMessageHandler) enqueueRequireHumanApproval(req veApprovalRequestPayload, rationale, matchedRule string, details map[string]interface{}) {
	if h == nil || h.app == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	title := firstNonEmptyMaclawAppString(req.Title, req.WorkflowName, "Approval request")
	inst := maclawAppApprovalInstance{
		AppID:           "hub-workflow",
		AppName:         firstNonEmptyMaclawAppString(req.WorkflowName, "Hub Workflow"),
		Title:           title,
		InstanceID:      "ve-appr-" + firstNonEmptyMaclawAppString(req.ID, shortRandomHex()),
		Status:          "pending",
		Lane:            "pending_my_approval",
		ApprovalEngine:  maclawAppApprovalEngineHub,
		HubInstanceID:   strings.TrimSpace(req.InstanceID),
		HubNodeID:       strings.TrimSpace(req.NodeID),
		CurrentNode:     strings.TrimSpace(req.NodeID),
		CurrentNodeIDs:  []string{strings.TrimSpace(req.NodeID)},
		WorkflowNodeIDs: []string{strings.TrimSpace(req.NodeID)},
		Owner:           firstNonEmptyMaclawAppString(req.RequesterID, "requester"),
		Applicant:       firstNonEmptyMaclawAppString(req.RequesterName, req.RequesterID, "requester"),
		Result:          firstNonEmptyMaclawAppString(rationale, req.Summary, "awaiting human approval"),
		BusinessStatus:  "pending",
		ResultStatus:    "pending",
		ResultPayload: map[string]any{
			"hub_request_id":   req.ID,
			"workflow_name":    req.WorkflowName,
			"summary":          req.Summary,
			"matched_rule":     matchedRule,
			"require_human":    true,
			"details":          details,
			"approval_result":  "pending",
			"panel_source":     "ve_require_human",
		},
		Events: []maclawAppApprovalEvent{{
			At: now, Node: strings.TrimSpace(req.NodeID), Actor: "ve",
			Decision: "require_human", Message: firstNonEmptyMaclawAppString(rationale, "escalated to human review"),
		}},
	}
	if stored, err := h.app.RecordMaclawAppApprovalInstance(inst); err != nil {
		log.Printf("[ve-handler] failed to enqueue require_human instance for request %s: %v", req.ID, err)
	} else {
		inst = stored
	}
	if h.app.ctx != nil {
		h.app.emitEvent("ve:approval-require-human", map[string]any{
			"request_id":      req.ID,
			"hub_instance_id": req.InstanceID,
			"hub_node_id":     req.NodeID,
			"title":           title,
			"rationale":       rationale,
			"matched_rule":    matchedRule,
			"instance":        inst,
		})
	}
	log.Printf("[ve-handler] require_human enqueued request=%s instance=%s node=%s local=%s", req.ID, req.InstanceID, req.NodeID, inst.InstanceID)
}

func (h *VEMessageHandler) recordHubApprovalAttention(req veApprovalRequestPayload, errMsg string) {
	if h == nil || h.app == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	inst := maclawAppApprovalInstance{
		AppID:          "hub-workflow",
		AppName:        firstNonEmptyMaclawAppString(req.WorkflowName, "Hub Workflow"),
		Title:          firstNonEmptyMaclawAppString(req.Title, req.WorkflowName, "Approval request"),
		InstanceID:     "ve-appr-" + firstNonEmptyMaclawAppString(req.ID, shortRandomHex()),
		Status:         "attention",
		Lane:           "attention",
		ApprovalEngine: maclawAppApprovalEngineHub,
		HubInstanceID:  strings.TrimSpace(req.InstanceID),
		HubNodeID:      strings.TrimSpace(req.NodeID),
		CurrentNode:    strings.TrimSpace(req.NodeID),
		HubSyncError:   errMsg,
		Owner:          firstNonEmptyMaclawAppString(req.RequesterID, "requester"),
		Applicant:      firstNonEmptyMaclawAppString(req.RequesterName, req.RequesterID),
		Result:         errMsg,
		ResultPayload:  map[string]any{"hub_sync_error": errMsg, "hub_request_id": req.ID},
		Events: []maclawAppApprovalEvent{{
			At: now, Node: strings.TrimSpace(req.NodeID), Actor: "ve",
			Decision: "attention", Message: errMsg,
		}},
	}
	if _, err := h.app.RecordMaclawAppApprovalInstance(inst); err != nil {
		log.Printf("[ve-handler] failed to record attention instance: %v", err)
	}
}

// loadVEApprovalConfig loads the VE approval configuration from the app config.
func (h *VEMessageHandler) loadVEApprovalConfig() *VEApprovalConfig {
	if h.app == nil {
		return nil
	}
	approvalCfg, err := h.app.GetVEApprovalConfig()
	if err != nil {
		return nil
	}
	return approvalCfg
}

// HandleIncomingMessage processes an incoming A2A discussion message
// when this maclaw instance is acting as a digital employee.
// It runs the local AI agent and sends streaming responses back via Hub.
// If the message contains attachments (TextAttachment/ImageAttachment/FileAttachment),
// they are decoded/downloaded and appended to the AI Agent input as context.
func (h *VEMessageHandler) HandleIncomingMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if h.shouldIgnoreIncomingVEMessage(msg) {
		return
	}

	hasAttachments := HasAttachments(msg)
	if hasAttachments && strings.TrimSpace(msg.Content) == "" {
		msg.Content = "Please inspect the attached file(s)."
	}

	// Process attachments and append context to message content
	if hasAttachments {
		attachmentContext := h.ProcessMessageAttachmentsForSession(sessionID, msg)
		if attachmentContext != "" {
			msg.Content = msg.Content + attachmentContext
		}
	}

	if strings.TrimSpace(msg.Content) == "" {
		return
	}

	h.mu.Lock()
	session, ok := h.activeSessions[sessionID]
	h.mu.Unlock()
	if !ok {
		restoredHistory := h.restoreSessionHistory(sessionID, msg)
		h.mu.Lock()
		session, ok = h.activeSessions[sessionID]
		if !ok {
			ctx, cancel := context.WithCancel(context.Background())
			session = &veSession{
				SessionID:    sessionID,
				RequesterID:  msg.FromID,
				LastActivity: time.Now(),
				Cancel:       cancel,
				ctx:          ctx,
				History:      restoredHistory,
			}
			h.activeSessions[sessionID] = session
		}
		h.mu.Unlock()
	}

	h.mu.Lock()
	session.LastActivity = time.Now()
	sessionCtx := session.ctx
	h.mu.Unlock()

	// Process in background goroutine to not block the WebSocket reader
	go h.processAndRespond(sessionCtx, sessionID, msg)
}

func (h *VEMessageHandler) shouldIgnoreIncomingVEMessage(msg a2a.GroupDiscussionMessage) bool {
	switch msg.Kind {
	case a2a.MessageStreamChunk, a2a.MessageStreamEnd:
		return true
	}
	fromID := strings.TrimSpace(msg.FromID)
	if fromID == "" || h == nil || h.app == nil {
		return false
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return false
	}
	localID := firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
	return localID != "" && veGroupParticipantIdentityMatches(fromID, localID)
}

// processAndRespond runs the AI agent on the incoming message and sends the final response back.
func (h *VEMessageHandler) processAndRespond(sessionCtx context.Context, sessionID string, msg a2a.GroupDiscussionMessage) {
	// Derive a per-message context from the session context so that
	// CloseSession() cancellation propagates to in-flight processing.
	ctx, cancel := context.WithTimeout(sessionCtx, 5*time.Minute)
	defer cancel()

	userMessage := msg.Content

	// Channel to signal that the first chunk has been sent
	firstChunkSent := make(chan struct{}, 1)
	// Channel to collect the final result or error
	type result struct {
		err error
	}
	resultCh := make(chan result, 1)

	// Start the AI agent processing with streaming
	go func() {
		err := h.runAgentWithStreaming(ctx, sessionID, userMessage, msg.ID, firstChunkSent)
		resultCh <- result{err: err}
	}()
	handleResult := func(r result, notifyRequester bool) {
		if r.err == nil {
			return
		}
		log.Printf("[ve-handler] error generating response for session %s: %v", sessionID, r.err)
		if notifyRequester {
			h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStatement,
				Content: fmt.Sprintf("[error] Failed to process message: %v", r.err),
			})
		}
	}
	sendTimeout := func() {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
				Kind:    a2a.MessageStatement,
				Content: "[timeout] Digital employee response timed out after 5 minutes. Please try again later",
			})
		}
	}

	select {
	case <-firstChunkSent:
		// A visible preflight notice was sent; keep waiting, but still honor the
		// per-message timeout if the final agent loop stalls.
		select {
		case r := <-resultCh:
			handleResult(r, true)
		case <-ctx.Done():
			sendTimeout()
		}
	case r := <-resultCh:
		// Agent finished (possibly with error) before timeout
		handleResult(r, true)
	case <-ctx.Done():
		sendTimeout()
		// Let the buffered result channel receive later; do not block after cancellation.
	}
}

// runAgentWithStreaming runs the AI agent and sends only user-visible output via Hub.
// Raw LLM deltas are internal to the agent loop; the final response is sent as
// one stream_chunk followed by stream_end.
func (h *VEMessageHandler) runAgentWithStreaming(ctx context.Context, sessionID, userMessage, requestID string, firstChunkSent chan<- struct{}) error {
	if query, ok := detectDigitalEmployeeSensitiveQuery(userMessage); ok {
		if h.shouldAnnounceSensitivePermissionRequest() {
			h.SendStreamChunk(sessionID, "\u6b63\u5728\u5bfb\u6c42\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef...")
			select {
			case firstChunkSent <- struct{}{}:
			default:
			}
		}
		if !h.authorizeSensitiveQuery(ctx, sessionID, query) {
			h.SendStreamChunk(sessionID, "\u4eba\u7c7b\u5458\u5de5\u672a\u6388\u6743\u63d0\u4f9b\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\uff0c\u5df2\u62d2\u7edd\u3002")
			h.SendStreamEnd(sessionID)
			return nil
		}
	}

	// Stream LLM deltas to the frontend in real-time so the user sees progressive
	// output instead of a static "思考中..." indicator. The agent loop calls
	// OnToken for intermediate rounds (tool-planning text) and the final round's
	// text is returned via LoopResult.Text (not via OnToken — see loop.go:302).
	// Both paths emit ve:stream_chunk events, giving a seamless streaming experience.
	//
	// IMPORTANT: onToken emits LOCAL Wails events only (no Hub network calls).
	// Hub sync uses batched chunks sent via a background goroutine with 80ms
	// flush intervals, giving remote devices progressive streaming without
	// per-token HTTP overhead.
	var streamingStarted int32
	senderID := h.getLocalAgentID() // cache once — avoid per-token config lock

	// Batched Hub streaming: accumulate deltas and flush every 80ms or 2KB.
	hubStreamCh := make(chan string, 256)
	hubStreamDone := make(chan struct{})
	go h.batchHubStreamChunks(sessionID, hubStreamCh, hubStreamDone)

	onToken := func(delta string) {
		if delta == "" {
			return
		}
		// Signal first visible output so the timeout watcher knows we're alive.
		if atomic.CompareAndSwapInt32(&streamingStarted, 0, 1) {
			select {
			case firstChunkSent <- struct{}{}:
			default:
			}
		}
		// Local frontend: immediate per-token display
		h.emitStreamChunkLocalWithSender(sessionID, delta, senderID)
		// Hub: queue for batched sending to remote devices
		select {
		case hubStreamCh <- delta:
		default:
			// Channel full — remote device will miss this delta but get the final
			// aggregated response below.
		}
	}

	fullResponse, err := h.runAgentForVE(ctx, sessionID, userMessage, requestID, onToken)
	// Close the Hub batch channel so the goroutine flushes remaining content and exits.
	close(hubStreamCh)
	<-hubStreamDone

	if err != nil {
		// If streaming was already in progress, close it so the frontend doesn't
		// hang in the streaming state with a blinking cursor forever.
		if atomic.LoadInt32(&streamingStarted) != 0 {
			h.emitStreamEndLocal(sessionID)
		}
		return err
	}

	if ctx.Err() != nil {
		if atomic.LoadInt32(&streamingStarted) != 0 {
			h.emitStreamEndLocal(sessionID)
		}
		return ctx.Err()
	}

	// The streaming LLM request already called onToken for each text delta during
	// the final round (doLLMRequestWithToolsStream emits deltas in real-time).
	// Only send fullResponse as a local chunk if no streaming occurred (e.g. the
	// streaming path fell back to non-streaming, or the loop produced text without
	// ever calling onToken).
	if strings.TrimSpace(fullResponse) != "" {
		if atomic.LoadInt32(&streamingStarted) == 0 {
			// No streaming occurred — send final text locally for immediate display
			// and to Hub for remote devices.
			h.emitStreamChunkLocal(sessionID, fullResponse)
			h.SendStreamChunk(sessionID, fullResponse)
		}
		// When streaming occurred, batched Hub chunks already delivered the content
		// progressively. No need to send fullResponse again (it would duplicate).
	}

	// Signal end of streaming locally (frontend transitions from streaming to final message).
	h.emitStreamEndLocal(sessionID)
	// Sync stream_end to Hub for remote devices.
	h.SendStreamEnd(sessionID)
	return nil
}

// runAgentForVE runs the AI agent for a VE session.
// Uses a dedicated agent loop with VE-specific system prompt and a safe tool subset.
// This is intentionally separate from the main IMMessageHandler to maintain security
// isolation: VE sessions don't trigger workflow engines, coding gates, or other
// main-agent middleware that could interfere with remote user requests.
func (h *VEMessageHandler) runAgentForVE(ctx context.Context, sessionID, userMessage, requestID string, onToken func(string)) (string, error) {
	if h.app == nil {
		return "", fmt.Errorf("app is nil")
	}

	llmCfg := h.app.GetMaclawLLMConfig()
	if llmCfg.URL == "" && llmCfg.Key == "" {
		return "", fmt.Errorf("LLM not configured")
	}

	ownerID := veAgentOwnerID(sessionID)
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		requestID = fmt.Sprintf("ve-%d", time.Now().UnixNano())
	}
	loopID := "ve-agent:" + strings.TrimSpace(sessionID)
	cleanupForegroundQoS := h.app.beginForegroundAgentLoop(ownerID, requestID, loopID)
	defer cleanupForegroundQoS()
	log.Printf("[ve-agent-loop] start owner=%q session=%q request_id=%q loop=%q", ownerID, sessionID, requestID, loopID)
	defer log.Printf("[ve-agent-loop] done owner=%q session=%q request_id=%q loop=%q", ownerID, sessionID, requestID, loopID)

	// Build VE-specific callbacks for the agent loop
	// Load conversation history for this VE session
	h.mu.Lock()
	history := h.getSessionHistory(sessionID)
	h.mu.Unlock()

	callbacks := &veAgentCallbacks{
		app:       h.app,
		ctx:       ctx,
		sessionID: sessionID,
		ownerID:   ownerID,
		requestID: requestID,
		loopID:    loopID,
		llmCfg:    llmCfg,
		onToken:   onToken,
		history:   history,
	}

	// Run the shared agent loop with VE-specific tools and system prompt
	result := agent.RunLoop(callbacks, userMessage, history, nil)

	// Save updated history
	h.mu.Lock()
	h.updateSessionHistory(sessionID, userMessage, result.Text)
	h.mu.Unlock()

	if result.Error != "" {
		return "", fmt.Errorf("%s", result.Error)
	}

	return result.Text, nil
}

func veAgentOwnerID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "digital-employee:unknown"
	}
	return "digital-employee:" + sessionID
}

// veAgentCallbacks implements agent.LoopCallbacks for VE sessions.
// It provides a simplified agent loop with VE-specific system prompt and tools.
type veAgentCallbacks struct {
	app       *App
	ctx       context.Context
	sessionID string
	ownerID   string
	requestID string
	loopID    string
	llmCfg    corelib.MaclawLLMConfig
	onToken   func(string)
	// history is the pre-turn conversation used for multi-turn auto-recall.
	history []agent.ConversationEntry

	// Cached knowledge base availability is computed once per agent loop invocation
	// to avoid repeated SQLite open/close and ensure BuildSystemPrompt and BuildTools
	// see a consistent value.
	knowledgeChecked bool
	hasKnowledge     bool
}

func (c *veAgentCallbacks) GetLLMConfig() corelib.MaclawLLMConfig {
	return c.llmCfg
}

func (c *veAgentCallbacks) LLMRequestContext(iteration int) (context.Context, func(error), error) {
	baseCtx := context.Background()
	if c != nil && c.ctx != nil {
		baseCtx = c.ctx
	}
	trace := llm.RequestTrace{
		Caller:    "ve-agent-loop",
		OwnerID:   strings.TrimSpace(c.ownerID),
		RequestID: strings.TrimSpace(c.requestID),
		LoopID:    strings.TrimSpace(c.loopID),
		Iteration: iteration,
	}
	ctx := llm.WithRequestTrace(baseCtx, trace)
	lease, scheduledTrace, err := acquireLLMSchedulerLease(ctx)
	if err != nil {
		return nil, nil, err
	}
	scheduledCtx, scheduledCancel := context.WithCancel(ctx)
	lease.SetCancel(scheduledCancel)
	return scheduledCtx, func(err error) {
		globalLLMScheduler.ObserveResult(scheduledTrace, err)
		scheduledCancel()
		lease.Release()
	}, nil
}

func (c *veAgentCallbacks) GetMaxIterations() int {
	return config.EffectiveMaxIterations(0) // use default
}

// veKnowledgeAvailable returns whether the knowledge base has content.
// Result is cached for the lifetime of this callbacks instance (one agent loop invocation).
func (c *veAgentCallbacks) veKnowledgeAvailable() bool {
	if c.knowledgeChecked {
		return c.hasKnowledge
	}
	c.knowledgeChecked = true
	c.hasKnowledge = veHasKnowledgeSources(c.app)
	return c.hasKnowledge
}
func (c *veAgentCallbacks) BuildSystemPrompt(userText string, isFirstTurn bool) string {
	var veName, veSkill string
	if c.app != nil {
		if status, err := c.app.GetVEStatus(); err == nil && status != nil && status.Employee != nil {
			veName = status.Employee.Name
			veSkill = status.Employee.SkillDescription
		}
	}

	var sb strings.Builder
	if veName != "" {
		sb.WriteString(fmt.Sprintf("You are %s, a digital employee AI assistant.", veName))
	} else {
		sb.WriteString("You are a digital employee AI assistant.")
	}
	if veSkill != "" {
		sb.WriteString(fmt.Sprintf(" Your specialty: %s.", veSkill))
	}
	if isFirstTurn {
		sb.WriteString("\nThis is the first turn of the session. Establish context and handle the request directly.")
	}

	sb.WriteString("\n\nCore rules:\n")
	sb.WriteString("- Reply in the user's language by default.\n")
	sb.WriteString("- Be concise, professional, and accurate; state uncertainty clearly.\n")
	sb.WriteString("- Provide complete runnable code examples when code is needed.\n")

	sb.WriteString("\nCapability boundaries:\n")
	sb.WriteString("- You run on the digital employee owner's machine for a remote user.\n")
	sb.WriteString("- You may read local files and list directories with read_file and list_directory.\n")

	hasKnowledge := c.veKnowledgeAvailable()
	if hasKnowledge {
		sb.WriteString("- You may search the local knowledge base with knowledge_search and knowledge_context_pack.\n")
		sb.WriteString("- The knowledge base is the preferred source for saved pages, documents, notes, and structured knowledge.\n")
	}

	allowedDirs := c.getVEAllowedDirectories()
	if len(allowedDirs) > 0 {
		sb.WriteString("- You cannot modify files, execute commands, or operate a browser; you may send files from the configured allowed directories.\n")
	} else {
		sb.WriteString("- You cannot modify files, execute commands, access the network, operate a browser, or send files until the owner adds at least one allowed access directory in Settings > Digital Employee.\n")
	}
	sb.WriteString("- Sensitive files such as .env, private keys, and credentials are blocked and must not be read or sent.\n")
	sb.WriteString("- If an operation is unsupported in digital employee mode, say so directly and do not invent reasons.\n")
	sb.WriteString("- You may answer questions, provide advice, generate text, analyze problems, and read allowed file content.\n")
	sb.WriteString("- You may use memory recall to retrieve the owner's accumulated facts, preferences, and project knowledge.\n")

	sb.WriteString("\nSafety rules:\n")
	sb.WriteString("- Do not reveal passwords, tokens, API keys, private keys, or other sensitive credentials.\n")
	sb.WriteString("- If sensitive information is unavailable or not approved, say that you cannot provide it.\n")

	if hasKnowledge {
		sb.WriteString("\n## Knowledge Base Rules\n")
		sb.WriteString("- Prefer the auto-recalled knowledge base context below when relevant, and cite sources when possible.\n")
		sb.WriteString("- If auto recall is insufficient, call knowledge_search or knowledge_context_pack.\n")
		sb.WriteString("- Distinguish knowledge-base information from general model knowledge.\n")
		sb.WriteString("- If the knowledge base has no relevant information, say that and then supplement with general knowledge.\n")
		prior := agent.PriorUserMessagesFromHistory(c.history, agent.KnowledgeAutoRecallPriorUserTurns)
		c.appendVEKnowledgeAutoRecall(&sb, userText, prior)
	}

	if len(allowedDirs) > 0 {
		sb.WriteString("\n## 文件发送能力 / File Sending\n")
		sb.WriteString("- You may use send_file to send files from the configured allowed directories.\n")
		sb.WriteString("- When the user asks you to send, give, attach, or share a file, call send_file; do not paste the file contents as plain text unless the user explicitly asks to view/read the contents.\n")
		sb.WriteString("- If the requested file is outside the allowed directories, say that the directory is not authorized and ask the owner to add it in Settings > Digital Employee > Allowed Access Directories.\n")
		sb.WriteString("- Allowed directories:\n")
		for _, dir := range allowedDirs {
			sb.WriteString(fmt.Sprintf("  - %s\n", dir))
		}
		sb.WriteString("- Before sending, browse with list_directory when needed and confirm the exact requested file.\n")
		sb.WriteString("- File size limit: 50 MB.\n")
		sb.WriteString("- 敏感文件 / Sensitive files must not be sent even from allowed directories.\n")
	}

	c.appendVEMemoryRecall(&sb, userText)
	return sb.String()
}

// appendVEKnowledgeAutoRecall searches the knowledge base using the user message
// and injects top results into the system prompt for VE sessions.
// Thresholds / inject counts / headers match IM + TUI via corelib/agent constants.
func (c *veAgentCallbacks) appendVEKnowledgeAutoRecall(b *strings.Builder, msg string, priorUserMessages []string) {
	if msg == "" || c.app == nil {
		return
	}
	minScore := agent.KnowledgeAutoRecallScoreThreshold
	if cfg, err := c.app.LoadConfig(); err == nil {
		if !cfg.IsKnowledgeAutoRecallEnabled() {
			return
		}
		minScore = cfg.EffectiveKnowledgeAutoRecallMinScore()
	}

	query := agent.ExpandKnowledgeAutoRecallQuery(msg, priorUserMessages)

	store, cleanupStore := getAutoRecallStoreForAppUse(c.app, true)
	defer cleanupStore()
	if store == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	results, err := store.Search(ctx, knowledge.SearchOptions{
		Query: query,
		Limit: agent.KnowledgeAutoRecallSearchLimit,
	})
	if err != nil {
		log.Printf("[ve_knowledge_auto_recall] search error: %v", err)
		return
	}
	if len(results) == 0 {
		// Caller only invokes when the KB has sources — hint deeper tool search.
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	topScore := results[0].Score
	maxInject := agent.KnowledgeAutoRecallMaxInjectWithMin(topScore, minScore)
	if maxInject == 0 {
		b.WriteString(agent.KnowledgeAutoRecallNoMatchHint)
		return
	}

	b.WriteString(agent.KnowledgeAutoRecallHeader)

	injected := 0
	for _, r := range results {
		if injected >= maxInject {
			break
		}
		if r.Score < minScore {
			break
		}
		source := r.Source.Title
		if source == "" {
			source = r.Source.RelativePath
		}
		if source == "" {
			source = r.Source.URI
		}
		text := knowledgeAutoRecallSnippet(r)
		if text == "" {
			continue
		}
		if len([]rune(text)) > agent.KnowledgeAutoRecallSnippetMaxRunes {
			text = string([]rune(text)[:agent.KnowledgeAutoRecallSnippetMaxRunes]) + "..."
		}
		b.WriteString(fmt.Sprintf("- [%s] %s\n", source, text))
		injected++
	}
}

// appendVEMemoryRecall performs proactive recall from the machine owner's memory
// store and injects relevant entries into the VE system prompt. This bridges the
// gap between the knowledge base (structured documents) and the memory system
// (accumulated facts, project knowledge, user preferences, task artifacts).
//
// Without this, the VE can only access knowledge base content but not memories
// like personal facts or SSH server credentials that the owner's maclaw has learned.
func (c *veAgentCallbacks) appendVEMemoryRecall(b *strings.Builder, msg string) {
	if c.app == nil {
		return
	}
	// Ensure memory store is initialized (it's lazily created; VE messages may
	// arrive before the main AI assistant triggers ensureMemoryStore).
	if c.app.memoryStore == nil {
		c.app.ensureMemoryStore()
	}
	memStore := c.app.memoryStore
	if memStore == nil {
		return
	}

	// --- User Facts: always inject the owner's user_fact summary ---
	// user_fact entries are excluded from
	// RecallDynamic (they're injected separately in the main AI assistant via
	// UserFactSummary). VE must also inject them to answer personal questions.
	b.WriteString(memStore.UserFactSummaryForPrompt(corememory.UserFactPromptOptions("\n## Owner Information")))

	// --- Dynamic memory context: index plus optional proactive recall. ---
	// Compact auto-extracted document bodies so VE recall embeds intent+paths only.
	promptContext, _ := memStore.ProactiveContextForPrompt(agent.CompactQueryForEmbedding(msg), corememory.VEProactivePromptOptions())
	b.WriteString(promptContext)

}

func (c *veAgentCallbacks) BuildTools(userText string) []map[string]interface{} {
	// VE sessions use the same ToolRegistry as the main agent, filtered by VE policy.
	// This ensures VE automatically inherits new read-only tools (knowledge, search, etc.)
	// without manual maintenance. Blocked tools (write, execute, modify) are removed.
	//
	// When VEAllowedDirectories is configured, filterToolsForVEWithConfig conditionally
	// unblocks send_file, list_directory, and read_file (Requirements 4.1, 4.2, 6.1).
	if c.app != nil {
		handler := c.app.ensureLocalIMHandler()
		if handler != nil && handler.registry != nil {
			allTools := NewDynamicToolBuilder(handler.registry).BuildAll()
			allowedDirs := c.getVEAllowedDirectories()
			return filterToolsForVEWithConfig(allTools, allowedDirs)
		}
	}
	// Fallback: if registry is unavailable, return minimal safe tools
	return veRemoteToolDefinitions(c.veKnowledgeAvailable())
}

// getVEAllowedDirectories reads the VEAllowedDirectories list from AppConfig.
// Returns an empty slice if config is unavailable or the field is not set.
func (c *veAgentCallbacks) getVEAllowedDirectories() []string {
	if c.app == nil {
		return nil
	}
	cfg, err := c.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.VEAllowedDirectories
}

func (c *veAgentCallbacks) ExecuteTool(name, argsJSON string) string {
	// Load allowedDirs once per tool invocation; used by both the blocked-tool
	// conditional unblock and the file-operation path validation below.
	allowedDirs := c.getVEAllowedDirectories()

	// Defense in depth: even if a blocked tool's definition leaked into the tool list,
	// the execution layer rejects it.
	//
	// Exception: tools in veConfigUnblockedTools are conditionally allowed when
	// VEAllowedDirectories is configured. The definition layer (filterToolsForVEWithConfig)
	// and execution layer must agree; both check allowedDirs.
	if isVEToolBlocked(name) {
		// Conditional unblock: when allowedDirs is configured, tools in
		// veConfigUnblockedTools are allowed through (execution-layer path
		// validation enforces directory scoping below).
		if !(len(allowedDirs) > 0 && veConfigUnblockedTools[name]) {
			if (name == "send_file" || name == "send_to_im") && len(allowedDirs) == 0 {
				return fmt.Sprintf("[error] %s is unavailable because no allowed access directories are configured. Add a directory in Settings > Digital Employee > Allowed Access Directories first.", name)
			}
			return fmt.Sprintf("[error] tool %s is unavailable in digital employee mode (safety policy)", name)
		}
	}

	// Parse args once; reused for action check and handler invocation.
	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf("[error] failed to parse arguments: %v", err)
		}
	}

	// Check per-tool action restrictions (e.g., memory save is blocked)
	if action, _ := args["action"].(string); action != "" {
		if isVEToolActionBlocked(name, action) {
			return fmt.Sprintf("[error] tool %s action %s is unavailable in digital employee mode", name, action)
		}
	}

	// VE MCP-only skill execution guard: run_skill is allowed only for
	// skills whose steps are all call_mcp_tool.
	if name == "run_skill" {
		skillName, _ := args["name"].(string)
		if skillName == "" {
			return "[error] missing name parameter"
		}
		allowed, reason := isVERunSkillAllowed(skillName, c.app)
		if !allowed {
			return fmt.Sprintf("[error] digital employee cannot run this Skill: %s", reason)
		}
		// Allowed; fall through to registry execution below.
	}

	// ---------------------------------------------------------------------------
	// Defense-in-depth path validation for VE file operations.
	// Validation chain (first failure stops execution):
	//   1. Tool blocked check (isVEToolBlocked) handled above
	//   2. Path parameter check (empty/missing)
	//   3. Directory containment check (ValidateVEFilePath / IsWithinAllowedDirs)
	//   4. Sensitive file check (CheckVEPathSensitive)
	//   5. File size check (> 50 MB, send_file only)
	//   6. Actual file read + send: success or OS error
	//
	// Requirements: 4.3, 4.6, 6.3, 6.4, 6.5
	// ---------------------------------------------------------------------------

	switch name {
	case "send_file", "send_to_im":
		path, _ := args["path"].(string)
		// Steps 2-3: empty path + directory containment check (also returns FileInfo to avoid double stat)
		canonicalPath, info, err := ValidateVEFilePathWithInfo(path, allowedDirs)
		if err != nil {
			return err.Error()
		}
		// Step 4: Sensitive file check
		if sensitiveErr := CheckVEPathSensitive(canonicalPath); sensitiveErr != nil {
			return sensitiveErr.Error()
		}
		// Step 5: File size check (50 MB limit for VE mode)
		// Uses info from ValidateVEFilePathWithInfo; no redundant os.Stat call.
		if info.Size() > veFileAttachmentMaxSize {
			return fmt.Sprintf("[error] file is too large: %d bytes; VE mode limit is 50 MB", info.Size())
		}
		// Step 6: Upload to Hub relay and emit a real A2A attachment message.
		if c.app == nil || strings.TrimSpace(c.sessionID) == "" {
			return fmt.Sprintf("[error] %s handler unavailable", name)
		}
		displayName, _ := args["file_name"].(string)
		message, _ := args["message"].(string)
		if strings.TrimSpace(message) == "" {
			message = fmt.Sprintf("已发送文件：%s", firstNonEmptyGroupString(displayName, filepath.Base(canonicalPath)))
		}
		if err := c.app.sendVEFileAttachmentMessage(c.sessionID, canonicalPath, displayName, message); err != nil {
			return fmt.Sprintf("[error] %s failed: %v", name, err)
		}
		return fmt.Sprintf("File %s has been sent to the user.", filepath.Base(canonicalPath))

	case "read_file":
		// When allowedDirs is configured, enforce directory containment.
		// When allowedDirs is empty, fall through to the original VE read_file
		// handler (veToolReadFile) which only does sensitive file check + size limit.
		// This preserves backward compatibility: VE can always read non-sensitive files.
		if len(allowedDirs) > 0 {
			path, _ := args["path"].(string)
			// Steps 2-3: empty path + directory containment check
			canonicalPath, err := ValidateVEFilePath(path, allowedDirs)
			if err != nil {
				return err.Error()
			}
			// Step 4: Sensitive file check
			if sensitiveErr := CheckVEPathSensitive(canonicalPath); sensitiveErr != nil {
				return sensitiveErr.Error()
			}
		}
		// Delegate to handler (applies its own sensitive check + size limit when no allowedDirs)
		return executeVERemoteTool(c.app, name, argsJSON)

	case "list_directory":
		// When allowedDirs is configured, enforce directory containment.
		// When allowedDirs is empty, fall through to the original VE list_directory
		// handler (veToolListDirectory) which only blocks sensitive directories.
		if len(allowedDirs) > 0 {
			path, _ := args["path"].(string)
			// Steps 2-3: empty path + directory containment check
			if _, err := IsWithinAllowedDirs(path, allowedDirs); err != nil {
				return err.Error()
			}
		}
		// Delegate to handler (applies its own sensitive dir check when no allowedDirs)
		return executeVERemoteTool(c.app, name, argsJSON)
	}

	// Execute via the main ToolRegistry; uses same handlers as main agent.
	// This gives VE access to knowledge_search, knowledge_context_pack, memory(recall),
	// web_search, web_fetch, discover_tool, and any future read-only tools automatically.
	if c.app != nil {
		handler := c.app.ensureLocalIMHandler()
		if handler != nil && handler.registry != nil {
			tool, ok := handler.registry.Get(name)
			if ok && tool.HandlerCtx != nil {
				ctx := c.ctx
				if ctx == nil {
					ctx = context.Background()
				}
				return tool.HandlerCtx(ctx, args, nil)
			}
			if ok && tool.Handler != nil {
				return tool.Handler(args)
			}
			if ok && tool.HandlerProg != nil {
				return tool.HandlerProg(args, nil)
			}
		}
	}

	// Final fallback for tools not in registry (shouldn't happen in normal operation)
	return executeVERemoteTool(c.app, name, argsJSON)
}

func (c *veAgentCallbacks) OnToken(delta string) {
	if c.onToken != nil {
		c.onToken(delta)
	}
}

func (c *veAgentCallbacks) OnProgress(text string) {}

func (c *veAgentCallbacks) OnToolCall(name string) {}

func (c *veAgentCallbacks) OnToolResult(name string) {}

func (c *veAgentCallbacks) ShouldStop() bool {
	return c.ctx.Err() != nil
}

const sensitivePermissionWaitingText = "\u6b63\u5728\u5bfb\u6c42\u4eba\u7c7b\u5458\u5de5\u8bb8\u53ef..."

func (h *VEMessageHandler) restoreSessionHistory(sessionID string, current a2a.GroupDiscussionMessage) []agent.ConversationEntry {
	if h == nil || h.app == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	detail, err := h.app.GroupDiscussionGetConsultationDetail(sessionID)
	if err != nil {
		return nil
	}
	cfg, _ := h.app.LoadConfig()
	localID := firstNonEmptyGroupString(cfg.RemoteMachineID, cfg.RemoteClientID)
	messages := detail.Messages
	if len(messages) == 0 && detail.Session != nil {
		messages = detail.Session.Messages
	}
	if detail.Session != nil && strings.TrimSpace(detail.Session.ContextSummary) != "" {
		messages = veMessagesAfterSummary(messages, detail.Session.SummaryUpToID)
	}
	history := buildVEConversationHistoryFromMessages(messages, localID, current)
	if detail.Session != nil {
		history = prependVEGroupContextSummary(history, detail.Session.ContextSummary)
	}
	return history
}

func veMessagesAfterSummary(messages []a2a.Message, summaryUpToID string) []a2a.Message {
	summaryUpToID = strings.TrimSpace(summaryUpToID)
	if summaryUpToID == "" {
		return messages
	}
	for i, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.ID), summaryUpToID) {
			return messages[i+1:]
		}
	}
	return messages
}

func prependVEGroupContextSummary(history []agent.ConversationEntry, summary string) []agent.ConversationEntry {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return history
	}
	entry := agent.ConversationEntry{Role: "user", Content: "Shared group memory:\n" + summary}
	out := make([]agent.ConversationEntry, 0, len(history)+1)
	out = append(out, entry)
	out = append(out, history...)
	return out
}

func buildVEConversationHistoryFromMessages(messages []a2a.Message, localID string, current a2a.GroupDiscussionMessage) []agent.ConversationEntry {
	localID = strings.TrimSpace(localID)
	entries := make([]agent.ConversationEntry, 0, len(messages))
	var stream strings.Builder
	streamFrom := ""
	flushStream := func() {
		content := cleanVESessionHistoryContent(stream.String())
		fromID := strings.TrimSpace(streamFrom)
		stream.Reset()
		streamFrom = ""
		if content == "" {
			return
		}
		entries = append(entries, agent.ConversationEntry{Role: veHistoryRoleForSender(fromID, localID), Content: veHistoryContentForSender(fromID, localID, content)})
	}
	for _, msg := range messages {
		if isCurrentVEHistoryMessage(msg, current) {
			break
		}
		fromID := strings.TrimSpace(msg.FromID)
		switch msg.Kind {
		case a2a.MessageStreamChunk:
			chunk := msg.Content
			if chunk == "" {
				continue
			}
			if streamFrom != "" && !veGroupParticipantIdentityMatches(streamFrom, fromID) {
				flushStream()
			}
			streamFrom = fromID
			stream.WriteString(chunk)
			continue
		case a2a.MessageStreamEnd:
			flushStream()
			continue
		default:
			flushStream()
		}
		content := cleanVESessionHistoryContent(msg.Content)
		if content == "" {
			continue
		}
		entries = append(entries, agent.ConversationEntry{Role: veHistoryRoleForSender(fromID, localID), Content: veHistoryContentForSender(fromID, localID, content)})
	}
	flushStream()
	if len(entries) > 40 {
		entries = entries[len(entries)-40:]
	}
	return entries
}

func isCurrentVEHistoryMessage(msg a2a.Message, current a2a.GroupDiscussionMessage) bool {
	currentID := strings.TrimSpace(current.ID)
	if currentID != "" {
		return strings.EqualFold(strings.TrimSpace(msg.ID), currentID)
	}
	if current.CreatedAt.IsZero() || !msg.CreatedAt.Equal(current.CreatedAt) {
		return false
	}
	return veGroupParticipantIdentityMatches(msg.FromID, current.FromID) &&
		msg.Kind == current.Kind &&
		strings.TrimSpace(msg.Content) == strings.TrimSpace(current.Content)
}

func veHistoryRoleForSender(fromID, localID string) string {
	if localID != "" && veGroupParticipantIdentityMatches(fromID, localID) {
		return "assistant"
	}
	return "user"
}

func veHistoryContentForSender(fromID, localID, content string) string {
	content = strings.TrimSpace(content)
	if content == "" || veHistoryRoleForSender(fromID, localID) == "assistant" {
		return content
	}
	fromID = strings.TrimSpace(fromID)
	if fromID == "" {
		return content
	}
	return "[" + fromID + "] " + content
}

func cleanVESessionHistoryContent(content string) string {
	content = strings.ReplaceAll(content, sensitivePermissionWaitingText, "")
	return strings.TrimSpace(content)
}

// getSessionHistory returns the conversation history for a VE session.
// Must be called with h.mu held.
func (h *VEMessageHandler) getSessionHistory(sessionID string) []agent.ConversationEntry {
	session, ok := h.activeSessions[sessionID]
	if !ok || session == nil {
		return nil
	}
	return session.History
}

// updateSessionHistory appends the user message and assistant response to the session history.
// Must be called with h.mu held.
func (h *VEMessageHandler) updateSessionHistory(sessionID, userMessage, assistantResponse string) {
	session, ok := h.activeSessions[sessionID]
	if !ok || session == nil {
		return
	}
	session.History = append(session.History,
		agent.ConversationEntry{Role: "user", Content: userMessage},
	)
	if strings.TrimSpace(assistantResponse) != "" {
		session.History = append(session.History,
			agent.ConversationEntry{Role: "assistant", Content: assistantResponse},
		)
	}
	// Keep history bounded
	if len(session.History) > 40 {
		session.History = session.History[len(session.History)-40:]
	}
}

// sendMessage sends a discussion message back through the Hub.
func (h *VEMessageHandler) sendMessage(sessionID string, msg a2a.GroupDiscussionMessage) {
	if !a2a.GroupDiscussionMessageHasPayload(msg) {
		log.Printf("[ve-handler] skipped empty outbound message for session %s kind=%s", sessionID, msg.Kind)
		return
	}
	if h.app == nil {
		return
	}

	// Get the agent ID for this maclaw instance
	cfg, _ := h.app.LoadConfig()
	agentID := cfg.RemoteMachineID
	if agentID == "" {
		agentID = cfg.RemoteClientID
	}

	msg.FromID = agentID
	msg.SessionID = sessionID
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	// Send via the VE A2A path so direct employee chat works without enabling group discussion tools.
	if err := h.app.sendVEA2AMessage(sessionID, msg); err != nil {
		log.Printf("[ve-handler] failed to send message for session %s: %v", sessionID, err)
	}
}

// SendStreamChunk sends a streaming response chunk back to the requester.
// Each chunk is constructed as a GroupDiscussionMessage with kind=stream_chunk.
func (h *VEMessageHandler) SendStreamChunk(sessionID, chunk string) {
	h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStreamChunk,
		Content: chunk,
	})
}

// SendStreamEnd signals the end of a streaming response.
// Constructed as a GroupDiscussionMessage with kind=stream_end.
func (h *VEMessageHandler) SendStreamEnd(sessionID string) {
	h.sendMessage(sessionID, a2a.GroupDiscussionMessage{
		Kind:    a2a.MessageStreamEnd,
		Content: "",
	})
}

// emitStreamChunkLocal sends a ve:stream_chunk event directly to the local frontend
// via Wails runtime, bypassing Hub network. Used for real-time token streaming where
// per-token HTTP calls would be prohibitively expensive.
func (h *VEMessageHandler) emitStreamChunkLocal(sessionID, chunk string) {
	if chunk == "" {
		return
	}
	h.emitStreamChunkLocalWithSender(sessionID, chunk, h.getLocalAgentID())
}

// emitStreamChunkLocalWithSender is the inner implementation that accepts a pre-resolved
// sender ID to avoid per-token config lock acquisition during high-frequency streaming.
func (h *VEMessageHandler) emitStreamChunkLocalWithSender(sessionID, chunk, senderID string) {
	if h.app == nil || h.app.ctx == nil || chunk == "" {
		return
	}
	h.app.emitEvent("ve:stream_chunk", map[string]any{
		"session_id":  sessionID,
		"content":     chunk,
		"chunk":       chunk,
		"sender_name": "本机AI",
		"sender_id":   senderID,
	})
}

// emitStreamEndLocal sends a ve:stream_end event directly to the local frontend.
func (h *VEMessageHandler) emitStreamEndLocal(sessionID string) {
	if h.app == nil || h.app.ctx == nil {
		return
	}
	senderID := h.getLocalAgentID()
	h.app.emitEvent("ve:stream_end", map[string]any{
		"session_id":  sessionID,
		"content":     "",
		"chunk":       "",
		"sender_name": "本机AI",
		"sender_id":   senderID,
	})
}

// batchHubStreamChunks consumes token deltas from ch, batches them with a short
// flush interval or size threshold, and sends each batch as a single
// SendStreamChunk call to Hub. This gives remote devices progressive streaming
// without per-token HTTP overhead.
//
// First non-empty content is flushed immediately so remote UIs leave "thinking"
// without waiting for the batch timer; subsequent deltas use the shared group
// Hub sync cadence (80ms / 2KB).
func (h *VEMessageHandler) batchHubStreamChunks(sessionID string, ch <-chan string, done chan<- struct{}) {
	defer close(done)

	flushInterval := groupHubSyncChunkFlushInterval
	maxBatchBytes := groupHubSyncChunkMaxBytes
	if flushInterval <= 0 {
		flushInterval = 80 * time.Millisecond
	}
	if maxBatchBytes <= 0 {
		maxBatchBytes = 2048
	}

	var buf strings.Builder
	timer := time.NewTimer(flushInterval)
	if !timer.Stop() {
		<-timer.C
	}
	timerActive := false
	firstFlushed := false

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		h.SendStreamChunk(sessionID, buf.String())
		buf.Reset()
		firstFlushed = true
	}

	for {
		select {
		case delta, ok := <-ch:
			if !ok {
				// Channel closed — flush remaining buffer.
				if timerActive && !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				flush()
				return
			}
			if delta == "" {
				continue
			}
			buf.WriteString(delta)
			// First paint ASAP for remote clients (TTFB for stream_chunk).
			if !firstFlushed {
				if timerActive && !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timerActive = false
				flush()
				continue
			}
			if buf.Len() >= maxBatchBytes {
				if timerActive && !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timerActive = false
				flush()
			} else if !timerActive {
				timer.Reset(flushInterval)
				timerActive = true
			}
		case <-timer.C:
			timerActive = false
			flush()
		}
	}
}

// getLocalAgentID returns the local machine/client ID for sender identification.
func (h *VEMessageHandler) getLocalAgentID() string {
	if h.app == nil {
		return "local-maclaw"
	}
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return "local-maclaw"
	}
	if id := cfg.RemoteMachineID; id != "" {
		return id
	}
	if id := cfg.RemoteClientID; id != "" {
		return id
	}
	return "local-maclaw"
}

// CloseSession closes a VE session and cleans up resources.
func (h *VEMessageHandler) CloseSession(sessionID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if session, ok := h.activeSessions[sessionID]; ok {
		if session.Cancel != nil {
			session.Cancel()
		}
		delete(h.activeSessions, sessionID)
	}
}

// ActiveSessionCount returns the number of active VE sessions.
func (h *VEMessageHandler) ActiveSessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.activeSessions)
}

// IsActiveSession returns true if the given session ID is currently active in this VE handler.
// Used by Hub echo filtering to suppress duplicate frontend display for locally-streamed responses.
func (h *VEMessageHandler) IsActiveSession(sessionID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, exists := h.activeSessions[sessionID]
	return exists
}
