package app

import (
	"context"
	"net/http"
	"sync"
	"time"

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

	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error
}

func (a *App) goBackground(fn func(context.Context)) {
	if a == nil || fn == nil {
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		fn(ctx)
	}()
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		done := make(chan struct{})
		go func() {
			a.wg.Wait()
			close(done)
		}()
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
		}
		if a.Provider != nil {
			a.closeErr = a.Provider.Close()
		}
	})
	return a.closeErr
}
