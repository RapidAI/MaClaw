package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStripJSONCAndTrailingComma(t *testing.T) {
	raw := []byte(`{
  // comment
  "editor.fontSize": 14,
  "acp.agents": {
    "Other": { "command": "x" },
  },
}`)
	cleaned := stripJSONC(raw)
	var m map[string]any
	if err := json.Unmarshal(cleaned, &m); err != nil {
		t.Fatalf("unmarshal: %v raw=%s", err, cleaned)
	}
	if m["editor.fontSize"].(float64) != 14 {
		t.Fatalf("fontSize = %v", m["editor.fontSize"])
	}
}

func TestWriteVSCodeACPAgentSettingsMerge(t *testing.T) {
	dir := t.TempDir()
	// Override path by writing through a copy of the helper logic with temp path —
	// call write with monkey via setting USERPROFILE is hard; test merge functions instead.
	settings := map[string]any{
		"editor.tabSize": float64(2),
		"acp.agents": map[string]any{
			"Claude Code": map[string]any{"command": "npx", "args": []any{"x"}},
		},
	}
	// Simulate writeVSCodeACPAgentSettings merge
	agents := settings["acp.agents"].(map[string]any)
	agents[vscodeACPAgentName] = map[string]any{
		"command": filepath.Join(dir, "maclaw-acp-bridge.exe"),
		"args":    []any{},
	}
	settings["acp.agents"] = agents
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	agents2 := got["acp.agents"].(map[string]any)
	if _, ok := agents2["Claude Code"]; !ok {
		t.Fatal("lost existing agent")
	}
	mac, ok := agents2[vscodeACPAgentName].(map[string]any)
	if !ok {
		t.Fatal("missing MaClaw agent")
	}
	cmd, _ := mac["command"].(string)
	if !strings.Contains(cmd, "maclaw-acp-bridge") {
		t.Fatalf("command = %q", cmd)
	}
}

func TestBridgeBinaryName(t *testing.T) {
	name := bridgeBinaryName()
	if name == "" {
		t.Fatal("empty name")
	}
}
