package httpapi

// Mobile meeting recordings are intentionally separate from document uploads:
// a meeting can be hours long and must survive an interrupted mobile upload.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/audioformat"
	"github.com/RapidAI/CodeClaw/hub/internal/auth"
)

// MeetingTranscriptionWorker runs outside the HTTP request path. Implement it
// with the deployment's ASR queue/service; it must return a real transcript.
type MeetingTranscriptionWorker interface {
	Transcribe(ctx context.Context, audioPath, contentType string) (string, error)
}

// MeetingMinutesWorker summarizes a verified transcript, never raw audio.
type MeetingMinutesWorker interface {
	Summarize(ctx context.Context, title, purpose, transcript string) (string, error)
}

// MeetingSpeakerSegmentationWorker is optional. It may return reliable speaker
// labels and timestamps after transcription; failures leave the transcript
// unchanged rather than inventing speaker attribution.
type MeetingSpeakerSegmentationWorker interface {
	Segment(ctx context.Context, audioPath, contentType, transcript string) ([]mobileMeetingSpeakerSegment, error)
}

var meetingWorkers = struct {
	sync.RWMutex
	transcriber MeetingTranscriptionWorker
	minutes     MeetingMinutesWorker
	segmenter   MeetingSpeakerSegmentationWorker
}{}

// SetMeetingRecordingWorkers wires production ASR/LLM workers and makes the
// processing boundary testable without coupling it to a particular provider.
func SetMeetingRecordingWorkers(transcriber MeetingTranscriptionWorker, minutes MeetingMinutesWorker) {
	meetingWorkers.Lock()
	meetingWorkers.transcriber = transcriber
	meetingWorkers.minutes = minutes
	meetingWorkers.Unlock()
}

// SetMeetingSpeakerSegmentationWorker wires an optional diarization provider.
// Passing nil disables diarization without changing ASR/minutes behavior.
func SetMeetingSpeakerSegmentationWorker(segmenter MeetingSpeakerSegmentationWorker) {
	meetingWorkers.Lock()
	meetingWorkers.segmenter = segmenter
	meetingWorkers.Unlock()
}

const (
	// One MiB gets the first durable upload under way after roughly 33 seconds
	// for the mobile 16 kHz mono PCM WAV recorder.  Four MiB delayed the first
	// pre-upload for more than two minutes, which largely defeated recording-time
	// transfer for short meetings.  The total quota remains unchanged.
	meetingRecordingChunkSize      = 1 << 20 // 1 MiB
	meetingRecordingMaxBytes       = 512 << 20
	meetingRecordingMaxDurationSec = 24 * 60 * 60
	// PCM WAV produced by the mobile recorder is fixed at 16kHz, mono, S16LE.
	// Retain the general 24-hour protocol limit for legacy compressed formats,
	// but reject a WAV whose declared duration cannot fit in the 512 MiB quota.
	meetingRecordingMaxWAVDurationSec = meetingRecordingMaxBytes / (16000 * 2)
	meetingRecordingMaxChunks         = meetingRecordingMaxBytes / meetingRecordingChunkSize
)

type mobileMeetingRecording struct {
	ID                string
	OwnerID           string
	TenantID          string
	ConversationID    string
	Title             string
	Purpose           string
	ContentType       string
	Status            string
	Message           string
	FailureCode       string
	RetryCount        int
	ProcessMode       string
	Progress          float64
	DurationSec       float64
	SizeBytes         int64
	SHA256            string
	Dir               string
	Transcript        string
	Minutes           string
	SpeakerSegments   []mobileMeetingSpeakerSegment
	TranscriptDraftID string
	MinutesDraftID    string
	RetentionUntil    time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
	// HardwareClientID is set only for recordings created through the device
	// gateway. It lets terminal processing status return to that same pet while
	// the record itself remains part of the shared Mobile document library.
	HardwareClientID string `json:"hardware_client_id,omitempty"`
	// HardwareNotifiedStatus records the exact terminal state last queued. A
	// failed run may be retried and later become ready; a single boolean would
	// suppress that successful result forever. HardwareNotified remains only to
	// read old persisted state and is migrated on load/update.
	HardwareNotifiedStatus string `json:"hardware_notified_status,omitempty"`
	HardwareNotified       bool   `json:"hardware_notified,omitempty"`
	// Effective document quota captured when the authenticated processing
	// request is claimed. Background/restart workers do not retain that request.
	DocumentQuotaBytes int64 `json:"document_quota_bytes,omitempty"`
}

// mobileMeetingSpeakerSegment is deliberately optional in P0. ASR workers
// that can diarize may return these labels; otherwise the transcript remains
// valid with no unreliable guessed speaker attribution.
type mobileMeetingSpeakerSegment struct {
	Speaker  string  `json:"speaker"`
	StartSec float64 `json:"start_sec"`
	EndSec   float64 `json:"end_sec"`
	Text     string  `json:"text"`
}

var mobileMeetingRecordings = struct {
	sync.Mutex
	items map[string]mobileMeetingRecording
}{items: make(map[string]mobileMeetingRecording)}

type mobileMeetingRecordingCreateRequest struct {
	Title          string `json:"title"`
	Purpose        string `json:"purpose,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	ContentType    string `json:"content_type,omitempty"`
}

type mobileMeetingRecordingCompleteRequest struct {
	Chunks      int     `json:"chunks"`
	SHA256      string  `json:"sha256,omitempty"`
	DurationSec float64 `json:"duration_sec,omitempty"`
}

type mobileMeetingRecordingProcessRequest struct {
	Mode string `json:"mode"`
}

// MobileMeetingRecordingsHandler owns the resumable audio upload and its
// asynchronous processing state.  A durable object-store/queue may replace the
// local backing implementation without changing the mobile protocol.
func MobileMeetingRecordingsHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := authenticateViewerRequest(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		mobileEnsureStateLoaded()
		ownerID := mobilePrincipalOwnerID(principal)
		if ownerID == "" {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer identity missing")
			return
		}
		id := strings.TrimSpace(r.PathValue("recordingId"))
		switch {
		case r.Method == http.MethodPost && id == "":
			mobileMeetingRecordingCreate(w, r, principal, ownerID)
		case r.Method == http.MethodGet && id != "":
			mobileMeetingRecordingGet(w, ownerID, principal.TenantID, id)
		case r.Method == http.MethodPut && id != "" && strings.TrimSpace(r.PathValue("chunkIndex")) != "":
			mobileMeetingRecordingPutChunk(w, r, ownerID, principal.TenantID, id)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			mobileMeetingRecordingComplete(w, r, ownerID, principal.TenantID, id)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/process"):
			mobileMeetingRecordingProcess(w, r, principal, id)
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/audio"):
			mobileMeetingRecordingDeleteAudio(w, ownerID, principal.TenantID, id)
		case r.Method == http.MethodDelete && id != "":
			mobileMeetingRecordingDelete(w, ownerID, principal.TenantID, id)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported meeting recording operation")
		}
	}
}

// MobileMeetingRecordingCapabilitiesHandler tells authenticated mobile clients
// which post-recording modes this Hub can actually execute. It keeps a missing
// ASR/minutes deployment from being discovered only after a long recording has
// been uploaded.
func MobileMeetingRecordingCapabilitiesHandler(identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
			return
		}
		if _, err := authenticateViewerRequest(r, identity); err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
			return
		}
		transcript, minutes := mobileMeetingRecordingWorkerAvailability()
		writeJSON(w, http.StatusOK, map[string]any{
			"modes": map[string]bool{
				"keep":       true,
				"transcript": transcript,
				"minutes":    transcript && minutes,
			},
		})
	}
}

func mobileMeetingRecordingWorkerAvailability() (transcript, minutes bool) {
	meetingWorkers.RLock()
	transcript = meetingWorkers.transcriber != nil
	minutes = meetingWorkers.minutes != nil
	meetingWorkers.RUnlock()
	if !transcript {
		transcript = strings.TrimSpace(os.Getenv("MACLAW_MEETING_TRANSCRIBE_COMMAND")) != ""
	}
	if !minutes {
		minutes = strings.TrimSpace(os.Getenv("MACLAW_MEETING_MINUTES_COMMAND")) != ""
	}
	return transcript, minutes
}

// TranscribeHardwarePairingWAV reuses the configured meeting ASR boundary for
// a short hardware-pairing utterance. It intentionally exposes no storage or
// meeting state: callers provide a temporary WAV and receive only text.
func TranscribeHardwarePairingWAV(ctx context.Context, audioPath, contentType string) (string, error) {
	meetingWorkers.RLock()
	transcriber := meetingWorkers.transcriber
	meetingWorkers.RUnlock()
	if transcriber == nil {
		transcriber = commandMeetingTranscriber{command: os.Getenv("MACLAW_MEETING_TRANSCRIBE_COMMAND")}
	}
	return transcriber.Transcribe(ctx, audioPath, contentType)
}

func mobileMeetingRecordingCreate(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, ownerID string) {
	mobileMeetingRecordingCreateWithHardware(w, r, principal, ownerID, "")
}

func mobileMeetingRecordingCreateForHardware(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, ownerID, clientID string) {
	mobileMeetingRecordingCreateWithHardware(w, r, principal, ownerID, strings.TrimSpace(clientID))
}

func mobileMeetingRecordingCreateWithHardware(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, ownerID, hardwareClientID string) {
	var req mobileMeetingRecordingCreateRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Meeting recording"
	}
	contentType := mobileMeetingRecordingContentType(req.ContentType)
	if contentType == "" {
		writeError(w, http.StatusBadRequest, "UNSUPPORTED_AUDIO_TYPE", "content_type must be audio/mp4, audio/aac, or audio/wav")
		return
	}
	dir, err := mobileMeetingRecordingDirectory()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", "Unable to create recording storage")
		return
	}
	now := time.Now().UTC()
	rec := mobileMeetingRecording{
		ID: fmt.Sprintf("meeting_%d", now.UnixNano()), OwnerID: ownerID, TenantID: strings.TrimSpace(principal.TenantID),
		ConversationID: strings.TrimSpace(req.ConversationID), Title: title, Purpose: strings.TrimSpace(req.Purpose),
		ContentType: contentType, Status: "uploading", Message: "ready for upload", Dir: dir,
		CreatedAt: now, UpdatedAt: now, RetentionUntil: now.Add(30 * 24 * time.Hour),
		HardwareClientID: hardwareClientID,
	}
	if rec.TenantID == "" {
		rec.TenantID = "default"
	}
	mobileMeetingRecordings.Lock()
	mobileMeetingRecordings.items[rec.ID] = rec
	mobileMeetingRecordings.Unlock()
	mobilePersistState()
	writeJSON(w, http.StatusCreated, mobileMeetingRecordingPayload(rec))
}

func mobileMeetingRecordingGet(w http.ResponseWriter, ownerID, tenantID, id string) {
	rec, ok := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	if !ok {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	writeJSON(w, http.StatusOK, mobileMeetingRecordingPayload(rec))
}

func mobileMeetingRecordingPutChunk(w http.ResponseWriter, r *http.Request, ownerID, tenantID, id string) {
	index := strings.TrimSpace(r.PathValue("chunkIndex"))
	chunkIndex, err := strconv.Atoi(index)
	if err != nil || chunkIndex < 0 || chunkIndex >= meetingRecordingMaxChunks {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "invalid chunk index")
		return
	}
	if !mobileMeetingRecordingUploadOpen(ownerID, tenantID, id) {
		writeError(w, http.StatusConflict, "UPLOAD_CLOSED", "recording upload is not open")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, meetingRecordingChunkSize+1))
	if err != nil || len(data) == 0 || len(data) > meetingRecordingChunkSize {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK", "chunk must be between 1 byte and 1 MiB")
		return
	}
	want := strings.TrimSpace(r.Header.Get("X-Chunk-SHA256"))
	if len(want) != sha256.Size*2 {
		writeError(w, http.StatusBadRequest, "CHUNK_HASH_REQUIRED", "X-Chunk-SHA256 must be a SHA-256 hex digest")
		return
	}
	if _, err := hex.DecodeString(want); err != nil {
		writeError(w, http.StatusBadRequest, "CHUNK_HASH_REQUIRED", "X-Chunk-SHA256 must be a SHA-256 hex digest")
		return
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(want, hex.EncodeToString(sum[:])) {
		writeError(w, http.StatusBadRequest, "CHUNK_HASH_MISMATCH", "chunk hash mismatch")
		return
	}
	if err := mobileMeetingRecordingStoreChunk(ownerID, tenantID, id, chunkIndex, data); err != nil {
		switch {
		case errors.Is(err, errMobileMeetingRecordingNotFound):
			writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		case errors.Is(err, errMobileMeetingRecordingUploadClosed):
			writeError(w, http.StatusConflict, "UPLOAD_CLOSED", "recording upload is not open")
		default:
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", "unable to save chunk")
		}
		return
	}
	mobilePersistState()
	w.WriteHeader(http.StatusNoContent)
}

func mobileMeetingRecordingUploadOpen(ownerID, tenantID, id string) bool {
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok := mobileMeetingRecordings.items[id]
	return ok && rec.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) && rec.Status == "uploading"
}

var (
	errMobileMeetingRecordingNotFound     = errors.New("meeting recording not found")
	errMobileMeetingRecordingUploadClosed = errors.New("meeting recording upload is closed")
)

// mobileMeetingRecordingStoreChunk writes a chunk to a private temporary file,
// then verifies and commits it while holding the recording lock. This ensures a
// late retry cannot add data after complete has transitioned the recording to
// finalizing.
func mobileMeetingRecordingStoreChunk(ownerID, tenantID, id string, chunkIndex int, data []byte) error {
	mobileMeetingRecordings.Lock()
	rec, ok := mobileMeetingRecordings.items[id]
	mobileMeetingRecordings.Unlock()
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
		return errMobileMeetingRecordingNotFound
	}

	temporary, err := os.CreateTemp(rec.Dir, ".chunk-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}

	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok = mobileMeetingRecordings.items[id]
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
		return errMobileMeetingRecordingNotFound
	}
	if rec.Status != "uploading" {
		return errMobileMeetingRecordingUploadClosed
	}
	path := filepath.Join(rec.Dir, fmt.Sprintf("chunk-%d", chunkIndex))
	// Windows cannot rename over an existing file. The lock keeps finalization
	// from observing the brief replacement window, so idempotent chunk retries
	// remain safe on every supported platform.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func mobileMeetingRecordingComplete(w http.ResponseWriter, r *http.Request, ownerID, tenantID, id string) {
	var req mobileMeetingRecordingCompleteRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.Chunks <= 0 || req.Chunks > meetingRecordingMaxChunks {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "chunks is required")
		return
	}
	recording, exists := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	if !exists {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	maxDuration := float64(meetingRecordingMaxDurationSec)
	if mobileMeetingRecordingContentType(recording.ContentType) == "audio/wav" {
		maxDuration = float64(meetingRecordingMaxWAVDurationSec)
	}
	if req.DurationSec < 0 || req.DurationSec > maxDuration {
		writeError(w, http.StatusBadRequest, "INVALID_DURATION", fmt.Sprintf("duration_sec exceeds the %.0f-second limit for this audio format", maxDuration))
		return
	}
	rec, claim := mobileMeetingRecordingClaimFinalize(ownerID, tenantID, id)
	if claim == "missing" {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	if claim == "finalizing" {
		writeError(w, http.StatusConflict, "FINALIZING", "recording finalization is already in progress")
		return
	}
	if claim == "complete" {
		writeJSON(w, http.StatusOK, mobileMeetingRecordingPayload(rec))
		return
	}
	mobilePersistState()
	finalized := false
	defer func() {
		if !finalized {
			mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
				if m.Status == "finalizing" {
					m.Status = "uploading"
					m.Message = "upload finalization interrupted; retry complete"
					m.Progress = 0
				}
			})
		}
	}()
	finalPath := filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType))
	out, err := os.OpenFile(finalPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", "unable to finalize recording")
		return
	}
	h := sha256.New()
	var total int64
	for i := 0; i < req.Chunks; i++ {
		part, err := os.Open(filepath.Join(rec.Dir, fmt.Sprintf("chunk-%d", i)))
		if err != nil {
			_ = out.Close()
			_ = os.Remove(finalPath)
			writeError(w, http.StatusConflict, "MISSING_CHUNK", fmt.Sprintf("chunk %d is missing", i))
			return
		}
		n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(part, meetingRecordingChunkSize+1))
		_ = part.Close()
		total += n
		if copyErr != nil || total > meetingRecordingMaxBytes {
			_ = out.Close()
			_ = os.Remove(finalPath)
			writeError(w, http.StatusBadRequest, "AUDIO_TOO_LARGE", "recording is invalid or exceeds 512 MiB")
			return
		}
	}
	if err := out.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", "unable to close recording")
		return
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if req.SHA256 != "" && !strings.EqualFold(strings.TrimSpace(req.SHA256), sum) {
		_ = os.Remove(finalPath)
		writeError(w, http.StatusBadRequest, "HASH_MISMATCH", "recording hash mismatch")
		return
	}
	if err := mobileValidateMeetingAudio(finalPath, rec.ContentType); err != nil {
		_ = os.Remove(finalPath)
		writeError(w, http.StatusBadRequest, "AUDIO_TYPE_MISMATCH", err.Error())
		return
	}
	for i := 0; i < req.Chunks; i++ {
		_ = os.Remove(filepath.Join(rec.Dir, fmt.Sprintf("chunk-%d", i)))
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Status = "uploaded"
		m.Message = "audio uploaded"
		m.Progress = .2
		m.SizeBytes = total
		m.SHA256 = sum
		m.DurationSec = req.DurationSec
	})
	finalized = true
	rec, _ = mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	mobilePersistState()
	writeJSON(w, http.StatusOK, mobileMeetingRecordingPayload(rec))
}

// mobileValidateMeetingAudio checks the completed file's container/signature,
// rather than trusting the client-provided MIME label. Full codec decoding stays
// in the ASR worker, but bogus uploads must not enter the durable audio queue.
func mobileValidateMeetingAudio(path, contentType string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("unable to read completed audio")
	}
	if len(data) < 2 {
		return fmt.Errorf("completed audio is too short")
	}
	switch mobileMeetingRecordingContentType(contentType) {
	case "audio/mp4":
		if len(data) < 12 || string(data[4:8]) != "ftyp" {
			return fmt.Errorf("completed audio is not an m4a/mp4 container")
		}
	case "audio/aac":
		if !audioformat.LooksLikeADTS(data) {
			return fmt.Errorf("completed audio is not ADTS AAC")
		}
	case "audio/wav":
		if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			return fmt.Errorf("completed audio is not WAV")
		}
	default:
		return fmt.Errorf("completed audio type is unsupported")
	}
	return nil
}

// mobileMeetingRecordingClaimFinalize atomically closes the chunk stream before
// reconstructing the final file. This prevents a late/retried PUT from racing
// with assembly, and makes repeated complete calls deterministic.
func mobileMeetingRecordingClaimFinalize(ownerID, tenantID, id string) (mobileMeetingRecording, string) {
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok := mobileMeetingRecordings.items[id]
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
		return mobileMeetingRecording{}, "missing"
	}
	switch rec.Status {
	case "uploading":
		rec.Status = "finalizing"
		rec.Message = "verifying uploaded audio"
		rec.Progress = .1
		rec.UpdatedAt = time.Now().UTC()
		mobileMeetingRecordings.items[id] = rec
		return rec, "claimed"
	case "finalizing":
		return rec, "finalizing"
	default:
		return rec, "complete"
	}
}

func mobileMeetingRecordingProcess(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, id string) {
	ownerID := mobilePrincipalOwnerID(principal)
	tenantID := ""
	if principal != nil {
		tenantID = principal.TenantID
	}
	var req mobileMeetingRecordingProcessRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "Request body must be valid JSON")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "minutes"
	}
	if mode != "minutes" && mode != "transcript" && mode != "keep" {
		writeError(w, http.StatusBadRequest, "INVALID_MODE", "mode must be minutes, transcript, or keep")
		return
	}
	rec, ok := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	if !ok {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	if rec.Status == "ready" && (rec.ProcessMode != "keep" || mode == "keep") {
		writeError(w, http.StatusConflict, "NOT_READY", "completed recording processing cannot be repeated")
		return
	}
	if rec.Status != "uploaded" && rec.Status != "failed" && rec.Status != "ready" {
		writeError(w, http.StatusConflict, "NOT_READY", "finish uploading before processing")
		return
	}
	// Every processing mode, including keep/archive, requires the durable
	// original file. Without it, reporting successful archiving would mislead
	// users into believing an audio object is retained.
	audioPath := filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType))
	if strings.TrimSpace(rec.Dir) == "" {
		mobileMeetingRecordingMarkAudioMissing(id)
		writeError(w, http.StatusConflict, "AUDIO_MISSING_FOR_RETRY", "raw audio is unavailable; record again to retry")
		return
	}
	if info, err := os.Stat(audioPath); err != nil || info.Size() == 0 {
		mobileMeetingRecordingMarkAudioMissing(id)
		writeError(w, http.StatusConflict, "AUDIO_MISSING_FOR_RETRY", "raw audio is unavailable; record again to retry")
		return
	}
	// Check whether this recording can be retried before worker availability.
	// Without retained audio, a later worker configuration cannot recover it.
	transcriptAvailable, minutesAvailable := mobileMeetingRecordingWorkerAvailability()
	if (mode == "transcript" && !transcriptAvailable) ||
		(mode == "minutes" && (!transcriptAvailable || !minutesAvailable)) {
		writeError(w, http.StatusConflict, "PROCESSING_MODE_UNAVAILABLE", "selected meeting processing mode is not configured on this Hub")
		return
	}
	rec, claim := mobileMeetingRecordingClaimProcess(ownerID, tenantID, id, mode, mobileEffectiveDocumentQuota(r.Context(), principal))
	if claim == "missing" {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	if claim != "claimed" {
		writeError(w, http.StatusConflict, "NOT_READY", "recording processing is already in progress")
		return
	}
	go mobileRunMeetingRecording(id)
	writeJSON(w, http.StatusAccepted, mobileMeetingRecordingPayload(rec))
}

// mobileMeetingRecordingClaimProcess atomically transitions an uploaded, failed,
// or archive-only-ready recording into processing. A ready transcript/minutes
// result stays immutable, while an archive-only recording may be promoted later
// when the user explicitly asks for transcription or meeting minutes.
func mobileMeetingRecordingClaimProcess(ownerID, tenantID, id, mode string, documentQuotaBytes int64) (mobileMeetingRecording, string) {
	mobileMeetingRecordings.Lock()
	rec, ok := mobileMeetingRecordings.items[id]
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
		mobileMeetingRecordings.Unlock()
		return mobileMeetingRecording{}, "missing"
	}
	if rec.Status != "uploaded" && rec.Status != "failed" &&
		!(rec.Status == "ready" && rec.ProcessMode == "keep" && mode != "keep") {
		mobileMeetingRecordings.Unlock()
		return rec, "busy"
	}
	rec.Status = "processing"
	rec.Message = "queued for transcription"
	rec.Progress = .25
	rec.ProcessMode = mode
	if documentQuotaBytes <= 0 {
		documentQuotaBytes = mobileCapDocFreeBytes()
	}
	rec.DocumentQuotaBytes = documentQuotaBytes
	rec.FailureCode = ""
	rec.RetryCount++
	// A new processing attempt owns a new terminal notification. Preserve the
	// last status for auditability, but legacy boolean state must no longer gate
	// the eventual ready message.
	rec.HardwareNotified = false
	rec.UpdatedAt = time.Now().UTC()
	mobileMeetingRecordings.items[id] = rec
	mobileMeetingRecordings.Unlock()
	mobilePersistState()
	mobileRealtimeBroadcast(rec.TenantID, rec.OwnerID, map[string]any{
		"type":         "meeting_recording",
		"recording":    mobileMeetingRecordingPayload(rec),
		"recording_id": rec.ID,
		"status":       rec.Status,
	})
	return rec, "claimed"
}

func mobileMeetingRecordingMarkAudioMissing(id string) {
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Status = "failed"
		m.FailureCode = "AUDIO_MISSING_FOR_RETRY"
		m.Message = "raw audio is unavailable; record again to retry"
		m.Progress = 1
	})
}

func mobileRunMeetingRecording(id string) {
	mobileMeetingRecordings.Lock()
	rec, ok := mobileMeetingRecordings.items[id]
	mobileMeetingRecordings.Unlock()
	if !ok {
		return
	}
	meetingWorkers.RLock()
	transcriber, minutesWorker, segmenter := meetingWorkers.transcriber, meetingWorkers.minutes, meetingWorkers.segmenter
	meetingWorkers.RUnlock()
	if transcriber == nil {
		transcriber = commandMeetingTranscriber{command: os.Getenv("MACLAW_MEETING_TRANSCRIBE_COMMAND")}
	}
	if minutesWorker == nil {
		minutesWorker = commandMeetingMinutes{command: os.Getenv("MACLAW_MEETING_MINUTES_COMMAND")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if rec.ProcessMode == "keep" {
		mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
			m.Status = "ready"
			m.Message = "audio archived"
			m.Progress = 1
		})
		return
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Status = "processing"
		m.Message = "transcribing audio"
		m.Progress = .45
	})
	transcript, err := transcriber.Transcribe(ctx, filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType)), rec.ContentType)
	if err != nil || strings.TrimSpace(transcript) == "" {
		msg := "transcription failed"
		if err != nil {
			msg = err.Error()
		}
		mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
			m.Status = "failed"
			m.Message = clipMeetingWorkerMessage(msg)
			m.Progress = 1
			m.FailureCode = "ASR_TRANSCRIPTION_FAILED"
		})
		return
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Transcript = transcript
		m.Message = "transcript ready"
		m.Progress = .72
	})
	if segmenter != nil {
		segments, segmentErr := segmenter.Segment(ctx, filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType)), rec.ContentType, transcript)
		if segmentErr == nil && len(segments) > 0 {
			mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
				m.SpeakerSegments = append([]mobileMeetingSpeakerSegment(nil), segments...)
			})
		}
	}
	if rec.ProcessMode == "transcript" {
		rec, _ = mobileMeetingRecordingOwned(rec.OwnerID, id)
		if err := mobileStoreMeetingResultDocuments(rec, transcript, ""); err != nil {
			mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
				m.Status = "failed"
				m.Message = "document storage quota exceeded; free space and retry"
				m.Progress = 1
				m.FailureCode = "DOCUMENT_QUOTA_EXCEEDED"
			})
			return
		}
		mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) { m.Status = "ready"; m.Message = "transcript ready"; m.Progress = 1 })
		return
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) { m.Message = "generating meeting minutes"; m.Progress = .82 })
	minutes, err := minutesWorker.Summarize(ctx, rec.Title, rec.Purpose, transcript)
	if err != nil || strings.TrimSpace(minutes) == "" {
		msg := "meeting minutes generation failed"
		if err != nil {
			msg = err.Error()
		}
		mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
			m.Status = "failed"
			m.Message = clipMeetingWorkerMessage(msg)
			m.Progress = 1
			m.FailureCode = "MEETING_MINUTES_FAILED"
		})
		return
	}
	rec, _ = mobileMeetingRecordingOwned(rec.OwnerID, id)
	if err := mobileStoreMeetingResultDocuments(rec, transcript, minutes); err != nil {
		mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
			m.Status = "failed"
			m.Message = "document storage quota exceeded; free space and retry"
			m.Progress = 1
			m.FailureCode = "DOCUMENT_QUOTA_EXCEEDED"
		})
		return
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Status = "ready"
		m.Message = "meeting minutes ready"
		m.Progress = 1
		m.Minutes = minutes
	})
}

// mobileResumeMeetingRecordingWorkers restores work interrupted by a Hub
// restart. Only records with a completed, retained audio file are resumed;
// uploads are resumed by the phone because their chunk stream is client-owned.
// A missing retained file remains failed and is never represented as a successful
// processing result.
func mobileResumeMeetingRecordingWorkers() {
	resume := make([]string, 0)
	terminal := make([]mobileMeetingRecording, 0)
	mobileMeetingRecordings.Lock()
	for id, rec := range mobileMeetingRecordings.items {
		// A terminal state may have been persisted before its queue notification
		// flag (or a Hub restart may have lost the in-memory device queue). Replay
		// once at startup; DeviceGateway will retain it until the hardware ACKs.
		if strings.TrimSpace(rec.HardwareClientID) != "" &&
			(rec.Status == "ready" || rec.Status == "failed") &&
			rec.HardwareNotifiedStatus != rec.Status {
			terminal = append(terminal, rec)
		}
		if rec.Status != "processing" || strings.TrimSpace(rec.Dir) == "" {
			continue
		}
		audioPath := filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType))
		if info, err := os.Stat(audioPath); err != nil || info.Size() == 0 {
			rec.Status = "failed"
			rec.FailureCode = "AUDIO_MISSING_FOR_RETRY"
			rec.Message = "processing could not resume because retained audio is unavailable"
			rec.Progress = 1
			rec.UpdatedAt = time.Now().UTC()
			mobileMeetingRecordings.items[id] = rec
			continue
		}
		rec.Message = "resuming interrupted processing"
		rec.Progress = .25
		rec.UpdatedAt = time.Now().UTC()
		mobileMeetingRecordings.items[id] = rec
		resume = append(resume, id)
	}
	mobileMeetingRecordings.Unlock()
	for _, rec := range terminal {
		mobileNotifyHardwareMeetingResult(rec.ID, rec)
	}
	if len(resume) == 0 {
		return
	}
	mobilePersistState()
	for _, id := range resume {
		go mobileRunMeetingRecording(id)
	}
}

// Commands provide a simple sidecar adapter: stdin is JSON and stdout must be
// {"transcript":"..."} or {"minutes":"..."}. The transcriber receives the
// original mobile audio path and content type (normally AAC in an m4a
// container); the ASR worker owns decoding and any PCM/resampling needed by its
// model. This keeps Hub independent of FFmpeg and lets a worker use its native
// media decoder or a managed ASR service. The adapter is deliberately outside
// the web server and can be replaced by a durable queue consumer.
type commandMeetingTranscriber struct{ command string }

func (w commandMeetingTranscriber) Transcribe(ctx context.Context, audioPath, contentType string) (string, error) {
	if strings.TrimSpace(w.command) == "" {
		return "", fmt.Errorf("ASR worker is not configured for this Hub")
	}
	return runMeetingWorkerCommand(ctx, w.command, map[string]string{"audio_path": audioPath, "content_type": contentType}, "transcript")
}

type commandMeetingMinutes struct{ command string }

func (w commandMeetingMinutes) Summarize(ctx context.Context, title, purpose, transcript string) (string, error) {
	if strings.TrimSpace(w.command) == "" {
		return "", fmt.Errorf("meeting-minutes worker is not configured for this Hub")
	}
	return runMeetingWorkerCommand(ctx, w.command, map[string]string{"title": title, "purpose": purpose, "transcript": transcript}, "minutes")
}
func runMeetingWorkerCommand(ctx context.Context, command string, input map[string]string, resultKey string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	go func() { _ = json.NewEncoder(stdin).Encode(input); _ = stdin.Close() }()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("meeting worker: %w", err)
	}
	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("meeting worker returned invalid JSON: %w", err)
	}
	value := strings.TrimSpace(result[resultKey])
	if value == "" {
		return "", fmt.Errorf("meeting worker returned empty %s", resultKey)
	}
	return value, nil
}
func clipMeetingWorkerMessage(msg string) string {
	runes := []rune(strings.TrimSpace(msg))
	if len(runes) > 500 {
		return string(runes[:500]) + "…"
	}
	return string(runes)
}

func meetingRecordingFilename(contentType string) string {
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "mp4") || strings.Contains(contentType, "aac") {
		return "recording.m4a"
	}
	return "recording.wav"
}

// mobileMeetingRecordingContentType accepts only the formats produced by the
// native mobile recorder (AAC/m4a) or the WAV fallback. Keeping this allowlist
// avoids content-type driven storage surprises and gives the ASR worker a
// trustworthy decoder hint.
func mobileMeetingRecordingContentType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if i := strings.IndexByte(value, ';'); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	switch value {
	case "", "audio/mp4", "audio/x-m4a", "audio/m4a":
		return "audio/mp4"
	case "audio/aac", "audio/aacp":
		return "audio/aac"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "audio/wav"
	default:
		return ""
	}
}

func mobileMeetingRecordingDelete(w http.ResponseWriter, ownerID, tenantID, id string) {
	// Claim removal under the same lock that processing uses to claim a retry.
	// Checking status before acquiring this lock would allow a concurrent retry to
	// transition a failed/archive-only recording back to processing just before
	// this delete removes its audio and metadata.
	mobileMeetingRecordings.Lock()
	rec, ok := mobileMeetingRecordings.items[id]
	if !ok || rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
		mobileMeetingRecordings.Unlock()
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	// The asynchronous worker may still hold/use the recording's audio and may
	// create result documents after the request returns. Do not permit a full
	// record delete until processing reaches a terminal state.
	if rec.Status == "processing" || rec.Status == "uploading" || rec.Status == "uploaded" || rec.Status == "finalizing" {
		mobileMeetingRecordings.Unlock()
		writeError(w, http.StatusConflict, "RECORDING_IN_USE", "wait for recording processing to finish before deleting it")
		return
	}
	delete(mobileMeetingRecordings.items, id)
	mobileMeetingRecordings.Unlock()
	mobileDeleteMeetingResultDocuments(ownerID, tenantID, rec.TranscriptDraftID, rec.MinutesDraftID)
	mobilePersistState()
	_ = os.RemoveAll(rec.Dir)
	w.WriteHeader(http.StatusNoContent)
}

// mobileDeleteMeetingResultDocuments removes the derived library documents
// together with their parent recording. Result documents cannot be deleted
// individually because their IDs are part of the recording's durable state.
func mobileDeleteMeetingResultDocuments(ownerID, tenantID string, draftIDs ...string) {
	ids := make(map[string]struct{}, len(draftIDs))
	for _, draftID := range draftIDs {
		if draftID = strings.TrimSpace(draftID); draftID != "" {
			ids[draftID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return
	}

	var blobPaths []string
	var images []mobileDocumentDraftImage
	mobileDocuments.Lock()
	for draftID := range ids {
		draft, ok := mobileDocuments.drafts[draftID]
		if !ok || draft.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, draft.TenantID) {
			continue
		}
		blobPaths = append(blobPaths, draft.SourcePath)
		images = append(images, draft.Images...)
		delete(mobileDocuments.drafts, draftID)
		for taskID, upload := range mobileDocuments.uploads {
			if upload.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, upload.TenantID) && upload.DraftID == draftID {
				blobPaths = append(blobPaths, upload.SourcePath)
				delete(mobileDocuments.uploads, taskID)
			}
		}
	}
	mobileDocuments.Unlock()
	for _, blobPath := range blobPaths {
		mobileDeleteDocumentBlob(blobPath)
	}
	mobileDraftDeleteImages(images)
}

// mobileMeetingRecordingDeleteAudio deletes only the raw audio. Transcript,
// minutes, document library entries and task metadata remain available.
func mobileMeetingRecordingDeleteAudio(w http.ResponseWriter, ownerID, tenantID, id string) {
	rec, ok := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	if !ok {
		writeError(w, http.StatusNotFound, "RECORDING_NOT_FOUND", "meeting recording not found")
		return
	}
	// The worker may still be reading the original audio. Deleting it while a
	// transcription/minutes task is processing creates a nondeterministic
	// failure, so raw-audio deletion is only allowed after it reaches a terminal
	// state. Failed recordings retain audio specifically for user retry.
	if rec.Status == "processing" || rec.Status == "uploading" || rec.Status == "uploaded" {
		writeError(w, http.StatusConflict, "AUDIO_IN_USE", "wait for recording processing to finish before deleting raw audio")
		return
	}
	if strings.TrimSpace(rec.Dir) != "" {
		if err := os.RemoveAll(rec.Dir); err != nil {
			writeError(w, http.StatusInternalServerError, "STORAGE_ERROR", "unable to delete raw audio")
			return
		}
	}
	mobileMeetingRecordingUpdate(id, func(m *mobileMeetingRecording) {
		m.Dir = ""
		m.Message = "raw audio deleted; transcript and minutes remain available"
	})
	updated, _ := mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id)
	writeJSON(w, http.StatusOK, mobileMeetingRecordingPayload(updated))
}

// mobileCleanupExpiredMeetingRecordings removes only raw audio/chunk storage
// after the retention deadline. The metadata record and transcript/minutes
// document IDs stay available, so an expired audio object never erases a
// completed meeting result. It is intentionally safe to run repeatedly.
func mobileCleanupExpiredMeetingRecordings(now time.Time) int {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cleaned := 0
	mobileMeetingRecordings.Lock()
	for id, rec := range mobileMeetingRecordings.items {
		if rec.RetentionUntil.IsZero() || rec.RetentionUntil.After(now) || strings.TrimSpace(rec.Dir) == "" {
			continue
		}
		if err := os.RemoveAll(rec.Dir); err != nil {
			continue
		}
		rec.Dir = ""
		if rec.Message == "" || !strings.Contains(strings.ToLower(rec.Message), "audio expired") {
			rec.Message = "raw audio expired; transcript and minutes remain available"
		}
		rec.UpdatedAt = now
		mobileMeetingRecordings.items[id] = rec
		cleaned++
	}
	mobileMeetingRecordings.Unlock()
	if cleaned > 0 {
		mobilePersistState()
	}
	return cleaned
}

func mobileMeetingRecordingOwned(ownerID, id string) (mobileMeetingRecording, bool) {
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok := mobileMeetingRecordings.items[id]
	return rec, ok && rec.OwnerID == ownerID
}

// mobileMeetingRecordingOwnedForTenant is the authorization boundary for
// externally addressed recordings. User IDs are normally unique, but the
// durable recording state is tenant-scoped as well, so both identities must
// match before a request can read or mutate a recording.
func mobileMeetingRecordingOwnedForTenant(ownerID, tenantID, id string) (mobileMeetingRecording, bool) {
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	rec, ok := mobileMeetingRecordings.items[id]
	return rec, ok && rec.OwnerID == ownerID && mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID)
}
func mobileMeetingRecordingUpdate(id string, mutate func(*mobileMeetingRecording)) {
	mobileMeetingRecordings.Lock()
	rec, ok := mobileMeetingRecordings.items[id]
	if ok {
		mutate(&rec)
		rec.UpdatedAt = time.Now().UTC()
		mobileMeetingRecordings.items[id] = rec
	}
	mobileMeetingRecordings.Unlock()
	mobilePersistState()
	if ok {
		mobileRealtimeBroadcast(rec.TenantID, rec.OwnerID, map[string]any{
			"type":         "meeting_recording",
			"recording":    mobileMeetingRecordingPayload(rec),
			"recording_id": rec.ID,
			"status":       rec.Status,
		})
		mobileNotifyHardwareMeetingResult(id, rec)
	}
}

func mobileNotifyHardwareMeetingResult(id string, rec mobileMeetingRecording) {
	clientID := strings.TrimSpace(rec.HardwareClientID)
	if clientID == "" || rec.HardwareNotifiedStatus == rec.Status ||
		(rec.Status != "ready" && rec.Status != "failed") {
		return
	}
	notifier := hardwareMeetingResults
	if notifier == nil {
		return
	}
	summary := strings.TrimSpace(rec.Minutes)
	if summary == "" {
		summary = strings.TrimSpace(rec.Transcript)
	}
	if summary == "" {
		summary = strings.TrimSpace(rec.Message)
	}
	if summary == "" {
		summary = map[bool]string{true: "会议处理失败", false: "会议记录已保存"}[rec.Status == "failed"]
	}
	runes := []rune(summary)
	if len(runes) > 180 {
		summary = string(runes[:180])
	}
	conversationID := strings.TrimSpace(rec.ConversationID)
	if conversationID == "" {
		conversationID = "system"
	}
	notifier.EnqueueReply(clientID, conversationID, map[string]any{
		"type": "meeting_result", "text": summary,
		"extra": map[string]any{
			"status": rec.Status, "summary": summary, "recording_id": rec.ID,
			"transcript_draft_id": rec.TranscriptDraftID, "minutes_draft_id": rec.MinutesDraftID,
		},
	})
	mobileMeetingRecordings.Lock()
	current, exists := mobileMeetingRecordings.items[id]
	if exists && current.HardwareNotifiedStatus != rec.Status {
		current.HardwareNotifiedStatus = rec.Status
		current.HardwareNotified = true
		current.UpdatedAt = time.Now().UTC()
		mobileMeetingRecordings.items[id] = current
	}
	mobileMeetingRecordings.Unlock()
	if exists {
		mobilePersistState()
	}
}
func mobileMeetingRecordingPayload(rec mobileMeetingRecording) map[string]any {
	return map[string]any{"recording_id": rec.ID, "title": rec.Title, "purpose": rec.Purpose, "conversation_id": rec.ConversationID, "status": rec.Status, "message": rec.Message, "failure_code": rec.FailureCode, "retry_count": rec.RetryCount, "mode": rec.ProcessMode, "progress": rec.Progress, "duration_sec": rec.DurationSec, "size_bytes": rec.SizeBytes, "sha256": rec.SHA256, "chunk_size": meetingRecordingChunkSize, "audio_available": mobileMeetingRecordingAudioAvailable(rec), "transcript": rec.Transcript, "minutes": rec.Minutes, "speaker_segments": rec.SpeakerSegments, "transcript_draft_id": rec.TranscriptDraftID, "minutes_draft_id": rec.MinutesDraftID, "retention_until": rec.RetentionUntil.Format(time.RFC3339), "created_at": rec.CreatedAt.Format(time.RFC3339), "updated_at": rec.UpdatedAt.Format(time.RFC3339)}
}

func mobileMeetingRecordingAudioAvailable(rec mobileMeetingRecording) bool {
	if strings.TrimSpace(rec.Dir) == "" {
		return false
	}
	info, err := os.Stat(filepath.Join(rec.Dir, meetingRecordingFilename(rec.ContentType)))
	return err == nil && info.Size() > 0
}

// mobileMeetingRecordingDirectory keeps recordings beside durable mobile state
// when Hub persistence is configured. The temp-directory fallback preserves the
// self-contained developer/test setup used by the rest of the mobile APIs.
func mobileMeetingRecordingDirectory() (string, error) {
	if statePath := strings.TrimSpace(mobileStatePath()); statePath != "" {
		base := filepath.Join(filepath.Dir(statePath), "meeting-recordings")
		if err := os.MkdirAll(base, 0o700); err != nil {
			return "", err
		}
		return os.MkdirTemp(base, "meeting-")
	}
	return os.MkdirTemp("", "maclaw-mobile-meeting-")
}

// mobileStoreMeetingResultDocuments makes completed output available in the
// shared document library (and therefore on desktop), while keeping the audio
// API as the canonical source for processing state. It is idempotent for a
// recording that has already received its result draft IDs.
func mobileStoreMeetingResultDocuments(rec mobileMeetingRecording, transcript, minutes string) error {
	transcript = strings.TrimSpace(transcript)
	minutes = strings.TrimSpace(minutes)
	if transcript == "" && minutes == "" {
		return nil
	}
	now := time.Now().UTC()
	principal := &auth.ViewerPrincipal{UserID: rec.OwnerID, TenantID: rec.TenantID}
	if principal.TenantID == "" {
		principal.TenantID = "default"
	}
	title := strings.TrimSpace(rec.Title)
	if title == "" {
		title = "Meeting recording"
	}
	var transcriptDraft, minutesDraft *mobileDocumentDraftRecord
	mobileDocumentQuotaAdmissionMu.Lock()
	defer mobileDocumentQuotaAdmissionMu.Unlock()
	mobileDocuments.Lock()
	transcriptDraftID := strings.TrimSpace(rec.TranscriptDraftID)
	if transcript != "" && transcriptDraftID == "" {
		// A stable ID makes result-document creation idempotent even when the
		// worker is retried before its recording metadata is persisted.
		transcriptDraftID = "mobdoc_meeting_transcript_" + rec.ID
	}
	if transcript != "" && transcriptDraftID != "" {
		if _, exists := mobileDocuments.drafts[transcriptDraftID]; !exists {
			draft := mobileDocumentDraftRecord{
				ID:        transcriptDraftID,
				OwnerID:   rec.OwnerID,
				TenantID:  rec.TenantID,
				Title:     title + " · Transcript",
				Template:  "report",
				Markdown:  mobileMeetingTranscriptMarkdown(title, transcript, rec.SpeakerSegments),
				UpdatedAt: now,
			}
			transcriptDraft = &draft
		}
	}
	minutesDraftID := strings.TrimSpace(rec.MinutesDraftID)
	if minutes != "" && minutesDraftID == "" {
		minutesDraftID = "mobdoc_meeting_minutes_" + rec.ID
	}
	if minutes != "" && minutesDraftID != "" {
		if _, exists := mobileDocuments.drafts[minutesDraftID]; !exists {
			draft := mobileDocumentDraftRecord{
				ID:        minutesDraftID,
				OwnerID:   rec.OwnerID,
				TenantID:  rec.TenantID,
				Title:     title + " · Meeting minutes",
				Template:  "meeting_minutes",
				Markdown:  "# " + title + " — Meeting minutes\n\n" + minutes + "\n",
				UpdatedAt: now,
			}
			minutesDraft = &draft
		}
	}
	mobileDocuments.Unlock()

	if transcriptDraftID == "" && minutesDraftID == "" {
		return nil
	}
	additionalBytes := int64(0)
	if transcriptDraft != nil {
		additionalBytes += int64(len(transcriptDraft.Markdown))
	}
	if minutesDraft != nil {
		additionalBytes += int64(len(minutesDraft.Markdown))
	}
	limit := rec.DocumentQuotaBytes
	if limit <= 0 {
		limit = mobileCapDocFreeBytes()
	}
	if additionalBytes > 0 {
		if err := mobileCheckDocumentQuota(rec.OwnerID, rec.TenantID, additionalBytes, limit); err != nil {
			return errMobileDocumentQuotaExceeded
		}
		mobileDocuments.Lock()
		if transcriptDraft != nil {
			if _, exists := mobileDocuments.drafts[transcriptDraft.ID]; !exists {
				mobileDocuments.drafts[transcriptDraft.ID] = *transcriptDraft
			} else {
				transcriptDraft = nil
			}
		}
		if minutesDraft != nil {
			if _, exists := mobileDocuments.drafts[minutesDraft.ID]; !exists {
				mobileDocuments.drafts[minutesDraft.ID] = *minutesDraft
			} else {
				minutesDraft = nil
			}
		}
		mobileDocuments.Unlock()
	}
	mobileMeetingRecordingUpdate(rec.ID, func(current *mobileMeetingRecording) {
		if transcriptDraftID != "" && current.TranscriptDraftID == "" {
			current.TranscriptDraftID = transcriptDraftID
		}
		if minutesDraftID != "" && current.MinutesDraftID == "" {
			current.MinutesDraftID = minutesDraftID
		}
	})
	if transcriptDraft != nil {
		go mobileIngestDocumentDraft(principal, *transcriptDraft)
	}
	if minutesDraft != nil {
		go mobileIngestDocumentDraft(principal, *minutesDraft)
	}
	return nil
}

func mobileMeetingTranscriptMarkdown(title, transcript string, segments []mobileMeetingSpeakerSegment) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "Meeting recording"
	}
	transcript = strings.TrimSpace(transcript)
	if len(segments) == 0 {
		return "# " + title + " — Transcript\n\n" + transcript + "\n"
	}
	var body strings.Builder
	body.WriteString("# ")
	body.WriteString(title)
	body.WriteString(" — Transcript\n\n")
	body.WriteString("## Speaker segments\n\n")
	for _, segment := range segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" {
			continue
		}
		speaker := strings.TrimSpace(segment.Speaker)
		if speaker == "" {
			speaker = "Speaker"
		}
		fmt.Fprintf(&body, "- **%s** (%s–%s): %s\n", speaker, mobileMeetingTimestamp(segment.StartSec), mobileMeetingTimestamp(segment.EndSec), text)
	}
	body.WriteString("\n## Full transcript\n\n")
	body.WriteString(transcript)
	body.WriteByte('\n')
	return body.String()
}

func mobileMeetingTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, (total/60)%60, total%60)
}

func mobileCollectMeetingRecordingJobs(ownerID, tenantID string) []mobileJobItem {
	mobileMeetingRecordings.Lock()
	defer mobileMeetingRecordings.Unlock()
	out := make([]mobileJobItem, 0)
	for _, rec := range mobileMeetingRecordings.items {
		if rec.OwnerID != ownerID || !mobileMeetingRecordingTenantMatches(tenantID, rec.TenantID) {
			continue
		}
		title := strings.TrimSpace(rec.Title)
		if title == "" {
			title = "Meeting recording"
		}
		out = append(out, mobileJobItem{
			JobID: rec.ID, Kind: "meeting_recording", Title: "Meeting · " + title,
			Status: rec.Status, Progress: rec.Progress, Message: rec.Message,
			DeepLink: "/assistant", UpdatedAt: nonZeroTime(rec.UpdatedAt, rec.CreatedAt),
		})
	}
	return out
}
