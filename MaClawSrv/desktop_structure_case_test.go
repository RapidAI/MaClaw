package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
)

// TestDesktopBotStructureCases is the MaClawSrv half of PR-7. action=computer
// on a browser does not call xdotool. A dialog without an element id does
// not click coordinates. A captcha stays a handoff.
func TestDesktopBotStructureCases(t *testing.T) {
	for _, id := range []string{"browser_no_pixel", "dialog_probe_first", "captcha_handoff"} {
		t.Run(id, func(t *testing.T) {
			runStructureToolCase(t, readStructureToolCase(t, id))
		})
	}
}

type structureToolCase struct {
	ID       string `json:"id"`
	Window   string `json:"window"`
	Page     string `json:"page"`
	Secret   string `json:"secret"`
	Expect   string `json:"expect"`
	Takeover string `json:"takeover"`
	Steps    []any  `json:"steps"`
}

func readStructureToolCase(t *testing.T, id string) structureToolCase {
	t.Helper()
	path := filepath.Join("..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c structureToolCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runStructureToolCase(t *testing.T, c structureToolCase) {
	t.Helper()
	calls := stubStructureDesktop(t, c.Window)
	scope := agentruntime.Scope{TenantID: "tenant-structure", UserID: "alice-" + c.ID, InstanceID: "structure-" + c.ID}
	t.Cleanup(func() {
		desktopPageNeed.Delete(desktopHandoffKey(scope.TenantID, scope.UserID, scope.InstanceID))
	})
	if c.Page != "" {
		noteDesktopPageNeed(scope, "page flags: "+c.Page)
	}
	turn := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{
			"bot_phase": "execute", "computer_helper": "1",
		}}},
	}
	args := map[string]any{"action": "computer", "actions": c.Steps}
	_, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", args)
	if err == nil || err.Error() != c.Expect {
		t.Fatalf("computer err=%v", err)
	}
	if c.Secret != "" && (strings.Contains(err.Error(), c.Secret) || structureCallsContain(calls, c.Secret)) {
		t.Fatalf("secret reached the desktop: err=%v calls=%v", err, *calls)
	}
	if structureCallsContain(calls, "click") || structureCallsContain(calls, "type") || structureCallsContain(calls, "mousemove") {
		t.Fatalf("pixel batch ran: %v", *calls)
	}
	if c.Takeover != "" {
		before := len(*calls)
		text, takeErr := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action": "request_takeover",
			"reason": c.Takeover,
		})
		if takeErr != nil || !strings.Contains(text, "Takeover requested") || !strings.Contains(text, "交还") || strings.Contains(text, c.Secret) || len(*calls) != before {
			t.Fatalf("takeover text=%q err=%v calls=%v", text, takeErr, *calls)
		}
	}
	if c.ID == "dialog_probe_first" {
		_, idErr := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
			"action":  "computer",
			"actions": []any{map[string]any{"action": "click", "id": 3.0}},
		})
		if idErr == nil || idErr.Error() == computeruse.ProbeFirst || structureCallsContain(calls, "mousemove") {
			t.Fatalf("element id was treated as a coordinate click: %v calls=%v", idErr, *calls)
		}
	}
}

func stubStructureDesktop(t *testing.T, class string) *[][]string {
	t.Helper()
	previousSession := desktopRemoteSession
	previousApp := desktopRemoteApp
	previousCtl := desktopRemoteCtl
	var calls [][]string
	desktopRemoteSession = func(context.Context, string, string) (desktopEndpoint, error) {
		return desktopEndpoint{CDP: "http://127.0.0.1:9", Display: ":99"}, nil
	}
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		if len(args) >= 2 && args[0] == "getactivewindow" && args[1] == "getwindowclassname" {
			return class, nil
		}
		return "", nil
	}
	// An element id is allowed through the gate. Without a control service
	// the step fails here, and it must not fall through to a coordinate click.
	desktopRemoteCtl = func(context.Context, string, string, string, map[string]any) (map[string]any, error) {
		return nil, fmt.Errorf("desktop control failed")
	}
	t.Cleanup(func() {
		desktopRemoteSession = previousSession
		desktopRemoteApp = previousApp
		desktopRemoteCtl = previousCtl
	})
	return &calls
}

func structureCallsContain(calls *[][]string, part string) bool {
	for _, argv := range *calls {
		if strings.Contains(strings.Join(argv, " "), part) {
			return true
		}
	}
	return false
}
