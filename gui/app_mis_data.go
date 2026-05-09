package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

type MISDataConnectionStatus struct {
	OK            bool   `json:"ok"`
	AuthOK        bool   `json:"auth_ok"`
	Endpoint      string `json:"endpoint"`
	Status        string `json:"status,omitempty"`
	Engine        string `json:"engine,omitempty"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	Error         string `json:"error,omitempty"`
}

func (a *App) GetMISDataConfig() (corelib.MISDataConfig, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return corelib.MISDataConfig{}.WithDefaults(), err
	}
	return cfg.MISData.WithDefaults(), nil
}

func (a *App) SaveMISDataConfig(next corelib.MISDataConfig) error {
	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.MISData = normalizeMISDataConfig(next)
	return a.SaveConfig(cfg)
}

func (a *App) TestMISDataConnection(next corelib.MISDataConfig) (MISDataConnectionStatus, error) {
	cfg := normalizeMISDataConfig(next)
	status := MISDataConnectionStatus{Endpoint: cfg.Endpoint}
	client := &http.Client{Timeout: 8 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	readyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Endpoint, "/")+"/readyz", nil)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	readyResp, err := client.Do(readyReq)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	defer readyResp.Body.Close()
	if readyResp.StatusCode < 200 || readyResp.StatusCode >= 300 {
		status.Error = fmt.Sprintf("readyz returned HTTP %d", readyResp.StatusCode)
		return status, nil
	}
	var ready struct {
		Status        string `json:"status"`
		Engine        string `json:"engine"`
		SchemaVersion int    `json:"schema_version"`
	}
	_ = json.NewDecoder(readyResp.Body).Decode(&ready)
	status.OK = true
	status.Status = ready.Status
	status.Engine = ready.Engine
	status.SchemaVersion = ready.SchemaVersion
	if strings.TrimSpace(cfg.Token) == "" {
		status.Error = "token is empty"
		return status, nil
	}

	authReq, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.Endpoint, "/")+"/api/v1/data/backups", nil)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	authReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	authReq.Header.Set("X-MaClaw-Tenant-ID", cfg.TenantID)
	authReq.Header.Set("X-MaClaw-User-ID", cfg.UserID)
	authReq.Header.Set("X-MaClaw-Role", cfg.Role)
	authResp, err := client.Do(authReq)
	if err != nil {
		status.Error = err.Error()
		return status, nil
	}
	defer authResp.Body.Close()
	status.AuthOK = authResp.StatusCode >= 200 && authResp.StatusCode < 300
	if !status.AuthOK {
		status.Error = fmt.Sprintf("authenticated API returned HTTP %d", authResp.StatusCode)
	}
	return status, nil
}

func normalizeMISDataConfig(cfg corelib.MISDataConfig) corelib.MISDataConfig {
	cfg = cfg.WithDefaults()
	cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	cfg.Token = strings.TrimSpace(cfg.Token)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.UserID = strings.TrimSpace(cfg.UserID)
	cfg.Role = strings.ToLower(strings.TrimSpace(cfg.Role))
	return cfg.WithDefaults()
}
