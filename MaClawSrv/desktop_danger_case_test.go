package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotDangerCases(t *testing.T) {
	for _, id := range []string{"bash_ls_allowed", "bash_rm_blocked", "git_push_asks", "silence_blocks"} {
		t.Run(id, func(t *testing.T) {
			desktopbot.ResetDangerState()
			t.Setenv("MACLAW_DESKTOP_INSTALL_ASK", "on")
			t.Cleanup(desktopbot.ResetDangerState)
			runDangerToolCase(t, readDangerToolCase(t, id))
		})
	}
}

type dangerToolCommand struct {
	Action  string `json:"action"`
	Command string `json:"command"`
}

type dangerToolCase struct {
	ID        string              `json:"id"`
	Verdict   string              `json:"verdict"`
	Secret    string              `json:"secret"`
	Commands  []dangerToolCommand `json:"commands"`
	Helper    bool                `json:"helper"`
	ExpectRan bool                `json:"expect_ran"`
	Ask       bool                `json:"ask"`
	Silence   bool                `json:"silence"`
}

func readDangerToolCase(t *testing.T, id string) dangerToolCase {
	t.Helper()
	path := filepath.Join("..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c dangerToolCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runDangerToolCase(t *testing.T, c dangerToolCase) {
	t.Helper()
	t.Setenv("MACLAW_DESKTOP_TIDY", "off")
	reviews := 0
	previousReview := desktopDangerReview
	desktopDangerReview = func(context.Context, desktopbot.DangerCall) (string, bool) {
		reviews++
		return c.Verdict, true
	}
	ran := 0
	previousBash := desktopRemoteBash
	previousInstall := desktopRemoteInstall
	previousSession := desktopRemoteSession
	desktopRemoteBash = func(context.Context, string, string, string) (string, error) {
		ran++
		return "listed", nil
	}
	desktopRemoteInstall = func(context.Context, string, string, []string) (string, error) {
		ran++
		return "installed", nil
	}
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:9", Display: ":99"}, nil
	}
	t.Cleanup(func() {
		desktopDangerReview = previousReview
		desktopRemoteBash = previousBash
		desktopRemoteInstall = previousInstall
		desktopRemoteSession = previousSession
	})
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	now := start
	desktopbot.SetDangerNow(func() time.Time { return now })
	scope := agentruntime.Scope{TenantID: "tenant-danger", UserID: "alice-" + c.ID, InstanceID: "danger-" + c.ID, SessionID: "sess-" + c.ID}
	meta := map[string]string{"bot_phase": "execute"}
	if c.Helper {
		meta["computer_helper"] = "1"
	}
	turn := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: meta}},
	}
	for _, command := range c.Commands {
		args := map[string]any{"action": command.Action, "command": command.Command}
		if command.Action == "install" {
			args["packages"] = []any{"vim"}
		}
		text, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", args)
		if c.Secret != "" && (strings.Contains(text, c.Secret) || (err != nil && strings.Contains(err.Error(), c.Secret))) {
			t.Fatalf("secret reached the reply: text=%q err=%v", text, err)
		}
		if c.ExpectRan {
			if err != nil || ran == 0 {
				t.Fatalf("%s text=%q err=%v ran=%d", command.Command, text, err, ran)
			}
			continue
		}
		if err == nil || err.Error() != desktopbot.DangerAskedError || strings.Contains(text, command.Command) && c.Secret != "" {
			t.Fatalf("%s text=%q err=%v", command.Command, text, err)
		}
	}
	if reviews == 0 {
		t.Fatal("the review was not called")
	}
	if c.ExpectRan {
		if ran == 0 {
			t.Fatal("an allowed command did not run")
		}
		return
	}
	if ran != 0 {
		t.Fatalf("a refused command ran %d times", ran)
	}
	ask, ok := desktopbot.PeekDangerAsk(scope.SessionID, scope.InstanceID)
	if !c.Ask || !ok || ask.Kind != "ask" || !strings.Contains(ask.Text, "可以吗？") || (c.Secret != "" && strings.Contains(ask.Text, c.Secret)) {
		t.Fatalf("ask=%+v ok=%v", ask, ok)
	}
	metaOut := map[string]string{}
	desktopbot.ApplyDangerAsk(metaOut, scope.SessionID)
	if metaOut["send_to_user"] != ask.Text || metaOut["send_to_user_kind"] != "ask" {
		t.Fatalf("send_to_user=%q", metaOut["send_to_user"])
	}
	if !c.Silence {
		return
	}
	now = start.Add(desktopbot.DangerWait)
	command := c.Commands[0]
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": command.Action, "command": command.Command,
	})
	if err == nil || err.Error() != desktopbot.DangerSilenceError || ran != 0 {
		t.Fatalf("silence err=%v ran=%d", err, ran)
	}
	again, _ := desktopbot.PeekDangerAsk(scope.SessionID)
	if again.Text != ask.Text {
		t.Fatalf("silence asked again: %q", again.Text)
	}
}
