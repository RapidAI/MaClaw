package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestCheckVEApprovalCapability_EmptyVEID(t *testing.T) {
	app := &App{}
	enabled, err := app.CheckVEApprovalCapability("")
	if err == nil {
		t.Fatal("expected error for empty ve_id")
	}
	if enabled {
		t.Fatal("expected enabled=false for empty ve_id")
	}
	if err.Error() != "ve_id is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCheckVEApprovalCapability_LocalVE_Enabled(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}

	// Set up config with machine ID and approval config enabled.
	approvalCfg := VEApprovalConfig{Enabled: true, MaxQueueSize: 50, TimeoutHours: 24, DailyQuota: 100, ACL: AccessControlList{Mode: ACLWhitelist}}
	data, _ := json.Marshal(approvalCfg)
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-001"
		cfg.VEApprovalConfigJSON = string(data)
	})

	enabled, err := app.CheckVEApprovalCapability("local-machine-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled=true for local VE with approval capability enabled")
	}
}

func TestCheckVEApprovalCapability_LocalVE_Disabled(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}

	// Set up config with machine ID and approval config disabled (default).
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-002"
		cfg.VEApprovalConfigJSON = "" // empty = default = disabled
	})

	enabled, err := app.CheckVEApprovalCapability("local-machine-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatal("expected enabled=false for local VE with approval capability disabled")
	}
}

func TestCheckVEApprovalCapability_RemoteVE_Enabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ve/remote-ve-001/approval_capability" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": true})
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-003"
		cfg.RemoteHubURL = server.URL
		cfg.RemoteMachineToken = "test-token"
	})

	enabled, err := app.CheckVEApprovalCapability("remote-ve-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !enabled {
		t.Fatal("expected enabled=true for remote VE with approval capability enabled")
	}
}

func TestCheckVEApprovalCapability_RemoteVE_Disabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ve/remote-ve-002/approval_capability" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-004"
		cfg.RemoteHubURL = server.URL
		cfg.RemoteMachineToken = "test-token"
	})

	enabled, err := app.CheckVEApprovalCapability("remote-ve-002")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enabled {
		t.Fatal("expected enabled=false for remote VE with approval capability disabled")
	}
}

func TestCheckVEApprovalCapability_RemoteVE_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-005"
		cfg.RemoteHubURL = server.URL
		cfg.RemoteMachineToken = "test-token"
	})

	enabled, err := app.CheckVEApprovalCapability("nonexistent-ve")
	if err == nil {
		t.Fatal("expected error for nonexistent VE")
	}
	if enabled {
		t.Fatal("expected enabled=false for nonexistent VE")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestCheckVEApprovalCapability_NoHubURL(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-006"
		cfg.RemoteHubURL = "" // no hub URL
	})

	enabled, err := app.CheckVEApprovalCapability("some-remote-ve")
	if err == nil {
		t.Fatal("expected error when hub URL is not configured")
	}
	if enabled {
		t.Fatal("expected enabled=false when hub URL is not configured")
	}
	if !strings.Contains(err.Error(), "hub URL not configured") {
		t.Fatalf("expected 'hub URL not configured' in error, got: %v", err)
	}
}

func TestValidateVEApproverAssignment_Valid(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}

	approvalCfg := VEApprovalConfig{Enabled: true, MaxQueueSize: 50, TimeoutHours: 24, DailyQuota: 100, ACL: AccessControlList{Mode: ACLWhitelist}}
	data, _ := json.Marshal(approvalCfg)
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-007"
		cfg.VEApprovalConfigJSON = string(data)
	})

	err := app.ValidateVEApproverAssignment("local-machine-007")
	if err != nil {
		t.Fatalf("expected no error for valid assignment, got: %v", err)
	}
}

func TestValidateVEApproverAssignment_Invalid(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}

	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-008"
		cfg.VEApprovalConfigJSON = "" // disabled
	})

	err := app.ValidateVEApproverAssignment("local-machine-008")
	if err == nil {
		t.Fatal("expected error for VE without approval capability")
	}
	if !strings.Contains(err.Error(), "does not have approval capability enabled") {
		t.Fatalf("expected capability error message, got: %v", err)
	}
}

func TestCheckVEApprovalCapabilityStatus_Success(t *testing.T) {
	tempHome := t.TempDir()
	app := &App{testHomeDir: tempHome}

	approvalCfg := VEApprovalConfig{Enabled: true, MaxQueueSize: 50, TimeoutHours: 24, DailyQuota: 100, ACL: AccessControlList{Mode: ACLWhitelist}}
	data, _ := json.Marshal(approvalCfg)
	_ = app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.RemoteMachineID = "local-machine-009"
		cfg.VEApprovalConfigJSON = string(data)
	})

	status := app.CheckVEApprovalCapabilityStatus("local-machine-009")
	if status.VEID != "local-machine-009" {
		t.Fatalf("expected ve_id=local-machine-009, got %q", status.VEID)
	}
	if !status.HasCapability {
		t.Fatal("expected has_capability=true")
	}
	if !status.Enabled {
		t.Fatal("expected enabled=true")
	}
	if status.Error != "" {
		t.Fatalf("expected no error, got %q", status.Error)
	}
}

func TestCheckVEApprovalCapabilityStatus_EmptyVEID(t *testing.T) {
	app := &App{}
	status := app.CheckVEApprovalCapabilityStatus("")
	if status.HasCapability {
		t.Fatal("expected has_capability=false for empty ve_id")
	}
	if status.Error == "" {
		t.Fatal("expected error for empty ve_id")
	}
}
