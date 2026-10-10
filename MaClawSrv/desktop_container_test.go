package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
)

func TestContainerFileAndBashStayInTheDesktop(t *testing.T) {
	scope := agentruntime.Scope{TenantID: "tenant-file", UserID: "alice", InstanceID: "file-bot"}
	previousFile := desktopRemoteFile
	previousBash := desktopRemoteBash
	var gotAction, gotPath, gotContent string
	var bashCalls int
	desktopRemoteFile = func(_ context.Context, tenantID, userID, action, filePath, content, _, _ string) (string, error) {
		if tenantID != scope.TenantID || userID != scope.UserID {
			t.Fatalf("account %s/%s", tenantID, userID)
		}
		gotAction, gotPath, gotContent = action, filePath, content
		if action == "read" {
			return "int main(){}\n", nil
		}
		return "Wrote " + filePath, nil
	}
	desktopRemoteBash = func(context.Context, string, string, string) (string, error) {
		bashCalls++
		return "hi", nil
	}
	t.Cleanup(func() {
		desktopRemoteFile = previousFile
		desktopRemoteBash = previousBash
	})
	plan := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "plan"}}},
	}
	mod := desktopRuntimeModule{}
	read, err := mod.InvokeTool(context.Background(), plan, "desktop", map[string]any{
		"action": "file_read",
		"path":   "~/Desktop/a.c",
	})
	if err != nil || read != "int main(){}\n" || gotAction != "read" || gotPath != "/home/desktop/Desktop/a.c" {
		t.Fatalf("plan read=%q path=%q action=%s err=%v", read, gotPath, gotAction, err)
	}
	for _, action := range []string{"file_write", "bash"} {
		_, err = mod.InvokeTool(context.Background(), plan, "desktop", map[string]any{
			"action":  action,
			"path":    "~/Desktop/a.c",
			"content": "int main(){}\n",
			"command": "echo hi",
		})
		if err == nil || !strings.Contains(err.Error(), "plan phase blocks "+action) {
			t.Fatalf("plan %s err=%v", action, err)
		}
	}
	if bashCalls != 0 {
		t.Fatal("plan ran bash")
	}
	execute := agentruntime.TurnRequest{
		Scope: scope,
		Input: agentservice.ExecuteRequest{Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}}},
	}
	content := "int main(){}\n"
	wrote, err := mod.InvokeTool(context.Background(), execute, "desktop", map[string]any{
		"action":  "file_write",
		"path":    "~/Desktop/a.c",
		"content": content,
	})
	if err != nil || gotAction != "write" || gotPath != "/home/desktop/Desktop/a.c" || gotContent != content || !strings.Contains(wrote, "/home/desktop/Desktop/a.c") {
		t.Fatalf("write=%q path=%q content=%q err=%v", wrote, gotPath, gotContent, err)
	}
	ran, err := mod.InvokeTool(context.Background(), execute, "desktop", map[string]any{
		"action":  "bash",
		"command": "echo hi",
	})
	if err != nil || ran != "hi" || bashCalls != 1 {
		t.Fatalf("bash=%q calls=%d err=%v", ran, bashCalls, err)
	}
}

func TestHubBotUsesThePersonsDesktop(t *testing.T) {
	t.Setenv(desktopHubURLEnv, "")
	t.Setenv(desktopHubTokenEnv, "")
	var gotTenant, gotUser, gotPath string
	previousFile := desktopRemoteFile
	previousEnsure := desktopEnsureFn
	desktopRemoteFile = func(_ context.Context, tenantID, userID, action, filePath, _, _, _ string) (string, error) {
		gotTenant, gotUser, gotPath = tenantID, userID, filePath
		if action == "read" {
			return "int main(){}\n", nil
		}
		return "Wrote " + filePath, nil
	}
	ensured := false
	desktopEnsureFn = func(string) (desktopEndpoint, error) {
		ensured = true
		return desktopEndpoint{}, fmt.Errorf("local desktop")
	}
	t.Cleanup(func() {
		desktopRemoteFile = previousFile
		desktopEnsureFn = previousEnsure
		desktopUserByInstance.Delete("inst_person")
	})
	turn := agentruntime.TurnRequest{
		Scope: agentruntime.Scope{TenantID: "maclaw-tenant", UserID: "service-account", InstanceID: "inst_person"},
		Input: agentservice.ExecuteRequest{
			Instance: agentservice.Instance{ID: "inst_person", Metadata: map[string]string{
				"hub_bot": "1", "hub_user_id": "alice", "hub_tenant_id": "tenant-a",
			}},
			Message: agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}},
		},
	}
	read, err := (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "file_read",
		"path":   "~/Desktop/a.c",
	})
	if err != nil || read != "int main(){}\n" || gotTenant != "tenant-a" || gotUser != "alice" || gotPath != "/home/desktop/Desktop/a.c" || ensured {
		t.Fatalf("read=%q account=%s/%s path=%q ensured=%v err=%v", read, gotTenant, gotUser, gotPath, ensured, err)
	}
	desktopRemoteFile = nil
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), turn, "desktop", map[string]any{
		"action": "navigate",
		"url":    "https://www.youtube.com/",
	})
	if err == nil || !strings.Contains(err.Error(), "person's cloud desktop") || ensured {
		t.Fatalf("navigate err=%v ensured=%v", err, ensured)
	}
	unbound := turn
	unbound.Input = agentservice.ExecuteRequest{
		Instance: agentservice.Instance{ID: "inst_unbound_turn", Metadata: map[string]string{"hub_bot": "1"}},
		Message:  agentservice.Message{Metadata: map[string]string{"bot_phase": "execute"}},
	}
	unbound.Scope.InstanceID = "inst_unbound_turn"
	_, err = (desktopRuntimeModule{}).InvokeTool(context.Background(), unbound, "desktop", map[string]any{
		"action": "file_read",
		"path":   "~/Desktop/a.c",
	})
	if err == nil || !strings.Contains(err.Error(), "not bound") || ensured {
		t.Fatalf("unbound err=%v ensured=%v", err, ensured)
	}
}
