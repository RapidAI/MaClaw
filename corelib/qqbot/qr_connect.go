package qqbot

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultQRHost is the production QQ Bot bind API host.
	DefaultQRHost = "q.qq.com"
	// DefaultQRSource is shown on the scan page as the connecting product.
	DefaultQRSource = "maclaw"
	// DefaultQRConnectPath is the mobile-facing bind page encoded into the QR payload.
	DefaultQRConnectPath = "/qqbot/openclaw/connect.html"

	bindTaskPath   = "/lite/create_bind_task"
	bindPollPath   = "/lite/poll_bind_result"
	bindKeyBytes   = 32
	gcmNonceSize   = 12
	gcmTagSize     = 16
	qrHTTPTimeout  = 10 * time.Second
	qrSessionTTL   = 10 * time.Minute
)

// Bind status values returned by poll_bind_result.
const (
	BindStatusNone      = 0
	BindStatusPending   = 1
	BindStatusCompleted = 2
	BindStatusExpired   = 3
)

// QRLoginStatus is the UI-facing poll status, aligned with the WeChat QR flow.
type QRLoginStatus string

const (
	QRLoginStatusWait      QRLoginStatus = "wait"
	QRLoginStatusPending   QRLoginStatus = "pending"
	QRLoginStatusConfirmed QRLoginStatus = "confirmed"
	QRLoginStatusExpired   QRLoginStatus = "expired"
)

func (s QRLoginStatus) String() string { return string(s) }

var (
	ErrQRCodeTokenEmpty   = errors.New("qrcode token is empty")
	ErrQRSessionNotFound  = errors.New("qrcode token is not active")
	ErrQRBindIncomplete   = errors.New("bind result is incomplete")
	ErrQRSecretDecrypt    = errors.New("decrypt bot secret failed")
	ErrQRCiphertextShort  = errors.New("encrypted secret is too short")
)

// QRCredentials is returned after a successful scan bind.
type QRCredentials struct {
	AppID      string
	AppSecret  string
	UserOpenID string
}

type bindSession struct {
	key     []byte
	created time.Time
}

// QRClient talks to q.qq.com lite bind APIs and keeps AES keys in memory.
type QRClient struct {
	// BaseURL overrides the API origin (httptest). Empty uses https://q.qq.com.
	BaseURL string
	// Source is appended to the QR URL. Empty uses DefaultQRSource.
	Source string
	// HTTP is the outbound client. Nil uses a 10s-timeout default.
	HTTP *http.Client

	mu       sync.Mutex
	sessions map[string]bindSession
}

func NewQRClient() *QRClient {
	return &QRClient{
		Source:   DefaultQRSource,
		sessions: map[string]bindSession{},
	}
}

var (
	defaultQRClient     = NewQRClient()
	defaultQRHTTPClient = &http.Client{Timeout: qrHTTPTimeout}
)

// DefaultQRClient returns the process-wide client used by GUI/TUI helpers.
func DefaultQRClient() *QRClient { return defaultQRClient }

func (c *QRClient) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return defaultQRHTTPClient
}

func (c *QRClient) apiURL(path string) string {
	base := ""
	if c != nil {
		base = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	}
	if base == "" {
		base = "https://" + DefaultQRHost
	}
	return base + path
}

func (c *QRClient) source() string {
	if c == nil || strings.TrimSpace(c.Source) == "" {
		return DefaultQRSource
	}
	return strings.TrimSpace(c.Source)
}

// BuildConnectURL returns the mobile QQ scan payload for a bind task.
func BuildConnectURL(taskID, source string) string {
	taskID = strings.TrimSpace(taskID)
	if source == "" {
		source = DefaultQRSource
	}
	u := url.URL{
		Scheme: "https",
		Host:   DefaultQRHost,
		Path:   DefaultQRConnectPath,
	}
	q := u.Query()
	q.Set("task_id", taskID)
	q.Set("source", source)
	q.Set("_wv", "2")
	u.RawQuery = q.Encode()
	return u.String()
}

// ValidConnectQRPayload reports whether value is a QQ Bot connect.html bind URL.
func ValidConnectQRPayload(value string) bool {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil || u == nil {
		return false
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return false
	}
	if strings.ToLower(u.Hostname()) != DefaultQRHost {
		return false
	}
	if u.Path != DefaultQRConnectPath {
		return false
	}
	return strings.TrimSpace(u.Query().Get("task_id")) != ""
}

// CreateBindTask starts a QR bind session and returns task_id plus scan URL.
func (c *QRClient) CreateBindTask(ctx context.Context) (taskID, qrURL string, err error) {
	if c == nil {
		c = DefaultQRClient()
	}
	key := make([]byte, bindKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return "", "", fmt.Errorf("generate bind key: %w", err)
	}
	defer zeroBindKey(key)
	body := map[string]string{"key": base64.StdEncoding.EncodeToString(key)}
	var resp struct {
		Retcode json.RawMessage `json:"retcode"`
		Msg     string          `json:"msg"`
		Data    struct {
			TaskID json.RawMessage `json:"task_id"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, bindTaskPath, body, &resp); err != nil {
		return "", "", err
	}
	if jsonRawToInt(resp.Retcode, 0) != 0 {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "create_bind_task failed"
		}
		return "", "", fmt.Errorf("%s", msg)
	}
	taskID = jsonRawToString(resp.Data.TaskID)
	if taskID == "" {
		return "", "", errors.New("create_bind_task: missing task_id")
	}
	c.putSession(taskID, key)
	return taskID, BuildConnectURL(taskID, c.source()), nil
}

// PollBindStatus polls one bind task. COMPLETED decrypts the secret and leaves
// the AES session in place until the caller saves credentials and calls CancelBindTask.
func (c *QRClient) PollBindStatus(ctx context.Context, taskID string) (QRLoginStatus, *QRCredentials, error) {
	if c == nil {
		c = DefaultQRClient()
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", nil, ErrQRCodeTokenEmpty
	}
	key, ok := c.getSession(taskID)
	if !ok {
		return "", nil, ErrQRSessionNotFound
	}
	defer zeroBindKey(key)
	var resp struct {
		Retcode json.RawMessage `json:"retcode"`
		Msg     string          `json:"msg"`
		Data    struct {
			Status           json.RawMessage `json:"status"`
			BotAppID         json.RawMessage `json:"bot_appid"`
			BotEncryptSecret string          `json:"bot_encrypt_secret"`
			UserOpenID       string          `json:"user_openid"`
		} `json:"data"`
	}
	if err := c.postJSON(ctx, bindPollPath, map[string]string{"task_id": taskID}, &resp); err != nil {
		return QRLoginStatusWait, nil, err
	}
	if jsonRawToInt(resp.Retcode, 0) != 0 {
		msg := strings.TrimSpace(resp.Msg)
		if msg == "" {
			msg = "poll_bind_result failed"
		}
		return "", nil, fmt.Errorf("%s", msg)
	}
	switch jsonRawToBindStatus(resp.Data.Status) {
	case BindStatusCompleted:
		appID := jsonRawToString(resp.Data.BotAppID)
		secret, err := decryptSecret(resp.Data.BotEncryptSecret, key)
		if err != nil {
			return "", nil, err
		}
		if appID == "" || secret == "" {
			return "", nil, ErrQRBindIncomplete
		}
		return QRLoginStatusConfirmed, &QRCredentials{
			AppID:      appID,
			AppSecret:  secret,
			UserOpenID: strings.TrimSpace(resp.Data.UserOpenID),
		}, nil
	case BindStatusExpired:
		c.CancelBindTask(taskID)
		return QRLoginStatusExpired, nil, nil
	case BindStatusPending:
		return QRLoginStatusPending, nil, nil
	default:
		return QRLoginStatusWait, nil, nil
	}
}

// CancelBindTask drops a local AES session. Safe if the token is unknown.
func (c *QRClient) CancelBindTask(taskID string) {
	if c == nil {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if sess, ok := c.sessions[taskID]; ok {
		zeroBindKey(sess.key)
		delete(c.sessions, taskID)
	}
}

// HasBindTask reports whether an in-memory AES session still exists for taskID.
func (c *QRClient) HasBindTask(taskID string) bool {
	if c == nil {
		return false
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now())
	_, ok := c.sessions[taskID]
	return ok
}

func (c *QRClient) putSession(taskID string, key []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessions == nil {
		c.sessions = map[string]bindSession{}
	}
	c.pruneLocked(time.Now())
	if old, ok := c.sessions[taskID]; ok {
		zeroBindKey(old.key)
	}
	copied := make([]byte, len(key))
	copy(copied, key)
	c.sessions[taskID] = bindSession{key: copied, created: time.Now()}
}

func (c *QRClient) getSession(taskID string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(time.Now())
	sess, ok := c.sessions[taskID]
	if !ok {
		return nil, false
	}
	copied := make([]byte, len(sess.key))
	copy(copied, sess.key)
	return copied, true
}

func (c *QRClient) pruneLocked(now time.Time) {
	for id, sess := range c.sessions {
		if now.Sub(sess.created) > qrSessionTTL {
			zeroBindKey(sess.key)
			delete(c.sessions, id)
		}
	}
}

func zeroBindKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

func (c *QRClient) postJSON(ctx context.Context, path string, payload any, dest any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, qrHTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.apiURL(path), bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, path, strings.TrimSpace(string(body[:min(len(body), 256)])))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func jsonRawToString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return strings.TrimSpace(n.String())
	}
	return strings.TrimSpace(string(raw))
}

func jsonRawToInt(raw json.RawMessage, fallback int) int {
	s := jsonRawToString(raw)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}

func jsonRawToBindStatus(raw json.RawMessage) int {
	s := strings.ToLower(jsonRawToString(raw))
	switch s {
	case "", "none", "wait", "waiting":
		return BindStatusNone
	case "pending":
		return BindStatusPending
	case "completed", "complete", "confirmed", "success":
		return BindStatusCompleted
	case "expired", "expire":
		return BindStatusExpired
	}
	return jsonRawToInt(raw, BindStatusNone)
}

func decodeBindCiphertext(encryptedB64 string) ([]byte, error) {
	encryptedB64 = strings.TrimSpace(encryptedB64)
	if encryptedB64 == "" {
		return nil, ErrQRBindIncomplete
	}
	var lastErr error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := enc.DecodeString(encryptedB64)
		if err != nil {
			lastErr = err
			continue
		}
		if len(data) < gcmNonceSize+gcmTagSize {
			return nil, ErrQRCiphertextShort
		}
		return data, nil
	}
	if lastErr == nil {
		lastErr = ErrQRSecretDecrypt
	}
	return nil, fmt.Errorf("%w: %v", ErrQRSecretDecrypt, lastErr)
}

func decryptSecret(encryptedB64 string, key []byte) (string, error) {
	data, err := decodeBindCiphertext(encryptedB64)
	if err != nil {
		return "", err
	}
	if len(key) != bindKeyBytes {
		return "", ErrQRSecretDecrypt
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrQRSecretDecrypt, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrQRSecretDecrypt, err)
	}
	nonce := data[:gcmNonceSize]
	ciphertext := data[gcmNonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrQRSecretDecrypt, err)
	}
	return string(plain), nil
}

// CreateBindTask starts a QR bind using the process-wide client.
func CreateBindTask(ctx context.Context) (taskID, qrURL string, err error) {
	return DefaultQRClient().CreateBindTask(ctx)
}

// PollBindStatus polls a QR bind using the process-wide client.
func PollBindStatus(ctx context.Context, taskID string) (QRLoginStatus, *QRCredentials, error) {
	return DefaultQRClient().PollBindStatus(ctx, taskID)
}

// CancelBindTask cancels a QR bind using the process-wide client.
func CancelBindTask(taskID string) {
	DefaultQRClient().CancelBindTask(taskID)
}
