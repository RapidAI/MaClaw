package commands

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/RapidAI/CodeClaw/corelib/plugin"
)

// DefaultPluginRegistry is the package-level PluginRegistry instance.
// It must be set by the application startup code before CLI commands are used.
var DefaultPluginRegistry *plugin.PluginRegistry

// RunPlugin executes the plugin sub-command.
func RunPlugin(args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui plugin <list|info|enable|disable|create>")
	}
	switch args[0] {
	case "list":
		return pluginList(args[1:])
	case "info":
		return pluginInfo(args[1:])
	case "enable":
		return pluginEnable(args[1:])
	case "disable":
		return pluginDisable(args[1:])
	case "create":
		return pluginCreate(args[1:])
	default:
		return NewUsageError("unknown plugin action: %s", args[0])
	}
}

func pluginList(args []string) error {
	fs := flag.NewFlagSet("plugin list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	plugins := DefaultPluginRegistry.List()

	if *jsonOut {
		return PrintJSON(plugins)
	}

	if len(plugins) == 0 {
		fmt.Println("No plugins registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tSTATUS\tTOOLS")
	for _, p := range plugins {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", p.Name, p.Type, p.Status, p.ToolCount)
	}
	return w.Flush()
}

func pluginInfo(args []string) error {
	fs := flag.NewFlagSet("plugin info", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin info <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	info, ok := DefaultPluginRegistry.Get(name)
	if !ok {
		return fmt.Errorf("plugin %q not found", name)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", info.Name)
	fmt.Fprintf(w, "Version:\t%s\n", info.Version)
	fmt.Fprintf(w, "Description:\t%s\n", info.Description)
	fmt.Fprintf(w, "Type:\t%s\n", info.Type)
	fmt.Fprintf(w, "Scope:\t%s\n", info.Scope)
	fmt.Fprintf(w, "Status:\t%s\n", info.Status)
	fmt.Fprintf(w, "Tools:\t%d\n", info.ToolCount)
	fmt.Fprintf(w, "Hooks:\t%d\n", info.HookCount)
	fmt.Fprintf(w, "Health:\t%s\n", info.Health.Status)
	if info.Error != "" {
		fmt.Fprintf(w, "Error:\t%s\n", info.Error)
	}
	return w.Flush()
}

func pluginEnable(args []string) error {
	fs := flag.NewFlagSet("plugin enable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin enable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	if err := DefaultPluginRegistry.Enable(context.Background(), name); err != nil {
		return err
	}
	fmt.Printf("plugin %q enabled\n", name)
	return nil
}

func pluginDisable(args []string) error {
	fs := flag.NewFlagSet("plugin disable", flag.ExitOnError)
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin disable <name>")
	}
	name := fs.Arg(0)

	if DefaultPluginRegistry == nil {
		return fmt.Errorf("plugin registry not initialized")
	}

	if err := DefaultPluginRegistry.Disable(name); err != nil {
		return err
	}
	fmt.Printf("plugin %q disabled\n", name)
	return nil
}

func pluginCreate(args []string) error {
	fs := flag.NewFlagSet("plugin create", flag.ExitOnError)
	pluginType := fs.String("type", "script", "Plugin type: script, local_mcp, mcp, nlskill")
	scope := fs.String("scope", "project", "Scope: project or user")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui plugin create [--type script|local_mcp|mcp|nlskill] [--scope project|user] <name>")
	}

	// Validate plugin type.
	validTypes := map[string]bool{"script": true, "local_mcp": true, "mcp": true, "nlskill": true}
	if !validTypes[*pluginType] {
		return NewUsageError("type must be one of: script, local_mcp, mcp, nlskill")
	}

	name := fs.Arg(0)

	// Validate plugin name: must be non-empty, alphanumeric + hyphens/underscores only.
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return NewUsageError("plugin name must contain only alphanumeric characters, hyphens, and underscores")
		}
	}

	// Determine target directory.
	var baseDir string
	switch *scope {
	case "project":
		baseDir = filepath.Join(".maclaw", "plugins", name)
	case "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("cannot determine home directory: %w", err)
		}
		baseDir = filepath.Join(home, ".maclaw", "plugins", name)
	default:
		return NewUsageError("scope must be 'project' or 'user'")
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}

	yamlPath := filepath.Join(baseDir, "plugin.yaml")

	// Check if plugin.yaml already exists to avoid overwriting.
	if _, err := os.Stat(yamlPath); err == nil {
		return fmt.Errorf("plugin %q already exists at %s", name, yamlPath)
	}

	yamlContent := generatePluginYAML(name, plugin.PluginType(*pluginType))
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o644); err != nil {
		return fmt.Errorf("write plugin.yaml: %w", err)
	}

	// For script type, also generate a sample script.
	if *pluginType == "script" {
		scriptPath := filepath.Join(baseDir, "run.sh")
		scriptContent := `#!/bin/bash
# Sample script plugin for maclaw.
# Input: JSON on stdin (tool arguments)
# Output: plain text on stdout (tool result)

# Read JSON input
INPUT=$(cat)

echo "Hello from plugin '` + name + `'!"
echo "Received input: $INPUT"
`
		if err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755); err != nil {
			return fmt.Errorf("write sample script: %w", err)
		}
		fmt.Printf("Created script plugin at %s\n", baseDir)
		fmt.Printf("  plugin.yaml: %s\n", yamlPath)
		fmt.Printf("  run.sh:      %s\n", scriptPath)
	} else {
		fmt.Printf("Created %s plugin at %s\n", *pluginType, baseDir)
		fmt.Printf("  plugin.yaml: %s\n", yamlPath)
	}

	fmt.Println("\nEdit plugin.yaml to customize, then restart maclaw to load the plugin.")
	return nil
}

func generatePluginYAML(name string, ptype plugin.PluginType) string {
	switch ptype {
	case plugin.PluginTypeScript:
		return fmt.Sprintf(`name: %s
version: "1.0.0"
description: "A custom script tool"
type: script
author: ""
tags: []

script:
  command: "bash"
  args: ["run.sh"]
  timeout: 30
  # tool_name: %s
  # description: "Override tool description here"
  # input_schema:
  #   query:
  #     type: string
  #     description: "Input query"
  # required: ["query"]

# settings:
#   key: value
`, name, name)

	case plugin.PluginTypeLocalMCP:
		return fmt.Sprintf(`name: %s
version: "1.0.0"
description: "A local MCP server plugin"
type: local_mcp
author: ""
tags: []

local_mcp:
  command: "npx"
  args: ["-y", "@example/mcp-server"]
  # env:
  #   API_KEY: "${MY_API_KEY}"
`, name)

	case plugin.PluginTypeMCP:
		return fmt.Sprintf(`name: %s
version: "1.0.0"
description: "A remote MCP server plugin"
type: mcp
author: ""
tags: []

mcp:
  endpoint_url: "https://example.com/mcp"
  auth_type: "none"
  # auth_secret: "${API_KEY}"
`, name)

	case plugin.PluginTypeNLSkill:
		return fmt.Sprintf(`name: %s
version: "1.0.0"
description: "A natural language skill"
type: nlskill
author: ""
tags: []

nlskill:
  triggers: ["%s"]
  steps:
    - action: bash
      params:
        command: "echo 'Hello from %s'"
`, name, name, name)

	default:
		return fmt.Sprintf(`name: %s
version: "1.0.0"
description: ""
type: %s
author: ""
tags: []
`, name, ptype)
	}
}
