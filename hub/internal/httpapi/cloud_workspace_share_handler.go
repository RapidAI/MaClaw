package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/cloudworkspace"
)

func cloudWorkspaceShareJSON(view cloudworkspace.WorkspaceShareView) map[string]any {
	recipients := make([]map[string]any, 0, len(view.Recipients))
	for _, rec := range view.Recipients {
		recipients = append(recipients, map[string]any{
			"user_id":      rec.UserID,
			"email":        rec.Email,
			"display_name": rec.DisplayName,
			"home_hub":     rec.HomeHub,
			"permission":   rec.Permission,
			"accepted_at":  rec.AcceptedAt,
		})
	}
	return map[string]any{
		"workspace_id":       view.WorkspaceID,
		"share_id":           view.ShareID,
		"share_url":          view.ShareURL,
		"token":              view.Token,
		"default_permission": view.DefaultPermission,
		"status":             view.Status,
		"password_set":       view.PasswordSet,
		"expires_at":         view.ExpiresAt,
		"recipients":         recipients,
	}
}

type cloudWorkspaceShareWriteRequest struct {
	Permission    string `json:"permission"`
	Password      string `json:"password"`
	ClearPassword bool   `json:"clear_password"`
	TTL           string `json:"ttl"`
	ExpiresAt     string `json:"expires_at"`
}

func decodeShareWriteRequest(r *http.Request) (cloudWorkspaceShareWriteRequest, error) {
	var req cloudWorkspaceShareWriteRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return cloudWorkspaceShareWriteRequest{}, err
	}
	return req, nil
}

func decodeSharePermission(r *http.Request) (string, error) {
	req, err := decodeShareWriteRequest(r)
	if err != nil {
		return "", err
	}
	return req.Permission, nil
}

func rejectSharePrincipalManage(w http.ResponseWriter, principal *auth.MachinePrincipal) bool {
	if principal != nil && cloudworkspace.SharePrincipal(*principal) {
		writeError(w, http.StatusForbidden, "CLOUD_WORKSPACE_FORBIDDEN", "share access cannot manage sharing")
		return true
	}
	return false
}

// CloudWorkspaceCreateShareHandler PUT/POST /api/v1/cloud-workspaces/{id}/share
func CloudWorkspaceCreateShareHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if rejectSharePrincipalManage(w, principal) {
			return
		}
		req, err := decodeShareWriteRequest(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace share request")
			return
		}
		view, err := svc.CreateOrGetShare(r.Context(), *principal, strings.TrimSpace(r.PathValue("id")), req.Permission, cloudworkspace.ShareWriteOptions{
			Password:      req.Password,
			ClearPassword: req.ClearPassword,
			TTL:           req.TTL,
			ExpiresAt:     req.ExpiresAt,
		})
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cloudWorkspaceShareJSON(view))
	}
}

// CloudWorkspaceGetShareHandler GET /api/v1/cloud-workspaces/{id}/share
func CloudWorkspaceGetShareHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if rejectSharePrincipalManage(w, principal) {
			return
		}
		view, err := svc.GetShare(r.Context(), *principal, strings.TrimSpace(r.PathValue("id")))
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, cloudWorkspaceShareJSON(view))
	}
}

// CloudWorkspaceRevokeShareHandler DELETE /api/v1/cloud-workspaces/{id}/share
func CloudWorkspaceRevokeShareHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if rejectSharePrincipalManage(w, principal) {
			return
		}
		if err := svc.RevokeShare(r.Context(), *principal, strings.TrimSpace(r.PathValue("id"))); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revoked": true})
	}
}

// CloudWorkspaceUpdateShareRecipientHandler PATCH /api/v1/cloud-workspaces/{id}/share/recipients/{user_id}
func CloudWorkspaceUpdateShareRecipientHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if rejectSharePrincipalManage(w, principal) {
			return
		}
		permission, err := decodeSharePermission(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace share request")
			return
		}
		if err := svc.UpdateShareRecipient(r.Context(), *principal, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("user_id")), permission); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"updated": true})
	}
}

// CloudWorkspaceRemoveShareRecipientHandler DELETE /api/v1/cloud-workspaces/{id}/share/recipients/{user_id}
func CloudWorkspaceRemoveShareRecipientHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authenticateCloudWorkspaceMachine(w, r, svc, identity)
		if !ok || !requireCloudWorkspaceGrant(w, r, svc, principal) {
			return
		}
		if rejectSharePrincipalManage(w, principal) {
			return
		}
		if err := svc.RemoveShareRecipient(r.Context(), *principal, strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.PathValue("user_id"))); err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"removed": true})
	}
}

// CloudWorkspaceAcceptShareHandler POST /api/v1/cloud-workspace-shares/{token}/accept
func writeCloudWorkspaceShareAccepted(w http.ResponseWriter, svc *cloudworkspace.Service, r *http.Request, ws *cloudworkspace.Workspace, permission, accessToken string) {
	ownerEmail := ""
	if svc != nil && svc.Users != nil && ws != nil {
		if user, userErr := svc.Users.GetByID(r.Context(), ws.UserID); userErr == nil && user != nil {
			ownerEmail = strings.TrimSpace(user.Email)
		}
	}
	payload := map[string]any{
		"workspace_id":     ws.ID,
		"name":             ws.Name,
		"share_permission": permission,
		"owner_user_id":    ws.UserID,
		"owner_email":      ownerEmail,
	}
	if strings.TrimSpace(accessToken) != "" {
		payload["access_token"] = accessToken
	}
	writeJSON(w, http.StatusOK, payload)
}

func CloudWorkspaceAcceptShareHandler(svc *cloudworkspace.Service, identity veMachineAuthenticator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.PathValue("token"))
		var claim cloudworkspace.ShareClaim
		rawBody, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if len(rawBody) > 0 {
			if err := json.Unmarshal(rawBody, &claim); err != nil {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid cloud workspace share claim")
				return
			}
		}
		if raw := bearerToken(r); strings.HasPrefix(raw, cloudworkspace.ShareAccessTokenPrefix) || raw == "" {
			if strings.TrimSpace(claim.HomeUserID) == "" {
				writeError(w, http.StatusBadRequest, "INVALID_INPUT", "home_user_id is required")
				return
			}
			ws, permission, accessToken, err := svc.ClaimShare(r.Context(), token, claim, time.Time{})
			if err != nil {
				writeCloudWorkspaceError(w, err)
				return
			}
			writeCloudWorkspaceShareAccepted(w, svc, r, ws, permission, accessToken)
			return
		}
		// Accepting a share is an identity join, not a workspace write. Recipients
		// often have no process instance session yet (and may not be granted the
		// feature, so they cannot issue one). Authenticate the machine user only.
		principal, ok := authenticateVEMachine(w, r, identity)
		if !ok {
			return
		}
		if strings.TrimSpace(principal.UserID) == "" {
			writeError(w, http.StatusUnauthorized, "MACHINE_UNAUTHORIZED", "machine is not associated with a user")
			return
		}
		ws, permission, err := svc.AcceptShare(r.Context(), *principal, token, claim.Password)
		if err != nil {
			writeCloudWorkspaceError(w, err)
			return
		}
		writeCloudWorkspaceShareAccepted(w, svc, r, ws, permission, "")
	}
}

// CloudWorkspaceSharePublicPageHandler GET /hub/cloud-workspaces/shares/{token}
func CloudWorkspaceSharePublicPageHandler(svc *cloudworkspace.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.PathValue("token"))
		note := "点击下方按钮，在已安装的 MaClaw 中打开。同一 Hub 或其他 Hub 登录均可加入该云端工作区，并导入到任务列表。"
		openEnabled := true
		if svc != nil {
			info, err := svc.PublicShare(r.Context(), token)
			switch {
			case err != nil:
				note = "该分享链接不存在或已停止分享。"
				openEnabled = false
			case info.Expired:
				note = "该分享链接已过期。"
				openEnabled = false
			case !info.Active:
				note = "该分享链接已停止分享。"
				openEnabled = false
			case info.PasswordRequired:
				note = "该分享设有密码。打开 MaClaw 后输入分享密码即可加入。"
			}
		}
		base := strings.TrimRight(userReferralPublicBaseURL(r), "/")
		openQuery := url.Values{"token": {token}}
		if base != "" {
			openQuery.Set("hub_url", base)
		}
		openHref := "maclaw://cloud-workspace-share?" + openQuery.Encode()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		button := ""
		script := ""
		if openEnabled {
			button = fmt.Sprintf(`<p><a class="button" href="%s">在 MaClaw 中打开</a></p>`, html.EscapeString(openHref))
			script = fmt.Sprintf(`<script>try{location.href=%q;}catch(e){}</script>`, openHref)
		}
		_, _ = fmt.Fprintf(w, `<!doctype html><html lang="zh"><head><meta charset="utf-8"><title>云端工作区分享</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:3rem auto;padding:0 1.2rem;color:#1c2733;background:#f4f7fb}
.card{background:#fff;border:1px solid #d9e1ec;border-radius:10px;padding:1.4rem 1.5rem;box-shadow:0 8px 24px rgba(30,58,95,.08)}
h1{font-size:1.15rem;margin:0 0 .6rem}p{line-height:1.55;color:#44546a}
a.button{display:inline-block;margin-top:.4rem;background:#2f6fbc;color:#fff;text-decoration:none;border-radius:8px;padding:.55rem 1rem;font-weight:700}
code{font-size:.85rem;word-break:break-all}</style></head>
<body><div class="card"><h1>云端工作区分享</h1>
<p>%s</p>
%s
<p>如果没有自动打开，请先启动并登录 MaClaw，再回到此页点击按钮。</p>
<p>分享令牌：<code>%s</code></p>
</div>%s</body></html>`, html.EscapeString(note), button, html.EscapeString(token), script)
	}
}
