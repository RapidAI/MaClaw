package guiapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const mobileDocumentMaxStoredBytes = 100 << 20

// MobileDocumentDraftImage is an illustration extracted from an Office original.
type MobileDocumentDraftImage struct {
	ID          string `json:"id"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Size        int    `json:"size,omitempty"`
	URL         string `json:"url,omitempty"`
}

// MobileDocumentDraftSummary is a Hub-shared emergency draft visible on desktop.
type MobileDocumentDraftSummary struct {
	ID                  string                     `json:"id"`
	Title               string                     `json:"title"`
	Template            string                     `json:"template"`
	UpdatedAt           string                     `json:"updated_at"`
	RuneCount           int                        `json:"rune_count"`
	Preview             string                     `json:"preview"`
	Markdown            string                     `json:"markdown,omitempty"`
	HasOriginal         bool                       `json:"has_original,omitempty"`
	SourceFilename      string                     `json:"source_filename,omitempty"`
	SourceContentType   string                     `json:"source_content_type,omitempty"`
	SourceSize          int                        `json:"source_size,omitempty"`
	SourceStorageSize   int                        `json:"source_storage_size,omitempty"`
	SourceDownloadURL   string                     `json:"source_download_url,omitempty"`
	Images              []MobileDocumentDraftImage `json:"images,omitempty"`
	Duplicate           bool                       `json:"duplicate,omitempty"`
	DuplicateOfTitle    string                     `json:"duplicate_of_title,omitempty"`
	DuplicateOfFilename string                     `json:"duplicate_of_filename,omitempty"`
}

// MobileLibraryAudio describes the original recording behind an audio item.
type MobileLibraryAudio struct {
	ContentType string  `json:"content_type,omitempty"`
	SizeBytes   int64   `json:"size_bytes,omitempty"`
	DurationSec float64 `json:"duration_sec,omitempty"`
	Available   bool    `json:"available"`
	DownloadURL string  `json:"download_url,omitempty"`
}

type MobileLibraryProcessing struct {
	Status      string  `json:"status,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Progress    float64 `json:"progress,omitempty"`
	Message     string  `json:"message,omitempty"`
	FailureCode string  `json:"failure_code,omitempty"`
}

type MobileLibraryDerivedDocuments struct {
	TranscriptDraftID string `json:"transcript_draft_id,omitempty"`
	MinutesDraftID    string `json:"minutes_draft_id,omitempty"`
}

type MobileDocumentQuota struct {
	TotalBytes     int64 `json:"document_quota_bytes"`
	UsedBytes      int64 `json:"document_quota_used_bytes"`
	RemainingBytes int64 `json:"document_quota_remaining"`
}

func (a *App) GetMobileDocumentQuota() (*MobileDocumentQuota, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to read document quota")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/quota", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("get mobile document quota failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get mobile document quota failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out MobileDocumentQuota
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode mobile document quota: %w", err)
	}
	return &out, nil
}

// MobileMeetingRecordingAudioPayload is intentionally capped in the desktop
// bridge. It gives the embedded WebView an authenticated playback source while
// keeping very large recordings on the download/open path instead of buffering
// them in the GUI process.
type MobileMeetingRecordingAudioPayload struct {
	ContentType string `json:"content_type"`
	Filename    string `json:"filename"`
	DataBase64  string `json:"data_base64"`
	SizeBytes   int64  `json:"size_bytes"`
}

// MobileLibraryItem is the Desktop-facing union of shared Markdown documents
// and Mobile recordings. It deliberately keeps the two Hub storage models apart.
type MobileLibraryItem struct {
	MobileDocumentDraftSummary
	Type                 string                         `json:"type"`
	Audio                *MobileLibraryAudio            `json:"audio,omitempty"`
	Processing           *MobileLibraryProcessing       `json:"processing,omitempty"`
	DerivedDocuments     *MobileLibraryDerivedDocuments `json:"derived_documents,omitempty"`
	ManagedByRecordingID string                         `json:"managed_by_recording_id,omitempty"`
	RetentionUntil       string                         `json:"retention_until,omitempty"`
}

// ListMobileDocumentDrafts returns the viewer's mobile/Hub drafts (same library as phone).
// Requires remote Hub URL + viewer token (desktop login).
func (a *App) ListMobileDocumentDrafts(limit int, includeBody bool) ([]MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to list mobile documents")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	if includeBody {
		q.Set("include_body", "1")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/drafts?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list mobile documents failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list mobile documents failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Drafts []MobileDocumentDraftSummary `json:"drafts"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode mobile documents: %w", err)
	}
	if payload.Drafts == nil {
		return []MobileDocumentDraftSummary{}, nil
	}
	return payload.Drafts, nil
}

// ListMobileLibraryItems returns the shared Desktop library, including audio
// uploaded by Mobile after its recording is finalized on Hub.
func (a *App) ListMobileLibraryItems(limit int) ([]MobileLibraryItem, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to list the mobile library")
	}
	if limit <= 0 {
		limit = 80
	}
	if limit > 200 {
		limit = 200
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprintf("%d", limit))
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/library/items?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("list mobile library failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("list mobile library failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Items []MobileLibraryItem `json:"items"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode mobile library: %w", err)
	}
	if payload.Items == nil {
		return []MobileLibraryItem{}, nil
	}
	return payload.Items, nil
}

// GetMobileLibraryItem fetches an audio or document item from the unified Hub view.
func (a *App) GetMobileLibraryItem(itemID string) (*MobileLibraryItem, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(itemID)
	if id == "" {
		return nil, fmt.Errorf("library item id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to open the mobile library")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/library/items/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("get mobile library item failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get mobile library item failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload struct {
		Item *MobileLibraryItem `json:"item"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode mobile library item: %w", err)
	}
	if payload.Item == nil || strings.TrimSpace(payload.Item.ID) == "" {
		return nil, fmt.Errorf("Hub did not return a library item")
	}
	return payload.Item, nil
}

// ProcessMobileMeetingRecording starts the existing Hub ASR + minutes workflow.
func (a *App) ProcessMobileMeetingRecording(recordingID string) (*MobileLibraryItem, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(recordingID)
	if id == "" {
		return nil, fmt.Errorf("recording id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to generate meeting minutes")
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/meeting-recordings/"+url.PathEscape(id)+"/process", strings.NewReader(`{"mode":"minutes"}`))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("start meeting minutes failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("start meeting minutes failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return a.GetMobileLibraryItem(id)
}

// DeleteMobileMeetingRecording deletes only the original audio. Hub retains the
// recording's library entry and any generated transcript/minutes documents.
//
// Success is defined by DELETE /audio alone. The response body is mapped into a
// library item so the UI can update without a follow-up GET — older Hub builds
// hide audio-less recordings without derived documents and returned
// LIBRARY_ITEM_NOT_FOUND after a successful delete.
func (a *App) DeleteMobileMeetingRecording(recordingID string) (*MobileLibraryItem, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(recordingID)
	if id == "" {
		return nil, fmt.Errorf("recording id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to delete original meeting audio")
	}
	req, err := http.NewRequest(http.MethodDelete, hubURL+"/api/mobile/meeting-recordings/"+url.PathEscape(id)+"/audio", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("delete original meeting audio failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("delete original meeting audio failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if item, ok := mobileLibraryItemFromMeetingRecordingPayload(id, data); ok {
		return markMobileLibraryAudioUnavailable(item), nil
	}
	// Empty/legacy DELETE bodies: try library GET, then a minimal stub so a
	// successful delete never surfaces as a hard client error.
	if full, err := a.GetMobileLibraryItem(id); err == nil && full != nil && strings.TrimSpace(full.ID) != "" {
		return markMobileLibraryAudioUnavailable(full), nil
	}
	return mobileLibraryItemAudioDeletedStub(id), nil
}

const mobileLibraryAudioDeletedMessage = "raw audio deleted; transcript and minutes remain available"

// markMobileLibraryAudioUnavailable forces available=false after a successful
// raw-audio delete. Hub may still echo a stale audio_available flag.
func markMobileLibraryAudioUnavailable(item *MobileLibraryItem) *MobileLibraryItem {
	if item == nil {
		return nil
	}
	if item.Audio == nil {
		item.Audio = &MobileLibraryAudio{}
	}
	item.Audio.Available = false
	return item
}

// mobileLibraryItemAudioDeletedStub is the last-resort library view after a
// successful raw-audio delete when neither the DELETE body nor GET is usable.
func mobileLibraryItemAudioDeletedStub(id string) *MobileLibraryItem {
	return &MobileLibraryItem{
		MobileDocumentDraftSummary: MobileDocumentDraftSummary{
			ID:      id,
			Title:   "Meeting recording",
			Preview: mobileLibraryAudioDeletedMessage,
		},
		Type: "audio",
		Audio: &MobileLibraryAudio{
			Available: false,
		},
		Processing: &MobileLibraryProcessing{
			Message: mobileLibraryAudioDeletedMessage,
		},
	}
}

// mobileLibraryItemFromMeetingRecordingPayload maps Hub's
// mobileMeetingRecordingPayload JSON (returned by DELETE /audio and similar)
// into the Desktop library item shape so the UI can update without a second GET.
func mobileLibraryItemFromMeetingRecordingPayload(fallbackID string, data []byte) (*MobileLibraryItem, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, false
	}
	var payload struct {
		RecordingID       string  `json:"recording_id"`
		ID                string  `json:"id"`
		Title             string  `json:"title"`
		Purpose           string  `json:"purpose"`
		Status            string  `json:"status"`
		Message           string  `json:"message"`
		FailureCode       string  `json:"failure_code"`
		Mode              string  `json:"mode"`
		Progress          float64 `json:"progress"`
		DurationSec       float64 `json:"duration_sec"`
		SizeBytes         int64   `json:"size_bytes"`
		TranscriptDraftID string  `json:"transcript_draft_id"`
		MinutesDraftID    string  `json:"minutes_draft_id"`
		RetentionUntil    string  `json:"retention_until"`
		UpdatedAt         string  `json:"updated_at"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	recordingID := strings.TrimSpace(payload.RecordingID)
	payloadID := strings.TrimSpace(payload.ID)
	status := strings.TrimSpace(payload.Status)
	message := strings.TrimSpace(payload.Message)
	title := strings.TrimSpace(payload.Title)
	// Reject bare error envelopes / empty objects served with unexpected 2xx.
	if recordingID == "" && payloadID == "" && status == "" && message == "" && title == "" &&
		payload.SizeBytes == 0 && payload.DurationSec == 0 &&
		strings.TrimSpace(payload.TranscriptDraftID) == "" && strings.TrimSpace(payload.MinutesDraftID) == "" {
		return nil, false
	}
	id := recordingID
	if id == "" {
		id = payloadID
	}
	if id == "" {
		id = strings.TrimSpace(fallbackID)
	}
	if id == "" {
		return nil, false
	}
	if title == "" {
		title = "Meeting recording"
	}
	preview := strings.TrimSpace(payload.Purpose)
	if preview == "" {
		preview = message
	}
	if preview == "" {
		preview = "Meeting recording"
	}
	item := &MobileLibraryItem{
		MobileDocumentDraftSummary: MobileDocumentDraftSummary{
			ID:        id,
			Title:     title,
			UpdatedAt: strings.TrimSpace(payload.UpdatedAt),
			Preview:   preview,
		},
		Type: "audio",
		Audio: &MobileLibraryAudio{
			SizeBytes:   payload.SizeBytes,
			DurationSec: payload.DurationSec,
			Available:   false, // DELETE /audio always removes the file
		},
		Processing: &MobileLibraryProcessing{
			Status:      status,
			Mode:        payload.Mode,
			Progress:    payload.Progress,
			Message:     message,
			FailureCode: payload.FailureCode,
		},
		DerivedDocuments: &MobileLibraryDerivedDocuments{
			TranscriptDraftID: payload.TranscriptDraftID,
			MinutesDraftID:    payload.MinutesDraftID,
		},
	}
	// Hub formats zero times as RFC3339 year 1; omit those from the library item.
	if ru := strings.TrimSpace(payload.RetentionUntil); ru != "" && !strings.HasPrefix(ru, "0001-01-01") {
		item.RetentionUntil = ru
	}
	return item, true
}

// DeleteMobileMeetingRecordingAndResults removes a completed recording and
// every transcript/minutes document generated from it. It is used when a user
// starts deletion from one of those managed result documents.
func (a *App) DeleteMobileMeetingRecordingAndResults(recordingID string) error {
	if a == nil {
		return fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(recordingID)
	if id == "" {
		return fmt.Errorf("recording id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return fmt.Errorf("MaClaw Hub login is required to delete meeting recordings")
	}
	req, err := http.NewRequest(http.MethodDelete, hubURL+"/api/mobile/meeting-recordings/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("delete meeting recording failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delete meeting recording failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

const mobileMeetingPlaybackMaxBytes = 128 << 20
const mobileMeetingDownloadMaxBytes = 512 << 20

func (a *App) fetchMobileMeetingRecordingAudio(recordingID string, maxBytes int64) (string, []byte, string, error) {
	if a == nil {
		return "", nil, "", fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(recordingID)
	if id == "" {
		return "", nil, "", fmt.Errorf("recording id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", nil, "", err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return "", nil, "", fmt.Errorf("MaClaw Hub login is required to download meeting recordings")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/meeting-recordings/"+url.PathEscape(id)+"/audio", nil)
	if err != nil {
		return "", nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return "", nil, "", fmt.Errorf("download meeting audio failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxBytes {
		return "", nil, "", fmt.Errorf("recording exceeds the %d MB desktop limit", maxBytes/(1024*1024))
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if readErr != nil {
		return "", nil, "", fmt.Errorf("download meeting audio failed: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, "", fmt.Errorf("download meeting audio failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if int64(len(body)) > maxBytes {
		return "", nil, "", fmt.Errorf("recording exceeds the %d MB desktop limit", maxBytes/(1024*1024))
	}
	filename := "meeting-recording"
	if disp := strings.TrimSpace(resp.Header.Get("Content-Disposition")); disp != "" {
		if _, params, parseErr := mime.ParseMediaType(disp); parseErr == nil && strings.TrimSpace(params["filename"]) != "" {
			filename = filepath.Base(params["filename"])
		}
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "audio/mp4"
	}
	return filename, body, contentType, nil
}

// GetMobileMeetingRecordingAudio fetches a reasonably-sized recording through authenticated Hub transport for the embedded audio player.
func (a *App) GetMobileMeetingRecordingAudio(recordingID string) (*MobileMeetingRecordingAudioPayload, error) {
	filename, body, contentType, err := a.fetchMobileMeetingRecordingAudio(recordingID, mobileMeetingPlaybackMaxBytes)
	if err != nil {
		return nil, err
	}
	return &MobileMeetingRecordingAudioPayload{ContentType: contentType, Filename: filename, DataBase64: base64.StdEncoding.EncodeToString(body), SizeBytes: int64(len(body))}, nil
}

// SaveMobileMeetingRecordingAudio downloads an owned recording and prompts for its destination.
func (a *App) SaveMobileMeetingRecordingAudio(recordingID string) (string, error) {
	filename, raw, _, err := a.fetchMobileMeetingRecordingAudio(recordingID, mobileMeetingDownloadMaxBytes)
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		dest := filepath.Join(os.TempDir(), "maclaw_meeting_"+sanitizeMobileOriginalFilename(filename))
		return dest, os.WriteFile(dest, raw, 0o600)
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Save recording", DefaultFilename: sanitizeMobileOriginalFilename(filename), Filters: []runtime.FileFilter{{DisplayName: "Audio files", Pattern: "*.m4a;*.mp3;*.wav;*.aac;*.ogg;*.webm"}, {DisplayName: "All Files (*.*)", Pattern: "*.*"}}})
	if err != nil || strings.TrimSpace(dest) == "" {
		return "", err
	}
	return dest, os.WriteFile(dest, raw, 0o600)
}

// OpenMobileMeetingRecordingAudio downloads an owned recording to a private temp directory and opens it with the default media app.
func (a *App) OpenMobileMeetingRecordingAudio(recordingID string) (string, error) {
	filename, raw, _, err := a.fetchMobileMeetingRecordingAudio(recordingID, mobileMeetingDownloadMaxBytes)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "maclaw_meeting_recordings")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, sanitizeMobileOriginalFilename(filename))
	if _, err := os.Stat(dest); err == nil {
		dest = filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), sanitizeMobileOriginalFilename(filename)))
	}
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		return "", err
	}
	if err := a.OpenFileOrShowInFolder(dest); err != nil {
		return dest, fmt.Errorf("saved recording to %s but open failed: %w", dest, err)
	}
	return dest, nil
}

// CreateMobileDocumentDraft uploads a draft into the shared Hub library so the
// phone app can open it (same owner / viewer token). Prefer markdown for full
// file content; content is used for simple title+body posts.
func (a *App) CreateMobileDocumentDraft(title, content, markdown, template string) (*MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to share documents to Mobile")
	}
	template = strings.TrimSpace(template)
	if template == "" {
		template = "note"
	}
	body := map[string]any{
		"title":    title,
		"template": template,
	}
	if md := strings.TrimSpace(markdown); md != "" {
		body["markdown"] = md
	} else if c := strings.TrimSpace(content); c != "" {
		body["content"] = c
	} else {
		body["content"] = "Shared from desktop."
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/documents/drafts", strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create mobile document failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("create mobile document failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return decodeMobileDocumentDraftResponse(data)
}

// DeleteMobileDocumentDraft removes a shared Hub draft (owner only).
func (a *App) DeleteMobileDocumentDraft(draftID string) error {
	if a == nil {
		return fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(draftID)
	if id == "" {
		return fmt.Errorf("draft id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return fmt.Errorf("MaClaw Hub login is required to delete mobile documents")
	}
	req, err := http.NewRequest(http.MethodDelete, hubURL+"/api/mobile/documents/drafts/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete mobile document failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delete mobile document failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

// ImportMobileDocumentFromPath reads a local filesystem path and publishes the
// ORIGINAL file to Hub (multipart upload). Hub keeps the original for Mobile
// preview/share and extracts text when possible for AI convenience.
func (a *App) ImportMobileDocumentFromPath(path string) (*MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	if err := a.fileCompanionRejectUngranted(path); err != nil {
		return nil, err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if strings.Contains(path, "\x00") {
		return nil, fmt.Errorf("invalid path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory")
	}
	// The Hub applies the 100MiB limit after optional compression.
	if info.Size() > mobileDocumentMaxStoredBytes*4 {
		return nil, fmt.Errorf("file too large to compress safely")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer f.Close()
	if info.Size() == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	hasher := sha256.New()
	n, err := io.Copy(hasher, f)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if dup, ok := a.skipDuplicateMobileDocument(hex.EncodeToString(hasher.Sum(nil)), n, filepath.Base(path)); ok {
		return dup, nil
	}
	return a.uploadMobileDocumentOriginalReader(filepath.Base(path), f, contentTypeForMobileImport(path))
}

// ImportMobileDocumentBytes publishes an original file from base64 content when
// the frontend only has a browser File blob (no OS path). Used by "选择文件".
func (a *App) ImportMobileDocumentBytes(filename, contentBase64 string) (*MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "upload.bin"
	}
	contentBase64 = strings.TrimSpace(contentBase64)
	if contentBase64 == "" {
		return nil, fmt.Errorf("file content is empty")
	}
	// Allow data-URL prefix if the UI sends one.
	if i := strings.Index(contentBase64, ","); i >= 0 && strings.Contains(contentBase64[:i], "base64") {
		contentBase64 = contentBase64[i+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		// Some bridges use raw URL-safe base64 without padding.
		raw, err = base64.RawStdEncoding.DecodeString(contentBase64)
		if err != nil {
			return nil, fmt.Errorf("decode file content: %w", err)
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	if len(raw) > mobileDocumentMaxStoredBytes*4 {
		return nil, fmt.Errorf("file too large to compress safely")
	}
	sum := sha256.Sum256(raw)
	if dup, ok := a.skipDuplicateMobileDocument(hex.EncodeToString(sum[:]), int64(len(raw)), filepath.Base(filename)); ok {
		return dup, nil
	}
	return a.uploadMobileDocumentOriginalReader(filepath.Base(filename), bytes.NewReader(raw), contentTypeForMobileImport(filename))
}

// skipDuplicateMobileDocument reports an existing cloud original with these
// bytes. A Hub that answers the content-hash probe is trusted, including a
// negative answer. A missing route cannot be trusted: the document list is
// checked and same-size originals are hashed, so opening the same file again
// does not create another copy.
func (a *App) skipDuplicateMobileDocument(contentSHA string, size int64, filename string) (*MobileDocumentDraftSummary, bool) {
	if a == nil || size <= 0 {
		return nil, false
	}
	contentSHA = strings.ToLower(strings.TrimSpace(contentSHA))
	if len(contentSHA) != sha256.Size*2 {
		return nil, false
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, false
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, false
	}
	found, answered := a.probeDuplicateMobileDocument(hubURL, viewerToken, contentSHA, size)
	if found != nil {
		return found, true
	}
	if answered {
		return nil, false
	}
	return a.duplicateFromDocumentLibrary(hubURL, viewerToken, contentSHA, size, filename)
}

// probeDuplicateMobileDocument asks Hub whether this account already holds
// these original bytes. answered is true only when Hub explicitly says no.
func (a *App) probeDuplicateMobileDocument(hubURL, viewerToken, contentSHA string, size int64) (*MobileDocumentDraftSummary, bool) {
	body, err := json.Marshal(map[string]any{
		"sha256": contentSHA,
		"size":   size,
	})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/documents/upload/duplicate", bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, true
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, false
	}
	dup, _ := payload["duplicate"].(bool)
	if !dup {
		return nil, true
	}
	out := duplicateSummaryFromUploadPayload(payload)
	if strings.TrimSpace(out.ID) == "" {
		return nil, false
	}
	return out, true
}

func mobileDraftByteSizeMatches(d MobileDocumentDraftSummary, size int64) bool {
	if size <= 0 || !d.HasOriginal || strings.TrimSpace(d.ID) == "" {
		return false
	}
	if int64(d.SourceSize) == size {
		return true
	}
	return d.SourceStorageSize > 0 && int64(d.SourceStorageSize) == size
}

// duplicateFromDocumentLibrary hashes same-size cloud originals when the Hub
// has no content-hash probe. Filename matches are checked first.
func (a *App) duplicateFromDocumentLibrary(hubURL, viewerToken, contentSHA string, size int64, filename string) (*MobileDocumentDraftSummary, bool) {
	drafts, err := a.ListMobileDocumentDrafts(200, false)
	if err != nil {
		return nil, false
	}
	base := strings.ToLower(filepath.Base(strings.TrimSpace(filename)))
	ordered := make([]MobileDocumentDraftSummary, 0)
	rest := make([]MobileDocumentDraftSummary, 0)
	for _, d := range drafts {
		if !mobileDraftByteSizeMatches(d, size) {
			continue
		}
		if base != "" && strings.EqualFold(filepath.Base(d.SourceFilename), base) {
			ordered = append(ordered, d)
			continue
		}
		rest = append(rest, d)
	}
	ordered = append(ordered, rest...)
	for _, d := range ordered {
		sum, n, hashErr := a.hashMobileDocumentOriginal(hubURL, viewerToken, &d)
		if hashErr != nil || n != size || sum != contentSHA {
			continue
		}
		title := strings.TrimSpace(d.Title)
		name := strings.TrimSpace(d.SourceFilename)
		if title == "" {
			title = name
		}
		if title == "" {
			title = d.ID
		}
		out := d
		out.Markdown = ""
		out.Preview = ""
		out.Duplicate = true
		out.DuplicateOfTitle = title
		out.DuplicateOfFilename = name
		return &out, true
	}
	return nil, false
}

func (a *App) hashMobileDocumentOriginal(hubURL, viewerToken string, draft *MobileDocumentDraftSummary) (string, int64, error) {
	if draft == nil {
		return "", 0, fmt.Errorf("draft is required")
	}
	req, err := http.NewRequest(http.MethodGet, mobileDocumentSourceURL(hubURL, draft), nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return "", 0, fmt.Errorf("download original failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(resp.Body, mobileDocumentOriginalMaxBytes+1))
	if err != nil {
		return "", 0, err
	}
	if n > mobileDocumentOriginalMaxBytes {
		return "", 0, fmt.Errorf("decoded original file exceeds 400MB safety limit")
	}
	if n == 0 {
		return "", 0, fmt.Errorf("original file is empty")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func mobileDocumentSourceURL(hubURL string, draft *MobileDocumentDraftSummary) string {
	sourcePath := ""
	if draft != nil {
		sourcePath = strings.TrimSpace(draft.SourceDownloadURL)
		if sourcePath == "" {
			sourcePath = "/api/mobile/documents/drafts/" + url.PathEscape(strings.TrimSpace(draft.ID)) + "/source"
		}
	}
	if strings.HasPrefix(sourcePath, "http://") || strings.HasPrefix(sourcePath, "https://") {
		return sourcePath
	}
	if !strings.HasPrefix(sourcePath, "/") {
		sourcePath = "/" + sourcePath
	}
	return strings.TrimRight(hubURL, "/") + sourcePath
}

func duplicateSummaryFromUploadPayload(uploadPayload map[string]any) *MobileDocumentDraftSummary {
	var out *MobileDocumentDraftSummary
	if draftMap, ok := uploadPayload["draft"].(map[string]any); ok {
		out = mobileDraftSummaryFromMap(draftMap)
	} else {
		out = &MobileDocumentDraftSummary{}
	}
	out.Duplicate = true
	if strings.TrimSpace(out.ID) == "" {
		out.ID = stringFromAny(uploadPayload["draft_id"])
	}
	if title := stringFromAny(uploadPayload["duplicate_of_title"]); title != "" {
		out.DuplicateOfTitle = title
	} else {
		out.DuplicateOfTitle = out.Title
	}
	if name := stringFromAny(uploadPayload["duplicate_of_filename"]); name != "" {
		out.DuplicateOfFilename = name
	} else {
		out.DuplicateOfFilename = out.SourceFilename
	}
	return out
}

// uploadMobileDocumentOriginal POSTs the original bytes to Hub upload API.
func (a *App) uploadMobileDocumentOriginal(filename string, raw []byte, contentType string) (*MobileDocumentDraftSummary, error) {
	return a.uploadMobileDocumentOriginalReader(filename, bytes.NewReader(raw), contentType)
}

// uploadMobileDocumentOriginalReader streams multipart data to Hub. Local-path
// imports therefore avoid holding both the original file and multipart body in RAM.
func (a *App) uploadMobileDocumentOriginalReader(filename string, raw io.Reader, contentType string) (*MobileDocumentDraftSummary, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to share documents to Mobile")
	}
	filename = strings.TrimSpace(filename)
	if filename == "" {
		filename = "upload.bin"
	}
	// Keep only the base name; strip any path segments from drag/drop.
	filename = filepath.Base(filename)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}
	// ASCII-safe filename in Content-Disposition so multipart parsers never fail
	// on Chinese / special characters (common Windows drag path issue).
	safeFilename := "upload" + ext
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(
		`form-data; name="file"; filename="%s"; filename*=UTF-8''%s`,
		safeFilename,
		percentEncodeRFC5987(filename),
	))
	h.Set("Content-Type", contentType)
	go func() {
		var writeErr error
		defer func() { _ = pipeWriter.CloseWithError(writeErr) }()
		if writeErr = writer.WriteField("filename", filename); writeErr != nil {
			return
		}
		var part io.Writer
		part, writeErr = writer.CreatePart(h)
		if writeErr != nil {
			return
		}
		_, writeErr = io.Copy(part, raw)
		if writeErr != nil {
			return
		}
		writeErr = writer.Close()
	}()

	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/documents/upload", pipeReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upload mobile document failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("upload mobile document failed: HTTP %d %s", resp.StatusCode, msg)
	}

	// Prefer nested draft from upload payload.
	var uploadPayload map[string]any
	if err := json.Unmarshal(data, &uploadPayload); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	if dup, _ := uploadPayload["duplicate"].(bool); dup {
		out := duplicateSummaryFromUploadPayload(uploadPayload)
		if strings.TrimSpace(out.ID) == "" {
			return nil, fmt.Errorf("Hub upload returned an empty draft id")
		}
		return out, nil
	}
	if draftMap, ok := uploadPayload["draft"].(map[string]any); ok {
		out := mobileDraftSummaryFromMap(draftMap)
		if strings.TrimSpace(out.ID) == "" {
			return nil, fmt.Errorf("Hub upload returned an empty draft id")
		}
		return out, nil
	}
	// Fallback: load draft by id if present.
	if draftID := strings.TrimSpace(fmt.Sprint(uploadPayload["draft_id"])); draftID != "" && draftID != "<nil>" {
		return a.GetMobileDocumentDraft(draftID)
	}
	return nil, fmt.Errorf("Hub upload did not return a draft (status=%v)", uploadPayload["status"])
}

// percentEncodeRFC5987 encodes a filename for Content-Disposition filename*=UTF-8”…
func percentEncodeRFC5987(s string) string {
	if !utf8.ValidString(s) {
		s = string([]rune(s))
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// attr-char from RFC 5987 (subset).
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '!' || c == '#' || c == '$' || c == '&' || c == '+' || c == '-' ||
			c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~' {
			b.WriteByte(c)
			continue
		}
		b.WriteString(fmt.Sprintf("%%%02X", c))
	}
	return b.String()
}

func contentTypeForMobileImport(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".doc":
		return "application/msword"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".pdf":
		return "application/pdf"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".zip":
		return "application/zip"
	case ".gz", ".gzip", ".tgz":
		return "application/gzip"
	case ".rar":
		return "application/vnd.rar"
	case ".7z":
		return "application/x-7z-compressed"
	case ".tar":
		return "application/x-tar"
	case ".md", ".markdown", ".txt", ".log", ".csv", ".json", ".yaml", ".yml":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func decodeMobileDocumentDraftResponse(data []byte) (*MobileDocumentDraftSummary, error) {
	var payload struct {
		Draft *MobileDocumentDraftSummary `json:"draft"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("decode create mobile document: %w", err)
	}
	if payload.Draft != nil && strings.TrimSpace(payload.Draft.ID) != "" {
		return payload.Draft, nil
	}
	var alt map[string]any
	if err := json.Unmarshal(data, &alt); err == nil {
		if draftMap, ok := alt["draft"].(map[string]any); ok {
			return mobileDraftSummaryFromMap(draftMap), nil
		}
	}
	return nil, fmt.Errorf("Hub did not return a draft")
}

func mobileDraftSummaryFromMap(draftMap map[string]any) *MobileDocumentDraftSummary {
	out := &MobileDocumentDraftSummary{
		ID:                stringFromAny(draftMap["id"]),
		Title:             stringFromAny(draftMap["title"]),
		Template:          stringFromAny(draftMap["template"]),
		Markdown:          stringFromAny(draftMap["markdown"]),
		UpdatedAt:         stringFromAny(draftMap["updated_at"]),
		Preview:           stringFromAny(draftMap["preview"]),
		SourceFilename:    stringFromAny(draftMap["source_filename"]),
		SourceContentType: stringFromAny(draftMap["source_content_type"]),
		SourceDownloadURL: stringFromAny(draftMap["source_download_url"]),
	}
	if v, ok := draftMap["has_original"].(bool); ok {
		out.HasOriginal = v
	}
	switch n := draftMap["source_size"].(type) {
	case float64:
		out.SourceSize = int(n)
	case int:
		out.SourceSize = n
	case json.Number:
		if i, err := n.Int64(); err == nil {
			out.SourceSize = int(i)
		}
	}
	if v, ok := draftMap["rune_count"].(float64); ok {
		out.RuneCount = int(v)
	}
	if rawImgs, ok := draftMap["images"].([]any); ok {
		for _, item := range rawImgs {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			img := MobileDocumentDraftImage{
				ID:          stringFromAny(m["id"]),
				Filename:    stringFromAny(m["filename"]),
				ContentType: stringFromAny(m["content_type"]),
				URL:         stringFromAny(m["url"]),
			}
			switch n := m["size"].(type) {
			case float64:
				img.Size = int(n)
			case int:
				img.Size = n
			}
			if img.ID != "" {
				out.Images = append(out.Images, img)
			}
		}
	}
	return out
}

// MobileDocumentDraftImagePayload is binary image content for desktop preview.
type MobileDocumentDraftImagePayload struct {
	ContentType string `json:"content_type"`
	Filename    string `json:"filename,omitempty"`
	// DataBase64 is standard base64 of the image bytes.
	DataBase64 string `json:"data_base64"`
	Size       int    `json:"size"`
}

// GetMobileDocumentDraftImage downloads one extracted illustration from Hub
// (authenticated). Used by desktop preview so <img> can use a data URL.
func (a *App) GetMobileDocumentDraftImage(draftID, imageID string) (*MobileDocumentDraftImagePayload, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	draftID = strings.TrimSpace(draftID)
	imageID = strings.TrimSpace(imageID)
	if draftID == "" || imageID == "" {
		return nil, fmt.Errorf("draft id and image id are required")
	}
	// Path-safe image id only (matches Hub allow-list imgN).
	imageID = filepath.Base(imageID)
	if imageID == "." || imageID == ".." || !strings.HasPrefix(imageID, "img") {
		return nil, fmt.Errorf("invalid image id")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to load document images")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/drafts/"+url.PathEscape(draftID)+"/images/"+url.PathEscape(imageID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download document image failed: %w", err)
	}
	defer resp.Body.Close()
	// Cap at 8 MiB for preview UI.
	const maxImage = 8 << 20
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download document image failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if len(data) > maxImage {
		return nil, fmt.Errorf("image too large for preview")
	}
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if ct == "" {
		ct = "application/octet-stream"
	}
	// filename from Content-Disposition is optional
	filename := imageID
	return &MobileDocumentDraftImagePayload{
		ContentType: ct,
		Filename:    filename,
		DataBase64:  base64.StdEncoding.EncodeToString(data),
		Size:        len(data),
	}, nil
}

// GetMobileDocumentDraft fetches one draft (full markdown) from Hub.
func (a *App) GetMobileDocumentDraft(draftID string) (*MobileDocumentDraftSummary, error) {
	if a == nil {
		return nil, fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(draftID)
	if id == "" {
		return nil, fmt.Errorf("draft id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return nil, fmt.Errorf("MaClaw Hub login is required to open mobile documents")
	}
	req, err := http.NewRequest(http.MethodGet, hubURL+"/api/mobile/documents/drafts/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get mobile document failed: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get mobile document failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return decodeMobileDocumentDraftResponse(data)
}

const mobileDocumentOriginalMaxBytes = 400 << 20

// SaveMobileDocumentOriginal downloads the Hub-stored original and prompts for a
// local save path. Returns the saved path, or empty string if the user cancels.
func (a *App) SaveMobileDocumentOriginal(draftID string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	filename, raw, err := a.fetchMobileDocumentOriginal(draftID)
	if err != nil {
		return "", err
	}
	if a.ctx == nil {
		// Headless / tests: write next to temp without dialog.
		dest := filepath.Join(os.TempDir(), "maclaw_mobile_original_"+sanitizeMobileOriginalFilename(filename))
		if err := os.WriteFile(dest, raw, 0o600); err != nil {
			return "", err
		}
		return dest, nil
	}
	dest, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save original / 保存原件",
		DefaultFilename: sanitizeMobileOriginalFilename(filename),
		Filters: []runtime.FileFilter{
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(dest) == "" {
		return "", nil
	}
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		return "", fmt.Errorf("save original: %w", err)
	}
	return dest, nil
}

// MaterializeMobileDocumentOriginal downloads the Hub original to a cached temp
// file and returns the local path without opening it. Previewers (PPTX slides,
// PDF reader, images) use this path.
func (a *App) MaterializeMobileDocumentOriginal(draftID string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(draftID)
	if id == "" {
		return "", fmt.Errorf("draft id is required")
	}
	draft, err := a.GetMobileDocumentDraft(id)
	if err != nil {
		return "", err
	}
	if draft == nil || !draft.HasOriginal {
		return "", fmt.Errorf("this draft has no original file on Hub")
	}
	return a.materializeMobileDocumentOriginal(id, draft)
}

func (a *App) materializeMobileDocumentOriginal(id string, draft *MobileDocumentDraftSummary) (string, error) {
	if draft == nil || !draft.HasOriginal {
		return "", fmt.Errorf("this draft has no original file on Hub")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = strings.TrimSpace(draft.ID)
	}
	if id == "" {
		return "", fmt.Errorf("draft id is required")
	}
	filename := mobileDraftOriginalFilename(draft)
	dir := filepath.Join(os.TempDir(), "maclaw_mobile_originals", sanitizeMobileOriginalFilename(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	safe := sanitizeMobileOriginalFilename(filename)
	dest := filepath.Join(dir, safe)
	if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() > 0 {
		if draft.SourceSize <= 0 || info.Size() == int64(draft.SourceSize) {
			return dest, nil
		}
	}
	_, raw, err := a.downloadMobileDocumentOriginal(draft)
	if err != nil {
		return "", err
	}
	tmp := dest + fmt.Sprintf(".%d.part", time.Now().UnixNano())
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return "", fmt.Errorf("write temp original: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(dest)
		if err2 := os.Rename(tmp, dest); err2 != nil {
			_ = os.Remove(tmp)
			return "", fmt.Errorf("write temp original: %w", err2)
		}
	}
	return dest, nil
}

// OpenMobileDocumentOriginal downloads the Hub original to a temp file and opens
// it with the OS default app. Returns the temp path.
func (a *App) OpenMobileDocumentOriginal(draftID string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	filename, raw, err := a.fetchMobileDocumentOriginal(draftID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(os.TempDir(), "maclaw_mobile_originals")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	safe := sanitizeMobileOriginalFilename(filename)
	dest := filepath.Join(dir, safe)
	// Avoid collisions when re-opening the same name.
	if _, statErr := os.Stat(dest); statErr == nil {
		dest = filepath.Join(dir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), safe))
	}
	if err := os.WriteFile(dest, raw, 0o600); err != nil {
		return "", fmt.Errorf("write temp original: %w", err)
	}
	if openErr := a.OpenFileOrShowInFolder(dest); openErr != nil {
		// Still return path so the UI can show it.
		return dest, fmt.Errorf("saved to %s but open failed: %w", dest, openErr)
	}
	return dest, nil
}

// OpenMobileDocumentInFileCompanion opens the cloud document in the companion window.
func (a *App) OpenMobileDocumentInFileCompanion(draftID string) (string, error) {
	if a == nil {
		return "", fmt.Errorf("app is not initialized")
	}
	id := strings.TrimSpace(draftID)
	if id == "" {
		return "", fmt.Errorf("draft id is required")
	}
	draft, err := a.GetMobileDocumentDraft(id)
	if err != nil {
		return "", err
	}
	if draft == nil || strings.TrimSpace(draft.ID) == "" {
		return "", fmt.Errorf("draft not found")
	}
	var path string
	if draft.HasOriginal {
		name := mobileDraftOriginalFilename(draft)
		if !companionSupportsFilename(name) {
			return "", fmt.Errorf("companion does not support this file")
		}
		// Preview refreshes the shared hub cache when the file size changes.
		// Companion can save markdown, text, and HTML back into the path it
		// opened, so those originals get a separate file. PDF and other
		// read-only previews keep using the shared cache.
		if companionKeepsLocalEdits(name) {
			path, err = a.openCompanionEditableOriginal(id, draft)
		} else {
			path, err = a.materializeMobileDocumentOriginal(id, draft)
		}
	} else {
		path, err = a.materializeMobileDocumentCompanionText(draft)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("companion file path is empty")
	}
	if err := a.LaunchFileCompanion([]string{path}); err != nil {
		return "", err
	}
	return path, nil
}

func (a *App) materializeMobileDocumentCompanionText(draft *MobileDocumentDraftSummary) (string, error) {
	if draft == nil {
		return "", fmt.Errorf("draft not found")
	}
	body := draft.Markdown
	if strings.TrimSpace(body) == "" {
		body = draft.Preview
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("this draft has no text to open in companion")
	}
	id := strings.TrimSpace(draft.ID)
	if id == "" {
		return "", fmt.Errorf("draft id is required")
	}
	dir := filepath.Join(os.TempDir(), "maclaw_mobile_originals", sanitizeMobileOriginalFilename(id))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, sanitizeMobileOriginalFilename(companionTextFilename(draft)))
	// A later open focuses the same file. Rewriting it would drop edits the
	// companion already saved into this temp copy.
	if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() > 0 {
		return dest, nil
	}
	if err := os.WriteFile(dest, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write companion text: %w", err)
	}
	return dest, nil
}

// companionSourceStampSuffix records the hub snapshot last copied into the
// companion-owned original. A matching file is still that snapshot and can be
// refreshed. A different file is a companion edit and is left in place.
const companionSourceStampSuffix = ".source-sha256"

// companionKeepsLocalEdits is true for extensions the companion window saves.
// It follows fileCompanionTextExtension, except an empty extension: that gate
// also accepts extensionless files, which this cloud-drive action does not open.
func companionKeepsLocalEdits(name string) bool {
	if strings.TrimSpace(filepath.Ext(name)) == "" {
		return false
	}
	return fileCompanionTextExtension(name)
}

func companionEditableOriginalPath(id string, draft *MobileDocumentDraftSummary) string {
	id = strings.TrimSpace(id)
	if id == "" && draft != nil {
		id = strings.TrimSpace(draft.ID)
	}
	name := sanitizeMobileOriginalFilename(mobileDraftOriginalFilename(draft))
	return filepath.Join(os.TempDir(), "maclaw_mobile_originals", sanitizeMobileOriginalFilename(id), "companion", name)
}

func (a *App) openCompanionEditableOriginal(id string, draft *MobileDocumentDraftSummary) (string, error) {
	dest := companionEditableOriginalPath(id, draft)
	stampPath := dest + companionSourceStampSuffix
	if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() > 0 {
		sum, sumErr := mobileOriginalSHA256(dest)
		stamp, stampErr := os.ReadFile(stampPath)
		if sumErr != nil || stampErr != nil || sum != strings.TrimSpace(string(stamp)) {
			return dest, nil
		}
		src, srcErr := a.materializeMobileDocumentOriginal(id, draft)
		if srcErr != nil {
			return dest, nil
		}
		srcSum, srcSumErr := mobileOriginalSHA256(src)
		if srcSumErr != nil || srcSum == sum {
			return dest, nil
		}
		if err := copyMobileOriginalFile(src, dest); err != nil {
			return "", err
		}
		_ = os.WriteFile(stampPath, []byte(srcSum), 0o600)
		return dest, nil
	}
	src, err := a.materializeMobileDocumentOriginal(id, draft)
	if err != nil {
		return "", err
	}
	if err := copyMobileOriginalFile(src, dest); err != nil {
		return "", err
	}
	sum, err := mobileOriginalSHA256(dest)
	if err != nil {
		_ = os.Remove(dest)
		return "", err
	}
	if err := os.WriteFile(stampPath, []byte(sum), 0o600); err != nil {
		_ = os.Remove(dest)
		_ = os.Remove(stampPath)
		return "", err
	}
	return dest, nil
}

func mobileOriginalSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func copyMobileOriginalFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("companion source is not a file")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	tmp := dest + fmt.Sprintf(".%d.part", time.Now().UnixNano())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(dest)
		if err2 := os.Rename(tmp, dest); err2 != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("write companion original: %w", err2)
		}
	}
	return nil
}

func companionTextFilename(draft *MobileDocumentDraftSummary) string {
	name := ""
	if draft != nil {
		name = strings.TrimSpace(draft.SourceFilename)
		if name == "" {
			name = strings.TrimSpace(draft.Title)
		}
	}
	name = filepath.Base(name)
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt", ".text", ".log", ".html", ".htm":
		return name
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	base = strings.TrimSpace(base)
	if base == "" || base == "." {
		base = "document"
	}
	return base + ".md"
}

// companionSupportsFilename matches companionSupportsFileName in
// guiapp/frontend/src/components/preview/filePreviewKind.ts. Code and unknown
// binaries stay out: the companion window shows 此文件类型暂不支持预览 for them.
func companionSupportsFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".md", ".markdown", ".txt", ".text", ".log", ".html", ".htm",
		".pdf", ".tex", ".latex", ".ltx", ".docx", ".pptx",
		".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".ico", ".tif", ".tiff", ".heic",
		".mp4", ".webm", ".mov", ".avi", ".mkv", ".m4v",
		".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac", ".oga",
		".doc", ".docm", ".dot", ".dotx", ".wps", ".wpt", ".rtf", ".odt",
		".xls", ".xlsx", ".xlsm", ".xlsb", ".et", ".ett", ".ods",
		".ppt", ".pptm", ".pps", ".ppsx", ".dps", ".dpt", ".odp":
		return true
	default:
		return false
	}
}

func mobileDraftOriginalFilename(draft *MobileDocumentDraftSummary) string {
	if draft == nil {
		return "original.bin"
	}
	filename := strings.TrimSpace(draft.SourceFilename)
	if filename == "" {
		filename = strings.TrimSpace(draft.Title)
	}
	if filename == "" {
		filename = strings.TrimSpace(draft.ID) + ".bin"
	}
	if filename == "" {
		return "original.bin"
	}
	return filepath.Base(filename)
}

// fetchMobileDocumentOriginal downloads original bytes for a draft the viewer owns.
func (a *App) fetchMobileDocumentOriginal(draftID string) (filename string, raw []byte, err error) {
	draft, err := a.GetMobileDocumentDraft(draftID)
	if err != nil {
		return "", nil, err
	}
	return a.downloadMobileDocumentOriginal(draft)
}

func (a *App) downloadMobileDocumentOriginal(draft *MobileDocumentDraftSummary) (filename string, raw []byte, err error) {
	if a == nil {
		return "", nil, fmt.Errorf("app is not initialized")
	}
	if draft == nil || !draft.HasOriginal {
		return "", nil, fmt.Errorf("this draft has no original file on Hub")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return "", nil, err
	}
	hubURL := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	viewerToken := strings.TrimSpace(cfg.RemoteViewerToken)
	if hubURL == "" || viewerToken == "" {
		return "", nil, fmt.Errorf("MaClaw Hub login is required to download originals")
	}
	req, err := http.NewRequest(http.MethodGet, mobileDocumentSourceURL(hubURL, draft), nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("download original failed: %w", err)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, mobileDocumentOriginalMaxBytes+1))
	if readErr != nil {
		return "", nil, fmt.Errorf("download original failed: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("download original failed: HTTP %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(body) > mobileDocumentOriginalMaxBytes {
		return "", nil, fmt.Errorf("decoded original file exceeds 400MB safety limit")
	}
	return mobileDraftOriginalFilename(draft), body, nil
}

func sanitizeMobileOriginalFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "original.bin"
	}
	// Strip characters unsafe on Windows paths.
	replacer := strings.NewReplacer(
		"<", "_", ">", "_", ":", "_", "\"", "_",
		"/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
	)
	name = replacer.Replace(name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "original.bin"
	}
	return name
}
