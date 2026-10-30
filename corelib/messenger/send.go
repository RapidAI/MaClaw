package messenger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var accessTokenInError = regexp.MustCompile(`access_token=[^&\s"']+`)

// SendRequest is one Graph Send API call.
type SendRequest struct {
	To        string
	Text      string
	MediaURL  string
	ReplyToID string
}

type sendAPIResponse struct {
	RecipientID string `json:"recipient_id"`
	MessageID   string `json:"message_id"`
	Error       *struct {
		Message      string `json:"message"`
		Code         int    `json:"code"`
		ErrorSubcode int    `json:"error_subcode"`
	} `json:"error"`
}

// Send delivers text or a public media URL through the Page messages edge.
// Text longer than MessageTextLimit is split. Only the first chunk quotes ReplyToID.
func (g *Gateway) Send(ctx context.Context, req SendRequest) (string, error) {
	if g == nil {
		return "", fmt.Errorf("facebook messenger gateway unavailable")
	}
	account, apiBase, client := g.transportSnapshot()
	if !account.CanRun() {
		return "", fmt.Errorf("facebook messenger is not configured")
	}
	req.To = NormalizePSID(req.To)
	req.Text = strings.TrimSpace(req.Text)
	req.MediaURL = strings.TrimSpace(req.MediaURL)
	req.ReplyToID = strings.TrimSpace(req.ReplyToID)
	if req.To == "" {
		return "", fmt.Errorf("facebook messenger target PSID is required")
	}
	if req.Text == "" && req.MediaURL == "" {
		return "", fmt.Errorf("facebook messenger send requires text or a media URL")
	}
	if req.MediaURL != "" && !strings.HasPrefix(strings.ToLower(req.MediaURL), "http://") && !strings.HasPrefix(strings.ToLower(req.MediaURL), "https://") {
		return "", fmt.Errorf("facebook messenger media sends require a public http(s) URL")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var lastID string
	if req.Text != "" {
		parts := splitMessengerText(req.Text, MessageTextLimit)
		for i, part := range parts {
			replyTo := ""
			if i == 0 {
				replyTo = req.ReplyToID
			}
			id, err := postMessage(ctx, client, apiBase, account, req.To, part, "", replyTo)
			if err != nil {
				return lastID, err
			}
			g.rememberQuote(id, part)
			lastID = id
		}
	}
	if req.MediaURL != "" {
		replyTo := ""
		if req.Text == "" {
			replyTo = req.ReplyToID
		}
		id, err := postMessage(ctx, client, apiBase, account, req.To, "", req.MediaURL, replyTo)
		if err != nil {
			return lastID, err
		}
		g.rememberQuote(id, req.MediaURL)
		lastID = id
	}
	return lastID, nil
}

func (g *Gateway) transportSnapshot() (Account, string, *http.Client) {
	g.mu.Lock()
	defer g.mu.Unlock()
	client := g.client
	if client == nil {
		client = http.DefaultClient
	}
	return g.account, g.apiBase, client
}

func postMessage(ctx context.Context, client *http.Client, apiBase string, account Account, to, text, mediaURL, replyToID string) (string, error) {
	message := map[string]any{}
	if text != "" {
		message["text"] = text
	}
	if mediaURL != "" {
		message["attachment"] = map[string]any{
			"type": inferAttachmentType(mediaURL),
			"payload": map[string]any{
				"url":         mediaURL,
				"is_reusable": true,
			},
		}
	}
	body := map[string]any{
		"recipient":      map[string]string{"id": to},
		"messaging_type": "RESPONSE",
		"message":        message,
	}
	if replyToID != "" {
		body["reply_to"] = map[string]string{"mid": replyToID}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	endpoint, err := sendAPIURL(apiBase, account)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", redactMessengerError(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed sendAPIResponse
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || parsed.Error != nil {
		message := strings.TrimSpace(parsed.ErrorMessage())
		if message == "" {
			message = strings.TrimSpace(resp.Status)
		}
		return "", redactMessengerError(fmt.Errorf("facebook messenger send failed: %s", message))
	}
	if parsed.MessageID == "" {
		return "", fmt.Errorf("facebook messenger send response did not include message_id")
	}
	return parsed.MessageID, nil
}

func redactMessengerError(err error) error {
	if err == nil {
		return nil
	}
	raw := err.Error()
	cleaned := accessTokenInError.ReplaceAllString(raw, "access_token=REDACTED")
	if cleaned == raw {
		return err
	}
	return errors.New(cleaned)
}

func (e *sendAPIResponse) ErrorMessage() string {
	if e == nil || e.Error == nil {
		return ""
	}
	msg := strings.TrimSpace(e.Error.Message)
	if e.Error.Code != 0 {
		msg = fmt.Sprintf("%s (code=%d)", msg, e.Error.Code)
	}
	if e.Error.ErrorSubcode != 0 {
		msg = fmt.Sprintf("%s (subcode=%d)", msg, e.Error.ErrorSubcode)
	}
	return strings.TrimSpace(msg)
}

func sendAPIURL(apiBase string, account Account) (string, error) {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = "https://graph.facebook.com"
	}
	account = account.Normalized()
	endpoint, err := url.Parse(apiBase + "/" + account.GraphAPIVersion + "/" + url.PathEscape(account.PageID) + "/messages")
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("access_token", account.PageAccessToken)
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func inferAttachmentType(rawURL string) string {
	clean := strings.ToLower(strings.SplitN(rawURL, "?", 2)[0])
	clean = strings.SplitN(clean, "#", 2)[0]
	switch {
	case strings.HasSuffix(clean, ".jpg"), strings.HasSuffix(clean, ".jpeg"), strings.HasSuffix(clean, ".png"), strings.HasSuffix(clean, ".gif"), strings.HasSuffix(clean, ".webp"):
		return "image"
	case strings.HasSuffix(clean, ".mp3"), strings.HasSuffix(clean, ".m4a"), strings.HasSuffix(clean, ".wav"), strings.HasSuffix(clean, ".ogg"), strings.HasSuffix(clean, ".oga"), strings.HasSuffix(clean, ".aac"):
		return "audio"
	case strings.HasSuffix(clean, ".mp4"), strings.HasSuffix(clean, ".mov"), strings.HasSuffix(clean, ".m4v"), strings.HasSuffix(clean, ".webm"):
		return "video"
	default:
		return "file"
	}
}

func splitMessengerText(text string, limit int) []string {
	if limit <= 0 {
		limit = MessageTextLimit
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	runes := []rune(text)
	if utf16Units(runes) <= limit {
		return []string{text}
	}
	out := make([]string, 0, utf16Units(runes)/limit+1)
	for len(runes) > 0 {
		n := runeCountWithinUTF16(runes, limit)
		if n == 0 {
			n = 1
		} else if n < len(runes) {
			if cut := lastBreak(runes[:n]); cut > 0 {
				n = cut
			}
		}
		part := strings.TrimSpace(string(runes[:n]))
		if part != "" {
			out = append(out, part)
		}
		runes = runes[n:]
		for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\n' || runes[0] == '\t') {
			runes = runes[1:]
		}
	}
	return out
}

// Meta counts Messenger text in UTF-16 code units. Emoji above U+FFFF take two.
func utf16Units(runes []rune) int {
	n := 0
	for _, r := range runes {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

func runeCountWithinUTF16(runes []rune, limit int) int {
	units := 0
	for i, r := range runes {
		u := 1
		if r > 0xFFFF {
			u = 2
		}
		if units+u > limit {
			return i
		}
		units += u
	}
	return len(runes)
}

func lastBreak(window []rune) int {
	floor := len(window) - 200
	if floor < 1 {
		floor = 1
	}
	for i := len(window) - 1; i >= floor; i-- {
		if window[i] == '\n' || window[i] == ' ' {
			return i
		}
	}
	return 0
}
