package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type RemoteActivationResult struct {
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	Code         string `json:"code,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	VIPFlag      bool   `json:"vip_flag,omitempty"`
}

type RemoteProbeResult struct {
	InvitationCodeRequired bool   `json:"invitation_code_required"`
	Status                 string `json:"status,omitempty"`
	Message                string `json:"message,omitempty"`
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

const remoteEnrollTimeout = 25 * time.Second

func (a *App) ProbeRemoteHub(hubURL string, email string) (RemoteProbeResult, error) {
	hubURL = strings.TrimSpace(hubURL)
	if hubURL == "" {
		return RemoteProbeResult{}, fmt.Errorf("hub URL is required")
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return RemoteProbeResult{}, fmt.Errorf("email is required")
	}

	payload := map[string]string{"email": email}
	data, err := json.Marshal(payload)
	if err != nil {
		return RemoteProbeResult{}, err
	}

	resp, err := hubHTTPClient.Post(strings.TrimRight(hubURL, "/")+"/api/entry/probe", "application/json", bytes.NewReader(data))
	if err != nil {
		return RemoteProbeResult{}, err
	}
	defer resp.Body.Close()

	var result RemoteProbeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RemoteProbeResult{}, err
	}
	if resp.StatusCode >= 300 {
		if result.Message != "" {
			return RemoteProbeResult{}, fmt.Errorf("%s", result.Message)
		}
		return RemoteProbeResult{}, fmt.Errorf("probe failed: %s", resp.Status)
	}

	return result, nil
}

// autoRegisterOnStartup re-registers a previously registered machine using saved config.
// Called in a goroutine during startup when email and hub URL are present but machine credentials are missing.
func (a *App) autoRegisterOnStartup(cfg AppConfig) {
	email := strings.TrimSpace(cfg.RemoteEmail)
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if email == "" || hubURL == "" {
		return
	}
	result, err := a.ActivateRemote(email, "", "")
	if err != nil {
		fmt.Printf("auto-register on startup failed: %v\n", err)
		return
	}
	if result.MachineID != "" {
		fmt.Printf("auto-register on startup succeeded: machine_id=%s\n", result.MachineID)
	}
}

func (a *App) ActivateRemote(email string, invitationCode string, mobile string) (RemoteActivationResult, error) {
	start := time.Now()
	cfgLoadStart := time.Now()
	cfg, err := a.LoadConfig()
	if err != nil {
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote load_config=%s", time.Since(cfgLoadStart))

	email = strings.TrimSpace(email)
	if email == "" {
		return RemoteActivationResult{}, fmt.Errorf("email is required")
	}

	hubResolveStart := time.Now()
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	if hubURL == "" {
		hubURL, err = a.resolveRemoteHubURL(cfg, email)
		if err != nil {
			return RemoteActivationResult{}, err
		}
		cfg.RemoteHubURL = hubURL
	}
	log.Printf("[onboarding] ActivateRemote resolve_hub=%s reused=%t", time.Since(hubResolveStart), strings.TrimSpace(cfg.RemoteHubURL) != "")

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
	if invitationCode != "" {
		body["invitation_code"] = invitationCode
	}
	if mobile != "" {
		body["mobile"] = strings.TrimSpace(mobile)
	}

	// Generate a stable client_id on first run so re-enrollment reuses the same machine record
	clientIDStart := time.Now()
	if cfg.RemoteClientID == "" {
		cfg.RemoteClientID = generateClientID()
		if err := a.SaveConfig(cfg); err != nil {
			return RemoteActivationResult{}, err
		}
	}
	log.Printf("[onboarding] ActivateRemote ensure_client_id=%s created=%t", time.Since(clientIDStart), cfg.RemoteClientID != "")
	body["client_id"] = cfg.RemoteClientID
	marshalStart := time.Now()
	data, err := json.Marshal(body)
	if err != nil {
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote marshal_request=%s", time.Since(marshalStart))

	enrollStart := time.Now()
	enrollURL := strings.TrimRight(hubURL, "/") + "/api/enroll/start"
	ctx, cancel := context.WithTimeout(context.Background(), remoteEnrollTimeout)
	defer cancel()
	log.Printf("[onboarding] ActivateRemote enroll_request:start timeout=%s url=%s", remoteEnrollTimeout, enrollURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, enrollURL, bytes.NewReader(data))
	if err != nil {
		return RemoteActivationResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	resp, err := hubHTTPClient.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || isHTTPTimeoutError(err) {
			log.Printf("[onboarding] ActivateRemote enroll_request:timeout after=%s url=%s err=%v", time.Since(enrollStart), enrollURL, err)
			return RemoteActivationResult{}, fmt.Errorf("registration timed out after %s", remoteEnrollTimeout)
		}
		log.Printf("[onboarding] ActivateRemote enroll_request:failed after=%s url=%s err=%v", time.Since(enrollStart), enrollURL, err)
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote enroll_http=%s status=%d", time.Since(enrollStart), resp.StatusCode)
	defer resp.Body.Close()

	decodeStart := time.Now()
	var result RemoteActivationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote decode_response=%s status=%d enrollment_status=%s", time.Since(decodeStart), resp.StatusCode, result.Status)
	if resp.StatusCode >= 300 {
		if result.Code != "" {
			if result.ExpiresAt != "" {
				return RemoteActivationResult{}, fmt.Errorf("%s: %s expires_at:%s", result.Code, result.Message, result.ExpiresAt)
			}
			return RemoteActivationResult{}, fmt.Errorf("%s: %s", result.Code, result.Message)
		}
		if result.Message != "" {
			return RemoteActivationResult{}, fmt.Errorf("%s", result.Message)
		}
		return RemoteActivationResult{}, fmt.Errorf("remote registration failed: %s", resp.Status)
	}

	persistStart := time.Now()
	cfg.RemoteEmail = result.Email
	cfg.RemoteSN = result.SN
	cfg.RemoteUserID = result.UserID
	cfg.RemoteMachineID = result.MachineID
	cfg.RemoteMachineToken = result.MachineToken
	cfg.RemoteHubURL = hubURL
	log.Printf("[onboarding] ActivateRemote save_config:start machine_id=%s email=%s", result.MachineID, result.Email)
	if err := a.SaveConfig(cfg); err != nil {
		log.Printf("[onboarding] ActivateRemote save_config:failed after=%s err=%v", time.Since(persistStart), err)
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote save_config=%s", time.Since(persistStart))

	a.emitRemoteStateChanged()
	go func(launchedAt time.Time) {
		infraStart := time.Now()
		if a.remoteSessions == nil {
			a.ensureRemoteInfra()
		}
		a.logMemorySnapshot("remoteActivation:before-connect")
		logAfterConnect := func() {
			a.logMemorySnapshot("remoteActivation:after-connect")
		}
		hubClient := (*RemoteHubClient)(nil)
		if a.remoteSessions != nil {
			hubClient = a.remoteSessions.hubClient
		}
		if hubClient == nil && a.remoteSessions != nil {
			hubClient = NewRemoteHubClient(a, a.remoteSessions)
			a.remoteSessions.SetHubClient(hubClient)
		}
		if hubClient == nil {
			hubClient = a.createAndWireHubClient()
		}
		log.Printf("[onboarding] ActivateRemote ensure_remote_infra=%s hub_client_ready=%t", time.Since(infraStart), hubClient != nil)
		if hubClient != nil && !hubClient.IsConnected() {
			if err := hubClient.Connect(); err != nil {
				log.Printf("[onboarding] ActivateRemote background_connect_failed total=%s err=%v", time.Since(launchedAt), err)
			} else {
				a.emitRemoteStateChanged()
				logAfterConnect()
				log.Printf("[onboarding] ActivateRemote background_connect_total=%s", time.Since(launchedAt))
			}
		} else {
			logAfterConnect()
		}
	}(time.Now())
	log.Printf("[onboarding] ActivateRemote total=%s", time.Since(start))

	return result, nil
}

func isHTTPTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
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

// VerifyRemoteActivation checks with the server whether the current activation
// is still valid. If the server reports the user as not_found or blocked,
// local machine credentials are cleared so the UI reflects the real state
// and the user can re-register. Email and hub URL are preserved so the user
// doesn't need to re-enter them.
// Returns true if activation is still valid, false if it was invalidated.
func (a *App) VerifyRemoteActivation() bool {
	cfg, err := a.LoadConfig()
	if err != nil {
		return true // can't verify, assume valid
	}
	if cfg.RemoteMachineID == "" || cfg.RemoteMachineToken == "" {
		return false // not activated
	}
	hubURL := strings.TrimSpace(cfg.RemoteHubURL)
	email := strings.TrimSpace(cfg.RemoteEmail)
	if hubURL == "" || email == "" {
		return true
	}

	probe, err := a.ProbeRemoteHub(hubURL, email)
	if err != nil {
		// Network error — don't clear, assume valid
		return true
	}

	switch probe.Status {
	case "not_found", "blocked":
		// Server no longer recognizes this user — clear machine credentials
		// but preserve email and hub URL for easier re-registration.
		fmt.Printf("[verify-activation] server reports status=%s for %s, clearing local machine credentials\n", probe.Status, email)
		a.clearMachineCredentials()
		return false
	default:
		return true
	}
}

// clearMachineCredentials removes machine_id, machine_token, sn, and user_id
// from the local config while preserving email and hub URL. It also disconnects
// the hub client. This is used when the server invalidates the user so they
// can re-register without re-entering connection details.
func (a *App) clearMachineCredentials() {
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		a.remoteSessions.hubClient.allowReconnect.Store(false)
		_ = a.remoteSessions.hubClient.Disconnect()
	}

	cfg, err := a.LoadConfig()
	if err != nil {
		return
	}
	cfg.RemoteSN = ""
	cfg.RemoteUserID = ""
	cfg.RemoteMachineID = ""
	cfg.RemoteMachineToken = ""
	_ = a.SaveConfig(cfg)

	a.emitRemoteStateChanged()
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

	resp, err := hubHTTPClient.Post(strings.TrimRight(centerURL, "/")+"/api/entry/resolve", "application/json", bytes.NewReader(data))
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

// generateClientID produces a UUID v4 string used to stably identify this desktop instance.
func generateClientID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	buf[6] = (buf[6] & 0x0f) | 0x40 // version 4
	buf[8] = (buf[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}
