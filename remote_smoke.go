package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RemoteSmokeReport struct {
	Tool            string                       `json:"tool"`
	ProjectPath     string                       `json:"project_path"`
	UseProxy        bool                         `json:"use_proxy"`
	Phase           string                       `json:"phase"`
	Success         bool                         `json:"success"`
	LastUpdated     string                       `json:"last_updated"`
	RecommendedNext string                       `json:"recommended_next,omitempty"`
	Connection      RemoteConnectionStatus       `json:"connection"`
	Readiness       RemoteToolReadiness          `json:"readiness"`
	PTYProbe        *RemotePTYProbeResult        `json:"pty_probe,omitempty"`
	LaunchProbe     *RemoteToolLaunchProbeResult `json:"launch_probe,omitempty"`
	Activation      *RemoteActivationResult      `json:"activation,omitempty"`
	StartedSession  *RemoteSessionView           `json:"started_session,omitempty"`
	HubVisibility   *RemoteHubVisibilityResult   `json:"hub_visibility,omitempty"`
}

type RemoteHubVisibilityResult struct {
	Attempted      bool   `json:"attempted"`
	Verified       bool   `json:"verified"`
	HubURL         string `json:"hub_url"`
	UserID         string `json:"user_id"`
	MachineID      string `json:"machine_id"`
	SessionID      string `json:"session_id"`
	MachineVisible bool   `json:"machine_visible"`
	SessionVisible bool   `json:"session_visible"`
	SessionStatus  string `json:"session_status,omitempty"`
	HostOnline     bool   `json:"host_online"`
	Message        string `json:"message,omitempty"`
}

func compactRemoteSmokeSessionView(view RemoteSessionView) RemoteSessionView {
	view.ID = sanitizeRemoteText(view.ID)
	view.Tool = sanitizeRemoteText(view.Tool)
	view.Title = sanitizeRemoteText(view.Title)
	view.ProjectPath = sanitizeRemoteText(view.ProjectPath)
	view.WorkspacePath = sanitizeRemoteText(view.WorkspacePath)
	view.WorkspaceRoot = sanitizeRemoteText(view.WorkspaceRoot)
	view.ModelID = sanitizeRemoteText(view.ModelID)

	summary := view.Summary
	sanitizeSessionSummary(&summary)
	view.Summary = summary

	preview := view.Preview
	preview.SessionID = sanitizeRemoteText(preview.SessionID)
	preview.PreviewLines = nil
	view.Preview = preview

	view.Events = nil
	return view
}

func runRemoteSmoke(app *App, args []string) int {
	fs := flag.NewFlagSet("remote-smoke", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	toolName := fs.String("tool", "claude", "tool name for remote smoke")
	projectDir := fs.String("project", "", "project directory for remote smoke")
	useProxy := fs.Bool("use-proxy", false, "enable proxy resolution")
	email := fs.String("email", "", "email for remote registration")
	hubURL := fs.String("hub-url", "", "override remote hub url for local smoke")
	centerURL := fs.String("center-url", "", "override remote hub center url for local smoke")
	doActivate := fs.Bool("activate", false, "register remote machine before checks")
	doPTYProbe := fs.Bool("pty-probe", false, "run interactive ConPTY probe")
	doLaunchProbe := fs.Bool("launch-probe", false, "run remote launch probe")
	doStart := fs.Bool("start", false, "start a real remote session")
	verifyHub := fs.Bool("verify-hub", false, "verify the started machine/session is visible from Hub debug endpoints")
	verifyTimeoutSeconds := fs.Int("verify-timeout-seconds", 20, "timeout in seconds for Hub visibility verification")
	holdSeconds := fs.Int("hold-seconds", 0, "keep the smoke process alive for N seconds after starting a session")
	progressFile := fs.String("progress-file", "", "write JSON progress snapshots to this file while running")
	jsonOutput := fs.Bool("json", false, "print JSON output")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *projectDir == "" {
		*projectDir = app.GetCurrentProjectPath()
	}
	if *projectDir != "" {
		*projectDir = filepath.Clean(*projectDir)
	}
	*toolName = normalizeRemoteToolName(*toolName)

	if *hubURL != "" || *centerURL != "" || *doStart {
		cfg, err := app.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "remote-smoke: load config failed: %v\n", err)
			return 1
		}

		if *hubURL != "" {
			cfg.RemoteHubURL = strings.TrimRight(strings.TrimSpace(*hubURL), "/")
		}
		if *centerURL != "" {
			cfg.RemoteHubCenterURL = strings.TrimRight(strings.TrimSpace(*centerURL), "/")
		}
		if *doStart {
			cfg.RemoteEnabled = true
		}

		if err := app.SaveConfig(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "remote-smoke: save config overrides failed: %v\n", err)
			return 1
		}
	}

	report := RemoteSmokeReport{
		Tool:         *toolName,
		ProjectPath:  *projectDir,
		UseProxy:     *useProxy,
		Phase:        "initializing",
		LastUpdated:  time.Now().Format(time.RFC3339),
		Connection:   app.GetRemoteConnectionStatus(),
		Readiness:    app.GetRemoteToolReadiness(*toolName, *projectDir, *useProxy),
		RecommendedNext: "Review readiness and run PTY / launch probes.",
	}
	writeRemoteSmokeProgress(*progressFile, report)

	if *doActivate {
		if *email == "" {
			fmt.Fprintln(os.Stderr, "remote-smoke: -email is required with -activate")
			return 2
		}
		result, err := app.ActivateRemote(*email, "", "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "remote-smoke: register failed: %v\n", err)
			report.Phase = "registration_failed"
			report.LastUpdated = time.Now().Format(time.RFC3339)
			report.RecommendedNext = "Check Hub / Hub Center health and remote email configuration."
			writeRemoteSmokeProgress(*progressFile, report)
			return 1
		}
		report.Activation = &result
		report.Connection = app.GetRemoteConnectionStatus()
		report.Readiness = app.GetRemoteToolReadiness(*toolName, *projectDir, *useProxy)
		report.Phase = "registered"
		report.LastUpdated = time.Now().Format(time.RFC3339)
		report.RecommendedNext = "Run PTY and launch probes, then start a real remote session."
		writeRemoteSmokeProgress(*progressFile, report)
	}

	if *doPTYProbe {
		probe := app.GetRemotePTYProbe()
		report.PTYProbe = &probe
		report.Phase = "pty_probed"
		report.LastUpdated = time.Now().Format(time.RFC3339)
		if probe.Ready {
			report.RecommendedNext = "Run the Claude launch probe."
		} else {
			report.RecommendedNext = "Fix ConPTY support before attempting a real session."
		}
		writeRemoteSmokeProgress(*progressFile, report)
	}

	if *doLaunchProbe {
		probe := app.GetRemoteToolLaunchProbe(*toolName, *projectDir, *useProxy)
		report.LaunchProbe = &probe
		report.Phase = "launch_probed"
		report.LastUpdated = time.Now().Format(time.RFC3339)
		if probe.Ready {
			report.RecommendedNext = "Start a real remote session and verify Hub visibility."
		} else {
			report.RecommendedNext = "Fix Claude launch readiness before starting a real session."
		}
		writeRemoteSmokeProgress(*progressFile, report)
	}

	if *doStart {
		session, err := app.StartRemoteSession(*toolName, *projectDir, *useProxy, "", RemoteLaunchSourceDesktop)
		if err != nil {
			fmt.Fprintf(os.Stderr, "remote-smoke: start failed: %v\n", err)
			report.Phase = "start_failed"
			report.LastUpdated = time.Now().Format(time.RFC3339)
			report.RecommendedNext = "Inspect the failed session summary or launch probe output."
			writeRemoteSmokeProgress(*progressFile, report)
			return 1
		}
		session = compactRemoteSmokeSessionView(session)
		report.StartedSession = &session
		report.Connection = app.GetRemoteConnectionStatus()
		report.Phase = "session_started"
		report.LastUpdated = time.Now().Format(time.RFC3339)
		report.RecommendedNext = "Verify the machine and session are visible from Hub."
		writeRemoteSmokeProgress(*progressFile, report)

		if *verifyHub {
			visibility, err := verifyRemoteHubVisibility(app, report, time.Duration(*verifyTimeoutSeconds)*time.Second)
			report.HubVisibility = &visibility
			report.LastUpdated = time.Now().Format(time.RFC3339)
			if visibility.Verified {
				report.Phase = "hub_verified"
				report.RecommendedNext = "Open Hub admin or PWA to inspect the live session."
			} else {
				report.Phase = "hub_verify_failed"
				report.RecommendedNext = "Check Hub debug endpoints and Desktop connection status."
			}
			writeRemoteSmokeProgress(*progressFile, report)
			if err != nil {
				if *jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					_ = enc.Encode(report)
				} else {
					printRemoteSmokeReport(report)
				}
				fmt.Fprintf(os.Stderr, "remote-smoke: hub verification failed: %v\n", err)
				return 1
			}
		}

		if *holdSeconds > 0 {
			report.Phase = "holding_session"
			report.LastUpdated = time.Now().Format(time.RFC3339)
			report.RecommendedNext = "Inspect Hub admin or PWA while the session is still alive."
			writeRemoteSmokeProgress(*progressFile, report)
			deadline := time.Now().Add(time.Duration(*holdSeconds) * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(2 * time.Second)
				report.Connection = app.GetRemoteConnectionStatus()
				refreshed := app.ListRemoteSessions()
				for i := range refreshed {
					if refreshed[i].ID == session.ID {
						sessionView := compactRemoteSmokeSessionView(refreshed[i])
						report.StartedSession = &sessionView
						break
					}
				}
				report.LastUpdated = time.Now().Format(time.RFC3339)
				writeRemoteSmokeProgress(*progressFile, report)
			}
		}
	}

	report.Success = true
	report.Phase = "completed"
	report.LastUpdated = time.Now().Format(time.RFC3339)
	report.RecommendedNext = "Use Hub admin or PWA to continue interacting with the live remote session."
	writeRemoteSmokeProgress(*progressFile, report)

	if *jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return 0
	}

	printRemoteSmokeReport(report)
	return 0
}

func writeRemoteSmokeProgress(path string, report RemoteSmokeReport) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o755)
	}

	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()

	snapshot := report
	if snapshot.StartedSession != nil {
		sessionView := compactRemoteSmokeSessionView(*snapshot.StartedSession)
		snapshot.StartedSession = &sessionView
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	_ = enc.Encode(snapshot)
}

func printRemoteSmokeReport(report RemoteSmokeReport) {
	fmt.Println("MaClaw Remote Smoke")
	fmt.Println("=====================")
	fmt.Printf("Tool: %s\n", remoteToolDisplayName(report.Tool))
	fmt.Printf("Project: %s\n", report.ProjectPath)
	fmt.Printf("Use Proxy: %v\n", report.UseProxy)
	fmt.Printf("Phase: %s\n", report.Phase)
	fmt.Printf("Success: %v\n", report.Success)
	if report.LastUpdated != "" {
		fmt.Printf("Last Updated: %s\n", report.LastUpdated)
	}
	if report.RecommendedNext != "" {
		fmt.Printf("Recommended Next: %s\n", report.RecommendedNext)
	}
	fmt.Println()

	fmt.Println("Connection")
	fmt.Printf("  Enabled: %v\n", report.Connection.Enabled)
	fmt.Printf("  Hub URL: %s\n", report.Connection.HubURL)
	fmt.Printf("  Machine ID: %s\n", report.Connection.MachineID)
	fmt.Printf("  Connected: %v\n", report.Connection.Connected)
	if report.Connection.LastError != "" {
		fmt.Printf("  Last Error: %s\n", report.Connection.LastError)
	}
	fmt.Printf("  Session Count: %d\n", report.Connection.SessionCount)
	fmt.Println()

	fmt.Println("Readiness")
	fmt.Printf("  Ready: %v\n", report.Readiness.Ready)
	fmt.Printf("  Tool Installed: %v\n", report.Readiness.ToolInstalled)
	fmt.Printf("  Model Configured: %v\n", report.Readiness.ModelConfigured)
	fmt.Printf("  Tool Path: %s\n", report.Readiness.ToolPath)
	fmt.Printf("  Command Path: %s\n", report.Readiness.CommandPath)
	fmt.Printf("  PTY Supported: %v\n", report.Readiness.PTYSupported)
	fmt.Printf("  PTY Message: %s\n", report.Readiness.PTYMessage)
	fmt.Printf("  Selected Model: %s\n", report.Readiness.SelectedModel)
	fmt.Printf("  Selected Model ID: %s\n", report.Readiness.SelectedModelID)
	for _, issue := range report.Readiness.Issues {
		fmt.Printf("  Issue: %s\n", issue)
	}
	for _, warning := range report.Readiness.Warnings {
		fmt.Printf("  Warning: %s\n", warning)
	}
	fmt.Println()

	if report.PTYProbe != nil {
		fmt.Println("ConPTY Probe")
		fmt.Printf("  Supported: %v\n", report.PTYProbe.Supported)
		fmt.Printf("  Ready: %v\n", report.PTYProbe.Ready)
		fmt.Printf("  Message: %s\n", report.PTYProbe.Message)
		fmt.Println()
	}

	if report.LaunchProbe != nil {
		fmt.Printf("%s Launch Probe\n", remoteToolDisplayName(report.Tool))
		fmt.Printf("  Supported: %v\n", report.LaunchProbe.Supported)
		fmt.Printf("  Ready: %v\n", report.LaunchProbe.Ready)
		fmt.Printf("  Command Path: %s\n", report.LaunchProbe.CommandPath)
		fmt.Printf("  Message: %s\n", report.LaunchProbe.Message)
		fmt.Println()
	}

	if report.Activation != nil {
		fmt.Println("Registration")
		fmt.Printf("  Status: %s\n", report.Activation.Status)
		fmt.Printf("  Email: %s\n", report.Activation.Email)
		fmt.Printf("  SN: %s\n", report.Activation.SN)
		fmt.Printf("  User ID: %s\n", report.Activation.UserID)
		fmt.Printf("  Machine ID: %s\n", report.Activation.MachineID)
		fmt.Println()
	}

	if report.StartedSession != nil {
		fmt.Println("Started Session")
		fmt.Printf("  ID: %s\n", report.StartedSession.ID)
		fmt.Printf("  Tool: %s\n", report.StartedSession.Tool)
		fmt.Printf("  Status: %s\n", report.StartedSession.Status)
		fmt.Printf("  Workspace: %s\n", report.StartedSession.WorkspacePath)
		fmt.Printf("  Current Task: %s\n", report.StartedSession.Summary.CurrentTask)
		fmt.Printf("  Last Result: %s\n", report.StartedSession.Summary.LastResult)
		if report.Connection.Connected {
			fmt.Printf("  Held Connection: %v\n", report.Connection.Connected)
		}
	}

	if report.HubVisibility != nil {
		fmt.Println()
		fmt.Println("Hub Visibility")
		fmt.Printf("  Attempted: %v\n", report.HubVisibility.Attempted)
		fmt.Printf("  Verified: %v\n", report.HubVisibility.Verified)
		fmt.Printf("  Hub URL: %s\n", report.HubVisibility.HubURL)
		fmt.Printf("  User ID: %s\n", report.HubVisibility.UserID)
		fmt.Printf("  Machine ID: %s\n", report.HubVisibility.MachineID)
		fmt.Printf("  Session ID: %s\n", report.HubVisibility.SessionID)
		fmt.Printf("  Machine Visible: %v\n", report.HubVisibility.MachineVisible)
		fmt.Printf("  Session Visible: %v\n", report.HubVisibility.SessionVisible)
		fmt.Printf("  Host Online: %v\n", report.HubVisibility.HostOnline)
		if report.HubVisibility.SessionStatus != "" {
			fmt.Printf("  Session Status: %s\n", report.HubVisibility.SessionStatus)
		}
		if report.HubVisibility.Message != "" {
			fmt.Printf("  Message: %s\n", report.HubVisibility.Message)
		}
	}
}

func verifyRemoteHubVisibility(app *App, report RemoteSmokeReport, timeout time.Duration) (RemoteHubVisibilityResult, error) {
	cfg, err := app.LoadConfig()
	if err != nil {
		return RemoteHubVisibilityResult{}, err
	}

	result := RemoteHubVisibilityResult{
		Attempted: true,
		HubURL:    strings.TrimRight(strings.TrimSpace(cfg.RemoteHubURL), "/"),
	}

	if result.HubURL == "" {
		result.Message = "remote hub url is empty"
		return result, fmt.Errorf("%s", result.Message)
	}

	if report.Activation != nil {
		result.UserID = report.Activation.UserID
		result.MachineID = report.Activation.MachineID
	}
	if result.UserID == "" {
		result.UserID = strings.TrimSpace(cfg.RemoteUserID)
	}
	if result.MachineID == "" {
		result.MachineID = strings.TrimSpace(cfg.RemoteMachineID)
	}
	if report.StartedSession != nil {
		result.SessionID = report.StartedSession.ID
	}

	if result.UserID == "" || result.MachineID == "" || result.SessionID == "" {
		result.Message = "missing user_id, machine_id or session_id for hub verification"
		return result, fmt.Errorf("%s", result.Message)
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		var machineResp struct {
			Machines []struct {
				ID        string `json:"id"`
				MachineID string `json:"machine_id"`
				Status    string `json:"status"`
			} `json:"machines"`
		}
		if err := getRemoteSmokeJSON(result.HubURL, "/api/debug/machines", url.Values{
			"user_id": []string{result.UserID},
		}, &machineResp); err != nil {
			lastErr = err
			time.Sleep(1500 * time.Millisecond)
			continue
		}

		for _, machine := range machineResp.Machines {
			machineID := machine.MachineID
			if machineID == "" {
				machineID = machine.ID
			}
			if machineID == result.MachineID {
				result.MachineVisible = true
				break
			}
		}

		var sessionResp struct {
			SessionID  string `json:"session_id"`
			HostOnline bool   `json:"host_online"`
			Summary    struct {
				Status string `json:"status"`
			} `json:"summary"`
		}
		if err := getRemoteSmokeJSON(result.HubURL, "/api/debug/session", url.Values{
			"user_id":    []string{result.UserID},
			"machine_id": []string{result.MachineID},
			"session_id": []string{result.SessionID},
		}, &sessionResp); err == nil {
			if sessionResp.SessionID == result.SessionID {
				result.SessionVisible = true
				result.HostOnline = sessionResp.HostOnline
				result.SessionStatus = sessionResp.Summary.Status
			}
		} else {
			lastErr = err
		}

		if result.MachineVisible && result.SessionVisible {
			result.Verified = true
			result.Message = "machine and session are visible from hub"
			return result, nil
		}

		time.Sleep(1500 * time.Millisecond)
	}

	if lastErr != nil {
		result.Message = lastErr.Error()
	} else {
		result.Message = "hub visibility timed out"
	}
	return result, fmt.Errorf("%s", result.Message)
}

func getRemoteSmokeJSON(baseURL, path string, query url.Values, target any) error {
	endpoint := strings.TrimRight(baseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	resp, err := http.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		var body map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&body)
		if msg, ok := body["message"].(string); ok && msg != "" {
			return fmt.Errorf("%s: %s", resp.Status, msg)
		}
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
