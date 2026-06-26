package app

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/chat"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/dingtalk"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/wecom"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type App struct {
	Config          *config.Config
	ConfigPath      string // path to YAML config file (for runtime updates)
	Provider        *sqlite.Provider
	AdminService    *auth.AdminService
	IdentityService *auth.IdentityService
	CenterService   *center.Service
	DeviceService   *device.Service
	SessionService  *session.Service
	Mailer          mail.Mailer
	WSGateway       *ws.Gateway
	HTTPHandler     http.Handler
	KnowledgeShares store.KnowledgeShareRepository

	// IM modules (Agent Passthrough)
	MessageRouter    *im.MessageRouter
	IMAdapter        *im.Adapter
	FeishuPlugin     *feishu.FeishuPlugin
	OpenclawIMPlugin *im.WebhookIMPlugin
	QQBotPlugin      *qqbot.Plugin
	WecomPlugin      *wecom.Plugin
	DingTalkPlugin   *dingtalk.Plugin
	QQRemotePlugin   *im.RemoteGatewayPlugin
	TelegramPlugin   *im.RemoteGatewayPlugin

	// Chat module
	ChatNotifier *chat.Notifier
}

func (a *App) StartBackgroundTasks() {
	if a.CenterService != nil {
		a.CenterService.StartBackgroundSync()
	}
	if a.KnowledgeShares != nil {
		go a.runKnowledgeShareExpiryCleanup()
	}
}

func (a *App) runKnowledgeShareExpiryCleanup() {
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		deleted, err := a.KnowledgeShares.DeleteExpired(ctx, time.Now().UTC())
		if err != nil {
			log.Printf("[knowledge-shares] expiry cleanup failed: %v", err)
			return
		}
		if deleted > 0 {
			log.Printf("[knowledge-shares] expired shares deleted: %d", deleted)
		}
	}
	cleanup()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cleanup()
	}
}
