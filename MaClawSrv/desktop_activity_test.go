package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

func TestDesktopActivityPhrasesStayOffTheTypedText(t *testing.T) {
	const runID = "run-activity"
	t.Cleanup(func() {
		clearDesktopActivity(runID)
		desktopTurnResults.Delete(runID)
	})

	setDesktopActivity(runID, "正在输入")
	if loadDesktopActivity(runID) != "正在输入" {
		t.Fatalf("stored = %q", loadDesktopActivity(runID))
	}
	setDesktopActivity(runID, "正在输入 secret")
	if loadDesktopActivity(runID) != "正在输入" {
		t.Fatalf("secret overwrote the step: %q", loadDesktopActivity(runID))
	}
	setDesktopActivity("", "正在点击")
	if loadDesktopActivity(runID) != "正在输入" {
		t.Fatal("an empty run id changed the stored step")
	}
	clearDesktopActivity(runID)
	if loadDesktopActivity(runID) != "" {
		t.Fatalf("clear left %q", loadDesktopActivity(runID))
	}

	typed := desktopArgvPhrase([]string{"type", "--delay", "20", "the typed words"})
	if typed != "正在输入" || strings.Contains(typed, "typed") || strings.Contains(typed, "delay") {
		t.Fatalf("type phrase = %q", typed)
	}
	if desktopArgvPhrase([]string{"search", "--name", "writer", "windowactivate", "--sync"}) != "正在切换窗口" {
		t.Fatal("search focus was not a window change")
	}
	if desktopArgvPhrase([]string{"windowactivate", "--sync", "12"}) != "正在切换窗口" {
		t.Fatal("windowactivate was not a window change")
	}
	if desktopScriptPhrase(`type("hello")`) != "正在输入" {
		t.Fatal("a type script was not 正在输入")
	}
	if got := desktopScriptPhrase(`click("button:nth-of-type(1)")`); got != "正在点击" {
		t.Fatalf("a click selector was %q", got)
	}
	if got := desktopScriptPhrase(`await type("#q", "weather")`); got != "正在输入" || strings.Contains(got, "weather") {
		t.Fatalf("await type = %q", got)
	}
	mixed := desktopScriptPhrase(`click(1); type("x")`)
	if mixed != "正在操作网页" || strings.Contains(mixed, "x") {
		t.Fatalf("mixed script = %q", mixed)
	}
	if desktopKeyPhrase("Return") != "正在输入" {
		t.Fatalf("Return = %q", desktopKeyPhrase("Return"))
	}
	if desktopKeyPhrase("ctrl+s") != "正在按键" {
		t.Fatalf("ctrl+s = %q", desktopKeyPhrase("ctrl+s"))
	}
	steps := desktopBrowserStepsPhrase([]browser.StepSpec{{Action: "type", Params: map[string]string{"text": "secret"}}})
	if steps != "正在输入" || strings.Contains(steps, "secret") {
		t.Fatalf("browser steps = %q", steps)
	}

	setDesktopActivity(runID, "正在输入")
	run := &agentservice.Run{ID: runID, Status: agentservice.RunStatusRunning}
	raw, err := json.Marshal(desktopRunAPIBody("", run))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"activity":"正在输入"`) {
		t.Fatalf("running body = %s", raw)
	}
	storeDesktopTurnResult(run, &agentservice.Message{Content: "写好了"}, false, "")
	if loadDesktopActivity(runID) != "" {
		t.Fatal("a finished run kept its step")
	}
	done, err := json.Marshal(desktopRunAPIBody("", run))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(done), `"activity"`) {
		t.Fatalf("finished body still names a step: %s", done)
	}
}
