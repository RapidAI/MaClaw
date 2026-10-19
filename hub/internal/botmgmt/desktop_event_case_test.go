package botmgmt

import (
	"strings"
	"testing"
	"time"
)

func TestDesktopBotEventCases(t *testing.T) {
	t.Run("round_end_drops_admit", func(t *testing.T) {
		now := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
		svc := &Service{desktopAdmits: map[string]*desktopAdmitState{}}
		freshKey := desktopAdmitKey("tenant-a", "alice", "bot", "")
		oldKey := desktopAdmitKey("tenant-a", "alice", "bot-old", "")
		runKey := desktopAdmitKey("tenant-a", "alice", "bot", "run-1")
		doneKey := desktopAdmitKey("tenant-a", "alice", "bot", "run-done")
		svc.desktopAdmits[freshKey] = &desktopAdmitState{userID: "alice", botID: "bot", started: now}
		svc.desktopAdmits[oldKey] = &desktopAdmitState{userID: "alice", botID: "bot-old", started: now.Add(-time.Minute), abandoned: now.Add(-time.Minute)}
		svc.desktopAdmits[runKey] = &desktopAdmitState{userID: "alice", botID: "bot", runID: "run-1", started: now.Add(-time.Hour)}
		svc.desktopAdmits[doneKey] = &desktopAdmitState{userID: "alice", botID: "bot", runID: "run-done", done: true, finished: now}

		svc.dropAbandonedDesktopReserves("tenant-a", "alice", "", now)
		if _, ok := svc.desktopAdmits[freshKey]; !ok {
			t.Fatal("a fresh empty reserve was dropped")
		}
		if _, ok := svc.desktopAdmits[oldKey]; ok {
			t.Fatal("an empty reserve older than 30s stayed")
		}
		if _, ok := svc.desktopAdmits[runKey]; !ok || svc.desktopAdmits[doneKey] == nil {
			t.Fatal("a finished round was dropped before it was acked")
		}

		svc.EndDesktopRound("tenant-a", "alice", "bot", "run-1")
		svc.EndDesktopRound("tenant-a", "alice", "bot", "run-done")
		if _, ok := svc.desktopAdmits[runKey]; ok {
			t.Fatal("the round left its admit")
		}
		if _, ok := svc.desktopAdmits[doneKey]; ok {
			t.Fatal("the finished round left its admit")
		}
		if _, ok := svc.desktopAdmits[freshKey]; !ok {
			t.Fatal("ending a run dropped the empty reserve")
		}
	})

	t.Run("handoff_does_not_refresh", func(t *testing.T) {
		clock := time.Date(2026, 10, 18, 8, 0, 0, 0, time.UTC)
		svc := &Service{Now: func() time.Time { return clock }}
		gated := svc.gateDesktopHandoff("http://10.1.2.3:6080/vnc.html")
		const marker = "/api/v1/desktop-handoff/"
		token := strings.TrimPrefix(gated, marker)
		if i := strings.IndexByte(token, '/'); i >= 0 {
			token = token[:i]
		}
		if !validHandoffToken(token) {
			t.Fatalf("token = %q", token)
		}
		first, ok := svc.handoffViewFor(token)
		if !ok || svc.handoffOrigin(token) == "" {
			t.Fatal("handoff was not stored")
		}
		second, ok := svc.handoffViewFor(token)
		if !ok || !second.until.Equal(first.until) || second.origin != first.origin {
			t.Fatal("looking up the token refreshed it")
		}
		clock = clock.Add(3 * time.Hour)
		if svc.handoffOrigin(token) != "" {
			t.Fatal("the two hour token was extended")
		}
	})
}
