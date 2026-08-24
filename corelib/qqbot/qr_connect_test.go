package qqbot

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func encryptSecretForTest(t *testing.T, plain string, key []byte) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

func TestDecryptSecretRoundTrip(t *testing.T) {
	key := make([]byte, bindKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc := encryptSecretForTest(t, "super-secret", key)
	got, err := decryptSecret(enc, key)
	if err != nil {
		t.Fatalf("decryptSecret: %v", err)
	}
	if got != "super-secret" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptSecretRejectsShortCiphertext(t *testing.T) {
	key := make([]byte, bindKeyBytes)
	if _, err := decryptSecret(base64.StdEncoding.EncodeToString([]byte("short")), key); err != ErrQRCiphertextShort {
		t.Fatalf("err = %v, want ErrQRCiphertextShort", err)
	}
}

func TestDecryptSecretAcceptsUnpaddedAndURLBase64(t *testing.T) {
	key := make([]byte, bindKeyBytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	padded := encryptSecretForTest(t, "url-secret", key)
	raw := strings.TrimRight(padded, "=")
	got, err := decryptSecret(raw, key)
	if err != nil {
		t.Fatalf("unpadded: %v", err)
	}
	if got != "url-secret" {
		t.Fatalf("unpadded got %q", got)
	}
	data, err := base64.StdEncoding.DecodeString(padded)
	if err != nil {
		t.Fatal(err)
	}
	urlEnc := base64.URLEncoding.EncodeToString(data)
	got, err = decryptSecret(urlEnc, key)
	if err != nil {
		t.Fatalf("url encoding: %v", err)
	}
	if got != "url-secret" {
		t.Fatalf("url encoding got %q", got)
	}
}

func TestPollBindStatusAcceptsStringStatus(t *testing.T) {
	var capturedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var in struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(body, &in)
			capturedKey = in.Key
			writeJSON(w, map[string]any{"retcode": "0", "data": map[string]any{"task_id": "str-status"}})
		case "/lite/poll_bind_result":
			key, err := base64.StdEncoding.DecodeString(capturedKey)
			if err != nil {
				t.Fatalf("decode key: %v", err)
			}
			enc := encryptSecretForTest(t, "str-secret", key)
			writeJSON(w, map[string]any{"retcode": "0", "data": map[string]any{
				"status":             "completed",
				"bot_appid":          "102088009",
				"bot_encrypt_secret": enc,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewQRClient()
	client.BaseURL = srv.URL
	taskID, _, err := client.CreateBindTask(context.Background())
	if err != nil {
		t.Fatalf("CreateBindTask: %v", err)
	}
	status, creds, err := client.PollBindStatus(context.Background(), taskID)
	if err != nil {
		t.Fatalf("PollBindStatus: %v", err)
	}
	if status != QRLoginStatusConfirmed || creds == nil || creds.AppID != "102088009" || creds.AppSecret != "str-secret" {
		t.Fatalf("status=%s creds=%#v", status, creds)
	}
}

func TestCreateBindTaskAndPollStatuses(t *testing.T) {
	var capturedKey string
	var pollN int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var in struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			if in.Key == "" {
				t.Fatal("missing key")
			}
			capturedKey = in.Key
			writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"task_id": "task-1"}})
		case "/lite/poll_bind_result":
			var in struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				t.Fatalf("decode poll: %v", err)
			}
			if in.TaskID != "task-1" {
				t.Fatalf("task_id = %q", in.TaskID)
			}
			pollN++
			switch pollN {
			case 1:
				writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"status": BindStatusNone}})
			case 2:
				writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"status": BindStatusPending}})
			default:
				key, err := base64.StdEncoding.DecodeString(capturedKey)
				if err != nil {
					t.Fatalf("decode key: %v", err)
				}
				enc := encryptSecretForTest(t, "app-secret-xyz", key)
				writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{
					"status":             BindStatusCompleted,
					"bot_appid":          102012345,
					"bot_encrypt_secret": enc,
					"user_openid":        "owner-openid",
				}})
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := NewQRClient()
	client.BaseURL = srv.URL
	ctx := context.Background()
	taskID, qrURL, err := client.CreateBindTask(ctx)
	if err != nil {
		t.Fatalf("CreateBindTask: %v", err)
	}
	if taskID != "task-1" {
		t.Fatalf("taskID = %q", taskID)
	}
	if !strings.Contains(qrURL, "task_id=task-1") || !strings.Contains(qrURL, "source=maclaw") || !strings.HasPrefix(qrURL, "https://q.qq.com/qqbot/openclaw/connect.html") {
		t.Fatalf("qrURL = %q", qrURL)
	}

	status, creds, err := client.PollBindStatus(ctx, taskID)
	if err != nil || status != QRLoginStatusWait || creds != nil {
		t.Fatalf("poll wait: status=%s creds=%v err=%v", status, creds, err)
	}
	status, creds, err = client.PollBindStatus(ctx, taskID)
	if err != nil || status != QRLoginStatusPending || creds != nil {
		t.Fatalf("poll pending: status=%s creds=%v err=%v", status, creds, err)
	}
	status, creds, err = client.PollBindStatus(ctx, taskID)
	if err != nil {
		t.Fatalf("poll confirmed: %v", err)
	}
	if status != QRLoginStatusConfirmed || creds == nil || creds.AppID != "102012345" || creds.AppSecret != "app-secret-xyz" || creds.UserOpenID != "owner-openid" {
		t.Fatalf("confirmed = status=%s creds=%#v", status, creds)
	}
	status, creds, err = client.PollBindStatus(ctx, taskID)
	if err != nil || status != QRLoginStatusConfirmed || creds == nil || creds.AppSecret != "app-secret-xyz" {
		t.Fatalf("confirmed retry = status=%s creds=%#v err=%v", status, creds, err)
	}
	client.CancelBindTask(taskID)
	if _, _, err := client.PollBindStatus(ctx, taskID); err != ErrQRSessionNotFound {
		t.Fatalf("after cancel poll err = %v, want ErrQRSessionNotFound", err)
	}
}

func TestCreateBindTaskHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()
	client := NewQRClient()
	client.BaseURL = srv.URL
	if _, _, err := client.CreateBindTask(context.Background()); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateBindTaskRetcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"retcode": 1, "msg": "rate limited"})
	}))
	defer srv.Close()
	client := NewQRClient()
	client.BaseURL = srv.URL
	if _, _, err := client.CreateBindTask(context.Background()); err == nil || err.Error() != "rate limited" {
		t.Fatalf("err = %v", err)
	}
}

func TestPollBindStatusExpiredAndCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lite/create_bind_task":
			writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"task_id": "exp-1"}})
		case "/lite/poll_bind_result":
			writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"status": BindStatusExpired}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewQRClient()
	client.BaseURL = srv.URL
	ctx := context.Background()
	taskID, _, err := client.CreateBindTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	status, creds, err := client.PollBindStatus(ctx, taskID)
	if err != nil || status != QRLoginStatusExpired || creds != nil {
		t.Fatalf("expired: status=%s creds=%v err=%v", status, creds, err)
	}
	if _, _, err := client.PollBindStatus(ctx, taskID); err != ErrQRSessionNotFound {
		t.Fatalf("after expire err = %v", err)
	}

	taskID, _, err = client.CreateBindTask(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client.CancelBindTask(taskID)
	if _, _, err := client.PollBindStatus(ctx, taskID); err != ErrQRSessionNotFound {
		t.Fatalf("after cancel err = %v", err)
	}
}

func TestPollBindStatusEmptyToken(t *testing.T) {
	if _, _, err := NewQRClient().PollBindStatus(context.Background(), "  "); err != ErrQRCodeTokenEmpty {
		t.Fatalf("err = %v", err)
	}
}

func TestQRSessionPrune(t *testing.T) {
	client := NewQRClient()
	client.putSession("old", make([]byte, bindKeyBytes))
	client.mu.Lock()
	sess := client.sessions["old"]
	sess.created = time.Now().Add(-qrSessionTTL - time.Second)
	client.sessions["old"] = sess
	client.mu.Unlock()
	if _, ok := client.getSession("old"); ok {
		t.Fatal("expected pruned session")
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestCreateBindTaskAcceptsNumericTaskID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lite/create_bind_task" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"retcode": 0, "data": map[string]any{"task_id": 102088001}})
	}))
	defer srv.Close()
	client := NewQRClient()
	client.BaseURL = srv.URL
	taskID, qrURL, err := client.CreateBindTask(context.Background())
	if err != nil {
		t.Fatalf("CreateBindTask: %v", err)
	}
	if taskID != "102088001" {
		t.Fatalf("taskID = %q", taskID)
	}
	if !ValidConnectQRPayload(qrURL) || !strings.Contains(qrURL, "task_id=102088001") {
		t.Fatalf("qrURL = %q", qrURL)
	}
	if !client.HasBindTask(taskID) {
		t.Fatal("numeric task_id should store a bind session")
	}
}

func TestQRClientReusesDefaultHTTPClient(t *testing.T) {
	a := &QRClient{}
	b := &QRClient{}
	if a.httpClient() != b.httpClient() {
		t.Fatal("nil HTTP should reuse the shared default client")
	}
	custom := &http.Client{Timeout: time.Second}
	c := &QRClient{HTTP: custom}
	if c.httpClient() != custom {
		t.Fatal("custom HTTP client should be used as-is")
	}
}

func TestValidConnectQRPayload(t *testing.T) {
	ok := BuildConnectURL("task-1", "maclaw")
	if !ValidConnectQRPayload(ok) {
		t.Fatalf("expected valid payload %q", ok)
	}
	for _, bad := range []string{
		"",
		"https://evil.example/qqbot/openclaw/connect.html?task_id=x",
		"http://q.qq.com/qqbot/openclaw/connect.html?task_id=x",
		"https://q.qq.com/qqbot/openclaw/login.html?task_id=x",
		"https://q.qq.com/qqbot/openclaw/connect.html",
		"javascript:alert(1)",
	} {
		if ValidConnectQRPayload(bad) {
			t.Fatalf("payload should be rejected: %q", bad)
		}
	}
}

func TestHasBindTaskTracksCancel(t *testing.T) {
	client := NewQRClient()
	if client.HasBindTask("missing") {
		t.Fatal("missing task should be inactive")
	}
	client.putSession("task-keep", make([]byte, bindKeyBytes))
	if !client.HasBindTask("task-keep") {
		t.Fatal("stored task should be active")
	}
	client.CancelBindTask("task-keep")
	if client.HasBindTask("task-keep") {
		t.Fatal("cancelled task should be inactive")
	}
}
