package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/tts"
)

const (
	srvThirdPartyProtocolVersion = coreim.ThirdPartyProtocolVersion
	srvThirdPartyRuntimeKey      = "maclawsrv:im:thirdparty"
	srvThirdPartySessionAgent    = "default"
	srvThirdPartyPollTimeoutSec  = coreim.ThirdPartyPollTimeoutSec
	srvThirdPartyMaxTimeoutSec   = coreim.ThirdPartyMaxTimeoutSec
	srvThirdPartyMaxBatchSize    = coreim.ThirdPartyMaxBatchSize
	srvThirdPartyMaxLimit        = coreim.ThirdPartyMaxPollLimit
	srvThirdPartyMaxTextChars    = coreim.ThirdPartyMaxTextChars
	srvThirdPartyMaxBodyBytes    = coreim.ThirdPartyMaxBodyBytes
	srvThirdPartyMaxMediaBytes   = coreim.ThirdPartyMaxMediaBytes
	srvThirdPartyMaxMediaObjects = 500
	srvThirdPartyMaxStoredMsgs   = 500
	srvThirdPartyMaxSeenEvents   = 2000
	srvThirdPartyMaxAckIDs       = coreim.ThirdPartyMaxAckIDs
)

type srvThirdPartyGatewayManager struct {
	svc      *agentservice.Service
	aiModels *srvAIModelManager

	mu               sync.Mutex
	clients          map[string]*srvThirdPartyClientState
	runtimeInstances map[string]srvThirdPartyRuntimeInstance
	media            map[string]*srvThirdPartyMediaObject
}

type srvThirdPartyRuntimeInstance struct {
	InstanceID         string
	LastIdentitySyncAt time.Time
}

type srvThirdPartyClientState struct {
	Next               int64
	Messages           []srvThirdPartyOutgoingMessage
	Acked              map[string]string
	Seen               map[string]string
	SeenOrder          []string
	Notify             chan struct{}
	ClientCapabilities agent.ClientCapabilities
}

type srvThirdPartyMediaObject struct {
	Principal      agentservice.Principal
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

type srvThirdPartyPrincipal struct {
	Principal agentservice.Principal
	Config    corelib.AppConfig
}

type srvThirdPartyHandshakeRequest = coreim.ThirdPartyHandshakeRequest

type srvThirdPartyIncomingRequest = coreim.ThirdPartyIncomingRequest

type srvThirdPartyUser = coreim.ThirdPartyUserRef

type srvThirdPartyMessageBody = coreim.ThirdPartyMessagePayload

type srvThirdPartyMessageMediaRef = coreim.ThirdPartyMediaReference

type srvThirdPartyAckRequest = coreim.ThirdPartyAckRequest

type srvThirdPartyToolResultRequest = coreim.ThirdPartyToolResultRequest

type srvThirdPartyOutgoingMessage = coreim.ThirdPartyOutgoingMessage

func newSrvThirdPartyGatewayManager(svc *agentservice.Service, aiModels ...*srvAIModelManager) *srvThirdPartyGatewayManager {
	var asrManager *srvAIModelManager
	if len(aiModels) > 0 {
		asrManager = aiModels[0]
	}
	return &srvThirdPartyGatewayManager{svc: svc, aiModels: asrManager, clients: map[string]*srvThirdPartyClientState{}, runtimeInstances: map[string]srvThirdPartyRuntimeInstance{}, media: map[string]*srvThirdPartyMediaObject{}}
}

func (s *HTTPServer) handleThirdPartyGatewayHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	writeJSON(w, http.StatusOK, coreim.NewThirdPartyGatewayHealthResponse(newThirdPartyGatewayRequestID(), time.Now().UnixMilli()))
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
	if err := coreim.NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	clientID := req.ClientID
	s.thirdPartyIM.setClientCapabilities(thirdPartyClientKey(tp.Principal, clientID), req.ClientCapabilities)
	response := coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{
		RequestID:      newThirdPartyGatewayRequestID(),
		ChannelID:      "thirdparty:" + clientID,
		ServerTime:     time.Now().UnixMilli(),
		MaxBodyBytes:   srvThirdPartyMaxBodyBytes,
		MaxMediaBytes:  srvThirdPartyMaxMediaBytes,
		PollTimeoutSec: srvThirdPartyPollTimeoutSec,
		MaxTimeoutSec:  srvThirdPartyMaxTimeoutSec,
		MaxBatchSize:   srvThirdPartyMaxBatchSize,
		MaxPollLimit:   srvThirdPartyMaxLimit,
	})
	response.CapabilitiesAccepted = req.ClientCapabilities
	writeJSON(w, http.StatusOK, response)
}

func (s *HTTPServer) handleThirdPartyGatewayMediaUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	var req coreim.ThirdPartyMediaPrepareRequest
	if !decodeThirdPartyGatewayJSON(w, r, &req) {
		return
	}
	if err := coreim.NormalizeThirdPartyMediaPrepareRequest(&req, srvThirdPartyMaxMediaBytes); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	media, err := s.thirdPartyIM.prepareMedia(tp.Principal, req, thirdPartyGatewayBaseURL(r))
	if err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, media)
}

func (s *HTTPServer) handleThirdPartyGatewayMediaUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "PUT required")
		return
	}
	id := strings.TrimSpace(r.PathValue("mediaId"))
	if err := s.thirdPartyIM.storeMediaUpload(r, id); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, coreim.NewThirdPartyMediaUploadCompleteResponse(newThirdPartyGatewayRequestID(), id))
}

func (s *HTTPServer) handleThirdPartyGatewayMediaDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
		return
	}
	id := strings.TrimSpace(r.PathValue("mediaId"))
	media, err := s.thirdPartyIM.mediaForDownload(r, id)
	if err != nil {
		writeThirdPartyGatewayError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if media.MimeType != "" {
		w.Header().Set("Content-Type", media.MimeType)
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", media.FileName))
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(media.Data)), 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(media.Data)
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
	if err := s.thirdPartyIM.validateIncomingMediaReferences(tp.Principal, &req); err != nil {
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
	writeJSON(w, http.StatusOK, coreim.NewThirdPartyIncomingAcceptedResponse(newThirdPartyGatewayRequestID(), maclawID, duplicate))
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
	poll, err := coreim.ParseThirdPartyPollQuery(r.URL.Query(), coreim.ThirdPartyGatewayConfig{
		PollTimeoutSec: srvThirdPartyPollTimeoutSec,
		MaxTimeoutSec:  srvThirdPartyMaxTimeoutSec,
		MaxBatchSize:   srvThirdPartyMaxBatchSize,
		MaxPollLimit:   srvThirdPartyMaxLimit,
	})
	if err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	clientKey := thirdPartyClientKey(tp.Principal, poll.ClientID)
	timer := time.NewTimer(time.Duration(poll.TimeoutSec) * time.Second)
	if poll.TimeoutSec == 0 {
		timer.Stop()
	}
	defer timer.Stop()
	for {
		msgs, next, hasMore, notify := s.thirdPartyIM.messagesAfter(clientKey, poll.Cursor, poll.Limit)
		if len(msgs) > 0 || poll.TimeoutSec == 0 {
			writeJSON(w, http.StatusOK, coreim.NewThirdPartyOutgoingPollResponse(newThirdPartyGatewayRequestID(), msgs, next, hasMore))
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			_, next, _, _ = s.thirdPartyIM.messagesAfter(clientKey, poll.Cursor, poll.Limit)
			writeJSON(w, http.StatusOK, coreim.NewThirdPartyOutgoingPollResponse(newThirdPartyGatewayRequestID(), nil, next, false))
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
	if err := coreim.NormalizeThirdPartyAckRequest(&req, srvThirdPartyMaxAckIDs); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	s.thirdPartyIM.ack(thirdPartyClientKey(tp.Principal, req.ClientID), req)
	writeJSON(w, http.StatusOK, coreim.NewThirdPartyGatewayOKResponse(newThirdPartyGatewayRequestID()))
}

func (s *HTTPServer) handleThirdPartyGatewayToolResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeThirdPartyGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	tp, ok := s.authorizeThirdPartyGateway(w, r)
	if !ok {
		return
	}
	var req srvThirdPartyToolResultRequest
	if !decodeThirdPartyGatewayJSON(w, r, &req) {
		return
	}
	if err := coreim.NormalizeThirdPartyToolResultRequest(&req); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	maclawID := fmt.Sprintf("mc_tool_%d_%s", time.Now().UnixMilli(), sanitizeThirdPartyGatewayID(firstNonEmptyThirdParty(req.ToolCallID, req.ToolPlanID)))
	eventID := coreim.ThirdPartyToolResultEventID(req)
	incoming := srvThirdPartyIncomingRequest{
		ClientID:       req.ClientID,
		EventID:        eventID,
		MessageID:      maclawID,
		ConversationID: firstNonEmptyThirdParty(req.ConversationID, "default"),
		User:           srvThirdPartyUser{ID: "client-tool:" + req.ClientID, Name: "Client Tool"},
		Message:        srvThirdPartyMessageBody{Type: "text", Text: coreim.ThirdPartyToolResultContent(req)},
		Metadata: map[string]string{
			"message_kind": "tool_result",
			"tool_call_id": req.ToolCallID,
			"tool_plan_id": req.ToolPlanID,
			"tool_step_id": req.StepID,
			"tool_status":  req.Status,
		},
		CreatedAt: time.Now().UnixMilli(),
	}
	clientKey := thirdPartyClientKey(tp.Principal, req.ClientID)
	duplicate, storedID := s.thirdPartyIM.markIncoming(clientKey, incoming.EventID, maclawID)
	maclawID = storedID
	if !duplicate {
		go s.thirdPartyIM.processIncoming(context.Background(), tp.Principal, incoming, maclawID)
	}
	writeJSON(w, http.StatusOK, coreim.NewThirdPartyIncomingAcceptedResponse(newThirdPartyGatewayRequestID(), maclawID, duplicate))
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
			"runtime":          "maclawsrv",
			"client_id":        req.ClientID,
			"conversation_id":  req.ConversationID,
			"event_id":         req.EventID,
			"maclaw_id":        maclawID,
			"message_type":     req.Message.Type,
			"attachment_count": strconv.Itoa(len(req.Message.Attachments)),
		},
	})
	voiceTranscript, voiceOK := m.transcribeThirdPartyVoice(ctx, p, req)
	content, attachments := m.thirdPartyAgentInput(p, req)
	if voiceOK {
		content = voiceTranscript
		metadata["asr_transcript"] = voiceTranscript
		metadata["asr_source"] = "maclawsrv"
	}
	sendInput := agentservice.SendMessageInput{
		AgentID:            srvThirdPartySessionAgent,
		Title:              "Third-party " + req.ClientID,
		Content:            content,
		InputType:          "text/plain",
		Attachments:        attachments,
		Metadata:           metadata,
		SessionMetadata:    metadata,
		ClientSessionKey:   "thirdparty:" + req.ClientID + ":" + req.ConversationID,
		ClientMessageID:    req.EventID,
		ClientCapabilities: m.clientCapabilities(thirdPartyClientKey(p, req.ClientID)),
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
		m.enqueueAssistantReply(ctx, p, req, maclawID, assistant.ID, assistant.Content, assistant.CreatedAt)
	}
	_ = parent
}

// enqueueAssistantReply turns one logical Agent answer into the richest output
// combination the concrete client declared. Voice-originated turns are spoken
// automatically; text-originated turns use the user's existing automatic TTS
// preference. A TTS/model/media failure always degrades to useful text when the
// client supports it.
func (m *srvThirdPartyGatewayManager) enqueueAssistantReply(ctx context.Context, p agentservice.Principal, req srvThirdPartyIncomingRequest, replyTo, assistantID, text string, createdAt time.Time) {
	text = strings.TrimSpace(text)
	if m == nil || text == "" {
		return
	}
	capabilities := agent.NormalizeClientCapabilities(m.clientCapabilities(thirdPartyClientKey(p, req.ClientID)))
	present := make([]string, 0, 2)
	if capabilities.SupportsOutput("text") {
		present = append(present, "text")
	}

	var audio *srvThirdPartyOutgoingMessage
	voiceTurn := strings.EqualFold(req.Message.Type, "voice") || strings.EqualFold(req.Message.Type, "audio")
	var cfg corelib.AppConfig
	if m.svc != nil {
		if userCfg, err := m.svc.GetUserConfig(ctx, p); err == nil && userCfg != nil {
			cfg = userCfg.AppConfig
		}
	}
	if (voiceTurn || cfg.TTSAutoVoiceSummary) && m.aiModels != nil &&
		capabilities.SupportsOutputMIME("audio", "audio/wav") &&
		capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback {
		// Hardware replies should stay short enough for bounded RAM/download
		// clients. The full useful answer is still carried by the text half.
		spoken := tts.CapSpeechText(text, 64)
		wav, _, err := m.aiModels.synthesizeText(ctx, cfg, spoken)
		if err == nil {
			if msg, ok := m.prepareAssistantAudio(p, req.ClientID, req.ConversationID, replyTo, assistantID, wav, createdAt); ok {
				audio = &msg
				present = append(present, "audio")
			}
		} else {
			_ = m.aiModels.startDownload(srvAIModelTTS, cfg, false)
		}
	}

	selected := agent.SelectClientOutputCombination(capabilities, present...)
	allows := func(modality string) bool {
		for _, item := range selected {
			if item == modality {
				return true
			}
		}
		return false
	}
	metadata := map[string]string{"assistant_message_id": assistantID}
	if allows("text") {
		m.enqueue(p, req, srvThirdPartyOutgoingMessage{
			ID:             "mc_out_" + assistantID,
			ReplyTo:        replyTo,
			ClientID:       req.ClientID,
			ConversationID: req.ConversationID,
			Type:           "text",
			Text:           text,
			CreatedAt:      createdAt.UnixMilli(),
			Metadata:       metadata,
		})
	}
	if allows("audio") && audio != nil {
		m.enqueue(p, req, *audio)
	}
}

// prepareAssistantAudio chooses inline or same-origin URL delivery using the
// negotiated byte ceilings. URL objects are private to the authenticated user
// and additionally protected by a random media token.
func (m *srvThirdPartyGatewayManager) prepareAssistantAudio(p agentservice.Principal, clientID, conversationID, replyTo, assistantID string, wav []byte, createdAt time.Time) (srvThirdPartyOutgoingMessage, bool) {
	clientKey := thirdPartyClientKey(p, clientID)
	capabilities := agent.NormalizeClientCapabilities(m.clientCapabilities(clientKey))
	size := int64(len(wav))
	msg := srvThirdPartyOutgoingMessage{
		ID:             "mc_voice_" + assistantID,
		ReplyTo:        replyTo,
		ClientID:       clientID,
		ConversationID: conversationID,
		Type:           "audio",
		FileName:       "reply.wav",
		MimeType:       "audio/wav",
		ContentType:    "audio/wav",
		SizeBytes:      size,
		CreatedAt:      createdAt.UnixMilli(),
		Metadata:       map[string]string{"assistant_message_id": assistantID, "tts": "true"},
	}
	if size <= 44 || size > srvThirdPartyMaxMediaBytes {
		return srvThirdPartyOutgoingMessage{}, false
	}
	if capabilities.SupportsOutputAudioDelivery("inline", size) {
		msg.Data = base64.StdEncoding.EncodeToString(wav)
		return msg, true
	}
	if !capabilities.SupportsOutputAudioDelivery("url", size) {
		return srvThirdPartyOutgoingMessage{}, false
	}
	id, err := randomThirdPartyGatewayToken()
	if err != nil {
		return srvThirdPartyOutgoingMessage{}, false
	}
	token, err := randomThirdPartyGatewayToken()
	if err != nil {
		return srvThirdPartyOutgoingMessage{}, false
	}
	now := time.Now().UTC()
	media := &srvThirdPartyMediaObject{
		Principal: p, ClientID: clientID, ID: id, Token: token, Type: "audio",
		FileName: "reply.wav", MimeType: "audio/wav", SizeBytes: size,
		Data: append([]byte(nil), wav...), Uploaded: true, CreatedAt: now, LastAccessedAt: now,
	}
	m.mu.Lock()
	m.media[id] = media
	m.pruneMediaLocked(now)
	m.mu.Unlock()
	// Keep the URL same-origin and relative. Constrained clients must never send
	// their durable gateway bearer to an arbitrary absolute host.
	msg.URL = fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", id, token)
	return msg, true
}

// transcribeThirdPartyVoice turns a gateway voice attachment into the text
// command that the normal MaClaw agent/tool pipeline executes. The device is
// intentionally only an audio/display surface: ASR and every privileged action
// remain server-side.
func (m *srvThirdPartyGatewayManager) transcribeThirdPartyVoice(ctx context.Context, p agentservice.Principal, req srvThirdPartyIncomingRequest) (string, bool) {
	if m == nil || m.aiModels == nil || (req.Message.Type != "voice" && req.Message.Type != "audio") {
		return "", false
	}
	cfg, err := m.svc.GetUserConfig(ctx, p)
	if err != nil || cfg == nil {
		return "", false
	}
	for _, ref := range req.Message.Attachments {
		if ref.Type != "voice" && ref.Type != "audio" {
			continue
		}
		data, ok := m.thirdPartyAttachmentBytes(p, ref)
		if !ok || len(data) == 0 {
			continue
		}
		wav, err := audioconv.ToWAV(data, firstNonEmptyThirdParty(ref.MimeType, ref.ContentType, ref.FileName))
		if err != nil {
			continue
		}
		text, err := m.aiModels.transcribeWAV(ctx, cfg.AppConfig, wav)
		if err != nil {
			_ = m.aiModels.startDownload(srvAIModelASR, cfg.AppConfig, false)
			return "", false
		}
		text = strings.TrimSpace(text)
		return text, text != ""
	}
	return "", false
}

func (m *srvThirdPartyGatewayManager) thirdPartyAgentInput(p agentservice.Principal, req srvThirdPartyIncomingRequest) (string, []agent.MessageAttachment) {
	content := coreim.ThirdPartyIncomingContent(req)
	if len(req.Message.Attachments) == 0 {
		return content, nil
	}
	var attachments []agent.MessageAttachment
	var notes []string
	for _, ref := range req.Message.Attachments {
		data, ok := m.thirdPartyAttachmentBytes(p, ref)
		if ok && len(data) <= coreim.ThirdPartyMaxDirectBytes {
			attachments = append(attachments, agent.MessageAttachment{
				Type:     ref.Type,
				FileName: ref.FileName,
				MimeType: ref.MimeType,
				Data:     base64.StdEncoding.EncodeToString(data),
				Size:     int64(len(data)),
			})
			notes = append(notes, fmt.Sprintf("- %s %s attached inline for agent reading/vision (%d bytes)", ref.Type, firstNonEmptyThirdParty(ref.FileName, ref.ID, "attachment"), len(data)))
			if text, ok := thirdPartyReadableTextAttachment(ref, data); ok {
				notes = append(notes, fmt.Sprintf("  content:\n%s", indentThirdPartyAttachmentText(text)))
			}
			continue
		}
		if ok && len(data) > coreim.ThirdPartyMaxDirectBytes {
			if strings.TrimSpace(ref.URL) != "" {
				notes = append(notes, fmt.Sprintf("- %s %s available at server media URL; size=%d bytes; url=%s", ref.Type, firstNonEmptyThirdParty(ref.FileName, ref.ID, "attachment"), len(data), ref.URL))
			} else {
				notes = append(notes, fmt.Sprintf("- %s %s available as server media id %s; size=%d bytes", ref.Type, firstNonEmptyThirdParty(ref.FileName, ref.ID, "attachment"), ref.ID, len(data)))
			}
			continue
		}
	}
	if len(notes) > 0 {
		if strings.TrimSpace(content) != "" {
			content += "\n\n"
		}
		content += "[Attachment access for agent]\n" + strings.Join(notes, "\n")
		if len(attachments) > 0 {
			content += "\nInline attachments are passed as agent attachments; image-capable models can inspect images directly."
		}
	}
	return content, attachments
}

func thirdPartyReadableTextAttachment(ref coreim.ThirdPartyMediaReference, data []byte) (string, bool) {
	mimeType := strings.ToLower(strings.TrimSpace(ref.MimeType))
	name := strings.ToLower(strings.TrimSpace(ref.FileName))
	if !(strings.HasPrefix(mimeType, "text/") ||
		strings.Contains(mimeType, "json") ||
		strings.Contains(mimeType, "xml") ||
		strings.Contains(mimeType, "csv") ||
		strings.HasSuffix(name, ".txt") ||
		strings.HasSuffix(name, ".md") ||
		strings.HasSuffix(name, ".json") ||
		strings.HasSuffix(name, ".csv") ||
		strings.HasSuffix(name, ".xml") ||
		strings.HasSuffix(name, ".yaml") ||
		strings.HasSuffix(name, ".yml")) {
		return "", false
	}
	const maxInlineText = 64 * 1024
	text := strings.ToValidUTF8(string(data), "\uFFFD")
	if len(text) > maxInlineText {
		text = text[:maxInlineText] + "\n[truncated]"
	}
	return text, strings.TrimSpace(text) != ""
}

func indentThirdPartyAttachmentText(text string) string {
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = "    " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *srvThirdPartyGatewayManager) thirdPartyAttachmentBytes(p agentservice.Principal, ref coreim.ThirdPartyMediaReference) ([]byte, bool) {
	if strings.TrimSpace(ref.Data) != "" {
		data, err := base64.StdEncoding.DecodeString(ref.Data)
		return data, err == nil
	}
	id := strings.TrimSpace(ref.ID)
	if id == "" {
		return nil, false
	}
	m.mu.Lock()
	media := m.media[id]
	m.mu.Unlock()
	if media == nil || !media.Uploaded || media.Principal.TenantID != p.TenantID || media.Principal.UserID != p.UserID {
		return nil, false
	}
	if ref.MimeType == "" {
		ref.MimeType = media.MimeType
	}
	return append([]byte(nil), media.Data...), true
}

func (m *srvThirdPartyGatewayManager) validateIncomingMediaReferences(p agentservice.Principal, req *srvThirdPartyIncomingRequest) error {
	for i := range req.Message.Attachments {
		ref := &req.Message.Attachments[i]
		if strings.TrimSpace(ref.URL) != "" {
			id, mediaReq, err := srvThirdPartyServerMediaRequestFromURL(ref.URL)
			if err != nil {
				return fmt.Errorf("message.attachments[%d].url: %w", i, err)
			}
			media, err := m.mediaForDownload(mediaReq, id)
			if err != nil {
				return fmt.Errorf("message.attachments[%d].url media not found", i)
			}
			if media.Principal.TenantID != p.TenantID || media.Principal.UserID != p.UserID {
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
			media, ok := m.mediaObjectForPrincipal(p, ref.ID)
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

func (m *srvThirdPartyGatewayManager) mediaObjectForPrincipal(p agentservice.Principal, id string) (srvThirdPartyMediaObject, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	media := m.media[strings.TrimSpace(id)]
	if media == nil || !media.Uploaded || media.Principal.TenantID != p.TenantID || media.Principal.UserID != p.UserID {
		return srvThirdPartyMediaObject{}, false
	}
	media.LastAccessedAt = time.Now().UTC()
	return *media, true
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

func (m *srvThirdPartyGatewayManager) setClientCapabilities(clientKey string, capabilities *agent.ClientCapabilities) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientKey)
	state.ClientCapabilities = agent.NormalizeClientCapabilities(capabilities)
}

func (m *srvThirdPartyGatewayManager) clientCapabilities(clientKey string) *agent.ClientCapabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientKey)
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	return &capabilities
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
	for key, media := range m.media {
		if media.Principal.TenantID == p.TenantID && media.Principal.UserID == p.UserID {
			delete(m.media, key)
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
	for key, media := range m.media {
		if media.Principal.TenantID == tenantID {
			delete(m.media, key)
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
	m.media = map[string]*srvThirdPartyMediaObject{}
}

func (m *srvThirdPartyGatewayManager) prepareMedia(p agentservice.Principal, req coreim.ThirdPartyMediaPrepareRequest, baseURL string) (*coreim.ThirdPartyMediaPrepareResponse, error) {
	if err := coreim.NormalizeThirdPartyMediaPrepareRequest(&req, srvThirdPartyMaxMediaBytes); err != nil {
		return nil, err
	}
	id, err := randomThirdPartyGatewayToken()
	if err != nil {
		return nil, err
	}
	token, err := randomThirdPartyGatewayToken()
	if err != nil {
		return nil, err
	}
	fileName := coreim.SafeThirdPartyFileName(req.FileName)
	mimeType := strings.TrimSpace(req.MimeType)
	downloadURL := fmt.Sprintf("%s/media/%s?mediaToken=%s", strings.TrimRight(baseURL, "/"), id, token)
	uploadURL := fmt.Sprintf("%s/media/%s/upload?mediaToken=%s", strings.TrimRight(baseURL, "/"), id, token)
	obj := &srvThirdPartyMediaObject{
		Principal:      p,
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
	m.media[id] = obj
	m.pruneMediaLocked(time.Now().UTC())
	m.mu.Unlock()
	ref := coreim.ThirdPartyMediaReference{ID: id, Type: req.Type, FileName: fileName, MimeType: mimeType, URL: downloadURL, SizeBytes: req.SizeBytes, DurationMs: req.DurationMs}
	return &coreim.ThirdPartyMediaPrepareResponse{
		OK:        true,
		RequestID: newThirdPartyGatewayRequestID(),
		Media:     ref,
		Upload:    coreim.ThirdPartyMediaUpload{Method: http.MethodPut, URL: uploadURL, ContentType: mimeType, MaxBytes: srvThirdPartyMaxMediaBytes},
		Download:  coreim.ThirdPartyMediaDownload{URL: downloadURL},
		ExpiresAt: time.Now().Add(24 * time.Hour).UnixMilli(),
	}, nil
}

func (m *srvThirdPartyGatewayManager) storeMediaUpload(r *http.Request, id string) error {
	m.mu.Lock()
	media := m.media[id]
	m.mu.Unlock()
	if media == nil {
		return errors.New("media not found")
	}
	if !coreim.ThirdPartyMediaTokenOK(r, media.Token) {
		return errors.New("invalid media token")
	}
	if r.ContentLength > srvThirdPartyMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", srvThirdPartyMaxMediaBytes)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, srvThirdPartyMaxMediaBytes+1))
	if err != nil {
		return err
	}
	if len(data) > srvThirdPartyMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", srvThirdPartyMaxMediaBytes)
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

func (m *srvThirdPartyGatewayManager) mediaForDownload(r *http.Request, id string) (*srvThirdPartyMediaObject, error) {
	m.mu.Lock()
	media := m.media[id]
	m.mu.Unlock()
	if media == nil || !media.Uploaded {
		return nil, errors.New("media not found")
	}
	if !coreim.ThirdPartyMediaTokenOK(r, media.Token) {
		return nil, errors.New("media not found")
	}
	m.mu.Lock()
	media.LastAccessedAt = time.Now().UTC()
	out := *media
	m.mu.Unlock()
	return &out, nil
}

func (m *srvThirdPartyGatewayManager) pruneMediaLocked(now time.Time) {
	if len(m.media) == 0 {
		return
	}
	cutoff := now.Add(-24 * time.Hour)
	for id, media := range m.media {
		if media.LastAccessedAt.Before(cutoff) {
			delete(m.media, id)
		}
	}
	for len(m.media) > srvThirdPartyMaxMediaObjects {
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
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	if !adaptThirdPartyOutgoingMessage(&msg, capabilities) {
		m.mu.Unlock()
		return
	}
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

func adaptThirdPartyOutgoingMessage(msg *srvThirdPartyOutgoingMessage, capabilities agent.ClientCapabilities) bool {
	if msg == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(msg.Type)) {
	case "image":
		return capabilities.SupportsOutputMIME("image", outgoingSrvThirdPartyMIME(*msg))
	case "file":
		return capabilities.SupportsOutputMIME("file", outgoingSrvThirdPartyMIME(*msg)) && capabilities.SupportsOutputBytes("file", msg.SizeBytes)
	case "voice", "audio":
		return capabilities.SupportsOutput("audio") && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback && capabilities.SupportsOutputMIME("audio", outgoingSrvThirdPartyMIME(*msg))
	default:
		if !capabilities.SupportsOutput("text") {
			return false
		}
		msg.Type = "text"
		if capabilities.Output.Text != nil && capabilities.Output.Text.MaxChars > 0 {
			msg.Text = truncateThirdPartyRunes(msg.Text, capabilities.Output.Text.MaxChars)
		}
		return strings.TrimSpace(msg.Text) != ""
	}
}

func outgoingSrvThirdPartyMIME(msg srvThirdPartyOutgoingMessage) string {
	if strings.TrimSpace(msg.MimeType) != "" {
		return msg.MimeType
	}
	return msg.ContentType
}

func truncateThirdPartyRunes(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
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
	status := coreim.NormalizeThirdPartyAckStatus(req.Status)
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
	return coreim.NormalizeThirdPartyIncomingRequest(req, coreim.ThirdPartyNormalizeOptions{DefaultConversationID: "default", MaxTextChars: srvThirdPartyMaxTextChars})
}

func decodeThirdPartyGatewayJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := coreim.DecodeThirdPartyGatewayJSON(w, r, out, int64(srvThirdPartyMaxBodyBytes)); err != nil {
		writeThirdPartyGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return false
	}
	return true
}

func writeThirdPartyGatewayError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, coreim.NewThirdPartyGatewayErrorResponse(newThirdPartyGatewayRequestID(), code, message))
}

func thirdPartyBearerToken(r *http.Request) string {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[len("Bearer "):])
}

func newThirdPartyGatewayRequestID() string {
	return fmt.Sprintf("gw_%d", time.Now().UnixNano())
}

func thirdPartyClientKey(p agentservice.Principal, clientID string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + normalizeThirdPartyGatewayID(clientID)
}

func normalizeThirdPartyGatewayID(value string) string {
	return coreim.NormalizeThirdPartyID(value)
}

func validateThirdPartyGatewayID(field, value string) error {
	return coreim.ValidateThirdPartyID(field, value)
}

func sanitizeThirdPartyGatewayID(value string) string {
	value = normalizeThirdPartyGatewayID(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func randomThirdPartyGatewayToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

func thirdPartyGatewayBaseURL(r *http.Request) string {
	scheme := thirdPartyGatewayForwardedScheme(r.Header.Get("X-Forwarded-Proto"), r.TLS != nil)
	host := platformLaunchHost(firstNonEmptyThirdParty(r.Header.Get("X-Forwarded-Host"), r.Host))
	return scheme + "://" + host + "/api/im-gateway/v1"
}

func srvThirdPartyServerMediaRequestFromURL(rawURL string) (string, *http.Request, error) {
	return coreim.ThirdPartyServerMediaRequestFromURL(rawURL)
}

func thirdPartyGatewayForwardedScheme(value string, isTLS bool) string {
	scheme := strings.ToLower(platformForwardedHeaderFirst(value))
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

func firstNonEmptyThirdParty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
