package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
)

func encryptQQBotSecretHTTPTest(t *testing.T, plain, keyB64 string) string {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	sealed := gcm.Seal(nil, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(append(nonce, sealed...))
}

func TestQQBotQRLoginSavesOnlyAuthenticatedUserConfig(t *testing.T) {
	ctx := context.Background()
	var capturedKey string
	bind := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/lite/create_bind_task":
			var in struct {
				Key string `json:"key"`
			}
			_ = json.Unmarshal(body, &in)
			capturedKey = in.Key
			writeJSON(w, http.StatusOK, map[string]any{"retcode": 0, "data": map[string]any{"task_id": "srv-task"}})
		case "/lite/poll_bind_result":
			enc := encryptQQBotSecretHTTPTest(t, "srv-secret", capturedKey)
			writeJSON(w, http.StatusOK, map[string]any{"retcode": 0, "data": map[string]any{
				"status":             qqbot.BindStatusCompleted,
				"bot_appid":          "102088001",
				"bot_encrypt_secret": enc,
				"user_openid":        "srv-owner",
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer bind.Close()

	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(ctx, agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	userA, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User A"})
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	userB, err := svc.CreateUser(ctx, agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User B"})
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	principalA := agentservice.Principal{TenantID: tenant.ID, UserID: userA.ID}
	principalB := agentservice.Principal{TenantID: tenant.ID, UserID: userB.ID}
	if _, err := svc.UpdateUserConfig(ctx, principalB, corelib.AppConfig{QQBotAppID: "other-app", QQBotAppSecret: "other-secret"}); err != nil {
		t.Fatalf("UpdateUserConfig B: %v", err)
	}
	tokenA, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principalA)
	if err != nil {
		t.Fatalf("Issue token A: %v", err)
	}
	tokenB, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(principalB)
	if err != nil {
		t.Fatalf("Issue token B: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	server.imRuntime = nil
	server.qqbotQR.BaseURL = bind.URL

	startReq := httptest.NewRequest(http.MethodPost, "/api/v1/im/qqbot/qr/start?user_id="+url.QueryEscape(userB.ID), bytes.NewReader([]byte("{}")))
	startReq.Header.Set("Authorization", "Bearer "+tokenA)
	startW := httptest.NewRecorder()
	server.Handler().ServeHTTP(startW, startReq)
	if startW.Code != http.StatusOK {
		t.Fatalf("start status = %d body = %s", startW.Code, startW.Body.String())
	}
	var startOut map[string]string
	if err := json.NewDecoder(startW.Body).Decode(&startOut); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if startOut["qrcode_token"] != "srv-task" || startOut["qrcode_url"] == "" {
		t.Fatalf("unexpected start output: %#v", startOut)
	}
	if !strings.HasPrefix(startOut["qrcode_image_url"], "/api/v1/im/qqbot/qr/image?value=") {
		t.Fatalf("missing image url: %#v", startOut)
	}
	imageReq := httptest.NewRequest(http.MethodGet, startOut["qrcode_image_url"], nil)
	imageReq.Header.Set("Authorization", "Bearer "+tokenA)
	imageW := httptest.NewRecorder()
	server.Handler().ServeHTTP(imageW, imageReq)
	if imageW.Code != http.StatusOK || imageW.Result().Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(imageW.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("qrcode image status = %d content-type = %s", imageW.Code, imageW.Result().Header.Get("Content-Type"))
	}

	crossPollReq := httptest.NewRequest(http.MethodPost, "/api/v1/im/qqbot/qr/poll", bytes.NewReader([]byte(`{"qrcode_token":"srv-task"}`)))
	crossPollReq.Header.Set("Authorization", "Bearer "+tokenB)
	crossPollW := httptest.NewRecorder()
	server.Handler().ServeHTTP(crossPollW, crossPollReq)
	if crossPollW.Code != http.StatusBadRequest {
		t.Fatalf("cross-user poll status = %d body = %s", crossPollW.Code, crossPollW.Body.String())
	}

	pollReq := httptest.NewRequest(http.MethodPost, "/api/v1/im/qqbot/qr/poll?user_id="+url.QueryEscape(userB.ID), bytes.NewReader([]byte(`{"qrcode_token":"srv-task"}`)))
	pollReq.Header.Set("Authorization", "Bearer "+tokenA)
	pollW := httptest.NewRecorder()
	server.Handler().ServeHTTP(pollW, pollReq)
	if pollW.Code != http.StatusOK {
		t.Fatalf("poll status = %d body = %s", pollW.Code, pollW.Body.String())
	}
	var pollOut map[string]any
	if err := json.NewDecoder(pollW.Body).Decode(&pollOut); err != nil {
		t.Fatalf("decode poll: %v", err)
	}
	if pollOut["status"] != qqbot.QRLoginStatusConfirmed.String() || pollOut["app_id"] != "102088001" {
		t.Fatalf("poll out = %#v", pollOut)
	}

	cfgA, err := svc.GetRawUserConfig(ctx, principalA)
	if err != nil {
		t.Fatalf("GetRawUserConfig A: %v", err)
	}
	if !cfgA.AppConfig.QQBotEnabled || cfgA.AppConfig.QQBotAppID != "102088001" || cfgA.AppConfig.QQBotAppSecret != "srv-secret" || cfgA.AppConfig.QQBotOwnerOpenID != "srv-owner" {
		t.Fatalf("user A config = %#v", cfgA.AppConfig)
	}
	cfgB, err := svc.GetRawUserConfig(ctx, principalB)
	if err != nil {
		t.Fatalf("GetRawUserConfig B: %v", err)
	}
	if cfgB.AppConfig.QQBotAppID != "other-app" || cfgB.AppConfig.QQBotAppSecret != "other-secret" {
		t.Fatalf("user B config mutated: %#v", cfgB.AppConfig)
	}
	events, err := svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{Action: "user.im.qqbot_qr_bound", UserID: userA.ID})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %#v", events)
	}
}

func TestQQBotQRCodeImageRejectsNonConnectURL(t *testing.T) {
	svc, err := agentservice.NewService(agentservice.Config{DataRoot: t.TempDir(), TokenSecret: "test-token-secret-0123456789012345"}, agentservice.NewMemoryStore(), agentservice.EchoExecutor{})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	tenant, err := svc.CreateTenant(context.Background(), agentservice.CreateTenantInput{Name: "Tenant"})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := svc.CreateUser(context.Background(), agentservice.CreateUserInput{TenantID: tenant.ID, Name: "User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	token, _, err := agentservice.NewTokenManager("test-token-secret-0123456789012345", time.Hour).Issue(agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	server := NewHTTPServer(svc, "admin-secret", nil)
	defer server.Close()
	server.imRuntime = nil

	req := httptest.NewRequest(http.MethodGet, "/api/v1/im/qqbot/qr/image?value="+url.QueryEscape("https://evil.example/qqbot/openclaw/connect.html?task_id=x"), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", w.Code, w.Body.String())
	}
}
