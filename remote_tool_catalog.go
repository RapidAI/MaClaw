package main

import (
	"fmt"
	"runtime"
	"strings"
	"unicode"
)

type RemoteToolMetadata struct {
	Name                  string
	DisplayName           string
	BinaryName            string
	DefaultTitle          string
	UsesOpenAICompat      bool
	RequiresSessionConfig bool
	SupportsProxy         bool
	SupportsRemote        bool
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
	SupportsRemote        bool   `json:"supports_remote"`
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
		SupportsRemote:  true,
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
		SupportsRemote:   true,
		ReadinessHint:    "Checks OpenAI-compatible auth, Codex command resolution, and exec --json SDK readiness.",
		SmokeHint:        "Runs registration, launch, real session start, and Hub visibility verification for Codex (SDK mode).",
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
		SupportsRemote:        true,
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
		SupportsRemote:        true,
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
		SupportsRemote:        true,
		ReadinessHint:         "Checks Kilo config sync plus KILO and OpenAI-compatible environment wiring.",
		SmokeHint:             "Runs registration, PTY, launch, real session start, and Hub visibility verification for Kilo.",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.Kilo },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewKiloAdapter(app) },
	},

	"gemini": {
		Name:           "gemini",
		DisplayName:    "Gemini",
		BinaryName:     "gemini",
		DefaultTitle:   "Gemini Session",
		SupportsProxy:  true,
		SupportsRemote: true,
		ReadinessHint:  "Checks Gemini CLI installation, API key, and ACP protocol readiness.",
		SmokeHint:      "Runs registration, launch, real session start, and Hub visibility verification for Gemini (ACP mode).",
		ConfigSelector: func(cfg AppConfig) ToolConfig { return cfg.Gemini },
		ProviderFactory: func(app *App) ProviderAdapter { return NewGeminiAdapter(app) },
	},
	"cursor": {
		Name:           "cursor",
		DisplayName:    "Cursor Agent",
		BinaryName:     "cursor-agent",
		DefaultTitle:   "Cursor Session",
		SupportsProxy:  true,
		SupportsRemote: true,
		ReadinessHint:  "Checks Cursor Agent CLI installation, SDK stream-json readiness, and remote capability.",
		SmokeHint:      "Runs registration, launch, real session start, and Hub visibility verification for Cursor Agent (SDK mode).",
		ConfigSelector: func(cfg AppConfig) ToolConfig { return cfg.Cursor },
		ProviderFactory: func(app *App) ProviderAdapter { return NewCursorAdapter(app) },
	},
	"codebuddy": {
		Name:                  "codebuddy",
		DisplayName:           "CodeBuddy",
		BinaryName:            "codebuddy",
		DefaultTitle:          "CodeBuddy Session",
		UsesOpenAICompat:      true,
		RequiresSessionConfig: true,
		SupportsProxy:         true,
		SupportsRemote:        true,
		ReadinessHint:         "Checks CodeBuddy CLI installation, SDK stream-json readiness, and remote capability.",
		SmokeHint:             "Runs registration, launch, real session start, and Hub visibility verification for CodeBuddy (SDK mode).",
		ConfigSelector:        func(cfg AppConfig) ToolConfig { return cfg.CodeBuddy },
		ProviderFactory:       func(app *App) ProviderAdapter { return NewCodeBuddyAdapter(app) },
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
	name := normalizeRemoteToolName(toolName)
	if len(name) == 0 {
		return name
	}
	// strings.Title is deprecated; capitalize first rune manually.
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
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

// remoteToolSupported returns true if the tool supports remote mode.
func remoteToolSupported(toolName string) bool {
	meta, ok := lookupRemoteToolMetadata(toolName)
	if !ok {
		return false
	}
	return meta.SupportsRemote
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
	case "gemini":
		return cfg.ShowGemini
	case "cursor":
		return cfg.ShowCursor && runtime.GOOS != "windows"
	case "codebuddy":
		return cfg.ShowCodeBuddy
	default:
		return false
	}
}

func listRemoteToolMetadataForApp(app *App) []RemoteToolMetadataView {
	order := []string{"claude", "gemini", "codex", "opencode", "cursor", "codebuddy", "iflow", "kilo"}
	out := make([]RemoteToolMetadataView, 0, len(order))
	cfg, err := app.LoadConfig()
	if err != nil {
		cfg = AppConfig{
			ShowGemini:    true,
			ShowCodex:     true,
			ShowOpenCode:  true,
			ShowCursor:    true,
			ShowCodeBuddy: true,
			ShowIFlow:     true,
			ShowKilo:      true,
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
			SupportsRemote:        meta.SupportsRemote,
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
