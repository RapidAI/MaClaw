package guiapp

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A cloud-desktop run that ended with its task not done must never become a
// "任务完成了" notification, on the live relay, on a delivery pulled after a
// reconnect, or on a scheduled run. "done", an ordinary answer, an older Hub
// without task_outcome and an unknown value keep the old behaviour.
func TestDesktopTaskOutcomeDecidesCompletion(t *testing.T) {
	cases := []struct {
		name, outcome string
		complete      bool
	}{
		{"done", "done", true},
		{"partial", "partial", false},
		{"blocked", "blocked", false},
		{"not_submitted", "not_submitted", false},
		{"unconfirmed", "unconfirmed", false},
		{"awaiting_user", "awaiting_user", false},
		{"running", "running", false},
		{"ordinary question", "", true},
		{"older hub", "", true},
		{"unknown value", "mostly", true},
	}
	t.Cleanup(func() {
		desktopBotExchangeOverride = nil
		desktopBotRelayOverride = nil
		desktopBotResultSink = nil
		desktopBotReportSink = nil
	})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Hub's run poll body, as the GUI reads it.
			wire := map[string]any{"text": "已经发好了！", "send_to_user": "已经发好了！", "status": "succeeded"}
			if tc.outcome != "" {
				wire["task_outcome"] = tc.outcome
			}
			desktopBotExchangeOverride = func(_ context.Context, method, path string, _ any, dest any, _ time.Duration, _ map[string]string) (int, error) {
				if method != http.MethodGet || !strings.Contains(path, "/runs/run_1") {
					t.Fatalf("poll = %s %s", method, path)
				}
				fillDesktopBotDest(t, dest, wire)
				return http.StatusOK, nil
			}
			result, err := (&App{}).waitDesktopBotRun(context.Background(), "bot_1", "run_1")
			if err != nil {
				t.Fatal(err)
			}
			want := desktopTaskOutcomeOf(tc.outcome)
			if result.TaskOutcome != want {
				t.Fatalf("relay outcome %q, want %q", result.TaskOutcome, want)
			}

			// Live turn: relay result -> IMAgentResponse -> tray decision.
			var got *IMAgentResponse
			desktopBotResultSink = func(resp *IMAgentResponse) { got = resp }
			desktopBotRelayOverride = func(context.Context, string, string, string) (desktopBotRelayResult, error) {
				return result, nil
			}
			(&App{}).finishDesktopBotTask("desktop-bot-outcome-"+tc.name, "alice:bot_1", "bot_1", "发帖", "execute")
			if got == nil || got.TaskOutcome != want {
				t.Fatalf("live response = %#v", got)
			}
			if trayResponseIsCompletion(got) != tc.complete {
				t.Fatalf("tray completion = %v, want %v", !tc.complete, tc.complete)
			}

			// Delivery pulled after a reconnect or restart.
			var payload string
			desktopBotReportSink = func(raw string) { payload = raw }
			item := desktopBotDelivery{BotID: "bot_1", RunID: "run_1", Status: "succeeded", Phase: "execute"}
			item.Reply.SendToUser = "已经发好了！"
			item.Reply.TaskOutcome = tc.outcome
			if !(&App{}).emitDesktopBotDelivery("local", item) {
				t.Fatal("emit")
			}
			if has := strings.Contains(payload, `"task_outcome":"`+want+`"`); has != (want != "") {
				t.Fatalf("delivery payload = %s", payload)
			}

			// A scheduled run that did not finish its task is reported.
			// A handed-off job is left to the watcher, not reported as a stall.
			if !tc.complete && tc.outcome != "running" && !desktopScheduleReports("无变化", desktopBotRelayResult{Text: "无变化", TaskOutcome: want}, nil) {
				t.Fatal("scheduled run with an unfinished task stayed quiet")
			}
			if tc.outcome == "running" && desktopScheduleReports("无变化", desktopBotRelayResult{Text: "无变化", TaskOutcome: want}, nil) {
				t.Fatal("a running handoff was reported as an unfinished stall")
			}
		})
	}
	if desktopScheduleReports("无变化", desktopBotRelayResult{Text: "无变化", TaskOutcome: "done"}, nil) {
		t.Fatal("a quiet done schedule started reporting")
	}
}
