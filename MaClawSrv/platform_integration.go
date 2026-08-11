package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
)

const (
	platformHubLLMProviderName = "hub-llm"
	platformHubLLMModel        = "auto"
	// platformDefaultLLMServiceGroupID is the Hub reserved free group used by
	// all server-side MaClawSrv agents when no explicit business group is set.
	platformDefaultLLMServiceGroupID = "system-free"
	platformAvatarImageMaxBytes      = 1024 * 1024
	platformAvatarDataURLMaxSize     = len("data:image/jpeg;base64,") + ((platformAvatarImageMaxBytes+2)/3)*4
	platformJSONBodyMaxBytes         = int64(platformAvatarDataURLMaxSize + 512*1024)
	platformAttachmentMaxBytes       = int64(50 * 1024 * 1024)
	platformAttachmentNameMaxLen     = 160
	platformAttachmentMaxCount       = 20
	platformAttachmentTimeout        = 60 * time.Second
)

func platformLLMServiceGroupID(values ...string) string {
	for _, v := range values {
		id := strings.TrimSpace(v)
		if id == "" {
			continue
		}
		// Normalize legacy Hub VE alias to the reserved free system group.
		if strings.EqualFold(id, "ve-service") {
			return platformDefaultLLMServiceGroupID
		}
		return id
	}
	return platformDefaultLLMServiceGroupID
}

func (s *HTTPServer) withPlatformAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimSpace(r.Header.Get("X-MaClaw-Admin-Secret")) == "" {
			if token := bearerToken(r.Header.Get("Authorization")); token != "" {
				r = cloneRequestWithHeader(r, "X-MaClaw-Admin-Secret", token)
			}
		}
		s.withAdmin(next)(w, r)
	}
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	if token == "" || strings.Contains(token, " ") {
		return ""
	}
	return token
}

func cloneRequestWithHeader(r *http.Request, key, value string) *http.Request {
	clone := r.Clone(r.Context())
	clone.Header = r.Header.Clone()
	clone.Header.Set(key, value)
	return clone
}

type platformVirtualEmployeeRequest struct {
	EmployeeID        string                  `json:"employee_id"`
	TenantID          string                  `json:"tenant_id"`
	PlatformTenantID  string                  `json:"platform_tenant_id"`
	TenantName        string                  `json:"tenant_name"`
	TenantCode        string                  `json:"tenant_code"`
	HubTenantCode     string                  `json:"hub_tenant_code"`
	Name              string                  `json:"name"`
	Handle            string                  `json:"handle"`
	VirtualEmail      string                  `json:"virtual_email"`
	SkillDescription  string                  `json:"skill_description"`
	SkillTags         platformSkillTags       `json:"skill_tags"`
	AvatarDataURL     string                  `json:"avatar_data_url"`
	DefaultLLM        string                  `json:"default_llm"`
	LLMServiceGroupID string                  `json:"llm_service_group_id"`
	HubLLMEndpoint    string                  `json:"hub_llm_endpoint"`
	HubLLMAPIKey      string                  `json:"hub_llm_api_key"`
	LLMModel          string                  `json:"llm_model"`
	HubLLMViewerToken string                  `json:"hub_llm_viewer_token"`
	ViewerToken       string                  `json:"viewer_token"`
	AccessToken       string                  `json:"access_token"`
	SSHHosts          []corelib.SSHHostEntry  `json:"ssh_hosts,omitempty"`
	MaclawSrvConfig   platformMaclawSrvConfig `json:"maclawsrv_config,omitempty"`
}

type platformMaclawSrvConfig struct {
	QQBotEnabled               *bool                `json:"qqbot_enabled,omitempty"`
	QQBotAppID                 *string              `json:"qqbot_app_id,omitempty"`
	QQBotAppSecret             *string              `json:"qqbot_app_secret,omitempty"`
	QQBotLocalMode             platformOptionalBool `json:"qqbot_local_mode,omitempty"`
	TelegramBotEnabled         *bool                `json:"telegram_bot_enabled,omitempty"`
	TelegramBotToken           *string              `json:"telegram_bot_token,omitempty"`
	TelegramLocalMode          platformOptionalBool `json:"telegram_local_mode,omitempty"`
	WeixinEnabled              *bool                `json:"weixin_enabled,omitempty"`
	WeixinToken                *string              `json:"weixin_token,omitempty"`
	WeixinBaseURL              *string              `json:"weixin_base_url,omitempty"`
	WeixinCDNURL               *string              `json:"weixin_cdn_url,omitempty"`
	WeixinAccountID            *string              `json:"weixin_account_id,omitempty"`
	WeixinLocalMode            platformOptionalBool `json:"weixin_local_mode,omitempty"`
	LansengerEnabled           *bool                `json:"lansenger_enabled,omitempty"`
	LansengerAppID             *string              `json:"lansenger_app_id,omitempty"`
	LansengerAppSecret         *string              `json:"lansenger_app_secret,omitempty"`
	LansengerGatewayURL        *string              `json:"lansenger_gateway_url,omitempty"`
	LansengerWSSURL            *string              `json:"lansenger_wss_url,omitempty"`
	LansengerLocalMode         platformOptionalBool `json:"lansenger_local_mode,omitempty"`
	ThirdPartyGatewayEnabled   *bool                `json:"thirdparty_gateway_enabled,omitempty"`
	ThirdPartyGatewayToken     *string              `json:"thirdparty_gateway_token,omitempty"`
	ThirdPartyGatewayHost      *string              `json:"thirdparty_gateway_host,omitempty"`
	ThirdPartyGatewayPort      *int                 `json:"thirdparty_gateway_port,omitempty"`
	ThirdPartyGatewayLocalMode platformOptionalBool `json:"thirdparty_gateway_local_mode,omitempty"`
}

type platformOptionalBool struct {
	Set   bool
	Value *bool
}

func (b *platformOptionalBool) UnmarshalJSON(data []byte) error {
	b.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		b.Value = nil
		return nil
	}
	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	b.Value = &value
	return nil
}

func (c platformMaclawSrvConfig) isZero() bool {
	return c.QQBotEnabled == nil && c.QQBotAppID == nil && c.QQBotAppSecret == nil && !c.QQBotLocalMode.Set && c.TelegramBotEnabled == nil && c.TelegramBotToken == nil && !c.TelegramLocalMode.Set && c.WeixinEnabled == nil && c.WeixinToken == nil && c.WeixinBaseURL == nil && c.WeixinCDNURL == nil && c.WeixinAccountID == nil && !c.WeixinLocalMode.Set && c.LansengerEnabled == nil && c.LansengerAppID == nil && c.LansengerAppSecret == nil && c.LansengerGatewayURL == nil && c.LansengerWSSURL == nil && !c.LansengerLocalMode.Set && c.ThirdPartyGatewayEnabled == nil && c.ThirdPartyGatewayToken == nil && c.ThirdPartyGatewayHost == nil && c.ThirdPartyGatewayPort == nil && !c.ThirdPartyGatewayLocalMode.Set
}

type platformSkillTags []string

func (t *platformSkillTags) UnmarshalJSON(data []byte) error {
	var tags []string
	if err := json.Unmarshal(data, &tags); err == nil {
		*t = cleanPlatformSkillTags(tags)
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*t = cleanPlatformSkillTags(strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\uff0c' || r == '\uff1b' || r == '\n' || r == '\t'
	}))
	return nil
}

func cleanPlatformSkillTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := map[string]bool{}
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

type platformRuntimeBinding struct {
	Tenant   agentservice.Tenant
	User     agentservice.User
	Instance agentservice.Instance
}

type platformSourceUserRequest struct {
	TenantID          string                 `json:"tenant_id"`
	SourceUser        platformSourceUser     `json:"source_user"`
	SourceUsers       []platformSourceUser   `json:"source_users,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Description       string                 `json:"description,omitempty"`
	InstanceID        string                 `json:"instance_id,omitempty"`
	Target            string                 `json:"target,omitempty"`
	SettingsTab       string                 `json:"settings_tab,omitempty"`
	DefaultLLM        string                 `json:"default_llm,omitempty"`
	LLMServiceGroupID string                 `json:"llm_service_group_id,omitempty"`
	LLMModel          string                 `json:"llm_model,omitempty"`
	HubLLMEndpoint    string                 `json:"hub_llm_endpoint,omitempty"`
	HubLLMAPIKey      string                 `json:"hub_llm_api_key,omitempty"`
	HubLLMViewerToken string                 `json:"hub_llm_viewer_token,omitempty"`
	ViewerToken       string                 `json:"viewer_token,omitempty"`
	AccessToken       string                 `json:"access_token,omitempty"`
	SSHHosts          []corelib.SSHHostEntry `json:"ssh_hosts,omitempty"`
}

type platformSourceUser struct {
	ID                string `json:"id"`
	TenantID          string `json:"tenant_id"`
	ExternalID        string `json:"external_id"`
	Email             string `json:"email"`
	DisplayName       string `json:"display_name"`
	Department        string `json:"department"`
	Title             string `json:"title"`
	SkillTags         string `json:"skill_tags,omitempty"`
	SkillTagsSet      bool   `json:"-"`
	Status            string `json:"status"`
	AccountType       string `json:"account_type,omitempty"`
	Provider          string `json:"provider,omitempty"`
	IsVirtualEmployee bool   `json:"is_virtual_employee,omitempty"`
}

func (u *platformSourceUser) UnmarshalJSON(data []byte) error {
	type alias platformSourceUser
	var out alias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	out.SkillTagsSet = false
	for key := range raw {
		if strings.EqualFold(key, "skill_tags") {
			out.SkillTagsSet = true
			break
		}
	}
	*u = platformSourceUser(out)
	return nil
}

type platformSourceUserBinding struct {
	Tenant agentservice.Tenant
	User   agentservice.User
	Source platformSourceUser
}

type platformVirtualEmployeeMessageRequest struct {
	EmployeeID      string          `json:"employee_id"`
	TenantID        string          `json:"tenant_id"`
	HubDiscussionID string          `json:"hub_discussion_id"`
	HubMessageID    string          `json:"hub_message_id"`
	RequestID       string          `json:"request_id"`
	Content         string          `json:"content"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

func (s *HTTPServer) handlePlatformCreateVirtualEmployee(w http.ResponseWriter, r *http.Request) {
	var in platformVirtualEmployeeRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	employeeID := strings.TrimSpace(in.EmployeeID)
	if employeeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "employee_id is required"})
		return
	}
	avatarDataURL, err := normalizePlatformAvatarDataURL(in.AvatarDataURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid avatar_data_url", "detail": err.Error()})
		return
	}
	in.AvatarDataURL = avatarDataURL
	runtimeTenantKey := firstPlatformNonEmpty(in.TenantID, in.PlatformTenantID, "default")
	tenant, err := s.findOrCreatePlatformTenant(r, runtimeTenantKey, in)
	if err != nil {
		s.writePlatformVirtualEmployeeStageError(w, "find_or_create_tenant", in, err)
		return
	}
	user, err := s.findOrCreatePlatformUser(r, tenant.ID, in)
	if err != nil {
		s.writePlatformVirtualEmployeeStageError(w, "find_or_create_user", in, err)
		return
	}
	principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
	llmURL := firstPlatformNonEmpty(in.HubLLMEndpoint, "http://127.0.0.1/managed-by-hub")
	llmModel := platformLLMModelFromRequest(in)
	llmKey := firstPlatformNonEmpty(platformLLMCredential(in), "managed-by-hub")
	if err := s.updatePlatformUserLLMConfig(r, principal, llmURL, llmKey, llmModel); err != nil {
		s.writePlatformVirtualEmployeeStageError(w, "update_llm_config", in, err)
		return
	}
	if err := s.updatePlatformUserSSHHosts(r, principal, in.SSHHosts); err != nil {
		s.writePlatformVirtualEmployeeStageError(w, "update_ssh_hosts", in, err)
		return
	}
	if !in.MaclawSrvConfig.isZero() {
		if err := s.updatePlatformUserMaclawSrvConfig(r, principal, in.MaclawSrvConfig); err != nil {
			s.writePlatformVirtualEmployeeStageError(w, "update_maclawsrv_config", in, err)
			return
		}
	}
	inst, created, err := s.findOrCreatePlatformInstance(r, principal, in)
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			if s.writePlatformInvalidConfig(w, r, principal) {
				return
			}
		}
		s.writePlatformVirtualEmployeeStageError(w, "find_or_create_instance", in, err)
		return
	}
	status := "ready"
	if inst.Readiness.ConfigValid == false || !inst.Readiness.Ready {
		status = "attention"
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": status, "created": created, "tenant_id": tenant.ID, "user_id": user.ID, "instance_id": inst.ID, "employee_id": employeeID, "readiness": inst.Readiness})
}

func (s *HTTPServer) writePlatformVirtualEmployeeStageError(w http.ResponseWriter, stage string, in platformVirtualEmployeeRequest, err error) {
	status := errorStatusCode(err)
	redacted := redactSupportBundleText(s.svc.DataRoot(), err.Error())
	log.Printf("[platform-runtime] virtual employee create failed stage=%s employee=%s hub_tenant=%s platform_tenant=%s status=%d err=%s", stage, platformRuntimeLogID(in.EmployeeID), strings.TrimSpace(in.TenantID), strings.TrimSpace(in.PlatformTenantID), status, redacted)
	writeJSON(w, status, map[string]any{
		"error":              redacted,
		"stage":              stage,
		"employee_id":        strings.TrimSpace(in.EmployeeID),
		"tenant_id":          strings.TrimSpace(in.TenantID),
		"platform_tenant_id": strings.TrimSpace(in.PlatformTenantID),
	})
}

func (s *HTTPServer) handleRuntimeVirtualEmployeeDiscussionMessage(w http.ResponseWriter, r *http.Request) {
	s.handleVirtualEmployeeMessage(w, r, "hub_runtime_a2a", "Hub Discussion")
}

func (s *HTTPServer) handleVirtualEmployeeMessage(w http.ResponseWriter, r *http.Request, source, title string) {
	started := time.Now()
	employeeID := strings.TrimSpace(r.PathValue("employeeId"))
	if employeeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "employeeId is required"})
		return
	}
	logEmployeeID := platformRuntimeLogID(employeeID)
	log.Printf("[platform-runtime] discussion message received employee=%s hub_tenant=%s source=%s", logEmployeeID, platformRuntimeRequestHubTenantID(r), source)
	var in platformVirtualEmployeeMessageRequest
	if !decodePlatformJSON(w, r, &in) {
		log.Printf("[platform-runtime] discussion message invalid json employee=%s duration=%s", logEmployeeID, time.Since(started))
		return
	}
	if err := normalizePlatformVirtualEmployeeMessageRequest(&in); err != nil {
		log.Printf("[platform-runtime] discussion message invalid payload employee=%s err=%v duration=%s", logEmployeeID, err, time.Since(started))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload", "detail": err.Error()})
		return
	}
	content := strings.TrimSpace(in.Content)
	attachments := platformMessageAttachmentsFromPayload(in.Payload)
	attachmentCount := platformMessageAttachmentCount(attachments)
	if content == "" && attachmentCount == 0 {
		log.Printf("[platform-runtime] discussion message empty content employee=%s request_id=%s hub_discussion=%s hub_message=%s duration=%s", logEmployeeID, in.RequestID, in.HubDiscussionID, in.HubMessageID, time.Since(started))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}
	if content == "" && attachmentCount > 0 {
		content = "Please inspect the attached file(s)."
	}
	if platformRuntimeRequestHubTenantID(r) == "" && strings.TrimSpace(in.TenantID) != "" {
		r = cloneRequestWithHeader(r, "X-VE-Hub-Tenant-ID", strings.TrimSpace(in.TenantID))
	}
	binding, ok, err := s.findPlatformMessageRuntimeBinding(r, employeeID)
	if err != nil {
		log.Printf("[platform-runtime] discussion binding lookup failed employee=%s request_id=%s hub_discussion=%s hub_message=%s err=%v duration=%s", logEmployeeID, in.RequestID, in.HubDiscussionID, in.HubMessageID, err, time.Since(started))
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !ok {
		log.Printf("[platform-runtime] discussion binding not found employee=%s request_id=%s hub_discussion=%s hub_message=%s hub_tenant=%s duration=%s", logEmployeeID, in.RequestID, in.HubDiscussionID, in.HubMessageID, platformRuntimeRequestHubTenantID(r), time.Since(started))
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "platform virtual employee runtime not found"})
		return
	}
	metadata := map[string]string{"source": strings.TrimSpace(source)}
	if value := strings.TrimSpace(in.RequestID); value != "" {
		metadata["ve_a2a_request_id"] = value
	}
	if value := strings.TrimSpace(in.HubDiscussionID); value != "" {
		metadata["ve_hub_discussion_id"] = value
	}
	if value := strings.TrimSpace(in.HubMessageID); value != "" {
		metadata["ve_hub_message_id"] = value
	}
	content = s.enrichPlatformMessageContentWithAttachments(r, binding, in, content, attachments, metadata)
	log.Printf("[platform-runtime] discussion send start employee=%s tenant=%s user=%s instance=%s request_id=%s hub_discussion=%s hub_message=%s content_chars=%d", logEmployeeID, binding.Tenant.ID, binding.User.ID, binding.Instance.ID, in.RequestID, in.HubDiscussionID, in.HubMessageID, len([]rune(content)))

	// Check if Hub supports streaming (indicated by Accept: text/event-stream header).
	wantsSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	log.Printf("[VE-STREAMING] ===== STAGE 1: maclawsrv received request ===== employee=%s wantsSSE=%v accept_header=%q", logEmployeeID, wantsSSE, r.Header.Get("Accept"))

	// Wire streaming token callback into the per-request SendMessageInput.
	var sseWriter *platformSSEWriter
	var onToken func(string)
	if wantsSSE {
		sseWriter = newPlatformSSEWriter(w)
		sseWriter.WriteHeader()
		log.Printf("[VE-STREAMING] ===== STAGE 2: SSE headers written, streaming mode active =====")
		tokenCount := 0
		onToken = func(delta string) {
			tokenCount++
			if tokenCount <= 3 || tokenCount%50 == 0 {
				log.Printf("[VE-STREAMING] STAGE 3: token delta #%d len=%d", tokenCount, len(delta))
			}
			sseWriter.WriteChunk(delta)
		}
	} else {
		log.Printf("[VE-STREAMING] ===== NOT STREAMING: Hub did not send Accept: text/event-stream =====")
	}

	sess, run, msg, err := s.svc.SendMessage(r.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}, binding.Instance.ID, agentservice.SendMessageInput{Title: strings.TrimSpace(title), Content: content, ClientSessionKey: strings.TrimSpace(in.HubDiscussionID), ClientMessageID: firstPlatformNonEmpty(in.HubMessageID, in.RequestID), Metadata: metadata, OnToken: onToken})
	if err != nil {
		log.Printf("[platform-runtime] discussion send failed employee=%s tenant=%s user=%s instance=%s request_id=%s hub_discussion=%s hub_message=%s run_id=%s duration=%s err=%v", logEmployeeID, binding.Tenant.ID, binding.User.ID, binding.Instance.ID, in.RequestID, in.HubDiscussionID, in.HubMessageID, platformRunID(run), time.Since(started), err)
		if sseWriter != nil {
			sseWriter.WriteError(err.Error())
			sseWriter.WriteDone("", nil, nil, nil)
			return
		}
		if run != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
			return
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	log.Printf("[platform-runtime] discussion send ok employee=%s tenant=%s user=%s instance=%s request_id=%s hub_discussion=%s hub_message=%s session=%s message=%s run_id=%s duration=%s", logEmployeeID, binding.Tenant.ID, binding.User.ID, binding.Instance.ID, in.RequestID, in.HubDiscussionID, in.HubMessageID, platformSessionID(sess), platformMessageID(msg), platformRunID(run), time.Since(started))
	if sseWriter != nil {
		sseWriter.WriteDone(msg.Content, sess, sanitizeRunPtrForAPI(s.svc.DataRoot(), run), msg)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sess, "run": sanitizeRunPtrForAPI(s.svc.DataRoot(), run), "message": msg, "employee_id": employeeID})
}

func platformRuntimeLogID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func platformSessionID(session *agentservice.Session) string {
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.ID)
}

func platformMessageID(message *agentservice.Message) string {
	if message == nil {
		return ""
	}
	return strings.TrimSpace(message.ID)
}

func platformRunID(run *agentservice.Run) string {
	if run == nil {
		return ""
	}
	return strings.TrimSpace(run.ID)
}

func normalizePlatformVirtualEmployeeMessageRequest(in *platformVirtualEmployeeMessageRequest) error {
	if in == nil || len(bytes.TrimSpace(in.Payload)) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(in.Payload, &payload); err != nil {
		return err
	}
	if strings.TrimSpace(in.RequestID) == "" {
		in.RequestID = platformEnvelopeStringField(payload, "request_id", "id")
	}
	if strings.TrimSpace(in.HubDiscussionID) == "" {
		in.HubDiscussionID = platformEnvelopeStringField(payload, "hub_discussion_id", "session_id")
	}
	if strings.TrimSpace(in.HubMessageID) == "" {
		in.HubMessageID = platformEnvelopeStringField(payload, "hub_message_id")
	}
	if strings.TrimSpace(in.Content) == "" {
		in.Content = platformEnvelopeStringField(payload, "content")
	}
	if env := platformEnvelopeField(payload, "envelope"); env != nil {
		if strings.TrimSpace(in.RequestID) == "" {
			in.RequestID = platformEnvelopeStringField(env, "id", "ID")
		}
		if strings.TrimSpace(in.HubDiscussionID) == "" {
			in.HubDiscussionID = platformEnvelopeStringField(env, "session_id", "SessionID")
		}
		if message := platformEnvelopeField(env, "message", "Message"); message != nil {
			if strings.TrimSpace(in.HubMessageID) == "" {
				in.HubMessageID = platformEnvelopeStringField(message, "id", "ID")
			}
			if strings.TrimSpace(in.Content) == "" {
				in.Content = platformEnvelopeStringField(message, "content", "Content")
			}
		}
	}
	return nil
}

type platformTextAttachment struct {
	Content   string `json:"content"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	LocalPath string `json:"local_path"`
}

type platformFileAttachment struct {
	FileURL   string `json:"file_url"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	LocalPath string `json:"local_path"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

type platformMessageAttachments struct {
	Text  []platformTextAttachment `json:"text_attachments"`
	Image []platformFileAttachment `json:"image_attachments"`
	File  []platformFileAttachment `json:"file_attachments"`
}

func (s *HTTPServer) enrichPlatformMessageContentWithAttachments(r *http.Request, binding platformRuntimeBinding, in platformVirtualEmployeeMessageRequest, content string, attachments platformMessageAttachments, metadata map[string]string) string {
	attachments = limitPlatformMessageAttachments(attachments)
	count := platformMessageAttachmentCount(attachments)
	if count == 0 {
		return content
	}
	if metadata != nil {
		metadata["ve_attachment_count"] = fmt.Sprint(count)
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(content))
	b.WriteString("\n\n[Hub attachments received]\n")
	for _, att := range attachments.Text {
		name := agent.NormalizeBinaryDocumentAttachmentFilename(att.Filename, att.MimeType)
		name = safePlatformAttachmentFilename(name)
		kind := "text"
		if agent.IsBinaryDocumentAttachment(att.Filename, att.MimeType) {
			kind = "file"
		}
		line := fmt.Sprintf("- %s: %s", kind, firstPlatformNonEmpty(name, "attachment.txt"))
		if localPath, err := materializePlatformTextAttachment(binding.Instance.Workspace, in.HubDiscussionID, att); err == nil && localPath != "" {
			line += fmt.Sprintf("; local_path=%s", localPath)
		} else if err != nil {
			line += fmt.Sprintf("; unavailable=%s", err.Error())
		}
		b.WriteString(line + "\n")
	}
	for _, att := range attachments.Image {
		b.WriteString(s.platformFileAttachmentLine(r, binding, in.HubDiscussionID, "image", att) + "\n")
	}
	for _, att := range attachments.File {
		b.WriteString(s.platformFileAttachmentLine(r, binding, in.HubDiscussionID, "file", att) + "\n")
	}
	b.WriteString("Use local_path when present to inspect the attachment.\n")
	return b.String()
}

func platformMessageAttachmentCount(attachments platformMessageAttachments) int {
	return len(attachments.Text) + len(attachments.Image) + len(attachments.File)
}

func limitPlatformMessageAttachments(attachments platformMessageAttachments) platformMessageAttachments {
	remaining := platformAttachmentMaxCount
	if len(attachments.Text) > remaining {
		attachments.Text = attachments.Text[:remaining]
	}
	remaining -= len(attachments.Text)
	if remaining < 0 {
		remaining = 0
	}
	if len(attachments.Image) > remaining {
		attachments.Image = attachments.Image[:remaining]
	}
	remaining -= len(attachments.Image)
	if remaining < 0 {
		remaining = 0
	}
	if len(attachments.File) > remaining {
		attachments.File = attachments.File[:remaining]
	}
	return attachments
}

func platformMessageAttachmentsFromPayload(raw json.RawMessage) platformMessageAttachments {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return platformMessageAttachments{}
	}
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return platformMessageAttachments{}
	}
	var out platformMessageAttachments
	for _, candidate := range platformAttachmentCandidates(payload) {
		var got platformMessageAttachments
		data, err := json.Marshal(candidate)
		if err != nil || json.Unmarshal(data, &got) != nil {
			continue
		}
		out.Text = append(out.Text, got.Text...)
		out.Image = append(out.Image, got.Image...)
		out.File = append(out.File, got.File...)
	}
	return out
}

func platformAttachmentCandidates(v any) []any {
	candidates := []any{v}
	if env := platformEnvelopeField(v, "envelope", "Envelope"); env != nil {
		candidates = append(candidates, env)
		if message := platformEnvelopeField(env, "message", "Message"); message != nil {
			candidates = append(candidates, message)
		}
	}
	if message := platformEnvelopeField(v, "message", "Message"); message != nil {
		candidates = append(candidates, message)
	}
	return candidates
}

func materializePlatformTextAttachment(workspace, discussionID string, att platformTextAttachment) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" || strings.TrimSpace(att.Content) == "" {
		return "", nil
	}
	name := agent.NormalizeBinaryDocumentAttachmentFilename(att.Filename, att.MimeType)
	name = safePlatformAttachmentFilename(name)
	if name == "" {
		name = "attachment.txt"
	}
	data, err := decodePlatformTextAttachment(att.Content)
	if err != nil {
		return "", fmt.Errorf("invalid inline text attachment")
	}
	if int64(len(data)) > platformAttachmentMaxBytesFor(att.Filename, att.MimeType) {
		return "", fmt.Errorf("attachment too large")
	}
	dir := filepath.Join(workspace, ".hub-attachments", safePlatformAttachmentFilename(discussionID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := uniquePlatformAttachmentPath(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// platformAttachmentMaxBytesFor keeps downloaded and inline Hub documents on
// the same source-size boundary enforced by the shared read_document path.
// Other file attachments retain the product's broader relay limit. MIME and
// filename here determine only the staging limit; the actual PDF/Office
// parser still validates content signatures and containers before extraction.
func platformAttachmentMaxBytesFor(fileName, mimeType string) int64 {
	if agent.IsBinaryDocumentAttachment(fileName, mimeType) {
		return agent.MaxOfficeReadFileBytes
	}
	return platformAttachmentMaxBytes
}

func decodePlatformTextAttachment(content string) ([]byte, error) {
	content = strings.TrimSpace(content)
	var lastErr error
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		data, err := encoding.DecodeString(content)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *HTTPServer) platformFileAttachmentLine(r *http.Request, binding platformRuntimeBinding, discussionID, kind string, att platformFileAttachment) string {
	name := agent.NormalizeBinaryDocumentAttachmentFilename(att.Filename, att.MimeType)
	name = safePlatformAttachmentFilename(name)
	if name == "" {
		name = "attachment"
	}
	if agent.IsBinaryDocumentAttachment(att.Filename, att.MimeType) {
		kind = "file"
	}
	parts := []string{fmt.Sprintf("- %s: %s", kind, name)}
	if att.MimeType != "" {
		parts = append(parts, "mime_type="+strings.TrimSpace(att.MimeType))
	}
	if att.SizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("size_bytes=%d", att.SizeBytes))
	}
	if localPath, err := s.downloadPlatformFileAttachment(r, binding, discussionID, att); err == nil && localPath != "" {
		parts = append(parts, "local_path="+localPath)
	} else if strings.TrimSpace(att.FileURL) != "" {
		parts = append(parts, "file_url="+strings.TrimSpace(att.FileURL))
		if err != nil {
			parts = append(parts, "download_unavailable="+err.Error())
		}
	}
	return strings.Join(parts, "; ")
}

func (s *HTTPServer) downloadPlatformFileAttachment(r *http.Request, binding platformRuntimeBinding, discussionID string, att platformFileAttachment) (string, error) {
	if strings.TrimSpace(att.FileURL) == "" || strings.TrimSpace(binding.Instance.Workspace) == "" {
		return "", nil
	}
	cfg, err := s.svc.GetUserConfig(r.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID})
	if err != nil || cfg == nil {
		return "", err
	}
	participantID := firstPlatformNonEmpty(cfg.AppConfig.RemoteMachineID, r.Header.Get("X-VE-Hub-Employee-ID"))
	downloadURL, attachmentID, err := platformAttachmentDownloadURL(cfg.AppConfig.RemoteHubURL, att.FileURL, discussionID, participantID)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(r.Context(), platformAttachmentTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	if token := strings.TrimSpace(cfg.AppConfig.RemoteMachineToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if participantID != "" {
		req.Header.Set("X-Machine-ID", participantID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("download failed HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	name := agent.NormalizeBinaryDocumentAttachmentFilename(att.Filename, att.MimeType)
	name = safePlatformAttachmentFilename(name)
	if name == "" {
		name = "attachment"
	}
	if attachmentID != "" {
		name = safePlatformAttachmentFilename(attachmentID + "-" + name)
	}
	dir := filepath.Join(binding.Instance.Workspace, ".hub-attachments", safePlatformAttachmentFilename(discussionID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := uniquePlatformAttachmentPath(dir, name)
	tmp, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	maxBytes := platformAttachmentMaxBytesFor(att.Filename, att.MimeType)
	size, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, maxBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if size > maxBytes {
		return "", fmt.Errorf("attachment too large")
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func uniquePlatformAttachmentPath(dir, name string) string {
	name = safePlatformAttachmentFilename(name)
	if name == "" {
		name = "attachment"
	}
	candidate := filepath.Join(dir, name)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), ext))
}

func platformAttachmentDownloadURL(hubURL, rawURL, discussionID, participantID string) (string, string, error) {
	base := strings.TrimRight(strings.TrimSpace(hubURL), "/")
	if base == "" {
		return "", "", fmt.Errorf("remote_hub_url is empty")
	}
	if strings.HasPrefix(rawURL, "/") {
		rawURL = base + rawURL
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", "", err
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", err
	}
	if !strings.EqualFold(baseURL.Scheme, u.Scheme) || !strings.EqualFold(baseURL.Host, u.Host) {
		return "", "", fmt.Errorf("attachment file url must belong to configured Hub")
	}
	path := u.EscapedPath()
	var escapedID string
	switch {
	case strings.HasPrefix(path, "/api/ve/files/download/"):
		escapedID = strings.TrimPrefix(path, "/api/ve/files/download/")
	case strings.HasPrefix(path, "/api/ve/files/"):
		escapedID = strings.TrimPrefix(path, "/api/ve/files/")
	default:
		return "", "", fmt.Errorf("attachment file url must use Hub file relay")
	}
	if escapedID == "" || strings.Contains(escapedID, "/") {
		return "", "", fmt.Errorf("attachment file url must identify one file")
	}
	id, err := url.PathUnescape(escapedID)
	if err != nil {
		return "", "", err
	}
	id = safePlatformAttachmentFilename(id)
	u.Path = "/api/ve/files/download/" + url.PathEscape(id)
	u.RawPath = ""
	q := url.Values{}
	q.Set("session_id", discussionID)
	if strings.TrimSpace(participantID) != "" {
		q.Set("participant_id", strings.TrimSpace(participantID))
	}
	u.RawQuery = q.Encode()
	return u.String(), id, nil
}

func safePlatformAttachmentFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = filepath.Base(value)
	if value == "." || value == string(filepath.Separator) {
		return ""
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ' ' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "." || out == ".." {
		return "_" + out
	}
	return truncatePlatformAttachmentFilename(out)
}

func truncatePlatformAttachmentFilename(name string) string {
	if len(name) <= platformAttachmentNameMaxLen {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if len(ext) > 16 {
		ext = ""
		base = name
	}
	limit := platformAttachmentNameMaxLen - len(ext)
	if limit < 1 {
		limit = platformAttachmentNameMaxLen
		ext = ""
	}
	if len(base) > limit {
		base = base[:limit]
	}
	return strings.TrimSpace(base + ext)
}

func platformEnvelopeStringField(v any, names ...string) string {
	value := platformEnvelopeField(v, names...)
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func platformEnvelopeField(v any, names ...string) any {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	for _, name := range names {
		if value, ok := m[name]; ok {
			return value
		}
		if value, ok := m[strings.ToLower(name)]; ok {
			return value
		}
	}
	return nil
}

func platformLLMModelFromRequest(in platformVirtualEmployeeRequest) string {
	if model := strings.TrimSpace(in.LLMModel); model != "" {
		return model
	}
	if strings.TrimSpace(in.HubLLMEndpoint) != "" || strings.TrimSpace(in.LLMServiceGroupID) != "" {
		return platformHubLLMModel
	}
	return firstPlatformNonEmpty(in.DefaultLLM, platformHubLLMModel)
}

func platformLLMCredential(in platformVirtualEmployeeRequest) string {
	return firstPlatformNonEmpty(in.HubLLMViewerToken, in.ViewerToken, in.AccessToken, in.HubLLMAPIKey)
}

func normalizePlatformAvatarDataURL(value string) (string, error) {
	avatarDataURL := strings.TrimSpace(value)
	if avatarDataURL == "" {
		return "", nil
	}
	if len(avatarDataURL) > platformAvatarDataURLMaxSize {
		return "", fmt.Errorf("avatar image is too large")
	}
	valid, tooLarge := validatePlatformAvatarDataURL(avatarDataURL)
	if tooLarge {
		return "", fmt.Errorf("avatar image is too large")
	}
	if !valid {
		return "", fmt.Errorf("avatar image must be a PNG, JPEG, or WebP data URL")
	}
	return avatarDataURL, nil
}

func isValidPlatformAvatarDataURL(value string) bool {
	valid, _ := validatePlatformAvatarDataURL(value)
	return valid
}

func validatePlatformAvatarDataURL(value string) (bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(value))
	declaredType := ""
	switch {
	case strings.HasPrefix(lower, "data:image/png;base64,"):
		declaredType = "image/png"
	case strings.HasPrefix(lower, "data:image/jpeg;base64,"), strings.HasPrefix(lower, "data:image/jpg;base64,"):
		declaredType = "image/jpeg"
	case strings.HasPrefix(lower, "data:image/webp;base64,"):
		declaredType = "image/webp"
	default:
		return false, false
	}
	comma := strings.Index(value, ",")
	if comma < 0 || comma == len(value)-1 {
		return false, false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(value[comma+1:])
	if err != nil {
		return false, false
	}
	if len(decoded) > platformAvatarImageMaxBytes {
		return false, true
	}
	return http.DetectContentType(decoded) == declaredType, false
}

func (s *HTTPServer) handlePlatformUpdateVirtualEmployeeConfig(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(r.PathValue("employeeId"))
	if employeeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "employeeId is required"})
		return
	}
	var in struct {
		TenantID          string                  `json:"tenant_id"`
		PlatformTenantID  string                  `json:"platform_tenant_id"`
		VirtualEmail      string                  `json:"virtual_email"`
		AvatarDataURL     *string                 `json:"avatar_data_url"`
		DefaultLLM        string                  `json:"default_llm"`
		LLMServiceGroupID string                  `json:"llm_service_group_id"`
		HubLLMEndpoint    string                  `json:"hub_llm_endpoint"`
		HubLLMAPIKey      string                  `json:"hub_llm_api_key"`
		LLMModel          string                  `json:"llm_model"`
		HubLLMViewerToken string                  `json:"hub_llm_viewer_token"`
		ViewerToken       string                  `json:"viewer_token"`
		AccessToken       string                  `json:"access_token"`
		MaclawSrvConfig   platformMaclawSrvConfig `json:"maclawsrv_config"`
	}
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	var avatarDataURL string
	if in.AvatarDataURL != nil {
		var err error
		avatarDataURL, err = normalizePlatformAvatarDataURL(*in.AvatarDataURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid avatar_data_url", "detail": err.Error()})
			return
		}
	}
	if platformRuntimeRequestHubTenantID(r) == "" && strings.TrimSpace(in.TenantID) != "" {
		r = cloneRequestWithHeader(r, "X-VE-Hub-Tenant-ID", strings.TrimSpace(in.TenantID))
	}
	binding, ok, err := s.findPlatformRuntimeBinding(r, employeeID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !ok {
		binding, ok, err = s.findPlatformRuntimeUserBindingFromDeletePayload(r, employeeID, platformVirtualEmployeeDeletePayload{TenantID: in.TenantID, PlatformTenantID: in.PlatformTenantID, VirtualEmail: in.VirtualEmail})
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "virtual employee runtime not found"})
		return
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	llmURL := strings.TrimSpace(in.HubLLMEndpoint)
	llmKey := firstPlatformNonEmpty(in.HubLLMViewerToken, in.ViewerToken, in.AccessToken, in.HubLLMAPIKey)
	llmGroupID := platformLLMServiceGroupID(in.LLMServiceGroupID, in.DefaultLLM)
	llmModel := strings.TrimSpace(in.LLMModel)
	if llmModel == "" && (llmURL != "" || llmGroupID != "") {
		llmModel = platformHubLLMModel
	}
	if llmKey != "" {
		if llmURL == "" || llmModel == "" {
			cfg, err := s.svc.GetUserConfig(r.Context(), principal)
			if err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
			llmURL = firstPlatformNonEmpty(llmURL, cfg.AppConfig.MaclawLLMUrl)
			llmModel = firstPlatformNonEmpty(llmModel, cfg.AppConfig.MaclawLLMModel)
		}
		if err := s.updatePlatformUserLLMConfig(r, principal, firstPlatformNonEmpty(llmURL, "http://127.0.0.1/managed-by-hub"), llmKey, firstPlatformNonEmpty(llmModel, platformHubLLMModel)); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	if err := s.updatePlatformUserMaclawSrvConfig(r, principal, in.MaclawSrvConfig); err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	metadataPatch := map[string]string{}
	if in.AvatarDataURL != nil {
		metadataPatch["ve_avatar_data_url"] = avatarDataURL
	}
	if llmGroupID != "" {
		metadataPatch["llm_service_group_id"] = llmGroupID
	}
	if len(metadataPatch) > 0 {
		metadata := mergePlatformInstanceMetadata(binding.Instance.Metadata, metadataPatch)
		if !stringMapEqual(binding.Instance.Metadata, metadata) {
			if _, err := s.svc.UpdateInstance(r.Context(), principal, binding.Instance.ID, agentservice.UpdateInstanceInput{Metadata: metadata}); err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "employee_id": employeeID, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID})
}

func (s *HTTPServer) updatePlatformUserLLMConfig(r *http.Request, p agentservice.Principal, llmURL, llmKey, llmModel string) error {
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := platformLLMAppConfig(cfg.AppConfig, llmURL, llmKey, llmModel)
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func platformLLMAppConfig(app corelib.AppConfig, llmURL, llmKey, llmModel string) corelib.AppConfig {
	llmURL = strings.TrimSpace(llmURL)
	llmKey = strings.TrimSpace(llmKey)
	llmModel = strings.TrimSpace(llmModel)
	app.MaclawLLMUrl = llmURL
	app.MaclawLLMKey = llmKey
	app.MaclawLLMModel = llmModel
	app.MaclawLLMCurrentProvider = platformHubLLMProviderName
	app.MaclawLLMProviders = []corelib.MaclawLLMProvider{{
		Name:  platformHubLLMProviderName,
		URL:   llmURL,
		Key:   llmKey,
		Model: llmModel,
	}}
	return app
}

func platformSourceUserLLMModelFromRequest(in platformSourceUserRequest) string {
	if model := strings.TrimSpace(in.LLMModel); model != "" {
		return model
	}
	if strings.TrimSpace(in.HubLLMEndpoint) != "" || strings.TrimSpace(in.LLMServiceGroupID) != "" {
		return platformHubLLMModel
	}
	return firstPlatformNonEmpty(in.DefaultLLM, platformHubLLMModel)
}

func platformSourceUserLLMCredential(in platformSourceUserRequest) string {
	return firstPlatformNonEmpty(in.HubLLMViewerToken, in.ViewerToken, in.AccessToken, in.HubLLMAPIKey)
}

func (s *HTTPServer) handlePlatformSourceUserAssistantInstances(w http.ResponseWriter, r *http.Request) {
	binding, found, ok := s.requireExistingPlatformSourceUserBinding(w, r, platformSourceUserRequest{TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id"))})
	if !ok {
		return
	}
	if !found {
		item := platformSourceUserNotProvisionedStatus(strings.TrimSpace(r.URL.Query().Get("tenant_id")), strings.TrimSpace(r.PathValue("sourceUserId")))
		item["items"] = []agentservice.Instance{}
		writeJSON(w, http.StatusOK, item)
		return
	}
	instances, err := s.platformSourceUserInstances(r, binding)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": instances, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID})
}

func (s *HTTPServer) handlePlatformSourceUserRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	binding, found, ok := s.requireExistingPlatformSourceUserBinding(w, r, platformSourceUserRequest{TenantID: strings.TrimSpace(r.URL.Query().Get("tenant_id"))})
	if !ok {
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, platformSourceUserNotProvisionedStatus(strings.TrimSpace(r.URL.Query().Get("tenant_id")), strings.TrimSpace(r.PathValue("sourceUserId"))))
		return
	}
	item, err := s.platformSourceUserRuntimeStatus(r, binding)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handlePlatformSourceUsersRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.TenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id is required"})
		return
	}
	items := make([]map[string]any, 0, len(in.SourceUsers))
	for _, source := range in.SourceUsers {
		if strings.TrimSpace(source.ID) == "" {
			continue
		}
		binding, found, err := s.platformSourceUserBindingFromRequest(r, platformSourceUserRequest{TenantID: in.TenantID, SourceUser: source}, false)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if !found {
			items = append(items, platformSourceUserNotProvisionedStatus(in.TenantID, source.ID))
			continue
		}
		item, err := s.platformSourceUserRuntimeStatus(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "tenant_id": in.TenantID, "items": items})
}

func (s *HTTPServer) platformSourceUserRuntimeStatus(r *http.Request, binding platformSourceUserBinding) (map[string]any, error) {
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instances, err := s.platformSourceUserInstances(r, binding)
	if err != nil {
		return nil, err
	}
	ready := 0
	var latest time.Time
	for _, inst := range instances {
		if inst.Ready {
			ready++
		}
		latest = maxPlatformTime(latest, inst.UpdatedAt)
	}
	validation, err := s.svc.ValidateUserConfig(r.Context(), principal)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":           "ready",
		"tenant_id":        binding.Tenant.ID,
		"user_id":          binding.User.ID,
		"source_user_id":   binding.Source.ID,
		"instance_count":   len(instances),
		"ready_instances":  ready,
		"latest_active_at": latest,
		"config_status":    validation,
	}, nil
}

func (s *HTTPServer) handlePlatformCreateSourceUserAssistantInstance(w http.ResponseWriter, r *http.Request) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	binding, ok := s.requirePlatformSourceUserBinding(w, r, in)
	if !ok {
		return
	}
	inst, err := s.svc.CreateInstance(r.Context(), agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(in.Name, binding.Source.DisplayName, binding.Source.Email, binding.Source.ExternalID, binding.Source.ID), Description: strings.TrimSpace(in.Description), Metadata: platformSourceUserInstanceMetadata(in.TenantID, binding.Source)})
	if err != nil {
		if errors.Is(err, agentservice.ErrInvalidConfig) {
			if s.writePlatformInvalidConfig(w, r, agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}) {
				return
			}
		}
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"instance": inst, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID})
}

func (s *HTTPServer) handlePlatformSourceUserAssistantLink(w http.ResponseWriter, r *http.Request) {
	s.handlePlatformSourceUserLink(w, r, "assistant")
}

func (s *HTTPServer) handlePlatformSourceUserKnowledgeLink(w http.ResponseWriter, r *http.Request) {
	s.handlePlatformSourceUserLink(w, r, "knowledge")
}

func (s *HTTPServer) handlePlatformSourceUserSettingsLink(w http.ResponseWriter, r *http.Request) {
	s.handlePlatformSourceUserLink(w, r, "settings")
}

func (s *HTTPServer) handlePlatformSourceUserLink(w http.ResponseWriter, r *http.Request, view string) {
	var in platformSourceUserRequest
	if !decodePlatformJSON(w, r, &in) {
		return
	}
	binding, ok := s.requirePlatformSourceUserBinding(w, r, in)
	if !ok {
		return
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instanceID := strings.TrimSpace(in.InstanceID)
	createdInstance := false
	if view == "assistant" && instanceID != "" {
		instances, err := s.platformSourceUserInstances(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if !platformInstanceIDExists(instances, instanceID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "assistant instance not found"})
			return
		}
	}
	if view == "assistant" && instanceID == "" {
		instances, err := s.platformSourceUserInstances(r, binding)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		if len(instances) > 0 {
			instanceID = instances[0].ID
		} else {
			inst, err := s.svc.CreateInstance(r.Context(), principal, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(binding.Source.DisplayName, binding.Source.Email, binding.Source.ExternalID, binding.Source.ID), Metadata: platformSourceUserInstanceMetadata(in.TenantID, binding.Source)})
			if err != nil {
				if errors.Is(err, agentservice.ErrInvalidConfig) {
					if s.writePlatformInvalidConfig(w, r, principal) {
						return
					}
				}
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
			instanceID = inst.ID
			createdInstance = true
		}
	}
	credExp := time.Now().UTC().Add(15 * time.Minute)
	cred, err := s.svc.CreateCredential(r.Context(), agentservice.CreateCredentialInput{TenantID: binding.Tenant.ID, UserID: binding.User.ID, Name: "VE Platform web launch", ExpiresAt: &credExp})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	tok, err := s.svc.IssueToken(r.Context(), agentservice.IssueTokenInput{APIKey: cred.APIKey, APISecret: cred.APISecret})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	launchMeta := webLaunchTokenRecord{TenantID: binding.Tenant.ID, UserID: binding.User.ID, SourceUserID: binding.Source.ID, InstanceID: instanceID, View: view}
	launchToken, launchTokenExpiresAt, launchTokenHash, err := s.newWebLaunchToken(tok.AccessToken, tok.ExpiresAt, time.Now().UTC(), launchMeta)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	_ = s.recordAdminAudit(r.Context(), "web.launch_token.created", "web_launch_token", binding.Source.ID, map[string]string{"tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID, "instance_id": instanceID, "view": view, "launch_token_hash_prefix": shortWebLaunchTokenHash(launchTokenHash), "remote_ip": requestClientIP(r)})
	launchURL := platformWebLaunchURL(r, launchToken, view, instanceID, strings.TrimSpace(in.SettingsTab), binding)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"url": launchURL, "launch_url": launchURL, "view": view, "expires_at": launchTokenExpiresAt, "access_expires_at": tok.ExpiresAt, "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "source_user_id": binding.Source.ID, "instance_id": instanceID, "created_instance": createdInstance})
}

func (s *HTTPServer) writePlatformInvalidConfig(w http.ResponseWriter, r *http.Request, principal agentservice.Principal) bool {
	validation, err := s.svc.ValidateUserConfig(r.Context(), principal)
	if err != nil || validation == nil {
		return false
	}
	safe := sanitizeConfigValidationForAPI(s.svc.DataRoot(), *validation)
	writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid config", "config_validation": safe, "issues": safe.Issues})
	return true
}

func platformInstanceIDExists(instances []agentservice.Instance, id string) bool {
	id = strings.TrimSpace(id)
	for _, inst := range instances {
		if inst.ID == id {
			return true
		}
	}
	return false
}

func (s *HTTPServer) platformSourceUserInstances(r *http.Request, binding platformSourceUserBinding) ([]agentservice.Instance, error) {
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instances, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		return nil, err
	}
	filtered := make([]agentservice.Instance, 0, len(instances))
	for _, inst := range instances {
		if platformSourceUserInstanceMatches(binding.Source, inst) {
			filtered = append(filtered, inst)
		}
	}
	return filtered, nil
}

func platformSourceUserNotProvisionedStatus(tenantID, sourceUserID string) map[string]any {
	return map[string]any{
		"status":          "not_provisioned",
		"tenant_id":       strings.TrimSpace(tenantID),
		"source_user_id":  strings.TrimSpace(sourceUserID),
		"instance_count":  0,
		"ready_instances": 0,
		"config_status": map[string]any{
			"valid":   false,
			"reason":  "not_provisioned",
			"message": "source user runtime has not been provisioned",
		},
	}
}

func platformSourceUserInstanceMatches(source platformSourceUser, inst agentservice.Instance) bool {
	sourceID := strings.TrimSpace(source.ID)
	if sourceID == "" {
		return false
	}
	if platformSourceUserIsVirtualEmployee(source) && strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_employee_id"]), sourceID) {
		return true
	}
	if strings.TrimSpace(inst.Metadata["ve_source_user_id"]) == "" && strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_employee_id"]), sourceID) {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_source_user_id"]), sourceID)
}

func (s *HTTPServer) handlePlatformDeleteVirtualEmployee(w http.ResponseWriter, r *http.Request) {
	employeeID := strings.TrimSpace(r.PathValue("employeeId"))
	deletePayload, err := readPlatformVirtualEmployeeDeletePayload(r)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if platformRuntimeRequestHubTenantID(r) == "" && strings.TrimSpace(deletePayload.TenantID) != "" {
		r = cloneRequestWithHeader(r, "X-VE-Hub-Tenant-ID", strings.TrimSpace(deletePayload.TenantID))
	}
	binding, ok, err := s.findPlatformRuntimeBinding(r, employeeID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	if !ok {
		binding, ok, err = s.findPlatformRuntimeUserBindingFromDeletePayload(r, employeeID, deletePayload)
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "virtual employee runtime not found"})
		return
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	userID := binding.User.ID
	instances, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	managedUser := platformManagedUser(binding.User)
	deletedInstanceIDs := make([]string, 0, len(instances))
	for _, inst := range instances {
		if !managedUser && !strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_employee_id"]), employeeID) && !strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_source_user_id"]), employeeID) {
			continue
		}
		if err := s.svc.DeleteInstance(r.Context(), principal, inst.ID); err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		deletedInstanceIDs = append(deletedInstanceIDs, inst.ID)
	}
	remaining, err := s.svc.ListInstances(r.Context(), principal)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	userDeleted := false
	userDeleteWarning := ""
	if len(remaining) == 0 && (managedUser || len(deletedInstanceIDs) > 0) {
		unprotected := false
		if managedUser {
			if _, err := s.svc.UpdateUser(r.Context(), binding.Tenant.ID, userID, agentservice.UpdateUserInput{DeleteProtected: &unprotected}); err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
		}
		if err := s.svc.DeleteUser(r.Context(), binding.Tenant.ID, userID); err != nil {
			if managedUser {
				protected := true
				reason := binding.User.DeleteProtectionReason
				_, _ = s.svc.UpdateUser(r.Context(), binding.Tenant.ID, userID, agentservice.UpdateUserInput{DeleteProtected: &protected, DeleteProtectionReason: &reason})
			}
			userDeleteWarning = err.Error()
		} else {
			userDeleted = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "employee_id": employeeID, "tenant_id": binding.Tenant.ID, "user_id": userID, "instance_id": binding.Instance.ID, "deleted_instance_ids": deletedInstanceIDs, "deleted_instances": len(deletedInstanceIDs), "user_deleted": userDeleted, "remaining_instances": len(remaining), "user_delete_warning": userDeleteWarning})
}

type platformVirtualEmployeeDeletePayload struct {
	TenantID         string `json:"tenant_id"`
	PlatformTenantID string `json:"platform_tenant_id"`
	VirtualEmail     string `json:"virtual_email"`
	HubAccountID     string `json:"hub_account_id"`
}

func readPlatformVirtualEmployeeDeletePayload(r *http.Request) (platformVirtualEmployeeDeletePayload, error) {
	var payload platformVirtualEmployeeDeletePayload
	if r == nil || r.Body == nil {
		return payload, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return payload, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}

func platformManagedUser(user agentservice.User) bool {
	return user.DeleteProtected && strings.EqualFold(strings.TrimSpace(user.DeleteProtectionReason), "Managed by VE Platform")
}

func (s *HTTPServer) findPlatformRuntimeUserBindingFromDeletePayload(r *http.Request, employeeID string, payload platformVirtualEmployeeDeletePayload) (platformRuntimeBinding, bool, error) {
	email := strings.ToLower(strings.TrimSpace(payload.VirtualEmail))
	hubAccountID := strings.TrimSpace(payload.HubAccountID)
	if email == "" && hubAccountID == "" {
		return platformRuntimeBinding{}, false, nil
	}
	tenantKeys := map[string]bool{}
	for _, key := range []string{payload.TenantID, payload.PlatformTenantID} {
		if key = strings.TrimSpace(key); key != "" {
			tenantKeys[strings.ToLower(key)] = true
		}
	}
	targetTenantScoped := len(tenantKeys) > 0
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return platformRuntimeBinding{}, false, err
	}
	for _, tenant := range tenants {
		if targetTenantScoped && !tenantKeys[strings.ToLower(strings.TrimSpace(tenant.ID))] && !strings.Contains(strings.ToLower(tenant.Name), strings.ToLower(payload.TenantID)) && !strings.Contains(strings.ToLower(tenant.Name), strings.ToLower(payload.PlatformTenantID)) {
			continue
		}
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			return platformRuntimeBinding{}, false, err
		}
		for _, user := range users {
			if !platformManagedUser(user) {
				continue
			}
			if email != "" && !strings.EqualFold(strings.TrimSpace(user.Email), email) {
				continue
			}
			if hubAccountID != "" && !strings.EqualFold(strings.TrimSpace(user.ID), hubAccountID) {
				continue
			}
			return platformRuntimeBinding{Tenant: tenant, User: user}, true, nil
		}
	}
	_ = employeeID
	return platformRuntimeBinding{}, false, nil
}

func (s *HTTPServer) handlePlatformRuntimeReport(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	hubTenantID := platformRuntimeRequestHubTenantID(r)
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return
	}
	usersOut := make([]map[string]any, 0)
	instancesOut := make([]map[string]any, 0)
	readyUsers := 0
	for _, tenant := range tenants {
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			writeRedactedError(w, err, s.svc.DataRoot())
			return
		}
		for _, user := range users {
			principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
			instances, err := s.svc.ListInstances(r.Context(), principal)
			if err != nil {
				writeRedactedError(w, err, s.svc.DataRoot())
				return
			}
			for _, inst := range instances {
				employeeID := strings.TrimSpace(inst.Metadata["ve_employee_id"])
				if employeeID == "" {
					continue
				}
				if !platformRuntimeReportIncludesInstance(inst, hubTenantID) {
					continue
				}
				status := platformRuntimeStatusFor(tenant, user, inst)
				if status == "ready" {
					readyUsers++
				}
				usersOut = append(usersOut, map[string]any{"employee_id": employeeID, "runtime_user_id": user.ID, "name": firstPlatformNonEmpty(inst.Name, user.Name), "virtual_email": user.Email, "runtime_status": status, "updated_at": maxPlatformTime(user.UpdatedAt, inst.UpdatedAt)})
				instancesOut = append(instancesOut, map[string]any{"instance_id": inst.ID, "employee_id": employeeID, "runtime_user_id": user.ID, "name": inst.Name, "status": inst.Status, "ready": inst.Ready, "ready_reason": inst.ReadyReason, "updated_at": inst.UpdatedAt})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "generated_at": now, "service": map[string]any{"status": "ok", "message": "MaClawSrv platform runtime report ready"}, "summary": map[string]any{"active_users": len(usersOut), "ready_users": readyUsers, "error_users": 0, "runtime_errors": 0}, "users": usersOut, "instances": instancesOut, "errors": []any{}})
}

func platformRuntimeReportIncludesInstance(inst agentservice.Instance, hubTenantID string) bool {
	hubTenantID = strings.TrimSpace(hubTenantID)
	if hubTenantID == "" {
		return true
	}
	instHubTenantID := strings.TrimSpace(inst.Metadata["ve_hub_tenant_id"])
	if instHubTenantID == "" {
		return true
	}
	return strings.EqualFold(instHubTenantID, hubTenantID)
}

func platformRuntimeStatusFor(tenant agentservice.Tenant, user agentservice.User, inst agentservice.Instance) string {
	if tenant.Status != agentservice.TenantStatusActive || user.Status != agentservice.UserStatusActive {
		return "attention"
	}
	if !inst.Ready {
		return "attention"
	}
	if strings.TrimSpace(string(inst.Status)) == "" {
		return "attention"
	}
	if inst.Status == agentservice.InstanceStatusReady {
		return "ready"
	}
	return "attention"
}

func (s *HTTPServer) requirePlatformSourceUserBinding(w http.ResponseWriter, r *http.Request, in platformSourceUserRequest) (platformSourceUserBinding, bool) {
	binding, _, ok := s.requirePlatformSourceUserBindingWithCreate(w, r, in, true)
	return binding, ok
}

func (s *HTTPServer) requireExistingPlatformSourceUserBinding(w http.ResponseWriter, r *http.Request, in platformSourceUserRequest) (platformSourceUserBinding, bool, bool) {
	return s.requirePlatformSourceUserBindingWithCreate(w, r, in, false)
}

func (s *HTTPServer) requirePlatformSourceUserBindingWithCreate(w http.ResponseWriter, r *http.Request, in platformSourceUserRequest, create bool) (platformSourceUserBinding, bool, bool) {
	sourceID := strings.TrimSpace(r.PathValue("sourceUserId"))
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source user id is required"})
		return platformSourceUserBinding{}, false, false
	}
	if strings.TrimSpace(in.TenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id is required"})
		return platformSourceUserBinding{}, false, false
	}
	in.SourceUser.ID = strings.TrimSpace(in.SourceUser.ID)
	if in.SourceUser.ID == "" {
		in.SourceUser.ID = sourceID
	}
	if !strings.EqualFold(in.SourceUser.ID, sourceID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source user id mismatch"})
		return platformSourceUserBinding{}, false, false
	}
	binding, found, err := s.platformSourceUserBindingFromRequest(r, in, create)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return platformSourceUserBinding{}, false, false
	}
	return binding, found, true
}

func (s *HTTPServer) platformSourceUserBindingFromRequest(r *http.Request, in platformSourceUserRequest, create bool) (platformSourceUserBinding, bool, error) {
	tenant, user, found, err := s.findPlatformSourceUser(r, in, create)
	if err != nil {
		return platformSourceUserBinding{}, false, err
	}
	if !found {
		return platformSourceUserBinding{}, false, nil
	}
	if create {
		if err := s.updatePlatformSourceUserConfig(r, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}, in); err != nil {
			return platformSourceUserBinding{}, false, err
		}
	}
	binding := platformSourceUserBinding{Tenant: *tenant, User: *user, Source: in.SourceUser}
	if create {
		if err := s.syncPlatformSourceUserInstanceMetadata(r, binding, in.TenantID); err != nil {
			return platformSourceUserBinding{}, false, err
		}
	}
	return binding, true, nil
}

func (s *HTTPServer) syncPlatformSourceUserInstanceMetadata(r *http.Request, binding platformSourceUserBinding, platformTenantID string) error {
	metadata := platformSourceUserSyncInstanceMetadata(platformTenantID, binding.Source)
	if len(metadata) == 0 {
		return nil
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	instances, err := s.platformSourceUserInstances(r, binding)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		merged := mergePlatformInstanceMetadata(inst.Metadata, metadata)
		if stringMapEqual(inst.Metadata, merged) {
			continue
		}
		if _, err := s.svc.UpdateInstance(r.Context(), principal, inst.ID, agentservice.UpdateInstanceInput{Metadata: merged}); err != nil {
			return err
		}
	}
	return nil
}

func platformSourceUserSyncInstanceMetadata(platformTenantID string, source platformSourceUser) map[string]string {
	metadata := platformSourceUserInstanceMetadata(platformTenantID, source)
	if !platformSourceUserIsVirtualEmployee(source) {
		return metadata
	}
	if strings.TrimSpace(source.DisplayName) == "" {
		delete(metadata, "ve_name")
	}
	if strings.TrimSpace(source.ExternalID) == "" {
		delete(metadata, "ve_handle")
	}
	if strings.TrimSpace(source.Title) == "" {
		delete(metadata, "ve_skill_description")
	}
	if source.SkillTagsSet && strings.TrimSpace(source.SkillTags) == "" {
		metadata["ve_skill_tags"] = ""
	} else if !source.SkillTagsSet && strings.TrimSpace(source.SkillTags) == "" {
		delete(metadata, "ve_skill_tags")
	}
	return metadata
}

func (s *HTTPServer) findPlatformSourceUser(r *http.Request, in platformSourceUserRequest, create bool) (*agentservice.Tenant, *agentservice.User, bool, error) {
	if binding, ok, err := s.findExistingVirtualSourceUserBinding(r, in); err != nil {
		return nil, nil, false, err
	} else if ok {
		if create {
			if err := s.repairPlatformHubServiceGroupModel(r, binding); err != nil {
				return nil, nil, false, err
			}
		}
		return &binding.Tenant, &binding.User, true, nil
	}
	if binding, ok, err := s.findExistingSourceUserBinding(r, in); err != nil {
		return nil, nil, false, err
	} else if ok {
		return &binding.Tenant, &binding.User, true, nil
	}
	if !create {
		return nil, nil, false, nil
	}
	virtualEmployee := platformSourceUserIsVirtualEmployee(in.SourceUser)
	virtualEmail := platformSourceUserRuntimeEmail(in.SourceUser)
	skillDescription := firstPlatformNonEmpty(in.Description, "VE Platform source user web assistant")
	if virtualEmployee {
		virtualEmail = firstPlatformNonEmpty(in.SourceUser.Email, virtualEmail)
		skillDescription = firstPlatformNonEmpty(in.Description, in.SourceUser.Title, "VE Platform virtual employee web assistant")
	}
	ve := platformVirtualEmployeeRequest{EmployeeID: in.SourceUser.ID, TenantID: in.TenantID, PlatformTenantID: in.TenantID, TenantName: in.TenantID, Name: firstPlatformNonEmpty(in.SourceUser.DisplayName, in.SourceUser.Email, in.SourceUser.ExternalID, in.SourceUser.ID), Handle: sanitizePlatformEmailLocal(firstPlatformNonEmpty(in.SourceUser.ExternalID, in.SourceUser.Email, in.SourceUser.ID)), VirtualEmail: virtualEmail, SkillDescription: skillDescription, DefaultLLM: in.DefaultLLM, LLMServiceGroupID: in.LLMServiceGroupID, LLMModel: in.LLMModel, HubLLMEndpoint: in.HubLLMEndpoint, HubLLMAPIKey: in.HubLLMAPIKey, HubLLMViewerToken: in.HubLLMViewerToken, ViewerToken: in.ViewerToken, AccessToken: in.AccessToken, SSHHosts: in.SSHHosts}
	tenant, err := s.findOrCreatePlatformTenant(r, in.TenantID, ve)
	if err != nil {
		return nil, nil, false, err
	}
	if tenant == nil {
		return nil, nil, false, nil
	}
	user, err := s.findPlatformUser(r, tenant.ID, ve, create)
	if err != nil {
		return nil, nil, false, err
	}
	if user == nil {
		return nil, nil, false, nil
	}
	if create {
		if err := s.ensurePlatformSourceUserDefaultConfig(r, agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}); err != nil {
			return nil, nil, false, err
		}
	}
	return tenant, user, true, nil
}

func (s *HTTPServer) findExistingSourceUserBinding(r *http.Request, in platformSourceUserRequest) (platformSourceUserBinding, bool, error) {
	sourceID := strings.TrimSpace(in.SourceUser.ID)
	if sourceID == "" {
		return platformSourceUserBinding{}, false, nil
	}
	ve := platformVirtualEmployeeRequest{TenantID: in.TenantID, PlatformTenantID: in.TenantID, TenantName: in.TenantID}
	tenant, err := s.findPlatformTenant(r, in.TenantID, ve)
	if err != nil || tenant == nil {
		return platformSourceUserBinding{}, false, err
	}
	users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
	if err != nil {
		return platformSourceUserBinding{}, false, err
	}
	source := in.SourceUser
	for _, user := range users {
		instances, err := s.svc.ListInstances(r.Context(), agentservice.Principal{TenantID: tenant.ID, UserID: user.ID})
		if err != nil {
			return platformSourceUserBinding{}, false, err
		}
		for _, inst := range instances {
			if platformSourceUserInstanceMatches(source, inst) {
				return platformSourceUserBinding{Tenant: *tenant, User: user, Source: source}, true, nil
			}
		}
	}
	return platformSourceUserBinding{}, false, nil
}

func (s *HTTPServer) updatePlatformSourceUserConfig(r *http.Request, p agentservice.Principal, in platformSourceUserRequest) error {
	if err := s.updatePlatformUserSSHHosts(r, p, in.SSHHosts); err != nil {
		return err
	}
	llmURL := strings.TrimSpace(in.HubLLMEndpoint)
	llmKey := strings.TrimSpace(platformSourceUserLLMCredential(in))
	llmModel := platformSourceUserLLMModelFromRequest(in)
	if llmURL == "" || llmKey == "" || llmModel == "" {
		return nil
	}
	return s.updatePlatformUserLLMConfig(r, p, llmURL, llmKey, llmModel)
}

func (s *HTTPServer) updatePlatformUserSSHHosts(r *http.Request, p agentservice.Principal, hosts []corelib.SSHHostEntry) error {
	if hosts == nil {
		return nil
	}
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	app.SSHHosts = normalizePlatformSSHHosts(hosts)
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func (s *HTTPServer) updatePlatformUserMaclawSrvConfig(r *http.Request, p agentservice.Principal, in platformMaclawSrvConfig) error {
	if in.isZero() {
		return nil
	}
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	applyString := func(dst *string, src *string) {
		if src == nil || agentservice.IsMaskedSecretPlaceholder(*src) {
			return
		}
		*dst = strings.TrimSpace(*src)
	}
	applyBool := func(dst *bool, src *bool) {
		if src != nil {
			*dst = *src
		}
	}
	applyBoolPtr := func(dst **bool, src platformOptionalBool) {
		if !src.Set {
			return
		}
		if src.Value == nil {
			*dst = nil
			return
		}
		value := *src.Value
		*dst = &value
	}
	applyBool(&app.QQBotEnabled, in.QQBotEnabled)
	applyString(&app.QQBotAppID, in.QQBotAppID)
	applyString(&app.QQBotAppSecret, in.QQBotAppSecret)
	applyBoolPtr(&app.QQBotLocalMode, in.QQBotLocalMode)
	applyBool(&app.TelegramBotEnabled, in.TelegramBotEnabled)
	applyString(&app.TelegramBotToken, in.TelegramBotToken)
	applyBoolPtr(&app.TelegramLocalMode, in.TelegramLocalMode)
	applyBool(&app.WeixinEnabled, in.WeixinEnabled)
	applyString(&app.WeixinToken, in.WeixinToken)
	applyString(&app.WeixinBaseURL, in.WeixinBaseURL)
	applyString(&app.WeixinCDNURL, in.WeixinCDNURL)
	applyString(&app.WeixinAccountID, in.WeixinAccountID)
	applyBoolPtr(&app.WeixinLocalMode, in.WeixinLocalMode)
	applyBool(&app.LansengerEnabled, in.LansengerEnabled)
	applyString(&app.LansengerAppID, in.LansengerAppID)
	applyString(&app.LansengerAppSecret, in.LansengerAppSecret)
	applyString(&app.LansengerGatewayURL, in.LansengerGatewayURL)
	applyString(&app.LansengerWSSURL, in.LansengerWSSURL)
	applyBoolPtr(&app.LansengerLocalMode, in.LansengerLocalMode)
	applyBool(&app.ThirdPartyGatewayEnabled, in.ThirdPartyGatewayEnabled)
	applyString(&app.ThirdPartyGatewayToken, in.ThirdPartyGatewayToken)
	applyString(&app.ThirdPartyGatewayHost, in.ThirdPartyGatewayHost)
	if in.ThirdPartyGatewayPort != nil {
		if *in.ThirdPartyGatewayPort < 0 || *in.ThirdPartyGatewayPort > 65535 {
			return fmt.Errorf("thirdparty_gateway_port must be between 0 and 65535")
		}
		app.ThirdPartyGatewayPort = *in.ThirdPartyGatewayPort
	}
	applyBoolPtr(&app.ThirdPartyGatewayLocalMode, in.ThirdPartyGatewayLocalMode)
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func normalizePlatformSSHHosts(hosts []corelib.SSHHostEntry) []corelib.SSHHostEntry {
	out := make([]corelib.SSHHostEntry, 0, len(hosts))
	seen := map[string]bool{}
	for _, host := range hosts {
		host.Label = strings.TrimSpace(host.Label)
		host.Host = strings.TrimSpace(host.Host)
		host.User = strings.TrimSpace(host.User)
		host.AuthMethod = strings.TrimSpace(host.AuthMethod)
		host.KeyPath = strings.TrimSpace(host.KeyPath)
		if host.Label == "" || host.Host == "" || host.User == "" {
			continue
		}
		key := strings.ToLower(host.Label)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, host)
	}
	return out
}

func (s *HTTPServer) repairPlatformHubServiceGroupModel(r *http.Request, binding platformRuntimeBinding) error {
	groupID := strings.TrimSpace(binding.Instance.Metadata["llm_service_group_id"])
	if groupID == "" {
		return nil
	}
	principal := agentservice.Principal{TenantID: binding.Tenant.ID, UserID: binding.User.ID}
	cfg, err := s.svc.GetUserConfig(r.Context(), principal)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	changed := false
	if (platformHubLLMEndpoint(app.MaclawLLMUrl) || strings.EqualFold(strings.TrimSpace(app.MaclawLLMCurrentProvider), platformHubLLMProviderName)) && strings.EqualFold(strings.TrimSpace(app.MaclawLLMModel), groupID) {
		app.MaclawLLMModel = platformHubLLMModel
		changed = true
	}
	for i := range app.MaclawLLMProviders {
		provider := app.MaclawLLMProviders[i]
		isCurrentHubProvider := strings.EqualFold(strings.TrimSpace(provider.Name), strings.TrimSpace(app.MaclawLLMCurrentProvider)) && strings.EqualFold(strings.TrimSpace(provider.Name), platformHubLLMProviderName)
		if (platformHubLLMEndpoint(provider.URL) || isCurrentHubProvider) && strings.EqualFold(strings.TrimSpace(provider.Model), groupID) {
			app.MaclawLLMProviders[i].Model = platformHubLLMModel
			changed = true
		}
	}
	if !changed {
		return nil
	}
	_, err = s.svc.UpdateUserConfig(r.Context(), principal, app)
	return err
}

func platformHubLLMEndpoint(rawURL string) bool {
	value := strings.ToLower(strings.TrimSpace(rawURL))
	return strings.Contains(value, "/api/llm/v1") || strings.Contains(value, "managed-by-hub")
}

func (s *HTTPServer) findExistingVirtualSourceUserBinding(r *http.Request, in platformSourceUserRequest) (platformRuntimeBinding, bool, error) {
	if strings.TrimSpace(in.SourceUser.ID) == "" {
		return platformRuntimeBinding{}, false, nil
	}
	if !platformSourceUserIsVirtualEmployee(in.SourceUser) && platformSourceUserHasIdentity(in.SourceUser) {
		return platformRuntimeBinding{}, false, nil
	}
	binding, ok, err := s.findPlatformRuntimeBinding(r, in.SourceUser.ID)
	if err != nil || !ok {
		return platformRuntimeBinding{}, ok, err
	}
	platformTenantID := strings.TrimSpace(binding.Instance.Metadata["ve_platform_tenant_id"])
	hubTenantID := strings.TrimSpace(binding.Instance.Metadata["ve_hub_tenant_id"])
	requestedTenantID := strings.TrimSpace(in.TenantID)
	if requestedTenantID != "" && platformTenantID != requestedTenantID && hubTenantID != requestedTenantID {
		if platformTenantID != "" || hubTenantID != "" {
			return platformRuntimeBinding{}, false, nil
		}
		if email := strings.TrimSpace(in.SourceUser.Email); email != "" && !strings.EqualFold(strings.TrimSpace(binding.User.Email), email) {
			return platformRuntimeBinding{}, false, nil
		}
	}
	return binding, true, nil
}

func platformSourceUserHasIdentity(source platformSourceUser) bool {
	return strings.TrimSpace(source.Email) != "" || strings.TrimSpace(source.ExternalID) != "" || strings.TrimSpace(source.DisplayName) != "" || strings.TrimSpace(source.AccountType) != "" || strings.TrimSpace(source.Provider) != ""
}

func platformSourceUserIsVirtualEmployee(source platformSourceUser) bool {
	if source.IsVirtualEmployee {
		return true
	}
	accountType := strings.ToLower(strings.TrimSpace(source.AccountType))
	provider := strings.ToLower(strings.TrimSpace(source.Provider))
	return accountType == "virtual_employee" || accountType == "digital_employee" || provider == "virtualemployee-platform" || provider == "virtual_employee_platform" || provider == "virtualemployee"
}

func (s *HTTPServer) ensurePlatformSourceUserDefaultConfig(r *http.Request, p agentservice.Principal) error {
	cfg, err := s.svc.GetUserConfig(r.Context(), p)
	if err != nil {
		return err
	}
	app := cfg.AppConfig
	if strings.TrimSpace(app.MaclawLLMUrl) != "" && strings.TrimSpace(app.MaclawLLMKey) != "" && strings.TrimSpace(app.MaclawLLMModel) != "" {
		return nil
	}
	if strings.TrimSpace(app.MaclawLLMUrl) == "" {
		app.MaclawLLMUrl = "http://127.0.0.1/managed-by-hub"
	}
	if strings.TrimSpace(app.MaclawLLMKey) == "" {
		app.MaclawLLMKey = "managed-by-hub"
	}
	if strings.TrimSpace(app.MaclawLLMModel) == "" {
		app.MaclawLLMModel = platformHubLLMModel
	}
	_, err = s.svc.UpdateUserConfig(r.Context(), p, app)
	return err
}

func platformSourceUserInstanceMetadata(platformTenantID string, source platformSourceUser) map[string]string {
	metadata := map[string]string{"ve_source_user_id": source.ID, "ve_source_user_external_id": source.ExternalID, "ve_source_user_email": source.Email, "ve_platform_tenant_id": platformTenantID, "ve_source_user_department": source.Department, "ve_source_user_title": source.Title}
	if platformSourceUserIsVirtualEmployee(source) {
		metadata["ve_employee_id"] = source.ID
		metadata["ve_name"] = firstPlatformNonEmpty(source.DisplayName, source.Email, source.ExternalID, source.ID)
		metadata["ve_handle"] = source.ExternalID
		metadata["ve_skill_description"] = source.Title
		metadata["ve_skill_tags"] = platformSourceUserSkillTags(source)
	}
	return compactPlatformMetadata(metadata)
}

func platformSourceUserSkillTags(source platformSourceUser) string {
	return strings.Join(cleanPlatformSkillTags(strings.FieldsFunc(source.SkillTags, func(r rune) bool {
		return r == ',' || r == ';' || r == '\uff0c' || r == '\uff1b' || r == '\n' || r == '\t'
	})), ", ")
}

func platformSourceUserRuntimeEmail(source platformSourceUser) string {
	seed := firstPlatformNonEmpty(source.ID, source.ExternalID, source.Email, "source-user")
	local := sanitizePlatformEmailLocal(firstPlatformNonEmpty(source.ID, source.ExternalID, source.Email, "source-user")) + "-" + shortPlatformHash(seed)
	return local + "@ve-platform.local"
}

func platformWebLaunchURL(r *http.Request, launchToken, view, instanceID, settingsTab string, binding platformSourceUserBinding) string {
	scheme := platformLaunchScheme(r.Header.Get("X-Forwarded-Proto"))
	host := platformLaunchHost(firstPlatformNonEmpty(r.Header.Get("X-Forwarded-Host"), r.Host))
	q := url.Values{}
	q.Set("launch_token", launchToken)
	q.Set("view", view)
	q.Set("tenant_id", binding.Tenant.ID)
	q.Set("user_id", binding.User.ID)
	q.Set("source_user_id", binding.Source.ID)
	if instanceID != "" {
		q.Set("instance_id", instanceID)
	}
	settingsTab = normalizePlatformSettingsTab(settingsTab)
	if view == "settings" && settingsTab != "" {
		q.Set("settings_tab", settingsTab)
	}
	return scheme + "://" + host + "/app/?" + q.Encode()
}

func normalizePlatformSettingsTab(tab string) string {
	tab = strings.ToLower(strings.TrimSpace(tab))
	switch tab {
	case "channels", "channels_more":
		return "im"
	case "advanced":
		return ""
	case "llm", "tools", "skills", "memory", "security", "im", "ui":
		return tab
	default:
		return ""
	}
}

func platformLaunchScheme(value string) string {
	scheme := strings.ToLower(platformForwardedHeaderFirst(value))
	if scheme == "https" {
		return "https"
	}
	return "http"
}

func platformLaunchHost(value string) string {
	return coreim.ThirdPartyForwardedHost(value)
}

func platformForwardedHeaderFirst(value string) string {
	return coreim.ThirdPartyForwardedHeaderFirst(value)
}

func maxPlatformTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (s *HTTPServer) handlePlatformKnowledgeImport(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	binding, ok := s.requirePlatformRuntimeBinding(w, r, r.PathValue("employeeId"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "employee_id": r.PathValue("employeeId"), "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "instance_id": binding.Instance.ID, "kind": "knowledge_import"})
}

func (s *HTTPServer) handlePlatformMigrationImport(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	binding, ok := s.requirePlatformRuntimeBinding(w, r, r.PathValue("employeeId"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "employee_id": r.PathValue("employeeId"), "tenant_id": binding.Tenant.ID, "user_id": binding.User.ID, "instance_id": binding.Instance.ID, "kind": "migration_import"})
}

func (s *HTTPServer) handlePlatformSyncJobRun(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	if employeeID := platformString(payload, "employee_id"); employeeID != "" {
		if _, ok := s.requirePlatformRuntimeBinding(w, r, employeeID); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "completed", "job_id": r.PathValue("jobId"), "conflicts": []any{}, "next_cursor": ""})
}

func (s *HTTPServer) handlePlatformSyncConflictResolve(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if !decodeJSON(w, r, &payload) {
		return
	}
	if employeeID := platformString(payload, "employee_id"); employeeID != "" {
		if _, ok := s.requirePlatformRuntimeBinding(w, r, employeeID); !ok {
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "resolved", "conflict_id": r.PathValue("conflictId")})
}

func (s *HTTPServer) requirePlatformRuntimeBinding(w http.ResponseWriter, r *http.Request, employeeID string) (platformRuntimeBinding, bool) {
	binding, ok, err := s.findPlatformRuntimeBinding(r, employeeID)
	if err != nil {
		writeRedactedError(w, err, s.svc.DataRoot())
		return platformRuntimeBinding{}, false
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "virtual employee runtime not found"})
		return platformRuntimeBinding{}, false
	}
	return binding, true
}

func (s *HTTPServer) findOrCreatePlatformTenant(r *http.Request, tenantKey string, in platformVirtualEmployeeRequest) (*agentservice.Tenant, error) {
	name := platformTenantDisplayName(tenantKey, in)
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return nil, err
	}
	for i := range tenants {
		if platformTenantMatches(r, s, tenants[i], tenantKey, in, name) {
			return s.renamePlatformTenantIfNeeded(r, tenants[i], name)
		}
	}
	return s.svc.CreateTenant(r.Context(), agentservice.CreateTenantInput{Name: name, DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
}

func (s *HTTPServer) findPlatformTenant(r *http.Request, tenantKey string, in platformVirtualEmployeeRequest) (*agentservice.Tenant, error) {
	name := platformTenantDisplayName(tenantKey, in)
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return nil, err
	}
	for i := range tenants {
		if platformTenantMatches(r, s, tenants[i], tenantKey, in, name) {
			return &tenants[i], nil
		}
	}
	return nil, nil
}

func (s *HTTPServer) renamePlatformTenantIfNeeded(r *http.Request, tenant agentservice.Tenant, name string) (*agentservice.Tenant, error) {
	if tenant.Name == name {
		return &tenant, nil
	}
	return s.svc.UpdateTenant(r.Context(), tenant.ID, agentservice.UpdateTenantInput{Name: &name})
}

func platformTenantMatches(r *http.Request, s *HTTPServer, tenant agentservice.Tenant, tenantKey string, in platformVirtualEmployeeRequest, displayName string) bool {
	if tenant.Name == "VE Platform "+strings.TrimSpace(tenantKey) {
		return true
	}
	if !platformManagedTenant(tenant) {
		return false
	}
	if tenant.Name == displayName {
		return true
	}
	code := firstPlatformNonEmpty(in.HubTenantCode, in.TenantCode)
	if code != "" && strings.Contains(tenant.Name, "("+code+")") {
		return true
	}
	return platformTenantHasRuntimeIdentity(r, s, tenant.ID, tenantKey, in)
}

func platformManagedTenant(tenant agentservice.Tenant) bool {
	return tenant.DeleteProtected && strings.EqualFold(strings.TrimSpace(tenant.DeleteProtectionReason), "Managed by VE Platform")
}

func platformTenantHasRuntimeIdentity(r *http.Request, s *HTTPServer, tenantID, tenantKey string, in platformVirtualEmployeeRequest) bool {
	users, err := s.svc.ListUsers(r.Context(), tenantID, agentservice.ListUsersAdminInput{})
	if err != nil {
		return false
	}
	for _, user := range users {
		instances, err := s.svc.ListInstances(r.Context(), agentservice.Principal{TenantID: tenantID, UserID: user.ID})
		if err != nil {
			continue
		}
		for _, inst := range instances {
			hubTenantID := strings.TrimSpace(tenantKey)
			platformTenantID := strings.TrimSpace(in.PlatformTenantID)
			hubTenantCode := strings.TrimSpace(in.HubTenantCode)
			if hubTenantID != "" && inst.Metadata["ve_hub_tenant_id"] == hubTenantID {
				return true
			}
			if platformTenantID != "" && inst.Metadata["ve_platform_tenant_id"] == platformTenantID {
				return true
			}
			if hubTenantCode != "" && inst.Metadata["ve_hub_tenant_code"] == hubTenantCode {
				return true
			}
		}
	}
	return false
}

func platformTenantDisplayName(tenantKey string, in platformVirtualEmployeeRequest) string {
	name := strings.TrimSpace(in.TenantName)
	code := firstPlatformNonEmpty(in.HubTenantCode, in.TenantCode)
	if name == "" {
		name = code
	}
	if name == "" {
		name = strings.TrimSpace(tenantKey)
	}
	if code != "" && !strings.EqualFold(name, code) {
		return "VE Platform " + name + " (" + code + ")"
	}
	return "VE Platform " + name
}

func (s *HTTPServer) findOrCreatePlatformUser(r *http.Request, tenantID string, in platformVirtualEmployeeRequest) (*agentservice.User, error) {
	return s.findPlatformUser(r, tenantID, in, true)
}

func (s *HTTPServer) findPlatformUser(r *http.Request, tenantID string, in platformVirtualEmployeeRequest, create bool) (*agentservice.User, error) {
	email := platformRuntimeEmail(in)
	name := firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID)
	users, err := s.svc.ListUsers(r.Context(), tenantID, agentservice.ListUsersAdminInput{Email: email})
	if err != nil {
		return nil, err
	}
	for i := range users {
		if strings.EqualFold(users[i].Email, email) {
			if !create {
				return &users[i], nil
			}
			return s.updatePlatformUserIfNeeded(r, tenantID, users[i], name)
		}
	}
	if !create {
		return nil, nil
	}
	return s.svc.CreateUser(r.Context(), agentservice.CreateUserInput{TenantID: tenantID, Name: name, Email: email, DeleteProtected: true, DeleteProtectionReason: "Managed by VE Platform"})
}

func platformRuntimeEmail(in platformVirtualEmployeeRequest) string {
	if email := strings.TrimSpace(in.VirtualEmail); email != "" {
		return email
	}
	seed := firstPlatformNonEmpty(in.EmployeeID, in.Handle, "employee")
	base := sanitizePlatformEmailLocal(firstPlatformNonEmpty(in.Handle, in.EmployeeID, "employee"))
	local := base + "-" + shortPlatformHash(seed)
	return local + "@ve-platform.local"
}

func shortPlatformHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:8]
}

func sanitizePlatformEmailLocal(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "employee"
	}
	return out
}

func (s *HTTPServer) updatePlatformUserIfNeeded(r *http.Request, tenantID string, user agentservice.User, name string) (*agentservice.User, error) {
	if !platformManagedUser(user) || user.Name == name {
		return &user, nil
	}
	return s.svc.UpdateUser(r.Context(), tenantID, user.ID, agentservice.UpdateUserInput{Name: &name})
}

func (s *HTTPServer) findOrCreatePlatformInstance(r *http.Request, p agentservice.Principal, in platformVirtualEmployeeRequest) (*agentservice.Instance, bool, error) {
	instances, err := s.svc.ListInstances(r.Context(), p)
	if err != nil {
		return nil, false, err
	}
	for i := range instances {
		if instances[i].Metadata["ve_employee_id"] == strings.TrimSpace(in.EmployeeID) {
			inst, err := s.updatePlatformInstanceIfNeeded(r, p, instances[i], in)
			return inst, false, err
		}
	}
	inst, err := s.svc.CreateInstance(r.Context(), p, agentservice.CreateInstanceInput{Name: firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID), Description: strings.TrimSpace(in.SkillDescription), Metadata: platformInstanceMetadata(in), AllowInvalidConfig: platformLLMCredential(in) == ""})
	return inst, true, err
}

func (s *HTTPServer) updatePlatformInstanceIfNeeded(r *http.Request, p agentservice.Principal, inst agentservice.Instance, in platformVirtualEmployeeRequest) (*agentservice.Instance, error) {
	name := firstPlatformNonEmpty(in.Name, in.Handle, in.EmployeeID)
	description := strings.TrimSpace(in.SkillDescription)
	metadata := mergePlatformInstanceMetadata(inst.Metadata, platformInstanceMetadata(in))
	update := agentservice.UpdateInstanceInput{}
	if inst.Name != name {
		update.Name = &name
	}
	if inst.Description != description {
		update.Description = &description
	}
	if !stringMapEqual(inst.Metadata, metadata) {
		update.Metadata = metadata
	}
	if update.Name == nil && update.Description == nil && update.Metadata == nil {
		return &inst, nil
	}
	return s.svc.UpdateInstance(r.Context(), p, inst.ID, update)
}

func mergePlatformInstanceMetadata(existing, platform map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range platform {
		if strings.TrimSpace(value) != "" {
			merged[key] = value
			continue
		}
		delete(merged, key)
	}
	return merged
}

func stringMapEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		if b[key] != av {
			return false
		}
	}
	return true
}

func platformInstanceMetadata(in platformVirtualEmployeeRequest) map[string]string {
	return compactPlatformMetadata(map[string]string{
		"ve_employee_id":        strings.TrimSpace(in.EmployeeID),
		"ve_name":               strings.TrimSpace(in.Name),
		"ve_handle":             strings.TrimSpace(in.Handle),
		"ve_avatar_data_url":    strings.TrimSpace(in.AvatarDataURL),
		"ve_skill_description":  strings.TrimSpace(in.SkillDescription),
		"ve_skill_tags":         strings.Join(cleanPlatformSkillTags(in.SkillTags), ", "),
		"ve_platform_tenant_id": strings.TrimSpace(in.PlatformTenantID),
		"ve_hub_tenant_id":      strings.TrimSpace(in.TenantID),
		"ve_tenant_code":        strings.TrimSpace(in.TenantCode),
		"ve_hub_tenant_code":    strings.TrimSpace(in.HubTenantCode),
		"llm_service_group_id":  platformLLMServiceGroupID(in.LLMServiceGroupID, in.DefaultLLM),
	})
}

func compactPlatformMetadata(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		value = strings.TrimSpace(value)
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func (s *HTTPServer) findPlatformRuntimeBinding(r *http.Request, employeeID string) (platformRuntimeBinding, bool, error) {
	return s.findPlatformRuntimeBindingByMetadata(r, employeeID, false)
}

func (s *HTTPServer) findPlatformMessageRuntimeBinding(r *http.Request, employeeID string) (platformRuntimeBinding, bool, error) {
	return s.findPlatformRuntimeBindingByMetadata(r, employeeID, true)
}

func (s *HTTPServer) findPlatformRuntimeBindingByMetadata(r *http.Request, employeeID string, allowSourceUserID bool) (platformRuntimeBinding, bool, error) {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" {
		return platformRuntimeBinding{}, false, nil
	}
	hubTenantID := platformRuntimeRequestHubTenantID(r)
	var legacyCandidate platformRuntimeBinding
	hasLegacyCandidate := false
	tenants, err := s.svc.ListTenants(r.Context(), agentservice.ListTenantsInput{})
	if err != nil {
		return platformRuntimeBinding{}, false, err
	}
	for _, tenant := range tenants {
		users, err := s.svc.ListUsers(r.Context(), tenant.ID, agentservice.ListUsersAdminInput{})
		if err != nil {
			return platformRuntimeBinding{}, false, err
		}
		for _, user := range users {
			principal := agentservice.Principal{TenantID: tenant.ID, UserID: user.ID}
			instances, err := s.svc.ListInstances(r.Context(), principal)
			if err != nil {
				return platformRuntimeBinding{}, false, err
			}
			for _, inst := range instances {
				if platformRuntimeInstanceMatchesEmployeeID(inst, employeeID, allowSourceUserID) {
					binding := platformRuntimeBinding{Tenant: tenant, User: user, Instance: inst}
					if hubTenantID != "" {
						instHubTenantID := strings.TrimSpace(inst.Metadata["ve_hub_tenant_id"])
						if instHubTenantID == "" {
							if !hasLegacyCandidate {
								legacyCandidate = binding
								hasLegacyCandidate = true
							}
							continue
						}
						if !strings.EqualFold(instHubTenantID, hubTenantID) {
							continue
						}
					}
					return binding, true, nil
				}
			}
		}
	}
	if hasLegacyCandidate {
		return legacyCandidate, true, nil
	}
	return platformRuntimeBinding{}, false, nil
}

func platformRuntimeRequestHubTenantID(r *http.Request) string {
	if r == nil {
		return ""
	}
	queryTenantID := ""
	if r.URL != nil {
		queryTenantID = r.URL.Query().Get("tenant_id")
	}
	return firstPlatformNonEmpty(r.Header.Get("X-VE-Hub-Tenant-ID"), r.Header.Get("X-Hub-Tenant-ID"), queryTenantID)
}

func platformRuntimeInstanceMatchesEmployeeID(inst agentservice.Instance, employeeID string, allowSourceUserID bool) bool {
	employeeID = strings.TrimSpace(employeeID)
	if employeeID == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_employee_id"]), employeeID) {
		return true
	}
	return allowSourceUserID && strings.EqualFold(strings.TrimSpace(inst.Metadata["ve_source_user_id"]), employeeID)
}

func platformString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func firstPlatformNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func decodePlatformJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, platformJSONBodyMaxBytes)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(out); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body", "detail": err.Error()})
		return false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body", "detail": "multiple json values"})
		return false
	}
	return true
}

// platformSSEWriter writes Server-Sent Events for streaming VE responses.
// Hub consumes these events to deliver progressive stream_chunk messages to the
// requesting client, replacing the static "思考中..." indicator with real-time output.
type platformSSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newPlatformSSEWriter(w http.ResponseWriter) *platformSSEWriter {
	flusher, _ := w.(http.Flusher)
	return &platformSSEWriter{w: w, flusher: flusher}
}

func (s *platformSSEWriter) WriteHeader() {
	s.w.Header().Set("Content-Type", "text/event-stream")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.Header().Set("Connection", "keep-alive")
	s.w.WriteHeader(http.StatusOK)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *platformSSEWriter) WriteChunk(chunk string) {
	if chunk == "" {
		return
	}
	data, _ := json.Marshal(map[string]string{"chunk": chunk})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *platformSSEWriter) WriteError(errMsg string) {
	data, _ := json.Marshal(map[string]string{"error": errMsg})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func (s *platformSSEWriter) WriteDone(content string, sess any, run any, msg any) {
	data, _ := json.Marshal(map[string]any{"done": true, "content": content, "session": sess, "run": run, "message": msg})
	fmt.Fprintf(s.w, "data: %s\n\n", data)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}
