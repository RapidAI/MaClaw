package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/remote"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

func TestCodingPlanExecutionPreservesPendingPlanWhenTargetUnavailable(t *testing.T) {
	cases := []struct {
		name string
		kind string
		run  func(*IMMessageHandler, string, stickyCodingWorkbenchMemory) *IMAgentResponse
	}{
		{
			name: "approve local project path missing",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeApprovedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
		{
			name: "skip local project path missing",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeSkippedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
		{
			name: "approve remote SSH session missing",
			kind: "remote",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeApprovedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
		{
			name: "skip remote SSH session missing",
			kind: "remote",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeSkippedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{}
			userID := "desktop-user:pending-plan-preserved:" + strings.ReplaceAll(tc.name, " ", "-")
			tasks := []*v2.TaskItem{
				{Index: 1, Title: "inspect", Description: "inspect code"},
				{Index: 2, Title: "implement", Description: "implement fix", DependsOn: []int{1}},
			}
			h.storeStickyPendingCodingPlan(userID, "fix the issue", "### T1: inspect\n### T2: implement", tasks)
			mem := h.getStickyCodingWorkbenchMemory(userID)
			mem.Kind = tc.kind
			h.storeStickyCodingWorkbenchMemory(userID, mem)
			t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

			resp := tc.run(h, userID, mem)
			if resp == nil || strings.TrimSpace(resp.Text) == "" {
				t.Fatalf("expected a recoverable failure response, got %+v", resp)
			}
			if pending, ok := h.loadStickyPendingCodingPlan(userID); !ok || len(pending.Tasks) != len(tasks) {
				t.Fatalf("pending plan must be preserved, got ok=%v pending=%+v", ok, pending)
			}
			if after := h.getStickyCodingWorkbenchMemory(userID); strings.TrimSpace(after.ApprovedPlanJSON) != "" {
				t.Fatalf("unrunnable plan must not be promoted: %+v", after)
			}
		})
	}
}

func TestCodingPlanExecutionPreservesPendingPlanWhenRemoteSessionCannotRecover(t *testing.T) {
	cases := []struct {
		name string
		run  func(*IMMessageHandler, string, stickyCodingWorkbenchMemory) *IMAgentResponse
	}{
		{
			name: "approve",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeApprovedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
		{
			name: "skip",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeSkippedCodingPlan(userID, "", mem, IMUserMessage{}, nil, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{}
			userID := "desktop-user:remote-recovery-preserved:" + tc.name
			tasks := []*v2.TaskItem{
				{Index: 1, Title: "inspect", Description: "inspect code"},
				{Index: 2, Title: "implement", Description: "implement fix", DependsOn: []int{1}},
			}
			h.storeStickyPendingCodingPlan(userID, "fix the issue", "### T1: inspect\n### T2: implement", tasks)
			mem := h.getStickyCodingWorkbenchMemory(userID)
			mem.Kind = "remote"
			mem.RemoteSessionID = "expired-session"
			h.storeStickyCodingWorkbenchMemory(userID, mem)
			t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

			resp := tc.run(h, userID, mem)
			if resp == nil || !strings.Contains(resp.Text, "计划已保留") {
				t.Fatalf("expected preserved-plan recovery response, got %+v", resp)
			}
			if _, ok := h.loadStickyPendingCodingPlan(userID); !ok {
				t.Fatal("pending plan must survive a failed remote session recovery")
			}
			if after := h.getStickyCodingWorkbenchMemory(userID); strings.TrimSpace(after.ApprovedPlanJSON) != "" {
				t.Fatalf("failed remote recovery must not promote the plan: %+v", after)
			}
		})
	}
}

func TestSSHSessionStatusUsableRejectsTerminalSessions(t *testing.T) {
	for _, status := range []remote.SessionStatus{remote.SessionExited, remote.SessionError} {
		if sshSessionStatusUsable(status) {
			t.Fatalf("terminal SSH status must be unusable: %s", status)
		}
	}
	for _, status := range []remote.SessionStatus{remote.SessionStarting, remote.SessionRunning, remote.SessionBusy, remote.SessionWaitingInput} {
		if !sshSessionStatusUsable(status) {
			t.Fatalf("non-terminal SSH status must stay usable: %s", status)
		}
	}
}

func TestCodingPlanExecutionPreservesPendingPlanWhenLocalProjectPathIsUnavailable(t *testing.T) {
	cases := []struct {
		name string
		run  func(*IMMessageHandler, string, stickyCodingWorkbenchMemory) *IMAgentResponse
	}{
		{
			name: "approve",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeApprovedCodingPlan(userID, mem.ProjectPath, mem, IMUserMessage{}, nil, nil)
			},
		},
		{
			name: "skip",
			run: func(h *IMMessageHandler, userID string, mem stickyCodingWorkbenchMemory) *IMAgentResponse {
				return h.executeSkippedCodingPlan(userID, mem.ProjectPath, mem, IMUserMessage{}, nil, nil)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &IMMessageHandler{}
			userID := "desktop-user:missing-local-project-preserved:" + tc.name
			tasks := []*v2.TaskItem{
				{Index: 1, Title: "inspect", Description: "inspect code"},
				{Index: 2, Title: "implement", Description: "implement fix", DependsOn: []int{1}},
			}
			h.storeStickyPendingCodingPlan(userID, "fix the issue", "### T1: inspect\n### T2: implement", tasks)
			mem := h.getStickyCodingWorkbenchMemory(userID)
			mem.ProjectPath = t.TempDir() + "/removed-workspace"
			h.storeStickyCodingWorkbenchMemory(userID, mem)
			t.Cleanup(func() { h.clearStickyCodingWorkbenchMemory(userID) })

			resp := tc.run(h, userID, mem)
			if resp == nil || !strings.Contains(resp.Text, "计划已保留") {
				t.Fatalf("expected preserved-plan workspace failure response, got %+v", resp)
			}
			if _, ok := h.loadStickyPendingCodingPlan(userID); !ok {
				t.Fatal("pending plan must survive an unavailable local workspace")
			}
			if after := h.getStickyCodingWorkbenchMemory(userID); strings.TrimSpace(after.ApprovedPlanJSON) != "" {
				t.Fatalf("unavailable local workspace must not promote the plan: %+v", after)
			}
		})
	}
}
