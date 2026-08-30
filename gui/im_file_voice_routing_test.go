package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestIMManagementToolNamesIncludesFileDeliveryToolsForVoiceStyleRequest(t *testing.T) {
	want := map[string]bool{"im_message": true, "send_to_im": true, "send_file": true}
	for _, name := range imManagementToolNames("把报告发到蓝信研发群") {
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing tools: %#v", want)
	}
}

func TestPrepareAgentLoopToolsKeepsIMFileToolsInLightProfile(t *testing.T) {
	app := &App{}
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	// Pin a neutral (non-centralized) Hub security policy: the developer
	// machine's real config (sandbox_mode=os, file_outbound off) legitimately
	// strips bash/send_file/send_to_im from the rendered surface, which is
	// not what this test is about.
	h.surfaceSecurityConfig = func() corelib.AppConfig { return corelib.AppConfig{} }
	ctx := &LoopContext{}
	ctx.Runtime.Execution = ExecutionProfile{
		Layer:                string(executionLayerLight),
		PromptProfile:        "light",
		ToolBudget:           4,
		RequiredCapabilities: []string{"current_data"},
	}
	set := h.prepareAgentLoopTools("thirdparty:pet:default", "把报告发到蓝信研发群", ctx, agentLoopPhase{})
	names := map[string]bool{}
	for _, def := range set.Tools {
		names[extractToolName(def)] = true
	}
	for _, required := range []string{"im_message", "send_to_im", "send_file"} {
		if !names[required] {
			t.Fatalf("tool %s missing from light-profile set: %#v", required, names)
		}
	}
}

func TestPrepareAgentLoopToolsDropsPolicyRejectedTools(t *testing.T) {
	app := &App{}
	h := NewIMMessageHandler(app, &RemoteSessionManager{app: app, sessions: map[string]*RemoteSession{}})
	h.surfaceSecurityConfig = func() corelib.AppConfig {
		return corelib.AppConfig{HubSecurityCentralized: true, SandboxMode: "os"}
	}
	ctx := &LoopContext{}
	set := h.prepareAgentLoopTools("thirdparty:pet:default", "把报告发到蓝信研发群", ctx, agentLoopPhase{})
	for _, def := range set.Tools {
		if name := extractToolName(def); name == "bash" || name == "send_file" || name == "send_to_im" {
			t.Fatalf("policy-rejected tool %q rendered in surface", name)
		}
	}
}
