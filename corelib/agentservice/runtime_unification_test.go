package agentservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestCoreAgentCallbacksUseSharedDefaultRole(t *testing.T) {
	cb := &coreAgentCallbacks{appCfg: corelib.AppConfig{}}
	prompt := cb.BuildSystemPrompt("hello", true)
	if !strings.Contains(prompt, agentruntime.DefaultRoleDescription) {
		t.Fatalf("srv prompt missing shared role %q: %s", agentruntime.DefaultRoleDescription, prompt)
	}
	if strings.Contains(prompt, "REST-served MaClaw agent runtime") {
		t.Fatal("srv still uses a transport-specific default role")
	}
	if agentruntime.DefaultRoleDescription != corelib.DefaultMaclawRoleDescription {
		t.Fatalf("role constants drifted: runtime=%q corelib=%q", agentruntime.DefaultRoleDescription, corelib.DefaultMaclawRoleDescription)
	}
}

func TestHeadlessScreenshotStaysOnSurfaceAndReturnsUnavailable(t *testing.T) {
	cb := &coreAgentCallbacks{host: agentruntime.HeadlessHostCapabilities{}}
	cb.bindRequestHostCapabilities()
	tools := cb.BuildTools("take a screenshot")
	found := false
	for _, tool := range tools {
		if tooldefName(tool) == "screenshot" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("headless screenshot must remain on the model surface")
	}
	result := cb.ExecuteToolStructured("screenshot", `{}`)
	if result.Outcome != agent.ToolExecutionOutcomeError || !agentruntime.IsCapabilityUnavailable(nil, result.Result) {
		t.Fatalf("headless screenshot = %#v", result)
	}
	if strings.Contains(result.Result, "unknown tool") {
		t.Fatalf("headless screenshot reported unknown tool: %s", result.Result)
	}
	cb.adaptiveRetry = agentruntime.NewAdaptiveRetry(nil)
	cb.adaptiveRetry.SetMaxFailuresForTesting(1)
	_ = cb.ExecuteToolStructured("screenshot", `{}`)
	_ = cb.ExecuteToolStructured("screenshot", `{}`)
	if msg, blocked := cb.adaptiveRetry.GuardDisabledTool("screenshot"); blocked {
		t.Fatalf("headless unavailable screenshot disabled the tool: %s", msg)
	}
}

func TestRequestHostClearsExecutorPortsWhenMissing(t *testing.T) {
	cb := &coreAgentCallbacks{
		desktopCapturer: stubDesktopCapturer{},
		host:            noPortHost{},
	}
	cb.bindRequestHostCapabilities()
	if cb.desktopCapturer != nil || cb.documentLauncher != nil || cb.urlLauncher != nil || cb.speechPlayer != nil {
		t.Fatal("request host without ports must clear executor-level host adapters")
	}
}

type stubDesktopCapturer struct{}

func (stubDesktopCapturer) CapturePrimary(context.Context) ([]byte, error) { return []byte("png"), nil }

type noPortHost struct{}

func (noPortHost) Profile() agentruntime.CapabilityProfile {
	return agentruntime.CapabilityProfile{Name: "custom", Headless: false}
}
func (noPortHost) DesktopCapture() (agentruntime.DesktopCapturePort, bool) {
	return nil, false
}
func (noPortHost) DocumentLauncher() (agentruntime.DocumentLauncherPort, bool) {
	return nil, false
}
func (noPortHost) URLLauncher() (agentruntime.URLLauncherPort, bool) { return nil, false }
func (noPortHost) SpeechRenderer() (agentruntime.SpeechRendererPort, bool) {
	return nil, false
}

func TestCoreAgentAdaptiveRetryDisablesTool(t *testing.T) {
	cb := &coreAgentCallbacks{adaptiveRetry: agentruntime.NewAdaptiveRetry(nil)}
	cb.adaptiveRetry.SetMaxFailuresForTesting(1)
	cb.adaptiveRetry.ObserveToolFailure("bash", "connection reset by peer", 0)
	cb.adaptiveRetry.ObserveToolFailure("bash", "connection reset by peer", 1)
	result := cb.ExecuteToolStructured("bash", `{"command":"echo hi"}`)
	if result.Outcome != agent.ToolExecutionOutcomeError || !strings.Contains(result.Result, "disabled") {
		t.Fatalf("expected AdaptiveRetry disable, got %#v", result)
	}
}

func tooldefName(tool map[string]interface{}) string {
	fn, _ := tool["function"].(map[string]interface{})
	name, _ := fn["name"].(string)
	if name != "" {
		return name
	}
	name, _ = tool["name"].(string)
	return name
}
