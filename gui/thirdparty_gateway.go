package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/RapidAI/CodeClaw/corelib/agent"
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
	pairings     map[string]thirdPartyDevicePairing
	// Per-IP sliding-window attempts for the voice pairing endpoint. Each
	// request costs a full local ASR inference, so an unauthenticated LAN
	// caller must not be able to spin the CPU with WAV uploads.
	voicePairAttempts map[string][]time.Time
}

// thirdPartyDevicePairing is deliberately short lived and single use.  It is
// only a bootstrap secret: once exchanged, the ESP stores the regular Gateway
// bearer and speaks the normal third-party protocol.
type thirdPartyDevicePairing struct {
	Token     string
	ExpiresAt time.Time
	Remote    bool
}

const thirdPartyMaxPendingPairings = 16

func (m *thirdPartyGatewayManager) currentLocalHandler() *IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.localHandler
}

type thirdPartyClientState struct {
	NextSeq            int64
	Messages           []thirdPartyOutgoingMessage
	SeenEvents         map[string]string
	Acked              map[string]string
	ClientCapabilities agent.ClientCapabilities
	LastWelcomeBootID  string
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
		pairings: make(map[string]thirdPartyDevicePairing),
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
		_ = lh // shared App conversation memory remains alive
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
	mux.HandleFunc("/api/device-gateway/v1/pair", m.handleDevicePair)
	mux.HandleFunc("/api/device-gateway/v1/pair/voice", m.handleDeviceVoicePair)

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
	_ = lh // shared App conversation memory remains alive
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
	m.localHandler = nil
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
	m.setClientCapabilities(req.ClientID, req.ClientCapabilities)
	response := coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{
		RequestID:      newGatewayRequestID(),
		ChannelID:      thirdPartyPlatform(req.ClientID),
		ServerTime:     time.Now().UnixMilli(),
		MaxBodyBytes:   thirdPartyMaxBodyBytes,
		MaxMediaBytes:  thirdPartyMaxMediaBytes,
		PollTimeoutSec: thirdPartyPollTimeoutSec,
		MaxTimeoutSec:  thirdPartyMaxTimeoutSec,
		MaxBatchSize:   thirdPartyMaxBatchSize,
		MaxPollLimit:   thirdPartyMaxLimit,
	})
	response.CapabilitiesAccepted = req.ClientCapabilities
	// The local gateway can render the selected desktop pack directly.  Ship a
	// compact RGB565 idle frame so the ESP shows the same pet, not an ID-based
	// approximation.
	data, _ := json.Marshal(response)
	var payload map[string]any
	_ = json.Unmarshal(data, &payload)
	if cfg, err := m.app.LoadConfig(); err == nil {
		profile := m.app.devicePetProfileForConfig(cfg)
		if req.ClientCapabilities != nil && req.ClientCapabilities.Features.PetAsset {
			if asset, ok := profile["asset"].(devicePetAsset); ok {
				// preparePetAssetLocked mutates m.media and requires m.mu, but
				// handleHandshake otherwise runs unlocked; broadcastPetProfile
				// calls the same helper with the mutex already held, so lock at
				// this call site instead of inside the helper.
				m.mu.Lock()
				ref := m.preparePetAssetLocked(req.ClientID, asset,
					req.ClientCapabilities.Features.PetAnimation && profile["motionEnabled"] == true)
				m.mu.Unlock()
				if ref != nil {
					payload["petAsset"] = ref
				}
			}
		}
		delete(profile, "asset")
		payload["pet"] = profile
	}
	// Send the persisted speaker level to devices that pair or reconnect after
	// the setting was chosen. It is a control-plane message, so it is safe to
	// deliver on an ordinary capability refresh as well. Enqueue before the
	// handshake response finishes, otherwise a fast client can begin its first
	// long poll before the asynchronous work runs and wait an unnecessary 30 s.
	m.queueHardwareConfigForClient(req.ClientID)
	// A welcome message belongs to cold-start initialization, not a regular
	// handshake refresh. The ESP sends a fresh boot session ID once per boot.
	if req.BootSessionID != "" {
		m.queueWelcomeForClient(req.ClientID, req.BootSessionID)
	}
	writeGatewayJSON(w, http.StatusOK, payload)
}

func (m *thirdPartyGatewayManager) queueHardwareConfigForClient(clientID string) {
	if m == nil || m.app == nil {
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		return
	}
	m.enqueueHardwareConfigForClient(clientID, cfg.HardwareVolume)
}

func (m *thirdPartyGatewayManager) queueWelcomeForClient(clientID, bootSessionID string) {
	if m == nil || m.app == nil {
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil || !cfg.HardwareWelcomeEnabled || strings.TrimSpace(cfg.HardwareWelcomeAudioPath) == "" {
		return
	}
	wav, err := os.ReadFile(cfg.HardwareWelcomeAudioPath)
	if err != nil || len(wav) == 0 || len(wav) > hardwareWelcomeMaxWAVBytes {
		return
	}
	m.enqueueHardwareAudioForBoot(clientID, bootSessionID, base64.StdEncoding.EncodeToString(wav))
}

func (m *thirdPartyGatewayManager) handleDevicePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var req struct {
		PairCode string `json:"pairCode"`
		// Code keeps local pairing compatible with previous ESP firmware.
		Code     string `json:"code"`
		ClientID string `json:"clientId"`
	}
	if err := decodeGatewayJSON(r, &req); err != nil {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	pairCode := strings.TrimSpace(req.PairCode)
	if pairCode == "" {
		pairCode = strings.TrimSpace(req.Code)
	}
	if len(pairCode) != 6 || strings.Trim(pairCode, "0123456789") != "" || strings.TrimSpace(req.ClientID) == "" {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "clientId and a six-digit pairCode are required")
		return
	}
	m.exchangeDevicePairing(w, httplessDevicePairRequest{PairCode: pairCode, ClientID: req.ClientID})
}

// voicePairWindow/voicePairMaxAttempts rate-limit the unauthenticated voice
// pairing endpoint: every attempt runs a full local ASR inference, so a LAN
// caller must not be able to spin the CPU with WAV uploads.
const (
	voicePairWindow       = time.Minute
	voicePairMaxAttempts  = 5
	voicePairMaxTrackedIP = 1024
)

// clientRemoteIP extracts the host part of the request's remote address.
func clientRemoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// allowVoicePairAttempt records one attempt from ip and reports whether it
// fits the per-IP sliding window. Attempts are counted before ASR runs —
// rejected codes are exactly the expensive case to keep bounded.
func (m *thirdPartyGatewayManager) allowVoicePairAttempt(ip string) bool {
	if m == nil {
		return true
	}
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.voicePairAttempts == nil {
		m.voicePairAttempts = make(map[string][]time.Time)
	}
	attempts := m.voicePairAttempts[ip]
	kept := attempts[:0]
	for _, at := range attempts {
		if now.Sub(at) < voicePairWindow {
			kept = append(kept, at)
		}
	}
	if len(kept) >= voicePairMaxAttempts {
		m.voicePairAttempts[ip] = kept
		return false
	}
	m.voicePairAttempts[ip] = append(kept, now)
	// Bound the map itself: spoofed source IPs must not grow it forever.
	if len(m.voicePairAttempts) > voicePairMaxTrackedIP {
		for candidate, list := range m.voicePairAttempts {
			if len(list) == 0 || now.Sub(list[len(list)-1]) >= voicePairWindow {
				delete(m.voicePairAttempts, candidate)
			}
		}
	}
	return true
}

// handleDeviceVoicePair is the screenless-device equivalent of handleDevicePair.
// The GUI's existing local SenseVoice model transcribes the short WAV and the
// resulting six digits are exchanged through the same single-use pairing store.
// Hub mode keeps this endpoint at the public Hub; this local handler is for
// deployments that deliberately expose the GUI gateway on their LAN.
func (m *thirdPartyGatewayManager) handleDeviceVoicePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeGatewayError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	if !m.allowVoicePairAttempt(clientRemoteIP(r)) {
		writeGatewayError(w, http.StatusTooManyRequests, "rate_limited", "too many voice pairing attempts; retry later")
		return
	}
	if m == nil || m.app == nil {
		writeGatewayError(w, http.StatusServiceUnavailable, "unavailable", "speech recognition is unavailable")
		return
	}
	clientID := strings.TrimSpace(r.Header.Get("X-MaClaw-Client-ID"))
	if clientID == "" {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "X-MaClaw-Client-ID is required")
		return
	}
	const maxPairingWAVBytes = 512 << 10
	wav, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPairingWAVBytes))
	if err != nil || len(wav) < 44 {
		writeGatewayError(w, http.StatusBadRequest, "bad_request", "a short WAV recording is required")
		return
	}
	transcript, err := m.app.transcribeWAVBytes(wav, asrTranscribeOpts{MaxDurationSec: 10, SkipVAD: true})
	if err != nil {
		log.Printf("[thirdparty-mgr] voice pairing ASR failed: %v", err)
		writeGatewayError(w, http.StatusServiceUnavailable, "unavailable", "speech recognition is unavailable")
		return
	}
	pairCode, ok := thirdPartyPairCodeFromTranscript(transcript)
	if !ok {
		writeGatewayError(w, http.StatusBadRequest, "bad_pair_code", "please speak exactly six digits")
		return
	}
	req := httplessDevicePairRequest{PairCode: pairCode, ClientID: clientID}
	m.exchangeDevicePairing(w, req)
}

type httplessDevicePairRequest struct {
	PairCode string
	ClientID string
}

func (m *thirdPartyGatewayManager) exchangeDevicePairing(w http.ResponseWriter, req httplessDevicePairRequest) {
	now := time.Now()
	m.mu.Lock()
	for candidate, pairing := range m.pairings {
		if !pairing.ExpiresAt.After(now) {
			delete(m.pairings, candidate)
		}
	}
	pairing, ok := m.pairings[req.PairCode]
	if ok && !pairing.Remote {
		delete(m.pairings, req.PairCode)
	}
	m.mu.Unlock()
	if !ok || pairing.Remote || !pairing.ExpiresAt.After(now) {
		writeGatewayError(w, http.StatusUnauthorized, "invalid_pairing_code", "pairing code is invalid or expired")
		return
	}
	writeGatewayJSON(w, http.StatusCreated, map[string]any{
		"ok": true, "gatewayToken": pairing.Token, "clientId": strings.TrimSpace(req.ClientID),
	})
}

func thirdPartyPairCodeFromTranscript(transcript string) (string, bool) {
	var digits strings.Builder
	normalized := strings.ToLower(strings.TrimSpace(transcript))
	for _, r := range normalized {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		if digit, ok := thirdPartySpokenChineseDigit(r); ok {
			digits.WriteByte(digit)
		}
	}
	for _, word := range strings.FieldsFunc(normalized, func(r rune) bool {
		return r < 'a' || r > 'z'
	}) {
		if digit, ok := thirdPartySpokenEnglishDigit(word); ok {
			digits.WriteByte(digit)
		}
	}
	code := digits.String()
	return code, len(code) == 6
}

func thirdPartySpokenChineseDigit(r rune) (byte, bool) {
	switch r {
	case '零', '〇':
		return '0', true
	case '一', '幺':
		return '1', true
	case '二', '两':
		return '2', true
	case '三':
		return '3', true
	case '四':
		return '4', true
	case '五':
		return '5', true
	case '六':
		return '6', true
	case '七':
		return '7', true
	case '八':
		return '8', true
	case '九':
		return '9', true
	default:
		return 0, false
	}
}

func thirdPartySpokenEnglishDigit(word string) (byte, bool) {
	switch word {
	case "zero", "oh":
		return '0', true
	case "one":
		return '1', true
	case "two", "to", "too":
		return '2', true
	case "three":
		return '3', true
	case "four", "for":
		return '4', true
	case "five":
		return '5', true
	case "six":
		return '6', true
	case "seven":
		return '7', true
	case "eight", "ate":
		return '8', true
	case "nine":
		return '9', true
	default:
		return 0, false
	}
}

func (m *thirdPartyGatewayManager) handleIncoming(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
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
	log.Printf("[thirdparty-mgr] incoming accepted client=%s event=%s maclawID=%s duplicate=%t type=%s elapsed=%s",
		req.ClientID, req.EventID, maclawID, duplicate, req.Message.Type, time.Since(started).Round(time.Millisecond))
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
	pruneThirdPartyMessagesLocked(state)
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

func (m *thirdPartyGatewayManager) setClientCapabilities(clientID string, capabilities *agent.ClientCapabilities) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	state.ClientCapabilities = agent.NormalizeClientCapabilities(capabilities)
}

func (m *thirdPartyGatewayManager) clientCapabilities(clientID string) agent.ClientCapabilities {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	return agent.NormalizeClientCapabilities(&state.ClientCapabilities)
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
	started := time.Now()
	m.mu.Lock()
	state := m.ensureClientLocked(clientID)
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	if !m.prepareOutgoingAudioLocked(clientID, &msg, capabilities) {
		m.mu.Unlock()
		return thirdPartyOutgoingMessage{}
	}
	if !adaptGUIThirdPartyOutgoingMessage(&msg, capabilities) {
		m.mu.Unlock()
		return thirdPartyOutgoingMessage{}
	}
	msg.Seq = state.NextSeq
	state.NextSeq++
	if msg.ID == "" {
		msg.ID = fmt.Sprintf("mc_out_%d_%06d", time.Now().UnixMilli(), msg.Seq)
	}
	if msg.CreatedAt == 0 {
		msg.CreatedAt = time.Now().UnixMilli()
	}
	state.Messages = append(state.Messages, msg)
	pruneThirdPartyMessagesLocked(state)
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
	log.Printf("[thirdparty-mgr] outgoing queued client=%s id=%s seq=%d type=%s replyTo=%s final=%t progress=%t textChars=%d elapsed=%s",
		clientID, msg.ID, msg.Seq, msg.Type, msg.ReplyToMessageID,
		strings.EqualFold(msg.Metadata["acp_turn"], "final"), msg.Progress, len([]rune(msg.Text)),
		time.Since(started).Round(time.Millisecond))
	return msg
}

func (m *thirdPartyGatewayManager) prepareOutgoingAudioLocked(clientID string, msg *thirdPartyOutgoingMessage, capabilities agent.ClientCapabilities) bool {
	if msg == nil || normalizeThirdPartyGatewayMessageKind(msg.Type) != thirdPartyGatewayMessageVoice {
		return true
	}
	if strings.TrimSpace(msg.Data) == "" {
		return strings.TrimSpace(msg.URL) != "" && capabilities.SupportsOutputAudioDelivery("url", msg.SizeBytes)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(msg.Data))
	if err != nil || len(data) == 0 {
		return false
	}
	msg.SizeBytes = int64(len(data))
	if capabilities.SupportsOutputAudioDelivery("inline", msg.SizeBytes) {
		return true
	}
	if !capabilities.SupportsOutputAudioDelivery("url", msg.SizeBytes) || msg.SizeBytes > thirdPartyMaxMediaBytes {
		return false
	}
	id, err := randomThirdPartyMediaToken()
	if err != nil {
		return false
	}
	token, err := randomThirdPartyMediaToken()
	if err != nil {
		return false
	}
	fileName := coreim.SafeThirdPartyFileName(msg.FileName)
	if fileName == "" {
		fileName = "response.wav"
	}
	mimeType := outgoingThirdPartyMIME(*msg)
	if mimeType == "" {
		mimeType = "audio/wav"
	}
	m.pruneMediaLocked(time.Now().UTC())
	m.media[id] = &thirdPartyMediaObject{
		ClientID: clientID, ID: id, Token: token, Type: "audio", FileName: fileName,
		MimeType: mimeType, SizeBytes: msg.SizeBytes, Data: data, Uploaded: true,
		CreatedAt: time.Now().UTC(), LastAccessedAt: time.Now().UTC(),
	}
	msg.URL = fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", id, token)
	msg.Data = ""
	msg.MimeType = mimeType
	return true
}

func (m *thirdPartyGatewayManager) preparePetAssetLocked(clientID string, asset devicePetAsset, animated bool) map[string]any {
	if asset.Encoding != "rgb565le" || asset.Width < 32 || asset.Width > 128 ||
		asset.Height < 32 || asset.Height > 128 || asset.Data == "" {
		return nil
	}
	encoded := []string{asset.Data}
	if animated && len(asset.Frames) > 0 {
		encoded = append(encoded, asset.Frames[0])
	}
	expected := asset.Width * asset.Height * 2
	frames := make([][]byte, 0, len(encoded))
	for _, text := range encoded {
		frame, err := base64.StdEncoding.DecodeString(text)
		if err != nil || len(frame) != expected {
			return nil
		}
		frames = append(frames, frame)
	}
	digest := sha256.New()
	for _, frame := range frames {
		_, _ = digest.Write(frame)
	}
	revision := hex.EncodeToString(digest.Sum(nil)[:8])
	urls := make([]string, 0, len(frames))
	for index, frame := range frames {
		id, err := randomThirdPartyMediaToken()
		if err != nil {
			return nil
		}
		token, err := randomThirdPartyMediaToken()
		if err != nil {
			return nil
		}
		m.pruneMediaLocked(time.Now().UTC())
		m.media[id] = &thirdPartyMediaObject{
			ClientID: clientID, ID: id, Token: token, Type: "pet_asset",
			FileName: fmt.Sprintf("pet-%s-%d.rgb565le", revision, index),
			MimeType: "application/vnd.maclaw.rgb565le", SizeBytes: int64(len(frame)),
			Data: frame, Uploaded: true, CreatedAt: time.Now().UTC(), LastAccessedAt: time.Now().UTC(),
		}
		urls = append(urls, fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", id, token))
	}
	return map[string]any{"encoding": asset.Encoding, "width": asset.Width, "height": asset.Height,
		"urls": urls, "frameMs": 700, "revision": revision}
}

// broadcastHardwareConfig sends a small settings message to every paired
// client.  It bypasses regular answer modality selection because controls such
// as speaker volume are negotiated through client feature capabilities rather
// than a text/audio output modality.
func (m *thirdPartyGatewayManager) broadcastHardwareConfig(extra map[string]any) {
	if m == nil || len(extra) == 0 {
		return
	}
	m.mu.Lock()
	for _, state := range m.clients {
		capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
		if !capabilities.Features.VolumeControl {
			continue
		}
		if volume, ok := hardwareConfigVolume(extra); ok && replaceQueuedThirdPartyVolume(state, volume) {
			continue
		}
		seq := state.NextSeq
		state.NextSeq++
		state.Messages = append(state.Messages, thirdPartyOutgoingMessage{
			ID:        fmt.Sprintf("mc_hardware_%d_%06d", time.Now().UnixMilli(), seq),
			Seq:       seq,
			Type:      "hardware_config",
			CreatedAt: time.Now().UnixMilli(),
			Extra:     extra,
		})
		pruneThirdPartyMessagesLocked(state)
	}
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
}

// broadcastPetProfile pushes a GUI pet settings change to every paired client.
// Like broadcastHardwareConfig it bypasses enqueue/adaptation: pet_profile is a
// control-plane message, not a text reply, and the adapt layer would otherwise
// coerce the unknown type to text. RGB565 frames are published through small
// media references only for clients that negotiated pet assets; everyone receives the
// message-level pet_skin/pet_motion_enabled fields the firmware parses.
func (m *thirdPartyGatewayManager) broadcastPetProfile(profile map[string]any) {
	if m == nil || len(profile) == 0 {
		return
	}
	skin, hasSkin := profile["skin"].(string)
	// Only a present, well-typed motionEnabled key is broadcast: a caller that
	// omits the key must not have its zero value mistaken for an explicit
	// "disable motion" update, so the pointer stays nil and the JSON field is
	// omitted entirely.
	var motionEnabled *bool
	if value, ok := profile["motionEnabled"].(bool); ok {
		motionEnabled = &value
	}
	m.mu.Lock()
	for clientID, state := range m.clients {
		var petExtra map[string]any
		capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
		if capabilities.Features.PetAsset {
			if asset, ok := profile["asset"].(devicePetAsset); ok && (asset.Data != "" || len(asset.Frames) > 0) {
				if ref := m.preparePetAssetLocked(clientID, asset, capabilities.Features.PetAnimation && motionEnabled != nil && *motionEnabled); ref != nil {
					petExtra = map[string]any{"pet_asset": ref}
				}
			}
		}
		if replaceQueuedThirdPartyPetProfile(state, skin, hasSkin, motionEnabled, petExtra) {
			continue
		}
		seq := state.NextSeq
		state.NextSeq++
		msg := thirdPartyOutgoingMessage{
			ID:               fmt.Sprintf("mc_pet_%d_%06d", time.Now().UnixMilli(), seq),
			Seq:              seq,
			Type:             "pet_profile",
			PetSkin:          skin,
			PetMotionEnabled: motionEnabled,
			CreatedAt:        time.Now().UnixMilli(),
			Extra:            petExtra,
		}
		state.Messages = append(state.Messages, msg)
		pruneThirdPartyMessagesLocked(state)
	}
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
}

func hardwareConfigVolume(extra map[string]any) (int, bool) {
	if extra == nil {
		return 0, false
	}
	volume, ok := extra["volume"].(int)
	return volume, ok && volume >= 0 && volume <= 100
}

func (m *thirdPartyGatewayManager) enqueueHardwareConfigForClient(clientID string, volume int) {
	if m == nil || volume < 0 || volume > 100 {
		return
	}
	m.mu.Lock()
	state := m.ensureClientLocked(clientID)
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	if capabilities.Features.VolumeControl && !replaceQueuedThirdPartyVolume(state, volume) {
		seq := state.NextSeq
		state.NextSeq++
		state.Messages = append(state.Messages, thirdPartyOutgoingMessage{
			ID:        fmt.Sprintf("mc_hardware_%d_%06d", time.Now().UnixMilli(), seq),
			Seq:       seq,
			Type:      "hardware_config",
			CreatedAt: time.Now().UnixMilli(),
			Extra:     map[string]any{"volume": volume},
		})
		pruneThirdPartyMessagesLocked(state)
		old := m.notifyCh
		m.notifyCh = make(chan struct{})
		close(old)
	}
	m.mu.Unlock()
}

// replaceQueuedThirdPartyVolume keeps one pending latest-wins control message.
// It prevents a burst of slider updates or handshake retries from replaying an
// old volume after the user has selected a newer one.
func replaceQueuedThirdPartyVolume(state *thirdPartyClientState, volume int) bool {
	if state == nil {
		return false
	}
	for index := len(state.Messages) - 1; index >= 0; index-- {
		message := &state.Messages[index]
		if message.Type != "hardware_config" || message.Extra == nil {
			continue
		}
		message.Extra = map[string]any{"volume": volume}
		return true
	}
	return false
}

// replaceQueuedThirdPartyPetProfile keeps one pending latest-wins pet_profile
// message per client: while a device is offline, repeated skin changes would
// otherwise pile up N messages whose pet_asset payload runs ~85KB each. The
// newest queued, still-unacked pet_profile is rewritten in place. The acked
// check is defensive: pruneThirdPartyMessagesLocked drops acked entries on
// every ack, so the branch is unreachable in production — but if an acked
// message ever lingered, rewriting it would silently drop the update because
// messagesAfter never resends acked entries. A nil motionEnabled or
// hasSkin == false means the caller omitted that key, so the queued value is
// preserved rather than reset to the zero value. Note the inherent
// latest-wins race: if the device has already fetched the old value and its
// ack is still in flight, this rewrite lands under the pending ack and gets
// pruned away — the same accepted trade-off as replaceQueuedThirdPartyVolume.
func replaceQueuedThirdPartyPetProfile(state *thirdPartyClientState, skin string, hasSkin bool, motionEnabled *bool, extra map[string]any) bool {
	if state == nil {
		return false
	}
	for index := len(state.Messages) - 1; index >= 0; index-- {
		message := &state.Messages[index]
		if message.Type != "pet_profile" || state.Acked[message.ID] != "" {
			continue
		}
		if hasSkin {
			message.PetSkin = skin
		}
		if motionEnabled != nil {
			message.PetMotionEnabled = motionEnabled
		}
		message.Extra = extra
		return true
	}
	return false
}

func (m *thirdPartyGatewayManager) broadcastHardwareAudio(payload map[string]any) {
	if m == nil || len(payload) == 0 {
		return
	}
	data, _ := payload["file_data"].(string)
	m.mu.Lock()
	for clientID := range m.clients {
		m.enqueueHardwareAudioForClientLocked(clientID, data)
	}
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
}

func (m *thirdPartyGatewayManager) enqueueHardwareAudioForClient(clientID, data string) {
	m.mu.Lock()
	m.enqueueHardwareAudioForClientLocked(clientID, data)
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
}

func (m *thirdPartyGatewayManager) enqueueHardwareAudioForBoot(clientID, bootSessionID, data string) {
	m.mu.Lock()
	state := m.ensureClientLocked(clientID)
	if bootSessionID == "" || state.LastWelcomeBootID == bootSessionID || !m.enqueueHardwareAudioForClientLocked(clientID, data) {
		m.mu.Unlock()
		return
	}
	// Bind the queued greeting to this boot. A client can reject a delayed
	// greeting after reconnecting or after the desktop gateway restarts.
	state.Messages[len(state.Messages)-1].Extra = map[string]any{"bootSessionId": bootSessionID}
	state.LastWelcomeBootID = bootSessionID
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
}

func (m *thirdPartyGatewayManager) enqueueHardwareAudioForClientLocked(clientID, data string) bool {
	state := m.ensureClientLocked(clientID)
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	if data == "" || !capabilities.SupportsOutput("audio") || capabilities.Output.Audio == nil || !capabilities.Output.Audio.Playback || !capabilities.SupportsOutputMIME("audio", "audio/wav") {
		return false
	}
	message := thirdPartyOutgoingMessage{Type: "audio", MimeType: "audio/wav", FileName: "welcome.wav", Data: data, CreatedAt: time.Now().UnixMilli()}
	if !m.prepareOutgoingAudioLocked(clientID, &message, capabilities) {
		return false
	}
	seq := state.NextSeq
	state.NextSeq++
	message.ID = fmt.Sprintf("mc_welcome_%d_%06d", time.Now().UnixMilli(), seq)
	message.Seq = seq
	state.Messages = append(state.Messages, message)
	pruneThirdPartyMessagesLocked(state)
	return true
}

// pruneThirdPartyMessagesLocked bounds history without allowing delivered
// entries to evict control messages a device has not acknowledged yet. Cursor
// progression stays monotonic, while pruning acknowledged entries keeps a
// frequently connected device from accumulating a full history in memory.
func pruneThirdPartyMessagesLocked(state *thirdPartyClientState) {
	if state == nil || len(state.Messages) == 0 {
		return
	}
	kept := state.Messages[:0]
	for _, message := range state.Messages {
		if state.Acked[message.ID] != "" {
			delete(state.Acked, message.ID)
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) > thirdPartyHistoryLimit {
		kept = kept[len(kept)-thirdPartyHistoryLimit:]
	}
	state.Messages = kept
}

func adaptGUIThirdPartyOutgoingMessage(msg *thirdPartyOutgoingMessage, capabilities agent.ClientCapabilities) bool {
	if msg == nil {
		return false
	}
	switch normalizeThirdPartyGatewayMessageKind(msg.Type) {
	case thirdPartyGatewayMessageImage:
		return capabilities.SupportsOutputMIME("image", outgoingThirdPartyMIME(*msg))
	case thirdPartyGatewayMessageFile:
		return capabilities.SupportsOutputMIME("file", outgoingThirdPartyMIME(*msg)) && capabilities.SupportsOutputBytes("file", msg.SizeBytes)
	case thirdPartyGatewayMessageVoice:
		delivery := "inline"
		if strings.TrimSpace(msg.URL) != "" {
			delivery = "url"
		}
		return capabilities.SupportsOutput("audio") && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback &&
			capabilities.SupportsOutputMIME("audio", outgoingThirdPartyMIME(*msg)) &&
			capabilities.SupportsOutputAudioDelivery(delivery, msg.SizeBytes)
	case thirdPartyGatewayMessageAmbient:
		return capabilities.Features.AmbientDisplay
	case thirdPartyGatewayMessagePetState:
		return capabilities.Features.PetStates
	case thirdPartyGatewayMessageMeetingResult:
		if !capabilities.Features.MeetingRecorder || !capabilities.SupportsOutput("text") {
			return false
		}
		if capabilities.Output.Text != nil && capabilities.Output.Text.MaxChars > 0 {
			msg.Text = truncateThirdPartyOutputText(msg.Text, capabilities.Output.Text.MaxChars)
		}
		return true
	default:
		if !capabilities.SupportsOutput("text") {
			return false
		}
		msg.Type = thirdPartyGatewayMessageText.String()
		if capabilities.Output.Text != nil && capabilities.Output.Text.MaxChars > 0 {
			msg.Text = truncateThirdPartyOutputText(msg.Text, capabilities.Output.Text.MaxChars)
		}
		return strings.TrimSpace(msg.Text) != ""
	}
}

func outgoingThirdPartyMIME(msg thirdPartyOutgoingMessage) string {
	if strings.TrimSpace(msg.MimeType) != "" {
		return msg.MimeType
	}
	return msg.ContentType
}

func truncateThirdPartyOutputText(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}
func (m *thirdPartyGatewayManager) processIncoming(req thirdPartyIncomingRequest, maclawID string) {
	started := time.Now()
	log.Printf("[thirdparty-mgr] processing start client=%s event=%s maclawID=%s type=%s",
		req.ClientID, req.EventID, maclawID, req.Message.Type)
	if isPassthroughSlashText(req.Message.Text) {
		log.Printf("[thirdparty-mgr] routing passthrough command locally: client=%s conversation=%s", req.ClientID, req.ConversationID)
		m.handleLocalMessage(req, maclawID)
		return
	}
	cfg, err := m.app.LoadConfig()
	if err != nil {
		m.enqueueError(req, maclawID, "bad_request", err.Error())
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
				// The HTTP acceptance response exposes maclawID as the canonical
				// correlation key. Preserve it across the Hub relay so the ESP32
				// waits for the same id that the final answer carries.
				"message_id":          maclawID,
				"event_id":            req.EventID,
				"user_id":             req.User.ID,
				"user_name":           req.User.Name,
				"client_capabilities": m.clientCapabilities(req.ClientID),
			}
			if req.Extra != nil {
				payload["extra"] = req.Extra
			}
			if err := hubClient.SendIMGatewayMessage(imGatewayPlatformThirdParty, payload); err == nil {
				log.Printf("[thirdparty-mgr] forwarded to Hub client=%s maclawID=%s elapsed=%s",
					req.ClientID, maclawID, time.Since(started).Round(time.Millisecond))
				return
			}
			log.Printf("[thirdparty-mgr] forwardToHub error: %v, falling back to local", err)
		}
	}
	m.handleLocalMessage(req, maclawID)
	log.Printf("[thirdparty-mgr] local processing complete client=%s maclawID=%s elapsed=%s",
		req.ClientID, maclawID, time.Since(started).Round(time.Millisecond))
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
	// Hardware requests may ask the Agent to send a generated file to a separate
	// IM group/user. Wire the same structured sender used by the desktop handler;
	// the ESP remains an input/display endpoint and never executes the send.
	h.SetStructuredIMFileSender(func(req agent.IMFileDeliveryRequest) error {
		return a.forwardDesktopFileToIMRequest(a.hubClient(), req)
	})
	a.ensureConversationArchiver()
	if a.conversationArchiver != nil {
		h.memory.Archiver = a.conversationArchiver
	}

	m.localHandler = h
	log.Printf("[thirdparty-mgr] local IMMessageHandler created")
	return h
}

func (m *thirdPartyGatewayManager) handleLocalMessage(req thirdPartyIncomingRequest, maclawID string) {
	started := time.Now()
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
			ReplyToMessageID: maclawID,
			Type:             thirdPartyGatewayMessageText.String(),
			Text:             reply,
			Metadata:         map[string]string{"acp_turn": "final"},
		})
		return
	}
	if !m.app.isMaclawLLMConfigured() {
		m.enqueue(req.ClientID, thirdPartyOutgoingMessage{
			ConversationID:   req.ConversationID,
			ReplyToMessageID: maclawID,
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
			m.enqueueError(req, maclawID, "bad_request", err.Error())
			return
		}
		if messageKind == thirdPartyGatewayMessageImage {
			attachments = append(attachments, buildLocalImageAttachment(mediaData, mediaName, mediaMime))
		} else {
			mediaPath, err := saveMediaToTempDir("thirdparty", "tp_", safeFileToken(req.User.ID), messageKind.String(), mediaData, mediaName)
			if err != nil {
				m.enqueueError(req, maclawID, "bad_request", err.Error())
				return
			}
			prefix := "[media " + mediaLabel(messageKind.String()) + ": " + mediaPath + "]\n"
			text = prefix + text
		}
	}
	if text == "" && len(attachments) == 0 {
		m.enqueueError(req, maclawID, "empty_input", "没有识别到可处理的语音或文字内容")
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
			ReplyToMessageID: maclawID,
			Type:             "text",
			Text:             i18n.T(i18n.MsgProgressPrefix, appUILang(m.app)) + stripped,
			Progress:         true,
		})
	}

	resp := handler.HandleIMMessageWithProgress(IMUserMessage{
		UserID:             thirdPartySessionUserID(req.ClientID, req.ConversationID),
		Platform:           thirdPartyPlatform(req.ClientID),
		Text:               text,
		Lang:               appUILang(m.app),
		Attachments:        attachments,
		ClientCapabilities: pointerToClientCapabilities(m.clientCapabilities(req.ClientID)),
	}, onProgress)
	log.Printf("[thirdparty-mgr] agent returned client=%s maclawID=%s deferred=%t hasResponse=%t elapsed=%s",
		req.ClientID, maclawID, resp != nil && resp.Deferred, resp != nil,
		time.Since(started).Round(time.Millisecond))
	if resp == nil || resp.Deferred {
		return
	}
	m.enqueueAgentResponse(req.ClientID, req.ConversationID, maclawID, resp)
}

func (m *thirdPartyGatewayManager) enqueueAgentResponse(clientID, conversationID, replyTo string, resp *IMAgentResponse) {
	capabilities := m.clientCapabilities(clientID)
	selected := agent.SelectClientOutputCombination(capabilities, agentResponseModalities(resp, capabilities)...)
	allow := func(modality string) bool { return containsString(selected, modality) }
	// A client declaring modalities [text,audio] with only singleton
	// combinations [[text],[audio]] resolves to ["audio"] alone; without this
	// fallback the text reply would be silently dropped (a regression for old
	// firmware that expects text alongside voice). Single-modality wins that
	// do not involve audio (e.g. image) still suppress text as declared.
	allowText := allow("text") || (containsString(selected, "audio") && capabilities.SupportsOutput("text"))
	enqueued := false
	textTerminalExpected := allowText && (resp.Text != "" || resp.Error != "" || len(resp.Actions) > 0)
	// Voice goes first: ESP32 firmware treats an incoming text message as the
	// end of the current command, so a voice reply arriving after the text
	// would be dropped as an unrelated message.
	if resp.VoiceData != "" && allow("audio") && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback && capabilities.SupportsOutputMIME("audio", resp.VoiceMimeType) {
		// Mark enqueued only when the message was actually queued: enqueue
		// silently drops audio that fails preparation, and a premature true
		// here would suppress the "(no output)" terminal fallback below.
		audioMetadata := map[string]string{}
		if !textTerminalExpected {
			audioMetadata["acp_turn"] = "final"
		}
		if queued := m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: resp.VoiceMimeType, FileName: resp.VoiceFileName, Data: resp.VoiceData, Metadata: audioMetadata}); queued.ID != "" {
			enqueued = true
		}
	}
	if resp.Text != "" && allowText {
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
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: map[string]string{"acp_turn": "final"}})
		enqueued = true
	} else if len(resp.Actions) > 0 && allowText {
		text := "Please reply with an option:"
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: map[string]string{"acp_turn": "final"}})
		enqueued = true
	}
	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 && allowText {
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "error", Error: "agent_error", Text: textutil.StripMarkdown(resp.Error), Metadata: map[string]string{"acp_turn": "final"}})
		enqueued = true
	}
	if resp.ImageKey != "" && allow("image") && capabilities.SupportsOutputMIME("image", "image/png") {
		if !enqueued {
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image", ContentType: "image/png", FileName: "image.png", Data: resp.ImageKey, Metadata: map[string]string{"acp_turn": "final"}})
	}
	if resp.FileData != "" && allow("file") && capabilities.SupportsOutputMIME("file", resp.FileMimeType) {
		decoded, decodeErr := base64.StdEncoding.DecodeString(resp.FileData)
		if decodeErr == nil && capabilities.SupportsOutputBytes("file", int64(len(decoded))) {
			if !enqueued {
				enqueued = true
			}
			m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: resp.FileMimeType, FileName: resp.FileName, Data: resp.FileData, SizeBytes: int64(len(decoded)), Metadata: map[string]string{"acp_turn": "final"}})
		}
	}
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, p := range paths {
		if !allow("file") || !capabilities.SupportsOutput("file") {
			break
		}
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

		if !capabilities.SupportsOutputMIME("file", ct) || !capabilities.SupportsOutputBytes("file", int64(len(data))) {
			continue
		}
		if !enqueued {
			enqueued = true
		}
		m.enqueue(clientID, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: ct, FileName: name, Data: base64.StdEncoding.EncodeToString(data), SizeBytes: int64(len(data)), Metadata: map[string]string{"acp_turn": "final"}})
	}
	// Ensure ACP bridge always sees one terminal marker even for empty results.
	if !enqueued && capabilities.SupportsOutput("text") {
		m.enqueue(clientID, thirdPartyOutgoingMessage{
			ConversationID: conversationID, ReplyToMessageID: replyTo,
			Type: "text", Text: "(no output)", Metadata: map[string]string{"acp_turn": "final"},
		})
	}
}

func agentResponseModalities(resp *IMAgentResponse, capabilities agent.ClientCapabilities) []string {
	modalities := make([]string, 0, 4)
	if resp == nil {
		return modalities
	}
	if resp.Text != "" || resp.Error != "" || len(resp.Actions) > 0 {
		modalities = append(modalities, "text")
	}
	if resp.ImageKey != "" && capabilities.SupportsOutputMIME("image", "image/png") {
		modalities = append(modalities, "image")
	}
	filePresent := false
	if resp.FileData != "" && capabilities.SupportsOutputMIME("file", resp.FileMimeType) {
		if decoded, err := base64.StdEncoding.DecodeString(resp.FileData); err == nil && capabilities.SupportsOutputBytes("file", int64(len(decoded))) {
			filePresent = true
		}
	}
	paths := resp.LocalFilePaths
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append([]string{resp.LocalFilePath}, paths...)
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		mimeType := mime.TypeByExtension(filepath.Ext(path))
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		if capabilities.SupportsOutputMIME("file", mimeType) && capabilities.SupportsOutputBytes("file", info.Size()) {
			filePresent = true
			break
		}
	}
	if filePresent {
		modalities = append(modalities, "file")
	}
	if resp.VoiceData != "" && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback && capabilities.SupportsOutputMIME("audio", resp.VoiceMimeType) {
		modalities = append(modalities, "audio")
	}
	return modalities
}
func pointerToClientCapabilities(capabilities agent.ClientCapabilities) *agent.ClientCapabilities {
	return &capabilities
}

func (m *thirdPartyGatewayManager) HandleGatewayReply(reply GatewayReplyPayload) {
	started := time.Now()
	clientID, conversationID := parseThirdPartyReplyTarget(reply)
	if clientID == "" {
		log.Printf("[thirdparty-mgr] hub reply missing client id")
		return
	}
	if m.effectiveMode() == "hub" {
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			if err := hubClient.SendDeviceGatewayReply(clientID, conversationID, reply); err == nil {
				return
			}
		}
	}
	msg := thirdPartyOutgoingMessage{
		ConversationID:   conversationID,
		ReplyToMessageID: thirdPartyReplyCorrelation(reply),
		Type:             reply.ReplyType.String(),
		Text:             reply.Text,
		Caption:          reply.Caption,
		FileName:         reply.FileName,
		ContentType:      reply.MimeType,
		// Deferred hub/async replies complete an ACP bridge turn.
		Metadata: map[string]string{"acp_turn": "final"},
	}
	log.Printf("[thirdparty-mgr] Hub reply received client=%s sourceMessageID=%s correlatedReplyTo=%s type=%s",
		clientID, reply.SourceMessageID, msg.ReplyToMessageID, reply.ReplyType.String())
	// The local gateway takes the same direct path as the Hub relay.  Attach
	// the compact glyph atlas before the message enters the ESP queue, so CJK
	// reply text cannot degrade into question-mark placeholders.
	if glyphs := deviceGlyphsForText(reply.Text, reply.Caption); len(glyphs) > 0 {
		msg.Glyphs = glyphs
	}
	replyKind := normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String())
	capabilities := m.clientCapabilities(clientID)
	switch {
	case replyKind == thirdPartyGatewayMessageImage && capabilities.SupportsOutput("image"):
		msg.Data = reply.ImageData
		if msg.ContentType == "" {
			msg.ContentType = "image/png"
		}
	case replyKind.IsMediaFile() && capabilities.SupportsOutput("file"):
		msg.Data = reply.FileData
	default:
		if !capabilities.SupportsOutput("text") {
			return
		}
		msg.Type = thirdPartyGatewayMessageText.String()
		msg.Data, msg.FileName, msg.ContentType = "", "", ""
	}
	m.enqueue(clientID, msg)
	log.Printf("[thirdparty-mgr] Hub reply delivered client=%s replyTo=%s elapsed=%s",
		clientID, msg.ReplyToMessageID, time.Since(started).Round(time.Millisecond))
}

func thirdPartyReplyCorrelation(reply GatewayReplyPayload) string {
	if sourceID := strings.TrimSpace(reply.SourceMessageID); sourceID != "" {
		return sourceID
	}
	for _, key := range []string{"replyTo", "replyToMessageId", "source_message_id", "sourceMessageId", "sourceMessageID"} {
		if value, ok := reply.Extra[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
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

// SendHardwareVolume persists and immediately sends the requested speaker
// level to every currently connected Macaron-style client.
func (a *App) SendHardwareVolume(volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("hardware volume must be between 0 and 100")
	}
	if _, err := a.PatchConfigFields(map[string]interface{}{"hardware_volume": volume}); err != nil {
		return err
	}
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.broadcastHardwareConfig(map[string]any{"volume": volume})
	}
	// PatchConfigFields synchronizes Hub mode after committing the same value.
	// Keeping the relay there also covers non-UI callers that patch volume
	// directly and prevents this public method from sending duplicates.
	return nil
}

// CreateThirdPartyDevicePairing produces a six-digit, thirty-minute bootstrap
// credential for a LAN device. The bearer itself never needs to be typed on
// the ESP32.
func (a *App) CreateThirdPartyDevicePairing() (map[string]any, error) {
	a.ensureThirdPartyGateway()
	if a.thirdPartyGateway == nil {
		return nil, fmt.Errorf("enable third-party access before pairing a device")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
	if !cfg.ThirdPartyGatewayEnabled || token == "" {
		return nil, fmt.Errorf("third-party gateway is not enabled")
	}
	expiresAt := time.Now().Add(30 * time.Minute)
	m := a.thirdPartyGateway
	m.mu.Lock()
	now := time.Now()
	for candidate, pairing := range m.pairings {
		if !pairing.ExpiresAt.After(now) {
			delete(m.pairings, candidate)
		}
	}
	if len(m.pairings) >= thirdPartyMaxPendingPairings {
		m.mu.Unlock()
		return nil, fmt.Errorf("too many pending device pairings; wait for an existing code to expire")
	}
	var code string
	for attempt := 0; attempt < 16; attempt++ {
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			m.mu.Unlock()
			return nil, err
		}
		candidate := fmt.Sprintf("%06d", (uint32(random[0])<<24|uint32(random[1])<<16|uint32(random[2])<<8|uint32(random[3]))%1000000)
		if _, exists := m.pairings[candidate]; !exists {
			code = candidate
			break
		}
	}
	if code == "" {
		m.mu.Unlock()
		return nil, fmt.Errorf("cannot allocate a unique device pairing code; try again")
	}
	if !cfg.IsThirdPartyGatewayLocalMode() {
		hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
		if hubURL == "" {
			m.mu.Unlock()
			return nil, fmt.Errorf("Hub URL is not configured")
		}
		// Reserve the code locally while Hub owns the actual pairing. This prevents
		// concurrent requests from choosing the same six-digit code without making
		// it exchangeable at the local endpoint.
		m.pairings[code] = thirdPartyDevicePairing{ExpiresAt: expiresAt, Remote: true}
		// Release the local gateway lock before WebSocket I/O so device requests
		// are never stalled by Hub.
		m.mu.Unlock()
		hubClient := a.hubClient()
		if hubClient == nil || !hubClient.IsConnected() {
			m.removeRemotePairingReservation(code)
			return nil, fmt.Errorf("Hub mode requires the GUI to be connected to Hub")
		}
		if err := hubClient.SendDeviceGatewayPairing(code); err != nil {
			m.removeRemotePairingReservation(code)
			return nil, err
		}
		result := map[string]any{"pairCode": code, "expiresAt": expiresAt.Format(time.RFC3339), "transport": "hub", "gatewayURL": hubURL}
		return result, nil
	} else {
		m.pairings[code] = thirdPartyDevicePairing{Token: token, ExpiresAt: expiresAt}
		m.mu.Unlock()
		host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
		if host == "" {
			host = thirdPartyDefaultHost
		}
		port := cfg.ThirdPartyGatewayPort
		if port <= 0 {
			port = thirdPartyDefaultPort
		}
		return map[string]any{
			"pairCode": code, "expiresAt": expiresAt.Format(time.RFC3339),
			"transport": "local", "gatewayURL": fmt.Sprintf("http://%s:%d", host, port),
		}, nil
	}
}

func (m *thirdPartyGatewayManager) removeRemotePairingReservation(code string) {
	if m == nil || code == "" {
		return
	}
	m.mu.Lock()
	if pairing, exists := m.pairings[code]; exists && pairing.Remote {
		delete(m.pairings, code)
	}
	m.mu.Unlock()
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
