package im

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"sort"
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
	codePairAttempts     map[string]*deviceVoicePairAttempt
	machineSender        MachineMessageSender

	mu       sync.Mutex
	pairings map[string]devicePairing
	tokens   map[string]devicePrincipal
	clients  map[string]*deviceClientState
	media    map[string]*deviceMedia
	hardware map[string]deviceHardwareConfig
	// dispatchLocks serializes the short Hub-relay hand-off for one physical
	// client with deletion of that same binding. It is deliberately per-device:
	// an ESP32 hand-off must never make another bound ESP32 wait.
	dispatchLocks map[string]*sync.Mutex
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

const (
	// Hardware binding capacity is fixed for every GUI. Keep this constant at
	// the protocol boundary so no client can create a sixth device by bypassing
	// the GUI.
	defaultMachineHardwareMaxDevices = maxMachineHardwareDevices
	maxMachineHardwareDevices        = 5
)

type persistedDeviceCredentials struct {
	Tokens          map[string]devicePrincipal      `json:"tokens"`
	MachineHardware map[string]deviceHardwareConfig `json:"machineHardware,omitempty"`
}

// deviceHardwareConfig is Hub-owned state for the hardware paired to one GUI.
// WelcomeAudio is base64 PCM WAV so Hub can play it after a hardware reboot
// even while the GUI is offline.
type deviceHardwareConfig struct {
	// Pointer preserves compatibility with credentials written before the
	// master switch existed: nil means legacy-enabled until the GUI publishes
	// its authoritative true/false value on connect.
	Enabled        *bool  `json:"enabled,omitempty"`
	WelcomeEnabled bool   `json:"welcomeEnabled,omitempty"`
	WelcomeAudio   string `json:"welcomeAudio,omitempty"`
	// Volume is a pointer so zero (mute) remains distinguishable from the
	// legacy "not configured" state. It remains the default for bindings that
	// have not selected their own level yet.
	Volume *int `json:"volume,omitempty"`
	// DeviceVolumes stores the explicit speaker level for an individual bound
	// ESP32. Keeping it with Hub-owned binding state makes the setting survive
	// desktop restarts and avoids broadcasting one device's change to others.
	DeviceVolumes map[string]int `json:"deviceVolumes,omitempty"`
	// AllowCustomPets is false by default. Hub enforces it as the authority for
	// per-device profile writes rather than trusting the desktop UI to hide the
	// selector.
	AllowCustomPets bool `json:"allowCustomPets,omitempty"`
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
	deviceCodePairAttemptWindow  = time.Minute
	deviceCodePairAttemptLimit   = 12
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
	LastWelcomeBootID string    `json:"lastWelcomeBootId,omitempty"`
	ClientName        string    `json:"clientName,omitempty"`
	ProtocolVersion   string    `json:"protocolVersion,omitempty"`
	PairedAt          time.Time `json:"pairedAt,omitempty"`
	LastSeenAt        time.Time `json:"lastSeenAt,omitempty"`
}

// HardwareDevice is the credential-free view exposed to the owning GUI.
// Bearer tokens never leave Hub after the one-time pairing exchange.
type HardwareDevice struct {
	ClientID        string                    `json:"clientId"`
	ClientName      string                    `json:"clientName,omitempty"`
	ProtocolVersion string                    `json:"protocolVersion,omitempty"`
	PairedAt        time.Time                 `json:"pairedAt,omitempty"`
	LastSeenAt      time.Time                 `json:"lastSeenAt,omitempty"`
	Online          bool                      `json:"online"`
	LastAckStatus   string                    `json:"lastAckStatus,omitempty"`
	Volume          *int                      `json:"volume,omitempty"`
	PetSkin         string                    `json:"petSkin,omitempty"`
	Capabilities    *agent.ClientCapabilities `json:"capabilities,omitempty"`
}

// MachineHardwareBindingState is the credential-free capacity view exposed to
// the owning GUI. Hub is the authority because it owns durable credentials.
type MachineHardwareBindingState struct {
	MaxDevices int `json:"maxDevices"`
	BoundCount int `json:"boundCount"`
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

// DevicePetAsset is GUI-rendered (not user-submitted) RGB565+A8. Keeping this
// with the authenticated device credential lets Hub relay the exact selected
// desktop pet without needing access to local pet-pack files.
type DevicePetAsset struct {
	Encoding string   `json:"encoding,omitempty"`
	Width    int      `json:"width,omitempty"`
	Height   int      `json:"height,omitempty"`
	Data     string   `json:"data,omitempty"`
	Frames   []string `json:"frames,omitempty"`
	FrameMS  int      `json:"frameMs,omitempty"`
}

// devicePetAssetReference is intentionally small enough for the handshake and
// poll JSON buffers used by embedded clients. The RGB565+A8 payload itself lives
// in the existing authenticated media store and is fetched after TLS setup.
type devicePetAssetReference struct {
	Encoding string   `json:"encoding"`
	Width    int      `json:"width"`
	Height   int      `json:"height"`
	URLs     []string `json:"urls"`
	FrameMS  int      `json:"frameMs,omitempty"`
	Revision string   `json:"revision"`
	SHA256   []string `json:"sha256"`
}

type deviceClientState struct {
	next          int64
	messages      []map[string]any
	acked         map[string]bool
	ackStatus     map[string]string
	bootSessionID string
	activeReplies map[string]struct{}
	activeOrder   []string
	notify        chan struct{}
	ambient       map[string]any
	capabilities  agent.ClientCapabilities
	tools         []agent.ClientToolDefinition
	seenEvents    map[string]struct{}
	seenOrder     []string
	// capabilitiesDeclared distinguishes a legacy client that never completed
	// capability negotiation from a modern client that intentionally declares
	// a restricted input contract. Before handshake, preserve the historical
	// text/audio upload behavior; after handshake, enforce the declaration.
	capabilitiesDeclared bool
	lastSeenAt           time.Time
	// lastSeenFlushAt throttles durable presence updates. Live presence remains
	// exact in memory, while the credential store is refreshed at most once per
	// interval so a long-polling device does not write settings on every poll.
	lastSeenFlushAt time.Time
}

type deviceMedia struct {
	ClientID            string
	MachineID           string
	ID                  string
	Token               string
	Type                string
	FileName            string
	MimeType            string
	SizeBytes           int64
	DurationMs          int64
	Data                []byte
	Uploaded            bool
	Uploading           bool
	UploadReservedBytes int64
	LastAccessedAt      time.Time
	ExpiresAt           time.Time
	QueueRefs           int
}

const (
	devicePetAssetMaxDimension                        = 256
	devicePetAssetMaxEncodedFrameBytes                = 270000
	devicePetAssetMaxFrames                           = 8
	deviceGatewayMaxMediaBytes                  int64 = 10 * 1024 * 1024
	deviceGatewayMaxMediaResidentBytes          int64 = 64 * 1024 * 1024
	deviceGatewayMaxMediaResidentBytesPerClient int64 = 16 * 1024 * 1024
	deviceGatewayMaxMediaObjects                      = 200
	deviceGatewayMaxMediaObjectsPerClient             = 64
	deviceGatewayMaxQueuedMessages                    = 100
	deviceGatewayMaxAckReceipts                       = 100
	deviceGatewayMaxSeenEvents                        = 2000
	deviceGatewayPresenceFlushInterval                = 5 * time.Minute
	deviceGatewayMediaTTL                             = 24 * time.Hour
	deviceGatewayQueuedMediaTTL                       = 5 * time.Minute
)

func NewDeviceGateway(plugin *RemoteGatewayPlugin) *DeviceGateway {
	return &DeviceGateway{plugin: plugin, pairings: make(map[string]devicePairing), tokens: make(map[string]devicePrincipal), clients: make(map[string]*deviceClientState), media: make(map[string]*deviceMedia), hardware: make(map[string]deviceHardwareConfig), dispatchLocks: make(map[string]*sync.Mutex), voicePairAttempts: make(map[string]*deviceVoicePairAttempt), codePairAttempts: make(map[string]*deviceVoicePairAttempt)}
}

// SetMachineMessageSender lets the HTTP-side hardware ACK complete an
// originating GUI preview request over the existing Hub WebSocket.
func (g *DeviceGateway) SetMachineMessageSender(sender MachineMessageSender) {
	g.mu.Lock()
	g.machineSender = sender
	g.mu.Unlock()
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
		copyHardware[machineID] = cloneDeviceHardwareConfig(config)
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
	case "/api/im-gateway/v1/tool-result":
		g.handleToolResult(w, r)
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
	if !g.allowCodePairAttempt(devicePairAttemptAddress(r), time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeDeviceError(w, http.StatusTooManyRequests, "rate_limited", "too many pairing attempts; retry later")
		return
	}
	g.exchangePairing(w, strings.TrimSpace(req.ClientID), pairCode)
}

func (g *DeviceGateway) exchangePairing(w http.ResponseWriter, clientID, pairCode string) {
	clientID = coreim.NormalizeThirdPartyID(clientID)
	if err := coreim.ValidateThirdPartyID("clientId", clientID); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	g.mu.Lock()
	pairing, ok := g.pairings[pairCode]
	g.mu.Unlock()
	if !ok || !pairing.ExpiresAt.After(time.Now()) {
		if ok {
			g.mu.Lock()
			if current, exists := g.pairings[pairCode]; exists && current.ExpiresAt.Equal(pairing.ExpiresAt) {
				delete(g.pairings, pairCode)
			}
			g.mu.Unlock()
		}
		writeDeviceError(w, http.StatusUnauthorized, "invalid_pairing_code", "pairing code is invalid or expired")
		return
	}
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot create credential")
		return
	}
	token := hex.EncodeToString(bytes[:])
	// Pairing can replace the bearer for an existing physical ID. Serialize that
	// credential rotation with the device's asynchronous inbound hand-off so an
	// old bearer cannot cross the relay boundary during re-pairing.
	dispatchMu := g.deviceDispatchMutex(clientID)
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	g.mu.Lock()
	currentPairing, pairingStillAvailable := g.pairings[pairCode]
	if !pairingStillAvailable || !currentPairing.ExpiresAt.Equal(pairing.ExpiresAt) || currentPairing.MachineID != pairing.MachineID {
		g.mu.Unlock()
		writeDeviceError(w, http.StatusUnauthorized, "invalid_pairing_code", "pairing code is invalid or already used")
		return
	}
	for _, principal := range g.tokens {
		if principal.ClientID == clientID && principal.MachineID != pairing.MachineID {
			g.mu.Unlock()
			writeDeviceError(w, http.StatusConflict, "client_id_already_bound", "clientId is already bound to another machine; remove the old binding before pairing again")
			return
		}
	}
	// Re-pairing an existing client stays available at capacity. A sixth physical
	// device is refused before issuing a bearer, even if several pairing codes
	// are outstanding. The user must unbind a device before binding another.
	if !g.machineOwnsClientLocked(pairing.MachineID, clientID) && g.machineBoundClientCountLocked(pairing.MachineID) >= g.machineHardwareMaxDevicesLocked(pairing.MachineID) {
		maxDevices := g.machineHardwareMaxDevicesLocked(pairing.MachineID)
		g.mu.Unlock()
		writeDeviceError(w, http.StatusConflict, "hardware_device_limit_reached", fmt.Sprintf("hardware binding limit reached (%d devices); remove a bound device before binding a new one", maxDevices))
		return
	}
	// A physical client ID identifies one device across the gateway. Re-pairing
	// it on the same machine must revoke the earlier credential and discard that
	// device's stale queue before issuing the new bearer. Cross-machine transfer
	// requires an explicit unlink, preventing a valid pairing code from silently
	// taking over another GUI's hardware identity.
	revoked := make(map[string]devicePrincipal)
	for existingToken, principal := range g.tokens {
		if principal.ClientID == clientID {
			revoked[existingToken] = principal
			delete(g.tokens, existingToken)
		}
	}
	previousState := g.clients[clientID]
	delete(g.clients, clientID)
	removedMedia := make(map[string]*deviceMedia)
	for id, media := range g.media {
		if media.ClientID == clientID {
			removedMedia[id] = media
			delete(g.media, id)
		}
	}
	g.tokens[token] = devicePrincipal{ClientID: clientID, MachineID: pairing.MachineID, TenantID: pairing.TenantID, UserID: pairing.UserID, Pet: pairing.Pet, PairedAt: time.Now().UTC()}
	if err := g.persistTokensLocked(); err != nil {
		delete(g.tokens, token)
		for revokedToken, principal := range revoked {
			g.tokens[revokedToken] = principal
		}
		if previousState != nil {
			g.clients[clientID] = previousState
		}
		for id, media := range removedMedia {
			g.media[id] = media
		}
		g.mu.Unlock()
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot persist device credential")
		return
	}
	// Consume the one-time pairing only after its credential is durable. This
	// lets the physical device retry the same request after a transient store
	// failure without requiring the owner to generate a new code.
	if current, exists := g.pairings[pairCode]; exists && current.ExpiresAt.Equal(pairing.ExpiresAt) && current.MachineID == pairing.MachineID {
		delete(g.pairings, pairCode)
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
	return devicePairAttemptAddress(r) + "\x00" + strings.TrimSpace(clientID)
}

func (g *DeviceGateway) allowVoicePairAttempt(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return allowDevicePairAttemptLocked(g.voicePairAttempts, key, now, deviceVoicePairAttemptWindow, deviceVoicePairAttemptLimit)
}

func (g *DeviceGateway) allowCodePairAttempt(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.codePairAttempts == nil {
		g.codePairAttempts = make(map[string]*deviceVoicePairAttempt)
	}
	return allowDevicePairAttemptLocked(g.codePairAttempts, key, now, deviceCodePairAttemptWindow, deviceCodePairAttemptLimit)
}

func allowDevicePairAttemptLocked(attempts map[string]*deviceVoicePairAttempt, key string, now time.Time, window time.Duration, limit int) bool {
	if len(attempts) > 1024 {
		for candidate, attempt := range attempts {
			if attempt == nil || now.Sub(attempt.WindowStart) >= window {
				delete(attempts, candidate)
			}
		}
	}
	attempt := attempts[key]
	if attempt == nil || now.Sub(attempt.WindowStart) >= window {
		attempts[key] = &deviceVoicePairAttempt{WindowStart: now, Count: 1}
		return true
	}
	if attempt.Count >= limit {
		return false
	}
	attempt.Count++
	return true
}

func devicePairAttemptAddress(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	// RemoteAddr is the only address authenticated by the HTTP connection.
	// X-Forwarded-For is client-controlled unless the deployment has an explicit
	// trusted-proxy policy, so using it here would let attackers rotate buckets.
	host := strings.TrimSpace(r.RemoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	if host == "" {
		return "unknown"
	}
	return host
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
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
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
	p.ClientName = strings.TrimSpace(req.ClientName)
	p.ProtocolVersion = strings.TrimSpace(req.ProtocolVersion)
	p.LastSeenAt = time.Now().UTC()
	if err := g.updatePrincipalLocked(p); err != nil {
		g.mu.Unlock()
		writeDeviceError(w, http.StatusInternalServerError, "internal_error", "cannot persist device metadata")
		return
	}
	state := g.clientLocked(p.ClientID)
	state.lastSeenAt = p.LastSeenAt
	state.lastSeenFlushAt = p.LastSeenAt
	state.capabilities = capabilities
	state.tools = append([]agent.ClientToolDefinition(nil), req.Tools...)
	state.capabilitiesDeclared = req.ClientCapabilities != nil
	if bootSessionID != "" && state.bootSessionID != bootSessionID {
		// A cold boot is a new interaction session. Results, progress updates and
		// tool calls that were queued for the process which just disappeared are
		// no longer actionable and must not repaint the new standby screen. Drop
		// the old runtime queue first; durable control state is reconstructed below
		// from hardware config, Welcome, pet profile and ambient snapshots.
		g.resetDeviceRuntimeQueueForBootLocked(state)
		state.bootSessionID = bootSessionID
	}
	g.queueHardwareConfigForClientLocked(p, state)
	startupWelcomeQueued := g.queueWelcomeForBootLocked(p, bootSessionID, state)
	petProfile := p.Pet
	var petAsset *devicePetAssetReference
	if capabilities.Features.PetAsset {
		petAsset = g.preparePetAssetLocked(p.ClientID, petProfile.Asset,
			capabilities.Features.PetAnimation && petProfile.MotionEnabled,
			capabilities.Features.PetAssetMaxFrames)
	}
	meetingRecordings := g.meetingRecordings
	meetingTranscript := g.meetingTranscript
	meetingMinutes := g.meetingMinutes
	g.mu.Unlock()
	// Never embed RGB565+A8 base64 in the handshake. Even one 256px frame expands
	// to ~66 KiB and can exhaust the ESP's memory-sensitive TLS path.
	petProfile.Asset = nil
	response := map[string]any{"ok": true, "mode": "maclaw", "channelId": "thirdparty:" + p.ClientID, "serverTime": time.Now().UnixMilli(), "pet": petProfile, "startupWelcomeQueued": startupWelcomeQueued, "poll": map[string]int{"timeoutSec": 30, "maxTimeoutSec": 30, "maxBatchSize": 20, "maxLimit": 20}, "limits": map[string]int{"maxBodyBytes": 1048576, "maxMediaBytes": 10485760}}
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

func (g *DeviceGateway) resetDeviceRuntimeQueueForBootLocked(state *deviceClientState) {
	if state == nil {
		return
	}
	for _, message := range state.messages {
		g.releaseDeviceMessageMediaLocked(message)
	}
	state.messages = nil
	// ACK bookkeeping belongs to the discarded queue. Keeping it provides no
	// replay protection because every new message receives a fresh ID/sequence.
	state.acked = make(map[string]bool)
	state.ackStatus = make(map[string]string)
	state.activeReplies = make(map[string]struct{})
	state.activeOrder = nil
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
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
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
	clientTools := append([]agent.ClientToolDefinition(nil), state.tools...)
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
	// Record replay suppression only after the complete request has passed
	// validation. Otherwise a client that fixes a rejected attachment while
	// retaining the same eventId (as required for a retry) would receive a
	// false duplicate success and the corrected message would never reach GUI.
	if g.markDeviceEvent(p.ClientID, "incoming:"+strings.TrimSpace(req.EventID)) {
		writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "messageId": req.MessageID, "duplicate": true})
		return
	}
	conversationID := firstDeviceValue(req.ConversationID, "default")
	messageID := firstDeviceValue(req.MessageID, req.EventID)
	g.activateDeviceReply(p.ClientID, messageID)
	payload, _ := json.Marshal(map[string]any{"tenant_id": p.TenantID, "platform_uid": "thirdparty:" + p.ClientID + ":" + conversationID, "reply_target": "thirdparty:" + p.ClientID + ":" + conversationID, "message_id": messageID, "message_type": firstDeviceValue(req.Message.Type, "text"), "text": req.Message.Text, "attachments": attachments, "client_capabilities": capabilities, "client_tools": clientTools, "client_tool_context": agent.ClientToolContext{ClientID: p.ClientID, ConversationID: conversationID, ReplyToMessageID: messageID}})
	// The HTTP response is intentionally fast, while GUI relay happens on a
	// goroutine. Capture a per-device hand-off lock so an unlink that wins this
	// race revokes the credential before the goroutine can reach the GUI.
	dispatchMu := g.deviceDispatchMutex(p.ClientID)
	go g.forwardIncomingDeviceMessage(dispatchMu, deviceBearerToken(r), p, ownerID, payload)
	writeDeviceJSON(w, 200, map[string]any{"ok": true, "accepted": true, "messageId": req.MessageID, "duplicate": false})
}

// deviceDispatchMutex returns the small critical section shared by one
// device's asynchronous Hub hand-off and its unbind operation. It does not
// protect Agent execution and is never shared by unrelated devices.
func (g *DeviceGateway) deviceDispatchMutex(clientID string) *sync.Mutex {
	clientID = strings.TrimSpace(clientID)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.dispatchLocks == nil {
		g.dispatchLocks = make(map[string]*sync.Mutex)
	}
	if lock := g.dispatchLocks[clientID]; lock != nil {
		return lock
	}
	lock := &sync.Mutex{}
	g.dispatchLocks[clientID] = lock
	return lock
}

// forwardIncomingDeviceMessage validates the original bearer a second time at
// the asynchronous boundary. A successful DELETE is therefore a hard fence:
// a request which had already been accepted by HTTP cannot create a fresh GUI
// runtime after the device was unbound.
func (g *DeviceGateway) forwardIncomingDeviceMessage(dispatchMu *sync.Mutex, token string, expected devicePrincipal, ownerID string, payload json.RawMessage) {
	if g == nil || dispatchMu == nil || g.plugin == nil {
		return
	}
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	if !g.hasCurrentDeviceCredential(token, expected.ClientID) {
		return
	}
	g.plugin.HandleGatewayMessage(ownerID, payload)
}

func (g *DeviceGateway) hasCurrentDeviceCredential(token, clientID string) bool {
	token, clientID = strings.TrimSpace(token), strings.TrimSpace(clientID)
	if token == "" || clientID == "" {
		return false
	}
	g.mu.Lock()
	principal, ok := g.tokens[token]
	g.mu.Unlock()
	return ok && principal.ClientID == clientID
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
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
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
	now := time.Now().UTC()
	media := &deviceMedia{ClientID: p.ClientID, MachineID: p.MachineID, ID: mediaID, Token: token, Type: firstDeviceValue(req.Type, "file"), FileName: safeDeviceFileName(req.FileName), MimeType: strings.TrimSpace(req.MimeType), SizeBytes: req.SizeBytes, DurationMs: req.DurationMs, LastAccessedAt: now, ExpiresAt: now.Add(deviceGatewayMediaTTL)}
	g.mu.Lock()
	if !g.ensureMediaCapacityLocked(p.ClientID, 1, 0, "", now) {
		g.mu.Unlock()
		writeDeviceError(w, http.StatusServiceUnavailable, "media_capacity_exceeded", "media capacity is temporarily exhausted")
		return
	}
	g.media[mediaID] = media
	g.mu.Unlock()
	downloadURL := fmt.Sprintf("%s/api/im-gateway/v1/media/%s?mediaToken=%s", baseURL, mediaID, token)
	uploadURL := fmt.Sprintf("%s/api/im-gateway/v1/media/%s/upload?mediaToken=%s", baseURL, mediaID, token)
	writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "media": map[string]any{"id": mediaID, "type": media.Type, "fileName": media.FileName, "mimeType": media.MimeType, "url": downloadURL, "sizeBytes": media.SizeBytes, "durationMs": media.DurationMs}, "upload": map[string]any{"method": http.MethodPut, "url": uploadURL, "contentType": media.MimeType, "maxBytes": deviceGatewayMaxMediaBytes}, "download": map[string]any{"url": downloadURL}, "expiresAt": media.ExpiresAt.UnixMilli()})
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
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
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
		// The master switch can change while this request is waiting on a long
		// poll. Re-check it under the queue lock so a request that began while
		// enabled cannot deliver a message after hardware has been disabled.
		if !g.machineHardwareEnabledLocked(p.MachineID) {
			g.mu.Unlock()
			writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
			return
		}
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
			messages = append(messages, cloneDeviceMessage(message))
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
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
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
	type previewReceipt struct {
		MachineID string
		RequestID string
		MessageID string
	}
	previewReceipts := make([]previewReceipt, 0, len(req.MessageIDs))
	for _, id := range req.MessageIDs {
		id = strings.TrimSpace(id)
		if _, exists := known[id]; exists {
			for _, message := range state.messages {
				messageID, _ := message["id"].(string)
				if messageID != id {
					continue
				}
				extra, _ := message["extra"].(map[string]any)
				preview, _ := extra["hardware_audio_preview"].(bool)
				requestID, _ := extra["hardware_audio_preview_request_id"].(string)
				if preview && strings.TrimSpace(requestID) != "" {
					previewReceipts = append(previewReceipts, previewReceipt{MachineID: p.MachineID, RequestID: strings.TrimSpace(requestID), MessageID: id})
				}
				break
			}
			state.acked[id] = true
			state.ackStatus[id] = status
		}
	}
	g.pruneDeviceMessagesLocked(state)
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
	sender := g.machineSender
	g.mu.Unlock()
	if sender != nil {
		for _, receipt := range previewReceipts {
			_ = sender.SendToMachine(receipt.MachineID, map[string]any{
				"type":       "im.device_gateway_playback_receipt",
				"request_id": receipt.RequestID,
				"payload": map[string]any{
					"clientId": p.ClientID, "messageId": receipt.MessageID, "status": status,
				},
			})
		}
	}
	writeDeviceJSON(w, 200, map[string]any{"ok": true})
}

func (g *DeviceGateway) handleToolResult(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeDeviceError(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST required")
		return
	}
	p, ok := g.principal(r)
	if !ok {
		writeDeviceError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid bearer token")
		return
	}
	if !g.machineHardwareEnabled(p.MachineID) {
		writeDeviceError(w, http.StatusServiceUnavailable, "hardware_disabled", "hardware is disabled in MaClaw")
		return
	}
	var req coreim.ThirdPartyToolResultRequest
	if !decodeDeviceJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.ClientID) != p.ClientID {
		writeDeviceError(w, http.StatusForbidden, "forbidden", "clientId does not match credential")
		return
	}
	if err := coreim.NormalizeThirdPartyToolResultRequest(&req); err != nil {
		writeDeviceError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if g.plugin == nil {
		writeDeviceError(w, http.StatusServiceUnavailable, "unavailable", "GUI relay is unavailable")
		return
	}
	ownerID := g.plugin.GatewayOwnerForTenant(p.TenantID)
	if ownerID == "" {
		writeDeviceError(w, http.StatusServiceUnavailable, "gui_offline", "the paired MaClaw GUI is not connected to Hub")
		return
	}
	eventID := coreim.ThirdPartyToolResultEventID(req)
	if g.markDeviceEvent(p.ClientID, eventID) {
		writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "duplicate": true})
		return
	}
	conversationID := firstDeviceValue(req.ConversationID, "default")
	g.mu.Lock()
	state := g.clientLocked(p.ClientID)
	capabilities := state.capabilities
	clientTools := append([]agent.ClientToolDefinition(nil), state.tools...)
	g.mu.Unlock()
	g.activateDeviceReply(p.ClientID, eventID)
	payload, _ := json.Marshal(map[string]any{
		"tenant_id": p.TenantID, "platform_uid": "thirdparty:" + p.ClientID + ":" + conversationID,
		"reply_target": "thirdparty:" + p.ClientID + ":" + conversationID,
		"message_id":   eventID, "message_type": "text",
		"text": coreim.ThirdPartyToolResultContent(req), "client_capabilities": capabilities,
		"client_tools": clientTools, "client_tool_context": agent.ClientToolContext{ClientID: p.ClientID, ConversationID: conversationID, ReplyToMessageID: eventID},
	})
	go g.plugin.HandleGatewayMessage(ownerID, payload)
	writeDeviceJSON(w, http.StatusOK, map[string]any{"ok": true, "accepted": true, "duplicate": false})
}

func (g *DeviceGateway) EnqueueReply(clientID, conversationID string, reply map[string]any) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return
	}
	g.mu.Lock()
	state := g.clientLocked(clientID)
	if !deviceReplyBelongsToCurrentBootLocked(state, reply) {
		g.mu.Unlock()
		log.Printf("[device-gateway] stale reply discarded client=%s replyTo=%s bootSessionId=%s", clientID, deviceReplyCorrelationID(reply), state.bootSessionID)
		return
	}
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
	g.queueDeviceMessageLocked(state, reply)
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
	if !g.ensureMediaCapacityLocked(clientID, 1, size, "", time.Now().UTC()) {
		return false
	}
	now := time.Now().UTC()
	g.media[mediaID] = &deviceMedia{
		ClientID: clientID, MachineID: g.machineIDForClientLocked(clientID), ID: mediaID, Token: token, Type: "audio", FileName: fileName,
		MimeType: mimeType, SizeBytes: size, Data: data, Uploaded: true, LastAccessedAt: now, ExpiresAt: now.Add(deviceGatewayMediaTTL),
	}
	reply["url"] = fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", mediaID, token)
	reply["sizeBytes"] = size
	reply["mime_type"] = mimeType
	delete(reply, "file_data")
	delete(reply, "data")
	return true
}

// preparePetAssetLocked publishes GUI-rendered transparent frames through the same
// short-lived, same-origin media transport used by server speech.  The caller
// holds g.mu. Invalid base64 or an unexpected byte count disables the remote
// asset and leaves the device's native skin renderer as a safe fallback.
func (g *DeviceGateway) preparePetAssetLocked(clientID string, asset *DevicePetAsset, animated bool, maxFrames int) *devicePetAssetReference {
	asset = normalizeDevicePetAsset(asset)
	if asset == nil {
		return nil
	}
	encoded := []string{asset.Data}
	if animated && len(asset.Frames) > 0 {
		encoded = append(encoded, asset.Frames...)
	}
	frameMS := asset.FrameMS
	if animated {
		// Missing means the legacy two-frame ESP contract, not unlimited.
		if maxFrames <= 0 {
			maxFrames = 2
		}
		if maxFrames < len(encoded) {
			selected := make([]string, 0, maxFrames)
			for index := 0; index < maxFrames; index++ {
				selected = append(selected, encoded[index*len(encoded)/maxFrames])
			}
			frameMS = asset.FrameMS * len(encoded) / maxFrames
			encoded = selected
		}
	}
	expected := asset.Width * asset.Height * 3
	if expected <= 0 || expected > devicePetAssetMaxDimension*devicePetAssetMaxDimension*3 {
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
	_, _ = digest.Write([]byte(fmt.Sprintf("/%d/%d/%d", asset.Width, asset.Height, frameMS)))
	for _, frame := range frames {
		_, _ = digest.Write(frame)
	}
	revision := hex.EncodeToString(digest.Sum(nil)[:8])

	type preparedFrame struct {
		id    string
		token string
		data  []byte
	}
	prepared := make([]preparedFrame, 0, len(frames))
	var totalBytes int64
	for _, frame := range frames {
		mediaID, token, err := newDeviceToken(16)
		if err != nil {
			return nil
		}
		prepared = append(prepared, preparedFrame{id: mediaID, token: token, data: frame})
		totalBytes += int64(len(frame))
	}
	if !g.ensureMediaCapacityLocked(clientID, len(prepared), totalBytes, "", time.Now().UTC()) {
		return nil
	}
	urls := make([]string, 0, len(frames))
	hashes := make([]string, 0, len(frames))
	now := time.Now().UTC()
	for index, frame := range prepared {
		g.media[frame.id] = &deviceMedia{
			ClientID: clientID, MachineID: g.machineIDForClientLocked(clientID), ID: frame.id, Token: frame.token, Type: "pet_asset",
			FileName: fmt.Sprintf("pet-%s-%d.rgb565a8", revision, index),
			MimeType: "application/vnd.maclaw.rgb565a8", SizeBytes: int64(len(frame.data)),
			Data: frame.data, Uploaded: true, LastAccessedAt: now, ExpiresAt: now.Add(deviceGatewayMediaTTL),
		}
		urls = append(urls, fmt.Sprintf("/api/im-gateway/v1/media/%s?mediaToken=%s", frame.id, frame.token))
		hash := sha256.Sum256(frame.data)
		hashes = append(hashes, hex.EncodeToString(hash[:]))
	}
	return &devicePetAssetReference{
		Encoding: asset.Encoding, Width: asset.Width, Height: asset.Height,
		URLs: urls, FrameMS: frameMS, Revision: revision, SHA256: hashes,
	}
}

// EnqueueMachineReply broadcasts a GUI-originated device configuration update
// only to the hardware credentials paired with that GUI machine.
func (g *DeviceGateway) EnqueueMachineReply(machineID, conversationID string, reply map[string]any) {
	g.EnqueueMachineReplyCount(machineID, conversationID, reply)
}

// EnqueueMachineReplyCount broadcasts a GUI-originated reply and reports how
// many compatible hardware clients actually accepted it. Hardware audio
// previews intentionally skip stale clients so the GUI can fail fast instead
// of waiting for a playback receipt from an offline ESP32.
func (g *DeviceGateway) EnqueueMachineReplyCount(machineID, conversationID string, reply map[string]any) int {
	return g.enqueueMachineClientReplyCount(machineID, "*", conversationID, reply)
}

// EnqueueMachineClientReply routes a GUI-originated message to one owned
// hardware client. The wildcard preserves the legacy broadcast behaviour.
func (g *DeviceGateway) EnqueueMachineClientReply(machineID, targetClientID, conversationID string, reply map[string]any) {
	g.enqueueMachineClientReplyCount(machineID, targetClientID, conversationID, reply)
}

// EnqueueMachineClientReplyCount is the ownership-enforcing variant used by
// request/response transports. A zero result means the target is not bound to
// that GUI (or cannot accept the reply), so callers can surface a correlated
// error instead of silently routing by globally visible client ID.
func (g *DeviceGateway) EnqueueMachineClientReplyCount(machineID, targetClientID, conversationID string, reply map[string]any) int {
	return g.enqueueMachineClientReplyCount(machineID, targetClientID, conversationID, reply)
}

func (g *DeviceGateway) enqueueMachineClientReplyCount(machineID, targetClientID, conversationID string, reply map[string]any) int {
	machineID = strings.TrimSpace(machineID)
	targetClientID = strings.TrimSpace(targetClientID)
	if machineID == "" || reply == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.machineHardwareEnabledLocked(machineID) {
		return 0
	}
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
	extra, _ := reply["extra"].(map[string]any)
	isHardwareAudioPreview, _ := extra["hardware_audio_preview"].(bool)
	queued := 0
	seenClients := make(map[string]struct{})
	for _, principal := range g.tokens {
		if principal.MachineID != machineID || (targetClientID != "" && targetClientID != "*" && principal.ClientID != targetClientID) {
			continue
		}
		if _, seen := seenClients[principal.ClientID]; seen {
			continue
		}
		seenClients[principal.ClientID] = struct{}{}
		state := g.clientLocked(principal.ClientID)
		if !deviceReplyBelongsToCurrentBootLocked(state, reply) {
			log.Printf("[device-gateway] stale machine reply discarded machine=%s client=%s replyTo=%s bootSessionId=%s", machineID, principal.ClientID, deviceReplyCorrelationID(reply), state.bootSessionID)
			continue
		}
		lastSeen := state.lastSeenAt
		if lastSeen.IsZero() {
			lastSeen = principal.LastSeenAt
		}
		if isHardwareAudioPreview && (lastSeen.IsZero() || time.Since(lastSeen) > 90*time.Second) {
			continue
		}
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
			queued++
			old := state.notify
			state.notify = make(chan struct{})
			close(old)
			continue
		}
		state.next++
		message["seq"] = state.next
		message["id"] = fmt.Sprintf("hub_hardware_%d_%d", time.Now().UnixMilli(), state.next)
		message["conversationId"] = firstDeviceValue(conversationID, "system")
		g.queueDeviceMessageLocked(state, message)
		queued++
		old := state.notify
		state.notify = make(chan struct{})
		close(old)
	}
	return queued
}

// ListMachineDevices returns all durable hardware bindings owned by one GUI.
func (g *DeviceGateway) ListMachineDevices(machineID string) []HardwareDevice {
	machineID = strings.TrimSpace(machineID)
	g.mu.Lock()
	defer g.mu.Unlock()
	byClient := make(map[string]devicePrincipal)
	for _, principal := range g.tokens {
		if principal.MachineID == machineID {
			byClient[principal.ClientID] = principal
		}
	}
	devices := make([]HardwareDevice, 0, len(byClient))
	for clientID, principal := range byClient {
		device := HardwareDevice{ClientID: clientID, ClientName: principal.ClientName, ProtocolVersion: principal.ProtocolVersion, PairedAt: principal.PairedAt, LastSeenAt: principal.LastSeenAt, PetSkin: principal.Pet.Skin}
		if volume, ok := g.hardwareVolumeForClientLocked(machineID, clientID); ok {
			device.Volume = &volume
		}
		if state := g.clients[clientID]; state != nil {
			lastSeen := state.lastSeenAt
			if lastSeen.IsZero() {
				lastSeen = principal.LastSeenAt
			}
			device.LastSeenAt = lastSeen
			device.Online = !lastSeen.IsZero() && time.Since(lastSeen) <= 90*time.Second
			caps := agent.NormalizeClientCapabilities(&state.capabilities)
			device.Capabilities = &caps
			var newest int64
			for _, message := range state.messages {
				seq, _ := message["seq"].(int64)
				id, _ := message["id"].(string)
				if seq >= newest && state.ackStatus[id] != "" {
					newest = seq
					device.LastAckStatus = state.ackStatus[id]
				}
			}
		}
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Online != devices[j].Online {
			return devices[i].Online
		}
		return devices[i].ClientID < devices[j].ClientID
	})
	return devices
}

func (g *DeviceGateway) ListMachineDevicesJSON(machineID string) []map[string]any {
	devices := g.ListMachineDevices(machineID)
	out := make([]map[string]any, 0, len(devices))
	for _, device := range devices {
		item := map[string]any{"clientId": device.ClientID, "online": device.Online}
		if device.ClientName != "" {
			item["clientName"] = device.ClientName
		}
		if device.ProtocolVersion != "" {
			item["protocolVersion"] = device.ProtocolVersion
		}
		if !device.PairedAt.IsZero() {
			item["pairedAt"] = device.PairedAt
		}
		if !device.LastSeenAt.IsZero() {
			item["lastSeenAt"] = device.LastSeenAt
		}
		if device.LastAckStatus != "" {
			item["lastAckStatus"] = device.LastAckStatus
		}
		if device.Volume != nil {
			item["volume"] = *device.Volume
		}
		if device.PetSkin != "" {
			item["petSkin"] = device.PetSkin
		}
		if device.Capabilities != nil {
			item["capabilities"] = device.Capabilities
		}
		out = append(out, item)
	}
	return out
}

// MigrateMachineHardwareBindings upgrades bindings created before the desktop
// had a stable machine ID. Migration is deliberately narrow: the target must
// not own any bindings yet, every candidate must be named explicitly by the
// desktop, and each moved credential must match the authenticated tenant and
// user. This prevents a user's separate GUI installations from ever seeing or
// controlling one another's hardware.
func (g *DeviceGateway) MigrateMachineHardwareBindings(machineID, tenantID, userID string, legacyMachineIDs []string) error {
	machineID = strings.TrimSpace(machineID)
	tenantID = normalizeRemoteTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	if machineID == "" || userID == "" || len(legacyMachineIDs) == 0 {
		return nil
	}
	legacy := make(map[string]struct{}, len(legacyMachineIDs))
	for _, candidate := range legacyMachineIDs {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && candidate != machineID {
			legacy[candidate] = struct{}{}
		}
	}
	if len(legacy) == 0 {
		return nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	// Once a current machine owns a device, its ownership is authoritative.
	// Do not merge a legacy candidate into an active, independent binding set.
	if g.machineBoundClientCountLocked(machineID) != 0 {
		return nil
	}
	previousTokens := make(map[string]devicePrincipal)
	migratedLegacyMachines := make(map[string]struct{})
	migratedClientIDs := make(map[string]struct{})
	for token, principal := range g.tokens {
		if _, ok := legacy[principal.MachineID]; !ok || normalizeRemoteTenantID(principal.TenantID) != tenantID || strings.TrimSpace(principal.UserID) != userID {
			continue
		}
		previousTokens[token] = principal
		migratedLegacyMachines[principal.MachineID] = struct{}{}
		migratedClientIDs[principal.ClientID] = struct{}{}
		principal.MachineID = machineID
		g.tokens[token] = principal
	}
	if len(previousTokens) == 0 {
		return nil
	}
	// Media is transient, but its machine ID is still an authorization boundary:
	// downloads are rejected when that machine disables hardware.  Keep every
	// live object belonging to a migrated device under the new owner as well,
	// otherwise a pre-migration URL could keep consulting the legacy switch.
	previousMediaMachineIDs := make(map[string]string)
	for mediaID, media := range g.media {
		if media == nil {
			continue
		}
		if _, moved := migratedClientIDs[media.ClientID]; !moved {
			continue
		}
		if _, legacyOwner := legacy[media.MachineID]; !legacyOwner {
			continue
		}
		previousMediaMachineIDs[mediaID] = media.MachineID
		media.MachineID = machineID
	}
	previousHardware, targetHardwareExisted := g.hardware[machineID]
	previousHardware = cloneDeviceHardwareConfig(previousHardware)
	removedHardware := make(map[string]deviceHardwareConfig)
	// A migrated binding can carry durable per-device volumes.  Preserve an
	// already-created current-machine configuration, but merge only the moved
	// clients' per-device values into it.  Other legacy machine-level defaults
	// must not overwrite current GUI settings.
	currentHardware := cloneDeviceHardwareConfig(previousHardware)
	currentHardwareExisted := targetHardwareExisted
	for legacyMachineID := range migratedLegacyMachines {
		legacyHardware, ok := g.hardware[legacyMachineID]
		if !ok {
			continue
		}
		// Do not move machine-wide state from a legacy owner that still has
		// another binding. That state may belong to a separate user/session
		// despite a malformed historical machine ID.
		legacyHasRemainingBindings := g.machineBoundClientCountLocked(legacyMachineID) != 0
		if !currentHardwareExisted {
			if !legacyHasRemainingBindings {
				currentHardware = cloneDeviceHardwareConfig(legacyHardware)
				currentHardwareExisted = true
			}
		} else if len(legacyHardware.DeviceVolumes) > 0 {
			if currentHardware.DeviceVolumes == nil {
				currentHardware.DeviceVolumes = make(map[string]int)
			}
			for clientID, volume := range legacyHardware.DeviceVolumes {
				if _, moved := migratedClientIDs[clientID]; moved {
					if _, alreadyConfigured := currentHardware.DeviceVolumes[clientID]; !alreadyConfigured {
						currentHardware.DeviceVolumes[clientID] = volume
					}
				}
			}
		}
		if !legacyHasRemainingBindings {
			removedHardware[legacyMachineID] = legacyHardware
			delete(g.hardware, legacyMachineID)
		}
	}
	if currentHardwareExisted {
		g.hardware[machineID] = currentHardware
	}
	if err := g.persistTokensLocked(); err != nil {
		for token, principal := range previousTokens {
			g.tokens[token] = principal
		}
		for mediaID, previousMachineID := range previousMediaMachineIDs {
			if media := g.media[mediaID]; media != nil {
				media.MachineID = previousMachineID
			}
		}
		if targetHardwareExisted {
			g.hardware[machineID] = previousHardware
		} else {
			delete(g.hardware, machineID)
		}
		for legacyMachineID, config := range removedHardware {
			g.hardware[legacyMachineID] = config
		}
		return fmt.Errorf("persist legacy hardware binding migration: %w", err)
	}
	return nil
}

// MachineHardwareBindingState returns the current durable binding capacity.
func (g *DeviceGateway) MachineHardwareBindingState(machineID string) MachineHardwareBindingState {
	machineID = strings.TrimSpace(machineID)
	g.mu.Lock()
	defer g.mu.Unlock()
	return MachineHardwareBindingState{
		MaxDevices: g.machineHardwareMaxDevicesLocked(machineID),
		BoundCount: g.machineBoundClientCountLocked(machineID),
	}
}

func (g *DeviceGateway) MachineHardwareBindingStateJSON(machineID string) map[string]any {
	state := g.MachineHardwareBindingState(machineID)
	return map[string]any{"maxDevices": state.MaxDevices, "boundCount": state.BoundCount}
}

func (g *DeviceGateway) machineHardwareMaxDevicesLocked(machineID string) int {
	return defaultMachineHardwareMaxDevices
}

func (g *DeviceGateway) machineBoundClientCountLocked(machineID string) int {
	clients := make(map[string]struct{})
	for _, principal := range g.tokens {
		if principal.MachineID == machineID && strings.TrimSpace(principal.ClientID) != "" {
			clients[principal.ClientID] = struct{}{}
		}
	}
	return len(clients)
}

func (g *DeviceGateway) machineOwnsClientLocked(machineID, clientID string) bool {
	for _, principal := range g.tokens {
		if principal.MachineID == machineID && principal.ClientID == clientID {
			return true
		}
	}
	return false
}

// DeleteMachineDevice removes every bearer for one owned client and its
// in-memory queue. A connected ESP receives 401 on its next request.
func (g *DeviceGateway) DeleteMachineDevice(machineID, clientID string) error {
	machineID, clientID = strings.TrimSpace(machineID), strings.TrimSpace(clientID)
	if machineID == "" || clientID == "" {
		return fmt.Errorf("machine ID and client ID are required")
	}
	// Linearize this delete with an already-accepted asynchronous inbound relay
	// from the same ESP32. Other devices continue normally while this short
	// critical section waits only for their own hand-off to complete.
	dispatchMu := g.deviceDispatchMutex(clientID)
	dispatchMu.Lock()
	defer dispatchMu.Unlock()
	g.mu.Lock()
	defer g.mu.Unlock()
	revoked := make(map[string]devicePrincipal)
	for token, principal := range g.tokens {
		if principal.MachineID == machineID && principal.ClientID == clientID {
			revoked[token] = principal
			delete(g.tokens, token)
		}
	}
	if len(revoked) == 0 {
		return fmt.Errorf("hardware device not found")
	}
	previousState := g.clients[clientID]
	delete(g.clients, clientID)
	removedMedia := make(map[string]*deviceMedia)
	for id, media := range g.media {
		if media.ClientID == clientID {
			removedMedia[id] = media
			delete(g.media, id)
		}
	}
	previousHardware, hardwareExisted := g.hardware[machineID]
	previousHardware = cloneDeviceHardwareConfig(previousHardware)
	if config, ok := g.hardware[machineID]; ok && config.DeviceVolumes != nil {
		config = cloneDeviceHardwareConfig(config)
		delete(config.DeviceVolumes, clientID)
		if len(config.DeviceVolumes) == 0 {
			config.DeviceVolumes = nil
		}
		g.hardware[machineID] = config
	}
	if err := g.persistTokensLocked(); err != nil {
		for token, principal := range revoked {
			g.tokens[token] = principal
		}
		if previousState != nil {
			g.clients[clientID] = previousState
		}
		for id, media := range removedMedia {
			g.media[id] = media
		}
		if hardwareExisted {
			g.hardware[machineID] = previousHardware
		}
		return fmt.Errorf("persist hardware deletion: %w", err)
	}
	// New requests can only create this lock after they authenticate. Once the
	// bearer is gone they fail authentication, so release the per-device entry
	// instead of retaining one mutex for every historical binding.
	delete(g.dispatchLocks, clientID)
	return nil
}

func (g *DeviceGateway) updatePrincipalLocked(updated devicePrincipal) error {
	previous := make(map[string]devicePrincipal)
	for token, principal := range g.tokens {
		if principal.ClientID == updated.ClientID && principal.MachineID == updated.MachineID {
			previous[token] = principal
			g.tokens[token] = updated
		}
	}
	if err := g.persistTokensLocked(); err != nil {
		for token, principal := range previous {
			g.tokens[token] = principal
		}
		return err
	}
	return nil
}

const deviceGatewayWelcomeMaxBytes = 96 * 1024

func (g *DeviceGateway) machineHardwareEnabled(machineID string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.machineHardwareEnabledLocked(machineID)
}

func (g *DeviceGateway) machineHardwareEnabledLocked(machineID string) bool {
	enabled := g.hardware[strings.TrimSpace(machineID)].Enabled
	return enabled == nil || *enabled
}

// UpdateMachineHardwareEnabled changes only the master switch. Durable device
// bindings and subordinate settings are intentionally preserved so re-enabling
// hardware does not require pairing or configuration again.
func (g *DeviceGateway) UpdateMachineHardwareEnabled(machineID string, enabled bool) error {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return fmt.Errorf("machine ID is required")
	}
	g.mu.Lock()
	previous, existed := g.hardware[machineID]
	config := previous
	config.Enabled = new(bool)
	*config.Enabled = enabled
	g.hardware[machineID] = config
	err := g.persistTokensLocked()
	if err != nil {
		if existed {
			g.hardware[machineID] = previous
		} else {
			delete(g.hardware, machineID)
		}
	} else {
		// Wake outstanding long polls so they observe the new switch state now,
		// rather than after their normal timeout. This closes the disable race in
		// which an already-waiting request could otherwise receive a later reply.
		seenClients := make(map[string]struct{})
		for _, principal := range g.tokens {
			if principal.MachineID != machineID {
				continue
			}
			if _, seen := seenClients[principal.ClientID]; seen {
				continue
			}
			seenClients[principal.ClientID] = struct{}{}
			if state := g.clients[principal.ClientID]; state != nil {
				old := state.notify
				state.notify = make(chan struct{})
				close(old)
			}
		}
	}
	g.mu.Unlock()
	return err
}

// machineIDForClientLocked resolves the durable owner of a globally unique
// hardware client ID. Callers hold g.mu. An empty result intentionally keeps
// synthetic legacy media (created without a paired credential) compatible.
func (g *DeviceGateway) machineIDForClientLocked(clientID string) string {
	clientID = strings.TrimSpace(clientID)
	for _, principal := range g.tokens {
		if principal.ClientID == clientID {
			return principal.MachineID
		}
	}
	return ""
}

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
	previous, existed := g.hardware[machineID]
	previous = cloneDeviceHardwareConfig(previous)
	config := cloneDeviceHardwareConfig(g.hardware[machineID])
	config.WelcomeEnabled = enabled
	if replaceAudio {
		config.WelcomeAudio = audioBase64
	} else if audioBase64 != "" {
		config.WelcomeAudio = audioBase64
	}
	g.hardware[machineID] = config
	err := g.persistTokensLocked()
	if err != nil {
		if existed {
			g.hardware[machineID] = previous
		} else {
			delete(g.hardware, machineID)
		}
	}
	g.mu.Unlock()
	return err
}

// UpdateMachineVolume changes the legacy machine default speaker level. It is
// retained for compatibility and is used only by bindings without an explicit
// per-device level.
func (g *DeviceGateway) UpdateMachineVolume(machineID string, value any) error {
	machineID = strings.TrimSpace(machineID)
	volume, ok := deviceVolume(value)
	if machineID == "" || !ok {
		return fmt.Errorf("invalid hardware volume")
	}
	g.mu.Lock()
	previous, existed := g.hardware[machineID]
	config := g.hardware[machineID]
	config.Volume = &volume
	g.hardware[machineID] = config
	err := g.persistTokensLocked()
	if err != nil {
		if existed {
			g.hardware[machineID] = previous
		} else {
			delete(g.hardware, machineID)
		}
	}
	g.mu.Unlock()
	return err
}

// UpdateMachineDeviceVolume makes one bound ESP32's speaker level durable.
// It verifies ownership before persisting so a machine cannot reserve settings
// for an arbitrary globally-visible client ID.
func (g *DeviceGateway) UpdateMachineDeviceVolume(machineID, clientID string, value any) error {
	machineID, clientID = strings.TrimSpace(machineID), strings.TrimSpace(clientID)
	volume, ok := deviceVolume(value)
	if machineID == "" || clientID == "" || !ok {
		return fmt.Errorf("invalid hardware device volume")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	owned := false
	for _, principal := range g.tokens {
		if principal.MachineID == machineID && principal.ClientID == clientID {
			owned = true
			break
		}
	}
	if !owned {
		return fmt.Errorf("hardware device not found")
	}
	previous, existed := g.hardware[machineID]
	previous = cloneDeviceHardwareConfig(previous)
	config := cloneDeviceHardwareConfig(g.hardware[machineID])
	if config.DeviceVolumes == nil {
		config.DeviceVolumes = make(map[string]int)
	}
	config.DeviceVolumes[clientID] = volume
	g.hardware[machineID] = config
	if err := g.persistTokensLocked(); err != nil {
		if existed {
			g.hardware[machineID] = previous
		} else {
			delete(g.hardware, machineID)
		}
		return err
	}
	// A connected device receives the change right away. For an offline binding
	// we intentionally do not create a synthetic client state: its first
	// capability handshake picks up the durable value below.
	if state := g.clients[clientID]; state != nil {
		if capabilities := agent.NormalizeClientCapabilities(&state.capabilities); capabilities.Features.VolumeControl {
			if replaceQueuedDeviceVolume(state, volume) {
				return nil
			}
			message := map[string]any{"reply_type": "hardware_config", "type": "hardware_config", "extra": map[string]any{"volume": volume}}
			state.next++
			message["seq"] = state.next
			message["id"] = fmt.Sprintf("hub_hardware_config_%d_%d", time.Now().UnixMilli(), state.next)
			message["conversationId"] = "system"
			g.queueDeviceMessageLocked(state, message)
			old := state.notify
			state.notify = make(chan struct{})
			close(old)
		}
	}
	return nil
}

func cloneDeviceHardwareConfig(config deviceHardwareConfig) deviceHardwareConfig {
	if config.DeviceVolumes == nil {
		return config
	}
	volumes := config.DeviceVolumes
	config.DeviceVolumes = make(map[string]int, len(volumes))
	for clientID, volume := range volumes {
		config.DeviceVolumes[clientID] = volume
	}
	return config
}

func (g *DeviceGateway) hardwareVolumeForClientLocked(machineID, clientID string) (int, bool) {
	config := g.hardware[machineID]
	if config.DeviceVolumes != nil {
		if volume, ok := config.DeviceVolumes[clientID]; ok {
			return volume, true
		}
	}
	if config.Volume != nil {
		return *config.Volume, true
	}
	return 0, false
}

// queueHardwareConfigForClientLocked places durable, lightweight settings
// ahead of boot-time media. At present this is the speaker volume; keeping it
// in the handshake path also covers a device paired after the desktop setting
// was chosen.
func (g *DeviceGateway) queueHardwareConfigForClientLocked(principal devicePrincipal, state *deviceClientState) {
	if state == nil {
		return
	}
	volume, ok := g.hardwareVolumeForClientLocked(principal.MachineID, principal.ClientID)
	if !ok {
		return
	}
	// A reconnect or rapid slider movement can arrive before the ESP ACKs the
	// prior setting. Coalesce into its pending control message so the device
	// always receives the latest level without filling the queue with stale
	// values that could later overwrite the user's last choice.
	if replaceQueuedDeviceVolume(state, volume) {
		return
	}
	message := map[string]any{"reply_type": "hardware_config", "type": "hardware_config", "extra": map[string]any{"volume": volume}}
	if !adaptDeviceGatewayReply(message, state.capabilities) {
		return
	}
	state.next++
	message["seq"] = state.next
	message["id"] = fmt.Sprintf("hub_hardware_config_%d_%d", time.Now().UnixMilli(), state.next)
	message["conversationId"] = "system"
	g.queueDeviceMessageLocked(state, message)
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
func (g *DeviceGateway) queueWelcomeForBootLocked(principal devicePrincipal, bootSessionID string, state *deviceClientState) bool {
	if bootSessionID == "" || state == nil {
		return false
	}
	// A reboot can happen after the speaker finishes but before its ACK reaches
	// Hub. Remove Welcome transactions from older boots before considering the
	// current one; otherwise cursor zero delivers the stale greeting first and
	// the newly queued greeting second. Preview audio has a different ID and is
	// deliberately unaffected.
	kept := state.messages[:0]
	for _, message := range state.messages {
		messageID, _ := message["id"].(string)
		queuedBootID, _ := message["bootSessionId"].(string)
		if strings.HasPrefix(messageID, "hub_welcome_") && queuedBootID != bootSessionID {
			g.releaseDeviceMessageMediaLocked(message)
			delete(state.acked, messageID)
			delete(state.ackStatus, messageID)
			continue
		}
		kept = append(kept, message)
	}
	state.messages = kept
	// A successful queue followed by a lost handshake response must still tell
	// the reconnecting device to consume the pending greeting before it starts
	// its wake-word model. The durable boot marker alone cannot distinguish that
	// case from a greeting which was already acknowledged.
	for _, message := range state.messages {
		queuedBootID, _ := message["bootSessionId"].(string)
		messageID, _ := message["id"].(string)
		if queuedBootID == bootSessionID && strings.HasPrefix(messageID, "hub_welcome_") {
			return true
		}
	}
	config := g.hardware[principal.MachineID]
	if !config.WelcomeEnabled || config.WelcomeAudio == "" || principal.LastWelcomeBootID == bootSessionID {
		return false
	}
	// principal was resolved just before this mutex was acquired. Check the
	// durable map as well so two overlapping handshakes cannot both emit the
	// same boot's greeting.
	for _, candidate := range g.tokens {
		if candidate.ClientID == principal.ClientID && candidate.MachineID == principal.MachineID && candidate.LastWelcomeBootID == bootSessionID {
			return false
		}
	}
	message := map[string]any{"reply_type": "audio", "type": "audio", "mime_type": "audio/wav", "file_data": config.WelcomeAudio, "bootSessionId": bootSessionID}
	if !g.prepareOutgoingAudioLocked(principal.ClientID, message, state.capabilities) {
		return false
	}
	if !adaptDeviceGatewayReply(message, state.capabilities) {
		return false
	}
	state.next++
	message["seq"] = state.next
	message["id"] = fmt.Sprintf("hub_welcome_%d_%d", time.Now().UnixMilli(), state.next)
	message["conversationId"] = "system"
	g.queueDeviceMessageLocked(state, message)
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
	return true
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
	case "tool_call", "tool_plan", "tool_cancel":
		raw, err := json.Marshal(reply)
		if err != nil {
			return false
		}
		var outgoing coreim.ThirdPartyOutgoingMessage
		if err := json.Unmarshal(raw, &outgoing); err != nil {
			return false
		}
		// Queue ids are assigned after adaptation; use a temporary id solely for
		// protocol validation and remove it again before enqueueing.
		if strings.TrimSpace(outgoing.ID) == "" {
			outgoing.ID = "pending"
		}
		return coreim.NormalizeThirdPartyOutgoingMessage(&outgoing) == nil
	case "hardware_config":
		extra, _ := reply["extra"].(map[string]any)
		if extra == nil {
			return false
		}
		return validDeviceVolume(extra["volume"]) && capabilities.Features.VolumeControl
	case "image":
		mimeType = firstDeviceValue(mimeType, "image/png")
		if !capabilities.SupportsOutputMIME("image", mimeType) {
			return false
		}
		if mimeType == coreim.ThirdPartyRGB565MIME {
			width := int(deviceReplyInt64(reply, "width"))
			height := int(deviceReplyInt64(reply, "height"))
			if width < 1 || width > 64 || height < 1 || height > 64 {
				return false
			}
			if capabilities.Output.Image != nil {
				if capabilities.Output.Image.MaxWidth > 0 && width > capabilities.Output.Image.MaxWidth {
					return false
				}
				if capabilities.Output.Image.MaxHeight > 0 && height > capabilities.Output.Image.MaxHeight {
					return false
				}
			}
			expectedSize := width * height * 2
			encoded := strings.TrimSpace(deviceReplyString(reply, "data", "image_data"))
			if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(expectedSize) {
				return false
			}
			data, err := base64.StdEncoding.DecodeString(encoded)
			declaredSize := deviceReplyInt64(reply, "sizeBytes", "size_bytes")
			if err != nil || len(data) != expectedSize || declaredSize < 0 || (declaredSize > 0 && declaredSize != int64(expectedSize)) {
				return false
			}
			reply["sizeBytes"] = expectedSize
			return true
		}
		return validateDeviceGatewayEncodedImage(reply, mimeType, capabilities)
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

func validateDeviceGatewayEncodedImage(reply map[string]any, mimeType string, capabilities agent.ClientCapabilities) bool {
	encoded := strings.TrimSpace(deviceReplyString(reply, "data", "image_data"))
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(int(deviceGatewayMaxMediaBytes)) {
		return false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || int64(len(data)) > deviceGatewayMaxMediaBytes {
		return false
	}
	declaredSize := deviceReplyInt64(reply, "sizeBytes", "size_bytes")
	if declaredSize < 0 || (declaredSize > 0 && declaredSize != int64(len(data))) {
		return false
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || !deviceImageFormatMatchesMIME(format, mimeType) {
		return false
	}
	if capabilities.Output.Image != nil {
		if capabilities.Output.Image.MaxWidth > 0 && config.Width > capabilities.Output.Image.MaxWidth {
			return false
		}
		if capabilities.Output.Image.MaxHeight > 0 && config.Height > capabilities.Output.Image.MaxHeight {
			return false
		}
	}
	reply["width"] = config.Width
	reply["height"] = config.Height
	reply["sizeBytes"] = len(data)
	return true
}

func deviceImageFormatMatchesMIME(format, mimeType string) bool {
	format = strings.ToLower(strings.TrimSpace(format))
	mimeType = strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	switch format {
	case "jpeg":
		return mimeType == "image/jpeg" || mimeType == "image/jpg"
	case "png", "gif":
		return mimeType == "image/"+format
	default:
		return false
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
		g.queueDeviceMessageLocked(state, map[string]any{
			"seq": state.next, "id": fmt.Sprintf("hub_ambient_%d_%d", time.Now().UnixMilli(), state.next),
			"type": "ambient", "conversationId": "system", "ambient": normalized,
		})
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

// UpdateMachineDevicePetProfileAsset persists and queues an independent pet
// for one owned hardware binding. Unlike the wildcard update, it does not
// alter other devices or pending pairings.
func (g *DeviceGateway) UpdateMachineDevicePetProfileAsset(machineID, clientID, skin string, motionEnabled bool, asset map[string]any) error {
	machineID, clientID = strings.TrimSpace(machineID), strings.TrimSpace(clientID)
	if machineID == "" || clientID == "" {
		return fmt.Errorf("machine ID and hardware client ID are required")
	}
	profile := normalizeDevicePetProfileAsset(skin, motionEnabled, DevicePetAssetFromMap(asset))
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.hardware[machineID].AllowCustomPets {
		return fmt.Errorf("individual hardware pets are disabled")
	}
	owned := false
	changed := false
	previous := make(map[string]devicePrincipal)
	for token, principal := range g.tokens {
		if principal.MachineID != machineID || principal.ClientID != clientID {
			continue
		}
		owned = true
		if devicePetProfilesEqual(principal.Pet, profile) {
			continue
		}
		previous[token] = principal
		principal.Pet = profile
		g.tokens[token] = principal
		changed = true
	}
	if !owned {
		return fmt.Errorf("hardware device not found")
	}
	if changed {
		if err := g.persistTokensLocked(); err != nil {
			for token, principal := range previous {
				g.tokens[token] = principal
			}
			return fmt.Errorf("persist hardware pet profile: %w", err)
		}
	}
	if !changed {
		return nil
	}
	if state := g.clients[clientID]; state != nil {
		var assetRef *devicePetAssetReference
		if state.capabilities.Features.PetAsset {
			assetRef = g.preparePetAssetLocked(clientID, profile.Asset, state.capabilities.Features.PetAnimation && profile.MotionEnabled, state.capabilities.Features.PetAssetMaxFrames)
		}
		state.next++
		message := map[string]any{"seq": state.next, "id": fmt.Sprintf("hub_pet_%d_%d", time.Now().UnixMilli(), state.next), "type": "pet_profile", "conversationId": "system", "pet_skin": profile.Skin, "pet_motion_enabled": profile.MotionEnabled}
		if assetRef != nil {
			message["pet_asset"] = *assetRef
		}
		replaceQueuedDevicePetProfileLocked(g, state)
		g.queueDeviceMessageLocked(state, message)
		old := state.notify
		state.notify = make(chan struct{})
		close(old)
	}
	return nil
}

// UpdateMachineAllowCustomPets persists the machine-level permission that
// gates independent device profiles. The desktop may use it for presentation,
// but Hub owns the actual authorization boundary.
func (g *DeviceGateway) UpdateMachineAllowCustomPets(machineID string, enabled bool) error {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return fmt.Errorf("machine ID is required")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	previous, existed := g.hardware[machineID]
	config := cloneDeviceHardwareConfig(previous)
	config.AllowCustomPets = enabled
	g.hardware[machineID] = config
	if err := g.persistTokensLocked(); err != nil {
		if existed {
			g.hardware[machineID] = previous
		} else {
			delete(g.hardware, machineID)
		}
		return err
	}
	return nil
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
				state.capabilities.Features.PetAnimation && profile.MotionEnabled,
				state.capabilities.Features.PetAssetMaxFrames)
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
		// Pet profiles are durable latest-wins state. Replace an older pending
		// profile before queueing so an offline device does not retain multiple
		// ~100 KiB frame sets. The replacement has a fresh ID/sequence, so an ACK
		// for a profile fetched just before this update cannot delete the new one.
		replaceQueuedDevicePetProfileLocked(g, state)
		g.queueDeviceMessageLocked(state, message)
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

func replaceQueuedDevicePetProfileLocked(g *DeviceGateway, state *deviceClientState) bool {
	if g == nil || state == nil {
		return false
	}
	for index := len(state.messages) - 1; index >= 0; index-- {
		message := state.messages[index]
		if deviceReplyString(message, "type", "reply_type") != "pet_profile" {
			continue
		}
		id, _ := message["id"].(string)
		if id != "" && state.acked[id] {
			continue
		}
		g.releaseDeviceMessageMediaLocked(message)
		copy(state.messages[index:], state.messages[index+1:])
		state.messages = state.messages[:len(state.messages)-1]
		return true
	}
	return false
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
	if left.Encoding != right.Encoding || left.Width != right.Width || left.Height != right.Height || left.FrameMS != right.FrameMS || left.Data != right.Data || len(left.Frames) != len(right.Frames) {
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
	if asset == nil || asset.Encoding != "rgb565a8" || asset.Width < 32 || asset.Width > devicePetAssetMaxDimension || asset.Height < 32 || asset.Height > devicePetAssetMaxDimension || len(asset.Data) == 0 || len(asset.Data) > devicePetAssetMaxEncodedFrameBytes || len(asset.Frames) > devicePetAssetMaxFrames-1 {
		return nil
	}
	for _, frame := range asset.Frames {
		if len(frame) == 0 || len(frame) > devicePetAssetMaxEncodedFrameBytes {
			return nil
		}
	}
	copy := *asset
	if copy.FrameMS < 50 || copy.FrameMS > 10000 {
		copy.FrameMS = 450
	}
	copy.Frames = append([]string(nil), asset.Frames...)
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
	switch value := raw["frameMs"].(type) {
	case float64:
		asset.FrameMS = int(value)
	case int:
		asset.FrameMS = value
	}
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
	var totalBytes int64
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) == "" {
			continue
		}
		g.mu.Lock()
		mediaID := strings.TrimSpace(ref.ID)
		media := g.media[mediaID]
		now := time.Now().UTC()
		if media != nil && deviceMediaExpired(media, now) {
			delete(g.media, mediaID)
			media = nil
		}
		if media == nil || !media.Uploaded || media.ClientID != clientID {
			g.mu.Unlock()
			return nil, fmt.Errorf("media %s not found", strings.TrimSpace(ref.ID))
		}
		if int64(len(media.Data)) > deviceGatewayMaxMediaResidentBytesPerClient-totalBytes {
			g.mu.Unlock()
			return nil, fmt.Errorf("attachments exceed %d bytes", deviceGatewayMaxMediaResidentBytesPerClient)
		}
		media.LastAccessedAt = time.Now().UTC()
		mediaType, fileName, mimeType := media.Type, media.FileName, media.MimeType
		data := append([]byte(nil), media.Data...)
		g.mu.Unlock()
		totalBytes += int64(len(data))
		attachments = append(attachments, MessageAttachment{Type: firstDeviceValue(ref.Type, mediaType), FileName: firstDeviceValue(ref.FileName, fileName), MimeType: firstDeviceValue(ref.MimeType, mimeType), Data: encodeDeviceData(data), Size: int64(len(data))})
	}
	return attachments, nil
}

func (g *DeviceGateway) storeMediaUpload(r *http.Request, id string) error {
	if r == nil || r.Body == nil {
		return fmt.Errorf("media body is required")
	}
	if r.ContentLength > deviceGatewayMaxMediaBytes {
		return fmt.Errorf("media exceeds %d bytes", deviceGatewayMaxMediaBytes)
	}
	g.mu.Lock()
	mediaID := strings.TrimSpace(id)
	media := g.media[mediaID]
	if media != nil && deviceMediaExpired(media, time.Now().UTC()) {
		delete(g.media, mediaID)
		media = nil
	}
	if media == nil || !deviceMediaTokenOK(r, media.Token) {
		g.mu.Unlock()
		return fmt.Errorf("media not found")
	}
	if media.MachineID != "" && !g.machineHardwareEnabledLocked(media.MachineID) {
		g.mu.Unlock()
		return fmt.Errorf("hardware is disabled")
	}
	if media.Uploading {
		g.mu.Unlock()
		return fmt.Errorf("media upload already in progress")
	}
	if media.Uploaded {
		// Treat a retry after a lost success response as idempotent. Media is
		// immutable once published, so a bearer cannot silently replace content
		// already referenced by an incoming message.
		g.mu.Unlock()
		return nil
	}
	if media.SizeBytes > 0 && r.ContentLength >= 0 && r.ContentLength != media.SizeBytes {
		g.mu.Unlock()
		return fmt.Errorf("media size mismatch")
	}
	reservation := expectedMediaUploadReservation(media.SizeBytes, r.ContentLength)
	if !g.ensureMediaCapacityLocked(media.ClientID, 0, reservation, mediaID, time.Now().UTC()) {
		g.mu.Unlock()
		return fmt.Errorf("media capacity is temporarily exhausted")
	}
	media.Uploading = true
	media.UploadReservedBytes = reservation
	expectedSize := media.SizeBytes
	expectedToken := media.Token
	g.mu.Unlock()
	uploadClaimed := true
	defer func() {
		if !uploadClaimed {
			return
		}
		g.mu.Lock()
		if current := g.media[mediaID]; current != nil && current.Token == expectedToken {
			current.Uploading = false
			current.UploadReservedBytes = 0
		}
		g.mu.Unlock()
	}()
	readLimit := expectedMediaUploadReadLimit(expectedSize, r.ContentLength)
	data, err := io.ReadAll(io.LimitReader(r.Body, readLimit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > readLimit {
		if expectedSize > 0 {
			return fmt.Errorf("media size mismatch")
		}
		return fmt.Errorf("media exceeds %d bytes", deviceGatewayMaxMediaBytes)
	}
	if expectedSize > 0 && int64(len(data)) != expectedSize {
		return fmt.Errorf("media size mismatch")
	}
	g.mu.Lock()
	media = g.media[mediaID]
	if media == nil || media.Token != expectedToken {
		g.mu.Unlock()
		return fmt.Errorf("media not found")
	}
	// The upload body is read outside g.mu. Hardware may be disabled during
	// that interval, so validate the switch again before publishing the bytes.
	if media.MachineID != "" && !g.machineHardwareEnabledLocked(media.MachineID) {
		media.Uploading = false
		media.UploadReservedBytes = 0
		g.mu.Unlock()
		return fmt.Errorf("hardware is disabled")
	}
	additionalBytes := int64(len(data) - len(media.Data))
	if additionalBytes < 0 {
		additionalBytes = 0
	}
	if additionalBytes > media.UploadReservedBytes &&
		!g.ensureMediaCapacityLocked(media.ClientID, 0, additionalBytes-media.UploadReservedBytes, mediaID, time.Now().UTC()) {
		g.mu.Unlock()
		return fmt.Errorf("media capacity is temporarily exhausted")
	}
	media.Data = data
	media.Uploaded = true
	media.Uploading = false
	media.UploadReservedBytes = 0
	media.SizeBytes = int64(len(data))
	if media.MimeType == "" {
		media.MimeType = strings.TrimSpace(r.Header.Get("Content-Type"))
	}
	media.LastAccessedAt = time.Now().UTC()
	uploadClaimed = false
	g.mu.Unlock()
	return nil
}

func expectedMediaUploadReservation(expectedSize, contentLength int64) int64 {
	if expectedSize > 0 {
		return expectedSize
	}
	if contentLength > 0 {
		return contentLength
	}
	// A zero declared size means the final size is unknown. Reserve the maximum
	// before reading so chunked uploads cannot bypass the aggregate memory cap.
	return deviceGatewayMaxMediaBytes
}

func expectedMediaUploadReadLimit(expectedSize, contentLength int64) int64 {
	if expectedSize > 0 {
		return expectedSize
	}
	if contentLength > 0 && contentLength < deviceGatewayMaxMediaBytes {
		return contentLength
	}
	return deviceGatewayMaxMediaBytes
}

func (g *DeviceGateway) mediaForDownload(r *http.Request, id string) (*deviceMedia, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	media := g.media[strings.TrimSpace(id)]
	if media != nil && deviceMediaExpired(media, time.Now().UTC()) {
		delete(g.media, strings.TrimSpace(id))
		media = nil
	}
	if media == nil || !media.Uploaded || !deviceMediaTokenOK(r, media.Token) {
		return nil, fmt.Errorf("media not found")
	}
	if media.MachineID != "" && !g.machineHardwareEnabledLocked(media.MachineID) {
		return nil, fmt.Errorf("media not found")
	}
	media.LastAccessedAt = time.Now().UTC()
	out := *media
	out.Data = append([]byte(nil), media.Data...)
	return &out, nil
}

func (g *DeviceGateway) pruneMediaLocked(now time.Time) {
	for id, media := range g.media {
		if !media.Uploading && media.QueueRefs == 0 && deviceMediaExpired(media, now) {
			delete(g.media, id)
		}
	}
	for len(g.media) > deviceGatewayMaxMediaObjects || g.mediaAllocatedBytesLocked("") > deviceGatewayMaxMediaResidentBytes {
		if !g.evictOldestMediaLocked("", "") {
			break
		}
	}
}

func deviceMediaExpired(media *deviceMedia, now time.Time) bool {
	if media == nil {
		return true
	}
	if !media.ExpiresAt.IsZero() {
		return !media.ExpiresAt.After(now)
	}
	return media.LastAccessedAt.Before(now.Add(-deviceGatewayMediaTTL))
}

func (g *DeviceGateway) mediaResidentBytesLocked(clientID string) int64 {
	var total int64
	for _, media := range g.media {
		if clientID == "" || media.ClientID == clientID {
			total += int64(len(media.Data))
		}
	}
	return total
}

func (g *DeviceGateway) mediaAllocatedBytesLocked(clientID string) int64 {
	var total int64
	for _, media := range g.media {
		if clientID == "" || media.ClientID == clientID {
			total += int64(len(media.Data)) + media.UploadReservedBytes
		}
	}
	return total
}

func (g *DeviceGateway) mediaObjectCountLocked(clientID string) int {
	count := 0
	for _, media := range g.media {
		if media.ClientID == clientID {
			count++
		}
	}
	return count
}

// ensureMediaCapacityLocked applies both global safety limits and per-client
// quotas. It first reclaims the requesting client's LRU entries, so one noisy
// ESP32 cannot routinely evict every other device's pending media.
func (g *DeviceGateway) ensureMediaCapacityLocked(clientID string, addObjects int, addBytes int64, excludeID string, now time.Time) bool {
	if addObjects < 0 || addBytes < 0 || addObjects > deviceGatewayMaxMediaObjectsPerClient || addBytes > deviceGatewayMaxMediaResidentBytesPerClient {
		return false
	}
	g.pruneMediaLocked(now)
	for g.mediaObjectCountLocked(clientID)+addObjects > deviceGatewayMaxMediaObjectsPerClient ||
		g.mediaAllocatedBytesLocked(clientID)+addBytes > deviceGatewayMaxMediaResidentBytesPerClient {
		if !g.evictOldestMediaLocked(clientID, excludeID) {
			return false
		}
	}
	for len(g.media)+addObjects > deviceGatewayMaxMediaObjects ||
		g.mediaAllocatedBytesLocked("")+addBytes > deviceGatewayMaxMediaResidentBytes {
		if !g.evictOldestMediaLocked(clientID, excludeID) && !g.evictOldestMediaLocked("", excludeID) {
			return false
		}
	}
	return true
}

func (g *DeviceGateway) evictOldestMediaLocked(clientID, excludeID string) bool {
	var oldestID string
	var oldest time.Time
	for id, media := range g.media {
		if id == excludeID || media.Uploading || media.QueueRefs > 0 || (clientID != "" && media.ClientID != clientID) {
			continue
		}
		if oldestID == "" || media.LastAccessedAt.Before(oldest) {
			oldestID, oldest = id, media.LastAccessedAt
		}
	}
	if oldestID != "" {
		delete(g.media, oldestID)
		return true
	}
	return false
}

func (g *DeviceGateway) queueDeviceMessageLocked(state *deviceClientState, message map[string]any) {
	if state == nil || message == nil {
		return
	}
	for _, mediaID := range deviceMessageMediaIDs(message) {
		if media := g.media[mediaID]; media != nil {
			media.QueueRefs++
			minimumExpiry := time.Now().UTC().Add(deviceGatewayQueuedMediaTTL)
			if media.ExpiresAt.Before(minimumExpiry) {
				media.ExpiresAt = minimumExpiry
			}
		}
	}
	state.messages = append(state.messages, message)
	g.pruneDeviceMessagesLocked(state)
}

func (g *DeviceGateway) releaseDeviceMessageMediaLocked(message map[string]any) {
	for _, mediaID := range deviceMessageMediaIDs(message) {
		if media := g.media[mediaID]; media != nil && media.QueueRefs > 0 {
			media.QueueRefs--
		}
	}
}

func deviceMessageMediaIDs(message map[string]any) []string {
	seen := make(map[string]struct{})
	addURL := func(raw any) {
		url, _ := raw.(string)
		url = strings.TrimSpace(url)
		path := url
		if parsed, err := neturl.Parse(url); err == nil {
			path = parsed.Path
		}
		const prefix = "/api/im-gateway/v1/media/"
		if !strings.HasPrefix(path, prefix) {
			return
		}
		id := strings.Trim(strings.TrimPrefix(path, prefix), "/")
		if id != "" && !strings.Contains(id, "/") {
			seen[id] = struct{}{}
		}
	}
	addURL(message["url"])
	if asset, ok := message["pet_asset"].(devicePetAssetReference); ok {
		for _, url := range asset.URLs {
			addURL(url)
		}
	} else if asset, ok := message["pet_asset"].(map[string]any); ok {
		switch urls := asset["urls"].(type) {
		case []string:
			for _, url := range urls {
				addURL(url)
			}
		case []any:
			for _, url := range urls {
				addURL(url)
			}
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	return ids
}

func cloneDeviceMessage(message map[string]any) map[string]any {
	if message == nil {
		return nil
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if json.Unmarshal(encoded, &cloned) != nil {
		return nil
	}
	return cloned
}

// activateDeviceReply registers the correlation id before asynchronous Hub/GUI
// work starts. A cold boot clears this set, so a result produced by the previous
// process can no longer enter the new process's queue even if it finishes late.
func (g *DeviceGateway) activateDeviceReply(clientID, replyTo string) {
	replyTo = strings.TrimSpace(replyTo)
	if replyTo == "" {
		return
	}
	g.mu.Lock()
	state := g.clientLocked(strings.TrimSpace(clientID))
	if state.activeReplies == nil {
		state.activeReplies = make(map[string]struct{})
	}
	if _, exists := state.activeReplies[replyTo]; !exists {
		state.activeReplies[replyTo] = struct{}{}
		state.activeOrder = append(state.activeOrder, replyTo)
		if len(state.activeOrder) > deviceGatewayMaxSeenEvents {
			drop := len(state.activeOrder) - deviceGatewayMaxSeenEvents
			for _, old := range state.activeOrder[:drop] {
				delete(state.activeReplies, old)
			}
			state.activeOrder = append([]string(nil), state.activeOrder[drop:]...)
		}
	}
	g.mu.Unlock()
}

func deviceReplyCorrelationID(reply map[string]any) string {
	for _, source := range []map[string]any{reply, deviceReplyNestedMap(reply, "metadata"), deviceReplyNestedMap(reply, "extra")} {
		for _, key := range []string{"replyTo", "replyToMessageId", "source_message_id", "sourceMessageId", "sourceMessageID"} {
			if value, ok := source[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func deviceReplyNestedMap(reply map[string]any, key string) map[string]any {
	if reply == nil {
		return nil
	}
	if nested, ok := reply[key].(map[string]any); ok {
		return nested
	}
	if nested, ok := reply[key].(map[string]string); ok {
		result := make(map[string]any, len(nested))
		for name, value := range nested {
			result[name] = value
		}
		return result
	}
	return nil
}

func deviceReplyBelongsToCurrentBootLocked(state *deviceClientState, reply map[string]any) bool {
	replyTo := deviceReplyCorrelationID(reply)
	if replyTo == "" || state == nil || state.bootSessionID == "" {
		// Unsolicited control-plane messages and legacy clients without a boot id
		// keep their existing behavior. Correlated command replies are gated once
		// a modern client has established a boot session.
		return true
	}
	_, ok := state.activeReplies[replyTo]
	return ok
}

func (g *DeviceGateway) clientLocked(clientID string) *deviceClientState {
	state := g.clients[clientID]
	if state == nil {
		state = &deviceClientState{acked: make(map[string]bool), ackStatus: make(map[string]string), activeReplies: make(map[string]struct{}), seenEvents: make(map[string]struct{}), notify: make(chan struct{})}
		g.clients[clientID] = state
	} else {
		// Older tests and in-memory states can predate delivery-status tracking.
		if state.ackStatus == nil {
			state.ackStatus = make(map[string]string)
		}
		if state.seenEvents == nil {
			state.seenEvents = make(map[string]struct{})
		}
	}
	return state
}

// markDeviceEvent provides bounded, per-client replay suppression at the HTTP
// acceptance boundary. It runs before the asynchronous GUI relay so a lost
// response followed by an ESP retry cannot execute the same incoming request
// or tool result twice.
func (g *DeviceGateway) markDeviceEvent(clientID, eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	state := g.clientLocked(clientID)
	if _, exists := state.seenEvents[eventID]; exists {
		return true
	}
	state.seenEvents[eventID] = struct{}{}
	state.seenOrder = append(state.seenOrder, eventID)
	if len(state.seenOrder) > deviceGatewayMaxSeenEvents {
		drop := len(state.seenOrder) - deviceGatewayMaxSeenEvents
		for _, old := range state.seenOrder[:drop] {
			delete(state.seenEvents, old)
		}
		copy(state.seenOrder, state.seenOrder[drop:])
		state.seenOrder = state.seenOrder[:deviceGatewayMaxSeenEvents]
	}
	return false
}

// pruneDeviceMessagesLocked bounds per-device memory and drops acknowledged
// entries eagerly. The cursor is monotonic, so retaining old acknowledged
// maps only wastes memory; if an offline device falls over the hard cap it can
// still recover current durable state through the next handshake.
func (g *DeviceGateway) pruneDeviceMessagesLocked(state *deviceClientState) {
	if state == nil || len(state.messages) == 0 {
		return
	}
	kept := state.messages[:0]
	for _, message := range state.messages {
		id, _ := message["id"].(string)
		if id != "" && state.acked[id] {
			g.releaseDeviceMessageMediaLocked(message)
			delete(state.acked, id)
			continue
		}
		kept = append(kept, message)
	}
	if len(kept) > deviceGatewayMaxQueuedMessages {
		drop := len(kept) - deviceGatewayMaxQueuedMessages
		for index := 0; index < drop; index++ {
			g.releaseDeviceMessageMediaLocked(kept[index])
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
	token := deviceBearerToken(r)
	if token == "" {
		return devicePrincipal{}, false
	}
	g.mu.Lock()
	p, ok := g.tokens[token]
	if ok {
		// Every authenticated device request is proof of liveness. Handshake-only
		// timestamps made a healthy long-polling ESP32 appear offline after 90s,
		// causing remote audio previews to be rejected until the next reboot.
		now := time.Now().UTC()
		state := g.clientLocked(p.ClientID)
		if state.lastSeenFlushAt.IsZero() && !p.LastSeenAt.IsZero() {
			state.lastSeenFlushAt = p.LastSeenAt
		}
		p.LastSeenAt = now
		state.lastSeenAt = now
		for bearer, principal := range g.tokens {
			if principal.ClientID == p.ClientID && principal.MachineID == p.MachineID {
				principal.LastSeenAt = p.LastSeenAt
				g.tokens[bearer] = principal
			}
		}
		if !state.lastSeenFlushAt.IsZero() && now.Sub(state.lastSeenFlushAt) >= deviceGatewayPresenceFlushInterval {
			// Throttle failed writes as well: availability must not turn a store
			// outage into one settings write per long poll.
			state.lastSeenFlushAt = now
			if err := g.persistTokensLocked(); err != nil {
				log.Printf("device gateway: persist last-seen for client %q: %v", p.ClientID, err)
			}
		}
	}
	g.mu.Unlock()
	return p, ok
}

func deviceBearerToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return ""
	}
	return strings.TrimSpace(auth[len("Bearer "):])
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
