package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"
)

type GroupDiscussionAttachmentDownloadResult struct {
	DiscussionID string `json:"discussion_id"`
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	LocalPath    string `json:"local_path"`
	SizeBytes    int64  `json:"size_bytes"`
}

func (a *App) GroupDiscussionDownloadAttachment(discussionID, fileURL, filename string) (GroupDiscussionAttachmentDownloadResult, error) {
	discussionID = strings.TrimSpace(discussionID)
	fileURL = strings.TrimSpace(fileURL)
	filename = safeGroupDiscussionFilename(filename)
	if discussionID == "" {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("discussion id is required")
	}
	if fileURL == "" {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("file url is required")
	}
	if filename == "" {
		filename = "attachment"
	}

	hubURL, token, err := a.getHubCredentials()
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("Hub credentials unavailable: %w", err)
	}
	cfg, _ := a.LoadConfig()
	participantID := strings.TrimSpace(groupDiscussionAgentID(cfg))
	downloadURL, attachmentID, err := groupDiscussionAttachmentDownloadURL(hubURL, fileURL, discussionID, participantID)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if cached, ok := a.cachedGroupDiscussionDownloadedAttachment(ctx, discussionID, fileURL, attachmentID, filename); ok {
		return cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if participantID != "" {
		req.Header.Set("X-Machine-ID", participantID)
	}
	resp, err := veFileRelayHTTPClient.Do(req)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("download failed (HTTP %d): %s", resp.StatusCode, truncateVEStr(string(body), 200))
	}

	dir := a.groupDiscussionAttachmentRoot(discussionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("create attachment dir: %w", err)
	}
	localName := filename
	if attachmentID != "" {
		localName = safeGroupDiscussionFilename(attachmentID + "-" + filename)
	}
	localPath := filepath.Join(dir, localName)
	out, err := os.Create(localPath)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("create local attachment: %w", err)
	}
	size, copyErr := io.Copy(out, io.LimitReader(resp.Body, veFileAttachmentMaxSize+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(localPath)
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("write attachment: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(localPath)
		return GroupDiscussionAttachmentDownloadResult{}, closeErr
	}
	if size > veFileAttachmentMaxSize {
		_ = os.Remove(localPath)
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("attachment is too large: %d bytes; VE mode limit is 50 MB", size)
	}

	result := GroupDiscussionAttachmentDownloadResult{DiscussionID: discussionID, AttachmentID: attachmentID, Filename: filename, LocalPath: localPath, SizeBytes: size}
	if store, err := a.openGroupDiscussionHistoryStore(); err == nil {
		_ = store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: firstNonEmptyGroupString(attachmentID, localName), DiscussionID: discussionID, Kind: groupDiscussionAttachmentKind(filename), Filename: filename, HubURL: fileURL, LocalPath: localPath, SizeBytes: size, DownloadState: "downloaded"})
		_ = store.Close()
	}
	return result, nil
}

func (a *App) cachedGroupDiscussionDownloadedAttachment(ctx context.Context, discussionID, fileURL, attachmentID, filename string) (GroupDiscussionAttachmentDownloadResult, bool) {
	store, err := a.openGroupDiscussionHistoryStore()
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, false
	}
	defer store.Close()
	records, err := store.DownloadedAttachments(ctx, discussionID)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, false
	}
	for _, record := range records {
		if !downloadedAttachmentRecordMatches(record, fileURL, attachmentID) {
			continue
		}
		localPath := strings.TrimSpace(record.LocalPath)
		if localPath == "" || !groupDiscussionPathWithinDir(localPath, a.groupDiscussionAttachmentRoot(discussionID)) {
			continue
		}
		info, err := os.Stat(localPath)
		if err != nil || info.IsDir() {
			continue
		}
		if info.Size() > veFileAttachmentMaxSize {
			continue
		}
		size := record.SizeBytes
		if size <= 0 || size > veFileAttachmentMaxSize {
			size = info.Size()
		}
		return GroupDiscussionAttachmentDownloadResult{
			DiscussionID: strings.TrimSpace(discussionID),
			AttachmentID: firstNonEmptyGroupString(strings.TrimSpace(attachmentID), strings.TrimSpace(record.AttachmentID)),
			Filename:     firstNonEmptyGroupString(strings.TrimSpace(record.Filename), strings.TrimSpace(filename), "attachment"),
			LocalPath:    localPath,
			SizeBytes:    size,
		}, true
	}
	return GroupDiscussionAttachmentDownloadResult{}, false
}

func downloadedAttachmentRecordMatches(record GroupDiscussionAttachmentRecord, fileURL, attachmentID string) bool {
	fileURL = strings.TrimSpace(fileURL)
	attachmentID = strings.TrimSpace(attachmentID)
	if attachmentID != "" && strings.EqualFold(strings.TrimSpace(record.AttachmentID), attachmentID) {
		return true
	}
	return fileURL != "" && strings.TrimSpace(record.HubURL) == fileURL
}

func groupDiscussionAttachmentDownloadURL(hubURL, rawURL, discussionID, participantID string) (string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if base == "" {
		return "", "", fmt.Errorf("hub url is required")
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return "", "", fmt.Errorf("invalid hub url")
	}
	if strings.HasPrefix(rawURL, "/") {
		rawURL = base + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("invalid file url: %w", err)
	}
	if !sameGroupDiscussionAttachmentOrigin(baseURL, u) {
		return "", "", fmt.Errorf("attachment file url must belong to the configured Hub")
	}
	path := u.EscapedPath()
	if !strings.HasPrefix(path, "/api/ve/files/") {
		return "", "", fmt.Errorf("attachment file url must use the Hub file relay")
	}
	if path == "/api/ve/files/upload" || strings.HasPrefix(path, "/api/ve/files/upload/") {
		return "", "", fmt.Errorf("attachment file url must use a file download endpoint")
	}
	attachmentID, err := groupDiscussionAttachmentIDFromRelayPath(path)
	if err != nil {
		return "", "", err
	}
	u.Path = "/api/ve/files/download/" + attachmentID
	u.RawPath = ""
	q := url.Values{}
	q.Set("session_id", discussionID)
	if participantID != "" {
		q.Set("participant_id", participantID)
	}
	u.RawQuery = q.Encode()
	return u.String(), attachmentID, nil
}

func sameGroupDiscussionAttachmentOrigin(baseURL, fileURL *url.URL) bool {
	if baseURL == nil || fileURL == nil {
		return false
	}
	return strings.EqualFold(fileURL.Scheme, baseURL.Scheme) && strings.EqualFold(fileURL.Host, baseURL.Host)
}

func groupDiscussionAttachmentIDFromRelayPath(escapedPath string) (string, error) {
	var escapedID string
	switch {
	case strings.HasPrefix(escapedPath, "/api/ve/files/download/"):
		escapedID = strings.TrimPrefix(escapedPath, "/api/ve/files/download/")
	case strings.HasPrefix(escapedPath, "/api/ve/files/"):
		escapedID = strings.TrimPrefix(escapedPath, "/api/ve/files/")
	default:
		return "", fmt.Errorf("attachment file url must use the Hub file relay")
	}
	if escapedID == "" || strings.Contains(escapedID, "/") {
		return "", fmt.Errorf("attachment file url must identify exactly one file")
	}
	id, err := url.PathUnescape(escapedID)
	if err != nil {
		return "", fmt.Errorf("invalid attachment file id: %w", err)
	}
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("attachment file url must identify exactly one file")
	}
	return safeGroupDiscussionPathSegment(id), nil
}

func groupDiscussionPathWithinDir(pathValue, dir string) bool {
	pathValue = strings.TrimSpace(pathValue)
	dir = strings.TrimSpace(dir)
	if pathValue == "" || dir == "" {
		return false
	}
	cleanPath, err := filepath.Abs(filepath.Clean(pathValue))
	if err != nil {
		return false
	}
	cleanDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanDir, cleanPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func pathBaseNoQuery(path string) string {
	path = strings.TrimRight(path, "/")
	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return safeGroupDiscussionPathSegment(base)
}

func safeGroupDiscussionFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimSpace(pathpkg.Base(value))
	if value == "." || value == "/" || value == string(filepath.Separator) {
		value = ""
	}
	if value == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "." || out == ".." {
		return "_" + out
	}
	return out
}

func groupDiscussionAttachmentKind(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return "image"
	case ".txt", ".md", ".csv", ".json", ".xml", ".yaml", ".yml", ".log", ".go", ".py", ".js", ".ts", ".html", ".css":
		return "text"
	default:
		return "file"
	}
}
