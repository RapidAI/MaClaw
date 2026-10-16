package agentservice

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// TestRuntimeSurfaceDigestParityAcrossHosts makes the parity guarantee
// executable: independently assembled GUI/headless adapters using the same
// composition-root registry must publish byte-equivalent prompt/tool/module
// surfaces. A digest mismatch is a release-level contract failure, not a
// transport detail that callers should paper over.
func TestRuntimeSurfaceDigestParityAcrossHosts(t *testing.T) {
	registryGUI, err := agentruntime.RegisterBuiltinModules(contributingRuntimeModule{})
	if err != nil {
		t.Fatalf("GUI registry: %v", err)
	}
	registrySrv, err := agentruntime.RegisterBuiltinModules(contributingRuntimeModule{})
	if err != nil {
		t.Fatalf("srv registry: %v", err)
	}
	host := agentruntime.HeadlessHostCapabilities{Capabilities: map[string]bool{"http": true}}
	guiRuntime := NewRuntimeExecutorWithModules(EchoExecutor{}, registryGUI)
	srvRuntime := NewRuntimeExecutorWithModules(EchoExecutor{}, registrySrv)
	guiSnapshot, err := guiRuntime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("GUI snapshot: %v", err)
	}
	srvSnapshot, err := srvRuntime.DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("srv snapshot: %v", err)
	}
	if guiSnapshot.PromptDigest != srvSnapshot.PromptDigest {
		t.Fatalf("prompt parity mismatch: GUI=%s srv=%s", guiSnapshot.PromptDigest, srvSnapshot.PromptDigest)
	}
	if guiSnapshot.SurfaceDigest != srvSnapshot.SurfaceDigest {
		t.Fatalf("surface parity mismatch: GUI=%s srv=%s", guiSnapshot.SurfaceDigest, srvSnapshot.SurfaceDigest)
	}
	if guiSnapshot.SurfaceDigest == "" {
		t.Fatal("surface digest must be populated")
	}
}

func TestRuntimeSurfaceDigestDetectsSharedContractDrift(t *testing.T) {
	first, err := agentruntime.RegisterBuiltinModules(contributingRuntimeModule{})
	if err != nil {
		t.Fatalf("first registry: %v", err)
	}
	second, err := agentruntime.RegisterBuiltinModules(driftedContributingRuntimeModule{})
	if err != nil {
		t.Fatalf("second registry: %v", err)
	}
	host := agentruntime.HeadlessHostCapabilities{}
	left, err := NewRuntimeExecutorWithModules(EchoExecutor{}, first).DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	right, err := NewRuntimeExecutorWithModules(EchoExecutor{}, second).DescribeCapabilities(context.Background(), agentruntime.CapabilityRequest{Host: host})
	if err != nil {
		t.Fatalf("second snapshot: %v", err)
	}
	if left.SurfaceDigest == right.SurfaceDigest {
		t.Fatalf("contract drift was not detected: digest=%s", left.SurfaceDigest)
	}
}

type driftedContributingRuntimeModule struct{}

func (driftedContributingRuntimeModule) Descriptor() agentruntime.ModuleDescriptor {
	return agentruntime.ModuleDescriptor{ModuleID: "contrib.test", Version: "2.0.0", HeadlessSupport: true}
}

func (driftedContributingRuntimeModule) ContributePrompt(context.Context, agentruntime.TurnRequest) (string, error) {
	return "shared prompt section changed", nil
}

func (driftedContributingRuntimeModule) Tools(context.Context, agentruntime.TurnRequest) ([]agentruntime.ToolDefinition, error) {
	return []agentruntime.ToolDefinition{{Name: "shared_tool", Description: "shared tool changed", Parameters: map[string]any{"type": "object"}}}, nil
}

func (driftedContributingRuntimeModule) InvokeTool(context.Context, agentruntime.TurnRequest, string, map[string]any) (string, error) {
	return "shared result changed", nil
}
