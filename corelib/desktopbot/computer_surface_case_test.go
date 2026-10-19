package desktopbot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopBotComputerCases(t *testing.T) {
	for _, id := range []string{
		"main_has_no_computer_tools",
		"gate_dispatches_helper",
		"readonly_screenshot_has_no_click",
		"helper_one_per_screen",
		"helper_compact_result",
	} {
		t.Run(id, func(t *testing.T) {
			runComputerCase(t, readComputerCase(t, id))
		})
	}
}

type computerCase struct {
	ID       string   `json:"id"`
	Names    []string `json:"names"`
	Expect   []string `json:"expect"`
	Forbid   []string `json:"forbid"`
	Open     bool     `json:"open"`
	Screen   string   `json:"screen"`
	HelperID string   `json:"helper_id"`
	Goal     string   `json:"goal"`
	First    string   `json:"first"`
	Second   string   `json:"second"`
	Params   []string `json:"params"`
	Trace    string   `json:"trace"`
	Report   string   `json:"report"`
}

func readComputerCase(t *testing.T, id string) computerCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c computerCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runComputerCase(t *testing.T, c computerCase) {
	t.Helper()
	switch c.ID {
	case "main_has_no_computer_tools":
		got := MainAgentToolNames(c.Names)
		if strings.Join(got, ",") != strings.Join(c.Expect, ",") {
			t.Fatalf("names = %v", got)
		}
		for _, name := range got {
			if MainDropsTool(name) {
				t.Fatalf("main agent kept %s", name)
			}
		}
	case "gate_dispatches_helper":
		got := DispatchComputerGate(GateInput{Open: c.Open, Screen: c.Screen, HelperID: c.HelperID, Goal: c.Goal})
		if got.MainActive || !got.Dispatched || got.Queued {
			t.Fatalf("gate = %+v", got)
		}
		text := got.Card.Text()
		if !strings.Contains(text, c.Goal) || !strings.Contains(text, "constraints:") || !strings.Contains(text, "success:") {
			t.Fatalf("card = %s", text)
		}
		for _, word := range []string{"click", "type", "computer", "聊天回复", "chat reply"} {
			if strings.Contains(strings.ToLower(text), word) {
				t.Fatalf("card names %s: %s", word, text)
			}
		}
	case "readonly_screenshot_has_no_click":
		if ScreenshotParamsHaveClick(ReadonlyScreenshotParams()) {
			t.Fatal("readonly screenshot advertises a click")
		}
		stripped := StripScreenshotClickParams(c.Params)
		if ScreenshotParamsHaveClick(stripped) {
			t.Fatalf("stripped params still click: %v", stripped)
		}
		for _, want := range c.Expect {
			found := false
			for _, got := range stripped {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("stripped %v lost %s", stripped, want)
			}
		}
	case "helper_one_per_screen":
		first := DispatchComputerGate(GateInput{Open: true, Screen: c.Screen, HelperID: c.First, Goal: c.Goal})
		second := DispatchComputerGate(GateInput{Open: true, Screen: c.Screen, HelperID: c.Second, Goal: c.Goal, Held: first.Held})
		if !first.Dispatched || first.Queued || second.Dispatched || !second.Queued {
			t.Fatalf("first=%+v second=%+v", first, second)
		}
		if second.Held[c.Screen] != c.First {
			t.Fatalf("screen holder = %s", second.Held[c.Screen])
		}
	case "helper_compact_result":
		shown, err := MainHelperView(c.Trace, c.Report)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"首页已经打开", "没有新邮件", "/home/desktop/tasks/a/out/1.txt"} {
			if !strings.Contains(shown, part) {
				t.Fatalf("shown = %s", shown)
			}
		}
		for _, word := range []string{"computer_click", "password", "x=12"} {
			if strings.Contains(shown, word) {
				t.Fatalf("trace leaked %s in %s", word, shown)
			}
		}
	default:
		t.Fatalf("unknown case %s", c.ID)
	}
}
