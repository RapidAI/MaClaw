package guiapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/fileutil"
)

var (
	errCloudWorkspaceSharePasswordRequired = errors.New("share password required")
	errCloudWorkspaceSharePasswordInvalid  = errors.New("share password invalid")
	errCloudWorkspaceShareExpired          = errors.New("share expired")
	errCloudWorkspaceShareSelf             = errors.New("share self")
	errCloudWorkspaceShareRevoked          = errors.New("share revoked")
)

type localizedCloudWorkspaceShareError struct {
	kind error
}

func (e *localizedCloudWorkspaceShareError) Error() string {
	if e == nil {
		return ""
	}
	return cloudWorkspaceShareKindText(e.kind)
}

func (e *localizedCloudWorkspaceShareError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.kind
}

func wrapCloudWorkspaceShareError(kind error) error {
	if kind == nil {
		return nil
	}
	return &localizedCloudWorkspaceShareError{kind: kind}
}

func cloudWorkspaceTr(en, zhHans, zhHant string) string {
	lang, _ := agentViewCurrentLang.Load().(string)
	switch normalizeAppLanguageKind(lang) {
	case appLanguageZhHant:
		return zhHant
	case appLanguageEnglish:
		return en
	default:
		return zhHans
	}
}

func cloudWorkspaceShareKindText(kind error) string {
	switch {
	case errors.Is(kind, errCloudWorkspaceShareSelf):
		return cloudWorkspaceTr("This is your own cloud workspace. You don't need the share link to open it.", "这是你自己的云端工作区，无需通过分享链接加入。", "這是你自己的雲端工作區，無需透過分享連結加入。")
	case errors.Is(kind, errCloudWorkspaceSharePasswordRequired):
		return cloudWorkspaceTr("This share requires a password.", "需要分享密码", "需要分享密碼")
	case errors.Is(kind, errCloudWorkspaceSharePasswordInvalid):
		return cloudWorkspaceTr("The share password is incorrect.", "分享密码不正确", "分享密碼不正確")
	case errors.Is(kind, errCloudWorkspaceShareExpired):
		return cloudWorkspaceTr("This share link has expired.", "分享链接已过期", "分享連結已過期")
	case errors.Is(kind, errCloudWorkspaceShareRevoked):
		return cloudWorkspaceTr("This share link is no longer active.", "该分享链接已停止分享", "該分享連結已停止分享")
	default:
		if kind == nil {
			return ""
		}
		return kind.Error()
	}
}

func cloudWorkspaceShareJoinErrorText(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, errCloudWorkspaceShareSelf),
		errors.Is(err, errCloudWorkspaceSharePasswordRequired),
		errors.Is(err, errCloudWorkspaceSharePasswordInvalid),
		errors.Is(err, errCloudWorkspaceShareExpired),
		errors.Is(err, errCloudWorkspaceShareRevoked):
		return cloudWorkspaceShareKindText(err)
	}
	msg := strings.TrimSpace(err.Error())
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "cannot accept your own"):
		return cloudWorkspaceShareKindText(errCloudWorkspaceShareSelf)
	case strings.Contains(lower, "share password required"):
		return cloudWorkspaceShareKindText(errCloudWorkspaceSharePasswordRequired)
	case strings.Contains(lower, "share password is incorrect"):
		return cloudWorkspaceShareKindText(errCloudWorkspaceSharePasswordInvalid)
	case strings.Contains(lower, "share has expired"), strings.Contains(lower, "share link has expired"):
		return cloudWorkspaceShareKindText(errCloudWorkspaceShareExpired)
	case strings.Contains(lower, "share is revoked"), strings.Contains(lower, "share is no longer"):
		return cloudWorkspaceShareKindText(errCloudWorkspaceShareRevoked)
	default:
		return msg
	}
}

func cloudWorkspaceShareImportToast(err error) (message, typ string) {
	if errors.Is(err, errCloudWorkspaceShareSelf) {
		return cloudWorkspaceShareKindText(errCloudWorkspaceShareSelf), "info"
	}
	return cloudWorkspaceTr("Failed to join cloud workspace: ", "加入云端工作区失败：", "加入雲端工作區失敗：") + cloudWorkspaceShareJoinErrorText(err), "error"
}

const (
	cloudWorkspaceShareReadTagPrefix  = "cloud_workspace_share:read"
	cloudWorkspaceShareWriteTagPrefix = "cloud_workspace_share:write"
	cloudWorkspaceSharedFromPrefix    = "cloud_workspace_shared_from:"
)

// CloudWorkspaceShareRecipient is one accepted recipient of a share link.
type CloudWorkspaceShareRecipient struct {
	UserID     string `json:"user_id"`
	Email      string `json:"email,omitempty"`
	Permission string `json:"permission"`
	AcceptedAt string `json:"accepted_at"`
}

// CloudWorkspaceShareView is the owner-facing share payload.
type CloudWorkspaceShareView struct {
	WorkspaceID       string                         `json:"workspace_id"`
	ShareID           string                         `json:"share_id,omitempty"`
	ShareURL          string                         `json:"share_url,omitempty"`
	Token             string                         `json:"token,omitempty"`
	DefaultPermission string                         `json:"default_permission,omitempty"`
	Status            string                         `json:"status,omitempty"`
	PasswordSet       bool                           `json:"password_set"`
	ExpiresAt         string                         `json:"expires_at,omitempty"`
	Recipients        []CloudWorkspaceShareRecipient `json:"recipients"`
}

func cloudWorkspaceTaskIsReadShare(result ProjectSearchResult) bool {
	for _, tag := range result.Tags {
		if strings.TrimSpace(tag) == cloudWorkspaceShareReadTagPrefix {
			return true
		}
	}
	return false
}

func cloudWorkspaceShareTag(permission string) string {
	if permission == "write" {
		return cloudWorkspaceShareWriteTagPrefix
	}
	return cloudWorkspaceShareReadTagPrefix
}

func parseCloudWorkspaceShareToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "/") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	const marker = "/hub/cloud-workspaces/shares/"
	if idx := strings.Index(parsed.Path, marker); idx >= 0 {
		token := strings.Trim(parsed.Path[idx+len(marker):], "/")
		if slash := strings.IndexByte(token, '/'); slash >= 0 {
			token = token[:slash]
		}
		if token != "" {
			return token
		}
	}
	const apiMarker = "/api/v1/cloud-workspace-shares/"
	if idx := strings.Index(parsed.Path, apiMarker); idx >= 0 {
		rest := strings.Trim(parsed.Path[idx+len(apiMarker):], "/")
		rest = strings.TrimSuffix(rest, "/accept")
		rest = strings.Trim(rest, "/")
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			rest = rest[:slash]
		}
		return rest
	}
	if strings.EqualFold(parsed.Scheme, "maclaw") && strings.EqualFold(parsed.Host, "cloud-workspace-share") {
		if token := strings.TrimSpace(parsed.Query().Get("token")); token != "" {
			return token
		}
		return strings.Trim(parsed.Path, "/")
	}
	return ""
}

type CloudWorkspaceShareLaunch struct {
	Token  string `json:"token"`
	HubURL string `json:"hub_url"`
}

func parseCloudWorkspaceShareHub(raw string) string {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	if strings.EqualFold(parsed.Scheme, "maclaw") {
		hub := strings.TrimRight(strings.TrimSpace(parsed.Query().Get("hub_url")), "/")
		if validReferralHandoffHubURL(hub) {
			return hub
		}
		return ""
	}
	if (strings.EqualFold(parsed.Scheme, "https") || strings.EqualFold(parsed.Scheme, "http")) && parseCloudWorkspaceShareToken(raw) != "" {
		origin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
		if parsed.Host != "" && validReferralHandoffHubURL(origin) {
			return origin
		}
	}
	return ""
}

func parseCloudWorkspaceShareLaunch(raw string) CloudWorkspaceShareLaunch {
	return CloudWorkspaceShareLaunch{
		Token:  parseCloudWorkspaceShareToken(raw),
		HubURL: parseCloudWorkspaceShareHub(raw),
	}
}

func cloudWorkspaceShareFromArgs(args []string) CloudWorkspaceShareLaunch {
	for _, arg := range args {
		if !strings.Contains(arg, "://") {
			continue
		}
		launch := parseCloudWorkspaceShareLaunch(arg)
		if launch.Token != "" {
			return launch
		}
	}
	return CloudWorkspaceShareLaunch{}
}

func sameHubOrigin(a, b string) bool {
	na, nb := normalizeHubOrigin(a), normalizeHubOrigin(b)
	return na != "" && na == nb
}

func normalizeHubOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Host) == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + strings.ToLower(parsed.Host)
}

func cloudWorkspaceSharedFromLabel(tags []string) string {
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if strings.HasPrefix(tag, cloudWorkspaceSharedFromPrefix) {
			return strings.TrimSpace(strings.TrimPrefix(tag, cloudWorkspaceSharedFromPrefix))
		}
	}
	return ""
}

func (a *App) absoluteCloudWorkspaceShareURL(pathOrURL string) string {
	pathOrURL = strings.TrimSpace(pathOrURL)
	if pathOrURL == "" {
		return ""
	}
	if strings.Contains(pathOrURL, "://") {
		return pathOrURL
	}
	hubURL, _, _, err := a.virtualRepositorySyncClient()
	if err != nil || strings.TrimSpace(hubURL) == "" {
		return pathOrURL
	}
	return strings.TrimRight(strings.TrimSpace(hubURL), "/") + pathOrURL
}

func decodeCloudWorkspaceShareView(data []byte) (CloudWorkspaceShareView, error) {
	var view CloudWorkspaceShareView
	if err := json.Unmarshal(data, &view); err != nil {
		return CloudWorkspaceShareView{}, err
	}
	if view.Recipients == nil {
		view.Recipients = []CloudWorkspaceShareRecipient{}
	}
	return view, nil
}

func (a *App) fillCloudWorkspaceShareURL(view *CloudWorkspaceShareView) {
	if view == nil {
		return
	}
	if strings.TrimSpace(view.ShareURL) == "" && strings.TrimSpace(view.Token) != "" {
		view.ShareURL = a.absoluteCloudWorkspaceShareURL("/hub/cloud-workspaces/shares/" + url.PathEscape(strings.TrimSpace(view.Token)))
	} else if strings.TrimSpace(view.ShareURL) != "" && !strings.Contains(view.ShareURL, "://") {
		view.ShareURL = a.absoluteCloudWorkspaceShareURL(view.ShareURL)
	}
}

// GetCloudWorkspaceShare returns the active share link and recipients for a workspace you own.
func (a *App) GetCloudWorkspaceShare(workspaceID string) (CloudWorkspaceShareView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return CloudWorkspaceShareView{}, fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodGet, cloudWorkspaceSharePath(workspaceID), cloudWorkspaceHTTPOptions{accept: "application/json"})
	if err != nil {
		return CloudWorkspaceShareView{}, err
	}
	if status >= 300 {
		return CloudWorkspaceShareView{}, cloudWorkspaceAPIError(status, data)
	}
	view, err := decodeCloudWorkspaceShareView(data)
	if err != nil {
		return CloudWorkspaceShareView{}, err
	}
	view.WorkspaceID = workspaceID
	a.fillCloudWorkspaceShareURL(&view)
	return view, nil
}

// CreateCloudWorkspaceShare generates or returns the share link. permission is read or write.
func (a *App) CreateCloudWorkspaceShare(workspaceID, permission, password, ttl string, clearPassword bool) (CloudWorkspaceShareView, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return CloudWorkspaceShareView{}, fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, cloudWorkspaceSharePath(workspaceID), cloudWorkspaceHTTPOptions{
		jsonBody: map[string]any{
			"permission":     strings.TrimSpace(permission),
			"password":       password,
			"ttl":            strings.TrimSpace(ttl),
			"clear_password": clearPassword,
		},
		accept: "application/json",
	})
	if err != nil {
		return CloudWorkspaceShareView{}, err
	}
	if status >= 300 {
		return CloudWorkspaceShareView{}, cloudWorkspaceAPIError(status, data)
	}
	view, err := decodeCloudWorkspaceShareView(data)
	if err != nil {
		return CloudWorkspaceShareView{}, err
	}
	view.WorkspaceID = workspaceID
	a.fillCloudWorkspaceShareURL(&view)
	return view, nil
}

// StopCloudWorkspaceShare revokes the link and removes every recipient.
func (a *App) StopCloudWorkspaceShare(workspaceID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return fmt.Errorf("workspace id is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodDelete, cloudWorkspaceSharePath(workspaceID), cloudWorkspaceHTTPOptions{accept: "application/json"})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	a.dropShareAccessSession(workspaceID)
	return nil
}

// UpdateCloudWorkspaceShareRecipient changes one recipient's permission.
func (a *App) UpdateCloudWorkspaceShareRecipient(workspaceID, recipientUserID, permission string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	recipientUserID = strings.TrimSpace(recipientUserID)
	if !validCloudWorkspaceCacheID(workspaceID) || recipientUserID == "" {
		return fmt.Errorf("workspace id and recipient are required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPatch, cloudWorkspaceShareRecipientPath(workspaceID, recipientUserID), cloudWorkspaceHTTPOptions{
		jsonBody: map[string]string{"permission": strings.TrimSpace(permission)},
		accept:   "application/json",
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	return nil
}

// RemoveCloudWorkspaceShareRecipient drops one recipient.
func (a *App) RemoveCloudWorkspaceShareRecipient(workspaceID, recipientUserID string) error {
	workspaceID = strings.TrimSpace(workspaceID)
	recipientUserID = strings.TrimSpace(recipientUserID)
	if !validCloudWorkspaceCacheID(workspaceID) || recipientUserID == "" {
		return fmt.Errorf("workspace id and recipient are required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodDelete, cloudWorkspaceShareRecipientPath(workspaceID, recipientUserID), cloudWorkspaceHTTPOptions{accept: "application/json"})
	if err != nil {
		return err
	}
	if status >= 300 {
		return cloudWorkspaceAPIError(status, data)
	}
	return nil
}

type cloudWorkspaceShareAccessSession struct {
	WorkspaceID string `json:"workspace_id"`
	HubURL      string `json:"hub_url"`
	AccessToken string `json:"access_token"`
	Permission  string `json:"permission"`
	OwnerEmail  string `json:"owner_email"`
	Name        string `json:"name"`
}

var (
	cloudWorkspaceShareAccessMu          sync.Mutex
	cloudWorkspaceShareAccessCache       map[string]cloudWorkspaceShareAccessSession
	cloudWorkspaceShareAccessLoaded      bool
	shareAccessBoundRemoteUser           string
	shareAccessBoundRemoteUserSet        bool
	shareAccessRemoteUserOverride        string
)

func resetCloudWorkspaceShareAccessCache() {
	cloudWorkspaceShareAccessMu.Lock()
	defer cloudWorkspaceShareAccessMu.Unlock()
	cloudWorkspaceShareAccessCache = nil
	cloudWorkspaceShareAccessLoaded = false
	shareAccessBoundRemoteUser = ""
	shareAccessBoundRemoteUserSet = false
	shareAccessRemoteUserOverride = ""
}

func (a *App) shareAccessFile() string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "cloud-workspace-share-access.json")
}

func (a *App) configuredRemoteUserID() string {
	if override := strings.TrimSpace(shareAccessRemoteUserOverride); override != "" {
		return override
	}
	if a == nil {
		return ""
	}
	cfg, _ := a.LoadConfig()
	return strings.TrimSpace(cfg.RemoteUserID)
}

func (a *App) reconcileShareAccessIdentityLocked() {
	current := a.configuredRemoteUserID()
	if !shareAccessBoundRemoteUserSet {
		shareAccessBoundRemoteUser = current
		shareAccessBoundRemoteUserSet = true
		return
	}
	if current != "" && shareAccessBoundRemoteUser != "" && current != shareAccessBoundRemoteUser {
		shareAccessBoundRemoteUser = current
		cloudWorkspaceShareAccessCache = map[string]cloudWorkspaceShareAccessSession{}
		cloudWorkspaceShareAccessLoaded = true
		a.persistShareAccessMapLocked()
		return
	}
	if current != "" {
		shareAccessBoundRemoteUser = current
	}
}

func (a *App) shareAccessMapLocked() map[string]cloudWorkspaceShareAccessSession {
	a.reconcileShareAccessIdentityLocked()
	if cloudWorkspaceShareAccessLoaded {
		if cloudWorkspaceShareAccessCache == nil {
			cloudWorkspaceShareAccessCache = map[string]cloudWorkspaceShareAccessSession{}
		}
		return cloudWorkspaceShareAccessCache
	}
	cloudWorkspaceShareAccessLoaded = true
	cloudWorkspaceShareAccessCache = map[string]cloudWorkspaceShareAccessSession{}
	path := a.shareAccessFile()
	if path == "" {
		return cloudWorkspaceShareAccessCache
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return cloudWorkspaceShareAccessCache
	}
	var stored cloudWorkspaceShareAccessFile
	if json.Unmarshal(raw, &stored) == nil && stored.Sessions != nil {
		current := a.configuredRemoteUserID()
		if current != "" && strings.TrimSpace(stored.BoundUserID) != "" && current != strings.TrimSpace(stored.BoundUserID) {
			a.persistShareAccessMapLocked()
			return cloudWorkspaceShareAccessCache
		}
		cloudWorkspaceShareAccessCache = stored.Sessions
		if current == "" && strings.TrimSpace(stored.BoundUserID) != "" {
			shareAccessBoundRemoteUser = strings.TrimSpace(stored.BoundUserID)
			shareAccessBoundRemoteUserSet = true
		}
		return cloudWorkspaceShareAccessCache
	}
	var parsed map[string]cloudWorkspaceShareAccessSession
	if json.Unmarshal(raw, &parsed) == nil && parsed != nil {
		cloudWorkspaceShareAccessCache = parsed
	}
	return cloudWorkspaceShareAccessCache
}

type cloudWorkspaceShareAccessFile struct {
	BoundUserID string                                    `json:"bound_user_id,omitempty"`
	Sessions    map[string]cloudWorkspaceShareAccessSession `json:"sessions"`
}

func (a *App) persistShareAccessMapLocked() {
	path := a.shareAccessFile()
	if path == "" {
		return
	}
	if cloudWorkspaceShareAccessCache == nil {
		cloudWorkspaceShareAccessCache = map[string]cloudWorkspaceShareAccessSession{}
	}
	raw, err := json.MarshalIndent(cloudWorkspaceShareAccessFile{
		BoundUserID: shareAccessBoundRemoteUser,
		Sessions:    cloudWorkspaceShareAccessCache,
	}, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = fileutil.AtomicWriteFile(path, raw, 0o600)
}

func (a *App) dropShareAccessSession(workspaceID string) {
	workspaceID = strings.TrimSpace(workspaceID)
	if a == nil || workspaceID == "" {
		return
	}
	cloudWorkspaceShareAccessMu.Lock()
	defer cloudWorkspaceShareAccessMu.Unlock()
	all := a.shareAccessMapLocked()
	if _, ok := all[workspaceID]; !ok {
		return
	}
	delete(all, workspaceID)
	a.persistShareAccessMapLocked()
}

func (a *App) saveShareAccessSession(sess cloudWorkspaceShareAccessSession) {
	id := strings.TrimSpace(sess.WorkspaceID)
	if a == nil || id == "" || strings.TrimSpace(sess.AccessToken) == "" || strings.TrimSpace(sess.HubURL) == "" {
		return
	}
	if !validReferralHandoffHubURL(sess.HubURL) {
		return
	}
	cloudWorkspaceShareAccessMu.Lock()
	defer cloudWorkspaceShareAccessMu.Unlock()
	all := a.shareAccessMapLocked()
	all[id] = sess
	a.persistShareAccessMapLocked()
}

func (a *App) lookupShareAccessSession(workspaceID string) (cloudWorkspaceShareAccessSession, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return cloudWorkspaceShareAccessSession{}, false
	}
	cloudWorkspaceShareAccessMu.Lock()
	defer cloudWorkspaceShareAccessMu.Unlock()
	sess, ok := a.shareAccessMapLocked()[workspaceID]
	if !ok || strings.TrimSpace(sess.AccessToken) == "" || strings.TrimSpace(sess.HubURL) == "" {
		return cloudWorkspaceShareAccessSession{}, false
	}
	return sess, true
}

func (a *App) lookupForeignShareAccessSession(workspaceID string) (cloudWorkspaceShareAccessSession, bool) {
	sess, ok := a.lookupShareAccessSession(workspaceID)
	if !ok {
		return cloudWorkspaceShareAccessSession{}, false
	}
	homeHub, _, _, err := a.virtualRepositorySyncClient()
	if !validReferralHandoffHubURL(sess.HubURL) {
		return cloudWorkspaceShareAccessSession{}, false
	}
	if err == nil && sameHubOrigin(sess.HubURL, homeHub) {
		return cloudWorkspaceShareAccessSession{}, false
	}
	return sess, true
}

func (a *App) localShareClaim() cloudworkspaceShareClaim {
	cfg, _ := a.LoadConfig()
	homeHub := strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/")
	homeUser := strings.TrimSpace(cfg.RemoteUserID)
	display := strings.TrimSpace(cfg.RemoteEmail)
	if homeUser == "" {
		homeUser = display
	}
	if homeUser == "" {
		homeUser = strings.TrimSpace(cfg.RemoteMachineID)
	}
	if homeUser == "" {
		homeUser = cloudWorkspaceClientInstanceID()
	}
	if display == "" {
		display = homeUser
	}
	return cloudworkspaceShareClaim{
		HomeHub:      homeHub,
		HomeUserID:   homeUser,
		HomeTenantID: strings.TrimSpace(cfg.RemoteTenantID),
		DisplayName:  display,
	}
}

type cloudworkspaceShareClaim struct {
	HomeHub      string `json:"home_hub"`
	HomeUserID   string `json:"home_user_id"`
	HomeTenantID string `json:"home_tenant_id"`
	DisplayName  string `json:"display_name"`
	Password     string `json:"password,omitempty"`
}

func (a *App) postAcceptCloudWorkspaceShare(ctx context.Context, token string, launch CloudWorkspaceShareLaunch) ([]byte, int, error) {
	return a.postAcceptCloudWorkspaceShareWithPassword(ctx, token, launch, "")
}

func (a *App) postAcceptCloudWorkspaceShareWithPassword(ctx context.Context, token string, launch CloudWorkspaceShareLaunch, password string) ([]byte, int, error) {
	path := cloudWorkspaceShareAcceptPath(token)
	claim := a.localShareClaim()
	claim.Password = password
	claimOpt := cloudWorkspaceHTTPOptions{jsonBody: claim, accept: "application/json"}
	homeHub, machineToken, machineID, homeErr := a.virtualRepositorySyncClient()
	if launch.HubURL != "" && (homeErr != nil || !sameHubOrigin(launch.HubURL, homeHub)) {
		return a.cloudWorkspaceHubDoRemote(ctx, launch.HubURL, "", http.MethodPost, path, claimOpt)
	}
	opt := cloudWorkspaceHTTPOptions{jsonBody: map[string]string{"password": password}, accept: "application/json"}
	data, status, err := a.cloudWorkspaceHubDo(ctx, http.MethodPost, path, opt)
	if err == nil && status < 300 {
		return data, status, nil
	}
	if err != nil || !cloudWorkspaceSessionRejected(status, data) {
		return data, status, err
	}
	if homeErr == nil {
		dropCloudWorkspaceInstanceSession(homeHub, machineToken, machineID)
	}
	opt.instanceSession = true
	retried, retryStatus, retryErr := a.cloudWorkspaceHubDo(ctx, http.MethodPost, path, opt)
	if retryErr == nil && retryStatus < 300 {
		return retried, retryStatus, nil
	}
	if retryErr == nil && !cloudWorkspaceSessionRejected(retryStatus, retried) {
		return retried, retryStatus, nil
	}
	claimHub := strings.TrimRight(strings.TrimSpace(launch.HubURL), "/")
	if claimHub == "" {
		claimHub = strings.TrimRight(strings.TrimSpace(homeHub), "/")
	}
	if claimHub != "" {
		return a.cloudWorkspaceHubDoRemote(ctx, claimHub, "", http.MethodPost, path, claimOpt)
	}
	if retryErr != nil {
		return retried, retryStatus, retryErr
	}
	return retried, retryStatus, nil
}

// AcceptCloudWorkspaceShare joins a share link and creates a local cloud task.
func (a *App) AcceptCloudWorkspaceShare(tokenOrURL, password string) (ProjectSearchResult, error) {
	launch := parseCloudWorkspaceShareLaunch(tokenOrURL)
	token := launch.Token
	if token == "" {
		token = parseCloudWorkspaceShareToken(tokenOrURL)
	}
	if token == "" {
		return ProjectSearchResult{}, fmt.Errorf("share token is required")
	}
	ctx, cancel := a.cloudWorkspaceRequestContext()
	defer cancel()
	data, status, err := a.postAcceptCloudWorkspaceShareWithPassword(ctx, token, launch, password)
	if err != nil {
		return ProjectSearchResult{}, err
	}
	if status >= 300 {
		return ProjectSearchResult{}, cloudWorkspaceAPIError(status, data)
	}
	var accepted struct {
		WorkspaceID     string `json:"workspace_id"`
		Name            string `json:"name"`
		SharePermission string `json:"share_permission"`
		OwnerUserID     string `json:"owner_user_id"`
		OwnerEmail      string `json:"owner_email"`
		AccessToken     string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &accepted); err != nil {
		return ProjectSearchResult{}, err
	}
	workspaceID := strings.TrimSpace(accepted.WorkspaceID)
	if !validCloudWorkspaceCacheID(workspaceID) {
		return ProjectSearchResult{}, fmt.Errorf("workspace id is required")
	}
	a.clearDismissedCloudWorkspaceTask(workspaceID)
	permission := strings.TrimSpace(accepted.SharePermission)
	if permission != "write" {
		permission = "read"
	}
	ownerLabel := strings.TrimSpace(accepted.OwnerEmail)
	if ownerLabel == "" {
		ownerLabel = strings.TrimSpace(accepted.OwnerUserID)
	}
	if token := strings.TrimSpace(accepted.AccessToken); token != "" {
		hub := launch.HubURL
		if hub == "" {
			hub, _, _, _ = a.virtualRepositorySyncClient()
		}
		a.saveShareAccessSession(cloudWorkspaceShareAccessSession{
			WorkspaceID: workspaceID,
			HubURL:      hub,
			AccessToken: token,
			Permission:  permission,
			OwnerEmail:  ownerLabel,
			Name:        strings.TrimSpace(accepted.Name),
		})
	}
	if existing := a.findVisibleCloudWorkspaceTask(workspaceID); strings.TrimSpace(existing.ProjectPath) != "" {
		return a.tagSharedCloudWorkspaceTask(existing, workspaceID, permission, ownerLabel), nil
	}
	name := strings.TrimSpace(accepted.Name)
	if name == "" {
		name = "云端工作区"
	}
	workingDir := a.cloudWorkspaceShareWorkingDir(workspaceID, permission)
	tags := []string{taskManagementTag, taskUserCreatedTag, cloudWorkspaceTag(workspaceID), cloudWorkspaceShareTag(permission)}
	if ownerLabel != "" {
		tags = append(tags, cloudWorkspaceSharedFromPrefix+ownerLabel)
	}
	result := a.createTaskRecordWithWorkingDir(name, "", tags, workingDir, false)
	if strings.TrimSpace(result.ProjectPath) == "" {
		return ProjectSearchResult{}, fmt.Errorf("加入云端工作区失败")
	}
	return a.bindPreparedCloudWorkspaceTask(workspaceID, result, workingDir), nil
}

func (a *App) cloudWorkspaceShareWorkingDir(workspaceID, permission string) string {
	tenantID := a.cloudWorkspaceTenantID()
	if permission == "write" {
		return a.cloudWorkspaceCachePath(tenantID, workspaceID)
	}
	return a.cloudWorkspaceReadOnlyCachePath(tenantID, workspaceID)
}

func (a *App) tagSharedCloudWorkspaceTask(result ProjectSearchResult, workspaceID, permission, ownerLabel string) ProjectSearchResult {
	addTags := []string{cloudWorkspaceShareTag(permission)}
	if owner := strings.TrimSpace(ownerLabel); owner != "" {
		addTags = append(addTags, cloudWorkspaceSharedFromPrefix+owner)
	}
	tags := append([]string{}, result.Tags...)
	kept := tags[:0]
	for _, tag := range tags {
		if strings.HasPrefix(tag, "cloud_workspace_share:") || strings.HasPrefix(tag, cloudWorkspaceSharedFromPrefix) {
			continue
		}
		kept = append(kept, tag)
	}
	result.Tags = append(kept, addTags...)
	path := normalizeProjectSessionPath(result.ProjectPath)
	if a != nil && path != "" && a.memoryStore != nil {
		if pi := a.memoryStore.ProjectIndex(); pi != nil {
			pi.ReplacePrefixedTags(path, []string{"cloud_workspace_share:", cloudWorkspaceSharedFromPrefix}, addTags)
		}
	}
	workingDir := strings.TrimSpace(result.WorkingDir)
	if next := a.cloudWorkspaceShareWorkingDir(workspaceID, permission); next != "" {
		workingDir = next
	}
	return a.bindPreparedCloudWorkspaceTask(workspaceID, result, workingDir)
}

func (a *App) setPendingCloudWorkspaceShare(launch CloudWorkspaceShareLaunch) {
	launch.Token = strings.TrimSpace(launch.Token)
	launch.HubURL = strings.TrimRight(strings.TrimSpace(launch.HubURL), "/")
	if a == nil || launch.Token == "" {
		return
	}
	a.cloudWorkspaceShareMu.Lock()
	a.pendingCloudWorkspaceShare = launch
	ready := a.ctx != nil && a.hasWailsEventsContext()
	a.cloudWorkspaceShareMu.Unlock()
	if ready {
		go a.applyPendingCloudWorkspaceShare()
	}
}

func (a *App) takePendingCloudWorkspaceShare() CloudWorkspaceShareLaunch {
	if a == nil {
		return CloudWorkspaceShareLaunch{}
	}
	a.cloudWorkspaceShareMu.Lock()
	defer a.cloudWorkspaceShareMu.Unlock()
	launch := a.pendingCloudWorkspaceShare
	a.pendingCloudWorkspaceShare = CloudWorkspaceShareLaunch{}
	return launch
}

func (a *App) applyPendingCloudWorkspaceShare() {
	launch := a.takePendingCloudWorkspaceShare()
	if strings.TrimSpace(launch.Token) == "" {
		return
	}
	raw := launch.Token
	if launch.HubURL != "" {
		raw = launch.HubURL + "/hub/cloud-workspaces/shares/" + url.PathEscape(launch.Token)
	}
	a.importCloudWorkspaceShareLink(raw)
}

func (a *App) importCloudWorkspaceShareLink(token string) {
	token = strings.TrimSpace(token)
	if a == nil || token == "" {
		return
	}
	result, err := a.AcceptCloudWorkspaceShare(token, "")
	if err != nil {
		if errors.Is(err, errCloudWorkspaceSharePasswordRequired) {
			a.emitEvent(EventCloudWorkspaceSharePassword, map[string]string{"url": token})
			return
		}
		log.Printf("[cloud_workspace] import share failed err=%v", err)
		msg, typ := cloudWorkspaceShareImportToast(err)
		a.ShowToast(msg, typ)
		return
	}
	path := strings.TrimSpace(result.ProjectPath)
	a.emitProjectIndexChanged(path)
	name := strings.TrimSpace(result.Name)
	if name == "" {
		name = cloudWorkspaceTr("Cloud workspace", "云端工作区", "雲端工作區")
	}
	from := cloudWorkspaceSharedFromLabel(result.Tags)
	if from != "" {
		a.ShowToast(cloudWorkspaceTr("Joined “"+name+"” (from "+from+")", "已加入「"+name+"」（来自 "+from+"）", "已加入「"+name+"」（來自 "+from+"）"), "success")
		return
	}
	a.ShowToast(cloudWorkspaceTr("Joined shared cloud workspace “"+name+"”", "已加入分享的云端工作区「"+name+"」", "已加入分享的雲端工作區「"+name+"」"), "success")
}
