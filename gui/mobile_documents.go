package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// MobileDocumentDraftSummary is a Hub-shared emergency draft visible on desktop.
type MobileDocumentDraftSummary struct {
	ID                string `json:"id"`
	Title             string `json:"title"`
	Template          string `json:"template"`
	UpdatedAt         string `json:"updated_at"`
	RuneCount         int    `json:"rune_count"`
	Preview           string `json:"preview"`
	Markdown          string `json:"markdown,omitempty"`
	HasOriginal       bool   `json:"has_original,omitempty"`
	SourceFilename    string `json:"source_filename,omitempty"`
	SourceContentType string `json:"source_content_type,omitempty"`
	SourceSize        int    `json:"source_size,omitempty"`
	SourceDownloadURL string `json:"source_download_url,omitempty"`
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

// percentEncodeRFC5987 encodes a filename for Content-Disposition filename*=UTF-8''…
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
	return out
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
