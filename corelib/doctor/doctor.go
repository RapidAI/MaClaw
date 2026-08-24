// Package doctor aggregates local readiness checks for MaClaw.
//
// It is intentionally free of GUI/TUI/Hub runtime dependencies so CLI, TUI,
// and GUI can share one report shape. Live network probes (gateway health,
// provider pings) are optional and supplied by the caller.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

// Status is the outcome of a single readiness check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
	StatusSkip Status = "skip"
	StatusInfo Status = "info"
)

// Check is one readiness finding.
type Check struct {
	ID      string         `json:"id"`
	Status  Status         `json:"status"`
	Message string         `json:"message"`
	Hint    string         `json:"hint,omitempty"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// Report is the aggregated doctor output.
type Report struct {
	OK          bool      `json:"ok"`
	Summary     string    `json:"summary"`
	GeneratedAt time.Time `json:"generated_at"`
	ConfigPath  string    `json:"config_path,omitempty"`
	BaseDir     string    `json:"base_dir,omitempty"`
	Checks      []Check   `json:"checks"`
	Blockers    int       `json:"blockers"`
	Warnings    int       `json:"warnings"`
}

// Input configures a local doctor run.
type Input struct {
	// Config is the loaded application configuration. Zero value is allowed;
	// checks will report missing LLM settings as failures.
	Config corelib.AppConfig

	// ConfigPath is the path the config was loaded from (for messaging only).
	ConfigPath string

	// BaseDir overrides the MaClaw home used for path checks. Empty uses
	// maclawpath.BaseDir().
	BaseDir string

	// Now overrides the report timestamp (tests). Zero uses time.Now().
	Now time.Time

	// ExtraChecks are appended after built-in local checks (e.g. gateway probe).
	ExtraChecks []Check
}

// Run evaluates local readiness from configuration and filesystem posture.
// It never opens network connections; pass live probes via ExtraChecks.
func Run(in Input) Report {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	baseDir := strings.TrimSpace(in.BaseDir)
	if baseDir == "" {
		baseDir = maclawpath.BaseDir()
	}

	r := Report{
		GeneratedAt: now,
		ConfigPath:  strings.TrimSpace(in.ConfigPath),
		BaseDir:     baseDir,
		Checks:      make([]Check, 0, 16),
	}

	cfg := in.Config
	add := func(c Check) {
		r.Checks = append(r.Checks, c)
	}

	// --- config file ---
	if path := strings.TrimSpace(in.ConfigPath); path != "" {
		if st, err := os.Stat(path); err != nil {
			if os.IsNotExist(err) {
				add(Check{
					ID:      "config.file",
					Status:  StatusWarn,
					Message: "config file not found; defaults will be used",
					Hint:    "Run the GUI onboarding wizard or create " + path,
					Detail:  map[string]any{"path": path},
				})
			} else {
				add(Check{
					ID:      "config.file",
					Status:  StatusFail,
					Message: "config file not readable: " + err.Error(),
					Hint:    "Fix permissions on " + path,
					Detail:  map[string]any{"path": path},
				})
			}
		} else if st.IsDir() {
			add(Check{
				ID:      "config.file",
				Status:  StatusFail,
				Message: "config path is a directory",
				Detail:  map[string]any{"path": path},
			})
		} else {
			add(Check{
				ID:      "config.file",
				Status:  StatusOK,
				Message: "config file present",
				Detail:  map[string]any{"path": path, "size": st.Size()},
			})
		}
	} else {
		add(Check{
			ID:      "config.file",
			Status:  StatusInfo,
			Message: "no config path provided; evaluated in-memory config only",
		})
	}

	// --- primary LLM ---
	llmURL := strings.TrimSpace(cfg.MaclawLLMUrl)
	llmModel := strings.TrimSpace(cfg.MaclawLLMModel)
	llmKey := strings.TrimSpace(cfg.MaclawLLMKey)
	provider := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	switch {
	case llmURL == "" || llmModel == "":
		add(Check{
			ID:      "llm.primary",
			Status:  StatusFail,
			Message: "primary LLM is not configured (need URL and model)",
			Hint:    "Open Settings → LLM, or set maclaw_llm_url and maclaw_llm_model in config.json",
			Detail: map[string]any{
				"url_set":   llmURL != "",
				"model_set": llmModel != "",
				"provider":  provider,
			},
		})
	default:
		add(Check{
			ID:      "llm.primary",
			Status:  StatusOK,
			Message: fmt.Sprintf("primary LLM configured: model=%s", llmModel),
			Detail: map[string]any{
				"model":     llmModel,
				"protocol":  strings.TrimSpace(cfg.MaclawLLMProtocol),
				"provider":  provider,
				"url_host":  redactURLHost(llmURL),
				"key_set":   llmKey != "",
				"providers": len(cfg.MaclawLLMProviders),
			},
		})
		if llmKey == "" {
			add(Check{
				ID:      "llm.primary_key",
				Status:  StatusWarn,
				Message: "primary LLM API key is empty",
				Hint:    "Local OpenAI-compatible servers may work without a key; cloud providers usually require one",
			})
		} else {
			add(Check{
				ID:      "llm.primary_key",
				Status:  StatusOK,
				Message: "primary LLM API key is set",
			})
		}
	}

	// --- auxiliary LLM ---
	if cfg.AuxiliaryLLM.IsConfigured() {
		add(Check{
			ID:      "llm.aux",
			Status:  StatusOK,
			Message: fmt.Sprintf("auxiliary LLM configured: model=%s", strings.TrimSpace(cfg.AuxiliaryLLM.Model)),
			Detail: map[string]any{
				"model":    strings.TrimSpace(cfg.AuxiliaryLLM.Model),
				"url_host": redactURLHost(cfg.AuxiliaryLLM.URL),
			},
		})
	} else {
		add(Check{
			ID:      "llm.aux",
			Status:  StatusWarn,
			Message: "auxiliary LLM not configured",
			Hint:    "Configure auxiliary_llm for cheaper intent/summary/compression turns",
		})
	}

	// --- model routes ---
	if n := len(cfg.ModelRoutes); n > 0 {
		keys := make([]string, 0, n)
		for k := range cfg.ModelRoutes {
			keys = append(keys, k)
		}
		add(Check{
			ID:      "llm.model_routes",
			Status:  StatusOK,
			Message: fmt.Sprintf("%d model route(s) configured", n),
			Detail:  map[string]any{"tasks": keys},
		})
	} else {
		add(Check{
			ID:      "llm.model_routes",
			Status:  StatusInfo,
			Message: "no per-task model routes; all turns use primary (or aux fallback for light tasks)",
			Hint:    "Set model_routes.intent/fast/summary to cut cost without changing the primary coding model",
		})
	}

	// --- combined routing posture (can cheap turns leave primary?) ---
	canRouteCheap := cfg.AuxiliaryLLM.IsConfigured() || len(cfg.ModelRoutes) > 0
	if canRouteCheap {
		add(Check{
			ID:      "llm.routing",
			Status:  StatusOK,
			Message: "turn routing can divert light tasks off the primary model",
			Detail: map[string]any{
				"aux":    cfg.AuxiliaryLLM.IsConfigured(),
				"routes": len(cfg.ModelRoutes),
			},
		})
	} else {
		add(Check{
			ID:      "llm.routing",
			Status:  StatusWarn,
			Message: "turn routing will always stay on primary (no aux and no model_routes)",
			Hint:    "Configure auxiliary_llm and/or model_routes.fast/summary to reduce cost on simple turns",
		})
	}

	// --- shared agent loop strangler (env + config) ---
	add(SharedLoopCheck(cfg))
	// --- adaptive system prompt hit rate / est. token savings ---
	add(AdaptivePromptCheck())
	add(WorkingStateCheck())
	// --- cost-route tier stats + fleet daily $ ---
	add(CostRouteCheck())
	add(MoACheck(cfg))

	// --- remote hub ---
	if cfg.RemoteEnabled {
		hubURL := strings.TrimSpace(cfg.RemoteHubURL)
		if hubURL == "" && strings.TrimSpace(cfg.RemoteHubCenterURL) == "" && len(cfg.RemoteHubCenterURLs) == 0 {
			add(Check{
				ID:      "hub.remote",
				Status:  StatusFail,
				Message: "remote mode enabled but no Hub/HubCenter URL is set",
				Hint:    "Set remote_hub_url or remote_hubcenter_url in config",
			})
		} else {
			add(Check{
				ID:      "hub.remote",
				Status:  StatusOK,
				Message: "remote mode enabled with hub endpoint(s)",
				Detail: map[string]any{
					"hub_url_set":        hubURL != "",
					"hubcenter_url_set":  strings.TrimSpace(cfg.RemoteHubCenterURL) != "",
					"hubcenter_url_list": len(cfg.RemoteHubCenterURLs),
					"user_id_set":        strings.TrimSpace(cfg.RemoteUserID) != "",
					"machine_token_set":  strings.TrimSpace(cfg.RemoteMachineToken) != "",
				},
			})
			if strings.TrimSpace(cfg.RemoteMachineToken) == "" {
				add(Check{
					ID:      "hub.auth",
					Status:  StatusWarn,
					Message: "remote machine token is empty",
					Hint:    "Re-login / re-bind the machine to Hub so remote features can authenticate",
				})
			}
		}
	} else {
		add(Check{
			ID:      "hub.remote",
			Status:  StatusSkip,
			Message: "remote mode disabled (local-only)",
		})
	}

	// --- third-party gateway config ---
	if cfg.ThirdPartyGatewayEnabled {
		token := strings.TrimSpace(cfg.ThirdPartyGatewayToken)
		host := strings.TrimSpace(cfg.ThirdPartyGatewayHost)
		port := cfg.ThirdPartyGatewayPort
		if port <= 0 {
			port = 18777
		}
		if token == "" {
			add(Check{
				ID:      "gateway.config",
				Status:  StatusFail,
				Message: "third-party gateway enabled but token is empty",
				Hint:    "Run: maclaw-cli bootstrap",
				Detail:  map[string]any{"host": host, "port": port},
			})
		} else {
			add(Check{
				ID:      "gateway.config",
				Status:  StatusOK,
				Message: "third-party gateway token present",
				Detail: map[string]any{
					"host":  host,
					"port":  port,
					"local": cfg.IsThirdPartyGatewayLocalMode(),
				},
			})
		}
	} else {
		add(Check{
			ID:      "gateway.config",
			Status:  StatusSkip,
			Message: "third-party gateway disabled",
			Hint:    "Enable with: maclaw-cli bootstrap",
		})
	}

	// --- home paths ---
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		add(Check{
			ID:      "paths.home",
			Status:  StatusFail,
			Message: "cannot create MaClaw home directory: " + err.Error(),
			Detail:  map[string]any{"base_dir": baseDir},
		})
	} else {
		// Write probe (best-effort, remove after).
		probe := filepath.Join(baseDir, ".doctor_write_probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
			add(Check{
				ID:      "paths.home",
				Status:  StatusFail,
				Message: "MaClaw home is not writable: " + err.Error(),
				Hint:    "Fix permissions on " + baseDir,
				Detail:  map[string]any{"base_dir": baseDir},
			})
		} else {
			_ = os.Remove(probe)
			dataDir := filepath.Join(baseDir, "data")
			logsDir := filepath.Join(baseDir, "logs")
			add(Check{
				ID:      "paths.home",
				Status:  StatusOK,
				Message: "MaClaw home is writable",
				Detail: map[string]any{
					"base_dir":        baseDir,
					"data_dir":        dataDir,
					"logs_dir":        logsDir,
					"data_dir_exists": dirExists(dataDir),
					"logs_dir_exists": dirExists(logsDir),
				},
			})
		}
	}

	// --- security ---
	mode := strings.TrimSpace(cfg.SecurityPolicyMode)
	if mode == "" {
		mode = "default"
	}
	add(Check{
		ID:      "security.policy",
		Status:  StatusOK,
		Message: "security policy mode: " + mode,
		Detail:  map[string]any{"mode": mode, "hub_centralized": cfg.HubSecurityCentralized},
	})

	// --- onboarding ---
	if cfg.OnboardingDone {
		add(Check{
			ID:      "onboarding",
			Status:  StatusOK,
			Message: "onboarding marked complete",
		})
	} else {
		add(Check{
			ID:      "onboarding",
			Status:  StatusWarn,
			Message: "onboarding not marked complete",
			Hint:    "Complete the first-run wizard in the GUI to finish provider and channel setup",
		})
	}

	// --- token usage (informational) ---
	if len(cfg.LLMTokenUsage) == 0 {
		add(Check{
			ID:      "usage.tokens",
			Status:  StatusInfo,
			Message: "no accumulated LLM token usage yet",
		})
	} else {
		var totalIn, totalOut, totalReq int64
		var totalCost float64
		providers := make([]string, 0, len(cfg.LLMTokenUsage))
		for name, st := range cfg.LLMTokenUsage {
			if st == nil {
				continue
			}
			providers = append(providers, name)
			totalIn += st.InputTokens
			totalOut += st.OutputTokens
			totalReq += st.Requests
			totalCost += st.TotalCostRMB
		}
		add(Check{
			ID:      "usage.tokens",
			Status:  StatusInfo,
			Message: fmt.Sprintf("token usage recorded for %d provider(s)", len(providers)),
			Detail: map[string]any{
				"providers":      providers,
				"input_tokens":   totalIn,
				"output_tokens":  totalOut,
				"requests":       totalReq,
				"total_cost_rmb": totalCost,
			},
		})
	}

	// --- feature flags (info) ---
	cuFlag := true
	if cfg.ComputerUseEnabled != nil {
		cuFlag = *cfg.ComputerUseEnabled
	}
	spFlag := true
	if cfg.ScreenParsingEnabled != nil {
		spFlag = *cfg.ScreenParsingEnabled
	}
	add(Check{
		ID:      "features.flags",
		Status:  StatusInfo,
		Message: "feature toggles snapshot",
		Detail: map[string]any{
			"vector_search":    cfg.VectorSearchEnabled,
			"asr":              cfg.ASREnabled,
			"tts":              cfg.TTSEnabled,
			"memory_compress":  cfg.MemoryAutoCompress,
			"local_needle":     cfg.LocalNeedleEnabled,
			"daily_budget_usd": cfg.DailyLLMBudgetUSD,
			"computer_use":     cuFlag,
			"screen_parsing":   spFlag,
		},
	})

	// --- embedding accel detect (info; Backend is not the About badge SoT) ---
	ai := embedding.CurrentAccelInfo()
	add(Check{
		ID:      "embedding.accel",
		Status:  StatusInfo,
		Message: "embedding accel: " + ai.Reason,
		Detail: map[string]any{
			"backend":     ai.Backend,
			"device":      ai.Device,
			"npu_present": ai.NPUPresent,
			"reason":      ai.Reason,
			"prefer_npu":  cfg.EmbedHWAccelEnabled(),
		},
	})

	// --- Computer Use (desktop control) ---
	for _, c := range ComputerUseChecks(cfg, baseDir) {
		add(c)
	}

	// --- caller-supplied probes ---
	for _, c := range in.ExtraChecks {
		if strings.TrimSpace(c.ID) == "" {
			continue
		}
		add(c)
	}

	r.Blockers, r.Warnings = countStatuses(r.Checks)
	r.OK = r.Blockers == 0
	r.Summary = summarize(r.Blockers, r.Warnings, len(r.Checks))
	return r
}

func countStatuses(checks []Check) (blockers, warnings int) {
	for _, c := range checks {
		switch c.Status {
		case StatusFail:
			blockers++
		case StatusWarn:
			warnings++
		}
	}
	return blockers, warnings
}

func summarize(blockers, warnings, total int) string {
	switch {
	case blockers == 0 && warnings == 0:
		return fmt.Sprintf("ready (%d checks)", total)
	case blockers == 0:
		return fmt.Sprintf("ready with %d warning(s) (%d checks)", warnings, total)
	default:
		return fmt.Sprintf("%d blocker(s), %d warning(s) (%d checks)", blockers, warnings, total)
	}
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// redactURLHost returns scheme://host (no path/query/userinfo) for safe logs.
func redactURLHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Avoid importing net/url just for host display when parse fails.
	if i := strings.Index(raw, "://"); i >= 0 {
		rest := raw[i+3:]
		if j := strings.IndexAny(rest, "/?#"); j >= 0 {
			rest = rest[:j]
		}
		// Strip userinfo if present.
		if at := strings.LastIndex(rest, "@"); at >= 0 {
			rest = rest[at+1:]
		}
		return raw[:i+3] + rest
	}
	if j := strings.IndexAny(raw, "/?#"); j >= 0 {
		return raw[:j]
	}
	return raw
}

// CheckFromError builds a fail/skip check from an optional probe error.
func CheckFromError(id, okMessage string, err error, hint string) Check {
	if err == nil {
		return Check{ID: id, Status: StatusOK, Message: okMessage}
	}
	return Check{
		ID:      id,
		Status:  StatusFail,
		Message: err.Error(),
		Hint:    hint,
	}
}
