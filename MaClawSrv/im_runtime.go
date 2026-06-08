package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/lansenger"
	"github.com/RapidAI/CodeClaw/corelib/qqbot"
	"github.com/RapidAI/CodeClaw/corelib/telegram"
)

const srvIMSessionAgent = "default"

const srvRuntimeIdentitySyncInterval = 5 * time.Second

type srvIMGateway interface {
	Start(context.Context) error
	Stop() error
	SetStatusCallback(func(string))
}

type srvIMRuntimeStatus struct {
	Status    string    `json:"status"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type srvIMRuntime struct {
	principal          agentservice.Principal
	platform           string
	configKey          string
	gateway            srvIMGateway
	status             srvIMRuntimeStatus
	instanceID         string
	lastIdentitySyncAt time.Time
}

type srvIMGatewayManager struct {
	svc       *agentservice.Service
	aiModels  *srvAIModelManager
	factories map[string]srvIMGatewayFactory

	mu       sync.Mutex
	runtimes map[string]*srvIMRuntime
}

type srvIMGatewayFactory struct {
	ConfigKey func(corelib.AppConfig) (string, bool)
	New       func(corelib.AppConfig, func(srvIMIncomingMessage)) srvIMGateway
}

type srvIMIncomingMessage struct {
	Platform      string
	ContactID     string
	Title         string
	Text          string
	ClientEventID string
	MediaType     string
	MediaName     string
	MimeType      string
	MediaData     []byte
	MediaBytes    int
	Extra         map[string]string
	Reply         func(context.Context, string) error
	ReplyMedia    func(context.Context, []byte, string, string, string) error
}

type srvTelegramGateway struct{ *telegram.Gateway }
type srvQQBotGateway struct{ *qqbot.Gateway }
type srvLansengerGateway struct{ *lansenger.Gateway }

func (g srvTelegramGateway) SetStatusCallback(cb func(string)) { g.Gateway.SetStatusCallback(cb) }
func (g srvQQBotGateway) SetStatusCallback(cb func(string))    { g.Gateway.SetStatusCallback(cb) }
func (g srvLansengerGateway) SetStatusCallback(cb func(string)) {
	g.Gateway.SetStatusCallback(cb)
}

func newSrvIMGatewayManager(svc *agentservice.Service, aiModels ...*srvAIModelManager) *srvIMGatewayManager {
	var mgr *srvAIModelManager
	if len(aiModels) > 0 {
		mgr = aiModels[0]
	}
	m := &srvIMGatewayManager{svc: svc, aiModels: mgr, runtimes: map[string]*srvIMRuntime{}}
	m.factories = map[string]srvIMGatewayFactory{
		"telegram":  {ConfigKey: srvTelegramRuntimeConfigKey, New: newSrvTelegramRuntimeGateway},
		"qq":        {ConfigKey: srvQQRuntimeConfigKey, New: newSrvQQRuntimeGateway},
		"lansenger": {ConfigKey: srvLansengerRuntimeConfigKey, New: newSrvLansengerRuntimeGateway},
	}
	return m
}

func srvTelegramRuntimeConfigKey(cfg corelib.AppConfig) (string, bool) {
	token := strings.TrimSpace(cfg.TelegramBotToken)
	if !cfg.TelegramBotEnabled || token == "" {
		return "", false
	}
	return token, true
}

func newSrvTelegramRuntimeGateway(cfg corelib.AppConfig, handler func(srvIMIncomingMessage)) srvIMGateway {
	token := strings.TrimSpace(cfg.TelegramBotToken)
	var gw *telegram.Gateway
	gw = telegram.NewGateway(telegram.Config{BotToken: token}, func(msg telegram.IncomingMessage) {
		contactID := strconv.FormatInt(msg.ChatID, 10)
		handler(srvIMIncomingMessage{
			Platform:      "telegram",
			ContactID:     contactID,
			Title:         "Telegram " + contactID,
			Text:          msg.Text,
			ClientEventID: srvIMMessageID("telegram", contactID, msg.Timestamp, msg.Text, msg.MediaType, msg.MediaName, len(msg.MediaData)),
			MediaType:     msg.MediaType,
			MediaName:     msg.MediaName,
			MimeType:      msg.MimeType,
			MediaData:     msg.MediaData,
			MediaBytes:    len(msg.MediaData),
			Extra: map[string]string{
				"username":      msg.Username,
				"language_code": msg.LanguageCode,
			},
			Reply: func(ctx context.Context, text string) error {
				return gw.SendText(ctx, telegram.OutgoingText{ChatID: msg.ChatID, Text: text})
			},
			ReplyMedia: func(ctx context.Context, data []byte, fileName, mediaType, mimeType string) error {
				fileType := "document"
				if normalizeSrvIMMediaType(mediaType) == "voice" {
					fileType = "audio"
				}
				return gw.SendMedia(ctx, telegram.OutgoingMedia{ChatID: msg.ChatID, FileType: fileType, FileData: base64.StdEncoding.EncodeToString(data), FileName: fileName, MimeType: mimeType})
			},
		})
	})
	return srvTelegramGateway{gw}
}

func srvQQRuntimeConfigKey(cfg corelib.AppConfig) (string, bool) {
	appID := strings.TrimSpace(cfg.QQBotAppID)
	secret := strings.TrimSpace(cfg.QQBotAppSecret)
	if !cfg.QQBotEnabled || appID == "" || secret == "" {
		return "", false
	}
	return appID + "\x00" + secret, true
}

func newSrvQQRuntimeGateway(cfg corelib.AppConfig, handler func(srvIMIncomingMessage)) srvIMGateway {
	var gw *qqbot.Gateway
	appID := strings.TrimSpace(cfg.QQBotAppID)
	secret := strings.TrimSpace(cfg.QQBotAppSecret)
	gw = qqbot.NewGateway(qqbot.Config{AppID: appID, AppSecret: secret}, func(msg qqbot.IncomingMessage) {
		handler(srvIMIncomingMessage{
			Platform:      "qq",
			ContactID:     msg.OpenID,
			Title:         "QQ " + msg.OpenID,
			Text:          msg.Text,
			ClientEventID: srvIMMessageID("qq", msg.OpenID, msg.Timestamp, msg.Text, msg.MediaType, msg.MediaName, len(msg.MediaData)),
			MediaType:     msg.MediaType,
			MediaName:     msg.MediaName,
			MimeType:      msg.MimeType,
			MediaData:     msg.MediaData,
			MediaBytes:    len(msg.MediaData),
			Reply: func(ctx context.Context, text string) error {
				return gw.SendText(ctx, qqbot.OutgoingText{OpenID: msg.OpenID, Text: text})
			},
			ReplyMedia: func(ctx context.Context, data []byte, fileName, mediaType, mimeType string) error {
				fileType := 4
				if normalizeSrvIMMediaType(mediaType) == "voice" {
					fileType = 3
				}
				return gw.SendMedia(ctx, qqbot.OutgoingMedia{OpenID: msg.OpenID, FileType: fileType, FileData: base64.StdEncoding.EncodeToString(data), FileName: fileName, MimeType: mimeType})
			},
		})
	})
	return srvQQBotGateway{gw}
}

func srvLansengerRuntimeConfigKey(cfg corelib.AppConfig) (string, bool) {
	appID := strings.TrimSpace(cfg.LansengerAppID)
	secret := strings.TrimSpace(cfg.LansengerAppSecret)
	baseURL := strings.TrimSpace(cfg.LansengerApiGatewayURL())
	wssURL := strings.TrimSpace(cfg.LansengerWebSocketGatewayURL())
	if !cfg.LansengerEnabled || appID == "" || secret == "" || baseURL == "" {
		return "", false
	}
	return strings.Join([]string{appID, secret, baseURL, wssURL}, "\x00"), true
}

func newSrvLansengerRuntimeGateway(cfg corelib.AppConfig, handler func(srvIMIncomingMessage)) srvIMGateway {
	appID := strings.TrimSpace(cfg.LansengerAppID)
	secret := strings.TrimSpace(cfg.LansengerAppSecret)
	baseURL := strings.TrimSpace(cfg.LansengerApiGatewayURL())
	wssURL := strings.TrimSpace(cfg.LansengerWebSocketGatewayURL())
	var gw *lansenger.Gateway
	gw = lansenger.NewGateway(lansenger.Config{AppID: appID, AppSecret: secret, ApiGatewayURL: baseURL, WebSocketBaseURL: wssURL}, func(msg lansenger.IncomingMessage) {
		contactID := msg.FromUserID
		if msg.ChatType == "group" && strings.TrimSpace(msg.GroupID) != "" {
			contactID = msg.GroupID
		}
		handler(srvIMIncomingMessage{
			Platform:      "lansenger",
			ContactID:     contactID,
			Title:         "Lansenger " + contactID,
			Text:          msg.Text,
			ClientEventID: firstNonEmptyString(msg.MessageID, srvIMMessageID("lansenger", contactID, time.Now(), msg.Text, msg.MediaType, msg.MediaName, len(msg.MediaData))),
			MediaType:     firstNonEmptyString(msg.MediaType, msg.MessageType),
			MediaName:     msg.MediaName,
			MediaData:     msg.MediaData,
			MediaBytes:    len(msg.MediaData),
			Extra: map[string]string{
				"chat_type": msg.ChatType,
				"group_id":  msg.GroupID,
			},
			Reply: func(ctx context.Context, text string) error {
				return gw.SendText(ctx, lansenger.OutgoingText{ToUserID: contactID, Text: text, IsGroup: msg.ChatType == "group"})
			},
			ReplyMedia: func(ctx context.Context, data []byte, fileName, mediaType, mimeType string) error {
				return gw.SendMedia(ctx, lansenger.OutgoingMedia{ToUserID: contactID, FileData: data, FileName: fileName, MediaType: normalizeSrvIMMediaType(mediaType), IsGroup: msg.ChatType == "group"})
			},
		})
	})
	return srvLansengerGateway{gw}
}

func (m *srvIMGatewayManager) SyncPrincipal(ctx context.Context, p agentservice.Principal, cfg corelib.AppConfig) {
	if m == nil {
		return
	}
	for platform, factory := range m.factories {
		if factory.ConfigKey == nil {
			m.replacePrincipalPlatformError(p, platform, "", "gateway config resolver is not available")
			continue
		}
		configKey, enabled := factory.ConfigKey(cfg)
		if !enabled {
			m.stopPrincipalPlatform(p, platform)
			continue
		}
		key := srvIMRuntimeKey(p, platform)
		m.mu.Lock()
		current := m.runtimes[key]
		if current != nil && current.configKey == configKey {
			if current.status.Status == srvWeixinStatusError && factory.New != nil {
				old := current
				delete(m.runtimes, key)
				m.mu.Unlock()
				stopSrvIMGateway(old.gateway)
			} else {
				m.mu.Unlock()
				continue
			}
			m.mu.Lock()
			current = m.runtimes[key]
			if current != nil && current.configKey == configKey {
				m.mu.Unlock()
				continue
			}
		}
		if factory.New == nil {
			old := current
			runtime := &srvIMRuntime{principal: p, platform: platform, configKey: configKey, status: srvIMRuntimeStatus{Status: srvWeixinStatusError, LastError: "gateway factory is not available", UpdatedAt: time.Now().UTC()}}
			m.runtimes[key] = runtime
			m.mu.Unlock()
			if old != nil {
				stopSrvIMGateway(old.gateway)
			}
			continue
		}
		old := current
		runtime := &srvIMRuntime{principal: p, platform: platform, configKey: configKey, status: srvIMRuntimeStatus{Status: srvWeixinStatusConnecting, UpdatedAt: time.Now().UTC()}}
		m.runtimes[key] = runtime
		m.mu.Unlock()
		if old != nil {
			stopSrvIMGateway(old.gateway)
		}
		gateway := factory.New(cfg, func(msg srvIMIncomingMessage) {
			m.handleIncomingMessage(ctx, p, runtime, msg)
		})
		if gateway == nil {
			m.mu.Lock()
			if m.runtimes[key] == runtime {
				runtime.status.Status = srvWeixinStatusError
				runtime.status.LastError = "gateway factory returned nil"
				runtime.status.UpdatedAt = time.Now().UTC()
				m.runtimes[key] = runtime
			}
			m.mu.Unlock()
			continue
		}
		gateway.SetStatusCallback(func(status string) {
			m.setStatus(p, platform, runtime, normalizeSrvWeixinStatus(status), "")
		})
		m.mu.Lock()
		stale := m.runtimes[key] != runtime
		if !stale {
			runtime.gateway = gateway
		}
		m.mu.Unlock()
		if stale {
			stopSrvIMGateway(gateway)
			continue
		}
		if err := gateway.Start(context.Background()); err != nil {
			stopSrvIMGateway(gateway)
			m.setStatus(p, platform, runtime, srvWeixinStatusError, err.Error())
			continue
		}
		if !m.isCurrentRuntime(p, platform, runtime) {
			stopSrvIMGateway(gateway)
			continue
		}
		m.markConnectedIfConnecting(p, platform, runtime)
	}
}

func (m *srvIMGatewayManager) replacePrincipalPlatformError(p agentservice.Principal, platform, configKey, lastError string) {
	key := srvIMRuntimeKey(p, platform)
	m.mu.Lock()
	old := m.runtimes[key]
	m.runtimes[key] = &srvIMRuntime{
		principal: p,
		platform:  platform,
		configKey: configKey,
		status:    srvIMRuntimeStatus{Status: srvWeixinStatusError, LastError: strings.TrimSpace(lastError), UpdatedAt: time.Now().UTC()},
	}
	m.mu.Unlock()
	if old != nil {
		stopSrvIMGateway(old.gateway)
	}
}

func (m *srvIMGatewayManager) Status(p agentservice.Principal, platform string) srvIMRuntimeStatus {
	if m == nil {
		return srvIMRuntimeStatus{Status: srvWeixinStatusDisabled}
	}
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil {
		return srvIMRuntimeStatus{Status: srvWeixinStatusDisabled}
	}
	return runtime.status
}

func (m *srvIMGatewayManager) Statuses(p agentservice.Principal) map[string]srvIMRuntimeStatus {
	out := map[string]srvIMRuntimeStatus{}
	if m == nil {
		return out
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for platform := range m.factories {
		runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
		if runtime == nil {
			out[platform] = srvIMRuntimeStatus{Status: srvWeixinStatusDisabled}
			continue
		}
		out[platform] = runtime.status
	}
	return out
}

func (m *srvIMGatewayManager) StopPrincipal(p agentservice.Principal) {
	if m == nil {
		return
	}
	for platform := range m.factories {
		m.stopPrincipalPlatform(p, platform)
	}
}

func (m *srvIMGatewayManager) stopPrincipalPlatform(p agentservice.Principal, platform string) {
	platform = normalizeSrvIMPlatform(platform)
	key := srvIMRuntimeKey(p, platform)
	m.mu.Lock()
	runtime := m.runtimes[key]
	if runtime != nil {
		delete(m.runtimes, key)
	}
	m.mu.Unlock()
	if runtime != nil {
		stopSrvIMGateway(runtime.gateway)
	}
}

func stopSrvIMGateway(gateway srvIMGateway) {
	if gateway == nil {
		return
	}
	gateway.SetStatusCallback(func(string) {})
	_ = gateway.Stop()
}

func (m *srvIMGatewayManager) setStatus(p agentservice.Principal, platform string, expected *srvIMRuntime, status, lastError string) {
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil || runtime != expected {
		return
	}
	runtime.status.Status = status
	runtime.status.LastError = strings.TrimSpace(lastError)
	runtime.status.UpdatedAt = time.Now().UTC()
}

func (m *srvIMGatewayManager) markConnectedIfConnecting(p agentservice.Principal, platform string, expected *srvIMRuntime) {
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil || runtime != expected || runtime.status.Status != srvWeixinStatusConnecting {
		return
	}
	runtime.status.Status = srvWeixinStatusConnected
	runtime.status.LastError = ""
	runtime.status.UpdatedAt = time.Now().UTC()
}

func (m *srvIMGatewayManager) handleIncomingMessage(parent context.Context, p agentservice.Principal, expected *srvIMRuntime, msg srvIMIncomingMessage) {
	platform := normalizeSrvIMPlatform(msg.Platform)
	contactID := strings.TrimSpace(msg.ContactID)
	if !m.isCurrentRuntime(p, platform, expected) {
		return
	}
	cfg, _ := m.svc.GetUserConfig(context.Background(), p)
	text := strings.TrimSpace(msg.Text)
	asrTranscript, asrOK := m.transcribeIncomingVoice(context.Background(), cfg, msg)
	if text == "" && asrOK {
		text = asrTranscript
	}
	if text == "" && msg.MediaType != "" {
		text = fmt.Sprintf("[received %s]", msg.MediaType)
	}
	if text == "" {
		return
	}
	if contactID == "" {
		m.setStatus(p, platform, expected, srvWeixinStatusError, "incoming message contact id is empty")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	instanceID, err := m.ensureInstance(ctx, p, platform)
	if err != nil {
		m.reply(ctx, p, expected, msg, fmt.Sprintf("%s channel is not ready: %s", platform, err.Error()))
		return
	}
	extra := map[string]string{"runtime": "maclawsrv"}
	for k, v := range msg.Extra {
		if strings.TrimSpace(v) != "" {
			extra[k] = v
		}
	}
	if msg.MediaType != "" {
		extra["media_type"] = msg.MediaType
	}
	if msg.MediaName != "" {
		extra["media_name"] = msg.MediaName
	}
	if msg.MimeType != "" {
		extra["mime_type"] = msg.MimeType
	}
	if msg.MediaBytes > 0 {
		extra["media_bytes"] = strconv.Itoa(msg.MediaBytes)
	}
	if asrOK {
		extra["asr_transcript"] = asrTranscript
		extra["asr_source"] = "maclawsrv"
	}
	clientMessageID := strings.TrimSpace(msg.ClientEventID)
	if clientMessageID == "" {
		clientMessageID = srvIMMessageID(platform, contactID, time.Now(), text, msg.MediaType, msg.MediaName, msg.MediaBytes)
	}
	metadata := agentservice.IMMessageMetadata(agentservice.IMMessageMetadataInput{Platform: platform, ContactID: contactID, Extra: extra})
	sendInput := agentservice.SendMessageInput{
		AgentID:          srvIMSessionAgent,
		Title:            msg.Title,
		Content:          text,
		InputType:        "text/plain",
		Metadata:         metadata,
		SessionMetadata:  metadata,
		ClientSessionKey: platform + ":" + contactID,
		ClientMessageID:  clientMessageID,
	}
	_, _, assistant, err := m.svc.SendMessage(ctx, p, instanceID, sendInput)
	if errors.Is(err, agentservice.ErrInstanceNotFound) {
		m.clearCachedInstanceID(p, platform)
		instanceID, retryErr := m.ensureInstance(ctx, p, platform)
		if retryErr != nil {
			err = retryErr
		} else {
			_, _, assistant, err = m.svc.SendMessage(ctx, p, instanceID, sendInput)
		}
	}
	if err != nil {
		m.reply(ctx, p, expected, msg, fmt.Sprintf("%s message failed: %s", platform, err.Error()))
		return
	}
	if assistant != nil && strings.TrimSpace(assistant.Content) != "" {
		m.reply(ctx, p, expected, msg, assistant.Content)
		m.replyVoiceIfEnabled(ctx, p, expected, msg, assistant.Content, cfg, normalizeSrvIMMediaType(msg.MediaType) == "voice")
	}
	_ = parent
}

func (m *srvIMGatewayManager) transcribeIncomingVoice(ctx context.Context, cfg *agentservice.UserConfig, msg srvIMIncomingMessage) (string, bool) {
	if m == nil || m.aiModels == nil || cfg == nil || normalizeSrvIMMediaType(msg.MediaType) != "voice" || len(msg.MediaData) == 0 {
		return "", false
	}
	wav, err := audioconv.ToWAV(msg.MediaData, srvIMAudioFormatHint(msg))
	if err != nil {
		return "", false
	}
	text, err := m.aiModels.transcribeWAV(ctx, cfg.AppConfig, wav)
	if err != nil {
		_ = m.aiModels.startDownload(srvAIModelASR, cfg.AppConfig, false)
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func (m *srvIMGatewayManager) replyVoiceIfEnabled(ctx context.Context, p agentservice.Principal, expected *srvIMRuntime, msg srvIMIncomingMessage, text string, cfg *agentservice.UserConfig, voiceReply bool) {
	if m == nil || m.aiModels == nil || cfg == nil || (!voiceReply && !cfg.AppConfig.TTSAutoVoiceSummary) || msg.ReplyMedia == nil {
		return
	}
	mp3, _, err := m.aiModels.synthesizeTextMP3(ctx, cfg.AppConfig, text)
	if err != nil {
		_ = m.aiModels.startDownload(srvAIModelTTS, cfg.AppConfig, false)
		return
	}
	m.replyMedia(ctx, p, expected, msg, mp3, "assistant.mp3", "file", "audio/mpeg")
}

func (m *srvIMGatewayManager) isCurrentRuntime(p agentservice.Principal, platform string, expected *srvIMRuntime) bool {
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	return expected != nil && m.runtimes[srvIMRuntimeKey(p, platform)] == expected
}

func (m *srvIMGatewayManager) ensureInstance(ctx context.Context, p agentservice.Principal, platform string) (string, error) {
	platform = normalizeSrvIMPlatform(platform)
	runtimeKey := "maclawsrv:im:" + platform
	if instanceID, ok := m.cachedInstanceID(p, platform); ok {
		return instanceID, nil
	}
	instances, err := m.svc.ListInstances(ctx, p)
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		if inst.Metadata != nil && inst.Metadata["im_runtime_key"] == runtimeKey {
			inst, err = srvSyncRuntimeIdentityInstance(ctx, m.svc, p, inst, instances, runtimeKey, platform, srvIMPlatformTitle(platform)+" Assistant", "MaClawSrv "+platform+" runtime")
			if err != nil {
				return "", err
			}
			if inst.Status == agentservice.InstanceStatusStopped {
				resumed, err := m.svc.ResumeInstance(ctx, p, inst.ID)
				if err != nil {
					return "", err
				}
				m.cacheInstanceID(p, platform, resumed.ID)
				return resumed.ID, nil
			}
			m.cacheInstanceID(p, platform, inst.ID)
			return inst.ID, nil
		}
	}
	inst, err := srvCreateRuntimeIdentityInstance(ctx, m.svc, p, instances, runtimeKey, platform, srvIMPlatformTitle(platform)+" Assistant", "MaClawSrv "+platform+" runtime")
	if err != nil {
		return "", err
	}
	m.cacheInstanceID(p, platform, inst.ID)
	return inst.ID, nil
}

func (m *srvIMGatewayManager) reply(ctx context.Context, p agentservice.Principal, expected *srvIMRuntime, msg srvIMIncomingMessage, text string) {
	platform := normalizeSrvIMPlatform(msg.Platform)
	if strings.TrimSpace(text) == "" || msg.Reply == nil || !m.isCurrentRuntime(p, platform, expected) {
		return
	}
	if err := msg.Reply(ctx, text); err != nil {
		m.setStatus(p, platform, expected, srvWeixinStatusError, err.Error())
	}
}

func (m *srvIMGatewayManager) replyMedia(ctx context.Context, p agentservice.Principal, expected *srvIMRuntime, msg srvIMIncomingMessage, data []byte, fileName, mediaType, mimeType string) {
	platform := normalizeSrvIMPlatform(msg.Platform)
	if len(data) == 0 || msg.ReplyMedia == nil || !m.isCurrentRuntime(p, platform, expected) {
		return
	}
	if err := msg.ReplyMedia(ctx, data, fileName, mediaType, mimeType); err != nil {
		m.setStatus(p, platform, expected, srvWeixinStatusError, err.Error())
	}
}

func srvIMRuntimeKey(p agentservice.Principal, platform string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + normalizeSrvIMPlatform(platform)
}

func normalizeSrvIMPlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func normalizeSrvIMMediaType(mediaType string) string {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch mediaType {
	case "audio", "audio/ogg", "audio/opus", "audio/wav", "audio/mpeg", "audio/mp3", "audio/mp4", "audio/m4a", "audio/x-m4a", "audio/aac", "ptt":
		return "voice"
	default:
		return mediaType
	}
}

func srvIMAudioFormatHint(msg srvIMIncomingMessage) string {
	for _, value := range []string{msg.MimeType, msg.MediaName, msg.MediaType} {
		value = strings.ToLower(strings.TrimSpace(value))
		switch {
		case strings.Contains(value, "silk"), strings.HasSuffix(value, ".silk"):
			return audioconv.FormatSilk
		case strings.Contains(value, "ogg"), strings.Contains(value, "opus"), strings.HasSuffix(value, ".ogg"), strings.HasSuffix(value, ".opus"), strings.HasSuffix(value, ".oga"):
			return audioconv.FormatOGG
		case strings.Contains(value, "wav"), strings.Contains(value, "wave"), strings.HasSuffix(value, ".wav"):
			return audioconv.FormatWAV
		case strings.Contains(value, "mpeg"), strings.Contains(value, "mp3"), strings.HasSuffix(value, ".mp3"):
			return audioconv.FormatMP3
		case strings.Contains(value, "m4a"), strings.Contains(value, "mp4"), strings.HasSuffix(value, ".m4a"):
			return audioconv.FormatM4A
		case strings.Contains(value, "aac"), strings.HasSuffix(value, ".aac"):
			return audioconv.FormatAAC
		}
	}
	return ""
}

func (m *srvIMGatewayManager) cachedInstanceID(p agentservice.Principal, platform string) (string, bool) {
	if m == nil {
		return "", false
	}
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil || strings.TrimSpace(runtime.instanceID) == "" || runtime.lastIdentitySyncAt.IsZero() {
		return "", false
	}
	if time.Since(runtime.lastIdentitySyncAt) > srvRuntimeIdentitySyncInterval {
		return "", false
	}
	return runtime.instanceID, true
}

func (m *srvIMGatewayManager) cacheInstanceID(p agentservice.Principal, platform, instanceID string) {
	if m == nil {
		return
	}
	platform = normalizeSrvIMPlatform(platform)
	instanceID = strings.TrimSpace(instanceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil {
		return
	}
	runtime.instanceID = instanceID
	runtime.lastIdentitySyncAt = time.Now().UTC()
}

func (m *srvIMGatewayManager) clearCachedInstanceID(p agentservice.Principal, platform string) {
	if m == nil {
		return
	}
	platform = normalizeSrvIMPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[srvIMRuntimeKey(p, platform)]
	if runtime == nil {
		return
	}
	runtime.instanceID = ""
	runtime.lastIdentitySyncAt = time.Time{}
}

func srvIMMessageID(platform, contactID string, ts time.Time, text, mediaType, mediaName string, mediaBytes int) string {
	n := ts.UnixNano()
	if n == 0 {
		n = time.Now().UnixNano()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{platform, contactID, text, mediaType, mediaName, strconv.Itoa(mediaBytes)}, "\x00")))
	return fmt.Sprintf("%s:%s:%d:%s", platform, contactID, n, hex.EncodeToString(sum[:8]))
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func srvIMPlatformTitle(platform string) string {
	switch strings.TrimSpace(strings.ToLower(platform)) {
	case "qq":
		return "QQ"
	case "telegram":
		return "Telegram"
	case "lansenger":
		return "Lansenger"
	default:
		platform = strings.TrimSpace(platform)
		if platform == "" {
			return "IM"
		}
		return strings.ToUpper(platform[:1]) + platform[1:]
	}
}

func srvCreateRuntimeIdentityInstance(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, instances []agentservice.Instance, runtimeKey, platform, fallbackName, fallbackDescription string) (*agentservice.Instance, error) {
	name, description, metadata := srvRuntimeIdentityProfile(instances, nil, runtimeKey, platform, fallbackName, fallbackDescription)
	return svc.CreateInstance(ctx, p, agentservice.CreateInstanceInput{
		Name:               name,
		Description:        description,
		AllowInvalidConfig: false,
		Metadata:           metadata,
	})
}

func srvSyncRuntimeIdentityInstance(ctx context.Context, svc *agentservice.Service, p agentservice.Principal, runtime agentservice.Instance, instances []agentservice.Instance, runtimeKey, platform, fallbackName, fallbackDescription string) (agentservice.Instance, error) {
	name, description, metadata := srvRuntimeIdentityProfile(instances, runtime.Metadata, runtimeKey, platform, fallbackName, fallbackDescription)
	update := agentservice.UpdateInstanceInput{}
	if strings.TrimSpace(runtime.Name) != name {
		update.Name = &name
	}
	if strings.TrimSpace(runtime.Description) != description {
		update.Description = &description
	}
	if !srvStringMapEqual(runtime.Metadata, metadata) {
		update.Metadata = metadata
	}
	if update.Name == nil && update.Description == nil && update.Metadata == nil {
		return runtime, nil
	}
	updated, err := svc.UpdateInstance(ctx, p, runtime.ID, update)
	if err != nil {
		return runtime, err
	}
	return *updated, nil
}

func srvRuntimeIdentityProfile(instances []agentservice.Instance, existingRuntimeMetadata map[string]string, runtimeKey, platform, fallbackName, fallbackDescription string) (string, string, map[string]string) {
	name := strings.TrimSpace(fallbackName)
	description := strings.TrimSpace(fallbackDescription)
	template := srvRuntimeIdentityTemplate(instances)
	if template != nil {
		if v := strings.TrimSpace(template.Name); v != "" {
			name = v
		}
		if v := strings.TrimSpace(template.Description); v != "" {
			description = v
		}
	}
	metadata := srvRuntimeIdentityMetadata(existingRuntimeMetadata, template, runtimeKey, platform)
	return name, description, metadata
}

func srvRuntimeIdentityMetadata(existingRuntimeMetadata map[string]string, template *agentservice.Instance, runtimeKey, platform string) map[string]string {
	metadata := map[string]string{}
	for key, value := range existingRuntimeMetadata {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" || strings.HasPrefix(key, "ve_") {
			continue
		}
		metadata[key] = value
	}
	metadata["im_runtime_key"] = strings.TrimSpace(runtimeKey)
	metadata["im_platform"] = strings.TrimSpace(platform)
	if template != nil {
		for key, value := range template.Metadata {
			if !strings.HasPrefix(key, "ve_") {
				continue
			}
			value = strings.TrimSpace(value)
			if value != "" {
				metadata[key] = value
			}
		}
	}
	return metadata
}

func srvRuntimeIdentityTemplate(instances []agentservice.Instance) *agentservice.Instance {
	candidates := make([]*agentservice.Instance, 0, len(instances))
	for i := range instances {
		inst := &instances[i]
		if inst.Metadata != nil && strings.TrimSpace(inst.Metadata["im_runtime_key"]) != "" {
			continue
		}
		if srvHasVirtualEmployeeIdentity(inst.Metadata) {
			candidates = append(candidates, inst)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			if candidates[i].CreatedAt.Equal(candidates[j].CreatedAt) {
				return candidates[i].ID > candidates[j].ID
			}
			return candidates[i].CreatedAt.After(candidates[j].CreatedAt)
		}
		return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
	})
	return candidates[0]
}

func srvHasVirtualEmployeeIdentity(metadata map[string]string) bool {
	for key, value := range metadata {
		if strings.HasPrefix(key, "ve_") && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func srvStringMapEqual(a, b map[string]string) bool {
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
