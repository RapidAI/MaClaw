package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestGUIRegistersDefiningSharedCapabilities(t *testing.T) {
	r := NewToolRegistry()
	registerBuiltinTools(r, &IMMessageHandler{})
	// GUI folds some core names into office / TUI search helpers. Those
	// families still exist as office or FileRead/ripgrep/Glob on srv.
	folded := map[string]bool{
		"FileRead": true, "ripgrep": true, "Glob": true,
		"read_excel": true, "write_excel": true, "read_pptx": true,
		"read_document":    true,
		"knowledge_search": true, "knowledge_context_pack": true, "knowledge_export": true,
		"knowledge_import_package": true, "knowledge_import_share": true,
		"knowledge_import_directory": true, "knowledge_import_files": true,
		"knowledge_save_text": true, "knowledge_save_url": true,
	}
	for _, name := range agent.SharedCoreCapabilityNames() {
		if folded[name] {
			continue
		}
		if _, ok := r.Get(name); !ok {
			t.Errorf("GUI builtin catalog missing shared core capability %s", name)
		}
	}
	for _, name := range []string{"delegate_task", "office", "list_mcp_tools", "asr", "tts"} {
		if _, ok := r.Get(name); !ok {
			t.Errorf("GUI builtin catalog missing extra shared capability %s", name)
		}
	}
}
