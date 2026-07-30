package skill

import (
	"strings"
	"testing"
)

func TestNormalizeStepActionNameCanonicalizesSpelling(t *testing.T) {
	for _, raw := range []string{"craft-tool", "Craft Tool", " CRAFT_TOOL "} {
		if got := NormalizeStepActionName(raw); got != "craft_tool" {
			t.Fatalf("NormalizeStepActionName(%q) = %q", raw, got)
		}
	}
}
func TestCheckStepActionSupport_TUICraftToolRequiresGUI(t *testing.T) {
	support := CheckStepActionSupport(RunnerBackendTUI, "craft_tool")
	if support.Supported {
		t.Fatal("craft_tool should not be supported by TUI")
	}
	if !strings.Contains(support.Message(), "requires GUI skill runner") {
		t.Fatalf("message = %q", support.Message())
	}
	if !strings.Contains(support.ActionHint, "open_gui") {
		t.Fatalf("ActionHint = %q", support.ActionHint)
	}
}

func TestCheckStepActionSupport_GUIRunsCraftTool(t *testing.T) {
	support := CheckStepActionSupport(RunnerBackendGUI, "craft-tool")
	if !support.Supported {
		t.Fatalf("craft_tool should be supported by GUI: %#v", support)
	}
}

func TestCheckStepActionSupport_GUIMCPActionSupported(t *testing.T) {
	support := CheckStepActionSupport(RunnerBackendGUI, "call_mcp_tool")
	if !support.Supported {
		t.Fatalf("call_mcp_tool should be supported by GUI: %#v", support)
	}
}

func TestCheckStepActionSupport_GUIRemoteCodingActionsSupported(t *testing.T) {
	for _, action := range []string{"ssh_bash", "ssh_list_dir", "ssh_read_file", "todo_write"} {
		if support := CheckStepActionSupport(RunnerBackendGUI, action); !support.Supported {
			t.Fatalf("%s should be supported by GUI: %#v", action, support)
		}
	}
}

func TestCheckStepActionSupport_GUIExternalSessionActionsDisabled(t *testing.T) {
	for _, action := range []string{"create_session", "send_input", "send_and_observe", "control_session"} {
		support := CheckStepActionSupport(RunnerBackendGUI, action)
		if support.Supported {
			t.Fatalf("%s should not be supported by GUI", action)
		}
		if !strings.Contains(support.Message(), "external coding sessions") {
			t.Fatalf("message = %q", support.Message())
		}
	}
}

func TestEnsureStepActionSupported_ReturnsClassifiableError(t *testing.T) {
	err := EnsureStepActionSupported(RunnerBackendTUI, "craft_tool")
	if err == nil {
		t.Fatal("expected unsupported error")
	}
	if !strings.Contains(err.Error(), "unsupported_step_action") {
		t.Fatalf("err = %q", err.Error())
	}
	classified := ClassifyStepError(0, "", err.Error(), "")
	if classified.Class != ErrUnsupportedAction {
		t.Fatalf("Class = %s", classified.Class)
	}
}

func TestSupportedStepActionsReturnsCopy(t *testing.T) {
	actions := SupportedStepActions(RunnerBackendTUI)
	actions[0] = "craft_tool"
	if got := SupportedStepActions(RunnerBackendTUI)[0]; got != "bash" {
		t.Fatalf("SupportedStepActions returned shared slice, got %q", got)
	}
}
