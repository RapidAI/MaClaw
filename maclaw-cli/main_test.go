package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/doctor"
	coreim "github.com/RapidAI/CodeClaw/corelib/im"
	"github.com/RapidAI/CodeClaw/corelib/security"
)

func TestAskAutoDiscoversGUIConfig(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var ack coreim.ThirdPartyAckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gui-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			if got := r.URL.Query().Get("clientId"); got != defaultClientID {
				t.Fatalf("clientId = %q", got)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", []coreim.ThirdPartyOutgoingMessage{{
				ID:             "out-1",
				ConversationID: "conv-a",
				Type:           "text",
				Text:           "pong",
				CreatedAt:      1,
			}}, 1, false))
		case "/api/im-gateway/v1/ack":
			if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
				t.Fatalf("decode ack: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-ack"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{
		ThirdPartyGatewayEnabled: true,
		ThirdPartyGatewayToken:   "gui-token",
		ThirdPartyGatewayHost:    "127.0.0.1",
		ThirdPartyGatewayPort:    mustPort(t, server.URL),
	})
	t.Setenv("MACLAW_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_GATEWAY_URL", "")
	t.Setenv("MACLAW_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"ask", "--text", "ping", "--conversation", "conv-a", "--timeout", "0"}); err != nil {
		t.Fatalf("run ask: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != defaultClientID || incoming.Message.Text != "ping" || incoming.ConversationID != "conv-a" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if len(ack.MessageIDs) != 1 || ack.MessageIDs[0] != "out-1" {
		t.Fatalf("ack = %#v", ack)
	}
	if !strings.Contains(stdout.String(), `"text": "pong"`) {
		t.Fatalf("stdout missing response: %s", stdout.String())
	}
}

func TestAckRejectsInvalidStatusBeforeProtocolDefault(t *testing.T) {
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"ack", "--ids", "msg-1", "--status", "cancelled"})
	if err == nil || !strings.Contains(err.Error(), "--status") {
		t.Fatalf("expected status error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestAckRejectsInvalidInputBeforeToken(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing ids", args: []string{"ack", "--status", "read"}, want: "--ids"},
		{name: "bad status", args: []string{"ack", "--ids", "msg-1", "--status", "cancelled"}, want: "--status"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACLAW_GATEWAY_TOKEN", "")
			t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
			if strings.Contains(err.Error(), "token") {
				t.Fatalf("expected input validation before token lookup, got %v", err)
			}
		})
	}
}

func TestAckAllowsReadStatus(t *testing.T) {
	var ack coreim.ThirdPartyAckRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/ack" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&ack); err != nil {
			t.Fatalf("decode ack: %v", err)
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-ack"))
	}))
	defer server.Close()
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", server.URL+"/api/im-gateway/v1")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"ack", "--ids", "msg-1", "--status", "read"}); err != nil {
		t.Fatalf("ack read: %v stderr=%s", err, stderr.String())
	}
	if ack.Status != "read" || len(ack.MessageIDs) != 1 || ack.MessageIDs[0] != "msg-1" {
		t.Fatalf("ack = %#v", ack)
	}
}

func TestWatchRejectsNegativeCountBeforeToken(t *testing.T) {
	t.Setenv("MACLAW_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"watch", "--session", "task-json", "--count", "-1"})
	if err == nil || !strings.Contains(err.Error(), "--count") {
		t.Fatalf("expected count error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(err.Error(), "token") {
		t.Fatalf("expected count validation before token lookup, got %v", err)
	}
}

func TestCommonConfigRejectsInvalidValuesBeforeToken(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "timeout", args: []string{"poll", "--session", "task-json", "--timeout", "-1"}, want: "--timeout"},
		{name: "limit", args: []string{"poll", "--session", "task-json", "--limit", "0"}, want: "--limit"},
		{name: "lock timeout", args: []string{"continue", "--session", "task-json", "--lock-timeout", "0", "--text", "go"}, want: "--lock-timeout"},
		{name: "client id", args: []string{"continue", "--client", "!!!", "--session", "task-json", "--text", "go"}, want: "--client"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACLAW_GATEWAY_TOKEN", "")
			t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
			if strings.Contains(err.Error(), "token") {
				t.Fatalf("expected numeric validation before token lookup, got %v", err)
			}
		})
	}
}

func TestHandshakeAdvertisesTools(t *testing.T) {
	var handshake coreim.ThirdPartyHandshakeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&handshake); err != nil {
			t.Fatalf("decode handshake: %v", err)
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{RequestID: "req-h"}))
	}))
	defer server.Close()
	toolsPath := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"demo.echo","description":"Echo","risk":"read","inputSchema":{"type":"object"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"handshake", "--tools", toolsPath}); err != nil {
		t.Fatalf("handshake: %v stderr=%s", err, stderr.String())
	}
	if handshake.ClientID != defaultClientID || handshake.ClientName != "MaClaw CLI" || len(handshake.Tools) != 1 || handshake.Tools[0].Name != "demo.echo" {
		t.Fatalf("handshake = %#v", handshake)
	}
	if handshake.Capabilities["client_tools"] != true {
		t.Fatalf("capabilities missing client_tools: %#v", handshake.Capabilities)
	}
}

func TestHandshakeRejectsUnknownToolFieldBeforeRequest(t *testing.T) {
	toolsPath := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[{"name":"demo.echo","descripton":"typo","inputSchema":{"type":"object"}}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"handshake", "--tools", toolsPath})
	if err == nil || !strings.Contains(err.Error(), `unknown field "descripton"`) {
		t.Fatalf("expected unknown field error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestHandshakeRejectsDuplicateToolNamesBeforeRequest(t *testing.T) {
	toolsPath := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(toolsPath, []byte(`[
		{"name":"plc.read_register","description":"read one register"},
		{"name":"plc.read_register","description":"duplicate"}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"handshake", "--tools", toolsPath})
	if err == nil || !strings.Contains(err.Error(), `tools[1].name duplicate "plc.read_register"`) {
		t.Fatalf("expected duplicate tool error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestBaseURLTrailingSlashIsNormalized(t *testing.T) {
	var healthPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		healthPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"health", "--base", server.URL + "/api/im-gateway/v1/"})
	if err != nil {
		t.Fatalf("health: %v stderr=%s", err, stderr.String())
	}
	if healthPath != "/api/im-gateway/v1/health" {
		t.Fatalf("health path = %q", healthPath)
	}
}

func TestRequireTokenExplainsGUIAutoDiscovery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	err := c.run([]string{"send", "--config", filepath.Join(t.TempDir(), "missing.json"), "--text", "hello"})
	if err == nil || !strings.Contains(err.Error(), "enable GUI IM third-party access") {
		t.Fatalf("expected discovery hint, got %v", err)
	}
}

func TestBootstrapWritesZeroConfigGatewaySettings(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"bootstrap", "--config", cfgPath, "--host", "127.0.0.1", "--port", "18777"}); err != nil {
		t.Fatalf("bootstrap: %v stderr=%s", err, stderr.String())
	}
	cfg, err := loadAppConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ThirdPartyGatewayEnabled || cfg.ThirdPartyGatewayHost != "127.0.0.1" || cfg.ThirdPartyGatewayPort != 18777 {
		t.Fatalf("gateway config = %#v", cfg)
	}
	if len(cfg.ThirdPartyGatewayToken) != 64 {
		t.Fatalf("token length = %d", len(cfg.ThirdPartyGatewayToken))
	}
	if !cfg.IsThirdPartyGatewayLocalMode() {
		t.Fatal("expected local mode enabled")
	}
	if strings.Contains(stdout.String(), cfg.ThirdPartyGatewayToken) {
		t.Fatal("bootstrap output leaked token")
	}
}

func TestDoctorLocalOnlyReportsReadiness(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		MaclawLLMUrl:   "http://127.0.0.1:9/v1",
		MaclawLLMModel: "test-model",
		MaclawLLMKey:   "sk-test",
		OnboardingDone: true,
	})
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"doctor", "--local-only", "--config", cfgPath, "--pretty=false"})
	if err != nil {
		t.Fatalf("doctor --local-only: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, stdout.String())
	}
	if out["ok"] != true {
		t.Fatalf("ok=%v out=%s", out["ok"], stdout.String())
	}
	if out["localOnly"] != true {
		t.Fatalf("localOnly=%v", out["localOnly"])
	}
	readiness, _ := out["readiness"].(map[string]any)
	if readiness == nil {
		t.Fatalf("missing readiness: %s", stdout.String())
	}
	checks, _ := readiness["checks"].([]any)
	if len(checks) < 5 {
		t.Fatalf("expected readiness checks, got %#v", checks)
	}
	// Live gateway fields should be absent when local-only.
	if _, has := out["health"]; has {
		t.Fatalf("health should be omitted in local-only mode: %s", stdout.String())
	}
}

func TestDoctorLocalOnlyFailsWithoutLLM(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{})
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"doctor", "--local-only", "--config", cfgPath, "--pretty=false"})
	if err == nil {
		t.Fatalf("expected error without LLM, stdout=%s", stdout.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, stdout.String())
	}
	if out["ok"] != false {
		t.Fatalf("ok should be false: %s", stdout.String())
	}
	if _, has := out["readiness"]; !has {
		t.Fatalf("expected readiness report even on failure: %s", stdout.String())
	}
}

func TestSharedLoopEnableDisable(t *testing.T) {
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_SHADOW", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  false,
		SharedAgentLoopMigrated: true,
	})

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"shared-loop", "status", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("status: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	var status map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v: %s", err, stdout.String())
	}
	if status["mode"] != "off" || status["configEnabled"] != false {
		t.Fatalf("expected off, got %s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := c.run([]string{"shared-loop", "enable", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("enable: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	var enabled map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &enabled); err != nil {
		t.Fatalf("decode enable: %v: %s", err, stdout.String())
	}
	if enabled["mode"] != "on" || enabled["configEnabled"] != true || enabled["changed"] != true {
		t.Fatalf("expected enable on/changed, got %s", stdout.String())
	}
	if !strings.Contains(fmt.Sprint(enabled["summary"]), "shared-loop: on") {
		t.Fatalf("summary missing: %s", stdout.String())
	}

	// Persist: re-load via status after process-less write
	stdout.Reset()
	stderr.Reset()
	if err := c.run([]string{"shared-loop", "disable", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("disable: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	var disabled map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &disabled); err != nil {
		t.Fatalf("decode disable: %v: %s", err, stdout.String())
	}
	if disabled["mode"] != "off" || disabled["configEnabled"] != false {
		t.Fatalf("expected disable off, got %s", stdout.String())
	}

	// Env overrides config after enable
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "shadow")
	stdout.Reset()
	stderr.Reset()
	if err := c.run([]string{"shared-loop", "enable", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("enable under env: %v", err)
	}
	var shadowed map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &shadowed); err != nil {
		t.Fatalf("decode shadow: %v: %s", err, stdout.String())
	}
	if shadowed["mode"] != "shadow" || shadowed["envLocksMode"] != true {
		t.Fatalf("env should lock mode to shadow: %s", stdout.String())
	}
	if !strings.Contains(fmt.Sprint(shadowed["hint"]), "MACLAW_SHARED_AGENT_LOOP") {
		t.Fatalf("expected env lock hint: %s", stdout.String())
	}
}

func TestSharedLoopEnableWritesPercentAndWorkflow(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  false,
		SharedAgentLoopMigrated: true,
	})
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_WORKFLOW", "")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{
		"shared-loop", "enable", "--config", cfgPath, "--percent", "25", "--workflow", "on", "--pretty=false",
	}); err != nil {
		t.Fatalf("enable: %v %s", err, stderr.String())
	}
	appCfg, err := loadAppConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !appCfg.SharedAgentLoopEnabled {
		t.Fatal("expected enabled")
	}
	if appCfg.SharedAgentLoopCanaryPercent == nil || *appCfg.SharedAgentLoopCanaryPercent != 25 {
		t.Fatalf("percent=%v", appCfg.SharedAgentLoopCanaryPercent)
	}
	if !appCfg.SharedAgentLoopWorkflow {
		t.Fatal("expected workflow pilot")
	}
	env := doctor.ResolveSharedLoopEnv(appCfg)
	if env.Percent != 25 || !env.WorkflowPilot {
		t.Fatalf("resolved=%+v", env)
	}
}

func TestSharedLoopStatusCanaryPreviewUser(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"shared_agent_loop_enabled":true,"shared_agent_loop_migrated":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	t.Setenv("MACLAW_SHARED_AGENT_LOOP_PERCENT", "50")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{
		"shared-loop", "status", "--config", cfgPath, "--user", "sticky-user-xyz", "--pretty=false",
	}); err != nil {
		t.Fatalf("err=%v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("json: %v body=%s", err, stdout.String())
	}
	preview, ok := out["canaryPreview"].(map[string]any)
	if !ok {
		t.Fatalf("missing canaryPreview: %#v", out)
	}
	if preview["user_id"] != "sticky-user-xyz" {
		t.Fatalf("preview=%#v", preview)
	}
	if _, ok := preview["bucket"]; !ok {
		t.Fatalf("bucket missing: %#v", preview)
	}
	if _, ok := preview["allows"]; !ok {
		t.Fatalf("allows missing: %#v", preview)
	}
	sum := fmt.Sprint(out["summary"])
	if !strings.Contains(sum, "canary-preview:") {
		t.Fatalf("summary=%q", sum)
	}
}

func TestSharedLoopStatusIncludesAdaptivePrompt(t *testing.T) {
	// Ensure status payload always has adaptivePrompt object (may be zeroed).
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  true,
		SharedAgentLoopMigrated: true,
	})
	t.Setenv("MACLAW_SHARED_AGENT_LOOP", "")
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"shared-loop", "status", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("status: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, stdout.String())
	}
	ap, ok := out["adaptivePrompt"].(map[string]any)
	if !ok || ap == nil {
		t.Fatalf("missing adaptivePrompt: %s", stdout.String())
	}
	if _, ok := ap["lightTurns"]; !ok {
		t.Fatalf("adaptivePrompt fields incomplete: %#v", ap)
	}
	if _, ok := ap["estTokensSaved"]; !ok {
		t.Fatalf("missing estTokensSaved: %#v", ap)
	}
	if ap["envKey"] != agent.PromptProfileEnvKey {
		t.Fatalf("envKey=%v want %s", ap["envKey"], agent.PromptProfileEnvKey)
	}
	if _, ok := ap["envOverride"]; !ok {
		t.Fatalf("missing envOverride: %#v", ap)
	}
}

func TestSharedLoopStatsExposesEnvOverride(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  true,
		SharedAgentLoopMigrated: true,
	})
	t.Setenv(agent.PromptProfileEnvKey, "full")
	agent.ResetPromptProfileStatsForTest()

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"shared-loop", "stats", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("stats: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, stdout.String())
	}
	ap, _ := out["adaptivePrompt"].(map[string]any)
	if ap == nil {
		t.Fatalf("missing adaptivePrompt: %s", stdout.String())
	}
	if ap["envOverride"] != true {
		t.Fatalf("envOverride=%v want true: %#v", ap["envOverride"], ap)
	}
	if ap["forcedProfile"] != "full" {
		t.Fatalf("forcedProfile=%v want full", ap["forcedProfile"])
	}
	if sum, _ := ap["summary"].(string); !strings.Contains(sum, agent.PromptProfileEnvKey+"=full") {
		t.Fatalf("summary missing env force: %q", sum)
	}
}

func TestSharedLoopExportAndMerge(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  true,
		SharedAgentLoopMigrated: true,
	})
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileDecision(agent.PromptProfileDecision{
		Profile:     agent.PromptProfileLight,
		FullTokens:  4000,
		LightTokens: 1000,
		Task:        "fast",
	})
	agent.RecordLightToolDeny("bash")

	dir := t.TempDir()
	outA := filepath.Join(dir, "a.json")
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"shared-loop", "export", "--config", cfgPath, "--out", outA, "--pretty=false"}); err != nil {
		t.Fatalf("export: %v stderr=%s", err, stderr.String())
	}
	var expOut map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &expOut); err != nil {
		t.Fatalf("decode export: %v: %s", err, stdout.String())
	}
	if expOut["action"] != "export" {
		t.Fatalf("action=%v", expOut["action"])
	}
	if expOut["written"] != outA {
		t.Fatalf("written=%v", expOut["written"])
	}

	// Second synthetic export for merge.
	outB := filepath.Join(dir, "b.json")
	b := agent.PromptProfileExport{
		SchemaVersion: 1,
		ExportedAt:    "2099-01-01T00:00:00Z",
		Host:          "other-host",
		Stats: agent.PromptProfileStats{
			LightTurns:     1,
			FullTurns:      2,
			EstTokensSaved: 100,
			LightUpgrades:  1,
		},
	}
	if err := agent.WritePromptProfileExport(outB, b); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := c.run([]string{"shared-loop", "merge-exports", outA, outB, "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("merge: %v stderr=%s", err, stderr.String())
	}
	var mergeOut map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &mergeOut); err != nil {
		t.Fatalf("decode merge: %v: %s", err, stdout.String())
	}
	if mergeOut["action"] != "merge-exports" {
		t.Fatalf("action=%v", mergeOut["action"])
	}
	merged, _ := mergeOut["merged"].(map[string]any)
	if merged == nil {
		t.Fatalf("missing merged: %s", stdout.String())
	}
	stats, _ := merged["stats"].(map[string]any)
	if stats == nil {
		t.Fatalf("missing stats: %#v", merged)
	}
	// light 1+1=2, full 0+2=2
	if n, _ := stats["light_turns"].(float64); n != 2 {
		t.Fatalf("light_turns=%v stats=%#v", stats["light_turns"], stats)
	}
	if n, _ := stats["full_turns"].(float64); n != 2 {
		t.Fatalf("full_turns=%v", stats["full_turns"])
	}
}

func TestCLICostAndDenialPause(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{DailyLLMBudgetUSD: 3.5})
	security.ResetProcessDenialLedgerForTest()
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"cost", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("cost: %v %s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["action"] != "cost" {
		t.Fatalf("%#v", out)
	}
	if out["dailyBudgetUSD"] != 3.5 && out["dailyBudgetUSD"] != float64(3.5) {
		t.Fatalf("budget=%#v", out["dailyBudgetUSD"])
	}
	if _, ok := out["llmCostDaily"]; !ok {
		t.Fatalf("expected llmCostDaily in cost payload: %#v", out)
	}
	stdout.Reset()
	stderr.Reset()
	if err := testCLI(&stdout, &stderr).run([]string{"denial-pause", "clear", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("denial-pause: %v", err)
	}
}

func TestNormalizeHubBaseURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://hub.example", "https://hub.example"},
		{"https://hub.example/", "https://hub.example"},
		{"https://hub.example/api", "https://hub.example"},
		{"https://hub.example/api/", "https://hub.example"},
		{"https://hub.example/api/admin", "https://hub.example"},
		{"https://hub.example/api/admin/adaptive-prompt/metrics", "https://hub.example"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := normalizeHubBaseURL(tc.in); got != tc.want {
			t.Fatalf("normalizeHubBaseURL(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestSharedLoopHubMetrics(t *testing.T) {
	var gotAuth, gotTenant string
	paths := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		paths[r.URL.Path]++
		gotTenant = r.URL.Query().Get("tenant_id")
		if r.Method != http.MethodGet {
			t.Errorf("method=%s", r.Method)
		}
		switch r.URL.Path {
		case "/api/admin/cost-ops/metrics":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                  true,
				"online_machines":     3,
				"machines_with_stats": 1,
				"totals": map[string]any{
					"route_decisions": 12,
					"daily_cost_usd":  0.5,
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                  true,
				"online_machines":     3,
				"machines_with_stats": 2,
				"totals": map[string]any{
					"light_turns": 10,
					"full_turns":  5,
				},
			})
		}
	}))
	t.Cleanup(server.Close)

	cfgPath := writeConfig(t, corelib.AppConfig{
		RemoteHubURL: server.URL + "/",
	})
	t.Setenv("MACLAW_HUB_ADMIN_TOKEN", "")
	t.Setenv("MACLAW_ADMIN_TOKEN", "")
	t.Setenv("MACLAW_HUB_URL", "")

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{
		"shared-loop", "hub-metrics",
		"--config", cfgPath,
		"--admin-token", "admin-jwt",
		"--tenant", "tenant_acme",
		"--pretty=false",
	}); err != nil {
		t.Fatalf("hub-metrics: %v stderr=%s", err, stderr.String())
	}
	if gotAuth != "Bearer admin-jwt" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
	if paths["/api/admin/adaptive-prompt/metrics"] != 1 {
		t.Fatalf("adaptive path hits=%v", paths)
	}
	if paths["/api/admin/cost-ops/metrics"] != 1 {
		t.Fatalf("cost-ops path hits=%v", paths)
	}
	if gotTenant != "tenant_acme" {
		t.Fatalf("tenant=%q", gotTenant)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, stdout.String())
	}
	if out["action"] != "hub-metrics" {
		t.Fatalf("action=%v", out["action"])
	}
	metrics, _ := out["metrics"].(map[string]any)
	if metrics == nil {
		t.Fatalf("missing metrics: %s", stdout.String())
	}
	if n, _ := metrics["online_machines"].(float64); n != 3 {
		t.Fatalf("online_machines=%v", metrics["online_machines"])
	}
	costOps, _ := out["costOps"].(map[string]any)
	if costOps == nil {
		t.Fatalf("missing costOps: %s", stdout.String())
	}
	if ok, _ := costOps["ok"].(bool); !ok {
		t.Fatalf("costOps=%#v", costOps)
	}
}

func TestSharedLoopHubMetricsRequiresToken(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		RemoteHubURL: "https://hub.example",
	})
	t.Setenv("MACLAW_HUB_ADMIN_TOKEN", "")
	t.Setenv("MACLAW_ADMIN_TOKEN", "")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"shared-loop", "hub-metrics", "--config", cfgPath, "--pretty=false"})
	if err == nil {
		t.Fatal("expected error without admin token")
	}
	if !strings.Contains(err.Error(), "admin token") {
		t.Fatalf("err=%v", err)
	}
}

func TestSharedLoopStatsAndReset(t *testing.T) {
	cfgPath := writeConfig(t, corelib.AppConfig{
		SharedAgentLoopEnabled:  true,
		SharedAgentLoopMigrated: true,
	})
	// Seed counters in this process (CLI shares agent package state).
	agent.ResetPromptProfileStatsForTest()
	agent.RecordPromptProfileSavings(agent.PromptProfileLight, 4000, 1000)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"shared-loop", "stats", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("stats: %v stderr=%s", err, stderr.String())
	}
	var stats map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v: %s", err, stdout.String())
	}
	if stats["action"] != "stats" {
		t.Fatalf("action=%v", stats["action"])
	}
	ap, _ := stats["adaptivePrompt"].(map[string]any)
	if ap == nil {
		t.Fatalf("missing adaptivePrompt: %s", stdout.String())
	}
	// lightTurns may be float64 from JSON
	if n, _ := ap["estTokensSaved"].(float64); n < 3000 {
		t.Fatalf("estTokensSaved=%v out=%s", ap["estTokensSaved"], stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := c.run([]string{"shared-loop", "stats-reset", "--config", cfgPath, "--pretty=false"}); err != nil {
		t.Fatalf("stats-reset: %v", err)
	}
	var reset map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &reset); err != nil {
		t.Fatalf("decode reset: %v: %s", err, stdout.String())
	}
	if reset["action"] != "stats-reset" || reset["changed"] != true {
		t.Fatalf("reset=%s", stdout.String())
	}
	ap2, _ := reset["adaptivePrompt"].(map[string]any)
	if ap2 == nil {
		t.Fatal("missing adaptivePrompt after reset")
	}
	if n, _ := ap2["lightTurns"].(float64); n != 0 {
		t.Fatalf("lightTurns after reset=%v", ap2["lightTurns"])
	}
	if n, _ := ap2["estTokensSaved"].(float64); n != 0 {
		t.Fatalf("estTokensSaved after reset=%v", ap2["estTokensSaved"])
	}
}

func TestInvokeBootstrapSupportsConfigFields(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	req := fmt.Sprintf(`{"action":"bootstrap","configPath":%q,"bootstrapHost":"0.0.0.0","bootstrapPort":18888,"forceToken":true}`, cfgPath)
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke bootstrap: %v stderr=%s", err, stderr.String())
	}
	cfg, err := loadAppConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ThirdPartyGatewayEnabled || cfg.ThirdPartyGatewayHost != "0.0.0.0" || cfg.ThirdPartyGatewayPort != 18888 {
		t.Fatalf("gateway config = %#v", cfg)
	}
	if strings.Contains(stdout.String(), cfg.ThirdPartyGatewayToken) {
		t.Fatal("bootstrap output leaked token")
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode bootstrap output: %v: %s", err, stdout.String())
	}
	if out["baseUrl"] != "http://127.0.0.1:18888/api/im-gateway/v1" {
		t.Fatalf("baseUrl = %#v", out["baseUrl"])
	}
}

func TestAskCreatesAndReusesStatefulSession(t *testing.T) {
	var conversations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			var incoming coreim.ThirdPartyIncomingRequest
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			conversations = append(conversations, incoming.ConversationID)
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", []coreim.ThirdPartyOutgoingMessage{{
				ID:             "out-1",
				ConversationID: "ignored",
				Type:           "text",
				Text:           "ok",
				CreatedAt:      1,
			}}, 7, false))
		case "/api/im-gateway/v1/ack":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-ack"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"ask", "--text", "start", "--timeout", "0"}); err != nil {
		t.Fatalf("first ask: %v stderr=%s", err, stderr.String())
	}
	if err := c.run([]string{"ask", "--text", "continue", "--timeout", "0"}); err != nil {
		t.Fatalf("second ask: %v stderr=%s", err, stderr.String())
	}
	if len(conversations) != 2 || conversations[0] == "" || conversations[0] != conversations[1] {
		t.Fatalf("conversations = %#v", conversations)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != conversations[0] {
		t.Fatalf("current session = %q want %q", st.CurrentSession, conversations[0])
	}
	sess := findSession(st, st.CurrentSession)
	if sess == nil || sess.Cursor != "7" {
		t.Fatalf("session state = %#v", st)
	}
}

func TestSessionUseCommandSetsCurrentSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "use", "project-42"}); err != nil {
		t.Fatalf("session use: %v stderr=%s", err, stderr.String())
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != "project-42" || findSession(st, "project-42") == nil {
		t.Fatalf("state = %#v", st)
	}
}

func TestSessionUsePreservesExistingCursor(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "other", Sessions: []sessionState{{ID: "project-42", ClientID: defaultClientID, Cursor: "13", CreatedAt: 1, UpdatedAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "use", "project-42"}); err != nil {
		t.Fatalf("session use: %v stderr=%s", err, stderr.String())
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	sess := findSessionForClient(st, "project-42", defaultClientID)
	if st.CurrentSession != "project-42" || sess == nil || sess.Cursor != "13" {
		t.Fatalf("state = %#v", st)
	}
}

func TestSessionNewRejectsExistingID(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{Sessions: []sessionState{{ID: "project-42", ClientID: defaultClientID, Cursor: "13", CreatedAt: 1, UpdatedAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"session", "new", "--id", "project-42"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate session error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	st, loadErr := loadCLIState(statePath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if sess := findSessionForClient(st, "project-42", defaultClientID); sess == nil || sess.Cursor != "13" {
		t.Fatalf("state = %#v", st)
	}
}

func TestRequireSessionRejectsImplicitCurrentSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "global", Sessions: []sessionState{{ID: "global", Cursor: "0", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"continue", "--require-session", "should fail"})
	if err == nil || !strings.Contains(err.Error(), "missing explicit session") {
		t.Fatalf("expected explicit session error, got %v", err)
	}
}

func TestContinueAliasUsesPositionalPromptAndSavedCursor(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var pollCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			pollCursor = r.URL.Query().Get("cursor")
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 12, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "project-42", Sessions: []sessionState{{ID: "project-42", Cursor: "9", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"continue", "--timeout", "0", "keep", "going"}); err != nil {
		t.Fatalf("continue: %v stderr=%s", err, stderr.String())
	}
	if incoming.ConversationID != "project-42" || incoming.Message.Text != "keep going" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if pollCursor != "9" {
		t.Fatalf("poll cursor = %q", pollCursor)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if sess := findSessionForClient(st, "project-42", defaultClientID); sess == nil || sess.Cursor != "12" {
		t.Fatalf("state = %#v", st)
	}
}

func TestPromptCommandsAcceptKnownFlagsAfterText(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var pollCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			pollCursor = r.URL.Query().Get("cursor")
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 5, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "old", Sessions: []sessionState{{ID: "task-123", ClientID: defaultClientID, Cursor: "4", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"continue", "keep", "going", "--session", "task-123", "--timeout", "0"})
	if err != nil {
		t.Fatalf("continue: %v stderr=%s", err, stderr.String())
	}
	if incoming.ConversationID != "task-123" || incoming.Message.Text != "keep going" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if pollCursor != "4" {
		t.Fatalf("poll cursor = %q", pollCursor)
	}
}

func TestDefaultMessageIDsHaveEntropy(t *testing.T) {
	var messageIDs []string
	var eventIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			var incoming coreim.ThirdPartyIncomingRequest
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			messageIDs = append(messageIDs, incoming.MessageID)
			eventIDs = append(eventIDs, incoming.EventID)
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 1, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	c := testCLI(&bytes.Buffer{}, &bytes.Buffer{})
	if err := c.run([]string{"continue", "--session", "task-123", "--timeout", "0", "first"}); err != nil {
		t.Fatalf("first continue: %v", err)
	}
	if err := c.run([]string{"continue", "--session", "task-123", "--timeout", "0", "second"}); err != nil {
		t.Fatalf("second continue: %v", err)
	}
	if len(messageIDs) != 2 || messageIDs[0] == messageIDs[1] || eventIDs[0] == eventIDs[1] {
		t.Fatalf("ids should be unique: message=%#v event=%#v", messageIDs, eventIDs)
	}
	for _, id := range messageIDs {
		if !strings.HasPrefix(id, "maclaw_cli_") || len(strings.Split(id, "_")) < 4 {
			t.Fatalf("message id format = %q", id)
		}
	}
}

func TestUnknownPromptFlagsRemainText(t *testing.T) {
	_, fs := newFlagSet("ask")
	got := reorderKnownFlags(fs, []string{"keep", "--literal", "value", "--timeout", "0"})
	if strings.Join(got, " ") != "--timeout 0 keep --literal value" {
		t.Fatalf("reordered args = %#v", got)
	}
	cleaned := strings.Join(cleanPromptArgs(got[2:]), " ")
	if cleaned != "keep --literal value" {
		t.Fatalf("cleaned prompt = %q", cleaned)
	}
}

func TestConversationOverridesSessionForProtocolID(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 1, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"continue", "--session", "task-123", "--conversation", "external-9", "--timeout", "0", "go"}); err != nil {
		t.Fatalf("continue: %v stderr=%s", err, stderr.String())
	}
	if incoming.ConversationID != "external-9" {
		t.Fatalf("conversation = %q", incoming.ConversationID)
	}
	var out askResult
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out.SessionID != "task-123" || out.ConversationID != "external-9" {
		t.Fatalf("out ids = %#v", out)
	}
	st, err := loadCLIState(os.Getenv("MACLAW_CLI_STATE"))
	if err != nil {
		t.Fatal(err)
	}
	if findSessionForClient(st, "external-9", defaultClientID) != nil {
		t.Fatalf("conversation override created state session: %#v", st)
	}
	if sess := findSessionForClient(st, "task-123", defaultClientID); sess == nil || sess.Cursor != "1" {
		t.Fatalf("state session = %#v", st)
	}
}

func TestExplicitSessionLoadsSavedCursorForSingleShotAgentCalls(t *testing.T) {
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursors = append(cursors, r.URL.Query().Get("cursor"))
			next := int64(3)
			if len(cursors) == 2 {
				next = 6
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var out1, err1 bytes.Buffer
	if err := testCLI(&out1, &err1).run([]string{"continue", "--session", "task-123", "--timeout", "0", "first"}); err != nil {
		t.Fatalf("first call: %v stderr=%s", err, err1.String())
	}
	var out2, err2 bytes.Buffer
	if err := testCLI(&out2, &err2).run([]string{"continue", "--session", "task-123", "--timeout", "0", "second"}); err != nil {
		t.Fatalf("second call: %v stderr=%s", err, err2.String())
	}
	if got := strings.Join(cursors, ","); got != "0,3" {
		t.Fatalf("cursors = %s", got)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if sess := findSession(st, "task-123"); sess == nil || sess.Cursor != "6" {
		t.Fatalf("state = %#v", st)
	}
}

func TestInvokeContinueUsesJSONRequest(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	var pollCursor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			pollCursor = r.URL.Query().Get("cursor")
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, 4, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	req := `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"from json","timeoutSec":1,"ack":false,"pretty":false,"metadata":{"source":"planner"}}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != "planner" || incoming.ConversationID != "task-json" || incoming.Message.Text != "from json" {
		t.Fatalf("incoming = %#v", incoming)
	}
	if incoming.Metadata["source"] != "planner" {
		t.Fatalf("incoming metadata = %#v", incoming.Metadata)
	}
	if pollCursor != "0" {
		t.Fatalf("poll cursor = %q", pollCursor)
	}
	var out askResult
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode invoke output: %v: %s", err, stdout.String())
	}
	if out.SessionID != "task-json" || out.NextCursor != "4" {
		t.Fatalf("out = %#v", out)
	}
}

func TestInvokeDryRunPrintsCommandMapping(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"continue","baseUrl":"http://127.0.0.1:18777/api/im-gateway/v1/","token":"secret-token-value","configPath":"cfg.json","statePath":"state.json","clientId":"planner","clientName":"Planner Agent","sessionId":"task-json","userId":"agent-7","userName":"Agent Seven","text":"from json","lockTimeoutSec":9}`})
	if err != nil {
		t.Fatalf("invoke dry-run: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	if out["ok"] != true || out["action"] != "continue" {
		t.Fatalf("dry-run output = %#v", out)
	}
	argv, ok := out["argv"].([]any)
	if !ok || len(argv) == 0 || argv[0] != "continue" {
		t.Fatalf("argv = %#v", out["argv"])
	}
	if strings.Contains(stdout.String(), "secret-token-value") {
		t.Fatalf("dry-run leaked token: %s", stdout.String())
	}
	if !containsAnyAdjacent(argv, "--base", "http://127.0.0.1:18777/api/im-gateway/v1") ||
		!containsAnyAdjacent(argv, "--token", "[redacted]") ||
		!containsAnyAdjacent(argv, "--config", "cfg.json") ||
		!containsAnyAdjacent(argv, "--state", "state.json") ||
		!containsAnyAdjacent(argv, "--client", "planner") ||
		!containsAnyAdjacent(argv, "--client-name", "Planner Agent") ||
		!containsAnyAdjacent(argv, "--session", "task-json") ||
		!containsAnyAdjacent(argv, "--user", "agent-7") ||
		!containsAnyAdjacent(argv, "--name", "Agent Seven") ||
		!containsAnyAdjacent(argv, "--lock-timeout", "9") {
		t.Fatalf("argv missing expected flags: %#v", argv)
	}
	if !containsAnyValue(argv, "--require-session") {
		t.Fatalf("stateful argv missing require-session: %#v", argv)
	}
}

func TestRedactedDryRunArgvRedactsTokenForms(t *testing.T) {
	got := redactedDryRunArgv([]string{"continue", "--token", "secret-a", "--token=secret-b", "--text", "keep"})
	want := []string{"continue", "--token", "[redacted]", "--token=[redacted]", "--text", "keep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("redacted argv = %#v", got)
	}
}

func TestInvokeDryRunNormalizesAskAndRun(t *testing.T) {
	for _, action := range []string{"ask", "run"} {
		var stdout, stderr bytes.Buffer
		err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", fmt.Sprintf(`{"action":%q,"clientId":"planner","sessionId":"task-json","text":"from json"}`, action)})
		if err != nil {
			t.Fatalf("invoke dry-run %s: %v stderr=%s", action, err, stderr.String())
		}
		var out map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
			t.Fatalf("decode dry-run %s: %v: %s", action, err, stdout.String())
		}
		if out["action"] != "continue" {
			t.Fatalf("dry-run action for %s = %#v", action, out["action"])
		}
		argv := out["argv"].([]any)
		if argv[0] != "continue" {
			t.Fatalf("argv for %s = %#v", action, argv)
		}
	}
}

func TestInvokeDryRunValidatesMetadata(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"from json","metadata":{"source":"agent"}}`})
	if err != nil {
		t.Fatalf("invoke dry-run metadata: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	if !containsAnyAdjacent(out["argv"].([]any), "--metadata-json", `{"source":"agent"}`) {
		t.Fatalf("argv missing metadata: %#v", out["argv"])
	}

	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"from json","metadata":{"attempt":1}}`})
	if err == nil || !strings.Contains(err.Error(), `metadata "attempt" must be a string`) {
		t.Fatalf("expected metadata validation error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestInvokeDryRunPreservesExplicitZeroTimeout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"poll","clientId":"planner","sessionId":"task-json","timeoutSec":0}`})
	if err != nil {
		t.Fatalf("invoke dry-run timeout zero: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	if !containsAnyAdjacent(out["argv"].([]any), "--timeout", "0") {
		t.Fatalf("argv missing timeout zero: %#v", out["argv"])
	}

	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"poll","clientId":"planner","sessionId":"task-json","timeoutSec":-1}`})
	if err == nil || !strings.Contains(err.Error(), "timeoutSec") {
		t.Fatalf("expected timeoutSec error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestInvokeDryRunRejectsInvalidPositiveOnlyFields(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want string
	}{
		{name: "lock timeout", req: `{"action":"poll","clientId":"planner","sessionId":"task-json","lockTimeoutSec":0}`, want: "lockTimeoutSec"},
		{name: "limit", req: `{"action":"poll","clientId":"planner","sessionId":"task-json","limit":0}`, want: "limit"},
		{name: "count", req: `{"action":"watch","clientId":"planner","sessionId":"task-json","count":0}`, want: "count"},
		{name: "wait polls", req: `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"go","waitPolls":0}`, want: "waitPolls"},
		{name: "bootstrap port", req: `{"action":"bootstrap","bootstrapPort":0}`, want: "bootstrapPort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %s error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeDryRunContinueRequiresText(t *testing.T) {
	for _, action := range []string{"continue", "ask", "run"} {
		t.Run(action, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", fmt.Sprintf(`{"action":%q,"clientId":"planner","sessionId":"task-json"}`, action)})
			if err == nil || !strings.Contains(err.Error(), "requires text") {
				t.Fatalf("expected text error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeRejectsExplicitBlankAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"  ","text":"go"}`})
	if err == nil || !strings.Contains(err.Error(), "action must not be blank") {
		t.Fatalf("expected blank action error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestInvokeOmittedActionDefaultsToContinue(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"clientId":"planner","sessionId":"task-json","text":"go"}`})
	if err != nil {
		t.Fatalf("invoke default action: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	if out["action"] != "continue" {
		t.Fatalf("default action = %#v", out["action"])
	}
}

func TestDirectTextCommandsRejectEmptyBeforeConfig(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "send", args: []string{"send", "--session", "task-json"}, want: "send requires"},
		{name: "continue", args: []string{"continue", "--session", "task-json"}, want: "continue requires"},
		{name: "ask", args: []string{"ask", "--session", "task-json"}, want: "continue requires"},
		{name: "wait polls", args: []string{"continue", "--session", "task-json", "--wait-polls", "-1", "--text", "go"}, want: "--wait-polls"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACLAW_GATEWAY_TOKEN", "")
			t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
			if strings.Contains(err.Error(), "token") {
				t.Fatalf("expected input validation before token lookup, got %v", err)
			}
		})
	}
}

func TestInvokeDryRunRejectsUnsupportedFieldsForAction(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want string
	}{
		{
			name: "tools on continue",
			req:  `{"action":"continue","clientId":"planner","sessionId":"task-json","text":"go","toolsPath":"tools.json"}`,
			want: "toolsPath",
		},
		{
			name: "message ids on poll",
			req:  `{"action":"poll","clientId":"planner","sessionId":"task-json","messageIds":["msg-1"]}`,
			want: "messageIds",
		},
		{
			name: "message on tool result",
			req:  `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","toolCallId":"tc-1","status":"success","message":{"type":"text","text":"wrong"}}`,
			want: "message",
		},
		{
			name: "cursor on send",
			req:  `{"action":"send","clientId":"planner","sessionId":"task-json","text":"go","cursor":"5"}`,
			want: "cursor",
		},
		{
			name: "bootstrap host on doctor",
			req:  `{"action":"doctor","bootstrapHost":"127.0.0.1"}`,
			want: "bootstrapHost",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeDryRunSendRequiresTextOrMessage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"send","clientId":"planner","sessionId":"task-json","attachments":[{"type":"file","id":"media-1"}]}`})
	if err == nil || !strings.Contains(err.Error(), "text or message") {
		t.Fatalf("expected send payload error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestInvokeDryRunMapsBootstrapFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"bootstrap","configPath":"cfg.json","sessionId":"ignored-session","conversationId":"ignored-conv","bootstrapHost":"0.0.0.0","bootstrapPort":18888,"forceToken":true}`})
	if err != nil {
		t.Fatalf("invoke dry-run bootstrap: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
	}
	argv := out["argv"].([]any)
	if out["action"] != "bootstrap" ||
		!containsAnyAdjacent(argv, "--config", "cfg.json") ||
		!containsAnyAdjacent(argv, "--host", "0.0.0.0") ||
		!containsAnyAdjacent(argv, "--port", "18888") ||
		!containsAnyValue(argv, "--force-token=true") {
		t.Fatalf("bootstrap argv = %#v", argv)
	}
	if containsAnyValue(argv, "--require-session") {
		t.Fatalf("bootstrap argv should not require session: %#v", argv)
	}
	if containsAnyValue(argv, "--session") || containsAnyValue(argv, "--conversation") {
		t.Fatalf("bootstrap argv should ignore session fields: %#v", argv)
	}
}

func TestInvokeDryRunRejectsInvalidBootstrapPort(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"bootstrap","bootstrapPort":70000}`})
	if err == nil || !strings.Contains(err.Error(), "bootstrapPort") {
		t.Fatalf("expected bootstrapPort error, got %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
}

func TestInvokeHandshakeSupportsClientName(t *testing.T) {
	var handshake coreim.ThirdPartyHandshakeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&handshake); err != nil {
			t.Fatalf("decode handshake: %v", err)
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{RequestID: "req-h"}))
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)

	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(`{"action":"handshake","clientId":"planner","clientName":"Planner Agent"}`)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke handshake: %v stderr=%s", err, stderr.String())
	}
	if handshake.ClientID != "planner" || handshake.ClientName != "Planner Agent" {
		t.Fatalf("handshake = %#v", handshake)
	}
}

func TestInvokeDryRunNonStatefulActionsIgnoreSessionFields(t *testing.T) {
	cases := []struct {
		name string
		req  string
	}{
		{name: "handshake", req: `{"action":"handshake","clientId":"planner","clientName":"Planner Agent","sessionId":"task-json","conversationId":"conv-json"}`},
		{name: "ack", req: `{"action":"ack","clientId":"planner","sessionId":"task-json","conversationId":"conv-json","messageIds":["msg-1"],"status":"read"}`},
		{name: "doctor", req: `{"action":"doctor","clientId":"planner","sessionId":"task-json","conversationId":"conv-json"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err != nil {
				t.Fatalf("dry-run %s: %v stderr=%s", tc.name, err, stderr.String())
			}
			var out map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
				t.Fatalf("decode dry-run: %v: %s", err, stdout.String())
			}
			argv := out["argv"].([]any)
			if containsAnyValue(argv, "--session") || containsAnyValue(argv, "--conversation") || containsAnyValue(argv, "--require-session") {
				t.Fatalf("%s argv should ignore session fields: %#v", tc.name, argv)
			}
		})
	}
}

func TestInvokeRejectsUnknownAndTrailingFields(t *testing.T) {
	var req invokeRequest
	if err := decodeInvokeRequest([]byte(`{"action":"poll","client":"typo"}`), &req); err == nil || !strings.Contains(err.Error(), `unknown field "client"`) {
		t.Fatalf("unknown field error = %v", err)
	}
	if err := decodeInvokeRequest([]byte(`{"action":"poll"} {"action":"poll"}`), &req); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("trailing JSON error = %v", err)
	}
}

func TestInvokeSendSupportsRichPayload(t *testing.T) {
	var incoming coreim.ThirdPartyIncomingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
				t.Fatalf("decode incoming: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))

	req := `{"action":"send","clientId":"planner","sessionId":"task-json","eventId":"evt-001","messageId":"msg-001","message":{"type":"file","fileName":"report.txt","url":"file:///tmp/report.txt"},"metadata":{"source":"agent","priority":"high"}}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke send: %v stderr=%s", err, stderr.String())
	}
	if incoming.ClientID != "planner" || incoming.ConversationID != "task-json" || incoming.EventID != "evt-001" || incoming.MessageID != "msg-001" {
		t.Fatalf("incoming ids = %#v", incoming)
	}
	if incoming.Message.Type != "file" || incoming.Message.FileName != "report.txt" || incoming.Message.URL != "file:///tmp/report.txt" {
		t.Fatalf("incoming message = %#v", incoming.Message)
	}
	if incoming.Metadata["source"] != "agent" || incoming.Metadata["priority"] != "high" {
		t.Fatalf("incoming metadata = %#v", incoming.Metadata)
	}
}

func TestSendRejectsInvalidMessageJSONBeforeRequest(t *testing.T) {
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"send", "--session", "task-json", "--message-json", `{"type":"video","url":"https://example.test/v.mp4"}`})
	if err == nil || !strings.Contains(err.Error(), "message.type") {
		t.Fatalf("expected message.type error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestSendRejectsUnknownMessageJSONField(t *testing.T) {
	t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
	t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"send", "--session", "task-json", "--message-json", `{"type":"text","text":"hi","typo":true}`})
	if err == nil || !strings.Contains(err.Error(), `unknown field "typo"`) {
		t.Fatalf("expected unknown field error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestSendRejectsInvalidAttachmentsJSONBeforeRequest(t *testing.T) {
	cases := []struct {
		name        string
		attachments string
		want        string
	}{
		{name: "bad type", attachments: `[{"type":"text","id":"media-1"}]`, want: "attachments[0].type"},
		{name: "unknown field", attachments: `[{"type":"file","id":"media-1","typo":true}]`, want: `unknown field "typo"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACLAW_GATEWAY_TOKEN", "token")
			t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
			t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"send", "--session", "task-json", "--text", "caption", "--attachments-json", tc.attachments})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeDryRunValidatesMessagePayload(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want string
	}{
		{
			name: "bad type",
			req:  `{"action":"send","clientId":"planner","sessionId":"task-json","message":{"type":"video","url":"https://example.test/v.mp4"}}`,
			want: "message.type",
		},
		{
			name: "unknown field",
			req:  `{"action":"send","clientId":"planner","sessionId":"task-json","message":{"type":"text","text":"hi","typo":true}}`,
			want: `unknown field "typo"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeDryRunValidatesAttachmentsPayload(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want string
	}{
		{
			name: "bad type",
			req:  `{"action":"send","clientId":"planner","sessionId":"task-json","text":"caption","attachments":[{"type":"text","id":"media-1"}]}`,
			want: "attachments[0].type",
		},
		{
			name: "unknown field",
			req:  `{"action":"send","clientId":"planner","sessionId":"task-json","text":"caption","attachments":[{"type":"file","id":"media-1","typo":true}]}`,
			want: `unknown field "typo"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvokeToolResultSupportsIdempotencyKey(t *testing.T) {
	var result coreim.ThirdPartyToolResultRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/tool-result":
			if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
				t.Fatalf("decode tool-result: %v", err)
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req-tool"))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", filepath.Join(t.TempDir(), "state.json"))

	req := `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","toolCallId":"tc-1","status":"error","idempotencyKey":"tc-1-success","result":{"ok":false},"errorCode":"E_TOOL","errorMessage":"try again","errorRetryable":true,"metadata":{"source":"agent","attempt":"1"}}`
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	c.stdin = strings.NewReader(req)
	if err := c.run([]string{"invoke"}); err != nil {
		t.Fatalf("invoke tool-result: %v stderr=%s", err, stderr.String())
	}
	if result.ClientID != "desktop-agent" || result.ConversationID != "task-json" || result.ToolCallID != "tc-1" || result.IdempotencyKey != "tc-1-success" {
		t.Fatalf("tool-result = %#v", result)
	}
	if result.Result["ok"] != false {
		t.Fatalf("result payload = %#v", result.Result)
	}
	if result.Error == nil || result.Error.Code != "E_TOOL" || !result.Error.Retryable {
		t.Fatalf("tool error = %#v", result.Error)
	}
	if result.Metadata["source"] != "agent" || result.Metadata["attempt"] != "1" {
		t.Fatalf("metadata = %#v", result.Metadata)
	}
}

func TestDirectToolResultRejectsInvalidInputBeforeConfig(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing target",
			args: []string{"tool-result", "--status", "success", "--result-json", `{"ok":true}`},
			want: "tool-result requires",
		},
		{
			name: "bad status",
			args: []string{"tool-result", "--tool-call-id", "tc-1", "--status", "delivered", "--result-json", `{"ok":true}`},
			want: "--status",
		},
		{
			name: "bad result json",
			args: []string{"tool-result", "--tool-call-id", "tc-1", "--status", "success", "--result-json", `{bad`},
			want: "decode JSON object",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MACLAW_GATEWAY_TOKEN", "")
			t.Setenv("MACLAW_GATEWAY_URL", "http://127.0.0.1:1/api/im-gateway/v1")
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
			if strings.Contains(err.Error(), "token") {
				t.Fatalf("expected input validation before token lookup, got %v", err)
			}
		})
	}
}

func TestInvokeDryRunToolResultRequiresToolTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","status":"success","result":{"ok":true}}`})
	if err == nil || !strings.Contains(err.Error(), "toolCallId or toolPlanId") {
		t.Fatalf("expected tool target error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestInvokeDryRunValidatesActionSpecificStatus(t *testing.T) {
	cases := []struct {
		name string
		req  string
		want string
	}{
		{
			name: "tool result missing status",
			req:  `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","toolCallId":"tc-1","result":{"ok":true}}`,
			want: "requires status",
		},
		{
			name: "tool result delivered",
			req:  `{"action":"tool-result","clientId":"desktop-agent","sessionId":"task-json","toolCallId":"tc-1","status":"delivered","result":{"ok":true}}`,
			want: "tool-result status",
		},
		{
			name: "ack invalid",
			req:  `{"action":"ack","clientId":"desktop-agent","messageIds":["msg-1"],"status":"cancelled"}`,
			want: "ack status",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", tc.req})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v stdout=%s stderr=%s", tc.want, err, stdout.String(), stderr.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"invoke", "--dry-run", "--json", `{"action":"ack","clientId":"desktop-agent","messageIds":["msg-1"],"status":"read"}`})
	if err != nil {
		t.Fatalf("ack read should be valid: %v stderr=%s", err, stderr.String())
	}
}

func TestSameSessionDifferentClientsKeepIndependentCursors(t *testing.T) {
	var cursors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursors = append(cursors, r.URL.Query().Get("clientId")+"="+r.URL.Query().Get("cursor"))
			next := int64(11)
			if r.URL.Query().Get("clientId") == "agent-b" {
				next = 21
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{Sessions: []sessionState{
		{ID: "same", ClientID: "agent-a", Cursor: "10", CreatedAt: 1, UpdatedAt: 1},
		{ID: "same", ClientID: "agent-b", Cursor: "20", CreatedAt: 1, UpdatedAt: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var outA, errA bytes.Buffer
	if err := testCLI(&outA, &errA).run([]string{"continue", "--client", "agent-a", "--session", "same", "--timeout", "0", "a"}); err != nil {
		t.Fatalf("agent-a: %v", err)
	}
	var outB, errB bytes.Buffer
	if err := testCLI(&outB, &errB).run([]string{"continue", "--client", "agent-b", "--session", "same", "--timeout", "0", "b"}); err != nil {
		t.Fatalf("agent-b: %v", err)
	}
	if got := strings.Join(cursors, ","); got != "agent-a=10,agent-b=20" {
		t.Fatalf("cursors = %s", got)
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-a").Cursor != "11" || findSessionForClient(st, "same", "agent-b").Cursor != "21" {
		t.Fatalf("state = %#v", st)
	}
}

func TestSameClientSessionConcurrentContinueRefreshesCursorAfterRunLock(t *testing.T) {
	var mu sync.Mutex
	var cursors []string
	firstPollStarted := make(chan struct{})
	releaseFirstPoll := make(chan struct{})
	firstPollOnce := sync.Once{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursor := r.URL.Query().Get("cursor")
			mu.Lock()
			cursors = append(cursors, cursor)
			call := len(cursors)
			mu.Unlock()
			if call == 1 {
				firstPollOnce.Do(func() { close(firstPollStarted) })
				<-releaseFirstPoll
			}
			next := int64(1)
			if cursor == "1" {
				next = 2
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{Sessions: []sessionState{{ID: "same", ClientID: "agent-a", Cursor: "0", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	errs := make(chan error, 2)
	go func() {
		var out, errOut bytes.Buffer
		errs <- testCLI(&out, &errOut).run([]string{"continue", "--client", "agent-a", "--session", "same", "--timeout", "0", "first"})
	}()
	select {
	case <-firstPollStarted:
	case <-time.After(time.Second):
		t.Fatal("first poll did not start")
	}
	go func() {
		var out, errOut bytes.Buffer
		errs <- testCLI(&out, &errOut).run([]string{"continue", "--client", "agent-a", "--session", "same", "--timeout", "0", "second"})
	}()
	time.Sleep(150 * time.Millisecond)
	close(releaseFirstPoll)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("continue %d: %v", i, err)
		}
	}
	mu.Lock()
	got := strings.Join(cursors, ",")
	mu.Unlock()
	if got != "0,1" {
		t.Fatalf("cursors = %s", got)
	}
}

func TestImplicitCurrentConcurrentContinueRefreshesCursorAfterRunLock(t *testing.T) {
	var mu sync.Mutex
	var cursors []string
	firstPollStarted := make(chan struct{})
	releaseFirstPoll := make(chan struct{})
	firstPollOnce := sync.Once{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/im-gateway/v1/incoming":
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyIncomingAcceptedResponse("req-in", "mc-1", false))
		case "/api/im-gateway/v1/outgoing":
			cursor := r.URL.Query().Get("cursor")
			mu.Lock()
			cursors = append(cursors, cursor)
			call := len(cursors)
			mu.Unlock()
			if call == 1 {
				firstPollOnce.Do(func() { close(firstPollStarted) })
				<-releaseFirstPoll
			}
			next := int64(1)
			if cursor == "1" {
				next = 2
			}
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyOutgoingPollResponse("req-out", nil, next, false))
		default:
			_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayOKResponse("req"))
		}
	}))
	defer server.Close()

	cfgPath := writeConfig(t, corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: "token", ThirdPartyGatewayHost: "127.0.0.1", ThirdPartyGatewayPort: mustPort(t, server.URL)})
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "implicit", Sessions: []sessionState{{ID: "implicit", ClientID: defaultClientID, Cursor: "0", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CONFIG", cfgPath)
	t.Setenv("MACLAW_CLI_STATE", statePath)

	errs := make(chan error, 2)
	go func() {
		var out, errOut bytes.Buffer
		errs <- testCLI(&out, &errOut).run([]string{"continue", "--timeout", "0", "first"})
	}()
	select {
	case <-firstPollStarted:
	case <-time.After(time.Second):
		t.Fatal("first poll did not start")
	}
	go func() {
		var out, errOut bytes.Buffer
		errs <- testCLI(&out, &errOut).run([]string{"continue", "--timeout", "0", "second"})
	}()
	time.Sleep(150 * time.Millisecond)
	close(releaseFirstPoll)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("continue %d: %v", i, err)
		}
	}
	mu.Lock()
	got := strings.Join(cursors, ",")
	mu.Unlock()
	if got != "0,1" {
		t.Fatalf("cursors = %s", got)
	}
}

func TestSessionRenameDeleteAndResetCursor(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "old", Sessions: []sessionState{{ID: "old", Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"session", "rename", "old", "new"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := c.run([]string{"session", "reset-cursor", "--id", "new"}); err != nil {
		t.Fatalf("reset-cursor: %v", err)
	}
	st, _ := loadCLIState(statePath)
	if st.CurrentSession != "new" || findSession(st, "old") != nil || findSession(st, "new").Cursor != "0" {
		t.Fatalf("after reset = %#v", st)
	}
	if err := c.run([]string{"session", "delete", "new"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if st.CurrentSession != "" || findSession(st, "new") != nil {
		t.Fatalf("after delete = %#v", st)
	}
}

func TestSessionCurrentIsReadOnly(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"session", "current"})
	if err == nil || !strings.Contains(err.Error(), "no current session") {
		t.Fatalf("expected no current session error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	st, loadErr := loadCLIState(statePath)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if st.CurrentSession != "" || len(st.Sessions) != 0 {
		t.Fatalf("session current mutated empty state: %#v", st)
	}

	if err := saveCLIState(statePath, cliState{CurrentSession: "work", Sessions: []sessionState{{ID: "work", ClientID: defaultClientID, Cursor: "7", CreatedAt: 1, UpdatedAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if err := testCLI(&stdout, &stderr).run([]string{"session", "current"}); err != nil {
		t.Fatalf("session current: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode current: %v: %s", err, stdout.String())
	}
	if out["currentSession"] != "work" {
		t.Fatalf("current output = %#v", out)
	}
}

func TestSessionRenameSupportsIDFlag(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "old", Sessions: []sessionState{{ID: "old", ClientID: defaultClientID, Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "rename", "--id", "old", "new"}); err != nil {
		t.Fatalf("rename --id: %v stderr=%s", err, stderr.String())
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "new", defaultClientID) == nil {
		t.Fatalf("state = %#v", st)
	}
}

func TestSessionCommandsRespectClientKey(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "same", Sessions: []sessionState{
		{ID: "same", ClientID: "agent-a", Cursor: "5", CreatedAt: 1, UpdatedAt: 1},
		{ID: "same", ClientID: "agent-b", Cursor: "9", CreatedAt: 1, UpdatedAt: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	var stdout, stderr bytes.Buffer
	c := testCLI(&stdout, &stderr)
	if err := c.run([]string{"session", "rename", "--client", "agent-a", "same", "renamed"}); err != nil {
		t.Fatalf("rename: %v", err)
	}
	st, _ := loadCLIState(statePath)
	if findSessionForClient(st, "renamed", "agent-a") == nil || findSessionForClient(st, "same", "agent-b") == nil {
		t.Fatalf("after client rename = %#v", st)
	}
	if st.CurrentSession != "same" {
		t.Fatalf("current session changed while another client kept old id: %#v", st)
	}
	if err := c.run([]string{"session", "reset-cursor", "--client", "agent-b", "--id", "same"}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-b").Cursor != "0" || findSessionForClient(st, "renamed", "agent-a").Cursor != "5" {
		t.Fatalf("after client reset = %#v", st)
	}
	if err := c.run([]string{"session", "delete", "--client", "agent-b", "same"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	st, _ = loadCLIState(statePath)
	if findSessionForClient(st, "same", "agent-b") != nil || findSessionForClient(st, "renamed", "agent-a") == nil {
		t.Fatalf("after client delete = %#v", st)
	}
	if st.CurrentSession != "" {
		t.Fatalf("current session should clear after last matching id deleted: %#v", st)
	}
	err := c.run([]string{"session", "delete", "--client", "agent-b", "renamed"})
	if err == nil || !strings.Contains(err.Error(), `session "renamed" not found`) {
		t.Fatalf("expected client-scoped delete failure, got %v", err)
	}
}

func TestSessionListCanFilterByClient(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "same", Sessions: []sessionState{
		{ID: "same", ClientID: "agent-a", Cursor: "5", CreatedAt: 1, UpdatedAt: 1},
		{ID: "same", ClientID: "agent-b", Cursor: "9", CreatedAt: 1, UpdatedAt: 1},
		{ID: "legacy", Cursor: "3", CreatedAt: 1, UpdatedAt: 1},
	}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)

	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"session", "list", "--client", "agent-a"}); err != nil {
		t.Fatalf("session list --client: %v stderr=%s", err, stderr.String())
	}
	var out struct {
		FilteredClient string         `json:"filteredClient"`
		Sessions       []sessionState `json:"sessions"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode list: %v: %s", err, stdout.String())
	}
	if out.FilteredClient != "agent-a" || len(out.Sessions) != 2 {
		t.Fatalf("filtered list = %#v", out)
	}
	for _, sess := range out.Sessions {
		if sess.ClientID != "" && sess.ClientID != "agent-a" {
			t.Fatalf("unexpected filtered session = %#v", sess)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if err := testCLI(&stdout, &stderr).run([]string{"session", "list"}); err != nil {
		t.Fatalf("session list: %v stderr=%s", err, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode unfiltered list: %v: %s", err, stdout.String())
	}
	if len(out.Sessions) != 3 {
		t.Fatalf("unfiltered list = %#v", out)
	}
}

func TestSessionCommandsUseStateLock(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "locked", Sessions: []sessionState{{ID: "locked", Cursor: "5", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MACLAW_CLI_STATE", statePath)
	lock, err := acquireStateLock(statePath)
	if err != nil {
		t.Fatalf("state lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- testCLI(&stdout, &stderr).run([]string{"session", "list"})
	}()
	select {
	case err := <-done:
		t.Fatalf("session list should block on state lock, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	lock.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("session list after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("session list did not finish after releasing state lock")
	}
}

func TestSaveCLIStateUsesAtomicTempCleanup(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := saveCLIState(statePath, cliState{CurrentSession: "a", Sessions: []sessionState{{ID: "a", Cursor: "1", CreatedAt: 1, UpdatedAt: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := saveCLIState(statePath, cliState{CurrentSession: "b", Sessions: []sessionState{{ID: "b", Cursor: "2", CreatedAt: 1, UpdatedAt: 2}}}); err != nil {
		t.Fatal(err)
	}
	st, err := loadCLIState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if st.CurrentSession != "b" {
		t.Fatalf("state = %#v", st)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "state.json.tmp.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %#v", matches)
	}
}

func TestPromptArgCleaningPreservesLiteralDashes(t *testing.T) {
	got := strings.Join(cleanPromptArgs([]string{"keep", "--literal", "0", "--"}), " ")
	if got != "keep --literal 0" {
		t.Fatalf("cleanPromptArgs = %q", got)
	}
}

func TestLockTimeoutCanComeFromEnvAndInvoke(t *testing.T) {
	t.Setenv("MACLAW_CLI_LOCK_TIMEOUT_SEC", "17")
	cfgp, _ := newFlagSet("test")
	if cfgp.LockTimeoutSec != 17 {
		t.Fatalf("env lock timeout = %d", cfgp.LockTimeoutSec)
	}
	action, args, err := invokeArgs(invokeRequest{Action: "continue", ClientID: "planner", SessionID: "task-123", Text: "go", LockTimeoutSec: intPtr(23)}, "continue")
	if err != nil {
		t.Fatal(err)
	}
	if action != "continue" {
		t.Fatalf("action = %q", action)
	}
	if !containsAdjacent(args, "--lock-timeout", "23") {
		t.Fatalf("invoke args missing lock timeout: %#v", args)
	}
	action, args, err = invokeArgs(invokeRequest{Action: "watch", ClientID: "planner", SessionID: "task-123", Count: intPtr(2)}, "watch")
	if err != nil {
		t.Fatal(err)
	}
	if action != "watch" || !containsAdjacent(args, "--count", "2") {
		t.Fatalf("watch invoke args = action %q args %#v", action, args)
	}
}

func TestVersionIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"version"}); err != nil {
		t.Fatalf("version: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode version: %v\n%s", err, stdout.String())
	}
	if out["name"] != "maclaw-cli" || out["version"] != cliVersion {
		t.Fatalf("version output = %#v", out)
	}
}

func TestAgentSpecIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"agent-spec"}); err != nil {
		t.Fatalf("agent-spec: %v stderr=%s", err, stderr.String())
	}
	var spec map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &spec); err != nil {
		t.Fatalf("decode spec: %v\n%s", err, stdout.String())
	}
	if spec["name"] != "maclaw-cli" {
		t.Fatalf("name = %#v", spec["name"])
	}
	if spec["version"] != cliVersion {
		t.Fatalf("version = %#v", spec["version"])
	}
	flags, ok := spec["requiredFlagsForAutomation"].([]any)
	if !ok || len(flags) != 3 {
		t.Fatalf("requiredFlagsForAutomation = %#v", spec["requiredFlagsForAutomation"])
	}
	if !strings.Contains(fmt.Sprint(spec["goldenRule"]), "--session") {
		t.Fatalf("goldenRule = %#v", spec["goldenRule"])
	}
	if !strings.Contains(fmt.Sprint(spec["commands"]), "invoke") {
		t.Fatalf("commands missing invoke: %#v", spec["commands"])
	}
	if !strings.Contains(fmt.Sprint(spec["importantFlags"]), "--json-errors") {
		t.Fatalf("importantFlags missing json-errors: %#v", spec["importantFlags"])
	}
	env, ok := spec["environment"].(map[string]any)
	if !ok || env["MACLAW_CLIENT_NAME"] == nil || env["MACLAW_CONVERSATION_ID"] == nil || env["MACLAW_USER_ID"] == nil || env["MACLAW_USER_NAME"] == nil || env["MACLAWSRV_GATEWAY_TOKEN"] == nil {
		t.Fatalf("environment missing expected vars: %#v", spec["environment"])
	}
	if !strings.Contains(fmt.Sprint(spec["importantFlags"]), "--client-name") {
		t.Fatalf("importantFlags missing client-name: %#v", spec["importantFlags"])
	}
	commands, ok := spec["commands"].(map[string]any)
	if !ok {
		t.Fatalf("commands = %#v", spec["commands"])
	}
	handshake := fmt.Sprint(commands["handshake"])
	if strings.Contains(handshake, "--session") || strings.Contains(handshake, "--require-session") || !strings.Contains(handshake, "Client-scoped") {
		t.Fatalf("handshake spec should be client-scoped, got %#v", commands["handshake"])
	}
	remote, ok := spec["remoteAccess"].(map[string]any)
	if !ok {
		t.Fatalf("remoteAccess missing: %#v", spec["remoteAccess"])
	}
	for _, want := range []string{"maclaw-cli srv info", "gatewayToken", "No tenant id"} {
		if !strings.Contains(fmt.Sprint(remote), want) {
			t.Fatalf("remoteAccess missing %q: %#v", want, remote)
		}
	}
}

func TestHumanHelpDocumentsAgentCriticalFields(t *testing.T) {
	topHelp := usage()
	for _, want := range []string{"--client-name", "MACLAW_CLIENT_NAME", "--conversation"} {
		if !strings.Contains(topHelp, want) {
			t.Fatalf("usage missing %q:\n%s", want, topHelp)
		}
	}
	agentHelp := agentUsage()
	for _, want := range []string{"sessionId       Saved state/session key used.", "conversationId  Protocol conversation id used."} {
		if !strings.Contains(agentHelp, want) {
			t.Fatalf("agent usage missing %q:\n%s", want, agentHelp)
		}
	}
}

func TestNewSessionIDHasEntropySuffix(t *testing.T) {
	var stdout, stderr bytes.Buffer
	id := testCLI(&stdout, &stderr).newSessionID()
	if !strings.HasPrefix(id, "sess_20260609_180000_") {
		t.Fatalf("session id prefix = %q", id)
	}
	suffix := strings.TrimPrefix(id, "sess_20260609_180000_")
	if len(suffix) < 8 {
		t.Fatalf("session id suffix too short: %q", id)
	}
}

func TestInvokeSchemaIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"invoke-schema"}); err != nil {
		t.Fatalf("invoke-schema: %v stderr=%s", err, stderr.String())
	}
	var schema map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &schema); err != nil {
		t.Fatalf("decode schema: %v\n%s", err, stdout.String())
	}
	if schema["title"] != "maclaw-cli invoke request" {
		t.Fatalf("title = %#v", schema["title"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || props["action"] == nil || props["sessionId"] == nil {
		t.Fatalf("properties = %#v", schema["properties"])
	}
	data, err := os.ReadFile(filepath.Join("invoke.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fileSchema map[string]any
	if err := json.Unmarshal(data, &fileSchema); err != nil {
		t.Fatalf("decode static schema: %v", err)
	}
	if fileSchema["title"] != schema["title"] {
		t.Fatalf("static schema title = %#v", fileSchema["title"])
	}
	if !jsonEqual(t, invokeSchema(), fileSchema) {
		codeSchema, _ := json.MarshalIndent(invokeSchema(), "", "  ")
		staticSchema, _ := json.MarshalIndent(fileSchema, "", "  ")
		t.Fatalf("static schema differs from invokeSchema()\ncode:\n%s\nstatic:\n%s", codeSchema, staticSchema)
	}
	statusProp, ok := props["status"].(map[string]any)
	if !ok {
		t.Fatalf("status property = %#v", props["status"])
	}
	if _, hasEnum := statusProp["enum"]; hasEnum {
		t.Fatalf("top-level status enum conflicts with action-specific status schema: %#v", statusProp)
	}
	if !schemaRequiresTextWhenActionOmitted(schema) {
		t.Fatalf("schema must require text when action is omitted: %#v", schema["allOf"])
	}
}

func TestManifestIsMachineReadable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest["name"] != "maclaw-cli" || manifest["schema"] != "invoke.schema.json" {
		t.Fatalf("manifest = %#v", manifest)
	}
	if manifest["version"] != cliVersion {
		t.Fatalf("manifest version = %#v", manifest["version"])
	}
	entrypoints, ok := manifest["entrypoints"].(map[string]any)
	if !ok || entrypoints["agentSpec"] != "maclaw-cli agent-spec" || entrypoints["invokeSchema"] != "maclaw-cli invoke-schema" {
		t.Fatalf("entrypoints = %#v", manifest["entrypoints"])
	}
	state, ok := manifest["state"].(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(state["key"]), "conversationId") {
		t.Fatalf("state metadata = %#v", manifest["state"])
	}
	specState, ok := agentSpec()["state"].(map[string]any)
	if !ok {
		t.Fatalf("agent spec state = %#v", agentSpec()["state"])
	}
	for _, phrase := range []string{"clientId + sessionId", "conversation"} {
		if !strings.Contains(fmt.Sprint(state["key"]), phrase) || !strings.Contains(fmt.Sprint(specState["key"]), phrase) {
			t.Fatalf("state key metadata drift: manifest=%q spec=%q", state["key"], specState["key"])
		}
	}
	remote, ok := manifest["remoteAccess"].(map[string]any)
	if !ok {
		t.Fatalf("manifest remoteAccess missing: %#v", manifest["remoteAccess"])
	}
	specRemote, ok := agentSpec()["remoteAccess"].(map[string]any)
	if !ok {
		t.Fatalf("agent spec remoteAccess missing: %#v", agentSpec()["remoteAccess"])
	}
	for _, phrase := range []string{"maclaw-cli srv info", "gatewayToken", "No tenant id", "clientUse.testArgv", "clientUse.env", "query strings"} {
		if !strings.Contains(fmt.Sprint(remote), phrase) || !strings.Contains(fmt.Sprint(specRemote), phrase) {
			t.Fatalf("remoteAccess metadata drift for %q: manifest=%#v spec=%#v", phrase, remote, specRemote)
		}
	}
}

func TestSrvThirdPartySetUpdatesRemoteUserConfig(t *testing.T) {
	current := corelib.AppConfig{
		MaclawLLMUrl:             "https://llm.example/v1",
		MaclawLLMKey:             "keep-key",
		MaclawLLMModel:           "keep-model",
		ThirdPartyGatewayEnabled: false,
		ThirdPartyGatewayToken:   "old-token",
		ThirdPartyGatewayHost:    "old.example",
		ThirdPartyGatewayPort:    18777,
	}
	var updated corelib.AppConfig
	var gotPut bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer admin-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/admin/tenants/tenant-1/users/user-1/config" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: current})
		case http.MethodPut:
			gotPut = true
			var in srvUserConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			updated = in.AppConfig
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: updated})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{
		"srv", "thirdparty", "set",
		"--srv", server.URL,
		"--admin-token", "admin-token",
		"--tenant", "tenant-1",
		"--user", "user-1",
		"--endpoint", "https://maclaw.example.test/api/im-gateway/v1",
		"--token", "auto",
	})
	if err != nil {
		t.Fatalf("srv set: %v stderr=%s", err, stderr.String())
	}
	if !gotPut {
		t.Fatal("expected PUT")
	}
	if !updated.ThirdPartyGatewayEnabled || updated.ThirdPartyGatewayToken == "" || updated.ThirdPartyGatewayToken == "old-token" {
		t.Fatalf("third-party config not updated: %#v", updated)
	}
	if updated.ThirdPartyGatewayHost != "maclaw.example.test" || updated.ThirdPartyGatewayPort != 443 || updated.IsThirdPartyGatewayLocalMode() {
		t.Fatalf("endpoint/local mode not applied: %#v", updated)
	}
	if updated.MaclawLLMUrl != current.MaclawLLMUrl || updated.MaclawLLMKey != current.MaclawLLMKey || updated.MaclawLLMModel != current.MaclawLLMModel {
		t.Fatalf("unrelated LLM config changed: %#v", updated)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out["token"] == "" || out["endpoint"] != "https://maclaw.example.test/api/im-gateway/v1" {
		t.Fatalf("output = %#v", out)
	}
}

func TestSrvThirdPartySetCanUseUserAuthTokenWithoutTenantUser(t *testing.T) {
	current := corelib.AppConfig{}
	var updated corelib.AppConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer user-api-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/api/v1/config" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: current})
		case http.MethodPut:
			var in srvUserConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			updated = in.AppConfig
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: updated})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "setup", server.URL, "user-api-token"})
	if err != nil {
		t.Fatalf("srv set auth-token: %v stderr=%s", err, stderr.String())
	}
	if !updated.ThirdPartyGatewayEnabled || updated.ThirdPartyGatewayToken == "" {
		t.Fatalf("third-party config not updated: %#v", updated)
	}
	if updated.IsThirdPartyGatewayLocalMode() {
		t.Fatalf("remote setup should default local mode false: %#v", updated)
	}
	if strings.Contains(stdout.String(), `"tenantId"`) && strings.Contains(stdout.String(), `"userId"`) {
		t.Fatalf("simple output should not require tenant/user: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"token"`) {
		t.Fatalf("new auto-generated token should be printed once: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"next"`) || !strings.Contains(stdout.String(), "maclaw-cli srv test") {
		t.Fatalf("setup output should include next command: %s", stdout.String())
	}
}

func TestSrvSetupPreservesExistingGatewayTokenWithoutLeaking(t *testing.T) {
	current := corelib.AppConfig{ThirdPartyGatewayToken: "existing-secret"}
	var updated corelib.AppConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: current})
		case http.MethodPut:
			var in srvUserConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			updated = in.AppConfig
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: updated})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "setup", "--srv", server.URL, "--user-token", "user-api-token"})
	if err != nil {
		t.Fatalf("srv setup existing token: %v stderr=%s", err, stderr.String())
	}
	if updated.ThirdPartyGatewayToken != "existing-secret" || !updated.ThirdPartyGatewayEnabled {
		t.Fatalf("existing token not preserved/enabled: %#v", updated)
	}
	if strings.Contains(stdout.String(), "existing-secret") {
		t.Fatalf("existing token leaked: %s", stdout.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if _, ok := out["token"]; ok {
		t.Fatalf("top-level token should not be present for preserved token: %#v", out)
	}
	clientUse, ok := out["clientUse"].(map[string]any)
	if !ok {
		t.Fatalf("clientUse missing: %#v", out)
	}
	if strings.Contains(fmt.Sprint(clientUse), "existing-secret") || !strings.Contains(fmt.Sprint(clientUse), "<token>") {
		t.Fatalf("clientUse should use token placeholder: %#v", clientUse)
	}
}

func TestSrvSetupIgnoresGatewayTokenEnvironment(t *testing.T) {
	current := corelib.AppConfig{ThirdPartyGatewayToken: "existing-secret"}
	var updated corelib.AppConfig
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: current})
		case http.MethodPut:
			var in srvUserConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			updated = in.AppConfig
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: updated})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	t.Setenv("MACLAWSRV_GATEWAY_TOKEN", "test-only-env-token")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "setup", "--srv", server.URL, "--user-token", "user-api-token"})
	if err != nil {
		t.Fatalf("srv setup with gateway env: %v stderr=%s", err, stderr.String())
	}
	if updated.ThirdPartyGatewayToken != "existing-secret" {
		t.Fatalf("setup should not apply test-only gateway env token: %#v", updated)
	}
	if strings.Contains(stdout.String(), "test-only-env-token") {
		t.Fatalf("setup leaked/applied gateway env token: %s", stdout.String())
	}
}

func TestSrvSetupIncludeTokenRevealsExistingGatewayToken(t *testing.T) {
	current := corelib.AppConfig{ThirdPartyGatewayToken: "existing-secret"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: current})
		case http.MethodPut:
			var in srvUserConfigUpdateRequest
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode put: %v", err)
			}
			_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: in.AppConfig})
		default:
			t.Fatalf("method = %s", r.Method)
		}
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "setup", "--srv", server.URL, "--user-token", "user-api-token", "--include-token"})
	if err != nil {
		t.Fatalf("srv setup include token: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out["token"] != "existing-secret" || !strings.Contains(fmt.Sprint(out["clientUse"]), "existing-secret") {
		t.Fatalf("include-token should reveal existing token: %#v", out)
	}
}

func TestSrvSummaryQuotesHumanCommandsButKeepsStructuredFieldsRaw(t *testing.T) {
	out := srvThirdPartyConfigSummary(
		srvConfig{BaseURL: `https://srv.example/path?tenant=a&debug=1`, Endpoint: `https://srv.example/api/im-gateway/v1`},
		corelib.AppConfig{ThirdPartyGatewayEnabled: true, ThirdPartyGatewayToken: `secret token`},
		true,
		"",
	)
	next := fmt.Sprint(out["next"])
	if !strings.Contains(next, `"https://srv.example/path?tenant=a&debug=1"`) || !strings.Contains(next, `"secret token"`) {
		t.Fatalf("next should quote shell-sensitive values: %q", next)
	}
	clientUse, ok := out["clientUse"].(map[string]any)
	if !ok {
		t.Fatalf("clientUse missing: %#v", out)
	}
	argv, ok := clientUse["testArgv"].([]string)
	if !ok || !containsStringValue(argv, `https://srv.example/path?tenant=a&debug=1`) || !containsStringValue(argv, `secret token`) {
		t.Fatalf("testArgv should keep raw values: %#v", clientUse["testArgv"])
	}
	env, ok := clientUse["env"].(map[string]string)
	if !ok || env["MACLAWSRV_URL"] != `https://srv.example/path?tenant=a&debug=1` || env["MACLAWSRV_GATEWAY_TOKEN"] != `secret token` {
		t.Fatalf("env should keep raw values: %#v", clientUse["env"])
	}
}

func TestSrvTestDerivesGatewayEndpointFromSrv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer gateway-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{RequestID: "req-1", ChannelID: "thirdparty:maclaw-cli"}))
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test", server.URL, "gateway-token"})
	if err != nil {
		t.Fatalf("srv test: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out["endpoint"] != server.URL+"/api/im-gateway/v1" {
		t.Fatalf("endpoint = %#v", out["endpoint"])
	}
	if out["channelId"] != "thirdparty:maclaw-cli" {
		t.Fatalf("flattened test output missing fields: %#v", out)
	}
}

func TestSrvTestCanUseEnvironmentOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/im-gateway/v1/handshake" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer env-gateway-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(coreim.NewThirdPartyGatewayHandshakeResponse(coreim.ThirdPartyGatewayConfig{RequestID: "req-env", ChannelID: "thirdparty:maclaw-cli"}))
	}))
	defer server.Close()
	t.Setenv("MACLAWSRV_URL", server.URL)
	t.Setenv("MACLAWSRV_GATEWAY_TOKEN", "env-gateway-token")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test"})
	if err != nil {
		t.Fatalf("srv test env: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), server.URL+"/api/im-gateway/v1") || !strings.Contains(stdout.String(), "thirdparty:maclaw-cli") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}
}

func TestSrvErrorsExplainRecoverableInputs(t *testing.T) {
	t.Setenv("MACLAWSRV_AUTH_TOKEN", "")
	t.Setenv("MACLAWSRV_ADMIN_TOKEN", "")
	t.Setenv("MACLAWSRV_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_GATEWAY_TOKEN", "")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test", "https://srv.example"})
	if err == nil || !strings.Contains(err.Error(), "MACLAWSRV_GATEWAY_TOKEN") || !strings.Contains(err.Error(), "positional GATEWAY_TOKEN") {
		t.Fatalf("gateway token error should be recoverable, got %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"srv", "show", "--srv", "https://srv.example"})
	if err == nil || !strings.Contains(err.Error(), "MACLAWSRV_AUTH_TOKEN") || !strings.Contains(err.Error(), "USER_API_TOKEN") || !strings.Contains(err.Error(), "admin fallback") {
		t.Fatalf("auth token error should be recoverable, got %v", err)
	}
}

func TestSrvRejectsInvalidURLsBeforeNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "show", "--srv", "not-a-url", "--auth-token", "user-api-token"})
	if err == nil || !strings.Contains(err.Error(), "invalid --srv URL") || !strings.Contains(err.Error(), "http:// or https://") {
		t.Fatalf("expected invalid srv URL error, got %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"srv", "setup", "https://srv.example", "user-api-token", "--endpoint", "ftp://srv.example/api/im-gateway/v1"})
	if err == nil || !strings.Contains(err.Error(), "invalid --endpoint URL scheme") {
		t.Fatalf("expected invalid endpoint URL error, got %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"srv", "show", "--srv", "https://srv.example?tenant=a", "--auth-token", "user-api-token"})
	if err == nil || !strings.Contains(err.Error(), "query and fragment are not supported") {
		t.Fatalf("expected srv query URL error, got %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	err = testCLI(&stdout, &stderr).run([]string{"srv", "test", "https://srv.example", "gateway-token", "--endpoint", "https://srv.example/api/im-gateway/v1#frag"})
	if err == nil || !strings.Contains(err.Error(), "query and fragment are not supported") {
		t.Fatalf("expected endpoint fragment URL error, got %v", err)
	}
}

func TestSrvRejectsInvalidExplicitPortBeforeNetwork(t *testing.T) {
	for _, value := range []string{"0", "70000"} {
		var stdout, stderr bytes.Buffer
		err := testCLI(&stdout, &stderr).run([]string{"srv", "setup", "https://srv.example", "user-api-token", "--port", value})
		if err == nil || !strings.Contains(err.Error(), "--port must be between 1 and 65535") {
			t.Fatalf("expected port validation for %q, got %v stdout=%s stderr=%s", value, err, stdout.String(), stderr.String())
		}
	}
}

func TestSrvRejectsNegativeTimeoutBeforeNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test", "https://srv.example", "gateway-token", "--timeout", "-1"})
	if err == nil || !strings.Contains(err.Error(), "--timeout must be non-negative") {
		t.Fatalf("expected timeout validation, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestSrvTestDoesNotUseLocalGatewayToken(t *testing.T) {
	t.Setenv("MACLAWSRV_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_SRV_GATEWAY_TOKEN", "")
	t.Setenv("MACLAW_GATEWAY_TOKEN", "local-gui-token")
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test", "https://srv.example"})
	if err == nil || !strings.Contains(err.Error(), "MACLAWSRV_GATEWAY_TOKEN") {
		t.Fatalf("srv test should require remote gateway token, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(err.Error(), "MACLAW_GATEWAY_TOKEN") {
		t.Fatalf("srv test error should not suggest local GUI token: %v", err)
	}
}

func TestSrvTestRejectsInvalidClientBeforeNetwork(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "test", "https://srv.example", "gateway-token", "--client", "!!!"})
	if err == nil || !strings.Contains(err.Error(), "--client must contain") {
		t.Fatalf("expected client validation error, got %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
}

func TestDoJSONReportsNonJSONHTTPErrorByStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	var out map[string]any
	err := testCLI(&bytes.Buffer{}, &bytes.Buffer{}).doJSON(context.Background(), http.MethodGet, server.URL, "", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") || !strings.Contains(err.Error(), "upstream unavailable") {
		t.Fatalf("expected HTTP status error, got %v", err)
	}
	if strings.Contains(err.Error(), "decode response") {
		t.Fatalf("non-JSON error should not be reported as decode failure: %v", err)
	}
}

func TestDoJSONReportsGatewayErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(gatewayErrorEnvelope{Error: &gatewayAPIError{Code: "bad_token", Message: "invalid token"}})
	}))
	defer server.Close()
	var out map[string]any
	err := testCLI(&bytes.Buffer{}, &bytes.Buffer{}).doJSON(context.Background(), http.MethodGet, server.URL, "", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "HTTP 401 [bad_token] invalid token") {
		t.Fatalf("expected gateway envelope error, got %v", err)
	}
}

func TestSrvThirdPartyShowDoesNotLeakTokenByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: corelib.AppConfig{
			ThirdPartyGatewayEnabled: true,
			ThirdPartyGatewayToken:   "secret-token",
		}})
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "thirdparty", "show", "--srv", server.URL, "--auth-token", "user-api-token"})
	if err != nil {
		t.Fatalf("srv show: %v stderr=%s", err, stderr.String())
	}
	if strings.Contains(stdout.String(), "secret-token") {
		t.Fatalf("show leaked token: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), `"tokenPresent": true`) {
		t.Fatalf("show missing tokenPresent: %s", stdout.String())
	}
}

func TestSrvInfoPrintsIntegrationCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/config" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer user-api-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(srvUserConfigResponse{AppConfig: corelib.AppConfig{
			ThirdPartyGatewayEnabled: true,
			ThirdPartyGatewayToken:   "secret-token",
		}})
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	err := testCLI(&stdout, &stderr).run([]string{"srv", "info", server.URL, "user-api-token"})
	if err != nil {
		t.Fatalf("srv info: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out["token"] != "secret-token" {
		t.Fatalf("info should print gateway token: %#v", out)
	}
	if !strings.Contains(fmt.Sprint(out["clientUse"]), "secret-token") {
		t.Fatalf("clientUse should include token for handoff: %#v", out["clientUse"])
	}
	clientUse, ok := out["clientUse"].(map[string]any)
	if !ok {
		t.Fatalf("clientUse missing: %#v", out)
	}
	if !containsAnyValue(clientUse["testArgv"].([]any), "secret-token") {
		t.Fatalf("testArgv should include token for direct exec: %#v", clientUse["testArgv"])
	}
	env, ok := clientUse["env"].(map[string]any)
	if !ok || env["MACLAWSRV_GATEWAY_TOKEN"] != "secret-token" || env["MACLAWSRV_URL"] != server.URL {
		t.Fatalf("client env should include remote gateway settings: %#v", clientUse["env"])
	}
	if !strings.Contains(fmt.Sprint(out["next"]), "maclaw-cli srv test "+server.URL+" secret-token") {
		t.Fatalf("next should include ready-to-run test command: %#v", out["next"])
	}
}

func TestSrvToolsValidateStrictToolDefinitions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tools.json")
	body := `[{"name":"plc.read_register","description":"Read one Modbus register.","risk":"read","inputSchema":{"type":"object","properties":{"address":{"type":"integer"}},"required":["address"]},"timeoutMs":5000}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := testCLI(&stdout, &stderr).run([]string{"srv", "tools", "validate", "--file", path}); err != nil {
		t.Fatalf("tools validate: %v stderr=%s", err, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode output: %v: %s", err, stdout.String())
	}
	if out["ok"] != true || int(out["count"].(float64)) != 1 || !strings.Contains(fmt.Sprint(out["tools"]), "plc.read_register") {
		t.Fatalf("output = %#v", out)
	}
}

func TestSessionRunLockPathIsPerClientSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	a := sessionRunLockPath(statePath, "agent-a", "task-1")
	b := sessionRunLockPath(statePath, "agent-b", "task-1")
	c := sessionRunLockPath(statePath, "agent-a", "task-2")
	if a == b || a == c || b == c {
		t.Fatalf("lock paths should differ: %q %q %q", a, b, c)
	}
	if filepath.Dir(filepath.Dir(a)) != filepath.Dir(statePath) {
		t.Fatalf("lock path should live under state dir: %q", a)
	}
}

func TestAcquireRunLockBlocksSameClientSession(t *testing.T) {
	cfg := config{StatePath: filepath.Join(t.TempDir(), "state.json"), ClientID: "agent-a", SessionID: "task-1", ConversationID: "external-1"}
	first, err := acquireRunLock(cfg)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		second, err := acquireRunLock(cfg)
		if second != nil {
			second.Release()
		}
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("second lock should block, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	first.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second lock after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second lock did not acquire after release")
	}
}

func TestAcquireLockFileRemovesStaleLock(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "state.json.lock")
	if err := os.WriteFile(lockPath, []byte("dead\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	lock, err := acquireLockFile(lockPath, "state lock", 1)
	if err != nil {
		t.Fatalf("acquire stale lock: %v", err)
	}
	lock.Release()
}

func TestStaleLockReleaseDoesNotRemoveNewOwner(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "state.json.lock")
	old := &stateLock{path: lockPath, token: "old-owner"}
	if err := os.WriteFile(lockPath, []byte("123 old-owner\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	newLock, err := acquireLockFile(lockPath, "state lock", 1)
	if err != nil {
		t.Fatalf("acquire replacement lock: %v", err)
	}
	old.Release()
	if _, err := os.Stat(lockPath); err != nil {
		newLock.Release()
		t.Fatalf("old release removed replacement lock: %v", err)
	}
	if !lockFileHasToken(lockPath, newLock.token) {
		newLock.Release()
		t.Fatalf("replacement lock token missing")
	}
	newLock.Release()
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("new owner release should remove lock, stat err=%v", err)
	}
}

func TestStateLockHeartbeatRefreshesMTime(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "state.json.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("create lock: %v", err)
	}
	lock := &stateLock{path: lockPath, file: f, token: "heartbeat-owner", heartbeat: make(chan struct{})}
	if _, err := fmt.Fprintln(f, "123 heartbeat-owner"); err != nil {
		t.Fatal(err)
	}
	lock.startHeartbeat(3 * time.Second)
	defer lock.Release()
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().After(old) {
		t.Fatalf("heartbeat did not refresh mtime: %v <= %v", info.ModTime(), old)
	}
}

func TestAcquireRunLockAllowsDifferentClientSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	first, err := acquireRunLock(config{StatePath: statePath, ClientID: "agent-a", SessionID: "task-1", ConversationID: "external-1"})
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer first.Release()
	second, err := acquireRunLock(config{StatePath: statePath, ClientID: "agent-b", SessionID: "task-1", ConversationID: "external-1"})
	if err != nil {
		t.Fatalf("second lock: %v", err)
	}
	second.Release()
}

func TestWriteCLIErrorCanEmitJSON(t *testing.T) {
	var stderr bytes.Buffer
	writeCLIError(&stderr, []string{"continue", "--json-errors"}, errors.New("boom"))
	var env map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("decode stderr JSON: %v: %s", err, stderr.String())
	}
	if env["ok"] != false || !strings.Contains(fmt.Sprint(env["error"]), "boom") {
		t.Fatalf("error envelope = %#v", env)
	}
}

func TestInvokeErrorsDefaultToJSON(t *testing.T) {
	var stderr bytes.Buffer
	writeCLIError(&stderr, []string{"invoke"}, errors.New("bad json"))
	var env map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &env); err != nil {
		t.Fatalf("decode invoke stderr JSON: %v: %s", err, stderr.String())
	}
	if env["ok"] != false || !strings.Contains(fmt.Sprint(env["error"]), "bad json") {
		t.Fatalf("invoke error envelope = %#v", env)
	}
}

func jsonEqual(t *testing.T, a, b any) bool {
	t.Helper()
	adata, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bdata, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	var av, bv any
	if err := json.Unmarshal(adata, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(bdata, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}

func containsAdjacent(values []string, key, value string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == key && values[i+1] == value {
			return true
		}
	}
	return false
}

func containsAnyAdjacent(values []any, key, value string) bool {
	for i := 0; i+1 < len(values); i++ {
		if values[i] == key && values[i+1] == value {
			return true
		}
	}
	return false
}

func containsAnyValue(values []any, value string) bool {
	for _, got := range values {
		if got == value {
			return true
		}
	}
	return false
}

func containsStringValue(values []string, value string) bool {
	for _, got := range values {
		if got == value {
			return true
		}
	}
	return false
}

func schemaRequiresTextWhenActionOmitted(schema map[string]any) bool {
	allOf, ok := schema["allOf"].([]any)
	if !ok {
		return false
	}
	for _, raw := range allOf {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ifPart, _ := item["if"].(map[string]any)
		notPart, _ := ifPart["not"].(map[string]any)
		notRequired, _ := notPart["required"].([]any)
		if !containsAnyValue(notRequired, "action") {
			continue
		}
		thenPart, _ := item["then"].(map[string]any)
		thenRequired, _ := thenPart["required"].([]any)
		if containsAnyValue(thenRequired, "text") {
			return true
		}
	}
	return false
}

func intPtr(v int) *int {
	return &v
}

func testCLI(stdout, stderr *bytes.Buffer) *cli {
	return &cli{
		stdin:  strings.NewReader(""),
		stdout: stdout,
		stderr: stderr,
		client: &http.Client{},
		now: func() time.Time {
			return time.UnixMilli(1781028000000)
		},
	}
}

func writeConfig(t *testing.T, cfg corelib.AppConfig) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	var port int
	if _, err := fmt.Sscanf(rawURL, "http://127.0.0.1:%d", &port); err != nil {
		t.Fatalf("parse port from %s: %v", rawURL, err)
	}
	return port
}
