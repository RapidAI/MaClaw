package main

import (
	"context"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"strings"
)

// A computerless bot has no desktop tool, so no finish action and no goal
// ledger: its task outcome must be measured from the reply itself. Without
// the stamp the service never promotes the reply into send_to_user, and the
// feed shows only the intake ack (hub bot_69ee2c14, run_03efc35d 2026-11-16).
func TestDecorateAssistantMessageMetadataNoDesktopStampsOutcome(t *testing.T) {
	server := &HTTPServer{}
	inst := agentservice.Instance{ID: "inst-1", Metadata: map[string]string{"hub_bot_desktop": desktop.BotDesktopModeNone}}
	run := agentservice.Run{ID: "run-nd-1", InstanceID: inst.ID, Metadata: map[string]string{"bot_desktop": desktop.BotDesktopModeNone}}

	out := server.decorateAssistantMessageMetadata(context.Background(), agentservice.Principal{}, inst, agentservice.Session{}, run,
		agentservice.Message{Content: "北京今天晴，气温 12℃。", Metadata: map[string]string{}}, corelib.AppConfig{})
	if out[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeDone {
		t.Fatalf("answered no-desktop run got %v", out)
	}

	out = server.decorateAssistantMessageMetadata(context.Background(), agentservice.Principal{}, inst, agentservice.Session{}, run,
		agentservice.Message{Content: "要查哪个城市？", Metadata: map[string]string{"response_source": "ask_user"}}, corelib.AppConfig{})
	if out[agentservice.TaskOutcomeKey] != agentservice.TaskOutcomeAwaitingUser {
		t.Fatalf("asking no-desktop run got %v", out)
	}

	out = server.decorateAssistantMessageMetadata(context.Background(), agentservice.Principal{}, inst, agentservice.Session{}, run,
		agentservice.Message{Metadata: map[string]string{}}, corelib.AppConfig{})
	if out != nil {
		t.Fatalf("quiet no-desktop run got %v", out)
	}
}

// The same run on a desktop bot keeps the ledger-measured path.
func TestDecorateAssistantMessageMetadataDesktopKeepsLedger(t *testing.T) {
	server := &HTTPServer{}
	inst := agentservice.Instance{ID: "inst-d", Metadata: map[string]string{}}
	run := agentservice.Run{ID: "run-d-1", InstanceID: inst.ID}
	out := server.decorateAssistantMessageMetadata(context.Background(), agentservice.Principal{}, inst, agentservice.Session{}, run,
		agentservice.Message{Content: "回答", Metadata: map[string]string{}}, corelib.AppConfig{})
	if out != nil && out[agentservice.TaskOutcomeKey] == agentservice.TaskOutcomeDone {
		t.Fatalf("desktop run with no ledger must not be stamped done: %v", out)
	}
}

// The pinned worth-saying rule must name a surface the bot actually has.
// A computerless bot's surface has no send_to_user tool and no PROGRESS.md.
func TestPinWorthSayingNoDesktopSurface(t *testing.T) {
	got := desktopbot.PinWorthSayingFor("", true)
	if got != desktopbot.WorthSayingPromptNoDesktop || strings.Contains(got, "send_to_user") || strings.Contains(got, "PROGRESS.md") {
		t.Fatalf("no-desktop worth-saying = %q", got)
	}
	if got := desktopbot.PinWorthSayingFor("", false); got != desktopbot.WorthSayingPrompt {
		t.Fatalf("desktop worth-saying = %q", got)
	}
}
