package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	qrcode "github.com/skip2/go-qrcode"
)

func (s *HTTPServer) qqBotQRClient() *qqbot.QRClient {
	if s.qqbotQR == nil {
		s.qqbotQR = qqbot.NewQRClient()
	}
	return s.qqbotQR
}

func (s *HTTPServer) handleStartQQBotQRLogin(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	taskID, qrURL, err := s.qqBotQRClient().CreateBindTask(ctx)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	taskID = strings.TrimSpace(taskID)
	qrURL = strings.TrimSpace(qrURL)
	if taskID == "" || qrURL == "" {
		if taskID != "" {
			s.qqBotQRClient().CancelBindTask(taskID)
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty qr bind response"})
		return
	}
	if s.qqbotQRTokens == nil {
		s.qqbotQRTokens = newWeixinQRTokenStore()
	}
	for _, old := range s.qqbotQRTokens.Put(taskID, weixinQRTokenRecord{TenantID: p.TenantID, UserID: p.UserID, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, time.Now().UTC()) {
		if old != taskID {
			s.qqBotQRClient().CancelBindTask(old)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"qrcode_url":       qrURL,
		"qrcode_image_url": qqbotQRCodeImageProxyURL(qrURL),
		"qrcode_token":     taskID,
	})
}

func qqbotQRCodeImageProxyURL(qrcodeURL string) string {
	qrcodeURL = strings.TrimSpace(qrcodeURL)
	if qrcodeURL == "" {
		return ""
	}
	return "/api/v1/im/qqbot/qr/image?value=" + url.QueryEscape(qrcodeURL)
}

func (s *HTTPServer) handleProxyQQBotQRCodeImage(w http.ResponseWriter, r *http.Request, _ agentservice.Principal) {
	value := strings.TrimSpace(r.URL.Query().Get("value"))
	if value == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode value is required"})
		return
	}
	if len(value) > 4096 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode value is too large"})
		return
	}
	if !qqbot.ValidConnectQRPayload(value) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode value is not a QQ Bot bind URL"})
		return
	}
	png, err := qrcode.Encode(value, qrcode.Medium, 360)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "generate qrcode image failed"})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *HTTPServer) handlePollQQBotQRLogin(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	var in struct {
		QRCodeToken string `json:"qrcode_token"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	in.QRCodeToken = strings.TrimSpace(in.QRCodeToken)
	if in.QRCodeToken == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode token is required"})
		return
	}
	if s.qqbotQRTokens == nil {
		s.qqbotQRTokens = newWeixinQRTokenStore()
	}
	if _, ok := s.qqbotQRTokens.Get(in.QRCodeToken, p, time.Now().UTC()); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode token is not active for this user", "status": "error"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), userWeixinQRStatusPollTimeout)
	defer cancel()
	status, creds, err := s.qqBotQRClient().PollBindStatus(ctx, in.QRCodeToken)
	if err != nil {
		if errors.Is(err, qqbot.ErrQRSessionNotFound) {
			s.qqbotQRTokens.Delete(in.QRCodeToken)
			writeJSON(w, http.StatusOK, map[string]any{"status": qqbot.QRLoginStatusExpired.String(), "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": qqbot.QRLoginStatusWait.String(), "retryable": true, "error": err.Error()})
		return
	}
	resp := map[string]any{"status": status.String()}
	if status == qqbot.QRLoginStatusConfirmed {
		if creds == nil || strings.TrimSpace(creds.AppID) == "" || strings.TrimSpace(creds.AppSecret) == "" {
			writeJSON(w, http.StatusOK, map[string]any{"status": qqbot.QRLoginStatusWait.String(), "retryable": true, "error": "qqbot login was not connected"})
			return
		}
		if err := s.saveQQBotQRLoginConfig(r.Context(), p, creds); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"status": qqbot.QRLoginStatusWait.String(), "retryable": true, "error": "save qqbot config failed"})
			return
		}
		s.qqBotQRClient().CancelBindTask(in.QRCodeToken)
		s.qqbotQRTokens.Delete(in.QRCodeToken)
		resp["app_id"] = creds.AppID
		writeJSON(w, http.StatusOK, resp)
		_ = s.svc.RecordAuditEvent(r.Context(), agentservice.AuditEvent{TenantID: p.TenantID, UserID: p.UserID, ActorType: "user", ActorTenant: p.TenantID, ActorUser: p.UserID, Action: "user.im.qqbot_qr_bound", ResourceType: "config", ResourceID: "qqbot", Metadata: map[string]string{"app_id": creds.AppID, "remote_ip": requestClientIP(r)}})
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			s.syncIMRuntimeFromRawConfig(ctx, p)
		}()
		return
	}
	if status == qqbot.QRLoginStatusExpired {
		s.qqbotQRTokens.Delete(in.QRCodeToken)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) saveQQBotQRLoginConfig(ctx context.Context, p agentservice.Principal, creds *qqbot.QRCredentials) error {
	if creds == nil {
		return errors.New("qqbot login result is nil")
	}
	cfg, _, err := s.currentUserConfigForVisibleMerge(ctx, p)
	if err != nil {
		return err
	}
	cfg.QQBotEnabled = true
	cfg.QQBotAppID = strings.TrimSpace(creds.AppID)
	cfg.QQBotAppSecret = creds.AppSecret
	if strings.TrimSpace(cfg.QQBotOwnerOpenID) == "" && strings.TrimSpace(creds.UserOpenID) != "" {
		cfg.QQBotOwnerOpenID = strings.TrimSpace(creds.UserOpenID)
	}
	if cfg.QQBotLocalMode == nil {
		local := true
		cfg.QQBotLocalMode = &local
	}
	_, err = s.svc.UpdateUserConfig(ctx, p, cfg)
	return err
}
