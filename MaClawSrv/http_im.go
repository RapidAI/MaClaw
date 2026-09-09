package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	qrcode "github.com/skip2/go-qrcode"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *HTTPServer) handleGetWeixinRuntimeStatus(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg, err := s.rawWeixinAppConfig(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	s.syncWeixinRuntimeFromRawConfig(r.Context(), p)
	status := srvWeixinRuntimeStatus{Status: srvWeixinStatusDisabled}
	if s.weixinRuntime != nil {
		status = s.weixinRuntime.Status(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":    cfg.WeixinEnabled,
		"bound":      strings.TrimSpace(cfg.WeixinToken) != "",
		"account_id": strings.TrimSpace(cfg.WeixinAccountID),
		"runtime":    status.Status,
		"last_error": status.LastError,
		"updated_at": status.UpdatedAt,
	})
}

func (s *HTTPServer) handleRestartWeixinRuntime(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	cfg, err := s.rawWeixinAppConfig(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !cfg.WeixinEnabled || strings.TrimSpace(cfg.WeixinToken) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "weixin is not bound or enabled"})
		return
	}
	if s.weixinRuntime != nil {
		s.weixinRuntime.RestartPrincipal(r.Context(), p, cfg)
	}
	status := srvWeixinRuntimeStatus{Status: srvWeixinStatusDisabled}
	if s.weixinRuntime != nil {
		status = s.weixinRuntime.Status(p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "restarted", "runtime": status.Status, "last_error": status.LastError, "updated_at": status.UpdatedAt})
}

func (s *HTTPServer) handleGetIMRuntimeStatuses(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	items := map[string]srvIMRuntimeStatus{}
	if s.imRuntime != nil {
		items = s.imRuntime.Statuses(p)
	}
	for _, platform := range []string{"qq", "telegram", "lansenger"} {
		if _, ok := items[platform]; !ok {
			items[platform] = srvIMRuntimeStatus{Status: srvWeixinStatusDisabled}
		}
	}
	items["thirdparty"] = s.thirdPartyRuntimeStatus(r.Context(), p)
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleStartWeixinQRLogin(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	baseURL, err := s.weixinQRBaseURL(r.Context(), p)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	qrcodeURL, qrcodeToken, err := weixin.StartQRLogin(ctx, baseURL, weixin.DefaultBotType)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if s.weixinQRTokens == nil {
		s.weixinQRTokens = newWeixinQRTokenStore()
	}
	s.weixinQRTokens.Put(qrcodeToken, weixinQRTokenRecord{TenantID: p.TenantID, UserID: p.UserID, BaseURL: baseURL, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}, time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]string{"qrcode_url": qrcodeURL, "qrcode_image_url": weixinQRCodeImageProxyURL(qrcodeURL), "qrcode_token": qrcodeToken})
}

func (s *HTTPServer) handleProxyWeixinQRCodeImage(w http.ResponseWriter, r *http.Request, _ agentservice.Principal) {
	if value := strings.TrimSpace(r.URL.Query().Get("value")); value != "" {
		if len(value) > 4096 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode value is too large"})
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
		return
	}
	u, err := validateWeixinQRCodeImageURL(r.URL.Query().Get("url"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid qrcode url"})
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "fetch qrcode image failed"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("qrcode image returned %d", resp.StatusCode)})
		return
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "read qrcode image failed"})
		return
	}
	if len(body) > 2*1024*1024 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "qrcode image is too large"})
		return
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		contentType = http.DetectContentType(body)
		if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "qrcode response is not an image"})
			return
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *HTTPServer) handlePollWeixinQRLogin(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
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
	if s.weixinQRTokens == nil {
		s.weixinQRTokens = newWeixinQRTokenStore()
	}
	rec, ok := s.weixinQRTokens.Get(in.QRCodeToken, p, time.Now().UTC())
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "qrcode token is not active for this user", "status": "error"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), userWeixinQRStatusPollTimeout)
	defer cancel()
	result, status, err := weixin.PollQRStatus(ctx, rec.BaseURL, in.QRCodeToken)
	if err != nil {
		writeJSON(w, http.StatusOK, weixinQRPollErrorResponse(err))
		return
	}
	status = normalizeWeixinQRPollStatus(status, result)
	resp := map[string]any{"status": status.String()}
	if msg := weixinQRPollMessage(status, result); msg != "" {
		resp["message"] = msg
	}
	if status == weixin.QRLoginStatusConfirmed {
		if result == nil || !result.Connected {
			message := "weixin login was not connected"
			if result != nil && strings.TrimSpace(result.Message) != "" {
				message = strings.TrimSpace(result.Message)
			}
			resp["error"] = message
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if err := s.saveWeixinQRLoginConfig(r.Context(), p, result); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		s.syncWeixinRuntimeFromRawConfig(r.Context(), p)
		s.weixinQRTokens.Delete(in.QRCodeToken)
		resp["account_id"] = result.AccountID
		_ = s.svc.RecordAuditEvent(r.Context(), agentservice.AuditEvent{TenantID: p.TenantID, UserID: p.UserID, ActorType: "user", ActorTenant: p.TenantID, ActorUser: p.UserID, Action: "user.im.weixin_qr_bound", ResourceType: "config", ResourceID: "weixin", Metadata: map[string]string{"account_id": result.AccountID, "remote_ip": requestClientIP(r)}})
	}
	if status == weixin.QRLoginStatusExpired {
		s.weixinQRTokens.Delete(in.QRCodeToken)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *HTTPServer) handleListIMAuditMessages(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	in, ok := parseIMAuditQuery(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ListIMAuditMessages(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	page, err := parsePageQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	items, meta := paginateIMAuditMessages(out, page)
	writeJSON(w, http.StatusOK, listResponse(items, meta))
}

func (s *HTTPServer) handleListIMAuditContacts(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	out, err := s.svc.ListIMAuditContacts(r.Context(), p, strings.TrimSpace(r.URL.Query().Get("platform")))
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *HTTPServer) handleGetIMAuditStats(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	in, ok := parseIMAuditQuery(w, r)
	if !ok {
		return
	}
	out, err := s.svc.GetIMAuditStats(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *HTTPServer) handleExportIMAuditCSV(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	in, ok := parseIMAuditQuery(w, r)
	if !ok {
		return
	}
	out, err := s.svc.ListIMAuditMessages(r.Context(), p, in)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="im-audit.csv"`)
	w.WriteHeader(http.StatusOK)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"created_at", "platform", "contact_id", "role", "content", "instance_id", "instance_name", "session_id", "session_title", "message_id"})
	for _, item := range out {
		_ = cw.Write([]string{
			csvSafeCell(item.CreatedAt.Format(time.RFC3339Nano)),
			csvSafeCell(item.Platform),
			csvSafeCell(item.ContactID),
			csvSafeCell(string(item.Message.Role)),
			csvSafeCell(item.Message.Content),
			csvSafeCell(item.InstanceID),
			csvSafeCell(item.InstanceName),
			csvSafeCell(item.SessionID),
			csvSafeCell(item.SessionTitle),
			csvSafeCell(item.Message.Metadata["im_message_id"]),
		})
	}
	cw.Flush()
}

func (s *HTTPServer) handleDeleteIMAuditMessages(w http.ResponseWriter, r *http.Request, p agentservice.Principal) {
	if err := requireAdminConfirmation(r, "IM history cleanup"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	in, ok := parseIMAuditQuery(w, r)
	if !ok {
		return
	}
	before, err := parseRequiredTimeQuery(r, "before")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.svc.DeleteIMAuditMessagesBefore(r.Context(), p, in, before)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, out)
}
