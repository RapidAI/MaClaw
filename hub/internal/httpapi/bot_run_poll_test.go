package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
)

func TestDesktopRunPollBodyEndsAFinishedRun(t *testing.T) {
	t.Run("real failure is 200 with the error", func(t *testing.T) {
		status, body, userErr := desktopRunPollBody(botmgmt.Reply{Text: "无变化"}, true, errors.New("browser broke"))
		if userErr != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v", status, userErr)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, `"error":"browser broke"`) || strings.Contains(text, `"error":"无变化"`) {
			t.Fatalf("body=%s", text)
		}
	})

	t.Run("filler is 200 without an error", func(t *testing.T) {
		status, body, userErr := desktopRunPollBody(botmgmt.Reply{}, true, errors.New("run ended without a result"))
		if userErr != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v", status, userErr)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"error"`) {
			t.Fatalf("body=%s", raw)
		}
	})

	t.Run("a failed handoff keeps the desktop", func(t *testing.T) {
		status, body, userErr := desktopRunPollBody(botmgmt.Reply{
			Handoff:         true,
			AttentionReason: "login_wall",
			NovncURL:        "/api/v1/desktop-handoff/tok/vnc.html",
			AskUserQuestion: "请输入密码。",
		}, true, errors.New("browser broke"))
		if userErr != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v", status, userErr)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, `"handoff":true`) || !strings.Contains(text, `"attention_reason":"login_wall"`) || !strings.Contains(text, `"novnc_url":"/api/v1/desktop-handoff/tok/vnc.html"`) || !strings.Contains(text, `"ask_user_question":"请输入密码。"`) || !strings.Contains(text, `"error":"browser broke"`) {
			t.Fatalf("body=%s", text)
		}
	})

	t.Run("a sentence hides the error", func(t *testing.T) {
		status, body, userErr := desktopRunPollBody(botmgmt.Reply{Text: "无变化", SendToUser: "首页没打开。"}, true, errors.New("browser broke"))
		if userErr != nil || status != http.StatusOK {
			t.Fatalf("status=%d err=%v", status, userErr)
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, `"send_to_user":"首页没打开。"`) || strings.Contains(text, `"error"`) {
			t.Fatalf("body=%s", text)
		}
	})

	t.Run("a run still going is 202", func(t *testing.T) {
		status, _, userErr := desktopRunPollBody(botmgmt.Reply{}, false, nil)
		if userErr != nil || status != http.StatusAccepted {
			t.Fatalf("status=%d err=%v", status, userErr)
		}
	})

	t.Run("an unknown run is still a user error", func(t *testing.T) {
		_, _, userErr := desktopRunPollBody(botmgmt.Reply{}, false, botmgmt.ErrNotFound)
		if !errors.Is(userErr, botmgmt.ErrNotFound) {
			t.Fatalf("err=%v", userErr)
		}
	})
}
