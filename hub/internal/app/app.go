package app

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/center"
	"github.com/RapidAI/CodeClaw/hub/internal/config"
	"github.com/RapidAI/CodeClaw/hub/internal/device"
	"github.com/RapidAI/CodeClaw/hub/internal/feishu"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/qqbot"
	"github.com/RapidAI/CodeClaw/hub/internal/mail"
	"github.com/RapidAI/CodeClaw/hub/internal/session"
	"github.com/RapidAI/CodeClaw/hub/internal/store/sqlite"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

type App struct {
	Config          *config.Config
	Provider        *sqlite.Provider
	AdminService    *auth.AdminService
	IdentityService *auth.IdentityService
	CenterService   *center.Service
	DeviceService   *device.Service
	SessionService  *session.Service
	Mailer          mail.Mailer
	WSGateway       *ws.Gateway
	HTTPHandler     http.Handler

	// IM modules (Agent Passthrough)
	MessageRouter    *im.MessageRouter
	IMAdapter        *im.Adapter
	FeishuPlugin     *feishu.FeishuPlugin
	OpenclawIMPlugin *im.WebhookIMPlugin
	QQBotPlugin      *qqbot.Plugin
}

func (a *App) StartBackgroundTasks() {
	if a.CenterService != nil {
		a.CenterService.StartBackgroundSync()
	}
}
