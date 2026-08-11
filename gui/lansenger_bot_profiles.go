package main

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
)

const lansengerBotProfileMaxDirectories = 64

// LansengerBotProfileView is the safe settings payload for one bot. Credentials
// are deliberately not returned to the frontend; SecretConfigured lets the UI
// retain a useful status without exposing a stored App Secret.
type LansengerBotProfileView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	AppID      string `json:"app_id"`
	GatewayURL string `json:"gateway_url,omitempty"`
	WSSURL     string `json:"wss_url,omitempty"`

	AssistantMode       string   `json:"assistant_mode"`
	ExpertID            string   `json:"expert_id,omitempty"`
	InitialPrompt       string   `json:"initial_prompt,omitempty"`
	WorkingDirectory    string   `json:"working_directory,omitempty"`
	DocumentDirectories []string `json:"document_directories,omitempty"`
	KnowledgeSourceIDs  []string `json:"knowledge_source_ids,omitempty"`

	GroupPolicy         string                    `json:"group_policy,omitempty"`
	AllowedGroupIDs     []string                  `json:"allowed_group_ids,omitempty"`
	IgnoredGroupIDs     []string                  `json:"ignored_group_ids,omitempty"`
	GroupFileMaxBytes   map[string]int64          `json:"group_file_max_bytes,omitempty"`
	RequireMention      *bool                     `json:"require_mention,omitempty"`
	RespondToAtAll      bool                      `json:"respond_to_at_all,omitempty"`
	AutoMentionReply    *bool                     `json:"auto_mention_reply,omitempty"`
	AutoQuoteReply      bool                      `json:"auto_quote_reply,omitempty"`
	AllowWebSearch      bool                      `json:"allow_web_search,omitempty"`
	AllowAllDirectories bool                      `json:"allow_all_directories,omitempty"`
	AllowedDirectories  []string                  `json:"allowed_directories,omitempty"`
	AnswerCache         corelib.AnswerCacheConfig `json:"answer_cache,omitempty"`

	// Status is the effective operating state, not only the transport state.
	// An expert-bound bot whose definition has been deleted remains connected to
	// Lansenger but must be visibly marked degraded because it rejects new work.
	Status       string `json:"status,omitempty"`
	StatusReason string `json:"status_reason,omitempty"`

	SecretConfigured bool `json:"secret_configured"`
}

func lansengerBotProfileView(profile corelib.LansengerBotProfile) LansengerBotProfileView {
	profile = cloneLansengerBotProfile(profile)
	view := LansengerBotProfileView{
		ID: profile.ID, Name: profile.Name, Enabled: profile.Enabled, AppID: profile.AppID,
		GatewayURL: profile.GatewayURL, WSSURL: profile.WSSURL,
		AssistantMode: profile.EffectiveAssistantMode(), ExpertID: profile.ExpertID,
		InitialPrompt: profile.InitialPrompt, WorkingDirectory: profile.WorkingDirectory,
		DocumentDirectories: profile.DocumentDirectories, KnowledgeSourceIDs: profile.KnowledgeSourceIDs,
		GroupPolicy: profile.GroupPolicy, AllowedGroupIDs: profile.AllowedGroupIDs, IgnoredGroupIDs: profile.IgnoredGroupIDs,
		GroupFileMaxBytes: profile.GroupFileMaxBytes, RequireMention: profile.RequireMention,
		RespondToAtAll: profile.RespondToAtAll, AutoMentionReply: profile.AutoMentionReply,
		AutoQuoteReply: profile.AutoQuoteReply, AllowWebSearch: profile.AllowWebSearch,
		AllowAllDirectories: profile.AllowAllDirectories, AllowedDirectories: profile.AllowedDirectories,
		AnswerCache:      profile.EffectiveAnswerCache(),
		SecretConfigured: strings.TrimSpace(profile.AppSecret) != "",
	}
	if profile.EffectiveAssistantMode() == corelib.LansengerAssistantModeExpert && loadExpertDefByID(profile.ExpertID) == nil {
		view.Status = "degraded"
		view.StatusReason = unavailableAssistantBindingExpertMessage
	}
	return view
}

// ListLansengerBots returns every independently configured Lansenger bot,
// sorted by its stable ID. It never returns stored credentials.
func (a *App) ListLansengerBots() ([]LansengerBotProfileView, error) {
	if err := a.ensureLansengerBotProfilesMigrated(); err != nil {
		return nil, err
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, err
	}
	profiles := make([]LansengerBotProfileView, 0, len(cfg.LansengerBots))
	for _, profile := range cfg.LansengerBots {
		profiles = append(profiles, lansengerBotProfileView(profile))
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return profiles, nil
}

// SaveLansengerBot creates or replaces a profile and immediately reconciles its
// independent gateway/agent runtime. An empty AppSecret on an existing profile
// means "keep the stored secret", so settings forms never need to read it back.
func (a *App) SaveLansengerBot(profile corelib.LansengerBotProfile) (LansengerBotProfileView, error) {
	profile, err := normalizeLansengerBotProfile(profile)
	if err != nil {
		return LansengerBotProfileView{}, err
	}
	if err := a.ensureLansengerBotProfilesMigrated(); err != nil {
		return LansengerBotProfileView{}, err
	}
	current, err := a.LoadConfig()
	if err != nil {
		return LansengerBotProfileView{}, err
	}
	for _, existing := range current.LansengerBots {
		if existing.ID != profile.ID {
			continue
		}
		if profile.AppSecret == "" {
			profile.AppSecret = existing.AppSecret
		}
		// Older frontends do not send this nested setting. Preserve it rather
		// than silently replacing an existing cache policy on a bot edit.
		if profile.AnswerCache == nil {
			profile.AnswerCache = cloneLansengerBotProfile(existing).AnswerCache
		}
		break
	}
	if profile.Enabled && (profile.AppID == "" || profile.AppSecret == "") {
		return LansengerBotProfileView{}, fmt.Errorf("enabled Lansenger bot requires an App ID and App Secret")
	}
	if profile.Enabled {
		for _, existing := range current.LansengerBots {
			if existing.ID != profile.ID && existing.Enabled && strings.EqualFold(strings.TrimSpace(existing.AppID), profile.AppID) {
				return LansengerBotProfileView{}, fmt.Errorf("another enabled Lansenger bot already uses this App ID")
			}
		}
	}

	var saved corelib.LansengerBotProfile
	err = a.PatchConfig(func(cfg *corelib.AppConfig) {
		for i := range cfg.LansengerBots {
			if cfg.LansengerBots[i].ID != profile.ID {
				continue
			}
			if profile.AppSecret == "" {
				profile.AppSecret = cfg.LansengerBots[i].AppSecret
			}
			cfg.LansengerBots[i] = cloneLansengerBotProfile(profile)
			saved = cloneLansengerBotProfile(profile)
			return
		}
		cfg.LansengerBotsMigrated = true
		cfg.LansengerBots = append(cfg.LansengerBots, cloneLansengerBotProfile(profile))
		saved = cloneLansengerBotProfile(profile)
	})
	if err != nil {
		return LansengerBotProfileView{}, err
	}
	a.ensureLansengerGateway()
	return lansengerBotProfileView(saved), nil
}

// DeleteLansengerBot removes a bot binding and stops its queue/runtime during
// the subsequent registry reconciliation. Audit history and watch jobs remain
// intact so a later re-created profile with the same ID can resume its rules.
func (a *App) DeleteLansengerBot(botID string) error {
	botID = strings.TrimSpace(botID)
	if !validLansengerBotProfileID(botID) {
		return fmt.Errorf("invalid Lansenger bot id")
	}
	if err := a.ensureLansengerBotProfilesMigrated(); err != nil {
		return err
	}
	changed, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		for i := range cfg.LansengerBots {
			if cfg.LansengerBots[i].ID != botID {
				continue
			}
			cfg.LansengerBots = append(cfg.LansengerBots[:i], cfg.LansengerBots[i+1:]...)
			return true
		}
		return false
	})
	if err != nil {
		return err
	}
	if !changed {
		return fmt.Errorf("Lansenger bot %q was not found", botID)
	}
	a.ensureLansengerGateway()
	return nil
}

func (a *App) ensureLansengerBotProfilesMigrated() error {
	cfg, err := a.LoadConfig()
	if err != nil || cfg.LansengerBotsMigrated {
		return err
	}
	return a.PatchConfig(func(next *corelib.AppConfig) {
		corelib.ApplyLansengerMultiBotMigration(next)
	})
}

func normalizeLansengerBotProfile(profile corelib.LansengerBotProfile) (corelib.LansengerBotProfile, error) {
	profile = cloneLansengerBotProfile(profile)
	profile.ID = strings.TrimSpace(profile.ID)
	if !validLansengerBotProfileID(profile.ID) {
		return corelib.LansengerBotProfile{}, fmt.Errorf("invalid Lansenger bot id: use 1-128 letters, digits, dots, underscores, or hyphens")
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" {
		profile.Name = profile.ID
	}
	profile.AppID = strings.TrimSpace(profile.AppID)
	profile.AppSecret = strings.TrimSpace(profile.AppSecret)
	profile.GatewayURL = strings.TrimSpace(profile.GatewayURL)
	profile.WSSURL = strings.TrimSpace(profile.WSSURL)
	profile.ExpertID = strings.TrimSpace(profile.ExpertID)
	profile.InitialPrompt = strings.TrimSpace(profile.InitialPrompt)
	if profile.AnswerCache != nil {
		value := profile.AnswerCache.WithDefaults()
		profile.AnswerCache = &value
	}
	var err error
	if profile.WorkingDirectory, err = normalizeLansengerBotDirectory(profile.WorkingDirectory); err != nil {
		return corelib.LansengerBotProfile{}, fmt.Errorf("normalize working directory: %w", err)
	}
	if profile.DocumentDirectories, err = normalizeLansengerBotPathList(profile.DocumentDirectories); err != nil {
		return corelib.LansengerBotProfile{}, fmt.Errorf("normalize document directories: %w", err)
	}
	if profile.AllowedDirectories, err = normalizeLansengerBotPathList(profile.AllowedDirectories); err != nil {
		return corelib.LansengerBotProfile{}, fmt.Errorf("normalize allowed directories: %w", err)
	}
	// A document directory is useful only when the tool boundary permits the
	// agent to reach it. Make the relationship explicit in persisted policy so
	// both private and group turns have the same, auditable read boundary. The
	// working directory is also a tool base, so it must be authorized whenever
	// this profile does not intentionally allow all local directories.
	if !profile.AllowAllDirectories {
		effectiveAllowed := make([]string, 0, 1+len(profile.AllowedDirectories)+len(profile.DocumentDirectories))
		if profile.WorkingDirectory != "" {
			effectiveAllowed = append(effectiveAllowed, profile.WorkingDirectory)
		}
		effectiveAllowed = append(effectiveAllowed, profile.AllowedDirectories...)
		effectiveAllowed = append(effectiveAllowed, profile.DocumentDirectories...)
		profile.AllowedDirectories = deduplicateLansengerBotDirectories(effectiveAllowed)
	}
	profile.KnowledgeSourceIDs = normalizeLansengerBotStringList(profile.KnowledgeSourceIDs)
	profile.AllowedGroupIDs = normalizeLansengerBotStringList(profile.AllowedGroupIDs)
	profile.IgnoredGroupIDs = normalizeLansengerBotStringList(profile.IgnoredGroupIDs)
	if len(profile.DocumentDirectories) > lansengerBotProfileMaxDirectories || len(profile.AllowedDirectories) > lansengerBotProfileMaxDirectories {
		return corelib.LansengerBotProfile{}, fmt.Errorf("a Lansenger bot can have at most %d document or authorized directories", lansengerBotProfileMaxDirectories)
	}
	if profile.EffectiveAssistantMode() == corelib.LansengerAssistantModeExpert {
		profile.AssistantMode = corelib.LansengerAssistantModeExpert
		if profile.ExpertID == "" {
			return corelib.LansengerBotProfile{}, fmt.Errorf("an expert bot requires an expert id")
		}
		if loadExpertDefByID(profile.ExpertID) == nil {
			return corelib.LansengerBotProfile{}, fmt.Errorf("expert %q was not found", profile.ExpertID)
		}
	} else {
		profile.AssistantMode = corelib.LansengerAssistantModeGeneral
		profile.ExpertID = ""
	}
	switch strings.ToLower(strings.TrimSpace(profile.GroupPolicy)) {
	case "", "open":
		profile.GroupPolicy = "open"
	case "allow", "allowlist", "whitelist":
		profile.GroupPolicy = "allowlist"
	case "disabled", "off", "none":
		profile.GroupPolicy = "disabled"
	default:
		return corelib.LansengerBotProfile{}, fmt.Errorf("invalid Lansenger group policy")
	}
	return profile, nil
}

func validLansengerBotProfileID(value string) bool {
	return expertIDPattern.MatchString(value)
}

func normalizeLansengerBotPathList(values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized, err := normalizeLansengerBotDirectory(value)
		if err != nil {
			return nil, err
		}
		if normalized != "" {
			result = append(result, normalized)
		}
	}
	return deduplicateLansengerBotDirectories(result), nil
}

// normalizeLansengerBotDirectory makes saved profile paths independent from
// the application process's current directory. Existing directories are
// symlink-resolved; a missing directory stays as a clean absolute path so a
// temporarily disconnected drive can be configured and diagnosed later.
func normalizeLansengerBotDirectory(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(corelib.EffectiveWorkspaceDir(), value)
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	abs = filepath.Clean(abs)
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		return "", fmt.Errorf("filesystem root is too broad for a bot profile")
	}
	return abs, nil
}

func deduplicateLansengerBotDirectories(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(filepath.ToSlash(filepath.Clean(value)))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeLansengerBotStringList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(strings.ReplaceAll(value, `\\`, "/"))
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func cloneLansengerBotGroupFileLimits(in map[string]int64) map[string]int64 {
	if in == nil {
		return nil
	}
	out := make(map[string]int64, len(in))
	for groupID, maxBytes := range in {
		out[groupID] = maxBytes
	}
	return out
}

func lansengerBotProfileFromConfig(cfg corelib.AppConfig, botID string) (corelib.LansengerBotProfile, bool) {
	botID = strings.TrimSpace(botID)
	for _, profile := range cfg.LansengerBots {
		if profile.ID == botID {
			return cloneLansengerBotProfile(profile), true
		}
	}
	// Compatibility for callers that invoke a profile-aware operation before
	// startup has persisted the one-time legacy migration.
	if botID == corelib.DefaultLansengerBotProfileID && len(cfg.LansengerBots) == 0 {
		return corelib.LansengerBotProfile{
			ID: corelib.DefaultLansengerBotProfileID, Enabled: cfg.LansengerEnabled,
			AppID: cfg.LansengerAppID, AppSecret: cfg.LansengerAppSecret,
			GatewayURL: cfg.LansengerGatewayURL, WSSURL: cfg.LansengerWSSURL,
			GroupPolicy: cfg.LansengerGroupPolicy, AllowedGroupIDs: append([]string(nil), cfg.LansengerAllowedGroupIDs...),
			IgnoredGroupIDs: append([]string(nil), cfg.LansengerIgnoredGroupIDs...), GroupFileMaxBytes: cloneLansengerBotGroupFileLimits(cfg.LansengerGroupFileMaxBytes),
			AnswerCache: corelibAnswerCacheConfigPtr(cfg.AnswerCache.WithDefaults()),
		}, true
	}
	return corelib.LansengerBotProfile{}, false
}

func corelibAnswerCacheConfigPtr(value corelib.AnswerCacheConfig) *corelib.AnswerCacheConfig {
	return &value
}

func (a *App) updateLansengerBotProfile(botID string, update func(*corelib.LansengerBotProfile)) error {
	botID = strings.TrimSpace(botID)
	if !validLansengerBotProfileID(botID) {
		return fmt.Errorf("invalid Lansenger bot id")
	}
	if err := a.ensureLansengerBotProfilesMigrated(); err != nil {
		return err
	}
	changed, err := a.PatchConfigIfChanged(func(cfg *corelib.AppConfig) bool {
		for i := range cfg.LansengerBots {
			if cfg.LansengerBots[i].ID != botID {
				continue
			}
			before := cloneLansengerBotProfile(cfg.LansengerBots[i])
			update(&cfg.LansengerBots[i])
			return !reflect.DeepEqual(before, cfg.LansengerBots[i])
		}
		// An unconfigured legacy channel is intentionally not promoted into a
		// runnable profile during migration. Keep the old default-bot group APIs
		// useful in that state: persist their settings in the legacy fields until
		// credentials are supplied and a real default profile can be created.
		if botID == corelib.DefaultLansengerBotProfileID && len(cfg.LansengerBots) == 0 {
			legacy, ok := lansengerBotProfileFromConfig(*cfg, botID)
			if !ok {
				return false
			}
			before := cloneLansengerBotProfile(legacy)
			update(&legacy)
			if reflect.DeepEqual(before, legacy) {
				return false
			}
			cfg.LansengerGroupPolicy = legacy.GroupPolicy
			cfg.LansengerAllowedGroupIDs = append([]string(nil), legacy.AllowedGroupIDs...)
			cfg.LansengerIgnoredGroupIDs = append([]string(nil), legacy.IgnoredGroupIDs...)
			cfg.LansengerGroupFileMaxBytes = cloneLansengerBotGroupFileLimits(legacy.GroupFileMaxBytes)
			return true
		}
		return false
	})
	if err != nil {
		return err
	}
	if !changed {
		cfg, loadErr := a.LoadConfig()
		if loadErr != nil {
			return loadErr
		}
		if _, exists := lansengerBotProfileFromConfig(cfg, botID); !exists {
			return fmt.Errorf("Lansenger bot %q was not found", botID)
		}
		return nil
	}
	a.ensureLansengerGateway()
	return nil
}

func updateLansengerBotGroupIDList(current []string, groupID string, include bool) []string {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return current
	}
	result := make([]string, 0, len(current)+1)
	found := false
	for _, id := range current {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if id == groupID {
			found = true
			if include {
				result = append(result, id)
			}
			continue
		}
		result = append(result, id)
	}
	if include && !found {
		result = append(result, groupID)
	}
	return result
}

func lansengerBotProfileGroupIgnored(profile corelib.LansengerBotProfile, groupID string) bool {
	return lansengerBotProfileGroupListContains(profile.IgnoredGroupIDs, groupID)
}

func lansengerBotProfileGroupAllowed(profile corelib.LansengerBotProfile, groupID string) bool {
	return lansengerBotProfileGroupListContains(profile.AllowedGroupIDs, groupID)
}

func lansengerBotProfileGroupListContains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func lansengerBotProfileGroupFileLimit(profile corelib.LansengerBotProfile, groupID string) int64 {
	if profile.GroupFileMaxBytes == nil {
		return 0
	}
	limit := profile.GroupFileMaxBytes[strings.TrimSpace(groupID)]
	if limit < 0 {
		return 0
	}
	return limit
}
