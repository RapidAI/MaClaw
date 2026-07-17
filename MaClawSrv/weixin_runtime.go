package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/audioconv"
	"github.com/RapidAI/CodeClaw/corelib/scheduler"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
)

const (
	srvWeixinStatusDisabled       = "disabled"
	srvWeixinStatusDisconnected   = "disconnected"
	srvWeixinStatusConnecting     = "connecting"
	srvWeixinStatusConnected      = "connected"
	srvWeixinStatusReconnecting   = "reconnecting"
	srvWeixinStatusSessionExpired = "session_expired"
	srvWeixinStatusError          = "error"
)

const (
	srvWeixinInstanceKey  = "maclawsrv:im:weixin"
	srvWeixinSessionAgent = "default"
)

type srvWeixinGateway interface {
	Start(context.Context) error
	Stop() error
	SendText(context.Context, weixin.OutgoingText) error
	SendMedia(context.Context, weixin.OutgoingMedia) error
	GetContextToken(string) string
	SetStatusCallback(weixin.StatusCallback)
}

type srvWeixinGatewayFactory func(weixin.Config, weixin.MessageHandler) srvWeixinGateway

type srvWeixinRuntimeStatus struct {
	Status    string    `json:"status"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type srvWeixinRuntime struct {
	principal          agentservice.Principal
	config             weixin.Config
	gateway            srvWeixinGateway
	status             srvWeixinRuntimeStatus
	instanceID         string
	lastIdentitySyncAt time.Time
	// lastUserID is the most recent private chat peer (for scheduled proactive push).
	lastUserID string
}

type srvWeixinGatewayManager struct {
	svc      *agentservice.Service
	aiModels *srvAIModelManager
	factory  srvWeixinGatewayFactory

	mu       sync.Mutex
	runtimes map[string]*srvWeixinRuntime
}

func newSrvWeixinGatewayManager(svc *agentservice.Service, aiModels ...*srvAIModelManager) *srvWeixinGatewayManager {
	var mgr *srvAIModelManager
	if len(aiModels) > 0 {
		mgr = aiModels[0]
	}
	return &srvWeixinGatewayManager{
		svc:      svc,
		aiModels: mgr,
		factory: func(cfg weixin.Config, handler weixin.MessageHandler) srvWeixinGateway {
			return weixin.NewGateway(cfg, handler)
		},
		runtimes: map[string]*srvWeixinRuntime{},
	}
}

func (m *srvWeixinGatewayManager) SyncPrincipal(ctx context.Context, p agentservice.Principal, cfg corelib.AppConfig) {
	if m == nil {
		return
	}
	token := strings.TrimSpace(cfg.WeixinToken)
	if !cfg.WeixinEnabled || token == "" {
		m.stopPrincipal(p, srvWeixinStatusDisabled, "")
		return
	}
	next := weixin.Config{
		Token:     token,
		BaseURL:   fallbackString(strings.TrimSpace(cfg.WeixinBaseURL), weixin.DefaultBaseURL),
		CDNURL:    fallbackString(strings.TrimSpace(cfg.WeixinCDNURL), weixin.DefaultCDNBaseURL),
		AccountID: strings.TrimSpace(cfg.WeixinAccountID),
	}

	key := principalRuntimeKey(p)
	m.mu.Lock()
	current := m.runtimes[key]
	if current != nil && sameWeixinRuntimeConfig(current.config, next) {
		if current.status.Status == srvWeixinStatusError && m.factory != nil {
			old := current
			delete(m.runtimes, key)
			m.mu.Unlock()
			stopSrvWeixinGateway(old.gateway)
		} else {
			m.mu.Unlock()
			return
		}
		m.mu.Lock()
		current = m.runtimes[key]
		if current != nil && sameWeixinRuntimeConfig(current.config, next) {
			m.mu.Unlock()
			return
		}
	}
	if m.factory == nil {
		old := current
		m.runtimes[key] = &srvWeixinRuntime{
			principal: p,
			config:    next,
			status:    srvWeixinRuntimeStatus{Status: srvWeixinStatusError, LastError: "gateway factory is not available", UpdatedAt: time.Now().UTC()},
		}
		m.mu.Unlock()
		if old != nil {
			stopSrvWeixinGateway(old.gateway)
		}
		return
	}
	old := current
	runtime := &srvWeixinRuntime{
		principal: p,
		config:    next,
		status:    srvWeixinRuntimeStatus{Status: srvWeixinStatusConnecting, UpdatedAt: time.Now().UTC()},
	}
	m.runtimes[key] = runtime
	m.mu.Unlock()

	if old != nil {
		stopSrvWeixinGateway(old.gateway)
	}
	gateway := m.factory(next, func(msg weixin.IncomingMessage) {
		m.handleIncomingMessage(ctx, p, runtime, msg)
	})
	if gateway == nil {
		m.mu.Lock()
		if m.runtimes[key] == runtime {
			runtime.status.Status = srvWeixinStatusError
			runtime.status.LastError = "gateway factory returned nil"
			runtime.status.UpdatedAt = time.Now().UTC()
		}
		m.mu.Unlock()
		return
	}
	gateway.SetStatusCallback(func(status string) {
		m.setStatus(p, runtime, normalizeSrvWeixinStatus(status), "")
	})
	m.mu.Lock()
	stale := m.runtimes[key] != runtime
	if !stale {
		runtime.gateway = gateway
	}
	m.mu.Unlock()
	if stale {
		stopSrvWeixinGateway(gateway)
		return
	}
	if err := gateway.Start(context.Background()); err != nil {
		stopSrvWeixinGateway(gateway)
		m.setStatus(p, runtime, srvWeixinStatusError, err.Error())
		return
	}
	if !m.isCurrentRuntime(p, runtime) {
		stopSrvWeixinGateway(gateway)
		return
	}
	m.markConnectedIfConnecting(p, runtime)
}

func (m *srvWeixinGatewayManager) Status(p agentservice.Principal) srvWeixinRuntimeStatus {
	if m == nil {
		return srvWeixinRuntimeStatus{Status: srvWeixinStatusDisabled}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil {
		return srvWeixinRuntimeStatus{Status: srvWeixinStatusDisabled}
	}
	return runtime.status
}

func (m *srvWeixinGatewayManager) RestartPrincipal(ctx context.Context, p agentservice.Principal, cfg corelib.AppConfig) {
	if m == nil {
		return
	}
	m.stopPrincipal(p, srvWeixinStatusDisconnected, "")
	m.SyncPrincipal(ctx, p, cfg)
}

func (m *srvWeixinGatewayManager) StopPrincipal(p agentservice.Principal) {
	if m == nil {
		return
	}
	m.stopPrincipal(p, srvWeixinStatusDisabled, "")
}

func (m *srvWeixinGatewayManager) StopAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	runtimes := make([]*srvWeixinRuntime, 0, len(m.runtimes))
	for _, runtime := range m.runtimes {
		runtimes = append(runtimes, runtime)
	}
	m.runtimes = map[string]*srvWeixinRuntime{}
	m.mu.Unlock()
	for _, runtime := range runtimes {
		stopSrvWeixinGateway(runtime.gateway)
	}
}

func (m *srvWeixinGatewayManager) stopPrincipal(p agentservice.Principal, status, lastError string) {
	key := principalRuntimeKey(p)
	m.mu.Lock()
	runtime := m.runtimes[key]
	if runtime != nil {
		delete(m.runtimes, key)
	}
	m.mu.Unlock()
	if runtime != nil {
		stopSrvWeixinGateway(runtime.gateway)
	}
}

func stopSrvWeixinGateway(gateway srvWeixinGateway) {
	if gateway == nil {
		return
	}
	gateway.SetStatusCallback(func(string) {})
	_ = gateway.Stop()
}

func (m *srvWeixinGatewayManager) setStatus(p agentservice.Principal, expected *srvWeixinRuntime, status, lastError string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil || runtime != expected {
		return
	}
	runtime.status.Status = status
	runtime.status.LastError = strings.TrimSpace(lastError)
	runtime.status.UpdatedAt = time.Now().UTC()
}

func (m *srvWeixinGatewayManager) markConnectedIfConnecting(p agentservice.Principal, expected *srvWeixinRuntime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil || runtime != expected || runtime.status.Status != srvWeixinStatusConnecting {
		return
	}
	runtime.status.Status = srvWeixinStatusConnected
	runtime.status.LastError = ""
	runtime.status.UpdatedAt = time.Now().UTC()
}

func (m *srvWeixinGatewayManager) handleIncomingMessage(parent context.Context, p agentservice.Principal, expected *srvWeixinRuntime, msg weixin.IncomingMessage) {
	if !m.isCurrentRuntime(p, expected) {
		return
	}
	// Remember last private peer for scheduled-task proactive delivery (user_id=self).
	if uid := strings.TrimSpace(msg.FromUserID); uid != "" {
		m.mu.Lock()
		if m.runtimes[principalRuntimeKey(p)] == expected {
			expected.lastUserID = uid
		}
		m.mu.Unlock()
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	instanceID, err := m.ensureWeixinInstance(ctx, p)
	if err != nil {
		m.reply(ctx, p, expected, msg, "WeChat channel is not ready: "+err.Error())
		return
	}
	metadata := agentservice.IMMessageMetadata(agentservice.IMMessageMetadataInput{
		Platform:  "weixin",
		ContactID: msg.FromUserID,
		Extra: map[string]string{
			"context_token": msg.ContextToken,
			"runtime":       "maclawsrv",
			"media_type":    msg.MediaType,
		},
	})
	if asrOK {
		metadata["asr_transcript"] = asrTranscript
		metadata["asr_source"] = "maclawsrv"
	}
	sendInput := agentservice.SendMessageInput{
		AgentID:          srvWeixinSessionAgent,
		Title:            "WeChat " + msg.FromUserID,
		Content:          text,
		InputType:        "text/plain",
		Metadata:         metadata,
		SessionMetadata:  metadata,
		ClientSessionKey: "weixin:" + msg.FromUserID,
		ClientMessageID:  srvWeixinClientMessageID(msg, text),
	}
	_, _, assistant, err := m.svc.SendMessage(ctx, p, instanceID, sendInput)
	if errors.Is(err, agentservice.ErrInstanceNotFound) {
		m.clearCachedInstanceID(p)
		instanceID, retryErr := m.ensureWeixinInstance(ctx, p)
		if retryErr != nil {
			err = retryErr
		} else {
			_, _, assistant, err = m.svc.SendMessage(ctx, p, instanceID, sendInput)
		}
	}
	if err != nil {
		m.reply(ctx, p, expected, msg, "WeChat message failed: "+err.Error())
		return
	}
	if assistant != nil && strings.TrimSpace(assistant.Content) != "" {
		m.reply(ctx, p, expected, msg, assistant.Content)
		m.replyVoiceIfEnabled(ctx, p, expected, msg, assistant.Content, cfg, srvWeixinIncomingIsVoice(msg))
	}
	_ = parent
}

func (m *srvWeixinGatewayManager) transcribeIncomingVoice(ctx context.Context, cfg *agentservice.UserConfig, msg weixin.IncomingMessage) (string, bool) {
	if m == nil || m.aiModels == nil || cfg == nil || !srvWeixinIncomingIsVoice(msg) || len(msg.MediaData) == 0 {
		return "", false
	}
	wav, err := audioconv.ToWAV(msg.MediaData, srvWeixinAudioFormatHint(msg))
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

func (m *srvWeixinGatewayManager) replyVoiceIfEnabled(ctx context.Context, p agentservice.Principal, expected *srvWeixinRuntime, msg weixin.IncomingMessage, text string, cfg *agentservice.UserConfig, voiceReply bool) {
	if m == nil || m.aiModels == nil || cfg == nil || (!voiceReply && !cfg.AppConfig.TTSAutoVoiceSummary) {
		return
	}
	mp3, _, err := m.aiModels.synthesizeTextMP3(ctx, cfg.AppConfig, text)
	if err != nil {
		_ = m.aiModels.startDownload(srvAIModelTTS, cfg.AppConfig, false)
		return
	}
	m.replyVoiceFile(ctx, p, expected, msg, mp3)
}

func srvWeixinAudioFormatHint(msg weixin.IncomingMessage) string {
	for _, value := range []string{msg.MediaName, msg.MediaType} {
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

func srvWeixinIncomingIsVoice(msg weixin.IncomingMessage) bool {
	if strings.EqualFold(strings.TrimSpace(msg.MediaType), "voice") {
		return true
	}
	return normalizeSrvIMMediaType(msg.MediaType) == "voice"
}

func (m *srvWeixinGatewayManager) isCurrentRuntime(p agentservice.Principal, expected *srvWeixinRuntime) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return expected != nil && m.runtimes[principalRuntimeKey(p)] == expected
}

func (m *srvWeixinGatewayManager) ensureWeixinInstance(ctx context.Context, p agentservice.Principal) (string, error) {
	if instanceID, ok := m.cachedInstanceID(p); ok {
		return instanceID, nil
	}
	instances, err := m.svc.ListInstances(ctx, p)
	if err != nil {
		return "", err
	}
	for _, inst := range instances {
		if inst.Metadata != nil && inst.Metadata["im_runtime_key"] == srvWeixinInstanceKey {
			inst, err = srvSyncRuntimeIdentityInstance(ctx, m.svc, p, inst, instances, srvWeixinInstanceKey, "weixin", "WeChat Assistant", "MaClawSrv WeChat runtime")
			if err != nil {
				return "", err
			}
			if inst.Status == agentservice.InstanceStatusStopped {
				resumed, err := m.svc.ResumeInstance(ctx, p, inst.ID)
				if err != nil {
					return "", err
				}
				m.cacheInstanceID(p, resumed.ID)
				return resumed.ID, nil
			}
			m.cacheInstanceID(p, inst.ID)
			return inst.ID, nil
		}
	}
	inst, err := srvCreateRuntimeIdentityInstance(ctx, m.svc, p, instances, srvWeixinInstanceKey, "weixin", "WeChat Assistant", "MaClawSrv WeChat runtime")
	if err != nil {
		return "", err
	}
	m.cacheInstanceID(p, inst.ID)
	return inst.ID, nil
}

func (m *srvWeixinGatewayManager) reply(ctx context.Context, p agentservice.Principal, expected *srvWeixinRuntime, msg weixin.IncomingMessage, text string) {
	m.mu.Lock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	m.mu.Unlock()
	if runtime == nil || runtime != expected || runtime.gateway == nil || strings.TrimSpace(text) == "" {
		return
	}
	contextToken := msg.ContextToken
	if contextToken == "" {
		contextToken = runtime.gateway.GetContextToken(msg.FromUserID)
	}
	if contextToken == "" {
		return
	}
	if err := runtime.gateway.SendText(ctx, weixin.OutgoingText{ToUserID: msg.FromUserID, Text: text, ContextToken: contextToken}); err != nil {
		m.setStatus(p, runtime, srvWeixinStatusError, err.Error())
	}
}

// ListProactivePeers returns known private peers across connected WeChat runtimes
// (for list_targets / admin delivery-targets catalog).
func (m *srvWeixinGatewayManager) ListProactivePeers() []scheduler.TargetRef {
	if m == nil {
		return nil
	}
	var out []scheduler.TargetRef
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rt := range m.runtimes {
		if rt == nil {
			continue
		}
		st := strings.TrimSpace(rt.status.Status)
		if st != srvWeixinStatusConnected && st != srvWeixinStatusConnecting {
			continue
		}
		uid := strings.TrimSpace(rt.lastUserID)
		if uid == "" {
			continue
		}
		out = append(out, scheduler.TargetRef{
			Kind:    scheduler.DeliveryKindUser,
			ID:      uid,
			Name:    "微信私聊 " + uid,
			Channel: scheduler.DeliveryChannelWeixin,
		})
	}
	return out
}

// SendProactiveTextAny pushes text to the most recently active WeChat private
// session among connected runtimes. Used by scheduled-task delivery (channel=weixin).
// Returns the peer user id on success (for DeliveryStateStore memory).
func (m *srvWeixinGatewayManager) SendProactiveTextAny(text string) (peer string, err error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("empty proactive text")
	}
	if m == nil {
		return "", fmt.Errorf("weixin runtime manager is nil")
	}
	type candidate struct {
		gw     srvWeixinGateway
		userID string
		p      agentservice.Principal
		rt     *srvWeixinRuntime
	}
	var cands []candidate
	m.mu.Lock()
	for _, rt := range m.runtimes {
		if rt == nil || rt.gateway == nil {
			continue
		}
		st := strings.TrimSpace(rt.status.Status)
		if st != srvWeixinStatusConnected && st != srvWeixinStatusConnecting {
			continue
		}
		uid := strings.TrimSpace(rt.lastUserID)
		if uid == "" {
			continue
		}
		cands = append(cands, candidate{gw: rt.gateway, userID: uid, p: rt.principal, rt: rt})
	}
	m.mu.Unlock()
	if len(cands) == 0 {
		return "", fmt.Errorf("no active weixin session (先用微信私聊机器人一次)")
	}
	var lastErr error
	for _, c := range cands {
		tok := strings.TrimSpace(c.gw.GetContextToken(c.userID))
		if tok == "" {
			lastErr = fmt.Errorf("weixin: no context token for %s", c.userID)
			continue
		}
		if err := c.gw.SendText(context.Background(), weixin.OutgoingText{
			ToUserID:     c.userID,
			Text:         text,
			ContextToken: tok,
		}); err != nil {
			lastErr = err
			m.setStatus(c.p, c.rt, srvWeixinStatusError, err.Error())
			continue
		}
		return c.userID, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("weixin proactive send failed")
}

func (m *srvWeixinGatewayManager) replyVoiceFile(ctx context.Context, p agentservice.Principal, expected *srvWeixinRuntime, msg weixin.IncomingMessage, mp3 []byte) {
	m.mu.Lock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	m.mu.Unlock()
	if runtime == nil || runtime != expected || runtime.gateway == nil || len(mp3) == 0 {
		return
	}
	contextToken := msg.ContextToken
	if contextToken == "" {
		contextToken = runtime.gateway.GetContextToken(msg.FromUserID)
	}
	if contextToken == "" {
		return
	}
	err := runtime.gateway.SendMedia(ctx, weixin.OutgoingMedia{ToUserID: msg.FromUserID, ContextToken: contextToken, FileData: mp3, FileName: "assistant.mp3", MediaType: "file"})
	if err != nil {
		m.setStatus(p, runtime, srvWeixinStatusError, err.Error())
	}
}

func principalRuntimeKey(p agentservice.Principal) string {
	return p.TenantID + "\x00" + p.UserID
}

func (m *srvWeixinGatewayManager) cachedInstanceID(p agentservice.Principal) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil || strings.TrimSpace(runtime.instanceID) == "" || runtime.lastIdentitySyncAt.IsZero() {
		return "", false
	}
	if time.Since(runtime.lastIdentitySyncAt) > srvRuntimeIdentitySyncInterval {
		return "", false
	}
	return runtime.instanceID, true
}

func (m *srvWeixinGatewayManager) cacheInstanceID(p agentservice.Principal, instanceID string) {
	if m == nil {
		return
	}
	instanceID = strings.TrimSpace(instanceID)
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil {
		return
	}
	runtime.instanceID = instanceID
	runtime.lastIdentitySyncAt = time.Now().UTC()
}

func (m *srvWeixinGatewayManager) clearCachedInstanceID(p agentservice.Principal) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	runtime := m.runtimes[principalRuntimeKey(p)]
	if runtime == nil {
		return
	}
	runtime.instanceID = ""
	runtime.lastIdentitySyncAt = time.Time{}
}

func sameWeixinRuntimeConfig(a, b weixin.Config) bool {
	return a.Token == b.Token && a.BaseURL == b.BaseURL && a.CDNURL == b.CDNURL && a.AccountID == b.AccountID
}

func normalizeSrvWeixinStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case srvWeixinStatusConnecting:
		return srvWeixinStatusConnecting
	case srvWeixinStatusConnected:
		return srvWeixinStatusConnected
	case srvWeixinStatusReconnecting:
		return srvWeixinStatusReconnecting
	case srvWeixinStatusSessionExpired:
		return srvWeixinStatusSessionExpired
	case srvWeixinStatusDisconnected:
		return srvWeixinStatusDisconnected
	case srvWeixinStatusError:
		return srvWeixinStatusError
	default:
		if strings.TrimSpace(status) == "" {
			return srvWeixinStatusDisconnected
		}
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func srvWeixinClientMessageID(msg weixin.IncomingMessage, text string) string {
	ts := msg.Timestamp.UnixNano()
	if ts == 0 {
		ts = time.Now().UnixNano()
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		msg.FromUserID,
		msg.ContextToken,
		msg.MediaType,
		msg.MediaName,
		fmt.Sprintf("%d", len(msg.MediaData)),
		text,
	}, "\x00")))
	return fmt.Sprintf("weixin:%s:%d:%s", msg.FromUserID, ts, hex.EncodeToString(sum[:8]))
}
