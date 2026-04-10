package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ScriptPluginAdapter wraps a shell/python script as a Plugin implementation.
// The script is invoked via the configured command with JSON-encoded arguments
// passed on stdin. Stdout is captured as the tool result.
type ScriptPluginAdapter struct {
	manifest PluginManifest
	config   PluginConfig
	tools    []ToolDefinition

	// Parsed from RawTypeConfig["script"]
	command     string            // e.g. "python", "bash", "node"
	scriptArgs  []string          // e.g. ["my_script.py"]
	env         map[string]string // extra env vars
	timeoutSec  int               // per-invocation timeout (default 30)
	toolName    string            // override tool name (default: manifest.Name)
	description string            // override tool description
	inputSchema map[string]interface{}
	required    []string
}

// NewScriptPluginAdapter creates a new ScriptPluginAdapter for the given manifest.
func NewScriptPluginAdapter(manifest PluginManifest) *ScriptPluginAdapter {
	return &ScriptPluginAdapter{
		manifest:   manifest,
		timeoutSec: 30,
	}
}

func (a *ScriptPluginAdapter) Manifest() PluginManifest { return a.manifest }

func (a *ScriptPluginAdapter) Init(cfg PluginConfig) error {
	a.config = cfg

	// Parse script-specific config from RawTypeConfig.
	raw, ok := a.manifest.RawTypeConfig["script"]
	if !ok {
		return fmt.Errorf("script plugin %q: missing 'script' config section", a.manifest.Name)
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("script plugin %q: 'script' config must be a map", a.manifest.Name)
	}

	if cmd, ok := m["command"].(string); ok && cmd != "" {
		a.command = cmd
	} else {
		return fmt.Errorf("script plugin %q: 'script.command' is required", a.manifest.Name)
	}

	if args, ok := m["args"]; ok {
		if argList, ok := args.([]interface{}); ok {
			for _, v := range argList {
				if s, ok := v.(string); ok {
					a.scriptArgs = append(a.scriptArgs, s)
				}
			}
		}
	}

	if envMap, ok := m["env"].(map[string]interface{}); ok {
		a.env = make(map[string]string, len(envMap))
		for k, v := range envMap {
			a.env[k] = fmt.Sprintf("%v", v)
		}
	}

	if t, ok := m["timeout"].(int); ok && t > 0 {
		a.timeoutSec = t
	} else if tf, ok := m["timeout"].(float64); ok && tf > 0 {
		a.timeoutSec = int(tf)
	}

	if tn, ok := m["tool_name"].(string); ok && tn != "" {
		a.toolName = tn
	}
	if desc, ok := m["description"].(string); ok && desc != "" {
		a.description = desc
	}

	if schema, ok := m["input_schema"].(map[string]interface{}); ok {
		a.inputSchema = schema
	}
	if req, ok := m["required"].([]interface{}); ok {
		for _, v := range req {
			if s, ok := v.(string); ok {
				a.required = append(a.required, s)
			}
		}
	}

	return nil
}

func (a *ScriptPluginAdapter) Start(_ context.Context) error {
	name := a.toolName
	if name == "" {
		name = a.manifest.Name
	}
	desc := a.description
	if desc == "" {
		desc = a.manifest.Description
	}
	if desc == "" {
		desc = "Script tool: " + name
	}

	a.tools = []ToolDefinition{
		{
			Name:        name,
			Description: desc,
			InputSchema: a.inputSchema,
			Required:    a.required,
			Tags:        a.manifest.Tags,
			Handler:     a.execute,
		},
	}
	return nil
}

func (a *ScriptPluginAdapter) Stop(_ context.Context) error {
	a.tools = nil
	return nil
}

func (a *ScriptPluginAdapter) Tools() []ToolDefinition {
	return a.tools
}

func (a *ScriptPluginAdapter) Health() HealthStatus {
	return HealthStatus{Status: "healthy"}
}

// execute runs the script with JSON args on stdin and returns stdout.
func (a *ScriptPluginAdapter) execute(args map[string]interface{}) (string, error) {
	timeout := time.Duration(a.timeoutSec) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdArgs := append(a.scriptArgs[:len(a.scriptArgs):len(a.scriptArgs)])
	cmd := exec.CommandContext(ctx, a.command, cmdArgs...)

	// Set working directory to plugin dir if available.
	if a.manifest.Dir != "" {
		cmd.Dir = a.manifest.Dir
	}

	// Set environment: inherit system env + overlay plugin env.
	cmd.Env = os.Environ()
	for k, v := range a.env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	// Pass args as JSON on stdin.
	if args != nil {
		data, err := json.Marshal(args)
		if err != nil {
			return "", fmt.Errorf("marshal args: %w", err)
		}
		cmd.Stdin = bytes.NewReader(data)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("script error: %s", errMsg)
	}

	return strings.TrimSpace(stdout.String()), nil
}
