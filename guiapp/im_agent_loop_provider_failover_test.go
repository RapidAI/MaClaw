package guiapp

import (
	"context"
	"fmt"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

func TestSharedLoopFailoverBackgroundUsesForegroundOnNetworkFailure(t *testing.T) {
	inherit := false
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "auto",
			Background: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{
				InheritHub: &inherit, ProviderID: "local", Model: "codegen-model",
			},
		},
	})
	bg, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if bg.ProviderName != hubServiceProviderName || bg.Model != "auto" {
		t.Fatalf("precondition background role = %+v, want hub auto", bg)
	}
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	next, ok := cb.FailoverLLMProvider(context.Background(), bg, fmt.Errorf("dial tcp 127.0.0.1:1: connectex: connection refused"))
	if !ok {
		t.Fatal("network failure on the background endpoint should fall back")
	}
	if next.ProviderName != "Local" || next.Model != "codegen-model" {
		t.Fatalf("fallback = %+v, want the configured foreground model", next)
	}
	if next.Profile != maclawLLMRoleBackground || next.RouteSource != "role-failover" {
		t.Fatalf("fallback attribution = %+v, want the background role kept", next)
	}
	// The fallback is per-turn: the saved allocation still points at Hub auto.
	after, err := app.ResolveMaclawLLMRole(maclawLLMRoleBackground)
	if err != nil {
		t.Fatal(err)
	}
	if after.ProviderName != hubServiceProviderName || after.Model != "auto" {
		t.Fatalf("saved background allocation changed to %+v", after)
	}

	// A gateway that just failed must not be chosen as the fallback either.
	foreground, err := app.ResolveMaclawLLMRole(maclawLLMRoleForeground)
	if err != nil {
		t.Fatal(err)
	}
	app.observeLLMEndpointResult(foreground, llmEndpointCategoryMainStream, false, fmt.Errorf("dial tcp: connection refused"))
	if _, ok := cb.FailoverLLMProvider(context.Background(), bg, fmt.Errorf("dial tcp: connection refused")); ok {
		t.Fatal("must not replace one recently failed endpoint with another")
	}
}

func TestResolveAgentLoopConfigSkipsRecentlyFailedBackgroundEndpoint(t *testing.T) {
	inherit := false
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground: "auto",
			Background: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{
				InheritHub: &inherit, ProviderID: "local", Model: "codegen-model",
			},
		},
	})
	h := &IMMessageHandler{app: app}
	// No owner path: this test only needs the background role, and a task-pin
	// lookup would open archive.db that TempDir cannot remove on Windows.
	ctx := &LoopContext{ChatTaskID: "task-1", Kind: LoopKindChat}
	initial := h.resolveAgentLoopLLMConfig(ctx)
	if initial.ProviderName != hubServiceProviderName || initial.Model != "auto" || initial.Profile != maclawLLMRoleBackground {
		t.Fatalf("initial config = %+v, want background hub auto", initial)
	}
	app.observeLLMEndpointResult(initial, llmEndpointCategoryMainStream, false, fmt.Errorf("dial tcp: connection refused"))
	got := h.resolveAgentLoopLLMConfig(ctx)
	if got.ProviderName != "Local" || got.Model != "codegen-model" || got.Profile != maclawLLMRoleBackground {
		t.Fatalf("config after a recent endpoint failure = %+v, want this turn on the foreground model", got)
	}
}

// The desktop helper runs the computer-use role. It is a worker role like
// background, so a dead endpoint must fall back to the live foreground model
// instead of leaving the task on "正在重新请求模型".
func TestSharedLoopFailoverComputerUseWorkerUsesForeground(t *testing.T) {
	inherit := false
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		HubRoleModels: corelib.MaclawLLMHubRoleModels{
			Foreground:  "auto",
			ComputerUse: "auto",
		},
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
			Foreground: corelib.MaclawLLMRoleAssignment{
				InheritHub: &inherit, ProviderID: "local", Model: "vision-model",
			},
		},
	})
	cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
	if err != nil {
		t.Fatal(err)
	}
	if cu.ProviderName != hubServiceProviderName || cu.Model != "auto" {
		t.Fatalf("precondition computer_use = %+v, want hub auto", cu)
	}
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	next, ok := cb.FailoverLLMProvider(context.Background(), cu, fmt.Errorf("dial tcp 127.0.0.1:1: connectex: connection refused"))
	if !ok {
		t.Fatal("a network failure on the computer-use endpoint should fall back to a vision model")
	}
	if next.ProviderName != "Local" || next.Model != "vision-model" || !next.SupportsVision {
		t.Fatalf("fallback = %+v, want the configured vision foreground model", next)
	}
	if next.Profile != maclawLLMRoleComputerUse || next.RouteSource != "role-failover" {
		t.Fatalf("fallback attribution = %+v, want the computer-use role kept", next)
	}
}

func TestComputerUseFailoverRequiresVision(t *testing.T) {
	makeApp := func(foregroundModel string) (*App, corelib.MaclawLLMConfig) {
		inherit := false
		app := newRoleTestApp(t, corelib.AppConfig{
			MaclawLLMProviders:       roleTestProviders(),
			MaclawLLMCurrentProvider: "Local",
			HubRoleModels: corelib.MaclawLLMHubRoleModels{
				Foreground: "auto", Background: "auto", ComputerUse: "auto",
			},
			MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
				Version:   maclawLLMProfilesVersion,
				Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
				Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
				Foreground: corelib.MaclawLLMRoleAssignment{
					InheritHub: &inherit, ProviderID: "local", Model: foregroundModel,
				},
			},
		})
		cu, err := app.ResolveMaclawLLMRole(maclawLLMRoleComputerUse)
		if err != nil {
			t.Fatal(err)
		}
		if cu.Profile != maclawLLMRoleComputerUse || !cu.SupportsVision {
			t.Fatalf("computer-use role = %+v, want a vision model", cu)
		}
		return app, cu
	}

	app, cu := makeApp("codegen-model")
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	if _, ok := cb.FailoverLLMProvider(context.Background(), cu, fmt.Errorf("dial tcp: connection refused")); ok {
		t.Fatal("computer use must not fail over to a text-only foreground model")
	}

	app, cu = makeApp("vision-model")
	cb = &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	next, ok := cb.FailoverLLMProvider(context.Background(), cu, fmt.Errorf("dial tcp: connection refused"))
	if !ok || next.Model != "vision-model" || next.Profile != maclawLLMRoleComputerUse || !next.SupportsVision {
		t.Fatalf("vision-capable computer-use fallback = %+v ok=%v", next, ok)
	}
}

func TestSharedLoopFailoverLeavesDeterministicErrorsAndForegroundAlone(t *testing.T) {
	app := newRoleTestApp(t, corelib.AppConfig{
		MaclawLLMProviders:       roleTestProviders(),
		MaclawLLMCurrentProvider: "Local",
		MaclawLLMProfiles: &corelib.MaclawLLMProfiles{
			Version:   maclawLLMProfilesVersion,
			Assistant: corelib.MaclawLLMProfile{ProviderID: "hub", Model: "auto"},
			Coding:    corelib.MaclawLLMProfile{InheritAssistant: true},
		},
	})
	cb := &sharedAgentLoopCallbacks{handler: &IMMessageHandler{app: app}}
	background := corelib.MaclawLLMConfig{
		URL: "https://hub.example/v1", ProviderName: hubServiceProviderName,
		Model: "auto", Profile: maclawLLMRoleBackground,
	}
	if _, ok := cb.FailoverLLMProvider(context.Background(), background, &llm.HTTPStatusError{StatusCode: 400}); ok {
		t.Fatal("a deterministic 400 must not switch models")
	}
	foreground := corelib.MaclawLLMConfig{
		URL: "https://hub.example/v1", ProviderName: hubServiceProviderName,
		Model: "auto", Profile: maclawLLMRoleForeground,
	}
	if _, ok := cb.FailoverLLMProvider(context.Background(), foreground, fmt.Errorf("connection refused")); ok {
		t.Fatal("the foreground role keeps its own assignment")
	}
	pinned := background
	pinned.RouteSource = mcpCallerModelRoute
	if _, ok := cb.FailoverLLMProvider(context.Background(), pinned, fmt.Errorf("connection refused")); ok {
		t.Fatal("an MCP caller's model pairing must not be replaced")
	}
}
