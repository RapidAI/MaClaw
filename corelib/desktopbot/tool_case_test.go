package desktopbot_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

// The offline cases for PR-3 that do not need MaClawSrv. Each file names one
// behavior. The runner calls the functions the product uses.

func TestDesktopBotToolCases(t *testing.T) {
	desktopbot.ResetToolSessionState()
	t.Cleanup(desktopbot.ResetToolSessionState)
	for _, id := range []string{"missing_binary_says_next_command", "connector_down_no_browser"} {
		t.Run(id, func(t *testing.T) {
			desktopbot.ResetToolSessionState()
			raw := readToolCase(t, id)
			switch id {
			case "missing_binary_says_next_command":
				runMissingBinary(t, raw)
			case "connector_down_no_browser":
				runConnectorDown(t, raw)
			default:
				t.Fatalf("no runner for %s", id)
			}
		})
	}
	t.Run("spill_keeps_path_and_one_line", func(t *testing.T) {
		runSpill(t)
	})
}

type toolCase struct {
	ID            string   `json:"id"`
	Script        string   `json:"script"`
	Expect        string   `json:"expect"`
	ExpectAlso    string   `json:"expect_also"`
	Forbid        []string `json:"forbid"`
	Connector     string   `json:"connector"`
	Status        int      `json:"status"`
	Error         string   `json:"error"`
	URL           string   `json:"url"`
	OtherURL      string   `json:"other_url"`
	Phrase        string   `json:"phrase"`
	EnglishPhrase string   `json:"english_phrase"`
}

func readToolCase(t *testing.T, id string) toolCase {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "desktopbot", "cases", id+".json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var c toolCase
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	if c.ID != id {
		t.Fatalf("case id = %s, file %s", c.ID, id)
	}
	return c
}

func runMissingBinary(t *testing.T, c toolCase) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash is required to run the copied start.sh")
	}
	src := filepath.Join("..", "..", filepath.FromSlash(c.Script))
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "start.sh")
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	if err := os.WriteFile(dst, body, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, dst)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		code = exitErr.ExitCode()
	}
	text := string(out)
	if code == 0 {
		t.Fatalf("missing binary exited 0: %s", text)
	}
	if !strings.Contains(text, c.Expect) || !strings.Contains(text, c.ExpectAlso) || !strings.Contains(text, "maclaw-hub") {
		t.Fatalf("output = %s", text)
	}
	for _, forbid := range c.Forbid {
		if strings.Contains(text, forbid) {
			t.Fatalf("output contains %q: %s", forbid, text)
		}
	}
}

func runConnectorDown(t *testing.T, c toolCase) {
	t.Helper()
	gate := desktopbot.NewConnectorGate()
	gate.Observe(c.Connector, c.Status, c.Error, c.URL)
	gate.Observe("web_search", c.Status, c.Error, c.URL)
	if desktopbot.ConnectorObservable("web_search") || desktopbot.ConnectorObservable("desktop") {
		t.Fatal("public web or desktop was treated as a connector")
	}
	first := gate.Decide(desktopbot.BypassCall{Action: "navigate", URL: c.URL})
	if first.Allowed || first.Level != desktopbot.LevelBrowser || first.Ask.Kind != "ask" || first.Ask.Text == "" {
		t.Fatalf("first navigate: %+v", first)
	}
	if !strings.Contains(first.Sentence, c.Connector) || !strings.Contains(first.Ask.Text, c.Connector) || !strings.Contains(first.Ask.Text, "401") {
		t.Fatalf("ask did not name the connector: %+v", first)
	}
	if strings.Contains(first.Sentence, "sk-") || strings.Contains(first.Ask.Text, "sk-") || strings.Contains(first.Ask.Text, "Bearer ") {
		t.Fatalf("ask leaked a token: %+v", first)
	}
	for _, action := range []string{"probe", "script", "computer", "click", "type"} {
		again := gate.Decide(desktopbot.BypassCall{Action: action, URL: c.URL})
		if again.Allowed || again.Ask.Text != "" {
			t.Fatalf("%s ran or asked again: %+v", action, again)
		}
	}
	empty := gate.Decide(desktopbot.BypassCall{Action: "probe"})
	if empty.Allowed {
		t.Fatal("a url-less probe ran while the failure was open")
	}
	if gate.Decide(desktopbot.BypassCall{Action: "navigate", URL: c.OtherURL}).Allowed == false {
		t.Fatal("a different host was blocked")
	}
	phraseOnly := gate.Decide(desktopbot.BypassCall{Action: "navigate", URL: c.URL, UserSaidUseBrowser: desktopbot.UserSaidUseBrowser(c.Phrase)})
	if phraseOnly.Allowed {
		t.Fatal("the phrase alone released the browser")
	}
	flagOnly := gate.Decide(desktopbot.BypassCall{Action: "navigate", URL: c.URL, UserAuthorizedBypass: true})
	if flagOnly.Allowed {
		t.Fatal("the flag alone released the browser")
	}
	if !desktopbot.UserSaidUseBrowser(c.EnglishPhrase) {
		t.Fatal("english phrase was not recognized")
	}
	released := gate.Decide(desktopbot.BypassCall{
		Action:               "navigate",
		URL:                  c.URL,
		UserAuthorizedBypass: true,
		UserSaidUseBrowser:   true,
	})
	if !released.Allowed || released.Level != desktopbot.LevelBrowser {
		t.Fatalf("level 4 was not released: %+v", released)
	}
	pixel := gate.Decide(desktopbot.BypassCall{
		Action:               "computer",
		URL:                  c.URL,
		UserAuthorizedBypass: true,
		UserSaidUseBrowser:   true,
	})
	if pixel.Allowed || pixel.Level != desktopbot.LevelPixel || !strings.Contains(pixel.Sentence, "computer") {
		t.Fatalf("level 5 was released: %+v", pixel)
	}
	if desktopbot.BrowserLevel("web_search") != 0 {
		t.Fatal("web_search was treated as the logged-in browser")
	}
}

func runSpill(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	short, err := desktopbot.SpillToolResult(root, "sess:1", 1, "short")
	if err != nil || short != "short" {
		t.Fatalf("short = %q err=%v", short, err)
	}
	body := strings.Repeat("甲", desktopbot.ToolResultSpillRunes+20)
	body = "token=sk-live-should-not-show " + body
	shown, err := desktopbot.SpillToolResult(root, "sess:1", 2, body)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(shown, "\n")
	if len(lines) != 2 || lines[0] != "/home/desktop/tasks/sess_1/out/2.txt" {
		t.Fatalf("shown = %q", shown)
	}
	if strings.Contains(lines[1], "sk-") || strings.Contains(shown, body) || len([]rune(lines[1])) > 120 {
		t.Fatalf("line leaked the body: %q", lines[1])
	}
	written, err := os.ReadFile(filepath.Join(root, "tasks", "sess_1", "out", "2.txt"))
	if err != nil || len(written) == 0 {
		t.Fatalf("file err=%v len=%d", err, len(written))
	}
	if _, err := desktopbot.SpillToolResult("", "sess:1", 3, body); err == nil {
		t.Fatal("an empty root dropped the body")
	}
}
