package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

// 闁冲厜鍋撻柍鍏夊亾 helpers 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func chatPrincipal(r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, error) {
	return authenticateViewerRequest(r, identity)
}

func chatAuth(r *http.Request, identity *auth.IdentityService) (string, error) {
	vp, err := chatPrincipal(r, identity)
	if err != nil {
		return "", err
	}
	return vp.UserID, nil
}

func chatUserInTenant(ctx context.Context, identity *auth.IdentityService, tenantID, userID string) (bool, error) {
	user, err := identity.UsersRepo().GetByID(ctx, userID)
	if err != nil || user == nil {
		return false, err
	}
	return store.NormalizeTenantID(user.TenantID) == store.NormalizeTenantID(tenantID), nil
}

func chatChannelInTenant(ctx context.Context, identity *auth.IdentityService, chSvc *chat.ChannelService, tenantID, channelID string) (bool, error) {
	channel, err := chSvc.GetChannel(channelID)
	if err != nil || channel == nil {
		return false, err
	}
	return chatUserInTenant(ctx, identity, tenantID, channel.CreatedBy)
}

func chatMemberInTenant(ctx context.Context, identity *auth.IdentityService, chSvc *chat.ChannelService, tenantID, channelID, userID string) (bool, error) {
	ok, err := chSvc.IsMember(channelID, userID)
	if err != nil || !ok {
		return false, err
	}
	return chatChannelInTenant(ctx, identity, chSvc, tenantID, channelID)
}

// 闁冲厜鍋撻柍鍏夊亾 Channels 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func chatVoiceCallInTenant(ctx context.Context, identity *auth.IdentityService, chSvc *chat.ChannelService, tenantID string, voiceSvc *chat.VoiceSignaling, callID string) (*chat.VoiceCall, bool, error) {
	call, err := voiceSvc.GetCall(callID)
	if err != nil || call == nil {
		return nil, false, err
	}
	ok, err := chatUserInTenant(ctx, identity, tenantID, call.CallerID)
	if err != nil || !ok {
		return call, false, err
	}
	if strings.TrimSpace(call.ChannelID) != "" {
		ok, err = chatChannelInTenant(ctx, identity, chSvc, tenantID, call.ChannelID)
		if err != nil || !ok {
			return call, false, err
		}
	}
	return call, true, nil
}
func ChatCreateChannelHandler(identity *auth.IdentityService, chSvc *chat.ChannelService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			Type      string   `json:"type"`
			Name      string   `json:"name"`
			MemberIDs []string `json:"member_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		for _, memberID := range req.MemberIDs {
			ok, err := chatUserInTenant(r.Context(), identity, principal.TenantID, memberID)
			if err != nil || !ok {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "member is outside tenant")
				return
			}
		}
		chType := chat.ChannelGroup
		if req.Type == "direct" {
			chType = chat.ChannelDirect
		}
		ch, err := chSvc.CreateChannelForTenant(principal.TenantID, uid, chType, req.Name, req.MemberIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, ch)
	}
}

func ChatListChannelsHandler(identity *auth.IdentityService, chSvc *chat.ChannelService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		channels, err := chSvc.GetTenantUserChannels(principal.TenantID, uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		filtered := channels[:0]
		for _, channel := range channels {
			ok, err := chatUserInTenant(r.Context(), identity, principal.TenantID, channel.CreatedBy)
			if err == nil && ok {
				filtered = append(filtered, channel)
			}
		}
		channels = filtered
		if channels == nil {
			channels = []chat.Channel{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"channels": channels})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Messages 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func ChatSendMessageHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, msgSvc *chat.MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		channelID := r.PathValue("id")
		if channelID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing channel id")
			return
		}
		ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, channelID, uid)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
			return
		}
		var req struct {
			Content     string            `json:"content"`
			MsgType     int               `json:"msg_type"`
			ClientMsgID string            `json:"client_msg_id"`
			Attachments []chat.Attachment `json:"attachments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		msg, err := msgSvc.SendMessage(uid, channelID, req.Content, req.ClientMsgID, chat.MessageType(req.MsgType), req.Attachments)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SEND_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, msg)
	}
}

func ChatGetMessagesHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, msgSvc *chat.MessageService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		channelID := r.PathValue("id")
		if channelID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing channel id")
			return
		}
		ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, channelID, uid)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 100 {
			limit = 50
		}
		var msgs []chat.Message
		if afterStr := r.URL.Query().Get("after_seq"); afterStr != "" {
			afterSeq, _ := strconv.ParseInt(afterStr, 10, 64)
			msgs, err = msgSvc.GetMessagesAfter(channelID, afterSeq, limit)
		} else if beforeStr := r.URL.Query().Get("before_seq"); beforeStr != "" {
			beforeSeq, _ := strconv.ParseInt(beforeStr, 10, 64)
			msgs, err = msgSvc.GetMessagesBefore(channelID, beforeSeq, limit)
		} else {
			// Default: latest messages (before_seq = max).
			msgs, err = msgSvc.GetMessagesBefore(channelID, 1<<62, limit)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
			return
		}
		if msgs == nil {
			msgs = []chat.Message{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Read Receipts 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func ChatReadReceiptsHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, rrSvc *chat.ReadReceiptService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			Receipts []chat.ReadReceipt `json:"receipts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		for _, receipt := range req.Receipts {
			ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, receipt.ChannelID, uid)
			if err != nil || !ok {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
				return
			}
		}
		if err := rrSvc.BatchUpdate(uid, req.Receipts); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Files 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func ChatFileUploadHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, fileSvc *chat.FileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		if err := r.ParseMultipartForm(32 << 20); err != nil { // 32 MB max
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid multipart form")
			return
		}
		channelID := r.FormValue("channel_id")
		if channelID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing channel_id")
			return
		}
		ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, channelID, uid)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing file")
			return
		}
		defer file.Close()

		rec, err := fileSvc.Upload(uid, channelID, header.Filename, header.Header.Get("Content-Type"), header.Size, file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "UPLOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

func ChatFileDownloadHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, fileSvc *chat.FileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		fileID := r.PathValue("id")
		if fileID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing file id")
			return
		}
		rec, err := fileSvc.GetFileRecord(fileID)
		if err != nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
			return
		}
		ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, rec.ChannelID, principal.UserID)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
			return
		}
		ext := filepath.Ext(rec.Filename)
		diskPath := fileSvc.FilePath(fileID, ext)
		w.Header().Set("Content-Type", rec.MimeType)
		// Sanitize filename for Content-Disposition header.
		safeName := strings.ReplaceAll(rec.Filename, "\"", "_")
		w.Header().Set("Content-Disposition", "inline; filename=\""+safeName+"\"")
		http.ServeFile(w, r, diskPath)
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Presence 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func ChatPresenceHandler(identity *auth.IdentityService, presenceSvc *chat.PresenceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		targetUID := r.PathValue("id")
		if targetUID == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "missing user id")
			return
		}
		ok, err := chatUserInTenant(r.Context(), identity, principal.TenantID, targetUID)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "user is outside tenant")
			return
		}
		online := presenceSvc.IsOnline(targetUID)
		writeJSON(w, http.StatusOK, map[string]any{
			"user_id": targetUID,
			"online":  online,
		})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Voice Signaling 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func ChatVoiceCallHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, voiceSvc *chat.VoiceSignaling) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			CalleeID  string `json:"callee_id"`
			ChannelID string `json:"channel_id"`
			CallType  int    `json:"call_type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		ok, err := chatUserInTenant(r.Context(), identity, principal.TenantID, req.CalleeID)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "callee is outside tenant")
			return
		}
		if req.ChannelID != "" {
			ok, err = chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, req.ChannelID, uid)
			if err != nil || !ok {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
				return
			}
		}
		call, err := voiceSvc.InitiateCall(uid, req.CalleeID, req.ChannelID, chat.CallType(req.CallType))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "CALL_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, call)
	}
}

func ChatVoiceAnswerHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, voiceSvc *chat.VoiceSignaling) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			CallID string `json:"call_id"`
			Accept bool   `json:"accept"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		if _, ok, err := chatVoiceCallInTenant(r.Context(), identity, chSvc, principal.TenantID, voiceSvc, req.CallID); err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "call is outside tenant")
			return
		}
		if err := voiceSvc.AnswerCall(req.CallID, uid, req.Accept); err != nil {
			writeError(w, http.StatusInternalServerError, "ANSWER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ChatVoiceICEHandler(identity *auth.IdentityService, voiceSvc *chat.VoiceSignaling) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			CallID    string         `json:"call_id"`
			ToUserID  string         `json:"to_user_id"`
			Candidate map[string]any `json:"candidate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		ok, err := chatUserInTenant(r.Context(), identity, principal.TenantID, req.ToUserID)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "target user is outside tenant")
			return
		}
		voiceSvc.ForwardICE(req.CallID, uid, req.ToUserID, req.Candidate)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func ChatVoiceHangupHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, voiceSvc *chat.VoiceSignaling) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			CallID string `json:"call_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		if _, ok, err := chatVoiceCallInTenant(r.Context(), identity, chSvc, principal.TenantID, voiceSvc, req.CallID); err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "call is outside tenant")
			return
		}
		if err := voiceSvc.Hangup(req.CallID, uid); err != nil {
			writeError(w, http.StatusInternalServerError, "HANGUP_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Push Token Registration 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋?
func ChatPushRegisterHandler(identity *auth.IdentityService, store *chat.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			Platform string `json:"platform"`
			Token    string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		if req.Platform == "" || req.Token == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "platform and token required")
			return
		}
		if req.Platform != "apns" && req.Platform != "fcm" && req.Platform != "hms" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "platform must be apns, fcm, or hms")
			return
		}
		if err := store.UpsertPushToken(uid, req.Platform, req.Token); err != nil {
			writeError(w, http.StatusInternalServerError, "REGISTER_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// 闁冲厜鍋撻柍鍏夊亾 Typing Indicator 闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾闁冲厜鍋撻柍鍏夊亾

func ChatTypingHandler(identity *auth.IdentityService, chSvc *chat.ChannelService, notifier *chat.Notifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, err := chatPrincipal(r, identity)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid token")
			return
		}
		uid := principal.UserID
		var req struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON")
			return
		}
		ok, err := chatMemberInTenant(r.Context(), identity, chSvc, principal.TenantID, req.ChannelID, uid)
		if err != nil || !ok {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "not a tenant channel member")
			return
		}
		notifier.NotifyTyping(req.ChannelID, uid)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
