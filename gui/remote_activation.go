package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"log"
	"net"
	"strings"
	"time"
)

type RemoteActivationResult struct {
	Status       string `json:"status"`
	TenantID     string `json:"tenant_id,omitempty"`
	TenantName   string `json:"tenant_name,omitempty"`
	Message      string `json:"message,omitempty"`
	Code         string `json:"code,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Email        string `json:"email,omitempty"`
	SN           string `json:"sn,omitempty"`
	MachineID    string `json:"machine_id,omitempty"`
	MachineToken string `json:"machine_token,omitempty"`
	ViewerToken  string `json:"viewer_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	VIPFlag      bool   `json:"vip_flag,omitempty"`
}

type RemoteProbeResult struct {
	InvitationCodeRequired bool   `json:"invitation_code_required"`
	TenantID               string `json:"tenant_id,omitempty"`
	TenantName             string `json:"tenant_name,omitempty"`
	Status                 string `json:"status,omitempty"`
	Message                string `json:"message,omitempty"`
}

type RemoteActivationStatus struct {
	Activated  bool   `json:"activated"`
	Email      string `json:"email"`
	SN         string `json:"sn"`
	TenantID   string `json:"tenant_id,omitempty"`
	TenantName string `json:"tenant_name,omitempty"`
	MachineID  string `json:"machine_id"`
	HubURL     string `json:"hub_url"`
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

var remoteEnrollTimeout = remote.EnrollTimeout

const skillMarketAutoLoginRetryDelay = 15 * time.Minute

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
	if err := remote.DecodeHTTPJSONResponse(resp, &result, "hub probe"); err != nil {
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
func (a *App) autoRegisterOnStartup(cfg corelib.AppConfig) {
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

	// Build enrollment config from app config.
	profile := a.currentRemoteMachineProfile(cfg.RemoteHeartbeatSec, 0)
	enrollCfg := remote.EnrollConfig{
		Email:          email,
		InvitationCode: invitationCode,
		Mobile:         mobile,
		ClientID:       cfg.RemoteClientID,
		HubURL:         strings.TrimSpace(cfg.RemoteHubURL),
		HubCenterURL:   strings.TrimSpace(cfg.RemoteHubCenterURL),
		HubCenterURLs:  cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs),
		MachineName:    profile.Name,
		Platform:       profile.Platform,
		Hostname:       profile.Hostname,
		Arch:           profile.Arch,
		AppVersion:     profile.AppVersion,
		HeartbeatSec:   profile.HeartbeatSec,
	}

	// Ensure stable client_id.
	if enrollCfg.ClientID == "" {
		enrollCfg.ClientID = remote.GenerateClientID()
		// Persist only client_id so concurrent settings edits are not overwritten.
		if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.RemoteClientID = enrollCfg.ClientID
		}); err != nil {
			return RemoteActivationResult{}, err
		}
	}

	// Delegate to shared enrollment client.
	enrollClient := &remote.EnrollmentClient{HTTPClient: hubHTTPClient, EnrollTimeout: remoteEnrollTimeout}
	enrollResult, err := enrollClient.Enroll(context.Background(), enrollCfg)
	if err != nil {
		return RemoteActivationResult{}, err
	}

	// Persist credentials atomically via PatchConfig to eliminate the TOCTOU
	// race between LoadConfig and SaveConfig. Only enrollment-specific fields
	// are patched — other fields (LLM settings, UI preferences, etc.) that
	// may have been modified concurrently are untouched.
	persistStart := time.Now()
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = enrollResult.Email
		cfg.RemoteSN = enrollResult.SN
		cfg.RemoteUserID = enrollResult.UserID
		cfg.RemoteTenantID = enrollResult.TenantID
		cfg.RemoteTenantName = enrollResult.TenantName
		cfg.RemoteMachineID = enrollResult.MachineID
		cfg.RemoteMachineName = profile.Name
		cfg.RemoteMachineToken = enrollResult.MachineToken
		cfg.RemoteNickname = ""
		cfg.RemoteHubURL = enrollResult.HubURL
		cfg.RemoteEnabled = true
		if enrollResult.ViewerToken != "" {
			cfg.RemoteViewerToken = enrollResult.ViewerToken
		} else {
			cfg.RemoteViewerToken = ""
		}
		if enrollResult.ClientID != "" && cfg.RemoteClientID == "" {
			cfg.RemoteClientID = enrollResult.ClientID
		}
		if enrollResult.HubCenterURL != "" {
			cfg.RemoteHubCenterURL = enrollResult.HubCenterURL
		}
		if len(enrollResult.DiscoveredURLs) > 0 {
			cfg.RemoteHubCenterURLs = remote.NormalizeHubCenterURLs(enrollResult.DiscoveredURLs)
		}
	}); err != nil {
		log.Printf("[onboarding] ActivateRemote PatchConfig:failed after=%s err=%v", time.Since(persistStart), err)
		return RemoteActivationResult{}, err
	}
	log.Printf("[onboarding] ActivateRemote PatchConfig=%s machine_id=%s email=%s", time.Since(persistStart), enrollResult.MachineID, enrollResult.Email)

	// If this Hub account already has MaClaw official service entitlement,
	// make it the active LLM provider immediately after registration. This
	// handles re-registration to another Hub where official service is available.
	if freshCfg, loadErr := a.LoadConfig(); loadErr == nil {
		if strings.TrimSpace(freshCfg.RemoteViewerToken) != "" {
			if status, statusErr := a.fetchHubLLMServiceStatusWithTimeout(freshCfg, hubServiceStatusTimeout); statusErr == nil {
				if _, syncErr := a.syncHubLLMServiceStatusToConfig(status, true); syncErr != nil {
					log.Printf("[onboarding] ActivateRemote hub_service_sync_failed err=%v", syncErr)
				}
			} else {
				if isHubLLMServiceAuthorizationError(statusErr) {
					if _, syncErr := a.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{}, false); syncErr != nil {
						log.Printf("[onboarding] ActivateRemote hub_service_clear_failed err=%v", syncErr)
					}
				}
				log.Printf("[onboarding] ActivateRemote hub_service_status_failed err=%v", statusErr)
			}
		} else if _, syncErr := a.syncHubLLMServiceStatusToConfig(HubLLMServiceStatus{}, false); syncErr != nil {
			log.Printf("[onboarding] ActivateRemote hub_service_clear_failed err=%v", syncErr)
		}
	} else {
		log.Printf("[onboarding] ActivateRemote hub_service_load_config_failed err=%v", loadErr)
	}

	// Auto-acquire SkillMarket session token via machine-login.
	// This allows the user to upload skills immediately after Hub registration
	// without a separate SkillMarket login step.
	go a.acquireSkillMarketTokenAfterEnroll(enrollResult.Email, enrollResult.MachineID, enrollResult.ViewerToken)

	// Convert to GUI result type.
	result := RemoteActivationResult{
		Status:       enrollResult.Status,
		TenantID:     enrollResult.TenantID,
		TenantName:   enrollResult.TenantName,
		Message:      enrollResult.Message,
		Code:         enrollResult.Code,
		UserID:       enrollResult.UserID,
		Email:        enrollResult.Email,
		SN:           enrollResult.SN,
		MachineID:    enrollResult.MachineID,
		MachineToken: enrollResult.MachineToken,
		ViewerToken:  enrollResult.ViewerToken,
		ExpiresAt:    enrollResult.ExpiresAt,
		VIPFlag:      enrollResult.VIPFlag,
	}

	// GUI-specific: emit state change + background hub connection.
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
		hubClient := a.ensureHubClient()
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
		Activated:  cfg.RemoteMachineID != "" && cfg.RemoteMachineToken != "",
		Email:      cfg.RemoteEmail,
		SN:         cfg.RemoteSN,
		TenantID:   cfg.RemoteTenantID,
		TenantName: cfg.RemoteTenantName,
		MachineID:  cfg.RemoteMachineID,
		HubURL:     cfg.RemoteHubURL,
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

	if normalizeRemoteProbeStatusKind(probe.Status).ShouldClearActivation() {
		// Server no longer recognizes this user — clear machine credentials
		// but preserve email and hub URL for easier re-registration.
		fmt.Printf("[verify-activation] server reports status=%s for %s, clearing local machine credentials\n", probe.Status, email)
		a.clearMachineCredentials()
		return false
	}
	return true
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

	_ = a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteSN = ""
		cfg.RemoteUserID = ""
		cfg.RemoteTenantID = ""
		cfg.RemoteTenantName = ""
		cfg.RemoteMachineID = ""
		cfg.RemoteMachineName = ""
		cfg.RemoteMachineToken = ""
		cfg.RemoteViewerToken = ""
		cfg.RemoteNickname = ""
	})

	a.emitRemoteStateChanged()
}

func (a *App) ClearRemoteActivation() error {
	if a.remoteSessions != nil && a.remoteSessions.hubClient != nil {
		_ = a.remoteSessions.hubClient.Disconnect()
	}

	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteEmail = ""
		cfg.RemoteSN = ""
		cfg.RemoteUserID = ""
		cfg.RemoteTenantID = ""
		cfg.RemoteTenantName = ""
		cfg.RemoteMachineID = ""
		cfg.RemoteMachineName = ""
		cfg.RemoteMachineToken = ""
		cfg.RemoteViewerToken = ""
		cfg.RemoteNickname = ""
	}); err != nil {
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

	// Delegate to the shared enrollment client for hub resolution.
	enrollClient := &remote.EnrollmentClient{HTTPClient: hubHTTPClient}
	result, usedCenter, ordered, err := enrollClient.ResolveHubs(
		context.Background(),
		email,
		strings.TrimSpace(centerURL),
		cfg.HubCenterBaseURLs(defaultRemoteHubCenterURL, remote.DefaultRemoteHubCenterURLs),
	)
	if err != nil {
		return nil, err
	}

	// Persist the successfully used HubCenter URL for next time.
	if usedCenter != "" {
		go a.rememberHubCenterSelectionThrottled(usedCenter, ordered)
	}

	if len(result.Hubs) == 0 {
		msg := result.Message
		if msg == "" {
			msg = "no available hubs found"
		}
		return nil, fmt.Errorf("%s", msg)
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

// generateClientID delegates to the shared corelib implementation.
func generateClientID() string {
	return remote.GenerateClientID()
}

// acquireSkillMarketTokenAfterEnroll calls the HubCenter machine-login endpoint
// to obtain a SkillMarket session token using the Hub enrollment credentials.
// Runs in background — failure is non-fatal (user can still upload via email fallback).
func (a *App) acquireSkillMarketTokenAfterEnroll(email, machineID, viewerToken string) {
	if email == "" || viewerToken == "" {
		return
	}
	if !a.shouldAttemptSkillMarketAutoLogin() {
		return
	}
	defer a.skillMarketAutoLoginRunning.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := a.LoadConfig()
	if err != nil {
		log.Printf("[skillmarket-auto-login] load config failed: %v", err)
		return
	}
	// Skip if user already has a valid SkillMarket token.
	if strings.TrimSpace(cfg.SkillMarketSessionToken) != "" {
		return
	}

	baseURL := cfg.SkillMarketBaseURL(defaultRemoteHubCenterURL)
	client := remote.NewSkillMarketAuthClient()
	result, err := client.MachineLogin(ctx, baseURL, email, machineID, viewerToken)
	if err != nil {
		a.deferSkillMarketAutoLoginRetry(skillMarketAutoLoginRetryDelay)
		log.Printf("[skillmarket-auto-login] machine-login failed (non-fatal): %v", err)
		return
	}
	if result.SessionToken == "" {
		a.deferSkillMarketAutoLoginRetry(skillMarketAutoLoginRetryDelay)
		log.Printf("[skillmarket-auto-login] empty token returned")
		return
	}

	// Persist token.
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.SkillMarketSessionToken = result.SessionToken
	}); err != nil {
		log.Printf("[skillmarket-auto-login] save token failed: %v", err)
		return
	}
	a.skillMarketAutoLoginNextAttempt.Store(time.Time{})
	log.Printf("[skillmarket-auto-login] success email=%s", email)
}

func (a *App) shouldAttemptSkillMarketAutoLogin() bool {
	if a == nil {
		return false
	}
	now := time.Now()
	if next, ok := a.skillMarketAutoLoginNextAttempt.Load().(time.Time); ok && !next.IsZero() && now.Before(next) {
		return false
	}
	return a.skillMarketAutoLoginRunning.CompareAndSwap(false, true)
}

func (a *App) deferSkillMarketAutoLoginRetry(delay time.Duration) {
	if a == nil {
		return
	}
	if delay <= 0 {
		delay = skillMarketAutoLoginRetryDelay
	}
	a.skillMarketAutoLoginNextAttempt.Store(time.Now().Add(delay))
}
