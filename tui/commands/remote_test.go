package commands

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

func TestRemoteSetHubCenterSavesNormalizedURL(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	out, err := captureRemoteStdout(t, func() error {
		return RunRemote([]string{"set-hubcenter", "https://center.example/"})
	})
	if err != nil {
		t.Fatalf("set-hubcenter error = %v", err)
	}
	if !strings.Contains(out, "HubCenter") {
		t.Fatalf("set-hubcenter output should mention HubCenter:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui setup") || !strings.Contains(out, "Hub URL") {
		t.Fatalf("set-hubcenter output should explain the next TUI activation step:\n%s", out)
	}
	cfg, err := NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RemoteHubCenterURL != "https://center.example" {
		t.Fatalf("RemoteHubCenterURL = %q", cfg.RemoteHubCenterURL)
	}
}

func TestRemoteSetHubIsDisplayOnlyAndDoesNotSaveHubURL(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	err := RunRemote([]string{"set-hub", "https://hub.example"})
	if err == nil {
		t.Fatal("set-hub should be rejected so users configure HubCenter instead")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HubCenter") || !strings.Contains(msg, "maclaw-tui setup") {
		t.Fatalf("set-hub error should guide to HubCenter/TUI setup: %s", msg)
	}
	cfg, err := NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RemoteHubURL != "" {
		t.Fatalf("RemoteHubURL should stay display-only, got %q", cfg.RemoteHubURL)
	}
}

func TestRemoteSetEmailValidatesAndNormalizesEmail(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)

	out, err := captureRemoteStdout(t, func() error {
		return RunRemote([]string{"set-email", "USER@Example.COM"})
	})
	if err != nil {
		t.Fatalf("set-email error = %v", err)
	}
	if !strings.Contains(out, "maclaw-tui setup") || !strings.Contains(out, "HubCenter") {
		t.Fatalf("set-email should guide to TUI activation and automatic Hub selection:\n%s", out)
	}
	cfg, err := NewFileConfigStore(dataDir).LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RemoteEmail != "user@example.com" {
		t.Fatalf("RemoteEmail = %q", cfg.RemoteEmail)
	}

	err = RunRemote([]string{"set-email", "not-an-email"})
	if err == nil {
		t.Fatal("set-email should reject invalid email")
	}
	if !strings.Contains(err.Error(), "maclaw-tui setup") {
		t.Fatalf("invalid email error should guide to TUI setup: %s", err)
	}
}

func TestRemoteStatusShowsHubCenterAndDisplayOnlyHubURL(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		RemoteEnabled:      true,
		RemoteEmail:        "user@example.com",
		RemoteHubCenterURL: "https://center.example/",
		RemoteHubURL:       "https://hub.example",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "machine-token",
		RemoteViewerToken:  "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return remoteStatus(nil)
	})
	if err != nil {
		t.Fatalf("remoteStatus error = %v", err)
	}
	if !strings.Contains(out, "HubCenter: https://center.example") {
		t.Fatalf("status should show configurable HubCenter:\n%s", out)
	}
	if !strings.Contains(out, "Hub URL:") || !strings.Contains(out, "https://hub.example") {
		t.Fatalf("status should display the selected Hub URL:\n%s", out)
	}
	if !strings.Contains(out, "maclaw-tui status") || !strings.Contains(out, "maclaw-tui redeem") {
		t.Fatalf("active status should guide to TUI status/redeem:\n%s", out)
	}
}

func TestRemoteStatusLocalizesEnglishOutput(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		Language:          "en",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return remoteStatus(nil)
	})
	if err != nil {
		t.Fatalf("remoteStatus error = %v", err)
	}
	for _, want := range []string{
		"Remote mode:",
		"service credentials ready",
		"Service credentials: yes",
		"Machine activation:  no",
		"Next: Run maclaw-tui redeem",
		"TUI status: maclaw-tui status",
		"Hub URL is display-only",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("English remote status missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "远程模式") || strings.Contains(out, "下一步") {
		t.Fatalf("English remote status should not mix Chinese labels:\n%s", out)
	}
}

func TestRemoteStatusJSONIncludesHubCenterAndViewerTokenState(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		RemoteEmail:        "user@example.com",
		RemoteHubCenterURL: "https://center.example/",
		RemoteHubURL:       "https://hub.example",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "machine-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return remoteStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("remoteStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(out), &info); err != nil {
		t.Fatalf("parse json %q: %v", out, err)
	}
	if info["hubcenter_url"] != "https://center.example" {
		t.Fatalf("hubcenter_url = %#v", info["hubcenter_url"])
	}
	if info["activation_state"] != "incomplete" {
		t.Fatalf("activation_state = %#v", info["activation_state"])
	}
	if info["viewer_token_ready"] != false {
		t.Fatalf("viewer_token_ready = %#v", info["viewer_token_ready"])
	}
	if info["hub_service_ready"] != false {
		t.Fatalf("hub_service_ready = %#v", info["hub_service_ready"])
	}
	if info["machine_token_ready"] != true {
		t.Fatalf("machine_token_ready = %#v", info["machine_token_ready"])
	}
	if info["next_tui_command"] != "maclaw-tui setup" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
	if next, ok := info["next_action"].(string); !ok || !strings.Contains(next, "maclaw-tui setup") {
		t.Fatalf("next_action = %#v", info["next_action"])
	}
}

func TestRemoteStatusInactiveGuidesToTUISetup(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	out, err := captureRemoteStdout(t, func() error {
		return remoteStatus(nil)
	})
	if err != nil {
		t.Fatalf("remoteStatus error = %v", err)
	}
	if !strings.Contains(out, "maclaw-tui setup") || !strings.Contains(out, "HubCenter") {
		t.Fatalf("inactive remote status should guide to TUI setup and HubCenter:\n%s", out)
	}
}

func TestRemoteStatusTreatsHubViewerAsServiceReady(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return remoteStatus(nil)
	})
	if err != nil {
		t.Fatalf("remoteStatus error = %v", err)
	}
	if !strings.Contains(out, "服务凭据") || !strings.Contains(out, "maclaw-tui status") || !strings.Contains(out, "maclaw-tui redeem") {
		t.Fatalf("service-ready remote status should guide to status/redeem instead of setup:\n%s", out)
	}

	jsonOut, err := captureRemoteStdout(t, func() error {
		return remoteStatus([]string{"--json"})
	})
	if err != nil {
		t.Fatalf("remoteStatus --json error = %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &info); err != nil {
		t.Fatalf("parse json %q: %v", jsonOut, err)
	}
	if info["activation_state"] != "service_ready" {
		t.Fatalf("activation_state = %#v", info["activation_state"])
	}
	if info["hub_service_ready"] != true || info["machine_activation_ready"] != false {
		t.Fatalf("readiness flags = hub %#v machine %#v", info["hub_service_ready"], info["machine_activation_ready"])
	}
	if info["next_tui_command"] != "maclaw-tui status" {
		t.Fatalf("next_tui_command = %#v", info["next_tui_command"])
	}
}

func TestRemoteActivateWithoutEmailGuidesToTUISetup(t *testing.T) {
	t.Setenv("MACLAW_DATA_DIR", t.TempDir())

	err := remoteActivate(nil)
	if err == nil {
		t.Fatal("expected missing email error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "maclaw-tui setup") || !strings.Contains(msg, "remote activate --email") {
		t.Fatalf("missing email should guide to TUI setup with script fallback: %s", msg)
	}
	if strings.Contains(msg, "set-email") {
		t.Fatalf("missing email should not make set-email the primary path: %s", msg)
	}
}

func TestRemoteActivateRejectsInvalidSavedEmailBeforeNetwork(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{RemoteEmail: "bad-email"}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	err := remoteActivate(nil)
	if err == nil {
		t.Fatal("expected invalid email error")
	}
	if !strings.Contains(err.Error(), "maclaw-tui setup") {
		t.Fatalf("invalid saved email should guide to TUI setup: %s", err)
	}
}

func TestRemoteEnrollmentProfileOmitsManualHubURL(t *testing.T) {
	profile := buildRemoteEnrollmentProfile(corelib.AppConfig{
		RemoteHubURL:        "https://legacy-hub.example",
		RemoteHubCenterURL:  "https://center.example/",
		RemoteHubCenterURLs: []string{"https://backup.example/"},
		RemoteClientID:      "client-1",
	}, " user@example.com ", " invite-1 ")

	if profile.HubURL != "" {
		t.Fatalf("HubURL should be resolved by HubCenter, got %q", profile.HubURL)
	}
	if profile.HubCenterURL != "https://center.example/" {
		t.Fatalf("HubCenterURL = %q", profile.HubCenterURL)
	}
	if profile.Email != "user@example.com" || profile.InvitationCode != "invite-1" {
		t.Fatalf("profile email/invite not normalized: %#v", profile)
	}
}

func TestRemoteActivationCompleteRequiresViewerToken(t *testing.T) {
	cfg := corelib.AppConfig{
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "machine-token",
	}
	if remoteActivationComplete(cfg) {
		t.Fatal("activation should be incomplete without viewer token")
	}
	if !remoteActivationIncomplete(cfg) {
		t.Fatal("missing viewer token should be reported as incomplete")
	}
	cfg.RemoteViewerToken = "viewer-token"
	cfg.RemoteHubURL = "https://hub.example"
	if !remoteActivationComplete(cfg) {
		t.Fatal("activation should be complete with machine and viewer tokens")
	}
}

func TestApplyRemoteEnrollResultMarksOnboardingDoneWhenViewerReady(t *testing.T) {
	cfg := applyRemoteEnrollResultToConfig(corelib.AppConfig{}, &remote.EnrollResult{
		Email:        "user@example.com",
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		ViewerToken:  "viewer-token",
		HubURL:       "https://hub.example",
		HubCenterURL: "https://center.example",
	})

	if !cfg.OnboardingDone {
		t.Fatal("successful remote activation should mark onboarding done for the next TUI launch")
	}
	if !cfg.RemoteEnabled || cfg.DefaultLaunchMode != "remote" {
		t.Fatalf("remote defaults not applied: %#v", cfg)
	}
	if cfg.RemoteHubURL != "https://hub.example" || cfg.RemoteHubCenterURL != "https://center.example" {
		t.Fatalf("hub URLs not applied: %#v", cfg)
	}

	incomplete := applyRemoteEnrollResultToConfig(corelib.AppConfig{}, &remote.EnrollResult{
		Email:        "user@example.com",
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		HubURL:       "https://hub.example",
	})
	if incomplete.OnboardingDone {
		t.Fatal("activation without viewer token should keep onboarding incomplete")
	}
}

func TestRemoteDeactivateClearsServiceReadyCredentialsWithoutMachineID(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("MACLAW_DATA_DIR", dataDir)
	store := NewFileConfigStore(dataDir)
	if err := store.SaveConfig(corelib.AppConfig{
		OnboardingDone:    true,
		DefaultLaunchMode: "remote",
		RemoteEnabled:     true,
		RemoteEmail:       "user@example.com",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, err := captureRemoteStdout(t, func() error {
		return remoteDeactivate(nil)
	})
	if err != nil {
		t.Fatalf("remoteDeactivate error = %v", err)
	}
	if !strings.Contains(out, "取消激活") {
		t.Fatalf("deactivate output should confirm clearing service-ready credentials:\n%s", out)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.RemoteHubURL != "" || cfg.RemoteViewerToken != "" || cfg.RemoteEmail != "" || cfg.RemoteEnabled || cfg.OnboardingDone || cfg.DefaultLaunchMode == "remote" {
		t.Fatalf("remote service credentials were not fully cleared: %#v", cfg)
	}
}

func captureRemoteStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	callErr := fn()
	_ = w.Close()
	os.Stdout = oldStdout
	outBytes, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("read stdout: %v", readErr)
	}
	return string(outBytes), callErr
}
