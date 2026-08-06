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
	"github.com/RapidAI/CodeClaw/corelib/tts"
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

	mu          sync.Mutex
	server      *http.Server
	listener    net.Listener
	status      gatewayConnectionStatus
	lastBindKey string
	// localHandler remains for legacy embedding activation probes. Requests use
	// localHandlers, which owns a runtime per physical hardware binding.
	localHandler  *IMMessageHandler
	localHandlers *hardwareAgentRuntimeRegistry
	clients       map[string]*thirdPartyClientState
	notifyCh      chan struct{}
	media         map[string]*thirdPartyMediaObject
	pairings      map[string]thirdPartyDevicePairing
	speechMu      sync.Mutex
	speechTurns   map[string]*deviceSpeechTurn
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
	if m.localHandlers == nil {
		return nil
	}
	return m.localHandlers.onlyHandler()
}

func (m *thirdPartyGatewayManager) localHardwareHandlers() []*IMMessageHandler {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	runtimes := m.localHandlers
	m.mu.Unlock()
	return runtimes.handlers()
}

type thirdPartyClientState struct {
	NextSeq            int64
	Messages           []thirdPartyOutgoingMessage
	SeenEvents         map[string]string
	Acked              map[string]string
	ClientCapabilities agent.ClientCapabilities
	ClientTools        []agent.ClientToolDefinition
	LastWelcomeBootID  string
}

type deviceSpeechTurn struct {
	cancel   context.CancelFunc
	replyTo  string
	expected int
	queued   int
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

func responseDeviceFileAudioCount(resp *IMAgentResponse, capabilities agent.ClientCapabilities) int {
	if resp == nil {
		return 0
	}
	count := 0
	if resp.FileData != "" {
		kind, mimeType := classifyDeviceResponseFile(resp.FileName, resp.FileMimeType)
		if data, err := base64.StdEncoding.DecodeString(resp.FileData); kind == deviceResponseFileAudio && err == nil && clientCanPlayDeviceAudio(capabilities, mimeType, int64(len(data))) {
			count++
		}
	}
	paths := append([]string(nil), resp.LocalFilePaths...)
	if resp.LocalFilePath != "" && !containsString(paths, resp.LocalFilePath) {
		paths = append(paths, resp.LocalFilePath)
	}
	for _, path := range paths {
		kind, mimeType := classifyDeviceResponseFile(path, mime.TypeByExtension(filepath.Ext(path)))
		if info, err := os.Stat(path); kind == deviceResponseFileAudio && err == nil && info.Mode().IsRegular() && clientCanPlayDeviceAudio(capabilities, mimeType, info.Size()) {
			count++
		}
	}
	return count
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
		app:         app,
		status:      gatewayConnectionStatusDisconnected,
		clients:     make(map[string]*thirdPartyClientState),
		notifyCh:    make(chan struct{}),
		media:       make(map[string]*thirdPartyMediaObject),
		pairings:    make(map[string]thirdPartyDevicePairing),
		speechTurns: make(map[string]*deviceSpeechTurn),
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
		lh := m.localHandlers
		m.localHandlers = nil
		m.mu.Unlock()
		if lh != nil {
			lh.stopAll()
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
		// The listener may have survived a Hub reconnect or a local/Hub-mode
		// switch. Reconcile its remote claim even when no socket restart is
		// required; otherwise Hub can retain a stale route indefinitely.
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			if cfg.IsThirdPartyGatewayLocalMode() {
				_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformThirdParty)
			} else {
				_ = hubClient.SendIMGatewayClaim(imGatewayPlatformThirdParty)
			}
		}
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

// stop closes the local listener. A full stop releases the Hub claim; a
// restart deliberately preserves it so paired hardware has no routing gap.
func (m *thirdPartyGatewayManager) stop(unclaim bool) {
	m.cancelDeviceSpeechTurns()
	m.mu.Lock()
	server := m.server
	m.server = nil
	m.listener = nil
	m.status = gatewayConnectionStatusDisconnected
	m.lastBindKey = ""
	lh := m.localHandlers
	m.localHandlers = nil
	m.mu.Unlock()
	if lh != nil {
		lh.stopAll()
	}
	if server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = server.Shutdown(ctx)
		cancel()
	}
	if unclaim && m.app != nil {
		if hubClient := m.app.hubClient(); hubClient != nil && hubClient.IsConnected() {
			_ = hubClient.SendIMGatewayUnclaim(imGatewayPlatformThirdParty)
		}
	}
	m.emitStatusEvent()
}

func (m *thirdPartyGatewayManager) Stop() { m.stop(true) }

func (m *thirdPartyGatewayManager) stopForRestart() { m.stop(false) }

func (m *thirdPartyGatewayManager) cancelDeviceSpeechTurns() {
	if m == nil {
		return
	}
	m.speechMu.Lock()
	turns := m.speechTurns
	m.speechTurns = make(map[string]*deviceSpeechTurn)
	m.speechMu.Unlock()
	for _, turn := range turns {
		if turn != nil && turn.cancel != nil {
			turn.cancel()
		}
	}
}

func (m *thirdPartyGatewayManager) cancelDeviceSpeechTurn(clientID, conversationID string) {
	if m == nil {
		return
	}
	turnKey := clientID + "\x00" + conversationID
	m.speechMu.Lock()
	turn := m.speechTurns[turnKey]
	delete(m.speechTurns, turnKey)
	m.speechMu.Unlock()
	if turn != nil && turn.cancel != nil {
		turn.cancel()
	}
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
	registry := m.localHandlers
	m.localHandlers = nil
	m.mu.Unlock()
	if registry != nil {
		registry.stopAll()
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
	m.setClientCapabilities(req.ClientID, req.ClientCapabilities)
	m.setClientTools(req.ClientID, req.Tools)
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
	// The local gateway can render the selected desktop pack directly. Publish
	// transparent high-resolution frames through media URLs so the ESP shows the
	// same animated pet without embedding megabytes of base64 in the handshake.
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
					req.ClientCapabilities.Features.PetAnimation && profile["motionEnabled"] == true,
					req.ClientCapabilities.Features.PetAssetMaxFrames)
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
	if m == nil || m.app == nil {
		return "local"
	}
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

func (m *thirdPartyGatewayManager) setClientTools(clientID string, tools []agent.ClientToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	state.ClientTools = append([]agent.ClientToolDefinition(nil), tools...)
}

func (m *thirdPartyGatewayManager) clientTools(clientID string) []agent.ClientToolDefinition {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.ensureClientLocked(clientID)
	return append([]agent.ClientToolDefinition(nil), state.ClientTools...)
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
	queued := m.enqueueBatch(clientID, []thirdPartyOutgoingMessage{msg}, false)
	if len(queued) == 0 {
		return thirdPartyOutgoingMessage{}
	}
	return queued[0]
}

// enqueueBatch prepares and publishes a logical response atomically. When
// terminalLast is set, exactly the last message that survives capability and
// media validation closes the ACP turn. This avoids exposing an intermediate
// text/image/file frame as terminal while later frames are still queued.
func (m *thirdPartyGatewayManager) enqueueBatch(clientID string, messages []thirdPartyOutgoingMessage, terminalLast bool) []thirdPartyOutgoingMessage {
	started := time.Now()
	m.mu.Lock()
	state := m.ensureClientLocked(clientID)
	capabilities := agent.NormalizeClientCapabilities(&state.ClientCapabilities)
	prepared := make([]thirdPartyOutgoingMessage, 0, len(messages))
	for _, msg := range messages {
		if !m.prepareOutgoingAudioLocked(clientID, &msg, capabilities) {
			continue
		}
		if !adaptGUIThirdPartyOutgoingMessage(&msg, capabilities) {
			continue
		}
		prepared = append(prepared, msg)
	}
	if len(prepared) == 0 {
		m.mu.Unlock()
		return nil
	}
	if terminalLast {
		for index := range prepared {
			delete(prepared[index].Metadata, "acp_turn")
		}
		last := len(prepared) - 1
		if prepared[last].Metadata == nil {
			prepared[last].Metadata = make(map[string]string)
		}
		prepared[last].Metadata["acp_turn"] = "final"
	}
	for index := range prepared {
		msg := &prepared[index]
		msg.Seq = state.NextSeq
		state.NextSeq++
		if msg.ID == "" {
			msg.ID = fmt.Sprintf("mc_out_%d_%06d", time.Now().UnixMilli(), msg.Seq)
		}
		if msg.CreatedAt == 0 {
			msg.CreatedAt = time.Now().UnixMilli()
		}
		state.Messages = append(state.Messages, *msg)
	}
	pruneThirdPartyMessagesLocked(state)
	old := m.notifyCh
	m.notifyCh = make(chan struct{})
	close(old)
	m.mu.Unlock()
	for _, msg := range prepared {
		log.Printf("[thirdparty-mgr] outgoing queued client=%s id=%s seq=%d type=%s replyTo=%s final=%t progress=%t textChars=%d elapsed=%s",
			clientID, msg.ID, msg.Seq, msg.Type, msg.ReplyToMessageID,
			strings.EqualFold(msg.Metadata["acp_turn"], "final"), msg.Progress, len([]rune(msg.Text)),
			time.Since(started).Round(time.Millisecond))
	}
	return prepared
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

func (m *thirdPartyGatewayManager) preparePetAssetLocked(clientID string, asset devicePetAsset, animated bool, maxFrames int) map[string]any {
	if asset.Encoding != "rgb565a8" || asset.Width < 32 || asset.Width > devicePetAssetWidth ||
		asset.Height < 32 || asset.Height > devicePetAssetHeight || asset.Data == "" {
		return nil
	}
	encoded := []string{asset.Data}
	if animated && len(asset.Frames) > 0 {
		encoded = append(encoded, asset.Frames...)
	}
	if len(encoded) > devicePetFrameCount {
		encoded = encoded[:devicePetFrameCount]
	}
	frameMS := asset.FrameMS
	if frameMS < 50 || frameMS > 10000 {
		frameMS = devicePetFrameMS
	}
	if animated {
		// A missing limit is the legacy two-frame contract, not unlimited.
		// Evenly sample longer loops and stretch the cadence so their authored
		// cycle duration is preserved on memory-constrained clients.
		if maxFrames <= 0 {
			maxFrames = 2
		} else if maxFrames > devicePetFrameCount {
			maxFrames = devicePetFrameCount
		}
		if maxFrames < len(encoded) {
			selected := make([]string, 0, maxFrames)
			for index := 0; index < maxFrames; index++ {
				selected = append(selected, encoded[index*len(encoded)/maxFrames])
			}
			frameMS = frameMS * len(encoded) / maxFrames
			encoded = selected
		}
	}
	expected := asset.Width * asset.Height * 3
	frames := make([][]byte, 0, len(encoded))
	for _, text := range encoded {
		frame, err := base64.StdEncoding.DecodeString(text)
		if err != nil || len(frame) != expected {
			return nil
		}
		frames = append(frames, frame)
	}
	digest := sha256.New()
	_, _ = digest.Write([]byte(asset.Encoding))
	_, _ = digest.Write([]byte(fmt.Sprintf("/%d/%d/%d", asset.Width, asset.Height, frameMS)))
	for _, frame := range frames {
		_, _ = digest.Write(frame)
	}
	revision := hex.EncodeToString(digest.Sum(nil)[:8])
	urls := make([]string, 0, len(frames))
	hashes := make([]string, 0, len(frames))
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
			FileName: fmt.Sprintf("pet-%s-%d.rgb565a8", revision, index),
			MimeType: "application/vnd.maclaw.rgb565a8", SizeBytes: int64(len(frame)),
			Data: frame, Uploaded: true, CreatedAt: time.Now().UTC(), LastAccessedAt: time.Now().UTC(),
		}
		urls = append(urls, fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", id, token))
		hash := sha256.Sum256(frame)
		hashes = append(hashes, hex.EncodeToString(hash[:]))
	}
	return map[string]any{"encoding": asset.Encoding, "width": asset.Width, "height": asset.Height,
		"urls": urls, "frameMs": frameMS, "revision": revision, "sha256": hashes}
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
				if ref := m.preparePetAssetLocked(clientID, asset,
					capabilities.Features.PetAnimation && motionEnabled != nil && *motionEnabled,
					capabilities.Features.PetAssetMaxFrames); ref != nil {
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
// otherwise pile up stale asset references and make it replay every selection.
// The newest queued, still-unacked pet_profile is moved to the queue tail with
// a fresh ID and sequence. A device may already have fetched its former
// sequence even though the ACK is still in flight; retaining that sequence
// makes the replacement invisible to its next cursor poll. Refreshing the
// identity guarantees that the latest selection is fetched. The acked
// check is defensive: pruneThirdPartyMessagesLocked drops acked entries on
// every ack, so the branch is unreachable in production — but if an acked
// message ever lingered, rewriting it would silently drop the update because
// messagesAfter never resends acked entries. A nil motionEnabled or
// hasSkin == false means the caller omitted that key, so the queued value is
// preserved rather than reset to the zero value.
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
		now := time.Now().UnixMilli()
		message.ID = fmt.Sprintf("mc_pet_%d_%06d", now, state.NextSeq)
		message.Seq = state.NextSeq
		message.CreatedAt = now
		state.NextSeq++
		updated := *message
		state.Messages = append(state.Messages[:index], state.Messages[index+1:]...)
		state.Messages = append(state.Messages, updated)
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
	// Protocol control frames carry no user-facing modality. They must bypass
	// the text fallback or an empty speech_end frame is silently discarded and
	// the ESP32 keeps waiting for TTS parts that will never arrive.
	if strings.EqualFold(strings.TrimSpace(msg.Type), "speech_end") {
		return true
	}
	// Client tools are protocol control messages, not user-facing output. They
	// must not be coerced into an empty text reply by modality negotiation.
	if strings.EqualFold(strings.TrimSpace(msg.Type), "tool_call") {
		return msg.ToolCall != nil && coreim.NormalizeThirdPartyToolCall(msg.ToolCall) == nil
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
	// A new command owns this device/conversation from acceptance onward. Stop
	// synthesizing an older answer immediately instead of waiting until the new
	// result is ready; otherwise stale parts can continue playing during the new
	// command's thinking/processing surface.
	m.cancelDeviceSpeechTurn(req.ClientID, req.ConversationID)
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
			payload := m.hubGatewayPayload(req, maclawID)
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

// hubGatewayPayload preserves the full hardware identity and declared client
// tool surface across the local-gateway -> Hub relay. The Hub's remote gateway
// understands client_tool_context (not the legacy client_id fields), so omitting
// it made Hub-mode device tools silently disappear even though local mode worked.
func (m *thirdPartyGatewayManager) hubGatewayPayload(req thirdPartyIncomingRequest, maclawID string) map[string]any {
	clientID := normalizeThirdPartyID(req.ClientID)
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		conversationID = "default"
	}
	payload := map[string]any{
		"platform_uid":    thirdPartySessionUserID(clientID, conversationID),
		"text":            req.Message.Text,
		"message_type":    req.Message.Type,
		"client_id":       clientID,
		"conversation_id": conversationID,
		// ClientToolContext is the canonical transport-neutral binding. It must
		// accompany every turn, including a tool-result, so the GUI can route a
		// follow-up client tool call to this exact device and conversation.
		"client_tool_context": agent.ClientToolContext{
			ClientID: clientID, ConversationID: conversationID, ReplyToMessageID: maclawID,
		},
		// The HTTP acceptance response exposes maclawID as the canonical
		// correlation key. Preserve it across the Hub relay so the ESP32 waits
		// for the same id that the final answer carries.
		"message_id":          maclawID,
		"event_id":            req.EventID,
		"user_id":             req.User.ID,
		"user_name":           req.User.Name,
		"client_capabilities": m.clientCapabilities(clientID),
		"client_tools":        m.clientTools(clientID),
	}
	if req.Extra != nil {
		payload["extra"] = req.Extra
	}
	return payload
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

func (m *thirdPartyGatewayManager) ensureLocalHandler(clientID string) (*IMMessageHandler, error) {
	m.mu.Lock()
	if m.localHandlers == nil {
		m.localHandlers = newHardwareAgentRuntimeRegistry(m.app, m.app.remoteSessions, m.app.configureHardwareAgent)
	}
	runtimes := m.localHandlers
	m.mu.Unlock()
	handler, err := runtimes.handler(clientID)
	if err == nil && handler != nil {
		// Keep local device tools inside the originating hardware runtime. The
		// target comes from the immutable per-turn ClientToolContext, so a tool
		// registered by one device can never be emitted to another device queue.
		handler.clientToolDispatcher = m.dispatchLocalClientTool
	}
	return handler, err
}

func (m *thirdPartyGatewayManager) dispatchLocalClientTool(_ context.Context, target agent.ClientToolContext, definition agent.ClientToolDefinition, callID string, arguments map[string]any) error {
	if m == nil {
		return fmt.Errorf("third-party gateway is not configured")
	}
	clientID := normalizeThirdPartyID(target.ClientID)
	if clientID == "" {
		return fmt.Errorf("client tool target is missing client ID")
	}
	if callID == "" {
		callID = "ct_" + randomHexID(12)
	}
	call := &coreim.ThirdPartyToolCall{
		ID: callID, Name: definition.Name, Arguments: arguments, Risk: definition.Risk,
		RequiresApproval: definition.RequiresApproval, TimeoutMs: definition.TimeoutMs,
		IdempotencyKey: callID, Metadata: definition.Metadata,
	}
	if err := coreim.NormalizeThirdPartyToolCall(call); err != nil {
		return err
	}
	m.enqueue(clientID, thirdPartyOutgoingMessage{
		ConversationID:   strings.TrimSpace(target.ConversationID),
		ReplyToMessageID: strings.TrimSpace(target.ReplyToMessageID),
		Type:             "tool_call",
		ToolCall:         call,
	})
	return nil
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

	handler, err := m.ensureLocalHandler(req.ClientID)
	if err != nil {
		m.enqueueError(req, maclawID, "agent_runtime_error", err.Error())
		return
	}
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
		ClientTools:        m.clientTools(req.ClientID),
		ClientToolContext: &agent.ClientToolContext{
			ClientID:         req.ClientID,
			ConversationID:   req.ConversationID,
			ReplyToMessageID: maclawID,
		},
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
	if resp == nil {
		return
	}
	response := *resp
	response.Actions = append([]IMResponseAction(nil), resp.Actions...)
	response.Text = cleanDeviceReplyText(response.Text)
	resp = &response
	capabilities := m.clientCapabilities(clientID)
	resultAudioParts := responseDeviceFileAudioCount(resp, capabilities)
	deferredSpeechParts := 0
	var deferredSpeechChunks []string
	// A hardware reply must expose its final result surface before synthesis.
	// Plan semantic parts cheaply from the cleaned text, enqueue the armed text
	// immediately, then synthesize/publish each MP3 part in order. This avoids
	// holding the ESP32 on "remote processing" for the whole TTS duration.
	if len(resp.VoiceParts) == 0 && resp.VoiceData == "" && resultAudioParts == 0 && resp.Text != "" &&
		capabilities.SupportsOutput("text") && clientCanSynthesizeDeviceSpeech(m, capabilities) {
		parts := tts.PrepareSpeechChunks(resp.Text, 0, deviceVoiceChunkRunes)
		if len(parts) > 0 {
			deferredSpeechParts = len(parts)
			deferredSpeechChunks = parts
		}
	}
	responseModalities := agentResponseModalities(resp, capabilities)
	selected := agent.SelectClientOutputCombination(capabilities, responseModalities...)
	// A concrete result audio file is the authoritative audio representation of
	// the reply. Keep it paired with its result text even when a legacy client
	// advertises only singleton combinations; this is the same hardware-specific
	// result-before-playback contract used for deferred TTS.
	if resultAudioParts > 0 && containsString(responseModalities, "audio") {
		selected = appendUniqueString(selected, "audio")
		if resp.Text != "" && capabilities.SupportsOutput("text") {
			selected = appendUniqueString(selected, "text")
		}
	}
	allow := func(modality string) bool { return containsString(selected, modality) }
	allowAudio := allow("audio") || deferredSpeechParts > 0
	// Hardware clients display the result text as well as playing the ordered
	// audio stream, even when their declared preference selects audio first.
	allowText := allow("text") || (allowAudio && capabilities.SupportsOutput("text"))
	messages := make([]thirdPartyOutgoingMessage, 0, len(resp.VoiceParts)+4)
	// Queue terminal text first. It switches the ESP32 to the result page and
	// arms the exact number of correlated speech parts accepted afterwards.
	voiceParts := validThirdPartyVoiceParts(resp.VoiceParts, capabilities)
	if resp.Text != "" && allowText {
		// The ESP32 result page and its spoken reply must carry the same user
		// content. Route/model diagnostics and the legacy [i] marker belong only
		// in the desktop detail view, never in the hardware-facing answer.
		text := textutil.StripMarkdown(tts.StripInternalResponseMetadata(resp.Text))
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
		metadata := map[string]string{"acp_turn": "final"}
		if speechParts := deferredSpeechParts + len(voiceParts) + resultAudioParts; speechParts > 0 && allowAudio {
			metadata["speech_parts_pending"] = strconv.Itoa(speechParts)
		}
		messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: metadata})
	} else if len(resp.Actions) > 0 && allowText {
		text := "Please reply with an option:"
		for i, action := range resp.Actions {
			text += fmt.Sprintf("\n%d. %s", i+1, action.Label)
		}
		messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: text, Metadata: map[string]string{"acp_turn": "final"}})
	} else if resultAudioParts > 0 && allowText {
		messages = append(messages, thirdPartyOutgoingMessage{
			ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "text", Text: "音频已就绪",
			Metadata: map[string]string{"acp_turn": "final", "speech_parts_pending": strconv.Itoa(resultAudioParts)},
		})
	}
	if len(voiceParts) > 0 && allowAudio && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback {
		for index, part := range voiceParts {
			audioMetadata := map[string]string{
				"speech_part":  strconv.Itoa(index + 1),
				"speech_parts": strconv.Itoa(len(voiceParts)),
			}
			messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: part.MimeType, FileName: part.FileName, Data: part.Data, Metadata: audioMetadata})
		}
	}
	if resp.Error != "" && resp.Text == "" && len(resp.Actions) == 0 && allowText {
		messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "error", Error: "agent_error", Text: textutil.StripMarkdown(resp.Error)})
	}
	if resp.ImageKey != "" && allow("image") && capabilities.SupportsOutputMIME("image", "image/png") {
		messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image", ContentType: "image/png", FileName: "image.png", Data: resp.ImageKey})
	}
	if resp.FileData != "" {
		decoded, decodeErr := base64.StdEncoding.DecodeString(resp.FileData)
		if decodeErr == nil {
			kind, contentType := classifyDeviceResponseFile(resp.FileName, resp.FileMimeType)
			switch kind {
			case deviceResponseFileImage:
				if allow("image") && clientSupportsAgentImage(capabilities) {
					prepared, err := prepareDeviceResponseImage(resp.FileData, capabilities)
					if err != nil {
						log.Printf("[thirdparty-mgr] prepare result image %s error: %v", resp.FileName, err)
					} else {
						messages = append(messages, thirdPartyOutgoingMessage{
							ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image",
							ContentType: prepared.MIMEType, MimeType: prepared.MIMEType, FileName: prepared.FileName,
							Data: prepared.Data, SizeBytes: prepared.Size, Width: prepared.Width, Height: prepared.Height,
						})
					}
				}
			case deviceResponseFileAudio:
				if allowAudio && clientCanPlayDeviceAudio(capabilities, contentType, int64(len(decoded))) {
					messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: contentType, MimeType: contentType, FileName: resp.FileName, Data: resp.FileData, SizeBytes: int64(len(decoded))})
				}
			default:
				if allow("file") && capabilities.SupportsOutputMIME("file", contentType) && capabilities.SupportsOutputBytes("file", int64(len(decoded))) {
					messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: contentType, FileName: resp.FileName, Data: resp.FileData, SizeBytes: int64(len(decoded))})
				}
			}
		}
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
		kind, ct := classifyDeviceResponseFile(name, mime.TypeByExtension(filepath.Ext(name)))
		switch kind {
		case deviceResponseFileImage:
			if allow("image") && clientSupportsAgentImage(capabilities) {
				prepared, err := prepareDeviceResponseImage(base64.StdEncoding.EncodeToString(data), capabilities)
				if err != nil {
					log.Printf("[thirdparty-mgr] prepare local result image %s error: %v", p, err)
				} else {
					messages = append(messages, thirdPartyOutgoingMessage{
						ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "image",
						ContentType: prepared.MIMEType, MimeType: prepared.MIMEType, FileName: prepared.FileName,
						Data: prepared.Data, SizeBytes: prepared.Size, Width: prepared.Width, Height: prepared.Height,
					})
				}
			}
		case deviceResponseFileAudio:
			if allowAudio && clientCanPlayDeviceAudio(capabilities, ct, int64(len(data))) {
				messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "voice", ContentType: ct, MimeType: ct, FileName: name, Data: base64.StdEncoding.EncodeToString(data), SizeBytes: int64(len(data))})
			}
		default:
			if allow("file") && capabilities.SupportsOutputMIME("file", ct) && capabilities.SupportsOutputBytes("file", int64(len(data))) {
				messages = append(messages, thirdPartyOutgoingMessage{ConversationID: conversationID, ReplyToMessageID: replyTo, Type: "file", ContentType: ct, FileName: name, Data: base64.StdEncoding.EncodeToString(data), SizeBytes: int64(len(data))})
			}
		}
	}
	// Publish the complete logical response in one lock scope so polling cannot
	// observe a premature terminal marker between its constituent messages.
	speechTransaction := deferredSpeechParts+len(voiceParts)+resultAudioParts > 0
	queued := m.enqueueBatch(clientID, messages, !speechTransaction)
	if len(queued) == 0 && capabilities.SupportsOutput("text") {
		m.enqueue(clientID, thirdPartyOutgoingMessage{
			ConversationID: conversationID, ReplyToMessageID: replyTo,
			Type: "text", Text: "(no output)", Metadata: map[string]string{"acp_turn": "final"},
		})
		return
	}
	if len(deferredSpeechChunks) > 0 {
		m.cancelDeviceSpeechTurn(clientID, conversationID)
		go m.streamDeviceSpeechAfterResult(clientID, conversationID, replyTo, deferredSpeechChunks)
	}
}

func appendUniqueString(values []string, value string) []string {
	if containsString(values, value) {
		return values
	}
	return append(values, value)
}

func clientCanSynthesizeDeviceSpeech(m *thirdPartyGatewayManager, capabilities agent.ClientCapabilities) bool {
	if m == nil || m.app == nil || m.app.ttsManager == nil || capabilities.Output.Audio == nil || !capabilities.Output.Audio.Playback {
		return false
	}
	if !capabilities.SupportsOutputMIME("audio", "audio/mpeg") {
		return false
	}
	cfg, err := m.app.LoadConfig()
	return err == nil && cfg.TTSEnabled
}

func (m *thirdPartyGatewayManager) streamDeviceSpeechAfterResult(clientID, conversationID, replyTo string, parts []string) {
	turnKey := clientID + "\x00" + conversationID
	ctx, cancel := context.WithCancel(context.Background())
	turn := &deviceSpeechTurn{cancel: cancel, replyTo: replyTo, expected: len(parts)}
	m.speechMu.Lock()
	if previous := m.speechTurns[turnKey]; previous != nil {
		previous.cancel()
	}
	m.speechTurns[turnKey] = turn
	m.speechMu.Unlock()
	defer func() {
		cancel()
		m.speechMu.Lock()
		if m.speechTurns[turnKey] == turn {
			delete(m.speechTurns, turnKey)
		}
		m.speechMu.Unlock()
	}()
	started := time.Now()
	ok := streamPreparedDeviceVoicePayload(m.app.ttsManager, parts, thirdPartyPlatform(clientID), func(part IMVoicePart, index, total int) bool {
		if ctx.Err() != nil {
			return false
		}
		queued := m.enqueue(clientID, thirdPartyOutgoingMessage{
			ConversationID: conversationID, ReplyToMessageID: replyTo,
			Type: "voice", ContentType: part.MimeType, FileName: part.FileName, Data: part.Data,
			Metadata: map[string]string{
				"speech_part": strconv.Itoa(index), "speech_parts": strconv.Itoa(total),
				"voice_part_index": strconv.Itoa(index), "voice_part_total": strconv.Itoa(total),
			},
		})
		if queued.ID != "" {
			m.speechMu.Lock()
			if m.speechTurns[turnKey] == turn {
				turn.queued = index
			}
			m.speechMu.Unlock()
		}
		return queued.ID != ""
	})
	m.speechMu.Lock()
	queuedParts := turn.queued
	m.speechMu.Unlock()
	if !ok && queuedParts < turn.expected {
		m.enqueue(clientID, thirdPartyOutgoingMessage{
			ConversationID: conversationID, ReplyToMessageID: replyTo,
			Type:  "speech_end",
			Extra: map[string]any{"speech_parts_expected": turn.expected, "speech_parts_sent": queuedParts},
		})
	}
	log.Printf("[thirdparty-mgr] post-result speech finished client=%s replyTo=%s parts=%d ok=%t elapsed=%s",
		clientID, replyTo, len(parts), ok, time.Since(started).Round(time.Millisecond))
}

// validThirdPartyVoiceParts performs the inexpensive checks that enqueue would
// otherwise apply one part at a time. Filtering first is important because the
// final ACP marker belongs on the last deliverable part, not necessarily the
// last element of the untrusted response slice.
type deviceResponseFileKind int

const (
	deviceResponseFileGeneric deviceResponseFileKind = iota
	deviceResponseFileImage
	deviceResponseFileAudio
)

func classifyDeviceResponseFile(fileName, contentType string) (deviceResponseFileKind, string) {
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if contentType == "" {
		contentType = strings.ToLower(mime.TypeByExtension(ext))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if mediaType, _, ok := strings.Cut(contentType, ";"); ok {
		contentType = strings.TrimSpace(mediaType)
	}
	switch {
	case strings.HasPrefix(contentType, "image/") || ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".gif":
		if !strings.HasPrefix(contentType, "image/") {
			contentType = strings.ToLower(mime.TypeByExtension(ext))
		}
		return deviceResponseFileImage, contentType
	case strings.HasPrefix(contentType, "audio/") || ext == ".mp3" || ext == ".wav":
		if ext == ".mp3" && !strings.HasPrefix(contentType, "audio/") {
			contentType = "audio/mpeg"
		} else if ext == ".wav" && !strings.HasPrefix(contentType, "audio/") {
			contentType = "audio/wav"
		}
		return deviceResponseFileAudio, contentType
	default:
		return deviceResponseFileGeneric, contentType
	}
}

func clientCanPlayDeviceAudio(capabilities agent.ClientCapabilities, contentType string, sizeBytes int64) bool {
	return capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback &&
		capabilities.SupportsOutputMIME("audio", contentType) &&
		(capabilities.SupportsOutputAudioDelivery("inline", sizeBytes) || capabilities.SupportsOutputAudioDelivery("url", sizeBytes))
}

func validThirdPartyVoiceParts(parts []IMVoicePart, capabilities agent.ClientCapabilities) []IMVoicePart {
	if capabilities.Output.Audio == nil || !capabilities.Output.Audio.Playback {
		return nil
	}
	valid := make([]IMVoicePart, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part.Data) == "" || !capabilities.SupportsOutputMIME("audio", part.MimeType) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(part.Data))
		if err != nil || len(decoded) == 0 || int64(len(decoded)) > thirdPartyMaxMediaBytes {
			continue
		}
		size := int64(len(decoded))
		if !capabilities.SupportsOutputAudioDelivery("inline", size) && !capabilities.SupportsOutputAudioDelivery("url", size) {
			continue
		}
		valid = append(valid, part)
	}
	return valid
}

func agentResponseModalities(resp *IMAgentResponse, capabilities agent.ClientCapabilities) []string {
	modalities := make([]string, 0, 4)
	if resp == nil {
		return modalities
	}
	if resp.Text != "" || resp.Error != "" || len(resp.Actions) > 0 {
		modalities = append(modalities, "text")
	}
	// ImageKey is source image data, not necessarily the client's wire MIME.
	// ESP32 advertises display-ready RGB565 only; prepareDeviceResponseImage
	// performs that conversion later, so modality selection must retain the
	// image whenever the client supports either native PNG or RGB565 output.
	if resp.ImageKey != "" && clientSupportsAgentImage(capabilities) {
		modalities = append(modalities, "image")
	}
	filePresent := false
	if resp.FileData != "" {
		if decoded, err := base64.StdEncoding.DecodeString(resp.FileData); err == nil {
			kind, contentType := classifyDeviceResponseFile(resp.FileName, resp.FileMimeType)
			switch kind {
			case deviceResponseFileImage:
				if clientSupportsAgentImage(capabilities) {
					modalities = append(modalities, "image")
				}
			case deviceResponseFileAudio:
				if clientCanPlayDeviceAudio(capabilities, contentType, int64(len(decoded))) {
					modalities = append(modalities, "audio")
				}
			default:
				filePresent = capabilities.SupportsOutputMIME("file", contentType) && capabilities.SupportsOutputBytes("file", int64(len(decoded)))
			}
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
		kind, mimeType := classifyDeviceResponseFile(path, mime.TypeByExtension(filepath.Ext(path)))
		switch kind {
		case deviceResponseFileImage:
			if clientSupportsAgentImage(capabilities) {
				modalities = append(modalities, "image")
			}
		case deviceResponseFileAudio:
			if clientCanPlayDeviceAudio(capabilities, mimeType, info.Size()) {
				modalities = append(modalities, "audio")
			}
		default:
			if capabilities.SupportsOutputMIME("file", mimeType) && capabilities.SupportsOutputBytes("file", info.Size()) {
				filePresent = true
			}
		}
		if filePresent {
			filePresent = true
			break
		}
	}
	if filePresent {
		modalities = append(modalities, "file")
	}
	if len(resp.VoiceParts) > 0 && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback {
		for _, part := range resp.VoiceParts {
			if part.Data != "" && capabilities.SupportsOutputMIME("audio", part.MimeType) {
				modalities = append(modalities, "audio")
				break
			}
		}
	}
	return modalities
}
func pointerToClientCapabilities(capabilities agent.ClientCapabilities) *agent.ClientCapabilities {
	return &capabilities
}

func gatewayReplyIsFinal(reply GatewayReplyPayload) bool {
	if gatewayReplyIsExplicitlyFinal(reply) {
		return true
	}
	return !reply.Progress
}

func gatewayReplyIsExplicitlyFinal(reply GatewayReplyPayload) bool {
	if reply.Final || reply.Complete {
		return true
	}
	for _, source := range []map[string]any{reply.Metadata, reply.Extra} {
		if source == nil {
			continue
		}
		for _, key := range []string{"final", "complete", "completed"} {
			if value, ok := source[key].(bool); ok && value {
				return true
			}
		}
		for _, key := range []string{"acp_turn", "turn"} {
			if value, ok := source[key].(string); ok {
				switch strings.ToLower(strings.TrimSpace(value)) {
				case "final", "complete", "completed", "done", "end", "terminal":
					return true
				}
			}
		}
	}
	return false
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
				// Direct-Hub speech is intentionally held until the terminal text
				// has crossed this exact GUI -> Hub queue boundary. Otherwise the
				// shorter direct audio path can overtake Hub's result relay and play
				// while the ESP32 still shows "远端处理中".
				if normalizeThirdPartyGatewayMessageKind(reply.ReplyType.String()) == thirdPartyGatewayMessageText && gatewayReplyIsFinal(reply) {
					hubClient.startHubDeviceSpeechAfterResult(clientID, conversationID, thirdPartyReplyCorrelation(reply))
				}
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
		Metadata:         map[string]string{},
	}
	for _, key := range []string{"speech_parts_pending", "speech_part", "speech_parts"} {
		if value, ok := reply.Metadata[key]; ok {
			msg.Metadata[key] = strings.TrimSpace(fmt.Sprint(value))
		}
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
	if reply.VoicePartIndex > 0 {
		msg.Metadata["voice_part_index"] = strconv.Itoa(reply.VoicePartIndex)
	}
	if reply.VoicePartTotal > 0 {
		msg.Metadata["voice_part_total"] = strconv.Itoa(reply.VoicePartTotal)
	}
	// A streamed voice part is presentation, not command completion, when a
	// final text frame follows. Explicit terminal metadata wins for voice-only
	// responses; all other deferred reply types remain terminal.
	if replyKind != thirdPartyGatewayMessageVoice || reply.VoicePartFinal ||
		(reply.VoicePartIndex == 0 && reply.VoicePartTotal == 0 && gatewayReplyIsFinal(reply)) {
		msg.Metadata["acp_turn"] = "final"
	}
	capabilities := m.clientCapabilities(clientID)
	switch {
	case strings.EqualFold(strings.TrimSpace(reply.ReplyType.String()), "speech_end"):
		// Protocol control frame: retain the type and correlation. Coercing this
		// to text would leave the ESP32 waiting for audio parts that cannot arrive.
	case replyKind == thirdPartyGatewayMessageImage && capabilities.SupportsOutput("image"):
		msg.Data = reply.ImageData
		if msg.ContentType == "" {
			msg.ContentType = "image/png"
		}
	case replyKind == thirdPartyGatewayMessageVoice && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback && capabilities.SupportsOutputMIME("audio", msg.ContentType):
		msg.Data = reply.FileData
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

// App integration. Callers already holding imGatewaySyncMu use the locked
// variant to avoid racing a gateway restart with configuration changes.
func (a *App) ensureThirdPartyGatewayLocked() {
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

func (a *App) ensureThirdPartyGateway() {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	a.ensureThirdPartyGatewayLocked()
}

func (a *App) thirdPartyGatewayStatusLocked() string {
	if a.thirdPartyGateway == nil {
		return gatewayConnectionStatusDisconnected.String()
	}
	return a.thirdPartyGateway.Status()
}

func (a *App) GetThirdPartyGatewayStatus() string {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	return a.thirdPartyGatewayStatusLocked()
}

// restartThirdPartyGatewayLocked refreshes the gateway while the caller holds
// imGatewaySyncMu. Keeping this variant separate prevents a hardware-setting
// change from interleaving with a concurrent gateway restart.
func (a *App) restartThirdPartyGatewayLocked() string {
	// SyncFromConfig correctly leaves a healthy identical listener alone. This
	// endpoint is explicitly a restart, so tear it down first.
	wasConnected := a.thirdPartyGatewayStatusLocked() == gatewayConnectionStatusConnected.String()
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.stopForRestart()
	}
	a.ensureThirdPartyGatewayLocked()
	status := a.thirdPartyGatewayStatusLocked()
	// Windows can retain a just-closed listener briefly. Retry only after a
	// healthy listener was stopped; a genuinely occupied port still fails fast.
	for attempt := 0; wasConnected && status == gatewayConnectionStatusError.String() && attempt < 4; attempt++ {
		time.Sleep(20 * time.Millisecond)
		if a.thirdPartyGateway != nil {
			a.thirdPartyGateway.stopForRestart()
		}
		a.ensureThirdPartyGatewayLocked()
		status = a.thirdPartyGatewayStatusLocked()
	}
	return status
}

func (a *App) RestartThirdPartyGateway() string {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	return a.restartThirdPartyGatewayLocked()
}

// SendHardwareVolume persists and immediately sends the requested speaker
// level to every currently connected Macaron-style client.
func (a *App) SendHardwareVolume(volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("hardware volume must be between 0 and 100")
	}
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	if _, err := a.requireHardwareEnabled(); err != nil {
		return err
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

// SendHardwareDeviceVolume updates only one Hub-bound ESP32. The old global
// setting remains a default for newly paired hardware and local gateway mode;
// this method deliberately never broadcasts a binding-specific choice.
func (a *App) SendHardwareDeviceVolume(clientID string, volume int) error {
	if volume < 0 || volume > 100 {
		return fmt.Errorf("hardware volume must be between 0 and 100")
	}
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return err
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return fmt.Errorf("per-device volume is managed by Hub mode")
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub is not connected")
	}
	return hub.SendDeviceGatewayHardwareConfigForClient(clientID, map[string]any{"volume": volume})
}

// SendHardwareDevicePetProfile changes only one Hub-bound ESP32's rendered
// pet. The explicit feature gate preserves the default shared system-pet
// behavior and prevents stale UI clients from creating accidental overrides.
func (a *App) SendHardwareDevicePetProfile(clientID, skin string) error {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return err
	}
	if !cfg.HardwareAllowCustomPets {
		return fmt.Errorf("enable individual hardware pets before selecting a device pet")
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return fmt.Errorf("per-device pets are managed by Hub mode")
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub is not connected")
	}
	profile := a.devicePetProfileForSkin(cfg, skin)
	return hub.SendDeviceGatewayPetProfileForClient(clientID, profile)
}

// SetHardwareAllowCustomPets commits the local preference and Hub's matching
// authorization gate as one user-visible operation. On a Hub failure, the
// local config is restored so the selector never claims a capability Hub will
// reject.
func (a *App) SetHardwareAllowCustomPets(enabled bool) error {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return err
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return fmt.Errorf("individual hardware pets are managed by Hub mode")
	}
	if cfg.HardwareAllowCustomPets == enabled {
		return nil
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub is not connected")
	}
	if err := hub.SendDeviceGatewayAllowCustomPets(enabled); err != nil {
		return err
	}
	if _, err := a.PatchConfigFields(map[string]interface{}{"hardware_allow_custom_pets": enabled}); err != nil {
		rollbackErr := hub.SendDeviceGatewayAllowCustomPets(cfg.HardwareAllowCustomPets)
		if rollbackErr != nil {
			return fmt.Errorf("save individual hardware pets setting: %v; Hub rollback failed: %w", err, rollbackErr)
		}
		return err
	}
	return nil
}

func (a *App) requireHardwareEnabled() (corelib.AppConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.AppConfig{}, err
	}
	if !cfg.HardwareEnabled {
		return corelib.AppConfig{}, fmt.Errorf("hardware is disabled; enable hardware first")
	}
	return cfg, nil
}

func randomThirdPartyGatewayToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate third-party gateway token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func validateHardwareGatewayInvariant(cfg corelib.AppConfig) error {
	if !cfg.HardwareEnabled {
		return nil
	}
	if !cfg.ThirdPartyGatewayEnabled {
		return fmt.Errorf("disable hardware before turning off third-party access")
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return fmt.Errorf("hardware requires third-party access to use Hub mode")
	}
	if strings.TrimSpace(cfg.ThirdPartyGatewayToken) == "" {
		return fmt.Errorf("hardware requires a third-party gateway token")
	}
	return nil
}

// syncEnabledHardwareStateToHub publishes all hardware state that must survive
// a GUI reconnect. Hub owns the durable device registry and rejects device
// traffic until this master switch is enabled.
func (a *App) syncEnabledHardwareStateToHub() error {
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		// A Hub-mode listener alone is not enough: paired ESP32s authenticate and
		// route through Hub, which rejects their traffic until the master switch
		// is acknowledged. Do not report hardware as enabled until that durable
		// control-plane state is actually reachable.
		return fmt.Errorf("Hub is not connected; connect to Hub before enabling hardware")
	}
	if err := hub.SendDeviceGatewayHardwareEnabled(true); err != nil {
		return fmt.Errorf("enable hardware relay: %w", err)
	}
	if err := a.SyncHardwareWelcome(); err != nil {
		log.Printf("[device-welcome] hardware enable sync failed: %v", err)
	}
	if err := a.SyncHardwareVolume(); err != nil {
		log.Printf("[device-volume] hardware enable sync failed: %v", err)
	}
	if cfg, err := a.LoadConfig(); err != nil {
		log.Printf("[device-pet] load custom-pet permission for sync failed: %v", err)
	} else if err := hub.SendDeviceGatewayAllowCustomPets(cfg.HardwareAllowCustomPets); err != nil {
		log.Printf("[device-pet] custom-pet permission sync failed: %v", err)
	}
	return nil
}

// SetHardwareEnabled manages the hardware switch together with its required
// Hub-mode gateway settings. It is intentionally not a generic config patch:
// changing only one field would leave a locally enabled device gateway in an
// invalid transport state.
func (a *App) SetHardwareEnabled(enabled bool) (string, error) {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	previous, err := a.LoadConfig()
	if err != nil {
		return "", err
	}
	if !enabled {
		if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.HardwareEnabled = false
		}); err != nil {
			return "", err
		}
		if hub := a.hubClient(); hub != nil && hub.IsConnected() {
			if err := hub.SendDeviceGatewayHardwareEnabled(false); err != nil {
				rollbackErr := a.PatchConfig(func(cfg *corelib.AppConfig) {
					cfg.HardwareEnabled = previous.HardwareEnabled
				})
				if rollbackErr != nil {
					return a.thirdPartyGatewayStatusLocked(), fmt.Errorf("disable hardware relay failed (%v) and restoring hardware state also failed: %w", err, rollbackErr)
				}
				return a.thirdPartyGatewayStatusLocked(), fmt.Errorf("disable hardware relay failed; hardware state was restored: %w", err)
			}
		}
		return a.thirdPartyGatewayStatusLocked(), nil
	}

	if strings.TrimSpace(previous.RemoteMachineID) == "" {
		return "", fmt.Errorf("please register to Hub before enabling hardware")
	}
	if previous.HardwareEnabled && previous.ThirdPartyGatewayEnabled && !previous.IsThirdPartyGatewayLocalMode() && strings.TrimSpace(previous.ThirdPartyGatewayToken) != "" {
		status := a.restartThirdPartyGatewayLocked()
		if status != gatewayConnectionStatusConnected.String() {
			return status, fmt.Errorf("restart third-party access failed while restoring enabled hardware")
		}
		if err := a.syncEnabledHardwareStateToHub(); err != nil {
			return status, fmt.Errorf("%w; local hardware remains enabled and will resync after Hub reconnect", err)
		}
		return status, nil
	}

	token := strings.TrimSpace(previous.ThirdPartyGatewayToken)
	if token == "" {
		token, err = randomThirdPartyGatewayToken()
		if err != nil {
			return "", err
		}
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.HardwareEnabled = true
		cfg.ThirdPartyGatewayEnabled = true
		cfg.ThirdPartyGatewayToken = token
		cfg.SetThirdPartyGatewayLocal(false)
	}); err != nil {
		return "", err
	}

	status := a.restartThirdPartyGatewayLocked()
	var relayErr error
	if status == gatewayConnectionStatusConnected.String() {
		if relayErr = a.syncEnabledHardwareStateToHub(); relayErr == nil {
			return status, nil
		} else {
			log.Printf("[device-hardware] enable relay failed, restoring settings: %v", relayErr)
		}
	}
	rollbackErr := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.HardwareEnabled = previous.HardwareEnabled
		cfg.ThirdPartyGatewayEnabled = previous.ThirdPartyGatewayEnabled
		cfg.ThirdPartyGatewayToken = previous.ThirdPartyGatewayToken
		cfg.ThirdPartyGatewayLocalMode = previous.ThirdPartyGatewayLocalMode
	})
	if rollbackErr != nil {
		return status, fmt.Errorf("restart third-party access failed and restoring hardware settings also failed: %w", rollbackErr)
	}
	if previous.ThirdPartyGatewayEnabled && strings.TrimSpace(previous.ThirdPartyGatewayToken) != "" {
		a.restartThirdPartyGatewayLocked()
	} else if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.Stop()
	}
	if status == gatewayConnectionStatusConnected.String() {
		return status, fmt.Errorf("enable hardware relay failed; hardware settings were restored: %w", relayErr)
	}
	return status, fmt.Errorf("restart third-party access failed; hardware settings were restored")
}

// CreateThirdPartyDevicePairing produces a six-digit, thirty-minute bootstrap
// credential for a LAN device. The bearer itself never needs to be typed on
// the ESP32.
func (a *App) CreateThirdPartyDevicePairing() (map[string]any, error) {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return nil, err
	}
	a.ensureThirdPartyGatewayLocked()
	if a.thirdPartyGateway == nil {
		return nil, fmt.Errorf("enable third-party access before pairing a device")
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

// ListThirdPartyHardwareDevices returns the Hub-owned bindings for this GUI.
// A local gateway has no durable device registry, so it deliberately returns
// an empty list instead of attempting a Hub request.
func (a *App) ListThirdPartyHardwareDevices() ([]HardwareDeviceBinding, error) {
	bindings, err := a.ListThirdPartyHardwareDeviceBindings()
	return bindings.Devices, err
}

// ListThirdPartyHardwareDeviceBindings returns Hub-owned bindings together
// with the fixed five-device capacity for this GUI.
func (a *App) ListThirdPartyHardwareDeviceBindings() (HardwareDeviceBindings, error) {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return HardwareDeviceBindings{}, err
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return HardwareDeviceBindings{Devices: []HardwareDeviceBinding{}, MaxDevices: 5}, nil
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return HardwareDeviceBindings{}, fmt.Errorf("Hub is not connected")
	}
	return hub.ListHardwareDeviceBindings()
}

// DeleteThirdPartyHardwareDevice removes a durable Hub-owned hardware
// binding. The Hub remains the authority for ownership and credentials.
func (a *App) DeleteThirdPartyHardwareDevice(clientID string) error {
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	cfg, err := a.requireHardwareEnabled()
	if err != nil {
		return err
	}
	if cfg.IsThirdPartyGatewayLocalMode() {
		return fmt.Errorf("durable hardware bindings are managed by Hub mode")
	}
	hub := a.hubClient()
	if hub == nil || !hub.IsConnected() {
		return fmt.Errorf("Hub is not connected")
	}
	return hub.DeleteHardwareDevice(clientID)
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
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()
	// Hardware owns this transport while it is enabled. A stale Wails stop
	// request must not silently leave the persisted hardware state invalid.
	if cfg, err := a.LoadConfig(); err == nil && cfg.HardwareEnabled {
		return
	}
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
	a.imGatewaySyncMu.Lock()
	defer a.imGatewaySyncMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	if !enabled && cfg.RemoteMachineID == "" {
		return fmt.Errorf("please register to Hub before enabling Hub mode")
	}
	if enabled && cfg.HardwareEnabled {
		return fmt.Errorf("disable hardware before switching third-party access to local mode")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SetThirdPartyGatewayLocal(enabled)
	}); err != nil {
		return err
	}
	if a.thirdPartyGateway != nil {
		a.thirdPartyGateway.resetLocalHandler()
	}
	// Reconcile the running listener with the new transport mode immediately.
	// SyncFromConfig emits the matching Hub claim/unclaim even when its listener
	// is already healthy, so no stale route remains until a later reconnect.
	a.ensureThirdPartyGatewayLocked()
	return nil
}
