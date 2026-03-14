package app

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/hubcenter/internal/auth"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/config"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/entry"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/hubs"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/mail"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/store/sqlite"
)

type App struct {
	Config       *config.Config
	Provider     *sqlite.Provider
	Store        *store.Store
	AdminService *auth.AdminService
	HubService   *hubs.Service
	EntryService *entry.Service
	Mailer       *mail.Service
	HTTPHandler  http.Handler
}
