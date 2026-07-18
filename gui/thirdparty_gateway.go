package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/textutil"
)

const (
	thirdPartyProtocolVersion = coreim.ThirdPartyProtocolVersion
	thirdPartyDefaultHost     = "127.0.0.1"
	thirdPartyDefaultPort     = 18777
	thirdPartyMaxBatchSize    = coreim.ThirdPartyMaxBatchSize
	thirdPartyMaxLimit        = coreim.ThirdPartyMaxPollLimit
	thirdPartyPollTimeoutSec  = coreim.ThirdPartyPollTimeoutSec
	thirdPartyMaxTimeoutSec   = coreim.ThirdPartyMaxTimeoutSec
	thirdPartyMaxTextChars    = coreim.ThirdPartyMaxTextChars
	thirdPartyMaxBodyBytes    = coreim.ThirdPartyMaxBodyBytes
	thirdPartyMaxMediaBytes   = coreim.ThirdPartyMaxMediaBytes
	thirdPartyMaxAckIDs       = coreim.ThirdPartyMaxAckIDs
	thirdPartyMaxMediaObjects = 500
	thirdPartyHistoryLimit    = 1000
)

type thirdPartyGatewayManager struct {
	app *App

	mu           sync.Mutex
	server       *http.Server
	listener     net.Listener
	status       gatewayConnectionStatus
	lastBindKey  string
	localHandler *IMMessageHandler
	clients      map[string]*thirdPartyClientState
	notifyCh     chan struct{}
	media        map[string]*thirdPartyMediaObject
}

type thirdPartyClientState struct {
	NextSeq    int64
	Messages   []thirdPartyOutgoingMessage
	SeenEvents map[string]string
	Acked      map[string]string
}

type thirdPartyMediaObject struct {
	ClientID       string
	ID             string
	Token          string
	Type           string
	FileName       string
	MimeType       string
	SizeBytes      int64
	DurationMs     int64
	Data           []byte
	Uploaded       bool
	CreatedAt      time.Time
	LastAccessedAt time.Time
}

type thirdPartyHandshakeRequest = coreim.ThirdPartyHandshakeRequest
type thirdPartyUserRef = coreim.ThirdPartyUserRef
type thirdPartyMessagePayload = coreim.ThirdPartyMessagePayload
type thirdPartyIncomingRequest = coreim.ThirdPartyIncomingRequest
type thirdPartyAckRequest = coreim.ThirdPartyAckRequest
type thirdPartyToolResultRequest = coreim.ThirdPartyToolResultRequest
type thirdPartyOutgoingMessage = coreim.ThirdPartyOutgoingMessage

func newThirdPartyGatewayManager(app *App) *thirdPartyGatewayManager {
	return &thirdPartyGatewayManager{
		app:      app,
		status:   gatewayConnectionStatusDisconnected,
		clients:  make(map[string]*thirdPartyClientState),
		notifyCh: make(chan struct{}),
		media:    make(map[string]*thirdPartyMediaObject),
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
		m.status = gatewayConnectionStatusDisconnected
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
	m.status = gatewayConnectionStatusConnecting
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
	mux.HandleFunc("/api/im-gateway/v1/tool-result", m.handleToolResult)
	mux.HandleFunc("/api/im-gateway/v1/media/upload-url", m.handleMediaUploadURL)
	mux.HandleFunc("/api/im-gateway/v1/media/", m.handleMedia)

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("[thirdparty-mgr] listen %s failed: %v", addr, err)
		m.mu.Lock()
		m.status = gatewayConnectionStatusError
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
	m.status = gatewayConnectionStatusConnected
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
				m.status = gatewayConnectionStatusError
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
	m.status = gatewayConnectionStatusDisconnected
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
	return m.status.String()
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
	writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyGatewayHealthResponse(newGatewayRequestID(), time.Now().UnixMilli()))
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
	if err := coreim.NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	m.ensureClient(req.ClientID)
	writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{
		RequestID:      newGatewayRequestID(),
		ChannelID:      thirdPartyPlatform(req.ClientID),
		ServerTime:     time.Now().UnixMilli(),
		MaxBodyBytes:   thirdPartyMaxBodyBytes,
		MaxMediaBytes:  thirdPartyMaxMediaBytes,
		PollTimeoutSec: thirdPartyPollTimeoutSec,
		MaxTimeoutSec:  thirdPartyMaxTimeoutSec,
		MaxBatchSize:   thirdPartyMaxBatchSize,
		MaxPollLimit:   thirdPartyMaxLimit,
	}))
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
	if err := m.validateIncomingMediaReferences(&req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	maclawID := fmt.Sprintf("mc_in_%d_%s", time.Now().UnixMilli(), sanitizeGatewayID(req.EventID))
	duplicate, storedID := m.markIncoming(req.ClientID, req.EventID, maclawID)
	maclawID = storedID
	if !duplicate {
		go m.processIncoming(req, maclawID)
	}
	writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyIncomingAcceptedResponse(newGatewayRequestID(), maclawID, duplicate))
}

func (m *thirdPartyGatewayManager) handleMediaUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req coreim.ThirdPartyMediaPrepareRequest
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := coreim.NormalizeThirdPartyMediaPrepareRequest(&req, thirdPartyMaxMediaBytes); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := m.prepareMedia(req, thirdPartyGatewayRequestBaseURL(r))
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeGatewayJSON(w, http.StatusOK, resp)
}

func (m *thirdPartyGatewayManager) handleMedia(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/im-gateway/v1/media/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) == 2 && parts[1] == "upload" {
		if r.Method != http.MethodPut {
			writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "PUT required")
			return
		}
		if err := m.storeMediaUpload(r, parts[0]); err != nil {
			writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyMediaUploadCompleteResponse(newGatewayRequestID(), parts[0]))
		return
	}
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodGet {
			writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
			return
		}
		media, err := m.mediaForDownload(r, parts[0])
		if err != nil {
			writeGatewayError(w, http.StatusNotFound, "not_found", err.Error())
			return
		}
		if media.MimeType != "" {
			w.Header().Set("Content-Type", media.MimeType)
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", media.FileName))
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(media.Data)), 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(media.Data)
		return
	}
	writeGatewayError(w, http.StatusNotFound, "not_found", "media not found")
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
	poll, err := coreim.ParseThirdPartyPollQuery(r.URL.Query(), coreim.ThirdPartyGatewayConfig{
		PollTimeoutSec: thirdPartyPollTimeoutSec,
		MaxTimeoutSec:  thirdPartyMaxTimeoutSec,
		MaxBatchSize:   thirdPartyMaxBatchSize,
		MaxPollLimit:   thirdPartyMaxLimit,
	})
	if err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	var deadline <-chan time.Time
	var timer *time.Timer
	if poll.TimeoutSec > 0 {
		timer = time.NewTimer(time.Duration(poll.TimeoutSec) * time.Second)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		msgs, next, hasMore := m.messagesAfter(poll.ClientID, poll.Cursor, poll.Limit)
		if len(msgs) > 0 || poll.TimeoutSec == 0 {
			writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyOutgoingPollResponse(newGatewayRequestID(), msgs, next, hasMore))
			return
		}
		m.mu.Lock()
		notify := m.notifyCh
		m.mu.Unlock()
		select {
		case <-r.Context().Done():
			return
		case <-deadline:
			_, next, _ = m.messagesAfter(poll.ClientID, poll.Cursor, poll.Limit)
			writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyOutgoingPollResponse(newGatewayRequestID(), nil, next, false))
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
	if err := coreim.NormalizeThirdPartyAckRequest(&req, thirdPartyMaxAckIDs); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	m.ack(req.ClientID, req)
	writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyGatewayOKResponse(newGatewayRequestID()))
}

func (m *thirdPartyGatewayManager) ack(clientID string, req thirdPartyAckRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(req.ClientID)
	status := coreim.NormalizeThirdPartyAckStatus(req.Status)
	known := map[string]bool{}
	for _, msg := range state.Messages {
		known[msg.ID] = true
	}
	for _, id := range req.MessageIDs {
		id = strings.TrimSpace(id)
		if id != "" && known[id] {
			state.Acked[id] = status
		}
	}
}

func (m *thirdPartyGatewayManager) handleToolResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.authorize(r) {
		writeGatewayError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req thirdPartyToolResultRequest
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := coreim.NormalizeThirdPartyToolResultRequest(&req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	maclawID := fmt.Sprintf("mc_tool_%d_%s", time.Now().UnixMilli(), sanitizeGatewayID(firstNonEmpty(req.ToolCallID, req.ToolPlanID)))
	incoming := thirdPartyIncomingRequest{
		ClientID:       req.ClientID,
		EventID:        coreim.ThirdPartyToolResultEventID(req),
		MessageID:      maclawID,
		ConversationID: firstNonEmpty(req.ConversationID, "default"),
		User:           thirdPartyUserRef{ID: "client-tool:" + req.ClientID, Name: "Client Tool"},
		Message:        thirdPartyMessagePayload{Type: "text", Text: coreim.ThirdPartyToolResultContent(req)},
		Metadata: map[string]string{
			"message_kind": "tool_result",
			"tool_call_id": req.ToolCallID,
			"tool_plan_id": req.ToolPlanID,
			"tool_step_id": req.StepID,
			"tool_status":  req.Status,
		},
		CreatedAt: time.Now().UnixMilli(),
	}
	duplicate, storedID := m.markIncoming(req.ClientID, incoming.EventID, maclawID)
	maclawID = storedID
	if !duplicate {
		go m.processIncoming(incoming, maclawID)
	}
	writeGatewayJSON(w, http.StatusOK, coreim.NewThirdPartyIncomingAcceptedResponse(newGatewayRequestID(), maclawID, duplicate))
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
		if state.Acked[msg.ID] != "" {
			if msg.Seq > nextCursor {
				nextCursor = msg.Seq
			}
			continue
		}
		if len(msgs) >= limit {
			for _, later := range state.Messages {
				if later.Seq > nextCursor && state.Acked[later.ID] == "" {
					hasMore = true
					break
				}
			}
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
		Metadata:         map[string]string{"acp_turn": "final"},
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
			Type:             thirdPartyGatewayMessageText.String(),
			Text:             reply,
			Metadata:         map[string]string{"acp_turn": "final"},
		})
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
			ConversationID:   req.ConversationID,
			ReplyToMessageID: req.MessageID,
			Type:             "text",
			Text:             i18n.T(i18n.MsgLLMNotConfigured, "zh"),
			Metadata:         map[string]string{"acp_turn": "final"},
		})
		return
	}

	text := req.Message.Text
	var attachments []MessageAttachment
	messageKind := normalizeThirdPartyGatewayMessageKind(req.Message.Type)
	if messageKind != thirdPartyGatewayMessageText {
		mediaData, mediaName, mediaMime, err := m.decodeThirdPartyMedia(req.Message)
		if err != nil {
			m.enqueueError(req, req.MessageID, "bad_request", err.Error())
			return
		}
		if messageKind == thirdPartyGatewayMessageImage {
			attachments = append(attachments, buildLocalImageAttachment(mediaData, mediaName, mediaMime))
		} else {
			mediaPath, err := saveMediaToTempDir("thirdparty", "tp_", safeFileToken(req.User.ID), messageKind.String(), mediaData, mediaName)
			if err != nil {
				m.enqueueError(req, req.MessageID, "bad_request", err.Error())
				return
			}
			prefix := "[收到" + mediaLabel(messageKind.String()) + ": " + mediaPath + "]\n"
			text = prefix + text
		}
	}
	if text == "" && len(attachments) == 0 {
		return
	}

	handler := m.ensureLocalHandler()
	progressFilter := newIMProgressVisibilityFilter(m.app)
	var lastProgress time.Time
	var lastProgressText string
	onProgress := func(progressText string) {
		if !progressFilter.ShouldSendProgress(progressText) {
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
			Text:             i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + stripped,
			Progress:         true,
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:      thirdPartySessionUserID(req.ClientID, req.ConversationID),
		Platform:    thirdPartyPlatform(req.ClientID),
		Text:        text,
		Lang:        appUILang(m.app),
		Attachments: attachments,
	}, onProgress)
	if resp == nil || resp.Deferred {
		return
	}
	m.enqueueAgentResponse(req.ClientID, req.ConversationID, req.MessageID, resp)
}

func (m *thirdPartyGatewayManager) enqueueAgentResponse(clientID, conversationID, replyTo string, resp *IMAgentResponse) {
	// acp_turn=final marks terminal messages for ACP bridge turn completion.
	finalMeta := map[string]string{"acp_turn": "final"}
	enqueued := false
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
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: finalMeta})
		enqueued = true
	} else if len(resp.Actions) > 0 {
		text := "Please reply with an option:"
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: finalMeta})
		enqueued = true
	}
	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "error", Error: "agent_error", Text: textutil.StripMarkdown(resp.Error), Metadata: finalMeta})
		enqueued = true
	}
	if resp.ImageKey != "" {
		meta := map[string]string{}
		if !enqueued {
			meta["acp_turn"] = "final"
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image", ContentType: "image/png", FileName: "image.png", Data: resp.ImageKey, Metadata: meta})
	}
	if resp.FileData != "" {
		meta := map[string]string{}
		if !enqueued {
			meta["acp_turn"] = "final"
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: resp.FileMimeType, FileName: resp.FileName, Data: resp.FileData, Metadata: meta})
	}
	if resp.VoiceData != "" {
		meta := map[string]string{}
		if !enqueued {
			meta["acp_turn"] = "final"
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: resp.VoiceMimeType, FileName: resp.VoiceFileName, Data: resp.VoiceData, Metadata: meta})
	}
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for i, p := range paths {
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
		meta := map[string]string{}
		if !enqueued && i == len(paths)-1 {
			meta["acp_turn"] = "final"
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: ct, FileName: name, Data: base64.StdEncoding.EncodeToString(data), Metadata: meta})
	}
	// Ensure ACP bridge always sees a terminal marker even for empty agent results.
	if !enqueued {
		m.enqueue(clientID, thirdPartyOutgoingMessage{
			ConversationID:   conversationID,
			ReplyToMessageID: replyTo,
			Type:             "text",
			Text:             "(no output)",
			Metadata:         finalMeta,
		})
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
		// Deferred hub/async replies complete an ACP bridge turn.
		Metadata: map[string]string{"acp_turn": "final"},
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

func decodeThirdPartyMedia(msg thirdPartyMessagePayload) ([]byte, string, string, error) {
	ref := coreim.ThirdPartyMediaReference{
		Type:        msg.Type,
		FileName:    msg.FileName,
		ContentType: msg.ContentType,
		MimeType:    msg.MimeType,
		Data:        msg.Data,
		URL:         msg.URL,
		SizeBytes:   msg.SizeBytes,
		DurationMs:  msg.DurationMs,
	}
	if len(msg.Attachments) > 0 {
		ref = msg.Attachments[0]
	}
	if ref.MimeType == "" {
		ref.MimeType = ref.ContentType
	}
	if ref.Data != "" {
		data, err := base64.StdEncoding.DecodeString(ref.Data)
		if err != nil {
			return nil, "", "", fmt.Errorf("invalid base64 media data: %w", err)
		}
		if len(data) > thirdPartyMaxMediaBytes {
			return nil, "", "", fmt.Errorf("media exceeds %d bytes", thirdPartyMaxMediaBytes)
		}
		return data, ref.FileName, ref.MimeType, nil
	}
	return nil, "", "", fmt.Errorf("message.data or server media id/url is required for %s", msg.Type)
}

func (m *thirdPartyGatewayManager) decodeThirdPartyMedia(msg thirdPartyMessagePayload) ([]byte, string, string, error) {
	data, name, mimeType, err := decodeThirdPartyMedia(msg)
	if err == nil {
		return data, name, mimeType, nil
	}
	ref := coreim.ThirdPartyMediaReference{
		ID:          msg.ID,
		Type:        msg.Type,
		FileName:    msg.FileName,
		ContentType: msg.ContentType,
		MimeType:    msg.MimeType,
		Data:        msg.Data,
		URL:         msg.URL,
		SizeBytes:   msg.SizeBytes,
		DurationMs:  msg.DurationMs,
	}
	if len(msg.Attachments) > 0 {
		ref = msg.Attachments[0]
	}
	id := strings.TrimSpace(ref.ID)
	var mediaReq *http.Request
	if id == "" && strings.TrimSpace(ref.URL) != "" {
		var parseErr error
		id, mediaReq, parseErr = thirdPartyServerMediaRequestFromURL(ref.URL)
		if parseErr != nil {
			return nil, "", "", parseErr
		}
	}
	if id == "" {
		return nil, "", "", err
	}
	if mediaReq != nil {
		media, err := m.mediaForDownload(mediaReq, id)
		if err != nil {
			return nil, "", "", err
		}
		if ref.FileName == "" {
			ref.FileName = media.FileName
		}
		if ref.MimeType == "" {
			ref.MimeType = media.MimeType
		}
		return append([]byte(nil), media.Data...), ref.FileName, ref.MimeType, nil
	}
	m.mu.Lock()
	media := m.media[id]
	var mediaData []byte
	var mediaFileName string
	var mediaMimeType string
	var uploaded bool
	if media != nil {
		media.LastAccessedAt = time.Now().UTC()
		mediaData = append([]byte(nil), media.Data...)
		mediaFileName = media.FileName
		mediaMimeType = media.MimeType
		uploaded = media.Uploaded
	}
	m.mu.Unlock()
	if media == nil || !uploaded {
		return nil, "", "", fmt.Errorf("media %s not found", id)
	}
	if ref.FileName == "" {
		ref.FileName = mediaFileName
	}
	if ref.MimeType == "" {
		ref.MimeType = mediaMimeType
	}
	return mediaData, ref.FileName, ref.MimeType, nil
}

func (m *thirdPartyGatewayManager) validateIncomingMediaReferences(req *thirdPartyIncomingRequest) error {
	for i := range req.Message.Attachments {
		ref := &req.Message.Attachments[i]
		if strings.TrimSpace(ref.URL) != "" {
			id, mediaReq, err := thirdPartyServerMediaRequestFromURL(ref.URL)
			if err != nil {
				return fmt.Errorf("message.attachments[%d].url: %w", i, err)
			}
			media, err := m.mediaForDownload(mediaReq, id)
			if err != nil {
				return fmt.Errorf("message.attachments[%d].url media not found", i)
			}
			ref.ID = id
			if ref.FileName == "" {
				ref.FileName = media.FileName
			}
			if ref.MimeType == "" {
				ref.MimeType = media.MimeType
			}
			if ref.SizeBytes == 0 {
				ref.SizeBytes = media.SizeBytes
			}
		}
		if strings.TrimSpace(ref.Data) != "" {
			continue
		}
		if strings.TrimSpace(ref.ID) != "" {
			media, ok := m.mediaObject(ref.ID)
			if !ok {
				return fmt.Errorf("message.attachments[%d].id media not found", i)
			}
			if ref.FileName == "" {
				ref.FileName = media.FileName
			}
			if ref.MimeType == "" {
				ref.MimeType = media.MimeType
			}
			if ref.SizeBytes == 0 {
				ref.SizeBytes = media.SizeBytes
			}
		}
	}
	return nil
}

func (m *thirdPartyGatewayManager) mediaObject(id string) (thirdPartyMediaObject, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	media := m.media[strings.TrimSpace(id)]
	if media == nil || !media.Uploaded {
		return thirdPartyMediaObject{}, false
	}
	media.LastAccessedAt = time.Now().UTC()
	return *media, true
}

func (m *thirdPartyGatewayManager) prepareMedia(req coreim.ThirdPartyMediaPrepareRequest, baseURL string) (*coreim.ThirdPartyMediaPrepareResponse, error) {
	if err := coreim.NormalizeThirdPartyMediaPrepareRequest(&req, thirdPartyMaxMediaBytes); err != nil {
		return nil, err
	}
	id, err := randomThirdPartyMediaToken()
	if err != nil {
		return nil, err
	}
	token, err := randomThirdPartyMediaToken()
	if err != nil {
		return nil, err
	}
	fileName := coreim.SafeThirdPartyFileName(req.FileName)
	mimeType := strings.TrimSpace(req.MimeType)
	downloadURL := fmt.Sprintf("%s/media/%s?mediaToken=%s", strings.TrimRight(baseURL, "/"), id, token)
	uploadURL := fmt.Sprintf("%s/media/%s/upload?mediaToken=%s", strings.TrimRight(baseURL, "/"), id, token)
	obj := &thirdPartyMediaObject{
		ClientID:       req.ClientID,
		ID:             id,
		Token:          token,
		Type:           req.Type,
		FileName:       fileName,
		MimeType:       mimeType,
		SizeBytes:      req.SizeBytes,
		DurationMs:     req.DurationMs,
		CreatedAt:      time.Now().UTC(),
		LastAccessedAt: time.Now().UTC(),
	}
	m.mu.Lock()
	m.pruneMediaLocked(time.Now().UTC())
	m.media[id] = obj
	m.mu.Unlock()
	ref := coreim.ThirdPartyMediaReference{ID: id, Type: req.Type, FileName: fileName, MimeType: mimeType, URL: downloadURL, SizeBytes: req.SizeBytes, DurationMs: req.DurationMs}
	return &coreim.ThirdPartyMediaPrepareResponse{
		OK:        true,
		RequestID: newGatewayRequestID(),
		Media:     ref,
		Upload:    coreim.ThirdPartyMediaUpload{Method: http.MethodPut, URL: uploadURL, ContentType: mimeType, MaxBytes: thirdPartyMaxMediaBytes},
		Download:  coreim.ThirdPartyMediaDownload{URL: downloadURL},
		ExpiresAt: time.Now().Add(24 * time.Hour).UnixMilli(),
	}, nil
}

func (m *thirdPartyGatewayManager) storeMediaUpload(r *http.Request, id string) error {
	m.mu.Lock()
	media := m.media[id]
	m.mu.Unlock()
	if media == nil {
		return fmt.Errorf("media not found")
	}
	if !coreim.ThirdPartyMediaTokenOK(r, media.Token) {
		return fmt.Errorf("invalid media token")
	}
	if r.ContentLength > thirdPartyMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", thirdPartyMaxMediaBytes)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, thirdPartyMaxMediaBytes+1))
	if err != nil {
		return err
	}
	if len(data) > thirdPartyMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", thirdPartyMaxMediaBytes)
	}
	if media.SizeBytes > 0 && int64(len(data)) != media.SizeBytes {
		return fmt.Errorf("media size mismatch: got %d bytes, want %d", len(data), media.SizeBytes)
	}
	m.mu.Lock()
	media.Data = data
	media.Uploaded = true
	media.SizeBytes = int64(len(data))
	if media.MimeType == "" {
		media.MimeType = strings.TrimSpace(r.Header.Get("Content-Type"))
	}
	media.LastAccessedAt = time.Now().UTC()
	m.mu.Unlock()
	return nil
}

func (m *thirdPartyGatewayManager) mediaForDownload(r *http.Request, id string) (*thirdPartyMediaObject, error) {
	m.mu.Lock()
	media := m.media[id]
	m.mu.Unlock()
	if media == nil || !media.Uploaded {
		return nil, fmt.Errorf("media not found")
	}
	if !coreim.ThirdPartyMediaTokenOK(r, media.Token) {
		return nil, fmt.Errorf("media not found")
	}
	m.mu.Lock()
	media.LastAccessedAt = time.Now().UTC()
	out := *media
	m.mu.Unlock()
	return &out, nil
}

func (m *thirdPartyGatewayManager) pruneMediaLocked(now time.Time) {
	if len(m.media) == 0 {
		return
	}
	cutoff := now.Add(-24 * time.Hour)
	for id, media := range m.media {
		if media.LastAccessedAt.Before(cutoff) {
			delete(m.media, id)
		}
	}
	for len(m.media) > thirdPartyMaxMediaObjects {
		var oldestID string
		var oldest time.Time
		for id, media := range m.media {
			if oldestID == "" || media.LastAccessedAt.Before(oldest) {
				oldestID = id
				oldest = media.LastAccessedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(m.media, oldestID)
	}
}

func thirdPartyServerMediaRequestFromURL(rawURL string) (string, *http.Request, error) {
	id, req, err := coreim.ThirdPartyServerMediaRequestFromURL(rawURL)
	if err != nil {
		return "", nil, fmt.Errorf("message.url %s", err.Error())
	}
	return id, req, nil
}

func randomThirdPartyMediaToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func thirdPartyGatewayRequestBaseURL(r *http.Request) string {
	scheme := thirdPartyForwardedScheme(r.Header.Get("X-Forwarded-Proto"), r.TLS != nil)
	host := thirdPartyForwardedHost(firstNonEmpty(r.Header.Get("X-Forwarded-Host"), r.Host))
	return scheme + "://" + host + "/api/im-gateway/v1"
}

func thirdPartyForwardedScheme(value string, isTLS bool) string {
	scheme := strings.ToLower(thirdPartyForwardedHeaderFirst(value))
	switch scheme {
	case "https":
		return "https"
	case "http":
		return "http"
	}
	if isTLS && scheme == "" {
		return "https"
	}
	return "http"
}

func thirdPartyForwardedHost(value string) string {
	host := thirdPartyForwardedHeaderFirst(value)
	if host == "" || strings.ContainsAny(host, " \t\r\n/@?#\\%\"'") {
		return "127.0.0.1"
	}
	return host
}

func thirdPartyForwardedHeaderFirst(value string) string {
	if idx := strings.Index(value, ","); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

func normalizeIncomingRequest(req *thirdPartyIncomingRequest) error {
	return coreim.NormalizeThirdPartyIncomingRequest(req, coreim.ThirdPartyNormalizeOptions{
		RequireMessageID:      true,
		RequireUserID:         true,
		DefaultConversationID: "default",
		MaxTextChars:          thirdPartyMaxTextChars,
	})
}

func decodeGatewayJSON(r *http.Request, v any) error {
	return coreim.DecodeThirdPartyGatewayJSON(nil, r, v, int64(thirdPartyMaxBodyBytes))
}

func writeGatewayJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeGatewayError(w http.ResponseWriter, status int, code, message string) {
	writeGatewayJSON(w, status, coreim.NewThirdPartyGatewayErrorResponse(newGatewayRequestID(), code, message))
}

func newGatewayRequestID() string {
	return fmt.Sprintf("gw_%d", time.Now().UnixNano())
}

func normalizeThirdPartyID(s string) string {
	return coreim.NormalizeThirdPartyID(s)
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
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetThirdPartyGatewayLocal(enabled)
	}); err != nil {
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
