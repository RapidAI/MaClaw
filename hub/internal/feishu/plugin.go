package feishu

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/go-lark/lark/v2"
)

// FeishuPlugin implements the im.IMPlugin interface by composing the existing
// Notifier (outbound messaging, user resolution, card building) and
// WebhookHandler (inbound message reception) functionality.
//
// When an IM Adapter is wired (via SetAdapter), incoming bot messages are
// converted to im.IncomingMessage and routed through the adapter pipeline.
// When no adapter is set, the plugin falls back to the legacy handleCommand /
// handleSendInput behaviour for full backward compatibility.
type FeishuPlugin struct {
	notifier *Notifier

	// messageHandler is the callback registered by IM Adapter via ReceiveMessage.
	messageHandler func(msg im.IncomingMessage)

	// adapter is an optional reference to the IM Adapter. When set,
	// handleBotMessage routes messages through the adapter pipeline.
	adapter IMAdapter

	// publicBaseURL is the hub's externally reachable URL, used to
	// construct temporary download URLs for large file uploads.
	publicBaseURL string

	// tempFiles stores metadata for temporary file downloads keyed by token.
	// File data is persisted to disk under tempDir; metadata is kept in memory
	// for fast lookup and expiry tracking.
	tempMu    sync.Mutex
	tempFiles map[string]*feishuTempFileEntry
	tempDir   string // directory for temp file storage on disk
}

// feishuTempFileEntry holds metadata for a temporary download file.
// The actual bytes live on disk at filepath.Join(tempDir, token).
type feishuTempFileEntry struct {
	FileName  string
	MimeType  string
	ExpiresAt time.Time
}

const (
	// feishuTempFileTTL is how long a temp file is kept before cleanup.
	feishuTempFileTTL = 10 * time.Minute
	// feishuUploadMaxSize: files larger than this (raw bytes) use the
	// temp-URL fallback instead of Feishu's UploadFile API.
	feishuUploadMaxSize = 30 * 1024 * 1024 // 30 MB — Feishu im/v1/files limit
)

// IMAdapter is a minimal interface for the IM Adapter so that the feishu
// package does not import hub/internal/im (avoiding circular deps if needed).
// In practice the *im.Adapter satisfies this interface.
type IMAdapter interface {
	HandleMessage(ctx context.Context, msg im.IncomingMessage)
}

// NewPlugin creates a FeishuPlugin wrapping the given Notifier.
func NewPlugin(n *Notifier) *FeishuPlugin {
	dir := filepath.Join(os.TempDir(), "feishu-tempfiles")
	_ = os.MkdirAll(dir, 0o700)
	// Clean up any stale files from a previous run.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}
	return &FeishuPlugin{
		notifier:  n,
		tempFiles: make(map[string]*feishuTempFileEntry),
		tempDir:   dir,
	}
}

// SetPublicBaseURL sets the hub's externally reachable URL for temp file downloads.
func (p *FeishuPlugin) SetPublicBaseURL(url string) {
	p.publicBaseURL = strings.TrimRight(url, "/")
}

// SetAdapter wires the IM Adapter for message routing. When set, incoming
// bot messages are converted to IncomingMessage and forwarded to the adapter
// instead of the legacy command handler.
func (p *FeishuPlugin) SetAdapter(a IMAdapter) {
	p.adapter = a
}

// ---------------------------------------------------------------------------
// im.IMPlugin interface implementation
// ---------------------------------------------------------------------------

// Name returns the plugin identifier.
func (p *FeishuPlugin) Name() string { return "feishu" }

// ReceiveMessage registers a callback that the IM Adapter uses to receive
// standardised incoming messages from this plugin.
func (p *FeishuPlugin) ReceiveMessage(handler func(msg im.IncomingMessage)) {
	p.messageHandler = handler
}

// SendText sends a plain text message to the target user via Feishu.
// Reuses the existing replyText logic.
func (p *FeishuPlugin) SendText(ctx context.Context, target im.UserTarget, text string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}
	replyText(p.notifier, openID, text)
	return nil
}

// SendCard sends a rich card message to the target user via Feishu.
// Converts the OutgoingMessage to a Feishu interactive card using the
// existing buildCardJSON logic.
func (p *FeishuPlugin) SendCard(ctx context.Context, target im.UserTarget, card im.OutgoingMessage) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	// Convert OutgoingMessage fields to cardField slice for buildCardJSON.
	var fields []cardField
	if card.StatusIcon != "" || card.StatusCode != 0 {
		// Include status as a field if present.
		statusText := card.StatusIcon
		if card.StatusCode != 0 {
			statusText = fmt.Sprintf("%s %d", card.StatusIcon, card.StatusCode)
		}
		if statusText != "" {
			fields = append(fields, cardField{label: "状态", value: statusText})
		}
	}
	if card.Body != "" {
		fields = append(fields, cardField{label: "详情", value: card.Body})
	}
	for _, f := range card.Fields {
		fields = append(fields, cardField{label: f.Label, value: f.Value})
	}
	// Append action buttons as text hints (Feishu cards support markdown).
	for _, a := range card.Actions {
		hint := a.Label
		if a.Command != "" {
			hint = fmt.Sprintf("%s → `%s`", a.Label, a.Command)
		}
		fields = append(fields, cardField{label: "操作", value: hint})
	}

	title := card.Title
	if title == "" {
		title = "消息"
	}
	color := statusColorFromCode(card.StatusCode)
	cardJSON := buildCardJSON(title, color, fields)

	p.notifier.sendToUserByOpenID(openID, cardJSON)
	return nil
}

// SendImage sends an image message to the target user via Feishu.
// If imageKey looks like base64-encoded PNG data (not a Feishu image_key),
// it is decoded, uploaded to Feishu, and then sent.
func (p *FeishuPlugin) SendImage(ctx context.Context, target im.UserTarget, imageKey string, caption string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	// Detect base64 PNG data vs Feishu image_key.
	// Feishu image_keys look like "img_v2_xxx" or "img_xxx".
	if !strings.HasPrefix(imageKey, "img_") && len(imageKey) > 200 {
		// Likely base64 image data — decode and upload to Feishu.
		uploaded, err := p.uploadBase64Image(ctx, imageKey)
		if err != nil {
			return fmt.Errorf("feishu: upload image failed: %w", err)
		}
		imageKey = uploaded
	}

	sendImagePost(p.notifier, openID, imageKey, caption)
	return nil
}

// uploadBase64Image decodes base64 PNG data and uploads it to Feishu,
// returning the Feishu image_key.
func (p *FeishuPlugin) uploadBase64Image(ctx context.Context, b64Data string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64Data)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	// Decode any supported image format (png, jpeg, webp, gif, bmp, tiff).
	img, format, err := decodeAnyImage(raw)
	if err != nil {
		return "", fmt.Errorf("image decode: %w", err)
	}
	log.Printf("[feishu] uploadBase64Image: detected format=%s", format)

	bot := p.notifier.Bot()
	if bot == nil {
		return "", fmt.Errorf("feishu bot not initialized")
	}

	// Use a dedicated context with generous timeout for image upload.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	resp, err := bot.UploadImageObject(uploadCtx, img)
	if err != nil {
		return "", fmt.Errorf("upload to feishu: %w", err)
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("feishu API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return resp.Data.ImageKey, nil
}

// SendAudio sends an audio message (voice bubble) to the target user via Feishu.
// The audioData must be OGG Opus format (飞书 requires file_type=opus for audio messages).
// Uses msg_type=audio which displays as a playable voice bubble in the chat.
func (p *FeishuPlugin) SendAudio(ctx context.Context, target im.UserTarget, audioData []byte) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required for audio")
	}

	bot := p.notifier.Bot()
	if bot == nil {
		return fmt.Errorf("feishu bot not initialized")
	}

	// Step 1: Upload audio with file_type=opus.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	uploadResp, err := bot.UploadFile(uploadCtx, lark.UploadFileRequest{
		FileType: "opus", // 飞书 requires opus for audio messages
		FileName: "voice.ogg",
		Reader:   bytes.NewReader(audioData),
	})
	if err != nil {
		return fmt.Errorf("feishu: upload audio: %w", err)
	}
	if uploadResp.Code != 0 {
		return fmt.Errorf("feishu: upload audio API error: code=%d msg=%s", uploadResp.Code, uploadResp.Msg)
	}

	fileKey := uploadResp.Data.FileKey

	// Step 2: Send audio message (msg_type=audio).
	msg := lark.NewMsgBuffer(lark.MsgAudio).
		BindOpenID(openID).
		Audio(fileKey).
		Build()

	if _, err := bot.PostMessage(ctx, msg); err != nil {
		return fmt.Errorf("feishu: send audio message: %w", err)
	}

	return nil
}

// SendFile sends a file to the target user via Feishu.
// Small files (≤ feishuUploadMaxSize) are uploaded via the Feishu file API
// and sent as native file messages. Large files are stored as temporary
// downloads on the hub and a text link is sent instead.
func (p *FeishuPlugin) SendFile(ctx context.Context, target im.UserTarget, fileData, fileName, mimeType string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}

	bot := p.notifier.Bot()
	if bot == nil {
		return fmt.Errorf("feishu bot not initialized")
	}

	raw, err := base64.StdEncoding.DecodeString(fileData)
	if err != nil {
		return fmt.Errorf("feishu: base64 decode: %w", err)
	}

	// Determine file type for Feishu API.
	if strings.HasPrefix(mimeType, "image/") {
		// For images, use SendImage path instead.
		return p.SendImage(ctx, target, fileData, fileName)
	}

	// Large file → temp URL fallback.
	if len(raw) > feishuUploadMaxSize {
		if p.publicBaseURL == "" {
			return fmt.Errorf("feishu: file too large (%d bytes, max %d) and no public base URL configured for temp download", len(raw), feishuUploadMaxSize)
		}
		return p.sendFileViaTempURL(ctx, target, raw, fileName, mimeType)
	}

	// Small file → native Feishu upload.
	fileType := "stream"

	// Use a dedicated context with generous timeout for file upload,
	// since the caller's context may have a shorter deadline that's
	// insufficient for large file uploads to Feishu.
	uploadCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Upload file to Feishu.
	uploadResp, err := bot.UploadFile(uploadCtx, lark.UploadFileRequest{
		FileType: fileType,
		FileName: fileName,
		Reader:   bytes.NewReader(raw),
	})
	if err != nil {
		return fmt.Errorf("feishu: upload file: %w", err)
	}
	if uploadResp.Code != 0 {
		return fmt.Errorf("feishu: upload file API error: code=%d msg=%s", uploadResp.Code, uploadResp.Msg)
	}

	fileKey := uploadResp.Data.FileKey

	// Send file message.
	msg := lark.NewMsgBuffer(lark.MsgFile).
		BindOpenID(openID).
		File(fileKey).
		Build()

	if _, err := bot.PostMessage(ctx, msg); err != nil {
		return fmt.Errorf("feishu: send file message: %w", err)
	}

	return nil
}

// sendFileViaTempURL stores the file on disk and sends a download link
// to the user as a text message.
func (p *FeishuPlugin) sendFileViaTempURL(ctx context.Context, target im.UserTarget, raw []byte, fileName, mimeType string) error {
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	token := feishuGenerateTempToken()

	// Write file data to disk instead of holding it in memory.
	filePath := filepath.Join(p.tempDir, token)
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		return fmt.Errorf("feishu: write temp file to disk: %w", err)
	}

	p.tempMu.Lock()
	p.tempFiles[token] = &feishuTempFileEntry{
		FileName:  fileName,
		MimeType:  mimeType,
		ExpiresAt: time.Now().Add(feishuTempFileTTL),
	}
	// Inline cleanup of expired entries.
	now := time.Now()
	for k, v := range p.tempFiles {
		if now.After(v.ExpiresAt) {
			delete(p.tempFiles, k)
			_ = os.Remove(filepath.Join(p.tempDir, k))
		}
	}
	p.tempMu.Unlock()

	downloadURL := fmt.Sprintf("%s/api/feishu/tempfile/%s", p.publicBaseURL, token)
	sizeMB := float64(len(raw)) / (1024 * 1024)
	text := fmt.Sprintf("📎 文件过大（%.1f MB），请通过链接下载（%d 分钟内有效）：\n%s\n文件名: %s",
		sizeMB, int(feishuTempFileTTL.Minutes()), downloadURL, fileName)

	return p.sendTextWithLink(ctx, target, text, downloadURL, "点击下载文件")
}

// ServeTempFile serves a previously stored temporary file for download.
func (p *FeishuPlugin) ServeTempFile(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}

	// Reject tokens that could be used for path traversal.
	if strings.Contains(token, "..") || strings.ContainsAny(token, "/\\") || token != filepath.Base(token) {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	// Feishu (and other IM clients) often send a preflight/preview request
	// (e.g. HEAD, or a GET from a link-preview bot) before the user actually
	// clicks the link. If we delete the file on the preview fetch, the real
	// user download will 404. Detect preview requests and skip deletion.
	ua := strings.ToLower(r.UserAgent())
	isPreview := r.Method == http.MethodHead ||
		strings.Contains(ua, "bot") ||
		strings.Contains(ua, "spider") ||
		strings.Contains(ua, "preview") ||
		strings.Contains(ua, "lark") ||
		strings.Contains(ua, "feishu")

	log.Printf("[feishu/tempfile] request token=%s method=%s preview=%v UA=%s", token, r.Method, isPreview, r.UserAgent())

	filePath := filepath.Join(p.tempDir, token)

	p.tempMu.Lock()
	entry, ok := p.tempFiles[token]
	p.tempMu.Unlock()

	if !ok {
		// Fallback: check if the file exists on disk even though metadata
		// is missing (e.g. after a process restart that cleared the map).
		if _, statErr := os.Stat(filePath); statErr != nil {
			log.Printf("[feishu/tempfile] token=%s not found in map or disk", token)
			http.Error(w, "file not found or expired", http.StatusNotFound)
			return
		}
		// File exists on disk but metadata was lost — serve with generic headers.
		p.serveTempFileFromDisk(w, filePath, "application/octet-stream", "download")
		if !isPreview {
			_ = os.Remove(filePath)
		}
		return
	}

	if time.Now().After(entry.ExpiresAt) {
		p.tempMu.Lock()
		delete(p.tempFiles, token)
		p.tempMu.Unlock()
		_ = os.Remove(filePath)
		http.Error(w, "file expired", http.StatusGone)
		return
	}

	p.serveTempFileFromDisk(w, filePath, entry.MimeType, entry.FileName)

	// Only remove after a real user download, not a bot/preview fetch.
	if !isPreview {
		p.tempMu.Lock()
		delete(p.tempFiles, token)
		p.tempMu.Unlock()
		_ = os.Remove(filePath)
	}
}

// serveTempFileFromDisk streams a file from disk to the HTTP response.
// Uses io.Copy for memory-efficient transfer of large files.
func (p *FeishuPlugin) serveTempFileFromDisk(w http.ResponseWriter, filePath, mimeType, fileName string) {
	f, err := os.Open(filePath)
	if err != nil {
		log.Printf("[feishu/tempfile] failed to open %s: %v", filePath, err)
		http.Error(w, "file not found or expired", http.StatusNotFound)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}

	log.Printf("[feishu/tempfile] serving file=%s size=%d", fileName, info.Size())

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	if fileName != "" {
		safe := strings.Map(func(r rune) rune {
			if r > 127 || r == '"' || r == '\\' {
				return '_'
			}
			return r
		}, fileName)
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, safe, url.PathEscape(fileName)))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

// sendTextWithLink sends a Feishu post (rich text) message that includes a
// clickable hyperlink. This avoids the problem where plain-text URLs in Feishu
// post messages are not always rendered as clickable links.
func (p *FeishuPlugin) sendTextWithLink(ctx context.Context, target im.UserTarget, text, linkURL, linkText string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}
	bot := p.notifier.Bot()
	if bot == nil {
		return fmt.Errorf("feishu bot not initialized")
	}

	// Build post content: split text by \n, replace the raw URL line with
	// an <a> element so it's clickable in Feishu.
	lines := strings.Split(text, "\n")
	var rows [][]lark.PostElem
	for _, line := range lines {
		if strings.TrimSpace(line) == linkURL {
			// Replace the raw URL line with a clickable link element.
			lt := linkText
			href := linkURL
			rows = append(rows, []lark.PostElem{
				{Tag: "a", Text: &lt, Href: &href},
			})
		} else {
			t := line
			rows = append(rows, []lark.PostElem{
				{Tag: "text", Text: &t},
			})
		}
	}
	pc := lark.PostContent{
		"zh_cn": {Content: rows},
	}
	msg := lark.NewMsgBuffer(lark.MsgPost).
		BindOpenID(openID).
		Post(&pc).
		Build()
	if _, err := bot.PostMessage(ctx, msg); err != nil {
		return fmt.Errorf("feishu: send post with link: %w", err)
	}
	return nil
}

func feishuGenerateTempToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SendUrgentText sends a text message with Feishu in-app urgent (加急) notification.
// Implements the im.UrgentSender interface.
func (p *FeishuPlugin) SendUrgentText(ctx context.Context, target im.UserTarget, text string) error {
	openID := target.PlatformUID
	if openID == "" {
		return fmt.Errorf("feishu: PlatformUID (open_id) is required")
	}
	bot := p.notifier.Bot()
	if bot == nil {
		return fmt.Errorf("feishu bot not initialized")
	}
	// Send the text message.
	msg := lark.NewMsgBuffer(lark.MsgText).
		BindOpenID(openID).
		Text(text).
		Build()
	resp, err := bot.PostMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("feishu: send urgent text: %w", err)
	}
	// Follow up with BuzzMessage (in-app urgent).
	if resp != nil && resp.Data.MessageID != "" {
		p.notifier.buzzMessage(resp.Data.MessageID, openID)
	}
	return nil
}

// ResolveUser maps a Feishu open_id to the unified internal user ID.
// Reuses the existing resolveUserID logic (open_id → email → userID).
func (p *FeishuPlugin) ResolveUser(ctx context.Context, platformUID string) (string, error) {
	userID := p.notifier.resolveUserID(platformUID)
	if userID == "" {
		return "", fmt.Errorf("feishu: cannot resolve user for open_id %s (not bound)", platformUID)
	}
	return userID, nil
}

// LookupByEmail returns the Feishu open_id bound to the given email, or "".
// Implements im.BindingLookup for cross-IM verification.
func (p *FeishuPlugin) LookupByEmail(email string) string {
	return p.notifier.resolveOpenID(email)
}

// Capabilities declares that Feishu supports rich text cards, Markdown,
// images, and button interactions.
func (p *FeishuPlugin) Capabilities() im.CapabilityDeclaration {
	return im.CapabilityDeclaration{
		SupportsRichCard:    true,
		SupportsMarkdown:    true,
		SupportsImage:       true,
		SupportsFile:        true,
		SupportsButton:      true,
		SupportsMessageEdit: false,
		SupportsVoice:       true, // OGG Opus via SendAudio (msg_type=audio)
		MaxTextLength:       4000,
	}
}

// Start is a no-op for Feishu — the webhook HTTP handler is registered
// externally and the lark.Bot is initialised by the Notifier constructor.
func (p *FeishuPlugin) Start(ctx context.Context) error {
	log.Printf("[feishu/plugin] started")
	return nil
}

// Stop is a no-op for Feishu — there are no persistent connections to tear down.
func (p *FeishuPlugin) Stop(ctx context.Context) error {
	log.Printf("[feishu/plugin] stopped")
	return nil
}

// ---------------------------------------------------------------------------
// Incoming message dispatch — bridge between webhook and IM Adapter
// ---------------------------------------------------------------------------

// DispatchBotMessage is called by the webhook handler (handleBotMessage) to
// route an incoming Feishu bot message. If the IM Adapter is wired, the
// message is converted to im.IncomingMessage and forwarded through the
// adapter pipeline. Otherwise, the legacy command/send-input flow is used.
//
// Returns true if the message was dispatched to the IM Adapter, false if
// the legacy path should handle it.
func (p *FeishuPlugin) DispatchBotMessage(openID, messageType, text string, attachments []im.MessageAttachment, raw json.RawMessage) bool {
	if p.adapter == nil && p.messageHandler == nil {
		return false // no adapter wired — use legacy path
	}

	msg := im.IncomingMessage{
		PlatformName: "feishu",
		PlatformUID:  openID,
		MessageType:  messageType,
		Text:         text,
		Attachments:  attachments,
		RawPayload:   raw,
		Timestamp:    time.Now(),
	}

	// If a message handler is registered (by IM Adapter's RegisterPlugin),
	// use it. This is the standard path.
	if p.messageHandler != nil {
		p.messageHandler(msg)
		return true
	}

	// Fallback: call adapter directly.
	if p.adapter != nil {
		p.adapter.HandleMessage(context.Background(), msg)
		return true
	}

	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// sendToUserByOpenID sends a Feishu interactive card to a user identified by
// open_id. This is a thin wrapper around the lark bot API.
func (n *Notifier) sendToUserByOpenID(openID, cardJSON string) {
	if n == nil || n.bot == nil || openID == "" {
		return
	}
	msg := newCardMessage(openID, cardJSON)
	ctx := context.Background()
	resp, err := n.bot.PostMessage(ctx, msg)
	if err != nil {
		log.Printf("[feishu/plugin] send card failed (open_id=%s): %v", openID, err)
		return
	}
	if resp != nil && resp.Code != 0 {
		log.Printf("[feishu/plugin] API error (open_id=%s): code=%d msg=%s", openID, resp.Code, resp.Msg)
	}
}

// newCardMessage builds a lark OutcomingMessage for an interactive card
// addressed to the given open_id.
func newCardMessage(openID, cardJSON string) lark.OutcomingMessage {
	return lark.NewMsgBuffer(lark.MsgInteractive).
		BindOpenID(openID).
		Card(cardJSON).
		Build()
}

// statusColorFromCode maps an HTTP-style status code to a Feishu card
// header template colour.
func statusColorFromCode(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "green"
	case code >= 400 && code < 500:
		return "orange"
	case code >= 500:
		return "red"
	default:
		return "blue"
	}
}

// isLegacyCommand returns true if the text starts with "/" — these should
// still be handled by the legacy handleCommand path even when the IM Adapter
// is active, to preserve full backward compatibility for slash commands.
func isLegacyCommand(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "/")
}
