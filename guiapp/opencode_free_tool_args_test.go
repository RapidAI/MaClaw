package guiapp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestConfineOpenCodeFreeToolArgsMovesPromptDirectory(t *testing.T) {
	workspace := `C:\Users\ma139\.maclaw\data\tasks\beijing`
	got := confineOpenCodeFreeToolArgs("write_file", `{"path":"D:\\oc-empty\\beijing-weather.pdf","content":"%PDF"}`, workspace)
	if !strings.Contains(got, `beijing-weather.pdf`) || strings.Contains(strings.ToLower(got), `oc-empty`) {
		t.Fatalf("write path = %s", got)
	}
	if !strings.Contains(got, `tasks\\beijing`) && !strings.Contains(got, `tasks/beijing`) {
		t.Fatalf("write path left the task workspace: %s", got)
	}

	script := confineOpenCodeFreeToolArgs("write_file", `{"path":"D:/oc-empty/sub/gen_pdf.py","content":"print(1)"}`, workspace)
	if !strings.Contains(script, `gen_pdf.py`) || strings.Contains(strings.ToLower(script), `oc-empty`) {
		t.Fatalf("nested write path = %s", script)
	}

	relative := `{"path":"beijing-weather.pdf","content":"x"}`
	if confineOpenCodeFreeToolArgs("write_file", relative, workspace) != relative {
		t.Fatal("relative path should stay relative")
	}
}

func TestConfineOpenCodeFreeToolArgsRewritesShell(t *testing.T) {
	workspace := `C:\task`
	got := confineOpenCodeFreeToolArgs("bash", `{"command":"python D:\\oc-empty\\gen_pdf.py","working_dir":"D:\\oc-empty"}`, workspace)
	if strings.Contains(strings.ToLower(got), `oc-empty`) {
		t.Fatalf("shell still points at the prompt directory: %s", got)
	}
	if !strings.Contains(got, `C:/task/gen_pdf.py`) {
		t.Fatalf("command = %s", got)
	}
	if strings.Contains(got, `working_dir`) {
		t.Fatalf("prompt working_dir should be dropped: %s", got)
	}
}

func TestConfineOpenCodeFreeToolArgsLeavesInstalledPrograms(t *testing.T) {
	workspace := `C:\task`
	python := `C:\Users\ma139\AppData\Local\Programs\Python\Python312\python.exe`
	body, err := json.Marshal(map[string]string{"command": python + ` D:\oc-empty\gen_pdf.py`})
	if err != nil {
		t.Fatal(err)
	}
	got := confineOpenCodeFreeToolArgs("bash", string(body), workspace)
	if !strings.Contains(got, `Python312\\python.exe`) && !strings.Contains(got, `Python312/python.exe`) {
		t.Fatalf("python path was rewritten: %s", got)
	}
	if strings.Contains(strings.ToLower(got), `oc-empty`) || !strings.Contains(got, `C:/task/gen_pdf.py`) {
		t.Fatalf("script path = %s", got)
	}
	other := `{"path":"C:\\other\\sub\\gen_pdf.py","content":"print(1)"}`
	if confineOpenCodeFreeToolArgs("write_file", other, workspace) != other {
		t.Fatal("paths outside the prompt directory should stay put")
	}
}

func TestConfineOpenCodeFreeToolArgsRewritesScriptOutput(t *testing.T) {
	workspace := `C:\Users\ma139\.maclaw\data\tasks\beijing`
	python := `C:\Users\ma139\AppData\Local\Programs\Python\Python312\python.exe`
	body, err := json.Marshal(map[string]string{
		"path": "gen_pdf.py",
		"content": "open(\"D:\\oc-empty\\beijing-weather.pdf\")\n" +
			"keep = \"D:\\oc-empty2\\nope\"\n" +
			"exe = r\"" + python + "\"\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := confineOpenCodeFreeToolArgs("write_file", string(body), workspace)
	const want = `C:/Users/ma139/.maclaw/data/tasks/beijing/beijing-weather.pdf`
	if !strings.Contains(got, want) || strings.Contains(got, `\beijing`) || strings.Contains(got, `\\t`) {
		t.Fatalf("script output path = %s", got)
	}
	if !strings.Contains(got, `oc-empty2`) || !strings.Contains(got, `Python312\\python.exe`) {
		t.Fatalf("unrelated paths were rewritten: %s", got)
	}
	if !strings.Contains(got, `"path":"gen_pdf.py"`) {
		t.Fatalf("relative script path changed: %s", got)
	}
}

func TestConfineOpenCodeFreeToolArgsRewritesEscapedPythonString(t *testing.T) {
	workspace := `C:\Users\ma139\.maclaw\data\tasks\beijing`
	body, err := json.Marshal(map[string]string{
		"path":    "gen_pdf.py",
		"content": "open(\"D:\\\\oc-empty\\\\beijing-weather.pdf\")\nkeep(\"D:\\\\oc-empty2\\\\nope\")\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := confineOpenCodeFreeToolArgs("write_file", string(body), workspace)
	const want = `C:/Users/ma139/.maclaw/data/tasks/beijing/beijing-weather.pdf`
	if !strings.Contains(got, want) || strings.Contains(got, `\beijing`) || !strings.Contains(got, `oc-empty2`) {
		t.Fatalf("escaped python path = %s", got)
	}
}

func TestConfineOpenCodeFreeToolArgsLeavesParentEscape(t *testing.T) {
	workspace := `C:\Users\ma139\.maclaw\data\tasks\beijing`
	body, err := json.Marshal(map[string]string{
		"path":    "gen_pdf.py",
		"content": "open(\"D:\\\\oc-empty\\\\..\\\\secret.pdf\")\nopen(\"D:\\\\oc-empty\\\\keep.pdf\")\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := confineOpenCodeFreeToolArgs("write_file", string(body), workspace)
	if strings.Contains(got, `tasks/beijing/../secret`) || strings.Contains(got, `tasks/beijing/..`) {
		t.Fatalf("parent segment was moved into the workspace: %s", got)
	}
	if !strings.Contains(got, `oc-empty`) || !strings.Contains(got, `C:/Users/ma139/.maclaw/data/tasks/beijing/keep.pdf`) {
		t.Fatalf("escaped parent path = %s", got)
	}
}

func TestConfineOpenCodeFreeToolArgsCollapsesInsideParent(t *testing.T) {
	workspace := `C:\Users\ma139\.maclaw\data\tasks\beijing`
	body, err := json.Marshal(map[string]string{
		"path":    "gen_pdf.py",
		"content": "open(\"D:\\\\oc-empty\\\\sub\\\\..\\\\keep.pdf\")\nopen(\"D:/oc-empty\\\\..\\\\secret.pdf\")\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := confineOpenCodeFreeToolArgs("write_file", string(body), workspace)
	if !strings.Contains(got, `C:/Users/ma139/.maclaw/data/tasks/beijing/keep.pdf`) || strings.Contains(got, `sub/../`) {
		t.Fatalf("inside parent = %s", got)
	}
	if strings.Contains(got, `tasks/beijing/../secret`) || strings.Contains(got, `tasks/beijing/..`) || !strings.Contains(got, `oc-empty`) {
		t.Fatalf("mixed parent escape = %s", got)
	}
}

func TestConfineOpenCodeFreeToolArgsRenamesWorkdir(t *testing.T) {
	workspace := `C:\task`
	got := confineOpenCodeFreeToolArgs("bash", `{"command":"python gen_pdf.py","workdir":"D:\\oc-empty\\sub"}`, workspace)
	if strings.Contains(got, `"workdir"`) || strings.Contains(strings.ToLower(got), "oc-empty") {
		t.Fatalf("workdir = %s", got)
	}
	if !strings.Contains(got, `C:\\task\\sub`) {
		t.Fatalf("working_dir = %s", got)
	}
}

func TestOpenCodeFreeHostToolSkipsSemanticGrant(t *testing.T) {
	surface := &semanticCallSurface{}
	epoch := surface.beginEpoch()
	cb := &sharedAgentLoopCallbacks{
		llmCfg:          corelib.MaclawLLMConfig{URL: "https://opencode.ai/zen/v1", Model: "deepseek-v4.1-flash-free"},
		semanticSurface: surface,
	}
	got := cb.ExecuteToolCallWithContext("bash", `{"command":"echo hi"}`, "call-1", agent.ToolCallExecutionContext{SurfaceEpoch: epoch}).Result
	if strings.Contains(got, "selection_not_authorized") || strings.Contains(got, "stale_surface") {
		t.Fatalf("free host tool was rejected before it could run: %s", got)
	}
	if !strings.Contains(got, "handler unavailable") {
		t.Fatalf("result = %s", got)
	}
	stale := cb.ExecuteToolCallWithContext("bash", `{"command":"echo hi"}`, "call-2", agent.ToolCallExecutionContext{SurfaceEpoch: "old"}).Result
	if !strings.Contains(stale, "stale_surface") {
		t.Fatalf("superseded response ran bash: %s", stale)
	}
}
