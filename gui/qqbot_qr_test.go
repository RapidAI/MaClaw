package main

import (
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

	"github.com/RapidAI/CodeClaw/corelib/qqbot"
)

func encryptQQBotSecretForTest(t *testing.T, plain string, keyB64 string) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

func TestQQBotQRLoginSavesConfigAndOwnerOpenID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}

	var capturedKey string
	pollN := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var in struct {
				Key string `json:"key"`
			}
			if err := json.Unmarshal(body, &in); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			capturedKey = in.Key
			_ = json.NewEncoder(w).Encode(map[string]any{"retcode": 0, "data": map[string]any{"task_id": "gui-task"}})
		case "/lite/poll_bind_result":
			pollN++
			if pollN == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"retcode": 0, "data": map[string]any{"status": qqbot.BindStatusPending}})
				return
			}
			enc := encryptQQBotSecretForTest(t, "gui-secret", capturedKey)
			_ = json.NewEncoder(w).Encode(map[string]any{"retcode": 0, "data": map[string]any{
				"status":             qqbot.BindStatusCompleted,
				"bot_appid":          "102099001",
				"bot_encrypt_secret": enc,
				"user_openid":        "owner-from-scan",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mgr := app.ensureQQBotQRManager()
	mgr.qr.BaseURL = srv.URL

	start := app.StartQQBotQRLogin()
	if start["error"] != "" {
		t.Fatalf("StartQQBotQRLogin error = %q", start["error"])
	}
	if start["qrcode_token"] != "gui-task" || !strings.Contains(start["qrcode_url"], "task_id=gui-task") {
		t.Fatalf("start = %#v", start)
	}

	pending := app.PollQQBotQRStatus(start["qrcode_token"])
	if pending["status"] != qqbot.QRLoginStatusPending.String() {
		t.Fatalf("pending = %#v", pending)
	}
	confirmed := app.PollQQBotQRStatus(start["qrcode_token"])
	if confirmed["error"] != "" || confirmed["status"] != qqbot.QRLoginStatusConfirmed.String() || confirmed["app_id"] != "102099001" {
		t.Fatalf("confirmed = %#v", confirmed)
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.QQBotEnabled || cfg.QQBotAppID != "102099001" || cfg.QQBotAppSecret != "gui-secret" || cfg.QQBotOwnerOpenID != "owner-from-scan" {
		t.Fatalf("config = %#v", cfg)
	}
	if !cfg.IsQQBotLocalMode() {
		t.Fatal("expected local mode default")
	}
	again := app.PollQQBotQRStatus(start["qrcode_token"])
	if again["status"] != qqbot.QRLoginStatusExpired.String() {
		t.Fatalf("session should drop after save, got %#v", again)
	}
}

func TestCancelQQBotQRLoginDropsSession(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lite/create_bind_task" {
			_ = json.NewEncoder(w).Encode(map[string]any{"retcode": 0, "data": map[string]any{"task_id": "cancel-task"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	mgr := app.ensureQQBotQRManager()
	mgr.qr.BaseURL = srv.URL
	start := app.StartQQBotQRLogin()
	app.CancelQQBotQRLogin(start["qrcode_token"])
	got := app.PollQQBotQRStatus(start["qrcode_token"])
	if got["status"] != qqbot.QRLoginStatusExpired.String() {
		t.Fatalf("after cancel = %#v", got)
	}
}
