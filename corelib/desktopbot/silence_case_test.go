package desktopbot_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestDesktopBotSilenceCases(t *testing.T) {
	if strings.Contains(desktopbot.WorthSayingPrompt, "没有变化就结束") {
		t.Fatal("the routine line is in the system prompt")
	}
	if !strings.Contains(desktopbot.RoutineQuietPrompt, "没有变化就结束") || !strings.Contains(desktopbot.RoutineQuietPrompt, "(无输出)") {
		t.Fatal("routine prompt lost the quiet ending")
	}
	pinned := desktopbot.PinWorthSaying("系统规则")
	if pinned != "系统规则\n\n"+desktopbot.WorthSayingPrompt || desktopbot.PinWorthSaying(pinned) != pinned {
		t.Fatalf("pin = %q", pinned)
	}
	for _, id := range []string{
		"no_bubble_per_step",
		"done_sends_once",
		"ask_progress_reads_file",
		"steer_says_on_it",
		"schedule_unchanged_silent",
		"schedule_changed_desktop",
		"schedule_failure_mobile",
		"event_quiet",
		"reply_stays_on_surface",
		"filler_not_delivered",
	} {
		t.Run(id, func(t *testing.T) {
			runSilenceCase(t, readSilenceCase(t, id))
		})
	}
	missed := desktopbot.SettleQuiet(desktopbot.SettleQuietInput{Waiting: true})
	if !missed.Missed || missed.Quiet {
		t.Fatalf("waiting empty = %+v", missed)
	}
	quiet := desktopbot.SettleQuiet(desktopbot.SettleQuietInput{})
	if !quiet.Quiet || quiet.Missed {
		t.Fatalf("unattended empty = %+v", quiet)
	}
	kept := desktopbot.SettleQuiet(desktopbot.SettleQuietInput{SendToUser: "首页没打开。"})
	if kept.Quiet || kept.Missed {
		t.Fatalf("a sentence was dropped: %+v", kept)
	}
	filler := desktopbot.SettleQuiet(desktopbot.SettleQuietInput{Waiting: true, Text: "无变化"})
	if filler.Quiet || filler.Missed {
		t.Fatalf("filler prose was treated as an empty body: %+v", filler)
	}
}

type silenceCase struct {
	ID           string   `json:"id"`
	Origin       string   `json:"origin"`
	Channel      string   `json:"channel"`
	Surface      string   `json:"surface"`
	Waiting      bool     `json:"waiting"`
	Step         bool     `json:"step"`
	Done         bool     `json:"done"`
	Changed      bool     `json:"changed"`
	Ask          bool     `json:"ask"`
	AskText      string   `json:"ask_text"`
	Failed       bool     `json:"failed"`
	FailureText  string   `json:"failure_text"`
	ProgressAsk  bool     `json:"progress_ask"`
	WhereWeAre   string   `json:"where_we_are"`
	NextStep     string   `json:"next_step"`
	OpenProblems string   `json:"open_problems"`
	Steer        bool     `json:"steer"`
	Lang         string   `json:"lang"`
	Prose        string   `json:"prose"`
	SendToUser   string   `json:"send_to_user"`
	Where        string   `json:"where"`
	ExpectText   string   `json:"expect_text"`
	ExpectKind   string   `json:"expect_kind"`
	ExpectCount  int      `json:"expect_count"`
	ExpectPhone  bool     `json:"expect_phone"`
	ExpectReport bool     `json:"expect_report"`
	Forbid       []string `json:"forbid"`
}

func readSilenceCase(t *testing.T, id string) silenceCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c silenceCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runSilenceCase(t *testing.T, c silenceCase) {
	t.Helper()
	file := ""
	if c.ProgressAsk {
		file = desktopbot.RenderProgress(desktopbot.Sections{
			WhereWeAre: c.WhereWeAre, NextStep: c.NextStep, OpenProblems: c.OpenProblems,
		})
	}
	got := desktopbot.Speak(desktopbot.SpeakInput{
		Origin: c.Origin, Channel: c.Channel, Surface: c.Surface, Waiting: c.Waiting,
		Step: c.Step, Done: c.Done, Changed: c.Changed, Ask: c.Ask, AskText: c.AskText,
		Failed: c.Failed, FailureText: c.FailureText, ProgressAsk: c.ProgressAsk,
		ProgressFile: file, Awake: c.ProgressAsk, Steer: c.Steer, Lang: c.Lang,
		Prose: c.Prose, SendToUser: c.SendToUser, WhereWeAre: c.Where,
	})
	if len(got.Records) != c.ExpectCount {
		t.Fatalf("records = %+v", got.Records)
	}
	if got.Phone != c.ExpectPhone || got.Report != c.ExpectReport {
		t.Fatalf("phone=%v report=%v decision=%+v", got.Phone, got.Report, got)
	}
	var blob strings.Builder
	for _, rec := range got.Records {
		blob.WriteString(rec.Channel)
		blob.WriteByte('\n')
		blob.WriteString(rec.Text)
		blob.WriteByte('\n')
		blob.WriteString(rec.Kind)
		blob.WriteByte('\n')
		if rec.Channel == "email" && c.Surface == "desktop" {
			t.Fatalf("desktop reply also went to email: %+v", rec)
		}
	}
	if c.ExpectText != "" {
		if len(got.Records) != 1 || got.Records[0].Text != c.ExpectText || got.Records[0].Kind != c.ExpectKind {
			t.Fatalf("record = %+v", got.Records)
		}
		if c.Channel != "" && c.Origin != "person" && got.Records[0].Channel != c.Channel {
			t.Fatalf("channel = %s", got.Records[0].Channel)
		}
		if c.Surface != "" && c.Origin == "person" && got.Records[0].Channel != c.Surface {
			t.Fatalf("surface = %s", got.Records[0].Channel)
		}
	}
	for _, word := range c.Forbid {
		if strings.Contains(blob.String(), word) {
			t.Fatalf("outbound contains %q: %s", word, blob.String())
		}
	}
	switch c.ID {
	case "no_bubble_per_step":
		if !got.UpdateProgress || got.WorkRow != c.Where || got.ClearWorkRow {
			t.Fatalf("step row = %+v", got)
		}
	case "ask_progress_reads_file":
		if !strings.Contains(got.Records[0].Text, c.NextStep) || !strings.Contains(got.Records[0].Text, c.WhereWeAre) {
			t.Fatalf("progress answer = %q", got.Records[0].Text)
		}
		view := desktopbot.Visible(desktopbot.Turn{Prose: c.WhereWeAre, SendToUser: ""})
		if view.Bubble {
			t.Fatal("reading the file opened a progress bubble")
		}
	case "schedule_unchanged_silent":
		if !got.UpdateProgress || !got.ClearWorkRow || !got.QuietSuccess {
			t.Fatalf("unchanged = %+v", got)
		}
	case "event_quiet":
		if !got.QuietSuccess || got.MissedReply {
			t.Fatalf("event = %+v", got)
		}
	case "filler_not_delivered":
		for _, prose := range []string{"无变化", "(无输出)", "no change"} {
			view := desktopbot.Visible(desktopbot.Turn{Prose: prose})
			if view.Bubble || view.Sentence != "" {
				t.Fatalf("surface showed %q", prose)
			}
		}
	case "steer_says_on_it":
		if strings.Contains(got.Records[0].Text, "安排") {
			t.Fatal("steer restated the arrangement")
		}
	}
}
