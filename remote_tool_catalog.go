package main

import (
	"fmt"
	"strings"
)

type RemoteToolMetadata struct {
	Name                  string
	DisplayName           string
	BinaryName            string
	DefaultTitle          string
	UsesOpenAICompat      bool
	RequiresSessionConfig bool
	SupportsProxy         bool
	ReadinessHint         string
	SmokeHint             string
	ConfigSelector        func(AppConfig) ToolConfig
	ProviderFactory       func(*App) ProviderAdapter
}

type RemoteToolMetadataView struct {
	Name                  string `json:"name"`
	DisplayName           string `json:"display_name"`
	BinaryName            string `json:"binary_name"`
	DefaultTitle          string `json:"default_title"`
	UsesOpenAICompat      bool   `json:"uses_openai_compat"`
	RequiresSessionConfig bool   `json:"requires_session_config"`
	SupportsProxy         bool   `json:"supports_proxy"`
	Visible               bool   `json:"visible"`
	Installed             bool   `json:"installed"`
	CanStart              bool   `json:"can_start"`
	ToolPath              string `json:"tool_path"`
	UnavailableReason     string `json:"unavailable_reason,omitempty"`
	ReadinessHint         string `json:"readiness_hint"`
	SmokeHint             string `json:"smoke_hint"`
}

var remoteToolCatalog = map[string]RemoteToolMetadata{
	"claude": {
		Name:            "claude",
		DisplayName:     "Claude",
		BinaryName:      "claude",
		DefaultTitle:    "Claude Session",
		SupportsProxy:   true,
		ReadinessHint:   "Checks Anthropic-compatible auth, Claude launch command, and SDK stream-json readiness.",
		SmokeHint:       "Runs registration, launch, real session start, and Hub visibility verification for Claude (SDK mode).",
		ConfigSelector:  func(cfg AppConfig) ToolConfig { return cfg.Claude },
		ProviderFactory: func(app *App) ProviderAdapter { return NewClaudeAdapter(app) },
	},
	"codex": {
		Name:             "codex",
		DisplayName:      "Codex",
		BinaryName:       "codex",
		DefaultTitle:     "Codex Session",
		UsesOpenAICompat: true,
		SupportsProxy:    true,
		ReadinessHint:    "Checks OpenAI-compatible auth, Codex command resolution, and remote PTY readiness.",
		SmokeHint:        "Runs registration, PTY, launch, real session start, and Hub visibility verification for Codex.",
		ConfigSelector:   func(cfg AppConfig) ToolConfig { return cfg.Codex },
		ProviderFactory:  func(app *App) ProviderAdapter { return NewCodexAdapter(app) },
	},
	"opencode": {
		Name:                  "opencode",
		DisplayName:           "OpenCode",
		BinaryName:            "opencode",
		DefaultTitle:          "OpenCode Session",
		UsesOpenAICompat:      true,
		RequiresSessionConfig: true,
		SupportsProxy:         true,
		ReadinessHint:         "Checks OpenCode config sync, OpenAI-compatible endpoints, and isolated session config.",
		SmokeHint:             "Runs registration, PTY, launch, real session start, and Hub visibility verification for OpenCode.",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.Opencode },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewOpencodeAdapter(app) },
	},
	"iflow": {
		Name:                  "iflow",
		DisplayName:           "iFlow",
		BinaryName:            "iflow",
		DefaultTitle:          "iFlow Session",
		UsesOpenAICompat:      true,
		RequiresSessionConfig: true,
		SupportsProxy:         true,
		ReadinessHint:         "Checks iFlow config sync plus IFLOW and OpenAI-compatible environment wiring.",
		SmokeHint:             "Runs registration, PTY, launch, real session start, and Hub visibility verification for iFlow.",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.IFlow },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewIFlowAdapter(app) },
	},
	"kilo": {
		Name:                  "kilo",
		DisplayName:           "Kilo",
		BinaryName:            "kilo",
		DefaultTitle:          "Kilo Session",
		UsesOpenAICompat:      true,
		RequiresSessionConfig: true,
		SupportsProxy:         true,
		ReadinessHint:         "Checks Kilo config sync plus KILO and OpenAI-compatible environment wiring.",
		SmokeHint:             "Runs registration, PTY, launch, real session start, and Hub visibility verification for Kilo.",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.Kilo },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewKiloAdapter(app) },
	},
	"kode": {
		Name:                  "kode",
		DisplayName:           "Kode",
		BinaryName:            "kode",
		DefaultTitle:          "Kode Session",
		UsesOpenAICompat:      true,
		RequiresSessionConfig: true,
		SupportsProxy:         true,
		ReadinessHint:         "Checks Kode profile generation and OpenAI-compatible endpoint wiring.",
		SmokeHint:             "Runs registration, PTY, launch, real session start, and Hub visibility verification for Kode.",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.Kode },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewKodeAdapter(app) },
	},
	"gemini": {
		Name:           "gemini",
		DisplayName:    "Gemini",
		BinaryName:     "gemini",
		DefaultTitle:   "Gemini Session",
		SupportsProxy:  true,
		ReadinessHint:  "Checks Gemini CLI installation, API key, and remote PTY readiness.",
		SmokeHint:      "Runs registration, PTY, launch, real session start, and Hub visibility verification for Gemini.",
		ConfigSelector: func(cfg AppConfig) ToolConfig { return cfg.Gemini },
		ProviderFactory: func(app *App) ProviderAdapter { return NewGeminiAdapter(app) },
	},
	"cursor": {
		Name:           "cursor",
		DisplayName:    "Cursor Agent",
		BinaryName:     "cursor-agent",
		DefaultTitle:   "Cursor Session",
		SupportsProxy:  true,
		ReadinessHint:  "Checks Cursor Agent CLI installation and remote PTY readiness.",
		SmokeHint:      "Runs registration, PTY, launch, real session start, and Hub visibility verification for Cursor Agent.",
		ConfigSelector: func(cfg AppConfig) ToolConfig { return cfg.Cursor },
		ProviderFactory: func(app *App) ProviderAdapter { return NewCursorAdapter(app) },
	},
}

func normalizeRemoteToolName(toolName string) string {
	tool := strings.ToLower(strings.TrimSpace(toolName))
	if tool == "" {
		return "claude"
	}
	return tool
}

func lookupRemoteToolMetadata(toolName string) (RemoteToolMetadata, bool) {
	tool := normalizeRemoteToolName(toolName)
	meta, ok := remoteToolCatalog[tool]
	return meta, ok
}

func getRemoteToolMetadata(toolName string) (RemoteToolMetadata, error) {
	meta, ok := lookupRemoteToolMetadata(toolName)
	if !ok {
		return RemoteToolMetadata{}, fmt.Errorf("unsupported remote tool: %s", toolName)
	}
	return meta, nil
}

func remoteToolDisplayName(toolName string) string {
	meta, ok := lookupRemoteToolMetadata(toolName)
	if ok {
		return meta.DisplayName
	}
	return strings.Title(normalizeRemoteToolName(toolName))
}

func remoteToolConfig(cfg AppConfig, toolName string) (ToolConfig, error) {
	meta, err := getRemoteToolMetadata(toolName)
	if err != nil {
		return ToolConfig{}, err
	}
	return meta.ConfigSelector(cfg), nil
}

func (a *App) remoteProviderAdapter(toolName string) (ProviderAdapter, error) {
	meta, err := getRemoteToolMetadata(toolName)
	if err != nil {
		return nil, err
	}
	return meta.ProviderFactory(a), nil
}

// listRemoteToolMetadata is unused dead code — use listRemoteToolMetadataForApp instead.
func listRemoteToolMetadata() []RemoteToolMetadataView {
	return nil
}

func remoteToolVisible(cfg AppConfig, toolName string) bool {
	switch normalizeRemoteToolName(toolName) {
	case "claude":
		return true
	case "codex":
		return cfg.ShowCodex
	case "opencode":
		return cfg.ShowOpenCode
	case "iflow":
		return cfg.ShowIFlow
	case "kilo":
		return cfg.ShowKilo
	case "kode":
		return cfg.ShowKode
	case "gemini":
		return cfg.ShowGemini
	case "cursor":
		return cfg.ShowCursor
	default:
		return false
	}
}

func listRemoteToolMetadataForApp(app *App) []RemoteToolMetadataView {
	order := []string{"claude", "gemini", "codex", "opencode", "cursor", "iflow", "kilo", "kode"}
	out := make([]RemoteToolMetadataView, 0, len(order))
	cfg, err := app.LoadConfig()
	if err != nil {
		cfg = AppConfig{
			ShowGemini:   true,
			ShowCodex:    true,
			ShowOpenCode: true,
			ShowCursor:   true,
			ShowIFlow:    true,
			ShowKilo:     true,
			ShowKode:     true,
		}
	}
	toolManager := NewToolManager(app)
	for _, name := range order {
		meta, ok := remoteToolCatalog[name]
		if !ok {
			continue
		}
		status := toolManager.GetToolStatus(name)
		visible := remoteToolVisible(cfg, name)
		canStart := visible && status.Installed && strings.TrimSpace(status.Path) != ""
		reason := ""
		if !visible {
			reason = "hidden in settings"
		} else if !status.Installed || strings.TrimSpace(status.Path) == "" {
			reason = "tool is not installed"
		}
		out = append(out, RemoteToolMetadataView{
			Name:                  meta.Name,
			DisplayName:           meta.DisplayName,
			BinaryName:            meta.BinaryName,
			DefaultTitle:          meta.DefaultTitle,
			UsesOpenAICompat:      meta.UsesOpenAICompat,
			RequiresSessionConfig: meta.RequiresSessionConfig,
			SupportsProxy:         meta.SupportsProxy,
			Visible:               visible,
			Installed:             status.Installed,
			CanStart:              canStart,
			ToolPath:              status.Path,
			UnavailableReason:     reason,
			ReadinessHint:         meta.ReadinessHint,
			SmokeHint:             meta.SmokeHint,
		})
	}
	return out
}
