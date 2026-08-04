package im

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
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

// DeviceGateway exposes the same small HTTP protocol as a third-party
// gateway, but Hub is the public endpoint and relays every message to the
// GUI that claimed the tenant's thirdparty channel. This lets a GUI remain
// behind NAT or a corporate firewall.
type DeviceGateway struct {
	plugin               *RemoteGatewayPlugin
	store                deviceCredentialStore
	meetingRecordings    http.Handler
	meetingTranscript    bool
	meetingMinutes       bool
	voicePairTranscriber func(context.Context, string, string) (string, error)
	voicePairAttempts    map[string]*deviceVoicePairAttempt

	mu       sync.Mutex
	pairings map[string]devicePairing
	tokens   map[string]devicePrincipal
	clients  map[string]*deviceClientState
	media    map[string]*deviceMedia
	hardware map[string]deviceHardwareConfig
}

// DeviceMeetingResultNotifier is the narrow callback used by the shared
// Mobile meeting pipeline to publish a terminal status back to the originating
// hardware client without importing the IM package from httpapi.
type DeviceMeetingResultNotifier interface {
	EnqueueReply(clientID, conversationID string, reply map[string]any)
}

type deviceCredentialStore interface {
	Set(ctx context.Context, key, valueJSON string) error
	Get(ctx context.Context, key string) (string, error)
}

const deviceGatewayCredentialsKey = "device_gateway_credentials_v1"

type persistedDeviceCredentials struct {
	Tokens          map[string]devicePrincipal      `json:"tokens"`
	MachineHardware map[string]deviceHardwareConfig `json:"machineHardware,omitempty"`
}

// deviceHardwareConfig is Hub-owned state for the hardware paired to one GUI.
// WelcomeAudio is base64 PCM WAV so Hub can play it after a hardware reboot
// even while the GUI is offline.
type deviceHardwareConfig struct {
	WelcomeEnabled bool   `json:"welcomeEnabled,omitempty"`
	WelcomeAudio   string `json:"welcomeAudio,omitempty"`
	// Volume is a pointer so zero (mute) remains distinguishable from the
	// legacy "not configured" state.
	Volume *int `json:"volume,omitempty"`
}

type devicePairing struct {
	MachineID string
	TenantID  string
	UserID    string
	Pet       devicePetProfile
	ExpiresAt time.Time
}

type deviceVoicePairAttempt struct {
	WindowStart time.Time
	Count       int
}

const (
	deviceVoicePairAttemptWindow = time.Minute
	deviceVoicePairAttemptLimit  = 6
)

// Hardware pairing often includes switching the phone to the device hotspot
// and back. Keep the one-time code usable long enough for that physical flow.
const deviceGatewayPairingTTL = 30 * time.Minute

type devicePrincipal struct {
	ClientID          string
	MachineID         string
	TenantID          string
	UserID            string
	Pet               devicePetProfile
	LastWelcomeBootID string `json:"lastWelcomeBootId,omitempty"`
}

type deviceAmbientWeather struct {
	Summary      string `json:"summary"`
	TemperatureC int    `json:"temperatureC"`
	Location     string `json:"location,omitempty"`
}

type deviceAmbient struct {
	Weather   deviceAmbientWeather `json:"weather"`
	Glyphs    map[string]string    `json:"glyphs,omitempty"`
	ExpiresAt int64                `json:"expiresAt,omitempty"`
}

const (
	deviceGlyphBytesPerGlyph = 72 // 24 rows * 3 packed bytes per row.
	// The ESP reply reader paginates long answers and caches up to 96 glyphs.
	// Keep the Hub validation limit aligned with that negotiated device limit;
	// otherwise a normal long Chinese answer is rejected as a whole and falls
	// back to question marks on the device.
	deviceGlyphMaxPerPayload = 96
)

// devicePetProfile is a deliberately small, portable description. The ESP
// renders its own matching skin and animation rather than downloading the
// desktop pack's unrestricted assets.
type devicePetProfile struct {
	Skin          string          `json:"skin"`
	MotionEnabled bool            `json:"motionEnabled"`
	Asset         *DevicePetAsset `json:"asset,omitempty"`
}

// DevicePetAsset is GUI-rendered (not user-submitted) RGB565.  Keeping this
// with the authenticated device credential lets Hub relay the exact selected
// desktop pet without needing access to local pet-pack files.
type DevicePetAsset struct {
	Encoding string   `json:"encoding,omitempty"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Data     string   `json:"data,omitempty"`
	Frames   []string `json:"frames,omitempty"`
}

// devicePetAssetReference is intentionally small enough for the handshake and
// poll JSON buffers used by embedded clients. The RGB565 payload itself lives
// in the existing authenticated media store and is fetched after TLS setup.
type devicePetAssetReference struct {
	Encoding string   `json:"encoding"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	URLs     []string `json:"urls"`
	FrameMS  int      `json:"frameMs,omitempty"`
	Revision string   `json:"revision"`
}

type deviceClientState struct {
	next         int64
	messages     []map[string]any
	acked        map[string]bool
	ackStatus    map[string]string
	notify       chan struct{}
	ambient      map[string]any
	capabilities agent.ClientCapabilities
	// capabilitiesDeclared distinguishes a legacy client that never completed
	// capability negotiation from a modern client that intentionally declares
	// a restricted input contract. Before handshake, preserve the historical
	// text/audio upload behavior; after handshake, enforce the declaration.
	capabilitiesDeclared bool
}

type deviceMedia struct {
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
	LastAccessedAt time.Time
}

const (
	deviceGatewayMaxMediaBytes     int64 = 10 * 1024 * 1024
	deviceGatewayMaxMediaObjects         = 200
	deviceGatewayMaxQueuedMessages       = 100
	deviceGatewayMaxAckReceipts          = 100
)

func NewDeviceGateway(plugin *RemoteGatewayPlugin) *DeviceGateway {
	return &DeviceGateway{plugin: plugin, pairings: make(map[string]devicePairing), tokens: make(map[string]devicePrincipal), clients: make(map[string]*deviceClientState), media: make(map[string]*deviceMedia), hardware: make(map[string]deviceHardwareConfig), voicePairAttempts: make(map[string]*deviceVoicePairAttempt)}
}

// NewPersistentDeviceGateway keeps hardware bearer credentials across Hub
// process restarts. Pairing codes remain deliberately in memory and expiring;
// only the durable token-to-owner binding is persisted.
func NewPersistentDeviceGateway(plugin *RemoteGatewayPlugin, store deviceCredentialStore) *DeviceGateway {
	g := NewDeviceGateway(plugin)
	g.store = store
	if store == nil {
		return g
	}
	raw, err := store.Get(context.Background(), deviceGatewayCredentialsKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return g
	}
	var saved persistedDeviceCredentials
	if json.Unmarshal([]byte(raw), &saved) == nil {
		if saved.Tokens != nil {
			g.tokens = saved.Tokens
		}
		if saved.MachineHardware != nil {
			g.hardware = saved.MachineHardware
		}
	}
	return g
}

func (g *DeviceGateway) persistTokensLocked() error {
	if g.store == nil {
		return nil
	}
	copyTokens := make(map[string]devicePrincipal, len(g.tokens))
	for token, principal := range g.tokens {
		copyTokens[token] = principal
	}
	copyHardware := make(map[string]deviceHardwareConfig, len(g.hardware))
	for machineID, config := range g.hardware {
		copyHardware[machineID] = config
	}
	raw, err := json.Marshal(persistedDeviceCredentials{Tokens: copyTokens, MachineHardware: copyHardware})
	if err != nil {
		return err
	}
	return g.store.Set(context.Background(), deviceGatewayCredentialsKey, string(raw))
}

func (g *DeviceGateway) RegisterPairing(machineID, tenantID, userID, code string) error {
	return g.registerPairingWithPetProfileAsset(machineID, tenantID, userID, code, "clawmate", true, nil)
}

func (g *DeviceGateway) RegisterPairingWithPetProfile(machineID, tenantID, userID, code, skin string, motionEnabled bool) error {
	return g.registerPairingWithPetProfileAsset(machineID, tenantID, userID, code, skin, motionEnabled, nil)
}

func (g *DeviceGateway) RegisterPairingWithPetProfileAsset(machineID, tenantID, userID, code, skin string, motionEnabled bool, asset map[string]any) error {
	return g.registerPairingWithPetProfileAsset(machineID, tenantID, userID, code, skin, motionEnabled, DevicePetAssetFromMap(asset))
}

func (g *DeviceGateway) registerPairingWithPetProfileAsset(machineID, tenantID, userID, code, skin string, motionEnabled bool, asset *DevicePetAsset) error {
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" || strings.TrimSpace(machineID) == "" {
		return fmt.Errorf("a six-digit pairing code is required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	for candidate, pairing := range g.pairings {
		if !pairing.ExpiresAt.After(now) {
			delete(g.pairings, candidate)
		}
	}
	g.pairings[code] = devicePairing{MachineID: machineID, TenantID: normalizeRemoteTenantID(tenantID), UserID: userID, Pet: normalizeDevicePetProfileAsset(skin, motionEnabled, asset), ExpiresAt: now.Add(deviceGatewayPairingTTL)}
	return nil
}

func (g *DeviceGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/device-gateway/v1/pair":
		g.handlePair(w, r)
	case "/api/device-gateway/v1/pair/voice":
		g.handleVoicePair(w, r)
	case "/api/im-gateway/v1/health":
		g.handleHealth(w, r)
	case "/api/im-gateway/v1/handshake":
		g.handleHandshake(w, r)
	case "/api/im-gateway/v1/incoming":
		g.handleIncoming(w, r)
	case "/api/im-gateway/v1/outgoing":
		g.handleOutgoing(w, r)
	case "/api/im-gateway/v1/ack":
		g.handleAck(w, r)
	case "/api/im-gateway/v1/media/upload-url":
		g.handleMediaUploadURL(w, r)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/device-gateway/v1/meeting-recordings") && g.meetingRecordings != nil {
			g.meetingRecordings.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/im-gateway/v1/media/") {
			g.handleMedia(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

// SetMeetingRecordingHandler wires the Hub-owned mobile-library recording
// implementation into the hardware gateway without making the IM package
// depend on the HTTP API package.
func (g *DeviceGateway) SetMeetingRecordingHandler(handler http.Handler) {
	g.mu.Lock()
	g.meetingRecordings = handler
	g.mu.Unlock()
}

// SetMeetingRecordingModes advertises only processing modes backed by a
// configured worker. Keeping this separate from the handler avoids an import
// cycle between the IM gateway and the HTTP API package.
func (g *DeviceGateway) SetMeetingRecordingModes(transcript, minutes bool) {
	g.mu.Lock()
	g.meetingTranscript = transcript
	g.meetingMinutes = minutes && transcript
	g.mu.Unlock()
}

// SetVoicePairTranscriber wires the deployment's existing ASR worker into the
// unauthenticated, rate-limited-at-the-edge spoken pairing bootstrap. The
// callback receives a temporary 16 kHz WAV file and must return plain text.
func (g *DeviceGateway) SetVoicePairTranscriber(transcriber func(context.Context, string, string) (string, error)) {
	g.mu.Lock()
	g.voicePairTranscriber = transcriber
	g.mu.Unlock()
}

// AuthenticatedDeviceOwner resolves a durable hardware credential to the Hub
// user who created its pairing code. It intentionally returns no bearer or
// machine secret; downstream handlers only receive the tenant/owner binding.
func (g *DeviceGateway) AuthenticatedDeviceOwner(r *http.Request) (tenantID, userID, clientID string, ok bool) {
	principal, ok := g.principal(r)
	if !ok {
		return "", "", "", false
	}
	return normalizeRemoteTenantID(principal.TenantID), strings.TrimSpace(principal.UserID), strings.TrimSpace(principal.ClientID), true
}

func (g *DeviceGateway) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	var req struct {
		PairCode string `json:"pairCode"`
		// Code keeps already-flashed firmware working while pairCode is the
		// canonical public device-gateway field.
		Code     string `json:"code"`
		ClientID string `json:"clientId"`
	}
	if !decodeDeviceJSON(w, r, &req) || strings.TrimSpace(req.ClientID) == "" {
		return
	}
	pairCode := strings.TrimSpace(req.PairCode)
	if pairCode == "" {
		pairCode = strings.TrimSpace(req.Code)
	}
	g.exchangePairing(w, strings.TrimSpace(req.ClientID), pairCode)
}

func (g *DeviceGateway) exchangePairing(w http.ResponseWriter, clientID, pairCode string) {
	g.mu.Lock()
	pairing, ok := g.pairings[pairCode]
	if ok {
		delete(g.pairings, pairCode)
	}
	g.mu.Unlock()
	if !ok || !pairing.ExpiresAt.After(time.Now()) {
		writeDeviceError(w, http.StatusUnauthorized, "invalid_pairing_code", "pairing code is invalid or expired")
		return
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot create credential")
		return
	}
	token := hex.EncodeToString(bytes[:])
	g.mu.Lock()
	// A physical client ID identifies one device across the gateway. Re-pairing
	// it must revoke an earlier credential (including one bound to another
	// machine) and discard that device's stale queue before issuing the new
	// bearer. Otherwise an old device could remain authorized after replacement.
	clientID = strings.TrimSpace(clientID)
	revoked := make(map[string]devicePrincipal)
	for existingToken, principal := range g.tokens {
		if principal.ClientID == clientID {
			revoked[existingToken] = principal
			delete(g.tokens, existingToken)
		}
	}
	previousState := g.clients[clientID]
	delete(g.clients, clientID)
	g.tokens[token] = devicePrincipal{ClientID: clientID, MachineID: pairing.MachineID, TenantID: pairing.TenantID, UserID: pairing.UserID, Pet: pairing.Pet}
	if err := g.persistTokensLocked(); err != nil {
		delete(g.tokens, token)
		for revokedToken, principal := range revoked {
			g.tokens[revokedToken] = principal
		}
		if previousState != nil {
			g.clients[clientID] = previousState
		}
		g.mu.Unlock()
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot persist device credential")
		return
	}
	g.mu.Unlock()
	writeDeviceJSON(w, http.StatusCreated, map[string]any{"ok": true, "gatewayToken": token, "clientId": clientID})
}

func (g *DeviceGateway) handleVoicePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	clientID := strings.TrimSpace(r.Header.Get("X-MaClaw-Client-ID"))
	if clientID == "" {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", "X-MaClaw-Client-ID is required")
		return
	}
	attemptKey := deviceVoicePairAttemptKey(r, clientID)
	if !g.allowVoicePairAttempt(attemptKey, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeDeviceError(w, http.StatusTooManyRequests, "rate_limited", "too many voice pairing attempts; retry later")
		return
	}
	g.mu.Lock()
	transcriber := g.voicePairTranscriber
	g.mu.Unlock()
	if transcriber == nil {
		writeDeviceError(w, http.StatusServiceUnavailable, "unavailable", "speech recognition is unavailable")
		return
	}
	const maxVoicePairWAVBytes = 512 << 10
	wav, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxVoicePairWAVBytes))
	if err != nil || len(wav) < 44 {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", "a short WAV recording is required")
		return
	}
	tmp, err := os.CreateTemp("", "maclaw-device-pair-*.wav")
	if err != nil {
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot prepare speech recognition")
		return
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err = tmp.Write(wav); err == nil {
		err = tmp.Close()
	} else {
		_ = tmp.Close()
	}
	if err != nil {
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot prepare speech recognition")
		return
	}
	transcript, err := transcriber(r.Context(), path, "audio/wav")
	if err != nil {
		log.Printf("[device-gateway] voice pairing ASR failed: %v", err)
		writeDeviceError(w, http.StatusServiceUnavailable, "unavailable", "speech recognition is unavailable")
		return
	}
	pairCode, ok := deviceGatewayPairCodeFromTranscript(transcript)
	if !ok {
		writeDeviceError(w, http.StatusBadRequest, "bad_pair_code", "please speak exactly six digits")
		return
	}
	g.exchangePairing(w, clientID, pairCode)
}

func deviceVoicePairAttemptKey(r *http.Request, clientID string) string {
	host := ""
	if r != nil {
		host = strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
		if comma := strings.IndexByte(host, ','); comma >= 0 {
			host = strings.TrimSpace(host[:comma])
		}
		if host == "" {
			host = strings.TrimSpace(r.RemoteAddr)
		}
	}
	return host + "\x00" + strings.TrimSpace(clientID)
}

func (g *DeviceGateway) allowVoicePairAttempt(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.voicePairAttempts == nil {
		g.voicePairAttempts = make(map[string]*deviceVoicePairAttempt)
	}
	if len(g.voicePairAttempts) > 1024 {
		for candidate, attempt := range g.voicePairAttempts {
			if attempt == nil || now.Sub(attempt.WindowStart) >= deviceVoicePairAttemptWindow {
				delete(g.voicePairAttempts, candidate)
			}
		}
	}
	attempt := g.voicePairAttempts[key]
	if attempt == nil || now.Sub(attempt.WindowStart) >= deviceVoicePairAttemptWindow {
		g.voicePairAttempts[key] = &deviceVoicePairAttempt{WindowStart: now, Count: 1}
		return true
	}
	if attempt.Count >= deviceVoicePairAttemptLimit {
		return false
	}
	attempt.Count++
	return true
}

func deviceGatewayPairCodeFromTranscript(transcript string) (string, bool) {
	var digits strings.Builder
	normalized := strings.ToLower(strings.TrimSpace(transcript))
	for _, r := range normalized {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
			continue
		}
		if digit, ok := deviceGatewaySpokenChineseDigit(r); ok {
			digits.WriteByte(digit)
		}
	}
	for _, word := range strings.FieldsFunc(normalized, func(r rune) bool { return r < 'a' || r > 'z' }) {
		if digit, ok := deviceGatewaySpokenEnglishDigit(word); ok {
			digits.WriteByte(digit)
		}
	}
	code := digits.String()
	return code, len(code) == 6
}

func deviceGatewaySpokenChineseDigit(r rune) (byte, bool) {
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

func deviceGatewaySpokenEnglishDigit(word string) (byte, bool) {
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

func (g *DeviceGateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDeviceError(w, 405, "method_not_allowed", "GET required")
		return
	}
	writeDeviceJSON(w, 200, map[string]any{"ok": true, "mode": "maclaw", "serverTime": time.Now().UnixMilli()})
}

func (g *DeviceGateway) handleHandshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, 405, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req coreim.ThirdPartyHandshakeRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if err := coreim.NormalizeThirdPartyHandshakeRequest(&req); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.ClientID) != p.ClientID {
		writeDeviceError(w, 403, "forbidden", "clientId does not match credential")
		return
	}
	capabilities := agent.NormalizeClientCapabilities(req.ClientCapabilities)
	bootSessionID := strings.TrimSpace(req.BootSessionID)
	if len(bootSessionID) > 96 {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", "bootSessionId is too long")
		return
	}
	g.mu.Lock()
	state := g.clientLocked(p.ClientID)
	state.capabilities = capabilities
	state.capabilitiesDeclared = req.ClientCapabilities != nil
	g.queueHardwareConfigForClientLocked(p, state)
	g.queueWelcomeForBootLocked(p, bootSessionID, state)
	petProfile := p.Pet
	var petAsset *devicePetAssetReference
	if capabilities.Features.PetAsset {
		petAsset = g.preparePetAssetLocked(p.ClientID, petProfile.Asset,
			capabilities.Features.PetAnimation && petProfile.MotionEnabled)
	}
	meetingRecordings := g.meetingRecordings
	meetingTranscript := g.meetingTranscript
	meetingMinutes := g.meetingMinutes
	g.mu.Unlock()
	// Never embed RGB565 base64 in the handshake. Even one 128px frame expands
	// to ~44 KiB and used to exhaust the ESP's memory-sensitive TLS path.
	petProfile.Asset = nil
	response := map[string]any{"ok": true, "mode": "maclaw", "channelId": "thirdparty:" + p.ClientID, "serverTime": time.Now().UnixMilli(), "pet": petProfile, "poll": map[string]int{"timeoutSec": 30, "maxTimeoutSec": 60, "maxBatchSize": 20, "maxLimit": 100}, "limits": map[string]int{"maxBodyBytes": 1048576, "maxMediaBytes": 10485760}}
	if petAsset != nil {
		response["petAsset"] = *petAsset
	}
	if meetingRecordings != nil {
		response["meetingRecording"] = map[string]any{
			"basePath": "/api/device-gateway/v1/meeting-recordings", "chunkSize": 1 << 20,
			"contentTypes": []string{"audio/wav"}, "modes": map[string]bool{"keep": true, "transcript": meetingTranscript, "minutes": meetingMinutes},
		}
	}
	response["protocolVersion"] = "1.1"
	response["capabilitiesAccepted"] = capabilities
	if ambient, ok := g.latestAmbientLocked(p.ClientID); ok {
		response["ambient"] = ambient
	}
	writeDeviceJSON(w, 200, response)
}

func (g *DeviceGateway) handleIncoming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, 405, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req struct {
		ClientID       string `json:"clientId"`
		EventID        string `json:"eventId"`
		MessageID      string `json:"messageId"`
		ConversationID string `json:"conversationId"`
		Message        struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			FileName    string `json:"fileName"`
			MimeType    string `json:"mimeType"`
			Attachments []struct {
				ID        string `json:"id"`
				Type      string `json:"type"`
				FileName  string `json:"fileName"`
				MimeType  string `json:"mimeType"`
				Data      string `json:"data"`
				SizeBytes int64  `json:"sizeBytes"`
			} `json:"attachments"`
		} `json:"message"`
	}
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ClientID) != p.ClientID || strings.TrimSpace(req.EventID) == "" {
		writeDeviceError(w, 400, "bad_request", "clientId and eventId are required")
		return
	}
	g.mu.Lock()
	state := g.clientLocked(p.ClientID)
	capabilities := state.capabilities
	capabilitiesDeclared := state.capabilitiesDeclared
	g.mu.Unlock()
	messageType := normalizeDeviceInputModality(req.Message.Type, req.Message.MimeType)
	if messageType == "" {
		messageType = strings.ToLower(strings.TrimSpace(req.Message.Type))
	}
	if messageType == "" {
		messageType = "text"
	}
	if capabilitiesDeclared && !capabilities.SupportsInput(messageType) {
		writeDeviceError(w, http.StatusBadRequest, "unsupported_input", "message type is not declared by client capabilities")
		return
	}
	if g.plugin == nil {
		writeDeviceError(w, 503, "unavailable", "GUI relay is unavailable")
		return
	}
	ownerID := g.plugin.GatewayOwnerForTenant(p.TenantID)
	if ownerID == "" {
		writeDeviceError(w, http.StatusServiceUnavailable, "gui_offline", "the paired MaClaw GUI is not connected to Hub")
		return
	}
	attachments, err := g.incomingAttachments(p.ClientID, req.Message.Attachments)
	if err != nil {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	for _, attachment := range attachments {
		modality := normalizeDeviceInputModality(attachment.Type, attachment.MimeType)
		if capabilitiesDeclared &&
			(modality == "" || !capabilities.SupportsInputMIME(modality, attachment.MimeType)) {
			writeDeviceError(w, http.StatusBadRequest, "unsupported_input", "attachment is not declared by client capabilities")
			return
		}
	}
	payload, _ := json.Marshal(map[string]any{"tenant_id": p.TenantID, "platform_uid": "thirdparty:" + p.ClientID + ":" + firstDeviceValue(req.ConversationID, "default"), "reply_target": "thirdparty:" + p.ClientID + ":" + firstDeviceValue(req.ConversationID, "default"), "message_id": firstDeviceValue(req.MessageID, req.EventID), "message_type": firstDeviceValue(req.Message.Type, "text"), "text": req.Message.Text, "attachments": attachments, "client_capabilities": capabilities})
	go g.plugin.HandleGatewayMessage(ownerID, payload)
	writeDeviceJSON(w, 200, map[string]any{"ok": true, "accepted": true, "messageId": req.MessageID, "duplicate": false})
}

func (g *DeviceGateway) handleMediaUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req struct {
		ClientID   string `json:"clientId"`
		Type       string `json:"type"`
		FileName   string `json:"fileName"`
		MimeType   string `json:"mimeType"`
		SizeBytes  int64  `json:"sizeBytes"`
		DurationMs int64  `json:"durationMs"`
	}
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ClientID) != p.ClientID || req.SizeBytes < 0 || req.SizeBytes > deviceGatewayMaxMediaBytes {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", "invalid media request")
		return
	}
	g.mu.Lock()
	state := g.clientLocked(p.ClientID)
	capabilities := state.capabilities
	capabilitiesDeclared := state.capabilitiesDeclared
	g.mu.Unlock()
	mediaType := normalizeDeviceInputModality(req.Type, req.MimeType)
	if capabilitiesDeclared && (mediaType == "" || !capabilities.SupportsInput(mediaType) ||
		!capabilities.SupportsInputMIME(mediaType, req.MimeType)) {
		writeDeviceError(w, http.StatusBadRequest, "unsupported_media", "media is not declared by client capabilities")
		return
	}
	mediaID, token, err := newDeviceToken(16)
	if err != nil {
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot prepare media")
		return
	}
	baseURL := deviceRequestBaseURL(r)
	media := &deviceMedia{ClientID: p.ClientID, ID: mediaID, Token: token, Type: firstDeviceValue(req.Type, "file"), FileName: safeDeviceFileName(req.FileName), MimeType: strings.TrimSpace(req.MimeType), SizeBytes: req.SizeBytes, DurationMs: req.DurationMs, LastAccessedAt: time.Now().UTC()}
	g.mu.Lock()
	g.pruneMediaLocked(time.Now().UTC())
	g.media[mediaID] = media
	g.mu.Unlock()
	downloadURL := fmt.Sprintf("%s/api/im-gateway/v1/media/%s?mediaToken=%s", baseURL, mediaID, token)
	uploadURL := fmt.Sprintf("%s/api/im-gateway/v1/media/%s/upload?mediaToken=%s", baseURL, mediaID, token)
	writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "media": map[string]any{"id": mediaID, "type": media.Type, "fileName": media.FileName, "mimeType": media.MimeType, "url": downloadURL, "sizeBytes": media.SizeBytes, "durationMs": media.DurationMs}, "upload": map[string]any{"method": http.MethodPut, "url": uploadURL, "contentType": media.MimeType, "maxBytes": deviceGatewayMaxMediaBytes}, "download": map[string]any{"url": downloadURL}, "expiresAt": time.Now().Add(24 * time.Hour).UnixMilli()})
}

func normalizeDeviceInputModality(messageType, mimeType string) string {
	messageType = strings.ToLower(strings.TrimSpace(messageType))
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	switch {
	case messageType == "voice" || messageType == "audio" || strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case messageType == "image" || strings.HasPrefix(mimeType, "image/"):
		return "image"
	default:
		return ""
	}
}

func (g *DeviceGateway) handleMedia(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/im-gateway/v1/media/"), "/"), "/")
	if len(parts) == 2 && parts[1] == "upload" {
		if r.Method != http.MethodPut {
			writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "PUT required")
			return
		}
		if err := g.storeMediaUpload(r, parts[0]); err != nil {
			writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "mediaId": parts[0]})
		return
	}
	if len(parts) == 1 && parts[0] != "" {
		if r.Method != http.MethodGet {
			writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET required")
			return
		}
		media, err := g.mediaForDownload(r, parts[0])
		if err != nil {
			writeDeviceError(w, http.StatusNotFound, "not_found", "media not found")
			return
		}
		if media.MimeType != "" {
			w.Header().Set("Content-Type", media.MimeType)
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", media.FileName))
		w.Header().Set("Content-Length", strconv.FormatInt(int64(len(media.Data)), 10))
		_, _ = w.Write(media.Data)
		return
	}
	writeDeviceError(w, http.StatusNotFound, "not_found", "media not found")
}

func (g *DeviceGateway) handleOutgoing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeDeviceError(w, 405, "method_not_allowed", "GET required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	if r.URL.Query().Get("clientId") != p.ClientID {
		writeDeviceError(w, 403, "forbidden", "clientId does not match credential")
		return
	}
	cursor, _ := strconv.ParseInt(r.URL.Query().Get("cursor"), 10, 64)
	limit := devicePollInt(r.URL.Query().Get("limit"), 20, 1, 20)
	timeoutSeconds := devicePollInt(r.URL.Query().Get("timeout"), 0, 0, 30)
	deadline := time.NewTimer(time.Duration(timeoutSeconds) * time.Second)
	defer deadline.Stop()
	for {
		g.mu.Lock()
		state := g.clientLocked(p.ClientID)
		messages := make([]map[string]any, 0, limit)
		next := cursor
		hasMore := false
		for _, message := range state.messages {
			seq, _ := message["seq"].(int64)
			id, _ := message["id"].(string)
			if seq <= cursor || state.acked[id] {
				continue
			}
			if len(messages) >= limit {
				hasMore = true
				break
			}
			messages = append(messages, message)
			next = seq
		}
		notify := state.notify
		g.mu.Unlock()
		if len(messages) > 0 || timeoutSeconds == 0 {
			writeDeviceJSON(w, 200, map[string]any{"ok": true, "messages": messages, "nextCursor": fmt.Sprintf("%d", next), "hasMore": hasMore})
			return
		}
		select {
		case <-notify:
			// A message was queued after the scan. Re-read under the lock.
		case <-deadline.C:
			writeDeviceJSON(w, 200, map[string]any{"ok": true, "messages": messages, "nextCursor": fmt.Sprintf("%d", cursor), "hasMore": false})
			return
		case <-r.Context().Done():
			return
		}
	}
}

func devicePollInt(raw string, fallback, min, max int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func (g *DeviceGateway) handleAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, 405, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, 401, "unauthorized", "missing or invalid bearer token")
		return
	}
	var req coreim.ThirdPartyAckRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if err := coreim.NormalizeThirdPartyAckRequest(&req, coreim.ThirdPartyMaxAckIDs); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.ClientID != p.ClientID {
		writeDeviceError(w, 403, "forbidden", "clientId does not match credential")
		return
	}
	status := coreim.NormalizeThirdPartyAckStatus(req.Status)
	g.mu.Lock()
	state := g.clientLocked(p.ClientID)
	known := make(map[string]struct{}, len(state.messages))
	for _, message := range state.messages {
		if id, _ := message["id"].(string); id != "" {
			known[id] = struct{}{}
		}
	}
	for _, id := range req.MessageIDs {
		id = strings.TrimSpace(id)
		if _, exists := known[id]; exists {
			state.acked[id] = true
			state.ackStatus[id] = status
		}
	}
	pruneDeviceMessagesLocked(state)
	// Keep a bounded diagnostic receipt after the queue entry is removed. This
	// lets Hub distinguish an ESP playback failure from successful delivery
	// without turning ACK metadata into an unbounded per-device map.
	if len(state.ackStatus) > deviceGatewayMaxAckReceipts {
		for id := range state.ackStatus {
			delete(state.ackStatus, id)
			if len(state.ackStatus) <= deviceGatewayMaxAckReceipts {
				break
			}
		}
	}
	g.mu.Unlock()
	writeDeviceJSON(w, 200, map[string]any{"ok": true})
}

func (g *DeviceGateway) EnqueueReply(clientID, conversationID string, reply map[string]any) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return
	}
	g.mu.Lock()
	state := g.clientLocked(clientID)
	if !g.prepareOutgoingAudioLocked(clientID, reply, state.capabilities) {
		g.mu.Unlock()
		return
	}
	// Text glyphs use the same compact format as ambient glyphs. Validate the
	// optional attachment before retaining it in the device's outgoing queue.
	if glyphs := normalizeDeviceGlyphs(reply["glyphs"]); len(glyphs) > 0 {
		reply["glyphs"] = glyphs
	} else {
		delete(reply, "glyphs")
	}
	if !adaptDeviceGatewayReply(reply, state.capabilities) {
		g.mu.Unlock()
		return
	}
	state.next++
	reply["seq"] = state.next
	reply["id"] = fmt.Sprintf("hub_out_%d_%d", time.Now().UnixMilli(), state.next)
	reply["conversationId"] = firstDeviceValue(conversationID, "default")
	state.messages = append(state.messages, reply)
	pruneDeviceMessagesLocked(state)
	old := state.notify
	state.notify = make(chan struct{})
	close(old)
	g.mu.Unlock()
}

func (g *DeviceGateway) prepareOutgoingAudioLocked(clientID string, reply map[string]any, capabilities agent.ClientCapabilities) bool {
	replyType := strings.ToLower(strings.TrimSpace(deviceReplyString(reply, "type", "reply_type")))
	if replyType != "audio" && replyType != "voice" {
		return true
	}
	capabilities = agent.NormalizeClientCapabilities(&capabilities)
	raw := firstDeviceValue(deviceReplyString(reply, "file_data"), deviceReplyString(reply, "data"))
	raw = strings.TrimSpace(raw)
	if raw == "" {
		url := deviceReplyString(reply, "url")
		size := deviceReplyInt64(reply, "sizeBytes", "size_bytes")
		return url != "" && capabilities.SupportsOutputAudioDelivery("url", size)
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) == 0 {
		return false
	}
	size := int64(len(data))
	if capabilities.SupportsOutputAudioDelivery("inline", size) {
		reply["sizeBytes"] = size
		return true
	}
	if !capabilities.SupportsOutputAudioDelivery("url", size) || size > deviceGatewayMaxMediaBytes {
		return false
	}
	mediaID, token, err := newDeviceToken(16)
	if err != nil {
		return false
	}
	mimeType := firstDeviceValue(deviceReplyString(reply, "mimeType", "mime_type", "contentType", "content_type"), "audio/wav")
	fileName := safeDeviceFileName(firstDeviceValue(deviceReplyString(reply, "fileName", "file_name"), "response.wav"))
	g.pruneMediaLocked(time.Now().UTC())
	g.media[mediaID] = &deviceMedia{
		ClientID: clientID, ID: mediaID, Token: token, Type: "audio", FileName: fileName,
		MimeType: mimeType, SizeBytes: size, Data: data, Uploaded: true, LastAccessedAt: time.Now().UTC(),
	}
	reply["url"] = fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", mediaID, token)
	reply["sizeBytes"] = size
	reply["mime_type"] = mimeType
	delete(reply, "file_data")
	delete(reply, "data")
	return true
}

// preparePetAssetLocked publishes GUI-rendered RGB565 frames through the same
// short-lived, same-origin media transport used by server speech.  The caller
// holds g.mu. Invalid base64 or an unexpected byte count disables the remote
// asset and leaves the device's native skin renderer as a safe fallback.
func (g *DeviceGateway) preparePetAssetLocked(clientID string, asset *DevicePetAsset, animated bool) *devicePetAssetReference {
	asset = normalizeDevicePetAsset(asset)
	if asset == nil {
		return nil
	}
	encoded := []string{asset.Data}
	if animated && len(asset.Frames) > 0 {
		encoded = append(encoded, asset.Frames...)
	}
	expected := asset.Width * asset.Height * 2
	if expected <= 0 || expected > 128*128*2 {
		return nil
	}
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
	_, _ = digest.Write([]byte(fmt.Sprintf("/%d/%d", asset.Width, asset.Height)))
	for _, frame := range frames {
		_, _ = digest.Write(frame)
	}
	revision := hex.EncodeToString(digest.Sum(nil)[:8])

	g.pruneMediaLocked(time.Now().UTC())
	urls := make([]string, 0, len(frames))
	for index, frame := range frames {
		mediaID, token, err := newDeviceToken(16)
		if err != nil {
			return nil
		}
		g.media[mediaID] = &deviceMedia{
			ClientID: clientID, ID: mediaID, Token: token, Type: "pet_asset",
			FileName: fmt.Sprintf("pet-%s-%d.rgb565le", revision, index),
			MimeType: "application/vnd.maclaw.rgb565le", SizeBytes: int64(len(frame)),
			Data: frame, Uploaded: true, LastAccessedAt: time.Now().UTC(),
		}
		urls = append(urls, fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", mediaID, token))
	}
	return &devicePetAssetReference{
		Encoding: asset.Encoding, Width: asset.Width, Height: asset.Height,
		URLs: urls, FrameMS: 700, Revision: revision,
	}
}

// EnqueueMachineReply broadcasts a GUI-originated device configuration update
// only to the hardware credentials paired with that GUI machine.
func (g *DeviceGateway) EnqueueMachineReply(machineID, conversationID string, reply map[string]any) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" || reply == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	replyType, _ := reply["reply_type"].(string)
	if strings.TrimSpace(replyType) == "" {
		replyType, _ = reply["type"].(string)
	}
	volume := 0
	isVolumeConfig := false
	if strings.EqualFold(strings.TrimSpace(replyType), "hardware_config") {
		extra, _ := reply["extra"].(map[string]any)
		volume, isVolumeConfig = deviceVolume(extra["volume"])
	}
	for _, principal := range g.tokens {
		if principal.MachineID != machineID {
			continue
		}
		state := g.clientLocked(principal.ClientID)
		message := make(map[string]any, len(reply)+4)
		for key, value := range reply {
			message[key] = value
		}
		if replyType, _ := message["reply_type"].(string); strings.TrimSpace(replyType) != "" {
			message["type"] = replyType
		}
		if !g.prepareOutgoingAudioLocked(principal.ClientID, message, state.capabilities) {
			continue
		}
		if !adaptDeviceGatewayReply(message, state.capabilities) {
			continue
		}
		if isVolumeConfig && replaceQueuedDeviceVolume(state, volume) {
			old := state.notify
			state.notify = make(chan struct{})
			close(old)
			continue
		}
		state.next++
		message["seq"] = state.next
		message["id"] = fmt.Sprintf("hub_hardware_%d_%d", time.Now().UnixMilli(), state.next)
		message["conversationId"] = firstDeviceValue(conversationID, "system")
		state.messages = append(state.messages, message)
		pruneDeviceMessagesLocked(state)
		old := state.notify
		state.notify = make(chan struct{})
		close(old)
	}
}

const deviceGatewayWelcomeMaxBytes = 96 * 1024

// UpdateMachineWelcome persists the boot-time greeting chosen in the GUI.
// replaceAudio makes an empty payload an intentional clear; callers that only
// change the switch can leave it false to preserve the existing sound.
func (g *DeviceGateway) UpdateMachineWelcome(machineID string, enabled bool, audioBase64 string, replaceAudio bool) error {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return fmt.Errorf("machine ID is required")
	}
	audioBase64 = strings.TrimSpace(audioBase64)
	if audioBase64 != "" {
		audio, err := base64.StdEncoding.DecodeString(audioBase64)
		if err != nil || len(audio) == 0 || len(audio) > deviceGatewayWelcomeMaxBytes {
			return fmt.Errorf("invalid welcome audio")
		}
	}
	g.mu.Lock()
	config := g.hardware[machineID]
	config.WelcomeEnabled = enabled
	if replaceAudio {
		config.WelcomeAudio = audioBase64
	} else if audioBase64 != "" {
		config.WelcomeAudio = audioBase64
	}
	g.hardware[machineID] = config
	err := g.persistTokensLocked()
	g.mu.Unlock()
	return err
}

// UpdateMachineVolume makes the requested speaker level durable for a GUI
// machine. This lets a device that pairs or reconnects later receive the
// current level during its handshake, not only while it happened to be online.
func (g *DeviceGateway) UpdateMachineVolume(machineID string, value any) error {
	machineID = strings.TrimSpace(machineID)
	volume, ok := deviceVolume(value)
	if machineID == "" || !ok {
		return fmt.Errorf("invalid hardware volume")
	}
	g.mu.Lock()
	config := g.hardware[machineID]
	config.Volume = &volume
	g.hardware[machineID] = config
	err := g.persistTokensLocked()
	g.mu.Unlock()
	return err
}

// queueHardwareConfigForClientLocked places durable, lightweight settings
// ahead of boot-time media. At present this is the speaker volume; keeping it
// in the handshake path also covers a device paired after the desktop setting
// was chosen.
func (g *DeviceGateway) queueHardwareConfigForClientLocked(principal devicePrincipal, state *deviceClientState) {
	if state == nil {
		return
	}
	config := g.hardware[principal.MachineID]
	if config.Volume == nil {
		return
	}
	// A reconnect or rapid slider movement can arrive before the ESP ACKs the
	// prior setting. Coalesce into its pending control message so the device
	// always receives the latest level without filling the queue with stale
	// values that could later overwrite the user's last choice.
	if replaceQueuedDeviceVolume(state, *config.Volume) {
		return
	}
	message := map[string]any{"reply_type": "hardware_config", "type": "hardware_config", "extra": map[string]any{"volume": *config.Volume}}
	if !adaptDeviceGatewayReply(message, state.capabilities) {
		return
	}
	state.next++
	message["seq"] = state.next
	message["id"] = fmt.Sprintf("hub_hardware_config_%d_%d", time.Now().UnixMilli(), state.next)
	message["conversationId"] = "system"
	state.messages = append(state.messages, message)
	pruneDeviceMessagesLocked(state)
	old := state.notify
	state.notify = make(chan struct{})
	close(old)
}

func replaceQueuedDeviceVolume(state *deviceClientState, volume int) bool {
	if state == nil {
		return false
	}
	for index := len(state.messages) - 1; index >= 0; index-- {
		message := state.messages[index]
		if deviceReplyString(message, "type", "reply_type") != "hardware_config" {
			continue
		}
		message["extra"] = map[string]any{"volume": volume}
		return true
	}
	return false
}

// queueWelcomeForBootLocked emits the greeting only after a freshly booted
// device finishes its handshake. Its durable boot ID prevents a Hub restart
// or a capability-refresh handshake from replaying the greeting.
func (g *DeviceGateway) queueWelcomeForBootLocked(principal devicePrincipal, bootSessionID string, state *deviceClientState) {
	if bootSessionID == "" || state == nil {
		return
	}
	config := g.hardware[principal.MachineID]
	if !config.WelcomeEnabled || config.WelcomeAudio == "" || principal.LastWelcomeBootID == bootSessionID {
		return
	}
	// principal was resolved just before this mutex was acquired. Check the
	// durable map as well so two overlapping handshakes cannot both emit the
	// same boot's greeting.
	for _, candidate := range g.tokens {
		if candidate.ClientID == principal.ClientID && candidate.MachineID == principal.MachineID && candidate.LastWelcomeBootID == bootSessionID {
			return
		}
	}
	message := map[string]any{"reply_type": "audio", "type": "audio", "mime_type": "audio/wav", "file_data": config.WelcomeAudio, "bootSessionId": bootSessionID}
	if !g.prepareOutgoingAudioLocked(principal.ClientID, message, state.capabilities) {
		return
	}
	if !adaptDeviceGatewayReply(message, state.capabilities) {
		return
	}
	state.next++
	message["seq"] = state.next
	message["id"] = fmt.Sprintf("hub_welcome_%d_%d", time.Now().UnixMilli(), state.next)
	message["conversationId"] = "system"
	state.messages = append(state.messages, message)
	pruneDeviceMessagesLocked(state)
	old := state.notify
	state.notify = make(chan struct{})
	close(old)
	for token, candidate := range g.tokens {
		if candidate.ClientID != principal.ClientID || candidate.MachineID != principal.MachineID {
			continue
		}
		candidate.LastWelcomeBootID = bootSessionID
		g.tokens[token] = candidate
	}
	if err := g.persistTokensLocked(); err != nil {
		log.Printf("device gateway: persist welcome boot ID for client %q: %v", principal.ClientID, err)
	}
}

func adaptDeviceGatewayReply(reply map[string]any, capabilities agent.ClientCapabilities) bool {
	if reply == nil {
		return false
	}
	capabilities = agent.NormalizeClientCapabilities(&capabilities)
	replyType := strings.ToLower(strings.TrimSpace(deviceReplyString(reply, "type", "reply_type")))
	mimeType := deviceReplyString(reply, "mimeType", "mime_type", "contentType", "content_type")
	sizeBytes := deviceReplyInt64(reply, "sizeBytes", "size_bytes")
	switch replyType {
	case "hardware_config":
		extra, _ := reply["extra"].(map[string]any)
		if extra == nil {
			return false
		}
		return validDeviceVolume(extra["volume"]) && capabilities.Features.VolumeControl
	case "image":
		return capabilities.SupportsOutputMIME("image", firstDeviceValue(mimeType, "image/png"))
	case "file":
		return capabilities.SupportsOutputMIME("file", mimeType) && capabilities.SupportsOutputBytes("file", sizeBytes)
	case "voice", "audio":
		sizeBytes := deviceReplyInt64(reply, "sizeBytes", "size_bytes")
		delivery := "inline"
		if deviceReplyString(reply, "url") != "" {
			delivery = "url"
		}
		return capabilities.SupportsOutput("audio") && capabilities.Output.Audio != nil && capabilities.Output.Audio.Playback &&
			capabilities.SupportsOutputMIME("audio", mimeType) &&
			capabilities.SupportsOutputAudioDelivery(delivery, sizeBytes)
	case "ambient":
		return capabilities.Features.AmbientDisplay
	case "pet_state":
		return capabilities.Features.PetStates
	case "meeting_result":
		if !capabilities.Features.MeetingRecorder || !capabilities.SupportsOutput("text") {
			return false
		}
		text := deviceReplyString(reply, "text")
		if capabilities.Output.Text != nil && capabilities.Output.Text.MaxChars > 0 {
			runes := []rune(text)
			if len(runes) > capabilities.Output.Text.MaxChars {
				reply["text"] = string(runes[:capabilities.Output.Text.MaxChars])
			}
		}
		return true
	default:
		if !capabilities.SupportsOutput("text") {
			return false
		}
		text := deviceReplyString(reply, "text")
		if capabilities.Output.Text != nil && capabilities.Output.Text.MaxChars > 0 {
			runes := []rune(text)
			if len(runes) > capabilities.Output.Text.MaxChars {
				text = string(runes[:capabilities.Output.Text.MaxChars])
				reply["text"] = text
			}
		}
		return strings.TrimSpace(text) != ""
	}
}

func validDeviceVolume(value any) bool {
	_, ok := deviceVolume(value)
	return ok
}

func deviceVolume(value any) (int, bool) {
	var volume int64
	switch value := value.(type) {
	case int:
		volume = int64(value)
	case int64:
		volume = value
	case float64:
		if value != float64(int64(value)) {
			return 0, false
		}
		volume = int64(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, false
		}
		volume = parsed
	default:
		return 0, false
	}
	if volume < 0 || volume > 100 {
		return 0, false
	}
	return int(volume), true
}

func deviceReplyString(reply map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := reply[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func deviceReplyInt64(reply map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := reply[key].(type) {
		case int64:
			return value
		case int:
			return int64(value)
		case float64:
			return int64(value)
		case json.Number:
			parsed, _ := value.Int64()
			return parsed
		}
	}
	return 0
}

// UpdateMachineAmbient accepts the GUI's compact weather result and publishes
// it to every paired hardware surface. The Hub never queries a weather vendor;
// it only relays GUI-owned context, preserving the existing trust boundary.
func (g *DeviceGateway) UpdateMachineAmbient(machineID string, ambient map[string]any) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return
	}
	normalized, ok := normalizeDeviceAmbient(ambient)
	if !ok {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, principal := range g.tokens {
		if principal.MachineID != machineID {
			continue
		}
		state := g.clientLocked(principal.ClientID)
		state.ambient = normalized
		state.next++
		state.messages = append(state.messages, map[string]any{
			"seq": state.next, "id": fmt.Sprintf("hub_ambient_%d_%d", time.Now().UnixMilli(), state.next),
			"type": "ambient", "conversationId": "system", "ambient": normalized,
		})
		pruneDeviceMessagesLocked(state)
		old := state.notify
		state.notify = make(chan struct{})
		close(old)
	}
}

func (g *DeviceGateway) latestAmbientLocked(clientID string) (map[string]any, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.clientLocked(clientID)
	return state.ambient, state.ambient != nil
}

func normalizeDeviceAmbient(raw map[string]any) (map[string]any, bool) {
	weatherRaw, ok := raw["weather"].(map[string]any)
	if !ok {
		return nil, false
	}
	summary, _ := weatherRaw["summary"].(string)
	location, _ := weatherRaw["location"].(string)
	temperature, ok := deviceAmbientNumber(weatherRaw["temperatureC"])
	summary = strings.TrimSpace(summary)
	if summary == "" || !ok || temperature < -80 || temperature > 80 {
		return nil, false
	}
	expiresAt, _ := raw["expiresAt"].(float64)
	if expiresAt <= 0 {
		expiresAt = float64(time.Now().Add(2 * time.Hour).UnixMilli())
	}
	normalized := map[string]any{
		"weather":   map[string]any{"summary": summary, "temperatureC": int(temperature), "location": strings.TrimSpace(location)},
		"expiresAt": int64(expiresAt),
	}
	if glyphs := normalizeDeviceGlyphs(raw["glyphs"]); len(glyphs) > 0 {
		normalized["glyphs"] = glyphs
	}
	return normalized, true
}

// normalizeDeviceGlyphs accepts only the fixed-size monochrome glyph format
// consumed by ESP devices. Keeping validation at the Hub boundary prevents a
// machine from using the durable ambient channel as an unbounded data relay.
func normalizeDeviceGlyphs(raw any) map[string]string {
	values, ok := raw.(map[string]any)
	if !ok || len(values) == 0 || len(values) > deviceGlyphMaxPerPayload {
		return nil
	}
	normalized := make(map[string]string, len(values))
	for key, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || !validDeviceGlyphKey(key) {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil || len(decoded) != deviceGlyphBytesPerGlyph {
			continue
		}
		normalized[key] = value
	}
	return normalized
}

func validDeviceGlyphKey(value string) bool {
	if len(value) != 6 || value[0:2] != "U+" {
		return false
	}
	for _, ch := range value[2:] {
		if !((ch >= '0' && ch <= '9') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	parsed, err := strconv.ParseUint(value[2:], 16, 32)
	return err == nil && parsed >= 0x20 && parsed <= 0xFFFF && !(parsed >= 0xD800 && parsed <= 0xDFFF)
}

func deviceAmbientNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case float32:
		return float64(number), true
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// UpdateMachinePetProfile applies the GUI's current pet choice to every
// device token paired through that GUI. It is sent on every device reply, so
// changing the GUI setting converges without exposing the GUI to the public
// network.
func (g *DeviceGateway) UpdateMachinePetProfile(machineID, skin string, motionEnabled bool) {
	g.updateMachinePetProfileAsset(machineID, skin, motionEnabled, nil)
}

func (g *DeviceGateway) UpdateMachinePetProfileAsset(machineID, skin string, motionEnabled bool, asset map[string]any) {
	g.updateMachinePetProfileAsset(machineID, skin, motionEnabled, DevicePetAssetFromMap(asset))
}

func (g *DeviceGateway) updateMachinePetProfileAsset(machineID, skin string, motionEnabled bool, asset *DevicePetAsset) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return
	}
	profile := normalizeDevicePetProfileAsset(skin, motionEnabled, asset)
	g.mu.Lock()
	defer g.mu.Unlock()
	// A profile update may race with the physical device consuming a pairing
	// code. Refresh pending pairings too, otherwise that device is born with the
	// old skin even though the GUI already shows the new one.
	for code, pairing := range g.pairings {
		if pairing.MachineID == machineID && !devicePetProfilesEqual(pairing.Pet, profile) {
			pairing.Pet = profile
			g.pairings[code] = pairing
		}
	}
	changedTokens := false
	notifiedClients := make(map[string]struct{})
	for token, principal := range g.tokens {
		if principal.MachineID != machineID || devicePetProfilesEqual(principal.Pet, profile) {
			continue
		}
		principal.Pet = profile
		g.tokens[token] = principal
		changedTokens = true
		// Re-pairing can leave more than one valid token for the same client ID.
		// Update every credential, but wake and enqueue to the client only once.
		if _, exists := notifiedClients[principal.ClientID]; exists {
			continue
		}
		notifiedClients[principal.ClientID] = struct{}{}
		state := g.clientLocked(principal.ClientID)
		var assetRef *devicePetAssetReference
		if state.capabilities.Features.PetAsset {
			assetRef = g.preparePetAssetLocked(principal.ClientID, profile.Asset,
				state.capabilities.Features.PetAnimation && profile.MotionEnabled)
		}
		state.next++
		message := map[string]any{
			"seq": state.next, "id": fmt.Sprintf("hub_pet_%d_%d", time.Now().UnixMilli(), state.next),
			"type": "pet_profile", "conversationId": "system",
			"pet_skin": profile.Skin, "pet_motion_enabled": profile.MotionEnabled,
		}
		if assetRef != nil {
			message["pet_asset"] = *assetRef
		}
		state.messages = append(state.messages, message)
		pruneDeviceMessagesLocked(state)
		old := state.notify
		state.notify = make(chan struct{})
		close(old)
	}
	if changedTokens {
		if err := g.persistTokensLocked(); err != nil {
			// The live session is already corrected. Keep serving it and surface the
			// durability failure so operators know a Hub restart could regress it.
			log.Printf("device gateway: persist pet profile for machine %q: %v", machineID, err)
		}
	}
}

func devicePetProfilesEqual(left, right devicePetProfile) bool {
	if left.Skin != right.Skin || left.MotionEnabled != right.MotionEnabled {
		return false
	}
	return devicePetAssetsEqual(left.Asset, right.Asset)
}

func devicePetAssetsEqual(left, right *DevicePetAsset) bool {
	if left == nil || right == nil {
		return left == right
	}
	if left.Encoding != right.Encoding || left.Width != right.Width || left.Height != right.Height || left.Data != right.Data || len(left.Frames) != len(right.Frames) {
		return false
	}
	for index := range left.Frames {
		if left.Frames[index] != right.Frames[index] {
			return false
		}
	}
	return true
}

func normalizeDevicePetProfile(skin string, motionEnabled bool) devicePetProfile {
	return normalizeDevicePetProfileAsset(skin, motionEnabled, nil)
}

func normalizeDevicePetProfileAsset(skin string, motionEnabled bool, asset *DevicePetAsset) devicePetProfile {
	skin = strings.TrimSpace(strings.ToLower(skin))
	if skin == "" {
		skin = "clawmate"
	}
	return devicePetProfile{Skin: skin, MotionEnabled: motionEnabled, Asset: normalizeDevicePetAsset(asset)}
}

func normalizeDevicePetAsset(asset *DevicePetAsset) *DevicePetAsset {
	if asset == nil || asset.Encoding != "rgb565le" || asset.Width < 32 || asset.Width > 128 || asset.Height < 32 || asset.Height > 128 || len(asset.Data) == 0 || len(asset.Data) > 50000 || len(asset.Frames) > 1 {
		return nil
	}
	for _, frame := range asset.Frames {
		if len(frame) == 0 || len(frame) > 50000 {
			return nil
		}
	}
	copy := *asset
	return &copy
}

// DevicePetAssetFromMap accepts only the concise primitive shape emitted by
// the authenticated GUI WebSocket.  It keeps the ws package independent from
// im, avoiding a Hub import cycle.
func DevicePetAssetFromMap(raw map[string]any) *DevicePetAsset {
	if raw == nil {
		return nil
	}
	asset := &DevicePetAsset{}
	asset.Encoding, _ = raw["encoding"].(string)
	asset.Data, _ = raw["data"].(string)
	if frames, ok := raw["frames"].([]any); ok {
		for _, frame := range frames {
			if text, ok := frame.(string); ok {
				asset.Frames = append(asset.Frames, text)
			}
		}
	}
	switch value := raw["width"].(type) {
	case float64:
		asset.Width = int(value)
	case int:
		asset.Width = value
	}
	switch value := raw["height"].(type) {
	case float64:
		asset.Height = int(value)
	case int:
		asset.Height = value
	}
	return normalizeDevicePetAsset(asset)
}

func (g *DeviceGateway) incomingAttachments(clientID string, refs []struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	FileName  string `json:"fileName"`
	MimeType  string `json:"mimeType"`
	Data      string `json:"data"`
	SizeBytes int64  `json:"sizeBytes"`
}) ([]MessageAttachment, error) {
	attachments := make([]MessageAttachment, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" {
			continue
		}
		g.mu.Lock()
		media := g.media[strings.TrimSpace(ref.ID)]
		if media != nil {
			media.LastAccessedAt = time.Now().UTC()
		}
		g.mu.Unlock()
		if media == nil || !media.Uploaded || media.ClientID != clientID {
			return nil, fmt.Errorf("media %s not found", strings.TrimSpace(ref.ID))
		}
		attachments = append(attachments, MessageAttachment{Type: firstDeviceValue(ref.Type, media.Type), FileName: firstDeviceValue(ref.FileName, media.FileName), MimeType: firstDeviceValue(ref.MimeType, media.MimeType), Data: encodeDeviceData(media.Data), Size: int64(len(media.Data))})
	}
	return attachments, nil
}

func (g *DeviceGateway) storeMediaUpload(r *http.Request, id string) error {
	g.mu.Lock()
	media := g.media[strings.TrimSpace(id)]
	g.mu.Unlock()
	if media == nil || !deviceMediaTokenOK(r, media.Token) {
		return fmt.Errorf("media not found")
	}
	if r.ContentLength > deviceGatewayMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", deviceGatewayMaxMediaBytes)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, deviceGatewayMaxMediaBytes+1))
	if err != nil {
		return err
	}
	if len(data) > int(deviceGatewayMaxMediaBytes) {
		return fmt.Errorf("media exceeds %d bytes", deviceGatewayMaxMediaBytes)
	}
	if media.SizeBytes > 0 && int64(len(data)) != media.SizeBytes {
		return fmt.Errorf("media size mismatch")
	}
	g.mu.Lock()
	media.Data = data
	media.Uploaded = true
	media.SizeBytes = int64(len(data))
	if media.MimeType == "" {
		media.MimeType = strings.TrimSpace(r.Header.Get("Content-Type"))
	}
	media.LastAccessedAt = time.Now().UTC()
	g.mu.Unlock()
	return nil
}

func (g *DeviceGateway) mediaForDownload(r *http.Request, id string) (*deviceMedia, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	media := g.media[strings.TrimSpace(id)]
	if media == nil || !media.Uploaded || !deviceMediaTokenOK(r, media.Token) {
		return nil, fmt.Errorf("media not found")
	}
	media.LastAccessedAt = time.Now().UTC()
	out := *media
	out.Data = append([]byte(nil), media.Data...)
	return &out, nil
}

func (g *DeviceGateway) pruneMediaLocked(now time.Time) {
	for id, media := range g.media {
		if media.LastAccessedAt.Before(now.Add(-24 * time.Hour)) {
			delete(g.media, id)
		}
	}
	for len(g.media) > deviceGatewayMaxMediaObjects {
		var oldestID string
		var oldest time.Time
		for id, media := range g.media {
			if oldestID == "" || media.LastAccessedAt.Before(oldest) {
				oldestID, oldest = id, media.LastAccessedAt
			}
		}
		delete(g.media, oldestID)
	}
}

func (g *DeviceGateway) clientLocked(clientID string) *deviceClientState {
	state := g.clients[clientID]
	if state == nil {
		state = &deviceClientState{acked: make(map[string]bool), ackStatus: make(map[string]string), notify: make(chan struct{})}
		g.clients[clientID] = state
	} else if state.ackStatus == nil {
		// Older tests and in-memory states can predate delivery-status tracking.
		state.ackStatus = make(map[string]string)
	}
	return state
}

// pruneDeviceMessagesLocked bounds per-device memory and drops acknowledged
// entries eagerly. The cursor is monotonic, so retaining old acknowledged
// maps only wastes memory; if an offline device falls over the hard cap it can
// still recover current durable state through the next handshake.
func pruneDeviceMessagesLocked(state *deviceClientState) {
	if state == nil || len(state.messages) == 0 {
		return
	}
	kept := state.messages[:0]
	for _, message := range state.messages {
		id, _ := message["id"].(string)
		if id != "" && state.acked[id] {
			delete(state.acked, id)
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) > deviceGatewayMaxQueuedMessages {
		drop := len(kept) - deviceGatewayMaxQueuedMessages
		for index := 0; index < drop; index++ {
			id, _ := kept[index]["id"].(string)
			delete(state.acked, id)
			delete(state.ackStatus, id)
		}
		copy(kept, kept[drop:])
		kept = kept[:deviceGatewayMaxQueuedMessages]
	}
	state.messages = kept
}

func (g *DeviceGateway) principal(r *http.Request) (devicePrincipal, bool) {
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return devicePrincipal{}, false
	}
	token := strings.TrimSpace(auth[len("Bearer "):])
	g.mu.Lock()
	p, ok := g.tokens[token]
	g.mu.Unlock()
	return p, ok
}

func newDeviceToken(bytes int) (string, string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(raw)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	return id, hex.EncodeToString(raw), nil
}
func deviceMediaTokenOK(r *http.Request, expected string) bool {
	provided := strings.TrimSpace(r.URL.Query().Get("mediaToken"))
	return provided != "" && len(provided) == len(expected) && subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}
func deviceRequestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}
func safeDeviceFileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "attachment"
	}
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}
func encodeDeviceData(data []byte) string { return base64.StdEncoding.EncodeToString(data) }
func decodeDeviceJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(out); err != nil {
		writeDeviceError(w, 400, "bad_request", "invalid JSON")
		return false
	}
	return true
}
func writeDeviceJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeDeviceError(w http.ResponseWriter, status int, code, message string) {
	writeDeviceJSON(w, status, map[string]any{"ok": false, "error": map[string]string{"code": code, "message": message}})
}
func firstDeviceValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
