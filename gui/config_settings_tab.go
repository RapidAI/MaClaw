package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib"
)

// settingsTabFieldKeys lists AppConfig JSON keys needed by each settings content tab.
// Tabs that load their own data via dedicated APIs (search/memory/knowledge/…) use
// empty slices so GetSettingsTabConfig returns nil without touching config.
//
// Keep in sync with SETTINGS_CONTENT_TAB_IDS / SETTINGS_TABS_NEEDING_CONFIG in
// gui/frontend/src/config/settingsTabConfig.ts and settingsTabs.ts.
var settingsTabFieldKeys = map[string][]string{
	"general": {
		"language",
		"working_directory",
		"show_app_entry",
		"show_workflow_entry",
		"show_utilities_entry",
		"survey_enabled",
		"workflow_enabled",
		"skill_evolution_enabled",
		"llm_trajectory_logging",
		"log_detail_enabled",
		"memory_recall_log_enabled",
		"gossip_auto_publish",
		"show_hub_ranking",
		"pause_env_check",
		"use_windows_terminal",
		"show_ai_trace_entry",
		"env_check_interval",
	},
	"proxy": {
		"default_proxy_enabled",
		"default_proxy_protocol",
		"default_proxy_host",
		"default_proxy_port",
		"default_proxy_username",
		"default_proxy_password",
		"default_proxy_bypass",
		"default_proxy_scope_maclaw",
		"default_proxy_scope_coding_tools",
		"default_proxy_scope_agent",
	},
	"ui": {
		"ui_zoom_factor",
		"chat_font_size",
	},
	"display": {
		"default_launch_mode",
		"remote_enabled",
		"show_coding_tool_entry",
		"show_codex",
		"show_opencode",
		"show_codebuddy",
		"show_iflow",
		"show_kilo",
		"ui_mode",
		"acp_host_enabled",
		"acp_host_mirror_ui",
		"acp_host_port",
	},
	"pet": {
		"pet_enabled",
		"pet_skin",
		"pet_size",
		"pet_variant",
		"pet_motion_enabled",
		"pet_motion_sound_enabled",
		"pet_motion_sound_preset",
		"pet_text_interaction_enabled",
		"pet_voice_input_enabled",
		"pet_voice_readback_enabled",
		"pet_file_drop_enabled",
		"pet_interaction_mode",
		"pet_conversation_mode",
		"pet_readback_mode",
		"pet_auto_retry_on_no_hear",
		"pet_continuous_timeout_sec",
		"pet_quiet_mode",
		"pet_reduced_motion",
		"pet_ambient_city",
		"pet_figurative_upgrade_prompt_pending",
		"asr_enabled",
		"tts_enabled",
		"remote_hub_url",
	},
	// Self-loading panels — empty DTO (dedicated APIs).
	"searchEngine":    {},
	"redeem":          {},
	"memory":          {},
	"knowledge":       {},
	"misData":         {},
	"embedding":       {},
	"migration":       {},
	"llm":             {"codex"}, // models list for optional codexModels prop
	"llmCache":        {"llm_prompt_cache"},
	"virtualEmployee": {"remote_machine_id", "favorite_employees", "favorite_employee_names"},
	"im": {
		"qqbot_enabled",
		"qqbot_app_id",
		"qqbot_app_secret",
		"qqbot_owner_openid",
		"qqbot_local_mode",
		"telegram_bot_enabled",
		"telegram_bot_token",
		"telegram_owner_chat_id",
		"telegram_local_mode",
		"weixin_enabled",
		"weixin_token",
		"weixin_base_url",
		"weixin_cdn_url",
		"weixin_account_id",
		"weixin_local_mode",
		"lansenger_enabled",
		"lansenger_app_id",
		"lansenger_app_secret",
		"lansenger_gateway_url",
		"lansenger_wss_url",
		"lansenger_ignored_group_ids",
		"lansenger_group_policy",
		"lansenger_allowed_group_ids",
		"lansenger_require_mention",
		"lansenger_respond_to_at_all",
		"lansenger_auto_mention_reply",
		"lansenger_auto_quote_reply",
		"lansenger_group_knowledge_source_ids",
		"lansenger_group_allow_all_directories",
		"lansenger_group_allowed_directories",
		"lansenger_local_mode",
		"thirdparty_gateway_enabled",
		"thirdparty_gateway_token",
		"thirdparty_gateway_host",
		"thirdparty_gateway_port",
		"thirdparty_gateway_local_mode",
		"im_progress_nudge_enabled",
		"acp_host_enabled",
		"acp_host_mirror_ui",
		"acp_host_port",
	},
	"security": {
		"security_policy_mode",
		"hub_security_centralized",
		"sandbox_mode",
		"network_level",
		"network_allowlist",
		"yolo_mode_allowed",
		"smart_route_enabled",
		"gossip_enabled",
		"file_outbound_enabled",
		"image_outbound_enabled",
		"skill_sources_allowed",
		"computer_use_enabled",
	},
	"system": {
		"remote_machine_id",
		"remote_user_id",
		"remote_client_id",
		"remote_sn",
		"remote_hub_url",
		"remote_email",
		"remote_heartbeat_sec",
		"remote_enabled",
		"remote_hub_id",
		"remote_hubcenter_url",
		"remote_tenant_id",
		"remote_tenant_name",
		"remote_machine_name",
		"remote_nickname",
		"weixin_local_mode",
		"screen_dim_timeout_min",
		"agent_response_timeout_sec",
		"skill_runner_timeout_sec",
		"maclaw_llm_timeout_sec",
		"workstation_mode",
		"power_optimization",
		"check_update_on_startup",
		"prefer_beta_channel",
		"audio_input_device_id",
		"audio_output_device_id",
		"data_dir",
		"tool_cache_maintenance",
		"computer_use_log_keep_newest",
		"computer_use_log_max_age_days",
		"computer_use_log_auto_prune",
	},
}

// Heavy / never-ship keys even if a tab list accidentally includes them.
var settingsTabBlockedKeys = map[string]struct{}{
	"nl_skills":         {},
	"llm_token_usage":   {},
	"mcp_servers":       {},
	"local_mcp_servers": {},
	// Large blobs that settings tabs never need from this DTO.
	"maclaw_llm_providers": {},
	"projects":             {},
	"web_search_providers": {},
	"ssh_hosts":            {},
	"skill_hub_urls":       {},
}

// appConfigJSONFieldIndex maps AppConfig `json` tag → struct field index.
// Built once; AppConfig shape is process-static.
var (
	appConfigJSONFieldOnce  sync.Once
	appConfigJSONFieldIndex map[string]int
)

func buildAppConfigJSONFieldIndex() {
	appConfigJSONFieldIndex = make(map[string]int, 128)
	t := reflect.TypeOf(corelib.AppConfig{})
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.PkgPath != "" { // unexported
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		name = strings.TrimSpace(name)
		if name == "" || name == "-" {
			continue
		}
		appConfigJSONFieldIndex[name] = i
	}
}

func appConfigFieldIndex(jsonKey string) (int, bool) {
	appConfigJSONFieldOnce.Do(buildAppConfigJSONFieldIndex)
	i, ok := appConfigJSONFieldIndex[jsonKey]
	return i, ok
}

// settingsTabNeedsConfig reports whether the tab DTO carries AppConfig fields.
func settingsTabNeedsConfig(tab string) bool {
	keys, ok := settingsTabFieldKeys[normalizeSettingsTabID(tab)]
	return ok && len(keys) > 0
}

func normalizeSettingsTabID(tab string) string {
	return strings.TrimSpace(tab)
}

// fieldToJSONValue converts one AppConfig field to a JSON-round-trippable value.
// Always encodes the field (no omitempty), so false/0/"" reach the frontend.
func fieldToJSONValue(fv reflect.Value) (interface{}, error) {
	if !fv.IsValid() {
		return nil, nil
	}
	// Interface/pointer: expose concrete value; nil pointer → JSON null.
	for fv.Kind() == reflect.Interface || fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil, nil
		}
		fv = fv.Elem()
	}
	switch fv.Kind() {
	case reflect.Bool:
		return fv.Bool(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fv.Uint(), nil
	case reflect.Float32, reflect.Float64:
		return fv.Float(), nil
	case reflect.String:
		return fv.String(), nil
	default:
		// Structs, maps, slices: marshal just this field (not the whole AppConfig).
		raw, err := json.Marshal(fv.Interface())
		if err != nil {
			return nil, err
		}
		if string(raw) == "null" {
			return nil, nil
		}
		var v interface{}
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
}

// filterSettingsTabConfig projects only the listed JSON keys from cfg.
// Takes a pointer so hot-path PeekConfig does not copy the whole AppConfig.
// Unlike full-config json.Marshal, this:
//   - skips heavy fields (providers, projects, skills, …) entirely
//   - always includes zero values for listed keys (false / 0 / "") so merge
//     cannot leave stale true from a prior client snapshot
func filterSettingsTabConfig(cfg *corelib.AppConfig, tab string) (map[string]interface{}, error) {
	if cfg == nil {
		return nil, nil
	}
	tab = normalizeSettingsTabID(tab)
	keys, ok := settingsTabFieldKeys[tab]
	if !ok || len(keys) == 0 {
		return nil, nil
	}

	rv := reflect.ValueOf(cfg).Elem()
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if _, blocked := settingsTabBlockedKeys[k]; blocked {
			continue
		}
		idx, found := appConfigFieldIndex(k)
		if !found {
			// Unknown key in the tab map — skip rather than fail the whole tab.
			continue
		}
		val, err := fieldToJSONValue(rv.Field(idx))
		if err != nil {
			return nil, fmt.Errorf("settings tab field %q: %w", k, err)
		}
		// Always set listed keys so false/0 are not dropped (omitempty hazard).
		out[k] = val
	}
	return out, nil
}

// GetSettingsTabConfig returns a fine-grained config DTO for one settings content tab.
// Prefer this over LoadConfigForUI when opening/switching settings panels so the
// Wails bridge does not ship the full AppConfig (providers, projects, skills, …).
//
// The returned map uses the same JSON keys as AppConfig. Frontend merges into
// the existing AppConfig state (never replaces it wholesale).
// Self-loading / unknown tabs return nil map (no allocation); non-empty maps
// are freshly allocated.
func (a *App) GetSettingsTabConfig(tab string) (map[string]interface{}, error) {
	tab = normalizeSettingsTabID(tab)
	if tab == "" {
		tab = "general"
	}
	if !settingsTabNeedsConfig(tab) {
		// Known empty / unknown tabs: no config load.
		return nil, nil
	}

	// Prefer lock-free snap; cold-start falls through to LoadConfig.
	// Do not dereference-copy the snap — filter reads fields via pointer.
	if p := a.PeekConfig(); p != nil {
		return filterSettingsTabConfig(p, tab)
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	// Drop skills before filter for consistency with LoadConfigForUI
	// (snap path already omits them).
	cfg.NLSkills = nil
	return filterSettingsTabConfig(&cfg, tab)
}

// validateSettingsTabFieldKeys reports tab field keys that are not AppConfig
// JSON tags (drift between settingsTabFieldKeys and corelib.AppConfig).
func validateSettingsTabFieldKeys() []string {
	var missing []string
	for tab, keys := range settingsTabFieldKeys {
		for _, k := range keys {
			if k == "" {
				continue
			}
			if _, blocked := settingsTabBlockedKeys[k]; blocked {
				continue
			}
			if _, ok := appConfigFieldIndex(k); !ok {
				missing = append(missing, tab+"."+k)
			}
		}
	}
	return missing
}
