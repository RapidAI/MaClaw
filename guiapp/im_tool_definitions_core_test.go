package guiapp

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

func TestGUIOverlappingToolsIncludeCoreSchemas(t *testing.T) {
	h := &IMMessageHandler{app: &App{}}
	defs := h.buildToolDefinitions()
	registry := NewToolRegistry()
	registerBuiltinTools(registry, h)

	names := []string{
		"bash", "ssh", "ask_user", "task", "read_file", "read_tool_result",
		"write_file", "edit_file", "list_directory", "Glob", "send_file",
		"send_to_im", "open", "screenshot", "manage_skill", "record_audio",
		"tts", "asr", "memory", "manage_schedule", "im_message",
		"web_search", "web_fetch", "list_mcp_tools", "delegate_task",
	}
	registryOnly := []string{"download_file", "import_mcp_servers", "edit_lines", "tts_render", "office", "generate_pdf"}
	for _, name := range names {
		core, ok := agent.CoreToolJSONSchema(name)
		if !ok {
			t.Fatalf("CoreToolJSONSchema(%s) missing", name)
		}
		want, _ := core["properties"].(map[string]interface{})
		defProps := toolDefinitionProperties(t, defs, name)
		for key := range want {
			if _, exists := defProps[key]; !exists {
				t.Errorf("buildToolDefinitions %s missing core property %s", name, key)
			}
		}
		reg, found := registry.Get(name)
		if !found || reg == nil {
			t.Fatalf("registry missing %s", name)
		}
		regProps := rawToolSchemaProperties(reg.InputSchema)
		for key := range want {
			if _, exists := regProps[key]; !exists {
				t.Errorf("registry %s missing core property %s", name, key)
			}
		}
	}
	for _, name := range registryOnly {
		core, ok := agent.CoreToolJSONSchema(name)
		if !ok {
			t.Fatalf("CoreToolJSONSchema(%s) missing", name)
		}
		want, _ := core["properties"].(map[string]interface{})
		reg, found := registry.Get(name)
		if !found || reg == nil {
			t.Fatalf("registry missing %s", name)
		}
		regProps := rawToolSchemaProperties(reg.InputSchema)
		for key := range want {
			if _, exists := regProps[key]; !exists {
				t.Errorf("registry %s missing core property %s", name, key)
			}
		}
	}
}

func rawToolSchemaProperties(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{}
	}
	if raw, ok := schema["properties"].(map[string]interface{}); ok {
		return raw
	}
	return schema
}
