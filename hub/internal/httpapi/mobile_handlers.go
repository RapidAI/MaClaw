package httpapi

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	gopdf "github.com/VantageDataChat/GoPDF2"
	"github.com/gorilla/websocket"
)

var mobileWebSearch func(context.Context, string, int) ([]websearch.SearchResult, error) = websearch.SearchCtx

var mobileDocuments = struct {
	sync.Mutex
	drafts  map[string]mobileDocumentDraftRecord
	exports map[string]mobileDocumentExportRecord
	uploads map[string]mobileDocumentUploadRecord
}{
	drafts:  make(map[string]mobileDocumentDraftRecord),
	exports: make(map[string]mobileDocumentExportRecord),
	uploads: make(map[string]mobileDocumentUploadRecord),
}

var mobileDigitalEmployeeTasks = struct {
	sync.Mutex
	tasks map[string]mobileDigitalEmployeeTaskRecord
}{
	tasks: make(map[string]mobileDigitalEmployeeTaskRecord),
}

var mobileBackendSSHSessions = struct {
	sync.Mutex
	sessions map[string]mobileBackendSSHSessionRecord
}{
	sessions: make(map[string]mobileBackendSSHSessionRecord),
}

var mobileBackendSSHTasks = struct {
	sync.Mutex
	tasks map[string]mobileBackendSSHTaskRecord
}{
	tasks: make(map[string]mobileBackendSSHTaskRecord),
}

var mobileBackendSSHFileOperations = struct {
	sync.Mutex
	operations map[string]mobileBackendSSHFileOperationRecord
}{
	operations: make(map[string]mobileBackendSSHFileOperationRecord),
}

var mobileServerProfiles = struct {
	sync.Mutex
	profiles map[string]mobileServerProfileRecord
}{
	profiles: make(map[string]mobileServerProfileRecord),
}

var mobileLlmAuthorizations = struct {
	sync.Mutex
	authorizations map[string]mobileLlmAuthorizationRecord
	qrSessions     map[string]mobileLlmQRSessionRecord
}{
	authorizations: make(map[string]mobileLlmAuthorizationRecord),
	qrSessions:     make(map[string]mobileLlmQRSessionRecord),
}

var mobileStatePersistence = struct {
	sync.Mutex
	loaded bool
}{}

var mobileOfficialHubCenterCandidates = []string{
	"https://hubs.mypapers.top",
	"https://hubs.maclaw.top",
	"https://hubs2.maclaw.top",
}

const mobileStatePathEnv = "MACLAW_MOBILE_STATE_PATH"

// mobileStatePathOverride is set by InitMobileStatePersistence when env is empty
// (Hub runtime data dir / mobile/state.json).
var mobileStatePathOverride string

type mobilePersistentState struct {
	Drafts               map[string]mobileDocumentDraftRecord       `json:"drafts"`
	Exports              map[string]mobileDocumentExportRecord      `json:"exports"`
	Uploads              map[string]mobileDocumentUploadRecord      `json:"uploads"`
	DigitalEmployeeTasks map[string]mobileDigitalEmployeeTaskRecord `json:"digital_employee_tasks"`
	// PushDevices / PushPending survive Hub restarts (Phase E).
	PushDevices map[string][]mobilePushDevice      `json:"push_devices,omitempty"`
	PushPending map[string][]mobilePushPendingItem `json:"push_pending,omitempty"`
	// ServerProfiles / SSHVaultSecrets power hub_exec + mobile AI ssh tools.
	ServerProfiles  map[string]mobileServerProfileRecord `json:"server_profiles,omitempty"`
	SSHVaultSecrets map[string]mobileSSHVaultRecord      `json:"ssh_vault_secrets,omitempty"`
	SavedAt         time.Time                            `json:"saved_at"`
}

// InitMobileStatePersistence sets default state file under runtime data when
// MACLAW_MOBILE_STATE_PATH is unset, then loads documents/employees/push state.
func InitMobileStatePersistence(runtimeDataDir string) {
	if strings.TrimSpace(os.Getenv(mobileStatePathEnv)) == "" {
		dir := strings.TrimSpace(runtimeDataDir)
		if dir != "" {
			mobileStatePathOverride = filepath.Join(dir, "mobile", "state.json")
		}
	}
	mobileEnsureStateLoaded()
}

type mobileDocumentDraftRecord struct {
	ID        string
	OwnerID   string
	Title     string
	Template  string
	Markdown  string
	UpdatedAt time.Time
	// Original uploaded file (source of truth for preview/share/AI). Optional for
	// pure-text drafts created without a file.
	SourceFilename    string
	SourceContentType string
	// SourcePath is relative to mobile blob dir (durable). SourceBytes is a
	// process-local cache and is stripped when writing state.json.
	SourcePath  string
	SourceSize  int
	SourceBytes []byte
}

type mobileDocumentExportRecord struct {
	JobID     string
	DraftID   string
	OwnerID   string
	Format    string
	Status    string
	Message   string
	CreatedAt time.Time
}

type mobileDocumentUploadRecord struct {
	TaskID      string
	OwnerID     string
	Filename    string
	ContentType string
	Status      string
	DraftID     string
	Message     string
	ClaimedBy   string
	SourcePath  string
	SourceSize  int
	SourceBytes []byte
	OCRMarkdown string
	OCRMessage  string
	OCRError    string
	UploadedAt  time.Time
	UpdatedAt   time.Time
}

type mobileDigitalEmployeeTaskRecord struct {
	TaskID     string
	EmployeeID string
	OwnerID    string
	Prompt     string
	TaskType   string
	Context    map[string]string
	Status     string
	Result     string
	Message    string
	ClaimedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type mobileBackendSSHSessionRecord struct {
	SessionID        string
	TenantID         string
	OwnerID          string
	ServerProfileID  string
	BackendSessionID string
	// ExecMode: desktop_exec (GUI/agent claim) or hub_exec (Hub vault + direct SSH).
	ExecMode     string
	Status       string
	State        string
	Message      string
	RecentOutput string
	OutputChunk  string
	OutputSeq    int64
	PendingInput []string
	ClaimedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type mobileBackendSSHTaskRecord struct {
	TaskID           string
	SessionID        string
	TenantID         string
	OwnerID          string
	BackendSessionID string
	Action           string
	Command          string
	Status           string
	Message          string
	LogTail          string
	ExitCode         *int
	TailLines        int
	TimeoutSeconds   int
	ClaimedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type mobileBackendSSHFileOperationRecord struct {
	OperationID      string
	SessionID        string
	TenantID         string
	OwnerID          string
	BackendSessionID string
	Action           string
	LocalPath        string
	RemotePath       string
	Status           string
	Message          string
	BytesTransferred int64
	DownloadURL      string
	ClaimedBy        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type mobileServerProfileRecord struct {
	ProfileID       string
	TenantID        string
	OwnerID         string
	SourceMachineID string
	Name            string
	Host            string
	Port            int
	Username        string
	AuthMode        string
	Tag             string
	Note            string
	UpdatedAt       time.Time
}

type mobileLlmAuthorizationRecord struct {
	AuthorizationID string
	OwnerID         string
	TenantID        string
	ProviderName    string
	ProviderURL     string
	APIKey          string
	Model           string
	Protocol        string
	AuthorizedAt    time.Time
}

type mobileLlmQRSessionRecord struct {
	SessionID    string
	OwnerID      string
	TenantID     string
	Purpose      string
	ProviderName string
	ProviderURL  string
	APIKey       string
	Model        string
	Protocol     string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   time.Time
}

type mobileDesktopLlmQRPayload struct {
	Version       int      `json:"v"`
	Type          string   `json:"type"`
	SessionID     string   `json:"session_id"`
	HubURL        string   `json:"hub_url"`
	ExpiresAt     string   `json:"expires_at"`
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	Key           string   `json:"key"`
	Model         string   `json:"model"`
	Models        []string `json:"models"`
	Protocol      string   `json:"protocol"`
	ContextLength int      `json:"context_length"`
}

const (
	mobileDesktopLlmAuthorizationQRType = "maclaw_mobile_llm_authorization"
	mobileQRSessionPurposeLLM           = "llm"
)

func mobileStatePath() string {
	if p := strings.TrimSpace(os.Getenv(mobileStatePathEnv)); p != "" {
		return p
	}
	return strings.TrimSpace(mobileStatePathOverride)
}

func mobileEnsureStateLoaded() {
	mobileStatePersistence.Lock()
	if mobileStatePersistence.loaded {
		mobileStatePersistence.Unlock()
		return
	}
	mobileStatePersistence.loaded = true
	path := mobileStatePath()
	mobileStatePersistence.Unlock()
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return
	}
	var state mobilePersistentState
	if err := json.Unmarshal(raw, &state); err != nil {
		return
	}
	mobileDocuments.Lock()
	if state.Drafts != nil {
		// Do not eagerly load multi-MB originals into RAM. Bytes are filled on
		// demand via mobileDraftLoadSourceBytes; only normalize SourceSize here.
		// Legacy state.json may still embed SourceBytes — keep them until next persist strips to disk.
		for id, rec := range state.Drafts {
			mobileNormalizeDraftSourceMeta(&rec)
			state.Drafts[id] = rec
		}
		mobileDocuments.drafts = state.Drafts
	}
	if state.Exports != nil {
		mobileDocuments.exports = state.Exports
	}
	if state.Uploads != nil {
		for id, rec := range state.Uploads {
			mobileNormalizeUploadSourceMeta(&rec)
			state.Uploads[id] = rec
		}
		mobileDocuments.uploads = state.Uploads
	}
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	if state.DigitalEmployeeTasks != nil {
		mobileDigitalEmployeeTasks.tasks = state.DigitalEmployeeTasks
	}
	mobileDigitalEmployeeTasks.Unlock()
	mobilePushLoadFromState(state.PushDevices, state.PushPending)
	if state.ServerProfiles != nil {
		mobileServerProfiles.Lock()
		mobileServerProfiles.profiles = state.ServerProfiles
		mobileServerProfiles.Unlock()
	}
	if state.SSHVaultSecrets != nil {
		mobileSSHVault.Lock()
		mobileSSHVault.secrets = state.SSHVaultSecrets
		mobileSSHVault.Unlock()
	}
}

func mobilePersistState() {
	path := mobileStatePath()
	if path == "" {
		return
	}
	state := mobilePersistentState{
		Drafts:               make(map[string]mobileDocumentDraftRecord),
		Exports:              make(map[string]mobileDocumentExportRecord),
		Uploads:              make(map[string]mobileDocumentUploadRecord),
		DigitalEmployeeTasks: make(map[string]mobileDigitalEmployeeTaskRecord),
		PushDevices:          make(map[string][]mobilePushDevice),
		PushPending:          make(map[string][]mobilePushPendingItem),
		ServerProfiles:       make(map[string]mobileServerProfileRecord),
		SSHVaultSecrets:      make(map[string]mobileSSHVaultRecord),
		SavedAt:              time.Now().UTC(),
	}
	mobileDocuments.Lock()
	for id, record := range mobileDocuments.drafts {
		// Do not embed multi-MB originals in state.json.
		stripped := mobileStripDraftBlobForPersist(record)
		state.Drafts[id] = stripped
		// Backfill durable path into the live record when strip just wrote the
		// blob. Avoids re-writing the same bytes on every subsequent persist and
		// keeps SourcePath available if SourceBytes is later dropped.
		if p := strings.TrimSpace(stripped.SourcePath); p != "" && strings.TrimSpace(record.SourcePath) == "" {
			record.SourcePath = p
			if record.SourceSize == 0 {
				record.SourceSize = stripped.SourceSize
			}
			if len(record.SourceBytes) > mobileDocumentSourceHotCacheMax {
				record.SourceBytes = nil
			}
			mobileDocuments.drafts[id] = record
		}
	}
	for id, record := range mobileDocuments.exports {
		state.Exports[id] = record
	}
	for id, record := range mobileDocuments.uploads {
		stripped := mobileStripUploadBlobForPersist(record)
		state.Uploads[id] = stripped
		if p := strings.TrimSpace(stripped.SourcePath); p != "" && strings.TrimSpace(record.SourcePath) == "" {
			record.SourcePath = p
			if record.SourceSize == 0 {
				record.SourceSize = stripped.SourceSize
			}
			if len(record.SourceBytes) > mobileDocumentSourceHotCacheMax {
				record.SourceBytes = nil
			}
			mobileDocuments.uploads[id] = record
		}
	}
	mobileDocuments.Unlock()
	mobileDigitalEmployeeTasks.Lock()
	for id, record := range mobileDigitalEmployeeTasks.tasks {
		state.DigitalEmployeeTasks[id] = record
	}
	mobileDigitalEmployeeTasks.Unlock()
	mobilePushSnapshotInto(&state)
	mobileServerProfiles.Lock()
	for id, record := range mobileServerProfiles.profiles {
		state.ServerProfiles[id] = record
	}
	mobileServerProfiles.Unlock()
	mobileSSHVault.Lock()
	for id, record := range mobileSSHVault.secrets {
		state.SSHVaultSecrets[id] = record
	}
	mobileSSHVault.Unlock()
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func mobileResetStatePersistenceForTest() {
	mobileStatePersistence.Lock()
	mobileStatePersistence.loaded = false
	mobileStatePathOverride = ""
	mobileStatePersistence.Unlock()
	mobilePushResetForTest()
}

var mobileRealtimeUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type mobileRealtimeJSONWriter interface {
	WriteJSON(v any) error
}

// Optional binary writer (satisfied by *websocket.Conn).
type mobileRealtimeBinaryWriter interface {
	WriteMessage(messageType int, data []byte) error
}

type mobileRealtimeClient struct {
	key       string
	conn      mobileRealtimeJSONWriter
	binary    mobileRealtimeBinaryWriter // optional
	binaryPty bool                       // client advertised caps=["pty_binary"]
	mu        sync.Mutex
}

var mobileRealtimeClients = struct {
	sync.Mutex
	clients map[string]map[*mobileRealtimeClient]struct{}
}{
	clients: make(map[string]map[*mobileRealtimeClient]struct{}),
}

func mobileRealtimeKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

func mobileRealtimeRegister(tenantID, userID string, conn mobileRealtimeJSONWriter) (*mobileRealtimeClient, func()) {
	client := &mobileRealtimeClient{
		key:  mobileRealtimeKey(tenantID, userID),
		conn: conn,
	}
	if bw, ok := conn.(mobileRealtimeBinaryWriter); ok {
		client.binary = bw
	}
	mobileRealtimeClients.Lock()
	if mobileRealtimeClients.clients[client.key] == nil {
		mobileRealtimeClients.clients[client.key] = make(map[*mobileRealtimeClient]struct{})
	}
	mobileRealtimeClients.clients[client.key][client] = struct{}{}
	mobileRealtimeClients.Unlock()
	return client, func() {
		mobileRealtimeUnregister(client)
	}
}

func mobileRealtimeUnregister(client *mobileRealtimeClient) {
	if client == nil {
		return
	}
	mobileRealtimeClients.Lock()
	if bucket := mobileRealtimeClients.clients[client.key]; bucket != nil {
		delete(bucket, client)
		if len(bucket) == 0 {
			delete(mobileRealtimeClients.clients, client.key)
		}
	}
	mobileRealtimeClients.Unlock()
}

func mobileRealtimeBroadcast(tenantID, userID string, event map[string]any) {
	key := mobileRealtimeKey(tenantID, userID)
	mobileRealtimeClients.Lock()
	clients := make([]*mobileRealtimeClient, 0, len(mobileRealtimeClients.clients[key]))
	for client := range mobileRealtimeClients.clients[key] {
		clients = append(clients, client)
	}
	mobileRealtimeClients.Unlock()
	payload := make(map[string]any, len(event)+1)
	for k, v := range event {
		payload[k] = v
	}
	if _, ok := payload["server_time"]; !ok {
		payload["server_time"] = time.Now().UTC().Format(time.RFC3339)
	}
	for _, client := range clients {
		client.mu.Lock()
		err := client.conn.WriteJSON(payload)
		// Dual-write PTY output as MCP1 binary when client opted in.
		if err == nil && client.binaryPty && client.binary != nil {
			if bin, binOK := mobileRealtimeMaybeBinaryPtyOut(payload); binOK {
				if werr := client.binary.WriteMessage(websocket.BinaryMessage, bin); werr != nil {
					err = werr
				}
			}
		}
		client.mu.Unlock()
		if err != nil {
			mobileRealtimeUnregister(client)
		}
	}
	// Offline completion: pending queue + optional remote (webhook/FCM) when
	// no live realtime clients (or MACLAW_MOBILE_PUSH_ALWAYS).
	mobilePushOnRealtimeEvent(tenantID, userID, payload, len(clients))
}

// mobileRealtimeMaybeBinaryPtyOut builds MCP1 pty_out for ssh_session output_chunk events.
func mobileRealtimeMaybeBinaryPtyOut(event map[string]any) ([]byte, bool) {
	if event == nil {
		return nil, false
	}
	typ, _ := event["type"].(string)
	if strings.ToLower(strings.TrimSpace(typ)) != "ssh_session" {
		return nil, false
	}
	chunk, _ := event["output_chunk"].(string)
	if chunk == "" {
		if sess, ok := event["session"].(map[string]any); ok {
			chunk, _ = sess["output_chunk"].(string)
		}
	}
	if chunk == "" {
		return nil, false
	}
	sessionID, _ := event["session_id"].(string)
	if sessionID == "" {
		if sess, ok := event["session"].(map[string]any); ok {
			sessionID, _ = sess["session_id"].(string)
		}
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, false
	}
	// Use raw bytes of the UTF-8 string (terminal stream is often not pure text).
	frame, err := mobilePtyBinaryEncodeOutput(sessionID, []byte(chunk))
	if err != nil {
		return nil, false
	}
	return frame, true
}

func mobileRealtimeDocumentTaskEvent(kind string, payload map[string]any) map[string]any {
	event := map[string]any{
		"type": kind,
		"task": payload,
	}
	if taskID, _ := payload["task_id"].(string); taskID != "" {
		event["task_id"] = taskID
	}
	if jobID, _ := payload["job_id"].(string); jobID != "" {
		event["job_id"] = jobID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

func mobileRealtimeDigitalEmployeeTaskEvent(payload map[string]any) map[string]any {
	event := map[string]any{
		"type": "digital_employee_task",
		"task": payload,
	}
	if taskID, _ := payload["task_id"].(string); taskID != "" {
		event["task_id"] = taskID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

// MobileMachinePush notifies online desktop workers over the machine WebSocket
// so they can claim digital-employee tasks immediately instead of waiting for poll.
type MobileMachinePush interface {
	ListOnlineMachineIDsForUser(ctx context.Context, userID string) []string
	SendToMachine(machineID string, msg any) error
}

var mobileMachinePush MobileMachinePush

// ConfigureMobileMachinePush wires desktop push for mobile digital-employee tasks.
func ConfigureMobileMachinePush(push MobileMachinePush) {
	mobileMachinePush = push
}

// mobileNotifyDesktopWorkersOfDigitalEmployeeTask wakes candidate GUI workers.
// Personal tasks go to all online desktops of the phone user; hosted VEs also
// try the registry host machine when resolvable.
func mobileNotifyDesktopWorkersOfDigitalEmployeeTask(ctx context.Context, tenantID, ownerID, employeeID string, taskPayload map[string]any) {
	if mobileMachinePush == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	ownerID = strings.TrimSpace(ownerID)
	employeeID = strings.TrimSpace(employeeID)
	if ownerID == "" {
		return
	}
	seen := map[string]struct{}{}
	add := func(machineID string) {
		machineID = strings.TrimSpace(machineID)
		if machineID == "" {
			return
		}
		if _, ok := seen[machineID]; ok {
			return
		}
		seen[machineID] = struct{}{}
	}
	for _, id := range mobileMachinePush.ListOnlineMachineIDsForUser(ctx, ownerID) {
		add(id)
	}
	// Host machine of the targeted VE (shared digital employee on another desktop).
	if employeeID != "" && mobileQuotaSystem != nil {
		tenantSystem := scopedSystemSettingsForTenant(tenantID, mobileQuotaSystem)
		for _, emp := range loadVERegistry(ctx, tenantSystem).Employees {
			if !groupDiscussionParticipantIdentityMatches(emp.ID, employeeID) &&
				!groupDiscussionParticipantIdentityMatches(emp.MachineID, employeeID) {
				continue
			}
			add(emp.MachineID)
		}
	}
	if len(seen) == 0 {
		return
	}
	msg := map[string]any{
		"type": "mobile.digital_employee_task",
		"ts":   time.Now().Unix(),
		"payload": map[string]any{
			"action":      "claim_now",
			"task_id":     taskPayload["task_id"],
			"employee_id": employeeID,
			"status":      taskPayload["status"],
		},
	}
	for machineID := range seen {
		_ = mobileMachinePush.SendToMachine(machineID, msg)
	}
}

// MobileRealtimeHandler upgrades authenticated mobile clients to a lightweight
// realtime channel. Long-running document and digital employee flows can still
// poll, but the mobile app now has an official Hub WebSocket endpoint for task
// completion and connection-health signals.
func MobileRealtimeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		conn, err := mobileRealtimeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		client, unregister := mobileRealtimeRegister(principal.TenantID, principal.UserID, conn)
		defer unregister()

		client.mu.Lock()
		err = client.conn.WriteJSON(map[string]any{
			"type":        "ready",
			"user_id":     principal.UserID,
			"tenant_id":   principal.TenantID,
			"server_time": time.Now().UTC().Format(time.RFC3339),
			"caps":        []string{"pty_binary", "pty_data_b64", "json"},
		})
		client.mu.Unlock()
		if err != nil {
			return
		}
		for {
			msgType, data, readErr := conn.ReadMessage()
			if readErr != nil {
				return
			}
			if msgType == websocket.BinaryMessage {
				go mobileHandleRealtimeBinaryMessage(client, principal, data)
				continue
			}
			// Text frames: JSON control / legacy pty_input.
			var msg map[string]any
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(fmt.Sprint(msg["type"]))) {
			case "ping":
				client.mu.Lock()
				err := client.conn.WriteJSON(map[string]any{
					"type":        "pong",
					"server_time": time.Now().UTC().Format(time.RFC3339),
				})
				client.mu.Unlock()
				if err != nil {
					return
				}
			case "hello":
				// Client capability advertisement (pty_binary enables MCP1 dual-write).
				client.mu.Lock()
				client.binaryPty = mobileRealtimeCapsIncludePtyBinary(msg["caps"])
				client.mu.Unlock()
				mobileRealtimeWriteClient(client, map[string]any{
					"type":        "hello_ack",
					"ok":          true,
					"binary_pty":  client.binaryPty,
					"server_time": time.Now().UTC().Format(time.RFC3339),
				})
			case "pty_input":
				// hub_exec interactive path: accept input over WS so the mobile
				// terminal can send keys without a new HTTP round-trip.
				// Run async so the socket keeps reading (and streaming) other events.
				sessionID, _ := msg["session_id"].(string)
				input, _ := msg["input"].(string)
				dataB64, _ := msg["data_b64"].(string)
				raw, _ := msg["raw"].(bool)
				go mobileHandleRealtimePtyInput(client, principal, sessionID, input, dataB64, raw)
			}
		}
	}
}

func mobileRealtimeCapsIncludePtyBinary(raw any) bool {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(item)), "pty_binary") {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if strings.EqualFold(strings.TrimSpace(item), "pty_binary") {
				return true
			}
		}
	case string:
		return strings.Contains(strings.ToLower(v), "pty_binary")
	}
	return false
}

// mobileHandleRealtimeBinaryMessage decodes MCP1 frames (pty_in).
func mobileHandleRealtimeBinaryMessage(client *mobileRealtimeClient, principal *auth.ViewerPrincipal, data []byte) {
	if client == nil || principal == nil {
		return
	}
	if !mobilePtyBinaryIsMagic(data) {
		return
	}
	frame, err := mobilePtyBinaryDecode(data)
	if err != nil {
		mobileRealtimeWritePtyAckJSONAndMaybeBinary(client, "", false, err.Error(), "INVALID_INPUT")
		return
	}
	if frame.Type != mobilePtyBinaryTypeIn {
		mobileRealtimeWritePtyAckJSONAndMaybeBinary(client, frame.SessionID, false, "unsupported binary frame type", "INVALID_INPUT")
		return
	}
	// Mark client as binary-capable on first binary frame (even without hello).
	client.mu.Lock()
	client.binaryPty = true
	client.mu.Unlock()
	input := string(frame.Payload)
	go mobileHandleRealtimePtyInput(client, principal, frame.SessionID, input, "", frame.Raw())
}

// mobileRealtimeWriteBinaryAck writes MCP1 pty_ack only (no JSON).
// Callers that need JSON should WriteJSON separately.
func mobileRealtimeWriteBinaryAck(client *mobileRealtimeClient, sessionID string, ok bool, errMsg string) {
	if client == nil {
		return
	}
	client.mu.Lock()
	binWriter := client.binary
	client.mu.Unlock()
	if binWriter == nil {
		return
	}
	frame, err := mobilePtyBinaryEncodeAck(sessionID, ok, errMsg)
	if err != nil {
		return
	}
	client.mu.Lock()
	_ = binWriter.WriteMessage(websocket.BinaryMessage, frame)
	client.mu.Unlock()
}

// mobileRealtimeWritePtyAckJSONAndMaybeBinary emits JSON pty_ack and optional MCP1 ack.
func mobileRealtimeWritePtyAckJSONAndMaybeBinary(client *mobileRealtimeClient, sessionID string, ok bool, errMsg, code string) {
	ack := map[string]any{
		"type":        "pty_ack",
		"ok":          ok,
		"session_id":  sessionID,
		"binary":      true,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}
	if !ok {
		if errMsg != "" {
			ack["error"] = errMsg
		}
		if code != "" {
			ack["code"] = code
		}
	}
	mobileRealtimeWriteClient(client, ack)
	client.mu.Lock()
	useBinary := client.binaryPty
	client.mu.Unlock()
	if useBinary {
		mobileRealtimeWriteBinaryAck(client, sessionID, ok, errMsg)
	}
}

// mobileResolvePtyInputBytes resolves plain input and/or base64 binary frames.
// data_b64 (std or raw encoding) wins when non-empty and forces raw mode.
// Max payload 16KiB to keep interactive frames bounded.
func mobileResolvePtyInputBytes(input, dataB64 string, raw bool) (resolved string, forceRaw bool, err error) {
	dataB64 = strings.TrimSpace(dataB64)
	if dataB64 != "" {
		decoded, decErr := base64.StdEncoding.DecodeString(dataB64)
		if decErr != nil {
			decoded, decErr = base64.RawStdEncoding.DecodeString(dataB64)
		}
		if decErr != nil {
			return "", false, fmt.Errorf("invalid data_b64: %w", decErr)
		}
		if len(decoded) == 0 {
			return "", false, fmt.Errorf("data_b64 is empty")
		}
		if len(decoded) > 16<<10 {
			return "", false, fmt.Errorf("data_b64 too large (max 16KiB)")
		}
		return string(decoded), true, nil
	}
	if !raw {
		input = strings.TrimSpace(input)
	}
	return input, raw, nil
}

// mobileHandleRealtimePtyInput applies hub_exec input for a viewer over the realtime socket.
func mobileHandleRealtimePtyInput(client *mobileRealtimeClient, principal *auth.ViewerPrincipal, sessionID, input, dataB64 string, raw bool) {
	if client == nil || principal == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		mobileRealtimeWriteClient(client, map[string]any{
			"type":        "pty_ack",
			"ok":          false,
			"error":       "session_id is required",
			"code":        "INVALID_INPUT",
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	resolved, forceRaw, resolveErr := mobileResolvePtyInputBytes(input, dataB64, raw)
	if resolveErr != nil {
		mobileRealtimeWriteClient(client, map[string]any{
			"type":        "pty_ack",
			"ok":          false,
			"session_id":  sessionID,
			"error":       resolveErr.Error(),
			"code":        "INVALID_INPUT",
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	input = resolved
	raw = forceRaw
	if input == "" {
		mobileRealtimeWriteClient(client, map[string]any{
			"type":        "pty_ack",
			"ok":          false,
			"session_id":  sessionID,
			"error":       "input is required",
			"code":        "INVALID_INPUT",
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	mobileBackendSSHSessions.Lock()
	record, ok := mobileBackendSSHSessions.sessions[sessionID]
	if ok && (record.OwnerID != principal.UserID || record.TenantID != principal.TenantID) {
		ok = false
	}
	mobileBackendSSHSessions.Unlock()
	if !ok {
		mobileRealtimeWriteClient(client, map[string]any{
			"type":        "pty_ack",
			"ok":          false,
			"session_id":  sessionID,
			"error":       "backend SSH session not found",
			"code":        "SESSION_NOT_FOUND",
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	if record.ExecMode != mobileSSHExecHub {
		mobileRealtimeWriteClient(client, map[string]any{
			"type":        "pty_ack",
			"ok":          false,
			"session_id":  sessionID,
			"error":       "pty_input requires exec_mode=hub_exec",
			"code":        "HUB_EXEC_REQUIRED",
			"server_time": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	out, runErr := mobileHubSSHRunInput(&record, input, raw)
	mobileBackendSSHSessions.Lock()
	mobileBackendSSHSessions.sessions[sessionID] = record
	mobileBackendSSHSessions.Unlock()
	payload := mobileBackendSSHSessionPayload(record)
	// Fan-out session state (includes progressive output already streamed mid-run).
	mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))

	client.mu.Lock()
	useBinary := client.binaryPty
	client.mu.Unlock()
	ack := map[string]any{
		"type":        "pty_ack",
		"ok":          runErr == nil,
		"session_id":  sessionID,
		"raw":         raw,
		"binary":      strings.TrimSpace(dataB64) != "" || useBinary,
		"output":      out,
		"status":      record.Status,
		"message":     record.Message,
		"session":     payload,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}
	if runErr != nil {
		ack["error"] = runErr.Error()
		ack["code"] = "HUB_SSH_INPUT_FAILED"
	}
	mobileRealtimeWriteClient(client, ack)
	if useBinary {
		errMsg := ""
		if runErr != nil {
			errMsg = runErr.Error()
		}
		mobileRealtimeWriteBinaryAck(client, sessionID, runErr == nil, errMsg)
	}
}

func mobileRealtimeWriteClient(client *mobileRealtimeClient, event map[string]any) {
	if client == nil {
		return
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.conn == nil {
		return
	}
	_ = client.conn.WriteJSON(event)
}

// MobileBootstrapHandler returns the small, cheap payload the mobile app needs
// immediately after restoring a viewer token. Expensive service details stay on
// their existing dedicated endpoints; grant snapshot is best-effort and cached.
func MobileBootstrapHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		writeJSON(w, http.StatusOK, mobileBootstrapPayloadForRequest(principal, r, system, securitySvc))
	}
}

func mobileBootstrapPayload(principal *auth.ViewerPrincipal) map[string]any {
	return mobileBootstrapPayloadForRequest(principal, nil, nil, nil)
}

func mobileBootstrapPayloadForRequest(principal *auth.ViewerPrincipal, r *http.Request, system store.SystemSettingsRepository, securitySvc *security.SecurityService) map[string]any {
	userID := ""
	email := ""
	tenantID := ""
	if principal != nil {
		userID = principal.UserID
		email = principal.Email
		tenantID = principal.TenantID
	}
	hubURL := mobileRequestBaseURL(r)
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	llmAccess := mobileLlmAccessPayload(ctx, principal)
	phoneNumber := mobilePrincipalPhoneNumber(email)
	creditsAccount := mobilePrincipalCreditsAccount(email, userID)
	grant := mobileResolveServiceGrantSnapshot(ctx, principal, system, securitySvc, hubURL)
	plan := mobilePlanForAccessWithGrant(llmAccess, grant)
	entitled := mobileOfficialEntitled(llmAccess) || grant.Active
	caps := mobilePlanCapsFor(plan, grant, mobileOfficialEntitled(llmAccess))
	return map[string]any{
		"user": map[string]any{
			"user_id":         userID,
			"email":           email,
			"phone_number":    phoneNumber,
			"account_id":      firstNonEmpty(email, userID),
			"credits_account": creditsAccount,
			"tenant_id":       tenantID,
		},
		"connection": map[string]any{
			"hubcenter_candidates":   append([]string(nil), mobileOfficialHubCenterCandidates...),
			"selected_hubcenter_url": mobileOfficialHubCenterCandidates[1],
			"hub_url":                hubURL,
			"hub_id":                 "",
			"tenant_id":              tenantID,
		},
		"llm_access":     llmAccess,
		"assistant_mode": mobileAssistantModeForAccess(llmAccess),
		"entitlements":   caps.toEntitlementMap(grant, entitled),
		"features": map[string]any{
			"search":               true,
			"documents":            true,
			"tasks":                true,
			"backend_ssh_sessions": true,
			"hub_ssh_exec":         caps.HubSSHExec,
			"digital_employees":    true,
			// Remote FCM/webhook only when transport env is set; pending sync always on.
			"push_notifications": mobilePushRemoteConfigured(),
			"push_pending_sync":  true,
		},
		"services": map[string]any{
			"hub_status":               "online",
			"llm_status":               "available",
			"search_status":            "available",
			"documents_status":         "available",
			"digital_employees_status": "available",
			"llm_status_path":          "/api/llm/service/status",
			"models_path":              "/api/llm/v1/models",
			"search_path":              "/api/mobile/search",
			"documents_path":           "/api/mobile/documents",
			"documents_quota_path":     "/api/mobile/documents/quota",
			"entitlements_caps_path":   "/api/mobile/entitlements/caps",
			"digital_employees_path":   "/api/mobile/digital-employees",
			"realtime_path":            "/api/mobile/realtime",
			"push_devices_path":        "/api/mobile/push/devices",
			"push_pending_path":        "/api/mobile/push/pending",
			"push_pending_ack_path":    "/api/mobile/push/pending/ack",
			"llm_account_path":         "/api/llm/service/account",
			"llm_card_redeem_path":     "/api/llm/service/redeem",
			"card_store_products_path": "/api/card-store/products",
		},
		"push": mobilePushTransportSummary(),
		"limits": map[string]any{
			"max_upload_bytes":            caps.MaxUploadBytes,
			"document_quota_bytes":        caps.DocumentQuotaBytes,
			"document_quota_used_bytes":   mobileDocumentQuotaUsedBytes(userID),
			"max_export_jobs":             caps.MaxExportJobs,
			"hub_file_download_max_bytes": caps.HubFileDownloadMaxBytes,
		},
		"server_time": time.Now().UTC().Format(time.RFC3339),
	}
}

type mobileServiceGrantSnapshot struct {
	Active            bool
	CreditsAvailable  float64
	CreditsRemaining  float64
	ServiceGroupCount int
	HasCardGrant      bool
}

// mobileResolveServiceGrantSnapshot is best-effort: failures yield zero values
// so bootstrap stays cheap and never hard-fails on registry load issues.
func mobileResolveServiceGrantSnapshot(ctx context.Context, principal *auth.ViewerPrincipal, system store.SystemSettingsRepository, securitySvc *security.SecurityService, hubBaseURL string) mobileServiceGrantSnapshot {
	out := mobileServiceGrantSnapshot{}
	if principal == nil || system == nil {
		return out
	}
	tenantID := strings.TrimSpace(principal.TenantID)
	if tenantID != "" {
		system = scopedSystemSettingsForTenant(tenantID, system)
		ctx = security.WithTenant(ctx, tenantID)
	}
	reg, err := loadCachedLLMServiceRegistry(ctx, system)
	if err != nil || reg == nil {
		return out
	}
	status, _, err := llmservice.ResolveStatusFromRegistryForUser(ctx, reg, securitySvc, principal.UserID, principal.Email, hubBaseURL)
	if err != nil || status == nil {
		return out
	}
	out.Active = status.Active
	out.CreditsAvailable = status.CreditsAvailable
	out.CreditsRemaining = status.CreditsRemaining
	// Count active grants that look like card redemptions.
	now := time.Now().UTC()
	groups := map[string]struct{}{}
	for _, g := range reg.Grants {
		email := strings.ToLower(strings.TrimSpace(g.Email))
		userEmail := strings.ToLower(strings.TrimSpace(principal.Email))
		if email == "" || email != userEmail {
			continue
		}
		if !g.ExpiresAt.IsZero() && !g.ExpiresAt.After(now) {
			continue
		}
		if !g.StartsAt.IsZero() && g.StartsAt.After(now) {
			continue
		}
		if sid := strings.TrimSpace(g.ServiceGroupID); sid != "" {
			groups[sid] = struct{}{}
		}
		src := strings.ToLower(strings.TrimSpace(g.Source))
		if src == "card" || src == "service_card" || src == "redeem" || src == "store" {
			out.HasCardGrant = true
		}
	}
	out.ServiceGroupCount = len(groups)
	return out
}

func mobileOfficialEntitled(llmAccess map[string]any) bool {
	if llmAccess == nil {
		return false
	}
	mode, _ := llmAccess["mode"].(string)
	status, _ := llmAccess["status"].(string)
	status = strings.ToLower(strings.TrimSpace(status))
	ready := status == "available" || status == "authorized" || status == "active" || status == "ready" || status == "configured"
	if !ready {
		return false
	}
	switch strings.TrimSpace(mode) {
	case "maclaw_official", "desktop_qr_third_party":
		return true
	default:
		return false
	}
}

func mobileAssistantModeForAccess(llmAccess map[string]any) string {
	if mobileOfficialEntitled(llmAccess) {
		return "official"
	}
	return "digital_twin"
}

// mobilePlanForAccess maps LLM access into a coarse commercial plan label for Mobile.
// free = no official path; official = MaClaw official credits; desktop_delegate = GUI QR keys.
func mobilePlanForAccess(llmAccess map[string]any) string {
	return mobilePlanForAccessWithGrant(llmAccess, mobileServiceGrantSnapshot{})
}

// mobilePlanForAccessWithGrant prefers service-card / active grant state when present.
func mobilePlanForAccessWithGrant(llmAccess map[string]any, grant mobileServiceGrantSnapshot) string {
	if llmAccess != nil {
		mode, _ := llmAccess["mode"].(string)
		if mobileOfficialEntitled(llmAccess) && strings.TrimSpace(mode) == "desktop_qr_third_party" {
			return "desktop_delegate"
		}
	}
	if grant.Active && (grant.HasCardGrant || grant.CreditsAvailable > 0) {
		return "service_card"
	}
	if mobileOfficialEntitled(llmAccess) {
		return "official"
	}
	if grant.Active {
		return "service_card"
	}
	return "free"
}

// mobileSharedEmployeesFromGrant enables tenant/public employee pools for paid paths.
// free / desktop_delegate stay own-only (design §3.2).
func mobileSharedEmployeesFromGrant(grant mobileServiceGrantSnapshot, plan string) bool {
	p := strings.ToLower(strings.TrimSpace(plan))
	if p == "service_card" || p == "paid" {
		return true
	}
	// Active card or spendable credits also unlock shared pools.
	if grant.Active && (grant.HasCardGrant || grant.CreditsAvailable > 0) {
		return true
	}
	return false
}

func mobileRequestBaseURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := "https"
	if r.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.Split(forwarded, ",")[0])
	} else if r.URL != nil && r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	host := strings.TrimSpace(r.Host)
	if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.Split(forwardedHost, ",")[0]
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if scheme != "https" && scheme != "http" {
		scheme = "https"
	}
	return scheme + "://" + host
}

func mobileLlmAccessPayload(ctx context.Context, principal *auth.ViewerPrincipal) map[string]any {
	if principal != nil {
		key := mobileLlmAuthorizationKey(principal.TenantID, principal.UserID)
		mobileLlmAuthorizations.Lock()
		record, ok := mobileLlmAuthorizations.authorizations[key]
		mobileLlmAuthorizations.Unlock()
		if !ok {
			record, ok = mobilePersistedLLMAuthorization(ctx, principal.TenantID, principal.UserID)
			if ok {
				mobileLlmAuthorizations.Lock()
				mobileLlmAuthorizations.authorizations[key] = record
				mobileLlmAuthorizations.Unlock()
			}
		}
		if ok {
			return map[string]any{
				"mode":             "desktop_qr_third_party",
				"status":           "available",
				"authorization_id": record.AuthorizationID,
				"authorized_by":    "maclaw-gui",
				"authorized_at":    record.AuthorizedAt.Format(time.RFC3339),
				"provider_name":    record.ProviderName,
				"provider_url":     record.ProviderURL,
				"model":            record.Model,
				"protocol":         record.Protocol,
			}
		}
	}
	return map[string]any{
		"mode":             "maclaw_official",
		"status":           "available",
		"authorization_id": "",
		"authorized_by":    "maclaw-official",
		"credits_account":  mobilePrincipalCreditsAccount(mobilePrincipalEmail(principal), mobilePrincipalUserID(principal)),
	}
}

func mobilePrincipalEmail(principal *auth.ViewerPrincipal) string {
	if principal == nil {
		return ""
	}
	return principal.Email
}

func mobilePrincipalUserID(principal *auth.ViewerPrincipal) string {
	if principal == nil {
		return ""
	}
	return principal.UserID
}

func mobilePrincipalPhoneNumber(account string) string {
	account = strings.TrimSpace(account)
	if strings.HasPrefix(strings.ToLower(account), "phone:") {
		return strings.TrimSpace(account[len("phone:"):])
	}
	return ""
}

func mobilePrincipalCreditsAccount(account, userID string) string {
	account = strings.TrimSpace(account)
	if account != "" {
		return account
	}
	return strings.TrimSpace(userID)
}

func mobileLlmAuthorizationKey(tenantID, userID string) string {
	return strings.TrimSpace(tenantID) + "\x00" + strings.TrimSpace(userID)
}

// MobileLLMDesktopQRSessionHandler creates an opaque, one-time QR payload for
// MaClaw GUI. The provider API key stays on the Hub; the QR only carries a
// random session id that a logged-in mobile viewer can redeem.
func MobileLLMDesktopQRSessionHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			Name     string   `json:"name"`
			URL      string   `json:"url"`
			Key      string   `json:"key"`
			Model    string   `json:"model"`
			Models   []string `json:"models"`
			Protocol string   `json:"protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		payload, err := normalizeMobileDesktopLlmProviderPayload(mobileDesktopLlmQRPayload{
			Type:     "maclaw_llm",
			Name:     req.Name,
			URL:      req.URL,
			Key:      req.Key,
			Model:    req.Model,
			Models:   req.Models,
			Protocol: req.Protocol,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_LLM_PROVIDER", err.Error())
			return
		}
		sessionID, err := newMobileLlmQRSessionID()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SESSION_CREATE_FAILED", "failed to create QR session")
			return
		}
		now := time.Now().UTC()
		expiresAt := now.Add(5 * time.Minute)
		record := mobileLlmQRSessionRecord{
			SessionID:    sessionID,
			OwnerID:      principal.UserID,
			TenantID:     principal.TenantID,
			Purpose:      mobileQRSessionPurposeLLM,
			ProviderName: payload.Name,
			ProviderURL:  payload.URL,
			APIKey:       payload.Key,
			Model:        payload.Model,
			Protocol:     payload.Protocol,
			CreatedAt:    now,
			ExpiresAt:    expiresAt,
		}
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.qrSessions[sessionID] = record
		mobileLlmAuthorizations.Unlock()

		qrPayloadBytes, _ := json.Marshal(mobileDesktopLlmQRPayload{
			Version:   2,
			Type:      mobileDesktopLlmAuthorizationQRType,
			SessionID: sessionID,
			HubURL:    mobileRequestBaseURL(r),
			ExpiresAt: expiresAt.Format(time.RFC3339),
			Name:      payload.Name,
			URL:       payload.URL,
			Model:     payload.Model,
			Protocol:  payload.Protocol,
		})
		writeJSON(w, http.StatusCreated, map[string]any{
			"status":     "created",
			"session_id": sessionID,
			"expires_at": expiresAt.Format(time.RFC3339),
			"qr_payload": string(qrPayloadBytes),
		})
	}
}

// MobileLLMDesktopQRAuthorizationHandler accepts the QR payload generated by
// MaClaw desktop GUI and records that this mobile viewer may use that delegated
// third-party LLM configuration through their discovered Hub.
func MobileLLMDesktopQRAuthorizationHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req struct {
			QRPayload string `json:"qr_payload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON")
			return
		}
		payload, err := parseMobileDesktopLlmQRPayload(req.QRPayload)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		if payload.Type != mobileDesktopLlmAuthorizationQRType {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", "desktop QR session is not an LLM authorization session")
			return
		}
		record, err := mobileLlmAuthorizationFromQR(principal, payload, time.Now().UTC())
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_DESKTOP_QR", err.Error())
			return
		}
		if err := persistMobileLLMAuthorization(r.Context(), record); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_AUTHORIZATION_STORE_FAILED", "failed to persist desktop LLM authorization")
			return
		}
		mobileLlmAuthorizations.Lock()
		mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(principal.TenantID, principal.UserID)] = record
		mobileLlmAuthorizations.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "authorized",
			"bootstrap": mobileBootstrapPayloadForRequest(principal, r, nil, nil),
		})
	}
}

// MobileLLMDesktopQRAuthorizationRevokeHandler removes the current viewer's
// delegated desktop GUI LLM authorization. The next bootstrap falls back to
// the phone account's MaClaw official LLM credits.
func MobileLLMDesktopQRAuthorizationRevokeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		if err := deletePersistedMobileLLMAuthorization(r.Context(), principal.TenantID, principal.UserID); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_AUTHORIZATION_DELETE_FAILED", "failed to revoke desktop LLM authorization")
			return
		}
		mobileLlmAuthorizations.Lock()
		delete(mobileLlmAuthorizations.authorizations, mobileLlmAuthorizationKey(principal.TenantID, principal.UserID))
		mobileLlmAuthorizations.Unlock()

		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "revoked",
			"bootstrap": mobileBootstrapPayloadForRequest(principal, r, nil, nil),
		})
	}
}

func parseMobileDesktopLlmQRPayload(raw string) (mobileDesktopLlmQRPayload, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload is required")
	}
	var payload mobileDesktopLlmQRPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload must be MaClaw desktop GUI JSON")
	}
	payload.Type = strings.TrimSpace(payload.Type)
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.ExpiresAt = strings.TrimSpace(payload.ExpiresAt)
	if payload.Type != mobileDesktopLlmAuthorizationQRType {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("qr_payload must be a MaClaw GUI LLM authorization session")
	}
	if payload.SessionID == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("session_id is required")
	}
	return payload, nil
}

func normalizeMobileDesktopLlmProviderPayload(payload mobileDesktopLlmQRPayload) (mobileDesktopLlmQRPayload, error) {
	payload.Name = strings.TrimSpace(payload.Name)
	payload.URL = strings.TrimSpace(payload.URL)
	payload.Key = strings.TrimSpace(payload.Key)
	payload.Model = strings.TrimSpace(payload.Model)
	payload.Protocol = strings.TrimSpace(payload.Protocol)
	if payload.Name == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider name is required")
	}
	if payload.Key == "" {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider key is required")
	}
	parsedURL, err := url.Parse(payload.URL)
	if err != nil || parsedURL.Host == "" || (parsedURL.Scheme != "https" && parsedURL.Scheme != "http") {
		return mobileDesktopLlmQRPayload{}, fmt.Errorf("provider url must be http or https")
	}
	if payload.Model == "" && len(payload.Models) > 0 {
		payload.Model = strings.TrimSpace(payload.Models[0])
	}
	if payload.Protocol == "" {
		payload.Protocol = "openai"
	}
	return payload, nil
}

func mobileLlmAuthorizationFromQR(principal *auth.ViewerPrincipal, payload mobileDesktopLlmQRPayload, now time.Time) (mobileLlmAuthorizationRecord, error) {
	return mobileLlmAuthorizationFromQRSession(principal, payload, now, false)
}

func mobileLlmAuthorizationFromQRSession(principal *auth.ViewerPrincipal, payload mobileDesktopLlmQRPayload, now time.Time, allowSessionAccount bool) (mobileLlmAuthorizationRecord, error) {
	tenantID := ""
	userID := ""
	if principal != nil {
		tenantID = principal.TenantID
		userID = principal.UserID
	}
	mobileLlmAuthorizations.Lock()
	session, ok := mobileLlmAuthorizations.qrSessions[payload.SessionID]
	if !ok {
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session was not found or has already been used")
	}
	if !session.ConsumedAt.IsZero() {
		delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session has already been used")
	}
	if now.After(session.ExpiresAt) {
		delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session has expired")
	}
	if allowSessionAccount {
		tenantID = session.TenantID
		userID = session.OwnerID
	} else if session.TenantID != tenantID || session.OwnerID != userID {
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session belongs to a different MaClaw account")
	}
	if session.Purpose != mobileQRSessionPurposeLLM {
		mobileLlmAuthorizations.Unlock()
		return mobileLlmAuthorizationRecord{}, fmt.Errorf("desktop QR session is not an LLM authorization session")
	}
	session.ConsumedAt = now
	delete(mobileLlmAuthorizations.qrSessions, payload.SessionID)
	mobileLlmAuthorizations.Unlock()
	sum := sha256.Sum256([]byte(strings.Join([]string{
		tenantID,
		userID,
		session.ProviderName,
		session.ProviderURL,
		session.Model,
		now.Format(time.RFC3339Nano),
	}, "\x00")))
	return mobileLlmAuthorizationRecord{
		AuthorizationID: "mllm_" + fmt.Sprintf("%x", sum[:8]),
		OwnerID:         userID,
		TenantID:        tenantID,
		ProviderName:    session.ProviderName,
		ProviderURL:     session.ProviderURL,
		APIKey:          session.APIKey,
		Model:           session.Model,
		Protocol:        session.Protocol,
		AuthorizedAt:    now,
	}, nil
}

func newMobileLlmQRSessionID() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "mlqr_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func mobileDocumentDraftPayload(record mobileDocumentDraftRecord) map[string]any {
	// Prefer display-safe body: never surface raw PDF binary / failed extract garbage.
	display := mobileDraftDisplayMarkdown(record)
	return mobileDocumentDraftPayloadWithMarkdown(record, display)
}

// mobileDocumentDraftPayloadWithMarkdown builds the wire payload using an already-resolved
// display body (avoids a second extract after heal).
func mobileDocumentDraftPayloadWithMarkdown(record mobileDocumentDraftRecord, markdown string) map[string]any {
	payload := map[string]any{
		"id":         record.ID,
		"title":      record.Title,
		"template":   record.Template,
		"markdown":   markdown,
		"updated_at": record.UpdatedAt.Format(time.RFC3339),
		"owner_id":   record.OwnerID,
	}
	if mobileDraftHasOriginal(record) {
		payload["has_original"] = true
		payload["source_filename"] = strings.TrimSpace(record.SourceFilename)
		payload["source_content_type"] = strings.TrimSpace(record.SourceContentType)
		payload["source_size"] = mobileDraftSourceSize(record)
		payload["source_download_url"] = "/api/mobile/documents/drafts/" + record.ID + "/source"
	} else {
		payload["has_original"] = false
	}
	return payload
}

// mobileAttachDraftOriginal stores the original file on a draft (disk + memory cache).
func mobileAttachDraftOriginal(draft *mobileDocumentDraftRecord, filename, contentType string, raw []byte) {
	if draft == nil || len(raw) == 0 {
		return
	}
	name := strings.TrimSpace(filepath.Base(filename))
	if name == "" {
		name = "upload"
	}
	draft.SourceFilename = name
	draft.SourceContentType = strings.TrimSpace(contentType)
	if draft.SourceContentType == "" {
		draft.SourceContentType = "application/octet-stream"
	}
	path, size, mem := mobilePersistDocumentOriginal(draft.OwnerID, "draft", draft.ID, raw)
	draft.SourcePath = path
	draft.SourceSize = size
	draft.SourceBytes = mem
}

// mobileDraftWorkingText returns text for AI/preview: prefer extracted/OCR markdown;
// for text-like originals fall back to raw UTF-8; otherwise a short original-file notice.
func mobileDraftWorkingText(draft mobileDocumentDraftRecord) string {
	return mobileDraftDisplayMarkdown(draft)
}

// mobileDraftDisplayMarkdown returns markdown safe for UI preview and AI context.
// When stored body looks like raw binary or failed PDF string-scrape garbage,
// re-extracts from the original or falls back to an original-file notice.
//
// Heavy PDF extract happens here (not under the documents lock). Callers that hold
// mobileDocuments.Lock must not call this; use mobileDraftListPreviewMarkdown instead.
func mobileDraftDisplayMarkdown(draft mobileDocumentDraftRecord) string {
	md := strings.TrimSpace(draft.Markdown)
	if md != "" && !mobileDraftRecordBodyUnreadable(draft, md) {
		return md
	}
	src := mobileDraftLoadSourceBytes(&draft)
	fname := mobileDraftFallbackFilename(draft)
	size := mobileDraftSourceSize(draft)
	if len(src) == 0 {
		if md != "" && mobileDraftRecordBodyUnreadable(draft, md) {
			return mobileDraftOriginalOnlyMarkdownSize(fname, size)
		}
		return md
	}
	// Extract from original on demand (docx/xlsx/pdf/text).
	if extracted, ok := mobileDraftMarkdownFromUpload(fname, src); ok && strings.TrimSpace(extracted) != "" {
		if !mobileDraftBodyLooksUnreadable(extracted) {
			return extracted
		}
	}
	// Only treat original bytes as UTF-8 text when the source is text-like.
	// Never string() a PDF/binary original (can be valid UTF-8 by chance on small files).
	if mobileDraftSourceLooksTextLike(draft, src) {
		text := strings.TrimSpace(string(src))
		if text != "" && !mobileDraftBodyLooksUnreadable(text) {
			return text
		}
	}
	if size <= 0 {
		size = len(src)
	}
	return mobileDraftOriginalOnlyMarkdownSize(fname, size)
}

// mobileDraftListPreviewMarkdown is a lock-safe, cheap display path for list rows.
// It never opens original blobs or runs PDF extract (those belong on GET-by-id heal).
func mobileDraftListPreviewMarkdown(draft mobileDocumentDraftRecord) string {
	md := strings.TrimSpace(draft.Markdown)
	if md == "" || !mobileDraftRecordBodyUnreadable(draft, md) {
		return md
	}
	return mobileDraftOriginalOnlyMarkdownSize(mobileDraftFallbackFilename(draft), mobileDraftSourceSize(draft))
}

func mobileDraftFallbackFilename(draft mobileDocumentDraftRecord) string {
	if name := strings.TrimSpace(draft.SourceFilename); name != "" {
		return name
	}
	if title := strings.TrimSpace(draft.Title); title != "" {
		return title
	}
	return "document"
}

func mobileDraftSourceIsPDF(draft mobileDocumentDraftRecord) bool {
	ct := strings.ToLower(strings.TrimSpace(draft.SourceContentType))
	if strings.Contains(ct, "pdf") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(draft.SourceFilename))
	return strings.HasSuffix(name, ".pdf")
}

func mobileDraftSourceLooksTextLike(draft mobileDocumentDraftRecord, raw []byte) bool {
	if mobileDraftSourceIsPDF(draft) {
		return false
	}
	ct := strings.ToLower(strings.TrimSpace(draft.SourceContentType))
	if strings.HasPrefix(ct, "image/") ||
		strings.Contains(ct, "officedocument") ||
		strings.Contains(ct, "msword") ||
		strings.Contains(ct, "spreadsheet") ||
		ct == "application/octet-stream" {
		// octet-stream still may be text — fall through to sniff.
		if ct != "application/octet-stream" && ct != "" {
			return false
		}
	}
	name := strings.ToLower(strings.TrimSpace(draft.SourceFilename))
	switch {
	case strings.HasSuffix(name, ".docx"), strings.HasSuffix(name, ".xlsx"),
		strings.HasSuffix(name, ".doc"), strings.HasSuffix(name, ".xls"),
		strings.HasSuffix(name, ".pptx"), strings.HasSuffix(name, ".png"),
		strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"),
		strings.HasSuffix(name, ".gif"), strings.HasSuffix(name, ".webp"),
		strings.HasSuffix(name, ".pdf"):
		return false
	}
	if len(raw) == 0 {
		return false
	}
	// Require full-buffer UTF-8 and no NULs in a prefix (binary reject).
	if !utf8.Valid(raw) {
		return false
	}
	limit := len(raw)
	if limit > 4096 {
		limit = 4096
	}
	for i := 0; i < limit; i++ {
		if raw[i] == 0 {
			return false
		}
	}
	return true
}

// mobileDraftHealMarkdownOutsideLock re-extracts a garbage body and returns the
// display text plus whether the stored markdown should be rewritten. Safe to call
// without holding mobileDocuments.Lock (may read blob from disk).
func mobileDraftHealMarkdownOutsideLock(draft mobileDocumentDraftRecord) (display string, shouldPersist bool) {
	stored := strings.TrimSpace(draft.Markdown)
	if stored == "" || !mobileDraftRecordBodyUnreadable(draft, stored) {
		return stored, false
	}
	display = mobileDraftDisplayMarkdown(draft)
	display = strings.TrimSpace(display)
	if display == "" || display == stored {
		return stored, false
	}
	return display, true
}

// mobileDraftApplyHealedMarkdown writes a previously computed display body.
// Caller must hold mobileDocuments.Lock.
func mobileDraftApplyHealedMarkdown(draft *mobileDocumentDraftRecord, display string) bool {
	if draft == nil {
		return false
	}
	display = strings.TrimSpace(display)
	if display == "" {
		return false
	}
	// Only replace while the stored body is still unreadable (avoid racing a good write).
	if !mobileDraftRecordBodyUnreadable(*draft, draft.Markdown) {
		return false
	}
	if display == strings.TrimSpace(draft.Markdown) {
		return false
	}
	draft.Markdown = display
	return true
}

func mobileDraftMarkdownLooksLikeRawBinary(text string) bool {
	if text == "" {
		return false
	}
	// ZIP/OOXML (docx/xlsx) or obvious binary garbage mistaken for text.
	if strings.HasPrefix(text, "PK") && (strings.Contains(text, "Content_Types") || strings.Contains(text, "[Content_Types]")) {
		return true
	}
	// Raw PDF file mistaken for text.
	if strings.HasPrefix(strings.TrimSpace(text), "%PDF-") {
		return true
	}
	nul := 0
	limit := len(text)
	if limit > 2048 {
		limit = 2048
	}
	for i := 0; i < limit; i++ {
		if text[i] == 0 {
			nul++
		}
	}
	return nul > 0
}

// mobileDraftBodyLooksUnreadable reports bodies unsafe for UI preview: raw binary
// or failed PDF literal-string scrapes (short symbol-heavy lines).
// Text-only helper for extract results / unit tests (no source metadata).
func mobileDraftBodyLooksUnreadable(text string) bool {
	if mobileDraftMarkdownLooksLikeRawBinary(text) {
		return true
	}
	return mobileDraftMarkdownLooksLikePDFGarbage(text)
}

// mobileDraftRecordBodyUnreadable uses source metadata so non-PDF drafts
// (code, logs) are not treated as PDF scrape garbage.
func mobileDraftRecordBodyUnreadable(draft mobileDocumentDraftRecord, text string) bool {
	if mobileDraftMarkdownLooksLikeRawBinary(text) {
		return true
	}
	// PDF scrape heuristic only for PDF originals (or unknown source — list rows
	// with garbage from older uploads may lack content-type).
	if mobileDraftSourceIsPDF(draft) || (!mobileDraftHasOriginal(draft) && strings.TrimSpace(draft.SourceFilename) == "") {
		return mobileDraftMarkdownLooksLikePDFGarbage(text)
	}
	// Non-PDF original: only replace when the body is extremely scrape-like
	// (almost no letters) — avoids flagging code/hex dumps with short lines.
	return mobileDraftMarkdownLooksLikeStrongScrapeOnly(text)
}

// mobileDraftMarkdownLooksLikeStrongScrapeOnly is a stricter bar used for non-PDF
// originals so symbol-heavy but letter-dense content (source code) is kept.
func mobileDraftMarkdownLooksLikeStrongScrapeOnly(text string) bool {
	if !mobileDraftMarkdownLooksLikePDFGarbage(text) {
		return false
	}
	body := mobileDraftBodyAfterTitle(text)
	letters, _, _, other, nonSpace := mobileCountTextClasses(body)
	if nonSpace == 0 {
		return false
	}
	return float64(letters)/float64(nonSpace) < 0.25 && float64(other)/float64(nonSpace) > 0.35
}

func mobileDraftBodyAfterTitle(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "#") {
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			return strings.TrimSpace(text[i+1:])
		}
		return ""
	}
	return text
}

// mobileDraftMarkdownLooksLikePDFGarbage detects naive PDF string-scrape output
// (many ultra-short / symbol-heavy lines). Must not treat short real drafts as garbage.
func mobileDraftMarkdownLooksLikePDFGarbage(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	// Cap work on huge accidental binary-as-text bodies.
	if len(text) > 16<<10 {
		text = text[:16<<10]
	}
	// Keep intentional original-only / OCR placeholders readable.
	// Match whole-word-ish markers used by our templates (not arbitrary "OCR" in papers).
	if strings.Contains(text, "原始文件已保存") || strings.Contains(text, "原件已保存") ||
		strings.Contains(text, "可预览元数据或分享原件") ||
		strings.Contains(text, "待识别内容") ||
		strings.Contains(strings.ToLower(text), "original file") {
		return false
	}
	body := mobileDraftBodyAfterTitle(text)
	if body == "" {
		return false
	}

	var lines []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	// Short real notes ("OK", "收到") are not scrape noise — need several crumb lines.
	if len(lines) < 5 {
		return mobileIsSymbolDominatedScrape(body, lines)
	}

	goodLines := 0
	garbageLines := 0
	for _, line := range lines {
		if mobilePDFLineLooksLikeProse(line) {
			goodLines++
		} else {
			garbageLines++
		}
	}
	letters, _, _, other, nonSpace := mobileCountTextClasses(body)
	if goodLines == 0 {
		// Chinese short bullets ("- 买") have few long lines but high letter density.
		// PDF scrapes are symbol-heavy with almost no letters.
		if nonSpace > 0 && float64(letters)/float64(nonSpace) >= 0.45 && float64(other)/float64(nonSpace) <= 0.4 {
			return false
		}
		return true
	}
	// Typical bad scrape: dozens of 1–3 char symbol lines and almost no prose.
	return garbageLines >= goodLines*3 && goodLines < 6
}

// mobilePDFLineLooksLikeProse reports a line that is unlikely to be PDF operator noise.
// CJK single-character lines count as prose; Latin needs a few letters.
func mobilePDFLineLooksLikeProse(line string) bool {
	runes := []rune(strings.TrimSpace(line))
	if len(runes) == 0 {
		return false
	}
	lc := 0
	cjk := 0
	for _, r := range runes {
		if unicode.IsLetter(r) {
			lc++
			if mobileRuneIsCJK(r) {
				cjk++
			}
		}
	}
	if cjk > 0 && lc >= 1 {
		return true
	}
	if len(runes) <= 3 {
		return false
	}
	return lc >= 4 && float64(lc) >= float64(len(runes))*0.35
}

func mobileRuneIsCJK(r rune) bool {
	// CJK Unified Ideographs + common extensions / kana / hangul syllables.
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

// mobileIsSymbolDominatedScrape flags short multi-crumb binary leftovers without
// treating a normal one-line note as unreadable.
func mobileIsSymbolDominatedScrape(body string, lines []string) bool {
	if len(lines) < 3 {
		return false
	}
	letters, _, _, other, nonSpace := mobileCountTextClasses(body)
	if nonSpace == 0 {
		return false
	}
	// Real short notes have mostly letters; scrape crumbs are symbol-heavy.
	if letters >= 20 && float64(letters)/float64(nonSpace) >= 0.5 {
		return false
	}
	short := 0
	for _, line := range lines {
		if len([]rune(line)) <= 4 {
			short++
		}
	}
	if short < 3 {
		return false
	}
	return float64(other)/float64(nonSpace) > 0.3 || letters < 12
}

func mobileCountTextClasses(text string) (letters, spaces, digits, other, nonSpace int) {
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			letters++
		case unicode.IsSpace(r):
			spaces++
		case unicode.IsDigit(r):
			digits++
		default:
			other++
		}
	}
	nonSpace = letters + digits + other
	return
}

func mobileDocumentExportPayload(record mobileDocumentExportRecord) map[string]any {
	downloadURL := ""
	if record.Status == "ready" {
		downloadURL = "/api/mobile/documents/export/" + record.JobID + "/download"
	}
	return map[string]any{
		"job_id":       record.JobID,
		"draft_id":     record.DraftID,
		"format":       record.Format,
		"status":       record.Status,
		"download_url": downloadURL,
		"message":      strings.TrimSpace(record.Message),
		"created_at":   record.CreatedAt.Format(time.RFC3339),
	}
}

func mobileDocumentUploadPayload(record mobileDocumentUploadRecord) map[string]any {
	payload, _ := mobileDocumentUploadPayloadTracked(record)
	return payload
}

// mobileDocumentUploadPayloadTracked is like mobileDocumentUploadPayload but reports
// whether upload/draft meta was repaired (caller should persist when true).
// Caller should hold mobileDocuments.Lock.
func mobileDocumentUploadPayloadTracked(record mobileDocumentUploadRecord) (map[string]any, bool) {
	repaired := false
	// Repair ghost upload SourcePath before advertising source_download_url.
	if mobileUploadRepairSourceMeta(&record) {
		repaired = true
		if record.TaskID != "" {
			mobileDocuments.uploads[record.TaskID] = record
		}
	}
	payload := map[string]any{
		"task_id":      record.TaskID,
		"filename":     record.Filename,
		"content_type": record.ContentType,
		"status":       record.Status,
		"draft_id":     record.DraftID,
		"message":      record.Message,
		"claimed_by":   record.ClaimedBy,
		"uploaded_at":  record.UploadedAt.Format(time.RFC3339),
		"updated_at":   record.UpdatedAt.Format(time.RFC3339),
		"owner_id":     record.OwnerID,
	}
	draftHasOriginal := false
	if record.DraftID != "" {
		if draft, ok := mobileDocuments.drafts[record.DraftID]; ok {
			// Single repair pass — avoid re-statting the same draft for source availability.
			if mobileDraftRepairSourceMeta(&draft) {
				mobileDocuments.drafts[record.DraftID] = draft
				repaired = true
			}
			draftHasOriginal = mobileDraftHasOriginal(draft)
			payload["draft"] = mobileDocumentDraftPayload(draft)
		}
	}
	// Source URL when upload still holds bytes/path, or draft can serve as fallback
	// (worker always hits /upload/{id}/source; handler may stream draft original).
	if record.TaskID != "" && (mobileUploadHasSource(record) || draftHasOriginal) {
		payload["source_download_url"] = "/api/mobile/documents/upload/" + record.TaskID + "/source"
	}
	return payload, repaired
}

// mobileUploadSourceAvailable reports whether a worker can download original bytes
// for this upload task (upload blob and/or linked draft original).
// repaired is true when upload/draft meta was cleaned (caller should persist and
// re-read the upload record from the map before mutating it further).
// Caller should hold mobileDocuments.Lock.
func mobileUploadSourceAvailable(record mobileDocumentUploadRecord) (ok bool, repaired bool) {
	// Ghost SourceSize must not make a task claimable — repair first.
	if mobileUploadRepairSourceMeta(&record) {
		repaired = true
		if record.TaskID != "" {
			mobileDocuments.uploads[record.TaskID] = record
		}
	}
	if mobileUploadHasSource(record) {
		return true, repaired
	}
	draft, draftRepaired := mobileUploadDraftOriginal(record)
	return draft != nil, repaired || draftRepaired
}

// mobileUploadDraftOriginal returns the linked draft when it owns an original.
// repaired is true when draft meta was mutated (caller should persist).
// Caller must hold mobileDocuments.Lock.
func mobileUploadDraftOriginal(record mobileDocumentUploadRecord) (draftPtr *mobileDocumentDraftRecord, repaired bool) {
	draftID := strings.TrimSpace(record.DraftID)
	if draftID == "" {
		return nil, false
	}
	draft, ok := mobileDocuments.drafts[draftID]
	if !ok || draft.OwnerID != record.OwnerID {
		return nil, false
	}
	if mobileDraftRepairSourceMeta(&draft) {
		mobileDocuments.drafts[draftID] = draft
		repaired = true
	}
	if !mobileDraftHasOriginal(draft) {
		return nil, repaired
	}
	out := draft
	return &out, repaired
}

func mobileApplyUploadPipelineResult(record mobileDocumentUploadRecord, now time.Time) (mobileDocumentUploadRecord, bool) {
	if record.Status != "needs_ocr" {
		return record, false
	}
	if strings.TrimSpace(record.OCRError) != "" {
		record.Status = "failed"
		record.Message = strings.TrimSpace(record.OCRError)
		record.UpdatedAt = now
		// If draft already holds the original, free the upload-side copy.
		mobileReleaseUploadOriginalWhenDraftOwns(&record)
		return record, true
	}
	ocrMarkdown := strings.TrimSpace(record.OCRMarkdown)
	if ocrMarkdown == "" || record.DraftID == "" {
		return record, false
	}
	draft, ok := mobileDocuments.drafts[record.DraftID]
	if !ok || draft.OwnerID != record.OwnerID {
		return record, false
	}
	// Drop ghost original meta before re-attach / release decisions.
	_ = mobileDraftRepairSourceMeta(&draft)
	draft.Markdown = ocrMarkdown
	draft.UpdatedAt = now
	// Ensure original survives OCR promotion.
	if !mobileDraftHasOriginal(draft) {
		if src := mobileUploadLoadSourceBytes(&record); len(src) > 0 {
			mobileAttachDraftOriginal(&draft, record.Filename, record.ContentType, src)
		}
	}
	mobileDocuments.drafts[draft.ID] = draft
	record.Status = "ready"
	record.Message = strings.TrimSpace(record.OCRMessage)
	if record.Message == "" {
		record.Message = "OCR/视觉识别已完成，已更新移动端文档草稿（原件仍可分享）。"
	}
	record.UpdatedAt = now
	// Draft owns the original; free upload-side blob/RAM.
	mobileReleaseUploadOriginalAfterReady(&record)
	// Best-effort knowledge index for OCR-ready drafts (owner known on record).
	go mobileIngestDocumentDraft(&auth.ViewerPrincipal{
		UserID:   draft.OwnerID,
		TenantID: "", // filled by store owner path; knowledge uses user_id primarily
	}, draft)
	return record, true
}

type mobileSearchRequest struct {
	Query   string   `json:"query"`
	Context []string `json:"context,omitempty"`
	// Messages is the preferred multi-turn payload (role/content). When present
	// it is sent to the LLM as chat messages; Context remains a legacy fallback.
	Messages []mobileChatMessage `json:"messages,omitempty"`
	// DocumentID binds a mobile draft as the assistant's working object (sync path).
	// Hub injects the owned draft Markdown into the agent system context.
	DocumentID string `json:"document_id,omitempty"`
	// Async enqueues a long-running assistant job and returns 202 immediately
	// (design: short SSE / long → 后台 job). Stream is ignored when Async is true.
	Async bool `json:"async,omitempty"`
	// Stream enables progressive SSE delivery (meta → delta* → done).
	// When possible the Hub forwards upstream token deltas; otherwise the
	// completed answer is chunked for typewriter UX.
	Stream bool `json:"stream,omitempty"`
}

type mobileChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// MobileSearchHandler keeps mobile citations while routing the actual official
// model call through the existing Hub LLM endpoint. That preserves the normal
// model authorization, credits, request ID, and usage-record path.
func MobileSearchHandler(identity *auth.IdentityService, llmHandlers ...http.Handler) http.HandlerFunc {
	var officialLLM http.Handler
	if len(llmHandlers) > 0 {
		officialLLM = llmHandlers[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req mobileSearchRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		query := strings.TrimSpace(req.Query)
		if query == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "query is required")
			return
		}

		// Long path: enqueue background assistant job (appears in /api/mobile/jobs).
		if req.Async {
			job, err := mobileEnqueueAgentJobFromSearch(r, principal, officialLLM, req)
			if err != nil {
				msg := err.Error()
				switch {
				case strings.Contains(msg, "document"):
					writeError(w, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "bound document not found or not owned by viewer")
				case strings.Contains(msg, "limit"):
					writeError(w, http.StatusTooManyRequests, "JOB_LIMIT", "too many active assistant jobs")
				default:
					writeError(w, http.StatusBadRequest, "INVALID_INPUT", msg)
				}
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":    "accepted",
				"async":     true,
				"job_id":    job.JobID,
				"job":       mobileAgentJobPayload(job),
				"query":     query,
				"message":   "assistant job queued; track via GET /api/mobile/agent/jobs/{job_id} or /api/mobile/jobs",
				"deep_link": "/tasks",
				"tenant_id": principal.TenantID,
				"user_id":   principal.UserID,
			})
			return
		}

		results, err := mobileWebSearch(r.Context(), query, 5)
		if err != nil {
			writeError(w, http.StatusBadGateway, "SEARCH_FAILED", "mobile search failed")
			return
		}
		links := mobileExtractQueryLinks(query)
		citations := mobileMergeLinkCitations(mobileSearchCitations(results), links)
		chatMessages := mobileBuildLLMMessages(query, citations, req.Messages, req.Context)
		boundDocID := ""
		boundDocTitle := ""
		if docID := strings.TrimSpace(req.DocumentID); docID != "" {
			draft, ok := mobileLookupOwnedDraft(principal, docID)
			if !ok {
				writeError(w, http.StatusNotFound, "DOCUMENT_NOT_FOUND", "bound document not found or not owned by viewer")
				return
			}
			chatMessages = mobileInjectBoundDocument(chatMessages, draft)
			boundDocID = draft.ID
			boundDocTitle = draft.Title
		}
		answer := mobileSearchAnswer(query, results, links)
		requestID := ""
		mode := "maclaw_official"

		// Resolve LLM backend once (stream and non-stream share the same choice).
		delegated, useDelegated := mobileThirdPartyLLMAuthorization(r.Context(), principal.TenantID, principal.UserID)
		hasLLM := useDelegated || officialLLM != nil
		if useDelegated {
			mode = "desktop_qr_third_party"
		}

		if req.Stream && hasLLM {
			// Tool-capable agent loop with progressive SSE (tool_call / tool_result / delta / done).
			if err := mobileStreamAgentSearchAnswer(r, w, principal, query, citations, officialLLM, delegated, useDelegated, chatMessages); err != nil {
				mobileWriteSearchSSEError(w, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
			}
			return
		}

		if hasLLM {
			answer, requestID, err = mobileRunAgentLoop(r.Context(), r, principal, officialLLM, delegated, useDelegated, chatMessages, nil)
		}
		if err != nil {
			if req.Stream {
				mobileWriteSearchSSEError(w, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
				return
			}
			writeError(w, http.StatusBadGateway, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
			return
		}
		usageRecordID := ""
		if mode == "maclaw_official" && strings.TrimSpace(requestID) != "" {
			// The official Hub usage buffer and access log correlate credits to
			// this request ID; expose only the read-only trace reference.
			usageRecordID = requestID
		}
		payload := map[string]any{
			"answer": answer, "citations": citations, "query": query,
			"tenant_id": principal.TenantID, "user_id": principal.UserID,
			"llm_mode": mode, "llm_request_id": requestID,
			"llm_usage_record_id": usageRecordID, "status": "ready",
			"agent": hasLLM,
		}
		if boundDocID != "" {
			payload["document_id"] = boundDocID
			if boundDocTitle != "" {
				payload["document_title"] = boundDocTitle
			}
		}
		if req.Stream {
			mobileWriteSearchSSE(w, payload)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

const (
	mobileLLMMaxHistoryTurns   = 16
	mobileLLMMaxTurnRunes      = 4000
	mobileLLMStreamPendingMax  = 1 << 20 // 1 MiB incomplete SSE line budget
	mobileLLMStreamJSONBodyMax = 2 << 20 // 2 MiB non-stream / error body cap
)

func mobileClipRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}

func mobileBuildLLMMessages(query string, citations []map[string]string, history []mobileChatMessage, legacyContext []string) []map[string]string {
	messages := make([]map[string]string, 0, mobileLLMMaxHistoryTurns+3)
	messages = append(messages, map[string]string{
		"role":    "system",
		"content": mobileSearchSystemPrompt(),
	})

	// Prefer structured multi-turn messages from the client.
	turns := make([]map[string]string, 0, len(history))
	if len(history) > 0 {
		for _, item := range history {
			role := strings.ToLower(strings.TrimSpace(item.Role))
			content := mobileClipRunes(item.Content, mobileLLMMaxTurnRunes)
			if content == "" {
				continue
			}
			switch role {
			case "user", "assistant", "system":
			default:
				role = "user"
			}
			turns = append(turns, map[string]string{"role": role, "content": content})
		}
		// Drop only a *trailing* user turn that duplicates the current query
		// (client often includes it; older identical questions stay).
		query = strings.TrimSpace(query)
		if n := len(turns); n > 0 && turns[n-1]["role"] == "user" && turns[n-1]["content"] == query {
			turns = turns[:n-1]
		}
	} else {
		// Legacy context lines ("user: ..." / "assistant: ...") when messages is empty.
		for _, item := range legacyContext {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			role := "user"
			content := item
			if strings.HasPrefix(item, "user:") {
				content = strings.TrimSpace(strings.TrimPrefix(item, "user:"))
				role = "user"
			} else if strings.HasPrefix(item, "assistant:") {
				content = strings.TrimSpace(strings.TrimPrefix(item, "assistant:"))
				role = "assistant"
			} else if strings.HasPrefix(item, "system:") {
				content = strings.TrimSpace(strings.TrimPrefix(item, "system:"))
				role = "system"
			}
			content = mobileClipRunes(content, mobileLLMMaxTurnRunes)
			if content == "" {
				continue
			}
			turns = append(turns, map[string]string{"role": role, "content": content})
		}
	}
	if len(turns) > mobileLLMMaxHistoryTurns {
		turns = turns[len(turns)-mobileLLMMaxHistoryTurns:]
	}
	messages = append(messages, turns...)

	// Final user turn: current question + evidence sources.
	var user strings.Builder
	user.WriteString(strings.TrimSpace(query))
	if len(citations) > 0 {
		user.WriteString("\n\nEvidence sources (use for synthesis only):\n")
		for index, citation := range citations {
			title := mobileCleanSearchText(citation["title"], 120)
			url := strings.TrimSpace(citation["url"])
			snippet := mobileCleanSearchText(citation["snippet"], 220)
			user.WriteString(fmt.Sprintf("\n[%d] %s\n%s\n%s", index+1, title, url, snippet))
		}
	}
	messages = append(messages, map[string]string{
		"role":    "user",
		"content": user.String(),
	})
	return messages
}

func mobileSearchSystemPrompt() string {
	return `You are MaClaw Mobile, a professional work companion (similar to MaClaw desktop AI assistant).
Synthesize an answer for a busy human — do NOT dump raw search snippets.

Output in Chinese when the user writes Chinese. Use clean Markdown:
1) 结论 — 2 to 4 sentences that directly answer the question
2) 要点 or a Markdown 表格/table when comparing facts (weather, status, options)
3) 注意/风险 — only if relevant
4) Cite sources only as [1][2] footnote numbers; never paste long webpage text
Never invent citations. Never emit HTML tags or HTML entities.`
}

const mobileBoundDocumentMaxRunes = 12000

// mobileLookupOwnedDraft returns a draft only when it belongs to the viewer.
func mobileLookupOwnedDraft(principal *auth.ViewerPrincipal, documentID string) (mobileDocumentDraftRecord, bool) {
	id := strings.TrimSpace(documentID)
	if id == "" || principal == nil {
		return mobileDocumentDraftRecord{}, false
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		return mobileDocumentDraftRecord{}, false
	}
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	draft, ok := mobileDocuments.drafts[id]
	if !ok || draft.OwnerID != ownerID {
		return mobileDocumentDraftRecord{}, false
	}
	return draft, true
}

// mobileInjectBoundDocument inserts a system message with the bound draft body
// so the full agent can treat the document as its working object (sync path).
// When an original file is attached, that file is the source of truth; extracted
// text is provided only as a convenience for models that cannot read binaries.
func mobileInjectBoundDocument(messages []map[string]string, draft mobileDocumentDraftRecord) []map[string]string {
	body := mobileClipRunes(mobileDraftWorkingText(draft), mobileBoundDocumentMaxRunes)
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = draft.ID
	}
	hasOriginal := mobileDraftHasOriginal(draft)
	sourceName := strings.TrimSpace(draft.SourceFilename)
	sourceType := strings.TrimSpace(draft.SourceContentType)
	sourceSize := mobileDraftSourceSize(draft)
	sys := fmt.Sprintf(
		`Bound mobile document object:
- document_id: %s
- title: %s
- updated_at: %s
- has_original: %v
- source_filename: %s
- source_content_type: %s
- source_size_bytes: %d

The user's working object is the ORIGINAL uploaded file when has_original is true.
Treat the original file as the source of truth for content, layout intent, and sharing.
Text below is extracted/OCR convenience for analysis — do not claim the environment lacks OCR/vision if extracted text is present.
Prefer concise, actionable Markdown in replies.
When proposing a full replacement of the draft body, wrap the new Markdown in a fenced code block labeled maclaw-document-edit (language tag maclaw-document-edit).
Do not claim the document was already saved — the Mobile client applies edits via Hub PATCH using document_id.

--- Document content (from original / extract) start ---
%s
--- Document content end ---`,
		draft.ID,
		title,
		draft.UpdatedAt.UTC().Format(time.RFC3339),
		hasOriginal,
		sourceName,
		sourceType,
		sourceSize,
		body,
	)
	if len(messages) == 0 {
		return []map[string]string{{"role": "system", "content": sys}}
	}
	// Keep the primary system prompt first; insert bound-doc context next.
	out := make([]map[string]string, 0, len(messages)+1)
	out = append(out, messages[0])
	out = append(out, map[string]string{"role": "system", "content": sys})
	if len(messages) > 1 {
		out = append(out, messages[1:]...)
	}
	return out
}

// mobileExtractDocumentEditFence pulls a proposed draft rewrite from assistant text.
// Used by clients/tests; Hub does not auto-apply edits without an explicit API call.
func mobileExtractDocumentEditFence(answer string) (string, bool) {
	const open = "```maclaw-document-edit"
	idx := strings.Index(answer, open)
	if idx < 0 {
		// also accept language tag with newline after
		open2 := "```maclaw-document-edit\n"
		idx = strings.Index(answer, open2)
		if idx < 0 {
			return "", false
		}
	}
	rest := answer[idx+len(open):]
	rest = strings.TrimPrefix(rest, "\n")
	rest = strings.TrimPrefix(rest, "\r\n")
	end := strings.Index(rest, "```")
	if end < 0 {
		return "", false
	}
	body := strings.TrimSpace(rest[:end])
	if body == "" {
		return "", false
	}
	return body, true
}

func mobileWriteSearchSSEError(w http.ResponseWriter, code, message string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusBadGateway, code, message)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = mobileWriteSSEEvent(w, "error", map[string]any{
		"code":    code,
		"message": message,
	})
	flusher.Flush()
}

func mobileWriteSearchSSE(w http.ResponseWriter, payload map[string]any) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusOK, payload)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	answer, _ := payload["answer"].(string)
	meta := map[string]any{}
	for k, v := range payload {
		if k == "answer" {
			continue
		}
		meta[k] = v
	}
	meta["status"] = "streaming"
	_ = mobileWriteSSEEvent(w, "meta", meta)
	flusher.Flush()

	mobileWriteAnswerDeltas(w, flusher, answer)
	payload["status"] = "ready"
	_ = mobileWriteSSEEvent(w, "done", payload)
	flusher.Flush()
}

func mobileWriteSSEEvent(w http.ResponseWriter, event string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", raw); err != nil {
		return err
	}
	return nil
}

func mobileChunkAnswerForSSE(answer string, maxRunes int) []string {
	text := strings.TrimSpace(answer)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = 48
	}
	runes := []rune(text)
	chunks := make([]string, 0, (len(runes)/maxRunes)+1)
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		// Prefer breaking near punctuation/space for smoother UI.
		if n < len(runes) {
			window := runes[:n]
			for i := len(window) - 1; i > n/2; i-- {
				switch window[i] {
				case ' ', '\n', '。', '！', '？', '；', '，', '.', '!', '?', ';', ',':
					n = i + 1
					i = 0
				}
			}
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}

func mobileSearchPrompt(query string, citations []map[string]string, context []string) string {
	var b strings.Builder
	b.WriteString(`You are MaClaw Mobile, a professional work companion (similar to MaClaw desktop AI assistant).
Synthesize an answer for a busy human — do NOT dump raw search snippets.

Output in Chinese when the user writes Chinese. Use clean Markdown:
1) 结论 — 2 to 4 sentences that directly answer the question
2) 要点 or a Markdown 表格/table when comparing facts (weather, status, options)
3) 注意/风险 — only if relevant
4) Cite sources only as [1][2] footnote numbers; never paste long webpage text
Never invent citations. Never emit HTML tags or HTML entities.

Question:
`)
	b.WriteString(strings.TrimSpace(query))
	for _, item := range context {
		if item = strings.TrimSpace(item); item != "" {
			b.WriteString("\n\nAdditional conversation context:\n")
			b.WriteString(item)
		}
	}
	if len(citations) > 0 {
		b.WriteString("\n\nEvidence sources (use for synthesis only):\n")
	}
	for index, citation := range citations {
		title := mobileCleanSearchText(citation["title"], 120)
		url := strings.TrimSpace(citation["url"])
		snippet := mobileCleanSearchText(citation["snippet"], 220)
		b.WriteString(fmt.Sprintf("\n[%d] %s\n%s\n%s", index+1, title, url, snippet))
	}
	return b.String()
}

// mobileCleanSearchText unescapes common HTML entities, strips simple tags,
// collapses whitespace, and optionally truncates for prompts/UI-safe snippets.
func mobileCleanSearchText(input string, maxLen int) string {
	text := strings.TrimSpace(input)
	if text == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"&nbsp;", " ",
		"&ensp;", " ",
		"&emsp;", " ",
		"&thinsp;", " ",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&apos;", "'",
		"&middot;", "·",
		"&bull;", "•",
		"&hellip;", "…",
		"&mdash;", "—",
		"&ndash;", "–",
	)
	text = replacer.Replace(text)
	// Numeric entities &#183; &#0183; &#xB7;
	text = mobileNumericEntityPattern.ReplaceAllStringFunc(text, func(match string) string {
		inner := match
		if strings.HasPrefix(inner, "&#x") || strings.HasPrefix(inner, "&#X") {
			hex := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(inner, "&#x"), "&#X"), ";")
			if n, err := strconv.ParseInt(hex, 16, 32); err == nil && n >= 0 && n <= 0x10FFFF {
				return string(rune(n))
			}
			return " "
		}
		num := strings.TrimSuffix(strings.TrimPrefix(inner, "&#"), ";")
		num = strings.TrimLeft(num, "0")
		if num == "" {
			num = "0"
		}
		if n, err := strconv.Atoi(num); err == nil && n >= 0 && n <= 0x10FFFF {
			return string(rune(n))
		}
		return " "
	})
	text = mobileHTMLTagPattern.ReplaceAllString(text, " ")
	text = strings.Join(strings.Fields(text), " ")
	if maxLen > 0 && len([]rune(text)) > maxLen {
		runes := []rune(text)
		text = string(runes[:maxLen]) + "…"
	}
	return text
}

var (
	mobileNumericEntityPattern = regexp.MustCompile(`&#x[0-9a-fA-F]{1,6};|&#0*\d{1,7};`)
	mobileHTMLTagPattern       = regexp.MustCompile(`<[^>]*>`)
)

func mobileSearchBasePayload(principal *auth.ViewerPrincipal, query string, citations []map[string]string, mode, requestID string) map[string]any {
	usageRecordID := ""
	if mode == "maclaw_official" && strings.TrimSpace(requestID) != "" {
		usageRecordID = requestID
	}
	return map[string]any{
		"citations":           citations,
		"query":               query,
		"tenant_id":           principal.TenantID,
		"user_id":             principal.UserID,
		"llm_mode":            mode,
		"llm_request_id":      requestID,
		"llm_usage_record_id": usageRecordID,
		"status":              "streaming",
	}
}

func mobileBeginSearchSSE(w http.ResponseWriter, meta map[string]any) (http.Flusher, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support streaming")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := mobileWriteSSEEvent(w, "meta", meta); err != nil {
		return nil, err
	}
	flusher.Flush()
	return flusher, nil
}

func mobileFinishSearchSSE(w http.ResponseWriter, flusher http.Flusher, payload map[string]any) {
	payload["status"] = "ready"
	_ = mobileWriteSSEEvent(w, "done", payload)
	if flusher != nil {
		flusher.Flush()
	}
}

// mobileEmitSSEClientError writes an error event after SSE headers are already open.
func mobileEmitSSEClientError(w http.ResponseWriter, flusher http.Flusher, code, message string) {
	_ = mobileWriteSSEEvent(w, "error", map[string]any{
		"code":    code,
		"message": message,
	})
	if flusher != nil {
		flusher.Flush()
	}
}

// mobileStreamAgentSearchAnswer runs the Hub-side tool agent and streams SSE:
// meta → (tool_call|tool_result)* → delta* → done.
func mobileStreamAgentSearchAnswer(
	r *http.Request,
	w http.ResponseWriter,
	principal *auth.ViewerPrincipal,
	query string,
	citations []map[string]string,
	officialLLM http.Handler,
	delegated mobileLlmAuthorizationRecord,
	useDelegated bool,
	messages []map[string]string,
) error {
	mode := "maclaw_official"
	if useDelegated {
		mode = "desktop_qr_third_party"
	}
	meta := mobileSearchBasePayload(principal, query, citations, mode, "")
	meta["agent"] = true
	// Full corelib agentservice surface when available; legacy is web_* only.
	meta["agent_runtime"] = "corelib_agentservice"
	meta["tools"] = []string{"web_search", "web_fetch", "skills", "mcp", "memory", "files"}
	flusher, err := mobileBeginSearchSSE(w, meta)
	if err != nil {
		// No flusher: non-stream agent then chunk.
		answer, requestID, callErr := mobileRunAgentLoop(r.Context(), r, principal, officialLLM, delegated, useDelegated, messages, nil)
		if callErr != nil {
			return callErr
		}
		payload := mobileSearchBasePayload(principal, query, citations, mode, requestID)
		payload["answer"] = answer
		payload["agent"] = true
		payload["status"] = "ready"
		mobileWriteSearchSSE(w, payload)
		return nil
	}

	toolEvents := make([]map[string]any, 0, 8)
	streamedTokens := false
	var emit mobileAgentEventWriter = func(event string, data map[string]any) {
		switch event {
		case "delta":
			streamedTokens = true
		case "tool_call":
			toolEvents = append(toolEvents, map[string]any{
				"kind": "call", "id": data["id"], "name": data["name"], "detail": data["arguments"],
			})
		case "tool_result":
			toolEvents = append(toolEvents, map[string]any{
				"kind": "result", "id": data["id"], "name": data["name"], "detail": data["result"],
			})
		}
		_ = mobileWriteSSEEvent(w, event, data)
		if flusher != nil {
			flusher.Flush()
		}
	}
	answer, requestID, err := mobileRunAgentLoop(r.Context(), r, principal, officialLLM, delegated, useDelegated, messages, emit)
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	if strings.TrimSpace(answer) == "" {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	// Core agent already streams via OnToken; avoid replaying the full answer.
	if !streamedTokens {
		mobileWriteAnswerDeltas(w, flusher, answer)
	}
	payload := mobileSearchBasePayload(principal, query, citations, mode, requestID)
	payload["answer"] = answer
	payload["agent"] = true
	if len(toolEvents) > 0 {
		payload["tool_events"] = toolEvents
	}
	mobileFinishSearchSSE(w, flusher, payload)
	return nil
}

// mobileStreamOfficialSearchAnswer streams mobile SSE while calling the official
// Hub LLM. When the upstream handler emits OpenAI chat-completion SSE, token
// deltas are forwarded live; non-stream JSON responses are chunked for UX.
// Deprecated for the main search path in favor of mobileStreamAgentSearchAnswer;
// retained for targeted tests/callers.
func mobileStreamOfficialSearchAnswer(r *http.Request, w http.ResponseWriter, principal *auth.ViewerPrincipal, query string, citations []map[string]string, handler http.Handler, messages []map[string]string) error {
	meta := mobileSearchBasePayload(principal, query, citations, "maclaw_official", "")
	flusher, err := mobileBeginSearchSSE(w, meta)
	if err != nil {
		// No flusher: fall back to non-stream completion then chunked SSE helper.
		answer, requestID, callErr := mobileOfficialSearchAnswer(r, handler, messages)
		if callErr != nil {
			return callErr
		}
		payload := mobileSearchBasePayload(principal, query, citations, "maclaw_official", requestID)
		payload["answer"] = answer
		payload["status"] = "ready"
		mobileWriteSearchSSE(w, payload)
		return nil
	}

	body, err := json.Marshal(map[string]any{
		"model":    "auto",
		"messages": messages,
		"stream":   true,
	})
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	request := r.Clone(r.Context())
	request.Method = http.MethodPost
	urlCopy := *request.URL
	request.URL = &urlCopy
	request.URL.Path = "/api/llm/v1/chat/completions"
	request.RequestURI = request.URL.RequestURI()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.Header = r.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")

	bridge := newMobileLLMStreamBridge(w, flusher)
	handler.ServeHTTP(bridge, request)
	requestID := bridge.requestID
	if requestID == "" {
		requestID = bridge.Header().Get("X-MaClaw-Request-ID")
	}

	if bridge.status != 0 && (bridge.status < http.StatusOK || bridge.status >= http.StatusMultipleChoices) {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}

	answer := strings.TrimSpace(bridge.answer.String())
	if answer == "" && bridge.jsonBuf.Len() > 0 {
		// Upstream ignored stream=true and returned a full JSON completion.
		parsed, rid, parseErr := mobileLLMResponseAnswer(bridge.jsonBuf.Bytes(), requestID)
		if parseErr != nil {
			mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
			return nil
		}
		if rid != "" {
			requestID = rid
		}
		answer = parsed
		mobileWriteAnswerDeltas(w, flusher, answer)
	}
	if strings.TrimSpace(answer) == "" {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}

	payload := mobileSearchBasePayload(principal, query, citations, "maclaw_official", requestID)
	payload["answer"] = answer
	mobileFinishSearchSSE(w, flusher, payload)
	return nil
}

func mobileWriteAnswerDeltas(w http.ResponseWriter, flusher http.Flusher, answer string) {
	for _, chunk := range mobileChunkAnswerForSSE(answer, 48) {
		_ = mobileWriteSSEEvent(w, "delta", map[string]any{"text": chunk})
		if flusher != nil {
			flusher.Flush()
		}
	}
}

// mobileStreamThirdPartySearchAnswer streams mobile SSE from a desktop-delegated
// OpenAI-compatible provider with stream=true when possible.
func mobileStreamThirdPartySearchAnswer(ctx context.Context, w http.ResponseWriter, principal *auth.ViewerPrincipal, query string, citations []map[string]string, record mobileLlmAuthorizationRecord, messages []map[string]string) error {
	meta := mobileSearchBasePayload(principal, query, citations, "desktop_qr_third_party", "")
	flusher, err := mobileBeginSearchSSE(w, meta)
	if err != nil {
		answer, requestID, callErr := mobileThirdPartySearchAnswer(ctx, record, messages)
		if callErr != nil {
			return callErr
		}
		payload := mobileSearchBasePayload(principal, query, citations, "desktop_qr_third_party", requestID)
		payload["answer"] = answer
		payload["status"] = "ready"
		mobileWriteSearchSSE(w, payload)
		return nil
	}

	if protocol := strings.TrimSpace(strings.ToLower(record.Protocol)); protocol != "" && protocol != "openai" {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	endpoint, err := mobileOpenAIChatCompletionURL(record.ProviderURL)
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "auto"
	}
	body, err := json.Marshal(map[string]any{"model": model, "messages": messages, "stream": true})
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(record.APIKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	response, err := mobileLLMHTTPClient.Do(req)
	if err != nil {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}
	defer response.Body.Close()
	requestID := response.Header.Get("X-Request-ID")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}

	contentType := response.Header.Get("Content-Type")
	var answer string
	// Some providers omit Content-Type; sniff SSE vs JSON from the first bytes.
	buffered := bufio.NewReaderSize(response.Body, 64*1024)
	looksLikeSSE := strings.Contains(contentType, "text/event-stream")
	if !looksLikeSSE {
		if prefix, peekErr := buffered.Peek(8); peekErr == nil {
			trimmed := bytes.TrimLeft(prefix, " \t\r\n")
			looksLikeSSE = bytes.HasPrefix(trimmed, []byte("data:")) ||
				bytes.HasPrefix(trimmed, []byte("event:")) ||
				bytes.HasPrefix(trimmed, []byte(":"))
		}
	}
	if looksLikeSSE {
		answer, _ = mobileForwardOpenAIStream(buffered, w, flusher)
	} else {
		payload, readErr := io.ReadAll(io.LimitReader(buffered, 2<<20))
		if readErr != nil {
			mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
			return nil
		}
		parsed, rid, parseErr := mobileLLMResponseAnswer(payload, requestID)
		if parseErr != nil {
			mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
			return nil
		}
		if rid != "" {
			requestID = rid
		}
		answer = parsed
		mobileWriteAnswerDeltas(w, flusher, answer)
	}
	if strings.TrimSpace(answer) == "" {
		mobileEmitSSEClientError(w, flusher, "MOBILE_LLM_FAILED", "mobile AI assistant request failed")
		return nil
	}

	done := mobileSearchBasePayload(principal, query, citations, "desktop_qr_third_party", requestID)
	done["answer"] = strings.TrimSpace(answer)
	mobileFinishSearchSSE(w, flusher, done)
	return nil
}

// mobileLLMStreamBridge captures an in-process official LLM ServeHTTP response
// and, for OpenAI SSE bodies, immediately re-emits content deltas as mobile SSE.
type mobileLLMStreamBridge struct {
	client    http.ResponseWriter
	flusher   http.Flusher
	header    http.Header
	status    int
	wrote     bool
	isSSE     bool
	pending   bytes.Buffer // incomplete SSE lines (byte-oriented for fewer copies)
	answer    strings.Builder
	jsonBuf   bytes.Buffer
	requestID string
}

func newMobileLLMStreamBridge(w http.ResponseWriter, flusher http.Flusher) *mobileLLMStreamBridge {
	return &mobileLLMStreamBridge{
		client:  w,
		flusher: flusher,
		header:  make(http.Header),
	}
}

func (b *mobileLLMStreamBridge) Header() http.Header {
	if b.header == nil {
		b.header = make(http.Header)
	}
	return b.header
}

func (b *mobileLLMStreamBridge) WriteHeader(status int) {
	if b.wrote {
		return
	}
	b.status = status
	b.requestID = b.header.Get("X-MaClaw-Request-ID")
	b.isSSE = strings.Contains(b.header.Get("Content-Type"), "text/event-stream")
	b.wrote = true
}

func (b *mobileLLMStreamBridge) Write(p []byte) (int, error) {
	if !b.wrote {
		b.WriteHeader(http.StatusOK)
	}
	if b.status != 0 && (b.status < http.StatusOK || b.status >= http.StatusMultipleChoices) {
		// Keep a bounded error body for diagnostics; do not forward upstream payload.
		b.appendJSONBuf(p)
		return len(p), nil
	}
	if b.isSSE {
		b.consumeOpenAIStreamBytes(p)
		return len(p), nil
	}
	b.appendJSONBuf(p)
	return len(p), nil
}

func (b *mobileLLMStreamBridge) appendJSONBuf(p []byte) {
	remain := mobileLLMStreamJSONBodyMax - b.jsonBuf.Len()
	if remain <= 0 {
		return
	}
	if len(p) > remain {
		p = p[:remain]
	}
	_, _ = b.jsonBuf.Write(p)
}

func (b *mobileLLMStreamBridge) Flush() {
	if b.flusher != nil {
		b.flusher.Flush()
	}
}

func (b *mobileLLMStreamBridge) consumeOpenAIStreamBytes(p []byte) {
	_, _ = b.pending.Write(p)
	for {
		data := b.pending.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			// Guard against a pathological upstream that never sends a newline.
			if b.pending.Len() > mobileLLMStreamPendingMax {
				keepFrom := b.pending.Len() - mobileLLMStreamPendingMax/2
				if keepFrom < 0 {
					keepFrom = 0
				}
				keep := append([]byte(nil), data[keepFrom:]...)
				b.pending.Reset()
				_, _ = b.pending.Write(keep)
			}
			return
		}
		lineBytes := bytes.TrimRight(data[:idx], "\r")
		rest := append([]byte(nil), data[idx+1:]...)
		b.pending.Reset()
		if len(rest) > 0 {
			_, _ = b.pending.Write(rest)
		}
		line := string(lineBytes)
		if delta, ok := mobileOpenAIStreamDelta(line); ok && delta != "" {
			b.answer.WriteString(delta)
			_ = mobileWriteSSEEvent(b.client, "delta", map[string]any{"text": delta})
			if b.flusher != nil {
				b.flusher.Flush()
			}
		}
	}
}

// mobileForwardOpenAIStream reads an upstream OpenAI SSE body and re-emits
// content deltas as mobile `delta` events. Returns the full assembled answer.
func mobileForwardOpenAIStream(body io.Reader, w http.ResponseWriter, flusher http.Flusher) (string, error) {
	var answer strings.Builder
	scanner := bufio.NewScanner(body)
	// Allow larger SSE lines (default 64K is usually enough; raise for safety).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		delta, ok := mobileOpenAIStreamDelta(line)
		if !ok || delta == "" {
			continue
		}
		answer.WriteString(delta)
		if err := mobileWriteSSEEvent(w, "delta", map[string]any{"text": delta}); err != nil {
			return answer.String(), err
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return answer.String(), scanner.Err()
}

// mobileOpenAIStreamDelta extracts choices[0].delta.content from one SSE line.
// Returns ok=false for non-data lines / [DONE] / unparseable payloads.
// Content may be a string or (rarely) an array of text parts.
func mobileOpenAIStreamDelta(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "data:") {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return "", false
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content json.RawMessage `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return "", false
	}
	if len(chunk.Choices) == 0 || len(chunk.Choices[0].Delta.Content) == 0 {
		return "", false
	}
	raw := bytes.TrimSpace(chunk.Choices[0].Delta.Content)
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	// Common case: "text"
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, true
	}
	// Rare: ["part1","part2"] or [{"type":"text","text":"..."}]
	var asArray []json.RawMessage
	if err := json.Unmarshal(raw, &asArray); err != nil || len(asArray) == 0 {
		return "", false
	}
	var b strings.Builder
	for _, part := range asArray {
		var s string
		if json.Unmarshal(part, &s) == nil {
			b.WriteString(s)
			continue
		}
		var obj struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(part, &obj) == nil && obj.Text != "" {
			b.WriteString(obj.Text)
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

func mobileOfficialSearchAnswer(r *http.Request, handler http.Handler, messages []map[string]string) (string, string, error) {
	if len(messages) == 0 {
		return "", "", fmt.Errorf("official LLM messages are required")
	}
	body, err := json.Marshal(map[string]any{
		"model": "auto", "messages": messages, "stream": false,
	})
	if err != nil {
		return "", "", err
	}
	request := r.Clone(r.Context())
	request.Method = http.MethodPost
	urlCopy := *request.URL
	request.URL = &urlCopy
	request.URL.Path = "/api/llm/v1/chat/completions"
	request.RequestURI = request.URL.RequestURI()
	request.Body = io.NopCloser(bytes.NewReader(body))
	request.ContentLength = int64(len(body))
	request.Header = r.Header.Clone()
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	requestID := recorder.Header().Get("X-MaClaw-Request-ID")
	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		return "", requestID, fmt.Errorf("official LLM returned HTTP %d", recorder.Code)
	}
	return mobileLLMResponseAnswer(recorder.Body.Bytes(), requestID)
}

func mobileThirdPartyLLMAuthorization(ctx context.Context, tenantID, userID string) (mobileLlmAuthorizationRecord, bool) {
	mobileLlmAuthorizations.Lock()
	record, ok := mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(tenantID, userID)]
	mobileLlmAuthorizations.Unlock()
	if !ok {
		record, ok = mobilePersistedLLMAuthorization(ctx, tenantID, userID)
		if ok {
			mobileLlmAuthorizations.Lock()
			mobileLlmAuthorizations.authorizations[mobileLlmAuthorizationKey(tenantID, userID)] = record
			mobileLlmAuthorizations.Unlock()
		}
	}
	return record, ok && strings.TrimSpace(record.ProviderURL) != "" && strings.TrimSpace(record.APIKey) != ""
}

func mobileThirdPartySearchAnswer(ctx context.Context, record mobileLlmAuthorizationRecord, messages []map[string]string) (string, string, error) {
	if protocol := strings.TrimSpace(strings.ToLower(record.Protocol)); protocol != "" && protocol != "openai" {
		return "", "", fmt.Errorf("unsupported desktop LLM protocol %q", protocol)
	}
	if len(messages) == 0 {
		return "", "", fmt.Errorf("desktop delegated LLM messages are required")
	}
	endpoint, err := mobileOpenAIChatCompletionURL(record.ProviderURL)
	if err != nil {
		return "", "", err
	}
	model := strings.TrimSpace(record.Model)
	if model == "" {
		model = "auto"
	}
	body, err := json.Marshal(map[string]any{"model": model, "messages": messages, "stream": false})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(record.APIKey))
	req.Header.Set("Content-Type", "application/json")
	response, err := mobileLLMHTTPClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return "", "", err
	}
	requestID := response.Header.Get("X-Request-ID")
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", requestID, fmt.Errorf("desktop delegated LLM returned HTTP %d", response.StatusCode)
	}
	return mobileLLMResponseAnswer(payload, requestID)
}

func mobileOpenAIChatCompletionURL(providerURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(providerURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("desktop delegated LLM provider URL is invalid")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(parsed.Path, "/chat/completions") {
		parsed.Path += "/chat/completions"
	}
	return parsed.String(), nil
}

func mobileLLMResponseAnswer(payload []byte, requestID string) (string, string, error) {
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &response); err != nil {
		return "", requestID, err
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", requestID, fmt.Errorf("LLM response did not contain an answer")
	}
	return strings.TrimSpace(response.Choices[0].Message.Content), requestID, nil
}

var mobileLLMHTTPClient = &http.Client{Timeout: 45 * time.Second}

func mobileSearchAnswer(query string, results []websearch.SearchResult, links []string) string {
	query = strings.TrimSpace(query)
	if len(results) == 0 {
		if len(links) > 0 {
			return "已识别你分享的链接，并会把它作为来源。我还没有检索到更多公开网页结果；你可以补充想了解的重点，或直接让我根据链接帮你整理要点。"
		}
		return "暂时没有找到可引用的公开来源。请换一个更具体的问题，或补充地点、时间、系统名称等关键信息后再试。"
	}
	// Fallback when no LLM is wired: never dump SERP snippets as the main answer.
	var b strings.Builder
	b.WriteString("已找到 ")
	b.WriteString(strconv.Itoa(len(results)))
	b.WriteString(" 条相关来源")
	if query != "" {
		b.WriteString("（关于「")
		b.WriteString(query)
		b.WriteString("」）")
	}
	b.WriteString("。完整结构化总结需要可用的 LLM 服务；当前先给出可点开核对的来源标题，展开「来源」可查看链接。")
	for i, result := range results {
		if i >= 3 {
			break
		}
		title := mobileCleanSearchText(result.Title, 80)
		if title == "" {
			title = strings.TrimSpace(result.URL)
		}
		if title == "" {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(title)
	}
	return b.String()
}

func mobileExtractQueryLinks(query string) []string {
	seen := map[string]bool{}
	var links []string
	for _, field := range strings.Fields(query) {
		candidate := strings.Trim(field, " \t\r\n<>（）()[]{}，,。；;\"'")
		u, err := url.Parse(candidate)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			continue
		}
		normalized := u.String()
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		links = append(links, normalized)
	}
	return links
}

func mobileMergeLinkCitations(citations []map[string]string, links []string) []map[string]string {
	if len(links) == 0 {
		return citations
	}
	seen := map[string]bool{}
	for _, citation := range citations {
		seen[strings.TrimSpace(citation["url"])] = true
	}
	for _, link := range links {
		if seen[link] {
			continue
		}
		citations = append(citations, map[string]string{
			"title":   link,
			"url":     link,
			"snippet": "用户分享的链接",
		})
	}
	return citations
}
func mobileSearchCitations(results []websearch.SearchResult) []map[string]string {
	citations := make([]map[string]string, 0, len(results))
	for _, result := range results {
		url := strings.TrimSpace(result.URL)
		if url == "" {
			continue
		}
		title := mobileCleanSearchText(result.Title, 160)
		if title == "" {
			title = url
		}
		citations = append(citations, map[string]string{
			"title":   title,
			"url":     url,
			"snippet": mobileCleanSearchText(result.Snippet, 280),
		})
	}
	return citations
}

type mobileDocumentDraftRequest struct {
	Title    string `json:"title"`
	Template string `json:"template"`
	Content  string `json:"content,omitempty"`
	// Markdown, when set, is stored as the draft body as-is (desktop file share / import).
	Markdown string `json:"markdown,omitempty"`
}

type mobileDocumentDraftUpdateRequest struct {
	Title    string `json:"title"`
	Markdown string `json:"markdown"`
}

type mobileDocumentProcessRequest struct {
	Action string `json:"action"`
	// Async forces background processing; large drafts auto-upgrade even when false.
	Async bool `json:"async,omitempty"`
}

type mobileDocumentExportRequest struct {
	DraftID string `json:"draft_id"`
	Format  string `json:"format"`
}

type mobileDocumentUploadResultRequest struct {
	Status   string `json:"status,omitempty"`
	Markdown string `json:"markdown,omitempty"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
}

type mobileSSHAnalyzeRequest struct {
	Output           string `json:"output"`
	BackendSessionID string `json:"backend_session_id,omitempty"`
}

type mobileBackendSSHSessionRequest struct {
	ServerProfileID string `json:"server_profile_id"`
	// ExecMode: desktop_exec (default) | hub_exec (requires vault secret).
	ExecMode string `json:"exec_mode,omitempty"`
}

type mobileBackendSSHInputRequest struct {
	Input string `json:"input"`
	// DataB64: optional base64 (std or raw) binary frame for hub_exec PTY.
	// When set, takes precedence over Input and forces raw=true (Phase E binary path).
	DataB64 string `json:"data_b64,omitempty"`
	// Raw: hub_exec only — write to interactive PTY without forcing a trailing newline
	// (for Ctrl-C, partial lines, or true interactive control sequences).
	Raw bool `json:"raw,omitempty"`
}

type mobileBackendSSHTaskRequest struct {
	Action    string `json:"action"`
	Command   string `json:"command"`
	TailLines int    `json:"tail_lines,omitempty"`
}

type mobileBackendSSHTaskWaitRequest struct {
	TimeoutSeconds int `json:"timeout,omitempty"`
	TailLines      int `json:"tail_lines,omitempty"`
}

type mobileBackendSSHTaskUpdateRequest struct {
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
	LogTail          string `json:"log_tail,omitempty"`
	Output           string `json:"output,omitempty"`
	ExitCode         *int   `json:"exit_code,omitempty"`
	BackendSessionID string `json:"backend_session_id,omitempty"`
}

type mobileBackendSSHFileOperationRequest struct {
	Action     string `json:"action"`
	LocalPath  string `json:"local_path,omitempty"`
	RemotePath string `json:"remote_path,omitempty"`
}

type mobileBackendSSHFileOperationUpdateRequest struct {
	Status           string `json:"status"`
	Message          string `json:"message,omitempty"`
	Error            string `json:"error,omitempty"`
	LocalPath        string `json:"local_path,omitempty"`
	RemotePath       string `json:"remote_path,omitempty"`
	BytesTransferred int64  `json:"bytes_transferred,omitempty"`
	DownloadURL      string `json:"download_url,omitempty"`
	BackendSessionID string `json:"backend_session_id,omitempty"`
}

type mobileBackendSSHSessionUpdateRequest struct {
	Status            string `json:"status"`
	State             string `json:"state,omitempty"`
	Message           string `json:"message,omitempty"`
	Error             string `json:"error,omitempty"`
	RecentOutput      string `json:"recent_output,omitempty"`
	OutputChunk       string `json:"output_chunk,omitempty"`
	BackendSessionID  string `json:"backend_session_id,omitempty"`
	ClearPendingInput bool   `json:"clear_pending_input,omitempty"`
	AppliedInputCount int    `json:"applied_input_count,omitempty"`
}

type mobileServerProfileRequest struct {
	Profiles []mobileServerProfilePayload `json:"profiles"`
}

type mobileServerProfilePayload struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	AuthMode string `json:"auth_mode"`
	Tag      string `json:"tag,omitempty"`
	Note     string `json:"note,omitempty"`
}

type mobileDigitalEmployeeTaskRequest struct {
	Prompt   string            `json:"prompt"`
	TaskType string            `json:"task_type,omitempty"`
	Context  map[string]string `json:"context,omitempty"`
}

type mobileDigitalEmployeeTaskUpdateRequest struct {
	Status  string `json:"status"`
	Result  string `json:"result"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func mobileDigitalEmployeeTaskPayload(record mobileDigitalEmployeeTaskRecord) map[string]any {
	return map[string]any{
		"task_id":     record.TaskID,
		"employee_id": record.EmployeeID,
		"prompt":      record.Prompt,
		"task_type":   record.TaskType,
		"context":     record.Context,
		"status":      record.Status,
		"result":      record.Result,
		"message":     record.Message,
		"claimed_by":  record.ClaimedBy,
		"created_at":  record.CreatedAt.Format(time.RFC3339),
		"updated_at":  record.UpdatedAt.Format(time.RFC3339),
	}
}

func mobileBackendSSHSessionPayload(record mobileBackendSSHSessionRecord) map[string]any {
	execMode := record.ExecMode
	if execMode == "" {
		execMode = mobileSSHExecDesktop
	}
	return map[string]any{
		"session_id":          record.SessionID,
		"server_profile_id":   record.ServerProfileID,
		"backend_session_id":  record.BackendSessionID,
		"exec_mode":           execMode,
		"status":              record.Status,
		"state":               record.State,
		"message":             record.Message,
		"recent_output":       record.RecentOutput,
		"output_chunk":        record.OutputChunk,
		"output_seq":          record.OutputSeq,
		"pending_input_count": len(record.PendingInput),
		"claimed_by":          record.ClaimedBy,
		"created_at":          record.CreatedAt.Format(time.RFC3339),
		"updated_at":          record.UpdatedAt.Format(time.RFC3339),
		"last_activity_at":    record.UpdatedAt.Format(time.RFC3339),
	}
}

func mobileBackendSSHWorkerSessionPayload(record mobileBackendSSHSessionRecord) map[string]any {
	payload := mobileBackendSSHSessionPayload(record)
	pending := make([]string, len(record.PendingInput))
	copy(pending, record.PendingInput)
	payload["pending_input"] = pending
	return payload
}

func mobileBackendSSHTaskPayload(record mobileBackendSSHTaskRecord) map[string]any {
	payload := map[string]any{
		"task_id":            record.TaskID,
		"session_id":         record.SessionID,
		"backend_session_id": record.BackendSessionID,
		"action":             record.Action,
		"command":            record.Command,
		"status":             record.Status,
		"message":            record.Message,
		"log_tail":           record.LogTail,
		"tail_lines":         record.TailLines,
		"timeout":            record.TimeoutSeconds,
		"claimed_by":         record.ClaimedBy,
		"created_at":         record.CreatedAt.Format(time.RFC3339),
		"updated_at":         record.UpdatedAt.Format(time.RFC3339),
	}
	if record.ExitCode != nil {
		payload["exit_code"] = *record.ExitCode
	}
	return payload
}

func mobileBackendSSHFileOperationPayload(record mobileBackendSSHFileOperationRecord) map[string]any {
	return map[string]any{
		"operation_id":       record.OperationID,
		"session_id":         record.SessionID,
		"backend_session_id": record.BackendSessionID,
		"action":             record.Action,
		"local_path":         record.LocalPath,
		"remote_path":        record.RemotePath,
		"status":             record.Status,
		"message":            record.Message,
		"bytes_transferred":  record.BytesTransferred,
		"download_url":       record.DownloadURL,
		"claimed_by":         record.ClaimedBy,
		"created_at":         record.CreatedAt.Format(time.RFC3339),
		"updated_at":         record.UpdatedAt.Format(time.RFC3339),
	}
}

func mobileServerProfileResponse(record mobileServerProfileRecord) map[string]any {
	return map[string]any{
		"id":                record.ProfileID,
		"name":              record.Name,
		"host":              record.Host,
		"port":              record.Port,
		"username":          record.Username,
		"auth_mode":         record.AuthMode,
		"tag":               record.Tag,
		"note":              record.Note,
		"source_machine_id": record.SourceMachineID,
		"updated_at":        record.UpdatedAt.Format(time.RFC3339),
	}
}

func normalizeMobileServerProfileAuthMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "key", "private_key", "private-key":
		return "private_key"
	case "agent":
		return "agent"
	default:
		return "password"
	}
}

func sanitizeMobileServerProfileText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit > 0 && len([]rune(value)) > limit {
		runes := []rune(value)
		value = string(runes[:limit])
	}
	return value
}

func mobileRealtimeBackendSSHSessionEvent(payload map[string]any) map[string]any {
	event := map[string]any{
		"type":    "ssh_session",
		"session": payload,
	}
	if sessionID, _ := payload["session_id"].(string); sessionID != "" {
		event["session_id"] = sessionID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	if outputChunk, _ := payload["output_chunk"].(string); outputChunk != "" {
		event["output_chunk"] = outputChunk
	}
	if outputSeq, ok := payload["output_seq"]; ok {
		event["output_seq"] = outputSeq
	}
	return event
}

func mobileRealtimeBackendSSHTaskEvent(payload map[string]any) map[string]any {
	event := map[string]any{
		"type": "ssh_task",
		"task": payload,
	}
	if taskID, _ := payload["task_id"].(string); taskID != "" {
		event["task_id"] = taskID
	}
	if sessionID, _ := payload["session_id"].(string); sessionID != "" {
		event["session_id"] = sessionID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

func mobileRealtimeBackendSSHFileOperationEvent(payload map[string]any) map[string]any {
	event := map[string]any{
		"type":      "ssh_file_operation",
		"operation": payload,
	}
	if operationID, _ := payload["operation_id"].(string); operationID != "" {
		event["operation_id"] = operationID
	}
	if sessionID, _ := payload["session_id"].(string); sessionID != "" {
		event["session_id"] = sessionID
	}
	if status, _ := payload["status"].(string); status != "" {
		event["status"] = status
	}
	return event
}

func normalizeMobileDigitalEmployeeTaskType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "server_maintenance", "remote_server", "ssh":
		return "server_maintenance"
	case "desktop_assist", "remote_desktop", "desktop":
		return "desktop_assist"
	case "document_work", "document", "office":
		return "document_work"
	case "information_check", "info_check", "search":
		return "information_check"
	default:
		return "general"
	}
}

func sanitizeMobileDigitalEmployeeContext(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string)
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		if len(key) > 64 {
			key = key[:64]
		}
		if len(value) > 512 {
			value = value[:512]
		}
		out[key] = value
		if len(out) >= 12 {
			break
		}
	}
	return out
}

type mobileDigitalEmployeeWorkerPrincipal struct {
	TenantID  string
	UserID    string
	MachineID string
}

func authenticateMobileDigitalEmployeeWorker(r *http.Request, identity *auth.IdentityService) (mobileDigitalEmployeeWorkerPrincipal, error) {
	if identity == nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, auth.ErrInvalidUserCredentials
	}
	machineID := strings.TrimSpace(r.Header.Get("X-Machine-ID"))
	if machineID == "" {
		machineID = strings.TrimSpace(r.Header.Get("X-MaClaw-Machine-ID"))
	}
	if machineID != "" {
		if token := extractBearerToken(r); token != "" {
			principal, err := identity.AuthenticateMachine(r.Context(), machineID, token)
			if err == nil && principal != nil {
				return mobileDigitalEmployeeWorkerPrincipal{
					TenantID:  principal.TenantID,
					UserID:    principal.UserID,
					MachineID: principal.MachineID,
				}, nil
			}
		}
	}
	viewer, err := authenticateViewerRequest(r, identity)
	if err != nil {
		return mobileDigitalEmployeeWorkerPrincipal{}, err
	}
	return mobileDigitalEmployeeWorkerPrincipal{
		TenantID: viewer.TenantID,
		UserID:   viewer.UserID,
	}, nil
}

// MobileDocumentDraftHandler creates an emergency document draft contract. It
// returns an ID and normalized markdown so the mobile app can continue editing
// offline while the richer document pipeline is implemented.
func MobileDocumentDraftHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()

		var req mobileDocumentDraftRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		template := strings.TrimSpace(req.Template)
		if template == "" {
			template = "report"
		}
		markdown := strings.TrimSpace(req.Markdown)
		if markdown == "" {
			content := strings.TrimSpace(req.Content)
			if content == "" {
				content = "请在这里补充正文。"
			}
			markdown = "# " + title + "\n\n" + content + "\n"
		}
		// Free baseline limit; paid bootstrap may show higher but enforcement uses free unless we re-resolve grants (kept simple).
		if err := mobileCheckDocumentQuotaForPrincipal(r.Context(), principal, int64(len(markdown))); err != nil {
			writeError(w, http.StatusInsufficientStorage, "DOCUMENT_QUOTA_EXCEEDED", "document storage quota exceeded")
			return
		}
		now := time.Now().UTC()
		draftID := fmt.Sprintf("mobdoc_%d", now.UnixNano())
		record := mobileDocumentDraftRecord{
			ID:        draftID,
			OwnerID:   principal.UserID,
			Title:     title,
			Template:  template,
			Markdown:  markdown,
			UpdatedAt: now,
		}
		mobileDocuments.Lock()
		mobileDocuments.drafts[draftID] = record
		mobileDocuments.Unlock()
		mobilePersistState()
		go mobileIngestDocumentDraft(principal, record)

		writeJSON(w, http.StatusCreated, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_created",
		})
	}
}

// MobileDocumentDraftUpdateHandler persists lightweight title/body edits made
// on the mobile device before export or sharing. DELETE removes the draft from
// the shared library (desktop GUI / phone both use this path).
func MobileDocumentDraftUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPatch:
			// fall through to update
		case http.MethodDelete:
			mobileDocumentDraftDelete(w, r, identity)
			return
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH or DELETE")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentDraftUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		title := strings.TrimSpace(req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "title is required")
			return
		}
		markdown := strings.TrimSpace(req.Markdown)
		if markdown == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "markdown is required")
			return
		}
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
			return
		}
		// Pre-check ownership + delta quota outside mutation.
		mobileDocuments.Lock()
		prev, okPrev := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !okPrev || prev.OwnerID != ownerID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		delta := int64(len(markdown)) - int64(len(prev.Markdown))
		if delta > 0 {
			if err := mobileCheckDocumentQuotaForPrincipal(r.Context(), principal, delta); err != nil {
				writeError(w, http.StatusInsufficientStorage, "DOCUMENT_QUOTA_EXCEEDED", "document storage quota exceeded")
				return
			}
		}
		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == ownerID {
			record.Title = title
			record.Markdown = markdown
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != ownerID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		mobilePersistState()
		go mobileIngestDocumentDraft(principal, record)
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "draft_updated",
		})
	}
}

func mobileDocumentDraftDelete(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService) {
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return
	}
	mobileEnsureStateLoaded()
	draftID := strings.TrimSpace(r.PathValue("draftId"))
	if draftID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
		return
	}
	ownerID := mobilePrincipalOwnerID(principal)
	if ownerID == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
		return
	}
	mobileDocuments.Lock()
	record, ok := mobileDocuments.drafts[draftID]
	if !ok || record.OwnerID != ownerID {
		// Idempotent: missing draft is treated as already deleted so clients can
		// clear local cache without a hard failure after Hub restart.
		mobileDocuments.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":   "draft_already_gone",
			"draft_id": draftID,
		})
		return
	}
	blobPath := record.SourcePath
	delete(mobileDocuments.drafts, draftID)
	// Drop related upload tasks that pointed at this draft (free quota/source).
	for taskID, upload := range mobileDocuments.uploads {
		if upload.OwnerID == ownerID && upload.DraftID == draftID {
			mobileDeleteDocumentBlob(upload.SourcePath)
			delete(mobileDocuments.uploads, taskID)
		}
	}
	mobileDocuments.Unlock()
	mobileDeleteDocumentBlob(blobPath)
	mobilePersistState()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "draft_deleted",
		"draft_id": draftID,
	})
}

// MobileDocumentProcessHandler applies lightweight emergency document actions.
// Large drafts (or async=true) upgrade to a background job listed in /api/mobile/jobs.
func MobileDocumentProcessHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		draftID := strings.TrimSpace(r.PathValue("draftId"))
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft id is required")
			return
		}
		var req mobileDocumentProcessRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		if !mobileDocumentProcessActionAllowed(action) {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "action must be one of summarize, translate, rewrite, expand, polish, format")
			return
		}

		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
			return
		}
		mobileDocuments.Lock()
		record, ok := mobileDocuments.drafts[draftID]
		ownerOK := ok && record.OwnerID == ownerID
		markdownSnapshot := ""
		if ownerOK {
			markdownSnapshot = record.Markdown
		}
		mobileDocuments.Unlock()
		if !ownerOK {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}

		// Long path: enqueue background document process job.
		if mobileDocumentProcessShouldAsync(req.Async, markdownSnapshot) {
			job := mobileEnqueueDocumentProcessJob(principal, draftID, action)
			writeJSON(w, http.StatusAccepted, map[string]any{
				"status":    "accepted",
				"async":     true,
				"action":    action,
				"job_id":    job.JobID,
				"job":       mobileDocumentProcessJobPayload(job),
				"draft_id":  draftID,
				"deep_link": "/documents",
				"message":   "document process queued; track via GET /api/mobile/documents/process-jobs/{job_id} or /api/mobile/jobs",
			})
			return
		}

		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok = mobileDocuments.drafts[draftID]
		if ok && record.OwnerID == ownerID {
			record.Markdown = mobileProcessDocumentMarkdown(action, record.Markdown)
			record.UpdatedAt = now
			mobileDocuments.drafts[draftID] = record
		}
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != ownerID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		mobilePersistState()
		go mobileIngestDocumentDraft(principal, record)
		writeJSON(w, http.StatusOK, map[string]any{
			"draft":  mobileDocumentDraftPayload(record),
			"status": "processed",
			"action": action,
		})
	}
}

func mobileDocumentProcessActionAllowed(action string) bool {
	switch action {
	case "summarize", "translate", "rewrite", "expand", "polish", "format":
		return true
	default:
		return false
	}
}

func mobileProcessDocumentMarkdown(action, markdown string) string {
	title, bodyLines := mobileDocumentTitleAndBody(markdown)
	switch action {
	case "summarize":
		return mobileProcessSummarize(title, bodyLines)
	case "translate":
		return mobileProcessTranslate(title, bodyLines)
	case "rewrite":
		return mobileProcessRewrite(title, bodyLines)
	case "expand":
		return mobileProcessExpand(title, bodyLines)
	case "polish":
		return mobileProcessPolish(title, bodyLines)
	case "format":
		return mobileProcessFormat(title, bodyLines)
	default:
		return markdown
	}
}

func mobileDocumentTitleAndBody(markdown string) (string, []string) {
	lines := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n")
	title := "文档"
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") && title == "文档" {
			title = strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
			continue
		}
		if trimmed != "" {
			body = append(body, trimmed)
		}
	}
	return title, body
}

func mobileProcessSummarize(title string, body []string) string {
	points := mobileFirstNonEmptyLines(body, 5)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 摘要\n\n")
	if len(points) == 0 {
		b.WriteString("- 暂无可摘要内容。\n")
		return b.String()
	}
	for _, point := range points {
		b.WriteString("- ")
		b.WriteString(mobileTrimRunes(point, 120))
		b.WriteString("\n")
	}
	return b.String()
}

func mobileProcessTranslate(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 翻译草稿\n\n")
	b.WriteString("> 当前为移动端应急翻译草稿。联网 LLM 可用后会替换为完整翻译。\n\n")
	for _, line := range body {
		b.WriteString(line)
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessRewrite(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 改写稿\n\n")
	for _, line := range body {
		b.WriteString("- ")
		b.WriteString(strings.Trim(strings.TrimPrefix(line, "-"), " 。"))
		b.WriteString("。\n")
	}
	return b.String()
}

func mobileProcessExpand(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 扩写稿\n\n")
	for _, line := range body {
		b.WriteString("## ")
		b.WriteString(mobileTrimRunes(strings.Trim(strings.TrimPrefix(line, "-"), " 。"), 36))
		b.WriteString("\n\n")
		b.WriteString(line)
		b.WriteString("\n\n待补充：背景、影响、处理建议和下一步行动。\n\n")
	}
	return b.String()
}

func mobileProcessPolish(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString(" 润色稿\n\n")
	for _, line := range body {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		b.WriteString(line)
		if !strings.HasSuffix(line, "。") && !strings.HasSuffix(line, ".") {
			b.WriteString("。")
		}
		b.WriteString("\n\n")
	}
	return b.String()
}

func mobileProcessFormat(title string, body []string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, line := range body {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "##") || strings.HasPrefix(line, "```") {
			b.WriteString(line)
		} else {
			b.WriteString("- ")
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func mobileFirstNonEmptyLines(lines []string, limit int) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func mobileTrimRunes(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

// MobileDocumentExportHandler validates export requests and returns an export
// job contract. Markdown, lightweight PDF, and lightweight DOCX are generated
// in-process for emergency mobile use.
func MobileDocumentExportHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()

		var req mobileDocumentExportRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		draftID := strings.TrimSpace(req.DraftID)
		if draftID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "draft_id is required")
			return
		}
		mobileDocuments.Lock()
		draft, ok := mobileDocuments.drafts[draftID]
		mobileDocuments.Unlock()
		if !ok || draft.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		format := strings.ToLower(strings.TrimSpace(req.Format))
		switch format {
		case "pdf", "word", "markdown":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "format must be one of pdf, word, markdown")
			return
		}

		now := time.Now().UTC()
		job := mobileDocumentExportRecord{
			JobID:     fmt.Sprintf("mobexp_%d", now.UnixNano()),
			DraftID:   draftID,
			OwnerID:   principal.UserID,
			Format:    format,
			Status:    "queued",
			Message:   "导出任务已提交，等待官方服务生成文件。",
			CreatedAt: now,
		}
		if format == "markdown" || format == "pdf" || format == "word" {
			job.Status = "ready"
			job.Message = "导出文件已生成，可下载或分享。"
		}
		mobileDocuments.Lock()
		mobileDocuments.exports[job.JobID] = job
		mobileDocuments.Unlock()
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", mobileDocumentExportPayload(job)))
		writeJSON(w, http.StatusAccepted, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportStatusHandler returns the current export job status.
func MobileDocumentExportStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDocumentExportPayload(job))
	}
}

// MobileDocumentExportDownloadHandler downloads finished lightweight exports.
// Markdown, emergency PDF, and emergency DOCX are generated in-process.
func MobileDocumentExportDownloadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		jobID := strings.TrimSpace(r.PathValue("jobId"))
		mobileDocuments.Lock()
		job, ok := mobileDocuments.exports[jobID]
		mobileDocuments.Unlock()
		if !ok || job.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "EXPORT_NOT_FOUND", "export job not found")
			return
		}
		if job.Status != "ready" || (job.Format != "markdown" && job.Format != "pdf" && job.Format != "word") {
			writeError(w, http.StatusConflict, "EXPORT_NOT_READY", "export job is not ready")
			return
		}
		mobileDocuments.Lock()
		draft, hasDraft := mobileDocuments.drafts[job.DraftID]
		mobileDocuments.Unlock()
		if !hasDraft || draft.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "DRAFT_NOT_FOUND", "draft not found")
			return
		}
		if job.Format == "pdf" {
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".pdf\"")
			_, _ = w.Write(mobileRenderDraftPDF(draft))
			return
		}
		if job.Format == "word" {
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".docx\"")
			_, _ = w.Write(mobileRenderDraftDOCX(draft))
			return
		}
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+job.DraftID+".md\"")
		_, _ = w.Write([]byte(draft.Markdown))
	}
}

func mobileRenderDraftPDF(draft mobileDocumentDraftRecord) []byte {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	const linesPerPage = 34
	pageCount := (len(lines) + linesPerPage - 1) / linesPerPage
	if pageCount == 0 {
		pageCount = 1
	}
	fontID := 3 + pageCount*2
	objects := make([]string, fontID+1)
	objects[1] = "<< /Type /Catalog /Pages 2 0 R >>"
	kids := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pageID := 3 + page*2
		contentID := pageID + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", pageID))
		start := page * linesPerPage
		end := start + linesPerPage
		if end > len(lines) {
			end = len(lines)
		}
		contents := mobilePDFPageContent(lines[start:end])
		objects[pageID] = fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 %d 0 R >> >> /Contents %d 0 R >>", fontID, contentID)
		objects[contentID] = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(contents), contents)
	}
	objects[2] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pageCount)
	objects[fontID] = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

	var out strings.Builder
	out.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objects))
	for id := 1; id < len(objects); id++ {
		offsets[id] = out.Len()
		out.WriteString(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", id, objects[id]))
	}
	xrefOffset := out.Len()
	out.WriteString(fmt.Sprintf("xref\n0 %d\n", len(objects)))
	out.WriteString("0000000000 65535 f \n")
	for id := 1; id < len(objects); id++ {
		out.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[id]))
	}
	out.WriteString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects), xrefOffset))
	return []byte(out.String())
}

func mobilePDFLines(title, markdown string) []string {
	var out []string
	title = strings.TrimSpace(title)
	if title != "" {
		out = append(out, title, "")
	}
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	for _, raw := range strings.Split(normalized, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "#")
		line = strings.TrimSpace(line)
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, mobileWrapPDFLine(line, 48)...)
	}
	return out
}

func mobileWrapPDFLine(line string, width int) []string {
	runes := []rune(line)
	if len(runes) <= width {
		return []string{line}
	}
	var out []string
	for len(runes) > 0 {
		n := width
		if len(runes) < n {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func mobilePDFPageContent(lines []string) string {
	var b strings.Builder
	b.WriteString("BT\n/F1 12 Tf\n50 790 Td\n")
	for idx, line := range lines {
		if idx > 0 {
			b.WriteString("0 -20 Td\n")
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString("<")
		b.WriteString(mobilePDFUTF16Hex(line))
		b.WriteString("> Tj\n")
	}
	b.WriteString("ET")
	return b.String()
}

func mobilePDFUTF16Hex(text string) string {
	units := utf16.Encode([]rune(text))
	var b strings.Builder
	b.WriteString("FEFF")
	for _, unit := range units {
		b.WriteString(fmt.Sprintf("%04X", unit))
	}
	return b.String()
}

func mobileRenderDraftDOCX(draft mobileDocumentDraftRecord) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	mobileDOCXWriteFile(zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	mobileDOCXWriteFile(zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	mobileDOCXWriteFile(zw, "word/document.xml", mobileDOCXDocumentXML(draft))
	_ = zw.Close()
	return buf.Bytes()
}

func mobileDOCXWriteFile(zw *zip.Writer, name, body string) {
	w, err := zw.Create(name)
	if err != nil {
		return
	}
	_, _ = io.WriteString(w, body)
}

func mobileDOCXDocumentXML(draft mobileDocumentDraftRecord) string {
	lines := mobilePDFLines(draft.Title, draft.Markdown)
	if len(lines) == 0 {
		lines = []string{draft.Title}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			b.WriteString(`<w:p/>`)
			continue
		}
		b.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
		b.WriteString(mobileXMLEscape(line))
		b.WriteString(`</w:t></w:r></w:p>`)
	}
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func mobileXMLEscape(text string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(text))
	return b.String()
}

const mobileDocumentUploadMaxBytes = 25 << 20

// Stale in_progress claims (worker crash / network drop) become claimable again.
const mobileDocumentUploadClaimTimeout = 5 * time.Minute

// mobileReclaimStaleDocumentUploadIfNeeded resets one timed-out in_progress task
// so it can be claimed again (or so Mobile status polling leaves in_progress).
// Returns the (possibly updated) record and true when a reclaim occurred.
func mobileReclaimStaleDocumentUploadIfNeeded(record mobileDocumentUploadRecord, now time.Time) (mobileDocumentUploadRecord, bool) {
	if record.Status != "in_progress" {
		return record, false
	}
	if now.Sub(record.UpdatedAt) < mobileDocumentUploadClaimTimeout {
		return record, false
	}
	// Prefer needs_ocr when a draft already exists (typical image OCR path).
	if strings.TrimSpace(record.DraftID) != "" || mobileUploadedFileIsImage(record.Filename) {
		record.Status = "needs_ocr"
		record.Message = "远程 OCR/解析超时，等待重新认领。"
	} else {
		record.Status = "queued"
		record.Message = "远程解析超时，等待重新认领。"
	}
	record.ClaimedBy = ""
	record.UpdatedAt = now
	return record, true
}

// mobileReclaimStaleDocumentUploadClaims resets timed-out in_progress tasks so
// another (or the same) worker can claim them. Caller must hold mobileDocuments.Lock.
func mobileReclaimStaleDocumentUploadClaims(now time.Time) int {
	reclaimed := 0
	for taskID, record := range mobileDocuments.uploads {
		next, ok := mobileReclaimStaleDocumentUploadIfNeeded(record, now)
		if !ok {
			continue
		}
		mobileDocuments.uploads[taskID] = next
		reclaimed++
	}
	return reclaimed
}

func mobileUploadedFileIsImmediateDraft(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".txt", ".md", ".markdown", ".log", ".csv", ".json", ".docx", ".xlsx", ".pdf":
		return true
	default:
		return false
	}
}

func mobileUploadedFileIsImage(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".tif", ".tiff":
		return true
	default:
		return false
	}
}

func mobileDraftMarkdownFromUpload(filename string, raw []byte) (string, bool) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".docx":
		return mobileDraftMarkdownFromDOCX(filename, raw)
	case ".xlsx":
		return mobileDraftMarkdownFromXLSX(filename, raw)
	case ".pdf":
		return mobileDraftMarkdownFromPDF(filename, raw)
	}
	if !utf8.Valid(raw) {
		return "", false
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		text = "_导入文件为空。_"
	}
	if ext == ".md" || ext == ".markdown" {
		return text + "\n", true
	}
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		title = "导入文档"
	}
	switch ext {
	case ".log":
		return "# " + title + "\n\n```text\n" + text + "\n```\n", true
	case ".csv":
		return "# " + title + "\n\n```csv\n" + text + "\n```\n", true
	case ".json":
		return "# " + title + "\n\n```json\n" + text + "\n```\n", true
	default:
		return "# " + title + "\n\n" + text + "\n", true
	}
}

func mobileDraftMarkdownFromDOCX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	var documentXML []byte
	for _, file := range zr.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", false
		}
		documentXML, err = io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return "", false
		}
		break
	}
	if len(documentXML) == 0 {
		return "", false
	}
	paragraphs := mobileDOCXParagraphs(documentXML)
	if len(paragraphs) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for _, paragraph := range paragraphs {
		b.WriteString(paragraph)
		b.WriteString("\n\n")
	}
	return b.String(), true
}

func mobileDOCXParagraphs(raw []byte) []string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var paragraphs []string
	var current strings.Builder
	inParagraph := false
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "p":
				inParagraph = true
				current.Reset()
			case "t":
				if inParagraph {
					var text string
					if err := decoder.DecodeElement(&text, &item); err == nil {
						current.WriteString(text)
					}
				}
			case "tab":
				if inParagraph {
					current.WriteString("\t")
				}
			case "br":
				if inParagraph {
					current.WriteString("\n")
				}
			}
		case xml.EndElement:
			if item.Name.Local == "p" && inParagraph {
				if text := strings.TrimSpace(current.String()); text != "" {
					paragraphs = append(paragraphs, text)
				}
				inParagraph = false
				current.Reset()
			}
		}
	}
	return paragraphs
}

func mobileDraftMarkdownFromXLSX(filename string, raw []byte) (string, bool) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", false
	}
	sharedStrings := mobileXLSXSharedStrings(zr)
	sheets := mobileXLSXSheets(zr, sharedStrings)
	if len(sheets) == 0 {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	for index, rows := range sheets {
		if len(rows) == 0 {
			continue
		}
		if index > 0 {
			b.WriteString("\n")
		}
		b.WriteString("## Sheet ")
		b.WriteString(fmt.Sprintf("%d", index+1))
		b.WriteString("\n\n")
		for _, row := range rows {
			b.WriteString("- ")
			b.WriteString(strings.Join(row, " | "))
			b.WriteString("\n")
		}
	}
	return b.String(), true
}

func mobileDraftMarkdownFromPDF(filename string, raw []byte) (string, bool) {
	text := strings.TrimSpace(mobilePDFExtractText(raw))
	if text == "" {
		return "", false
	}
	title := mobileUploadTitle(filename)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(text)
	b.WriteString("\n")
	return b.String(), true
}

func mobileDraftMarkdownFromImage(filename string, raw []byte) string {
	title := mobileUploadTitle(filename)
	format := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if format == "jpg" {
		format = "jpeg"
	}
	width, height := mobileImageDimensions(format, raw)
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString("图片原件已保存。可直接分享原图；OCR/视觉识别完成后会补充可读正文。\n\n")
	b.WriteString("- 文件名：")
	b.WriteString(filepath.Base(filename))
	b.WriteString("\n")
	b.WriteString("- 格式：")
	b.WriteString(format)
	b.WriteString("\n")
	b.WriteString("- 大小：")
	b.WriteString(fmt.Sprintf("%d bytes", len(raw)))
	b.WriteString("\n")
	if width > 0 && height > 0 {
		b.WriteString("- 分辨率：")
		b.WriteString(fmt.Sprintf("%d x %d", width, height))
		b.WriteString("\n")
	}
	b.WriteString("\n## 待识别内容\n\n")
	b.WriteString("_OCR 完成后会把识别文本更新到这里。_\n")
	return b.String()
}

func mobileDraftOriginalOnlyMarkdown(filename string, raw []byte) string {
	return mobileDraftOriginalOnlyMarkdownSize(filename, len(raw))
}

func mobileDraftOriginalOnlyMarkdownSize(filename string, size int) string {
	title := mobileUploadTitle(filename)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		ext = "bin"
	}
	if size < 0 {
		size = 0
	}
	return fmt.Sprintf(
		"# %s\n\n原始文件已保存到文稿库，可预览元数据或分享原件。\n\n- 文件名：%s\n- 类型：%s\n- 大小：%d bytes\n\n_正文提取有限或尚未完成；AI 处理以原件为准。_\n",
		title,
		filepath.Base(filename),
		ext,
		size,
	)
}
func mobileImageDimensions(format string, raw []byte) (int, int) {
	switch format {
	case "png":
		if len(raw) >= 24 && bytes.Equal(raw[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
			width := int(raw[16])<<24 | int(raw[17])<<16 | int(raw[18])<<8 | int(raw[19])
			height := int(raw[20])<<24 | int(raw[21])<<16 | int(raw[22])<<8 | int(raw[23])
			return width, height
		}
	case "jpg", "jpeg":
		for i := 2; i+9 < len(raw); {
			if raw[i] != 0xFF {
				i++
				continue
			}
			marker := raw[i+1]
			if marker == 0xC0 || marker == 0xC2 {
				height := int(raw[i+5])<<8 | int(raw[i+6])
				width := int(raw[i+7])<<8 | int(raw[i+8])
				return width, height
			}
			if i+3 >= len(raw) {
				break
			}
			size := int(raw[i+2])<<8 | int(raw[i+3])
			if size < 2 {
				break
			}
			i += 2 + size
		}
	}
	return 0, 0
}

// mobilePDFPreviewMaxPages caps extract cost for mobile draft preview / AI context.
// Full-document RAG still uses knowledge's unrestricted path.
const mobilePDFPreviewMaxPages = 12

// mobilePDFNaiveScanMaxBytes caps the legacy literal/hex scrape. Scanning multi-MB
// academic PDFs as a Go string is expensive and almost never yields usable text.
const mobilePDFNaiveScanMaxBytes = 2 << 20 // 2 MiB

func mobilePDFExtractText(raw []byte) string {
	// Prefer GoPDF2 (same stack as knowledge import) for compressed / modern PDFs.
	// Cap pages so a 100-page paper does not block upload/get for long.
	if text := mobilePDFExtractTextGoPDF(raw); mobilePDFExtractedTextIsUsable(text) {
		return text
	}
	// Fallback: lightweight scan of uncompressed literal/hex strings
	// (covers simple PDFs produced by mobileRenderDraftPDF).
	scan := raw
	if len(scan) > mobilePDFNaiveScanMaxBytes {
		scan = scan[:mobilePDFNaiveScanMaxBytes]
	}
	// Compressed streams almost never expose plain Tj operators outside content;
	// skip naive scrape for large FlateDecode PDFs after GoPDF already failed.
	if len(raw) > 512<<10 && bytes.Contains(scan, []byte("/FlateDecode")) {
		return ""
	}
	text := string(scan)
	var out []string
	out = append(out, mobilePDFExtractHexStrings(text)...)
	out = append(out, mobilePDFExtractLiteralStrings(text)...)
	joined := strings.Join(mobileCompactTextLines(out), "\n")
	if mobilePDFExtractedTextIsUsable(joined) {
		return joined
	}
	return ""
}

func mobilePDFExtractTextGoPDF(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	n, err := gopdf.GetSourcePDFPageCountFromBytes(raw)
	if err == nil && n > 0 {
		// Small PDFs: one-shot all-pages extract (cheaper than N page opens).
		if n <= mobilePDFPreviewMaxPages {
			text, err2 := gopdf.ExtractAllPagesText(raw)
			if err2 == nil {
				return strings.TrimSpace(text)
			}
		}
		// Large PDFs: only the first N pages for preview latency.
		limit := mobilePDFPreviewMaxPages
		if n < limit {
			limit = n
		}
		var b strings.Builder
		for i := 0; i < limit; i++ {
			pageText, pageErr := gopdf.ExtractPageText(raw, i)
			if pageErr != nil {
				continue
			}
			pageText = strings.TrimSpace(pageText)
			if pageText == "" {
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(pageText)
		}
		text := strings.TrimSpace(b.String())
		if text != "" {
			if n > mobilePDFPreviewMaxPages {
				text += fmt.Sprintf("\n\n_…已截取前 %d 页（共 %d 页）；完整内容请打开原件。_\n", mobilePDFPreviewMaxPages, n)
			}
			return text
		}
		// Page count worked but no text — scanned/image PDF; skip full re-parse.
		return ""
	}
	// Page count failed — all-pages helper only for small PDFs (avoid multi-second stalls).
	if len(raw) > mobilePDFNaiveScanMaxBytes {
		return ""
	}
	text, err := gopdf.ExtractAllPagesText(raw)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// mobilePDFExtractedTextIsUsable rejects empty or binary-like scrape noise so the
// UI falls back to original-file metadata instead of showing PDF operator garbage.
// Used only to accept/reject *extraction results*, not to judge user-written drafts.
func mobilePDFExtractedTextIsUsable(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	letters, spaces, digits, other, nonSpace := mobileCountTextClasses(text)
	total := letters + spaces + digits + other
	if total < 8 || letters < 12 {
		return false
	}
	if nonSpace == 0 {
		return false
	}
	// Real prose is mostly letters; PDF scrape noise is symbols/digits.
	if float64(letters)/float64(nonSpace) < 0.4 {
		return false
	}
	if float64(other)/float64(nonSpace) > 0.45 {
		return false
	}

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return false
	}
	// Single-block extracts (no newlines) only need letter ratio.
	if len(lines) == 1 {
		return true
	}
	goodLines := 0
	garbageLines := 0
	for _, line := range lines {
		if mobilePDFLineLooksLikeProse(line) {
			goodLines++
		} else {
			garbageLines++
		}
	}
	if goodLines == 0 {
		return false
	}
	// Typical bad scrape: dozens of 1–3 char symbol lines and almost no prose.
	if garbageLines >= goodLines*3 && goodLines < 6 {
		return false
	}
	return true
}

func mobilePDFExtractHexStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '<' || (i+1 < len(text) && text[i+1] == '<') {
			continue
		}
		end := strings.IndexByte(text[i+1:], '>')
		if end < 0 {
			break
		}
		body := text[i+1 : i+1+end]
		if decoded := mobileDecodePDFHexString(body); decoded != "" {
			out = append(out, decoded)
		}
		i = i + end + 1
	}
	return out
}

func mobileDecodePDFHexString(value string) string {
	var hexDigits []byte
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			hexDigits = append(hexDigits, c)
		}
	}
	if len(hexDigits) < 2 {
		return ""
	}
	if len(hexDigits)%2 == 1 {
		hexDigits = append(hexDigits, '0')
	}
	data := make([]byte, 0, len(hexDigits)/2)
	for i := 0; i+1 < len(hexDigits); i += 2 {
		hi, okHi := mobileHexNibble(hexDigits[i])
		lo, okLo := mobileHexNibble(hexDigits[i+1])
		if !okHi || !okLo {
			return ""
		}
		data = append(data, byte(hi<<4|lo))
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		var runes []rune
		for i := 2; i+1 < len(data); i += 2 {
			runes = append(runes, rune(data[i])<<8|rune(data[i+1]))
		}
		return strings.TrimSpace(string(runes))
	}
	if utf8.Valid(data) {
		return strings.TrimSpace(string(data))
	}
	return ""
}

func mobileHexNibble(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	default:
		return 0, false
	}
}

func mobilePDFExtractLiteralStrings(text string) []string {
	var out []string
	for i := 0; i < len(text); i++ {
		if text[i] != '(' {
			continue
		}
		decoded, next, ok := mobileReadPDFLiteralString(text, i)
		if ok && strings.TrimSpace(decoded) != "" {
			out = append(out, decoded)
		}
		i = next
	}
	return out
}

func mobileReadPDFLiteralString(text string, start int) (string, int, bool) {
	var b strings.Builder
	escaped := false
	depth := 0
	for i := start + 1; i < len(text); i++ {
		c := text[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			default:
				b.WriteByte(c)
			}
			escaped = false
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '(':
			depth++
			b.WriteByte(c)
		case ')':
			if depth == 0 {
				return strings.TrimSpace(b.String()), i, true
			}
			depth--
			b.WriteByte(c)
		default:
			if c >= 0x20 || c == '\n' || c == '\r' || c == '\t' {
				b.WriteByte(c)
			}
		}
	}
	return "", start, false
}

func mobileCompactTextLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	seen := make(map[string]bool)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || len(line) > 1000 || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}

func mobileXLSXSharedStrings(zr *zip.Reader) []string {
	raw := mobileZipReadFile(zr, "xl/sharedStrings.xml")
	if len(raw) == 0 {
		return nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var values []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "si" {
			continue
		}
		values = append(values, mobileXLSXInlineText(decoder, start.Name.Local))
	}
	return values
}

func mobileXLSXSheets(zr *zip.Reader, sharedStrings []string) [][][]string {
	var sheets [][][]string
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "xl/worksheets/sheet") || !strings.HasSuffix(file.Name, ".xml") {
			continue
		}
		raw := mobileZipReadFile(zr, file.Name)
		rows := mobileXLSXRows(raw, sharedStrings)
		if len(rows) > 0 {
			sheets = append(sheets, rows)
		}
	}
	return sheets
}

func mobileXLSXRows(raw []byte, sharedStrings []string) [][]string {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	var rows [][]string
	var current []string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "row":
				current = nil
			case "c":
				current = append(current, mobileXLSXCellText(decoder, item, sharedStrings))
			}
		case xml.EndElement:
			if item.Name.Local == "row" {
				row := mobileTrimEmptyTrailingCells(current)
				if len(row) > 0 {
					rows = append(rows, row)
				}
				current = nil
			}
		}
	}
	return rows
}

func mobileXLSXCellText(decoder *xml.Decoder, cell xml.StartElement, sharedStrings []string) string {
	cellType := ""
	for _, attr := range cell.Attr {
		if attr.Name.Local == "t" {
			cellType = attr.Value
			break
		}
	}
	var value string
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			switch item.Name.Local {
			case "v":
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					value = strings.TrimSpace(text)
				}
			case "is":
				value = strings.TrimSpace(mobileXLSXInlineText(decoder, item.Name.Local))
			}
		case xml.EndElement:
			if item.Name.Local == "c" {
				if cellType == "s" {
					if index, ok := mobileParseInt(value); ok && index >= 0 && index < len(sharedStrings) {
						return sharedStrings[index]
					}
				}
				return value
			}
		}
	}
	return value
}

func mobileXLSXInlineText(decoder *xml.Decoder, endElement string) string {
	var b strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "t" {
				var text string
				if err := decoder.DecodeElement(&text, &item); err == nil {
					b.WriteString(text)
				}
			}
		case xml.EndElement:
			if item.Name.Local == endElement {
				return strings.TrimSpace(b.String())
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func mobileTrimEmptyTrailingCells(cells []string) []string {
	end := len(cells)
	for end > 0 && strings.TrimSpace(cells[end-1]) == "" {
		end--
	}
	return cells[:end]
}

func mobileZipReadFile(zr *zip.Reader, name string) []byte {
	for _, file := range zr.File {
		if file.Name != name {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil
		}
		raw, err := io.ReadAll(io.LimitReader(rc, mobileDocumentUploadMaxBytes))
		_ = rc.Close()
		if err != nil {
			return nil
		}
		return raw
	}
	return nil
}

func mobileUploadTitle(filename string) string {
	title := strings.TrimSpace(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
	if title == "" {
		return "导入文档"
	}
	return title
}

func mobileParseInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// MobileDocumentUploadHandler validates emergency document upload metadata and
// returns a parse task contract. Lightweight text-like files are converted into
// drafts immediately; heavier Office/PDF/image parsing remains queued for the
// document pipeline.
func MobileDocumentUploadHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		if err := r.ParseMultipartForm(mobileDocumentUploadMaxBytes); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "file upload must be multipart/form-data")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "file is required")
			return
		}
		defer file.Close()
		// Prefer explicit form field for original display name (non-ASCII safe).
		name := strings.TrimSpace(r.FormValue("filename"))
		if name == "" {
			name = strings.TrimSpace(header.Filename)
		}
		name = filepath.Base(name)
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "upload"
		}
		body, err := io.ReadAll(io.LimitReader(file, mobileDocumentUploadMaxBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "UPLOAD_READ_FAILED", "failed to read uploaded file")
			return
		}
		if len(body) > mobileDocumentUploadMaxBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "file exceeds mobile upload limit")
			return
		}
		if err := mobileCheckDocumentQuotaForPrincipal(r.Context(), principal, int64(len(body))); err != nil {
			writeError(w, http.StatusInsufficientStorage, "DOCUMENT_QUOTA_EXCEEDED", "document storage quota exceeded")
			return
		}
		now := time.Now().UTC()
		taskID := fmt.Sprintf("mobparse_%d", now.UnixNano())
		blobPath, blobSize, blobMem := mobilePersistDocumentOriginal(principal.UserID, "upload", taskID, body)
		record := mobileDocumentUploadRecord{
			TaskID:      taskID,
			OwnerID:     principal.UserID,
			Filename:    name,
			ContentType: strings.TrimSpace(header.Header.Get("Content-Type")),
			Status:      "queued",
			Message:     "已上传，等待文档解析管线处理。",
			SourcePath:  blobPath,
			SourceSize:  blobSize,
			SourceBytes: blobMem,
			UploadedAt:  now,
			UpdatedAt:   now,
		}
		contentType := strings.TrimSpace(header.Header.Get("Content-Type"))
		record.ContentType = contentType

		// Always keep the original file on the draft (source of truth for share/AI).
		attachOriginal := func(draft *mobileDocumentDraftRecord) {
			mobileAttachDraftOriginal(draft, name, contentType, body)
		}

		if mobileUploadedFileIsImmediateDraft(name) {
			if markdown, ok := mobileDraftMarkdownFromUpload(name, body); ok {
				draft := mobileDocumentDraftRecord{
					ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
					OwnerID:   principal.UserID,
					Title:     strings.TrimSpace(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))),
					Template:  "report",
					Markdown:  markdown,
					UpdatedAt: now,
				}
				if draft.Title == "" {
					draft.Title = name
				}
				attachOriginal(&draft)
				record.Status = "ready"
				record.DraftID = draft.ID
				record.Message = "文件已导入（保留原件，并生成可读正文）。"
				payload := mobileStoreDraftAndUpload(draft, &record, true)
				mobilePersistState()
				mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
				writeJSON(w, http.StatusAccepted, payload)
				return
			}
			// Office/binary that failed extract: still store original as a draft.
			draft := mobileDocumentDraftRecord{
				ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
				OwnerID:   principal.UserID,
				Title:     mobileUploadTitle(name),
				Template:  "report",
				Markdown:  mobileDraftOriginalOnlyMarkdown(name, body),
				UpdatedAt: now,
			}
			attachOriginal(&draft)
			record.Status = "ready"
			record.DraftID = draft.ID
			record.Message = "原件已保存到文稿库（正文提取有限，可分享原文件）。"
			payload := mobileStoreDraftAndUpload(draft, &record, true)
			mobilePersistState()
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
			writeJSON(w, http.StatusAccepted, payload)
			return
		}
		if mobileUploadedFileIsImage(name) {
			draft := mobileDocumentDraftRecord{
				ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
				OwnerID:   principal.UserID,
				Title:     mobileUploadTitle(name),
				Template:  "report",
				Markdown:  mobileDraftMarkdownFromImage(name, body),
				UpdatedAt: now,
			}
			attachOriginal(&draft)
			record.Status = "needs_ocr"
			record.DraftID = draft.ID
			record.Message = "图片原件已保存，等待 OCR/视觉识别（可先分享原图）。"
			// Keep upload source for OCR workers until ready.
			payload := mobileStoreDraftAndUpload(draft, &record, false)
			mobilePersistState()
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
			writeJSON(w, http.StatusAccepted, payload)
			return
		}
		// Unknown binary: still keep original as shareable draft.
		draft := mobileDocumentDraftRecord{
			ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
			OwnerID:   principal.UserID,
			Title:     mobileUploadTitle(name),
			Template:  "report",
			Markdown:  mobileDraftOriginalOnlyMarkdown(name, body),
			UpdatedAt: now,
		}
		attachOriginal(&draft)
		record.Status = "ready"
		record.DraftID = draft.ID
		record.Message = "原件已保存到文稿库。"
		payload := mobileStoreDraftAndUpload(draft, &record, true)
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusAccepted, payload)
	}
}

// mobileStoreDraftAndUpload writes draft+upload under lock. When releaseUploadOriginal
// is true and draft has an original, the upload-side blob is freed.
func mobileStoreDraftAndUpload(draft mobileDocumentDraftRecord, record *mobileDocumentUploadRecord, releaseUploadOriginal bool) map[string]any {
	mobileDocuments.Lock()
	defer mobileDocuments.Unlock()
	mobileDocuments.drafts[draft.ID] = draft
	if releaseUploadOriginal {
		mobileReleaseUploadOriginalAfterReady(record)
	}
	mobileDocuments.uploads[record.TaskID] = *record
	return mobileDocumentUploadPayload(*record)
}

// MobileDocumentUploadStatusHandler returns the current upload parse task.
func MobileDocumentUploadStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		ownerID := mobilePrincipalOwnerID(principal)
		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, ok := mobileDocuments.uploads[taskID]
		stateChanged := false
		if ok && record.OwnerID == ownerID {
			// Mobile polls status even when no worker is claiming. Reclaim this
			// task if the worker claim timed out so the UI leaves in_progress.
			if next, reclaimed := mobileReclaimStaleDocumentUploadIfNeeded(record, now); reclaimed {
				record = next
				mobileDocuments.uploads[taskID] = record
				stateChanged = true
			}
			var pipelineChanged bool
			record, pipelineChanged = mobileApplyUploadPipelineResult(record, now)
			if pipelineChanged {
				mobileDocuments.uploads[taskID] = record
				stateChanged = true
			}
		}
		payload, repaired := mobileDocumentUploadPayloadTracked(record)
		mobileDocuments.Unlock()
		if !ok || record.OwnerID != ownerID {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload task not found")
			return
		}
		if stateChanged || repaired {
			mobilePersistState()
		}
		if stateChanged {
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileDocumentUploadSourceHandler streams the original upload to the claimed
// official worker so OCR/Office parsing can run outside the phone.
// If the upload-side blob was already released, falls back to the linked draft original.
func MobileDocumentUploadSourceHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDocuments.Lock()
		record, exists := mobileDocuments.uploads[taskID]
		repaired := false
		if exists && mobileUploadRepairSourceMeta(&record) {
			mobileDocuments.uploads[taskID] = record
			repaired = true
		}
		path := record.SourcePath
		var mem []byte
		// Prefer disk path; only clone memory when there is no blob path.
		if strings.TrimSpace(path) == "" && len(record.SourceBytes) > 0 {
			mem = append([]byte(nil), record.SourceBytes...)
		}
		contentType := record.ContentType
		filename := record.Filename
		owner := record.OwnerID
		claimedBy := record.ClaimedBy
		hasSrc := mobileUploadHasSource(record)
		// Fallback: draft original (source of truth after upload-side release).
		if exists && !hasSrc {
			if draft, draftRepaired := mobileUploadDraftOriginal(record); draft != nil {
				path = draft.SourcePath
				mem = nil
				if strings.TrimSpace(path) == "" && len(draft.SourceBytes) > 0 {
					mem = append([]byte(nil), draft.SourceBytes...)
				}
				if strings.TrimSpace(draft.SourceContentType) != "" {
					contentType = draft.SourceContentType
				}
				if strings.TrimSpace(draft.SourceFilename) != "" {
					filename = draft.SourceFilename
				}
				hasSrc = true
				if draftRepaired {
					repaired = true
				}
			} else if draftRepaired {
				repaired = true
			}
		}
		mobileDocuments.Unlock()
		if repaired {
			mobilePersistState()
		}
		if !exists || owner != principal.UserID || !hasSrc {
			writeError(w, http.StatusNotFound, "UPLOAD_SOURCE_NOT_FOUND", "upload source not found")
			return
		}
		if strings.TrimSpace(claimedBy) != "" && claimedBy != principal.MachineID {
			writeError(w, http.StatusForbidden, "UPLOAD_CLAIMED_BY_OTHER_WORKER", "upload task is claimed by another worker")
			return
		}
		if !mobileWriteOriginalHTTP(w, contentType, filename, mem, path) {
			// Stream failed. Clear meta only when the attempted path is confirmed
			// missing under an online store — never during store outages.
			failedPath := strings.TrimSpace(path)
			dirty := false
			if mobileShouldClearSourceMetaAfterStreamFail(failedPath) {
				mobileDocuments.Lock()
				if rec, ok := mobileDocuments.uploads[taskID]; ok && rec.OwnerID == owner {
					if len(rec.SourceBytes) == 0 {
						upPath := strings.TrimSpace(rec.SourcePath)
						if upPath != "" && (failedPath == "" || upPath == failedPath) {
							rec.SourcePath = ""
							rec.SourceSize = 0
							mobileDocuments.uploads[taskID] = rec
							dirty = true
						} else if upPath == "" && rec.SourceSize != 0 {
							rec.SourceSize = 0
							mobileDocuments.uploads[taskID] = rec
							dirty = true
						}
					}
					if draftID := strings.TrimSpace(rec.DraftID); draftID != "" && failedPath != "" {
						if draft, ok := mobileDocuments.drafts[draftID]; ok && draft.OwnerID == owner && len(draft.SourceBytes) == 0 {
							if strings.TrimSpace(draft.SourcePath) == failedPath {
								draft.SourcePath = ""
								draft.SourceSize = 0
								mobileDocuments.drafts[draftID] = draft
								dirty = true
							}
						}
					}
				}
				mobileDocuments.Unlock()
			}
			if dirty {
				mobilePersistState()
			}
			writeError(w, http.StatusNotFound, "UPLOAD_SOURCE_NOT_FOUND", "upload source not found")
		}
	}
}

// MobileDocumentUploadClaimHandler lets an authenticated official worker claim
// one pending mobile document parse task for its user.
func MobileDocumentUploadClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		claimKind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
		if claimKind == "" {
			claimKind = "all"
		}
		if claimKind != "all" && claimKind != "document" && claimKind != "ocr" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "kind must be one of all, document, ocr")
			return
		}
		now := time.Now().UTC()
		var claimed mobileDocumentUploadRecord
		reclaimed := 0
		stateDirty := false
		// Cache once per claim pass — avoid N directory stats while scanning uploads.
		storeReady := mobileDocumentBlobStoreReady()
		// Terminal failures (ghost originals) to push over realtime after unlock.
		var terminalEvents []map[string]any
		mobileDocuments.Lock()
		reclaimed = mobileReclaimStaleDocumentUploadClaims(now)
		if reclaimed > 0 {
			stateDirty = true
		}
		// Opportunistic: free upload-side originals for terminal tasks (migration
		// for records created before release-on-ready).
		for taskID, record := range mobileDocuments.uploads {
			if record.OwnerID != principal.UserID {
				continue
			}
			if record.Status != "ready" && record.Status != "failed" {
				continue
			}
			if mobileReleaseUploadOriginalWhenDraftOwns(&record) {
				mobileDocuments.uploads[taskID] = record
				stateDirty = true
			}
		}
		for taskID, record := range mobileDocuments.uploads {
			if record.OwnerID != principal.UserID {
				continue
			}
			if record.Status != "queued" && record.Status != "needs_ocr" {
				continue
			}
			if claimKind == "document" && record.Status != "queued" {
				continue
			}
			if claimKind == "ocr" && record.Status != "needs_ocr" {
				continue
			}
			if strings.TrimSpace(record.ClaimedBy) != "" && record.ClaimedBy != principal.MachineID {
				continue
			}
			// Worker needs a downloadable original (upload blob or draft fallback).
			avail, repaired := mobileUploadSourceAvailable(record)
			if repaired {
				stateDirty = true
				// Re-read after repair so we do not re-persist ghost SourcePath.
				if refreshed, ok := mobileDocuments.uploads[taskID]; ok {
					record = refreshed
				}
			}
			if !avail {
				// Store online + no source: OCR/parse can never complete. Terminal-fail
				// so Mobile stops polling instead of leaving needs_ocr forever.
				// When the store is offline, leave the task for a later claim retry.
				if storeReady &&
					(record.Status == "queued" || record.Status == "needs_ocr") {
					record.Status = "failed"
					record.Message = "原件不可用，无法完成远程解析。"
					record.ClaimedBy = ""
					record.UpdatedAt = now
					mobileDocuments.uploads[taskID] = record
					stateDirty = true
					terminalEvents = append(terminalEvents, mobileDocumentUploadPayload(record))
				}
				continue
			}
			record.Status = "in_progress"
			record.ClaimedBy = principal.MachineID
			record.Message = "远程解析 worker 正在处理移动端文档。"
			record.UpdatedAt = now
			mobileDocuments.uploads[taskID] = record
			claimed = record
			break
		}
		payload := mobileDocumentUploadPayload(claimed)
		mobileDocuments.Unlock()
		// Persist before realtime so a crash cannot drop terminal-fail / claim state
		// after the client already observed the event.
		if stateDirty || claimed.TaskID != "" {
			mobilePersistState()
		}
		// Notify Mobile when ghost tasks were terminal-failed during this claim pass.
		for _, ev := range terminalEvents {
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", ev))
		}
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "no_task",
				"task":   nil,
			})
			return
		}
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "claimed",
			"task":   payload,
		})
	}
}

// MobileDocumentUploadResultHandler lets official Hub workers or remote digital
// employees write OCR/vision parse results back into a mobile upload task.
func MobileDocumentUploadResultHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileDocumentUploadResultRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		markdown := strings.TrimSpace(req.Markdown)
		message := strings.TrimSpace(req.Message)
		errText := strings.TrimSpace(req.Error)
		if status == "" {
			if errText != "" {
				status = "failed"
			} else if markdown != "" {
				status = "ready"
			}
		}
		if status != "ready" && status != "failed" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status must be ready or failed")
			return
		}
		if status == "ready" && markdown == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "markdown is required when status is ready")
			return
		}
		if status == "failed" && errText == "" {
			if message != "" {
				errText = message
			} else {
				errText = "OCR/视觉识别失败。"
			}
		}

		now := time.Now().UTC()
		mobileDocuments.Lock()
		record, exists := mobileDocuments.uploads[taskID]
		if exists && record.OwnerID == principal.UserID {
			if strings.TrimSpace(record.ClaimedBy) != "" && record.ClaimedBy != principal.MachineID {
				mobileDocuments.Unlock()
				writeError(w, http.StatusForbidden, "UPLOAD_CLAIMED_BY_OTHER_WORKER", "upload task is claimed by another worker")
				return
			}
			if record.Status == "ready" || record.Status == "failed" {
				mobileDocuments.Unlock()
				writeError(w, http.StatusConflict, "UPLOAD_ALREADY_FINISHED", "upload task already finished")
				return
			}
			record.OCRMarkdown = markdown
			record.OCRMessage = message
			record.OCRError = errText
			// Intermediate status so mobileApplyUploadPipelineResult can promote to ready/failed.
			if record.Status == "" || record.Status == "queued" || record.Status == "in_progress" {
				record.Status = "needs_ocr"
			}
			if markdown != "" {
				draft, hasDraft := mobileDocuments.drafts[record.DraftID]
				if record.DraftID == "" || !hasDraft || draft.OwnerID != record.OwnerID {
					draft = mobileDocumentDraftRecord{
						ID:        fmt.Sprintf("mobdoc_%d", now.UnixNano()),
						OwnerID:   record.OwnerID,
						Title:     mobileUploadTitle(record.Filename),
						Template:  "report",
						Markdown:  markdown,
						UpdatedAt: now,
					}
					// Preserve original from upload when creating draft late.
					if src := mobileUploadLoadSourceBytes(&record); len(src) > 0 {
						mobileAttachDraftOriginal(&draft, record.Filename, record.ContentType, src)
					}
					record.DraftID = draft.ID
					mobileDocuments.drafts[draft.ID] = draft
				} else {
					// Keep draft body current; never drop the original attachment.
					// Repair ghost SourcePath before deciding whether to re-attach.
					_ = mobileDraftRepairSourceMeta(&draft)
					draft.Markdown = markdown
					draft.UpdatedAt = now
					if !mobileDraftHasOriginal(draft) {
						if src := mobileUploadLoadSourceBytes(&record); len(src) > 0 {
							mobileAttachDraftOriginal(&draft, record.Filename, record.ContentType, src)
						}
					}
					mobileDocuments.drafts[draft.ID] = draft
				}
			}
			record.UpdatedAt = now
			record, _ = mobileApplyUploadPipelineResult(record, now)
			mobileDocuments.uploads[taskID] = record
		}
		payload := mobileDocumentUploadPayload(record)
		mobileDocuments.Unlock()
		if !exists || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "UPLOAD_NOT_FOUND", "upload task not found")
			return
		}
		mobilePersistState()
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDocumentTaskEvent("document_task", payload))
		writeJSON(w, http.StatusOK, payload)
	}
}

func mobileSSHAnalysisPayload(output string) map[string]any {
	lower := strings.ToLower(output)
	summary := "已读取终端输出。"
	recommendation := "先确认当前目录、服务名和影响范围，再手动执行排查命令。高风险命令不要直接复制执行。"
	commandDraft := ""
	switch {
	case strings.Contains(lower, "permission denied"):
		summary = "输出显示权限被拒绝。"
		recommendation = "检查当前用户、目标文件权限、sudo 策略和 SSH 密钥是否匹配。"
		commandDraft = "id && ls -la"
	case strings.Contains(lower, "no space left") || strings.Contains(lower, "disk full"):
		summary = "输出显示磁盘空间不足。"
		recommendation = "先查看磁盘和大目录占用，再决定是否清理日志或扩容。"
		commandDraft = "df -h && du -sh ./* 2>/dev/null | sort -h"
	case strings.Contains(lower, "connection refused"):
		summary = "输出显示连接被拒绝。"
		recommendation = "检查服务是否监听、端口是否正确，以及防火墙或安全组规则。"
		commandDraft = "ss -lntp"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "timed out"):
		summary = "输出显示连接超时。"
		recommendation = "优先检查网络连通性、DNS、路由、防火墙和上游服务状态。"
		commandDraft = "ping -c 4 <host> && traceroute <host>"
	case strings.Contains(lower, "nginx"):
		summary = "输出包含 nginx 相关信息。"
		recommendation = "先校验配置，再查看服务状态和最近错误日志。"
		commandDraft = "nginx -t && systemctl status nginx --no-pager"
	case strings.Contains(lower, "failed") || strings.Contains(lower, "error"):
		summary = "输出包含失败或错误信息。"
		recommendation = "先定位具体服务和最近日志，避免直接重启或删除数据。"
		commandDraft = "systemctl --failed && journalctl -xe --no-pager | tail -n 80"
	}
	return map[string]any{
		"summary":        summary,
		"recommendation": recommendation,
		"command_draft":  commandDraft,
		"status":         "ready",
	}
}

// MobileSSHAnalyzeHandler gives the mobile SSH surface a lightweight analysis
// without allowing the server or AI to execute commands.
func MobileSSHAnalyzeHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		if _, err := authenticateViewerRequest(r, identity); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		var req mobileSSHAnalyzeRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		output := strings.TrimSpace(req.Output)
		if output == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "output is required")
			return
		}
		payload := mobileSSHAnalysisPayload(output)
		if backendSessionID := sanitizeMobileServerProfileText(req.BackendSessionID, 160); backendSessionID != "" {
			payload["backend_session_id"] = backendSessionID
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileServerProfilesHandler lets mobile viewers list tenant-scoped,
// sanitized SSH profile metadata. Desktop agents may publish hosts they can
// service; mobile viewers may also upsert their own profiles for hub_exec /
// AI assistant SSH (no passwords in this API — secrets use /ssh/vault).
func MobileServerProfilesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			principal, err := authenticateViewerRequest(r, identity)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
				return
			}
			ownerID := mobilePrincipalOwnerID(principal)
			profiles := make([]map[string]any, 0)
			mobileServerProfiles.Lock()
			for _, record := range mobileServerProfiles.profiles {
				if record.TenantID != principal.TenantID {
					continue
				}
				if record.OwnerID != ownerID && record.OwnerID != principal.UserID {
					continue
				}
				profiles = append(profiles, mobileServerProfileResponse(record))
			}
			mobileServerProfiles.Unlock()
			sort.Slice(profiles, func(i, j int) bool {
				leftName, _ := profiles[i]["name"].(string)
				rightName, _ := profiles[j]["name"].(string)
				if leftName == rightName {
					leftID, _ := profiles[i]["id"].(string)
					rightID, _ := profiles[j]["id"].(string)
					return leftID < rightID
				}
				return strings.ToLower(leftName) < strings.ToLower(rightName)
			})
			writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
		case http.MethodPut, http.MethodPost:
			// Desktop worker = machine token + X-Machine-ID. Mobile viewer
			// uses the same route to upsert profiles for hub_exec / AI ssh.
			// Note: authenticateMobileDigitalEmployeeWorker falls back to
			// viewer, so we detect real workers via non-empty MachineID.
			worker, workerErr := authenticateMobileDigitalEmployeeWorker(r, identity)
			var tenantID, ownerID, sourceMachineID string
			viewerUpsert := false
			if workerErr == nil && strings.TrimSpace(worker.MachineID) != "" {
				tenantID = worker.TenantID
				ownerID = strings.TrimSpace(worker.UserID)
				sourceMachineID = strings.TrimSpace(worker.MachineID)
			} else {
				viewer, err := authenticateViewerRequest(r, identity)
				if err != nil {
					writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer or worker authentication failed")
					return
				}
				viewerUpsert = true
				tenantID = viewer.TenantID
				ownerID = mobilePrincipalOwnerID(viewer)
				sourceMachineID = "mobile-viewer"
			}
			var req mobileServerProfileRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			now := time.Now().UTC()
			next := make(map[string]mobileServerProfileRecord)
			for _, item := range req.Profiles {
				profileID := sanitizeMobileServerProfileText(item.ID, 128)
				host := sanitizeMobileServerProfileText(item.Host, 255)
				username := sanitizeMobileServerProfileText(item.Username, 128)
				if profileID == "" || host == "" || username == "" {
					continue
				}
				port := item.Port
				if port == 0 {
					port = 22
				}
				if port < 1 || port > 65535 {
					continue
				}
				name := sanitizeMobileServerProfileText(item.Name, 128)
				if name == "" {
					name = profileID
				}
				key := tenantID + "\x00" + ownerID + "\x00" + sourceMachineID + "\x00" + profileID
				next[key] = mobileServerProfileRecord{
					ProfileID:       profileID,
					TenantID:        tenantID,
					OwnerID:         ownerID,
					SourceMachineID: sourceMachineID,
					Name:            name,
					Host:            host,
					Port:            port,
					Username:        username,
					AuthMode:        normalizeMobileServerProfileAuthMode(item.AuthMode),
					Tag:             sanitizeMobileServerProfileText(item.Tag, 64),
					Note:            sanitizeMobileServerProfileText(item.Note, 256),
					UpdatedAt:       now,
				}
			}
			mobileServerProfiles.Lock()
			// Worker replace-by-source; mobile viewer upserts without wiping desktop publishes.
			if !viewerUpsert {
				for key, record := range mobileServerProfiles.profiles {
					if record.TenantID == tenantID && record.OwnerID == ownerID && record.SourceMachineID == sourceMachineID {
						delete(mobileServerProfiles.profiles, key)
					}
				}
			}
			for key, record := range next {
				mobileServerProfiles.profiles[key] = record
			}
			mobileServerProfiles.Unlock()
			go mobilePersistState()
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "ok",
				"count":  len(next),
				"source": map[bool]string{true: "mobile_viewer", false: "desktop_worker"}[viewerUpsert],
			})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET, POST, or PUT")
		}
	}
}

// MobileBackendSSHSessionsHandler lists or creates backend-managed SSH
// sessions. Mobile submits control intent; an authorized remote agent is
// responsible for attaching this record to corelib/remote.SSHSessionManager.
func MobileBackendSSHSessionsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		switch r.Method {
		case http.MethodGet:
			sessions := make([]map[string]any, 0)
			mobileBackendSSHSessions.Lock()
			for _, record := range mobileBackendSSHSessions.sessions {
				if record.OwnerID != principal.UserID || record.TenantID != principal.TenantID {
					continue
				}
				sessions = append(sessions, mobileBackendSSHSessionPayload(record))
			}
			mobileBackendSSHSessions.Unlock()
			sort.Slice(sessions, func(i, j int) bool {
				left, _ := sessions[i]["updated_at"].(string)
				right, _ := sessions[j]["updated_at"].(string)
				return left > right
			})
			writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
		case http.MethodPost:
			var req mobileBackendSSHSessionRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			serverProfileID := strings.TrimSpace(req.ServerProfileID)
			if serverProfileID == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "server_profile_id is required")
				return
			}
			execMode := mobileNormalizeSSHExecMode(req.ExecMode)
			now := time.Now().UTC()
			record := mobileBackendSSHSessionRecord{
				SessionID:       fmt.Sprintf("mobssh_%d", now.UnixNano()),
				TenantID:        principal.TenantID,
				OwnerID:         principal.UserID,
				ServerProfileID: serverProfileID,
				ExecMode:        execMode,
				Status:          "queued",
				State:           "pending_agent",
				Message:         "Backend SSH session request queued for an authorized MaClaw agent.",
				RecentOutput:    "Waiting for remote MaClaw agent to create or attach the SSH session.",
				CreatedAt:       now,
				UpdatedAt:       now,
			}
			if execMode == mobileSSHExecHub {
				if err := mobileStartHubSSHSession(&record, principal.TenantID, principal.UserID); err != nil {
					writeError(w, http.StatusBadRequest, "HUB_SSH_UNAVAILABLE", err.Error())
					return
				}
			}
			mobileBackendSSHSessions.Lock()
			mobileBackendSSHSessions.sessions[record.SessionID] = record
			mobileBackendSSHSessions.Unlock()
			payload := mobileBackendSSHSessionPayload(record)
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
			status := http.StatusAccepted
			if execMode == mobileSSHExecHub {
				status = http.StatusOK
			}
			writeJSON(w, status, map[string]any{"session": payload})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		}
	}
}

func MobileBackendSSHSessionAttachHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileBackendSSHSessionHubOrDesktopTransition(
		identity,
		http.MethodPost,
		"attach_requested",
		"attaching",
		"Attach request queued for the backend SSH session.",
		mobileHubSSHHandleAttach,
	)
}

func MobileBackendSSHSessionReconnectHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileBackendSSHSessionHubOrDesktopTransition(
		identity,
		http.MethodPost,
		"reconnect_requested",
		"reconnecting",
		"Reconnect request queued for the backend SSH session.",
		mobileHubSSHHandleReconnect,
	)
}

func MobileBackendSSHSessionInterruptHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileBackendSSHSessionHubOrDesktopTransition(
		identity,
		http.MethodPost,
		"interrupt_requested",
		"interrupting",
		"Interrupt request queued for the backend SSH session.",
		mobileHubSSHHandleInterrupt,
	)
}

// mobileHubSSHSessionAction applies a hub_exec control action; returns updated record.
type mobileHubSSHSessionAction func(record *mobileBackendSSHSessionRecord, principal *auth.ViewerPrincipal) error

func mobileHubSSHHandleAttach(record *mobileBackendSSHSessionRecord, principal *auth.ViewerPrincipal) error {
	// Re-open interactive shell on existing dial (or dial if needed).
	profile, ok := mobileFindServerProfile(principal.TenantID, principal.UserID, record.ServerProfileID)
	if !ok {
		return fmt.Errorf("server profile not found")
	}
	vault, ok := mobileSSHVaultLookup(principal.TenantID, principal.UserID, record.ServerProfileID)
	if !ok {
		return fmt.Errorf("vault secret missing")
	}
	if _, err := mobileHubLiveEnsureShell(record.SessionID, profile, vault); err != nil {
		return err
	}
	now := time.Now().UTC()
	record.Status = "ready"
	record.State = "hub_connected"
	record.Message = "Hub shell re-attached (hub_exec)."
	record.UpdatedAt = now
	return nil
}

func mobileHubSSHHandleReconnect(record *mobileBackendSSHSessionRecord, principal *auth.ViewerPrincipal) error {
	if err := mobileHubSSHReconnectSession(record, principal.TenantID, principal.UserID); err != nil {
		return err
	}
	return nil
}

func mobileHubSSHHandleInterrupt(record *mobileBackendSSHSessionRecord, principal *auth.ViewerPrincipal) error {
	out, err := mobileHubSSHInterruptSession(record.SessionID)
	now := time.Now().UTC()
	note := "\n[hub_exec interrupt]\n" + out + "\n"
	record.RecentOutput = mobileClipRunes(record.RecentOutput+note, 8000)
	record.OutputChunk = note
	record.OutputSeq++
	record.UpdatedAt = now
	if err != nil {
		record.Status = "ready"
		record.State = "hub_connected"
		record.Message = "hub_exec interrupt failed: " + err.Error()
		return err
	}
	record.Status = "ready"
	record.State = "hub_connected"
	record.Message = "hub_exec interrupt sent (Ctrl-C to interactive shell)."
	return nil
}

func mobileBackendSSHSessionHubOrDesktopTransition(
	identity *auth.IdentityService,
	method, status, state, message string,
	hubAction mobileHubSSHSessionAction,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use "+method)
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		now := time.Now().UTC()
		mobileBackendSSHSessions.Lock()
		record, ok := mobileBackendSSHSessions.sessions[sessionID]
		if ok && (record.OwnerID != principal.UserID || record.TenantID != principal.TenantID) {
			ok = false
		}
		if !ok {
			mobileBackendSSHSessions.Unlock()
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		if record.ExecMode == mobileSSHExecHub && hubAction != nil {
			// Apply hub_exec action outside the map lock (may dial / read shell).
			mobileBackendSSHSessions.Unlock()
			if runErr := hubAction(&record, principal); runErr != nil {
				// Persist best-effort status then return error.
				mobileBackendSSHSessions.Lock()
				if existing, exists := mobileBackendSSHSessions.sessions[sessionID]; exists {
					// Keep any fields hubAction already mutated on record.
					existing.Status = record.Status
					if existing.Status == "" || existing.Status == "interrupt_requested" || existing.Status == "reconnect_requested" || existing.Status == "attach_requested" {
						existing.Status = "failed"
					}
					existing.State = record.State
					if existing.State == "" {
						existing.State = "hub_error"
					}
					existing.Message = record.Message
					if existing.Message == "" {
						existing.Message = runErr.Error()
					}
					existing.RecentOutput = record.RecentOutput
					existing.OutputChunk = record.OutputChunk
					existing.OutputSeq = record.OutputSeq
					existing.UpdatedAt = time.Now().UTC()
					mobileBackendSSHSessions.sessions[sessionID] = existing
					record = existing
				}
				mobileBackendSSHSessions.Unlock()
				payload := mobileBackendSSHSessionPayload(record)
				mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
				writeError(w, http.StatusBadGateway, "HUB_SSH_CONTROL_FAILED", runErr.Error())
				return
			}
			mobileBackendSSHSessions.Lock()
			mobileBackendSSHSessions.sessions[sessionID] = record
			mobileBackendSSHSessions.Unlock()
			payload := mobileBackendSSHSessionPayload(record)
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
			writeJSON(w, http.StatusOK, map[string]any{"session": payload})
			return
		}
		// desktop_exec: queue for worker claim.
		record.Status = status
		record.State = state
		record.Message = message
		record.UpdatedAt = now
		mobileBackendSSHSessions.sessions[sessionID] = record
		mobileBackendSSHSessions.Unlock()
		payload := mobileBackendSSHSessionPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"session": payload})
	}
}

func mobileBackendSSHSessionTransitionHandler(identity *auth.IdentityService, method, status, state, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use "+method)
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		now := time.Now().UTC()
		mobileBackendSSHSessions.Lock()
		record, ok := mobileBackendSSHSessions.sessions[sessionID]
		if ok && (record.OwnerID != principal.UserID || record.TenantID != principal.TenantID) {
			ok = false
		}
		if ok {
			record.Status = status
			record.State = state
			record.Message = message
			record.UpdatedAt = now
			mobileBackendSSHSessions.sessions[sessionID] = record
		}
		mobileBackendSSHSessions.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		payload := mobileBackendSSHSessionPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"session": payload})
	}
}

// MobileBackendSSHSessionInputHandler records mobile input for a backend SSH
// session. It deliberately does not execute the input in the HTTP handler.
func MobileBackendSSHSessionInputHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		var req mobileBackendSSHInputRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		input, raw, resolveErr := mobileResolvePtyInputBytes(req.Input, req.DataB64, req.Raw)
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", resolveErr.Error())
			return
		}
		if input == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "input or data_b64 is required")
			return
		}
		now := time.Now().UTC()
		mobileBackendSSHSessions.Lock()
		record, ok := mobileBackendSSHSessions.sessions[sessionID]
		if ok && (record.OwnerID != principal.UserID || record.TenantID != principal.TenantID) {
			ok = false
		}
		if !ok {
			mobileBackendSSHSessions.Unlock()
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		// hub_exec: run input as a short one-shot command on Hub (no desktop agent).
		if record.ExecMode == mobileSSHExecHub {
			out, runErr := mobileHubSSHRunInput(&record, input, raw)
			mobileBackendSSHSessions.sessions[sessionID] = record
			mobileBackendSSHSessions.Unlock()
			payload := mobileBackendSSHSessionPayload(record)
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
			status := http.StatusOK
			if runErr != nil {
				status = http.StatusBadGateway
			}
			writeJSON(w, status, map[string]any{
				"session_id": sessionID,
				"status":     record.Status,
				"message":    record.Message,
				"output":     out,
				"raw":        raw,
				"binary":     strings.TrimSpace(req.DataB64) != "",
				"session":    payload,
			})
			return
		}
		if raw {
			// desktop_exec has no raw PTY path on Hub.
			mobileBackendSSHSessions.Unlock()
			writeError(w, http.StatusBadRequest, "RAW_INPUT_UNSUPPORTED", "raw input requires exec_mode=hub_exec")
			return
		}
		record.PendingInput = append(record.PendingInput, input)
		if len(record.PendingInput) > 50 {
			record.PendingInput = record.PendingInput[len(record.PendingInput)-50:]
		}
		record.Status = "input_queued"
		record.State = "pending_agent"
		record.Message = "Input queued for authorized backend SSH session handling."
		record.RecentOutput = "Input queued. The mobile app did not execute this command locally."
		record.UpdatedAt = now
		mobileBackendSSHSessions.sessions[sessionID] = record
		mobileBackendSSHSessions.Unlock()
		payload := mobileBackendSSHSessionPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
		writeJSON(w, http.StatusAccepted, map[string]any{
			"session_id": sessionID,
			"status":     record.Status,
			"message":    record.Message,
			"session":    payload,
		})
	}
}

func mobileBackendSSHOwnedSession(sessionID string, principal *auth.ViewerPrincipal) (mobileBackendSSHSessionRecord, bool) {
	mobileBackendSSHSessions.Lock()
	defer mobileBackendSSHSessions.Unlock()
	record, ok := mobileBackendSSHSessions.sessions[sessionID]
	if !ok || record.OwnerID != principal.UserID || record.TenantID != principal.TenantID {
		return mobileBackendSSHSessionRecord{}, false
	}
	return record, true
}

func MobileBackendSSHSessionTasksHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		session, ok := mobileBackendSSHOwnedSession(sessionID, principal)
		if !ok {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			mobileBackendSSHTasks.Lock()
			tasks := make([]map[string]any, 0)
			for _, task := range mobileBackendSSHTasks.tasks {
				if task.SessionID == sessionID && task.OwnerID == principal.UserID && task.TenantID == principal.TenantID {
					tasks = append(tasks, mobileBackendSSHTaskPayload(task))
				}
			}
			mobileBackendSSHTasks.Unlock()
			writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
		case http.MethodPost:
			var req mobileBackendSSHTaskRequest
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
				return
			}
			action := strings.ToLower(strings.TrimSpace(req.Action))
			if action == "" {
				action = "exec_background"
			}
			if action != "exec_background" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "unsupported backend SSH task action")
				return
			}
			command := strings.TrimSpace(req.Command)
			if command == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "command is required")
				return
			}
			now := time.Now().UTC()
			task := mobileBackendSSHTaskRecord{
				TaskID:           fmt.Sprintf("mobsshtask_%d", now.UnixNano()),
				SessionID:        sessionID,
				TenantID:         principal.TenantID,
				OwnerID:          principal.UserID,
				BackendSessionID: session.BackendSessionID,
				Action:           action,
				Command:          command,
				Status:           "queued",
				Message:          "Background task request queued for MaClaw GUI/agent.",
				LogTail:          "Queued for GUI/agent; the mobile app did not execute this command locally.",
				TailLines:        req.TailLines,
				ClaimedBy:        session.ClaimedBy,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			if session.ExecMode == mobileSSHExecHub {
				async := mobileHubSSHTaskShouldAsync(command, false) ||
					strings.EqualFold(action, "exec_background")
				// Always queue first so list/jobs can show it.
				mobileBackendSSHTasks.Lock()
				mobileBackendSSHTasks.tasks[task.TaskID] = task
				mobileBackendSSHTasks.Unlock()
				if async {
					// Long path: run in background; client polls task / jobs / realtime.
					taskCopy := task
					sessionCopy := session
					go mobileRunHubSSHTask(&taskCopy, sessionCopy)
					payload := mobileBackendSSHTaskPayload(task)
					mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
					writeJSON(w, http.StatusAccepted, map[string]any{
						"task":    payload,
						"async":   true,
						"message": "hub_exec task accepted; poll task status or /api/mobile/jobs",
					})
					return
				}
				// Short path: run inline for snappy emergency commands.
				mobileRunHubSSHTask(&task, session)
				payload := mobileBackendSSHTaskPayload(task)
				mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
				code := http.StatusOK
				if task.Status == "failed" {
					code = http.StatusBadGateway
				}
				writeJSON(w, code, map[string]any{"task": payload, "async": false})
				return
			}
			mobileBackendSSHTasks.Lock()
			mobileBackendSSHTasks.tasks[task.TaskID] = task
			mobileBackendSSHTasks.Unlock()
			payload := mobileBackendSSHTaskPayload(task)
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
			writeJSON(w, http.StatusAccepted, map[string]any{"task": payload})
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
		}
	}
}

func mobileBackendSSHOwnedTask(sessionID, taskID string, principal *auth.ViewerPrincipal) (mobileBackendSSHTaskRecord, bool) {
	mobileBackendSSHTasks.Lock()
	defer mobileBackendSSHTasks.Unlock()
	task, ok := mobileBackendSSHTasks.tasks[taskID]
	if !ok || task.SessionID != sessionID || task.OwnerID != principal.UserID || task.TenantID != principal.TenantID {
		return mobileBackendSSHTaskRecord{}, false
	}
	return task, true
}

func MobileBackendSSHSessionTaskStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if sessionID == "" || taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id and task id are required")
			return
		}
		task, ok := mobileBackendSSHOwnedTask(sessionID, taskID, principal)
		if !ok {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "backend SSH task not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task": mobileBackendSSHTaskPayload(task)})
	}
}

func MobileBackendSSHSessionTaskWaitHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileBackendSSHSessionTaskTransitionHandler(identity, "wait_requested", "Wait request queued for MaClaw GUI/agent.")
}

func MobileBackendSSHSessionTaskKillHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if sessionID == "" || taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id and task id are required")
			return
		}
		now := time.Now().UTC()
		var task mobileBackendSSHTaskRecord
		var hubExec bool
		mobileBackendSSHSessions.Lock()
		if sess, sessOK := mobileBackendSSHSessions.sessions[sessionID]; sessOK &&
			sess.OwnerID == principal.UserID && sess.TenantID == principal.TenantID {
			hubExec = sess.ExecMode == mobileSSHExecHub
		}
		mobileBackendSSHSessions.Unlock()

		mobileBackendSSHTasks.Lock()
		existing, ok := mobileBackendSSHTasks.tasks[taskID]
		if ok && (existing.SessionID != sessionID || existing.OwnerID != principal.UserID || existing.TenantID != principal.TenantID) {
			ok = false
		}
		if ok {
			if hubExec {
				status := strings.ToLower(strings.TrimSpace(existing.Status))
				switch status {
				case "ready", "failed", "cancelled":
					// Terminal — acknowledge without changing.
				case "queued", "":
					existing.Status = "cancelled"
					existing.Message = "cancelled on Hub before start"
				default:
					// running / kill_requested / agent_claimed …
					_ = mobileHubTaskCancel(taskID)
					existing.Status = "kill_requested"
					existing.Message = "kill requested on Hub; stopping remote command"
				}
				existing.UpdatedAt = now
				mobileBackendSSHTasks.tasks[taskID] = existing
				task = existing
			} else {
				existing.Status = "kill_requested"
				existing.Message = "Kill request queued for MaClaw GUI/agent."
				existing.UpdatedAt = now
				mobileBackendSSHTasks.tasks[taskID] = existing
				task = existing
			}
		}
		mobileBackendSSHTasks.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "backend SSH task not found")
			return
		}
		payload := mobileBackendSSHTaskPayload(task)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
		code := http.StatusAccepted
		if hubExec && (task.Status == "cancelled" || task.Status == "ready" || task.Status == "failed") {
			code = http.StatusOK
		}
		writeJSON(w, code, map[string]any{"task": payload})
	}
}

func mobileBackendSSHSessionTaskTransitionHandler(identity *auth.IdentityService, status, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if sessionID == "" || taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id and task id are required")
			return
		}
		var req mobileBackendSSHTaskWaitRequest
		if r.Body != nil {
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req)
		}
		now := time.Now().UTC()
		var task mobileBackendSSHTaskRecord
		mobileBackendSSHTasks.Lock()
		existing, ok := mobileBackendSSHTasks.tasks[taskID]
		if ok && (existing.SessionID != sessionID || existing.OwnerID != principal.UserID || existing.TenantID != principal.TenantID) {
			ok = false
		}
		if ok {
			// hub_exec wait: no desktop agent — just return current status (poll).
			if sess, sessOK := mobileBackendSSHOwnedSession(sessionID, principal); sessOK && sess.ExecMode == mobileSSHExecHub {
				// Keep status; optionally refresh timeout fields for client.
				if req.TimeoutSeconds > 0 {
					existing.TimeoutSeconds = req.TimeoutSeconds
				}
				if req.TailLines > 0 {
					existing.TailLines = req.TailLines
				}
				existing.UpdatedAt = now
				if existing.Message == "" {
					existing.Message = "hub_exec task; poll status (no desktop wait)"
				}
				mobileBackendSSHTasks.tasks[taskID] = existing
				task = existing
				mobileBackendSSHTasks.Unlock()
				payload := mobileBackendSSHTaskPayload(task)
				writeJSON(w, http.StatusOK, map[string]any{"task": payload, "hub_exec": true})
				return
			}
			existing.Status = status
			existing.Message = message
			existing.TimeoutSeconds = req.TimeoutSeconds
			if req.TailLines > 0 {
				existing.TailLines = req.TailLines
			}
			existing.UpdatedAt = now
			mobileBackendSSHTasks.tasks[taskID] = existing
			task = existing
		}
		mobileBackendSSHTasks.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "backend SSH task not found")
			return
		}
		payload := mobileBackendSSHTaskPayload(task)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
		writeJSON(w, http.StatusAccepted, map[string]any{"task": payload})
	}
}

func MobileBackendSSHSessionFilesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		session, ok := mobileBackendSSHOwnedSession(sessionID, principal)
		if !ok {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		var req mobileBackendSSHFileOperationRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		action := strings.ToLower(strings.TrimSpace(req.Action))
		switch action {
		case "stat", "list", "read", "preview", "cat", "download", "upload":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "unsupported backend SSH file operation")
			return
		}
		if action == "preview" || action == "cat" {
			action = "read"
		}
		localPath := strings.TrimSpace(req.LocalPath)
		remotePath := strings.TrimSpace(req.RemotePath)
		switch action {
		case "upload":
			if localPath == "" || remotePath == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "upload requires both local_path and remote_path")
				return
			}
		case "download":
			// hub_exec: remote only (Hub stores blob). desktop_exec: still wants local path.
			if remotePath == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "download requires remote_path")
				return
			}
			if session.ExecMode != mobileSSHExecHub && localPath == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "desktop_exec download requires local_path and remote_path")
				return
			}
		case "stat", "list", "read":
			if remotePath == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "stat/list/read require remote_path")
				return
			}
		}
		now := time.Now().UTC()
		operation := mobileBackendSSHFileOperationRecord{
			OperationID:      fmt.Sprintf("mobsshfile_%d", now.UnixNano()),
			SessionID:        sessionID,
			TenantID:         principal.TenantID,
			OwnerID:          principal.UserID,
			BackendSessionID: session.BackendSessionID,
			Action:           action,
			LocalPath:        localPath,
			RemotePath:       remotePath,
			Status:           "queued",
			Message:          "File operation request queued for MaClaw GUI/agent.",
			ClaimedBy:        session.ClaimedBy,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if session.ExecMode == mobileSSHExecHub {
			switch action {
			case "stat", "list", "read", "download":
				// Hub-side remote inspection / download (no desktop required).
				// download: Hub pulls remote file (chunked base64 up to absolute cap) into a short-lived token URL.
				opCopy := operation
				mobileBackendSSHFileOperations.Lock()
				mobileBackendSSHFileOperations.operations[operation.OperationID] = operation
				mobileBackendSSHFileOperations.Unlock()
				mobileHubSSHRunFileOp(session, &opCopy)
				// Reload result written by runner.
				mobileBackendSSHFileOperations.Lock()
				if latest, ok := mobileBackendSSHFileOperations.operations[operation.OperationID]; ok {
					operation = latest
				} else {
					operation = opCopy
				}
				mobileBackendSSHFileOperations.Unlock()
				payload := mobileBackendSSHFileOperationPayload(operation)
				code := http.StatusOK
				if operation.Status == "failed" {
					code = http.StatusBadGateway
				}
				writeJSON(w, code, map[string]any{"operation": payload, "hub_exec": true})
				return
			case "upload":
				operation.Status = "failed"
				operation.Message = "hub_exec does not support upload (phone local paths); use desktop_exec"
				operation.UpdatedAt = now
				mobileBackendSSHFileOperations.Lock()
				mobileBackendSSHFileOperations.operations[operation.OperationID] = operation
				mobileBackendSSHFileOperations.Unlock()
				payload := mobileBackendSSHFileOperationPayload(operation)
				writeJSON(w, http.StatusBadRequest, map[string]any{"operation": payload, "hub_exec": true})
				return
			}
		}
		mobileBackendSSHFileOperations.Lock()
		mobileBackendSSHFileOperations.operations[operation.OperationID] = operation
		mobileBackendSSHFileOperations.Unlock()
		payload := mobileBackendSSHFileOperationPayload(operation)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHFileOperationEvent(payload))
		writeJSON(w, http.StatusAccepted, map[string]any{"operation": payload})
	}
}

func mobileBackendSSHWorkerID(principal mobileDigitalEmployeeWorkerPrincipal) string {
	if strings.TrimSpace(principal.MachineID) != "" {
		return strings.TrimSpace(principal.MachineID)
	}
	return strings.TrimSpace(principal.UserID)
}

func MobileBackendSSHTaskClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		now := time.Now().UTC()
		workerID := mobileBackendSSHWorkerID(principal)
		claimable := map[string]bool{
			"queued":         true,
			"wait_requested": true,
			"kill_requested": true,
			"agent_claimed":  true,
			"running":        true,
		}
		var claimed mobileBackendSSHTaskRecord
		mobileBackendSSHTasks.Lock()
		for taskID, task := range mobileBackendSSHTasks.tasks {
			if task.TenantID != principal.TenantID || task.OwnerID != principal.UserID || !claimable[task.Status] {
				continue
			}
			if task.ClaimedBy != "" && task.ClaimedBy != workerID {
				continue
			}
			task.ClaimedBy = workerID
			if task.Status == "queued" {
				task.Status = "agent_claimed"
				task.Message = "Authorized MaClaw GUI/agent claimed the backend SSH task."
			}
			task.UpdatedAt = now
			mobileBackendSSHTasks.tasks[taskID] = task
			claimed = task
			break
		}
		mobileBackendSSHTasks.Unlock()
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{"task": nil, "status": "empty"})
			return
		}
		payload := mobileBackendSSHTaskPayload(claimed)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"task": payload, "status": "claimed"})
	}
}

func MobileBackendSSHTaskUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileBackendSSHTaskUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status is required")
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(req.Error)
		}
		workerID := mobileBackendSSHWorkerID(principal)
		now := time.Now().UTC()
		var task mobileBackendSSHTaskRecord
		mobileBackendSSHTasks.Lock()
		existing, ok := mobileBackendSSHTasks.tasks[taskID]
		if ok && (existing.TenantID != principal.TenantID || existing.OwnerID != principal.UserID) {
			ok = false
		}
		if ok && existing.ClaimedBy != "" && existing.ClaimedBy != workerID {
			ok = false
		}
		if ok {
			existing.ClaimedBy = workerID
			existing.Status = status
			if message != "" {
				existing.Message = message
			}
			if logTail := strings.TrimSpace(req.LogTail); logTail != "" {
				existing.LogTail = logTail
			} else if output := strings.TrimSpace(req.Output); output != "" {
				existing.LogTail = output
			}
			if req.ExitCode != nil {
				existing.ExitCode = req.ExitCode
			}
			if backendSessionID := strings.TrimSpace(req.BackendSessionID); backendSessionID != "" {
				existing.BackendSessionID = backendSessionID
			}
			existing.UpdatedAt = now
			mobileBackendSSHTasks.tasks[taskID] = existing
			task = existing
		}
		mobileBackendSSHTasks.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "backend SSH task not found")
			return
		}
		payload := mobileBackendSSHTaskPayload(task)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHTaskEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"task": payload})
	}
}

func MobileBackendSSHFileOperationClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		now := time.Now().UTC()
		workerID := mobileBackendSSHWorkerID(principal)
		claimable := map[string]bool{
			"queued":        true,
			"agent_claimed": true,
			"running":       true,
		}
		var claimed mobileBackendSSHFileOperationRecord
		mobileBackendSSHFileOperations.Lock()
		for operationID, operation := range mobileBackendSSHFileOperations.operations {
			if operation.TenantID != principal.TenantID || operation.OwnerID != principal.UserID || !claimable[operation.Status] {
				continue
			}
			if operation.ClaimedBy != "" && operation.ClaimedBy != workerID {
				continue
			}
			operation.ClaimedBy = workerID
			if operation.Status == "queued" {
				operation.Status = "agent_claimed"
				operation.Message = "Authorized MaClaw GUI/agent claimed the backend SSH file operation."
			}
			operation.UpdatedAt = now
			mobileBackendSSHFileOperations.operations[operationID] = operation
			claimed = operation
			break
		}
		mobileBackendSSHFileOperations.Unlock()
		if claimed.OperationID == "" {
			writeJSON(w, http.StatusOK, map[string]any{"operation": nil, "status": "empty"})
			return
		}
		payload := mobileBackendSSHFileOperationPayload(claimed)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHFileOperationEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"operation": payload, "status": "claimed"})
	}
}

func MobileBackendSSHFileOperationUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		operationID := strings.TrimSpace(r.PathValue("operationId"))
		if operationID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "operation id is required")
			return
		}
		var req mobileBackendSSHFileOperationUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status is required")
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(req.Error)
		}
		workerID := mobileBackendSSHWorkerID(principal)
		now := time.Now().UTC()
		var operation mobileBackendSSHFileOperationRecord
		mobileBackendSSHFileOperations.Lock()
		existing, ok := mobileBackendSSHFileOperations.operations[operationID]
		if ok && (existing.TenantID != principal.TenantID || existing.OwnerID != principal.UserID) {
			ok = false
		}
		if ok && existing.ClaimedBy != "" && existing.ClaimedBy != workerID {
			ok = false
		}
		if ok {
			existing.ClaimedBy = workerID
			existing.Status = status
			if message != "" {
				existing.Message = message
			}
			if localPath := strings.TrimSpace(req.LocalPath); localPath != "" {
				existing.LocalPath = localPath
			}
			if remotePath := strings.TrimSpace(req.RemotePath); remotePath != "" {
				existing.RemotePath = remotePath
			}
			if req.BytesTransferred > 0 {
				existing.BytesTransferred = req.BytesTransferred
			}
			if downloadURL := strings.TrimSpace(req.DownloadURL); downloadURL != "" {
				existing.DownloadURL = downloadURL
			}
			if backendSessionID := strings.TrimSpace(req.BackendSessionID); backendSessionID != "" {
				existing.BackendSessionID = backendSessionID
			}
			existing.UpdatedAt = now
			mobileBackendSSHFileOperations.operations[operationID] = existing
			operation = existing
		}
		mobileBackendSSHFileOperations.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "OPERATION_NOT_FOUND", "backend SSH file operation not found")
			return
		}
		payload := mobileBackendSSHFileOperationPayload(operation)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHFileOperationEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{"operation": payload})
	}
}

func MobileBackendSSHSessionCloseHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use DELETE")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		now := time.Now().UTC()
		var record mobileBackendSSHSessionRecord
		mobileBackendSSHSessions.Lock()
		existing, ok := mobileBackendSSHSessions.sessions[sessionID]
		if ok && (existing.OwnerID != principal.UserID || existing.TenantID != principal.TenantID) {
			ok = false
		}
		if ok {
			if existing.ExecMode == mobileSSHExecHub {
				// hub_exec: drop live TCP/shell immediately; no desktop worker claim.
				existing.Status = "closed"
				existing.State = "hub_closed"
				existing.Message = "Hub SSH live connection and interactive shell closed."
			} else {
				existing.Status = "close_requested"
				existing.State = "closing"
				existing.Message = "Close request queued for the backend SSH session."
			}
			existing.UpdatedAt = now
			record = existing
			mobileBackendSSHSessions.sessions[sessionID] = existing
		}
		mobileBackendSSHSessions.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		// Always tear down any hub_exec live resources keyed by this session id.
		mobileHubLiveCloseSession(sessionID)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(mobileBackendSSHSessionPayload(record)))
		w.WriteHeader(http.StatusNoContent)
	}
}

// MobileBackendSSHSessionClaimHandler lets an authorized desktop/agent claim
// one pending mobile backend SSH control request. The agent binds it to the
// local corelib SSHSessionManager and reports status through the update handler.
func MobileBackendSSHSessionClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}

		now := time.Now().UTC()
		claimedBy := principal.MachineID
		if claimedBy == "" {
			claimedBy = principal.UserID
		}
		claimable := map[string]bool{
			"queued":              true,
			"attach_requested":    true,
			"reconnect_requested": true,
			"interrupt_requested": true,
			"input_queued":        true,
			"close_requested":     true,
			"agent_claimed":       true,
			"connecting":          true,
			"connected":           true,
			"running":             true,
			"attached":            true,
		}
		var claimed mobileBackendSSHSessionRecord
		mobileBackendSSHSessions.Lock()
		for sessionID, record := range mobileBackendSSHSessions.sessions {
			if record.TenantID != principal.TenantID || record.OwnerID != principal.UserID || !claimable[record.Status] {
				continue
			}
			if record.ClaimedBy != "" && record.ClaimedBy != claimedBy {
				continue
			}
			record.ClaimedBy = claimedBy
			if record.Status == "queued" {
				record.Status = "agent_claimed"
				record.State = "agent_handling"
				record.Message = "Authorized MaClaw agent claimed the backend SSH session request."
			}
			record.UpdatedAt = now
			mobileBackendSSHSessions.sessions[sessionID] = record
			claimed = record
			break
		}
		mobileBackendSSHSessions.Unlock()
		if claimed.SessionID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"session": nil,
				"status":  "empty",
			})
			return
		}
		payload := mobileBackendSSHWorkerSessionPayload(claimed)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
		writeJSON(w, http.StatusOK, map[string]any{
			"session": payload,
			"status":  "claimed",
		})
	}
}

// MobileBackendSSHSessionUpdateHandler lets the authorized desktop/agent report
// the actual SSHSessionManager state and recent output back to mobile.
func MobileBackendSSHSessionUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		sessionID := strings.TrimSpace(r.PathValue("sessionId"))
		if sessionID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "session id is required")
			return
		}
		var req mobileBackendSSHSessionUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status is required")
			return
		}
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(req.Error)
		}
		workerID := principal.MachineID
		if workerID == "" {
			workerID = principal.UserID
		}
		now := time.Now().UTC()
		invalidMessage := ""
		mobileBackendSSHSessions.Lock()
		record, ok := mobileBackendSSHSessions.sessions[sessionID]
		if ok && (record.TenantID != principal.TenantID || record.OwnerID != principal.UserID) {
			ok = false
		}
		if ok && record.ClaimedBy != "" && record.ClaimedBy != workerID {
			ok = false
		}
		if ok {
			nextState := strings.TrimSpace(req.State)
			if nextState == "" {
				nextState = status
			}
			outputChunk := strings.TrimSpace(req.OutputChunk)
			backendSessionID := strings.TrimSpace(req.BackendSessionID)
			if backendSessionID == "" {
				backendSessionID = strings.TrimSpace(record.BackendSessionID)
			}
			if backendSessionID == "" && (status == "connected" || strings.EqualFold(nextState, "running") || outputChunk != "") {
				invalidMessage = "backend_session_id is required for connected backend SSH session updates"
			} else {
				record.ClaimedBy = workerID
				record.Status = status
				record.State = nextState
				if message != "" {
					record.Message = message
				}
				if output := strings.TrimSpace(req.RecentOutput); output != "" {
					record.RecentOutput = output
				}
				record.OutputChunk = outputChunk
				if outputChunk != "" {
					record.OutputSeq++
				}
				if backendSessionID := strings.TrimSpace(req.BackendSessionID); backendSessionID != "" {
					record.BackendSessionID = backendSessionID
				}
				if req.ClearPendingInput {
					record.PendingInput = nil
				} else if req.AppliedInputCount > 0 {
					if req.AppliedInputCount >= len(record.PendingInput) {
						record.PendingInput = nil
					} else {
						record.PendingInput = append([]string(nil), record.PendingInput[req.AppliedInputCount:]...)
					}
				}
				record.UpdatedAt = now
				mobileBackendSSHSessions.sessions[sessionID] = record
			}
		}
		mobileBackendSSHSessions.Unlock()
		if !ok {
			writeError(w, http.StatusNotFound, "SESSION_NOT_FOUND", "backend SSH session not found")
			return
		}
		if invalidMessage != "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", invalidMessage)
			return
		}
		payload := mobileBackendSSHSessionPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeBackendSSHSessionEvent(payload))
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileDigitalEmployeeTaskHandler creates a mobile-origin task request for a
// remote digital employee. The first version is intentionally asynchronous and
// permission-aware: mobile submits intent, the remote worker/owner confirms and
// executes according to its own policy.
func MobileDigitalEmployeeTaskHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		employeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if employeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}
		var req mobileDigitalEmployeeTaskRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		prompt := strings.TrimSpace(req.Prompt)
		if prompt == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "prompt is required")
			return
		}
		taskType := normalizeMobileDigitalEmployeeTaskType(req.TaskType)
		taskContext := sanitizeMobileDigitalEmployeeContext(req.Context)
		now := time.Now().UTC()
		record := mobileDigitalEmployeeTaskRecord{
			TaskID:     fmt.Sprintf("mobve_%d", now.UnixNano()),
			EmployeeID: employeeID,
			OwnerID:    principal.UserID,
			Prompt:     prompt,
			TaskType:   taskType,
			Context:    taskContext,
			Status:     "queued",
			Result:     "任务已提交，等待远程数字员工或授权策略处理。",
			Message:    "任务已提交，等待远程数字员工领取。",
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		mobileDigitalEmployeeTasks.Lock()
		mobileDigitalEmployeeTasks.tasks[record.TaskID] = record
		mobileDigitalEmployeeTasks.Unlock()
		mobilePersistState()
		payload := mobileDigitalEmployeeTaskPayload(record)
		mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		// Wake desktop GUI workers immediately (sub-second claim vs multi-second poll).
		mobileNotifyDesktopWorkersOfDigitalEmployeeTask(r.Context(), principal.TenantID, principal.UserID, employeeID, payload)
		writeJSON(w, http.StatusAccepted, payload)
	}
}

// MobileDigitalEmployeeTaskClaimHandler lets an authorized remote worker claim
// one queued mobile-origin task for a digital employee. This closes the mobile
// to remote-capability loop without letting the phone execute commands itself.
//
// Path forms:
//   - POST .../digital-employees/{employeeId}/tasks/claim  — claim for one employee (or its ve_/machine aliases)
//   - POST .../digital-employees/tasks/claim              — claim next task this worker can host
func MobileDigitalEmployeeTaskClaimHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileDigitalEmployeeTaskClaim(identity, true)
}

// MobileDigitalEmployeeTaskClaimAnyHandler claims the next queued task that this
// machine can host (own machine aliases + VE registry hosted on this machine).
func MobileDigitalEmployeeTaskClaimAnyHandler(identity *auth.IdentityService) http.HandlerFunc {
	return mobileDigitalEmployeeTaskClaim(identity, false)
}

func mobileDigitalEmployeeTaskClaim(identity *auth.IdentityService, requirePathEmployee bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		pathEmployeeID := strings.TrimSpace(r.PathValue("employeeId"))
		if requirePathEmployee && pathEmployeeID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "employee id is required")
			return
		}

		now := time.Now().UTC()
		claimedBy := principal.MachineID
		if claimedBy == "" {
			claimedBy = principal.UserID
		}
		var claimed mobileDigitalEmployeeTaskRecord
		// Optional registry for "this machine hosts VE X" matching.
		var registryEmployees []digitalEmployeeEntry
		if system := mobileQuotaSystem; system != nil {
			tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
			registryEmployees = loadVERegistry(r.Context(), tenantSystem).Employees
		}

		mobileDigitalEmployeeTasks.Lock()
		// Map iteration order is random; claim the oldest queued task this worker
		// can host so multi-task mobile queues stay FIFO-fair.
		var claimID string
		for taskID, record := range mobileDigitalEmployeeTasks.tasks {
			if record.Status != "queued" {
				continue
			}
			if requirePathEmployee && !groupDiscussionParticipantIdentityMatches(record.EmployeeID, pathEmployeeID) {
				continue
			}
			if !mobileWorkerCanClaimDigitalEmployeeTask(record, principal, registryEmployees) {
				continue
			}
			if claimID == "" || record.CreatedAt.Before(mobileDigitalEmployeeTasks.tasks[claimID].CreatedAt) {
				claimID = taskID
			}
		}
		if claimID != "" {
			record := mobileDigitalEmployeeTasks.tasks[claimID]
			record.Status = "in_progress"
			record.Result = "远程数字员工已领取任务，正在处理。"
			record.Message = "远程数字员工已领取任务，正在处理。"
			record.ClaimedBy = claimedBy
			record.UpdatedAt = now
			mobileDigitalEmployeeTasks.tasks[claimID] = record
			claimed = record
		}
		mobileDigitalEmployeeTasks.Unlock()
		if claimed.TaskID == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"task":   nil,
				"status": "empty",
			})
			return
		}
		mobilePersistState()
		payload := mobileDigitalEmployeeTaskPayload(claimed)
		// Notify the mobile task owner (may differ from machine owner for hosted VEs).
		mobileRealtimeBroadcast(principal.TenantID, claimed.OwnerID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		if claimed.OwnerID != principal.UserID {
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task":   payload,
			"status": "claimed",
		})
	}
}

// mobileWorkerCanClaimDigitalEmployeeTask decides if a machine worker may claim a
// queued mobile task. Cases:
//  1. Same Hub user as the phone viewer + any online desktop of that user (personal
//     execution host). Covers chats with shared/platform employees when the user's
//     own GUI is online — otherwise those tasks never leave "queued".
//  2. This machine hosts the target VE in the registry (even for other mobile users).
//  3. Physical alias match (machine / ve_machine) for the employee id.
func mobileWorkerCanClaimDigitalEmployeeTask(
	record mobileDigitalEmployeeTaskRecord,
	principal mobileDigitalEmployeeWorkerPrincipal,
	registry []digitalEmployeeEntry,
) bool {
	employeeID := strings.TrimSpace(record.EmployeeID)
	if employeeID == "" {
		return false
	}
	machineID := strings.TrimSpace(principal.MachineID)
	userID := strings.TrimSpace(principal.UserID)

	// (1) Personal: phone user == machine-bound user; any of their desktops can run it.
	if userID != "" && machineID != "" && strings.TrimSpace(record.OwnerID) == userID {
		return true
	}

	// (2) Host machine for this employee (shared/public VE on this desktop).
	if machineID != "" && mobileMachineHostsEmployeeInRegistry(machineID, userID, employeeID, registry) {
		return true
	}
	// (3) Physical alias without registry entry.
	if machineID != "" && mobileMachineIdentityMatchesEmployee(machineID, employeeID) {
		return true
	}
	return false
}

func mobileMachineIdentityMatchesEmployee(machineID, employeeID string) bool {
	machineID = strings.TrimSpace(machineID)
	employeeID = strings.TrimSpace(employeeID)
	if machineID == "" || employeeID == "" {
		return false
	}
	if groupDiscussionParticipantIdentityMatches(employeeID, machineID) {
		return true
	}
	ve := "ve_" + machineID
	return groupDiscussionParticipantIdentityMatches(employeeID, ve)
}

func mobileMachineHostsEmployeeInRegistry(machineID, userID, employeeID string, registry []digitalEmployeeEntry) bool {
	machineID = strings.TrimSpace(machineID)
	employeeID = strings.TrimSpace(employeeID)
	if machineID == "" || employeeID == "" || len(registry) == 0 {
		return false
	}
	for _, entry := range registry {
		if entry.Status != "" && entry.Status != veStatusActive && entry.Status != veStatusPending {
			// Still allow active/pending; skip disabled.
			if entry.Status == veStatusDisabled || entry.Status == veStatusRejected {
				continue
			}
		}
		if !groupDiscussionParticipantIdentityMatches(entry.ID, employeeID) &&
			!groupDiscussionParticipantIdentityMatches(entry.MachineID, employeeID) &&
			!groupDiscussionParticipantIdentityMatches(entry.PlatformEmployeeID, employeeID) {
			continue
		}
		// Hosted on this machine.
		if groupDiscussionParticipantIdentityMatches(entry.MachineID, machineID) ||
			groupDiscussionParticipantIdentityMatches(entry.ID, "ve_"+machineID) ||
			groupDiscussionParticipantIdentityMatches(entry.ID, machineID) {
			return true
		}
		// Owned by this user and physical-type on any of their machines matching id.
		if userID != "" && strings.TrimSpace(entry.OwnerUserID) == userID &&
			mobileMachineIdentityMatchesEmployee(machineID, entry.MachineID) {
			return true
		}
	}
	return false
}

// MobileDigitalEmployeeTaskUpdateHandler lets the remote worker report task
// progress and final results back to the mobile user.
//
// Authorization is worker-centric (not owner-only): after claim, any machine that
// holds ClaimedBy may PATCH progress/completion so hosted VEs can report back to
// a different phone user. Live updates always target record.OwnerID (the phone
// viewer); offline push still only fires on terminal status.
func MobileDigitalEmployeeTaskUpdateHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PATCH")
			return
		}
		principal, err := authenticateMobileDigitalEmployeeWorker(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Worker authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		if taskID == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "task id is required")
			return
		}
		var req mobileDigitalEmployeeTaskUpdateRequest
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
			return
		}
		status := strings.ToLower(strings.TrimSpace(req.Status))
		switch status {
		case "in_progress", "done", "failed":
		default:
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "status must be one of in_progress, done, failed")
			return
		}
		result := strings.TrimSpace(req.Result)
		message := strings.TrimSpace(req.Message)
		if message == "" {
			message = strings.TrimSpace(req.Error)
		}
		if (status == "done" || status == "failed") && result == "" && message == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "result or message is required for final status")
			return
		}

		now := time.Now().UTC()
		workerID := principal.MachineID
		if workerID == "" {
			workerID = principal.UserID
		}
		// Optional registry for unclaimed fallback (hosted VE without prior claim).
		var registryEmployees []digitalEmployeeEntry
		if system := mobileQuotaSystem; system != nil {
			tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
			registryEmployees = loadVERegistry(r.Context(), tenantSystem).Employees
		}

		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		if !ok {
			mobileDigitalEmployeeTasks.Unlock()
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		// Terminal tasks are immutable: reject progress and cross-terminal flips
		// (e.g. late failed after done) so racing worker PATCHes cannot reopen work.
		if record.Status == "done" || record.Status == "failed" {
			if status != record.Status {
				mobileDigitalEmployeeTasks.Unlock()
				writeError(w, http.StatusConflict, "TASK_ALREADY_FINISHED", "task already finished")
				return
			}
			// Idempotent re-report of the same terminal status: return current snapshot.
			payload := mobileDigitalEmployeeTaskPayload(record)
			mobileDigitalEmployeeTasks.Unlock()
			writeJSON(w, http.StatusOK, payload)
			return
		}
		claimedBy := strings.TrimSpace(record.ClaimedBy)
		authorized := false
		switch {
		case claimedBy != "" && claimedBy == workerID:
			// Primary path: this machine claimed the task (personal or hosted VE).
			authorized = true
		case claimedBy != "" && claimedBy != workerID:
			authorized = false
		case strings.TrimSpace(record.OwnerID) == strings.TrimSpace(principal.UserID):
			// Same Hub user as phone owner; any of their desktops may update.
			authorized = true
		case mobileWorkerCanClaimDigitalEmployeeTask(record, principal, registryEmployees):
			// Hosted VE host updating before/without claim race.
			authorized = true
		}
		if !authorized {
			mobileDigitalEmployeeTasks.Unlock()
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		nextResult := record.Result
		if result != "" {
			nextResult = result
		}
		nextMessage := record.Message
		if message != "" {
			nextMessage = message
		} else if status == "in_progress" && result != "" {
			// Progress patches often only set result; surface a short message for mobile UI.
			nextMessage = mobileClipRunes(result, 120)
		}
		// Skip no-op progress patches (identical status/result/message) to cut
		// realtime fan-out and disk persist pressure under high token rates.
		if status == record.Status && nextResult == record.Result && nextMessage == record.Message {
			payload := mobileDigitalEmployeeTaskPayload(record)
			mobileDigitalEmployeeTasks.Unlock()
			writeJSON(w, http.StatusOK, payload)
			return
		}
		record.Status = status
		record.Result = nextResult
		record.Message = nextMessage
		record.ClaimedBy = workerID
		record.UpdatedAt = now
		mobileDigitalEmployeeTasks.tasks[taskID] = record
		ownerID := record.OwnerID
		mobileDigitalEmployeeTasks.Unlock()

		// Streaming in_progress patches are high-frequency; keep them in memory +
		// realtime only. Persist durable snapshots on terminal status (claim already
		// persisted the in_progress ownership handoff).
		if status == "done" || status == "failed" {
			mobilePersistState()
		}
		payload := mobileDigitalEmployeeTaskPayload(record)
		// Phone owner always gets the live event (progress + completion).
		mobileRealtimeBroadcast(principal.TenantID, ownerID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		if ownerID != "" && ownerID != principal.UserID {
			// Hosted-VE operator console may also watch the same task id.
			mobileRealtimeBroadcast(principal.TenantID, principal.UserID, mobileRealtimeDigitalEmployeeTaskEvent(payload))
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

// MobileDigitalEmployeeTaskStatusHandler returns a mobile-origin digital
// employee task. Full execution is delegated to the remote worker side.
func MobileDigitalEmployeeTaskStatusHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		taskID := strings.TrimSpace(r.PathValue("taskId"))
		mobileDigitalEmployeeTasks.Lock()
		record, ok := mobileDigitalEmployeeTasks.tasks[taskID]
		mobileDigitalEmployeeTasks.Unlock()
		if !ok || record.OwnerID != principal.UserID {
			writeError(w, http.StatusNotFound, "TASK_NOT_FOUND", "digital employee task not found")
			return
		}
		writeJSON(w, http.StatusOK, mobileDigitalEmployeeTaskPayload(record))
	}
}

// MobileDigitalEmployeesHandler lists digital employees a mobile viewer may use
// as remote capability entry points. It intentionally uses viewer auth instead
// of the desktop machine token required by /api/ve/discoverable.
//
// Design §3.2: free / default Mobile shows only the viewer's own twins
// (OwnerUserID match + bound machines). Tenant shared pools require
// entitlements.shared_employees; group-restricted VEs use VisibleGroupIDs.
// MobileDigitalEmployeesHandler lists digital employees for Mobile.
// Online status uses the same live presence rules as Hub admin GET /api/ve/list
// (deviceSvc + MacLawSrv runtime presence via applyVEDiscoverablePresence).
func MobileDigitalEmployeesHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, securitySvc *security.SecurityService, presenceGetters ...veMachinePresenceGetter) http.HandlerFunc {
	var presenceGetter veMachinePresenceGetter
	if len(presenceGetters) > 0 {
		presenceGetter = presenceGetters[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}

		allowShared := mobileViewerAllowsSharedEmployees(r.Context(), principal, system)

		// Resolve org group path for VisibleGroupIDs filtering (shared pool only).
		var (
			requesterGroupPath         []string
			requesterGroupPathResolved bool
		)
		if allowShared && securitySvc != nil && identity != nil {
			resolver := veSecurityVisibilityResolver{
				securitySvc: securitySvc,
				users:       identity.UsersRepo(),
			}
			if path, gerr := resolver.RequesterGroupPath(r.Context(), principal.TenantID, principal.UserID); gerr == nil {
				requesterGroupPath = path
				requesterGroupPathResolved = true
			}
		}

		tenantSystem := scopedSystemSettingsForTenant(principal.TenantID, system)
		authz := loadVEDigitalEmployeeAuthorization(r.Context(), tenantSystem)
		// Bound machines: online from live presence (same source as Hub VE list), not only DB status.
		machineEmployees := mobileMachineDigitalEmployeeEntries(r.Context(), identity, principal, presenceGetter)
		if !veAuthorizationActive(authz) {
			writeJSON(w, http.StatusOK, map[string]any{
				"employees":           publicDigitalEmployeeEntries(machineEmployees),
				"authorization":       authz,
				"scope":               mobileEmployeeListScope(allowShared),
				"shared_employees":    allowShared,
				"group_path_resolved": requesterGroupPathResolved,
			})
			return
		}

		baseSystem := globalSystemSettings(system)
		runtimePresence := emptyMacLawSrvRuntimePresence()
		registry := loadVERegistry(r.Context(), tenantSystem)
		if veRegistryHasMacLawSrvRuntimeEmployees(registry, true) {
			runtimePresence = loadMacLawSrvRuntimePresence(r.Context(), baseSystem, principal.TenantID)
		}

		employees := mobileFilterEmployeesForViewer(
			registry.Employees,
			principal.UserID,
			allowShared,
			requesterGroupPath,
			requesterGroupPathResolved,
		)
		// Same presence path as VEAdminListHandler /api/ve/list.
		for i := range employees {
			employees[i] = applyVEDiscoverablePresence(r.Context(), employees[i], presenceGetter, runtimePresence)
		}
		employees = appendMobileMachineDigitalEmployees(employees, machineEmployees)
		sort.SliceStable(employees, func(i, j int) bool {
			if employees[i].OnlineStatus != employees[j].OnlineStatus {
				return employees[i].OnlineStatus == veOnlineStatusOnline
			}
			return employees[i].Name < employees[j].Name
		})
		employees = publicDigitalEmployeeEntries(employees)

		writeJSON(w, http.StatusOK, map[string]any{
			"employees":           employees,
			"scope":               mobileEmployeeListScope(allowShared),
			"shared_employees":    allowShared,
			"group_path_resolved": requesterGroupPathResolved,
		})
	}
}

func mobileEmployeeListScope(allowShared bool) string {
	if allowShared {
		return "shared"
	}
	return "own"
}

// mobileFilterEmployeesForViewer applies free/own vs shared pool + group visibility.
// Only active entries are considered. Owners always see their own VEs.
func mobileFilterEmployeesForViewer(
	entries []digitalEmployeeEntry,
	userID string,
	allowShared bool,
	requesterGroupPath []string,
	requesterGroupPathResolved bool,
) []digitalEmployeeEntry {
	out := make([]digitalEmployeeEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status != veStatusActive {
			continue
		}
		owned := mobileEmployeeOwnedByViewer(entry, userID)
		// Free / own-only: only owned VEs (+ bound machines appended by caller).
		if !allowShared && !owned {
			continue
		}
		// Owners always see their own VE regardless of VisibleGroupIDs.
		// Shared pool entries must pass group visibility when restricted.
		if !owned {
			if !veVisibleToRequester(entry, requesterGroupPath, requesterGroupPathResolved) {
				continue
			}
		}
		if !veAccessAllowed(entry, userID) {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// mobileEmployeeOwnedByViewer is true when the VE is explicitly owned by the viewer.
// Entries without OwnerUserID are treated as pool/shared (hidden on free tier).
func mobileEmployeeOwnedByViewer(entry digitalEmployeeEntry, userID string) bool {
	owner := strings.TrimSpace(entry.OwnerUserID)
	userID = strings.TrimSpace(userID)
	if owner == "" || userID == "" {
		return false
	}
	return owner == userID
}

// mobileViewerAllowsSharedEmployees gates tenant/public employee pools.
// Aligns with bootstrap entitlements.shared_employees (service_card / paid).
func mobileViewerAllowsSharedEmployees(ctx context.Context, principal *auth.ViewerPrincipal, system store.SystemSettingsRepository) bool {
	if principal == nil {
		return false
	}
	sys := system
	if sys == nil {
		sys = mobileQuotaSystem
	}
	grant := mobileResolveServiceGrantSnapshot(ctx, principal, sys, mobileQuotaSecurity, "")
	llmAccess := mobileLlmAccessPayload(ctx, principal)
	plan := mobilePlanForAccessWithGrant(llmAccess, grant)
	return mobileSharedEmployeesFromGrant(grant, plan)
}

func mobileMachineDigitalEmployeeEntries(ctx context.Context, identity *auth.IdentityService, principal *auth.ViewerPrincipal, presenceGetters ...veMachinePresenceGetter) []digitalEmployeeEntry {
	if identity == nil || principal == nil {
		return nil
	}
	repo := identity.MachinesRepo()
	if repo == nil || strings.TrimSpace(principal.UserID) == "" {
		return nil
	}
	var presenceGetter veMachinePresenceGetter
	if len(presenceGetters) > 0 {
		presenceGetter = presenceGetters[0]
	}
	machines, err := repo.ListByUserID(ctx, principal.UserID)
	if err != nil {
		return nil
	}
	out := make([]digitalEmployeeEntry, 0, len(machines))
	for _, machine := range machines {
		if machine == nil || strings.TrimSpace(machine.ID) == "" {
			continue
		}
		if store.NormalizeTenantID(machine.TenantID) != store.NormalizeTenantID(principal.TenantID) {
			continue
		}
		name := strings.TrimSpace(machine.Alias)
		if name == "" {
			name = strings.TrimSpace(machine.Name)
		}
		if name == "" {
			name = strings.TrimSpace(machine.Hostname)
		}
		if name == "" {
			name = "MaClaw Remote"
		}
		// Prefer live device presence (Hub VE list source); fall back to machines.status.
		onlineStatus := veOnlineStatusOffline
		if strings.EqualFold(strings.TrimSpace(machine.Status), veOnlineStatusOnline) {
			onlineStatus = veOnlineStatusOnline
		}
		entry := digitalEmployeeEntry{
			ID:               "ve_" + strings.TrimSpace(machine.ID),
			MachineID:        strings.TrimSpace(machine.ID),
			EmployeeType:     veEmployeeTypePhysical,
			OwnerUserID:      principal.UserID,
			Name:             name,
			SkillDescription: "通过已绑定的 MaClaw GUI 远程电脑/服务器处理手机端应急任务，可用于日志分析、服务器维护、文档处理和信息核查；高风险命令需要用户确认。",
			AccessPolicy:     "public",
			Resident:         true,
			Status:           veStatusActive,
			OnlineStatus:     onlineStatus,
			RegisteredAt:     machine.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:        machine.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if presenceGetter != nil {
			entry = applyVEDiscoverablePresence(ctx, entry, presenceGetter, emptyMacLawSrvRuntimePresence())
		}
		out = append(out, entry)
	}
	return out
}

func appendMobileMachineDigitalEmployees(employees []digitalEmployeeEntry, machineEmployees []digitalEmployeeEntry) []digitalEmployeeEntry {
	if len(machineEmployees) == 0 {
		return employees
	}
	seen := make(map[string]struct{}, len(employees)*2)
	for _, employee := range employees {
		for _, value := range []string{employee.ID, employee.MachineID} {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			seen[value] = struct{}{}
			if !strings.HasPrefix(value, "ve_") {
				seen["ve_"+value] = struct{}{}
			}
		}
	}
	for _, employee := range machineEmployees {
		id := strings.ToLower(strings.TrimSpace(employee.ID))
		machineID := strings.ToLower(strings.TrimSpace(employee.MachineID))
		if id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
		}
		if machineID != "" {
			if _, ok := seen[machineID]; ok {
				continue
			}
		}
		employees = append(employees, employee)
	}
	return employees
}
