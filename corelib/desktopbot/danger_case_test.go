package desktopbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDesktopBotDangerCases(t *testing.T) {
	for _, id := range []string{"bash_ls_allowed", "bash_rm_blocked", "git_push_asks", "silence_blocks"} {
		t.Run(id, func(t *testing.T) {
			ResetDangerState()
			t.Setenv("MACLAW_DESKTOP_INSTALL_ASK", "on")
			t.Cleanup(ResetDangerState)
			runDangerCase(t, readDangerCase(t, id))
		})
	}
}

type dangerCaseCommand struct {
	Action  string `json:"action"`
	Command string `json:"command"`
}

type dangerCase struct {
	ID        string              `json:"id"`
	Verdict   string              `json:"verdict"`
	Secret    string              `json:"secret"`
	Commands  []dangerCaseCommand `json:"commands"`
	Helper    bool                `json:"helper"`
	ExpectRan bool                `json:"expect_ran"`
	Ask       bool                `json:"ask"`
	Silence   bool                `json:"silence"`
}

func readDangerCase(t *testing.T, id string) dangerCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c dangerCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runDangerCase(t *testing.T, c dangerCase) {
	t.Helper()
	start := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for i, command := range c.Commands {
		result := GateDanger(DangerInput{
			Call:      DangerCall{Action: command.Action, Command: command.Command, Helper: c.Helper},
			Verdict:   c.Verdict,
			VerdictOK: true,
			Key:       c.ID + ":" + command.Command,
			Now:       start,
		})
		if result.Run != c.ExpectRan {
			t.Fatalf("%s run=%v", command.Command, result.Run)
		}
		if c.Secret != "" && (strings.Contains(result.Error, c.Secret) || strings.Contains(result.Ask.Text, c.Secret)) {
			t.Fatalf("secret in %s: %+v", command.Command, result)
		}
		if c.ExpectRan {
			if result.Ask.Text != "" || result.Error != "" {
				t.Fatalf("allowed call asked: %+v", result)
			}
			continue
		}
		if result.Error != DangerAskedError {
			t.Fatalf("%s err=%q", command.Command, result.Error)
		}
		if c.Ask && i == 0 && (result.Ask.Kind != "ask" || !strings.Contains(result.Ask.Text, "才能继续，可以吗？")) {
			t.Fatalf("ask=%+v", result.Ask)
		}
		NoteDangerAsk([]string{c.ID}, result.Ask)
	}
	if !c.Ask {
		return
	}
	meta := map[string]string{}
	ApplyDangerAsk(meta, c.ID)
	if meta["send_to_user_kind"] != "ask" || !strings.Contains(meta["send_to_user"], "才能继续，可以吗？") || (c.Secret != "" && strings.Contains(meta["send_to_user"], c.Secret)) {
		t.Fatalf("send_to_user=%q kind=%q", meta["send_to_user"], meta["send_to_user_kind"])
	}
	meta["send_to_user"] = "已经有一句"
	ApplyDangerAsk(meta, c.ID)
	if meta["send_to_user"] != "已经有一句" {
		t.Fatalf("replaced an existing sentence: %q", meta["send_to_user"])
	}
	if !c.Silence {
		return
	}
	command := c.Commands[0]
	later := GateDanger(DangerInput{
		Call:      DangerCall{Action: command.Action, Command: command.Command, Helper: c.Helper},
		Verdict:   c.Verdict,
		VerdictOK: true,
		Key:       c.ID + ":" + command.Command,
		Now:       start.Add(DangerWait),
	})
	if later.Run || later.Error != DangerSilenceError || later.Ask.Text != "" {
		t.Fatalf("silence=%+v", later)
	}
}
