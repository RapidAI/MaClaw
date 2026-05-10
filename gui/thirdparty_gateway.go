package main

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/i18n"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

const (
	thirdPartyProtocolVersion = "1.0"
	thirdPartyDefaultHost     = "127.0.0.1"
	thirdPartyDefaultPort     = 18777
	thirdPartyMaxBatchSize    = 20
	thirdPartyMaxLimit        = 100
	thirdPartyPollTimeoutSec  = 30
	thirdPartyMaxTimeoutSec   = 60
	thirdPartyMaxTextChars    = 12000
	thirdPartyMaxBodyBytes    = 16 * 1024 * 1024
	thirdPartyMaxMediaBytes   = 10 * 1024 * 1024
	thirdPartyHistoryLimit    = 1000
)

type thirdPartyGatewayManager struct {
	app *App

	mu           sync.Mutex
	server       *http.Server
	listener     net.Listener
	status       string
	lastBindKey  string
	localHandler *IMMessageHandler
	clients      map[string]*thirdPartyClientState
	notifyCh     chan struct{}
}

type thirdPartyClientState struct {
	NextSeq    int64
	Messages   []thirdPartyOutgoingMessage
	SeenEvents map[string]string
	Acked      map[string]string
}

type thirdPartyHandshakeRequest struct {
	ClientID        string         `json:"clientId"`
	ClientName      string         `json:"clientName"`
	ProtocolVersion string         `json:"protocolVersion"`
	Capabilities    map[string]any `json:"capabilities"`
}

type thirdPartyUserRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type thirdPartyMessagePayload struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	FileName    string `json:"fileName,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	Data        string `json:"data,omitempty"`
	URL         string `json:"url,omitempty"`
}

type thirdPartyIncomingRequest struct {
	ClientID       string                   `json:"clientId"`
	EventID        string                   `json:"eventId"`
	MessageID      string                   `json:"messageId"`
	ConversationID string                   `json:"conversationId"`
	User           thirdPartyUserRef        `json:"user"`
	Message        thirdPartyMessagePayload `json:"message"`
	CreatedAt      int64                    `json:"createdAt,omitempty"`
	Extra          map[string]any           `json:"extra,omitempty"`
}

type thirdPartyAckRequest struct {
	ClientID   string   `json:"clientId"`
	MessageIDs []string `json:"messageIds"`
	Status     string   `json:"status"`
}

type thirdPartyOutgoingMessage struct {
	ID               string         `json:"id"`
	Seq              int64          `json:"seq"`
	ConversationID   string         `json:"conversationId"`
	ReplyToMessageID string         `json:"replyToMessageId,omitempty"`
	Type             string         `json:"type"`
	Text             string         `json:"text,omitempty"`
	Caption          string         `json:"caption,omitempty"`
	FileName         string         `json:"fileName,omitempty"`
	ContentType      string         `json:"contentType,omitempty"`
	Data             string         `json:"data,omitempty"`
	Progress         bool           `json:"progress,omitempty"`
	Error            string         `json:"error,omitempty"`
	CreatedAt        int64          `json:"createdAt"`
	Extra            map[string]any `json:"extra,omitempty"`
}

func newThirdPartyGatewayManager(app *App) *thirdPartyGatewayManager {
	return &thirdPartyGatewayManager{
		app:      app,
		status:   gatewayConnectionStatusDisconnected.String(),
		clients:  make(map[string]*thirdPartyClientState),
		notifyCh: make(chan struct{}),
	}
}

func (m *thirdPartyGatewayManager) SyncFromConfig() {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}

	token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
	if host == "" {
		host = thirdPartyDefaultHost
	}
	port := cfg.ThirdPartyGatewayPort
	if port <= 0 {
		port = thirdPartyDefaultPort
	}
	bindKey := fmt.Sprintf("%s|%d|%s", host, port, token)

	m.mu.Lock()
	if !cfg.ThirdPartyGatewayEnabled || token == "" {
		server := m.server
		m.server = nil
		m.listener = nil
		m.lastBindKey = ""
		m.status = gatewayConnectionStatusDisconnected.String()
		lh := m.localHandler
		m.localHandler = nil
		m.mu.Unlock()
		if lh != nil {
			lh.memory.Stop()
		}
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = server.Shutdown(ctx)
			cancel()
		}
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformThirdParty)
		}
		m.emitStatusEvent()
		return
	}
	if m.server != nil && m.lastBindKey == bindKey {
		m.mu.Unlock()
		return
	}
	oldServer := m.server
	m.server = nil
	m.listener = nil
	m.status = gatewayConnectionStatusConnecting.String()
	m.mu.Unlock()

	if oldServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = oldServer.Shutdown(ctx)
		cancel()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/im-gateway/v1/health", m.handleHealth)
	mux.HandleFunc("/api/im-gateway/v1/handshake", m.handleHandshake)
	mux.HandleFunc("/api/im-gateway/v1/incoming", m.handleIncoming)
	mux.HandleFunc("/api/im-gateway/v1/outgoing", m.handleOutgoing)
	mux.HandleFunc("/api/im-gateway/v1/ack", m.handleAck)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[thirdparty-mgr] listen %s failed: %v", addr, err)
		m.mu.Lock()
		m.status = gatewayConnectionStatusError.String()
		m.lastBindKey = ""
		m.mu.Unlock()
		m.emitStatusEvent()
		return
	}

	server := &http.Server{Handler: mux}
	m.mu.Lock()
	m.server = server
	m.listener = ln
	m.lastBindKey = bindKey
	m.status = gatewayConnectionStatusConnected.String()
	m.mu.Unlock()
	m.emitStatusEvent()

	if !cfg.IsThirdPartyGatewayLocalMode() {
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayClaim(imGatewayPlatformThirdParty)
		}
	}

	go func() {
		log.Printf("[thirdparty-mgr] listening on http://%s", addr)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[thirdparty-mgr] server error: %v", err)
			m.mu.Lock()
			if m.server == server {
				m.server = nil
				m.listener = nil
				m.status = gatewayConnectionStatusError.String()
				m.lastBindKey = ""
			}
			m.mu.Unlock()
			m.emitStatusEvent()
		}
	}()
}

func (m *thirdPartyGatewayManager) Stop() {
	m.mu.Lock()
	server := m.server
	m.server = nil
	m.listener = nil
	m.status = gatewayConnectionStatusDisconnected.String()
	m.lastBindKey = ""
	lh := m.localHandler
	m.localHandler = nil
	m.mu.Unlock()
	if lh != nil {
		lh.memory.Stop()
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = server.Shutdown(ctx)
		cancel()
	}
	m.emitStatusEvent()
}

func (m *thirdPartyGatewayManager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *thirdPartyGatewayManager) emitStatusEvent() {
	m.app.emitEvent("thirdparty-gateway-status-changed", m.Status())
}

func (m *thirdPartyGatewayManager) resetLocalHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		m.localHandler.memory.Stop()
		m.localHandler = nil
	}
}

func (m *thirdPartyGatewayManager) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newGatewayRequestID(),
		"status":          m.Status(),
		"protocolVersion": thirdPartyProtocolVersion,
		"serverTime":      time.Now().UnixMilli(),
	})
}

func (m *thirdPartyGatewayManager) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req thirdPartyHandshakeRequest
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.ClientID = normalizeThirdPartyID(req.ClientID)
	if req.ClientID == "" {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "clientId is required")
		return
	}
	m.ensureClient(req.ClientID)
	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newGatewayRequestID(),
		"channelId":       thirdPartyPlatform(req.ClientID),
		"protocolVersion": thirdPartyProtocolVersion,
		"serverTime":      time.Now().UnixMilli(),
		"mode":            m.effectiveMode(),
		"capabilities": []string{
			"text",
			"image",
			"file",
			"voice",
			"long_poll",
			"ack",
			"idempotency",
		},
		"poll": map[string]int{
			"recommendedTimeoutSec": thirdPartyPollTimeoutSec,
			"maxTimeoutSec":         thirdPartyMaxTimeoutSec,
			"defaultLimit":          thirdPartyMaxBatchSize,
			"maxLimit":              thirdPartyMaxLimit,
		},
		"limits": map[string]int{
			"maxTextChars":  thirdPartyMaxTextChars,
			"maxBodyBytes":  thirdPartyMaxBodyBytes,
			"maxMediaBytes": thirdPartyMaxMediaBytes,
		},
		"delivery": map[string]string{
			"guarantee": "at_least_once_by_cursor",
			"dedupeKey": "message.id",
			"ack":       "delivery_receipt",
		},
		"pollTimeoutSec": thirdPartyPollTimeoutSec,
		"maxBatchSize":   thirdPartyMaxBatchSize,
		"features": map[string]bool{
			"text":        true,
			"image":       true,
			"file":        true,
			"voice":       true,
			"longPolling": true,
			"ack":         true,
		},
	})
}

func (m *thirdPartyGatewayManager) handleIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req thirdPartyIncomingRequest
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := normalizeIncomingRequest(&req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if !isSupportedThirdPartyMessageType(req.Message.Type) {
		writeGatewayError(w, http.StatusBadRequest, "unsupported_message_type", "unsupported message type")
		return
	}
	maclawID := fmt.Sprintf("mc_in_%d_%s", time.Now().UnixMilli(), sanitizeGatewayID(req.EventID))
	duplicate, storedID := m.markIncoming(req.ClientID, req.EventID, maclawID)
	maclawID = storedID
	if !duplicate {
		go m.processIncoming(req, maclawID)
	}
	writeGatewayJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"requestId":       newGatewayRequestID(),
		"accepted":        true,
		"duplicate":       duplicate,
		"maclawMessageId": maclawID,
	})
}

func (m *thirdPartyGatewayManager) handleOutgoing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	clientID := normalizeThirdPartyID(r.URL.Query().Get("clientId"))
	if clientID == "" {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "clientId is required")
		return
	}
	cursor, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("cursor")), 10, 64)
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = thirdPartyMaxBatchSize
	}
	if limit > thirdPartyMaxLimit {
		limit = thirdPartyMaxLimit
	}
	timeoutRaw := strings.TrimSpace(r.URL.Query().Get("timeout"))
	timeoutSec := thirdPartyPollTimeoutSec
	if timeoutRaw != "" {
		parsed, err := strconv.Atoi(timeoutRaw)
		if err != nil || parsed < 0 {
			writeGatewayError(w, http.StatusBadRequest, "bad_request", "timeout must be a non-negative integer")
			return
		}
		timeoutSec = parsed
	}
	if timeoutSec > thirdPartyMaxTimeoutSec {
		timeoutSec = thirdPartyMaxTimeoutSec
	}

	var deadline <-chan time.Time
	var timer *time.Timer
	if timeoutSec > 0 {
		timer = time.NewTimer(time.Duration(timeoutSec) * time.Second)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		msgs, next, hasMore := m.messagesAfter(clientID, cursor, limit)
		if len(msgs) > 0 || timeoutSec == 0 {
			writeGatewayJSON(w, http.StatusOK, map[string]any{
				"ok":         true,
				"requestId":  newGatewayRequestID(),
				"messages":   msgs,
				"nextCursor": strconv.FormatInt(next, 10),
				"hasMore":    hasMore,
			})
			return
		}
		m.mu.Lock()
		notify := m.notifyCh
		m.mu.Unlock()
		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			_, next, _ = m.messagesAfter(clientID, cursor, limit)
			writeGatewayJSON(w, http.StatusOK, map[string]any{
				"ok":         true,
				"requestId":  newGatewayRequestID(),
				"messages":   []thirdPartyOutgoingMessage{},
				"nextCursor": strconv.FormatInt(next, 10),
				"hasMore":    false,
			})
			return
		case <-notify:
		}
	}
}

func (m *thirdPartyGatewayManager) handleAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req thirdPartyAckRequest
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.ClientID = normalizeThirdPartyID(req.ClientID)
	if req.ClientID == "" {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "clientId is required")
		return
	}
	m.mu.Lock()
	state := m.ensureClientLocked(req.ClientID)
	status := normalizeThirdPartyGatewayAckStatus(req.Status).String()
	for _, id := range req.MessageIDs {
		id = strings.TrimSpace(id)
		if id != "" {
			state.Acked[id] = status
		}
	}
	m.mu.Unlock()
	writeGatewayJSON(w, http.StatusOK, map[string]any{"ok": true, "requestId": newGatewayRequestID()})
}

func (m *thirdPartyGatewayManager) authorize(r *http.Request) bool {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return false
	}
	token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	if token == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return false
	}
	provided := strings.TrimSpace(auth[len("Bearer "):])
	if len(provided) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func (m *thirdPartyGatewayManager) effectiveMode() string {
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return "local"
	}
	if cfg.ThirdPartyGatewayLocalMode == nil {
		if cfg.RemoteMachineID != "" {
			return "hub"
		}
		return "local"
	}
	if *cfg.ThirdPartyGatewayLocalMode {
		return "local"
	}
	return "hub"
}

func (m *thirdPartyGatewayManager) ensureClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureClientLocked(clientID)
}

func (m *thirdPartyGatewayManager) ensureClientLocked(clientID string) *thirdPartyClientState {
	if m.clients == nil {
		m.clients = make(map[string]*thirdPartyClientState)
	}
	state := m.clients[clientID]
	if state == nil {
		state = &thirdPartyClientState{
			NextSeq:    1,
			SeenEvents: make(map[string]string),
			Acked:      make(map[string]string),
		}
		m.clients[clientID] = state
	}
	return state
}

func (m *thirdPartyGatewayManager) markIncoming(clientID, eventID, maclawID string) (bool, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	if existingID, ok := state.SeenEvents[eventID]; ok {
		return true, existingID
	}
	state.SeenEvents[eventID] = maclawID
	return false, maclawID
}

func (m *thirdPartyGatewayManager) messagesAfter(clientID string, cursor int64, limit int) ([]thirdPartyOutgoingMessage, int64, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	nextCursor := cursor
	msgs := make([]thirdPartyOutgoingMessage, 0, limit)
	hasMore := false
	for _, msg := range state.Messages {
		if msg.Seq <= cursor {
			if msg.Seq > nextCursor {
				nextCursor = msg.Seq
			}
			continue
		}
		if len(msgs) >= limit {
			hasMore = true
			break
		}
		msgs = append(msgs, msg)
		if msg.Seq > nextCursor {
			nextCursor = msg.Seq
		}
	}
	return msgs, nextCursor, hasMore
}

func (m *thirdPartyGatewayManager) enqueue(clientID string, msg thirdPartyOutgoingMessage) thirdPartyOutgoingMessage {
	m.mu.Lock()
	state := m.ensureClientLocked(clientID)
	msg.Seq = state.NextSeq
	state.NextSeq++
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("mc_out_%d_%06d", time.Now().UnixMilli(), msg.Seq)
	}
	if msg.CreatedAt == 0 {
		msg.CreatedAt = time.Now().UnixMilli()
	}
	state.Messages = append(state.Messages, msg)
	if len(state.Messages) > thirdPartyHistoryLimit {
		state.Messages = state.Messages[len(state.Messages)-thirdPartyHistoryLimit:]
	}
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
	return msg
}

func (m *thirdPartyGatewayManager) processIncoming(req thirdPartyIncomingRequest, maclawID string) {
	if isPassthroughSlashText(req.Message.Text) {
		log.Printf("[thirdparty-mgr] routing passthrough command locally: client=%s conversation=%s", req.ClientID, req.ConversationID)
		m.handleLocalMessage(req, maclawID)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		m.enqueueError(req, req.MessageID, "bad_request", err.Error())
		return
	}
	if !cfg.IsThirdPartyGatewayLocalMode() {
		hubClient := m.app.hubClient()
		if hubClient != nil && hubClient.IsConnected() {
			payload := map[string]any{
				"platform_uid":    thirdPartySessionUserID(req.ClientID, req.ConversationID),
				"text":            req.Message.Text,
				"message_type":    req.Message.Type,
				"client_id":       req.ClientID,
				"conversation_id": req.ConversationID,
				"message_id":      req.MessageID,
				"event_id":        req.EventID,
				"user_id":         req.User.ID,
				"user_name":       req.User.Name,
			}
			if req.Extra != nil {
				payload["extra"] = req.Extra
			}
			if err := hubClient.SendIMGatewayMessage(imGatewayPlatformThirdParty, payload); err == nil {
				return
			}
			log.Printf("[thirdparty-mgr] forwardToHub error: %v, falling back to local", err)
		}
	}
	m.handleLocalMessage(req, maclawID)
}

func (m *thirdPartyGatewayManager) enqueueError(req thirdPartyIncomingRequest, replyTo, code, text string) {
	m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
		ConversationID:   req.ConversationID,
		ReplyToMessageID: replyTo,
		Type:             "error",
		Error:            code,
		Text:             text,
	})
}

func (m *thirdPartyGatewayManager) ensureLocalHandler() *IMMessageHandler {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.localHandler != nil {
		return m.localHandler
	}

	a := m.app
	a.ensureInteractionInfra()
	if a.memoryStore == nil {
		a.ensureMemoryStore()
	}
	if a.contextResolver == nil {
		a.ensureContextResolver()
	}
	if a.sessionPrecheck == nil {
		a.ensureSessionPrecheck()
	}

	h := NewIMMessageHandler(a, a.remoteSessions)
	if a.capabilityGapDetector == nil {
		a.ensureCapabilityGapDetector()
	}
	if a.capabilityGapDetector != nil {
		h.SetCapabilityGapDetector(a.capabilityGapDetector)
	}
	if a.toolDefGenerator != nil {
		h.SetToolDefGenerator(a.toolDefGenerator)
	}
	if a.toolRouter != nil {
		h.SetToolRouter(a.toolRouter)
	}
	if a.usageTracker != nil {
		h.SetUsageTracker(a.usageTracker)
	}
	if a.memoryStore != nil {
		h.SetMemoryStore(a.memoryStore)
	}
	h.SetTrajectoryRecorderFactory(a.buildTrajectoryRecorderFactory())
	if a.configManager != nil {
		h.SetConfigManager(a.configManager)
	}
	if a.templateManager != nil {
		h.SetTemplateManager(a.templateManager)
	}
	if a.scheduledTaskManager != nil {
		h.SetScheduledTaskManager(a.scheduledTaskManager)
	}
	if a.contextResolver != nil {
		h.SetContextResolver(a.contextResolver)
	}
	if a.sessionPrecheck != nil {
		h.SetSessionPrecheck(a.sessionPrecheck)
	}
	a.ensureStartupFeedback()
	if a.startupFeedback != nil {
		h.SetStartupFeedback(a.startupFeedback)
	}
	if a.securityFirewall == nil {
		a.ensureSecurityFirewall()
	}
	if a.securityFirewall != nil {
		h.SetSecurityFirewall(a.securityFirewall)
	}
	a.ensureConversationArchiver()
	if a.conversationArchiver != nil {
		h.memory.Archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[thirdparty-mgr] local IMMessageHandler created")
	return h
}

func (m *thirdPartyGatewayManager) handleLocalMessage(req thirdPartyIncomingRequest, maclawID string) {
	if resp, handled := m.app.TryHandlePassthroughSlashCommandWithSource(req.Message.Text, "thirdparty:"+req.ClientID+":"+req.ConversationID); handled {
		reply := resp.Text
		if reply == "" {
			reply = resp.Error
		}
		if reply == "" {
			reply = "(no output)"
		}
		m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
			ConversationID:   req.ConversationID,
			ReplyToMessageID: req.MessageID,
			Type:             "text",
			Text:             reply,
		})
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
			ConversationID:   req.ConversationID,
			ReplyToMessageID: req.MessageID,
			Type:             "text",
			Text:             i18n.T(i18n.MsgLLMNotConfigured, "zh"),
		})
		return
	}

	text := req.Message.Text
	var attachments []MessageAttachment
	if req.Message.Type != "text" {
		mediaData, err := decodeThirdPartyMedia(req.Message)
		if err != nil {
			m.enqueueError(req, req.MessageID, "bad_request", err.Error())
			return
		}
		if req.Message.Type == "image" {
			attachments = append(attachments, buildLocalImageAttachment(mediaData, req.Message.FileName, req.Message.ContentType))
		} else {
			mediaPath, err := saveMediaToTempDir("thirdparty", "tp_", safeFileToken(req.User.ID), req.Message.Type, mediaData, req.Message.FileName)
			if err != nil {
				m.enqueueError(req, req.MessageID, "bad_request", err.Error())
				return
			}
			prefix := "[鏀跺埌" + mediaLabel(req.Message.Type) + ": " + mediaPath + "]\n"
			text = prefix + text
		}
	}
	if text == "" && len(attachments) == 0 {
		return
	}

	handler := m.ensureLocalHandler()
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if progressText == "" || progressText == imHeartbeatMsg {
			return
		}
		now := time.Now()
		if now.Sub(lastProgress) < 2*time.Second {
			return
		}
		stripped := textutil.StripMarkdown(progressText)
		if stripped == lastProgressText {
			return
		}
		lastProgress = now
		lastProgressText = stripped
		m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
			ConversationID:   req.ConversationID,
			ReplyToMessageID: req.MessageID,
			Type:             "text",
			Text:             i18n.T(i18n.MsgProgressPrefix, "zh") + stripped,
			Progress:         true,
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      thirdPartySessionUserID(req.ClientID, req.ConversationID),
		Platform:    thirdPartyPlatform(req.ClientID),
		Text:        text,
		Lang:        "zh",
		Attachments: attachments,
	}, onProgress)
	if resp == nil || resp.Deferred {
		return
	}
	m.enqueueAgentResponse(req.ClientID, req.ConversationID, req.MessageID, resp)
}

func (m *thirdPartyGatewayManager) enqueueAgentResponse(clientID, conversationID, replyTo string, resp *IMAgentResponse) {
	if resp.Text != "" {
		text := textutil.StripMarkdown(resp.Text)
		if len(resp.Actions) > 0 {
			text = strings.TrimSpace(text)
			if text != "" {
				text += "\n\n"
			}
			text += "Please reply with an option:"
			for i, action := range resp.Actions {
				text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
			}
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text})
	} else if len(resp.Actions) > 0 {
		text := "Please reply with an option:"
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text})
	}
	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "error", Error: "agent_error", Text: textutil.StripMarkdown(resp.Error)})
	}
	if resp.ImageKey != "" {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image", ContentType: "image/png", FileName: "image.png", Data: resp.ImageKey})
	}
	if resp.FileData != "" {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: resp.FileMimeType, FileName: resp.FileName, Data: resp.FileData})
	}
	if resp.VoiceData != "" {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: resp.VoiceMimeType, FileName: resp.VoiceFileName, Data: resp.VoiceData})
	}
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			log.Printf("[thirdparty-mgr] read local file %s error: %v", p, err)
			continue
		}
		name := filepath.Base(p)
		ct := mime.TypeByExtension(filepath.Ext(name))
		if ct == "" {
			ct = "application/octet-stream"
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: ct, FileName: name, Data: base64.StdEncoding.EncodeToString(data)})
	}
}

func (m *thirdPartyGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	clientID, conversationID := parseThirdPartyReplyTarget(reply)
	if clientID == "" {
		log.Printf("[thirdparty-mgr] hub reply missing client id")
		return
	}
	msg := thirdPartyOutgoingMessage{
		ConversationID: conversationID,
		Type:           reply.ReplyType.String(),
		Text:           reply.Text,
		Caption:        reply.Caption,
		FileName:       reply.FileName,
		ContentType:    reply.MimeType,
	}
	replyKind := normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String())
	switch {
	case replyKind == thirdPartyGatewayMessageImage:
		msg.Data = reply.ImageData
		if msg.ContentType == "" {
			msg.ContentType = "image/png"
		}
	case replyKind.IsMediaFile():
		msg.Data = reply.FileData
	default:
		if msg.Type == "" {
			msg.Type = thirdPartyGatewayMessageText.String()
		}
	}
	m.enqueue(clientID, msg)
}

func parseThirdPartyReplyTarget(reply GatewayReplyPayload) (string, string) {
	if reply.Extra != nil {
		clientID, _ := reply.Extra["client_id"].(string)
		conversationID, _ := reply.Extra["conversation_id"].(string)
		if clientID != "" {
			return normalizeThirdPartyID(clientID), strings.TrimSpace(conversationID)
		}
	}
	uid := strings.TrimSpace(reply.PlatformUID)
	if strings.HasPrefix(uid, "thirdparty:") {
		parts := strings.SplitN(uid, ":", 3)
		if len(parts) == 3 {
			return normalizeThirdPartyID(parts[1]), parts[2]
		}
	}
	return "", ""
}

func decodeThirdPartyMedia(msg thirdPartyMessagePayload) ([]byte, error) {
	if msg.Data == "" {
		return nil, fmt.Errorf("message.data is required for %s", msg.Type)
	}
	data, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 media data: %w", err)
	}
	if len(data) > thirdPartyMaxMediaBytes {
		return nil, fmt.Errorf("media exceeds %d bytes", thirdPartyMaxMediaBytes)
	}
	return data, nil
}

func normalizeIncomingRequest(req *thirdPartyIncomingRequest) error {
	req.ClientID = normalizeThirdPartyID(req.ClientID)
	req.EventID = strings.TrimSpace(req.EventID)
	req.MessageID = strings.TrimSpace(req.MessageID)
	req.ConversationID = strings.TrimSpace(req.ConversationID)
	req.User.ID = strings.TrimSpace(req.User.ID)
	req.User.Name = strings.TrimSpace(req.User.Name)
	req.Message.Type = strings.ToLower(strings.TrimSpace(req.Message.Type))
	if req.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}
	if req.EventID == "" {
		return fmt.Errorf("eventId is required")
	}
	if req.MessageID == "" {
		return fmt.Errorf("messageId is required")
	}
	if req.ConversationID == "" {
		return fmt.Errorf("conversationId is required")
	}
	if req.User.ID == "" {
		return fmt.Errorf("user.id is required")
	}
	if req.Message.Type == "" {
		req.Message.Type = thirdPartyGatewayMessageText.String()
	}
	if normalizeThirdPartyGatewayMessageKind(req.Message.Type) == thirdPartyGatewayMessageText && strings.TrimSpace(req.Message.Text) == "" {
		return fmt.Errorf("message.text is required for text messages")
	}
	if req.Message.Text != "" && len([]rune(req.Message.Text)) > thirdPartyMaxTextChars {
		return fmt.Errorf("message.text exceeds %d characters", thirdPartyMaxTextChars)
	}
	return nil
}

func isSupportedThirdPartyMessageType(t string) bool {
	return normalizeThirdPartyGatewayMessageKind(t).IsSupported()
}

func decodeGatewayJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, thirdPartyMaxBodyBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeGatewayJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeGatewayError(w http.ResponseWriter, status int, code, message string) {
	requestID := newGatewayRequestID()
	writeGatewayJSON(w, status, map[string]any{
		"ok":        false,
		"code":      code,
		"message":   message,
		"requestId": requestID,
		"error": map[string]string{
			"code":      code,
			"message":   message,
			"requestId": requestID,
		},
	})
}

func newGatewayRequestID() string {
	return fmt.Sprintf("gw_%d", time.Now().UnixNano())
}

func normalizeThirdPartyID(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func sanitizeGatewayID(s string) string {
	s = safeFileToken(s)
	if s == "" {
		return "event"
	}
	return s
}

func safeFileToken(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func thirdPartyPlatform(clientID string) string {
	return "thirdparty:" + normalizeThirdPartyID(clientID)
}

func thirdPartySessionUserID(clientID, conversationID string) string {
	return "thirdparty:" + normalizeThirdPartyID(clientID) + ":" + strings.TrimSpace(conversationID)
}

// App integration.
func (a *App) ensureThirdPartyGateway() {
	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	if !cfg.ThirdPartyGatewayEnabled || strings.TrimSpace(cfg.ThirdPartyGatewayToken) == "" {
		if a.thirdPartyGateway != nil {
			a.thirdPartyGateway.SyncFromConfig()
		}
		return
	}
	if a.thirdPartyGateway == nil {
		a.thirdPartyGateway = newThirdPartyGatewayManager(a)
	}
	a.thirdPartyGateway.SyncFromConfig()
}

func (a *App) GetThirdPartyGatewayStatus() string {
	if a.thirdPartyGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.thirdPartyGateway.Status()
}

func (a *App) RestartThirdPartyGateway() string {
	a.ensureThirdPartyGateway()
	if a.thirdPartyGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.thirdPartyGateway.Status()
}

func (a *App) StopThirdPartyGateway() {
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.Stop()
	}
}

func (a *App) GetThirdPartyGatewayLocalMode() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true
	}
	return cfg.IsThirdPartyGatewayLocalMode()
}

func (a *App) SetThirdPartyGatewayLocalMode(enabled bool) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("please register to Hub before enabling Hub mode")
	}
	cfg.SetThirdPartyGatewayLocal(enabled)
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.resetLocalHandler()
	}
	if !enabled {
		if hubClient := a.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayClaim(imGatewayPlatformThirdParty)
		}
	}
	return nil
}
