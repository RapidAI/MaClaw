
# MaClaw Plugin System Documentation

## Introduction

The MaClaw plugin system is a unified extension framework that allows users to extend MaClaw's functionality through multiple methods. The plugin system unifies MCP Server, NLSkill, and local script extension mechanisms into a single `Plugin` interface, with three-layer discovery (user-level, project-level, package-level) and unified `PluginRegistry` for lifecycle management.

### Core Features

- **Unified Interface**: All plugin types (MCP, LocalMCP, NLSkill, Script, Native) share the same lifecycle interface
- **Three-Layer Discovery**: Supports user-level (`~/.maclaw/plugins/`), project-level (`.maclaw/plugins/`), and package-level (Go entry points) plugins
- **Declarative Configuration**: Plugins are declared via `plugin.yaml` files
- **Hot Management**: Supports runtime enable/disable without restarting MaClaw
- **Health Checks**: Background periodic health status checks
- **Security**: Project-level plugins require user confirmation on first load

## Plugin Types

MaClaw supports the following plugin types:

| Type | Description | Use Case |
|------|-------------|----------|
| `mcp` | Remote MCP Server (HTTP/WS connection) | Existing MCP servers |
| `local_mcp` | Local stdio MCP Server (command-started) | Local Node.js/Python MCP services |
| `nlskill` | Natural Language Skill (multi-step automation) | Complex automation workflows |
| `script` | Script tool (shell/Python/Node.js) | Simple CLI tool wrappers |
| `native` | Native Go plugin (compile-time registered) | High-performance or deep integration |

## Plugin Structure

Each plugin is a directory containing:

```
plugin-name/
├── plugin.yaml      # Plugin configuration (required)
└── run.sh           # Example script (script type only)
```

### plugin.yaml Format

```yaml
name: my-plugin
version: "1.0.0"
description: "A custom plugin"
type: script
author: "Your Name"
tags: ["tool", "automation"]

script:
  command: "bash"
  args: ["run.sh"]
  timeout: 30
  tool_name: "my_tool"
  description: "My custom tool"
  input_schema:
    query:
      type: string
      description: "Input query"
  required: ["query"]
```

## Usage

### 1. Create a Plugin

```bash
# Create a script plugin (project-level)
maclaw-tui plugin create --type script --scope project my-tool

# Create a local_mcp plugin (user-level)
maclaw-tui plugin create --type local_mcp --scope user my-mcp
```

### 2. Edit Plugin Configuration

Edit the generated `plugin.yaml` to configure plugin parameters.

### 3. Enable Plugin

```bash
maclaw-tui plugin enable my-tool
```

### 4. Disable Plugin

```bash
maclaw-tui plugin disable my-tool
```

### 5. View Plugin List

```bash
maclaw-tui plugin list
maclaw-tui plugin list --json
maclaw-tui plugin info my-tool
```

## Plugin Development

### Script Plugin Development

Script plugins are the simplest type. Just write an executable script.

**Input:** JSON object via stdin

**Output:** stdout (plain text)

**Example: `run.sh`**

```bash
#!/bin/bash
INPUT=$(cat)
QUERY=$(echo "$INPUT" | jq -r '.query // "default"')
echo "Processing: $QUERY"
curl -s "https://api.example.com/search?q=$QUERY"
```

### LocalMCP Plugin Development

LocalMCP plugins implement the MCP protocol (JSON-RPC over stdio).

**Basic flow:**
1. Receive `initialize` request on startup
2. Return capabilities
3. Send `notifications/initialized` notification
4. Receive `tools/list` request, return tool list
5. Receive `tools/call` request, execute tool and return result

### Native Plugin Development

Native plugins are Go plugins implementing `plugin.EntryPointProvider`.

```go
type MyPlugin struct{}

func (p *MyPlugin) Manifest() plugin.PluginManifest {
    return plugin.PluginManifest{
        Name:        "my-native-tool",
        Version:     "1.0.0",
        Description: "A native Go plugin",
        Type:        plugin.PluginTypeNative,
    }
}
```

## Configuration Reference

### plugin.yaml Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Plugin name (alphanumeric, `-`, `_`) |
| `version` | string | No | Version number |
| `description` | string | No | Plugin description |
| `type` | string | Yes | Plugin type: `mcp`, `local_mcp`, `nlskill`, `script`, `native` |
| `author` | string | No | Author |
| `tags` | []string | No | Tag list |
| `settings` | map | No | Custom settings |

## Environment Variables

Plugin config supports `${ENV_VAR}` syntax:

```yaml
mcp:
  auth_secret: "${API_TOKEN}"
```

## Security

- Never store secrets in `plugin.yaml`
- Use `${ENV_VAR}` to read from environment variables
- Project-level plugins require user confirmation

## Best Practices

1. Use lowercase with hyphens for plugin names
2. Use semantic versioning
3. Handle errors gracefully
4. Set reasonable timeouts
5. Document plugin functionality


## Usage Examples

### Create a Script Plugin

```bash
maclaw-tui plugin create --type script --scope project github-search
```

This creates:
- `.maclaw/plugins/github-search/plugin.yaml`
- `.maclaw/plugins/github-search/run.sh`

### Create a LocalMCP Plugin

```bash
maclaw-tui plugin create --type local_mcp --scope user file-tools
```

Edit `plugin.yaml`:

```yaml
local_mcp:
  command: "npx"
  args: ["-y", "@modelcontextprotocol/server-filesystem", "/path/to/allowed/dir"]
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `maclaw-tui plugin list` | List all plugins |
| `maclaw-tui plugin list --json` | JSON output |
| `maclaw-tui plugin info <name>` | Show plugin details |
| `maclaw-tui plugin enable <name>` | Enable a plugin |
| `maclaw-tui plugin disable <name>` | Disable a plugin |
| `maclaw-tui plugin create` | Create new plugin |

## Troubleshooting

### Plugin Not Loading

1. Check `plugin.yaml` exists and is valid YAML
2. Run `maclaw-tui plugin list` to see status
3. Check MaClaw logs for errors

### Script Execution Failed

1. Ensure script has execute permission
2. Check shebang line (`#!/bin/bash` or `#!/usr/bin/env python3`)
3. Verify JSON input format

## Best Practices

1. Use lowercase with hyphens for plugin names
2. Use semantic versioning (1.0.0)
3. Set reasonable timeouts for long operations
4. Document plugin functionality in description
5. Use environment variables for secrets

## Example Plugins

### GitHub Search (Script)

```yaml
name: github-search
type: script
script:
  command: "python"
  args: ["search.py"]
  input_schema:
    query:
      type: string
```

```python
# search.py
import json, sys, requests
args = json.loads(sys.stdin.read())
query = args.get("query", "")
url = "https://api.github.com/search/repositories"
params = {"q": query}
print(requests.get(url, params=params).json())
```

## Security

- Never commit secrets to `plugin.yaml`
- Use `${ENV_VAR}` for sensitive data
- Review project-level plugins before trusting

