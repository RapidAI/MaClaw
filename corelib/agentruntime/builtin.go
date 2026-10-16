package agentruntime

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

// BuiltinModules returns the compiled-in Runtime modules shared by GUI and
// srv. Hosts must not maintain a second list of these tools.
func BuiltinModules() []Module {
	return []Module{
		defaultRolePromptModule{},
		hostScreenshotModule{},
		hostOpenModule{},
	}
}

// RegisterBuiltinModules is the composition-root spelling used by GUI and
// headless hosts. Compiled builtins are always included; extra modules are
// tenant/test overlays and must not reuse a builtin module id.
func RegisterBuiltinModules(modules ...Module) (*ModuleRegistry, error) {
	if len(modules) == 0 {
		return defaultBuiltinRegistry()
	}
	builtins := BuiltinModules()
	combined := make([]Module, 0, len(builtins)+len(modules))
	combined = append(combined, builtins...)
	combined = append(combined, modules...)
	return NewModuleRegistry(combined...)
}

var defaultBuiltins struct {
	once     sync.Once
	registry *ModuleRegistry
	err      error
}

func defaultBuiltinRegistry() (*ModuleRegistry, error) {
	defaultBuiltins.once.Do(func() {
		builtins := BuiltinModules()
		defaultBuiltins.registry, defaultBuiltins.err = NewModuleRegistry(builtins...)
	})
	return defaultBuiltins.registry, defaultBuiltins.err
}

type defaultRolePromptModule struct{}

func (defaultRolePromptModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{ModuleID: "prompt.default_role", Version: "1.0.0", HeadlessSupport: true}
}

type hostScreenshotModule struct{}

func (hostScreenshotModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{
		ModuleID:                 "host.screenshot",
		Version:                  "1.0.0",
		HeadlessSupport:          false,
		RequiredHostCapabilities: []string{"desktop_capture"},
	}
}

func (hostScreenshotModule) Tools(context.Context, TurnRequest) ([]ToolDefinition, error) {
	return []ToolDefinition{{
		Name:        "screenshot",
		Description: "Capture the operator desktop. Unavailable on a headless host without a display adapter.",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"display": map[string]any{"type": "integer"}}},
	}}, nil
}

func (hostScreenshotModule) InvokeTool(ctx context.Context, request TurnRequest, name string, args map[string]any) (string, error) {
	if strings.TrimSpace(name) != "screenshot" {
		return "", fmt.Errorf("unknown builtin tool %s", name)
	}
	if request.Host == nil {
		return "", CapabilityUnavailableError{Capability: "desktop_capture"}
	}
	port, ok := request.Host.DesktopCapture()
	if !ok || port == nil {
		return "", CapabilityUnavailableError{Capability: "desktop_capture"}
	}
	capture := DesktopCaptureRequest{Display: DesktopCaptureAllDisplays}
	if raw, exists := args["display"]; exists && raw != nil {
		display, parseErr := ParseDesktopDisplayIndex(raw)
		if parseErr != nil {
			return "", parseErr
		}
		capture.Display = display
	}
	data, mime, err := port.Capture(ctx, capture)
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("desktop capture returned no image")
	}
	if mime == "" {
		mime = "image/png"
	}
	return fmt.Sprintf("captured screenshot (%d bytes, %s)", len(data), mime), nil
}

type hostOpenModule struct{}

func (hostOpenModule) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{
		ModuleID:        "host.open",
		Version:         "1.0.0",
		HeadlessSupport: false,
	}
}

func (hostOpenModule) Tools(context.Context, TurnRequest) ([]ToolDefinition, error) {
	return []ToolDefinition{{
		Name:        "open",
		Description: "Open a file or URL with the host default handler. Desktop-display-only on hosts without a launcher.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"target": map[string]any{"type": "string"}},
			"required":   []any{"target"},
		},
	}}, nil
}

func (hostOpenModule) InvokeTool(ctx context.Context, request TurnRequest, name string, args map[string]any) (string, error) {
	if strings.TrimSpace(name) != "open" {
		return "", fmt.Errorf("unknown builtin tool %s", name)
	}
	target := builtinToolStringArg(args, "target")
	if target == "" {
		return "", fmt.Errorf("missing target")
	}
	if request.Host == nil {
		return "", CapabilityUnavailableError{Capability: "document_launcher"}
	}
	if openURL, ok := builtinAbsoluteURL(target); ok {
		port, ok := request.Host.URLLauncher()
		if !ok || port == nil {
			return "", CapabilityUnavailableError{Capability: "url_launcher"}
		}
		if err := port.OpenURL(ctx, openURL); err != nil {
			return "", err
		}
		return "opened " + openURL, nil
	}
	if builtinHasFileURLScheme(target) {
		path, ok := builtinFileURLPath(target)
		if !ok {
			return "", fmt.Errorf("missing file path")
		}
		target = path
	}
	port, ok := request.Host.DocumentLauncher()
	if !ok || port == nil {
		return "", CapabilityUnavailableError{Capability: "document_launcher"}
	}
	if err := port.OpenDocument(ctx, target); err != nil {
		return "", err
	}
	return "opened " + target, nil
}

func builtinAbsoluteURL(target string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "mailto":
		return strings.TrimSpace(target), true
	default:
		return "", false
	}
}

func builtinHasFileURLScheme(target string) bool {
	parsed, err := url.Parse(strings.TrimSpace(target))
	return err == nil && strings.EqualFold(parsed.Scheme, "file")
}

func builtinFileURLPath(target string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil || !strings.EqualFold(parsed.Scheme, "file") {
		return "", false
	}
	path := parsed.Path
	if path == "" {
		path = parsed.Opaque
	}
	if unescaped, unescapeErr := url.PathUnescape(path); unescapeErr == nil {
		path = unescaped
	}
	if path == "" {
		return "", false
	}
	// url.Parse("file:///C:/foo") yields Path "/C:/foo".
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return path, true
}

func builtinToolStringArg(args map[string]any, key string) string {
	if len(args) == 0 {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return strings.TrimSpace(text)
}
