package main

import (
	"sort"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

func newLoopCycleTestCallbacks() *tuiLoopCycleCallbacks {
	reg := agent.NewCoreToolRegistry()
	agent.RegisterCoreTools(reg, agent.CoreToolDeps{})
	return &tuiLoopCycleCallbacks{
		parent: &tuiLoopCommandCallbacks{
			app: &TUIApp{toolRegistry: reg},
		},
	}
}

func TestLoopCycleBuildToolsReturnsHistoricalFiveNames(t *testing.T) {
	cb := newLoopCycleTestCallbacks()
	defs := cb.BuildTools("make tests pass")
	var got []string
	for _, def := range defs {
		name := tooldef.Name(def)
		if name == "" {
			t.Fatalf("def missing function.name: %#v", def)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), loopCycleToolNames...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("BuildTools names = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildTools names = %v, want %v", got, want)
		}
	}
}

func TestLoopCycleBuildToolsNilSafe(t *testing.T) {
	var nilCb *tuiLoopCycleCallbacks
	if defs := nilCb.BuildTools("x"); defs != nil {
		t.Fatalf("nil callbacks BuildTools = %v, want nil", defs)
	}
	if defs := (&tuiLoopCycleCallbacks{}).BuildTools("x"); defs != nil {
		t.Fatalf("nil-parent BuildTools = %v, want nil", defs)
	}
}

// TestLoopCycleEditFileUsesRegistrySchema is the regression pin for the
// deliberate edit_file schema fix: the old hardcoded definition advertised
// old_content/new_content while ToolEditFile requires old_string/new_string,
// so every /loop edit_file call always failed. A revert to the defective
// shape must fail this test.
func TestLoopCycleEditFileUsesRegistrySchema(t *testing.T) {
	cb := newLoopCycleTestCallbacks()
	defs := cb.BuildTools("make tests pass")
	var editDef map[string]interface{}
	for _, def := range defs {
		if tooldef.Name(def) == "edit_file" {
			editDef = def
			break
		}
	}
	if editDef == nil {
		t.Fatal("edit_file missing from loop-cycle surface")
	}
	params, _ := editDef["function"].(map[string]interface{})["parameters"].(map[string]interface{})
	if params == nil {
		t.Fatalf("edit_file def has no parameters object: %#v", editDef)
	}
	properties, _ := params["properties"].(map[string]interface{})
	for _, defective := range []string{"old_content", "new_content"} {
		if _, present := properties[defective]; present {
			t.Fatalf("edit_file schema regressed to defective field %q (ToolEditFile would reject every call)", defective)
		}
	}
	required, _ := params["required"].([]string)
	if !stringSliceContains(required, "old_string") || !stringSliceContains(required, "new_string") {
		t.Fatalf("edit_file required = %v, want old_string+new_string", required)
	}
	for _, needed := range []string{"old_string", "new_string"} {
		if _, present := properties[needed]; !present {
			t.Fatalf("edit_file properties missing %q: %#v", needed, properties)
		}
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
