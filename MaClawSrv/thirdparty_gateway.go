package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

const (
	srvThirdPartyProtocolVersion = "1"
	srvThirdPartyRuntimeKey      = "maclawsrv:im:thirdparty"
	srvThirdPartySessionAgent    = "default"
	srvThirdPartyPollTimeoutSec  = 20
	srvThirdPartyMaxTimeoutSec   = 60
	srvThirdPartyMaxBatchSize    = 20
	srvThirdPartyMaxLimit        = 100
	srvThirdPartyMaxTextChars    = 20000
	srvThirdPartyMaxStoredMsgs   = 500
	srvThirdPartyMaxSeenEvents   = 2000
	srvThirdPartyMaxIDChars      = 128
	srvThirdPartyMaxAckIDs       = 100
)

type srvThirdPartyGatewayManager struct {
	svc *agentservice.Service

	mu               sync.Mutex
	clients          map[string]*srvThirdPartyClientState
	runtimeInstances map[string]srvThirdPartyRuntimeInstance
}

type srvThirdPartyRuntimeInstance struct {
	InstanceID         string
	LastIdentitySyncAt time.Time
}

type srvThirdPartyClientState struct {
	Next      int64
	Messages  []srvThirdPartyOutgoingMessage
	Acked     map[string]string
	Seen      map[string]string
	SeenOrder []string
	Notify    chan struct{}
}

type srvThirdPartyPrincipal struct {
	Principal agentservice.Principal
	Config    corelib.AppConfig
}

type srvThirdPartyHandshakeRequest struct {
	ClientID string `json:"clientId"`
}

type srvThirdPartyIncomingRequest struct {
	ClientID       string                   `json:"clientId"`
	EventID        string                   `json:"eventId"`
	ConversationID string                   `json:"conversationId"`
	User           srvThirdPartyUser        `json:"user"`
	Message        srvThirdPartyMessageBody `json:"message"`
	Metadata       map[string]string        `json:"metadata,omitempty"`
}

type srvThirdPartyUser struct {
	ID          string `json:"id,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

type srvThirdPartyMessageBody struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Text     string `json:"text,omitempty"`
	FileName string `json:"fileName,omitempty"`
}

type srvThirdPartyAckRequest struct {
	ClientID   string   `json:"clientId"`
	MessageIDs []string `json:"messageIds"`
	Status     string   `json:"status,omitempty"`
}

type srvThirdPartyOutgoingMessage struct {
	ID             string            `json:"id"`
	ReplyTo        string            `json:"replyTo,omitempty"`
	ClientID       string            `json:"clientId"`
	ConversationID string            `json:"conversationId,omitempty"`
	Type           string            `json:"type"`
	Text           string            `json:"text,omitempty"`
	CreatedAt      int64             `json:"createdAt"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Cursor         string            `json:"cursor"`
}

func newSrvThirdPartyGatewayManager(svc *agentservice.Service) *srvThirdPartyGatewayManager {
	return &srvThirdPartyGatewayManager{svc: svc, clients: map[string]*srvThirdPartyClientState{}, runtimeInstances: map[string]srvThirdPartyRuntimeInstance{}}
}

func (s *HTTPServer) handleThirdPartyGatewayHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newThirdPartyGatewayRequestID(),
		"status":          "connected",
		"protocolVersion": srvThirdPartyProtocolVersion,
		"serverTime":      time.Now().UnixMilli(),
	})
}

func (s *HTTPServer) handleThirdPartyGatewayHandshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	var req srvThirdPartyHandshakeRequest
	if !decodeThirdPartyGatewayJSON(w, r, &req) {
		return
	}
	clientID := normalizeThirdPartyGatewayID(req.ClientID)
	if err := validateThirdPartyGatewayID("clientId", clientID); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	s.thirdPartyIM.ensureClient(thirdPartyClientKey(tp.Principal, clientID))
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newThirdPartyGatewayRequestID(),
		"channelId":       "thirdparty:" + clientID,
		"protocolVersion": srvThirdPartyProtocolVersion,
		"serverTime":      time.Now().UnixMilli(),
		"mode":            "maclawsrv",
		"capabilities":    []string{"text", "long_poll", "ack", "idempotency"},
		"poll": map[string]int{
			"recommendedTimeoutSec": srvThirdPartyPollTimeoutSec,
			"maxTimeoutSec":         srvThirdPartyMaxTimeoutSec,
			"defaultLimit":          srvThirdPartyMaxBatchSize,
			"maxLimit":              srvThirdPartyMaxLimit,
		},
		"limits": map[string]int{"maxTextChars": srvThirdPartyMaxTextChars},
	})
}

func (s *HTTPServer) handleThirdPartyGatewayIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	var req srvThirdPartyIncomingRequest
	if !decodeThirdPartyGatewayJSON(w, r, &req) {
		return
	}
	if err := normalizeThirdPartyIncomingRequest(&req); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	maclawID := fmt.Sprintf("mc_in_%d_%s", time.Now().UnixMilli(), sanitizeThirdPartyGatewayID(req.EventID))
	clientKey := thirdPartyClientKey(tp.Principal, req.ClientID)
	duplicate, storedID := s.thirdPartyIM.markIncoming(clientKey, req.EventID, maclawID)
	maclawID = storedID
	if !duplicate {
		go s.thirdPartyIM.processIncoming(context.Background(), tp.Principal, req, maclawID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newThirdPartyGatewayRequestID(),
		"accepted":        true,
		"duplicate":       duplicate,
		"maclawMessageId": maclawID,
	})
}

func (s *HTTPServer) handleThirdPartyGatewayOutgoing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	clientID := normalizeThirdPartyGatewayID(r.URL.Query().Get("clientId"))
	if err := validateThirdPartyGatewayID("clientId", clientID); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	cursor, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("cursor")), 10, 64)
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = srvThirdPartyMaxBatchSize
	}
	if limit > srvThirdPartyMaxLimit {
		limit = srvThirdPartyMaxLimit
	}
	timeoutSec := srvThirdPartyPollTimeoutSec
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", "timeout must be a non-negative integer")
			return
		}
		timeoutSec = parsed
	}
	if timeoutSec > srvThirdPartyMaxTimeoutSec {
		timeoutSec = srvThirdPartyMaxTimeoutSec
	}
	clientKey := thirdPartyClientKey(tp.Principal, clientID)
	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	if timeoutSec == 0 {
		timer.Stop()
	}
	defer timer.Stop()
	for {
		msgs, next, hasMore, notify := s.thirdPartyIM.messagesAfter(clientKey, cursor, limit)
		if len(msgs) > 0 || timeoutSec == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requestId": newThirdPartyGatewayRequestID(), "messages": msgs, "nextCursor": strconv.FormatInt(next, 10), "hasMore": hasMore})
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			_, next, _, _ = s.thirdPartyIM.messagesAfter(clientKey, cursor, limit)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requestId": newThirdPartyGatewayRequestID(), "messages": []srvThirdPartyOutgoingMessage{}, "nextCursor": strconv.FormatInt(next, 10), "hasMore": false})
			return
		case <-notify:
		}
	}
}

func (s *HTTPServer) handleThirdPartyGatewayAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	var req srvThirdPartyAckRequest
	if !decodeThirdPartyGatewayJSON(w, r, &req) {
		return
	}
	clientID := normalizeThirdPartyGatewayID(req.ClientID)
	if err := validateThirdPartyGatewayID("clientId", clientID); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(req.MessageIDs) > srvThirdPartyMaxAckIDs {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("messageIds exceeds %d items", srvThirdPartyMaxAckIDs))
		return
	}
	s.thirdPartyIM.ack(thirdPartyClientKey(tp.Principal, clientID), req)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requestId": newThirdPartyGatewayRequestID()})
}

func (s *HTTPServer) authorizeThirdPartyGateway(w http.ResponseWriter, r *http.Request) (srvThirdPartyPrincipal, bool) {
	token := thirdPartyBearerToken(r)
	if token == "" {
		writeThirdPartyGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return srvThirdPartyPrincipal{}, false
	}
	tp, err := s.thirdPartyPrincipalByToken(r.Context(), token)
	if err != nil {
		writeThirdPartyGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return srvThirdPartyPrincipal{}, false
	}
	return tp, true
}

func (s *HTTPServer) thirdPartyPrincipalByToken(ctx context.Context, token string) (srvThirdPartyPrincipal, error) {
	users, err := s.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return srvThirdPartyPrincipal{}, err
	}
	var matched *srvThirdPartyPrincipal
	for _, user := range users {
		p := agentservice.Principal{TenantID: user.TenantID, UserID: user.ID}
		if !s.isActivePrincipal(ctx, p) {
			continue
		}
		cfg, err := s.svc.GetRawUserConfig(ctx, p)
		if err != nil || cfg == nil || !cfg.AppConfig.ThirdPartyGatewayEnabled {
			continue
		}
		expected := strings.TrimSpace(cfg.AppConfig.ThirdPartyGatewayToken)
		if expected == "" || len(expected) != len(token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(token)) == 1 {
			current := srvThirdPartyPrincipal{Principal: p, Config: cfg.AppConfig}
			if matched != nil {
				return srvThirdPartyPrincipal{}, errors.New("third-party gateway token is not unique")
			}
			matched = &current
		}
	}
	if matched != nil {
		return *matched, nil
	}
	return srvThirdPartyPrincipal{}, errors.New("third-party gateway token not found")
}

func (s *HTTPServer) validateThirdPartyGatewayTokenUnique(ctx context.Context, p agentservice.Principal, cfg corelib.AppConfig) error {
	if !cfg.ThirdPartyGatewayEnabled {
		return nil
	}
	token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	if token == "" {
		return nil
	}
	if agentservice.IsMaskedSecretPlaceholder(token) {
		current, err := s.svc.GetRawUserConfig(ctx, p)
		if err != nil {
			if errors.Is(err, agentservice.ErrUserConfigNotFound) {
				return nil
			}
			return err
		}
		if current == nil {
			return nil
		}
		token = strings.TrimSpace(current.AppConfig.ThirdPartyGatewayToken)
		if token == "" || agentservice.IsMaskedSecretPlaceholder(token) {
			return nil
		}
	}
	users, err := s.svc.ListAllUsers(ctx, agentservice.ListAllUsersAdminInput{Status: agentservice.UserStatusActive})
	if err != nil {
		return err
	}
	for _, user := range users {
		if user.TenantID == p.TenantID && user.ID == p.UserID {
			continue
		}
		otherPrincipal := agentservice.Principal{TenantID: user.TenantID, UserID: user.ID}
		if !s.isActivePrincipal(ctx, otherPrincipal) {
			continue
		}
		other, err := s.svc.GetRawUserConfig(ctx, otherPrincipal)
		if err != nil || other == nil || !other.AppConfig.ThirdPartyGatewayEnabled {
			continue
		}
		otherToken := strings.TrimSpace(other.AppConfig.ThirdPartyGatewayToken)
		if otherToken == "" || len(otherToken) != len(token) {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(otherToken), []byte(token)) == 1 {
			return fmt.Errorf("third-party gateway token is already used by user %s", user.ID)
		}
	}
	return nil
}

func (m *srvThirdPartyGatewayManager) processIncoming(parent context.Context, p agentservice.Principal, req srvThirdPartyIncomingRequest, maclawID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	instanceID, err := m.ensureThirdPartyInstance(ctx, p)
	if err != nil {
		m.enqueueError(p, req, maclawID, "third-party channel is not ready: "+err.Error())
		return
	}
	metadata := agentservice.IMMessageMetadata(agentservice.IMMessageMetadataInput{
		Platform:  "thirdparty",
		ContactID: req.ClientID + ":" + req.ConversationID,
		Extra: map[string]string{
			"runtime":         "maclawsrv",
			"client_id":       req.ClientID,
			"conversation_id": req.ConversationID,
			"event_id":        req.EventID,
			"maclaw_id":       maclawID,
		},
	})
	sendInput := agentservice.SendMessageInput{
		AgentID:          srvThirdPartySessionAgent,
		Title:            "Third-party " + req.ClientID,
		Content:          req.Message.Text,
		InputType:        "text/plain",
		Metadata:         metadata,
		SessionMetadata:  metadata,
		ClientSessionKey: "thirdparty:" + req.ClientID + ":" + req.ConversationID,
		ClientMessageID:  req.EventID,
	}
	_, _, assistant, err := m.svc.SendMessage(ctx, p, instanceID, sendInput)
	if errors.Is(err, agentservice.ErrInstanceNotFound) {
		m.clearCachedRuntimeInstanceID(p)
		instanceID, retryErr := m.ensureThirdPartyInstance(ctx, p)
		if retryErr != nil {
			err = retryErr
		} else {
			_, _, assistant, err = m.svc.SendMessage(ctx, p, instanceID, sendInput)
		}
	}
	if err != nil {
		m.enqueueError(p, req, maclawID, "third-party message failed: "+err.Error())
		return
	}
	if assistant != nil && strings.TrimSpace(assistant.Content) != "" {
		m.enqueue(p, req, srvThirdPartyOutgoingMessage{
			ID:             "mc_out_" + assistant.ID,
			ReplyTo:        maclawID,
			ClientID:       req.ClientID,
			ConversationID: req.ConversationID,
			Type:           "text",
			Text:           assistant.Content,
			CreatedAt:      assistant.CreatedAt.UnixMilli(),
			Metadata:       map[string]string{"assistant_message_id": assistant.ID},
		})
	}
	_ = parent
}

func (m *srvThirdPartyGatewayManager) ensureThirdPartyInstance(ctx context.Context, p agentservice.Principal) (string, error) {
	if instanceID, ok := m.cachedRuntimeInstanceID(p); ok {
		return instanceID, nil
	}
	instances, err := m.svc.ListInstances(ctx, p)
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		if inst.Metadata != nil && inst.Metadata["im_runtime_key"] == srvThirdPartyRuntimeKey {
			inst, err = srvSyncRuntimeIdentityInstance(ctx, m.svc, p, inst, instances, srvThirdPartyRuntimeKey, "thirdparty", "Third-party IM Assistant", "MaClawSrv third-party IM runtime")
			if err != nil {
				return "", err
			}
			if inst.Status == agentservice.InstanceStatusStopped {
				resumed, err := m.svc.ResumeInstance(ctx, p, inst.ID)
				if err != nil {
					return "", err
				}
				m.cacheRuntimeInstanceID(p, resumed.ID)
				return resumed.ID, nil
			}
			m.cacheRuntimeInstanceID(p, inst.ID)
			return inst.ID, nil
		}
	}
	inst, err := srvCreateRuntimeIdentityInstance(ctx, m.svc, p, instances, srvThirdPartyRuntimeKey, "thirdparty", "Third-party IM Assistant", "MaClawSrv third-party IM runtime")
	if err != nil {
		return "", err
	}
	m.cacheRuntimeInstanceID(p, inst.ID)
	return inst.ID, nil
}

func (m *srvThirdPartyGatewayManager) ensureClient(clientKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureClientLocked(clientKey)
}

func (m *srvThirdPartyGatewayManager) ensureClientLocked(clientKey string) *srvThirdPartyClientState {
	state := m.clients[clientKey]
	if state == nil {
		state = &srvThirdPartyClientState{Acked: map[string]string{}, Seen: map[string]string{}, Notify: make(chan struct{}, 1)}
		m.clients[clientKey] = state
	}
	return state
}

func (m *srvThirdPartyGatewayManager) StopPrincipal(p agentservice.Principal) {
	if m == nil {
		return
	}
	prefix := p.TenantID + "\x00" + p.UserID + "\x00"
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.clients {
		if strings.HasPrefix(key, prefix) {
			delete(m.clients, key)
		}
	}
	delete(m.runtimeInstances, principalRuntimeKey(p))
}

func (m *srvThirdPartyGatewayManager) StopTenant(tenantID string) {
	if m == nil {
		return
	}
	prefix := tenantID + "\x00"
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.clients {
		if strings.HasPrefix(key, prefix) {
			delete(m.clients, key)
		}
	}
	for key := range m.runtimeInstances {
		if strings.HasPrefix(key, prefix) {
			delete(m.runtimeInstances, key)
		}
	}
}

func (m *srvThirdPartyGatewayManager) StopAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients = map[string]*srvThirdPartyClientState{}
	m.runtimeInstances = map[string]srvThirdPartyRuntimeInstance{}
}

func (m *srvThirdPartyGatewayManager) cachedRuntimeInstanceID(p agentservice.Principal) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.runtimeInstances[principalRuntimeKey(p)]
	if !ok || strings.TrimSpace(rec.InstanceID) == "" || rec.LastIdentitySyncAt.IsZero() {
		return "", false
	}
	if time.Since(rec.LastIdentitySyncAt) > srvRuntimeIdentitySyncInterval {
		return "", false
	}
	return rec.InstanceID, true
}

func (m *srvThirdPartyGatewayManager) cacheRuntimeInstanceID(p agentservice.Principal, instanceID string) {
	if m == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimeInstances == nil {
		m.runtimeInstances = map[string]srvThirdPartyRuntimeInstance{}
	}
	m.runtimeInstances[principalRuntimeKey(p)] = srvThirdPartyRuntimeInstance{
		InstanceID:         instanceID,
		LastIdentitySyncAt: time.Now().UTC(),
	}
}

func (m *srvThirdPartyGatewayManager) clearCachedRuntimeInstanceID(p agentservice.Principal) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimeInstances, principalRuntimeKey(p))
}

func (m *srvThirdPartyGatewayManager) markIncoming(clientKey, eventID, maclawID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientKey)
	if existing := state.Seen[eventID]; existing != "" {
		return true, existing
	}
	state.Seen[eventID] = maclawID
	state.SeenOrder = append(state.SeenOrder, eventID)
	if len(state.SeenOrder) > srvThirdPartyMaxSeenEvents {
		drop := len(state.SeenOrder) - srvThirdPartyMaxSeenEvents
		for _, old := range state.SeenOrder[:drop] {
			delete(state.Seen, old)
		}
		state.SeenOrder = state.SeenOrder[drop:]
	}
	return false, maclawID
}

func (m *srvThirdPartyGatewayManager) enqueueError(p agentservice.Principal, req srvThirdPartyIncomingRequest, replyTo, text string) {
	m.enqueue(p, req, srvThirdPartyOutgoingMessage{ID: "mc_err_" + sanitizeThirdPartyGatewayID(replyTo), ReplyTo: replyTo, ClientID: req.ClientID, ConversationID: req.ConversationID, Type: "text", Text: text, CreatedAt: time.Now().UnixMilli(), Metadata: map[string]string{"error": "true"}})
}

func (m *srvThirdPartyGatewayManager) enqueue(p agentservice.Principal, req srvThirdPartyIncomingRequest, msg srvThirdPartyOutgoingMessage) {
	clientKey := thirdPartyClientKey(p, req.ClientID)
	m.mu.Lock()
	state := m.ensureClientLocked(clientKey)
	state.Next++
	msg.Cursor = strconv.FormatInt(state.Next, 10)
	state.Messages = append(state.Messages, msg)
	if len(state.Messages) > srvThirdPartyMaxStoredMsgs {
		state.Messages = state.Messages[len(state.Messages)-srvThirdPartyMaxStoredMsgs:]
		live := map[string]bool{}
		for _, stored := range state.Messages {
			live[stored.ID] = true
		}
		for id := range state.Acked {
			if !live[id] {
				delete(state.Acked, id)
			}
		}
	}
	notify := state.Notify
	select {
	case notify <- struct{}{}:
	default:
	}
	m.mu.Unlock()
}

func (m *srvThirdPartyGatewayManager) messagesAfter(clientKey string, cursor int64, limit int) ([]srvThirdPartyOutgoingMessage, int64, bool, <-chan struct{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientKey)
	out := []srvThirdPartyOutgoingMessage{}
	next := cursor
	for _, msg := range state.Messages {
		msgCursor, _ := strconv.ParseInt(msg.Cursor, 10, 64)
		if msgCursor <= cursor {
			continue
		}
		if state.Acked[msg.ID] != "" {
			next = msgCursor
			continue
		}
		out = append(out, msg)
		next = msgCursor
		if len(out) >= limit {
			break
		}
	}
	hasMore := false
	for _, msg := range state.Messages {
		msgCursor, _ := strconv.ParseInt(msg.Cursor, 10, 64)
		if msgCursor > next {
			if state.Acked[msg.ID] != "" {
				continue
			}
			hasMore = true
			break
		}
	}
	return out, next, hasMore, state.Notify
}

func (m *srvThirdPartyGatewayManager) ack(clientKey string, req srvThirdPartyAckRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientKey)
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "delivered"
	}
	known := map[string]bool{}
	for _, msg := range state.Messages {
		known[msg.ID] = true
	}
	for _, id := range req.MessageIDs {
		if id = strings.TrimSpace(id); id != "" {
			if !known[id] {
				continue
			}
			state.Acked[id] = status
		}
	}
}

func normalizeThirdPartyIncomingRequest(req *srvThirdPartyIncomingRequest) error {
	req.ClientID = normalizeThirdPartyGatewayID(req.ClientID)
	req.EventID = strings.TrimSpace(req.EventID)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.Message.Type = strings.ToLower(strings.TrimSpace(req.Message.Type))
	if req.Message.Type == "" {
		req.Message.Type = "text"
	}
	req.Message.Text = strings.TrimSpace(req.Message.Text)
	if req.ClientID == "" {
		return errors.New("clientId is required")
	}
	if err := validateThirdPartyGatewayID("clientId", req.ClientID); err != nil {
		return err
	}
	if req.EventID == "" {
		return errors.New("eventId is required")
	}
	if utf8.RuneCountInString(req.EventID) > srvThirdPartyMaxIDChars {
		return fmt.Errorf("eventId exceeds %d characters", srvThirdPartyMaxIDChars)
	}
	if req.ConversationID == "" {
		req.ConversationID = "default"
	}
	if utf8.RuneCountInString(req.ConversationID) > srvThirdPartyMaxIDChars {
		return fmt.Errorf("conversationId exceeds %d characters", srvThirdPartyMaxIDChars)
	}
	if req.Message.Type != "text" {
		return errors.New("only text messages are currently supported by MaClawSrv third-party gateway")
	}
	if req.Message.Text == "" {
		return errors.New("message.text is required")
	}
	if len(req.Message.Text) > srvThirdPartyMaxTextChars {
		return fmt.Errorf("message.text exceeds %d characters", srvThirdPartyMaxTextChars)
	}
	return nil
}

func decodeThirdPartyGatewayJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func writeThirdPartyGatewayError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"ok": false, "requestId": newThirdPartyGatewayRequestID(), "error": map[string]string{"code": code, "message": message}})
}

func thirdPartyBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[len("Bearer "):])
}

func newThirdPartyGatewayRequestID() string {
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func thirdPartyClientKey(p agentservice.Principal, clientID string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + normalizeThirdPartyGatewayID(clientID)
}

func normalizeThirdPartyGatewayID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_', r == '.', r == ':':
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), ".:-_")
}

func validateThirdPartyGatewayID(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(value) > srvThirdPartyMaxIDChars {
		return fmt.Errorf("%s exceeds %d characters", field, srvThirdPartyMaxIDChars)
	}
	return nil
}

func sanitizeThirdPartyGatewayID(value string) string {
	value = normalizeThirdPartyGatewayID(value)
	if value == "" {
		return "unknown"
	}
	return value
}
