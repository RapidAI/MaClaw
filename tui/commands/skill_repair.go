package commands

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// tuiRepairHTTPClient is a shared HTTP client for skill repair LLM calls.
var tuiRepairHTTPClient = &http.Client{Timeout: 60 * time.Second}

// tuiSkillRepairer adapts the TUI's LLM calling to skill.LLMRepairer.
type tuiSkillRepairer struct {
	cfg corelib.MaclawLLMConfig
}

// NewTUISkillRepairer builds an LLMRepairer from a resolved MaclawLLMConfig.
func NewTUISkillRepairer(cfg corelib.MaclawLLMConfig) skill.LLMRepairer {
	return &tuiSkillRepairer{cfg: cfg}
}

// NewTUISkillRepairerFromAppConfig resolves provider credentials then builds a repairer.
func NewTUISkillRepairerFromAppConfig(cfg corelib.AppConfig) skill.LLMRepairer {
	return NewTUISkillRepairer(repairLLMConfigFromAppConfig(cfg))
}

func (r *tuiSkillRepairer) ChatCall(messages []map[string]string) (string, error) {
	ifaces := make([]interface{}, len(messages))
	for i, m := range messages {
		ifaces[i] = m
	}
	resp, err := agent.DoSimpleLLMRequest(r.cfg, ifaces, tuiRepairHTTPClient, 60*time.Second)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (r *tuiSkillRepairer) IsConfigured() bool {
	return strings.TrimSpace(r.cfg.URL) != "" && (strings.TrimSpace(r.cfg.Key) != "" || strings.TrimSpace(r.cfg.Model) != "")
}

// ConfigStore abstracts config load/save for skill repair.
type ConfigStore interface {
	LoadConfig() (corelib.AppConfig, error)
	SaveConfig(cfg corelib.AppConfig) error
}

// maybeRepairSkillTUI checks if a skill is eligible for self-repair and
// attempts an LLM-driven repair. Runs in the background to avoid blocking.
func maybeRepairSkillTUI(entry *corelib.NLSkillEntry, cfg corelib.AppConfig, store ConfigStore) {
	MaybeRepairSkillTUI(entry, cfg, store)
}

// MaybeRepairSkillTUI checks if a skill is eligible for self-repair and
// attempts an LLM-driven repair in the background.
func MaybeRepairSkillTUI(entry *corelib.NLSkillEntry, cfg corelib.AppConfig, store ConfigStore) bool {
	if !CanStartTUISkillRepair(entry, cfg) {
		return false
	}
	go PerformTUISkillRepair(entry, cfg, store)
	return true
}

// CanStartTUISkillRepair reports whether entry is eligible and LLM is configured.
func CanStartTUISkillRepair(entry *corelib.NLSkillEntry, cfg corelib.AppConfig) bool {
	if entry == nil {
		return false
	}
	if skill.IsFileBackedSkill(*entry) {
		log.Printf("[skill-repair-tui] skipped file-backed skill %q; repair requires reviewed patch flow", entry.Name)
		return false
	}
	if !skill.ShouldAttemptRepair(entry) {
		return false
	}
	repairer := &tuiSkillRepairer{cfg: repairLLMConfigFromAppConfig(cfg)}
	return repairer.IsConfigured()
}

// PerformTUISkillRepair runs LLM self-repair synchronously (caller may be an
// EvolutionPipeline worker or a background goroutine from MaybeRepairSkillTUI).
func PerformTUISkillRepair(entry *corelib.NLSkillEntry, cfg corelib.AppConfig, store ConfigStore) {
	PerformTUISkillRepairWithForce(entry, cfg, store, false)
}

// PerformTUISkillRepairWithForce is like PerformTUISkillRepair but force=true
// allows CanForceAttemptRepair when usage-rate thresholds are not met.
func PerformTUISkillRepairWithForce(entry *corelib.NLSkillEntry, cfg corelib.AppConfig, store ConfigStore, force bool) {
	if entry == nil {
		return
	}
	if skill.IsFileBackedSkill(*entry) {
		return
	}
	ok := skill.ShouldAttemptRepair(entry)
	if !ok && force {
		ok = skill.CanForceAttemptRepair(entry)
	}
	if !ok {
		return
	}
	llmCfg := repairLLMConfigFromAppConfig(cfg)
	repairer := &tuiSkillRepairer{cfg: llmCfg}
	if !repairer.IsConfigured() {
		return
	}

	log.Printf("[skill-repair-tui] attempting repair for %q", entry.Name)
	// Param contract (DeclaredParams vs actual args) is filled by NewRepairContext.
	// TUI path may not have recent run args; schema still helps the repair LLM.
	repairCtx := skill.NewRepairContext(entry, nil)
	result, err := skill.AttemptRepairWithContext(repairer, entry, repairCtx)
	if err != nil {
		log.Printf("[skill-repair-tui] repair failed for %q: %v", entry.Name, err)
		return
	}
	if result == nil {
		log.Printf("[skill-repair-tui] repair returned nil result for %q", entry.Name)
		return
	}
	applied := skill.ApplyRepair(entry, result)
	if applied || result.ShouldDisable {
		if err := persistTUISkillRepairResult(store, entry); err != nil {
			log.Printf("[skill-repair-tui] persist repair result for %q failed: %v", entry.Name, err)
			return
		}
		if result.ShouldDisable {
			log.Printf("[skill-repair-tui] marked skill %q as needs_review", entry.Name)
		} else {
			log.Printf("[skill-repair-tui] repaired skill %q", entry.Name)
		}
	}
}

func persistTUISkillRepairResult(store ConfigStore, entry *corelib.NLSkillEntry) error {
	if store == nil || entry == nil {
		return nil
	}
	freshCfg, err := store.LoadConfig()
	if err != nil {
		return err
	}
	for i := range freshCfg.NLSkills {
		if freshCfg.NLSkills[i].MatchesName(entry.Name) {
			freshCfg.NLSkills[i].Steps = entry.Steps
			freshCfg.NLSkills[i].Status = entry.Status
			freshCfg.NLSkills[i].LastError = entry.LastError
			freshCfg.NLSkills[i].RepairAttemptCount = entry.RepairAttemptCount
			freshCfg.NLSkills[i].LastRepairAt = entry.LastRepairAt
			freshCfg.NLSkills[i].RepairHistory = append([]corelib.SkillRepairRecord(nil), entry.RepairHistory...)
			return store.SaveConfig(freshCfg)
		}
	}
	return store.SaveConfig(freshCfg)
}

func repairLLMConfigFromAppConfig(cfg corelib.AppConfig) corelib.MaclawLLMConfig {
	llmCfg := corelib.MaclawLLMConfig{
		URL:           strings.TrimSpace(cfg.MaclawLLMUrl),
		Key:           strings.TrimSpace(cfg.MaclawLLMKey),
		Model:         strings.TrimSpace(cfg.MaclawLLMModel),
		Protocol:      strings.TrimSpace(cfg.MaclawLLMProtocol),
		ContextLength: cfg.MaclawLLMContextLength,
		TimeoutSec:    cfg.MaclawLLMTimeoutSec,
		ProviderName:  strings.TrimSpace(cfg.MaclawLLMCurrentProvider),
		ThinkingMode:  cfg.MaclawLLMThinkingMode,
	}
	current := strings.TrimSpace(cfg.MaclawLLMCurrentProvider)
	matchedProvider := false
	for _, p := range cfg.MaclawLLMProviders {
		if current != "" && p.Name != current {
			continue
		}
		applyRepairLLMProvider(&llmCfg, p)
		matchedProvider = true
		break
	}
	if !matchedProvider && len(cfg.MaclawLLMProviders) > 0 && llmCfg.URL == "" && llmCfg.Model == "" {
		applyRepairLLMProvider(&llmCfg, cfg.MaclawLLMProviders[0])
	}
	if llmCfg.Key == "" && strings.TrimSpace(cfg.RemoteViewerToken) != "" {
		llmCfg.Key = strings.TrimSpace(cfg.RemoteViewerToken)
	}
	llmCfg.Model = corelib.MigrateZhipuCodingModel(llmCfg.ProviderName, llmCfg.Model)
	return llmCfg
}

func applyRepairLLMProvider(llmCfg *corelib.MaclawLLMConfig, p corelib.MaclawLLMProvider) {
	if llmCfg.URL == "" {
		llmCfg.URL = strings.TrimSpace(p.URL)
	}
	if token := p.CodexSubscriptionOAuthToken(); token != "" {
		llmCfg.Key = token
	} else if llmCfg.Key == "" {
		llmCfg.Key = strings.TrimSpace(p.Key)
	}
	if llmCfg.Model == "" {
		llmCfg.Model = strings.TrimSpace(p.Model)
	}
	if llmCfg.Protocol == "" {
		llmCfg.Protocol = strings.TrimSpace(p.Protocol)
	}
	if llmCfg.ContextLength == 0 {
		llmCfg.ContextLength = p.ContextLength
	}
	if llmCfg.TimeoutSec == 0 {
		llmCfg.TimeoutSec = p.TimeoutSec
	}
	llmCfg.AgentType = p.AgentType
	llmCfg.SupportsVision = p.SupportsVision
	llmCfg.WireAPI = p.WireAPI
	llmCfg.ProviderName = strings.TrimSpace(p.Name)
}
