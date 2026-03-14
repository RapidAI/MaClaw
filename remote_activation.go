package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type RemoteActivationResult struct {
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
}

type RemoteActivationStatus struct {
	Activated bool   `json:"activated"`
	Email     string `json:"email"`
	SN        string `json:"sn"`
	MachineID string `json:"machine_id"`
	HubURL    string `json:"hub_url"`
}

type RemoteHubCenterHub struct {
	HubID          string `json:"hub_id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	PWAURL         string `json:"pwa_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
	Status         string `json:"status"`
}

type hubCenterResolveResult struct {
	Email        string                `json:"email"`
	Mode         string                `json:"mode"`
	DefaultHubID string                `json:"default_hub_id,omitempty"`
	DefaultPWA   string                `json:"default_pwa_url,omitempty"`
	Hubs         []hubCenterResolveHub `json:"hubs,omitempty"`
	Message      string                `json:"message,omitempty"`
}

type hubCenterResolveHub struct {
	HubID          string `json:"hub_id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	PWAURL         string `json:"pwa_url"`
	Visibility     string `json:"visibility"`
	EnrollmentMode string `json:"enrollment_mode"`
	Status         string `json:"status"`
}

func (a *App) ActivateRemote(email string) (RemoteActivationResult, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationResult{}, err
	}

	email = strings.TrimSpace(email)
	if email == "" {
		return RemoteActivationResult{}, fmt.Errorf("email is required")
	}

	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if hubURL == "" {
		hubURL, err = a.resolveRemoteHubURL(cfg, email)
		if err != nil {
			return RemoteActivationResult{}, err
		}
		cfg.RemoteHubURL = hubURL
	}

	profile := a.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	body := map[string]any{
		"email":        email,
		"machine_name": profile.Name,
		"platform":     profile.Platform,
		"hostname":     profile.Hostname,
		"arch":         profile.Arch,
		"app_version":  profile.AppVersion,
	}
	body["heartbeat_interval_sec"] = profile.HeartbeatSec
	data, err := json.Marshal(body)
	if err != nil {
		return RemoteActivationResult{}, err
	}

	resp, err := http.Post(strings.TrimRight(hubURL, "/")+"/api/enroll/start", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteActivationResult{}, err
	}
	defer resp.Body.Close()

	var result RemoteActivationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RemoteActivationResult{}, err
	}
	if resp.StatusCode >= 300 {
		if result.Message != "" {
			return RemoteActivationResult{}, fmt.Errorf("%s", result.Message)
		}
		return RemoteActivationResult{}, fmt.Errorf("remote activation failed: %s", resp.Status)
	}

	cfg.RemoteEmail = result.Email
	cfg.RemoteSN = result.SN
	cfg.RemoteUserID = result.UserID
	cfg.RemoteMachineID = result.MachineID
	cfg.RemoteMachineToken = result.MachineToken
	cfg.RemoteHubURL = hubURL
	if err := a.SaveConfig(cfg); err != nil {
		return RemoteActivationResult{}, err
	}

	if a.remoteSessions == nil {
		a.remoteSessions = NewRemoteSessionManager(a)
	}
	hubClient := a.remoteSessions.hubClient
	if hubClient == nil {
		hubClient = NewRemoteHubClient(a, a.remoteSessions)
		a.remoteSessions.SetHubClient(hubClient)
	}
	if !hubClient.IsConnected() {
		_ = hubClient.Connect()
	}

	a.emitRemoteStateChanged()

	return result, nil
}

func normalizedRemotePlatform() string {
	switch remotePlatformGOOS() {
	case "windows":
		return "windows"
	case "darwin":
		return "mac"
	case "linux":
		return "linux"
	default:
		return "linux"
	}
}

func (a *App) GetRemoteActivationStatus() RemoteActivationStatus {
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationStatus{}
	}
	return RemoteActivationStatus{
		Activated: cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "",
		Email:     cfg.RemoteEmail,
		SN:        cfg.RemoteSN,
		MachineID: cfg.RemoteMachineID,
		HubURL:    cfg.RemoteHubURL,
	}
}

func (a *App) ClearRemoteActivation() error {
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		_ = a.remoteSessions.hubClient.Disconnect()
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return err
	}
	cfg.RemoteEmail = ""
	cfg.RemoteSN = ""
	cfg.RemoteUserID = ""
	cfg.RemoteMachineID = ""
	cfg.RemoteMachineToken = ""
	if err := a.SaveConfig(cfg); err != nil {
		return err
	}

	a.emitRemoteStateChanged()
	return nil
}

func (a *App) ListRemoteHubs(centerURL string, email string) ([]RemoteHubCenterHub, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}

	email = strings.TrimSpace(email)
	if email == "" {
		email = strings.TrimSpace(cfg.RemoteEmail)
	}
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}

	result, err := a.resolveRemoteHubCenter(centerURL, email, cfg)
	if err != nil {
		return nil, err
	}
	if len(result.Hubs) == 0 {
		if result.Message == "" {
			result.Message = "no available hubs found"
		}
		return nil, fmt.Errorf("%s", result.Message)
	}

	hubs := make([]RemoteHubCenterHub, 0, len(result.Hubs))
	for _, hub := range result.Hubs {
		hubs = append(hubs, RemoteHubCenterHub{
			HubID:          hub.HubID,
			Name:           hub.Name,
			BaseURL:        strings.TrimRight(strings.TrimSpace(hub.BaseURL), "/"),
			PWAURL:         strings.TrimSpace(hub.PWAURL),
			Visibility:     hub.Visibility,
			EnrollmentMode: hub.EnrollmentMode,
			Status:         hub.Status,
		})
	}

	return hubs, nil
}

func (a *App) resolveRemoteHubURL(cfg AppConfig, email string) (string, error) {
	result, err := a.resolveRemoteHubCenter("", email, cfg)
	if err != nil {
		return "", err
	}

	if len(result.Hubs) == 0 {
		if result.Message == "" {
			result.Message = "no available hubs found"
		}
		return "", fmt.Errorf("%s", result.Message)
	}

	if result.DefaultHubID != "" {
		for _, hub := range result.Hubs {
			if hub.HubID == result.DefaultHubID && strings.TrimSpace(hub.BaseURL) != "" {
				return strings.TrimRight(hub.BaseURL, "/"), nil
			}
		}
	}

	for _, hub := range result.Hubs {
		if strings.TrimSpace(hub.BaseURL) != "" {
			return strings.TrimRight(hub.BaseURL, "/"), nil
		}
	}

	return "", fmt.Errorf("hub center did not return a usable hub url")
}

func (a *App) resolveRemoteHubCenter(centerURL string, email string, cfg AppConfig) (hubCenterResolveResult, error) {
	centerURL = strings.TrimSpace(centerURL)
	if centerURL == "" {
		centerURL = strings.TrimSpace(cfg.RemoteHubCenterURL)
	}
	if centerURL == "" {
		centerURL = defaultRemoteHubCenterURL
	}

	payload := map[string]string{
		"email": strings.TrimSpace(email),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return hubCenterResolveResult{}, err
	}

	resp, err := http.Post(strings.TrimRight(centerURL, "/")+"/api/entry/resolve", "application/json", bytes.NewReader(data))
	if err != nil {
		return hubCenterResolveResult{}, fmt.Errorf("resolve remote hub via center: %w", err)
	}
	defer resp.Body.Close()

	var result hubCenterResolveResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return hubCenterResolveResult{}, fmt.Errorf("decode center response: %w", err)
	}
	if resp.StatusCode >= 300 {
		if result.Message != "" {
			return hubCenterResolveResult{}, fmt.Errorf("%s", result.Message)
		}
		return hubCenterResolveResult{}, fmt.Errorf("hub center resolve failed: %s", resp.Status)
	}

	return result, nil
}
