package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"
)

const veImageAttachmentPreviewMaxSize = 10 * 1024 * 1024
const veImageAttachmentThumbnailMaxSide = 240

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
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("create local attachment temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	size, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, veFileAttachmentMaxSize+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("write attachment: %w", copyErr)
	}
	if closeErr != nil {
		return GroupDiscussionAttachmentDownloadResult{}, closeErr
	}
	if size > veFileAttachmentMaxSize {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("attachment is too large: %d bytes; VE mode limit is 50 MB", size)
	}
	localPath, localName, err := groupDiscussionCommitTempAttachment(tmpPath, dir, localName)
	if err != nil {
		return GroupDiscussionAttachmentDownloadResult{}, fmt.Errorf("store local attachment: %w", err)
	}

	result := GroupDiscussionAttachmentDownloadResult{DiscussionID: discussionID, AttachmentID: attachmentID, Filename: filename, LocalPath: localPath, SizeBytes: size}
	if store, err := a.openGroupDiscussionHistoryStore(); err == nil {
		_ = store.UpsertDownloadedAttachment(ctx, GroupDiscussionAttachmentRecord{AttachmentID: firstNonEmptyGroupString(attachmentID, localName), DiscussionID: discussionID, Kind: groupDiscussionAttachmentKind(filename), Filename: filename, HubURL: fileURL, LocalPath: localPath, SizeBytes: size, DownloadState: "downloaded"})
		_ = store.Close()
	}
	return result, nil
}

func (a *App) GroupDiscussionAttachmentPreviewDataURL(discussionID, localPath string) (string, error) {
	discussionID = strings.TrimSpace(discussionID)
	localPath = strings.TrimSpace(localPath)
	if discussionID == "" {
		return "", fmt.Errorf("discussion id is required")
	}
	if localPath == "" {
		return "", fmt.Errorf("local path is required")
	}
	attachmentRoot := a.groupDiscussionAttachmentRoot(discussionID)
	if !groupDiscussionPathWithinDir(localPath, attachmentRoot) {
		return "", fmt.Errorf("attachment preview path must stay within the discussion attachment directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(attachmentRoot)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(localPath)
	if err != nil {
		return "", err
	}
	if !groupDiscussionPathWithinDir(resolvedPath, resolvedRoot) {
		return "", fmt.Errorf("attachment preview path must stay within the discussion attachment directory")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("attachment preview path is a directory")
	}
	if info.Size() > veImageAttachmentPreviewMaxSize {
		return "", fmt.Errorf("image attachment preview is too large: %d bytes", info.Size())
	}
	mimeType := groupDiscussionPreviewImageMimeType(localPath)
	if mimeType == "" {
		return "", fmt.Errorf("attachment is not a supported preview image")
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", err
	}
	if thumb, ok := thumbnailImageDataURL(data); ok {
		return thumb, nil
	}
	if !previewImageBytesMatch(mimeType, data) {
		return "", fmt.Errorf("attachment preview image bytes are invalid")
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
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
		if localPath == "" || !groupDiscussionResolvedPathWithinDir(localPath, a.groupDiscussionAttachmentRoot(discussionID)) {
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

func ensureGroupDiscussionAttachmentWritableTarget(localPath string) error {
	existing, err := os.Lstat(localPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect local attachment: %w", err)
	}
	if existing.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("local attachment path is a symlink")
	}
	if existing.IsDir() {
		return fmt.Errorf("local attachment path is a directory")
	}
	return nil
}

func groupDiscussionAvailableAttachmentTarget(dir, localName string) (string, string, error) {
	localName = firstNonEmptyGroupString(safeGroupDiscussionFilename(localName), "attachment")
	ext := filepath.Ext(localName)
	base := strings.TrimSuffix(localName, ext)
	if base == "" {
		base = "attachment"
	}
	for i := 0; i < 1000; i++ {
		candidateName := localName
		if i > 0 {
			candidateName = safeGroupDiscussionFilename(fmt.Sprintf("%s (%d)%s", base, i, ext))
		}
		candidatePath := filepath.Join(dir, candidateName)
		existing, err := os.Lstat(candidatePath)
		if os.IsNotExist(err) {
			return candidatePath, candidateName, nil
		}
		if err != nil {
			return "", "", fmt.Errorf("inspect local attachment: %w", err)
		}
		if existing.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("local attachment path is a symlink")
		}
		if existing.IsDir() {
			return "", "", fmt.Errorf("local attachment path is a directory")
		}
	}
	return "", "", fmt.Errorf("no available local attachment filename")
}

func groupDiscussionCommitTempAttachment(tmpPath, dir, localName string) (string, string, error) {
	for i := 0; i < 1000; i++ {
		localPath, candidateName, err := groupDiscussionAvailableAttachmentTarget(dir, localName)
		if err != nil {
			return "", "", err
		}
		if err := ensureGroupDiscussionAttachmentWritableTarget(localPath); err != nil {
			return "", "", err
		}
		if err := os.Link(tmpPath, localPath); err != nil {
			if os.IsExist(err) {
				continue
			}
			if err := copyTempAttachmentNoOverwrite(tmpPath, localPath); err != nil {
				if os.IsExist(err) {
					continue
				}
				return "", "", err
			}
		}
		return localPath, candidateName, nil
	}
	return "", "", fmt.Errorf("no available local attachment filename")
}

func copyTempAttachmentNoOverwrite(tmpPath, localPath string) error {
	in, err := os.Open(tmpPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	removeOnError := true
	defer func() {
		_ = out.Close()
		if removeOnError {
			_ = os.Remove(localPath)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	removeOnError = false
	return nil
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

func groupDiscussionResolvedPathWithinDir(pathValue, dir string) bool {
	if !groupDiscussionPathWithinDir(pathValue, dir) {
		return false
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	resolvedPath, err := filepath.EvalSymlinks(pathValue)
	if err != nil {
		return false
	}
	return groupDiscussionPathWithinDir(resolvedPath, resolvedDir)
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

func groupDiscussionPreviewImageMimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".avif":
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return strings.Split(mimeType, ";")[0]
		}
	}
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".avif":
		return "image/avif"
	default:
		return ""
	}
}

func thumbnailImageDataURL(data []byte) (string, bool) {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", false
	}
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return "", false
	}
	thumbWidth, thumbHeight := scaledImageSize(width, height, veImageAttachmentThumbnailMaxSide)
	dst := image.NewRGBA(image.Rect(0, 0, thumbWidth, thumbHeight))
	for y := 0; y < thumbHeight; y++ {
		srcY := bounds.Min.Y + y*height/thumbHeight
		for x := 0; x < thumbWidth; x++ {
			srcX := bounds.Min.X + x*width/thumbWidth
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 82}); err != nil {
		return "", false
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), true
}

func previewImageBytesMatch(mimeType string, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/webp":
		return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	case "image/bmp":
		return len(data) >= 2 && data[0] == 'B' && data[1] == 'M'
	case "image/avif":
		return len(data) >= 12 && string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis")
	default:
		return strings.EqualFold(strings.Split(http.DetectContentType(data), ";")[0], mimeType)
	}
}

func scaledImageSize(width, height, maxSide int) (int, int) {
	if maxSide <= 0 || (width <= maxSide && height <= maxSide) {
		return width, height
	}
	if width >= height {
		scaledHeight := height * maxSide / width
		if scaledHeight < 1 {
			scaledHeight = 1
		}
		return maxSide, scaledHeight
	}
	scaledWidth := width * maxSide / height
	if scaledWidth < 1 {
		scaledWidth = 1
	}
	return scaledWidth, maxSide
}
