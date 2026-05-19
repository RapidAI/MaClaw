package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	corea2a "github.com/RapidAI/CodeClaw/corelib/a2a"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type groupDiscussionMachineSender interface {
	SendToMachine(machineID string, msg any) error
}

type GroupDiscussionHandler struct {
	svc    *GroupDiscussionService
	sender groupDiscussionMachineSender
}

func NewGroupDiscussionHandler(svc *GroupDiscussionService, senders ...groupDiscussionMachineSender) *GroupDiscussionHandler {
	var sender groupDiscussionMachineSender
	for _, candidate := range senders {
		if candidate != nil {
			sender = candidate
			break
		}
	}
	return &GroupDiscussionHandler{svc: svc, sender: sender}
}

func (h *GroupDiscussionHandler) RegisterRuntimeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/runtime/a2a/sessions", h.handleSessions)
	mux.HandleFunc("/runtime/a2a/sessions/", h.handleSessionAction)
}

func (h *GroupDiscussionHandler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/a2a/group-discussions", h.handleAdminGroupDiscussions)
}

func (h *GroupDiscussionHandler) RegisterHubRoutes(mux *http.ServeMux) {
	h.RegisterHubRoutesWithMiddleware(mux, nil)
}

func (h *GroupDiscussionHandler) RegisterHubRoutesWithMiddleware(mux *http.ServeMux, wrap func(http.HandlerFunc) http.HandlerFunc) {
	if wrap == nil {
		wrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	}
	mux.HandleFunc("/api/a2a/experts", wrap(h.handleHubExperts))
	mux.HandleFunc("/api/a2a/expert-profile", wrap(h.handleHubExpertProfile))
	mux.HandleFunc("/api/a2a/discussions/mine", wrap(h.handleHubDiscussionsMine))
	mux.HandleFunc("/api/a2a/invites/mine", wrap(h.handleHubInvitesMine))
	mux.HandleFunc("/api/a2a/consultations", wrap(h.handleHubConsultations))
	mux.HandleFunc("/api/a2a/consultations/", wrap(h.handleHubConsultationAction))
	mux.HandleFunc("/api/a2a/invites/", wrap(h.handleHubInviteAction))
}

func authenticatedGroupMachineID(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Header.Get("X-Authenticated-Machine-ID"))
}

func enforceAuthenticatedGroupIdentity(w http.ResponseWriter, r *http.Request, value *string, field string) bool {
	authenticatedID := authenticatedGroupMachineID(r)
	if authenticatedID == "" || value == nil {
		return true
	}
	current := strings.TrimSpace(*value)
	if current == "" {
		*value = authenticatedID
		return true
	}
	if strings.EqualFold(current, authenticatedID) {
		*value = authenticatedID
		return true
	}
	writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", field+" must match authenticated machine")
	return false
}

func requireAuthenticatedDiscussionParticipant(w http.ResponseWriter, r *http.Request, session *corea2a.Session) bool {
	authenticatedID := authenticatedGroupMachineID(r)
	if authenticatedID == "" {
		return true
	}
	if findGroupDiscussionParticipant(session, authenticatedID) != nil {
		return true
	}
	writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "authenticated machine is not a participant in this discussion")
	return false
}

func requireAuthenticatedDiscussionInitiator(w http.ResponseWriter, r *http.Request, session *corea2a.Session) bool {
	authenticatedID := authenticatedGroupMachineID(r)
	if authenticatedID == "" {
		return true
	}
	participant := findGroupDiscussionParticipant(session, authenticatedID)
	if participant != nil && strings.EqualFold(strings.TrimSpace(participant.RoleCode), "initiator") {
		return true
	}
	writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "only the discussion initiator can perform this action")
	return false
}

func (h *GroupDiscussionHandler) handleAdminGroupDiscussions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	snapshot, err := h.svc.AdminGroupDiscussionSnapshot(requestGroupDiscussionTenantID(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (h *GroupDiscussionHandler) handleHubExperts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	experts := h.svc.ListExpertProfiles(requestGroupDiscussionTenantID(r), 10*time.Minute)
	writeJSON(w, http.StatusOK, map[string]any{"experts": experts})
}

func (h *GroupDiscussionHandler) handleHubExpertProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}
	var profile corea2a.GroupProfile
	if !decodeJSON(w, r, &profile) {
		return
	}
	if !enforceAuthenticatedGroupIdentity(w, r, &profile.AgentID, "agent_id") {
		return
	}
	stored, err := h.svc.UpsertExpertProfile(requestGroupDiscussionTenantID(r), profile)
	if err != nil {
		writeError(w, http.StatusBadRequest, "PROFILE_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profile": stored})
}

func (h *GroupDiscussionHandler) handleHubDiscussionsMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	filter := listFilterFromRequest(r)
	if authenticatedID := authenticatedGroupMachineID(r); authenticatedID != "" {
		if filter.ParticipantID != "" && !strings.EqualFold(filter.ParticipantID, authenticatedID) {
			writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
			return
		}
		filter.ParticipantID = authenticatedID
	}
	items, err := h.svc.ListDiscussionSummaries(requestGroupDiscussionTenantID(r), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"discussions": items})
}

func (h *GroupDiscussionHandler) handleHubInvitesMine(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	q := r.URL.Query()
	filter := ListInvitationsFilter{ToID: q.Get("to_id"), Status: q.Get("status"), Limit: intQuery(q, "limit"), Offset: intQuery(q, "offset")}
	if authenticatedID := authenticatedGroupMachineID(r); authenticatedID != "" {
		if filter.ToID != "" && !strings.EqualFold(filter.ToID, authenticatedID) {
			writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "to_id must match authenticated machine")
			return
		}
		filter.ToID = authenticatedID
	}
	invites := h.svc.ListInvitations(requestGroupDiscussionTenantID(r), "", "", filter)
	writeJSON(w, http.StatusOK, map[string]any{"invites": invites})
}

func (h *GroupDiscussionHandler) handleHubConsultations(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req corea2a.GroupConsultationRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !enforceAuthenticatedGroupIdentity(w, r, &req.FromID, "from_id") {
		return
	}
	out, err := h.svc.CreateConsultation(requestGroupDiscussionTenantID(r), req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "CONSULTATION_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (h *GroupDiscussionHandler) handleHubConsultationAction(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	id, action := parseHubConsultationAction(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "MISSING_CONSULTATION_ID", "consultation id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		session, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		if !requireAuthenticatedDiscussionParticipant(w, r, session) {
			return
		}
		discussion := discussionSummaryFromSession(session)
		if authenticatedID := authenticatedGroupMachineID(r); authenticatedID != "" {
			decorateSummaryForParticipant(&discussion, session, authenticatedID)
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussion})
		return
	}
	if action == "detail" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		detail, err := h.svc.GetDiscussionDetail(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		if !requireAuthenticatedDiscussionParticipant(w, r, detail.Session) {
			return
		}
		participantID := firstNonEmptyQuery(r.URL.Query(), "participant_id", "agent_id", "from_id")
		if authenticatedID := authenticatedGroupMachineID(r); authenticatedID != "" {
			if participantID != "" && !strings.EqualFold(participantID, authenticatedID) {
				writeError(w, http.StatusForbidden, "MACHINE_FORBIDDEN", "participant_id must match authenticated machine")
				return
			}
			participantID = authenticatedID
		}
		if participantID != "" {
			decorateSummaryForParticipant(&detail.Discussion, detail.Session, participantID)
		}
		writeJSON(w, http.StatusOK, detail)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	switch action {
	case "invites":
		var inv corea2a.GroupInvitation
		if !decodeJSON(w, r, &inv) {
			return
		}
		if !enforceAuthenticatedGroupIdentity(w, r, &inv.FromID, "from_id") {
			return
		}
		inviteID, err := h.svc.AddInvitation(tid, id, inv)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVITE_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"invite_id": inviteID})
	case "messages":
		var msg corea2a.GroupDiscussionMessage
		if !decodeJSON(w, r, &msg) {
			return
		}
		if !enforceAuthenticatedGroupIdentity(w, r, &msg.FromID, "from_id") {
			return
		}
		session, err := h.svc.AddDiscussionMessage(tid, id, msg)
		if err != nil {
			writeError(w, http.StatusBadRequest, "MESSAGE_REJECTED", err.Error())
			return
		}
		h.notifyDiscussionMessage(session, persistedGroupDiscussionMessage(session, msg))
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "proposals":
		var req AddProposalRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if !enforceAuthenticatedGroupIdentity(w, r, &req.AuthorID, "author_id") {
			return
		}
		session, err := h.svc.AddProposal(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "PROPOSAL_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "reviews":
		var req AddReviewRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if !enforceAuthenticatedGroupIdentity(w, r, &req.ReviewerID, "reviewer_id") {
			return
		}
		session, err := h.svc.AddReview(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "REVIEW_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "decide":
		var req DecideRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		current, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		if !requireAuthenticatedDiscussionInitiator(w, r, current) {
			return
		}
		session, err := h.svc.Decide(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "DECISION_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "escalate":
		var req EscalateRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		if !enforceAuthenticatedGroupIdentity(w, r, &req.RaisedBy, "raised_by") {
			return
		}
		session, err := h.svc.Escalate(tid, id, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "ESCALATION_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "result":
		var result corea2a.GroupDiscussionResult
		if !decodeJSON(w, r, &result) {
			return
		}
		current, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		if !requireAuthenticatedDiscussionInitiator(w, r, current) {
			return
		}
		session, err := h.svc.SubmitDiscussionResult(tid, id, result)
		if err != nil {
			writeError(w, http.StatusBadRequest, "RESULT_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	case "pause", "resume", "cancel":
		current, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "CONSULTATION_NOT_FOUND", err.Error())
			return
		}
		if !requireAuthenticatedDiscussionInitiator(w, r, current) {
			return
		}
		session, err := h.svc.SetDiscussionState(tid, id, action)
		if err != nil {
			writeError(w, http.StatusBadRequest, "STATE_REJECTED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"discussion": discussionSummaryFromSession(session)})
	default:
		writeError(w, http.StatusNotFound, "ACTION_NOT_FOUND", "unsupported consultation action")
	}
}

func (h *GroupDiscussionHandler) handleHubInviteAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	inviteID, action := parseHubInviteAction(r.URL.Path)
	if inviteID == "" || (action != "accept" && action != "reject") {
		writeError(w, http.StatusNotFound, "INVITE_ACTION_NOT_FOUND", "unsupported invite action")
		return
	}
	var resp corea2a.GroupInvitationResponse
	if !decodeJSON(w, r, &resp) {
		return
	}
	if !enforceAuthenticatedGroupIdentity(w, r, &resp.FromID, "from_id") {
		return
	}
	if action == "accept" {
		resp.Decision = corea2a.GroupInvitationAccept
	} else {
		resp.Decision = corea2a.GroupInvitationReject
	}
	if err := h.svc.RespondInvitation(requestGroupDiscussionTenantID(r), inviteID, resp); err != nil {
		writeError(w, http.StatusBadRequest, "INVITE_RESPONSE_REJECTED", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *GroupDiscussionHandler) handleSessions(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	switch r.Method {
	case http.MethodGet:
		filter := listFilterFromRequest(r)
		sessions, err := h.svc.ListSessions(tid, filter)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
	case http.MethodPost:
		var req CreateSessionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON")
			return
		}
		session, err := h.svc.CreateSession(tid, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, session)
	default:
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *GroupDiscussionHandler) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	tid := requestGroupDiscussionTenantID(r)
	id, action := parseSessionAction(r.URL.Path)
	if id == "" {
		writeError(w, http.StatusBadRequest, "MISSING_SESSION_ID", "session id is required")
		return
	}
	if action == "" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		session, err := h.svc.GetSession(tid, id)
		if err != nil {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, session)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var session any
	var err error
	switch action {
	case "messages":
		var req AddMessageRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddMessage(tid, id, req)
		}
	case "proposals":
		var req AddProposalRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddProposal(tid, id, req)
		}
	case "reviews":
		var req AddReviewRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.AddReview(tid, id, req)
		}
	case "decide":
		var req DecideRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.Decide(tid, id, req)
		}
	case "escalate":
		var req EscalateRequest
		if decodeJSON(w, r, &req) {
			session, err = h.svc.Escalate(tid, id, req)
		}
	default:
		writeError(w, http.StatusNotFound, "ACTION_NOT_FOUND", "unsupported a2a action")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "A2A_ACTION_FAILED", err.Error())
		return
	}
	if session != nil {
		writeJSON(w, http.StatusOK, session)
	}
}

func listFilterFromRequest(r *http.Request) ListSessionsFilter {
	q := r.URL.Query()
	return ListSessionsFilter{
		OrgUnitID:     normalizeOrgUnitID(q.Get("org_unit_id"), q.Get("department_id")),
		Status:        normalizeSessionStatus(q.Get("status")),
		ParticipantID: firstNonEmptyQuery(q, "participant_id", "agent_id", "from_id"),
		Role:          strings.TrimSpace(q.Get("role")),
		Limit:         intQuery(q, "limit"),
		Offset:        intQuery(q, "offset"),
	}
}

func intQuery(q map[string][]string, key string) int {
	values := q[key]
	if len(values) == 0 {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(values[0]))
	if err != nil {
		return 0
	}
	return n
}

func firstNonEmptyQuery(q map[string][]string, keys ...string) string {
	for _, key := range keys {
		if values, ok := q[key]; ok {
			for _, value := range values {
				if strings.TrimSpace(value) != "" {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_BODY", "invalid JSON")
		return false
	}
	return true
}

func normalizeSessionStatus(value string) corea2a.SessionStatus {
	value = strings.TrimSpace(value)
	switch corea2a.SessionStatus(value) {
	case corea2a.SessionOpen, corea2a.SessionDecided, corea2a.SessionEscalated, corea2a.SessionClosed:
		return corea2a.SessionStatus(value)
	default:
		return ""
	}
}

func parseSessionAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/runtime/a2a/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func parseHubConsultationAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/a2a/consultations/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func parseHubInviteAction(path string) (string, string) {
	rest := strings.TrimPrefix(path, "/api/a2a/invites/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}

func requestGroupDiscussionTenantID(r *http.Request) string {
	if r == nil {
		return store.DefaultTenantID
	}
	if AdminFromContext(r.Context()) != nil {
		return store.NormalizeTenantID(RequestTenantID(r))
	}
	for _, key := range []string{"X-Hub-Tenant-ID", "X-Tenant-ID"} {
		if value := strings.TrimSpace(r.Header.Get(key)); value != "" {
			return store.NormalizeTenantID(value)
		}
	}
	if tenantID := RequestTenantID(r); strings.TrimSpace(tenantID) != "" {
		return store.NormalizeTenantID(tenantID)
	}
	return store.DefaultTenantID
}

func persistedGroupDiscussionMessage(session *corea2a.Session, fallback corea2a.GroupDiscussionMessage) corea2a.GroupDiscussionMessage {
	if session == nil || len(session.Messages) == 0 {
		return fallback
	}
	last := session.Messages[len(session.Messages)-1]
	return corea2a.GroupDiscussionMessage{
		ID:               last.ID,
		SessionID:        firstNonEmptyGroupStatus(last.SessionID, session.ID),
		FromID:           last.FromID,
		ToIDs:            append([]string(nil), last.ToIDs...),
		Kind:             last.Kind,
		Content:          last.Content,
		TextAttachments:  last.TextAttachments,
		ImageAttachments: last.ImageAttachments,
		FileAttachments:  last.FileAttachments,
		CreatedAt:        last.CreatedAt,
	}
}

func (h *GroupDiscussionHandler) notifyDiscussionMessage(session *corea2a.Session, msg corea2a.GroupDiscussionMessage) {
	if h == nil || h.sender == nil || session == nil {
		return
	}
	fromID := strings.TrimSpace(msg.FromID)
	if msg.SessionID == "" {
		msg.SessionID = session.ID
	}
	targetFilter := map[string]struct{}{}
	for _, id := range msg.ToIDs {
		if id = strings.TrimSpace(id); id != "" {
			targetFilter[strings.ToLower(id)] = struct{}{}
		}
	}
	for _, participant := range session.Participants {
		targetID := strings.TrimSpace(participant.ID)
		if targetID == "" || strings.EqualFold(targetID, fromID) {
			continue
		}
		if len(targetFilter) > 0 {
			if _, ok := targetFilter[strings.ToLower(targetID)]; !ok {
				continue
			}
		}
		envelope := corea2a.NewGroupEnvelope(newGroupDiscussionID("a2aenv"), corea2a.GroupMessageDiscussionMessage, fromID, time.Now().UTC())
		envelope.SessionID = session.ID
		envelope.ToIDs = []string{targetID}
		envelope.Message = &msg
		_ = h.sender.SendToMachine(targetID, map[string]any{
			"type": "ve:discussion_message",
			"ts":   time.Now().Unix(),
			"payload": map[string]any{
				"envelope":    envelope,
				"target_role": strings.TrimSpace(participant.RoleCode),
			},
		})
	}
}
