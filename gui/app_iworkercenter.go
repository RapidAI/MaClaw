package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type IWorkerCenterConfigRequest struct {
	URL                  string `json:"url"`
	TenantID             string `json:"tenant_id"`
	ColleagueID          string `json:"colleague_id"`
	GoalWatchIntervalSec int    `json:"goalwatch_interval_sec"`
	AutoStart            bool   `json:"auto_start"`
}

func NewIWorkerCenterClientFromAppConfig(cfg corelib.AppConfig, httpClient *http.Client) (*IWorkerCenterClient, error) {
	return NewIWorkerCenterClient(IWorkerCenterClientConfig{
		BaseURL:     cfg.IWorkerCenterURL,
		TenantID:    cfg.IWorkerCenterTenantID,
		ColleagueID: cfg.IWorkerCenterColleagueID,
		HTTPClient:  httpClient,
	})
}

func NewIWorkerGoalWatchServiceFromAppConfig(cfg corelib.AppConfig, httpClient *http.Client) (*IWorkerGoalWatchService, error) {
	client, err := NewIWorkerCenterClientFromAppConfig(cfg, httpClient)
	if err != nil {
		return nil, err
	}
	interval := time.Duration(cfg.IWorkerCenterGoalWatchIntervalSec) * time.Second
	runner := NewIWorkerGoalWatchRunner(IWorkerGoalWatchRunnerConfig{Recoverer: client})
	return NewIWorkerGoalWatchService(IWorkerGoalWatchServiceConfig{Runner: runner, Heartbeater: client, Interval: interval}), nil
}

func (a *App) NewIWorkerCenterClientFromConfig() (*IWorkerCenterClient, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	return NewIWorkerCenterClientFromAppConfig(cfg, nil)
}

func (a *App) RunIWorkerGoalWatchOnce() IWorkerGoalWatchRunStatus {
	service, err := a.ensureIWorkerGoalWatchService()
	if err != nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "iworkercenter_not_configured", Summary: IWorkerCenterRecoverySummary{Errors: []string{err.Error()}}}
	}
	return service.RunNow(context.Background())
}

func (a *App) StartIWorkerGoalWatch() map[string]any {
	service, err := a.ensureIWorkerGoalWatchService()
	if err != nil {
		return map[string]any{"started": false, "error": err.Error()}
	}
	started := service.Start(context.Background())
	return map[string]any{"started": started, "status": service.Status()}
}

func (a *App) StopIWorkerGoalWatch() map[string]any {
	if a == nil {
		return map[string]any{"stopped": false, "error": "app is nil"}
	}
	a.stopIWorkerGoalWatch()
	return map[string]any{"stopped": true}
}

func (a *App) IWorkerGoalWatchStatus() IWorkerGoalWatchRunStatus {
	if a == nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "app_is_nil"}
	}
	a.iworkerGoalWatchMu.Lock()
	service := a.iworkerGoalWatch
	a.iworkerGoalWatchMu.Unlock()
	if service == nil {
		return IWorkerGoalWatchRunStatus{SkippedReason: "goalwatch_service_not_started"}
	}
	return service.Status()
}

func (a *App) SaveIWorkerCenterConfig(req IWorkerCenterConfigRequest) map[string]any {
	if a == nil {
		return map[string]any{"ok": false, "error": "app is nil"}
	}
	url := strings.TrimRight(strings.TrimSpace(req.URL), "/")
	tenantID := strings.TrimSpace(req.TenantID)
	colleagueID := strings.TrimSpace(req.ColleagueID)
	intervalSec := req.GoalWatchIntervalSec
	if intervalSec < 0 {
		intervalSec = 0
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	cfg.IWorkerCenterURL = url
	cfg.IWorkerCenterTenantID = tenantID
	cfg.IWorkerCenterColleagueID = colleagueID
	cfg.IWorkerCenterGoalWatchIntervalSec = intervalSec
	if err := a.SaveConfig(cfg); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}

	a.stopIWorkerGoalWatch()
	if req.AutoStart && isIWorkerCenterConfigured(cfg) {
		service, err := NewIWorkerGoalWatchServiceFromAppConfig(cfg, nil)
		if err != nil {
			return map[string]any{"ok": false, "error": err.Error(), "configured": true}
		}
		a.iworkerGoalWatchMu.Lock()
		a.iworkerGoalWatch = service
		a.iworkerGoalWatchMu.Unlock()
		service.Start(context.Background())
	}
	return map[string]any{"ok": true, "configured": isIWorkerCenterConfigured(cfg), "status": a.IWorkerGoalWatchStatus()}
}
func (a *App) IWorkerCenterConfigStatus() map[string]any {
	status := map[string]any{"configured": false}
	if a == nil {
		status["error"] = "app is nil"
		return status
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		status["error"] = err.Error()
		return status
	}
	status["iworkercenter_url"] = strings.TrimSpace(cfg.IWorkerCenterURL)
	status["tenant_id"] = strings.TrimSpace(cfg.IWorkerCenterTenantID)
	status["colleague_id"] = strings.TrimSpace(cfg.IWorkerCenterColleagueID)
	status["goalwatch_interval_sec"] = normalizedIWorkerGoalWatchIntervalSec(cfg)
	configured := isIWorkerCenterConfigured(cfg)
	status["configured"] = configured
	if !configured {
		return status
	}

	client, err := NewIWorkerCenterClientFromAppConfig(cfg, nil)
	if err != nil {
		status["center_service"] = map[string]any{"ok": false, "error": err.Error()}
		return status
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	serviceStatus, err := client.ValidateServiceIdentity(ctx)
	if err != nil {
		status["center_service"] = map[string]any{"ok": false, "error": err.Error(), "status": serviceStatus}
		return status
	}
	status["center_service"] = map[string]any{"ok": true, "status": serviceStatus}
	return status
}

func (a *App) startIWorkerGoalWatchIfConfigured(cfg corelib.AppConfig) {
	if a == nil || !isIWorkerCenterConfigured(cfg) {
		return
	}
	service, err := NewIWorkerGoalWatchServiceFromAppConfig(cfg, nil)
	if err != nil {
		return
	}
	a.iworkerGoalWatchMu.Lock()
	if a.iworkerGoalWatch != nil {
		a.iworkerGoalWatchMu.Unlock()
		return
	}
	a.iworkerGoalWatch = service
	a.iworkerGoalWatchMu.Unlock()
	service.Start(context.Background())
}

func (a *App) ensureIWorkerGoalWatchService() (*IWorkerGoalWatchService, error) {
	if a == nil {
		return nil, fmt.Errorf("app is nil")
	}
	a.iworkerGoalWatchMu.Lock()
	if a.iworkerGoalWatch != nil {
		service := a.iworkerGoalWatch
		a.iworkerGoalWatchMu.Unlock()
		return service, nil
	}
	a.iworkerGoalWatchMu.Unlock()

	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	service, err := NewIWorkerGoalWatchServiceFromAppConfig(cfg, nil)
	if err != nil {
		return nil, err
	}
	a.iworkerGoalWatchMu.Lock()
	if a.iworkerGoalWatch == nil {
		a.iworkerGoalWatch = service
	} else {
		service = a.iworkerGoalWatch
	}
	a.iworkerGoalWatchMu.Unlock()
	return service, nil
}

func (a *App) stopIWorkerGoalWatch() {
	if a == nil {
		return
	}
	a.iworkerGoalWatchMu.Lock()
	service := a.iworkerGoalWatch
	a.iworkerGoalWatch = nil
	a.iworkerGoalWatchMu.Unlock()
	if service != nil {
		service.Stop()
	}
}

func isIWorkerCenterConfigured(cfg corelib.AppConfig) bool {
	return strings.TrimSpace(cfg.IWorkerCenterURL) != "" && strings.TrimSpace(cfg.IWorkerCenterTenantID) != "" && strings.TrimSpace(cfg.IWorkerCenterColleagueID) != ""
}

func normalizedIWorkerGoalWatchIntervalSec(cfg corelib.AppConfig) int {
	if cfg.IWorkerCenterGoalWatchIntervalSec > 0 {
		return cfg.IWorkerCenterGoalWatchIntervalSec
	}
	return int(defaultIWorkerGoalWatchInterval.Seconds())
}
