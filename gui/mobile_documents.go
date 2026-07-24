package main

import (
	"bytes"
	"encoding/base64"
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
	ID                string                     `json:"id"`
	Title             string                     `json:"title"`
	Template          string                     `json:"template"`
	UpdatedAt         string                     `json:"updated_at"`
	RuneCount         int                        `json:"rune_count"`
	Preview           string                     `json:"preview"`
	Markdown          string                     `json:"markdown,omitempty"`
	HasOriginal       bool                       `json:"has_original,omitempty"`
	SourceFilename    string                     `json:"source_filename,omitempty"`
	SourceContentType string                     `json:"source_content_type,omitempty"`
	SourceSize        int                        `json:"source_size,omitempty"`
	SourceDownloadURL string                     `json:"source_download_url,omitempty"`
	Images            []MobileDocumentDraftImage `json:"images,omitempty"`
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
	Type             string                         `json:"type"`
	Audio            *MobileLibraryAudio            `json:"audio,omitempty"`
	Processing       *MobileLibraryProcessing       `json:"processing,omitempty"`
	DerivedDocuments *MobileLibraryDerivedDocuments `json:"derived_documents,omitempty"`
	RetentionUntil   string                         `json:"retention_until,omitempty"`
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
	return a.GetMobileLibraryItem(id)
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
	// Match Hub mobileDocumentUploadMaxBytes (25MiB).
	const maxBytes = 25 << 20
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file too large (max 25MB)")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("file is empty")
	}
	return a.uploadMobileDocumentOriginal(filepath.Base(path), raw, contentTypeForMobileImport(path))
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
	const maxBytes = 25 << 20
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("file too large (max 25MB)")
	}
	return a.uploadMobileDocumentOriginal(filepath.Base(filename), raw, contentTypeForMobileImport(filename))
}

// uploadMobileDocumentOriginal POSTs the original bytes to Hub upload API.
func (a *App) uploadMobileDocumentOriginal(filename string, raw []byte, contentType string) (*MobileDocumentDraftSummary, error) {
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

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Original display name as a separate field (handles non-ASCII filenames).
	if err := writer.WriteField("filename", filename); err != nil {
		return nil, err
	}
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
	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(raw); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, hubURL+"/api/mobile/documents/upload", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	client := &http.Client{Timeout: 90 * time.Second}
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
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
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

const mobileDocumentOriginalMaxBytes = 25 << 20

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

// fetchMobileDocumentOriginal downloads original bytes for a draft the viewer owns.
func (a *App) fetchMobileDocumentOriginal(draftID string) (filename string, raw []byte, err error) {
	draft, err := a.GetMobileDocumentDraft(draftID)
	if err != nil {
		return "", nil, err
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
	sourcePath := strings.TrimSpace(draft.SourceDownloadURL)
	if sourcePath == "" {
		sourcePath = "/api/mobile/documents/drafts/" + url.PathEscape(strings.TrimSpace(draft.ID)) + "/source"
	}
	fullURL := sourcePath
	if !strings.HasPrefix(sourcePath, "http://") && !strings.HasPrefix(sourcePath, "https://") {
		if !strings.HasPrefix(sourcePath, "/") {
			sourcePath = "/" + sourcePath
		}
		fullURL = hubURL + sourcePath
	}
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
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
		return "", nil, fmt.Errorf("original file exceeds 25MB limit")
	}
	filename = strings.TrimSpace(draft.SourceFilename)
	if filename == "" {
		filename = strings.TrimSpace(draft.Title)
	}
	if filename == "" {
		filename = draft.ID + ".bin"
	}
	filename = filepath.Base(filename)
	return filename, body, nil
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
